#!/usr/bin/env bash
# Test d'intégration des sauvegardes (Docker requis) : PostgreSQL + MinIO + conteneur de sauvegarde.
# Les clés age sont JETABLES, générées dans un répertoire temporaire hors du dépôt, puis détruites.
# Critères de P1-20 prouvés : aller-retour avec nombre de lignes identique, sauvegarde illisible
# sans la clé privée, rétention de 30 jours avec dates simulées, alerte du test de restauration.
set -euo pipefail

root="$(cd "$(dirname "$0")/.." && pwd)"
id="bk$$"
net="finance-${id}"
keys="$(mktemp -d)"            # hors dépôt
chmod 777 "$keys"
pgpass="$(head -c 12 /dev/urandom | od -An -tx1 | tr -d ' \n')"
s3user="test$(head -c 4 /dev/urandom | od -An -tx1 | tr -d ' \n')"
s3pass="$(head -c 12 /dev/urandom | od -An -tx1 | tr -d ' \n')"
BACKUP_IMG="finance/backup:test"
MINIO_IMG="finance/minio-test:local"
PG_IMG="postgres:16-alpine@sha256:0cefaabd882e42bcb2a2f3dc56d3ce4770560ba62fd7f197f1a2e008fd5731ab"
bucket="sauvegardes-test"

cleanup() {
  docker rm -f "${id}-pg" "${id}-minio" >/dev/null 2>&1 || true
  docker network rm "$net" >/dev/null 2>&1 || true
  rm -rf "$keys"
}
trap cleanup EXIT

pass=0
ok() { pass=$((pass + 1)); echo "  ok  - $1"; }
fail() { echo "ECHEC - $1" >&2; exit 1; }

echo "== construction des images"
docker build -q -t "$BACKUP_IMG" "$root" >/dev/null
docker build -q -t "$MINIO_IMG" -f "$root/tests/minio.Dockerfile" "$root/tests" >/dev/null

docker network create "$net" >/dev/null
docker run -d --name "${id}-pg" --network "$net" --network-alias pg \
  -e POSTGRES_USER=finance -e POSTGRES_DB=finance -e POSTGRES_PASSWORD="$pgpass" "$PG_IMG" >/dev/null
docker run -d --name "${id}-minio" --network "$net" --network-alias minio --tmpfs /data \
  -e MINIO_ROOT_USER="$s3user" -e MINIO_ROOT_PASSWORD="$s3pass" "$MINIO_IMG" server /data --address :9000 >/dev/null

# Exécute une commande dans le conteneur de sauvegarde (même réseau, même configuration).
bk() { # [VAR=val ...] -- args
  local extra=()
  while [ "${1:-}" != "--" ]; do extra+=(-e "$1"); shift; done
  shift
  docker run --rm -i --network "$net" --read-only --tmpfs /tmp -v "$keys:/keys" \
    -e PGHOST=pg -e PGUSER=finance -e PGPASSWORD="$pgpass" -e PGDATABASE=finance \
    -e BACKUP_BUCKET="$bucket" -e BACKUP_S3_ENDPOINT=http://minio:9000 -e BACKUP_S3_REGION=us-east-1 \
    -e BACKUP_S3_ACCESS_KEY_ID="$s3user" -e BACKUP_S3_SECRET_ACCESS_KEY="$s3pass" \
    "${extra[@]}" "$@"
}
# rclone appelé directement (hors lib.sh) : remote « s3 » configuré par l'environnement.
rc() {
  bk RCLONE_CONFIG_S3_TYPE=s3 RCLONE_CONFIG_S3_PROVIDER=Other RCLONE_CONFIG_S3_ENDPOINT=http://minio:9000 \
    RCLONE_CONFIG_S3_ACCESS_KEY_ID="$s3user" RCLONE_CONFIG_S3_SECRET_ACCESS_KEY="$s3pass" \
    RCLONE_CONFIG_S3_FORCE_PATH_STYLE=true RCLONE_CONFIG_S3_NO_CHECK_BUCKET=false \
    -- --entrypoint rclone "$BACKUP_IMG" "$@"
}
psql_pg() { docker exec -i "${id}-pg" psql -U finance -qAt -v ON_ERROR_STOP=1 "$@"; }

