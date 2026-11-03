# Sauvegardes chiffrées (P1-20)

Chaque nuit : `pg_dump` (instantané cohérent) + comptes de lignes de chaque table, chiffrés avec **age**
vers ta clé publique, envoyés à OVH Object Storage (API S3), rétention de 30 jours.
Le serveur ne possède que la clé publique : sans ta clé privée, une sauvegarde est illisible.

| Fichier | Rôle |
| --- | --- |
| `Dockerfile` | Image `finance/backup` (client PostgreSQL 16, age, rclone) |
| `scripts/backup.sh` | Sauvegarde, puis rétention (`backup.sh restore\|restore-test\|prune` aiguille vers les autres) |
| `scripts/restore.sh` | Télécharge la dernière sauvegarde, déchiffre, restaure dans une base neuve, compare les lignes |
| `scripts/restore-test.sh` | Test mensuel avec alerte : complet (si clé privée) ou léger (serveur) |
| `scripts/prune.sh` | Supprime les sauvegardes de plus de 30 jours (jamais la plus récente) |
| `restore-local.sh` | Test ou restauration complète sur TON POSTE, dans un PostgreSQL jetable |
| `systemd/` | Minuteries du serveur ; `systemd/poste/` : minuterie du poste |
| `tests/` | Test d'intégration (MinIO) : `make test-backup` |

Les objets sont `finance-AAAAMMJJTHHMMSSZ.tar.age` sous `<bucket>/finance/`. L'archive contient
`finance.dump` (format custom) et `counts.tsv` (lignes par table, lues dans le même instantané).

## Mise en place (une fois, par le propriétaire)

1. **Clé age de production, sur ton poste uniquement** (rien de ceci n'est fait par un agent) :
   `age-keygen -o ~/.config/finance-privee/age-prod.txt` puis `chmod 600`. Copie la ligne
   `# public key: age1…` : c'est `AGE_RECIPIENT`. Garde la clé privée hors dépôt, hors serveur, et une
   copie hors ligne (gestionnaire de mots de passe, papier) : **sans elle, aucune sauvegarde n'est récupérable.**
2. **Bucket OVH** privé, versionnement désactivé, et un utilisateur S3 dédié avec une politique limitée à
   ce bucket (lecture/écriture/suppression pour le serveur ; un second utilisateur en lecture seule pour le poste).
3. **Serveur** : dans `deploy/.env` (hors git, chmod 600), ajouter `AGE_RECIPIENT`, `BACKUP_BUCKET`,
   `BACKUP_S3_ENDPOINT` (ex. `https://s3.gra.io.cloud.ovh.net`), `BACKUP_S3_REGION` (ex. `gra`),
   `BACKUP_S3_ACCESS_KEY_ID`, `BACKUP_S3_SECRET_ACCESS_KEY`, éventuellement `ALERT_WEBHOOK_URL`
   (voir `deploy/.env.example`). Le dépôt est attendu dans `/opt/finance-privee`.
4. `sudo cp deploy/backup/systemd/*.{service,timer} /etc/systemd/system/ && sudo systemctl daemon-reload`
   puis `sudo systemctl enable --now finance-backup.timer finance-restore-test.timer`.
5. Première sauvegarde à la main : `sudo systemctl start finance-backup.service`, puis `journalctl -u finance-backup`.
6. **Poste** : fichier `~/.config/finance-privee/restore.env` (chmod 600, hors dépôt) avec
   `AGE_IDENTITY_FILE`, `BACKUP_BUCKET`, `BACKUP_S3_*` ; copier `systemd/poste/*` dans `~/.config/systemd/user/`
   puis `systemctl --user enable --now finance-restore-full.timer`. Ou lancer `deploy/backup/restore-local.sh` à la main.

## Restaurer

```
set -a; . ~/.config/finance-privee/restore.env; set +a
RESTORE_CMD=restore deploy/backup/restore-local.sh      # vérifie la dernière sauvegarde
```

Pour restaurer pour de vrai dans la base de production : récupère l'objet (`rclone`/`aws s3 cp`), déchiffre
avec `age -d -i age-prod.txt`, extrais `finance.dump`, puis `pg_restore --no-owner -d finance_neuve finance.dump`
sur une base vide, vérifie, puis bascule (`ALTER DATABASE … RENAME`) pendant que l'API est arrêtée. Les droits du
rôle `finance_app` (D41) sont recréés par `goose up` ; le dump est pris avec `--no-owner`.

## Rotation de la clé

Générer une nouvelle clé, ajouter sa clé publique dans `AGE_RECIPIENT`, attendre 30 jours (ou rechiffrer les
anciennes sauvegardes avec l'ancienne clé), puis détruire l'ancienne clé privée.

## Coût de `make test-backup`

Compile MinIO depuis ses sources (image `tests/minio.Dockerfile`, environ 3 minutes la première fois, en cache
ensuite) puis exécute le scénario complet en quelques secondes. Hors `make ci` ; job CI `backup` dédié.
Aucune clé réelle : les clés age du test sont créées dans un répertoire temporaire puis supprimées.
