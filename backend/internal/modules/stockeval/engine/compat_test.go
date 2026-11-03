package engine

import (
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/shopspring/decimal"
)

func fp(x float64) *float64 { return &x }

func (g goldenRow) compat() CompatMetrics {
	i := g.Inputs
	rises := int(i.EPSIncreases)
	return CompatMetrics{
		EPSGrowth: fp(i.EPSGrowth), EPSRises: &rises, DPSGrowth: fp(i.DPSGrowth),
		PayoutRatio: fp(i.Payout), DebtToEquity: fp(i.DebtToEquity), DividendChange: fp(i.DividendChange),
		Buyback: i.Buyback, PE: fp(i.PE), CurrentYield: fp(i.CurrentYield), AverageYield: fp(i.AvgYield), PB: fp(i.PB),
	}
}

// Les 15 lignes de référence donnent, en compat, les échecs lus dans la feuille.
func TestCompatGoldenSamples(t *testing.T) {
	t.Parallel()
	raw, err := os.ReadFile(filepath.Join("..", "..", "..", "..", "..", "testdata", "golden", "simply_investing_samples.json"))
	if err != nil {
		t.Fatal(err)
	}
	var doc struct {
		Rows []goldenRow `json:"rows"`
	}
	if err := json.Unmarshal(raw, &doc); err != nil {
		t.Fatal(err)
	}
	if len(doc.Rows) != 15 {
		t.Fatalf("lignes de référence: %d, attendu 15", len(doc.Rows))
	}
	rules := mustRules(t)
	for _, row := range doc.Rows {
		t.Run(fmt.Sprintf("%s_ligne%d", row.Symbol, row.Row), func(t *testing.T) {
			t.Parallel()
			ev, err := CompatScore(row.compat(), rules, Context{})
			if err != nil {
				t.Fatal(err)
			}
			h := ev.Horizons[0]
			if h.Horizon != 20 {
				t.Errorf("horizon %d, attendu 20", h.Horizon)
			}
			for _, c := range h.Criteria {
				if got, want := c.Status == StatusFailed, row.Strict[c.ID] == 1; got != want {
					t.Errorf("règle %s: statut %s, échecs attendus %d", c.ID, c.Status, row.Strict[c.ID])
				}
			}
			if h.Strict.Failures != row.StrictCount {
				t.Errorf("compte %d, attendu %d", h.Strict.Failures, row.StrictCount)
			}
			if h.Relaxed.Failures != h.Strict.Failures {
				t.Errorf("le profil assoupli doit reprendre le strict en compat: %d vs %d", h.Relaxed.Failures, h.Strict.Failures)
			}
		})
	}
}

func TestCompatEmptyRow(t *testing.T) {
	t.Parallel()
	rules := mustRules(t)
	for _, in := range []CompatInputs{{}, {EPS: make(CompatCells, 20), DPS: make(CompatCells, 20)}} {
		ev, err := CompatEvaluate(in, rules, Context{Gate: Gate{AnswerYes, AnswerYes, AnswerYes, AnswerYes}})
		if err != nil {
			t.Fatal(err)
		}
		h := ev.Horizons[0]
		if h.Strict.Failures != 0 || h.Relaxed.Failures != 0 {
			t.Errorf("ligne vide: %d échecs, attendu 0", h.Strict.Failures)
		}
		if len(h.Warnings) != 11 { // 10 règles absentes + la ligne entièrement vide
			t.Errorf("avertissements: %d (%v), attendu 11", len(h.Warnings), h.Warnings)
		}
		if !strings.Contains(strings.Join(h.Warnings, "|"), "entièrement vide") {
			t.Errorf("avertissement de ligne vide absent: %v", h.Warnings)
		}
		if ev.Verdict != VerdictCandidate {
			t.Errorf("verdict %s", ev.Verdict)
		}
	}
}

func TestCompatDocumentedCase(t *testing.T) {
	t.Parallel()
	cells := make(CompatCells, 20)
	for i, s := range []string{"1.00", "1.10", "1.21", "1.331", "1.4641"} {
		cells[15+i] = decp(s)
	}
	div := mustRules(t).Compat.Divisor
	eps, err := CompatEPSGrowth(cells, div)
	if err != nil {
		t.Fatal(err)
	}
	if math.Abs(eps.Average-0.2/18) > 1e-9 || eps.Count != 4 {
		t.Errorf("BPA: %+v", eps)
	}
	if math.Abs(eps.Average-0.011111111) > 1e-8 {
		t.Errorf("moyenne %v, attendu 0.0111", eps.Average)
	}
	dps, err := CompatDividendGrowth(cells, div)
	if err != nil {
		t.Fatal(err)
	}
	if dps.Count != 19 {
		t.Errorf("hausses consécutives %d, attendu 19", dps.Count)
	}
}

