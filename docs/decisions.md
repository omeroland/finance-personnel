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
| D90 | Image de l'oracle | `debian:bookworm-slim` épinglée par digest + LibreOffice Calc 7.4.7 et python3-uno épinglés par version apt (`4:7.4.7-1+deb12u14`) ; openpyxl 3.1.5 (MIT) pour écrire/lire les xlsx, écarté : xlsxwriter (ne relit pas), lecture par UNO (plus de code). Si Debian retire la version du miroir, remonter le pin | Instantané snapshot.debian.org si le pin casse |
| D91 | Format et exécution de l'oracle | `testdata/oracle/cases.json` : par cas `eps`/`dividends` (20 valeurs, `null` = vide) et `attendu_eps`/`attendu_dividendes` (`moyenne`, compte, `erreur`). Résultats relus dans le xlsx enregistré après `calculateAll()`. `make oracle` (Docker) hors `make ci`, qui reste sans conteneur ; le fichier est versionné et régénéré à la demande | Étape CI Plateforme quand un runner l'accepte |

## D110 à D114 — Client web (P1-14)

| # | Sujet | Décision MVP | À réévaluer |
| --- | --- | --- | --- |
| D110 | Traductions du web | Fichier `web/src/app/i18n/fr-CA.json` importé à la compilation, tube `t` typé sur les clés ; pas d'`@angular/localize` (une seule langue, aucune image par langue) | `@angular/localize` + xlf si une 2e langue arrive |
| D111 | Configuration d'exécution du web | `config.json` lu par `fetch` avant tout autre initialiseur (`apiUrl`, `oidc.issuer`, `oidc.clientId`). `web/public/config.json` porte les valeurs de développement (Keycloak sur 8180) ; la production le remplace (volume ou génération au démarrage, côté Plateforme) | Génération par l'image Caddy |
| D112 | Session OIDC du web | Jetons dans `sessionStorage` (défaut de la bibliothèque), jamais `localStorage`. Renouvellement par jeton de rafraîchissement sans `offline_access` (session SSO Keycloak). Intercepteur de la bibliothèque limité à `{apiUrl}/api/` | Mémoire seule (BFF) |
| D113 | Pile de test Playwright | `e2e/web/compose.yaml` : Keycloak 26.4 `start-dev` avec `realm-finance-test.json`, port 8180 ; Prism (port 4010) et `ng serve` (4200) lancés par `playwright.config.ts`. Prérequis : `npm ci` dans `api/` et `web/` | Pile branchée sur `make e2e` (P1-22) |
| D114 | CSP et styles Angular | Build de production avec `inlineCritical: false` (pas de script `onload`). Angular et Material injectent encore des éléments `<style>` à l'exécution : `style-src 'self'` de D74 peut les bloquer, à vérifier sur la pile `make up` (nonce `CSP_NONCE` ou `style-src 'unsafe-inline'`) | Nonce par requête dans Traefik/Caddy |

## D100 à D105 — Dépôts PostgreSQL (P1-10)

