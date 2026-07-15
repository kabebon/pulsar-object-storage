# Pulsar — developer commands.
# All targets callable from the repo root. Requires Go 1.26+, Docker optional.

SHELL := /bin/bash
GO    ?= go
APP   := pulsar

.PHONY: help tidy build run dev test fmt vet generate templates migrate-up migrate-down docker-up docker-up-prod docker-down docker-logs docker-build psql redis-cli minio-mc

help: ## Show this help
	@grep -E '^[a-zA-Z_-]+:.*?## .*$$' $(MAKEFILE_LIST) | awk 'BEGIN{FS=":.*?## "}{printf "  \033[36m%-18s\033[0m %s\n", $$1, $$2}'

tidy: ## go mod tidy
	$(GO) mod tidy

generate: templates ## Generate templ sources

templates: ## Render .templ -> .go
	$(GO) run github.com/a-h/templ/cmd/templ generate

build: templates ## Build the binary into ./bin/pulsar
	$(GO) build -trimpath -o bin/$(APP) ./cmd/pulsar

run: ## Run locally (requires postgres+redis+minio running)
	$(GO) run ./cmd/pulsar

dev: docker-up ## Up infra + run app with hot reload (assumes air installed)
	$(GO) run ./cmd/pulsar

test: ## Run the test suite
	$(GO) test -race -count=1 ./...

fmt: ## Format Go sources
	$(GO) fmt ./...
	@command -v gofumpt >/dev/null 2>&1 && gofumpt -w -l . || true

vet: ## go vet
	$(GO) vet ./...

migrate-up: ## Apply DB migrations
	$(GO) run ./cmd/pulsar -migrate

migrate-down: ## Roll back the last DB migration
	$(GO) run ./cmd/pulsar -migrate-down

# ----------------- Docker -----------------
# --env-file .env is REQUIRED: with `-f deploy/docker-compose.yml`, Compose
# otherwise reads .env for ${VAR} interpolation from the deploy/ directory
# (where no .env exists), so S3_PUBLIC_ENDPOINT/CORS_ALLOWED_ORIGINS/CDN_HOST
# fall back to their localhost defaults even though they are set in the root
# .env. env_file (container runtime) is unaffected; this fixes interpolation.

docker-up: ## Start the full stack via docker compose
	docker compose --env-file .env -f deploy/docker-compose.yml up -d --build

docker-up-prod: ## Start the full stack WITH Caddy (TLS) via the prod profile
	docker compose --env-file .env -f deploy/docker-compose.yml --profile prod up -d --build

docker-down: ## Stop the stack
	docker compose --env-file .env -f deploy/docker-compose.yml --profile prod down

docker-logs: ## Tail service logs
	docker compose --env-file .env -f deploy/docker-compose.yml logs -f --tail=200

docker-build: ## Build the app image only
	docker compose --env-file .env -f deploy/docker-compose.yml build pulsar

# ----------------- Inspect -----------------

psql: ## Connect to Postgres via psql
	@docker compose --env-file .env -f deploy/docker-compose.yml exec postgres psql -U pulsar -d pulsar

redis-cli: ## Connect to Redis via redis-cli
	@docker compose --env-file .env -f deploy/docker-compose.yml exec redis redis-cli

minio-mc: ## Open a shell with the MinIO client configured
	@docker run --rm -it --network=pulsar_default minio/mc sh -c "mc alias set local http://minio:9000 pulsar pulsar12345 && mc ls local/pulsar && sh"
