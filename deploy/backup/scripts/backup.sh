#!/usr/bin/env bash
# Sauvegarde : pg_dump (instantané cohérent) + comptes de lignes -> tar -> age -> S3 -> rétention.
# Le serveur n'a que la clé publique : AGE_RECIPIENT (age1…). Jamais de clé privée ici.
# Sous-commandes : backup (défaut), restore, restore-test, prune.
set -euo pipefail
here="$(dirname "$0")"

case "${1:-backup}" in
  backup) ;;
  restore | restore-test | prune) exec "$here/$1.sh" "${@:2}" ;;
  *) echo "usage: backup.sh [backup|restore|restore-test|prune]" >&2; exit 2 ;;
esac

# shellcheck source=lib.sh
. "$here/lib.sh"

: "${AGE_RECIPIENT:?AGE_RECIPIENT manquant (clé publique age1…)}"
case "$AGE_RECIPIENT" in age1*) ;; *) die "AGE_RECIPIENT doit être une clé publique age1…" ;; esac
export PGDATABASE="${PGDATABASE:-finance}"

work="$(mktemp -d)"
trap 'rm -rf "$work"' EXIT
umask 077
name="finance-$(stamp_of_epoch "$(now_epoch)").tar.age"

# Une seule transaction REPEATABLE READ : son instantané sert au pg_dump et aux comptes,
# donc le manifeste correspond exactement au contenu de la sauvegarde.
coproc PSQL { psql -qAt -d "$PGDATABASE" -v ON_ERROR_STOP=1; }
to_psql="${PSQL[1]}"
from_psql="${PSQL[0]}"

echo "BEGIN ISOLATION LEVEL REPEATABLE READ READ ONLY;" >&"$to_psql"
echo "SELECT pg_export_snapshot();" >&"$to_psql"
IFS= read -r snapshot <&"$from_psql" || true   # la ligne « BEGIN » n'est pas imprimée en mode -q
[ -n "${snapshot:-}" ] || die "instantané PostgreSQL indisponible"

pg_dump --format=custom --snapshot="$snapshot" --no-owner --file "$work/finance.dump" "$PGDATABASE"

# Tables listées dans la même session, puis comptées une à une.
echo "${LIST_TABLES_SQL};" >&"$to_psql"
echo "SELECT '--FIN--';" >&"$to_psql"
tables=()
while IFS= read -r line <&"$from_psql"; do
  [ "$line" = "--FIN--" ] && break
  tables+=("$line")
done
: > "$work/counts.tsv"
for t in "${tables[@]}"; do
  echo "SELECT count(*) FROM ${t};" >&"$to_psql"
  IFS= read -r n <&"$from_psql"
  printf '%s\t%s\n' "$t" "$n" >> "$work/counts.tsv"
done
echo "COMMIT;" >&"$to_psql"
exec {to_psql}>&-
wait "$PSQL_PID" || die "session psql en échec"

[ -s "$work/finance.dump" ] || die "sauvegarde vide"
tar -C "$work" -cf "$work/payload.tar" finance.dump counts.tsv
age -r "$AGE_RECIPIENT" -o "$work/$name" "$work/payload.tar"
rm -f "$work/payload.tar" "$work/finance.dump"

rclone copyto --s3-no-check-bucket "$work/$name" "$REMOTE/$name"
size_local="$(wc -c < "$work/$name")"
size_remote="$(rclone lsjson "$REMOTE/$name" | sed -n 's/.*"Size":\([0-9]*\).*/\1/p')"
[ "$size_local" = "$size_remote" ] || die "taille distante ($size_remote) différente de la taille locale ($size_local)"
log "sauvegarde ${name} envoyée (${size_local} octets, ${#tables[@]} tables)"

"$here/prune.sh"
