#!/usr/bin/env bash
# Test de restauration (mensuel) avec alerte en cas d'échec.
#  - Mode complet : si AGE_IDENTITY_FILE est lisible, restaure la dernière sauvegarde dans une base
#    neuve et compare les lignes (restore.sh). À lancer là où se trouve la clé privée : ton poste.
#  - Mode léger : sans clé privée (le serveur n'en a pas), vérifie que la dernière sauvegarde existe,
#    est récente (BACKUP_MAX_AGE_HOURS), non vide et bien chiffrée par age.
# Code de sortie non nul + alerte (journal, webhook ALERT_WEBHOOK_URL) au moindre échec.
set -euo pipefail
# shellcheck source=lib.sh
. "$(dirname "$0")/lib.sh"

fail() { alert "$1"; exit 1; }

if [ -n "${AGE_IDENTITY_FILE:-}" ] && [ -r "${AGE_IDENTITY_FILE}" ]; then
  log "test de restauration complet"
  if ! "$(dirname "$0")/restore.sh" >/dev/null; then
    fail "test de restauration complet en échec"
  fi
  log "test de restauration complet réussi"
  exit 0
fi

log "test de restauration léger (pas de clé privée sur ce serveur)"
last="$(list_backups | tail -n 1)"
[ -n "$last" ] || fail "aucune sauvegarde dans ${REMOTE}"
age_s=$(($(now_epoch) - $(epoch_of_name "$last")))
[ "$age_s" -le $((BACKUP_MAX_AGE_HOURS * 3600)) ] \
  || fail "dernière sauvegarde trop ancienne : ${last} (${age_s} s)"
header="$(rclone cat --count 22 "$REMOTE/$last" | head -c 21 || true)"
[ "$header" = "age-encryption.org/v1" ] || fail "${last} n'a pas l'en-tête age attendu"
size="$(rclone lsjson "$REMOTE/$last" | sed -n 's/.*"Size":\([0-9]*\).*/\1/p')"
[ "${size:-0}" -gt 200 ] || fail "${last} est anormalement petite (${size:-0} octets)"
log "test léger réussi : ${last} (${size} octets)"
