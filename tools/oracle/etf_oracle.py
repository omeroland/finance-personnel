"""Oracle LibreOffice de l'outil SÉLECTION ET GESTION (P2-07).

1. prend les cas de etf_scenarios.py (données synthétiques, graine fixe) ;
2. remplit par programme une copie du gabarit par cas (un classeur par compte pour RENDEMENT) ;
3. fait recalculer les copies par LibreOffice (recalc_uno.py, calculateAll) ;
4. relit les cellules de l'outil, calcule en parallèle le résultat attendu de l'app corrigée
   (etf_model.py) et écrit testdata/oracle/etf_cases.json.

Chaque cas est étiqueté « fidele » ou porte l'un des 9 écarts. La génération échoue si un cas
fidèle diverge de l'outil, si un écart ne diverge pas, ou si l'un des 9 écarts manque.
Sortie déterministe : ordre des cas fixe, pas d'horodatage, flottants arrondis à 12 décimales.
"""
from __future__ import annotations

import argparse
import json
import os
import subprocess
import sys
import tempfile
from pathlib import Path

try:  # absent hors du conteneur de l'oracle : les tests de logique n'en ont pas besoin
    import openpyxl
except ImportError:  # pragma: no cover
    openpyxl = None

import etf_model as M
import etf_scenarios as S

VERSION = 1
GABARIT = "selection_gestion_template.xlsx"
MIN_CAS = 30


def norm(v):
    """Valeur de cellule recalculée -> JSON : vide = None, nombres arrondis, erreurs et textes tels quels."""
    if v is None or v == "":
        return None
    if isinstance(v, bool):
        return v
    if isinstance(v, (int, float)):
        r = round(float(v), 12)
        return int(r) if r == int(r) and abs(r) < 1e15 else r
    return v


def cell(ws, addr):
    return norm(ws[addr].value)


# ---------------------------------------------------------------- écriture des entrées

def _ecrire_catalogue(wb, textes: dict | None = None) -> None:
    ws = wb["FNB"]
    for row in ws.iter_rows(min_row=2, max_row=ws.max_row, max_col=37):
        for c in row:
            if not isinstance(c, openpyxl.cell.cell.MergedCell):
                c.value = None
    for i, f in enumerate(S.CATALOGUE):
        r = 2 + i
        ws.cell(r, 1, f["symbole"])
        ws.cell(r, 2, f["nom"])
        ws.cell(r, 3, "Fonds synthétique (P2-07)")
        ws.cell(r, 4, "Indiciel")
        for j, k in enumerate(["ca", "us", "intl", "obl_cad", "obl_autres", "rfg"]):
            ws.cell(r, 7 + j, f[k])
        ws.cell(r, 13, "100M$")
        for col in (18, 19, 20, 21):
            ws.cell(r, col, "-")
        if textes and textes["symbole"] == f["symbole"]:
            ws.cell(r, 13, textes["grosseur"])
            ws.cell(r, 18, textes["rendement_1an"])
            ws.cell(r, 19, textes["rendement_3ans"])
            ws.cell(r, 39, f"=ISNUMBER(R{r})")   # AM
            ws.cell(r, 40, f"=ISNUMBER(M{r})")   # AN
            ws.cell(r, 41, f"=SUM(R{r}:R{r})")   # AO


def _ligne_fonds(wb, fonds, cle_pct):
    ws = wb["CRÉATION"]
    for i, f in enumerate(fonds):
        ws.cell(28 + i, 3, f["symbole"])
        ws.cell(28 + i, 5, f[cle_pct])


