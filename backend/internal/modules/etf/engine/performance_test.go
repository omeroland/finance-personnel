package engine

import (
	"encoding/json"
	"errors"
	"math"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/shopspring/decimal"
)

const perfTol = 1e-9

type perfAccount struct {
	Depart float64   `json:"depart"`
	Ajouts []float64 `json:"ajouts"`
	Fins   []float64 `json:"fins"`
}

type perfInputs struct {
	perfAccount
	Annuels []float64     `json:"annuels"`
	Comptes []perfAccount `json:"comptes"`
}

type perfExpect struct {
	Mensuel   []float64 `json:"rendement_mensuel"`
	AACJ      []float64 `json:"aacj_mois"`
	Periode   *float64  `json:"rendement_periode"`
	TriAnnee  *float64  `json:"tri_annee"`
	Annualise *float64  `json:"rendement_annualise"`
	Annuel    *float64  `json:"rendement_annuel"`
	Consolide *perfExpect
}

type perfCase struct {
	ID      string     `json:"id"`
	Onglet  string     `json:"onglet"`
	Entrees perfInputs `json:"entrees"`
	Attendu struct {
		perfExpect
		Consolide *perfExpect `json:"consolide"`
	} `json:"attendu_app"`
}

func (a perfAccount) account() StatementAccount {
	st := make([]Statement, len(a.Fins))
	for i := range a.Fins {
		st[i] = Statement{
			Month:        time.Date(2026, time.Month(i+1), 1, 0, 0, 0, 0, time.UTC).Format("2006-01"),
			Contribution: decimal.NewFromFloat(a.Ajouts[i]),
			EndValue:     decimal.NewFromFloat(a.Fins[i]),
		}
	}
	return StatementAccount{Start: decimal.NewFromFloat(a.Depart), Statements: st}
}

func perfNear(t *testing.T, name string, got, want float64) {
	t.Helper()
	if math.IsNaN(got) || math.Abs(got-want) > perfTol {
		t.Errorf("%s: obtenu %.12f, attendu %.12f", name, got, want)
	}
}

func checkPerf(t *testing.T, p Performance, w perfExpect) {
	t.Helper()
	for i, want := range w.Mensuel {
		if !p.Monthly[i].Defined {
			t.Fatalf("mois %d non défini", i+1)
		}
		perfNear(t, "mensuel", p.Monthly[i].Return, want)
	}
	for i, want := range w.AACJ {
		if !p.Monthly[i].HasAACJ {
			t.Fatalf("AACJ %d absent", i+1)
		}
		perfNear(t, "aacj", p.Monthly[i].AACJ, want)
	}
	if w.Periode != nil {
		perfNear(t, "période", p.Cumulative, *w.Periode)
	}
	if w.TriAnnee != nil {
		perfNear(t, "tri année", p.IRR, *w.TriAnnee)
	}
}

func TestOracleRendement(t *testing.T) {
	raw, err := os.ReadFile(filepath.Join("..", "..", "..", "..", "..", "testdata", "oracle", "etf_cases.json"))
	if err != nil {
		t.Fatalf("lecture de l'oracle: %v", err)
	}
	var doc struct {
		Cas []perfCase `json:"cas"`
	}
	if err := json.Unmarshal(raw, &doc); err != nil {
		t.Fatal(err)
	}
	n := 0
	for _, c := range doc.Cas {
		if c.Onglet != "RENDEMENT" {
			continue
		}
		n++
		t.Run(c.ID, func(t *testing.T) {
			switch {
			case len(c.Entrees.Annuels) > 0:
				got, err := Annualize(c.Entrees.Annuels)
				if err != nil {
					t.Fatal(err)
				}
				perfNear(t, "annualisé", got, *c.Attendu.Annualise)
			case len(c.Entrees.Comptes) > 0:
				var accts []StatementAccount
				for _, a := range c.Entrees.Comptes {
					accts = append(accts, a.account())
				}
				p, err := ConsolidateStatements(accts)
				if err != nil {
					t.Fatal(err)
				}
				checkPerf(t, p, perfExpect(c.Attendu.Consolide.copy()))
			default:
				p, err := StatementPerformance(decimal.NewFromFloat(c.Entrees.Depart), c.Entrees.account().Statements)
				if err != nil {
					t.Fatal(err)
				}
				checkPerf(t, p, c.Attendu.perfExpect)
				if c.Attendu.Annuel != nil {
					ann := p.Annual()
					if len(ann) != 1 {
						t.Fatalf("années: %+v", ann)
					}
					perfNear(t, "annuel déduit du mensuel", ann[0].Return, *c.Attendu.Annuel)
				}
			}
		})
	}
	if n != 13 {
		t.Fatalf("cas RENDEMENT: %d, attendu 13", n)
	}
}

