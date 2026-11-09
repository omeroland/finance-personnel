"""Tests de la logique Python de l'oracle de l'outil SÉLECTION ET GESTION, sans LibreOffice.

python3 -I -m unittest discover -s tools/oracle
"""
import json
import unittest
from decimal import Decimal
from pathlib import Path

import etf_model as M
import etf_oracle as O
import etf_scenarios as S

CASES_JSON = Path(__file__).resolve().parents[2] / "testdata" / "oracle" / "etf_cases.json"


class ScenariosTest(unittest.TestCase):
    def setUp(self):
        self.cases = S.generate()

    def test_au_moins_30_cas_ids_uniques(self):
        self.assertGreaterEqual(len(self.cases), 30)
        self.assertEqual(len({c["id"] for c in self.cases}), len(self.cases))

    def test_deterministe(self):
        self.assertEqual(json.dumps(S.generate(), sort_keys=True), json.dumps(S.generate(), sort_keys=True))

    def test_les_9_ecarts_apparaissent(self):
        codes = {c["ecart"] for c in self.cases if c["ecart"]}
        self.assertEqual(codes, {code for code, _, _ in S.ECARTS})
        self.assertEqual(len(S.ECARTS), 9)

    def test_chaque_cas_est_etiquete(self):
        for c in self.cases:
            if c["etiquette"] == "fidele":
                self.assertIsNone(c["ecart"], c["id"])
                self.assertTrue(c["egal"], c["id"])
                self.assertFalse(c["different"], c["id"])
            else:
                self.assertEqual(c["etiquette"], "ecart", c["id"])
                self.assertIn(c["ecart"], {code for code, _, _ in S.ECARTS}, c["id"])
                self.assertTrue(c["different"], c["id"])

    def test_les_trois_onglets_demandes_sont_couverts(self):
        for onglet in ("ACHATS", "RÉÉQUILIBRAGE", "RENDEMENT"):
            self.assertGreaterEqual(sum(c["onglet"] == onglet for c in self.cases), 5, onglet)

    def test_catalogue_synthetique(self):
        self.assertTrue(all(f["symbole"].startswith("SYN-") for f in S.CATALOGUE))
        for c in self.cases:
            for f in c["entrees"].get("fonds", []):
                self.assertIn(f["symbole"], S.PROFILS, c["id"])

    def test_cas_fideles_de_reequilibrage_loin_des_limites_de_bande(self):
        # Un statut collé à une limite dépendrait du bruit de virgule flottante de LibreOffice.
        for c in self.cases:
            if c["kind"] != "reequilibrage":
                continue
            r = M.reequilibrage(c["entrees"], S.PROFILS)
            for f in r["fonds"]:
                if f["cible"]:
                    self.assertGreater(M.marge_bande(M.D(f["pct"]), M.D(f["cible"]), True), Decimal("0.001"), c["id"])
            for k in ("actions", "obligations"):
                self.assertGreater(M.marge_bande(M.D(r[k]["pct"]), M.D(r[k]["cible"]), False), Decimal("0.001"), c["id"])

    def test_cibles_de_fonds_a_100_pour_cent(self):
        for c in self.cases:
            if c["kind"] == "reequilibrage":
                self.assertAlmostEqual(sum(f["pct_cible"] for f in c["entrees"]["fonds"]), 1.0, 9, c["id"])

    def test_aucune_donnee_personnelle(self):
        blob = json.dumps(self.cases, ensure_ascii=False).lower()
        for mot in ("@", "http", "iban", "nas "):
            self.assertNotIn(mot, blob)


