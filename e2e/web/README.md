Tests de bout en bout Playwright du client web.

Prérequis : Docker, `npm ci` dans `api/` (Prism) et `web/` (Angular), puis `npm ci` et `npx playwright install chromium` ici.

```sh
cd e2e/web && npm test        # démarre Keycloak de test (Compose), Prism et ng serve
E2E_NO_STACK=1 npm test       # réutilise un Keycloak déjà démarré (npm run stack:up)
```

## Parcours complet sur la pile réelle (P1-22)

```sh
make up-test     # pile Compose complète, realm de test
make e2e-full    # classeur de 5 actions + attendus LibreOffice (conteneur de l'oracle), puis Playwright sur https://localhost:9443
```

Rapport : `docs/gates/phase-1-integration.md`. Piles neuves : `COMPOSE_PROJECT_NAME=financee2e` (projet dédié) avant `make up-test`, et `docker compose ... down -v` pour ce projet seulement.

## Parcours complet de la phase 2 (P2-14)

```sh
COMPOSE_PROJECT_NAME=financegate2p2 make up-test                       # pile neuve, realm de test
cd e2e/web && COMPOSE_PROJECT_NAME=financegate2p2 npx playwright test -c playwright.full-p2.config.ts
python3 e2e/gate/gate_security_p2.py --project financegate2p2            # sécurité de phase 2 (accès admin Keycloak requis, voir le script)
```

Rapport : `docs/gates/phase-2.md`. Une pile neuve par exécution (D484). Le catalogue de FNB synthétique est posé en SQL par le test (`docker exec <projet>-db-1 psql`, D485).
