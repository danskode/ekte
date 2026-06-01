#!/usr/bin/env bash
set -euo pipefail

cd "$(git rev-parse --show-toplevel)"

RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
NC='\033[0m'

if ! command -v claude &>/dev/null; then
  echo -e "${RED}Fejl: 'claude' CLI ikke fundet.${NC}"
  echo "Installer med: npm install -g @anthropic-ai/claude-code"
  exit 1
fi

echo "Kører tests..."
if go test -v ./... 2>&1 | tee /tmp/ekte-test-output.txt; then
  echo -e "${GREEN}Alle tests grønne.${NC}"
  exit 0
fi

echo -e "${RED}Tests fejlede.${NC} Sender output til Claude..."

FAIL_OUTPUT=$(cat /tmp/ekte-test-output.txt)

RESPONSE=$(claude --print "Du er en Go-ekspert. Her er output fra mislykkede tests i projektet github.com/danskode/ekte:

<test-output>
${FAIL_OUTPUT}
</test-output>

Returner KUN valid JSON (ingen markdown-wrapper, ingen forklaring uden for JSON):
{
  \"strategy\": \"markdown: hvad fejler og hvorfor\",
  \"commands\": [\"sed -i 's/old/new/' fil.go\", \"gofmt -w fil.go\"]
}

commands: kun sed -i, gofmt -w, go mod tidy. Ingen rm, ingen git, ingen curl." 2>/dev/null)

if ! echo "$RESPONSE" | python3 -c "import sys,json; json.load(sys.stdin)" 2>/dev/null; then
  echo -e "${YELLOW}Claude returnerede ikke valid JSON. Her er svaret:${NC}"
  echo "$RESPONSE"
  exit 1
fi

STRATEGY=$(echo "$RESPONSE" | python3 -c "import sys,json; print(json.load(sys.stdin)['strategy'])")
COMMANDS=$(echo "$RESPONSE" | python3 -c "import sys,json; [print(c) for c in json.load(sys.stdin)['commands']]")

echo ""
echo -e "${YELLOW}━━━ AI-diagnose ━━━━━━━━━━━━━━━━━━━━━━━━${NC}"
echo "$STRATEGY"
echo ""

if [ -z "$COMMANDS" ]; then
  echo "Ingen automatiske rettelser foreslået."
  exit 1
fi

echo -e "${YELLOW}━━━ Foreslåede rettelser ━━━━━━━━━━━━━━━${NC}"
echo "$COMMANDS"
echo ""

read -rp "Anvend rettelser? [j/N] " CONFIRM
if [[ ! "$CONFIRM" =~ ^[jJ]$ ]]; then
  echo "Afbrudt."
  exit 1
fi

echo "Anvender rettelser..."
while IFS= read -r CMD; do
  [ -z "$CMD" ] && continue
  case "$CMD" in
    "sed -i"*|"gofmt -w"*|"go mod tidy"*|"go generate"*)
      echo "  → $CMD"
      bash -c "$CMD"
      ;;
    *)
      echo -e "  ${YELLOW}SKIPPED (ikke tilladt): $CMD${NC}"
      ;;
  esac
done <<< "$COMMANDS"

echo ""
echo "Kører tests igen..."
if go test -v ./... 2>&1 | tee /tmp/ekte-retest-output.txt; then
  echo -e "${GREEN}Tests grønne efter rettelse.${NC}"
else
  echo -e "${RED}Tests fejler stadig. Manuel inspektion nødvendig.${NC}"
  exit 1
fi
