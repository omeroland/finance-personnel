"""Cas de l'oracle de l'outil SÉLECTION ET GESTION (P2-07).

Données SYNTHÉTIQUES seulement : un catalogue fictif de 10 fonds (symboles SYN-*), des montants
inventés ou tirés d'une graine fixe. Aucun nom de compte, aucun montant réel.

Chaque cas : id, kind, onglet, etiquette ("fidele" | "ecart"), ecart (code ou None), description,
entrees, egal (groupes de sortie identiques outil / app), different (groupes qui divergent),
verifs (paires outil/app qui doivent coïncider : l'outil corrigé à la main redonne l'app).
"""
from __future__ import annotations

import random
from decimal import ROUND_HALF_EVEN, Decimal

SEED = 20261108

# Les 9 écarts de docs/architecture.md § « Module 2 — Écarts entre l'outil et l'app ».
ECARTS = [
    ("totaux_achats_decales", "ACHATS", "Totaux d'ACHATS décalés (F30 et H30 : plages de tailles différentes)"),
    ("parts_fractionnaires", "ACHATS", "Parts fractionnaires (Montant / Prix sans arrondi)"),
    ("fonds_partiellement_classes", "RÉÉQUILIBRAGE", "Fonds partiellement classés (total = actions + obligations seulement)"),
    ("cibles_au_dela_de_100", "RÉÉQUILIBRAGE", "Cibles au-delà de 100 % (cible d'encaisse en plus de fonds à 100 %)"),
    ("obligations_autres_sans_cible", "CRÉATION", "Obligations « autres » sans cible (comparaison sur CAD seulement)"),
    ("erreurs_si_vide", "DIVERSIFICATION", "Erreurs quand c'est vide (#VALUE!, #DIV/0!)"),
    ("catalogue_fige", "FNB", "Catalogue figé en 2021, valeurs en texte (« 19,6 % », « 880M$ »)"),
    ("rendements_annuels_a_part", "RENDEMENT", "Rendements annuels saisis à part, sans lien avec le calcul mensuel"),
    ("un_compte_par_fichier", "RENDEMENT", "Un compte par fichier, pas de vue consolidée"),
]

_BASE = {"ca": 0, "us": 0, "intl": 0, "obl_cad": 0, "obl_autres": 0}


def _fonds(symbole, nom, rfg, **classes):
    return {"symbole": symbole, "nom": nom, "rfg": rfg, **_BASE, **classes}


# Catalogue fictif. SYN-OR et SYN-IMM sont partiellement ou pas du tout classés dans les 5 classes.
CATALOGUE = [
    _fonds("SYN-CA", "Fonds synthétique actions canadiennes", 0.0006, ca=1.0),
    _fonds("SYN-US", "Fonds synthétique actions américaines", 0.0009, us=1.0),
    _fonds("SYN-INT", "Fonds synthétique actions internationales", 0.0020, intl=1.0),
    _fonds("SYN-OBL", "Fonds synthétique obligations canadiennes", 0.0010, obl_cad=1.0),
    _fonds("SYN-OBX", "Fonds synthétique obligations étrangères", 0.0015, obl_autres=1.0),
    _fonds("SYN-BAL", "Fonds synthétique équilibré 70/30", 0.0020,
           ca=0.15, us=0.25, intl=0.20, obl_cad=0.30, obl_autres=0.10),
    _fonds("SYN-CROIS", "Fonds synthétique croissance 90/10", 0.0018,
           ca=0.20, us=0.40, intl=0.30, obl_cad=0.10),
    _fonds("SYN-GLB", "Fonds synthétique obligations mondiales", 0.0025, obl_cad=0.30, obl_autres=0.70),
    _fonds("SYN-OR", "Fonds synthétique métal précieux", 0.0040),
    _fonds("SYN-IMM", "Fonds synthétique immobilier", 0.0061, ca=0.30),
]
PROFILS = {f["symbole"]: f for f in CATALOGUE}


def _q(x: Decimal) -> float:
    return float(x.quantize(Decimal("0.01"), rounding=ROUND_HALF_EVEN))


