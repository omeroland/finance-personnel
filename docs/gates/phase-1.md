# Porte de phase 1 (P1-24), rapport partiel

Vérificateur, 2026-11-04. Branche `task/P1-24-partiel`, mis à jour par `task/P1-24-maj` après les corrections P1-13-fix (PR #36) et P1-19-fix (PR #37) : preuves rejouées sur une seconde pile neuve du projet `financegate2` (détruite à la fin par `down -v` de ce seul projet). Pile neuve du projet Compose dédié `financegate` (`COMPOSE_PROJECT_NAME=financegate make up-test`, https://localhost:9443, realm de test, aucune donnée réelle), détruite à la fin par `down -v` de ce seul projet. Les volumes `finance_pgdata`, `financep112b_pgdata` et `pgdata` n'ont pas été touchés. Décisions D260 à D262.

**P1-24 reste « à faire »** : elle dépend de P1-23 (déploiement sur le VPS, non terminé), d'un point que seul le propriétaire peut fournir (la feuille réelle) et de la signature. Ce rapport ne couvre que ce qui se vérifie aujourd'hui.

Mise à jour (rejeu après correctifs) : les lignes 2f (images), 2g (`/auth`) et 3 passent de « échoué » à « réussi », avec les preuves ci-dessous. Mise à jour 2026-11-08 (Orchestrateur, D304 et D305) : le job Trivy est dans la CI (PR #39, exécution verte : https://github.com/omeroland/finance-personnel/actions/runs/41918783746/job/126885438403) et P1-19-fix est terminée ; la ligne 1b est reformulée (le propriétaire n'a pas d'autre feuille que le gabarit). Restent ouverts : la signature et P1-23. Le critère « exécution rouge » de P1-19-fix n'a pas été rejoué.

Légende : **réussi** (preuve exécutée) ; **échoué, fiche** (écart, fiche de correction créée) ; **en attente du propriétaire** ; **non vérifiable sans VPS**.

## Synthèse

| # | Critère | Résultat |
| --- | --- | --- |
| 1a | Les 15 lignes de référence passent | **réussi** |
| 1b | Les scores donnent les mêmes comptes d'échecs que Google Sheets / le site | **réussi** sur le gabarit (oracle LibreOffice, 200 cas, et 5 actions du parcours complet, P1-22) et sur 42 titres du site Simply Investing (168 combinaisons, 0 écart, seuils et pointage strict, D304) ; **non prouvé** : le calcul des moyennes tronquées sur des séries réelles (le site ne fournit que des agrégats) |
| 1c | Une restauration de sauvegarde réussit | **réussi** (MinIO local) ; restauration depuis OVH Object Storage : **non vérifiable sans VPS** |
| 2a | Isolement par `owner_id` sur chaque route | **réussi** |
| 2b | Validation du JWT (cas de P1-13) sur la pile réelle | **réussi** |
| 2c | En-têtes et CSP | **réussi** avec une réserve (D224, `'unsafe-inline'` pour les styles) |
| 2d | Secrets (gitleaks, tout l'historique) | **réussi** |
| 2e | govulncheck | **réussi** |
| 2f | Trivy | `make scan-images` sur toute la pile (api, backup, web, postgres, keycloak, traefik) **réussi** ; `e2e/gate/trivy_tierces.sh` **réussi** ; dans la CI depuis la PR #39 (job `trivy`, vert) |
| 2g | Limitation de débit active | **réussi** sur `/api` et sur `/auth` (D281) ; `/` non limité (accepté, D281) |
| 2h | Journal d'audit alimenté par chaque écriture | **réussi** |
| 3 | Surface exposée : console d'administration Keycloak | **réussi** (403 sur admin, `master`, API d'administration ; login et jeton validé) |
| 4 | TLS public (Let's Encrypt), images signées, secrets sops, déploiement et retour arrière, sauvegarde vers OVH | **non vérifiable sans VPS** (P1-23) |

Fiches de correction : `docs/tasks/P1-13-fix.md` (terminée, preuve rejouée ici), `docs/tasks/P1-19-fix.md` (terminée, job `trivy` dans la CI).

Signature du propriétaire : ______________________  Date : ______________

## 1. Critère de sortie

### 1a. Les 15 lignes de référence : réussi

```
cd backend && go test -count=1 -v -run TestGoldenSamples ./internal/modules/stockeval/engine/ | grep -c "    --- PASS"   -> 15
make test-engine   -> ok ... coverage: 98.7% of statements
go test -count=1 -v -run TestCompatOracle ./internal/modules/stockeval/engine/   -> --- PASS: TestCompatOracle
```

Les 15 sous-tests de `TestGoldenSamples` (PEP_ligne10, NKE_ligne14, CVX_ligne13, KO_ligne8, XOM_ligne9, JNJ_ligne15, MCD_ligne12, HD_ligne9, KO_ligne11, PG_ligne16, WMT_ligne8, AAPL_ligne7, IBM_ligne11, MSFT_ligne10, AXP_ligne7) passent, sur `testdata/golden/simply_investing_samples.json` non modifié. `make test-engine` complet : 47 tests et fuzz réussis, aucun échec. L'oracle LibreOffice et les 5 actions du parcours complet ont été prouvés en P1-22 (`docs/gates/phase-1-integration.md`) ; je ne les ai pas rejoués ici.

### 1b. Scores identiques à Google Sheets et au site : réussi sur le gabarit et 42 titres, séries réelles non prouvées

Le propriétaire n'a pas d'autre feuille que le gabarit `simply_investing_template.xlsx`, qui définit les règles de calcul. La preuve repose sur trois éléments : (1) l'oracle LibreOffice recalcule les formules du gabarit (200 cas) et le mode compat reproduit ses résultats ; (2) 5 actions importées de bout en bout donnent les mêmes comptes que LibreOffice (P1-22) ; (3) `TestSiteScoresStrict` compare nos seuils et notre pointage strict au score du site pour 42 titres, 168 combinaisons, 0 écart (`testdata/fixtures/simply_investing_site_scores.json`, D304). Limite : le site ne donne que des agrégats, pas les séries annuelles ; le calcul des moyennes tronquées et le mode compat sur des séries réelles ne sont pas comparés au site. Pour le prouver, le propriétaire fournit les séries annuelles de quelques titres.

### 1c. Restauration d'une sauvegarde : réussi (environnement local)

Commande : `make test-backup` (PostgreSQL 16 + MinIO + conteneur de sauvegarde, clés age jetables). Résultat : « 9 vérifications réussies » (`docs/gates/phase-1-preuves/test-backup.txt`) : sauvegarde stockée ; objet chiffré sans donnée en clair ; restauration refusée sans clé ou avec une autre clé ; restauration dans une base neuve avec nombre de lignes identique pour chaque table (instruments 250 = 250, liste de suivi 120 = 120…) ; base de restauration supprimée ; test mensuel complet et léger ; alerte si la dernière sauvegarde est trop ancienne ; rétention de 30 jours avec dates simulées ; la plus récente jamais supprimée.
Réserve : la preuve porte sur des données synthétiques et MinIO. La sauvegarde de nuit vers OVH Object Storage et la restauration réelle (minuteries systemd, clé publique age sur le serveur) : **non vérifiable sans VPS**.

## 2. Revue de sécurité

Toutes les vérifications 2a, 2b, 2g et 2h viennent d'une seule exécution de `e2e/gate/gate_security.py` sur la pile réelle (42 vérifications, 0 échec ; sortie complète dans `docs/gates/phase-1-preuves/securite-pile-reelle.txt`). Deux utilisateurs distincts du realm `finance` : `test-user` (A, importé) et `gate-user-b` (B, créé par l'API d'administration de Keycloak). Le script ne contient et n'affiche aucun secret.

### 2a. Isolement par `owner_id` sur chaque route : réussi

A crée `TSX:GATEA`, y écrit un instantané, des faits, un jugement, archive une évaluation et importe un classeur. B rejoue ensuite chaque route de `api/openapi.yaml` sur les objets de A :

| Route (operationId) | Réponse pour B |
| --- | --- |
| `GET /stocks` (listStocks) | 200, liste sans `TSX:GATEA` (0 élément) |
| `GET /stocks/{symbol}` (getStock) | 404 |
| `PATCH /stocks/{symbol}` (updateStock) | 404 |
| `PUT /stocks/{symbol}/facts` (replaceFacts) | 404 |
| `PUT /stocks/{symbol}/qualitative` (replaceQualitative) | 404 |
| `GET /stocks/{symbol}/evaluation` (getEvaluation) | 404 |
| `GET /stocks/{symbol}/evaluations` (listEvaluations) | 404 |
| `POST /stocks/{symbol}/evaluations` (createEvaluation) | 404 |
| `POST /stocks/{symbol}/refresh` (refreshStock) | 404 (pas 501 : la propriété est vérifiée avant) |
| `GET /imports/{id}` (getImport), id d'un job de A | 404 |
| `DELETE /stocks/{symbol}` (deleteStock) | 404 |
| `POST /stocks` (addStock) avec le même symbole | 201 : B obtient sa propre fiche (D40), celle de A est intacte |
| `POST /imports/xlsx` (importXlsx) | écriture sans objet pré-existant ; le job de B n'est lisible que par B (voir `getImport`) |
| `GET /rules` (getRules) | 200, référentiel global sans donnée d'utilisateur : n/a |

Après les tentatives de B : les faits de A sont intacts (`diluted_eps` 2025 = 7.55), son historique est présent, aucune ligne d'audit n'est créée pour B par ses écritures refusées. Couverture : les 14 opérations du contrat sont traitées ; 13 sont rejouées par B (dont `getRules`, n/a) ; `importXlsx` ne lit aucun objet existant, son isolement passe par `getImport` (job de A introuvable pour B). Les requêtes SQL elles-mêmes (filtre `owner_id` dans chaque dépôt) sont couvertes par `make test-int` en CI ; je ne les ai pas relues une à une.

### 2b. Validation du JWT : réussi

Cas de P1-13 rejoués sur la pile réelle (`GET /api/v1/stocks`) :

| Cas | Résultat |
| --- | --- |
| Sans jeton | 401 `application/problem+json` |
| Jeton d'un autre realm (realm `gateother` créé pour le test) | 401 |
| Jeton expiré (jeton valide, rejoué 30 s après `exp`, durée de 5 min) | 401 `problem+json` |
| HS256 forgé avec la clé publique du JWKS comme secret | 401 |
| Mauvaise audience (même realm, signature valide, client sans mapper `finance-api` : le jeton ne porte aucune claim `aud`) | 401 |
| `alg: none` | 401 |
| Signature altérée (claim `sub` modifiée) | 401 |
| Jeton illisible | 401 |
| Jeton valide | 200 |
| `/healthz`, `/readyz` sans jeton | 200 |
| Les 14 routes de l'API sans jeton | 401 sur chacune |

Réserve : le cas « mauvaise audience » prouve le rejet d'un jeton sans `finance-api` ; un jeton portant une autre valeur d'`aud` n'a pas été produit.

### 2c. En-têtes et CSP : réussi, avec une réserve

`make test-headers` : 10 contrôles ok (HSTS `max-age=63072000; includeSubDomains`, `nosniff`, `Referrer-Policy: no-referrer`, CSP sur `/` et `/api`, pas de métriques Traefik publiques, HTTP clair redirigé en 301). Relevé direct : `Permissions-Policy: camera=(), microphone=(), geolocation=()`, CSP `default-src 'self'; connect-src 'self'; img-src 'self' data:; style-src 'self' 'unsafe-inline'; script-src 'self'; object-src 'none'; base-uri 'self'; form-action 'self'; frame-ancestors 'none'`.

**Réserve (D224)** : `style-src` autorise `'unsafe-inline'` parce qu'Angular, Material et AG Grid injectent des `<style>` à l'exécution. `script-src 'self'` reste strict, donc l'exécution de script injecté reste bloquée ; mais une injection de style (exfiltration par sélecteurs CSS, habillage) n'est pas bloquée. Décision documentée du propriétaire, non corrigée ici ; à réévaluer avec un nonce (Angular `CSP_NONCE`) comme D224 l'indique. Autres remarques sans fiche : les pages `/auth` n'ont pas la CSP de Traefik (Keycloak envoie la sienne) ; l'en-tête `Server: Caddy` du web est visible.

### 2d. Secrets : réussi

gitleaks v8.24.3, image Docker `zricethezav/gitleaks@sha256:e1b35e12a8c6fa8901f060459cfb6b2fc4c484d3afbe3b029733a3bbfab07055` (digest lu après le `pull`), sur un clone miroir du dépôt (toutes les références, 70 commits listés par `git log --all`) : `69 commits scanned ... no leaks found`, 0 fuite. Le job `gitleaks` existe aussi dans `.github/workflows/ci.yml`. `deploy/.env` généré par `make up` est ignoré par git.

### 2e. govulncheck : réussi

`cd backend && govulncheck ./...` (Go 1.27.3, govulncheck 1.9.0, base du 2026-11-02) : « Your code is affected by 0 vulnerabilities ». Une vulnérabilité (GO-2026-5932, `golang.org/x/crypto`) est dans un module requis mais le code ne l'appelle pas ; à surveiller à la prochaine mise à jour de dépendances.

### 2f. Trivy : images et CI réussis

Rejeu sur le dépôt à jour (D290 : Traefik v3.7.17, Keycloak 26.8.1, seuils HIGH, CRITICAL, `--ignore-unfixed` inchangés, image Trivy 0.76.1 épinglée par digest).

- `make scan-images` : sort 0 (`docs/gates/phase-1-preuves/scan-images.txt`, ligne `code=0`). Il construit puis scanne les 6 images de `deploy/compose.yaml` : `finance/api`, `finance/backup`, `finance/web`, `postgres:16-alpine`, `keycloak:26.8.1`, `traefik:v3.7.17`. `finance/api` et `finance/web` sont scannés sans exception. **Réussi.**
- `e2e/gate/trivy_tierces.sh` : réparé (il ignorait la politique de D290 et ne tenait pas compte de `.trivyignore`). Il lit désormais l'image Trivy dans le Makefile (`TRIVY_IMAGE`), applique `--ignorefile .trivyignore` (les échéances `exp:` comptent) et échoue (code 2) si un scan ne produit pas de rapport, au lieu de compter 0. Aucun seuil modifié. Résultat : 0 / 0 / 0 pour postgres, keycloak, traefik, code 0 (`docs/gates/phase-1-preuves/trivy-images-tierces.txt`). **Réussi.**
- Contrôle négatif : le même scan de `postgres:16-alpine` sans `.trivyignore` trouve 26 CVE (stdlib de `gosu`) et sort 1. Les exceptions de `.trivyignore` sont donc ce qui rend le scan vert pour cette image : 26 CVE de `gosu` (Go 1.24.6) et 1 CVE guava de Keycloak, toutes avec échéance 2026-12-04, justifiées dans le fichier. Je ne juge pas leur exploitabilité ; passé cette date, le scan échoue de nouveau.
- Trivy est dans la CI GitHub depuis la PR #39 (job `trivy`, `make scan-images`, hors `make ci`) : exécution verte https://github.com/omeroland/finance-personnel/actions/runs/41918783746/job/126885438403. La preuve d'une exécution rouge (CVE retirée de `.trivyignore`) n'a pas été rejouée. **Réussi.**

### 2g. Limitation de débit : réussi sur `/api` et `/auth`

D34 : middleware Traefik `api-ratelimit` (20 req/s, rafale de 40). Rafale de 400 requêtes `GET /api/v1/rules` avec jeton, 60 en parallèle (rejeu `gate_security.py`) : une partie en 200, le reste en 429, aucune autre réponse ; après une pause, retour à 200 (`securite-pile-reelle.txt`, `debit.rafale_api`, `debit.reprise`).
D281 : middleware `auth-ratelimit` (30 req/s, rafale de 60) sur `/auth`. Rafale de 400 requêtes sur `/auth/realms/finance/.well-known/openid-configuration` : 69 x 200, 331 x 429, aucune autre ; reprise à 200 après 4 s (`rejeu-keycloak-debit-entetes.txt` : 74 x 200, 326 x 429, puis 200). **Réussi.**
`/` reste sans limite (400 x 200), accepté par D281 (fichiers statiques sans donnée) ; consigné, pas un écart. La limite par adresse source derrière le vrai réseau public n'est pas vérifiable sans VPS.

### 2h. Journal d'audit : réussi

Pour chaque route d'écriture, le nombre de lignes de `core.audit_log` ajoutées pour A a été lu avec `psql` avant et après l'appel :

| Écriture | Lignes ajoutées |
| --- | --- |
| addStock | 1 (`watchlist.add`) |
| updateStock | 1 (`snapshot.save`) |
| replaceFacts (2 valeurs) | 2 (`fact.create`) |
| replaceQualitative | 1 (`qualitative.save`) |
| createEvaluation | 1 (`evaluation.archive`) |
| importXlsx | 3 (`import_job.create`, 2 x `import_job.update`) |
| deleteStock | 1 (`watchlist.remove`) |
| refreshStock | 0 : répond 501 sans écrire (D32) |

Contrôles complémentaires (`e2e/gate/db_checks.sh`, `docs/gates/phase-1-preuves/base-et-conteneurs.txt`) : chaque ligne porte l'acteur (= `sub` du propriétaire), l'horodatage, et avant/après (JSON) selon l'action (`watchlist.remove` : avant seulement, `*.create` : après seulement) ; le rôle applicatif `finance_app` reçoit `permission denied` sur `UPDATE` et `DELETE` de `core.audit_log` et de `stockeval.evaluation` (journal et archive en insertion seule) ; la base ne publie aucun port ; les conteneurs `api` et `web` tournent en utilisateur 65532, système de fichiers en lecture seule, `cap_drop ALL`, `no-new-privileges`. Réponses d'erreur contrôlées : JSON invalide 400, symbole piégé (`';DROP TABLE x;--`) 400, corps de 2 Mo sur `/stocks` 413, fichier de 11 Mo sur l'import 413 ; toutes en `problem+json`, sans trace.

## 3. Surface exposée : console d'administration Keycloak : réussi

Écart initial (200 sur `/auth/admin/master/console/` et `/auth/realms/master`) corrigé par P1-13-fix (D280). Rejeu sur la pile `financegate2` (`deploy/tests/keycloak-admin.sh`, code 0, `rejeu-keycloak-debit-entetes.txt`) :

| Chemin | Résultat |
| --- | --- |
| `/auth/admin/master/console/`, `/auth/admin/`, `/auth/admin/realms` | 403 |
| `/auth/realms/master`, `/auth/realms/master/protocol/openid-connect/token`, `/auth/realms/finance/../master`, `/auth/realms/financeX` | 403 |
| `/auth`, `/auth/`, `/auth/metrics`, `/auth/health` | 403 |
| `/auth/realms/finance/.well-known/openid-configuration`, `/protocol/openid-connect/certs` | 200 |
| page de connexion (code + PKCE) et sa feuille de style `/auth/resources/...css` | 200 |

Login et jeton validé : `make smoke` (code 0) obtient un jeton du realm `finance`, l'API le valide (JWKS par l'adresse interne) et accepte `POST`/`GET /api/v1/stocks` ; `gate_security.py` obtient ensuite des jetons des deux utilisateurs par Traefik et l'API les accepte (200), tandis que les 8 cas invalides sont refusés (2b). Le parcours navigateur (`make e2e-full`) n'a pas été rejoué ici ; il est prouvé par la fiche P1-13-fix.
Conséquence pour la porte : `gate_security.py` administrait Keycloak par `/auth/realms/master`, désormais refusé. Il prend un paramètre `--admin-base` (défaut `http://127.0.0.1:8180`, accès local de `deploy/compose.admin.yaml`) ; seuls l'administration et le jeton du realm `gateother` (non routé par Traefik) passent par là.

## 4. Non vérifiable sans VPS (P1-23)

Certificat Let's Encrypt et HSTS sur le vrai domaine ; image non signée refusée par le serveur (cosign) ; secrets déchiffrés par sops sur le serveur ; import du realm de production sans client de test ; test de fumée de déploiement, retour arrière ; sauvegarde de nuit vers OVH Object Storage ; limitation de débit derrière l'adresse réelle. Le workflow de déploiement (PR #34) est une préparation non exécutée.

## Reproduire

```
export COMPOSE_PROJECT_NAME=financegate2
make up-test && make smoke && deploy/tests/keycloak-admin.sh && make test-headers
docker compose -f deploy/compose.yaml -f deploy/compose.test-realm.yaml -f deploy/compose.admin.yaml --env-file deploy/.env --profile local up -d --no-deps --wait keycloak   # accès d'administration local 127.0.0.1:8180
python3 -I e2e/gate/gate_security.py --project financegate2   # environ 7 min (attend l'expiration du jeton)
e2e/gate/db_checks.sh financegate2
make scan-images && e2e/gate/trivy_tierces.sh
make test-backup && (cd backend && govulncheck ./...)          # non rejoués lors de la mise à jour
docker compose -f deploy/compose.yaml -f deploy/compose.test-realm.yaml -f deploy/compose.admin.yaml --env-file deploy/.env --profile local --profile prod down -v
```

## Environnement

Ports 9080, 9443, 4010, 4200, 8180 libres au départ et à la fin. Disque à 93 % : aucun cache Docker ou npm supprimé ; seuls les conteneurs, le volume `financegate_pgdata` et les réseaux du projet `financegate` ont été retirés, ainsi que `deploy/.env` et `deploy/web-config.json` générés dans le worktree (ignorés par git). Images tirées en plus : gitleaks (19 Mo).

Mise à jour `task/P1-24-maj` : ports 9080, 9443 et 8180 libres au départ et à la fin ; seuls les conteneurs, le volume `financegate2_pgdata` et les réseaux du projet `financegate2` ont été retirés (les volumes `finance_pgdata` et `financep112b_pgdata` sont intacts). Rejeu des autres preuves sur les nouvelles images : en-têtes (10 contrôles ok), JWT et isolement (42 vérifications, 0 échec), audit et conteneurs (`base-et-conteneurs.txt`). Non rejoués car non touchés par les nouvelles images : les 15 lignes de référence (1a), la sauvegarde (1c, `make test-backup` n'a pas été relancé ; l'image `finance/backup` a été reconstruite et scannée), gitleaks (2d) et govulncheck (2e).
