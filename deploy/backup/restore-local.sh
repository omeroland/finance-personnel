#!/usr/bin/env bash
# À lancer sur TON POSTE (jamais sur le serveur) : restauration complète d'un test dans un
# PostgreSQL jetable local, avec ta clé privée age. Sert au test mensuel (restore-test) ou à une
# vraie restauration (RESTORE_CMD=restore, voir README.md).
#
# Variables requises (à exporter ou dans un fichier hors dépôt, chargé par `set -a; . fichier`) :
#   AGE_IDENTITY_FILE, BACKUP_BUCKET, BACKUP_S3_ENDPOINT, BACKUP_S3_REGION,
#   BACKUP_S3_ACCESS_KEY_ID, BACKUP_S3_SECRET_ACCESS_KEY   (accès en lecture suffisant)
# Facultatif : ALERT_WEBHOOK_URL, BACKUP_IMAGE (défaut finance/backup:dev, construite si absente).
set -euo pipefail

root="$(cd "$(dirname "$0")" && pwd)"
img="${BACKUP_IMAGE:-finance/backup:dev}"
: "${AGE_IDENTITY_FILE:?AGE_IDENTITY_FILE manquant}"
[ -r "$AGE_IDENTITY_FILE" ] || { echo "clé privée illisible" >&2; exit 1; }
cmd="${RESTORE_CMD:-restore-test}"

docker image inspect "$img" >/dev/null 2>&1 || docker build -q -t "$img" "$root" >/dev/null

id="restore$$"
pgpass="$(head -c 12 /dev/urandom | od -An -tx1 | tr -d ' \n')"
net="finance-${id}"
trap 'docker rm -f "${id}-pg" >/dev/null 2>&1 || true; docker network rm "$net" >/dev/null 2>&1 || true' EXIT
docker network create "$net" >/dev/null
docker run -d --name "${id}-pg" --network "$net" --network-alias pg \
  -e POSTGRES_USER=finance -e POSTGRES_DB=finance -e POSTGRES_PASSWORD="$pgpass" \
  postgres:16-alpine@sha256:0cefaabd882e42bcb2a2f3dc56d3ce4770560ba62fd7f197f1a2e008fd5731ab >/dev/null
for _ in $(seq 1 60); do
  docker exec "${id}-pg" pg_isready -h 127.0.0.1 -U finance -d finance >/dev/null 2>&1 && break
  sleep 1
done

docker run --rm --network "$net" --read-only --tmpfs /tmp \
  -v "$AGE_IDENTITY_FILE:/run/age-identity:ro" \
  -e AGE_IDENTITY_FILE=/run/age-identity \
  -e PGHOST=pg -e PGUSER=finance -e PGPASSWORD="$pgpass" -e PGDATABASE=finance \
  -e BACKUP_BUCKET -e BACKUP_S3_ENDPOINT -e BACKUP_S3_REGION \
  -e BACKUP_S3_ACCESS_KEY_ID -e BACKUP_S3_SECRET_ACCESS_KEY -e BACKUP_S3_PATH_STYLE \
  -e ALERT_WEBHOOK_URL -e BACKUP_OBJECT \
  "$img" "$cmd"
