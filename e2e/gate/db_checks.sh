#!/usr/bin/env bash
# P1-24 : contrôles de la base de la pile `financegate` (après e2e/gate/gate_security.py).
#  - le rôle applicatif finance_app ne peut ni modifier ni supprimer le journal d'audit ni les évaluations archivées ;
#  - le journal contient avant/après (JSON), l'acteur (sub) et l'horodatage ;
#  - aucun port de la base n'est publié ; conteneurs api/web non root, système de fichiers en lecture seule.
# Aucun secret n'est affiché : le mot de passe de finance_app vient de deploy/.env (hors git).
set -uo pipefail
projet="${1:-financegate}"
env="${2:-deploy/.env}"
pw="$(sed -n 's/^FINANCE_APP_PASSWORD=//p' "$env")"
app() { docker exec -e PGPASSWORD="$pw" "$projet-db-1" psql -h 127.0.0.1 -U finance_app -d finance -Atc "$1" 2>&1 | head -2; }
own() { docker exec "$projet-db-1" psql -U finance -d finance -Atc "$1" 2>&1 | head -20; }

for sql in "UPDATE core.audit_log SET actor='x'" "DELETE FROM core.audit_log" \
	"UPDATE stockeval.evaluation SET mode='x'" "DELETE FROM stockeval.evaluation"; do
	echo "finance_app> $sql  =>  $(app "$sql")"
done
echo "finance_app> INSERT autorisé (attendu) : $(app "SELECT has_table_privilege('finance_app','core.audit_log','INSERT')")"
echo "--- lignes d'audit (type, acteur = propriétaire ?, avant/après présents ?)"
own "SELECT action, count(*), bool_and(actor = owner_id::text) AS acteur_ok, bool_or(before IS NOT NULL) AS a_avant, bool_or(after IS NOT NULL) AS a_apres FROM core.audit_log GROUP BY action ORDER BY action"
echo "--- exemple facts (avant/après)"
own "SELECT action, left(before::text,60), left(after::text,60), at FROM core.audit_log WHERE action LIKE 'fact.%' ORDER BY id LIMIT 2"
echo "--- ports publiés par la base : '$(docker port "$projet-db-1")' (vide attendu)"
for c in api web; do
	docker inspect "$projet-$c-1" --format '{{.Name}} user={{.Config.User}} readonly={{.HostConfig.ReadonlyRootfs}} capdrop={{.HostConfig.CapDrop}} secopt={{.HostConfig.SecurityOpt}}'
done
