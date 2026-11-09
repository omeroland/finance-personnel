Cas de l'oracle et résultats attendus.

`etf_cases.json` (P2-07) : cas de l'outil SÉLECTION ET GESTION, produits par `make oracle-etf`. Chaque cas est étiqueté `fidele` ou porte un des 9 écarts de `docs/architecture.md` (`ecart`), avec le résultat de l'outil (`outil`) et celui de l'app corrigée (`attendu_app`). `comparaison.egal` liste les groupes de sortie identiques, `comparaison.different` ceux qui divergent. Ratios en fractions, argent en dollars, `null` = vide, `"#VALUE!"` = erreur de l'outil. Données synthétiques seulement.
