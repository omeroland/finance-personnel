"""Modèle de référence de l'app corrigée pour l'outil SÉLECTION ET GESTION (P2-07).

Calcule, en Python pur (Decimal pour l'argent), ce que l'app doit produire pour les mêmes entrées
que l'outil. L'oracle compare ce modèle au classeur recalculé par LibreOffice :
égal pour les cas « fidèle à l'outil », différent (et justifié) pour chacun des 9 écarts.

Ce module ne fait aucune entrée-sortie. Ratios et taux : fractions (0.20 = 20 %).
"""
from __future__ import annotations

import math
from decimal import ROUND_DOWN, Decimal, getcontext

getcontext().prec = 40

BANDE_RELATIVE = Decimal("0.2")   # fonds : ±20 % relatifs de la cible
BANDE_POINTS = Decimal("0.05")    # encaisse et classes : cible + 5 points
OK, A_EQUILIBRER = "OK", "À équilibrer"
TOL = 1e-9


def D(x) -> Decimal:
    """Decimal exact à partir d'un nombre JSON (repr le plus court, jamais le binaire)."""
    return x if isinstance(x, Decimal) else Decimal(repr(x) if isinstance(x, float) else str(x))


def num(d: Decimal | None):
    """Decimal -> nombre JSON stable (12 décimales), None reste None."""
    if d is None:
        return None
    r = round(float(round(d, 12)), 12)
    return int(r) if r == int(r) and abs(r) < 1e15 else r


# ---------------------------------------------------------------- ACHATS

def achats(montant, fonds: list[dict]) -> dict:
    """Plan d'achat en parts entières ; le reste de chaque ligne va en encaisse.

    fonds : [{pct, prix}]. Totaux recalculés sur les bonnes lignes (écart « totaux décalés ») :
    prix moyen pondéré par % ; parts totales = somme des parts entières.
    """
    m = D(montant)
    montants, parts, depenses = [], [], []
    for f in fonds:
        mt = m * D(f["pct"])
        n = int((mt / D(f["prix"])).to_integral_value(rounding=ROUND_DOWN))
        montants.append(mt)
        parts.append(n)
        depenses.append(n * D(f["prix"]))
    total_pct = sum((D(f["pct"]) for f in fonds), Decimal(0))
    prix_moyen = (sum((D(f["prix"]) * D(f["pct"]) for f in fonds), Decimal(0)) / total_pct) if total_pct else None
    return {
        "montants": [num(x) for x in montants],
        "parts": parts,
        "total_pct": num(total_pct),
        "total_montant": num(sum(montants, Decimal(0))),
        "prix_moyen": num(prix_moyen),
        "parts_total": sum(parts),
        "depense_totale": num(sum(depenses, Decimal(0))),
        "encaisse_restante": num(m - sum(depenses, Decimal(0))),
    }


def parts_fractionnaires(montant, fonds: list[dict]) -> list:
    """Ce que fait l'outil : Montant / Prix, sans arrondi (pour les tests du modèle)."""
    m = D(montant)
    return [num(m * D(f["pct"]) / D(f["prix"])) for f in fonds]


# ---------------------------------------------------------------- RÉÉQUILIBRAGE

def classes(profil: dict) -> tuple[Decimal, Decimal, Decimal]:
    """(actions, obligations, autres actifs) d'un fonds ; autres = ce que les 5 classes ne couvrent pas."""
    a = D(profil["ca"]) + D(profil["us"]) + D(profil["intl"])
    o = D(profil["obl_cad"]) + D(profil["obl_autres"])
    return a, o, Decimal(1) - a - o


def statut_fonds(pct: Decimal, cible: Decimal) -> str:
    if cible == 0:
        return A_EQUILIBRER if pct > 0 else OK
    return A_EQUILIBRER if (pct > cible * (1 + BANDE_RELATIVE) or pct < cible * (1 - BANDE_RELATIVE)) else OK


def statut_points(pct: Decimal, cible: Decimal) -> str:
    return A_EQUILIBRER if (pct > cible + BANDE_POINTS or pct < cible - BANDE_POINTS) else OK


def marge_bande(pct: Decimal, cible: Decimal, relative: bool) -> Decimal:
    """Distance à la limite de bande la plus proche (sert à écarter les cas collés à une limite)."""
    if relative:
        lim = [cible * (1 + BANDE_RELATIVE), cible * (1 - BANDE_RELATIVE)]
    else:
        lim = [cible + BANDE_POINTS, cible - BANDE_POINTS]
    return min(abs(pct - x) for x in lim)


