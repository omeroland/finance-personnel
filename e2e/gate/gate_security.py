#!/usr/bin/env python3
"""P1-24 : revue de sécurité rejouée sur la pile réelle (realm de TEST, aucune donnée réelle).

Pile démarrée par `COMPOSE_PROJECT_NAME=financegate make up-test`, avec l'accès d'administration local
(`-f deploy/compose.admin.yaml`, voir deploy/README.md) depuis P1-13-fix. Le script :
  1. crée par l'API d'administration Keycloak un second utilisateur, un client sans mapper d'audience
     et un second realm (tout est détruit avec `down -v` du projet financegate) ;
  2. rejoue les cas de P1-13 (sans jeton, autre realm, expiré, HS256 forgé, mauvaise audience, alg none) ;
  3. vérifie l'isolement par owner_id sur CHAQUE route de api/openapi.yaml avec deux utilisateurs ;
  4. vérifie que chaque écriture ajoute une ligne à core.audit_log ;
  5. envoie une rafale pour vérifier la limitation de débit de Traefik (D34).

Usage : e2e/gate/gate_security.py [--base https://localhost:9443] [--project financegate]
Aucun secret n'est affiché : le mot de passe administrateur vient de deploy/.env (hors git).
Sortie : une ligne `RESULT <ok|ECHEC> <identifiant> : <détail>` par vérification.
"""
import argparse
import base64
import concurrent.futures
import hashlib
import hmac
import json
import pathlib
import ssl
import subprocess
import sys
import time
import urllib.error
import urllib.parse
import urllib.request
import uuid

CTX = ssl.create_default_context()
CTX.check_hostname = False
CTX.verify_mode = ssl.CERT_NONE
FAUX_MDP = "FAUX-mot-de-passe-de-test"  # mot de passe factice du realm de test
RESULTS = []


def b64(b):
    return base64.urlsafe_b64encode(b).rstrip(b"=").decode()


def unb64(s):
    return base64.urlsafe_b64decode(s + "=" * (-len(s) % 4))


def call(method, url, token=None, body=None, form=None, headers=None, raw=None):
    h = dict(headers or {})
    data = None
    if token:
        h["Authorization"] = "Bearer " + token
    if body is not None:
        data = json.dumps(body).encode()
        h["Content-Type"] = "application/json"
    if form is not None:
        data = urllib.parse.urlencode(form).encode()
        h["Content-Type"] = "application/x-www-form-urlencoded"
    if raw is not None:
        data, h["Content-Type"] = raw
    req = urllib.request.Request(url, data=data, method=method, headers=h)
    try:
        with urllib.request.urlopen(req, context=CTX, timeout=30) as r:
            return r.status, dict(r.headers), r.read()
    except urllib.error.HTTPError as e:
        return e.code, dict(e.headers), e.read()


def record(ok, ident, detail=""):
    RESULTS.append((ok, ident, detail))
    print("RESULT %s %s : %s" % ("ok" if ok else "ECHEC", ident, detail), flush=True)


def jbody(b):
    try:
        return json.loads(b)
    except Exception:
        return None


class Kc:
    def __init__(self, base, admin_pw, admin_base):
        # Depuis P1-13-fix (D280), Traefik refuse le realm master et /auth/admin : l'administration passe par
        # l'accès local de deploy/compose.admin.yaml (admin_base, 127.0.0.1:8180). Les jetons du realm finance
        # (ceux que l'API valide) restent demandés par Traefik (base).
        self.base = base
        self.admin_base = admin_base
        r = call("POST", admin_base + "/auth/realms/master/protocol/openid-connect/token",
                 form={"grant_type": "password", "client_id": "admin-cli", "username": "admin", "password": admin_pw})
        self.admin_token = jbody(r[2])["access_token"]

    def admin(self, method, path, body=None):
        return call(method, self.admin_base + "/auth/admin" + path, token=self.admin_token, body=body)

    def token(self, realm, client, user, pw, direct=False):
        s, _, b = call("POST", "%s/auth/realms/%s/protocol/openid-connect/token" % (self.admin_base if direct else self.base, realm),
                       form={"grant_type": "password", "client_id": client, "username": user, "password": pw})
        if s != 200:
            raise RuntimeError("jeton %s/%s : HTTP %s" % (realm, user, s))
        return jbody(b)["access_token"]

    def create_user(self, realm, name):
        s, _, b = self.admin("POST", "/realms/%s/users" % realm, {
            "username": name, "enabled": True, "emailVerified": True, "email": name + "@example.invalid",
            "firstName": "Gate", "lastName": "Test",
            "credentials": [{"type": "password", "value": FAUX_MDP, "temporary": False}]})
        assert s in (201, 409), (s, b)


