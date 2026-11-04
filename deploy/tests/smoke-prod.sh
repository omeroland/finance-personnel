#!/usr/bin/env bash
# Test de fumée APRÈS déploiement en production (P1-23), lancé par .github/workflows/deploy.yml.
# Aucun jeton ni identifiant : sondes publiques seulement. Pas de -k : la chaîne TLS doit être valide.
# Usage : deploy/tests/smoke-prod.sh https://domaine   (origine sans chemin ni port)
# Vérifie : certificat TLS (chaîne, nom d'hôte, validité restante), /healthz, page de connexion Keycloak
# du client finance-web, absence du client de test, API protégée (401 sans jeton), page web.
# shellcheck disable=SC2015  # `[[ ]] && ok || ko` : ok ne échoue jamais
set -euo pipefail

origin="${1:?usage : smoke-prod.sh https://domaine}"
[[ $origin =~ ^https://([a-z0-9.-]+)$ ]] || { echo "origine invalide : attendu https://domaine" >&2; exit 2; }
host="${BASH_REMATCH[1]}"
attente="${SMOKE_WAIT_SECONDS:-180}"   # 1er déploiement : le temps d'obtenir le certificat Let's Encrypt
min_jours="${SMOKE_MIN_CERT_DAYS:-7}"
echec=0

ok()  { echo "ok    $*"; }
ko()  { echo "ECHEC $*" >&2; echec=1; }

curl_s() { curl --proto '=https' --tlsv1.2 --silent --show-error --max-time 15 "$@"; }

# 0. Attendre que le service réponde avec un certificat de confiance.
debut=$SECONDS
until curl_s -o /dev/null --fail "$origin/healthz" 2>/dev/null; do
  if (( SECONDS - debut >= attente )); then
    curl_s -o /dev/null --fail "$origin/healthz" || true
    ko "/healthz injoignable ou certificat invalide après ${attente} s"
    exit 1
  fi
  sleep 5
done

# 1. Certificat TLS : chaîne vérifiée par le magasin système, nom d'hôte, expiration.
sortie=$(echo | openssl s_client -connect "$host:443" -servername "$host" -verify_hostname "$host" -verify_return_error 2>&1 || true)
if grep -q 'Verify return code: 0 (ok)' <<<"$sortie" && grep -q 'Verified peername' <<<"$sortie"; then
  ok "certificat TLS valide pour $host"
else
  ko "certificat TLS non valide pour $host"
fi
if echo | openssl s_client -connect "$host:443" -servername "$host" 2>/dev/null | openssl x509 -noout -checkend $((min_jours * 86400)) >/dev/null; then
  ok "certificat valide encore au moins ${min_jours} jours"
else
  ko "certificat expirant sous ${min_jours} jours"
fi

# 2. /healthz
code=$(curl_s -o /dev/null -w '%{http_code}' "$origin/healthz" || true)
[[ $code == 200 ]] && ok "/healthz 200" || ko "/healthz : $code"

# 3. Page de connexion Keycloak (flux code + PKCE du client public finance-web).
challenge="E9Melhoa2OwvFrEMTJguCHaoeK1t8URWbuGJSstw-cM"
auth="$origin/auth/realms/finance/protocol/openid-connect/auth?response_type=code&scope=openid&state=smoke&code_challenge=$challenge&code_challenge_method=S256"
page=$(curl_s -L -w '\n%{http_code}' "$auth&client_id=finance-web&redirect_uri=$origin/" || true)
code=${page##*$'\n'}
if [[ $code == 200 ]] && grep -q 'name="username"' <<<"$page"; then
  ok "page de connexion Keycloak affichée"
else
  ko "page de connexion absente (code $code)"
fi

# 4. Le client de test n'existe pas en production.
code=$(curl_s -o /dev/null -w '%{http_code}' "$auth&client_id=finance-test&redirect_uri=$origin/" || true)
[[ $code == 400 ]] && ok "client finance-test absent (400)" || ko "client finance-test : code $code (attendu 400)"

# 5. API protégée : 401 sans jeton.
code=$(curl_s -o /dev/null -w '%{http_code}' "$origin/api/v1/stocks" || true)
[[ $code == 401 ]] && ok "/api/v1/stocks sans jeton : 401" || ko "/api/v1/stocks sans jeton : $code (attendu 401)"

# 6. Page web.
code=$(curl_s -o /dev/null -w '%{http_code}' "$origin/" || true)
[[ $code == 200 ]] && ok "page web 200" || ko "page web : $code"

exit "$echec"