def reequilibrage(entrees: dict, catalogue: dict[str, dict]) -> dict:
    """Rééquilibrage de l'app : valeur de marché réelle, cibles normalisées, parts entières.

    entrees : {cible_encaisse, encaisse, fonds:[{symbole, pct_cible, valeur, prix}]}
    Les cibles de fonds (somme 100 % de la partie investie) sont multipliées par (1 - cible d'encaisse).
    """
    c0 = D(entrees.get("cible_encaisse", 0))
    cash = D(entrees["encaisse"])
    fonds = entrees["fonds"]
    total = cash + sum((D(f["valeur"]) for f in fonds), Decimal(0))
    if total == 0:
        return {"total": 0, "encaisse": None, "fonds": [], "parts": [], "actions": None, "obligations": None}
    sortie_fonds, sortie_parts = [], []
    cible_cl = [Decimal(0)] * 3
    val_cl = [Decimal(0)] * 3
    for f in fonds:
        cible = D(f["pct_cible"]) * (1 - c0)
        v = D(f["valeur"])
        pct = v / total
        ecart = v - cible * total
        parts = None
        if f.get("prix"):
            parts = int(((-ecart) / D(f["prix"])).to_integral_value(rounding=ROUND_DOWN))
        sortie_fonds.append({
            "cible": num(cible), "valeur": num(v), "pct": num(pct), "statut": statut_fonds(pct, cible),
            "ecart": num(ecart),
        })
        sortie_parts.append(parts)
        cl = classes(catalogue[f["symbole"]])
        for i in range(3):
            cible_cl[i] += cible * cl[i]
            val_cl[i] += v * cl[i]
    pct_cash = cash / total

    def classe(i):
        pct = val_cl[i] / total
        return {"cible": num(cible_cl[i]), "pct": num(pct), "statut": statut_points(pct, cible_cl[i]),
                "ecart": num(val_cl[i] - cible_cl[i] * total)}
    return {
        "total": num(total),
        "cible_totale": num(c0 + sum((D(f["pct_cible"]) * (1 - c0) for f in fonds), Decimal(0))),
        "encaisse": {"cible": num(c0), "pct": num(pct_cash),
                     "statut": A_EQUILIBRER if pct_cash > c0 + BANDE_POINTS else OK,
                     "ecart": num(cash - c0 * total)},
        "fonds": sortie_fonds,
        "parts": sortie_parts,
        "actions": classe(0),
        "obligations": classe(1),
        "autres_actifs": classe(2),
    }


# ---------------------------------------------------------------- CRÉATION (obligations « autres »)

def exposition(positions: list[dict], catalogue: dict[str, dict]) -> dict:
    """Exposition par transparence : somme(w * a) / somme(w) pour les 5 classes (+ autres actifs)."""
    tw = sum((D(p["pct"]) for p in positions), Decimal(0))
    cles = ["ca", "us", "intl", "obl_cad", "obl_autres"]
    if tw == 0:
        return {k: None for k in cles + ["autres_actifs"]}
    out = {k: sum((D(p["pct"]) * D(catalogue[p["symbole"]][k]) for p in positions), Decimal(0)) / tw for k in cles}
    out["autres_actifs"] = Decimal(1) - sum(out.values(), Decimal(0))
    return {k: num(v) for k, v in out.items()}


def ecart_obligations(expo: dict, cibles: dict) -> dict:
    """Écart réel - cible, une cible par classe d'obligations (cad, autres)."""
    return {"cad": num(D(expo["obl_cad"]) - D(cibles["cad"])),
            "autres": num(D(expo["obl_autres"]) - D(cibles["autres"]))}


# ---------------------------------------------------------------- DIVERSIFICATION

def diversification(positions: list[dict], catalogue: dict[str, dict]) -> dict:
    """Vide ou classe absente = None (aucun résultat), jamais d'erreur."""
    expo = exposition(positions, catalogue)
    if expo["ca"] is None:
        return {"actions_pct": [None] * 3, "obligations_pct": [None] * 2,
                "portion_actions": None, "portion_obligations": None}
    a = D(expo["ca"]) + D(expo["us"]) + D(expo["intl"])
    o = D(expo["obl_cad"]) + D(expo["obl_autres"])
    return {
        "actions_pct": [num(D(expo[k]) / a) if a else None for k in ("ca", "us", "intl")],
        "obligations_pct": [num(D(expo[k]) / o) if o else None for k in ("obl_cad", "obl_autres")],
        "portion_actions": num(a),
        "portion_obligations": num(o),
    }


# ---------------------------------------------------------------- CATALOGUE

