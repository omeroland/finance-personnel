#!/usr/bin/env bash
# Rétention : supprime les sauvegardes de plus de BACKUP_RETENTION_DAYS jours (30 par défaut).
# L'âge vient de l'horodatage UTC du nom (pas de la date de l'objet), donc il est simulable
# avec BACKUP_NOW_EPOCH. La sauvegarde la plus récente n'est jamais supprimée.
set -euo pipefail
# shellcheck source=lib.sh
. "$(dirname "$0")/lib.sh"

case "$BACKUP_RETENTION_DAYS" in '' | *[!0-9]*) die "BACKUP_RETENTION_DAYS invalide" ;; esac
[ "$BACKUP_RETENTION_DAYS" -ge 1 ] || die "BACKUP_RETENTION_DAYS doit être >= 1"

cutoff=$(($(now_epoch) - BACKUP_RETENTION_DAYS * 86400))
mapfile -t objects < <(list_backups)
[ "${#objects[@]}" -gt 0 ] || { log "rétention : aucune sauvegarde"; exit 0; }

newest="${objects[${#objects[@]} - 1]}"
deleted=0
for o in "${objects[@]}"; do
  [ "$o" = "$newest" ] && continue
  if [ "$(epoch_of_name "$o")" -lt "$cutoff" ]; then
    rclone deletefile "$REMOTE/$o"
    log "rétention : ${o} supprimée"
    deleted=$((deleted + 1))
  fi
done
log "rétention : ${deleted} supprimée(s), $((${#objects[@]} - deleted)) conservée(s)"
