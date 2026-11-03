# CLAUDE.md — Finance privée

Règles communes à tous les agents. Lis ce fichier en entier avant chaque tâche.

## Ce dépôt

Application de finance privée auto-hébergée : un monolithe modulaire en Go, une API REST unique,
des clients Angular (web) et Flutter (mobile, phase 4), PostgreSQL 16, Keycloak.

- Module 1 — Évaluation d'actions (grille Simply Investing) : `backend/internal/modules/stockeval/`
- Module 2 — Portefeuille de FNB (phase 2) : `backend/internal/modules/etf/`
- Socle : `backend/internal/platform/` (technique) et `backend/internal/core/` (comptes, instruments, registre, prix, devises)

Documents de référence (ne les modifie que si ta fiche le demande) :

| Fichier | Contenu |
| --- | --- |
| `docs/architecture.md` | Architecture et conception (source de vérité) |
| `docs/rules.md` | Référentiel des règles 5a à 11c, seuils strict et assoupli |
| `docs/decisions.md` | Journal des décisions D1, D2… |
| `docs/tasks/README.md` | Index des tâches, dépendances, statuts |
| `docs/tasks/P1-xx.md` | Ta fiche de tâche |

## Avant de commencer

1. Lis ta fiche en entier. Lis seulement les fichiers de contexte qu'elle nomme, plus ce fichier et le `CLAUDE.md` de ton sous-répertoire.
2. Vérifie dans `docs/tasks/README.md` que toutes les dépendances de ta fiche sont « terminée ». Sinon, arrête-toi et signale-le.
3. Travaille dans ton worktree, sur la branche `task/P1-xx`.
4. Reste dans le périmètre de fichiers de ton rôle (tableau plus bas). Si ta tâche exige de sortir du périmètre, arrête-toi et signale-le.

## Commandes

| Commande | Effet |
| --- | --- |
| `make gen` | Régénère le code issu du contrat OpenAPI et des requêtes sqlc |
| `make gen-check` | Régénère puis échoue si le dépôt change (`git diff --exit-code`) |
| `make lint` | golangci-lint, Spectral (contrat), ESLint (web) |
| `make test` | Tests unitaires, sans Docker ni réseau |
| `make test-engine` | Tests du moteur de règles seulement |
| `make test-int` | Tests d'intégration (testcontainers ; Docker requis) |
| `make e2e` | Playwright |
| `make oracle` | Recalcule les cas de l'oracle avec LibreOffice |
| `make mock` | Serveur simulé Prism à partir du contrat |
| `make up` / `make down` | Pile complète en local (Compose) |
| `make ci` | `lint` + `gen-check` + `test` + `test-int` + `govulncheck` ; doit passer avant toute PR |

## Conventions

**Go**
- Go 1.24. Outils épinglés avec la directive `tool` de `go.mod` (`go tool oapi-codegen`, `go tool sqlc`, `go tool goose`).
- `context.Context` en premier paramètre de toute fonction qui fait des entrées-sorties.
- Erreurs enveloppées avec contexte : `fmt.Errorf("charger les faits %s: %w", symbol, err)`. Pas de `panic` hors de l'initialisation.
- Journaux : `log/slog` en JSON. Jamais de jeton, de mot de passe ni de montant de portefeuille dans un journal.
- Pas d'état global mutable. Dépendances injectées par constructeur.
- Tests en table ; `t.Parallel()` quand c'est sûr.

**Nombres**
- Argent : `github.com/shopspring/decimal` en Go, `numeric(20,6)` en SQL. Jamais `float64` pour de l'argent.
- Ratios et taux : `float64` en fraction (0.70 = 70 %). Jamais d'échelle 0–100 dans le code ni dans l'API.

**Contrat et code généré**
- `api/openapi.yaml` est la seule source du contrat. Code généré dans `backend/internal/api/gen/` et `web/src/app/api/` : jamais édité à la main.
- SQL : requêtes dans `queries/*.sql` à côté de chaque dépôt, générées par sqlc ; migrations goose en SQL pur dans `backend/migrations/`.