def _serie(rng: random.Random, depart: float, n: int, derive: float = 0.004) -> dict:
    """n mois synthétiques : cotisation en fin de mois, valeur de fin = début x (1 + r) + cotisation."""
    ajouts, fins, debut = [], [], Decimal(str(depart))
    for _ in range(n):
        a = Decimal(str(rng.choice([0, 0, 250, 500, 500, 1000, -300])))
        r = Decimal(str(round(rng.gauss(derive, 0.025), 5)))
        fin = (debut * (1 + r) + a).quantize(Decimal("0.01"), rounding=ROUND_HALF_EVEN)
        ajouts.append(float(a))
        fins.append(float(fin))
        debut = fin
    return {"depart": depart, "ajouts": ajouts, "fins": fins}


def _case(cid, kind, onglet, etiquette, description, entrees, egal, different=(), ecart=None, verifs=()):
    return {"id": cid, "kind": kind, "onglet": onglet, "etiquette": etiquette, "ecart": ecart,
            "description": description, "entrees": entrees, "egal": list(egal), "different": list(different),
            "verifs": [list(v) for v in verifs]}


def _lignes(*triples):
    return [{"symbole": s, "pct": p, "prix": px} for s, p, px in triples]


def _positions(*paires):
    return [{"symbole": s, "pct": p} for s, p in paires]


def _rq(cid, etiquette, description, entrees, egal, different=(), ecart=None):
    return _case(cid, "reequilibrage", "RÉÉQUILIBRAGE", etiquette, description, entrees, egal, different, ecart)


def _fr(symbole, cible, valeur, prix=None):
    return {"symbole": symbole, "pct_cible": cible, "valeur": valeur, "prix": prix}


