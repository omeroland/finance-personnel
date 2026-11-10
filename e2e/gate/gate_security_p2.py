#!/usr/bin/env python3
"""P2-14 : revue de sécurité de la phase 2 rejouée sur la pile réelle (realm de TEST, données synthétiques).

Script voisin de gate_security.py (phase 1, dont il réutilise les fonctions d'appel, de jeton et de journal).
Pile démarrée par `COMPOSE_PROJECT_NAME=financegate2p2 make up-test`, puis l'accès d'administration local de Keycloak
(`docker compose -p financegate2p2 -f deploy/compose.yaml -f deploy/compose.test-realm.yaml -f deploy/compose.admin.yaml
--env-file deploy/.env --profile local up -d keycloak`, voir deploy/README.md). Le script :
  1. crée par l'API d'administration Keycloak un second utilisateur (détruit avec `down -v` du projet) ;
  2. vérifie que CHAQUE route de phase 2 de api/openapi.yaml (lue dans le fichier) est couverte, exige un JWT (401 sans jeton)
     et rejette des jetons forgés sur deux routes de phase 2 ;
  3. vérifie l'isolement par owner_id avec deux utilisateurs sur chacune de ces routes (404 ou liste sans les objets de A) ;
  4. vérifie que chaque écriture ajoute une ligne à core.audit_log et que le journal ne contient aucun montant ni quantité ;
  5. vérifie que le registre refuse UPDATE, DELETE et TRUNCATE : avec le rôle applicatif (droits) et avec le propriétaire
     des tables (déclencheur de D320), et que l'API n'a aucune route de modification ou de suppression ;
  6. rejoue la validation aux frontières, les en-têtes de sécurité et la limitation de débit sur des routes de phase 2.

Usage : e2e/gate/gate_security_p2.py [--base https://localhost:9443] [--project financegate2p2] [--openapi api/openapi.yaml]
Aucun secret n'est affiché : les mots de passe viennent de deploy/.env (hors git) ou sont factices (realm de test).
Sortie : une ligne `RESULT <ok|ECHEC> <identifiant> : <détail>` par vérification.
"""
import argparse
import base64
import concurrent.futures
import hashlib
import hmac
import json
import os
import pathlib
import re
import subprocess
import sys
import tempfile
import time
import urllib.parse
import uuid

sys.path.insert(0, os.path.dirname(os.path.abspath(__file__)))
import gate_security as gs  # noqa: E402

record, call, jbody = gs.record, gs.call, gs.jbody


def phase2_operations(openapi):
    """(operationId, méthode, chemin) des routes sous /api/v1/accounts et /api/v1/portfolio."""
    ops, chemin, methode = [], None, None
    for ligne in pathlib.Path(openapi).read_text().splitlines():
        m = re.match(r"^  (/api/v1/\S+):\s*$", ligne)
        if m:
            chemin, methode = m.group(1), None
            continue
        m = re.match(r"^    (get|post|put|patch|delete):\s*$", ligne)
        if m and chemin:
            methode = m.group(1).upper()
            continue
        m = re.match(r"^      operationId: (\w+)\s*$", ligne)
        if m and chemin and methode and re.match(r"^/api/v1/(accounts|portfolio)", chemin):
            ops.append((m.group(1), methode, chemin))
    return ops


