# Finance privée — Architecture et conception

Nov 1, 2026 · @Omeroland

L'application de finance privée est un monolithe modulaire auto-hébergé : un backend Go, une API unique, des clients Angular et Flutter. Son premier module, **Évaluation d'actions**, automatise la grille de ta feuille Simply Investing : les 10 critères chiffrés sur 4 horizons (5, 10, 15 et 20 ans), pour le TSX et les marchés américains. Le deuxième, Portefeuille de FNB, reprend ton outil SÉLECTION ET GESTION : catalogue de FNB, construction, achats, rééquilibrage, rendement et diversification. Les modules suivants se branchent sur le même socle (comptes, titres, transactions, prix, devises, identité).

## Portée

L'application remplace tes deux classeurs : l'évaluation d'actions (Simply Investing) et la gestion de portefeuille de FNB (Outil SÉLECTION ET GESTION). Elle n'achète ni ne vend rien et ne fait aucune recommandation.

**Règle de travail.** En cas d'ambiguïté, le MVP applique la décision proposée et la consigne dans le journal des décisions MVP, en fin de document. On affine ou on corrige après le MVP, une décision à la fois.

**Fonctionnalités principales** (toutes sont prévues ; leur phase est dans la feuille de route, en fin de document)

- Liste de suivi d'actions TSX et US, avec un score par action et par horizon (5, 10, 15, 20 ans).
- Règles 1 à 4 saisies à la main comme barrière qualitative ; une action sans quatre « oui » est marquée exclue.
- Critères 5a à 11c calculés automatiquement, avec le détail règle par règle (valeur, seuil, statut).
- Deux modes de calcul : **corrigé** (par défaut) et **compat** (reproduit exactement la feuille, pour comparer).
- Trois sources de données : saisie manuelle, import de la feuille xlsx, fournisseur API.
- Historique immuable des évaluations (règle 12 appliquée par le système).
- Portefeuille de FNB : catalogue, portefeuille modèle, achat initial, rééquilibrage, rendement et diversification, sur plusieurs comptes.
- Clients Angular (web) et Flutter (mobile) sur la même API.

**Hors périmètre, toutes phases confondues**

- Passage d'ordres chez le courtier : l'app prépare les ordres, tu les saisis toi-même.
- Recommandations : l'app filtre, calcule et archive ; elle ne conseille pas.

**Exigences non fonctionnelles**

| Exigence | Cible |
| --- | --- |
| Évaluation d'une action | < 50 ms (calcul pur, en mémoire) |
| Rafraîchissement d'une liste de 200 actions | < 5 min, borné par les quotas du fournisseur |
| Reproductibilité | Une évaluation archivée se recalcule à l'identique à partir de ses entrées |
| Disponibilité | Usage perso : un seul conteneur, redémarrage auto, sauvegarde quotidienne de la base |
| Confidentialité | Auto-hébergé sur ton VPS, aucune donnée de portefeuille chez un tiers |

## Vue d'ensemble

&#91;embedded content: architecture · clients, backend modulaire, services externes\]

Les clients ne calculent rien : tout passe par l'API. Le moteur de règles ne fait aucune entrée-sortie, et seule l'ingestion parle au fournisseur de données. Le module Portefeuille de FNB s'ajoute à côté du premier, et les modules suivants de la même façon : même socle, même binaire.

