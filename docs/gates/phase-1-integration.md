# Passerelle de la phase 1 : intégration bout en bout (P1-22)

Vérificateur, 2026-11-03. Branche `task/P1-22`. Pile réelle (`make up-test`, https://localhost:9443), realm de test, aucune donnée réelle.

## Verdict

| Critère | Résultat |
| --- | --- |
| Le scénario passe trois fois de suite sur une pile neuve | **Prouvé** (3 passes sur 3, 7 tests chacune) |
| Scores compat de l'app = scores LibreOffice pour les 5 actions | **Non prouvé : 4 actions sur 5 égales ; l'écart vient d'un défaut de P1-12** |

P1-22 reste « à faire » : le second critère exige les 5 actions. Un échec renvoie la tâche fautive à son agent (Défaut 1).

## Ce qui est livré

- `e2e/web/full-flow.spec.ts` et `e2e/web/playwright.full.config.ts` (hors `make e2e`, qui garde Prism et `ng serve`).
- `tools/oracle/e2e_workbook.py` : écrit 5 actions fictives par programme dans une copie du gabarit (D33), la fait recalculer par LibreOffice 7.4.7 (même `recalc_uno.py` que `make oracle`) et relit la feuille principale : D, E, F, drapeaux R à AA, compte d'échecs AB. Sorties dans `e2e/web/.generated/` (ignoré par git).
- `make e2e-full` (cible ajoutée, hors `make ci`) : produit le classeur et les attendus dans le conteneur de l'oracle, puis lance Playwright sur la pile en marche.
- Décisions D240 à D242.

Les 5 actions (entrées dérivées des séries pour que feuille et app voient les mêmes valeurs : ratio de distribution = DPS / BPA, prix = P/E x BPA) : A croissance régulière ; B croissance faible, endettée ; C année de perte et dividende gelé ; D dividende réduit, chère ; E historique de 12 ans (la feuille divise par 18).

## Scénario (`full-flow.spec.ts`, en série)

1. Connexion (test-user), import du classeur (année 2025) : « Import terminé », 5 importées ; la liste montre 5 actions.
2. Par action : `GET /evaluation?mode=compat` (jeton `finance-test`) comparé à LibreOffice (échecs, score strict, 10 statuts, 5a, 5b, 6), puis la liste en mode Compat affiche `10 - échecs`/10.
3. Fiche de TSX:FAUXD, grille : BPA 2025 changé à 30, enregistré, le score affiché change (il vient de l'API), « Archiver », l'historique montre l'évaluation avec le même score strict.

## Les trois passes

Chaque passe : `docker compose ... down -v` du seul projet `financee2e` (volume `financee2e_pgdata` recréé), `make up-test`, puis `npx playwright test -c playwright.full.config.ts` dans `e2e/web` (équivalent de `make e2e-full` sans la génération, déjà faite).

```
=== PASSE 1 ===  7 passed (9.3s)   code 0
=== PASSE 2 ===  7 passed (9.6s)   code 0
=== PASSE 3 ===  7 passed (9.5s)   code 0
```

Détail d'une passe : test 1 (import, 5 actions) vert ; comparaison FAUXA, FAUXB, FAUXC, FAUXD vertes ; FAUXE en échec attendu (`test.fail`, D241 ; Playwright affiche ✘ et le compte comme réussi) ; test 3 (fiche, saisie, score, archivage, historique) vert.

Échecs de la feuille (LibreOffice) : A 1, B 7, C 2, D 5, E 4. App en compat : A 1, B 7, C 2, D 5, **E 3**.

## Défaut 1 : 11b compat avec un historique de rendement court (tâche fautive : P1-12)

- Symptôme : TSX:FAUXE (rendement annuel présent sur 12 ans seulement) : l'app compat donne 3 échecs (7/10), LibreOffice 4 (6/10). Seule la règle 11b diffère : la feuille échoue (L < M), l'app renvoie `passed` avec l'avertissement « règle 11b: donnée absente, réussite comme dans la feuille ».
- Cause : `backend/internal/modules/stockeval/http/evaluate.go`, `compatInputs`, prend `AverageYield` du moteur corrigé (`engine/metrics.go`, `averageYield`), qui renvoie « historique insuffisant » (nil) quand la série n'a pas 20 valeurs. La feuille calcule `AVERAGE(B6:U6)` sur les cellules présentes (`'Average Dividend Yield'!V6`). Une correction côté moteur compat (P1-08) est possible aussi ; à trancher par l'agent Backend.
- Reproduction : `make up-test`, importer `e2e/web/.generated/classeur-5-actions.xlsx` (année 2025), puis `GET /api/v1/stocks/TSX:FAUXE/evaluation?mode=compat` : `failureCount` 3, critère 11b `passed`, sans valeur. Ou `make e2e-full` : le test « compat = LibreOffice pour TSX:FAUXE » est marqué `test.fail` et échouera dès la correction ; retirer alors `test.fail`.
- Les 9 autres critères de FAUXE (dont 5a = 0,055 avec la division par 18, 5b = 11) sont égaux à la feuille.

## Observation sans défaut

L'API publie les ratios arrondis à 7 décimales (5a de FAUXA : 0,0944443 contre 0,09444429539 pour la feuille, écart 4,6e-9). Les ratios sont comparés à 5e-7 (D240) ; statuts, comptes et scores le sont exactement. Si le contrat doit promettre 1e-9, à préciser côté P1-12.

## Environnement et réserves

- L'image de l'oracle ne se reconstruit pas ici : le cache de construction est absent et `apt-get` n'aboutissait pas (téléchargement bloqué) ; j'ai interrompu ma construction. L'image existante `finance-oracle:p1-07` a servi, avec `tools/oracle` monté en lecture seule sur `/oracle` (le script vient donc du dépôt). `make e2e-full` ne reconstruit que si l'image manque.
- Ports 9080, 9443, 4010, 4200, 8180 libres au départ. Disque à 92 % : rien supprimé hors du projet `financee2e` ; volumes `finance_pgdata`, `financep112b_pgdata`, `pgdata` intacts après les essais.
- `make smoke` n'est pas lancé avant le scénario : il crée une action (TSX:SMOKE...) et fausserait le compte de 5 ; exécuté une fois à part sur la première pile (vert).
- `make up-test` régénère `deploy/web-config.json` (hors git) et reconstruit `finance/api:dev` et `finance/web:dev` (mêmes étiquettes que la pile habituelle).
