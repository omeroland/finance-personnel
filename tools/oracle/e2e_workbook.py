"""Classeur de test à 5 actions et résultats LibreOffice (P1-22, D33, D240).

1. écrit 5 actions fictives, produites par programme (aucune donnée réelle), dans une copie du
   gabarit : `classeur-5-actions.xlsx` (onglets SI Worksheet, EPS, Dividend, Average Dividend Yield) ;
2. fait recalculer une copie par LibreOffice (recalc_uno.py, `calculateAll()`) ;
3. relit pour chaque action les colonnes calculées de la feuille principale (moyenne et compte
   de hausses du BPA, croissance du dividende, 10 drapeaux R à AA, compte d'échecs AB) et écrit
   `attendu-libreoffice.json`.

À exécuter dans l'image de l'oracle (`make e2e-workbook` ; Docker requis). Hors `make ci`.
"""
from __future__ import annotations

import argparse
import json
import os
import subprocess
import sys
import tempfile
from pathlib import Path

import openpyxl

MAIN = "SI Worksheet"
EPS = "EPS Growth Calculation"
DIV = "Dividend Growth Calculation"
YLD = "Average Dividend Yield"
FIRST_STOCK_ROW = 7
FIRST_BLOCK_ROW = 6
FLAGS = ["5a", "5b", "6", "7", "8", "9", "10", "11a", "11b", "11c"]  # colonnes R à AA
ERRORS = {"#DIV/0!", "#VALUE!", "#NUM!", "#N/A", "#REF!", "#NAME?", "#NULL!"}


def geo(start: float, rate: float, n: int = 20, pad: int = 0) -> list:
    """Série de n valeurs, les `pad` premières vides (historique court), arrondie à 4 décimales."""
    return [None] * pad + [round(start * (1 + rate) ** i, 4) for i in range(n - pad)]


def build(symbol, name, eps, dps, yld, debt, buyback, pe, pb, fwd_ratio) -> dict:
    """Dérive les saisies de l'instantané des séries : ratio de distribution = DPS / BPA du dernier
    exercice, prix = P/E x BPA, dividende prévu = DPS x fwd_ratio. La feuille et l'app voient ainsi les
    mêmes valeurs (la feuille lit G, K, O, P, Q saisis ; l'app les recalcule des séries)."""
    last_eps = [v for v in eps if v is not None][-1]
    last_dps = [v for v in dps if v is not None][-1]
    return dict(symbol=symbol, name=name, eps=eps, dps=dps, yld=yld, debt=debt, buyback=buyback,
                payout=round(last_dps / last_eps, 4), pe=pe, pb=pb, price=round(pe * last_eps, 2),
                fwd=round(last_dps * fwd_ratio, 4))


def stocks() -> list[dict]:
    """5 actions fictives couvrant des profils différents (aucun seuil frôlé, sauf les écarts de compat)."""
    c_eps = geo(1.5, 0.12)
    c_eps[8] = -0.4                    # une année de perte au milieu
    return [
        build("TSX:FAUXA", "Société fictive A", geo(2.0, 0.10), geo(0.5, 0.10), [0.030] * 20,
              0.40, "yes", 14.0, 2.0, 1.05),
        build("NYSE:FAUXB", "Société fictive B", geo(3.0, 0.03), geo(2.6, 0.03), [0.025] * 20,
              0.90, "no", 30.0, 4.0, 1.03),
        build("NASDAQ:FAUXC", "Société fictive C", c_eps, [0.8] * 20, [0.020] * 20,
              0.30, "yes", 12.0, 1.5, 1.0),
        build("TSX:FAUXD", "Société fictive D", geo(2.5, 0.09), geo(1.0, 0.08), [0.035] * 20,
              0.60, "yes", 40.0, 5.0, 0.9),
        build("TSX:FAUXE", "Société fictive E", geo(1.0, 0.11, pad=8), geo(0.3, 0.09, pad=8),
              [None] * 8 + [0.028] * 12, 0.50, "no", 18.0, 2.5, 1.04),
    ]


def write_workbook(template: Path, dest: Path) -> list[dict]:
    wb = openpyxl.load_workbook(template)
    main, eps, div, yld = wb[MAIN], wb[EPS], wb[DIV], wb[YLD]
    sts = stocks()
    for k, s in enumerate(sts):
        r = FIRST_STOCK_ROW + k
        main.cell(r, 2).value = s["name"]
        main.cell(r, 3).value = s["symbol"]
        main[f"G{r}"], main[f"H{r}"], main[f"J{r}"] = s["payout"], s["debt"], s["buyback"]
        main[f"K{r}"], main[f"O{r}"], main[f"P{r}"], main[f"Q{r}"] = s["pe"], s["pb"], s["fwd"], s["price"]
        for j in range(20):
            for ws, key, row in ((eps, "eps", FIRST_BLOCK_ROW + 4 * k), (div, "dps", FIRST_BLOCK_ROW + 4 * k),
                                 (yld, "yld", FIRST_BLOCK_ROW + 2 * k)):
                ws.cell(row, 2 + j).value = s[key][j]
    wb.save(dest)
    return sts


def cell(v):
    if isinstance(v, str):
        return None if v == "" else v
    return v


def read_expected(path: Path, sts: list[dict]) -> list[dict]:
    ws = openpyxl.load_workbook(path, data_only=True)[MAIN]
    out = []
    for k, s in enumerate(sts):
        r = FIRST_STOCK_ROW + k
        flags = {n: cell(ws.cell(r, 18 + i).value) for i, n in enumerate(FLAGS)}  # R = colonne 18
        out.append({
            "symbole": s["symbol"],
            "moyenne_bpa": cell(ws[f"D{r}"].value),
            "hausses_bpa": cell(ws[f"E{r}"].value),
            "moyenne_dividende": cell(ws[f"F{r}"].value),
            "drapeaux": flags,
            "echecs": cell(ws[f"AB{r}"].value),
        })
    return out


def main() -> None:
    ap = argparse.ArgumentParser(description=__doc__)
    ap.add_argument("--template", type=Path, required=True)
    ap.add_argument("--out", type=Path, required=True)
    a = ap.parse_args()
    a.out.mkdir(parents=True, exist_ok=True)
    sts = write_workbook(a.template, a.out / "classeur-5-actions.xlsx")
    with tempfile.TemporaryDirectory() as tmp:
        src, dst = Path(tmp, "in"), Path(tmp, "out")
        src.mkdir()
        write_workbook(a.template, src / "calc.xlsx")
        subprocess.run([os.environ.get("ORACLE_UNO_PYTHON", sys.executable),
                        str(Path(__file__).with_name("recalc_uno.py")), str(src), str(dst)], check=True)
        expected = read_expected(dst / "calc.xlsx", sts)
    doc = {"version": 1, "libreoffice": "7.4.7", "actions": expected}
    (a.out / "attendu-libreoffice.json").write_text(
        json.dumps(doc, indent=1, ensure_ascii=False) + "\n", encoding="utf-8")
    print(f"{len(expected)} actions : classeur et attendu écrits dans {a.out}")


if __name__ == "__main__":
    main()
