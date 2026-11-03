# Référentiel des règles

Extrait de `docs/architecture.md` (§ Module 1) : source de vérité des règles 5a à 11c. Les règles 1 à 4 sont qualitatives (saisies à la main, barrière « quatre oui » ; sinon l'action est *exclue*). La règle 12 est appliquée par le système (règles versionnées, archive immuable). Le texte intégral des règles 1 à 4 n'est pas fourni dans le document d'architecture : à compléter par le propriétaire.

## Moteur de règles

Le moteur est un package Go pur, sans entrées-sorties : `Evaluate(entrées, règles, mode) → Évaluation`. Il travaille en deux couches testées séparément : le calcul des mesures à partir des séries annuelles, puis le pointage des mesures contre les seuils.

**Entrées.** Quatre séries annuelles (BPA dilué, dividende par action ordinaire, rendement annuel, actions en circulation diluées) et un instantané courant (prix, dividende annuel prévu, dette LT, capitaux propres, valeur comptable par action, taux de change).

**Fenêtre.** L'horizon h prend les h dernières valeurs annuelles, donc h − 1 taux de croissance. Un horizon sans h valeurs est marqué « historique insuffisant », jamais extrapolé.

**Croissance annuelle**, reprise telle quelle de la feuille (la valeur absolue au dénominateur gère un BPA précédent négatif) :

```latex
g_t = \frac{x_t - x_{t-1}}{\lvert x_{t-1} \rvert}
```

**Moyenne.** Moyenne tronquée (on retire le min et le max) dès qu'il y a au moins 5 taux, sinon moyenne simple. C'est l'intention de la feuille (TRIMMEAN sur 20 ans, AVERAGE pour l'onglet UK à 5 ans), sans son dénominateur codé en dur.

**Référentiel des règles.** Chaque critère a un seuil strict et, pour sept d'entre eux, un seuil assoupli à utiliser avec flexibilité.

