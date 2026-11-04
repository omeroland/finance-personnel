#!/usr/bin/env bash
# Déploiement sur le VPS (P1-23). S'exécute SUR LE SERVEUR, lancé par .github/workflows/deploy.yml
# (ou à la main pour un retour arrière). Aucune étape n'affiche de secret : pas de `set -x`.
#
#   deploy.sh deploy   vX.Y.Z <image-api@sha256:...> <image-web@sha256:...>
#   deploy.sh rollback vX.Y.Z
#
# Étapes : valider les entrées, REFUSER toute image non signée (cosign, identité GitHub contrainte),
# garantir que le realm importé est celui de production, déchiffrer les secrets avec sops (clé age du
# serveur) dans un tmpfs, `compose pull` puis `compose up --wait`, mémoriser la version.
set -euo pipefail
umask 077

ROOT="${FINANCE_ROOT:-/opt/finance}"                       # releases/, state/, current
export SOPS_AGE_KEY_FILE="${SOPS_AGE_KEY_FILE:-/etc/finance/age.key}"
REPO="${FINANCE_GITHUB_REPOSITORY:-omeroland/finance-personnel}"
REGISTRY_PREFIX="ghcr.io/${REPO}"
OIDC_ISSUER="https://token.actions.githubusercontent.com"
TAG_RE='^v[0-9]+\.[0-9]+\.[0-9]+$'

die() { printf 'deploy: %s\n' "$*" >&2; exit 1; }