def remplir(wb, case: dict, sub: int = 0) -> None:
    e, kind = case["entrees"], case["kind"]
    _ecrire_catalogue(wb, e if kind == "catalogue" else None)
    if kind == "achats":
        _ligne_fonds(wb, e["fonds"], "pct")
        ws = wb["ACHATS"]
        ws["C7"] = e["montant"]
        n = len(e["fonds"])
        for i, f in enumerate(e["fonds"]):
            ws.cell(10 + i, 6, f["prix"])
        if e.get("e32_remplie"):
            ws["E32"] = 0.1  # lève le garde-fou IF($E$32="","",...) de F30 et H30
        ws["K31"] = f"=SUMPRODUCT(F10:F{9 + n},E10:E{9 + n})/SUM(E10:E{9 + n})"
        ws["L31"] = f"=SUMPRODUCT(H10:H{9 + n},E10:E{9 + n})/SUM(E10:E{9 + n})"
    elif kind == "reequilibrage":
        _ligne_fonds(wb, e["fonds"], "pct_cible")
        ws = wb["RÉÉQUILIBRAGE"]
        ws["D15"] = e["cible_encaisse"]
        ws["E15"] = e["encaisse"]
        for i, f in enumerate(e["fonds"]):
            ws.cell(20 + i, 5, f["valeur"])
            if f.get("prix"):
                ws.cell(20 + i, 10, f["prix"])
    elif kind == "creation":
        _ligne_fonds(wb, [{"symbole": p["symbole"], "pct": p["pct"]} for p in e["fonds"]], "pct")
        wb["CRÉATION"]["C9"] = e["part_actions"]
    elif kind == "diversification":
        ws = wb["DIVERSIFICATION"]
        for i, p in enumerate(e["positions"]):
            ws.cell(8 + i, 3, p["symbole"])
            ws.cell(8 + i, 5, p["pct"])
    elif kind in ("rendement", "rendement_annuel", "rendement_multi"):
        s = e["comptes"][sub] if kind == "rendement_multi" else e
        ws = wb["RENDEMENT"]
        for j, v in enumerate(s.get("annuels", [])):
            ws.cell(10 + j, 12, v)
        if "annuel_saisi" in s:
            ws["L10"] = s["annuel_saisi"]
        if "fins" in s:
            ws["C6"] = s["depart"]
            n = len(s["fins"])
            for i in range(n):
                ws.cell(10 + i, 5, s["ajouts"][i])
                ws.cell(10 + i, 7, s["fins"][i])
            ws["AJ10"] = "=" + "*".join(f"(1+H{10 + i})" for i in range(n)) + "-1"
    elif kind == "catalogue":
        pass
    else:
        raise ValueError(kind)


def n_jobs(case: dict) -> int:
    return len(case["entrees"]["comptes"]) if case["kind"] == "rendement_multi" else 1


# ---------------------------------------------------------------- lecture de l'outil

def _lire_rendement(wb, s: dict) -> dict:
    ws = wb["RENDEMENT"]
    out = {}
    if "fins" in s:
        n = len(s["fins"])
        out["rendement_mensuel"] = [cell(ws, f"H{10 + i}") for i in range(n)]
        out["aacj_mois"] = [cell(ws, f"I{10 + i}") for i in range(n)]
        if n == 12:
            out["tri_annee"] = cell(ws, "U22")
    if "annuels" in s:
        out["annees"] = cell(ws, "K6")
        out["rendement_annualise"] = cell(ws, "K23")
    return out


def lire(wb, case: dict, sub: int = 0) -> dict:
    e, kind = case["entrees"], case["kind"]
    if kind == "achats":
        ws, n = wb["ACHATS"], len(e["fonds"])
        return {
            "montants": [cell(ws, f"G{10 + i}") for i in range(n)],
            "parts": [cell(ws, f"H{10 + i}") for i in range(n)],
            "total_pct": cell(ws, "E30"),
            "total_montant": cell(ws, "G30"),
            "prix_moyen": cell(ws, "F30"),
            "parts_total": cell(ws, "H30"),
            "formule_corrigee": {"prix_moyen": cell(ws, "K31"), "parts_pondere": cell(ws, "L31")},
        }
    if kind == "reequilibrage":
        ws, n = wb["RÉÉQUILIBRAGE"], len(e["fonds"])

        def cl(r):
            return {"cible": cell(ws, f"D{r}"), "pct": cell(ws, f"F{r}"), "statut": cell(ws, f"G{r}"),
                    "ecart": cell(ws, f"H{r}")}
        return {
            "total": cell(ws, "E48"),
            "cible_totale": cell(ws, "D48"),
            "encaisse": cl(15),
            "fonds": [{"cible": cell(ws, f"D{20 + i}"), "valeur": cell(ws, f"E{20 + i}"),
                       "pct": cell(ws, f"F{20 + i}"), "statut": cell(ws, f"G{20 + i}"),
                       "ecart": cell(ws, f"H{20 + i}")} for i in range(n)],
            "parts": [cell(ws, f"K{20 + i}") if e["fonds"][i].get("prix") else None for i in range(n)],
            "actions": cl(46),
            "obligations": cl(47),
        }
    if kind == "creation":
        ws = wb["CRÉATION"]
        ex = dict(zip(["ca", "us", "intl", "obl_cad", "obl_autres"], (cell(ws, f"{c}48") for c in "FGHIJ")))
        cible = cell(ws, "I50")
        return {"exposition": ex, "cible_obligations": {"cad": cible, "autres": None},
                "ecart_obligations": {"cad": norm(ex["obl_cad"] - cible), "autres": None}}
    if kind == "diversification":
        ws = wb["DIVERSIFICATION"]
        return {"actions_pct": [cell(ws, f"E{r}") for r in (94, 95, 96)],
                "obligations_pct": [cell(ws, f"E{r}") for r in (99, 100)],
                "portion_actions": cell(ws, "E103"), "portion_obligations": cell(ws, "E104")}
    if kind == "catalogue":
        ws = wb["FNB"]
        return {"rendement_1an": cell(ws, "R2"), "rendement_3ans": cell(ws, "S2"), "grosseur": cell(ws, "M2"),
                "rendement_1an_numerique": cell(ws, "AM2"), "grosseur_numerique": cell(ws, "AN2"),
                "somme_rendement_1an": cell(ws, "AO2")}
    if kind == "rendement":
        return _lire_rendement(wb, e)
    if kind == "rendement_multi":
        return _lire_rendement(wb, e["comptes"][sub])
    if kind == "rendement_annuel":
        o = _lire_rendement(wb, e)
        ws = wb["RENDEMENT"]
        return {"rendement_mensuel": o["rendement_mensuel"], "aacj_mois": o["aacj_mois"],
                "rendement_annuel": cell(ws, "K23"), "annees": cell(ws, "K6"),
                "rendement_annuel_depuis_mois": cell(ws, "AJ10")}
    raise ValueError(kind)


