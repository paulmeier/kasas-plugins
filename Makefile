.PHONY: help fmt vet test build validate index index-check check

help: ## Show this help.
	@grep -E '^[a-zA-Z_-]+:.*?## .*$$' $(MAKEFILE_LIST) | awk 'BEGIN{FS=":.*?## "}{printf "  \033[36m%-14s\033[0m %s\n", $$1, $$2}'

fmt: ## Format Go code.
	gofmt -w .

vet: ## Run go vet.
	go vet ./...

test: ## Run tests with the race detector.
	go test -race ./...

build: ## Build the kasas-plugins CLI.
	go build -o bin/kasas-plugins ./cmd/kasas-plugins

validate: ## Run the submission gate over every plugin.
	go run ./cmd/kasas-plugins validate

index: ## Regenerate registry/index.json from the plugins directory.
	go run ./cmd/kasas-plugins index

index-check: ## Verify the committed registry index is current (CI gate).
	go run ./cmd/kasas-plugins index --check

check: fmt vet test validate index-check ## Run everything CI runs.
