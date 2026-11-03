#!/usr/bin/env bash
# Fonctions communes. À sourcer ; ne contient aucun secret.
# Configuration par variables d'environnement (voir deploy/backup/README.md).
set -euo pipefail

: "${BACKUP_BUCKET:?BACKUP_BUCKET manquant}"
BACKUP_PREFIX="${BACKUP_PREFIX:-finance}"
BACKUP_RETENTION_DAYS="${BACKUP_RETENTION_DAYS:-30}"
BACKUP_MAX_AGE_HOURS="${BACKUP_MAX_AGE_HOURS:-36}"

# rclone sans fichier de configuration : tout vient de l'environnement (remote « s3 »).
export RCLONE_LOG_LEVEL=ERROR   # masque l'avis « fichier de configuration absent »
export RCLONE_CONFIG_S3_TYPE=s3
export RCLONE_CONFIG_S3_PROVIDER="${BACKUP_S3_PROVIDER:-Other}"
export RCLONE_CONFIG_S3_ENDPOINT="${BACKUP_S3_ENDPOINT:?BACKUP_S3_ENDPOINT manquant}"
export RCLONE_CONFIG_S3_REGION="${BACKUP_S3_REGION:-}"
export RCLONE_CONFIG_S3_ACCESS_KEY_ID="${BACKUP_S3_ACCESS_KEY_ID:?BACKUP_S3_ACCESS_KEY_ID manquant}"
export RCLONE_CONFIG_S3_SECRET_ACCESS_KEY="${BACKUP_S3_SECRET_ACCESS_KEY:?BACKUP_S3_SECRET_ACCESS_KEY manquant}"
export RCLONE_CONFIG_S3_FORCE_PATH_STYLE="${BACKUP_S3_PATH_STYLE:-true}"
export RCLONE_CONFIG_S3_NO_CHECK_BUCKET=true

REMOTE="s3:${BACKUP_BUCKET}/${BACKUP_PREFIX}"
OBJECT_RE='^finance-[0-9]{8}T[0-9]{6}Z\.tar\.age$'

log() { printf '%s %s\n' "$(date -u +%Y-%m-%dT%H:%M:%SZ)" "$*" >&2; }
die() { log "ERREUR: $*"; exit 1; }

# Heure courante (secondes epoch) ; BACKUP_NOW_EPOCH permet de simuler les dates en test.
now_epoch() { echo "${BACKUP_NOW_EPOCH:-$(date -u +%s)}"; }
stamp_of_epoch() { date -u -d "@$1" +%Y%m%dT%H%M%SZ; }
# Horodatage du nom d'objet -> secondes epoch.
epoch_of_name() {
  local s="${1#finance-}"; s="${s%.tar.age}"
  date -u -d "${s:0:4}-${s:4:2}-${s:6:2} ${s:9:2}:${s:11:2}:${s:13:2}" +%s
}

# Objets de sauvegarde, du plus ancien au plus récent (le nom porte l'horodatage UTC).
list_backups() {
  rclone lsf --files-only "$REMOTE" | { grep -E "$OBJECT_RE" || true; } | sort
}

latest_backup() {
  local last
  last="$(list_backups | tail -n 1)"
  [ -n "$last" ] || die "aucune sauvegarde dans ${REMOTE}"
  echo "$last"
}

# Tables utilisateur (catalogue ; jamais d'entrée utilisateur dans le SQL construit).
LIST_TABLES_SQL="SELECT format('%I.%I', schemaname, tablename) FROM pg_tables WHERE schemaname NOT IN ('pg_catalog','information_schema') AND schemaname NOT LIKE 'pg_toast%' ORDER BY 1"

# Comptes exacts « schéma.table<TAB>n » d'une base.
count_rows() { # $1 = base
  local db="$1" t
  while IFS= read -r t; do
    printf '%s\t%s\n' "$t" "$(psql -qAt -d "$db" -v ON_ERROR_STOP=1 -c "SELECT count(*) FROM ${t}")"
  done < <(psql -qAt -d "$db" -v ON_ERROR_STOP=1 -c "$LIST_TABLES_SQL")
}

# Alerte : journal, webhook facultatif ; le code de sortie non nul reste la source d'alerte.
alert() { # $1 = message
  log "ALERTE: $1"
  if [ -n "${ALERT_WEBHOOK_URL:-}" ]; then
    wget -q -O /dev/null -T 10 --header 'Content-Type: application/json' \
      --post-data "{\"text\":\"finance-privee: $1\"}" "$ALERT_WEBHOOK_URL" \
      || log "alerte webhook non envoyée"
  fi
}
