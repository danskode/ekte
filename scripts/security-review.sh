#!/usr/bin/env bash
set -euo pipefail

cd "$(git rev-parse --show-toplevel)"

RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
NC='\033[0m'

FULL_REVIEW=false
[ "${1:-}" = "--full" ] && FULL_REVIEW=true

if ! command -v claude &>/dev/null; then
  echo -e "${RED}Fejl: 'claude' CLI ikke fundet.${NC}"
  exit 1
fi

if $FULL_REVIEW; then
  FILE_COUNT=$(find . -name "*.go" -not -path "*/vendor/*" | wc -l | tr -d ' ')
  CODE_BYTES=$(find . -name "*.go" -not -path "*/vendor/*" -print0 | xargs -0 cat | wc -c)
  if [ "$CODE_BYTES" -gt 524288 ]; then
    echo -e "${YELLOW}Kodebase > 512 KB — brug 'git push' (diff-review) i stedet.${NC}"
    exit 1
  fi
  echo "Indlæser $FILE_COUNT Go-filer til security review..."
  CODE=$(find . -name "*.go" -not -path "*/vendor/*" -print0 | sort -z | while IFS= read -r -d '' f; do
    printf '=== %s ===\n' "$f"
    cat "$f"
    printf '\n'
  done)
  CONTEXT="Hele Go-kodebasen ($FILE_COUNT filer)"
else
  UPSTREAM=$(git rev-parse --abbrev-ref --symbolic-full-name "@{u}" 2>/dev/null || echo "")
  if [ -z "$UPSTREAM" ]; then
    if git show-ref --quiet "refs/remotes/origin/main"; then
      UPSTREAM="origin/main"
    elif git show-ref --quiet "refs/remotes/origin/master"; then
      UPSTREAM="origin/master"
    else
      echo "Ingen upstream fundet — kør 'make security' for fuld review."
      exit 0
    fi
  fi
  UPSTREAM=$(printf '%s' "$UPSTREAM" | tr -cd '[:alnum:]/_.-')
  if [ -z "$UPSTREAM" ]; then
    echo "Ugyldig upstream efter sanitering."
    exit 1
  fi
  CODE=$(git diff "$UPSTREAM"..HEAD)
  if [ -z "$CODE" ]; then
    echo -e "${GREEN}Ingen upushede ændringer.${NC}"
    exit 0
  fi
  COMMIT_COUNT=$(git rev-list "$UPSTREAM"..HEAD --count || echo "?")
  CONTEXT="Git diff af $COMMIT_COUNT upushede commits mod $UPSTREAM"
  echo "Reviewe $COMMIT_COUNT upushede commits..."
fi

# Prompt skrives til tempfil og sendes via stdin (undgår CWE-214/procesarg-eksponering).
TMPFILE=$(mktemp)
ERRFILE="${TMPFILE}.err"
trap 'rm -f "$TMPFILE" "$ERRFILE"' EXIT

# Escape </code> i kodeindhold så det ikke bryder XML-afgrænseren i prompten.
SAFE_CODE=$(printf '%s' "$CODE" | sed 's|</code>|<\\/code>|g')

printf '%s\n' "Du er en Go-sikkerhedsekspert (OWASP Top 10, CWE). Analyser følgende kode." > "$TMPFILE"
printf '\nKontekst: %s\n' "$CONTEXT" >> "$TMPFILE"
printf '\n<code>\n' >> "$TMPFILE"
printf '%s\n' "$SAFE_CODE" >> "$TMPFILE"
printf '</code>\n\n' >> "$TMPFILE"
cat >> "$TMPFILE" <<'STATIC'
Returner KUN valid JSON uden markdown-wrapper:
{
  "risk_level": "low|medium|high|critical",
  "findings": [
    {
      "severity": "low|medium|high|critical",
      "file": "sti/til/fil.go",
      "issue": "hvad problemet er",
      "recommendation": "specifik rettelse"
    }
  ],
  "summary": "kort samlet vurdering på dansk"
}