func (e *perfExpect) copy() perfExpect { return *e }

func perfD(s string) decimal.Decimal { return decimal.RequireFromString(s) }

func perfDay(y int, m time.Month, dd int) time.Time { return time.Date(y, m, dd, 0, 0, 0, 0, time.UTC) }

func TestXIRRReference(t *testing.T) {
	t.Parallel()
	// Exemple classique de la documentation du tableur (XIRR = 0,373362535).
	flows := []DatedFlow{
		{perfDay(2008, 1, 1), perfD("-10000")},
		{perfDay(2008, 3, 1), perfD("2750")},
		{perfDay(2008, 10, 30), perfD("4250")},
		{perfDay(2009, 2, 15), perfD("3250")},
		{perfDay(2009, 4, 1), perfD("2750")},
	}
	got, err := XIRR(flows)
	if err != nil {
		t.Fatal(err)
	}
	if math.Abs(got-0.373362535) > 1e-6 {
		t.Fatalf("XIRR = %.9f", got)
	}
	// Un an exact : 10 % ; l'ordre des flux n'a pas d'effet.
	got, err = XIRR([]DatedFlow{{perfDay(2021, 1, 1), perfD("1100")}, {perfDay(2020, 1, 1).AddDate(0, 0, 0), perfD("-1000")}})
	if err != nil {
		t.Fatal(err)
	}
	// 2020 est bissextile : 366 jours / 365.
	want := math.Pow(1.1, 365.0/366.0) - 1
	perfNear(t, "un an", got, want)
}

func TestXIRRLimits(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name  string
		flows []DatedFlow
		want  error
	}{
		{"aucun flux", nil, ErrNoFlows},
		{"flux unique", []DatedFlow{{perfDay(2020, 1, 1), perfD("-100")}}, ErrSingleFlow},
		{"valeur finale nulle", []DatedFlow{{perfDay(2020, 1, 1), perfD("-100")}, {perfDay(2021, 1, 1), perfD("0")}}, ErrNoSignChange},
		{"départ nul", []DatedFlow{{perfDay(2020, 1, 1), perfD("0")}, {perfDay(2021, 1, 1), perfD("100")}}, ErrNoSignChange},
		{"date absente", []DatedFlow{{time.Time{}, perfD("-100")}, {perfDay(2021, 1, 1), perfD("110")}}, ErrFlowDate},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			_, err := XIRR(tc.flows)
			if !errors.Is(err, tc.want) {
				t.Fatalf("erreur %v, attendu %v", err, tc.want)
			}
		})
	}
	// Taux hors de portée d'un float64 (1000^365) : erreur typée, pas de NaN.
	r, err := XIRR([]DatedFlow{{perfDay(2020, 1, 1), perfD("-1")}, {perfDay(2020, 1, 2), perfD("1000")}})
	if !errors.Is(err, ErrNoConvergence) || r != 0 {
		t.Fatalf("r=%v err=%v", r, err)
	}
	// Taux très élevé mais représentable : repli par bissection.
	r, err = XIRR([]DatedFlow{{perfDay(2020, 1, 1), perfD("-1")}, {perfDay(2021, 1, 1), perfD("1000")}})
	if err != nil || math.Abs(r-(math.Pow(1000, 365.0/366.0)-1)) > 1e-6 {
		t.Fatalf("r=%v err=%v", r, err)
	}
}

