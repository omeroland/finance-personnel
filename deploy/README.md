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

### Administrer Keycloak (P1-13-fix, D280)

Traefik n'expose que `/auth/realms/finance/`, `/auth/resources/` et `/auth/js/` ; la console `/auth/admin`, le realm `master` et l'API d'administration répondent 403 depuis Internet. Pour administrer, depuis le serveur :

```sh
# 1. publier Keycloak sur 127.0.0.1:8180 du serveur seulement (ajoute compose.prod.yaml si tu es en production)
docker compose -f deploy/compose.yaml -f deploy/compose.admin.yaml --env-file deploy/.env --profile prod up -d keycloak
# 2. depuis ton poste : tunnel SSH, puis http://localhost:8180/auth/admin/ (compte admin, KEYCLOAK_ADMIN_PASSWORD)
ssh -N -L 8180:127.0.0.1:8180 utilisateur@serveur
# 3. à la fin : retirer la publication
docker compose -f deploy/compose.yaml --env-file deploy/.env --profile prod up -d keycloak
```

Rejouer le contrôle (pile démarrée, hors `make ci`) : `make test-keycloak-admin`.

Retour au realm de production : `make down` puis `make up`. Si `deploy/.env` a été régénéré alors qu'un ancien volume `pgdata` existe, la connexion de `migrate` échoue (mot de passe différent) : supprime ce volume de développement.

## Déploiement en production (P1-23)

Statut : **préparé, jamais exécuté.** Le VPS, le domaine et les clés n'existent pas encore ; aucun critère d'acceptation de P1-23 n'est prouvé tant que le propriétaire ne les a pas fournis.

### Fonctionnement

Une étiquette `vX.Y.Z` poussée sur un commit de `main` lance `.github/workflows/deploy.yml` (seul déclencheur ; ni branche ni PR) :

