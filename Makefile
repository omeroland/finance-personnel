SHELL := /bin/bash
.DEFAULT_GOAL := help

BACKEND := backend
ENGINE  := ./internal/modules/stockeval/engine/

.PHONY: help gen gen-check lint test test-engine test-int e2e oracle mock up down ci govulncheck

help: ## Liste les cibles
	@grep -E '^[a-z-]+:.*##' $(MAKEFILE_LIST) | awk -F':.*## ' '{printf "  %-12s %s\n", $$1, $$2}'

gen: ## Régénère le code issu du contrat et de sqlc
	@echo "gen : non configurée (P1-03)"

gen-check: ## Régénère puis échoue si le dépôt change
	@echo "gen-check : non configurée (P1-03)"

lint: ## golangci-lint, Spectral, ESLint
	@if command -v golangci-lint >/dev/null 2>&1; then \
		cd $(BACKEND) && golangci-lint run ./... ; \
	else \
		echo "lint : golangci-lint absent, installe-le (voir .github/workflows/ci.yml)" >&2; exit 1; \
	fi
	@echo "lint (Spectral) : non configurée (P1-03)"
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

oracle: ## Recalcule l'oracle avec LibreOffice
	@echo "oracle : non configurée (P1-07)"

mock: ## Serveur simulé Prism
	@echo "mock : non configurée (P1-03)"

up: ## Pile locale (Compose)
	@echo "up : non configurée (P1-19)"

down: ## Arrête la pile locale
	@echo "down : non configurée (P1-19)"

govulncheck: ## Vulnérabilités connues des dépendances
	cd $(BACKEND) && govulncheck ./...

ci: lint gen-check test test-int govulncheck ## Doit passer avant toute PR
