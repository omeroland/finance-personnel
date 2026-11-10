# P2-13-fix — Le tableau de bord appelle les positions une fois par compte

**Agent** : Frontend Angular  
**Dépend de** : P2-13  
**Statut** : à faire (non bloquante)  
**Origine** : porte de phase 2 (P2-14), `docs/gates/phase-2.md`, D484

## Contexte à lire

- CLAUDE.md, `web/CLAUDE.md`
- `web/src/app/features/portfolio/dashboard.ts` (ligne 244 : `forkJoin(l.items.map(... listPositions(compte.id)))`)
- D34 (limitation de débit de Traefik : 20 req/s, rafale de 40), D458

## Objectif

Le tableau de bord `/portefeuille` lance en parallèle un `GET /accounts/{id}/positions` par compte, plus l'exposition et le rendement. Avec une quarantaine de comptes, la rafale dépasse la limite de Traefik : les réponses 429 font échouer les sections (« Les modèles et les comptes n'ont pas pu être chargés ») et les écrans du portefeuille (exposition, rééquilibrage) restent sur l'erreur. Preuve : journal Traefik de la pile `financegate2p2`, 19:25:30, 429 sur `/accounts/{id}`, `/portfolio/exposure` et `/portfolio/performance`. Le cas réel (quelques comptes) n'est pas touché : écart de robustesse, pas un échec du critère de sortie.

Corriger côté client : limiter la concurrence des appels de positions (par lots de 5 à 10) et traiter 429 par une nouvelle tentative après `Retry-After` ou un délai. Ne pas relever la limite de débit (rôle Plateforme).

## Critères d'acceptation

- [ ] Un test e2e avec 45 comptes charge le tableau de bord sans 429 visible
- [ ] `make ci` vert

## Contraintes

Aucune logique de calcul financier dans le client.
