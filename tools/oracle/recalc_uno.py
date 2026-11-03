"""Recalcule des classeurs avec LibreOffice sans interface, via l'API UNO (P1-07, D28).

Usage : python3 recalc_uno.py DOSSIER_ENTREE DOSSIER_SORTIE
À exécuter avec un Python qui sait importer `uno` (python3-uno).

Le recalcul au chargement est désactivé par défaut pour les xlsx : on force
`document.calculateAll()` avant d'enregistrer.
"""
from __future__ import annotations

import os
import subprocess
import sys
import tempfile
import time
from pathlib import Path

import uno
from com.sun.star.beans import PropertyValue

PORT = 2002


def _prop(name, value):
    p = PropertyValue()
    p.Name, p.Value = name, value
    return p


def _connect(proc):
    local = uno.getComponentContext()
    resolver = local.ServiceManager.createInstanceWithContext("com.sun.star.bridge.UnoUrlResolver", local)
    url = f"uno:socket,host=127.0.0.1,port={PORT};urp;StarOffice.ComponentContext"
    for _ in range(120):
        if proc.poll() is not None:
            raise RuntimeError("soffice s'est arrêté avant la connexion UNO")
        try:
            return resolver.resolve(url)
        except Exception:
            time.sleep(0.5)
    raise RuntimeError("connexion UNO impossible après 60 s")


def main(src: Path, dst: Path) -> None:
    dst.mkdir(parents=True, exist_ok=True)
    with tempfile.TemporaryDirectory() as profile:
        proc = subprocess.Popen([
            "soffice", "--headless", "--invisible", "--norestore", "--nologo", "--nodefault",
            f"-env:UserInstallation=file://{profile}",
            f"--accept=socket,host=127.0.0.1,port={PORT};urp;",
        ], stdout=subprocess.DEVNULL, stderr=subprocess.DEVNULL)
        try:
            ctx = _connect(proc)
            desktop = ctx.ServiceManager.createInstanceWithContext("com.sun.star.frame.Desktop", ctx)
            for f in sorted(src.glob("*.xlsx")):
                doc = desktop.loadComponentFromURL(
                    uno.systemPathToFileUrl(str(f.resolve())), "_blank", 0, (_prop("Hidden", True),))
                try:
                    doc.calculateAll()
                    doc.storeToURL(uno.systemPathToFileUrl(str((dst / f.name).resolve())),
                                   (_prop("FilterName", "Calc MS Excel 2007 XML"),))
                finally:
                    doc.close(True)
            try:
                desktop.terminate()
            except Exception:
                pass
        finally:
            try:
                proc.wait(timeout=20)
            except subprocess.TimeoutExpired:
                proc.kill()


if __name__ == "__main__":
    if len(sys.argv) != 3:
        sys.exit(__doc__)
    main(Path(sys.argv[1]), Path(sys.argv[2]))