**Web** : voir `web/CLAUDE.md`. **Backend** : voir `backend/CLAUDE.md`.

**Commits et PR**
- Conventional Commits, avec l'ID de la tâche : `feat(engine): P1-06 pointage, statuts et verdict`.
- La description de la PR cite la fiche, coche chaque critère d'acceptation avec la commande qui le prouve, et liste les décisions Dxx ajoutées.

## Interdits

- Modifier `api/openapi.yaml` ou `backend/migrations/` hors d'une tâche de contrat ou de schéma.
- Modifier un fichier sous `testdata/golden/` ou `testdata/oracle/` hors d'une tâche qui le demande.
- Désactiver, sauter ou affaiblir un test, un seuil de couverture ou un contrôle de CI pour faire passer une PR.
- Appel réseau dans un test (sauf conteneurs locaux de testcontainers).
- Secret, jeton ou donnée personnelle dans un fichier suivi par git.
- Ajouter une dépendance sans la justifier dans la PR (rôle, licence, alternative écartée).
- `float64` pour de l'argent.
- Toute logique de calcul financier dans un client (web ou mobile) : les scores viennent de l'API.

## Devant une ambiguïté

1. Cherche la réponse dans `docs/decisions.md`, puis dans `docs/architecture.md`.
2. Si rien ne la tranche : applique l'option la plus simple et la plus réversible, ajoute une ligne `Dxx` à `docs/decisions.md` dans la même PR (sujet, décision, à réévaluer), et signale-la en tête de la description de la PR.
3. Si l'option la plus simple changerait un contrat, un schéma ou un résultat de règle : arrête-toi et signale-le, sans coder.

## Escalade

Après 3 échecs de suite à la CI ou à la vérification indépendante, arrête-toi. Écris dans la PR ce que tu as essayé, ce qui échoue et la commande qui le reproduit. Ne contourne jamais un contrôle.

## Définition de « terminé »

- [ ] Chaque critère d'acceptation de la fiche est coché, avec la commande qui le prouve.
- [ ] `make ci` est vert en local et en CI.
- [ ] Aucun fichier modifié hors du périmètre du rôle.
- [ ] Contrat et code généré sans dérive (`make gen-check`).
- [ ] Décisions ajoutées listées dans la PR et dans `docs/decisions.md`.
- [ ] Vérification indépendante passée.

## Périmètres de fichiers par rôle

| Rôle | Peut modifier | Lecture seule |
| --- | --- | --- |
| Orchestrateur | `docs/`, `testdata/` (hors `oracle/`) | Tout le reste |
| Backend Go | `api/`, `backend/` sauf `**/ingest/` | Tout le reste |
| Données | `backend/internal/**/ingest/`, `testdata/fixtures/` | Tout le reste |
| Frontend Angular | `web/`, `e2e/web/` | Tout le reste |
| Mobile Flutter | `mobile/` (phase 4) | Tout le reste |
| Plateforme | `deploy/`, `.github/`, `Makefile`, fichiers `Dockerfile` | Tout le reste |
| Vérificateur | `tools/oracle/`, `testdata/oracle/`, `e2e/`, `docs/gates/` | Tout le reste |

Les fichiers de CI et les seuils de couverture ne changent qu'avec l'agent Plateforme et l'approbation du propriétaire.

## Sécurité

- Chaque requête SQL d'un dépôt filtre par `owner_id`. Une requête sans `owner_id` est un défaut.
- Chaque route de l'API exige un JWT valide, sauf `/healthz` et `/readyz`.
- Entrées utilisateur validées à la frontière (taille, type, bornes). Fichiers téléversés limités à 10 Mo.
- Erreurs de l'API au format RFC 9457 (`application/problem+json`), sans trace interne.
