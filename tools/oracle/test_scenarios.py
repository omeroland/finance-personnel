"""Tests de la logique Python de l'oracle, sans LibreOffice : python3 -I -m unittest."""
import json
import unittest

import scenarios


class ScenariosTest(unittest.TestCase):
    def test_200_cas_ids_uniques(self):
        cases = scenarios.generate()
        self.assertEqual(len(cases), 200)
        self.assertEqual(len({c["id"] for c in cases}), 200)

    def test_deterministe(self):
        self.assertEqual(json.dumps(scenarios.generate()), json.dumps(scenarios.generate()))

    def test_series_de_20_valeurs(self):
        for c in scenarios.generate():
            self.assertEqual(len(c["eps"]), 20, c["id"])
            self.assertEqual(len(c["dividends"]), 20, c["id"])

    def test_cas_documente(self):
        c = next(c for c in scenarios.generate() if c["id"] == scenarios.DOCUMENTED_ID)
        self.assertEqual(c["eps"][15:], [1.0, 1.1, 1.21, 1.331, 1.4641])
        self.assertTrue(all(v is None for v in c["eps"][:15]))

    def test_scenarios_attendus(self):
        cases = scenarios.generate()
        self.assertTrue(any(0.0 in c["eps"][:19] for c in cases if c["scenario"] == "zeros"))
        self.assertTrue(any(None in c["eps"] for c in cases if c["scenario"] == "trous"))
        self.assertTrue(any(v < 0 for c in cases if c["scenario"] == "bpa_negatifs" for v in c["eps"]))
        self.assertTrue(all(sum(v is not None for v in c["eps"]) < 20
                            for c in cases if c["scenario"] == "historique_court"))
        self.assertTrue(all(v is None or v >= 0 for c in cases for v in c["dividends"]))


if __name__ == "__main__":
    unittest.main()
