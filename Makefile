SHELL := /bin/bash
.DEFAULT_GOAL := help

BACKEND := backend
ENGINE  := ./internal/modules/stockeval/engine/
API     := api
WEB_API := web/src/app/api
# Chemins générés : gen-check échoue si l'un d'eux dérive.
SQLC_OUT := $(BACKEND)/internal/modules/stockeval/store/storedb
GENERATED := $(BACKEND)/internal/api/gen $(WEB_API) $(SQLC_OUT)

.PHONY: test-keycloak-admin e2e-full up-test smoke help test-backup gen gen-api gen-sqlc gen-check lint test test-engine test-int e2e oracle mock up down ci govulncheck test-headers scan-images

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

gen-sqlc:
	cd $(BACKEND) && go tool sqlc generate

gen: gen-api gen-sqlc ## Régénère le code issu du contrat et de sqlc

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
	cd web && npm ci --no-audit --no-fund && npx ng lint

test: ## Tests unitaires, sans Docker ni réseau
	cd $(BACKEND) && go test -race -count=1 ./...
	cd web && npm ci --no-audit --no-fund && npm run test

test-engine: ## Tests du moteur de règles
	@if [ -d $(BACKEND)/internal/modules/stockeval/engine ] && ls $(BACKEND)/internal/modules/stockeval/engine/*.go >/dev/null 2>&1; then \
		cd $(BACKEND) && go test -race -count=1 -cover $(ENGINE) ; \
	else \
		echo "test-engine : non configurée (P1-05)"; \
	fi

test-int: ## Tests d'intégration (testcontainers ; Docker requis)
	cd $(BACKEND) && go test -tags integration -race -count=1 ./...

e2e: ## Playwright
	cd e2e/web && npm ci --no-audit --no-fund && npx playwright install chromium && npm test

# P1-22 : parcours complet sur la pile réelle (démarrée par `make up-test`) ; hors `make ci`.
# Produit le classeur de 5 actions et les attendus LibreOffice (conteneur de l'oracle), puis lance Playwright.
e2e-full: ## Parcours complet Playwright sur la pile réelle (make up-test d'abord ; Docker requis)
	docker image inspect $(ORACLE_IMAGE) >/dev/null 2>&1 || docker build -q -t $(ORACLE_IMAGE) tools/oracle
	mkdir -p e2e/web/.generated
	docker run --rm --user "$$(id -u):$$(id -g)" -e HOME=/tmp --entrypoint python3 \
		-v "$(CURDIR)/tools/oracle:/oracle:ro" -v "$(CURDIR)/testdata/xlsx:/data/xlsx:ro" \
		-v "$(CURDIR)/e2e/web/.generated:/data/out" $(ORACLE_IMAGE) \
		e2e_workbook.py --template /data/xlsx/simply_investing_template.xlsx --out /data/out
	cd e2e/web && npm ci --no-audit --no-fund && npx playwright install chromium && npx playwright test -c playwright.full.config.ts

ORACLE_IMAGE := finance-oracle:p1-07

# Hors make ci (exige Docker et télécharge LibreOffice) ; sortie : testdata/oracle/cases.json.
oracle: ## Recalcule l'oracle avec LibreOffice (conteneur épinglé)
	docker build -q -t $(ORACLE_IMAGE) tools/oracle
	docker run --rm --user "$$(id -u):$$(id -g)" -e HOME=/tmp \
		-v "$(CURDIR)/testdata/xlsx:/data/xlsx:ro" -v "$(CURDIR)/testdata/oracle:/data/oracle" $(ORACLE_IMAGE)

mock: $(API)/node_modules/.package-lock.json ## Serveur simulé Prism (port 4010)
	cd $(API) && npx prism mock --host 127.0.0.1 --port 4010 openapi.yaml

# Projet Compose dédié au local : évite un ancien volume `finance_pgdata` aux mots de passe périmés (D303). Surcharge : COMPOSE_PROJECT_NAME=… make up.
export COMPOSE_PROJECT_NAME ?= financelocal
COMPOSE := docker compose -f deploy/compose.yaml $(COMPOSE_EXTRA)
PROFILE ?= local

# Génère deploy/.env (hors git) avec des mots de passe aléatoires s'il est absent.
deploy/.env:
	@umask 077 && { \
		echo "POSTGRES_PASSWORD=$$(openssl rand -hex 24)"; \
		echo "KEYCLOAK_DB_PASSWORD=$$(openssl rand -hex 24)"; \
		echo "KEYCLOAK_ADMIN_PASSWORD=$$(openssl rand -hex 24)"; \
		echo "FINANCE_APP_PASSWORD=$$(openssl rand -hex 24)"; \
	} > $@
	@echo "up : deploy/.env généré (secrets aléatoires, ignoré par git)"

up: deploy/.env ## Pile locale (Compose) ; PROFILE=prod pour la production
	@grep -q '^FINANCE_APP_PASSWORD=' deploy/.env || { umask 077; echo "FINANCE_APP_PASSWORD=$$(openssl rand -hex 24)" >> deploy/.env; echo "up : FINANCE_APP_PASSWORD ajouté à deploy/.env"; }
	@origin="$${FINANCE_PUBLIC_ORIGIN:-$$(sed -n 's/^FINANCE_PUBLIC_ORIGIN=//p' deploy/.env)}"; origin="$${origin:-https://localhost:9443}"; \
		printf '{"apiUrl": "", "oidc": {"issuer": "%s/auth/realms/finance", "clientId": "finance-web"}}\n' "$$origin" > deploy/web-config.json
	$(COMPOSE) --env-file deploy/.env --profile $(PROFILE) up -d --build --wait --wait-timeout 120

up-test: ## Pile locale avec le realm de TEST (client finance-test) ; jamais en production
	$(MAKE) up PROFILE=local COMPOSE_EXTRA="-f deploy/compose.test-realm.yaml"

smoke: ## Test de fumée de bout en bout (pile démarrée par make up-test)
	deploy/tests/smoke.sh

down: ## Arrête la pile locale
	$(COMPOSE) --env-file deploy/.env --profile local --profile prod down

test-headers: ## En-têtes de sécurité sur / et /api (pile démarrée par make up)
	deploy/tests/headers.sh

test-keycloak-admin: ## Console d'administration Keycloak bloquée, login servi (pile démarrée ; hors make ci)
	deploy/tests/keycloak-admin.sh

# P1-19-fix : toutes les images de la pile, lues dans deploy/compose.yaml (tous profils, sans liste en dur).
# api et web : scan strict, sans exception. Autres images (tierces, et backup qui hérite de gosu) : .trivyignore (exceptions datées).
# Trivy est lancé par son image épinglée par digest ; seuils inchangés (HIGH, CRITICAL, --ignore-unfixed).
TRIVY_IMAGE := aquasec/trivy:0.76.1@sha256:790fd66d1697395d8693d1acaa1ea702406a20f109b6fc7bac8db623eca6c06c
COMPOSE_SCAN := env POSTGRES_PASSWORD=x KEYCLOAK_DB_PASSWORD=x KEYCLOAK_ADMIN_PASSWORD=x FINANCE_APP_PASSWORD=x docker compose -f deploy/compose.yaml --profile '*'

scan-images: ## Trivy sur toutes les images de la pile (HIGH/CRITICAL) ; Docker requis
	$(COMPOSE_SCAN) build --quiet
	@set -e; for img in $$($(COMPOSE_SCAN) config --images | sort -u); do \
		case "$$img" in finance/api:*|finance/web:*) ign="";; *) ign="--ignorefile /exceptions/.trivyignore";; esac; \
		echo "scan-images : $$img"; \
		docker run --rm -v /var/run/docker.sock:/var/run/docker.sock -v $(CURDIR)/.trivyignore:/exceptions/.trivyignore:ro \
			$(TRIVY_IMAGE) image --quiet --severity HIGH,CRITICAL --exit-code 1 --ignore-unfixed $$ign $$img; \
	done

govulncheck: ## Vulnérabilités connues des dépendances
	cd $(BACKEND) && govulncheck ./...

ci: lint gen-check test test-int govulncheck ## Doit passer avant toute PR

test-backup: ## Sauvegarde/restauration/retention contre MinIO (P1-20 ; Docker requis, hors make ci)
	bash deploy/backup/tests/test.sh
