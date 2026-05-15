GOLANGCI_LINT := go run github.com/golangci/golangci-lint/v2/cmd/golangci-lint@v2.12.1

PI_SMOKE_SCRIPT := ./scripts/pi-smoke.sh

.PHONY: fmt lint smoke smoke-pi test

fmt:
	$(GOLANGCI_LINT) fmt

lint:
	$(GOLANGCI_LINT) run

smoke:
	go run ./cmd/agentlab --prompt 'Search the sandbox for aurora, then tell me the reported status.'

smoke-pi:
	$(PI_SMOKE_SCRIPT)

test:
	go test ./...
