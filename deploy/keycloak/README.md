# Realm Keycloak

- `realm-finance.json` : realm de production. Variable d'import `FINANCE_WEB_ORIGIN` (ex. `https://finance.example.org`). Aucun utilisateur, aucune obtention directe de jetons.
- `realm-finance-test.json` : tests seulement (client `finance-test`, utilisateur au mot de passe factice). Ne jamais l'importer en production.

Import : monter le fichier dans `/opt/keycloak/data/import/` et démarrer avec `--import-realm`.
