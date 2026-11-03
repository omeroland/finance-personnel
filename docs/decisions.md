# Journal des décisions

Une nouvelle ambiguïté s'ajoute à la suite. D1 à D16 : `docs/architecture.md` (journal MVP). D23 à D34 : `docs/tasks/README.md`. D40 et suivantes : ce fichier.

**D17 à D22 : non fournies** dans le document d'architecture ; à compléter par le propriétaire (voir PR).

## D1 à D16 — Produit et règles

| # | Sujet | Décision MVP | À réévaluer après le MVP |
| --- | --- | --- | --- |
| D1 | Utilisateurs | Un seul utilisateur ; chaque table porte déjà un `owner_id` | Ménage en phase 7 (F36) |
| D2 | Règle 8, compagnie sans dette | Réussit ; seuls des capitaux propres négatifs échouent | Texte strict (« au-dessus de 0 ») |
| D3 | Banques et assureurs, règle 8 | Drapeau « financière » disponible, désactivé par défaut | Exemption par défaut pour le secteur financier |
| D4 | Rendement annuel historique | Dividende de l'année / prix de clôture moyen de l'année | Cours de fin d'année |
| D5 | BPA pour P/E et ratio de distribution | Dernier exercice | 12 derniers mois |
| D6 | Type de BPA | Dilué | De base |
| D7 | Règle 10, période | 1 an ; 5 ans configurable | 5 ans par défaut |
| D8 | Règle 5b assouplie | 1, 3, 4, 6 hausses sur 5, 10, 15, 20 ans | Seuils d'une source publiée |
| D9 | Horizon de 5 ans | Indicatif, sans verdict | Le retirer ou le garder |
| D10 | Courtiers | Saisie manuelle et un format CSV générique de l'app ; aucun adapter courtier | Gabarits par courtier en phase 6 (F33) |
| D11 | Prix de base rajusté | Phase 6 (F30 à F32) | Cas limites après une première année fiscale réelle |
| D12 | Bandes de rééquilibrage | Celles de ton outil : ±20 % relatifs ; encaisse cible + 5 points | Option 5/25 en phase 7 (F38) |
| D13 | Portefeuille modèle | Un modèle par compte, comme ton outil | Modèle multi-comptes en phase 7 (F37) |
| D14 | Ordre des modules | Module 1, puis module 2 | Inverser si le module 2 sert plus |
| D15 | Rendement des relevés de valeur | Cotisation en fin de mois, comme ton outil | Cotisation en milieu de mois (Dietz modifiée) |
| D16 | Nom de l'application | « Finance privée », nom de travail | Nom définitif |

## D17 à D22 — à fournir

## D23 à D34 — Techniques

| # | Sujet | Décision MVP | À réévaluer |
| --- | --- | --- | --- |
| D23 | Migrations | goose, SQL pur, embarqué dans le binaire | golang-migrate |
| D24 | Lecture xlsx | excelize (Go) | — |
| D25 | Argent en Go | shopspring/decimal | cockroachdb/apd si besoin de précision arbitraire |
| D26 | Accès PostgreSQL | pgx v5 + sqlc | — |
| D27 | Chaîne du contrat | oapi-codegen (strict), openapi-generator `typescript-angular`, Prism, Spectral | Orval pour Angular |
| D28 | Oracle | Python + LibreOffice sans interface, recalcul forcé par UNO | — |
| D29 | Authentification | go-oidc v3 côté API ; angular-auth-oidc-client côté web | — |
| D30 | Instruments | `core.instrument` créé dès la phase 1 ; une action est un instrument de type `stock` | — |
| D31 | Unités à l'import | Ratio de distribution ou dette / capitaux propres > 5, ou rendement > 1 : lu comme un pourcentage, divisé par 100, avec avertissement | Lire le format de cellule |
| D32 | Rafraîchissement par fournisseur | Route au contrat, répond 501 jusqu'à la phase 3 | — |
| D33 | Données de test xlsx | Ta feuille fournie est un gabarit vide ; les tests remplissent une copie par programme | Une copie réelle anonymisée |
| D34 | Limitation de débit et audit | Débit limité par Traefik ; journal d'audit écrit par les dépôts | Limitation dans l'API |
| D40 | Unicité du symbole | `core.instrument.symbol` unique par propriétaire (`UNIQUE (owner_id, symbol)`) ; `owner_id NOT NULL` sur toutes les tables, `core.instrument` comprise (D1) | Instruments partagés entre ménages (phase 7) |
| D41 | Rôle `finance_app` | Créé `NOLOGIN` par la migration 0003 ; le déploiement lui donne `LOGIN` et un mot de passe hors dépôt. Le `down` retire les droits mais conserve le rôle (global au serveur). `SELECT, INSERT` seulement sur `stockeval.evaluation` et `core.audit_log`, tous droits DML sur les autres tables | Rôle distinct pour la lecture seule |
| D42 | Directive `go` de `go.mod` | Passée de 1.24 à 1.26.0 : goose, pgx et testcontainers tirent des dépendances (`golang.org/x/*`) qui l'exigent. Le CI utilise `go-version: stable`. `.golangci.yml` (`run.go: 1.24`) est à aligner par l'agent Plateforme | Épingler des versions plus anciennes des dépendances pour revenir à 1.24 |