func TestCompatGrowthEdges(t *testing.T) {
	t.Parallel()
	div := 18
	one := func(i int) CompatCells {
		c := make(CompatCells, 20)
		c[i] = decp("2")
		return c
	}
	// Seule la dernière cellule: aucun taux, SUM, MIN et MAX valent 0.
	g, err := CompatEPSGrowth(one(19), div)
	if err != nil || g.Blank || g.Average != 0 || g.Count != 0 {
		t.Errorf("une seule valeur: %+v %v", g, err)
	}
	// Dernière cellule vide: la feuille affiche "" même avec une erreur plus tôt.
	c := one(3)
	c[2] = decp("0")
	if g, err := CompatEPSGrowth(c, div); err != nil || !g.Blank {
		t.Errorf("dernière cellule vide: %+v %v", g, err)
	}
	if g, err := CompatDividendGrowth(c, div); err != nil || !g.Blank {
		t.Errorf("dividendes, dernière cellule vide: %+v %v", g, err)
	}
	// Un trou: la cellule vide vaut 0, donc -100 % ; le taux suivant est vide.
	c = make(CompatCells, 20)
	for i := range c {
		c[i] = decp("1")
	}
	c[10] = nil
	g, err = CompatEPSGrowth(c, div)
	if err != nil || g.Blank {
		t.Fatal(err)
	}
	if want := (-1.0 + 0 - -1.0 - 0) / 18; math.Abs(g.Average-want) > 1e-12 {
		t.Errorf("moyenne avec trou %v", g.Average)
	}
	// Valeur précédente nulle.
	c[10] = decp("0")
	if _, err := CompatEPSGrowth(c, div); !errors.Is(err, ErrSheetDivZero) {
		t.Errorf("erreur %v, attendu ErrSheetDivZero", err)
	}
	if _, err := CompatDividendGrowth(c, div); !errors.Is(err, ErrSheetDivZero) {
		t.Errorf("erreur %v, attendu ErrSheetDivZero", err)
	}
	// Mauvaise longueur.
	if _, err := CompatEPSGrowth(make(CompatCells, 5), div); err == nil {
		t.Error("longueur invalide acceptée")
	}
}

func TestCompatCellsFromSeries(t *testing.T) {
	t.Parallel()
	s := Series{{Year: 2024, Value: decimal.NewFromInt(3)}, {Year: 2020, Value: decimal.NewFromInt(1)}, {Year: 1990, Value: decimal.NewFromInt(9)}}
	c := CompatCellsFromSeries(s)
	if len(c) != 20 || c[19] == nil || !c[19].Equal(decimal.NewFromInt(3)) || c[15] == nil || c[16] != nil || c[0] != nil {
		t.Errorf("cellules: %v", c)
	}
	if e := CompatCellsFromSeries(nil); len(e) != 20 || e[19] != nil {
		t.Errorf("série vide: %v", e)
	}
}

func TestCompatCompute(t *testing.T) {
	t.Parallel()
	rules := mustRules(t)
	if _, err := CompatCompute(CompatInputs{}, nil); err == nil {
		t.Error("référentiel absent accepté")
	}
	if _, err := CompatScore(CompatMetrics{}, nil, Context{}); err == nil {
		t.Error("référentiel absent accepté")
	}
	bad := make(CompatCells, 20)
	bad[4], bad[5] = decp("0"), decp("1")
	bad[19] = decp("1")
	for name, in := range map[string]CompatInputs{"BPA": {EPS: bad}, "dividendes": {DPS: bad}} {
		_, err := CompatEvaluate(in, rules, Context{})
		if !errors.Is(err, ErrSheetDivZero) || !strings.Contains(err.Error(), name) {
			t.Errorf("%s: %v", name, err)
		}
	}
	var s Series
	for y := 2005; y <= 2024; y++ {
		s = append(s, Point{Year: y, Value: decimal.NewFromInt(int64(y - 2000))})
	}
	m, err := CompatCompute(CompatInputs{EPS: CompatCellsFromSeries(s), DPS: CompatCellsFromSeries(s), PE: fp(10)}, rules)
	if err != nil || m.EPSGrowth == nil || *m.EPSRises != 18 || m.DPSGrowth == nil || *m.PE != 10 {
		t.Errorf("%+v %v", m, err)
	}
}

