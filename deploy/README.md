Déploiement : Compose et configuration d'infrastructure.

## Pile locale

`make up` (profil `local`, https://localhost:9443, certificat auto-signé), `make down`, `make test-headers`, `make scan-images`. Production : `make up PROFILE=prod` avec `FINANCE_PUBLIC_ORIGIN` et `TRAEFIK_ACME_EMAIL` dans `deploy/.env`.

Sauvegardes chiffrées (age, OVH Object Storage), restauration et minuteries : voir `deploy/backup/README.md` (`make test-backup`).
