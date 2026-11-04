#!/usr/bin/env bash
# P1-13-fix : la console d'administration et le realm master ne sont pas joignables par Traefik,
# et le login du realm finance reste servi. Pile locale démarrée (make up-test) ; hors `make ci`.
# Usage : deploy/tests/keycloak-admin.sh [base_url]   (défaut : https://127.0.0.1:9443)
set -euo pipefail

base="${1:-https://127.0.0.1:9443}"
echec=0

code() { curl -ks -o /dev/null -w '%{http_code}' "$base$1"; }

attendu() { # chemin motif_de_code
	local c
	c="$(code "$1")"
	if [[ "$c" =~ ^($2)$ ]]; then echo "ok   $1 ($c)"; else echo "ECHEC $1 : attendu $2, obtenu $c" >&2; echec=1; fi
}

# Administration : refusée par Traefik (403 ; 404 accepté).
for chemin in /auth/admin/master/console/ /auth/admin/ /auth/admin/realms /auth/realms/master \
	/auth/realms/master/protocol/openid-connect/token /auth/realms/financeX /auth/realms/finance/../master \
	/auth/ /auth /auth/metrics /auth/health; do
	attendu "$chemin" '403|404'
done

# Login du realm finance : servi.
lien="/auth/realms/finance/protocol/openid-connect/auth?client_id=finance-web&response_type=code&scope=openid&redirect_uri=https://localhost:9443/&code_challenge=E9Melhoa2OwvFrEMTJguCHaoeK1t8URWbuGJSstw-cM&code_challenge_method=S256"
attendu /auth/realms/finance/.well-known/openid-configuration 200
attendu /auth/realms/finance/protocol/openid-connect/certs 200
attendu "$lien" '200|302'

# Une ressource statique du login est servie (adresse relevée dans la page de connexion).
res="$(curl -ksL "$base$lien" | grep -o '/auth/resources/[^"]*\.css' | head -1 || true)"
if [[ -n "$res" ]]; then attendu "$res" 200; else echo "ECHEC aucune ressource /auth/resources dans la page de login" >&2; echec=1; fi

exit "$echec"
