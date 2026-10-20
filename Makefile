# With certificates from `make certs`, zot serves HTTPS (compose.tls.yaml) so
# sbx can pull published blueprints from it.
TLS     := $(wildcard deploy/compose/certs/registry.pem)
COMPOSE := docker compose -f deploy/compose/compose.yaml $(if $(TLS),-f deploy/compose/compose.tls.yaml)
export KITMAKER_REGISTRY_INSECURE := $(if $(TLS),false,true)
CHART   := deploy/helm/kitmaker

# Host ports chosen by deploy/compose/ports.sh (see compose.yaml).
-include deploy/compose/.env
KITMAKER_PORT  ?= 8080
POSTGRES_PORT  ?= 5432
MINIO_PORT     ?= 9000
ZOT_PORT       ?= 5000
BUILDKIT_PORT  ?= 1234

DEV_ENV := \
	KITMAKER_DATABASE_URL="postgres://kitmaker:kitmaker@localhost:$(POSTGRES_PORT)/kitmaker?sslmode=disable" \
	KITMAKER_STORAGE_URL="s3://kitmaker?endpoint=http://localhost:$(MINIO_PORT)&use_path_style=true&region=us-east-1" \
	AWS_ACCESS_KEY_ID=kitmaker AWS_SECRET_ACCESS_KEY=kitmaker-secret \
	KITMAKER_HTTP_ADDR=":$(KITMAKER_PORT)" KITMAKER_BASE_URL="http://localhost:$(KITMAKER_PORT)" \
	KITMAKER_LOG_FORMAT=text KITMAKER_REGISTRY_URL=zot:5000 KITMAKER_REGISTRY_PULL_HOST="localhost:$(ZOT_PORT)" \
	KITMAKER_BUILDKIT_ADDR="tcp://127.0.0.1:$(BUILDKIT_PORT)"

# air (live reload for Go), run from the module cache so nothing is installed.
AIR := go run github.com/air-verse/air@v1.67.3

.PHONY: help
help: ## Show targets
	@grep -hE '^[a-z-]+:.*## ' $(MAKEFILE_LIST) | awk -F':.*## ' '{printf "  %-14s %s\n", $$1, $$2}'

## --- Run ------------------------------------------------------------------

.PHONY: env
env: ## Create/complete deploy/compose/kitmaker.env (local settings, secrets key, bootstrap password)
	@sh deploy/compose/env.sh

.PHONY: certs
certs: ## Serve the local registry over HTTPS (mkcert certificate) so sbx can run published blueprints
	@command -v mkcert >/dev/null || (echo "install mkcert first: brew install mkcert"; exit 1)
	@mkdir -p deploy/compose/certs
	mkcert -cert-file deploy/compose/certs/registry.pem -key-file deploy/compose/certs/registry-key.pem localhost 127.0.0.1 ::1 zot host.docker.internal
	@cp "$$(mkcert -CAROOT)/rootCA.pem" deploy/compose/certs/ca.pem
	@echo "Trust the local CA once (asks for your password): mkcert -install"
	@echo "Then restart the registry and BuildKit: make up (or make dev)"

.PHONY: up
up: ports env ## Build and start the whole stack in Docker → http://localhost:8080
	$(COMPOSE) up -d --build --wait kitmaker worker zot buildkit mailpit
	$(COMPOSE) run --rm --no-deps seed
	@echo "Kit Maker: http://localhost:$$(grep KITMAKER_PORT deploy/compose/.env | cut -d= -f2)"

.PHONY: restart
restart: env ## Recreate the app containers so kitmaker.env changes apply
	$(COMPOSE) up -d --wait --force-recreate kitmaker worker

.PHONY: cli
cli: env ## Run a kitmaker command in the stack: make cli ARGS="seed --help"
	$(COMPOSE) run --rm --no-deps kitmaker $(ARGS)

.PHONY: down
down: ## Stop the stack (keeps data volumes)
	$(COMPOSE) down

.PHONY: logs
logs: ## Follow app logs
	$(COMPOSE) logs -f kitmaker worker

.PHONY: dev
dev: ports env web/node_modules ## Hot reload on :8080, signed in as the bootstrap admin: air rebuilds Go, Vite hot-swaps the UI
	$(COMPOSE) stop kitmaker worker 2>/dev/null || true
	$(COMPOSE) up -d --wait postgres minio zot buildkit mailpit
	$(COMPOSE) up minio-init
	@set -a; . deploy/compose/kitmaker.env; set +a; \
	: "$${KITMAKER_DEV_USER=$${KITMAKER_BOOTSTRAP_ADMIN:-admin@kitmaker.local}}"; export KITMAKER_DEV_USER; \
	trap 'kill 0' INT TERM EXIT; \
	(cd web && npm run dev) & \
	$(DEV_ENV) $(AIR)

.PHONY: ports
ports:
	@sh deploy/compose/ports.sh

## --- Build & test -----------------------------------------------------------

web/node_modules: web/package-lock.json
	cd web && npm ci
	@touch $@

.PHONY: web
web: web/node_modules ## Build the frontend into web/dist (embedded by go build)
	cd web && npm run build

.PHONY: build
build: web ## Build bin/kitmaker
	CGO_ENABLED=0 go build -trimpath -o bin/kitmaker ./cmd/kitmaker

.PHONY: image
image: ## Build the container image
	docker build -t kitmaker:dev .

.PHONY: test
test: ## Run Go tests (needs Docker for testcontainers)
	go test ./...

.PHONY: generate
generate: ## Regenerate Ent code from internal/ent/schema
	go generate ./internal/ent

.PHONY: migrate-diff
migrate-diff: ## New migration from the Ent schema: make migrate-diff name=add_widgets
	@test -n "$(name)" || (echo "usage: make migrate-diff name=<name>"; exit 1)
	go run ./internal/tools/migratediff $(name)

.PHONY: migrate-lint
migrate-lint: ## Lint the latest migration with the Atlas CLI
	atlas migrate lint --env local --latest 1

.PHONY: helm-lint
helm-lint: ## Lint and render the Helm chart
	helm lint $(CHART) --set database.url=postgres://x --set storage.url=s3://x --set secrets.url=base64key://x
	helm template kitmaker $(CHART) --set database.url=postgres://x --set storage.url=s3://x --set secrets.url=base64key://x \
		--set route.enabled=true --set ingress.enabled=true >/dev/null