echo "== attente des services"
for _ in $(seq 1 60); do docker exec "${id}-pg" pg_isready -h 127.0.0.1 -U finance -d finance >/dev/null 2>&1 && break; sleep 1; done
docker exec "${id}-pg" pg_isready -h 127.0.0.1 -U finance -d finance >/dev/null || fail "PostgreSQL ne démarre pas"
for _ in $(seq 1 60); do rc mkdir "s3:${bucket}" >/dev/null 2>&1 && break; sleep 1; done
rc mkdir "s3:${bucket}" || fail "MinIO ne démarre pas"

echo "== clés age jetables"
for k in main other; do
  bk -- --user "$(id -u):$(id -g)" --entrypoint age-keygen "$BACKUP_IMG" -o "/keys/${k}.txt" 2>/dev/null
done
chmod 644 "$keys"/*.txt
recipient="$(grep -o 'age1[a-z0-9]*' "$keys/main.txt" | head -n 1)"
[ -n "$recipient" ] || fail "clé publique introuvable"

echo "== données de test"
psql_pg <<'SQL'
CREATE SCHEMA core; CREATE SCHEMA stockeval;
CREATE TABLE core.instrument (id serial PRIMARY KEY, symbol text NOT NULL, note text);
CREATE TABLE stockeval.watchlist (id serial PRIMARY KEY, instrument_id int REFERENCES core.instrument(id), montant numeric(20,6));
CREATE TABLE stockeval.vide (id int);
INSERT INTO core.instrument(symbol, note)
  SELECT 'SYM' || g, 'MARQUEUR_SECRET_éàü_' || g FROM generate_series(1, 250) g;
INSERT INTO stockeval.watchlist(instrument_id, montant) SELECT g, g * 1.5 FROM generate_series(1, 120) g;
SQL

echo "== sauvegarde"
bk "AGE_RECIPIENT=$recipient" -- "$BACKUP_IMG" backup
objects="$(rc lsf "s3:${bucket}/finance")"
[ "$(echo "$objects" | grep -c '^finance-[0-9]\{8\}T[0-9]\{6\}Z\.tar\.age$')" = 1 ] || fail "objet de sauvegarde attendu"
object="$(echo "$objects" | head -n 1)"
ok "sauvegarde stockée : ${object}"

echo "== illisible sans la clé privée"
stored="$keys/stored.bin"
rc cat "s3:${bucket}/finance/${object}" > "$stored"
head -c 21 "$stored" | grep -q 'age-encryption.org/v1' || fail "en-tête age absent"
if grep -a -q -e 'MARQUEUR_SECRET' -e 'PGDMP' -e 'SYM1' "$stored"; then fail "contenu en clair dans la sauvegarde"; fi
ok "objet chiffré, aucune donnée en clair"
if bk -- "$BACKUP_IMG" restore >/dev/null 2>&1; then fail "restauration sans clé acceptée"; fi
if bk AGE_IDENTITY_FILE=/keys/other.txt -- "$BACKUP_IMG" restore >/dev/null 2>&1; then fail "mauvaise clé acceptée"; fi
ok "restauration refusée sans clé ou avec une autre clé"
rm -f "$stored"

echo "== restauration dans une base neuve, nombre de lignes par table"
restored="$(bk AGE_IDENTITY_FILE=/keys/main.txt KEEP_RESTORED_DB=1 -- "$BACKUP_IMG" restore | tail -n 1)"
case "$restored" in restore_*) ;; *) fail "base restaurée inattendue : $restored" ;; esac
tables="$(psql_pg -c "SELECT format('%I.%I', schemaname, tablename) FROM pg_tables WHERE schemaname IN ('core','stockeval') ORDER BY 1")"
[ "$(echo "$tables" | wc -l)" = 3 ] || fail "3 tables attendues"
for t in $tables; do
  src="$(psql_pg -d finance -c "SELECT count(*) FROM $t")"
  dst="$(psql_pg -d "$restored" -c "SELECT count(*) FROM $t")"
  [ "$src" = "$dst" ] || fail "$t : $src lignes à la source, $dst restaurées"
  echo "      $t : $src = $dst"
done
[ "$(psql_pg -d "$restored" -c "SELECT count(*) FROM core.instrument WHERE note LIKE 'MARQUEUR_SECRET_éàü_%'")" = 250 ] || fail "contenu restauré altéré"
psql_pg -d postgres -c "DROP DATABASE \"$restored\"" >/dev/null
ok "nombre de lignes identique pour chaque table"
[ -z "$(psql_pg -c "SELECT datname FROM pg_database WHERE datname LIKE 'restore_%'")" ] || fail "base temporaire non supprimée"
bk AGE_IDENTITY_FILE=/keys/main.txt -- "$BACKUP_IMG" restore >/dev/null
[ -z "$(psql_pg -c "SELECT datname FROM pg_database WHERE datname LIKE 'restore_%'")" ] || fail "base temporaire non nettoyée"
ok "base de restauration supprimée après le test"

echo "== test de restauration (complet et léger)"
bk AGE_IDENTITY_FILE=/keys/main.txt -- "$BACKUP_IMG" restore-test >/dev/null 2>&1 || fail "test complet en échec"
bk -- "$BACKUP_IMG" restore-test >/dev/null 2>&1 || fail "test léger en échec"
ok "test mensuel réussi en modes complet et léger"
stale=$(($(date -u +%s) + 3 * 86400))
if bk "BACKUP_NOW_EPOCH=$stale" -- "$BACKUP_IMG" restore-test >"$keys/alert.log" 2>&1; then fail "sauvegarde périmée non détectée"; fi
grep -q 'ALERTE' "$keys/alert.log" || fail "pas d'alerte pour une sauvegarde périmée"
ok "alerte et code non nul si la dernière sauvegarde est trop ancienne"

echo "== rétention de 30 jours (dates simulées)"
now="$(date -u +%s)"
fake() { # jours d'âge
  local name="finance-$(date -u -d "@$((now - $1 * 86400))" +%Y%m%dT%H%M%SZ).tar.age"
  echo "factice" | rc rcat "s3:${bucket}/finance/${name}"
  echo "$name"
}
old45="$(fake 45)"; old31="$(fake 31)"; keep29="$(fake 29)"; keep1="$(fake 1)"
bk "BACKUP_NOW_EPOCH=$now" -- "$BACKUP_IMG" prune >/dev/null 2>&1
after="$(rc lsf "s3:${bucket}/finance")"
echo "$after" | grep -q "$old45" && fail "sauvegarde de 45 jours conservée"
echo "$after" | grep -q "$old31" && fail "sauvegarde de 31 jours conservée"
echo "$after" | grep -q "$keep29" || fail "sauvegarde de 29 jours supprimée"
echo "$after" | grep -q "$keep1" || fail "sauvegarde de 1 jour supprimée"
echo "$after" | grep -q "$object" || fail "sauvegarde du jour supprimée"
ok "plus de 30 jours supprimées, moins de 30 jours conservées"
# Dans 40 jours, tout est périmé, sauf la plus récente qu'on ne supprime jamais.
bk "BACKUP_NOW_EPOCH=$((now + 40 * 86400))" -- "$BACKUP_IMG" prune >/dev/null 2>&1
after="$(rc lsf "s3:${bucket}/finance")"
[ "$(echo "$after" | wc -l)" = 1 ] && echo "$after" | grep -q "$object" || fail "la plus récente doit rester seule : $after"
ok "la sauvegarde la plus récente n'est jamais supprimée"

echo "== ${pass} vérifications réussies"
