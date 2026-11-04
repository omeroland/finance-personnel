# P1-13-fix — Console d'administration Keycloak et realm master exposés par Traefik

**Agent** : Plateforme  
**Dépend de** : P1-13, P1-19  
**Statut** : à faire  
**Origine** : porte de phase 1 (P1-24), `docs/gates/phase-1.md`

## Contexte à lire

- CLAUDE.md
- docs/architecture.md § Sécurité et déploiement (« Keycloak seul point d'authentification », durcissement)
- deploy/traefik/dynamic/routes.yml (routeur `keycloak` : `PathPrefix(/auth)`, sans filtre ni limitation de débit)
- docs/gates/phase-1.md

## Objectif

Le routeur `keycloak` expose tout `/auth`, donc aussi la console d'administration et le realm `master` : sur la pile locale, `GET /auth/admin/master/console/` et `GET /auth/realms/master` répondent 200. En production, ces pages seraient joignables depuis Internet (l'API d'administration exige un jeton, mais la surface est inutile et le compte `admin` est exposé à des essais de mots de passe). Restreindre le chemin public de Keycloak à ce dont le web et l'API ont besoin : `/auth/realms/finance/` et `/auth/resources/` (et `/auth/js/` si requis) ; l'administration passe par un tunnel SSH ou une liste d'adresses autorisées.

Observation liée (sans écart propre) : la limitation de débit de Traefik (D34, `api-ratelimit`) ne couvre que `/api`, `/healthz`, `/readyz` ; `/auth` et `/` n'en ont pas (400 requêtes en rafale : 0 réponse 429 ; Keycloak a `bruteForceProtected: true`). Dire dans la PR si une limite sur `/auth/realms/finance/protocol` est ajoutée ou refusée.

## Hors portée

Changer de fournisseur d'identité. Le realm de test (jamais en production).

## Livrables

- `deploy/traefik/dynamic/routes.yml` (et `deploy/README.md` : comment administrer Keycloak)
- test dans `deploy/tests/` (ou extension de `headers.sh`)

## Critères d'acceptation

- [ ] `curl -ks -o /dev/null -w '%{http_code}' https://localhost:9443/auth/admin/master/console/` ne donne pas 200 (403 ou 404)
- [ ] Même chose pour `/auth/realms/master` et `/auth/admin/realms`
- [ ] La connexion web (code + PKCE), l'obtention des clés JWKS par l'API et `make smoke` fonctionnent : `make up-test && make smoke`
- [ ] `make e2e-full` passe (parcours complet, y compris la connexion)
- [ ] La procédure d'accès à la console d'administration est décrite dans `deploy/README.md`

## Contraintes

Ne pas modifier `realm-finance.json` pour la production sans le dire ; aucun secret dans le dépôt.

## Reproduction de l'écart

```
COMPOSE_PROJECT_NAME=financegate make up-test
for p in /auth/admin/master/console/ /auth/realms/master; do curl -ks -o /dev/null -w "$p %{http_code}\n" https://localhost:9443$p; done
# /auth/admin/master/console/ 200 ; /auth/realms/master 200
```

## Si c'est ambigu

Applique `docs/decisions.md`. Sinon, prends l'option la plus simple et réversible, ajoute une ligne Dxx dans la même PR et signale-la en tête de la description. Si cette option change un contrat, un schéma ou un résultat de règle, arrête-toi et signale-le.