# Identité exacte du certificat Fulcio : le workflow deploy.yml de CE dépôt, sur une étiquette vX.Y.Z.
# Le point est échappé ; l'ancre ^$ interdit tout autre dépôt, workflow ou référence.
identity_regexp() {
  local repo_re=${REPO//./\\.}
  printf '^https://github\\.com/%s/\\.github/workflows/deploy\\.yml@refs/tags/v[0-9]+\\.[0-9]+\\.[0-9]+$' "$repo_re"
}

verify_image() {
  local ref=$1 kind=$2
  [[ $ref =~ ^${REGISTRY_PREFIX//./\\.}/${kind}@sha256:[0-9a-f]{64}$ ]] \
    || die "référence d'image ${kind} invalide (attendu ${REGISTRY_PREFIX}/${kind}@sha256:<64 hex>)"
  cosign verify \
    --certificate-oidc-issuer "$OIDC_ISSUER" \
    --certificate-identity-regexp "$(identity_regexp)" \
    --certificate-github-workflow-repository "$REPO" \
    --certificate-github-workflow-trigger push \
    "$ref" >/dev/null \
    || die "image ${kind} NON SIGNÉE ou signature non conforme : déploiement refusé"
  printf 'deploy: signature vérifiée pour %s\n' "$kind"
}

[[ $# -ge 2 ]] || die "usage : deploy.sh deploy <tag> <api> <web> | deploy.sh rollback <tag>"
mode=$1 tag=$2
[[ $tag =~ $TAG_RE ]] || die "étiquette invalide : $tag"
for bin in cosign sops docker; do command -v "$bin" >/dev/null || die "outil manquant : $bin (voir deploy/scripts/install-tools.sh)"; done
[[ -r $SOPS_AGE_KEY_FILE ]] || die "clé age du serveur illisible : $SOPS_AGE_KEY_FILE"

release="$ROOT/releases/$tag"
[[ -d $release/deploy ]] || die "release absente : $release"

case $mode in
  deploy)
    [[ $# -eq 4 ]] || die "usage : deploy.sh deploy <tag> <api> <web>"
    api_ref=$3 web_ref=$4
    secrets_file="$release/deploy/sops/prod.sops.env"
    ;;
  rollback)
    [[ $# -eq 2 ]] || die "usage : deploy.sh rollback <tag>"
    [[ -r $release/release.env ]] || die "release.env absent : cette version n'a jamais été déployée avec succès"
    api_ref=$(sed -n 's/^API_IMAGE=//p' "$release/release.env")
    web_ref=$(sed -n 's/^WEB_IMAGE=//p' "$release/release.env")
    # Secrets de la version actuellement en service (une rotation ne doit pas être annulée par un retour arrière).
    secrets_file="$ROOT/state/prod.sops.env"
    ;;
  *) die "mode inconnu : $mode" ;;
esac
[[ -r $secrets_file ]] || die "secrets chiffrés absents : $secrets_file (voir deploy/README.md)"

# 1. Refus des images non signées : AVANT tout pull dans la pile, AVANT tout up.
verify_image "$api_ref" api
verify_image "$web_ref" web

# 2. Realm de production seulement : aucun client ni utilisateur de test.
cd "$release/deploy"
if grep -q 'finance-test' keycloak/realm-finance.json; then
  die "keycloak/realm-finance.json contient finance-test : refus (D222)"
fi

# 3. Secrets déchiffrés en mémoire (tmpfs), supprimés à la sortie.
work=$(mktemp -d -p "${XDG_RUNTIME_DIR:-/dev/shm}" finance-deploy.XXXXXX)
trap 'rm -rf "$work"' EXIT
sops decrypt --input-type dotenv --output-type dotenv "$secrets_file" > "$work/secrets.env" \
  || die "déchiffrement sops impossible (clé age du serveur absente de .sops.yaml ?)"
origin=$(sed -n 's/^FINANCE_PUBLIC_ORIGIN=//p' "$work/secrets.env")
[[ $origin =~ ^https://[a-z0-9.-]+$ ]] || die "FINANCE_PUBLIC_ORIGIN manquant ou invalide dans les secrets (attendu https://domaine)"
for var in POSTGRES_PASSWORD KEYCLOAK_DB_PASSWORD KEYCLOAK_ADMIN_PASSWORD FINANCE_APP_PASSWORD TRAEFIK_ACME_EMAIL; do
  grep -q "^${var}=." "$work/secrets.env" || die "variable absente des secrets : $var"
done

# Configuration d'exécution du client web (identique à `make up`).
printf '{"apiUrl": "", "oidc": {"issuer": "%s/auth/realms/finance", "clientId": "finance-web"}}\n' "$origin" > web-config.json

compose() {
  API_IMAGE=$api_ref WEB_IMAGE=$web_ref \
    docker compose -f compose.yaml -f compose.prod.yaml --env-file "$work/secrets.env" --profile prod "$@"
}

# Le realm monté doit être realm-finance.json (la sortie contient des secrets : jamais affichée).
cfg=$(compose config) || die "configuration Compose invalide"
if grep -q 'realm-finance-test' <<<"$cfg"; then die "le realm de test est monté : refus"; fi
grep -q 'keycloak/realm-finance\.json' <<<"$cfg" || die "le realm de production n'est pas monté : refus"
unset cfg

# 4. Mise à jour : pull (images déjà vérifiées, référencées par digest), puis up.
compose pull api web
compose up -d --wait --wait-timeout 300 --remove-orphans \
  || { compose ps >&2 || true; die "la pile n'est pas saine après up (retour arrière : deploy.sh rollback <tag précédent>)"; }

# 5. Mémoriser la version en service.
install -d -m 700 "$ROOT/state"
previous=$(readlink "$ROOT/current" 2>/dev/null || true)
printf 'API_IMAGE=%s\nWEB_IMAGE=%s\n' "$api_ref" "$web_ref" > "$release/release.env"
cp "$secrets_file" "$ROOT/state/prod.sops.env.new" && mv "$ROOT/state/prod.sops.env.new" "$ROOT/state/prod.sops.env"
ln -sfn "$release" "$ROOT/current.new" && mv -T "$ROOT/current.new" "$ROOT/current"
printf '%s %s %s (précédent : %s)\n' "$(date -u +%FT%TZ)" "$mode" "$tag" "${previous:-aucun}" >> "$ROOT/state/history.log"
printf 'deploy: %s %s terminé\n' "$mode" "$tag"
