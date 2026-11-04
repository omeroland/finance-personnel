#!/usr/bin/env bash
# Installe sur le VPS (linux/amd64) les outils du déploiement, épinglés par version ET par sha256 :
# cosign (vérification des signatures), sops (secrets), age (génération de la clé du serveur).
# À lancer une fois, en root : sudo bash install-tools.sh. Le téléchargement est rejeté si l'empreinte diffère.
#
# Sources des empreintes : cosign_checksums.txt (cosign) et sops-*.checksums.txt (sops), publiés avec
# chaque version ; age ne publie pas d'empreintes : valeur calculée sur l'archive officielle de la version (D270).
set -euo pipefail

COSIGN_VERSION=v3.1.4
COSIGN_SHA256=424e2c97d0b45fd7a6b608a87f090f35633f4c23ece5f092d48bba11aaf4bbfa
SOPS_VERSION=v3.13.3
SOPS_SHA256=e5bec3346a873ae91d871550f3e698c1aad962aff462a080e40f25fde17fef6b
AGE_VERSION=v1.3.2
AGE_SHA256=cbe24006683f8eb669266162894b9a522a1af52f2665fbc63a4bb032ed26ac10

[[ $(uname -sm) == "Linux x86_64" ]] || { echo "install-tools: linux/amd64 seulement" >&2; exit 1; }
dest=${INSTALL_DIR:-/usr/local/bin}
tmp=$(mktemp -d)
trap 'rm -rf "$tmp"' EXIT

fetch() { # url sha256 fichier
  curl --proto '=https' --tlsv1.2 -fsSL -o "$3" "$1"
  echo "$2  $3" | sha256sum -c --quiet - || { echo "install-tools: empreinte différente pour $1" >&2; exit 1; }
}

fetch "https://github.com/sigstore/cosign/releases/download/${COSIGN_VERSION}/cosign-linux-amd64" "$COSIGN_SHA256" "$tmp/cosign"
fetch "https://github.com/getsops/sops/releases/download/${SOPS_VERSION}/sops-${SOPS_VERSION}.linux.amd64" "$SOPS_SHA256" "$tmp/sops"
fetch "https://github.com/FiloSottile/age/releases/download/${AGE_VERSION}/age-${AGE_VERSION}-linux-amd64.tar.gz" "$AGE_SHA256" "$tmp/age.tar.gz"
tar -xzf "$tmp/age.tar.gz" -C "$tmp" age/age age/age-keygen

install -m 0755 "$tmp/cosign" "$dest/cosign"
install -m 0755 "$tmp/sops" "$dest/sops"
install -m 0755 "$tmp/age/age" "$dest/age"
install -m 0755 "$tmp/age/age-keygen" "$dest/age-keygen"
echo "install-tools: cosign ${COSIGN_VERSION}, sops ${SOPS_VERSION}, age ${AGE_VERSION} installés dans ${dest}"