class ModeleTest(unittest.TestCase):
    def test_achats_parts_entieres_et_reste(self):
        r = M.achats(10000, [{"pct": 0.6, "prix": 33.47}, {"pct": 0.4, "prix": 27.19}])
        self.assertEqual(r["parts"], [179, 147])
        self.assertEqual(r["montants"], [6000, 4000])
        self.assertEqual(r["depense_totale"], 9988.06)
        self.assertEqual(r["encaisse_restante"], 11.94)
        self.assertEqual(r["prix_moyen"], 30.958)
        self.assertEqual(r["parts_total"], 326)

    def test_achats_part_trop_chere(self):
        r = M.achats(500, [{"pct": 0.5, "prix": 150.0}, {"pct": 0.5, "prix": 300.0}])
        self.assertEqual(r["parts"], [1, 0])
        self.assertEqual(r["encaisse_restante"], 350)

    def test_parts_fractionnaires_de_l_outil(self):
        self.assertAlmostEqual(M.parts_fractionnaires(10000, [{"pct": 0.6, "prix": 33.47}])[0], 179.265013444876, 9)

    def test_statuts_de_bande(self):
        self.assertEqual(M.statut_fonds(Decimal("0.5"), Decimal("0.4")), M.A_EQUILIBRER)
        self.assertEqual(M.statut_fonds(Decimal("0.1"), Decimal("0.2")), M.A_EQUILIBRER)
        self.assertEqual(M.statut_fonds(Decimal("0.45"), Decimal("0.4")), M.OK)
        self.assertEqual(M.statut_points(Decimal("0.56"), Decimal("0.5")), M.A_EQUILIBRER)
        self.assertEqual(M.statut_points(Decimal("0.54"), Decimal("0.5")), M.OK)

    def test_reequilibrage_valeur_de_marche_et_autres_actifs(self):
        r = M.reequilibrage({"cible_encaisse": 0, "encaisse": 0, "fonds": [
            {"symbole": "SYN-US", "pct_cible": 0.4, "valeur": 4000, "prix": None},
            {"symbole": "SYN-OBL", "pct_cible": 0.3, "valeur": 3000, "prix": None},
            {"symbole": "SYN-OR", "pct_cible": 0.3, "valeur": 3000, "prix": None}]}, S.PROFILS)
        self.assertEqual(r["total"], 10000)
        self.assertEqual([f["statut"] for f in r["fonds"]], [M.OK] * 3)
        self.assertEqual(r["autres_actifs"]["pct"], 0.3)

    def test_reequilibrage_cible_d_encaisse_normalise(self):
        r = M.reequilibrage({"cible_encaisse": 0.25, "encaisse": 2500, "fonds": [
            {"symbole": "SYN-CROIS", "pct_cible": 0.5, "valeur": 3750, "prix": None},
            {"symbole": "SYN-OBL", "pct_cible": 0.5, "valeur": 3750, "prix": None}]}, S.PROFILS)
        self.assertEqual(r["cible_totale"], 1)
        self.assertEqual([f["cible"] for f in r["fonds"]], [0.375, 0.375])
        self.assertEqual(r["encaisse"]["statut"], M.OK)

    def test_parts_a_bouger_arrondies_vers_zero(self):
        r = M.reequilibrage({"cible_encaisse": 0, "encaisse": 0, "fonds": [
            {"symbole": "SYN-US", "pct_cible": 0.5, "valeur": 6000, "prix": 70.0},
            {"symbole": "SYN-OBL", "pct_cible": 0.5, "valeur": 4000, "prix": 30.0}]}, S.PROFILS)
        self.assertEqual(r["parts"], [-14, 33])  # vente de 1000 $ = 14.28 parts ; achat de 1000 $ = 33.33 parts

    def test_exposition_et_obligations_autres(self):
        expo = M.exposition([{"symbole": "SYN-US", "pct": 0.4}, {"symbole": "SYN-GLB", "pct": 0.6}], S.PROFILS)
        self.assertEqual((expo["obl_cad"], expo["obl_autres"]), (0.18, 0.42))
        self.assertEqual(M.ecart_obligations(expo, {"cad": 0.3, "autres": 0.3}), {"cad": -0.12, "autres": 0.12})

    def test_diversification_vide_sans_erreur(self):
        v = M.diversification([], S.PROFILS)
        self.assertEqual(v["actions_pct"], [None] * 3)
        sans_actions = M.diversification([{"symbole": "SYN-OBL", "pct": 1.0}], S.PROFILS)
        self.assertEqual(sans_actions["actions_pct"], [None] * 3)
        self.assertEqual(sans_actions["obligations_pct"], [1, 0])

    def test_parse_du_catalogue(self):
        self.assertEqual(M.parse_pct("19,6%"), 0.196)
        self.assertEqual(M.parse_pct("-2,4%"), -0.024)
        self.assertIsNone(M.parse_pct("-"))
        self.assertEqual(M.parse_taille("880M$"), 880_000_000)
        self.assertEqual(M.parse_taille("1,2G$"), 1_200_000_000)
        self.assertIsNone(M.parse_taille("-"))

    def test_rendement_mensuel_cotisation_en_fin_de_mois(self):
        self.assertEqual(M.rendement_mensuel(20000, [500, 500], [20700, 21450]), [0.01, 0.012077294686])

    def test_irr_et_aacj(self):
        self.assertAlmostEqual(M.irr([-100.0, 110.0]), 0.10, 9)
        self.assertIsNone(M.irr([-100.0, -10.0]))
        # Un seul mois : AACJ du mois 1 = rendement du mois.
        self.assertAlmostEqual(M.aacj_mois(10000, [0], [10120])[0], 0.012, 9)

    def test_annualise_moyenne_geometrique(self):
        self.assertAlmostEqual(M.annualise([0.08, -0.05, 0.12]), (1.08 * 0.95 * 1.12) ** (1 / 3) - 1, 9)
        self.assertIsNone(M.annualise([]))

    def test_consolide_somme_les_comptes(self):
        a = {"depart": 1000, "ajouts": [0, 0], "fins": [1100, 1210]}
        b = {"depart": 3000, "ajouts": [0, 0], "fins": [3000, 3000]}
        c = M.consolide([a, b])
        self.assertEqual(c["rendement_mensuel"], [0.025, 0.026829268293])
        self.assertEqual(c["rendement_periode"], 0.0525)

    def test_proche(self):
        self.assertTrue(M.proche([1, {"a": 0.1 + 0.2}], [1, {"a": 0.3}]))
        self.assertFalse(M.proche("#VALUE!", None))
        self.assertFalse(M.proche(True, 1))


