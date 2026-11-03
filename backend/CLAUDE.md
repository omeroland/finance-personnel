# backend/CLAUDE.md

Complète le `CLAUDE.md` racine pour tout ce qui est sous `backend/`.

## Arborescence

```text
backend/
├── cmd/server/            point d'entrée ; assemble les dépendances, rien d'autre
├── migrations/            goose, SQL pur, embarqué dans le binaire
└── internal/
    ├── api/gen/           généré par oapi-codegen (ne pas éditer)
    ├── platform/          auth, config, db, httpx (middlewares, problem+json), audit
    ├── core/              instruments, comptes, registre, prix, devises
    └── modules/
        └── stockeval/
            ├── engine/    moteur de règles PUR : aucune E/S, aucun import de platform/ ni de store/
            ├── ingest/    import xlsx et adapters (rôle Données)
            ├── store/     dépôts PostgreSQL (sqlc)
            └── http/      handlers : traduisent DTO <-> domaine, rien de plus
```

## Règles de dépendance

- `engine/` n'importe que la bibliothèque standard et son propre package. Le vérificateur le contrôle avec `go list -deps`.
- `http/` appelle `engine/` et `store/` ; `store/` n'appelle jamais `engine/`.
- Un module n'importe jamais un autre module. Ce qui est partagé va dans `core/` ou `platform/`.

## Base de données

- pgx v5 et sqlc. Une requête = un fichier `.sql` nommé dans `queries/`.
- Toute écriture passe par une transaction et écrit une ligne dans `core.audit_log` (qui, quoi, quand, avant, après).
- La table `stockeval.evaluation` est en insertion seule : le rôle applicatif n'a pas `UPDATE` ni `DELETE` dessus.

## Tests

- Unitaires : `make test`. Intégration (PostgreSQL, Keycloak) : `make test-int` avec testcontainers, images épinglées par digest.
- Fuzzing des fonctions de calcul : `go test -fuzz=<Nom> -fuzztime=60s ./internal/modules/stockeval/engine/`.
- Couverture minimale : 90 % pour `engine/`, 75 % ailleurs.
