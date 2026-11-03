package engine

import (
	"strings"
	"testing"
)

func mustDefault(t *testing.T) *Rules {
	t.Helper()
	r, err := DefaultRules()
	if err != nil {
		t.Fatalf("DefaultRules: %v", err)
	}
	return r
}

func ptr(v float64) *float64 { return &v }

func eq(a, b *float64) bool { return equalPtr(a, b) }

func TestRulesValeursDuTableau(t *testing.T) {
	t.Parallel()
	r := mustDefault(t)

	type bounds struct{ gt, ge, le *float64 }
	cases := []struct {
		id      string
		strict  bounds
		relaxed *bounds // nil : aucun seuil assoupli
	}{
		{"5a", bounds{ge: ptr(0.08)}, &bounds{ge: ptr(0.06)}},
		{"6", bounds{ge: ptr(0.08)}, &bounds{ge: ptr(0.06)}},
		{"7", bounds{gt: ptr(0), le: ptr(0.75)}, &bounds{gt: ptr(0), le: ptr(0.85)}},
		{"8", bounds{ge: ptr(0), le: ptr(0.70)}, &bounds{ge: ptr(0), le: ptr(0.95)}},
		{"9", bounds{ge: ptr(0)}, nil},
		{"11a", bounds{gt: ptr(0), le: ptr(25)}, &bounds{gt: ptr(0), le: ptr(35)}},
		{"11c", bounds{gt: ptr(0), le: ptr(3)}, &bounds{gt: ptr(0), le: ptr(6)}},
	}
	for _, tc := range cases {
		t.Run(tc.id, func(t *testing.T) {
			t.Parallel()
			c, ok := r.Criterion(tc.id)
			if !ok {
				t.Fatal("critère absent")
			}
			got := bounds{c.Strict.Gt, c.Strict.Ge, c.Strict.Le}
			if !eq(got.gt, tc.strict.gt) || !eq(got.ge, tc.strict.ge) || !eq(got.le, tc.strict.le) {
				t.Errorf("strict: %+v", c.Strict)
			}
			if tc.relaxed == nil {
				if c.Relaxed != nil {
					t.Errorf("assoupli attendu absent: %+v", c.Relaxed)
				}
				return
			}
			if c.Relaxed == nil {
				t.Fatal("assoupli absent")
			}
			x := c.Relaxed
			if !eq(x.Gt, tc.relaxed.gt) || !eq(x.Ge, tc.relaxed.ge) || !eq(x.Le, tc.relaxed.le) {
				t.Errorf("assoupli: %+v", *x)
			}
		})
	}

	t.Run("5b", func(t *testing.T) {
		t.Parallel()
		c, _ := r.Criterion("5b")
		for h, want := range map[int]int{5: 2, 10: 4, 15: 6, 20: 8} {
			if c.Strict.MinRises[h] != want {
				t.Errorf("strict %d ans: %d, attendu %d", h, c.Strict.MinRises[h], want)
			}
		}
		for h, want := range map[int]int{5: 1, 10: 3, 15: 4, 20: 6} {
			if c.Relaxed.MinRises[h] != want {
				t.Errorf("assoupli %d ans: %d, attendu %d", h, c.Relaxed.MinRises[h], want)
			}
		}
	})

	t.Run("10 et 11b", func(t *testing.T) {
		t.Parallel()
		c10, _ := r.Criterion("10")
		c11b, _ := r.Criterion("11b")
		if c10.Strict.Direction != DirectionDecrease || c10.Relaxed != nil {
			t.Errorf("10: %+v", c10)
		}
		if c11b.Strict.Direction != DirectionGreater || c11b.Relaxed != nil {
			t.Errorf("11b: %+v", c11b)
		}
	})

	t.Run("paramètres", func(t *testing.T) {
		t.Parallel()
		p := r.Parameters
		if len(p.Horizons) != 4 || p.Horizons[0] != 5 || p.Horizons[3] != 20 ||
			p.TrimMinRates != 5 || p.MinVerdictHorizon != 10 || p.BuybackPeriodYears != 1 {
			t.Errorf("paramètres: %+v", p)
		}
	})

	t.Run("compat", func(t *testing.T) {
		t.Parallel()
		c := r.Compat
		want := Compat{
			Divisor: 18, Rule5bHorizon: 20, Rule5bMinRises: 8, Rule5bSkipFirstTransition: true,
			LowerBoundsChecked: false, EmptyDataPasses: true, EmptyCellAsZero: true, Rule9Operator: ">=",
		}
		if c != want {
			t.Errorf("compat: %+v", c)
		}
	})

	if r.Version != "1" || len(r.Hash) != 64 {
		t.Errorf("version %q, empreinte %q", r.Version, r.Hash)
	}
}

