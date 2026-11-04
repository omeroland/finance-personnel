#!/usr/bin/env bash
# Test de fumée de bout en bout sur la pile locale démarrée par `make up-test` (realm de TEST,
# jamais la production) : sondes, 401 sans jeton, puis liste et ajout d'une action avec un jeton.
# Usage : deploy/tests/smoke.sh [base_url]   (défaut : https://localhost:9443)
set -euo pipefail

base="${1:-https://localhost:9443}"
sym="TSX:SMOKE$RANDOM"

echec() { echo "ECHEC $*" >&2; exit 1; }
code() { curl -ksS -o /dev/null -w '%{http_code}' "$@"; }

[[ "$(code "$base/healthz")" == 200 ]] || echec "/healthz"
[[ "$(code "$base/readyz")" == 200 ]] || echec "/readyz (base injoignable ?)"
echo "ok   sondes /healthz et /readyz"

[[ "$(code "$base/api/v1/stocks")" == 401 ]] || echec "GET /api/v1/stocks sans jeton devrait donner 401"
echo "ok   401 sans jeton"

# Obtention directe d'un jeton : client et utilisateur du realm de test (mot de passe factice).
token="$(curl -ksS "$base/auth/realms/finance/protocol/openid-connect/token" \
	-d grant_type=password -d client_id=finance-test \
	-d username=test-user -d password=FAUX-mot-de-passe-de-test |
	python3 -c 'import sys,json; print(json.load(sys.stdin)["access_token"])')" || echec "jeton (realm de test importé ? voir make up-test)"
auth=(-H "Authorization: Bearer $token")

corps="$(curl -ksS "${auth[@]}" -w '\n%{http_code}' "$base/api/v1/stocks")"
[[ "${corps##*$'\n'}" == 200 ]] || echec "GET /api/v1/stocks avec jeton : $corps"
echo "ok   GET /api/v1/stocks -> 200 : ${corps%$'\n'*}"

rep="$(curl -ksS "${auth[@]}" -H 'Content-Type: application/json' -w '\n%{http_code}' \
	-d "{\"symbol\":\"$sym\",\"name\":\"Action de fumée\",\"quoteCurrency\":\"CAD\",\"reportingCurrency\":\"CAD\"}" \
	"$base/api/v1/stocks")"
[[ "${rep##*$'\n'}" == 201 ]] || echec "POST /api/v1/stocks : $rep"
echo "ok   POST /api/v1/stocks -> 201"

curl -ksS "${auth[@]}" "$base/api/v1/stocks" | grep -q "$sym" || echec "l'action ajoutée n'apparaît pas dans la liste"
echo "ok   GET /api/v1/stocks contient $sym"
