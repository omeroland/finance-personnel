#!/usr/bin/env bash
# Vérifie les en-têtes de sécurité sur / et /api (pile locale démarrée par `make up`).
# Usage : deploy/tests/headers.sh [base_url]   (défaut : https://127.0.0.1:9443)
set -euo pipefail

base="${1:-https://127.0.0.1:9443}"
echec=0

verifier() { # chemin en-tête motif
	local chemin="$1" entete="$2" motif="$3" valeur
	valeur="$(curl -ksS -D - -o /dev/null "$base$chemin" | tr -d '\r' | grep -i "^$entete:" || true)"
	if [[ "$valeur" =~ $motif ]]; then
		echo "ok   $chemin $entete"
	else
		echo "ECHEC $chemin $entete : attendu /$motif/, obtenu '${valeur:-absent}'" >&2
		echec=1
	fi
}

for chemin in / /api/v1/health; do
	verifier "$chemin" Strict-Transport-Security 'max-age=[0-9]{8,}'
	verifier "$chemin" X-Content-Type-Options 'nosniff'
	verifier "$chemin" Referrer-Policy 'no-referrer'
	verifier "$chemin" Content-Security-Policy "default-src 'self'.*connect-src 'self'.*frame-ancestors 'none'"
done

# Les métriques Traefik ne sont jamais servies en public (le repli SPA de web renvoie 200 : on cherche le contenu).
if curl -ksS "$base/metrics" | grep -q '^traefik_'; then
	echo "ECHEC /metrics expose les métriques Traefik" >&2
	echec=1
else
	echo "ok   /metrics n'expose pas de métriques"
fi

# HTTP en clair redirige vers HTTPS.
code="$(curl -sS -o /dev/null -w '%{http_code}' "http://127.0.0.1:${LOCAL_HTTP_PORT:-9080}/" || true)"
if [[ "$code" == "301" || "$code" == "308" ]]; then
	echo "ok   HTTP clair redirigé ($code)"
else
	echo "ECHEC HTTP clair : attendu 301 ou 308, obtenu $code" >&2
	echec=1
fi

exit "$echec"