def main():
    ap = argparse.ArgumentParser()
    ap.add_argument("--base", default="https://localhost:9443")
    ap.add_argument("--project", default="financegate2p2")
    ap.add_argument("--admin-base", default="http://127.0.0.1:8180")
    ap.add_argument("--env", default="deploy/.env")
    ap.add_argument("--openapi", default="api/openapi.yaml")
    a = ap.parse_args()
    base = a.base
    env = dict(l.strip().split("=", 1) for l in pathlib.Path(a.env).read_text().splitlines() if "=" in l)
    kc = gs.Kc(base, env["KEYCLOAK_ADMIN_PASSWORD"], a.admin_base)

    def psql(sql, user="finance", password=None):
        e = dict(os.environ)
        cmd = ["docker", "exec"] + (["-e", "PGPASSWORD=" + password] if password else []) + [
            a.project + "-db-1", "psql"] + (["-h", "127.0.0.1"] if password else []) + ["-U", user, "-d", "finance", "-Atc", sql]
        r = subprocess.run(cmd, capture_output=True, text=True, env=e)
        return (r.stdout + r.stderr).strip()

    kc.create_user("finance", "gate-user-b")
    tok_a = kc.token("finance", "finance-test", "test-user", gs.FAUX_MDP)
    tok_b = kc.token("finance", "finance-test", "gate-user-b", gs.FAUX_MDP)
    claims = lambda t: json.loads(gs.unb64(t.split(".")[1]))
    sub_a, sub_b = claims(tok_a)["sub"], claims(tok_b)["sub"]
    record(sub_a != sub_b, "comptes", "deux sub distincts dans le realm finance")

    def A(m, p, body=None, **kw):
        return call(m, base + "/api/v1" + p, token=tok_a, body=body, **kw)

    def B(m, p, body=None, **kw):
        return call(m, base + "/api/v1" + p, token=tok_b, body=body, **kw)

    def retry(fn):  # limitation de débit de Traefik (D34) : on rejoue seulement après un 429
        for _ in range(8):
            r = fn()
            if r[0] != 429:
                return r
            time.sleep(1.5)
        return r

    # --- 1. Couverture : chaque route de phase 2 du contrat ---------------------------------------------
    ops = phase2_operations(a.openapi)
    ids = sorted(o[0] for o in ops)
    attendues = sorted(["listAccounts", "createAccount", "listTransactions", "createTransaction", "createStatement", "importCsv", "listPositions",
                        "listEtfs", "listModels", "createModel", "updateModel", "createPurchasePlan", "createRebalancePlan", "getExposure", "getPerformance"])
    record(ids == attendues, "couverture.routes_phase2", "%d routes de phase 2 dans %s : %s" % (len(ops), a.openapi, ", ".join(ids)))
    fake = str(uuid.uuid4())
    tag = uuid.uuid4().hex[:6]  # noms uniques : le script peut être rejoué sur la même pile
    chemin_ex = {"accounts/{id}": fake, "models/{id}": fake}
    sans = []
    for ident, m, p in sorted(ops):
        url = re.sub(r"\{id\}", fake, p)
        s, h, b = call(m, base + url, body={} if m in ("POST", "PUT") else None)
        if s != 401 or "problem+json" not in h.get("Content-Type", ""):
            sans.append("%s %s=%s" % (m, p, s))
    record(not sans, "jwt.15_routes_sans_jeton", ("%d routes de phase 2 sans jeton -> 401 problem+json" % len(ops)) if not sans else "; ".join(sans))

    # Jetons forgés sur deux routes de phase 2 (le contrôle de la phase 1 portait sur /stocks).
    c = claims(tok_a)
    now = int(time.time())
    payload = {"iss": c["iss"], "aud": "finance-api", "sub": c["sub"], "exp": now + 300, "iat": now}
    jwks = jbody(call("GET", base + "/auth/realms/finance/protocol/openid-connect/certs")[2])
    hdr = gs.b64(json.dumps({"alg": "HS256", "typ": "JWT"}).encode())
    pl = gs.b64(json.dumps(payload).encode())
    forge = ".".join([hdr, pl, gs.b64(hmac.new(json.dumps(jwks["keys"][0]).encode(), (hdr + "." + pl).encode(), hashlib.sha256).digest())])
    nonet = ".".join([gs.b64(json.dumps({"alg": "none", "typ": "JWT"}).encode()), pl, ""])
    p0, p1, p2 = tok_a.split(".")
    altere = ".".join([p0, gs.b64(json.dumps({**c, "sub": str(uuid.uuid4())}).encode()), p2])
    for nom, t in [("hs256_forge", forge), ("alg_none", nonet), ("signature_alteree", altere), ("jeton_poubelle", "abc.def.ghi")]:
        for chemin in ("/accounts", "/portfolio/models"):
            s, _, _ = call("GET", base + "/api/v1" + chemin, token=t)
            record(s == 401, "jwt.%s%s" % (nom, chemin), "HTTP %s" % s)
    s, _, _ = A("GET", "/accounts")
    record(s == 200, "jwt.jeton_valide", "GET /accounts avec un jeton valide : HTTP %s" % s)

    # --- 2. Données de A, avec des montants reconnaissables pour l'audit ---------------------------------
    montants = ["7777.77", "123.456", "98.7654", "4242.42", "5555.55", "777.77"]
    symbole = "TSX:GATEP2"
    audit_max = lambda: int(psql("SELECT coalesce(max(id),0) FROM core.audit_log"))

    def lignes(owner, depuis):
        out = psql("SELECT id||'|'||action||'|'||entity||'|'||(%s) FROM core.audit_log WHERE owner_id='%s' AND id>%d ORDER BY id" % (
            " OR ".join("(coalesce(entity,'')||coalesce(before::text,'')||coalesce(after::text,'')) LIKE '%%%s%%'" % m for m in montants), owner, depuis))
        return [l for l in out.splitlines() if l]

    def ecrit(ident, fn, want):
        avant = audit_max()
        s, _, b = retry(fn)
        rows = lignes(sub_a, avant)
        fuite = [r for r in rows if r.endswith("|t")]
        record(s == want and len(rows) >= 1 and not fuite, "audit." + ident,
               "HTTP %s ; %d ligne(s) d'audit : %s ; montant ou quantité dans le journal : %s" % (
                   s, len(rows), "; ".join(r.split("|")[1] for r in rows), "OUI" if fuite else "non"))
        return jbody(b)

    acc = ecrit("createAccount POST /accounts", lambda: A("POST", "/accounts", {"name": "Gate A celi " + tag, "type": "celi", "broker": "Synthétique", "currency": "CAD"}), 201)
    acc2 = ecrit("createAccount (compte des relevés)", lambda: A("POST", "/accounts", {"name": "Gate A reer " + tag, "type": "reer", "broker": "Synthétique", "currency": "CAD"}), 201)
    ida, ida2 = acc["id"], acc2["id"]
    ecrit("createTransaction contribution", lambda: A("POST", "/accounts/%s/transactions" % ida, {"type": "contribution", "tradeDate": "2025-01-10", "amount": montants[0], "currency": "CAD"}), 201)
    tx = ecrit("createTransaction buy", lambda: A("POST", "/accounts/%s/transactions" % ida, {"type": "buy", "tradeDate": "2025-01-15", "symbol": symbole, "quantity": montants[1], "price": montants[2], "currency": "CAD"}), 201)
    ecrit("createTransaction reversesId (écriture inverse)", lambda: A("POST", "/accounts/%s/transactions" % ida, {
        "type": "buy", "tradeDate": "2025-01-15", "symbol": symbole, "quantity": montants[1], "price": montants[2], "currency": "CAD", "reversesId": tx["id"]}), 201)
    ecrit("createTransaction buy (position conservée)", lambda: A("POST", "/accounts/%s/transactions" % ida, {"type": "buy", "tradeDate": "2025-02-01", "symbol": symbole, "quantity": "10", "price": "50", "currency": "CAD"}), 201)
    ecrit("createStatement POST /statements", lambda: A("POST", "/accounts/%s/statements" % ida2, {"month": "2025-01", "endValue": montants[3], "currency": "CAD"}), 201)
    bnd = "----gate" + uuid.uuid4().hex
    csv_a = ("date,compte,type,symbole,quantité,prix,devise,frais,montant,ratio,contrepartie,note\n2025-03-01,Gate A celi %s,contribution,,,,CAD,,%s,,,\n" % (tag, montants[4])).encode()

    def csv_post(who, ida_, contenu):
        mp = ("--%s\r\nContent-Disposition: form-data; name=\"file\"; filename=\"t.csv\"\r\nContent-Type: text/csv\r\n\r\n" % bnd).encode() + contenu + ("\r\n--%s--\r\n" % bnd).encode()
        return who("POST", "/accounts/%s/imports/csv?broker=generic" % ida_, raw=(mp, "multipart/form-data; boundary=" + bnd))

    ecrit("importCsv POST /imports/csv", lambda: csv_post(A, ida, csv_a), 200)
    # Catalogue : aucune route d'import (D401) ; profil synthétique posé en SQL pour le propriétaire A seulement.
    psql("INSERT INTO etf.etf_profile (owner_id, instrument_id, as_of, type, region, equity_us, mer, esg, source) "
         "SELECT '%s', id, DATE '2025-10-31', 'equity', 'us', 1, 0.001, false, 'manual' FROM core.instrument WHERE owner_id='%s' AND symbol='%s' ON CONFLICT DO NOTHING" % (sub_a, sub_a, symbole))
    modele = ecrit("createModel POST /portfolio/models", lambda: A("POST", "/portfolio/models", {
        "name": "Gate A modèle " + tag, "targets": [{"assetClass": "equity_us", "target": 1}], "holdings": [{"symbol": symbole, "weight": 1}]}), 201)
    idm = modele["id"]
    ecrit("updateModel PUT /portfolio/models/{id}", lambda: A("PUT", "/portfolio/models/" + idm, {
        "name": "Gate A modèle " + tag, "targets": [{"assetClass": "equity_us", "target": 1}], "holdings": [{"symbol": symbole, "weight": 1}], "bands": {"relative": 0.25}}), 200)
    # Lectures et calculs : aucune écriture au journal.
    avant = audit_max()
    lect = [("listAccounts", lambda: A("GET", "/accounts")), ("listTransactions", lambda: A("GET", "/accounts/%s/transactions" % ida)),
            ("listPositions", lambda: A("GET", "/accounts/%s/positions" % ida)), ("listEtfs", lambda: A("GET", "/portfolio/etfs")),
            ("listModels", lambda: A("GET", "/portfolio/models")),
            ("createPurchasePlan", lambda: A("POST", "/portfolio/models/%s/purchase-plan" % idm, {"amount": "1000", "currency": "CAD", "prices": {symbole: "10"}})),
            ("createRebalancePlan", lambda: A("POST", "/portfolio/rebalance-plans", {"modelId": idm, "accountIds": [ida], "currency": "CAD", "prices": {symbole: "10"}})),
            ("getExposure", lambda: A("GET", "/portfolio/exposure?accounts=%s&modelId=%s" % (ida, idm))),
            ("getPerformance", lambda: A("GET", "/portfolio/performance?accounts=%s&to=2025-12-31" % ida))]
    codes = {n: retry(f)[0] for n, f in lect}
    record(all(v == 200 for v in codes.values()), "owner.A_lit_ses_donnees", "lectures et plans de A : " + ", ".join("%s=%s" % kv for kv in codes.items()))
    record(audit_max() == avant, "audit.lectures_sans_ecriture", "9 lectures ou plans calculés : %d ligne(s) ajoutée(s) (attendu 0)" % (audit_max() - avant))
    pos_a = jbody(A("GET", "/accounts/%s/positions" % ida)[2])
    nb_tx_a = len(jbody(A("GET", "/accounts/%s/transactions" % ida)[2])["items"])
    mod_a = jbody(A("GET", "/portfolio/models")[2])

    # --- 3. Isolement : B rejoue chaque route de phase 2 sur les objets de A ----------------------------
    audit_a_avant = int(psql("SELECT count(*) FROM core.audit_log WHERE owner_id='%s'" % sub_a))
    b_compte = jbody(B("POST", "/accounts", {"name": "Gate B celi " + tag, "type": "celi", "broker": "Synthétique", "currency": "CAD"})[2])
    idb = b_compte["id"]
    # B a son propre FNB au catalogue : son corps est valide pour lui, donc seul le propriétaire du modèle peut faire refuser la requête.
    sym_b = "TSX:GATEPB"
    B("POST", "/accounts/%s/transactions" % idb, {"type": "buy", "tradeDate": "2025-01-15", "symbol": sym_b, "quantity": "1", "price": "10", "currency": "CAD"})
    psql("INSERT INTO etf.etf_profile (owner_id, instrument_id, as_of, type, region, equity_us, mer, esg, source) "
         "SELECT '%s', id, DATE '2025-10-31', 'equity', 'us', 1, 0.001, false, 'manual' FROM core.instrument WHERE owner_id='%s' AND symbol='%s' ON CONFLICT DO NOTHING" % (sub_b, sub_b, sym_b))
    corps_b_valide = {"name": "Gate A modèle " + tag, "targets": [{"assetClass": "equity_us", "target": 1}], "holdings": [{"symbol": sym_b, "weight": 1}]}
    corps_tx = {"type": "contribution", "tradeDate": "2025-04-01", "amount": "10", "currency": "CAD"}
    cas = {
        "listAccounts": (lambda: B("GET", "/accounts"), "liste_sans_A"),
        "createAccount": (lambda: B("POST", "/accounts", {"name": "Gate A celi " + tag, "type": "celi", "broker": "X", "currency": "CAD"}), 201),
        "listTransactions": (lambda: B("GET", "/accounts/%s/transactions" % ida), 404),
        "createTransaction": (lambda: B("POST", "/accounts/%s/transactions" % ida, corps_tx), 404),
        "createStatement": (lambda: B("POST", "/accounts/%s/statements" % ida2, {"month": "2025-02", "endValue": "1", "currency": "CAD"}), 404),
        "importCsv": (lambda: csv_post(B, ida, csv_a), 404),
        "listPositions": (lambda: B("GET", "/accounts/%s/positions" % ida), 404),
        "listEtfs": (lambda: B("GET", "/portfolio/etfs"), "etfs_sans_A"),
        "listModels": (lambda: B("GET", "/portfolio/models"), "modeles_sans_A"),
        "createModel": (lambda: B("POST", "/portfolio/models", {"name": "Gate B modèle " + tag, "targets": [{"assetClass": "equity_us", "target": 1}], "holdings": [{"symbol": symbole, "weight": 1}]}), 400),
        "updateModel": (lambda: B("PUT", "/portfolio/models/" + idm, corps_b_valide), 404),
        "createPurchasePlan": (lambda: B("POST", "/portfolio/models/%s/purchase-plan" % idm, {"amount": "1000", "currency": "CAD", "prices": {symbole: "10"}}), 404),
        "createRebalancePlan": (lambda: B("POST", "/portfolio/rebalance-plans", {"modelId": idm, "accountIds": [ida], "currency": "CAD", "prices": {symbole: "10"}}), 404),
        "getExposure": (lambda: B("GET", "/portfolio/exposure?accounts=%s&modelId=%s" % (ida, idm)), 404),
        "getPerformance": (lambda: B("GET", "/portfolio/performance?accounts=%s&to=2025-12-31" % ida), 404),
    }
    for ident, m, p in sorted(ops):
        fn, want = cas[ident]
        s, _, b = retry(fn)
        d = jbody(b) or {}
        if want == "liste_sans_A":
            ok = s == 200 and not any(x.get("id") in (ida, ida2) for x in d.get("items", []))
            record(ok, "isolement.%s %s %s" % (ident, m, p), "HTTP %s ; %d compte(s) vus par B, aucun de A" % (s, len(d.get("items", []))))
        elif want == "etfs_sans_A":
            record(s == 200 and not any(x.get("symbol") == symbole for x in d.get("items", [])), "isolement.%s %s %s" % (ident, m, p), "HTTP %s ; le profil de A est absent du catalogue de B (%d FNB)" % (s, len(d.get("items", []))))
        elif want == "modeles_sans_A":
            record(s == 200 and not any(x.get("id") == idm for x in d.get("items", [])), "isolement.%s %s %s" % (ident, m, p), "HTTP %s ; le modèle de A est absent de la liste de B" % s)
        else:
            record(s == want, "isolement.%s %s %s" % (ident, m, p), "HTTP %s (attendu %s) pour B sur un objet de A" % (s, want))
    s1, _, b1 = retry(lambda: B("PUT", "/portfolio/models/" + idm, corps_b_valide))
    s2, _, b2 = retry(lambda: B("PUT", "/portfolio/models/" + str(uuid.uuid4()), corps_b_valide))
    record(s1 == s2 == 404 and jbody(b1).get("title") == jbody(b2).get("title") and jbody(b1).get("detail") == jbody(b2).get("detail"),
           "isolement.updateModel_modele_de_A_indiscernable_d_un_modele_inexistant", "HTTP %s et %s, titre et détail identiques (aucun oracle d'existence)" % (s1, s2))
    s1, _, b1 = retry(lambda: B("GET", "/accounts/%s/positions" % ida))
    s2, _, b2 = retry(lambda: B("GET", "/accounts/%s/positions" % uuid.uuid4()))
    record(s1 == s2 == 404 and jbody(b1).get("detail") == jbody(b2).get("detail"), "isolement.compte_de_A_indiscernable_d_un_compte_inexistant", "HTTP %s et %s, détail identique" % (s1, s2))
    # Cas particuliers : inverse d'une transaction de A depuis un compte de B ; consolidé de B ; plan de B avec compte de A.
    s, _, _ = retry(lambda: B("POST", "/accounts/%s/transactions" % idb, {"type": "buy", "tradeDate": "2025-01-15", "symbol": symbole, "quantity": montants[1], "price": montants[2], "currency": "CAD", "reversesId": tx["id"]}))
    record(s in (404, 400), "isolement.reversesId_d_un_autre_proprietaire", "écriture inverse visant une transaction de A depuis un compte de B : HTTP %s" % s)
    s, _, b = retry(lambda: B("GET", "/portfolio/performance?to=2025-12-31"))
    ids_vus = [x.get("accountId") for x in (jbody(b) or {}).get("accounts", [])]
    record(s == 200 and ida not in ids_vus and ida2 not in ids_vus, "isolement.performance_consolidee_de_B", "HTTP %s ; comptes vus par B : %d, aucun de A" % (s, len(ids_vus)))
    s, _, b = retry(lambda: B("GET", "/accounts/%s/positions" % ida))
    record(s == 404 and symbole not in b.decode("utf-8", "ignore") and montants[1] not in b.decode("utf-8", "ignore"), "isolement.aucune_fuite_dans_l_erreur", "corps de la réponse 404 sans symbole ni quantité de A")
    # A est intact et son journal n'a pas bougé.
    pos_apres = jbody(A("GET", "/accounts/%s/positions" % ida)[2])
    nb_apres = len(jbody(A("GET", "/accounts/%s/transactions" % ida)[2])["items"])
    mod_apres = jbody(A("GET", "/portfolio/models")[2])
    record(pos_apres == pos_a and nb_apres == nb_tx_a and mod_apres == mod_a, "isolement.A_intact",
           "positions, %d transaction(s) et modèles de A identiques après les tentatives de B" % nb_apres)
    record(int(psql("SELECT count(*) FROM core.audit_log WHERE owner_id='%s'" % sub_a)) == audit_a_avant, "isolement.audit_A_inchange", "aucune ligne d'audit créée pour A par les écritures de B")
    # Chaque requête de B est refusée AVANT la base ou filtrée par owner_id : aucune ligne de B ne référence un objet de A.
    croise = psql("SELECT (SELECT count(*) FROM core.transaction t JOIN core.account c ON c.id=t.account_id WHERE t.owner_id<>c.owner_id) + "
                  "(SELECT count(*) FROM etf.model_holding h JOIN etf.model m ON m.id=h.model_id WHERE h.owner_id<>m.owner_id)")
    record(croise == "0", "isolement.aucune_ligne_croisee", "lignes de la base dont le propriétaire diffère de celui de leur parent : %s" % croise)

    # --- 4. Registre en insertion seule -------------------------------------------------------------------
    pw = env["FINANCE_APP_PASSWORD"]
    for sql in ["UPDATE core.transaction SET note='x'", "DELETE FROM core.transaction", "TRUNCATE core.transaction",
                "UPDATE etf.model_version SET cash_target=0", "DELETE FROM etf.model_version", "UPDATE core.audit_log SET actor='x'", "DELETE FROM core.audit_log"]:
        r = psql(sql, user="finance_app", password=pw)
        record("permission denied" in r, "registre.finance_app." + sql.split()[0] + "_" + sql.split()[-1].split(".")[-1].split(" ")[0],
               "finance_app> %s => %s" % (sql, r.splitlines()[0] if r else "aucune sortie"))
    for sql in ["UPDATE core.transaction SET note='x'", "DELETE FROM core.transaction", "TRUNCATE core.transaction"]:
        r = psql("BEGIN; %s; ROLLBACK" % sql)
        err = [l for l in r.splitlines() if "ERROR" in l]
        record(bool(err), "registre.proprietaire_des_tables." + sql.split()[0], "propriétaire des tables> %s => %s" % (sql, err[0] if err else r))
    nb = psql("SELECT count(*) FROM core.transaction WHERE account_id='%s'" % ida)
    record(nb == str(nb_tx_a), "registre.lignes_intactes", "le registre du compte de test compte toujours %s écritures (attendu %s) après les tentatives" % (nb, nb_tx_a))
    ent = {"Authorization": "Bearer " + tok_a}
    for m in ("PUT", "PATCH", "DELETE"):
        s, _, _ = call(m, base + "/api/v1/accounts/%s/transactions/%s" % (ida, tx["id"]), token=tok_a, body={} if m != "DELETE" else None)
        record(s in (404, 405), "registre.api_%s_transaction" % m, "%s /accounts/{id}/transactions/{id} => HTTP %s (aucune route de modification ni de suppression)" % (m, s))
    s, _, b = A("POST", "/accounts/%s/transactions" % ida, {"type": "buy", "tradeDate": "2025-01-15", "symbol": symbole, "quantity": "1", "price": "1", "currency": "CAD", "reversesId": tx["id"]})
    record(s in (400, 409), "registre.inverse_doit_recopier_l_original", "inverse qui ne recopie pas l'original ou déjà inversée => HTTP %s" % s)

    # --- 5. Validation aux frontières -----------------------------------------------------------------------
    def probleme(ident, r, attendus):
        s, h, b = r
        txt = b.decode("utf-8", "ignore")
        h = {k.title(): v for k, v in h.items()}
        ok = s in attendus and "problem+json" in h.get("Content-Type", "") and not re.search(r"goroutine|\.go:\d+|panic|runtime error|pq:|SQLSTATE", txt)
        record(ok, "frontiere." + ident, "HTTP %s %s ; sans trace interne" % (s, h.get("Content-Type", "")))

    probleme("json_invalide", call("POST", base + "/api/v1/accounts", token=tok_a, raw=(b"{pas du json", "application/json")), (400,))
    probleme("nom_de_compte_trop_long", A("POST", "/accounts", {"name": "x" * 101, "type": "celi", "broker": "X", "currency": "CAD"}), (400,))
    probleme("type_de_compte_inconnu", A("POST", "/accounts", {"name": "Gate T " + tag, "type": "crypto", "broker": "X", "currency": "CAD"}), (400,))
    probleme("montant_negatif", A("POST", "/accounts/%s/transactions" % ida, {"type": "contribution", "tradeDate": "2025-05-01", "amount": "-5", "currency": "CAD"}), (400,))
    probleme("montant_hors_bornes", A("POST", "/accounts/%s/transactions" % ida, {"type": "contribution", "tradeDate": "2025-05-01", "amount": "1" + "0" * 20, "currency": "CAD"}), (400,))
    probleme("symbole_piege", A("POST", "/accounts/%s/transactions" % ida, {"type": "buy", "tradeDate": "2025-05-01", "symbol": "';DROP TABLE x;--", "quantity": "1", "price": "1", "currency": "CAD"}), (400,))
    probleme("devise_differente_du_compte", A("POST", "/accounts/%s/transactions" % ida, {"type": "contribution", "tradeDate": "2025-05-01", "amount": "5", "currency": "USD"}), (400,))
    probleme("cibles_hors_100_pour_cent", A("POST", "/portfolio/models", {"name": "Gate cibles " + tag, "targets": [{"assetClass": "equity_us", "target": 0.5}], "holdings": [{"symbol": symbole, "weight": 1}]}), (400,))
    probleme("identifiant_non_uuid", A("GET", "/accounts/pas-un-uuid/positions"), (400, 404))
    probleme("courtier_inconnu", csv_post(lambda m, p, raw: A(m, p.replace("generic", "inconnu"), raw=raw), ida, csv_a), (400,))
    def curl(chemin, contenu, type_contenu):
        # curl lit la réponse 413 même si le serveur coupe l'envoi avant la fin (urllib lève BrokenPipe).
        with tempfile.NamedTemporaryFile(delete=False) as f:
            f.write(contenu)
            nom = f.name
        try:
            r = subprocess.run(["curl", "-ks", "-D", "-", "-o", "-", "-X", "POST", "-H", "Authorization: Bearer " + tok_a, "-H", "Content-Type: " + type_contenu,
                                "--data-binary", "@" + nom, base + "/api/v1" + chemin], capture_output=True)
        finally:
            os.unlink(nom)
        tete, _, corps = r.stdout.partition(b"\r\n\r\n")
        lignes = tete.decode("utf-8", "ignore").splitlines()
        code = int(lignes[0].split()[1]) if lignes and lignes[0].startswith("HTTP") else 0
        return code, {l.split(":", 1)[0].title(): l.split(":", 1)[1].strip() for l in lignes[1:] if ":" in l}, corps

    gros = b"date,compte,type,montant\n" + ("2025-01-01,Gate A celi %s,contribution,1\n" % tag).encode() * (11 * 1024 * 1024 // 40)
    mp = ("--%s\r\nContent-Disposition: form-data; name=\"file\"; filename=\"t.csv\"\r\nContent-Type: text/csv\r\n\r\n" % bnd).encode() + gros + ("\r\n--%s--\r\n" % bnd).encode()
    probleme("csv_de_plus_de_10_Mo", curl("/accounts/%s/imports/csv?broker=generic" % ida, mp, "multipart/form-data; boundary=" + bnd), (413,))
    probleme("csv_vide", csv_post(A, ida, b""), (400,))
    probleme("corps_json_de_2_Mo", curl("/accounts", json.dumps({"name": "x", "type": "celi", "broker": "y" * (2 * 1024 * 1024), "currency": "CAD"}).encode(), "application/json"), (400, 413))

    # --- 6. En-têtes de sécurité (phase 1, rejoués sur des routes de phase 2) --------------------------------
    for chemin, tok in [("/", None), ("/api/v1/accounts", tok_a), ("/api/v1/portfolio/models", tok_a)]:
        s, h, _ = call("GET", base + chemin, token=tok)
        hl = {k.lower(): v for k, v in h.items()}
        ok = (re.search(r"max-age=\d{8,}", hl.get("strict-transport-security", "")) and hl.get("x-content-type-options") == "nosniff"
              and hl.get("referrer-policy") == "no-referrer" and "frame-ancestors 'none'" in hl.get("content-security-policy", ""))
        record(bool(ok), "entetes." + chemin, "HTTP %s ; HSTS, nosniff, Referrer-Policy et CSP (frame-ancestors 'none') présents" % s)
    s, h, _ = call("GET", base + "/api/v1/accounts", token=tok_a)
    cc = {k.lower(): v for k, v in h.items()}.get("cache-control")
    # Observation, pas un critère : aucun document du dépôt n'exige `Cache-Control: no-store` sur les réponses de l'API.
    print("OBSERVATION entetes.cache_control_api : Cache-Control de GET /accounts = %s (aucune exigence écrite ; à envisager pour des données financières)" % (cc or "absent"), flush=True)

    # --- 7. Limitation de débit sur des routes de phase 2 (en dernier : elle bloque les appels suivants) ------
    time.sleep(2)
    with concurrent.futures.ThreadPoolExecutor(max_workers=60) as ex:
        codes = list(ex.map(lambda _: call("GET", base + "/api/v1/accounts", token=tok_a)[0], range(400)))
    n429 = codes.count(429)
    record(n429 > 0 and set(codes) <= {200, 429}, "debit.rafale_phase2", "400 requêtes GET /api/v1/accounts en rafale : %d x 200, %d x 429, autres %s" % (codes.count(200), n429, sorted(set(codes) - {200, 429})))
    time.sleep(3)
    s, _, _ = A("GET", "/accounts")
    record(s == 200, "debit.reprise", "après la rafale et une pause : HTTP %s" % s)

    bad = [r for r in gs.RESULTS if not r[0]]
    print("TOTAL %d vérifications, %d échec(s)" % (len(gs.RESULTS), len(bad)))
    return 1 if bad else 0


if __name__ == "__main__":
    sys.exit(main())