Ingen fund → findings tom liste, risk_level "low".
STATIC

RESPONSE=$(claude --print < "$TMPFILE" 2>"$ERRFILE")
if [ -s "$ERRFILE" ]; then
  echo -e "${YELLOW}Claude fejl:${NC}" >&2
  cat "$ERRFILE" >&2
fi

# Strip eventuelle markdown-code-fences og ANSI-sekvenser fra svaret.
RESPONSE=$(printf '%s' "$RESPONSE" | python3 -c "
import sys, re
content = sys.stdin.read().strip()
content = re.sub(r'^[ \t]*\`\`\`(?:json)?\s*\n?', '', content)
content = re.sub(r'\n?\`\`\`\s*$', '', content.strip())
# Strip ANSI escape-sekvenser (terminal injection)
content = re.sub(r'\x1b\[[0-9;]*[A-Za-z]', '', content)
print(content, end='')
")

if ! printf '%s' "$RESPONSE" | python3 -c "import sys,json; json.load(sys.stdin)" 2>/dev/null; then
  echo -e "${YELLOW}Uventet svar fra Claude:${NC}"
  printf '%s\n' "$RESPONSE"
  exit 1
fi

RISK=$(printf '%s' "$RESPONSE" | python3 -c "import sys,json; print(json.load(sys.stdin)['risk_level'])")
SUMMARY=$(printf '%s' "$RESPONSE" | python3 -c "import sys,json; print(json.load(sys.stdin)['summary'])")
FINDING_COUNT=$(printf '%s' "$RESPONSE" | python3 -c "import sys,json; print(len(json.load(sys.stdin)['findings']))")

echo ""
case "$RISK" in
  critical) echo -e "${RED}━━━ SECURITY: KRITISK — $FINDING_COUNT fund ━━━━━━━━━━━━━━━${NC}" ;;
  high)     echo -e "${RED}━━━ SECURITY: HØJ RISIKO — $FINDING_COUNT fund ━━━━━━━━━━━━${NC}" ;;
  medium)   echo -e "${YELLOW}━━━ SECURITY: MEDIUM RISIKO — $FINDING_COUNT fund ━━━━━━━━${NC}" ;;
  *)        echo -e "${GREEN}━━━ SECURITY: LAV RISIKO — $FINDING_COUNT fund ━━━━━━━━━━━${NC}" ;;
esac
printf '%s\n' "$SUMMARY"
echo ""

if [ "$FINDING_COUNT" -gt 0 ]; then
  printf '%s' "$RESPONSE" | python3 -c "
import sys, json, re

def sanitize(s):
    return re.sub(r'\x1b\[[0-9;]*[A-Za-z]', '', str(s))

data = json.load(sys.stdin)
SEV_COLOR = {
  'critical': '\033[0;31m',
  'high':     '\033[0;31m',
  'medium':   '\033[1;33m',
  'low':      '\033[0;36m',
}
RESET = '\033[0m'
for f in data['findings']:
    sev = f.get('severity', 'low')
    c = SEV_COLOR.get(sev, '')
    print(f\"{c}[{sev.upper()}] {sanitize(f.get('file', '?'))}{RESET}\")
    print(f\"  Problem: {sanitize(f.get('issue', ''))}\")
    print(f\"  Fix:     {sanitize(f.get('recommendation', ''))}\")
    print()
"
fi

# Pre-push mode: bloker ved medium+ fund
if ! $FULL_REVIEW && [[ "$RISK" =~ ^(medium|high|critical)$ ]]; then
  if [ -c /dev/tty ]; then
    read -rp "Push alligevel? [j/N] " CONFIRM </dev/tty
    if [[ ! "$CONFIRM" =~ ^[jJ]$ ]]; then
      echo "Push afbrudt."
      exit 1
    fi
  else
    echo "Ingen terminal tilgængelig og medium+ risiko — push afbrudt. Kør 'make security' og push manuelt."
    exit 1
  fi
fi
