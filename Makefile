GOLANGCI_LINT := go run github.com/golangci/golangci-lint/v2/cmd/golangci-lint@v2.12.1

.PHONY: fmt lint test

fmt:
	$(GOLANGCI_LINT) fmt

lint:
	$(GOLANGCI_LINT) run

test:
	go test ./...