| # | Sujet | Décision MVP | À réévaluer |
| --- | --- | --- | --- |
| D100 | Priorité des sources des faits | Seule une valeur `manual` est protégée : `xlsx` ou `provider` de valeur différente la laissent en place et créent une ligne `fact_conflict` (une seule par divergence ouverte). Entre sources non manuelles, la valeur entrante remplace l'existante ; une valeur `manual` remplace tout. Une valeur identique de source moins fiable est ignorée ; la même valeur saisie à la main promeut la source en `manual` | Ordre `xlsx` > `provider` si le fournisseur écrase l'import à tort |
| D101 | Instantané et priorité | Même protection que D100 pour `snapshot`, sans table de conflit : `SaveSnapshot` renvoie `written=false` quand une ligne manuelle protège la valeur | Table de conflits d'instantané si l'usage l'exige |
| D102 | Audit des écritures | Une ligne `core.audit_log` par écriture effective, dans la même transaction ; aucune pour une écriture sans effet. L'ajout à la liste de suivi (instrument + suivi) est une seule écriture. L'acteur vient du contexte (`store.ContextWithActor`, le `sub` du JWT), `system` sinon | Acteur passé par paramètre |
| D103 | Empreinte des entrées | SHA-256 du JSON canonique : décodage avec `json.Number`, réencodage à clés triées, sans espaces ni échappement HTML. Entrées et résultat sont archivés sous cette forme canonique | JSON canonique RFC 8785 |
| D104 | sqlc | `backend/sqlc.yaml` lit `backend/migrations` comme schéma ; code généré dans `store/storedb` (suivi par git, contrôlé par `make gen-check`, cible `gen-sqlc`). Types : `uuid` -> `google/uuid`, `numeric` -> `shopspring/decimal`, booléens et `char(3)` nullables en pointeurs | — |
| D105 | Conflits | P1-10 crée et liste les conflits ouverts ; leur résolution (colonne `resolved_at`) relève des handlers (P1-12) | Ajouter `ResolveConflict` à P1-12 |
| D120 | Statut d'un critère (pointage) | Un seul statut par critère et horizon : `passed` (seuil strict), `borderline` (assoupli seulement), `failed`, `missing`, `insufficient`, `exempt`. Profil strict : réussite = `passed` ; assoupli : `passed` ou `borderline`. Sans seuil assoupli (9, 10, 11b), le strict s'applique aux deux. `missing` et `insufficient` comptent comme échec ; `exempt` sort du dénominateur (qualité sur 6, total sur 9). Règle 11b : une mesure manquante l'emporte sur un historique insuffisant | Statut séparé par profil |
| D121 | Verdict et horizons | Verdict calculé sur les horizons ≥ `min_verdict_horizon` : `candidate` si un a 0 échec strict, sinon `watch` si un a 0 échec assoupli, sinon `rejected` ; `excluded` si une réponse 1 à 4 n'est pas `yes` (réponse absente comprise). Le plus petit horizon qui l'atteint est reporté (`verdict_horizon`). « 10/10 » = 0 échec, ce qui vaut 9/9 avec règle 8 exemptée | Exiger 10/10 littéral |
| D122 | Entrées du pointage | `Score(mesures, règles, Context)` et `Evaluate(entrées, règles, Context)` ; `Context` = barrière des règles 1 à 4 (`Gate`) et drapeau « financière ». Règle 10 : `ShareChange < 0`. Les points ouverts de P1-05 (D81 moyenne simple, règle 9 avec dividende précédent nul = `missing`) sont conservés : aucun ne change le golden ni docs/rules.md | Dividende précédent nul : exempter la règle 9 |
| D130 | Cible des données importées (xlsx) | Instantané : G ratio de distribution, H dette/capitaux propres, K P/E, O P/B, P dividende prévu, Q prix, fusionnés avec l'existant (source `xlsx`), réécrit seulement si une valeur change ; un instantané `manual` n'est pas touché (avertissement). Rachat (J oui/non) : `qualitative.buyback_manual`, seulement s'il est vide. EPS/DPS : devise de cotation déduite du préfixe (`NYSE`/`NASDAQ`/`AMEX` = USD, sinon `Options.Currency`, CAD par défaut) ; rendement sans devise | Colonne de devise de déclaration dans la feuille |
| D131 | Atomicité de l'import xlsx | Pas de transaction globale : chaque écriture est transactionnelle, un échec interrompt l'import et un nouvel import reprend sans doublon (idempotent). Ligne en double ou symbole > 32 caractères : ignorée avec avertissement. Plausibilité : année manquante, BPA > 10x l'année précédente | Transaction unique par import dans `store/` |
| D132 | Dépendance excelize | `github.com/xuri/excelize/v2` v2.11.1 (BSD-3-Clause), lecture en mémoire d'un .xlsx <= 10 Mo, valeurs en cache seulement (ni macro ni recalcul). Écartés : `tealeg/xlsx` (lecture des types moins fiable, peu maintenu) ; conversion par LibreOffice (processus externe lourd) | — |



