# Porte de phase 2 (P2-14)

Vérificateur, 2026-11-10. Branche `task/P2-14`. Pile réelle du projet Compose dédié `financegate2p2` (`COMPOSE_PROJECT_NAME=financegate2p2 make up-test`, https://localhost:9443, realm de test, **aucune donnée réelle**, données entièrement synthétiques), détruite à la fin par `down -v` de ce seul projet. La pile locale du propriétaire (projet `financelocal`) avait été arrêtée par le coordinateur, sans `-v`, le temps de la porte ; ni elle ni ses volumes n'ont été touchés. Décisions D480 à D485.

**P2-14 : terminée sauf critère de sortie réel (en attente du propriétaire).** Tout ce qui se prouve avec des données synthétiques est prouvé ; le critère de sortie de l'architecture (positions réelles et rendement égal au relevé du courtier à 0,1 point près) ne se prouve qu'avec les données du propriétaire. Aucun résultat n'est déclaré pour lui.

Légende : **réussi** (preuve exécutée) ; **échoué, fiche** (écart, fiche de correction créée) ; **en attente du propriétaire** ; **non vérifiable** (infrastructure absente). Preuves rejouables dans `docs/gates/phase-2-preuves/`.

## Synthèse

| # | Critère | Résultat |
| --- | --- | --- |
| 1 | Parcours complet sur la pile réelle (comptes, import CSV, positions, relevés, modèle, plans, exposition, rendement), trois exécutions propres de suite | **réussi** : 3 passes sur 3, 16 tests chacune, chacune sur une pile neuve |
| 2a | Positions reconstituées à l'unité près (données synthétiques) | **réussi** : quantités, coût moyen et encaisse égaux, chaîne par chaîne, au calcul indépendant (API) et à l'écran |
| 2b | Tous les cas de P2-07 égaux ou écart expliqué | **réussi** pour 35 des 41 cas (33 rejoués par le moteur Go, dont 9 aussi sur la pile, plus 2 de diversification sur la pile) ; 6 cas non rejoués, voir la limite en 2b |
| 2c | Rendement TWR et TRI avec leur base, égaux à l'oracle (relevés) et au calcul indépendant (transactions) | **réussi** |
| 3a | Isolement par `owner_id` sur chacune des 15 routes de phase 2 | **réussi** |
| 3b | JWT exigé sur chaque route de phase 2 | **réussi** |
| 3c | Audit des écritures, sans montant | **réussi** |
| 3d | Registre en insertion seule (UPDATE, DELETE, TRUNCATE refusés) | **réussi** |
| 3e | Validation aux frontières, en-têtes, limitation de débit (rejeu de la phase 1) | **réussi** |
| 3f | Console d'administration Keycloak bloquée, conteneurs durcis, govulncheck | **réussi** |
| 3g | Secrets (gitleaks) et images (Trivy) | en CI sur la PR (jobs `gitleaks` et `trivy`) ; non rejoués en local |
| 4 | **Critère de sortie réel** : positions réelles reconstituées à l'unité près ; rendement d'un compte égal à celui du relevé annuel du courtier à 0,1 point près | **en attente du propriétaire** |
| 5 | Phase 1 : restauration depuis OVH, déploiement sur VPS (P1-23) | **non vérifiable** sans VPS (inchangé, D306) |

Écarts : aucun échec de critère. Une observation de robustesse du client (fiche `docs/tasks/P2-13-fix.md`, non bloquante, D484) et un manque de fonction (aucune route d'import du catalogue, D485).

Signature du propriétaire : ______________________  Date : ______________

## 1. Parcours complet (`e2e/web/full-flow-p2.spec.ts`)

Commande : `COMPOSE_PROJECT_NAME=financegate2p2 make up-test` puis, dans `e2e/web`, `npx playwright test -c playwright.full-p2.config.ts` (le Makefile n'a pas de cible pour ce parcours : une cible `e2e-full-p2` est à ajouter par l'agent Plateforme). Les trois passes : `parcours-complet-run1.txt`, `-run2.txt`, `-run3.txt` (« 16 passed » chacune, 33,5 s, code 0). Chaque passe part d'une pile neuve (`down -v` puis `make up-test`) : sur une même pile qui accumule des comptes, le tableau de bord du client échoue (D484).

Le test écrit une ligne `PREUVE` par comparaison. Les données sont décrites une fois dans `p2-donnees.ts` ; le calcul de référence, sans aucun import du backend, est `p2-reference.ts` (D482). Étapes (en série) :

| Étape | Ce qui est fait | Ce qui est comparé |
| --- | --- | --- |
| 0 | Catalogue : 9 FNB synthétiques posés en SQL dans la base de test (aucune route d'import, D485) | `GET /portfolio/etfs` les relit |
| 1 | CELI et non enregistré créés **à l'écran** ; REER (relevés) et compte de l'oracle par l'API | type et devise de la réponse |
| 2 | CSV du CELI importé à l'écran (`;` et virgule décimale) : 11 écritures (achats avec frais, vente, DRIP, fractionnement 2 pour 1, dividende, cotisations, parts fractionnaires) + 4 lignes piégées (date impossible, type inconnu, quantité négative, autre compte) | lues 15, créées 11, invalides 3, refusées 1 ; **ré-import** : 0 créée, 11 doublons ; compteurs de l'écran = rapport de l'API |
| 3 | CSV du compte non enregistré (7 écritures, `,` et point) ; compte de l'oracle importé par l'API | 7 créées ; registre lu de la plus ancienne à la plus récente |
| 4-5 | Positions des deux comptes, API et onglet « Positions » | voir 2a |
| 6 | 7 relevés mensuels saisis à l'écran (cas `rendement-fidele-03`) | voir 2c |
| 7 | Modèle créé à l'écran (cibles en %, 3 fonds) | cibles et poids reçus = fractions exactes ; un modèle à 50 % refusé (400) |
| 8 | Plan d'achat à l'écran, 10 000 $, prix saisis | voir 2b |
| 9-10 | Plans de l'oracle par l'API (achats) et rééquilibrage `reequilibrage-fidele-02` | voir 2b |
| 11 | Rééquilibrage à l'écran, cotisation par compte (1 500 $ et 500 $) et prix saisis | invariants et poids recalculés indépendamment |
| 12 | Exposition vs modèle, RFG pondéré (API et écran) | calcul indépendant |
| 12b | Diversification de l'oracle (`fidele-01`, `-02`) par la route d'exposition | oracle LibreOffice, 1e-9 |
| 13 | Rendement des comptes à transactions, consolidé et par compte (API et écran) | calcul indépendant |
| 14 | Frais puis écriture inverse ; PUT, PATCH, DELETE d'une transaction | encaisse revenue à l'identique ; 404 |

### 2a. Positions à l'unité près : réussi

Comparaison par chaîne décimale exacte (aucune tolérance) entre l'API et le calcul indépendant des règles D409, puis lecture à l'écran (tolérance 5e-7, arrondi d'affichage).

| Compte | Encaisse | Positions (quantité @ coût moyen) |
| --- | --- | --- |
| CELI | 7873.035 | VFV 10.5 @ 100.25 ; XEQT 151.5 @ 15.414145 ; ZAG 60 @ 14.25 |
| Non enregistré | 1768.3 | XEQT 100 @ 16.0695 ; ZAG 100 @ 14.5 |

Calcul à la main du CELI (garde-fou du calcul indépendant, vérifié dans le test) : XEQT (100 - 30 + 0,75) x 2 + 10 = 151,5 ; ZAG 80 - 20 = 60 ; encaisse 10 000 - 3 059,99 - 1 140 + 958,05 + 45,60 - 170 + 2 000 - 1 052,625 + 292 = 7 873,035. Les prix et valeurs de marché ne sont pas dans `listPositions` en phase 2 (D409) : « à l'unité près » porte sur les quantités, le coût moyen et l'encaisse.

### 2b. Oracle LibreOffice de P2-07 : égal ou écart expliqué

- `docs/gates/phase-2-preuves/oracle-etf.txt` : l'oracle recalculé par LibreOffice (conteneur épinglé, même commande que `make oracle-etf`, sortie hors du dépôt) est **identique octet pour octet** à `testdata/oracle/etf_cases.json` ; les tests Python (38) et les 33 sous-tests Go d'ACHATS, de RÉÉQUILIBRAGE et de RENDEMENT passent.
- Sur la pile réelle (étapes 8, 9, 10, 12b) :

| Cas | Résultat |
| --- | --- |
| `achats-fidele-02` | **égal** : parts 200, 200, 100, encaisse 0 (= outil LibreOffice) |
| `achats-ecart-parts-01` | **égal** à `attendu_app` (179, 147, encaisse 11,94) ; l'outil donne 179,265 et 147,113 : écart `parts_fractionnaires` |
| `achats-fidele-04`, `-ecart-parts-02`, `-ecart-parts-03` | **écart expliqué, D415** : le moteur redistribue le reliquat en parts entières (D361), l'oracle le laisse en encaisse ; valeurs épinglées par le test du moteur (158/134/187/0, encaisse 22,36 ; 155/141/170/202, 23,96 ; 1/1, 50) retrouvées sur la pile ; chaque part >= celle de l'oracle |
| `reequilibrage-fidele-02` | poids avant (0,5, 0,1, 0,4), cibles et statuts des trois fonds **égaux** à l'oracle ; ordres : vendre 20 VFV, acheter 20 XIC ; poids après = cibles. Statuts de **classes** : écart expliqué D416 (l'outil juge 3 classes à ±5 points, l'app 6 classes à ±20 % relatifs) |
| `diversification-fidele-01`, `-02` | **égaux** (portions actions et obligations et répartitions, 1e-9) |
| `rendement-fidele-03` | **égal** (voir 2c) |

Plan d'achat saisi à l'écran (10 000 $, prix XEQT 31,40, VFV 112,35, ZAG 14,80, poids 0,5 / 0,2 / 0,3) : 160, 17 et 203 parts, investi 9 938,35, encaisse 61,65, égaux au calcul indépendant de D331 et D361 (calcul à la main dans le test : planchers 159, 17, 202, puis une part à ZAG et une à XEQT).

**Limite (6 cas sur 41)** : `creation-ecart-obligations-01/-02`, `diversification-ecart-vide-01/-02` et `catalogue-ecart-fige-01/-02` ne sont rejoués par aucun test du dépôt ni par une route : leur seule vérification est la cohérence interne du générateur de l'oracle. Ils décrivent des comportements de l'outil (obligations « autres », erreurs `#VALUE!` d'un portefeuille vide, catalogue figé), pas une route de l'app.

### 2c. Rendement : réussi

Relevés (cas `rendement-fidele-03` de l'oracle, 7 relevés saisis à l'écran, compte REER sans transaction) : méthode `statements`, base du TRI `cumulative`. TWR 0,05784586122910462 (oracle 0,057845861229), TRI cumulé 0,05650044695778211 (oracle AACJ du mois 6 = 0,056500446958) ; les 6 rendements mensuels et les 6 AACJ égaux à l'oracle à 5e-9 ; l'écran affiche « 5,78 % », « 5,65 % » et dit que la base est cumulée.

Transactions (méthode `transactions`, base `annualized`, XIRR jours/365), période jusqu'au 2025-12-31, comparées au calcul indépendant (TWR : produit entre flux externes, valeurs avant flux, D411 ; TRI : bissection) :

| Série | TWR API | TWR de référence | TRI API | TRI de référence |
| --- | --- | --- | --- | --- |
| CELI | 0,03726253 | 0,03726253 | 0,03519766 | 0,03519766 |
| Non enregistré | -0,00234000 | -0,00234000 | -0,00259291 | -0,00259291 |
| Consolidé | 0,02384396 | 0,02384396 | 0,02400133 | 0,02400133 |

Tolérances du test : TWR 1e-8, TRI 1e-6 (écarts observés nuls à 8 décimales). Le TWR du CELI est aussi recalculé à la main dans le test : 1,034916 x 12 377,16 / 12 349,16 - 1. Une seule série : le produit des rendements mensuels et l'année 2025 redonnent le TWR de la période (1e-9). L'écran du consolidé affiche « 2,38 % », « 2,40 % » et « taux annualisé (XIRR sur les flux datés) ».

Autres : le plan de rééquilibrage à l'écran (valeur 19 590,835 $ = positions + encaisses + cotisations ; 5 ordres, tous des achats, encaisse restante 70,335 $) donne des poids avant, des statuts de bande, des poids après (ordres appliqués) et des encaisses par compte égaux au calcul indépendant ; l'exposition (valeur des positions 7 664,125 $, RFG pondéré 0,1500 %, classes et écarts au modèle) aussi. Le plan de rééquilibrage ne passe pas par la vente en dernier recours dans ce scénario ; la vente est couverte par le cas `reequilibrage-fidele-02`.

## 3. Sécurité rejouée sur les routes de phase 2

Une exécution de `e2e/gate/gate_security_p2.py` (script voisin de `gate_security.py`, dont il réutilise les fonctions) sur la pile réelle : **80 vérifications, 0 échec** (`securite-phase2.txt`). Deux utilisateurs du realm `finance` : `test-user` (A) et `gate-user-b` (B, créé par l'API d'administration de Keycloak). Le script lit les routes sous `/accounts` et `/portfolio` dans `api/openapi.yaml` et échoue si leur liste (15) change (D483).

### 3a. Isolement par `owner_id` : réussi

A crée comptes, transactions, relevé, import CSV et modèle ; B rejoue chaque route sur les objets de A.

| Route (operationId) | Réponse pour B |
| --- | --- |
| `listAccounts` | 200, aucun compte de A |
| `createAccount` (même nom que A) | 201 : compte propre à B (nom unique par propriétaire) |
| `listTransactions`, `createTransaction`, `createStatement`, `importCsv`, `listPositions` sur un compte de A | 404 |
| `listEtfs` | 200, le profil de A est absent du catalogue de B |
| `listModels` | 200, le modèle de A est absent |
| `createModel` avec le symbole de A | 400 (le symbole n'est pas dans le catalogue de B) |
| `updateModel`, `createPurchasePlan`, `createRebalancePlan`, `getExposure`, `getPerformance` sur un objet de A | 404 |

Contrôles complémentaires : le 404 d'un modèle ou d'un compte de A est **indiscernable** de celui d'un identifiant inexistant (même titre et même détail : pas d'oracle d'existence) ; la réponse ne contient ni symbole ni quantité de A ; une écriture inverse de B visant une transaction de A répond 404 ; le consolidé de B ne montre aucun compte de A ; positions, registre (5 écritures) et modèles de A identiques avant et après ; aucune ligne d'audit créée pour A par B ; aucune ligne de la base dont le propriétaire diffère de celui de son parent (`core.transaction` / `core.account`, `etf.model_holding` / `etf.model`) : 0. Les requêtes SQL elles-mêmes (filtre `owner_id` par dépôt) restent couvertes par `make test-int` en CI ; je ne les ai pas relues une à une.

### 3b. JWT : réussi

Les 15 routes de phase 2 sans jeton : 401 `application/problem+json`. Sur `GET /accounts` et `GET /portfolio/models` : HS256 forgé avec la clé publique du JWKS, `alg: none`, signature altérée et jeton illisible : 401 (8 cas). Jeton valide : 200. **Non rejoué** : jeton expiré, autre realm, mauvaise audience (contrôle commun à toutes les routes, rejoué en phase 1 sur `/stocks`, `docs/gates/phase-1.md` 2b).

### 3c. Audit des écritures, sans montant : réussi

Des montants et quantités reconnaissables (7777.77, 123.456, 98.7654, 4242.42, 5555.55) sont envoyés ; après chaque écriture, les lignes ajoutées à `core.audit_log` sont lues avec `psql` et fouillées (`entity`, `before`, `after`) : aucun montant (D408). Lignes ajoutées : `account.create` (1), `transaction.create` (1 ; `instrument.create` en plus à la première transaction sur un nouveau symbole), `statement.create` (1), import CSV (1 `transaction.create`), `model.create` et `model.version` (2), `model.version` (1 pour une nouvelle version). Neuf lectures et plans (listes, plans, exposition, rendement) : **0 ligne** ajoutée. Acteur = `sub` du jeton et horodatage présents (`base-et-conteneurs.txt`).

### 3d. Registre en insertion seule : réussi

- Rôle applicatif `finance_app` : `permission denied` sur UPDATE, DELETE et TRUNCATE de `core.transaction`, sur UPDATE et DELETE de `etf.model_version` et de `core.audit_log`.
- Propriétaire des tables (`finance`) : UPDATE, DELETE et TRUNCATE de `core.transaction` refusés par le déclencheur (D320), dans une transaction annulée ; le nombre d'écritures du compte est inchangé.
- API : PUT, PATCH et DELETE d'une transaction répondent 404 (aucune route) ; une inverse qui ne recopie pas l'original ou une seconde inverse répondent 400 et 409 ; l'écriture inverse fonctionne (étape 14 : encaisse 1 768,3 -> 1 756,3 -> 1 768,3).

### 3e. Frontières, en-têtes, débit : réussi

- Validation (tous en `problem+json`, sans trace interne) : JSON invalide, nom de compte de 101 caractères, type de compte inconnu, montant négatif, montant hors bornes, symbole piégé (`';DROP TABLE x;--`), devise différente de celle du compte, cibles de modèle à 50 %, identifiant non UUID, courtier inconnu, CSV vide : 400 ; CSV de plus de 10 Mo et corps JSON de 2 Mo : 413.
- En-têtes sur `/`, `/api/v1/accounts` et `/api/v1/portfolio/models` : HSTS, `nosniff`, `Referrer-Policy: no-referrer`, CSP avec `frame-ancestors 'none'` ; `deploy/tests/headers.sh` : 10 contrôles ok (`en-tetes.txt`). Réserve de phase 1 inchangée : `'unsafe-inline'` dans `style-src` (D224).
- Limitation de débit : 400 requêtes `GET /api/v1/accounts` en rafale : 45 x 200, 355 x 429, aucune autre réponse ; reprise à 200 après une pause.
- **Observation (pas un critère)** : les réponses de l'API n'ont pas d'en-tête `Cache-Control` (`no-store` absent). Aucun document n'en exige un ; à envisager pour des données financières (D483).

### 3f. Surface, conteneurs, dépendances : réussi

`deploy/tests/keycloak-admin.sh` : code 0 (console, `master`, API d'administration : 403 ; login et jeton validé) ; `e2e/gate/db_checks.sh` : base sans port publié, conteneurs `api` et `web` en utilisateur 65532, système de fichiers en lecture seule, `cap_drop ALL`, `no-new-privileges` ; `govulncheck ./...` : 0 vulnérabilité appelée (une dans un module requis, non appelée ; `govulncheck.txt`). Gitleaks et Trivy tournent dans la CI de la PR.

## 4. Critère de sortie réel : en attente du propriétaire

Le critère de l'architecture (feuille de route, phase 2) : « tes positions réelles sont reconstituées à l'unité près ; le rendement d'un compte égale celui du relevé annuel du courtier, à 0,1 point près ». Rien de ce qui précède ne le prouve : ce sont des données inventées, et les relevés des courtiers n'ont aucun lien avec elles.

**Ce que le propriétaire doit fournir** (hors dépôt, jamais dans git) :

1. Ses transactions en CSV, un fichier par compte, au format de `docs/import-csv.md` (colonnes `date`, `compte`, `type`, `symbole`, `quantité`, `prix`, `devise`, `frais`, `montant`, `ratio`, `contrepartie`, `note`), couvrant tout l'historique du compte jusqu'à la date du relevé, avec les fractionnements et les réinvestissements.
2. Le relevé annuel de son courtier pour au moins un compte : les positions de fin d'année (symbole, quantité) et le rendement annuel que le courtier publie, avec la **méthode** du courtier (pondéré dans le temps ou personnel, brut ou net de frais).

**Ce qui sera comparé** : positions de l'app (`GET /accounts/{id}/positions?asOf=<date du relevé>`) contre le relevé, quantité par quantité ; rendement de l'année du compte (`GET /portfolio/performance?accounts=<id>&from=...&to=...`, TWR ou TRI selon la méthode du courtier) contre le rendement du relevé, à 0,1 point près. Limites connues à garder en tête : l'app valorise au dernier prix d'écriture, sans table de prix de marché avant la phase 3 (D410, D411) : le rendement ne sera égal à celui du courtier que si le registre contient des écritures proches des dates de valorisation ; sans cela, l'écart vient du prix, pas du calcul. Le coût moyen des titres reçus par transfert est inconnu (D409).

Résultat : **en attente du propriétaire**. Aucun résultat n'est déclaré.

Signature du propriétaire : ______________________  Date : ______________

## 5. Limites

- Données synthétiques seulement ; le catalogue de FNB est posé en SQL sur la pile de test (D485). Sur un déploiement réel, aucune route ne permet d'alimenter le catalogue, donc de créer un modèle : manque de fonction à planifier.
- L'indépendance du calcul de référence est celle d'une seconde implémentation écrite par le Vérificateur à partir des règles de `docs/decisions.md` ; une règle mal comprise des deux côtés passerait inaperçue. Les oracles LibreOffice limitent ce risque pour ACHATS, RÉÉQUILIBRAGE fidele-02, RENDEMENT fidele-03 et DIVERSIFICATION fidele ; pas pour le TWR et le TRI des comptes à transactions, ni pour l'exposition avec modèle, qui n'ont pas de cas d'oracle.
- Les poids après du rééquilibrage sont vérifiés pour un plan sans vente ; les ventes le sont sur le seul cas de l'oracle.
- Le tableau de bord du client échoue en 429 au-delà d'environ 35 comptes (D484, `docs/tasks/P2-13-fix.md`) : sans effet pour quelques comptes réels, mais le parcours doit s'exécuter sur une pile neuve.
- Jetons expirés, autre realm et mauvaise audience non rejoués sur les routes de phase 2 (contrôle commun, phase 1). Gitleaks et Trivy non rejoués en local (CI). Sauvegarde et déploiement réels : phase 1, P1-23.
- `make ci` n'exécute pas ce parcours (comme `make e2e-full`) ; le parcours est lancé à la main sur une pile `up-test`.
