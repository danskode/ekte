.PHONY: test vet fix build install

test:
	go test -v -race ./...

vet:
	go vet ./...

fix:
	bash scripts/test-fix.sh

build:
	go build ./cmd/ekte/

install:
	go install ./cmd/ekte/