1. **controle** : étiquette valide, commit dans `main`, `realm-finance.json` sans `finance-test`, `deploy/sops/prod.sops.env` présent et chiffré.
2. **images** : construit `api` et `web` (linux/amd64), les envoie vers `ghcr.io/omeroland/finance-personnel/{api,web}`, signe chaque **digest** avec cosign sans clé (jeton OIDC de GitHub ; `id-token: write` n'existe que dans ce job), puis vérifie la signature.
3. **deploiement** (environnement GitHub `production`, approbation manuelle) : envoie l'arbre `deploy/` de l'étiquette par SSH dans `/opt/finance/releases/<tag>/`, puis exécute `deploy/scripts/deploy.sh` sur le serveur :
   - `cosign verify` de chaque image **avant** tout `pull` ou `up` : émetteur `https://token.actions.githubusercontent.com`, identité `github.com/omeroland/finance-personnel/.github/workflows/deploy.yml@refs/tags/vX.Y.Z`, dépôt et déclencheur `push`. Une image non signée, ou signée par un autre dépôt, workflow ou branche, **fait échouer le déploiement** (D272) ;
   - refus si le realm de test est monté ; import du realm de production (`realm-finance.json`, aucun client ni utilisateur de test) ;
   - déchiffrement de `prod.sops.env` par sops avec la clé age du serveur, dans un tmpfs supprimé à la sortie ; `docker compose pull`, puis `up -d --wait` avec `compose.yaml` + `compose.prod.yaml` (images par digest).
4. **test de fumée** (`deploy/tests/smoke-prod.sh`, depuis le runner) : certificat TLS valide (chaîne, nom d'hôte, au moins 7 jours), `/healthz`, page de connexion Keycloak de `finance-web`, client `finance-test` inconnu, API en 401 sans jeton, page web.

Aucun secret n'apparaît dans les journaux : pas de `set -x`, secrets GitHub masqués, SSH par fichier de configuration (rien sur la ligne de commande), secrets applicatifs déchiffrés seulement sur le serveur. Keycloak n'importe le realm qu'au premier démarrage : une modification ultérieure de `realm-finance.json` n'est pas appliquée à une base existante (D273).

### Ce que le propriétaire doit fournir

| Élément | Détail |
| --- | --- |
| VPS | Linux amd64, Docker Engine avec Compose >= 2.24.4, ports 80 et 443 ouverts. Utilisateur `deploy` dans le groupe `docker`, propriétaire de `/opt/finance` (équivalent root : clé SSH dédiée, protégée) |
| Domaine et DNS | Enregistrement A (et AAAA) du domaine vers l'IP du VPS, avant la première étiquette (Let's Encrypt TLS-ALPN-01 sur 443) |
| Outils sur le VPS | `sudo bash deploy/scripts/install-tools.sh` : cosign, sops, age, épinglés par version et sha256 |
| Clé age du serveur | `sudo install -d -m 755 /etc/finance && sudo age-keygen -o /etc/finance/age.key && sudo chown deploy /etc/finance/age.key && sudo chmod 600 /etc/finance/age.key` ; sauvegardée hors serveur (sans elle, `prod.sops.env` est illisible). Clé publique : `age-keygen -y /etc/finance/age.key`. Distincte de la clé de sauvegarde (P1-20), dont la clé privée ne quitte pas ton poste |
| Secrets chiffrés | Copier `deploy/sops/.sops.yaml.example` en `.sops.yaml` (clé publique du serveur, plus la tienne pour éditer), remplir une copie de `prod.env.example` (en `prod.env`, ignoré par git), chiffrer : `sops encrypt --input-type dotenv --output-type dotenv prod.env > prod.sops.env`, supprimer `prod.env`, committer `prod.sops.env`. Contient aussi `FINANCE_PUBLIC_ORIGIN` (`https://domaine`, sans port) et `TRAEFIK_ACME_EMAIL`. Pour la sauvegarde (profil `backup`), ajouter les variables `AGE_RECIPIENT` et `BACKUP_*` si le propriétaire les veut dans le même fichier |
| Lecture de GHCR | Sur le VPS : `docker login ghcr.io` avec un jeton classique limité à `read:packages` (images privées), ou rendre les deux paquets publics |
| Clé SSH de déploiement | `ssh-keygen -t ed25519` dédiée ; publique dans `~deploy/.ssh/authorized_keys`, privée dans le secret GitHub |
| Environnement GitHub `production` | Settings > Environments : relecteurs requis (approbation manuelle recommandée), règle de déploiement limitée aux étiquettes `v*` |

Secrets de l'environnement `production` (noms exacts) :

| Nom | Contenu |
| --- | --- |
| `DEPLOY_HOST` | nom DNS ou IP du VPS |
| `DEPLOY_PORT` | port SSH (facultatif, défaut 22) |
| `DEPLOY_USER` | utilisateur SSH (`deploy`) |
| `DEPLOY_SSH_KEY` | clé privée SSH de déploiement, complète |
| `DEPLOY_KNOWN_HOSTS` | sortie de `ssh-keyscan -t ed25519 <hôte>`, empreinte vérifiée via la console OVH |

Variable de l'environnement `production` : `FINANCE_PUBLIC_ORIGIN` = `https://domaine` (même valeur que dans `prod.sops.env`).

### Déployer une version

```sh
git tag vX.Y.Z <commit-de-main>   # le propriétaire seulement
git push origin vX.Y.Z            # lance le workflow ; approuver l'environnement `production`
```

### Retour arrière

Redéployer l'étiquette précédente, au choix :

- **Depuis GitHub** : onglet Actions, exécution du workflow `deploy` de l'étiquette précédente, « Re-run all jobs » (reconstruit, resigne et redéploie cette version, test de fumée compris) ;
- **Sur le serveur, sans reconstruction** : `/opt/finance/releases/vX.Y.Z/deploy/scripts/deploy.sh rollback vX.Y.Z` (images mémorisées par digest, signatures revérifiées, secrets de la version en service). `/opt/finance/state/history.log` liste les déploiements ; `/opt/finance/current` pointe sur la version en service.

Limites : les migrations goose vont en avant seulement. Si la version abandonnée a migré le schéma, vérifier que l'ancienne image s'en accommode, sinon restaurer la dernière sauvegarde (`deploy/backup/README.md`). Un déploiement qui échoue ne revient pas en arrière tout seul (D274).

### Ce qui n'est pas prouvé (VPS requis)

- [ ] Une étiquette déploie la version ; le test de fumée passe : non prouvé (VPS requis).
- [ ] Le serveur refuse une image non signée : non prouvé (VPS requis). Test à faire : `deploy.sh deploy` avec le digest d'une image poussée hors du workflow doit échouer à l'étape de signature.
- [ ] Retour arrière vers l'étiquette précédente testé une fois : non prouvé (VPS requis).

Vérifié sans serveur : `actionlint` et `shellcheck` sans avertissement, `docker compose config` avec `compose.prod.yaml` (images par digest, realm de production monté, aucun `build`), empreintes des outils recoupées avec les fichiers de sommes officiels.
