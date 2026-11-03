"""Générateur déterministe des 200 cas de l'oracle (P1-07).

Une série = 20 valeurs (colonnes B à U : « il y a 20 ans » à « année 1 ») ;
None = cellule vide. Aucune donnée réelle : tout vient de la graine fixe.
"""
from __future__ import annotations

import random

SEED = 20260101
HORIZON = 20

# Cas documenté (fiche P1-07) : BPA de 1.00 à 1.4641 sur 5 ans, soit +10 % par an,
# dans les 5 dernières colonnes (Q à U) ; les 15 premières cellules sont vides.
# La feuille divise la somme tronquée par 18 au lieu de n - 2 :
# (0.4 - 0.1 - 0.1) / 18 = 0.0111... au lieu de 0.1 (écart « Moyenne divisée par 18 »).
DOCUMENTED_ID = "bpa-1.00-a-1.4641-5ans"
DOCUMENTED_SERIES = [None] * 15 + [1.0, 1.1, 1.21, 1.331, 1.4641]

# (scénario, nombre de cas) ; total 200, dont le cas documenté.
PLAN = [
    ("croissance_reguliere", 40),
    ("bpa_negatifs", 30),
    ("zeros", 30),
    ("trous", 30),
    ("historique_court", 30),
    ("valeurs_extremes", 20),
    ("croissance_reguliere_dividende_gele", 19),
    ("documente", 1),
]


def _r(x: float) -> float:
    return round(x, 6)


def _regular(rng: random.Random) -> list:
    v = rng.uniform(0.5, 5.0)
    g = rng.uniform(-0.05, 0.25)
    out = []
    for _ in range(HORIZON):
        out.append(_r(v))
        v *= 1 + g + rng.uniform(-0.03, 0.03)
    return out


def _gen(scenario: str, rng: random.Random) -> list:
    s = _regular(rng)
    if scenario == "croissance_reguliere":
        return s
    if scenario == "croissance_reguliere_dividende_gele":
        k = rng.randint(2, 6)  # dernières années : valeur répétée
        for i in range(HORIZON - k, HORIZON):
            s[i] = s[HORIZON - k - 1]
        return s
    if scenario == "bpa_negatifs":
        for i in rng.sample(range(HORIZON), rng.randint(2, 8)):
            s[i] = _r(-abs(s[i]) * rng.uniform(0.2, 2.0))
        return s
    if scenario == "zeros":
        # Un zéro avant la dernière colonne divise par zéro à la ligne suivante.
        for i in rng.sample(range(HORIZON - 1), rng.randint(1, 3)):
            s[i] = 0.0
        return s
    if scenario == "trous":
        for i in rng.sample(range(HORIZON), rng.randint(1, 4)):
            s[i] = None
        return s
    if scenario == "historique_court":
        n = rng.randint(2, 19)
        return [None] * (HORIZON - n) + s[HORIZON - n:]
    if scenario == "valeurs_extremes":
        mag = rng.choice([1e-6, 1e-3, 1e6, 1e9, 1e12])
        s = [round(x * mag, 12) for x in s]
        if rng.random() < 0.5:
            s[rng.randrange(HORIZON)] = rng.choice([1e-9, 9.99e14, -9.99e14])
        return s
    raise ValueError(scenario)


def _dividends(scenario: str, rng: random.Random) -> list:
    """Série de dividendes du même scénario, sans valeur négative."""
    base = "croissance_reguliere" if scenario == "bpa_negatifs" else scenario
    return [None if v is None else abs(v) for v in _gen(base, rng)]


def generate() -> list[dict]:
    rng = random.Random(SEED)
    cases = []
    for scenario, n in PLAN:
        for k in range(n):
            if scenario == "documente":
                eps, div, cid = list(DOCUMENTED_SERIES), list(DOCUMENTED_SERIES), DOCUMENTED_ID
            else:
                eps = _gen(scenario, rng)
                div = _dividends(scenario, rng)
                cid = f"{scenario}-{k + 1:03d}"
            cases.append({"id": cid, "scenario": scenario, "eps": eps, "dividends": div})
    return cases
