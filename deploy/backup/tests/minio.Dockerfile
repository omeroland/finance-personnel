# MinIO (S3 local) pour le test d'intégration des sauvegardes : remplace OVH Object Storage en CI.
# MinIO ne publie plus d'images ; on compile le commit de la version RELEASE.2025-09-07T16-13-09Z
# (dépôt minio/minio, tag 01ce918d…, commit 07c3a429…). Outil de test seulement, jamais déployé.
# Licence AGPL-3.0 : usage en CI uniquement, sans redistribution (D191).
FROM golang:1.25-alpine@sha256:1ae0735f00daffa3aaf1363a5184c0d2dc55c78e3db4ec70241cdac97bf84b59 AS build
RUN apk add --no-cache git
RUN CGO_ENABLED=0 GOBIN=/out go install -ldflags "-s -w" \
    github.com/minio/minio@07c3a429bfed433e49018cb0f78a52145d4bedeb

FROM scratch
COPY --from=build /out/minio /minio
ENTRYPOINT ["/minio"]
