Déploiement : Compose et configuration d'infrastructure.

## Pile locale

`make up` (profil `local`, https://localhost:9443, certificat auto-signé), `make down`, `make test-headers`, `make scan-images`. Production : `make up PROFILE=prod` avec `FINANCE_PUBLIC_ORIGIN` et `TRAEFIK_ACME_EMAIL` dans `deploy/.env`.

Sauvegardes chiffrées (age, OVH Object Storage), restauration et minuteries : voir `deploy/backup/README.md` (`make test-backup`).

## Pile complète et vérification de bout en bout

`make up` démarre `db`, `migrate` (tâche ponctuelle : migrations goose, puis mot de passe du rôle `finance_app`, D221), `api`, `web`, `keycloak` et Traefik. L'API se connecte en `finance_app`, jamais en propriétaire ; le réseau de la base reste interne. `deploy/.env` (hors dépôt) reçoit `FINANCE_APP_PASSWORD` s'il manque.

Pour vérifier avec un jeton, démarre la pile avec le realm de TEST (client `finance-test`, mot de passe factice ; local seulement, D222), puis lance le test de fumée :

```sh
make up-test    # = make up avec deploy/compose.test-realm.yaml
make smoke      # /healthz, /readyz, 401 sans jeton, GET puis POST /api/v1/stocks avec jeton
```

Jeton à la main :

```sh
curl -ks https://localhost:9443/auth/realms/finance/protocol/openid-connect/token \
  -d grant_type=password -d client_id=finance-test -d username=test-user -d password=FAUX-mot-de-passe-de-test
```

Retour au realm de production : `make down` puis `make up`. Si `deploy/.env` a été régénéré alors qu'un ancien volume `pgdata` existe, la connexion de `migrate` échoue (mot de passe différent) : supprime ce volume de développement.
