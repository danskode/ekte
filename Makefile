.PHONY: test vet fix security install-hooks setup build install

test:
	go test -v -race ./...

vet:
	go vet ./...

fix:
	bash scripts/test-fix.sh

security:
	bash scripts/security-review.sh --full

setup: install-hooks
	@echo "Dev-miljø klar."

install-hooks:
	@printf '#!/bin/bash\nbash "$$(git rev-parse --show-toplevel)/scripts/security-review.sh"\n' > .git/hooks/pre-push
	@chmod +x .git/hooks/pre-push
	@echo "Pre-push hook installeret."

build:
	go build ./cmd/ekte/

install:
	go install ./cmd/ekte/