func TestTWRChaining(t *testing.T) {
	t.Parallel()
	// 1000 au départ ; +10 % ; cotisation de 1000 ; -10 % : TWR = 1,1 × 0,9 - 1 = -1 %.
	pts := []Valuation{
		{perfDay(2026, 1, 1), perfD("0"), perfD("1000")},
		{perfDay(2026, 1, 31), perfD("1100"), perfD("1000")},
		{perfDay(2026, 2, 28), perfD("1890"), perfD("0")},
	}
	p, err := TWR(pts)
	if err != nil {
		t.Fatal(err)
	}
	perfNear(t, "cumulé", p.Cumulative, -0.01)
	if len(p.Monthly) != 2 {
		t.Fatalf("mensuel: %+v", p.Monthly)
	}
	perfNear(t, "janvier", p.Monthly[0].Return, 0.1)
	perfNear(t, "février", p.Monthly[1].Return, -0.1)
	ann := p.Annual()
	if len(ann) != 1 || ann[0].Months != 2 {
		t.Fatalf("annuel: %+v", ann)
	}
	perfNear(t, "annuel déduit", ann[0].Return, -0.01)
	if !p.HasIRR || math.IsNaN(p.IRR) {
		t.Fatalf("TRI absent: %+v", p)
	}
}

func TestTWRLimits(t *testing.T) {
	t.Parallel()
	if _, err := TWR(nil); !errors.Is(err, ErrNoPeriod) {
		t.Fatalf("nil: %v", err)
	}
	if _, err := TWR([]Valuation{{perfDay(2026, 1, 1), perfD("10"), perfD("0")}}); !errors.Is(err, ErrNoPeriod) {
		t.Fatalf("un point: %v", err)
	}
	bad := []Valuation{{perfDay(2026, 2, 1), perfD("10"), perfD("0")}, {perfDay(2026, 1, 1), perfD("10"), perfD("0")}}
	if _, err := TWR(bad); !errors.Is(err, ErrValuations) {
		t.Fatalf("dates: %v", err)
	}
	// Départ nul sans flux : rendement indéfini, pas de NaN.
	p, err := TWR([]Valuation{{perfDay(2026, 1, 1), perfD("0"), perfD("0")}, {perfDay(2026, 2, 1), perfD("0"), perfD("0")}})
	if err != nil || p.HasCumulative || p.HasIRR || p.Monthly[0].Defined {
		t.Fatalf("p=%+v err=%v", p, err)
	}
	// Valeur finale nulle : -100 %.
	p, err = TWR([]Valuation{{perfDay(2026, 1, 1), perfD("0"), perfD("100")}, {perfDay(2026, 2, 1), perfD("0"), perfD("0")}})
	if err != nil || !p.HasCumulative {
		t.Fatalf("p=%+v err=%v", p, err)
	}
	perfNear(t, "perte totale", p.Cumulative, -1)
}

func TestMergeValuations(t *testing.T) {
	t.Parallel()
	a := []Valuation{{perfDay(2026, 1, 1), perfD("0"), perfD("100")}, {perfDay(2026, 2, 1), perfD("110"), perfD("0")}}
	b := []Valuation{{perfDay(2026, 1, 1), perfD("0"), perfD("300")}, {perfDay(2026, 2, 1), perfD("270"), perfD("0")}}
	m, err := MergeValuations([][]Valuation{a, b})
	if err != nil {
		t.Fatal(err)
	}
	p, err := TWR(m)
	if err != nil {
		t.Fatal(err)
	}
	perfNear(t, "consolidé", p.Cumulative, -0.05)
	if _, err := MergeValuations([][]Valuation{a, b[:1]}); !errors.Is(err, ErrHorizon) {
		t.Fatalf("horizon: %v", err)
	}
}

