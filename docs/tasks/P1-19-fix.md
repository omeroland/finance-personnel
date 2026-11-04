# P1-19-fix — Trivy sur toutes les images de la pile et dans la CI

**Agent** : Plateforme  
**Dépend de** : P1-19  
**Statut** : à faire  
**Origine** : porte de phase 1 (P1-24), `docs/gates/phase-1.md`

## Contexte à lire

- CLAUDE.md
- docs/architecture.md § Sécurité et déploiement (CI/CD : « analyse (govulncheck, Trivy) »)
- docs/tasks/P1-19.md (critère « aucune vulnérabilité HIGH ou CRITICAL non justifiée »)
- docs/gates/phase-1.md, docs/gates/phase-1-preuves/trivy-images-tierces.txt

## Objectif

Deux écarts :

1. `make scan-images` ne scanne que `finance/api:dev` et `finance/web:dev` (propres : 0). Les images tierces épinglées de `deploy/compose.yaml` ne sont pas couvertes et portent des vulnérabilités HIGH/CRITICAL avec correctif : Traefik v3.6 (26 : `libssl3`/`libcrypto3` 3.5.7-r0, `go-jose` 4.1.4, `golang.org/x/crypto` 0.54.0, binaire Traefik 3.6.25), Keycloak 26.4 (92 : paquets RHEL et bibliothèques Java dont `jackson-databind` 2.19.2), PostgreSQL 16-alpine (26, toutes dans `gosu`, stdlib Go 1.24.6). Étendre le scan à toutes les images de la pile, en mettant à jour les digests vers des versions corrigées, ou en justifiant chaque exception dans `.trivyignore` (CVE, raison, échéance).
2. Trivy n'est ni dans `make ci` ni dans `.github/workflows/ci.yml` (seuls `ci`, `gitleaks`, `backup`), alors que l'architecture le prévoit. Ajouter un job CI qui construit les images et lance `make scan-images`. gitleaks de la CI utilise une action ; épingler aussi une exécution locale (`make gitleaks`, image par digest) est un plus.

## Hors portée

Changer de produit (autre proxy, autre base).

## Livrables

- `Makefile` (cible `scan-images` étendue ; cible `gitleaks` facultative), `.github/workflows/ci.yml`, `deploy/compose.yaml` (digests), `.trivyignore` (exceptions justifiées)

## Critères d'acceptation

- [ ] `make scan-images` scanne api, web, traefik, keycloak, db (et backup si construite) et sort 0 : `make scan-images; echo $?`
- [ ] `e2e/gate/trivy_tierces.sh` sort 0 : `e2e/gate/trivy_tierces.sh; echo $?`
- [ ] Un job de la CI échoue si une image porte une vulnérabilité HIGH/CRITICAL non justifiée (preuve : lien d'une exécution)
- [ ] Chaque ligne de `.trivyignore` porte la CVE, la raison et une échéance
- [ ] `make up` fonctionne avec les nouveaux digests : `make up-test && make smoke && make test-headers`

## Contraintes

Digests réels, vérifiés avec `docker buildx imagetools inspect`. Aucune modification des seuils de Trivy (HIGH, CRITICAL, `--ignore-unfixed`).

## Reproduction de l'écart

```
make scan-images; echo $?                   # 0 : api et web seulement
e2e/gate/trivy_tierces.sh; echo $?          # 1 : 26 (traefik), 92 (keycloak), 26 (postgres, gosu)
grep -n trivy .github/workflows/ci.yml      # aucune ligne
```

## Si c'est ambigu

Applique `docs/decisions.md`. Sinon, prends l'option la plus simple et réversible, ajoute une ligne Dxx dans la même PR et signale-la en tête de la description. Si cette option change un contrat, un schéma ou un résultat de règle, arrête-toi et signale-le.
