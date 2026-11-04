# Porte de phase 1 (P1-24), rapport partiel

Vérificateur, 2026-11-04. Branche `task/P1-24-partiel`. Pile neuve du projet Compose dédié `financegate` (`COMPOSE_PROJECT_NAME=financegate make up-test`, https://localhost:9443, realm de test, aucune donnée réelle), détruite à la fin par `down -v` de ce seul projet. Les volumes `finance_pgdata`, `financep112b_pgdata` et `pgdata` n'ont pas été touchés. Décisions D260 à D262.

**P1-24 reste « à faire »** : elle dépend de P1-23 (déploiement sur le VPS, non terminé), d'un point que seul le propriétaire peut fournir (la feuille réelle) et de la signature. Ce rapport ne couvre que ce qui se vérifie aujourd'hui.

Légende : **réussi** (preuve exécutée) ; **échoué, fiche** (écart, fiche de correction créée) ; **en attente du propriétaire** ; **non vérifiable sans VPS**.

## Synthèse

| # | Critère | Résultat |
| --- | --- | --- |
| 1a | Les 15 lignes de référence passent | **réussi** |
| 1b | Ta feuille réelle importée donne en compat les mêmes comptes d'échecs que Google Sheets | **en attente du propriétaire** |
| 1c | Une restauration de sauvegarde réussit | **réussi** (MinIO local) ; restauration depuis OVH Object Storage : **non vérifiable sans VPS** |
| 2a | Isolement par `owner_id` sur chaque route | **réussi** |
| 2b | Validation du JWT (cas de P1-13) sur la pile réelle | **réussi** |
| 2c | En-têtes et CSP | **réussi** avec une réserve (D224, `'unsafe-inline'` pour les styles) |
| 2d | Secrets (gitleaks, tout l'historique) | **réussi** |
| 2e | govulncheck | **réussi** |
| 2f | Trivy | `make scan-images` **réussi** (api, web) ; images tierces **échoué, fiche** `P1-19-fix` ; absent de la CI **échoué, fiche** `P1-19-fix` |
| 2g | Limitation de débit active | **réussi** sur `/api` ; `/auth` et `/` non limités (observation, `P1-13-fix`) |
| 2h | Journal d'audit alimenté par chaque écriture | **réussi** |
| 3 | Surface exposée : console d'administration Keycloak | **échoué, fiche** `P1-13-fix` |
| 4 | TLS public (Let's Encrypt), images signées, secrets sops, déploiement et retour arrière, sauvegarde vers OVH | **non vérifiable sans VPS** (P1-23) |

Fiches de correction créées : `docs/tasks/P1-19-fix.md`, `docs/tasks/P1-13-fix.md` (index `docs/tasks/README.md`, statut « à faire »).

Signature du propriétaire : ______________________  Date : ______________

## 1. Critère de sortie

### 1a. Les 15 lignes de référence : réussi

```
cd backend && go test -count=1 -v -run TestGoldenSamples ./internal/modules/stockeval/engine/ | grep -c "    --- PASS"   -> 15
make test-engine   -> ok ... coverage: 98.7% of statements
go test -count=1 -v -run TestCompatOracle ./internal/modules/stockeval/engine/   -> --- PASS: TestCompatOracle
```

Les 15 sous-tests de `TestGoldenSamples` (PEP_ligne10, NKE_ligne14, CVX_ligne13, KO_ligne8, XOM_ligne9, JNJ_ligne15, MCD_ligne12, HD_ligne9, KO_ligne11, PG_ligne16, WMT_ligne8, AAPL_ligne7, IBM_ligne11, MSFT_ligne10, AXP_ligne7) passent, sur `testdata/golden/simply_investing_samples.json` non modifié. `make test-engine` complet : 47 tests et fuzz réussis, aucun échec. L'oracle LibreOffice et les 5 actions du parcours complet ont été prouvés en P1-22 (`docs/gates/phase-1-integration.md`) ; je ne les ai pas rejoués ici.

### 1b. Feuille réelle, scores identiques à Google Sheets : en attente du propriétaire

Non fait, volontairement. Le dépôt ne contient qu'un gabarit vide (D33). Pour clore ce point, le propriétaire fournit : (1) sa feuille réelle (copie .xlsx), (2) la capture des scores qu'affiche Google Sheets (compte d'échecs par action). Je l'importe ensuite (`POST /api/v1/imports/xlsx`), je lis `GET /api/v1/stocks/{symbol}/evaluation?mode=compat` pour chaque action et je compare les comptes d'échecs. Aucun résultat n'est déclaré ici.

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

### 2f. Trivy : partiellement échoué, fiche P1-19-fix

- `make scan-images` : sort 0 ; `finance/api:dev` (debian 12.15 + binaire `server`) 0 vulnérabilité, `finance/web:dev` (alpine 3.23.7 + `caddy`) 0 vulnérabilité (`docs/gates/phase-1-preuves/scan-images.txt`). **Réussi.**
- Images tierces, que cette cible ne scanne pas (`e2e/gate/trivy_tierces.sh`, même image Trivy épinglée, HIGH/CRITICAL avec correctif) : Traefik v3.6 26, Keycloak 26.4 92, PostgreSQL 16-alpine 26 (toutes dans `gosu`, stdlib Go 1.24.6, dont CVE-2025-68121 CRITICAL). **Échoué : fiche `P1-19-fix`.** Preuve : `docs/gates/phase-1-preuves/trivy-images-tierces.txt`. Aucune de ces vulnérabilités n'a été étudiée pour son exploitabilité ici.
- Trivy n'est ni dans `make ci` ni dans la CI GitHub (jobs `ci`, `gitleaks`, `backup`), alors que l'architecture prévoit « analyse (govulncheck, Trivy) ». **Échoué : fiche `P1-19-fix`.**

### 2g. Limitation de débit : réussi sur l'API

D34 : middleware Traefik `api-ratelimit` (20 req/s, rafale de 40). Rafale de 400 requêtes `GET /api/v1/rules` avec jeton, 60 en parallèle : 45 réponses 200, **355 réponses 429**, aucune autre ; après 3 s de pause, retour à 200. **Observation** : `/auth` et `/` n'ont pas ce middleware (400 requêtes chacun : 0 réponse 429) ; Keycloak a `bruteForceProtected: true` ; la décision est renvoyée à la fiche `P1-13-fix`. La limite par adresse source derrière le vrai réseau public n'est pas vérifiable sans VPS.

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

## 3. Écart de surface : console d'administration Keycloak exposée : échoué, fiche P1-13-fix

`GET /auth/admin/master/console/` répond 200 et `GET /auth/realms/master` aussi, parce que le routeur Traefik `keycloak` expose tout `/auth`. L'API d'administration exige un jeton (`/auth/admin/realms` donne 401), mais en production la console et le realm `master` seraient joignables d'Internet. Reproduction et critères : `docs/tasks/P1-13-fix.md`.

## 4. Non vérifiable sans VPS (P1-23)

Certificat Let's Encrypt et HSTS sur le vrai domaine ; image non signée refusée par le serveur (cosign) ; secrets déchiffrés par sops sur le serveur ; import du realm de production sans client de test ; test de fumée de déploiement, retour arrière ; sauvegarde de nuit vers OVH Object Storage ; limitation de débit derrière l'adresse réelle. Le workflow de déploiement (PR #34) est une préparation non exécutée.

## Reproduire

```
COMPOSE_PROJECT_NAME=financegate make up-test
python3 e2e/gate/gate_security.py          # environ 7 min (attend l'expiration du jeton)
e2e/gate/db_checks.sh && make test-headers
make test-backup && make scan-images && e2e/gate/trivy_tierces.sh
(cd backend && govulncheck ./...)
COMPOSE_PROJECT_NAME=financegate docker compose -f deploy/compose.yaml -f deploy/compose.test-realm.yaml --env-file deploy/.env --profile local --profile prod down -v
```

## Environnement

Ports 9080, 9443, 4010, 4200, 8180 libres au départ et à la fin. Disque à 93 % : aucun cache Docker ou npm supprimé ; seuls les conteneurs, le volume `financegate_pgdata` et les réseaux du projet `financegate` ont été retirés, ainsi que `deploy/.env` et `deploy/web-config.json` générés dans le worktree (ignorés par git). Images tirées en plus : gitleaks (19 Mo).