func TestCompatScoreRules(t *testing.T) {
	t.Parallel()
	rules := mustRules(t)
	status := func(m CompatMetrics, id string) Status {
		t.Helper()
		ev, err := CompatScore(m, rules, Context{})
		if err != nil {
			t.Fatal(err)
		}
		for _, c := range ev.Horizons[0].Criteria {
			if c.ID == id {
				return c.Status
			}
		}
		t.Fatalf("critère %s introuvable", id)
		return ""
	}
	rises := func(n int) *int { return &n }
	tests := []struct {
		name string
		id   string
		m    CompatMetrics
		want Status
	}{
		{"5a sous le seuil", "5a", CompatMetrics{EPSGrowth: fp(0.0799)}, StatusFailed},
		{"5a au seuil", "5a", CompatMetrics{EPSGrowth: fp(0.08)}, StatusPassed},
		{"5b 7 hausses", "5b", CompatMetrics{EPSRises: rises(7)}, StatusFailed},
		{"5b 8 hausses", "5b", CompatMetrics{EPSRises: rises(8)}, StatusPassed},
		{"6 sous le seuil", "6", CompatMetrics{DPSGrowth: fp(-0.1)}, StatusFailed},
		{"7 négatif réussit", "7", CompatMetrics{PayoutRatio: fp(-2)}, StatusPassed},
		{"7 trop haut", "7", CompatMetrics{PayoutRatio: fp(0.76)}, StatusFailed},
		{"8 négatif réussit", "8", CompatMetrics{DebtToEquity: fp(-5)}, StatusPassed},
		{"8 trop haut", "8", CompatMetrics{DebtToEquity: fp(0.71)}, StatusFailed},
		{"9 zéro réussit", "9", CompatMetrics{DividendChange: fp(0)}, StatusPassed},
		{"9 baisse", "9", CompatMetrics{DividendChange: fp(-0.01)}, StatusFailed},
		{"10 non", "10", CompatMetrics{Buyback: "No"}, StatusFailed},
		{"10 oui", "10", CompatMetrics{Buyback: " YES "}, StatusPassed},
		{"10 absent", "10", CompatMetrics{}, StatusPassed},
		{"11a négatif réussit", "11a", CompatMetrics{PE: fp(-30)}, StatusPassed},
		{"11a trop haut", "11a", CompatMetrics{PE: fp(25.1)}, StatusFailed},
		{"11b égal réussit", "11b", CompatMetrics{CurrentYield: fp(0.02), AverageYield: fp(0.02)}, StatusPassed},
		{"11b plus bas", "11b", CompatMetrics{CurrentYield: fp(0.01), AverageYield: fp(0.02)}, StatusFailed},
		{"11b une seule valeur", "11b", CompatMetrics{CurrentYield: fp(0.01)}, StatusPassed},
		{"11c négatif réussit", "11c", CompatMetrics{PB: fp(-1)}, StatusPassed},
		{"11c trop haut", "11c", CompatMetrics{PB: fp(3.01)}, StatusFailed},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if got := status(tt.m, tt.id); got != tt.want {
				t.Errorf("statut %s, attendu %s", got, tt.want)
			}
		})
	}
	// Opérateur « > » de la règle 9 (onglet d'exemple).
	strict := *rules
	strict.Compat.Rule9Operator = ">"
	ev, err := CompatScore(CompatMetrics{DividendChange: fp(0)}, &strict, Context{})
	if err != nil {
		t.Fatal(err)
	}
	for _, c := range ev.Horizons[0].Criteria {
		if c.ID == "9" && (c.Status != StatusFailed || c.Strict.Gt == nil || c.Strict.Ge != nil) {
			t.Errorf("règle 9 avec >: %+v", c)
		}
	}
}

func TestCompatViewsAndVerdict(t *testing.T) {
	t.Parallel()
	rules := mustRules(t)
	gate := Gate{AnswerYes, AnswerYes, AnswerYes, AnswerYes}
	ev, err := CompatScore(CompatMetrics{}, rules, Context{Gate: Gate{AnswerNo}})
	if err != nil || ev.Verdict != VerdictExcluded {
		t.Fatalf("verdict %s %v", ev.Verdict, err)
	}
	ev, err = CompatScore(CompatMetrics{PE: fp(40)}, rules, Context{Gate: gate})
	if err != nil || ev.Verdict != VerdictRejected {
		t.Fatalf("verdict %s %v", ev.Verdict, err)
	}
	for _, c := range ev.Horizons[0].Criteria {
		if c.Relaxed != nil {
			t.Errorf("règle %s: seuil assoupli affiché en compat", c.ID)
		}
		switch c.ID {
		case "7", "8", "11a", "11c":
			if c.Strict.Gt != nil || c.Strict.Ge != nil || c.Strict.Le == nil {
				t.Errorf("règle %s: borne basse présente en compat: %+v", c.ID, c.Strict)
			}
		case "5b":
			if c.Strict.MinRises == nil || *c.Strict.MinRises != 8 {
				t.Errorf("5b: %+v", c.Strict)
			}
		}
	}
}
