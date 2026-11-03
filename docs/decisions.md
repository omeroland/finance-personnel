# Journal des décisions

Une nouvelle ambiguïté s'ajoute à la suite. D1 à D16 : `docs/architecture.md` (journal MVP). D23 à D34 : `docs/tasks/README.md`. D35 à D39 et D40 et suivantes : ce fichier.

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

## D35 à D39 — Contrat OpenAPI (P1-03)

| # | Sujet | Décision MVP | À réévaluer |
| --- | --- | --- | --- |
| D35 | Préfixe de l'API | Les chemins du contrat portent `/api/v1` et le serveur déclaré est `/` : Prism ignore le chemin d'un serveur et ne servirait pas le préfixe, et le client généré reste relatif à l'origine | Préfixe dans `servers` si Prism le gère |
| D36 | Montants et séries annuelles | Chaînes décimales (`Decimal`) avec devise, y compris `annual_return` (fraction en chaîne), pour ne jamais passer par un `float64`. Ratios, scores, seuils et valeurs de critères restent des `number` (fraction, 0.70 = 70 %) | `number` pour `annual_return` si le client le réclame |
| D37 | Valeur inconnue | Propriété absente, jamais `null` ; OpenAPI 3.1 sans `exclusiveMinimum` numérique ni `type` en tableau, que oapi-codegen 2.7 ne lit pas | `nullable` explicite avec un générateur 3.1 complet |
| D38 | Outils de génération | oapi-codegen v2.7.0 (directive `tool`; la v2.8 n'est pas adoptée, pour ne pas changer le code généré sans raison), Spectral 6.17.0, Prism 5.16.0 et openapi-generator-cli 2.42.0 (générateur 7.21.0) épinglés dans `api/package.json` et `api/openapitools.json`. Node et Java 11+ requis pour `make gen` | Image Docker par digest |
| D39 | Vocabulaire du contrat | Modes `corrected` et `compat`; horizons 5, 10, 15, 20; verdict `excluded`, `candidate`, `watch`, `rejected` porté par horizon et absent à 5 ans (D9); `Conflict` de sources nommé `SourceConflict`; liste de suivi et historique sans pagination | Pagination si la liste dépasse quelques centaines d'actions |

## D50 — Référentiel `rules.yaml` (P1-04)

| # | Sujet | Décision MVP | À réévaluer |
| --- | --- | --- | --- |
| D50 | Format de `rules.yaml` | `version: "1"` (chaîne, à incrémenter à chaque changement de seuil) ; empreinte SHA-256 des octets du fichier ; analyseur `go.yaml.in/yaml/v3` en mode strict (champ inconnu refusé) ; le chargeur exige un seuil assoupli pour 5a, 5b, 6, 7, 8, 11a, 11c et aucun pour 9, 10, 11b, et refuse un assoupli qui change la borne basse d'un intervalle (7, 8, 11a, 11c) | Schéma de version sémantique |
