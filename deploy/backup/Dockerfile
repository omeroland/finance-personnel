# Conteneur de sauvegarde : pg_dump (client PostgreSQL 16), age (chiffrement), rclone (API S3).
# Base : postgres 16-alpine, même digest que deploy/compose.yaml (client = version du serveur).
# Paquets Alpine installés à la construction (bash, age, rclone) : versions non figées (D192).
FROM postgres:16-alpine@sha256:0cefaabd882e42bcb2a2f3dc56d3ce4770560ba62fd7f197f1a2e008fd5731ab
RUN apk add --no-cache bash age rclone tar
COPY scripts/ /usr/local/bin/
# Utilisateur `postgres` de l'image (uid 70) : pas de root.
USER postgres
WORKDIR /tmp
ENTRYPOINT ["/usr/local/bin/backup.sh"]