## D140 à D143 — Liste de suivi web (P1-15)

| # | Sujet | Décision MVP | À réévaluer |
| --- | --- | --- | --- |
| D140 | Colonne « plus long horizon à 10/10 » | Le contrat renvoie un seul horizon par appel de `listStocks`. Le web interroge les 4 horizons en parallèle (1 seul en mode compat) et retient, par action, le plus long horizon dont la réponse de l'API déclare `strict = outOf` (sans historique insuffisant). Sélection d'une valeur fournie par l'API, aucun score recalculé | Champ dédié dans `ScoreSummary` côté API (une requête au lieu de quatre) |
| D141 | Mode compat et horizon | Le contrat limite `compat` à l'horizon de 20 ans : choisir `compat` fixe l'horizon à 20 et désactive le sélecteur d'horizon | Voir si l'API accepte d'autres horizons |
| D142 | Filtres bourse et verdict | Filtres côté client sur les champs reçus (`stock.exchange`, `score.verdict`) ; mode et horizon sont des paramètres de requête. Verdict absent (horizon de 5 ans) affiché « Indicatif seulement » | Filtres serveur si la liste grandit |
| D143 | Icônes de verdict et tests e2e de la liste | Pictogrammes textuels (✔ ◐ ✖ ⊘, `aria-hidden`) toujours accompagnés du libellé : aucune police d'icônes externe (CSP, D114). Les scénarios Playwright simulent `/api/v1/stocks` par `page.route` (liste en mémoire dérivée de l'exemple du contrat servi par Prism), car l'exemple statique de Prism ne permet ni tri, ni filtre, ni ajout/retrait | Prism avec exemples multiples ou API réelle (P1-22) |

## D150 à D153 — Écran d'import (P1-18)

| ID | Sujet | Décision | À réévaluer |
| --- | --- | --- | --- |
| D150 | Compte rendu d'import | Le contrat (`JobEntry.level` : info, warning, error) n'a ni « importé », ni « ignoré », ni « conflit » : l'écran affiche le statut du job, `stocksImported`, des compteurs par niveau et le tableau ligne à ligne (niveau en texte et en glyphe, jamais en couleur seule). Les conflits se lisent dans les messages | Ajouter un champ `kind` à `JobEntry` (changement de contrat) |
| D151 | Icônes de l'interface | Glyphes Unicode dans un `<span aria-hidden>` plutôt que `mat-icon` : aucune police d'icônes n'est chargée (CSP, D114) | Police Material Symbols auto-hébergée |
| D152 | Suivi du job | `timer(2 s, 2 s)` + `getImport`, arrêt à `succeeded`/`failed` ou après 2 min (message « suivi arrêté »). Une erreur réseau d'une interrogation est ignorée jusqu'à l'échéance | Backoff exponentiel |
| D153 | Tests e2e contre Prism | `apiUrl` vide vise l'origine du web, sans proxy : les scénarios relaient `/api/v1/**` vers Prism (4010) avec `page.route`. Le classeur de test est `testdata/xlsx/simply_investing_template.xlsx` ; Prism ne lit pas son contenu | Proxy `ng serve` vers Prism |
| D160 | Noms des ajouts de contrat P1-03b | `ScoreSummary.perfectHorizon` (Horizon, absent si aucun horizon n'atteint 10/10 strict, D37) remplace les 4 appels par horizon de D140. `JobEntry.kind` : imported, skipped, conflict, normalized, protected (optionnel). `ingest.Report` ne porte pas encore de `kind` : le rôle Données devra l'ajouter ; tant que le champ est absent, le serveur l'omet | Renommer ; ajouter `kind` côté ingest et cesser de lire les conflits dans les messages (D150) |