func TestStatementLimits(t *testing.T) {
	t.Parallel()
	if _, err := StatementPerformance(perfD("100"), nil); !errors.Is(err, ErrNoPeriod) {
		t.Fatalf("vide: %v", err)
	}
	if _, err := StatementPerformance(perfD("100"), []Statement{{Month: "2026-13", EndValue: perfD("1")}}); !errors.Is(err, ErrStatements) {
		t.Fatalf("mois: %v", err)
	}
	if _, err := StatementPerformance(perfD("100"), []Statement{{Month: "2026-02", EndValue: perfD("1")}, {Month: "2026-01", EndValue: perfD("1")}}); !errors.Is(err, ErrStatements) {
		t.Fatalf("ordre: %v", err)
	}
	// Départ nul : le premier mois n'a pas de rendement, le suivant oui.
	p, err := StatementPerformance(perfD("0"), []Statement{
		{Month: "2026-01", Contribution: perfD("100"), EndValue: perfD("100")},
		{Month: "2026-02", Contribution: perfD("0"), EndValue: perfD("110")},
	})
	if err != nil || p.Monthly[0].Defined || !p.Monthly[1].Defined {
		t.Fatalf("p=%+v err=%v", p, err)
	}
	perfNear(t, "février", p.Monthly[1].Return, 0.1)
	// Valeur finale nulle : -100 %, TRI absent.
	p, err = StatementPerformance(perfD("100"), []Statement{{Month: "2026-01", EndValue: perfD("0")}})
	if err != nil || p.HasIRR {
		t.Fatalf("p=%+v err=%v", p, err)
	}
	perfNear(t, "perte", p.Monthly[0].Return, -1)
	// Consolidation d'horizons différents.
	a := StatementAccount{Start: perfD("1"), Statements: []Statement{{Month: "2026-01", EndValue: perfD("1")}}}
	b := StatementAccount{Start: perfD("1"), Statements: []Statement{{Month: "2026-02", EndValue: perfD("1")}}}
	if _, err := ConsolidateStatements([]StatementAccount{a, b}); !errors.Is(err, ErrHorizon) {
		t.Fatalf("horizon: %v", err)
	}
	if _, err := Annualize(nil); !errors.Is(err, ErrNoPeriod) {
		t.Fatalf("annualiser: %v", err)
	}
}

func FuzzXIRR(f *testing.F) {
	f.Add(-1000.0, 1100.0, 365, 50.0)
	f.Add(0.0, 0.0, 0, 0.0)
	f.Add(-1.0, 1e12, 1, -5.0)
	f.Fuzz(func(t *testing.T, a, b float64, days int, c float64) {
		if math.IsNaN(a) || math.IsInf(a, 0) || math.IsNaN(b) || math.IsInf(b, 0) || math.IsNaN(c) || math.IsInf(c, 0) {
			return
		}
		days = days % 20000
		t0 := perfDay(2000, 1, 1)
		r, err := XIRR([]DatedFlow{
			{t0, decimal.NewFromFloat(math.Mod(a, 1e12))},
			{t0.AddDate(0, 0, days/2), decimal.NewFromFloat(math.Mod(c, 1e12))},
			{t0.AddDate(0, 0, days), decimal.NewFromFloat(math.Mod(b, 1e12))},
		})
		if err == nil && (math.IsNaN(r) || math.IsInf(r, 0) || r <= -1) {
			t.Fatalf("taux invalide %v", r)
		}
	})
}

func FuzzStatementPerformance(f *testing.F) {
	f.Add(1000.0, 50.0, 1100.0, 0.0, 1050.0)
	f.Add(0.0, 0.0, 0.0, 0.0, 0.0)
	f.Fuzz(func(t *testing.T, start, c1, e1, c2, e2 float64) {
		for _, x := range []float64{start, c1, e1, c2, e2} {
			if math.IsNaN(x) || math.IsInf(x, 0) {
				return
			}
		}
		p, err := StatementPerformance(decimal.NewFromFloat(math.Abs(math.Mod(start, 1e12))), []Statement{
			{"2026-01", decimal.NewFromFloat(math.Mod(c1, 1e12)), decimal.NewFromFloat(math.Abs(math.Mod(e1, 1e12)))},
			{"2026-02", decimal.NewFromFloat(math.Mod(c2, 1e12)), decimal.NewFromFloat(math.Abs(math.Mod(e2, 1e12)))},
		})
		if err != nil {
			return
		}
		for _, m := range p.Monthly {
			if math.IsNaN(m.Return) || math.IsNaN(m.AACJ) || math.IsInf(m.Return, 0) {
				t.Fatalf("NaN: %+v", m)
			}
		}
		if math.IsNaN(p.Cumulative) || math.IsNaN(p.IRR) {
			t.Fatalf("NaN: %+v", p)
		}
		for _, y := range p.Annual() {
			if math.IsNaN(y.Return) {
				t.Fatalf("NaN annuel")
			}
		}
	})
}