# ---------------------------------------------------------------- résultat attendu de l'app

def attendu(case: dict, outils: list[dict]) -> dict:
    e, kind = case["entrees"], case["kind"]
    if kind == "achats":
        return M.achats(e["montant"], e["fonds"])
    if kind == "reequilibrage":
        return M.reequilibrage(e, S.PROFILS)
    if kind == "creation":
        expo = M.exposition(e["fonds"], S.PROFILS)
        autres = expo.pop("autres_actifs")
        c = e["cibles_obligations"]
        return {"exposition": expo, "autres_actifs": autres, "cible_obligations": c,
                "ecart_obligations": M.ecart_obligations(expo, c)}
    if kind == "diversification":
        return M.diversification(e["positions"], S.PROFILS)
    if kind == "catalogue":
        r1, r3, g = M.parse_pct(e["rendement_1an"]), M.parse_pct(e["rendement_3ans"]), M.parse_taille(e["grosseur"])
        return {"rendement_1an": r1, "rendement_3ans": r3, "grosseur": g,
                "rendement_1an_numerique": r1 is not None, "grosseur_numerique": g is not None,
                "somme_rendement_1an": r1}
    if kind == "rendement":
        if "annuels" in e:
            return {"annees": len(e["annuels"]), "rendement_annualise": M.annualise(e["annuels"])}
        return M.rendement(e)
    if kind == "rendement_annuel":
        r = M.rendement(e)
        return {"rendement_mensuel": r["rendement_mensuel"], "aacj_mois": r["aacj_mois"],
                "rendement_annuel": r["rendement_periode"]}
    if kind == "rendement_multi":
        return {"rendement_mensuel_par_compte": [M.rendement(c)["rendement_mensuel"] for c in e["comptes"]],
                "consolide": M.consolide(e["comptes"])}
    raise ValueError(kind)


# ---------------------------------------------------------------- orchestration

def _chemin(doc: dict, chemin: str):
    v = doc
    for k in chemin.split("."):
        v = v[k]
    return v


def verifier(cas: dict) -> list[str]:
    """Défauts du cas : liste vide quand le cas tient ses promesses."""
    defauts = []
    o, a = cas["outil"], cas["attendu_app"]
    egal, diff = cas["comparaison"]["egal"], cas["comparaison"]["different"]
    if cas["etiquette"] == "fidele" and (not egal or diff or cas["ecart"]):
        defauts.append("un cas fidèle doit comparer au moins un groupe et n'en déclarer aucun divergent")
    if cas["etiquette"] == "ecart" and (not diff or cas["ecart"] is None):
        defauts.append("un écart doit déclarer un code et au moins un groupe divergent")
    for k in egal:
        if not M.proche(o.get(k), a.get(k)):
            defauts.append(f"« {k} » devrait être égal : outil={o.get(k)!r} app={a.get(k)!r}")
    for k in diff:
        if M.proche(o.get(k), a.get(k)):
            defauts.append(f"« {k} » devrait diverger : outil={o.get(k)!r} app={a.get(k)!r}")
    for p_outil, p_app in cas["verifications"]:
        vo, va = _chemin({"outil": o, "attendu_app": a}, p_outil), _chemin({"outil": o, "attendu_app": a}, p_app)
        if not M.proche(vo, va):
            defauts.append(f"vérification {p_outil} = {p_app} : {vo!r} != {va!r}")
    return defauts