// modifier remplace old par repl dans le fichier embarqué ; échoue si old est absent.
func modifier(t *testing.T, old, repl string) []byte {
	t.Helper()
	s := string(embeddedRules)
	if !strings.Contains(s, old) {
		t.Fatalf("motif %q absent de rules.yaml", old)
	}
	return []byte(strings.Replace(s, old, repl, 1))
}

func TestRulesRefus(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name      string
		old, repl string
		want      string
	}{
		{"assoupli de 11a à 20", "relaxed: {gt: 0, le: 35}", "relaxed: {gt: 0, le: 20}", "11a"},
		{"assoupli de 5a plus strict", "relaxed: {ge: 0.06}", "relaxed: {ge: 0.09}", "5a"},
		{"assoupli de 5b plus strict", "{5: 1, 10: 3, 15: 4, 20: 6}", "{5: 1, 10: 5, 15: 4, 20: 6}", "5b"},
		{"borne basse assouplie de 7", "relaxed: {gt: 0, le: 0.85}", "relaxed: {gt: -1, le: 0.85}", "7"},
		{"critère manquant", "  11c:\n    strict: {gt: 0, le: 3}\n    relaxed: {gt: 0, le: 6}\n", "", "11c"},
		{"critère inconnu", "  9:\n", "  9b:\n", "9"},
		{"assoupli non prévu pour 9", "  9:\n    strict: {ge: 0}\n", "  9:\n    strict: {ge: 0}\n    relaxed: {ge: 0}\n", "9"},
		{"version absente", "version: \"1\"", "", "version"},
		{"champ inconnu", "divisor: 18", "diviseur: 18", "diviseur"},
		{"horizon de verdict hors liste", "min_verdict_horizon: 10", "min_verdict_horizon: 7", "min_verdict_horizon"},
		{"opérateur règle 9", "rule_9_operator: \">=\"", "rule_9_operator: \"<\"", "rule_9_operator"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			_, err := LoadRules(modifier(t, tc.old, tc.repl))
			if err == nil {
				t.Fatal("refus attendu")
			}
			if !strings.Contains(err.Error(), tc.want) {
				t.Errorf("le message doit nommer %q: %v", tc.want, err)
			}
		})
	}

	t.Run("fichier vide et YAML invalide", func(t *testing.T) {
		t.Parallel()
		for _, in := range []string{"", "version: [", "criteria: 3"} {
			if _, err := LoadRules([]byte(in)); err == nil {
				t.Errorf("refus attendu pour %q", in)
			}
		}
	})
}

func TestRulesEmpreinte(t *testing.T) {
	t.Parallel()
	base := mustDefault(t)
	again := mustDefault(t)
	if base.Hash != again.Hash {
		t.Fatal("empreinte instable")
	}
	r, err := LoadRules(modifier(t, "relaxed: {gt: 0, le: 35}", "relaxed: {gt: 0, le: 30}"))
	if err != nil {
		t.Fatal(err)
	}
	if r.Hash == base.Hash {
		t.Error("l'empreinte doit changer quand un seuil change")
	}
}

func TestRulesValidationLimites(t *testing.T) {
	t.Parallel()
	cases := []struct{ name, old, repl string }{
		{"horizons non croissants", "horizons: [5, 10, 15, 20]", "horizons: [10, 5, 15, 20]"},
		{"trim trop bas", "trim_min_rates: 5 ", "trim_min_rates: 2 "},
		{"rachats 0 an", "buyback_period_years: 1 ", "buyback_period_years: 0 "},
		{"diviseur nul", "divisor: 18", "divisor: 0"},
		{"horizon compat inconnu", "rule_5b_horizon: 20", "rule_5b_horizon: 7"},
		{"direction 10", "direction: decrease", "direction: up"},
		{"direction 11b", "direction: greater", "direction: less"},
		{"5b incomplet", "{5: 2, 10: 4, 15: 6, 20: 8}", "{5: 2, 10: 4}"},
		{"5b hors bornes", "{5: 2, 10: 4, 15: 6, 20: 8}", "{5: 2, 10: 4, 15: 6, 20: 25}"},
		{"gt et ge", "strict: {gt: 0, le: 25}", "strict: {gt: 0, ge: 0, le: 25}"},
		{"sans borne", "strict: {ge: 0}\n  10:", "strict: {}\n  10:"},
		{"bornes inversées", "strict: {gt: 0, le: 3}", "strict: {gt: 4, le: 3}"},
		{"assoupli manquant", "    relaxed: {ge: 0.06}\n  5b:", "  5b:"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if _, err := LoadRules(modifier(t, tc.old, tc.repl)); err == nil {
				t.Error("refus attendu")
			}
		})
	}
}
