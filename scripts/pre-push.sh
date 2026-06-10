#!/usr/bin/env bash
# Pre-push: vet → tests (race) → sikkerhedsreview. Fejler hårdt ved første fund.
set -euo pipefail

cd "$(git rev-parse --show-toplevel)"

RED='\033[0;31m'
GREEN='\033[0;32m'
NC='\033[0m'

echo "→ go vet ./..."
if ! go vet ./...; then
  echo -e "${RED}✗ go vet fejlede — push afbrudt.${NC}"
  exit 1
fi

echo "→ go test -race ./..."
if ! go test -race ./...; then
  echo -e "${RED}✗ Tests fejlede — push afbrudt.${NC}"
  exit 1
fi
echo -e "${GREEN}✓ vet og tests grønne${NC}"

bash scripts/security-review.sh