**Pourquoi un monolithe modulaire.** Un seul utilisateur, un seul VPS : des microservices ajouteraient du réseau, des déploiements et des pannes sans bénéfice. Les frontières entre modules sont tenues dans le code (un package Go par module, un schéma PostgreSQL par module, aucun accès direct aux tables d'un autre module) ; un module pourra sortir en service séparé plus tard sans réécriture.

## Module 1 — Évaluation d'actions : moteur de règles

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

## Module 1 — Écarts entre la feuille et l'app

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

## Module 2 — Portefeuille de FNB

L'app garde les six fonctions de ton outil, mais remplace les montants saisis à la main par un registre de transactions et des prix, sur plusieurs comptes. La valeur, l'exposition et le rendement deviennent des calculs, plus des saisies.

| Onglet | Ce qu'il fait dans l'outil | Ce qu'il devient dans l'app |
| --- | --- | --- |
| FNB | Catalogue de 137 FNB : type, région, répartition en 5 classes, RFG, taille, ESG, rendements 1 à 10 ans | Catalogue d'instruments partagé avec le module 1 ; répartition et RFG datés et rafraîchis par le fournisseur |
| CRÉATION | Cible actions / obligations, sous-répartition des actions (27,5 % CA, 43,5 % US, 29 % international), jusqu'à 20 fonds ; exposition et RFG pondérés | Portefeuille modèle versionné : cibles par classe, fonds retenus, exposition calculée par transparence |
| ACHATS | Montant à investir × % alloué ÷ prix = nombre de parts | Plan d'achat en parts entières, reste en encaisse, ordres prêts à saisir chez le courtier |
| RÉÉQUILIBRAGE | % actuel vs cible ; « À équilibrer » hors d'une bande de ±20 % relatifs (fonds) ou au-delà de la cible + 5 points (encaisse) ; montant et parts à bouger | Moteur de rééquilibrage : bandes configurables, cotisations utilisées avant toute vente, ordres en parts entières |
| RENDEMENT | Valeur de fin de mois et cotisations saisies ; rendement mensuel, AACJ par TRI, rendements annuels saisis à part, rendement annualisé | Rendement tiré des transactions et des prix : pondéré dans le temps et personnel (TRI), par compte et consolidé |
| DIVERSIFICATION | Exposition réelle d'un portefeuille existant (12 fonds) vs les cibles | Vue de transparence sur toutes tes positions réelles, comparée au modèle |

**Le socle change.** Ce module fait entrer des comptes et des positions dans l'app ; ils vont dans le socle, pas dans le module, parce que les modules suivants en auront besoin aussi :

- **Comptes** : type (CELI, REER, CELIAPP, REEE, non enregistré), courtier, devise.
- **Instruments** : actions et FNB dans une seule table ; le module 1 évalue les actions, le module 2 compose avec les FNB.
- **Transactions** : achat, vente, dividende, distribution réinvestie, cotisation, retrait, frais, transfert, fractionnement. Registre en insertion seule ; une erreur se corrige par une écriture inverse.
- **Positions** : jamais saisies, toujours dérivées du registre.
- **Relevé de valeur** : une valeur de fin de mois saisie pour un compte sans transactions, comme dans ton onglet RENDEMENT. Ça permet de migrer sans ressaisir l'historique.

**Exposition par transparence.** Pour chaque classe c (actions CA, US, international, obligations CA, autres), avec w\_f le poids du fonds f et a\_f,c la part de la classe c dans ce fonds :

```latex
E_c = \frac{\sum_f w_f \, a_{f,c}}{\sum_f w_f}
```

Le RFG pondéré se calcule de la même façon. Une sixième classe, « autres actifs » (immobilier, or, crypto), recueille ce que les 5 classes ne couvrent pas.

**Rééquilibrage, dans l'ordre :**

1. Détecter : une classe ou un fonds hors de sa bande (par défaut, celles de ton outil : ±20 % relatifs, encaisse cible + 5 points).
2. Acheter avec l'encaisse et les nouvelles cotisations ce qui est sous-pondéré, sans rien vendre.
3. Si c'est encore hors bande, vendre ce qui est surpondéré, en commençant par les comptes enregistrés (aucun impact fiscal).
4. Arrondir aux parts entières : arrondi vers le bas, puis le reste va aux fonds les plus sous-pondérés, une part à la fois.
5. Ignorer les ordres sous un montant minimum, pour ne pas payer des frais pour un ordre de quelques dollars.

**Rendement.** Deux mesures, parce qu'elles répondent à deux questions. Le rendement pondéré dans le temps chaîne les sous-périodes entre deux flux : c'est la performance des placements, comparable à un indice. Le TRI (XIRR sur les flux datés) est ton rendement à toi, qui dépend du moment de tes cotisations. Un compte sans transactions utilise la formule de ton outil, par mois, à partir des relevés de valeur.

**Données du catalogue.** La répartition par région et classe et le RFG d'un FNB viennent du fournisseur ; c'est à tester au même moment que les fondamentaux du module 1, sur une dizaine de FNB canadiens. À défaut, saisie manuelle avec une date de mise à jour visible.

**Import.** Ton outil s'importe une fois (catalogue, portefeuille modèle, positions actuelles). Ensuite, les relevés CSV des courtiers alimentent le registre, avec un adapter par courtier.

## Module 2 — Écarts entre l'outil et l'app

L'outil a neuf problèmes que l'app corrige. Pas de mode compat ici : contrairement à la grille Simply Investing, il n'y a pas de méthode publiée à reproduire au chiffre près.

| Écart | Dans l'outil | Effet | Dans l'app |
| --- | --- | --- | --- |
| Totaux d'ACHATS décalés | `F30` et `H30` multiplient `F3:F29` par `E32:E51` | Plages de tailles différentes : totaux en erreur | Totaux recalculés sur les bonnes lignes |
| Parts fractionnaires | `Montant / Prix`, sans arrondi | Plan inapplicable chez la plupart des courtiers | Parts entières, reste en encaisse |
| Fonds partiellement classés | Le total du rééquilibrage additionne seulement actions + obligations de chaque fonds | Un FNB d'or, d'immobilier ou de crypto disparaît en partie du total ; tous les % actuels sont faux | Valeur de marché réelle ; classe « autres actifs » |
| Cibles au-delà de 100 % | La cible d'encaisse s'ajoute à des cibles de fonds qui font déjà 100 % | Les fonds paraissent toujours sous-pondérés dès que l'encaisse a une cible | Cibles normalisées, total validé à 100 % |
| Obligations « autres » sans cible | CRÉATION fixe une seule cible pour « CAD et autres » ; la comparaison se fait sur CAD seulement | Écart d'obligations mal mesuré | Une cible par classe |
| Erreurs quand c'est vide | DIVERSIFICATION divise sans protection | `#VALUE!` et `#DIV/0!` | Vide = aucun résultat, pas d'erreur |
| Catalogue figé en 2021 | Rendements, tailles et ESG datés de 2021, saisis en texte (« 19,6 % », « 880M$ ») | Données périmées, impossibles à calculer ou trier | Valeurs numériques, datées, rafraîchies |
| Rendements annuels à part | Saisis à la main, sans lien avec le calcul mensuel | Deux vérités qui peuvent diverger | Une seule série, calculée |
| Un compte par fichier | Un seul « nom du compte » par onglet | Pas de vue consolidée entre CELI, REER et non enregistré | Multi-comptes, vue par compte et consolidée |

**Hypothèse MVP (D15).** Le rendement mensuel de l'outil, (valeur de fin − cotisation) / valeur de début − 1, suppose que la cotisation arrive en fin de mois. Avec les dates réelles des transactions, l'app n'a plus besoin de cette hypothèse, mais les relevés de valeur mensuels la gardent.

## Modèle de données

PostgreSQL 16, un schéma par module et un pour le socle. Module 1 : six tables. Les données brutes sont stockées par année fiscale avec leur source ; les ratios sont calculés par le moteur, jamais stockés, sauf dans l'archive des évaluations.

| Table | Clé | Colonnes principales | Rôle |
| --- | --- | --- | --- |
| `stock` | `id`, `symbol` unique | nom, bourse (TSX, NYSE, NASDAQ), devise de cotation, devise de déclaration, drapeau financière | Liste de suivi |
| `annual_fact` | `stock_id`, `fiscal_year`, `metric` | valeur, devise, source, `fetched_at` | Séries annuelles : BPA dilué, dividende par action, rendement, actions diluées |
| `snapshot` | `stock_id` | prix, dividende prévu, dette LT, capitaux propres, valeur comptable par action, taux de change, `as_of`, source | Valeurs courantes |
| `qualitative` | `stock_id` | règles 1 à 4 (oui / non / vide), note par règle, `reviewed_at`, rachat manuel oui/non | Ton jugement |
| `evaluation` | `id` | `stock_id`, mode, version des règles, empreinte des entrées, entrées (JSONB), résultat (JSONB), `created_at` | Archive immuable |
| `import_job` | `id` | type (xlsx, fournisseur), statut, compte rendu ligne par ligne | Traçabilité des imports |

**Socle (comptes et registre) et module 2 :**

| Table | Schéma | Colonnes principales | Rôle |
| --- | --- | --- | --- |
| `account` | socle | type (CELI, REER, CELIAPP, REEE, non enregistré), courtier, devise, ouvert le | Comptes |
| `instrument` | socle | symbole canonique, nom, type (action, FNB, encaisse), devise, bourse | Titres partagés ; `stock` du module 1 devient une vue sur les actions |
| `transaction` | socle | compte, instrument, type, date de règlement, quantité, prix, montant, frais, devise, source | Registre en insertion seule |
| `price` | socle | instrument, date, clôture, devise, source | Prix quotidiens |
| `valuation_statement` | socle | compte, date, valeur, cotisations du mois | Relevés de valeur pour les comptes sans transactions |
| `etf_profile` | module 2 | instrument, répartition par classe (6 classes), RFG, actif sous gestion, ESG, `as_of`, source | Catalogue FNB, daté |
| `model_portfolio` | module 2 | nom, version, cibles par classe, bandes de tolérance | Portefeuille modèle, chaque modification = nouvelle version |
| `model_holding` | module 2 | version du modèle, instrument, poids cible | Fonds retenus |
| `account_model` | module 2 | compte (ou groupe de comptes), version du modèle | Quel modèle s'applique à quel compte |
| `rebalance_plan` | module 2 | comptes, date, entrées, ordres proposés (JSONB), statut | Plans de rééquilibrage archivés |

**Positions** : vue matérialisée dérivée de `transaction`, rafraîchie à chaque écriture. **Montants** : `numeric(20,6)` en PostgreSQL et un type décimal en Go, jamais de virgule flottante pour de l'argent.

**Format étroit pour les séries.** Une ligne par (action, année, mesure) plutôt qu'une colonne par mesure : ajouter une mesure (flux de trésorerie libre, par exemple) ne demande pas de migration.

**Archive immuable.** `evaluation` est en insertion seule : un rôle PostgreSQL sans `UPDATE` ni `DELETE` sur cette table. Avec les entrées et la version des règles archivées, chaque évaluation se recalcule à l'identique.

**Migrations** versionnées dans le dépôt, appliquées au démarrage (goose ou golang-migrate).

## Ingestion des données

Les trois sources passent par une même interface Go, `Source`, qui produit des faits normalisés : (action, année, mesure, valeur, devise, source). Le moteur ne sait jamais d'où vient une donnée.

| Source | Ce qu'elle fournit | Quand |
| --- | --- | --- |
| Saisie manuelle | Tout, champ par champ, plus les règles 1 à 4 | À la demande, dans Angular ou Flutter |
| Import xlsx | Les quatre onglets de saisie de ta feuille | Une fois, pour migrer ; ensuite au besoin |
| Fournisseur API | Séries annuelles, prix, bilan, historique des dividendes | Prix : chaque nuit ; fondamentaux : chaque semaine et après les résultats |

**Priorité des sources.** Chaque fait garde sa source. Un rafraîchissement du fournisseur n'écrase jamais une valeur saisie à la main ; il crée un conflit visible dans la fiche de l'action, que tu tranches.

**Import xlsx : correspondance avec la feuille**

| Onglet | Emplacement | Donnée |
| --- | --- | --- |
| SI Worksheet | Ligne 7 et suivantes : C, G, H, J, K, O, P, Q | Symbole, ratio de distribution, dette LT / capitaux propres, rachat oui/non, P/E, P/B, dividende prévu, prix |
| EPS Growth Calculation | Un bloc de 4 lignes par action, dès la ligne 6 ; colonnes B (il y a 20 ans) à U (année 1) | BPA dilué annuel |
| Dividend Growth Calculation | Même disposition | Dividende par action annuel |
| Average Dividend Yield | Un bloc de 2 lignes par action, dès la ligne 6 ; B à U | Rendement annuel |

La feuille ne contient aucune année absolue : l'import demande quelle année fiscale correspond à « Year 1 ». Les ratios saisis dans la feuille (P/E, ratio de distribution) sont gardés comme valeurs importées ; le mode compat les utilise, le mode corrigé les recalcule à partir des séries.

**Fournisseur API.** Adapter d'abord FMP ou EODHD, derrière la même interface ; un deuxième fournisseur s'ajoute sans toucher au moteur. Exigences à vérifier avant de payer, sur 5 ou 6 titres TSX connus :

- 20 ans de BPA dilué annuel et de dividendes, ajustés pour les fractionnements ;
- dividendes spéciaux distingués des dividendes ordinaires (sinon faux échecs à la règle 9) ;
- actions en circulation diluées et bilan annuels ;
- prix historiques, pour reconstituer le rendement annuel (dividende de l'année / prix moyen de l'année).

**Devises.** Plusieurs titres du TSX déclarent leurs résultats en USD et cotent en CAD. Chaque fait garde sa devise ; le moteur convertit avec le taux de la Banque du Canada (API Valet, gratuite) à la date du prix avant de calculer P/E, P/B et rendement.

**Symboles.** Format canonique `BOURSE:SYMBOLE` (`TSX:RY`, `NYSE:KO`) ; chaque adapter traduit vers le format de son fournisseur (`RY.TO`).

**Contrôles de plausibilité** à l'ingestion : année manquante, BPA multiplié par plus de 10 d'une année à l'autre, devise inattendue. Ce sont des avertissements dans la fiche, pas des rejets.

## API REST

L'API est définie d'abord dans un contrat OpenAPI 3.1 versionné dans le dépôt ; serveur et clients en sont générés. Chaque module possède son préfixe : l'évaluation d'actions vit sous `/api/v1/stocks`.

| Méthode | Chemin | Rôle |
| --- | --- | --- |
| GET | `/stocks?mode=&horizon=` | Liste de suivi avec score, sous-scores et verdict |
| POST | `/stocks` | Ajouter une action (symbole canonique) |
| GET | `/stocks/{symbol}` | Fiche complète : séries, instantané, qualitatif, sources, conflits |
| PATCH | `/stocks/{symbol}` | Métadonnées et instantané (saisie manuelle) |
| PUT | `/stocks/{symbol}/facts` | Remplacer des valeurs annuelles (saisie manuelle) |
| PUT | `/stocks/{symbol}/qualitative` | Règles 1 à 4, rachat manuel |
| DELETE | `/stocks/{symbol}` | Retirer de la liste (l'archive des évaluations reste) |
| GET | `/stocks/{symbol}/evaluation?mode=` | Évaluation calculée à la volée, non archivée |
| POST | `/stocks/{symbol}/evaluations` | Calculer et archiver |
| GET | `/stocks/{symbol}/evaluations` | Historique archivé |
| POST | `/stocks/{symbol}/refresh` | Tirer les données du fournisseur (asynchrone, renvoie un job) |
| POST | `/imports/xlsx` | Importer la feuille (multipart, avec l'année de « Year 1 ») |
| GET | `/imports/{id}` | Compte rendu d'import ou de rafraîchissement |
| GET | `/rules` | Version et seuils des règles, pour les libellés des clients |

**Génération.** Serveur Go avec oapi-codegen (mode strict), client Angular avec openapi-generator (`typescript-angular`), client Flutter avec openapi-generator (`dart-dio`). La CI échoue si le code généré ne correspond plus au contrat.

**Conventions.** Ratios en fraction (0,70), montants avec leur devise, dates ISO 8601, erreurs au format RFC 9457 (`application/problem+json`). Les traitements longs (rafraîchissement, import) renvoient `202` et un job à interroger.

**Socle et module 2.** Le registre est sous `/api/v1/accounts`, le module 2 sous `/api/v1/portfolio`.

| Méthode | Chemin | Rôle |
| --- | --- | --- |
| GET, POST | `/accounts` | Lister, créer des comptes |
| GET, POST | `/accounts/{id}/transactions` | Lire le registre, ajouter une transaction (ou son écriture inverse) |
| POST | `/accounts/{id}/statements` | Saisir un relevé de valeur mensuel |
| POST | `/accounts/{id}/imports/csv?broker=` | Importer un relevé de courtier |
| GET | `/accounts/{id}/positions` | Positions et valeur au jour donné |
| GET | `/portfolio/etfs?type=&region=&maxMer=` | Catalogue filtrable |
| GET, POST, PUT | `/portfolio/models` | Portefeuilles modèles et leurs versions |
| POST | `/portfolio/models/{id}/purchase-plan` | Plan d'achat initial pour un montant donné |
| POST | `/portfolio/rebalance-plans` | Plan de rééquilibrage pour un ou plusieurs comptes, avec ou sans nouvelle cotisation |
| GET | `/portfolio/exposure?accounts=` | Exposition par transparence vs modèle |
| GET | `/portfolio/performance?accounts=&from=&to=` | Rendement pondéré dans le temps et TRI, par mois et par année |

## Clients

Angular est le poste de travail : saisie de 20 ans de données, import, analyse. Flutter est le compagnon mobile : consulter, trancher les règles 1 à 4, être averti. Les deux n'ont aucune logique de calcul ; le score vient toujours de l'API.

| Écran | Contenu | Angular | Flutter |
| --- | --- | --- | --- |
| Liste de suivi | Tableau triable : score, qualité / prix, verdict, horizon ; filtres par bourse et par mode | Oui | Oui (liste compacte) |
| Résultat d'une action | Les 10 critères par horizon : valeur, seuil, statut ; comparaison corrigé / compat | Oui | Oui |
| Données annuelles | Grille de 20 ans éditable (BPA, dividende, rendement, actions) ; source et conflits par cellule | Oui | Lecture seule |
| Règles 1 à 4 | Oui / non avec note et date de revue | Oui | Oui |
| Historique | Évaluations archivées, écart entre deux dates | Oui | Oui |
| Import | Téléverser la feuille, compte rendu ligne par ligne | Oui | Non |
| Paramètres | Version des règles, fournisseur, drapeau financière | Oui | Non |

**Écrans du module 2 :**

| Écran | Contenu | Angular | Flutter |
| --- | --- | --- | --- |
| Tableau de bord | Valeur totale et par compte, exposition vs modèle, alertes de bande | Oui | Oui |
| Catalogue FNB | Filtres (type, région, RFG), comparaison de 2 ou 3 FNB | Oui | Lecture seule |
| Portefeuille modèle | Cibles, fonds, exposition et RFG résultants, versions | Oui | Lecture seule |
| Plan d'achat ou de rééquilibrage | Montant à investir, ordres en parts entières, reste en encaisse | Oui | Oui |
| Registre | Transactions par compte, import CSV, relevés de valeur | Oui | Saisie rapide d'une transaction |
| Rendement | Par mois et par année, pondéré dans le temps et TRI, par compte et consolidé | Oui | Oui |

**Angular.** Composants autonomes, signaux, une route chargée à la demande par module de l'application (`/stocks`, puis les modules à venir). Bibliothèque de composants : Angular Material, avec une grille de données pour la saisie annuelle (AG Grid communautaire).

**Flutter.** Ta pile habituelle : Riverpod, GoRouter, Freezed, organisation par fonctionnalité. Cache local Drift + SQLCipher pour consulter la liste hors ligne ; les écritures (règles 1 à 4) sont mises en file et envoyées au retour du réseau.

**Navigation commune.** Les deux clients prévoient dès le départ une coquille à plusieurs modules (menu latéral en web, barre de navigation en mobile), même si un seul module existe en V1.

## Sécurité et déploiement

Tout tourne sur ton VPS OVH derrière Traefik, avec Keycloak comme seul point d'authentification. Le seul tiers est le fournisseur de données, qui ne voit que des symboles boursiers, jamais tes positions ni tes montants.

**Identité.** Un realm Keycloak dédié (`finance`) avec deux clients publics en code d'autorisation + PKCE : Angular et Flutter (AppAuth). L'API valide les JWT (signature via JWKS, audience, expiration) et ne gère aucun mot de passe. Session mobile : jeton de rafraîchissement en stockage sécurisé (Keychain / Keystore).

**Conteneurs** (Docker Compose) :

| Service | Image | Exposé |
| --- | --- | --- |
| `api` | Binaire Go statique sur distroless, utilisateur non root, système de fichiers en lecture seule | Via Traefik, `/api` |
| `web` | Build Angular servi par Caddy | Via Traefik, `/` |
| `db` | PostgreSQL 16 | Réseau interne seulement |
| `keycloak` | Instance existante ou dédiée | Via Traefik, `/auth` |

**Secrets.** Clé du fournisseur et mot de passe de la base chiffrés avec sops + age dans le dépôt, montés en secrets Docker. Vault serait possible, mais lourd pour une instance perso.

**Sauvegardes.** `pg_dump` chaque nuit, chiffré avec age, envoyé vers OVH Object Storage ; rétention 30 jours. Un test de restauration mensuel automatisé : une sauvegarde jamais restaurée n'en est pas une.

**Durcissement.** TLS de bout en bout (Let's Encrypt via Traefik), en-têtes de sécurité et CSP stricte pour Angular, limitation de débit sur l'API, journal d'audit des écritures (qui, quoi, quand, avant/après).

**Observabilité.** Journaux structurés (`slog`, JSON), métriques Prometheus (durée des évaluations, appels et quotas du fournisseur, échecs d'ingestion), `/healthz` et `/readyz`.

**CI/CD.** GitHub Actions : tests, vérification du contrat OpenAPI, analyse (govulncheck, Trivy), images signées (cosign) poussées dans GHCR, déploiement sur le VPS par tag.

## Stratégie de tests

La feuille elle-même sert d'oracle : le mode compat doit donner exactement ses résultats, et le mode corrigé ne doit s'en écarter que sur les huit points listés plus haut.

**Vecteurs de référence (couche de pointage).** Les onglets d'exemple donnent 15 lignes (10 en achat, 5 en vente) avec leurs drapeaux attendus ; les seuils stricts du référentiel les reproduisent toutes. Le test injecte les mesures de chaque ligne et compare les 10 drapeaux et le compte d'échecs.

| Onglet | Action | Échecs attendus (strict) | Échecs (assoupli, calculé) |
| --- | --- | --- | --- |
| Sample Data-Buying | AAPL | 5 | 4 |
| Sample Data-Buying | WMT | 0 | 0 |
| Sample Data-Buying | HD | 1 | 0 |
| Sample Data-Buying | PEP | 2 | 2 |
| Sample Data-Buying | KO | 2 | 2 |
| Sample Data-Buying | MCD | 0 | 0 |
| Sample Data-Buying | CVX | 1 | 1 |
| Sample Data-Buying | NKE | 5 | 2 |
| Sample Data-Buying | JNJ | 0 | 0 |
| Sample Data-Buying | PG | 1 | 1 |
| Sample Data- Selling | AXP | 9 | 8 |
| Sample Data- Selling | KO | 1 | 0 |
| Sample Data- Selling | XOM | 5 | 1 |
| Sample Data- Selling | MSFT | 3 | 2 |
| Sample Data- Selling | IBM | 7 | 7 |

**Oracle LibreOffice (couche de calcul).** Un générateur produit des séries de 20 ans (croissance régulière, BPA négatifs, zéros, trous, historiques courts), les écrit dans une copie de ta feuille, la fait recalculer par LibreOffice sans interface, puis relit les moyennes et les comptes de hausses. Le mode compat doit les reproduire à 10⁻⁹ près. Les cas qui produisent `#DIV/0!` dans la feuille doivent produire une erreur en compat.

**Propriétés et fuzzing (mode corrigé).** Fuzzing natif Go sur le calcul de croissance (jamais de panique, jamais de NaN publié) ; propriétés : une série strictement croissante réussit 5b, la moyenne tronquée ne dépend pas de l'ordre, multiplier une série par une constante positive ne change pas sa croissance.

**Autres niveaux.**

- Base de données : tests d'intégration sur un vrai PostgreSQL (testcontainers), dont le refus de `UPDATE` sur l'archive.
- Fournisseur : réponses enregistrées une fois, rejouées avec `httptest` ; aucun appel réseau en CI.
- Contrat : chaque réponse de l'API validée contre le schéma OpenAPI.
- Angular : tests de composants, plus Playwright de bout en bout sur import puis liste puis fiche.
- Flutter : tests de widgets et images de référence de la liste et de la fiche.

**Module 2.** Les mêmes principes : un portefeuille de test saisi dans une copie de ton outil, recalculé par LibreOffice, sert de référence pour l'exposition, le RFG pondéré et les statuts de bande, en excluant les 9 écarts connus. Rendement : cas à la main vérifiés contre `XIRR` d'un tableur. Rééquilibrage, en propriétés : un plan ne dépasse jamais l'encaisse disponible, ne vend rien quand les cotisations suffisent, et rapproche toujours le portefeuille de sa cible.

## Feuille de route et décisions MVP

Toutes les fonctionnalités sont prévues, réparties en 7 phases. Le MVP (phase 1) met en ligne l'évaluation d'actions sur le web ; chaque phase suivante ajoute un bloc utilisable seul.

| # | Fonctionnalité | Module | Phase |
| --- | --- | --- | --- |
| F01 | Socle : Keycloak, API, PostgreSQL, Traefik, déploiement sur le VPS, sauvegardes chiffrées | Socle | 1 — MVP |
| F02 | Liste de suivi d'actions TSX et US | 1 | 1 — MVP |
| F03 | Saisie manuelle des séries annuelles et de l'instantané | 1 | 1 — MVP |
| F04 | Import de la feuille Simply Investing | 1 | 1 — MVP |
| F05 | Règles 1 à 4, barrière qualitative | 1 | 1 — MVP |
| F06 | Moteur 5a à 11c : 4 horizons, profils strict et assoupli | 1 | 1 — MVP |
| F07 | Modes corrigé et compat | 1 | 1 — MVP |
| F08 | Archive immuable des évaluations, règles versionnées | 1 | 1 — MVP |
| F09 | Écrans Angular : liste, fiche, grille de saisie, import | 1 | 1 — MVP |
| F10 | Comptes (CELI, REER, CELIAPP, REEE, non enregistré) | Socle | 2 |
| F11 | Registre de transactions : saisie manuelle et CSV générique | Socle | 2 |
| F12 | Relevés de valeur mensuels | Socle | 2 |
| F13 | Import de l'outil SÉLECTION ET GESTION | 2 | 2 |
| F14 | Catalogue FNB filtrable, comparaison de 2 ou 3 FNB | 2 | 2 |
| F15 | Portefeuille modèle versionné, exposition par transparence, RFG pondéré | 2 | 2 |
| F16 | Plan d'achat initial en parts entières | 2 | 2 |
| F17 | Rééquilibrage par bandes, cotisations utilisées d'abord | 2 | 2 |
| F18 | Rendement pondéré dans le temps et TRI, par compte et consolidé | 2 | 2 |
| F19 | Tableau de bord consolidé | 2 | 2 |
| F20 | Adapter fournisseur (FMP ou EODHD) : fondamentaux sur 20 ans, prix quotidiens | Socle | 3 |
| F21 | Rafraîchissement planifié, conflits de sources | Socle | 3 |
| F22 | Taux de change de la Banque du Canada, devises CAD et USD | Socle | 3 |
| F23 | Catalogue FNB rafraîchi (répartition, RFG) | 2 | 3 |
| F24 | App Flutter : liste, fiche, règles 1 à 4, tableau de bord, plans | Clients | 4 |
| F25 | Saisie rapide d'une transaction sur mobile | Clients | 4 |
| F26 | Cache hors ligne chiffré | Clients | 4 |
| F27 | Prix d'entrée maximal par action | 1 | 5 |
| F28 | Alertes : action devenue candidate ou sortie, coupe de dividende, prix sous le prix d'entrée, portefeuille hors bande | Socle | 5 |
| F29 | Notifications : push par ntfy auto-hébergé, courriel | Socle | 5 |
| F30 | Prix de base rajusté par titre, comptes non enregistrés | Socle | 6 |
| F31 | Gains et pertes en capital par année fiscale, en CAD ; détection des pertes apparentes | Socle | 6 |
| F32 | Distributions des FNB : remboursements de capital, gains réinvestis | 2 | 6 |
| F33 | Gabarits d'import par courtier, dédoublonnage | Socle | 6 |
| F34 | Backtest des règles sur l'historique | 1 | 7 |
| F35 | Marché UK | 1 | 7 |
| F36 | Ménage : plusieurs utilisateurs, comptes partagés | Socle | 7 |
| F37 | Portefeuille modèle réparti sur plusieurs comptes | 2 | 7 |
| F38 | Bandes de rééquilibrage 5/25 en option | 2 | 7 |

**Phases et critères de sortie.** Une phase est terminée quand son critère est vérifié, pas quand son code est écrit.

1. **MVP — Évaluation d'actions sur le web.** Les 15 lignes de référence passent ; ta feuille importée donne les mêmes scores en mode compat ; une restauration de sauvegarde réussit.
2. **Portefeuille de FNB.** Tes positions réelles sont reconstituées à l'unité près ; le rendement d'un compte égale celui du relevé annuel du courtier, à 0,1 point près.
3. **Données automatiques.** Écart nul ou expliqué avec les rapports annuels sur 3 actions et 3 FNB ; deux semaines de rafraîchissement de nuit sans intervention.
4. **Mobile.** Consultation complète en mode avion ; une transaction saisie hors ligne arrive au serveur au retour du réseau.
5. **Alertes et prix cibles.** Prix d'entrée vérifié à la main sur 5 actions ; deux semaines sans alerte en double ni manquée.
6. **Fiscalité et courtiers.** Prix de base rajusté et gain en capital d'une année réelle égaux à ton calcul existant ou à tes feuillets.
7. **Analyse avancée et ménage.** Backtest reproductible à l'identique ; 3 titres UK vérifiés à la main ; un membre du ménage ne voit que ses comptes et ceux qu'on lui partage.

**Conception des phases 5 à 7, en bref**

- **Prix d'entrée maximal (F27).** Le prix le plus élevé auquel l'action passe encore 11a, 11b et 11c. Avec le BPA, la valeur comptable par action (VCPA), le dividende prévu D et le rendement moyen R sur 20 ans, il faut un prix inférieur à la plus petite des trois bornes. En profil assoupli, 35 et 6 remplacent 25 et 3.

```latex
P_{\max} = \min\left(25 \times \text{BPA},\; 3 \times \text{VCPA},\; \frac{D}{\bar{R}}\right)
```

- **Alertes (F28, F29).** Une tâche compare chaque nuit la nouvelle évaluation à la précédente et n'envoie que les changements d'état. Push par ntfy auto-hébergé sur ton VPS (aucun service de Google ou d'Apple ne voit le contenu), courriel en option.
- **Fiscalité (F30 à F32).** Prix de base rajusté par la méthode du coût moyen, calculé depuis le registre ; chaque montant converti en CAD au taux de la Banque du Canada du jour de règlement. Une perte est signalée comme perte apparente si le même titre est racheté dans les 30 jours avant ou après la vente. Les avis annuels des FNB (remboursement de capital, gains réinvestis) se saisissent une fois par année et ajustent le prix de base.
- **Gabarits de courtier (F33).** Une correspondance de colonnes enregistrée par courtier, créée dans l'app sans code ; une empreinte par ligne évite d'importer deux fois la même transaction.
- **Backtest (F34).** Pour chaque fin d'année passée, le moteur réévalue les actions avec seulement les données publiées à cette date (résultats annuels disponibles 90 jours après la fin d'exercice), titres radiés compris. Les candidates forment un panier équipondéré tenu un an, comparé à un FNB indiciel de référence.
- **Marché UK (F35).** Horizon de 5 ans avec moyenne simple, comme l'onglet UK de ta feuille ; cotes de Londres converties de pence en livres, puis en CAD.
- **Ménage (F36).** Rôles Keycloak (propriétaire, lecteur) par compte, vérifiés par l'API, et sécurité au niveau des lignes dans PostgreSQL en deuxième ligne de défense ; le `owner_id` prévu dès le MVP rend ça possible sans migration lourde.

**Journal des décisions MVP.** Chaque ambiguïté est tranchée ici pour le MVP ; la dernière colonne dit ce qu'on réévalue une fois le MVP en main. Une nouvelle ambiguïté trouvée en développement s'ajoute à la suite (D17, D18…).

| # | Sujet | Décision MVP | À réévaluer après le MVP |
| --- | --- | --- | --- |
| D1 | Utilisateurs | Un seul utilisateur ; chaque table porte déjà un `owner_id` | Ménage en phase 7 (F36) |
| D2 | Règle 8, compagnie sans dette | Réussit ; seuls des capitaux propres négatifs échouent | Texte strict (« au-dessus de 0 ») |
| D3 | Banques et assureurs, règle 8 | Drapeau « financière » disponible, désactivé par défaut | Exemption par défaut pour le secteur financier |
| D4 | Rendement annuel historique | Dividende de l'année / prix de clôture moyen de l'année | Cours de fin d'année |
| D5 | BPA pour P/E et ratio de distribution | Dernier exercice | 12 derniers mois |
| D6 | Type de BPA | Dilué | De base |
| D7 | Règle 10, période | 1 an ; 5 ans configurable | 5 ans par défaut |
| D8 | Règle 5b assouplie | 1, 3, 4, 6 hausses sur 5, 10, 15, 20 ans | Seuils d'une source publiée |
| D9 | Horizon de 5 ans | Indicatif, sans verdict | Le retirer ou le garder |
| D10 | Courtiers | Saisie manuelle et un format CSV générique de l'app ; aucun adapter courtier | Gabarits par courtier en phase 6 (F33) |
| D11 | Prix de base rajusté | Phase 6 (F30 à F32) | Cas limites après une première année fiscale réelle |
| D12 | Bandes de rééquilibrage | Celles de ton outil : ±20 % relatifs ; encaisse cible + 5 points | Option 5/25 en phase 7 (F38) |
| D13 | Portefeuille modèle | Un modèle par compte, comme ton outil | Modèle multi-comptes en phase 7 (F37) |
| D14 | Ordre des modules | Module 1, puis module 2 | Inverser si le module 2 sert plus |
| D15 | Rendement des relevés de valeur | Cotisation en fin de mois, comme ton outil | Cotisation en milieu de mois (Dietz modifiée) |
| D16 | Nom de l'application | « Finance privée », nom de travail | Nom définitif |

**Seule question vraiment ouverte :** les autres fonctionnalités de l'application de finance. Chacune pourra ajouter un module et, au besoin, de nouvelles décisions au journal.

**Plan de développement pour agents IA :** Plan de développement (agents IA)