class ControleTest(unittest.TestCase):
    def _cas(self, **kw):
        base = {"id": "x", "etiquette": "fidele", "ecart": None, "outil": {"a": 1}, "attendu_app": {"a": 1},
                "comparaison": {"egal": ["a"], "different": []}, "verifications": []}
        base.update(kw)
        return base

    def test_cas_coherent(self):
        self.assertEqual(O.verifier(self._cas()), [])

    def test_fidele_qui_diverge_est_refuse(self):
        self.assertTrue(O.verifier(self._cas(attendu_app={"a": 2})))

    def test_ecart_qui_ne_diverge_pas_est_refuse(self):
        cas = self._cas(etiquette="ecart", ecart="catalogue_fige", comparaison={"egal": [], "different": ["a"]})
        self.assertTrue(O.verifier(cas))

    def test_verification_outil_corrige(self):
        cas = self._cas(etiquette="ecart", ecart="catalogue_fige", outil={"a": 1, "b": 5}, attendu_app={"a": 2, "b": 6},
                        comparaison={"egal": [], "different": ["a"]}, verifications=[["outil.b", "attendu_app.b"]])
        self.assertTrue(O.verifier(cas))

    def test_norm(self):
        self.assertIsNone(O.norm(""))
        self.assertEqual(O.norm(5.0), 5)
        self.assertEqual(O.norm("#DIV/0!"), "#DIV/0!")
        self.assertIs(O.norm(True), True)


@unittest.skipUnless(CASES_JSON.exists(), "etf_cases.json absent")
class FichierProduitTest(unittest.TestCase):
    """Le fichier versionné tient ses promesses (régénéré par `make oracle-etf`)."""

    def setUp(self):
        self.doc = json.loads(CASES_JSON.read_text(encoding="utf-8"))

    def test_au_moins_30_cas_et_9_ecarts(self):
        cas = self.doc["cas"]
        self.assertGreaterEqual(len(cas), 30)
        self.assertEqual({c["ecart"] for c in cas if c["ecart"]}, {e["code"] for e in self.doc["ecarts"]})
        self.assertEqual(len(self.doc["ecarts"]), 9)

    def test_chaque_cas_etiquete_et_coherent(self):
        for c in self.doc["cas"]:
            self.assertIn(c["etiquette"], ("fidele", "ecart"), c["id"])
            self.assertEqual(O.verifier(c), [], c["id"])

    def test_les_ecarts_portent_outil_et_app(self):
        for c in self.doc["cas"]:
            if c["etiquette"] == "ecart":
                self.assertIn("outil", c)
                self.assertIn("attendu_app", c)
                self.assertTrue(c["comparaison"]["different"], c["id"])

    def test_meme_catalogue_que_le_generateur(self):
        ids = [c["id"] for c in S.generate()]
        self.assertEqual([c["id"] for c in self.doc["cas"]], ids)
        self.assertEqual(self.doc["graine"], S.SEED)


if __name__ == "__main__":
    unittest.main()
