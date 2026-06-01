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

# Strip CSI- og OSC-ANSI-escape-sekvenser fra en streng
strip_ansi() {
  python3 -c "
import sys, re
content = sys.stdin.read()
content = re.sub(r'\x1b(?:\[[0-9;]*[A-Za-z]|\][^\x07\x1b]*(?:\x07|\x1b\\\\))', '', content)
print(content, end='')
"
}

if $FULL_REVIEW; then
  # -not -type l: følg ikke symbolske links ud af arbejdstræet
  FILE_COUNT=$(find . -not -type l -name "*.go" -not -path "*/vendor/*" | wc -l | tr -d ' ')
  CODE_BYTES=$(find . -not -type l -name "*.go" -not -path "*/vendor/*" -print0 | xargs -0 cat | wc -c)
  if [ "$CODE_BYTES" -gt 524288 ]; then
    echo -e "${YELLOW}Kodebase > 512 KB — brug 'git push' (diff-review) i stedet.${NC}"
    exit 1
  fi
  echo "Indlæser $FILE_COUNT Go-filer til security review..."
  CODE=$(find . -not -type l -name "*.go" -not -path "*/vendor/*" -print0 | sort -z | while IFS= read -r -d '' f; do
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

# Instruktioner i --system-prompt-file (betroet kanal).
# Kodeindhold sendes via stdin/user-turn (ikke-betroet kanal).
# Adskillelsen svarer til OWASP LLM01 "segregate external content"-princippet.
SYSTEM_PROMPT="$(git rev-parse --show-toplevel)/scripts/security-review-system.txt"
if [ ! -f "$SYSTEM_PROMPT" ]; then
  echo -e "${RED}Fejl: scripts/security-review-system.txt ikke fundet.${NC}"
  exit 1
fi

TMPFILE=$(mktemp)
ERRFILE=$(mktemp)
trap 'rm -f "$TMPFILE" "$ERRFILE"' EXIT

# HTML-entity-escape al bruger-kontrolleret indhold (CWE-77).
# Forhindrer at < eller > i kode/git-metadata kan bryde XML-afgrænsningen.
escape_xml() { sed 's|&|\&amp;|g; s|<|\&lt;|g; s|>|\&gt;|g'; }

SAFE_CODE=$(printf '%s' "$CODE" | escape_xml)
SAFE_CONTEXT=$(printf '%s' "$CONTEXT" | escape_xml)

printf '<untrusted-code>\n' > "$TMPFILE"
printf 'Kontekst: %s\n\n' "$SAFE_CONTEXT" >> "$TMPFILE"
printf '%s\n' "$SAFE_CODE" >> "$TMPFILE"
printf '</untrusted-code>\n' >> "$TMPFILE"

RESPONSE=$(claude --print --system-prompt-file "$SYSTEM_PROMPT" < "$TMPFILE" 2>"$ERRFILE")
if [ -s "$ERRFILE" ]; then
  echo -e "${YELLOW}Claude fejl:${NC}" >&2
  strip_ansi < "$ERRFILE" >&2
fi

# Strip markdown-code-fences og ANSI-sekvenser (CSI + OSC) fra svaret.
RESPONSE=$(printf '%s' "$RESPONSE" | python3 -c "
import sys, re
content = sys.stdin.read().strip()
content = re.sub(r'^[ \t]*\`\`\`(?:json)?\s*\n?', '', content)
content = re.sub(r'\n?\`\`\`\s*$', '', content.strip())
content = re.sub(r'\x1b(?:\[[0-9;]*[A-Za-z]|\][^\x07\x1b]*(?:\x07|\x1b\\\\))', '', content)
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
    return re.sub(r'\x1b(?:\[[0-9;]*[A-Za-z]|\][^\x07\x1b]*(?:\x07|\x1b\\\\))', '', str(s))

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