| Critère | Formule | Période | Seuil strict | Seuil assoupli |
| --- | --- | --- | --- | --- |
| 5a — Croissance du BPA | Moyenne tronquée des taux g du BPA ; BPA = bénéfice net annuel / actions en circulation | Au moins 10 ans | ≥ 8 % | ≥ 6 % |
| 5b — Hausses du BPA | Nombre de taux g > 0 dans la fenêtre | 5, 10, 15, 20 ans | ≥ 2, 4, 6, 8 | ≥ 6 sur 20 ans ; 1, 3, 4 sur 5, 10, 15 ans (D8) |
| 6 — Croissance du dividende | Moyenne tronquée des taux g du dividende par action | Au moins 10 ans | ≥ 8 % | ≥ 6 % |
| 7 — Ratio de distribution | Dividende annuel par action / BPA | Dernier exercice | 0 < x ≤ 75 % | ≤ 85 % |
| 8 — Dette | Dette à long terme / capitaux propres | Dernier bilan | 0 ≤ x ≤ 70 % | ≤ 95 % |
| 9 — Coupe de dividende | Dividende actuel vs dividende de l'année précédente | 1 an | Aucune baisse ni annulation | Aucun |
| 10 — Rachats d'actions | Actions ordinaires en circulation : année la plus récente vs année précédente | 1 an | En baisse | Aucun |
| 11a — P/E | Prix de l'action / BPA | Courant | 0 < x ≤ 25 | ≤ 35 |
| 11b — Rendement vs moyenne | Rendement actuel (dividende annuel par action / prix) vs rendement moyen | 20 ans (fenêtre de l'horizon) | Actuel > moyen, strictement | Aucun |
| 11c — P/B | Prix de l'action / valeur comptable par action | Courant | 0 < x ≤ 3 | ≤ 6 |

Les bornes basses (« > 0 ») ne s'assouplissent pas : un BPA négatif ou des capitaux propres négatifs échouent dans les deux profils.

**Statuts.** Chaque critère sort avec sa valeur, ses deux seuils et un statut : *réussi* (seuil strict), *limite* (réussi seulement au seuil assoupli), *échoué*, *donnée manquante*, *historique insuffisant* ou *exempté*. Le moteur calcule les deux profils en une seule passe ; tu n'as rien à relancer pour voir l'un ou l'autre. En mode corrigé, une donnée manquante compte comme un échec et reste signalée comme telle.

**Score.** Par horizon, deux scores sur 10 : strict et assoupli (ex. « 8/10 strict, 10/10 assoupli »), plus le compte d'échecs de la feuille (0 = parfait). Deux sous-scores : *qualité* (5a à 10, sur 7) et *prix* (11a à 11c, sur 3). Une bonne compagnie trop chère se lit donc 7/7 et 0/3.

**Verdict.** *Exclue* si une des règles 1 à 4 n'est pas à « oui ». Sinon *candidate* si 10/10 au seuil strict sur un horizon d'au moins 10 ans, *à surveiller* si 10/10 n'est atteint qu'au seuil assoupli, *rejetée* autrement. L'horizon de 5 ans reste affiché, mais seulement à titre indicatif : les règles 5a et 6 exigent au moins 10 ans.

**Règles versionnées (règle 12).** Les deux profils de seuils vivent dans un fichier `rules.yaml` embarqué et versionné dans git. Chaque évaluation archive la version des règles. Aucun résultat ne se modifie à la main, et le seuil assoupli ne remplace jamais le strict : il s'affiche à côté. C'est la règle 12 appliquée par le système : on ne peut pas assouplir une règle sous le coup de l'émotion sans que ça laisse une trace.

## Écarts entre la feuille et l'app

La feuille a huit comportements que le mode corrigé change. Le mode compat les reproduit tous, sur l'horizon de 20 ans seulement, comme la feuille.

| Écart | Dans la feuille | Effet | Mode corrigé |
| --- | --- | --- | --- |
| Moyenne divisée par 18 | `(SUM − MIN − MAX) / 18`, codé en dur | Moyenne fausse dès qu'il manque une année sur 20 | Divise par n − 2 (ou moyenne simple sous 5 taux) |
| 5b ignore une année | Somme `D8:U8` au lieu de `C8:U8` | 18 transitions comptées sur 19 | Toutes les transitions de la fenêtre |
| 5b à un seul seuil | `E7 < 8`, horizon 20 ans seulement | Pas de seuils 2, 4, 6 de ta grille | Seuils 2, 4, 6, 8 par horizon |
| « Au-dessus de 0 » absent | 7, 8, 11a, 11c testent seulement le plafond | P/E négatif (compagnie en perte) réussit 11a | Borne basse appliquée |
| Ligne vide réussie | Sheets compare `"" < 8 %` à faux | Une action sans données obtient 0 échec | Donnée manquante = échec signalé |
| Cellule vide = 0 | Un trou au milieu de la série donne une croissance de −100 % | Faux échec, puis faux rebond | Le taux n'est pas calculé ; l'horizon est marqué incomplet |
| Division par zéro | BPA ou dividende à 0 une année | `#DIV/0!` qui casse toute la ligne | Taux ignoré, avertissement |
| Règle 9 incohérente | `>= 0` (feuille principale), `> 0` (onglet exemple) | Un dividende gelé réussit ou échoue selon l'onglet | `>= 0`, comme le texte des règles |

Deux décisions MVP (D2, D3) s'écartent aussi du texte des règles :

- **Dette nulle (règle 8).** Le texte exige « au-dessus de 0 », ce qui ferait échouer une compagnie sans dette. Le mode corrigé la fait réussir ; seuls des capitaux propres négatifs font échouer.
- **Banques et assureurs (règle 8).** Leur dette LT / capitaux propres n'a pas le même sens. Un drapeau « financière » par action met la règle 8 à *exempté* ; le score devient alors sur 9. Désactivé par défaut.

**Conflits entre tes sources.** Le référentiel consolidé et la feuille ne disent pas toujours la même chose. Le mode corrigé suit le référentiel ; le mode compat suit la feuille.

| Sujet | Référentiel consolidé | Feuille | Mode corrigé |
| --- | --- | --- | --- |
| Règle 10, période | Année la plus récente vs année précédente | Onglet des règles : vs il y a 5 ans ; feuille principale : oui/non saisi | 1 an ; 5 ans configurable |
| BPA | Bénéfice net / actions en circulation (BPA de base) | BPA dilué de l'état des résultats | BPA dilué, plus prudent (D6) |
| Règles 5a et 6, période | Au moins 10 ans | Horizons de 5 à 20 ans (ta grille) | 5 ans indicatif seulement |
| Seuils assouplis | Donnés pour 5a, 5b, 6, 7, 8, 11a, 11c | Absents | Calculés et affichés à côté ; ignorés en mode compat |

**Unités.** L'onglet d'exemple mélange les unités (dette / capitaux propres d'AAPL saisie `117.92`, celle de WMT `0.6849`). L'app stocke tous les ratios en fraction (0,70 = 70 %) et convertit à l'entrée.

