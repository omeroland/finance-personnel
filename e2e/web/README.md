Tests de bout en bout Playwright du client web.

Prérequis : Docker, `npm ci` dans `api/` (Prism) et `web/` (Angular), puis `npm ci` et `npx playwright install chromium` ici.

```sh
cd e2e/web && npm test        # démarre Keycloak de test (Compose), Prism et ng serve
E2E_NO_STACK=1 npm test       # réutilise un Keycloak déjà démarré (npm run stack:up)
```