def assembler(cases: list[dict], lectures: dict) -> list[dict]:
    """lectures[(i, sub)] = sortie de l'outil. Retourne les cas complets."""
    res = []
    for i, case in enumerate(cases):
        subs = [lectures[(i, s)] for s in range(n_jobs(case))]
        if case["kind"] == "rendement_multi":
            outil = {"rendement_mensuel_par_compte": [s["rendement_mensuel"] for s in subs], "consolide": None}
        else:
            outil = subs[0]
        app = attendu(case, subs)
        res.append({
            "id": case["id"], "onglet": case["onglet"], "etiquette": case["etiquette"], "ecart": case["ecart"],
            "description": case["description"], "entrees": case["entrees"], "outil": outil, "attendu_app": app,
            "comparaison": {"egal": case["egal"], "different": case["different"]},
            "verifications": case["verifs"],
        })
    return res


def controler(res: list[dict]) -> None:
    defauts = [f"{c['id']} : {d}" for c in res for d in verifier(c)]
    codes = {c["ecart"] for c in res if c["ecart"]}
    attendus = {code for code, _, _ in S.ECARTS}
    if codes != attendus:
        defauts.append(f"écarts manquants ou inconnus : {sorted(attendus ^ codes)}")
    if len(res) < MIN_CAS:
        defauts.append(f"{len(res)} cas, {MIN_CAS} exigés")
    if len({c["id"] for c in res}) != len(res):
        defauts.append("identifiants en double")
    if defauts:
        raise SystemExit("L'oracle est incohérent :\n- " + "\n- ".join(defauts))


def run(template: Path, uno_python: str) -> list[dict]:
    cases = S.generate()
    with tempfile.TemporaryDirectory() as tmp:
        src, dst = Path(tmp, "in"), Path(tmp, "out")
        src.mkdir()
        jobs = []
        for i, case in enumerate(cases):
            for s in range(n_jobs(case)):
                wb = openpyxl.load_workbook(template)
                remplir(wb, case, s)
                wb.save(src / f"cas_{i:03d}_{s}.xlsx")
                jobs.append((i, s))
        subprocess.run([uno_python, str(Path(__file__).with_name("recalc_uno.py")), str(src), str(dst)], check=True)
        lectures = {}
        for i, s in jobs:
            wb = openpyxl.load_workbook(dst / f"cas_{i:03d}_{s}.xlsx", data_only=True)
            lectures[(i, s)] = lire(wb, cases[i], s)
    return assembler(cases, lectures)


def dump(res: list[dict], out: Path) -> None:
    doc = {
        "version": VERSION,
        "graine": S.SEED,
        "gabarit": GABARIT,
        "outil": "LibreOffice 7.4.7, calculateAll() forcé",
        "unites": "Ratios en fractions (0.20 = 20 %), argent en dollars canadiens. null = cellule vide ; "
                  "chaîne « #VALUE! » etc. = erreur renvoyée par l'outil.",
        "ecarts": [{"code": c, "onglet": o, "libelle": lib} for c, o, lib in S.ECARTS],
        "cas": res,
    }
    out.parent.mkdir(parents=True, exist_ok=True)
    out.write_text(json.dumps(doc, indent=1, ensure_ascii=False) + "\n", encoding="utf-8")


def main() -> None:
    ap = argparse.ArgumentParser(description=__doc__)
    ap.add_argument("--template", type=Path, required=True)
    ap.add_argument("--out", type=Path, required=True)
    ap.add_argument("--uno-python", default=os.environ.get("ORACLE_UNO_PYTHON", sys.executable))
    a = ap.parse_args()
    res = run(a.template, a.uno_python)
    controler(res)
    dump(res, a.out)
    ecarts = sum(1 for c in res if c["ecart"])
    print(f"{len(res)} cas écrits dans {a.out} ({len(res) - ecarts} fidèles, {ecarts} écarts)")


if __name__ == "__main__":
    main()