def generate() -> list[dict]:
    rng = random.Random(SEED)
    cases: list[dict] = []
    add = cases.append
    ACH = "ACHATS"
    base = ["montants", "total_pct", "total_montant"]

    # ------------------------------------------------------------ ACHATS
    add(_case("achats-fidele-01", "achats", ACH, "fidele",
              "Deux fonds, divisions exactes : montant x part / prix donne des parts entières.",
              {"montant": 10000, "fonds": _lignes(("SYN-CA", 0.5, 25.0), ("SYN-OBL", 0.5, 20.0))},
              base + ["parts"]))
    add(_case("achats-fidele-02", "achats", ACH, "fidele",
              "Trois fonds, divisions exactes.",
              {"montant": 12000, "fonds": _lignes(("SYN-CROIS", 0.4, 24.0), ("SYN-BAL", 0.35, 21.0), ("SYN-OBL", 0.25, 30.0))},
              base + ["parts"]))
    add(_case("achats-fidele-03", "achats", ACH, "fidele",
              "Un seul fonds à 100 %.",
              {"montant": 2500, "fonds": _lignes(("SYN-BAL", 1.0, 50.0))}, base + ["parts"]))
    add(_case("achats-fidele-04", "achats", ACH, "fidele",
              "Quatre fonds, prix non entiers : seuls les montants par ligne sont comparés (les parts relèvent de l'écart parts_fractionnaires).",
              {"montant": 18000, "fonds": _lignes(("SYN-CA", 0.275, 31.47), ("SYN-US", 0.435, 58.12),
                                                   ("SYN-INT", 0.29, 27.9), ("SYN-OBL", 0.0, 22.15))},
              base))
    add(_case("achats-ecart-parts-01", "achats", ACH, "ecart",
              "Parts fractionnaires : l'outil donne 179.13... parts, l'app 179 et le reste en encaisse.",
              {"montant": 10000, "fonds": _lignes(("SYN-CROIS", 0.6, 33.47), ("SYN-OBL", 0.4, 27.19))},
              ["montants", "total_montant"], ["parts"], "parts_fractionnaires"))
    add(_case("achats-ecart-parts-02", "achats", ACH, "ecart",
              "Parts fractionnaires, quatre fonds.",
              {"montant": 25000, "fonds": _lignes(("SYN-CA", 0.25, 40.33), ("SYN-US", 0.35, 61.87),
                                                   ("SYN-INT", 0.2, 29.41), ("SYN-OBL", 0.2, 24.76))},
              ["montants", "total_montant"], ["parts"], "parts_fractionnaires"))
    add(_case("achats-ecart-parts-03", "achats", ACH, "ecart",
              "Parts fractionnaires : un fonds trop cher pour sa part (0 part entière), tout son montant reste en encaisse.",
              {"montant": 500, "fonds": _lignes(("SYN-US", 0.5, 150.0), ("SYN-INT", 0.5, 300.0))},
              ["montants", "total_montant"], ["parts"], "parts_fractionnaires"))
    add(_case("achats-ecart-totaux-01", "achats", ACH, "ecart",
              "Totaux décalés : F30 et H30 multiplient F3:F29 par E32:E51 (27 et 20 lignes) ; avec E32 vide, comme dans le gabarit, ils restent vides ; l'app recalcule sur F10:F29 / E10:E29.",
              {"montant": 10000, "fonds": _lignes(("SYN-CA", 0.5, 25.0), ("SYN-OBL", 0.5, 20.0))},
              base, ["prix_moyen", "parts_total"], "totaux_achats_decales",
              verifs=[("outil.formule_corrigee.prix_moyen", "attendu_app.prix_moyen")]))
    add(_case("achats-ecart-totaux-02", "achats", ACH, "ecart",
              "Totaux décalés, trois fonds, prix non entiers, E32 renseignée : sans le garde-fou IF($E$32=\"\") de l'outil, les plages de tailles différentes donnent #VALUE!.",
              {"montant": 30000, "fonds": _lignes(("SYN-CROIS", 0.5, 44.5), ("SYN-BAL", 0.3, 36.2), ("SYN-OBX", 0.2, 19.8)),
               "e32_remplie": True},
              base, ["prix_moyen", "parts_total"], "totaux_achats_decales",
              verifs=[("outil.formule_corrigee.prix_moyen", "attendu_app.prix_moyen")]))

    # ------------------------------------------------------------ RÉÉQUILIBRAGE
    cl = ["total", "encaisse", "fonds", "actions", "obligations"]
    add(_rq("reequilibrage-fidele-01", "fidele",
            "Deux fonds exactement sur leur cible, sans encaisse : tout est OK.",
            {"cible_encaisse": 0, "encaisse": 0,
             "fonds": [_fr("SYN-CROIS", 0.6, 6000), _fr("SYN-OBL", 0.4, 4000)]}, cl))
    add(_rq("reequilibrage-fidele-02", "fidele",
            "Bandes relatives de +-20 % : US au-dessus (50 % pour 40 %), CA en dessous (10 % pour 20 %), obligations OK.",
            {"cible_encaisse": 0, "encaisse": 0,
             "fonds": [_fr("SYN-US", 0.4, 5000), _fr("SYN-CA", 0.2, 1000), _fr("SYN-OBL", 0.4, 4000)]}, cl))
    add(_rq("reequilibrage-fidele-03", "fidele",
            "Encaisse à 15 % pour une cible de 0 % : au-delà de cible + 5 points, donc À équilibrer.",
            {"cible_encaisse": 0, "encaisse": 1500,
             "fonds": [_fr("SYN-BAL", 0.5, 4250), _fr("SYN-OBL", 0.5, 4250)]}, cl))
    add(_rq("reequilibrage-fidele-04", "fidele",
            "Encaisse à 3 % pour une cible de 0 % : dans la bande de 5 points, OK.",
            {"cible_encaisse": 0, "encaisse": 300,
             "fonds": [_fr("SYN-BAL", 0.5, 4850), _fr("SYN-GLB", 0.5, 4850)]}, cl))
    add(_rq("reequilibrage-fidele-05", "fidele",
            "Fonds tous dans leur bande relative mais classes d'actifs à plus de 5 points de la cible : la classe est À équilibrer.",
            {"cible_encaisse": 0, "encaisse": 0,
             "fonds": [_fr("SYN-US", 0.5, 5600), _fr("SYN-OBL", 0.5, 4400)]}, cl))
    add(_rq("reequilibrage-ecart-parts-01", "ecart",
            "Parts fractionnaires : l'outil donne un nombre de parts à bouger avec décimales, l'app arrondit vers zéro.",
            {"cible_encaisse": 0, "encaisse": 0,
             "fonds": [_fr("SYN-US", 0.4, 5000, 61.37), _fr("SYN-CA", 0.2, 1000, 33.11),
                       _fr("SYN-OBL", 0.4, 4000, 24.83)]},
            ["total", "fonds", "actions", "obligations"], ["parts"], "parts_fractionnaires"))
    add(_rq("reequilibrage-ecart-parts-02", "ecart",
            "Parts fractionnaires, quatre fonds et encaisse en bande.",
            {"cible_encaisse": 0, "encaisse": 400,
             "fonds": [_fr("SYN-INT", 0.25, 2200, 28.9), _fr("SYN-US", 0.35, 3400, 57.45),
                       _fr("SYN-OBL", 0.3, 2900, 25.62), _fr("SYN-CROIS", 0.1, 1100, 41.0)]},
            ["total", "fonds", "actions", "obligations"], ["parts"], "parts_fractionnaires"))
    add(_rq("reequilibrage-ecart-classes-01", "ecart",
            "Fonds partiellement classé : un fonds de métal précieux (0 % dans les 5 classes) disparaît du total de l'outil (7000 $ au lieu de 10000 $) et tous les % actuels sont faux.",
            {"cible_encaisse": 0, "encaisse": 0,
             "fonds": [_fr("SYN-US", 0.4, 4000), _fr("SYN-OBL", 0.3, 3000), _fr("SYN-OR", 0.3, 3000)]},
            ["encaisse"], ["total", "fonds", "actions"], "fonds_partiellement_classes"))
    add(_rq("reequilibrage-ecart-classes-02", "ecart",
            "Fonds partiellement classé : un fonds immobilier classé à 30 % seulement ; l'app garde la valeur de marché et la classe « autres actifs ».",
            {"cible_encaisse": 0, "encaisse": 0,
             "fonds": [_fr("SYN-IMM", 0.25, 2500), _fr("SYN-BAL", 0.5, 5000), _fr("SYN-OBL", 0.25, 2500)]},
            ["encaisse"], ["total", "fonds", "actions"], "fonds_partiellement_classes"))
    add(_rq("reequilibrage-ecart-cibles-01", "ecart",
            "Cibles au-delà de 100 % : cible d'encaisse 25 % + fonds à 100 % = 125 % pour l'outil ; les fonds paraissent tous sous-pondérés.",
            {"cible_encaisse": 0.25, "encaisse": 2500,
             "fonds": [_fr("SYN-CROIS", 0.5, 3750), _fr("SYN-OBL", 0.5, 3750)]},
            ["total", "encaisse"], ["cible_totale", "fonds", "actions"], "cibles_au_dela_de_100"))
    add(_rq("reequilibrage-ecart-cibles-02", "ecart",
            "Cibles au-delà de 100 % : trois fonds, encaisse cible 25 %, portefeuille exactement sur la cible réelle.",
            {"cible_encaisse": 0.25, "encaisse": 2500,
             "fonds": [_fr("SYN-CA", 0.3, 2250), _fr("SYN-US", 0.3, 2250), _fr("SYN-OBL", 0.4, 3000)]},
            ["total", "encaisse"], ["cible_totale", "fonds", "actions"], "cibles_au_dela_de_100"))

    # ------------------------------------------------------------ CRÉATION (obligations « autres »)
    CRE = "CRÉATION"
    add(_case("creation-ecart-obligations-01", "creation", CRE, "ecart",
              "Obligations « autres » : 60 % d'obligations dont 70 % de fonds mondiaux ; l'outil compare CAD (18 %) à la cible de toute la portion (60 %).",
              {"part_actions": 0.4, "fonds": _positions(("SYN-US", 0.4), ("SYN-GLB", 0.6)),
               "cibles_obligations": {"cad": 0.3, "autres": 0.3}},
              ["exposition"], ["cible_obligations", "ecart_obligations"], "obligations_autres_sans_cible"))
    add(_case("creation-ecart-obligations-02", "creation", CRE, "ecart",
              "Obligations « autres » : cibles par classe 20 % CAD et 30 % autres.",
              {"part_actions": 0.5, "fonds": _positions(("SYN-CA", 0.25), ("SYN-US", 0.25), ("SYN-GLB", 0.5)),
               "cibles_obligations": {"cad": 0.2, "autres": 0.3}},
              ["exposition"], ["cible_obligations", "ecart_obligations"], "obligations_autres_sans_cible"))

    # ------------------------------------------------------------ DIVERSIFICATION
    DIV = "DIVERSIFICATION"
    dg = ["actions_pct", "obligations_pct", "portion_actions", "portion_obligations"]
    add(_case("diversification-fidele-01", "diversification", DIV, "fidele",
              "Portefeuille de trois fonds : répartition actions et obligations de l'outil.",
              {"positions": _positions(("SYN-BAL", 0.5), ("SYN-US", 0.3), ("SYN-OBL", 0.2))}, dg))
    add(_case("diversification-fidele-02", "diversification", DIV, "fidele",
              "Portefeuille de deux fonds avec obligations étrangères.",
              {"positions": _positions(("SYN-INT", 0.6), ("SYN-GLB", 0.4))}, dg))
    add(_case("diversification-ecart-vide-01", "diversification", DIV, "ecart",
              "Erreurs si vide : aucune position, l'outil renvoie #VALUE! ; l'app ne renvoie aucun résultat.",
              {"positions": []}, [], ["actions_pct", "obligations_pct"], "erreurs_si_vide"))
    add(_case("diversification-ecart-vide-02", "diversification", DIV, "ecart",
              "Erreurs si vide : portefeuille sans actions, la portion actions divise par zéro (#DIV/0!) ; l'app renvoie null.",
              {"positions": _positions(("SYN-OBL", 1.0))}, ["obligations_pct", "portion_actions", "portion_obligations"],
              ["actions_pct"], "erreurs_si_vide"))

    # ------------------------------------------------------------ Catalogue (FNB)
    add(_case("catalogue-ecart-fige-01", "catalogue", "FNB", "ecart",
              "Catalogue figé : « 19,6% » et « 880M$ » sont du texte ; ISNUMBER est faux et SUM vaut 0. L'app stocke 0.196 et 880000000, datés.",
              {"symbole": "SYN-CA", "rendement_1an": "19,6%", "rendement_3ans": "-", "grosseur": "880M$"},
              [], ["rendement_1an", "grosseur", "rendement_3ans", "rendement_1an_numerique", "grosseur_numerique"],
              "catalogue_fige"))
    add(_case("catalogue-ecart-fige-02", "catalogue", "FNB", "ecart",
              "Catalogue figé : rendement négatif « -2,4% » et petite taille « 12M$ », encore du texte.",
              {"symbole": "SYN-OBL", "rendement_1an": "-2,4%", "rendement_3ans": "5,5%", "grosseur": "12M$"},
              [], ["rendement_1an", "grosseur", "rendement_3ans", "rendement_1an_numerique", "grosseur_numerique"],
              "catalogue_fige"))

    # ------------------------------------------------------------ RENDEMENT
    REN = "RENDEMENT"
    rm = ["rendement_mensuel", "aacj_mois"]
    add(_case("rendement-fidele-01", "rendement", REN, "fidele",
              "Un mois, sans flux : (fin - ajout) / début - 1 et AACJ du mois 1.",
              {"depart": 10000, "ajouts": [0], "fins": [10120]}, rm))
    add(_case("rendement-fidele-02", "rendement", REN, "fidele",
              "Trois mois avec cotisation mensuelle de 500 $ (cotisation en fin de mois, D15).",
              {"depart": 20000, "ajouts": [500, 500, 500], "fins": [20700, 21450, 21800]}, rm))
    add(_case("rendement-fidele-03", "rendement", REN, "fidele",
              "Six mois avec un retrait de 1000 $ au mois 4 et un mois négatif.",
              {"depart": 15000, "ajouts": [250, 0, 250, -1000, 250, 0],
               "fins": [15400.0, 15350.0, 15900.0, 14700.0, 15200.0, 15600.0]}, rm))
    add(_case("rendement-fidele-04", "rendement", REN, "fidele",
              "Année complète (12 mois) tirée de la graine : rendement mensuel, AACJ et TRI de l'année (U22).",
              _serie(rng, 24000.0, 12), rm + ["tri_annee"]))
    add(_case("rendement-fidele-05", "rendement", REN, "fidele",
              "Année complète à dérive négative, avec cotisations et retraits.",
              _serie(rng, 18500.0, 12, derive=-0.006), rm + ["tri_annee"]))
    add(_case("rendement-fidele-06", "rendement", REN, "fidele",
              "Neuf mois tirés de la graine.",
              _serie(rng, 52000.0, 9, derive=0.007), rm))
    add(_case("rendement-fidele-07", "rendement", REN, "fidele",
              "Rendement annualisé (K23) de trois rendements annuels saisis : moyenne géométrique.",
              {"annuels": [0.08, -0.05, 0.12]}, ["annees", "rendement_annualise"]))
    add(_case("rendement-fidele-08", "rendement", REN, "fidele",
              "Rendement annualisé d'une seule année saisie.",
              {"annuels": [0.0625]}, ["annees", "rendement_annualise"]))
    add(_case("rendement-fidele-09", "rendement", REN, "fidele",
              "Rendement annualisé de douze années saisies (graine).",
              {"annuels": [round(rng.gauss(0.065, 0.12), 4) for _ in range(12)]}, ["annees", "rendement_annualise"]))
    s1, s2 = _serie(rng, 30000.0, 12), _serie(rng, 8000.0, 6)
    s1["annuel_saisi"], s2["annuel_saisi"] = 0.05, 0.14
    add(_case("rendement-ecart-annuel-01", "rendement_annuel", REN, "ecart",
              "Rendements annuels à part : 5 % saisi à la main pour l'année 1 alors que les 12 mois calculés donnent autre chose.",
              s1, ["rendement_mensuel"], ["rendement_annuel"], "rendements_annuels_a_part",
              verifs=[("outil.rendement_annuel_depuis_mois", "attendu_app.rendement_annuel")]))
    add(_case("rendement-ecart-annuel-02", "rendement_annuel", REN, "ecart",
              "Rendements annuels à part : 14 % saisi, six mois de données seulement.",
              s2, ["rendement_mensuel"], ["rendement_annuel"], "rendements_annuels_a_part",
              verifs=[("outil.rendement_annuel_depuis_mois", "attendu_app.rendement_annuel")]))
    add(_case("rendement-ecart-comptes-01", "rendement_multi", REN, "ecart",
              "Un compte par fichier : un CELI et un REER (six mois) ; l'outil ne sait rien d'un total, l'app consolide.",
              {"comptes": [dict(type="CELI", **_serie(rng, 12000.0, 6)), dict(type="REER", **_serie(rng, 30000.0, 6))]},
              ["rendement_mensuel_par_compte"], ["consolide"], "un_compte_par_fichier"))
    add(_case("rendement-ecart-comptes-02", "rendement_multi", REN, "ecart",
              "Un compte par fichier : CELI, REER et non enregistré (quatre mois).",
              {"comptes": [dict(type="CELI", **_serie(rng, 9000.0, 4)), dict(type="REER", **_serie(rng, 41000.0, 4)),
                           dict(type="NON_ENREGISTRE", **_serie(rng, 15000.0, 4))]},
              ["rendement_mensuel_par_compte"], ["consolide"], "un_compte_par_fichier"))
    return cases
