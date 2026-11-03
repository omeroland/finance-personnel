"""Oracle LibreOffice (P1-07, D28) : fait calculer la vraie feuille Simply Investing.

1. génère 200 cas déterministes (scenarios.py) ;
2. les écrit dans des copies du gabarit, 10 actions par copie, un bloc de 4 lignes
   par action à partir de la ligne 6, colonnes B à U (onglets EPS et Dividend) ;
3. fait recalculer les copies par LibreOffice (recalc_uno.py, calculateAll) ;
4. relit V (moyenne) et W (nombre de hausses) et écrit cases.json.

Sortie déterministe : tri stable, pas d'horodatage, flottants en repr Python.
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

import scenarios

EPS_SHEET = "EPS Growth Calculation"
DIV_SHEET = "Dividend Growth Calculation"
FIRST_ROW = 6
BLOCK = 4
PER_COPY = 10
COL_FIRST = 2  # B
ERRORS = {"#DIV/0!", "#VALUE!", "#NUM!", "#N/A", "#REF!", "#NAME?", "#NULL!"}


def block_row(i: int) -> int:
    """Ligne des données de la i-ème action (0-based) d'une copie."""
    return FIRST_ROW + BLOCK * i


def write_copy(template: Path, cases: list[dict], dest: Path) -> None:
    wb = openpyxl.load_workbook(template)
    for i, case in enumerate(cases):
        r = block_row(i)
        for sheet, key in ((EPS_SHEET, "eps"), (DIV_SHEET, "dividends")):
            ws = wb[sheet]
            for j, v in enumerate(case[key]):
                ws.cell(r, COL_FIRST + j).value = v
    wb.save(dest)


def parse_cell(v):
    """(valeur, erreur) d'une cellule recalculée : nombre, vide ('' ou None) ou erreur."""
    if isinstance(v, str):
        if v in ERRORS:
            return None, v
        if v == "":
            return None, None
        raise ValueError(f"valeur texte inattendue : {v!r}")
    return v, None


def read_copy(path: Path, cases: list[dict]) -> list[dict]:
    wb = openpyxl.load_workbook(path, data_only=True)
    out = []
    for i, case in enumerate(cases):
        r = block_row(i) + 1  # ligne des taux : V = moyenne, W = compte
        res = dict(case)
        for sheet, key, count in ((EPS_SHEET, "attendu_eps", "hausses"),
                                  (DIV_SHEET, "attendu_dividendes", "hausses_consecutives")):
            ws = wb[sheet]
            avg, err_a = parse_cell(ws.cell(r, 22).value)
            cnt, err_c = parse_cell(ws.cell(r, 23).value)
            res[key] = {
                "moyenne": avg,
                count: None if cnt is None else int(cnt),
                "erreur": err_a or err_c,
            }
        out.append(res)
    return out


def run(template: Path, out: Path, uno_python: str) -> list[dict]:
    cases = scenarios.generate()
    with tempfile.TemporaryDirectory() as tmp:
        src, dst = Path(tmp, "in"), Path(tmp, "out")
        src.mkdir()
        chunks = [cases[k:k + PER_COPY] for k in range(0, len(cases), PER_COPY)]
        for n, chunk in enumerate(chunks):
            write_copy(template, chunk, src / f"copie_{n:02d}.xlsx")
        subprocess.run([uno_python, str(Path(__file__).with_name("recalc_uno.py")), str(src), str(dst)],
                       check=True)
        results = []
        for n, chunk in enumerate(chunks):
            results.extend(read_copy(dst / f"copie_{n:02d}.xlsx", chunk))
    return results


def dump(results: list[dict], out: Path) -> None:
    doc = {
        "version": 1,
        "graine": scenarios.SEED,
        "horizon": scenarios.HORIZON,
        "colonnes": "B (il y a 20 ans) à U (année 1) ; null = cellule vide",
        "cas": results,
    }
    out.parent.mkdir(parents=True, exist_ok=True)
    out.write_text(json.dumps(doc, indent=1, ensure_ascii=False) + "\n", encoding="utf-8")


def main() -> None:
    ap = argparse.ArgumentParser(description=__doc__)
    ap.add_argument("--template", type=Path, required=True)
    ap.add_argument("--out", type=Path, required=True)
    ap.add_argument("--uno-python", default=os.environ.get("ORACLE_UNO_PYTHON", sys.executable))
    a = ap.parse_args()
    results = run(a.template, a.out, a.uno_python)
    dump(results, a.out)
    errs = sum(1 for c in results if c["attendu_eps"]["erreur"] == "#DIV/0!")
    print(f"{len(results)} cas écrits dans {a.out} ({errs} avec #DIV/0! côté BPA)")


if __name__ == "__main__":
    main()