def parse_pct(s):
    """'19,6%' -> 0.196 ; '-' ou vide -> None."""
    if s is None or str(s).strip() in ("", "-"):
        return None
    return num(D(str(s).strip().rstrip("%").replace(",", ".")) / 100)


def parse_taille(s):
    """'880M$' -> 880000000 (dollars) ; '-' -> None. Suffixes M (million) et G (milliard)."""
    if s is None or str(s).strip() in ("", "-"):
        return None
    t = str(s).strip().rstrip("$").replace(",", ".")
    mult = {"M": Decimal(10) ** 6, "G": Decimal(10) ** 9}[t[-1]]
    return num(D(t[:-1]) * mult)


# ---------------------------------------------------------------- RENDEMENT

def rendement_mensuel(depart, ajouts: list, fins: list) -> list:
    """(fin - ajout) / début - 1, la cotisation arrivant en fin de mois (D15)."""
    out, debut = [], D(depart)
    for e, g in zip(ajouts, fins):
        out.append(num((D(g) - D(e)) / debut - 1))
        debut = D(g)
    return out


def irr(flux: list[float]) -> float | None:
    """Taux interne par bissection sur [-0.99, 10] ; None sans changement de signe."""
    def npv(r):
        return sum(f / (1 + r) ** i for i, f in enumerate(flux))
    lo, hi = -0.99, 10.0
    flo, fhi = npv(lo), npv(hi)
    if flo == 0:
        return lo
    if fhi == 0:
        return hi
    if flo * fhi > 0:
        return None
    for _ in range(300):
        mid = (lo + hi) / 2
        fm = npv(mid)
        if fm == 0:
            return mid
        if flo * fm < 0:
            hi, fhi = mid, fm
        else:
            lo, flo = mid, fm
    return (lo + hi) / 2


def aacj_mois(depart, ajouts: list, fins: list) -> list:
    """Colonne « AACJ Mois k » : (1 + TRI mensuel des flux jusqu'au mois k)^k - 1."""
    out = []
    for k in range(1, len(fins) + 1):
        flux = [-float(D(depart))] + [-float(D(e)) for e in ajouts[:k - 1]]
        flux.append(-float(D(ajouts[k - 1])) + float(D(fins[k - 1])))
        r = irr(flux)
        out.append(None if r is None else round((1 + r) ** k - 1, 12))
    return out


def chaine(rendements: list) -> Decimal:
    """Rendement de la période : produit des (1 + r) - 1."""
    p = Decimal(1)
    for r in rendements:
        p *= 1 + D(r)
    return p - 1


def annualise(annuels: list):
    """Rendement annualisé de l'outil (K23) : moyenne géométrique des rendements annuels."""
    n = len(annuels)
    if n == 0:
        return None
    p = chaine(annuels) + 1
    return round(float(p) ** (1.0 / n) - 1, 12)


def rendement(entrees: dict) -> dict:
    """Une série mensuelle : rendement mensuel, TRI cumulé par mois, rendement de la période."""
    rm = rendement_mensuel(entrees["depart"], entrees["ajouts"], entrees["fins"])
    ac = aacj_mois(entrees["depart"], entrees["ajouts"], entrees["fins"])
    out = {"rendement_mensuel": rm, "aacj_mois": ac, "rendement_periode": num(chaine(rm))}
    if len(rm) == 12:
        out["tri_annee"] = ac[-1]
    return out


def consolide(comptes: list[dict]) -> dict:
    """Vue consolidée de plusieurs comptes de même horizon : somme des départs, flux et valeurs."""
    n = len(comptes[0]["fins"])
    dep = sum((D(c["depart"]) for c in comptes), Decimal(0))
    aj = [sum((D(c["ajouts"][i]) for c in comptes), Decimal(0)) for i in range(n)]
    fi = [sum((D(c["fins"][i]) for c in comptes), Decimal(0)) for i in range(n)]
    return rendement({"depart": dep, "ajouts": aj, "fins": fi})


def proche(a, b, tol=TOL) -> bool:
    """Égalité à tolérance, récursive : nombres, chaînes (erreurs), listes, dictionnaires, None."""
    if isinstance(a, bool) or isinstance(b, bool):
        return a is b
    if isinstance(a, (int, float)) and isinstance(b, (int, float)):
        return math.isclose(a, b, rel_tol=tol, abs_tol=tol)
    if isinstance(a, list) and isinstance(b, list):
        return len(a) == len(b) and all(proche(x, y, tol) for x, y in zip(a, b))
    if isinstance(a, dict) and isinstance(b, dict):
        return a.keys() == b.keys() and all(proche(a[k], b[k], tol) for k in a)
    return a == b