def main():
    ap = argparse.ArgumentParser()
    ap.add_argument("--base", default="https://localhost:9443")
    ap.add_argument("--project", default="financegate")
    ap.add_argument("--admin-base", default="http://127.0.0.1:8180", help="accès d'administration local (compose.admin.yaml)")
    ap.add_argument("--env", default="deploy/.env")
    ap.add_argument("--xlsx", default="testdata/xlsx/simply_investing_template.xlsx")
    ap.add_argument("--expiry-wait", type=int, default=330)
    a = ap.parse_args()
    base = a.base
    env = dict(l.strip().split("=", 1) for l in pathlib.Path(a.env).read_text().splitlines() if "=" in l)
    kc = Kc(base, env["KEYCLOAK_ADMIN_PASSWORD"], a.admin_base)

    # --- Comptes de test : A = test-user (realm importé), B = créé ici ---------------------------------
    kc.create_user("finance", "gate-user-b")
    tok_a = kc.token("finance", "finance-test", "test-user", FAUX_MDP)
    tok_b = kc.token("finance", "finance-test", "gate-user-b", FAUX_MDP)
    claims = lambda t: json.loads(unb64(t.split(".")[1]))
    sub_a, sub_b = claims(tok_a)["sub"], claims(tok_b)["sub"]
    record(sub_a != sub_b, "comptes", "deux sub distincts dans le realm finance")
    expiring = tok_a  # sera rejoué après son expiration
    t_expiring = time.time()

    # --- Jetons invalides -----------------------------------------------------------------------------
    # Autre realm : second realm « gateother », même client et même nom d'utilisateur.
    kc.admin("POST", "/realms", {"realm": "gateother", "enabled": True})
    kc.admin("POST", "/realms/gateother/clients", {"clientId": "other-cli", "publicClient": True,
                                                    "directAccessGrantsEnabled": True})
    kc.create_user("gateother", "intrus")
    tok_other = kc.token("gateother", "other-cli", "intrus", FAUX_MDP, direct=True)  # Traefik ne route que le realm finance
    # Mauvaise audience : même realm, signature valide, client sans mapper « finance-api ».
    s, h, _ = kc.admin("POST", "/realms/finance/clients", {"clientId": "gate-noaud", "publicClient": True,
                                                            "directAccessGrantsEnabled": True,
                                                            "fullScopeAllowed": False})
    tok_noaud = kc.token("finance", "gate-noaud", "test-user", FAUX_MDP)
    aud_noaud = claims(tok_noaud).get("aud")
    # HS256 forgé : la clé publique (JWKS) sert de secret HMAC.
    jwks = jbody(call("GET", base + "/auth/realms/finance/protocol/openid-connect/certs")[2])
    pub_material = json.dumps(jwks["keys"][0]).encode()
    c = claims(tok_a)
    now = int(time.time())
    payload = {"iss": c["iss"], "aud": "finance-api", "sub": c["sub"], "exp": now + 300, "iat": now}
    hdr = b64(json.dumps({"alg": "HS256", "typ": "JWT"}).encode())
    pl = b64(json.dumps(payload).encode())
    sig = b64(hmac.new(pub_material, (hdr + "." + pl).encode(), hashlib.sha256).digest())
    tok_hs256 = ".".join([hdr, pl, sig])
    # alg none
    tok_none = ".".join([b64(json.dumps({"alg": "none", "typ": "JWT"}).encode()), pl, ""])
    # RS256 signée par une clé inconnue : signature altérée d'un vrai jeton.
    p0, p1, p2 = tok_a.split(".")
    tok_tamper = ".".join([p0, b64(json.dumps({**c, "sub": str(uuid.uuid4())}).encode()), p2])

    def code(token, path="/api/v1/stocks"):
        s, h, b = call("GET", base + path, token=token)
        return s, h.get("Content-Type", ""), b

    s, ct, b = code(None)
    record(s == 401 and "problem+json" in ct, "jwt.sans_jeton", "HTTP %s %s" % (s, ct))
    for ident, t in [("autre_realm", tok_other), ("hs256_forge", tok_hs256), ("alg_none", tok_none),
                     ("signature_alteree", tok_tamper), ("mauvaise_audience(aud=%s)" % aud_noaud, tok_noaud),
                     ("jeton_poubelle", "abc.def.ghi")]:
        s, ct, b = code(t)
        record(s == 401, "jwt." + ident, "HTTP %s %s" % (s, ct))
    s, _, _ = code(tok_a)
    record(s == 200, "jwt.jeton_valide", "HTTP %s" % s)
    for p in ("/healthz", "/readyz"):
        s, _, _ = code(None, p)
        record(s == 200, "jwt.exception" + p, "HTTP %s sans jeton" % s)
    # Toutes les routes du contrat sans jeton : 401.
    routes = [("GET", "/api/v1/stocks"), ("POST", "/api/v1/stocks"), ("GET", "/api/v1/stocks/TSX:X"),
              ("PATCH", "/api/v1/stocks/TSX:X"), ("DELETE", "/api/v1/stocks/TSX:X"),
              ("PUT", "/api/v1/stocks/TSX:X/facts"), ("PUT", "/api/v1/stocks/TSX:X/qualitative"),
              ("GET", "/api/v1/stocks/TSX:X/evaluation"), ("GET", "/api/v1/stocks/TSX:X/evaluations"),
              ("POST", "/api/v1/stocks/TSX:X/evaluations"), ("POST", "/api/v1/stocks/TSX:X/refresh"),
              ("POST", "/api/v1/imports/xlsx"), ("GET", "/api/v1/imports/" + str(uuid.uuid4())),
              ("GET", "/api/v1/rules")]
    bad = []
    for m, p in routes:
        s, _, _ = call(m, base + p.replace(":", "%3A"), body={} if m in ("POST", "PUT", "PATCH") else None)
        if s != 401:
            bad.append("%s %s=%s" % (m, p, s))
    record(not bad, "jwt.14_routes_sans_jeton", "14 routes -> 401" if not bad else "; ".join(bad))

    # --- Isolement owner_id : données de A, puis B essaie chaque route ----------------------------------
    sym = "TSX:GATEA"
    q = urllib.parse.quote(sym, safe="")
    st = "/api/v1/stocks/" + q
    ctj = {}

    def A(m, p, body=None, **kw):
        return call(m, base + p, token=tok_a, body=body, **kw)

    def B(m, p, body=None, **kw):
        return call(m, base + p, token=tok_b, body=body, **kw)

    def audit_count(owner):
        out = subprocess.run(["docker", "exec", a.project + "-db-1", "psql", "-U", "finance", "-d", "finance", "-Atc",
                              "SELECT count(*) FROM core.audit_log WHERE owner_id='%s'" % owner],
                             capture_output=True, text=True, check=True).stdout.strip()
        return int(out)

    def audit_rows(owner, since_id):
        out = subprocess.run(["docker", "exec", a.project + "-db-1", "psql", "-U", "finance", "-d", "finance", "-Atc",
                              "SELECT id, actor=owner_id::text, action, entity FROM core.audit_log WHERE owner_id='%s' AND id>%d ORDER BY id" % (owner, since_id)],
                             capture_output=True, text=True, check=True).stdout.strip()
        return out.splitlines()

    def max_audit():
        return int(subprocess.run(["docker", "exec", a.project + "-db-1", "psql", "-U", "finance", "-d", "finance", "-Atc",
                                   "SELECT coalesce(max(id),0) FROM core.audit_log"], capture_output=True, text=True, check=True).stdout.strip())

    def write_audited(ident, fn, want):
        before = max_audit()
        s, _, b = fn()
        rows = audit_rows(sub_a, before)
        record(s == want and len(rows) >= 1, "audit." + ident,
               "HTTP %s ; %d ligne(s) ajoutée(s) à core.audit_log : %s" % (s, len(rows), "; ".join(r.split("|", 1)[1] if "|" in r else r for r in rows)))
        return s, b

    write_audited("addStock POST /stocks", lambda: A("POST", "/api/v1/stocks", {
        "symbol": sym, "name": "Action de verification", "quoteCurrency": "CAD", "reportingCurrency": "CAD"}), 201)
    write_audited("updateStock PATCH /stocks/{symbol}", lambda: A("PATCH", st, {"snapshot": {
        "currency": "CAD", "price": "100.00", "annualDividend": "2.00", "longTermDebt": "100.000000",
        "equity": "500.000000", "bookValuePerShare": "25.00", "fxRate": 1.0, "asOf": "2026-10-30", "source": "manual"}}), 200)
    write_audited("replaceFacts PUT /facts", lambda: A("PUT", st + "/facts", {"facts": [
        {"fiscalYear": 2025, "metric": "diluted_eps", "value": "7.55", "currency": "CAD", "source": "manual"},
        {"fiscalYear": 2025, "metric": "diluted_shares", "value": "640000000", "currency": "CAD", "source": "manual"}]}), 200)
    write_audited("replaceQualitative PUT /qualitative", lambda: A("PUT", st + "/qualitative", {
        "rule1": {"answer": "yes"}, "rule2": {"answer": "yes"}, "rule3": {"answer": "yes"}, "rule4": {"answer": "yes"},
        "manualBuyback": True, "reviewedAt": "2026-10-30T12:05:00Z"}), 200)
    s, b = write_audited("createEvaluation POST /evaluations", lambda: A("POST", st + "/evaluations", {"mode": "corrected"}), 201)
    ev = jbody(b) or {}
    # import xlsx (A)
    bnd = "----gate" + uuid.uuid4().hex
    xl = pathlib.Path(a.xlsx).read_bytes()
    mp = ("--%s\r\nContent-Disposition: form-data; name=\"year1\"\r\n\r\n2025\r\n--%s\r\nContent-Disposition: form-data; name=\"file\"; filename=\"t.xlsx\"\r\n"
          "Content-Type: application/vnd.openxmlformats-officedocument.spreadsheetml.sheet\r\n\r\n" % (bnd, bnd)).encode() + xl + ("\r\n--%s--\r\n" % bnd).encode()
    s, b = write_audited("importXlsx POST /imports/xlsx", lambda: A("POST", "/api/v1/imports/xlsx", raw=(mp, "multipart/form-data; boundary=" + bnd)), 202)
    job = (jbody(b) or {}).get("id")
    time.sleep(3)  # le job se termine en arrière-plan (FinishImportJob audité)

    # B essaie chaque route sur les objets de A
    cases = [
        ("listStocks GET /stocks", lambda: B("GET", "/api/v1/stocks"), "liste_sans_A"),
        ("getStock GET /stocks/{symbol}", lambda: B("GET", st), 404),
        ("updateStock PATCH /stocks/{symbol}", lambda: B("PATCH", st, {"snapshot": {"currency": "CAD", "price": "1.00", "asOf": "2026-10-30", "source": "manual"}}), 404),
        ("replaceFacts PUT /facts", lambda: B("PUT", st + "/facts", {"facts": [{"fiscalYear": 2025, "metric": "diluted_eps", "value": "1.00", "currency": "CAD", "source": "manual"}]}), 404),
        ("replaceQualitative PUT /qualitative", lambda: B("PUT", st + "/qualitative", {"rule1": {"answer": "no"}, "rule2": {"answer": "no"}, "rule3": {"answer": "no"}, "rule4": {"answer": "no"}, "manualBuyback": False, "reviewedAt": "2026-10-30T12:05:00Z"}), 404),
        ("getEvaluation GET /evaluation", lambda: B("GET", st + "/evaluation?mode=corrected"), 404),
        ("listEvaluations GET /evaluations", lambda: B("GET", st + "/evaluations"), 404),
        ("createEvaluation POST /evaluations", lambda: B("POST", st + "/evaluations", {"mode": "corrected"}), 404),
        ("refreshStock POST /refresh", lambda: B("POST", st + "/refresh"), (404, 501)),
        ("getImport GET /imports/{id}", lambda: B("GET", "/api/v1/imports/" + str(job)), 404),
        ("deleteStock DELETE /stocks/{symbol}", lambda: B("DELETE", st), 404),
    ]
    audit_b_before = audit_count(sub_b)
    for ident, fn, want in cases:
        s, _, b = fn()
        if want == "liste_sans_A":
            items = (jbody(b) or {}).get("items", [])
            record(s == 200 and not any(i.get("stock", i).get("symbol") == sym for i in items), "isolement." + ident,
                   "HTTP %s ; %d élément(s) vus par B, TSX:GATEA absent" % (s, len(items)))
        else:
            okc = s in want if isinstance(want, tuple) else s == want
            record(okc, "isolement." + ident, "HTTP %s (B sur un objet de A)" % s)
    s, _, b = B("GET", "/api/v1/rules")
    record(s == 200, "isolement.getRules GET /rules", "HTTP %s ; référentiel global sans donnée d'utilisateur (n/a)" % s)
    record(audit_count(sub_b) == audit_b_before, "isolement.audit_B_inchange", "aucune ligne d'audit créée pour B par ses écritures refusées")
    # A est intact : valeurs, jugement, historique.
    s, _, b = A("GET", st)
    d = jbody(b) or {}
    eps = [f for f in d.get("facts", []) if f.get("metric") == "diluted_eps" and f.get("fiscalYear") == 2025]
    record(s == 200 and eps and eps[0]["value"] == "7.55", "isolement.A_intact_facts", "diluted_eps 2025 de A = %s après les tentatives de B" % (eps[0]["value"] if eps else "absent"))
    s, _, b = A("GET", st + "/evaluations")
    record(s == 200 and len((jbody(b) or {}).get("items", jbody(b) or [])) >= 1, "isolement.A_intact_historique", "historique de A présent")
    # B ne peut pas non plus créer un doublon qui écrase : même symbole chez B = objet distinct.
    s, _, b = B("POST", "/api/v1/stocks", {"symbol": sym, "name": "Homonyme de B", "quoteCurrency": "CAD", "reportingCurrency": "CAD"})
    record(s == 201, "isolement.homonyme", "B crée son propre %s (UNIQUE(owner_id,symbol), D40) : HTTP %s" % (sym, s))
    s, _, b = A("GET", st)
    record((jbody(b) or {}).get("stock", {}).get("name") == "Action de verification", "isolement.homonyme_A_intact", "la fiche de A n'est pas modifiée par l'homonyme de B")
    B("DELETE", st)
    # Audit de la suppression (A)
    write_audited("deleteStock DELETE /stocks/{symbol}", lambda: A("DELETE", st), 204)
    # Import terminé : le job de A a-t-il écrit son audit de fin ? (ligne compte)
    jr = A("GET", "/api/v1/imports/" + str(job))
    record(jr[0] == 200, "isolement.A_lit_son_job", "A relit son job d'import : HTTP %s" % jr[0])

    # --- Limitation de débit ---------------------------------------------------------------------------
    def hit(_):
        return call("GET", base + "/api/v1/rules", token=tok_a)[0]
    time.sleep(2)
    with concurrent.futures.ThreadPoolExecutor(max_workers=60) as ex:
        codes = list(ex.map(hit, range(400)))
    n429 = codes.count(429)
    record(n429 > 0, "debit.rafale_api", "400 requêtes GET /api/v1/rules en rafale : %d x 200, %d x 429, autres %s" % (codes.count(200), n429, sorted(set(codes) - {200, 429})))
    time.sleep(3)
    s, _, _ = A("GET", "/api/v1/rules")
    record(s == 200, "debit.reprise", "après la rafale et une pause : HTTP %s" % s)
    with concurrent.futures.ThreadPoolExecutor(max_workers=60) as ex:
        kcodes = list(ex.map(lambda _: call("GET", base + "/auth/realms/finance/.well-known/openid-configuration")[0], range(400)))
    record(kcodes.count(429) > 0 and set(kcodes) <= {200, 429}, "debit.rafale_auth", "400 requêtes sur /auth/realms/finance (D281, auth-ratelimit) : %d x 200, %d x 429, autres %s" % (kcodes.count(200), kcodes.count(429), sorted(set(kcodes) - {200, 429})))
    with concurrent.futures.ThreadPoolExecutor(max_workers=60) as ex:
        wcodes = list(ex.map(lambda _: call("GET", base + "/")[0], range(400)))
    record(wcodes.count(429) == 0, "debit.web_non_limite", "400 requêtes sur / : %d x 429 (observation)" % wcodes.count(429))

    # --- Jeton expiré : rejoué quand le jeton de départ a dépassé exp ------------------------------------
    exp = claims(expiring)["exp"]
    wait = max(0, exp + 30 - time.time())
    print("attente de l'expiration du jeton : %.0f s" % wait, flush=True)
    time.sleep(wait)
    s, ct, _ = code(expiring)
    record(s == 401 and "problem+json" in ct, "jwt.expire", "jeton valide à t0, rejoué %.0f s après exp : HTTP %s" % (time.time() - exp, s))

    bad = [r for r in RESULTS if not r[0]]
    print("TOTAL %d vérifications, %d échec(s)" % (len(RESULTS), len(bad)))
    return 1 if bad else 0


if __name__ == "__main__":
    sys.exit(main())
