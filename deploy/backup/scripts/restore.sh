#!/usr/bin/env bash
# Restauration : télécharge la dernière sauvegarde (ou BACKUP_OBJECT), la déchiffre avec la clé
# PRIVÉE age (AGE_IDENTITY_FILE, fournie par toi, jamais stockée sur le serveur), la restaure dans
# une base NEUVE et compare le nombre de lignes de chaque table au manifeste de la sauvegarde.
# Code de sortie 0 seulement si toutes les tables concordent. Ne touche jamais la base d'origine.
set -euo pipefail
# shellcheck source=lib.sh
. "$(dirname "$0")/lib.sh"

: "${AGE_IDENTITY_FILE:?AGE_IDENTITY_FILE manquant (clé privée age, fichier local)}"
[ -r "$AGE_IDENTITY_FILE" ] || die "clé privée illisible : ${AGE_IDENTITY_FILE}"
export PGDATABASE="${PGDATABASE:-finance}"
object="${BACKUP_OBJECT:-$(latest_backup)}"
newdb="restore_$(date -u +%Y%m%d_%H%M%S)_$$"

work="$(mktemp -d)"
cleanup() {
  rm -rf "$work"
  if [ "${KEEP_RESTORED_DB:-0}" != "1" ]; then
    psql -qAt -d postgres -c "DROP DATABASE IF EXISTS \"${newdb}\"" >/dev/null 2>&1 || true
  fi
}
trap cleanup EXIT
umask 077

log "téléchargement de ${object}"
rclone copyto "$REMOTE/$object" "$work/payload.tar.age"
age -d -i "$AGE_IDENTITY_FILE" -o "$work/payload.tar" "$work/payload.tar.age" \
  || die "déchiffrement impossible (mauvaise clé privée ?)"
tar -C "$work" -xf "$work/payload.tar" finance.dump counts.tsv

psql -qAt -d postgres -v ON_ERROR_STOP=1 -c "CREATE DATABASE \"${newdb}\""
pg_restore --no-owner --no-privileges --exit-on-error -d "$newdb" "$work/finance.dump"

count_rows "$newdb" > "$work/restored.tsv"
if diff -u "$work/counts.tsv" "$work/restored.tsv" >&2; then
  n="$(wc -l < "$work/counts.tsv")"
  log "restauration OK : ${n} tables, nombre de lignes identique (base ${newdb})"
  echo "$newdb"
else
  die "les nombres de lignes diffèrent (voir ci-dessus)"
fi
