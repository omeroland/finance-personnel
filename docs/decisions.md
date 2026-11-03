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

## D60 à D62 — Authentification (P1-13)

| # | Sujet | Décision MVP | À réévaluer |
| --- | --- | --- | --- |
| D60 | Découverte OIDC | Faite à la première requête authentifiée, réessayée tant qu'elle échoue (fermé par défaut : 401) : l'API démarre sans Keycloak. `OIDC_ISSUER` est obligatoire au démarrage ; `OIDC_DISCOVERY_URL` optionnel quand Keycloak est joint par une adresse interne distincte de l'issuer public ; `OIDC_AUDIENCE` vaut `finance-api` par défaut | Découverte au démarrage avec nouvelle tentative |
| D61 | Realm Keycloak | `deploy/keycloak/realm-finance.json` : URI de redirection et origine web via la variable `${FINANCE_WEB_ORIGIN}` (substitution native de l'import Keycloak), sans utilisateur ; client Flutter ajouté en phase 4. Version Keycloak 26.4, image épinglée par digest dans le test d'intégration | Un fichier par environnement |
| D62 | Erreurs HTTP | `platform/httpx.WriteProblem` (RFC 9457) créé ici, avec le strict nécessaire, faute d'être livré ailleurs | Reprise par la tâche des middlewares |
| D70 | Image web avant P1-14 | `web/Dockerfile` a deux cibles : `app` (build Angular + Caddy) et `placeholder` (page statique), cible par défaut. Compose utilise `placeholder` tant que `web/package.json` n'existe pas | Passer `target: app` dans `deploy/compose.yaml` à la fusion de P1-14 |
| D71 | Sondes de santé | L'image de l'API est distroless (sans shell ni client HTTP) : la sonde de `/healthz` est exécutée par le conteneur Traefik (`wget`). Traefik racine sans capacité en prod (volume ACME) ; non root en local. PostgreSQL garde cinq capacités nécessaires à son entrypoint | Drapeau `-healthcheck` dans le binaire de l'API |
| D72 | Réseaux et métriques | Réseau `internal` (sans sortie) pour la base ; aucun port de `db` publié. Les points d'entrée `ping` (8081) et `metrics` (8082) de Traefik ne sont ni routés ni publiés. Ports locaux 127.0.0.1:9080 et 9443 (`LOCAL_HTTP_PORT`, `LOCAL_HTTPS_PORT`) | Prometheus sur le réseau interne plus tard |
| D73 | Caddy recompilé | Le binaire de `caddy:2-alpine` embarque une stdlib et des dépendances avec des CVE HIGH corrigées en amont ; il est recompilé avec le Go de l'image de build, `golang.org/x/net` et `go-jose` à jour (non figés par go.sum), sans attribut de fichier. `apk upgrade` appliqué | Image Caddy officielle dès qu'elle est corrigée |
| D74 | Secrets, OIDC et CSP | Mots de passe aléatoires dans `deploy/.env` (généré par `make up`, hors git), en attendant sops + age (P1-23). Origine publique `FINANCE_PUBLIC_ORIGIN` : `OIDC_ISSUER`, `KC_HOSTNAME` et `FINANCE_WEB_ORIGIN` ; discovery interne `http://keycloak:8080`. CSP : `style-src 'self'` sans `unsafe-inline`, `connect-src 'self'` car API et Keycloak sont sur la même origine | Nonce de style si Angular l'exige (P1-14) |
| D75 | Tests et Trivy | `make test-headers` (pile démarrée) et `make scan-images` (Trivy épinglé) ne sont pas dans `make ci`, qui reste sans pile ; `.trivyignore` vide | Étape CI Plateforme avec approbation du propriétaire |

## D80 à D83 — Moteur : calcul des mesures (P1-05)

| # | Sujet | Décision MVP | À réévaluer |
| --- | --- | --- | --- |
| D80 | Devises et séries | Le prix est en devise de cotation ; BPA, dividendes, valeur comptable, dette et capitaux propres en devise de déclaration. `fx` (nil = 1) multiplie BPA et valeur comptable (P/E, P/B) et le dividende prévu (rendement actuel) ; les ratios d'une même devise (distribution, dette/capitaux, variations) ne sont pas convertis. Le rendement annuel est déjà une fraction | Dividendes déclarés dans une autre devise que le BPA |
| D81 | Rendement moyen (11b) | Moyenne simple du rendement annuel sur la fenêtre de l'horizon (pas tronquée : la troncature de la fiche vise les taux de croissance) | Moyenne tronquée si l'oracle l'exige |
| D82 | Fenêtre, manquant et insuffisant | La fenêtre se termine à la dernière année de la série ; série vide ou donnée d'instantané absente = `missing`, fenêtre sans h années consécutives = `insufficient`. « Dernier exercice » = dernière année du BPA ; dividende du même exercice requis. Dénominateur nul (BPA, prix, capitaux, dernier dividende, valeur comptable) = `missing` plus avertissement ; valeurs négatives conservées pour que le pointage les fasse échouer. Tous les taux ignorés = `missing` | Dividende nul : exempter au pointage (P1-06) |
| D83 | Année courante | `Inputs.CurrentYear` ne sert qu'à un avertissement (dernier exercice plus ancien que année - 2) ; elle ne change aucune valeur | Exclure les séries périmées |
