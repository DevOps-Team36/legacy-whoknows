.PHONY: run dev build test lint lint-fix swagger swagger-check clean docker-build docker-up docker-down deploy spinup spinup-app spinup-monitoring help

BINARY    := whoknows-server
MAIN      := ./cmd/server
DOCS_DIR  := docs
SWAGGER   := swagger.json

help: ## Show this help
	@grep -E '^[a-zA-Z_-]+:.*?## .*$$' $(MAKEFILE_LIST) | awk 'BEGIN {FS = ":.*?## "}; {printf "  \033[36m%-16s\033[0m %s\n", $$1, $$2}'

# ── Development ──────────────────────────────────────────────

run: ## Run the server locally
	go run $(MAIN)

dev: ## Run with hot reload (requires: go install github.com/air-verse/air@latest)
	air

build: ## Build the binary
	go build -o $(BINARY) $(MAIN)

test: ## Run all tests
	go test -v ./...

lint: ## Run golangci-lint
	golangci-lint run ./...

lint-fix: ## Run golangci-lint with auto-fix
	golangci-lint run --fix ./...

# ── Swagger / API spec ──────────────────────────────────────

swagger: ## Regenerate swagger docs (docs/)
	swag init -g $(MAIN)/main.go -o $(DOCS_DIR)

swagger-check: ## Validate generated spec matches reference openapi.json
	go test -v -run TestSpecMatchesReference ./internal/httpapi/

# ── Docker (local) ──────────────────────────────────────────

docker-build: ## Build Docker image locally
	docker compose -f deploy/docker-compose.server.yml build

docker-up: ## Start container locally
	docker compose -f deploy/docker-compose.server.yml up -d

docker-down: ## Stop container
	docker compose -f deploy/docker-compose.server.yml down

# ── Deploy ──────────────────────────────────────────────────

deploy: ## Deploy to production via Ansible
	ansible-playbook deploy/ansible/playbook.yml

deploy-check: ## Ansible dry-run (no changes)
	ansible-playbook deploy/ansible/playbook.yml --check

spinup: ## Spin up begge servere fra scratch (Terraform + DNS + Ansible)
	bash deploy/spinup.sh all

spinup-app: ## Spin up kun app-server (brug når kun huw.dk er nede)
	bash deploy/spinup.sh app

spinup-monitoring: ## Spin up kun monitoring-server (brug når monitor.huw.dk er nede)
	bash deploy/spinup.sh monitoring

# ── Maintenance ─────────────────────────────────────────────

tidy: ## Run go mod tidy
	go mod tidy

clean: ## Remove build artifacts
	rm -f $(BINARY) server.exe
