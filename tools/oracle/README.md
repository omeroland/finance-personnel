# Oracle LibreOffice (P1-07, D28)

Fait calculer la vraie feuille Simply Investing par LibreOffice, pour juger le mode compat du moteur (P1-08) contre elle.

- `make oracle` : construit l'image (`Dockerfile`, base Debian épinglée par digest, LibreOffice 7.4.7 épinglé), exécute `oracle.py` et écrit `testdata/oracle/cases.json`. Exige Docker ; **hors `make ci`**.
- `scenarios.py` : 200 cas déterministes (graine 20260101) : croissance régulière (40), BPA négatifs (30), zéros (30), trous (30), historiques courts (30), valeurs extrêmes (20), dividende gelé (19), cas documenté (1).
- `oracle.py` : écrit 10 actions par copie du gabarit (onglets `EPS Growth Calculation` et `Dividend Growth Calculation`, un bloc de 4 lignes par action dès la ligne 6, colonnes B à U), appelle `recalc_uno.py`, relit V (moyenne) et W (compte) à la ligne des taux.
- `recalc_uno.py` : LibreOffice sans interface, `document.calculateAll()` forcé (le recalcul au chargement des xlsx est désactivé par défaut), puis enregistrement.
- Tests de la logique Python, sans LibreOffice : `python3 -I -m unittest discover -s tools/oracle`.

## Format de `cases.json`

Par cas : `id`, `scenario`, `eps` et `dividends` (20 valeurs, `null` = cellule vide), puis `attendu_eps {moyenne, hausses, erreur}` et `attendu_dividendes {moyenne, hausses_consecutives, erreur}`. `moyenne` et le compte valent `null` si la feuille renvoie `""` ou une erreur ; `erreur` vaut alors `"#DIV/0!"`, etc. Les ratios sont des fractions (0.10 = 10 %).

## Cas documenté `bpa-1.00-a-1.4641-5ans`

BPA 1.00, 1.10, 1.21, 1.331, 1.4641 dans les colonnes Q à U (+10 % par an, 4 taux), le reste vide. La feuille donne une moyenne de `(0.4 - 0.1 - 0.1) / 18 = 0.01111...` et non 0.10 : c'est l'écart « moyenne divisée par 18 » ; le mode compat doit reproduire 0.0111 et 4 hausses, le mode corrigé 0.10. Côté dividendes, la feuille compte 19 hausses consécutives : les cellules vides de gauche donnent `"" > 0 %` vrai dans Calc, une autre bizarrerie que compat reproduit.
