SHELL := /bin/bash
.DEFAULT_GOAL := help

BACKEND := backend
ENGINE  := ./internal/modules/stockeval/engine/
API     := api
WEB_API := web/src/app/api
# Chemins générés : gen-check échoue si l'un d'eux dérive.
GENERATED := $(BACKEND)/internal/api/gen $(WEB_API)

.PHONY: help gen gen-api gen-check lint test test-engine test-int e2e oracle mock up down ci govulncheck test-headers scan-images

help: ## Liste les cibles
	@grep -E '^[a-z-]+:.*##' $(MAKEFILE_LIST) | awk -F':.*## ' '{printf "  %-12s %s\n", $$1, $$2}'

# Outils Node du contrat (Spectral, openapi-generator, Prism) : versions exactes dans api/package.json.
$(API)/node_modules/.package-lock.json: $(API)/package.json $(API)/package-lock.json
	cd $(API) && npm ci --no-audit --no-fund

gen-api: $(API)/node_modules/.package-lock.json
	cd $(BACKEND) && go tool oapi-codegen -config ../$(API)/oapi-codegen.yaml ../$(API)/openapi.yaml
	rm -rf $(WEB_API)
	cd $(API) && npx openapi-generator-cli generate -i openapi.yaml -g typescript-angular -o ../$(WEB_API) \
		--additional-properties=providedInRoot=true,stringEnums=true,enumPropertyNaming=original,fileNaming=kebab-case
	rm -f $(WEB_API)/git_push.sh $(WEB_API)/.gitignore $(WEB_API)/.openapi-generator-ignore

gen: gen-api ## Régénère le code issu du contrat et de sqlc

gen-check: gen ## Régénère puis échoue si le dépôt change
	@git diff --exit-code -- $(GENERATED)
	@if [ -n "$$(git ls-files --others --exclude-standard -- $(GENERATED))" ]; then \
		echo "gen-check : fichiers générés non suivis par git :" >&2; \
		git ls-files --others --exclude-standard -- $(GENERATED) >&2; exit 1; \
	fi

lint: $(API)/node_modules/.package-lock.json ## golangci-lint, Spectral, ESLint
	@if command -v golangci-lint >/dev/null 2>&1; then \
		cd $(BACKEND) && golangci-lint run ./... ; \
	else \
		echo "lint : golangci-lint absent, installe-le (voir .github/workflows/ci.yml)" >&2; exit 1; \
	fi
	cd $(API) && npx spectral lint openapi.yaml --ruleset ../.spectral.yaml --fail-severity=info
	@echo "lint (ESLint) : non configurée (P1-14)"

test: ## Tests unitaires, sans Docker ni réseau
	cd $(BACKEND) && go test -race -count=1 ./...

test-engine: ## Tests du moteur de règles
	@if [ -d $(BACKEND)/internal/modules/stockeval/engine ] && ls $(BACKEND)/internal/modules/stockeval/engine/*.go >/dev/null 2>&1; then \
		cd $(BACKEND) && go test -race -count=1 -cover $(ENGINE) ; \
	else \
		echo "test-engine : non configurée (P1-05)"; \
	fi

test-int: ## Tests d'intégration (testcontainers ; Docker requis)
	cd $(BACKEND) && go test -tags integration -race -count=1 ./...

e2e: ## Playwright
	@echo "e2e : non configurée (P1-22)"

ORACLE_IMAGE := finance-oracle:p1-07

# Hors make ci (exige Docker et télécharge LibreOffice) ; sortie : testdata/oracle/cases.json.
oracle: ## Recalcule l'oracle avec LibreOffice (conteneur épinglé)
	docker build -q -t $(ORACLE_IMAGE) tools/oracle
	docker run --rm --user "$$(id -u):$$(id -g)" -e HOME=/tmp \
		-v "$(CURDIR)/testdata/xlsx:/data/xlsx:ro" -v "$(CURDIR)/testdata/oracle:/data/oracle" $(ORACLE_IMAGE)

mock: $(API)/node_modules/.package-lock.json ## Serveur simulé Prism (port 4010)
	cd $(API) && npx prism mock --host 127.0.0.1 --port 4010 openapi.yaml

COMPOSE := docker compose -f deploy/compose.yaml
PROFILE ?= local

# Génère deploy/.env (hors git) avec des mots de passe aléatoires s'il est absent.
deploy/.env:
	@umask 077 && { \
		echo "POSTGRES_PASSWORD=$$(openssl rand -hex 24)"; \
		echo "KEYCLOAK_DB_PASSWORD=$$(openssl rand -hex 24)"; \
		echo "KEYCLOAK_ADMIN_PASSWORD=$$(openssl rand -hex 24)"; \
	} > $@
	@echo "up : deploy/.env généré (secrets aléatoires, ignoré par git)"

up: deploy/.env ## Pile locale (Compose) ; PROFILE=prod pour la production
	$(COMPOSE) --env-file deploy/.env --profile $(PROFILE) up -d --build --wait --wait-timeout 120

down: ## Arrête la pile locale
	$(COMPOSE) --env-file deploy/.env --profile local --profile prod down

test-headers: ## En-têtes de sécurité sur / et /api (pile démarrée par make up)
	deploy/tests/headers.sh

scan-images: ## Trivy sur les images construites (HIGH/CRITICAL) ; Docker requis
	@for img in finance/api:dev finance/web:dev; do \
		docker run --rm -v /var/run/docker.sock:/var/run/docker.sock -v $(CURDIR)/.trivyignore:/.trivyignore:ro \
			aquasec/trivy:latest@sha256:790fd66d1697395d8693d1acaa1ea702406a20f109b6fc7bac8db623eca6c06c \
			image --severity HIGH,CRITICAL --exit-code 1 --ignore-unfixed --ignorefile /.trivyignore $$img || exit 1; \
	done

govulncheck: ## Vulnérabilités connues des dépendances
	cd $(BACKEND) && govulncheck ./...

ci: lint gen-check test test-int govulncheck ## Doit passer avant toute PR
