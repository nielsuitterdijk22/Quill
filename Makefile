# Quill — developer tasks.
# Run `make help` for the list.

COMPOSE := docker compose -f deploy/compose/docker-compose.yml

# go.mod targets a newer Go than some hosts ship; let the toolchain auto-download
# if the local default is older so `go build`/`test`/air work without a manual SDK.
export GOTOOLCHAIN := auto

.DEFAULT_GOAL := help

.PHONY: help
help: ## Show this help
	@grep -E '^[a-zA-Z_-]+:.*?## .*$$' $(MAKEFILE_LIST) | sort | \
		awk 'BEGIN{FS=":.*?## "}{printf "  \033[36m%-14s\033[0m %s\n", $$1, $$2}'

## ---- local stack ----------------------------------------------------------

.PHONY: up
up: ## Dev stack: Postgres + Forgejo in Docker + hot-reload api, dispatch & web
	./scripts/dev-up.sh

.PHONY: stack
stack: ## Start the full containerised stack (Forgejo + Postgres + api + web, no hot reload)
	$(COMPOSE) up -d --build

.PHONY: down
down: ## Stop the local stack
	$(COMPOSE) down

.PHONY: logs
logs: ## Tail stack logs
	$(COMPOSE) logs -f

.PHONY: ps
ps: ## Show stack status
	$(COMPOSE) ps

## ---- forgejo --------------------------------------------------------------
.PHONY: forgejo
fj-run: ## Run Forgejo locally (http://localhost:3000)
	$(COMPOSE) up -d forgejo

## ---- backend --------------------------------------------------------------

.PHONY: be-run
be-run: ## Run the backend API locally
	cd backend && go run ./cmd/api

.PHONY: dispatch-run
dispatch-run: ## Run the pipeline dispatcher locally
	cd backend && go run ./cmd/dispatch

.PHONY: be-build
be-build: ## Build the backend
	cd backend && go build ./...

.PHONY: be-test
be-test: ## Run backend tests
	# -p 1 serialises package test binaries so the gated integration tests, which
	# share one Postgres database, don't race each other when a DB is configured.
	# Without QUILL_TEST_DATABASE_URL those tests skip and this is a no-op.
	cd backend && go test -p 1 ./...

.PHONY: be-fmt
be-fmt: ## Format Go code
	cd backend && gofmt -w .

.PHONY: be-vet
be-vet: ## Vet Go code
	cd backend && go vet ./...

## ---- frontend -------------------------------------------------------------

.PHONY: fe-install
fe-install: ## Install frontend dependencies
	cd frontend && npm install

.PHONY: fe-dev
fe-dev: ## Run the frontend dev server (http://localhost:3001)
	cd frontend && npm run dev

.PHONY: fe-build
fe-build: ## Build the frontend
	cd frontend && npm run build

.PHONY: fe-lint
fe-lint: ## Lint the frontend
	cd frontend && npm run lint

## ---- aggregate ------------------------------------------------------------

.PHONY: build
build: be-build fe-build ## Build backend and frontend

.PHONY: test
test: be-test ## Run all tests

.PHONY: lint
lint: be-vet fe-lint ## Lint backend and frontend
