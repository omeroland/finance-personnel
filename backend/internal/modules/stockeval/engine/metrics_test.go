package engine

import (
	"errors"
	"math"
	"strings"
	"testing"

	"github.com/shopspring/decimal"
)

func dec(s string) decimal.Decimal { return decimal.RequireFromString(s) }
func decp(s string) *decimal.Decimal {
	d := dec(s)
	return &d
}

// series construit une série annuelle à partir de startYear.
func series(startYear int, vals ...string) Series {
	s := make(Series, len(vals))
	for i, v := range vals {
		s[i] = Point{Year: startYear + i, Value: dec(v)}
	}
	return s
}

func near(a, b float64) bool { return math.Abs(a-b) < 1e-9 }

func TestGrowth(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name      string
		prev, cur string
		want      float64
		err       error
	}{
		{"hausse", "1.00", "1.10", 0.10, nil},
		{"baisse", "2", "1", -0.5, nil},
		{"précédent négatif", "-1.00", "0.50", 1.50, nil},
		{"précédent négatif aggravé", "-1", "-2", -1, nil},
		{"précédent nul", "0", "1", 0, ErrZeroBase},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got, err := Growth(dec(tt.prev), dec(tt.cur))
			if !errors.Is(err, tt.err) {
				t.Fatalf("erreur = %v, attendu %v", err, tt.err)
			}
			if !near(got, tt.want) {
				t.Fatalf("got %v, attendu %v", got, tt.want)
			}
		})
	}
}

func TestGrowthNotFinite(t *testing.T) {
	t.Parallel()
	huge := decimal.New(1, 400)
	if _, err := Growth(dec("1e-300").Div(dec("1e100")), huge); !errors.Is(err, ErrNotFinite) {
		// la division décimale peut aussi arrondir à zéro ; seul un résultat fini est exclu
		if err == nil {
			t.Fatal("un résultat infini ne doit pas être retourné")
		}
	}
	if _, err := Growth(dec("1e-400"), huge); !errors.Is(err, ErrNotFinite) {
		t.Fatalf("erreur = %v, attendu ErrNotFinite", err)
	}
}

func TestWindow(t *testing.T) {
	t.Parallel()
	s := series(2015, "1", "2", "3", "4", "5", "6")
	tests := []struct {
		name string
		s    Series
		h    int
		ok   bool
		from int
	}{
		{"5 dernières", s, 5, true, 2016},
		{"toute la série", s, 6, true, 2015},
		{"trop long", s, 7, false, 0},
		{"h nul", s, 0, false, 0},
		{"série vide", nil, 5, false, 0},
		{"trou", Series{{2015, dec("1")}, {2016, dec("1")}, {2018, dec("1")}}, 3, false, 0},
		{"trou hors fenêtre", Series{{2010, dec("1")}, {2016, dec("1")}, {2017, dec("1")}}, 2, true, 2016},
		{"désordre", Series{{2017, dec("3")}, {2015, dec("1")}, {2016, dec("2")}}, 3, true, 2015},
		{"doublon", Series{{2015, dec("1")}, {2015, dec("9")}, {2016, dec("2")}}, 2, true, 2015},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			w, ok := Window(tt.s, tt.h)
			if ok != tt.ok {
				t.Fatalf("ok = %v, attendu %v", ok, tt.ok)
			}
			if ok && (len(w) != tt.h || w[0].Year != tt.from) {
				t.Fatalf("fenêtre %v", w)
			}
		})
	}
	// la série d'origine n'est pas modifiée
	orig := Series{{2017, dec("3")}, {2015, dec("1")}, {2016, dec("2")}}
	Window(orig, 3)
	if orig[0].Year != 2017 {
		t.Fatal("Window a modifié son entrée")
	}
}

func TestTrimmedMean(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name string
		xs   []float64
		min  int
		want float64
		err  error
	}{
		{"simple sous le seuil", []float64{0.1, 0.1, 0.1, 0.1}, 5, 0.1, nil},
		{"tronquée", []float64{1, 2, 3, 4, 100}, 5, 3, nil},
		{"tronquée min et max", []float64{-100, 1, 2, 3, 100}, 5, 2, nil},
		{"simple avec extrêmes", []float64{1, 2, 3, 4, 100}, 6, 22, nil},
		{"seuil trop bas, 2 valeurs", []float64{1, 3}, 1, 2, nil},
		{"vide", nil, 5, 0, ErrNoData},
		{"NaN", []float64{1, math.NaN()}, 5, 0, ErrNotFinite},
		{"infini", []float64{math.Inf(1), 1}, 5, 0, ErrNotFinite},
		{"débordement", []float64{math.MaxFloat64, math.MaxFloat64}, 5, 0, ErrNotFinite},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got, err := TrimmedMean(tt.xs, tt.min)
			if !errors.Is(err, tt.err) || !near(got, tt.want) {
				t.Fatalf("got %v, %v ; attendu %v, %v", got, err, tt.want, tt.err)
			}
		})
	}
}

func TestCountIncreases(t *testing.T) {
	t.Parallel()
	if n := CountIncreases([]float64{0.1, 0, -0.2, 0.3}); n != 2 {
		t.Fatalf("got %d", n)
	}
	if n := CountIncreases(nil); n != 0 {
		t.Fatalf("got %d", n)
	}
}

func testOpts() Options {
	return Options{Horizons: []int{5, 10, 15, 20}, TrimMinRates: 5, BuybackYears: 1}
}

func byHorizon(ms []Metrics, h int) Metrics {
	for _, m := range ms {
		if m.Horizon == h {
			return m
		}
	}
	return Metrics{}
}

func TestComputeMetricsGeometricEPS(t *testing.T) {
	t.Parallel()
	in := Inputs{EPS: series(2020, "1.00", "1.10", "1.21", "1.331", "1.4641")}
	ms := ComputeMetrics(in, Options{Horizons: []int{5, 10}, TrimMinRates: 5})
	m := byHorizon(ms, 5)
	if !m.EPSGrowth.Ok() || !near(m.EPSGrowth.Value, 0.10) {
		t.Fatalf("croissance %+v", m.EPSGrowth)
	}
	if !m.EPSRises.Ok() || m.EPSRises.Value != 4 {
		t.Fatalf("hausses %+v", m.EPSRises)
	}
	if m10 := byHorizon(ms, 10); m10.EPSGrowth.Status != StatusInsufficient || m10.EPSRises.Status != StatusInsufficient {
		t.Fatalf("10 ans %+v", m10)
	}
}

func TestComputeMetricsTrimmedWithSixRates(t *testing.T) {
	t.Parallel()
	// 7 années, 6 taux : 0.5, 0.1 x4, -0.4 ; tronquée -> 0.1
	in := Inputs{EPS: series(2018, "1", "1.5", "1.65", "1.815", "1.9965", "2.19615", "1.317690")}
	m := byHorizon(ComputeMetrics(in, Options{Horizons: []int{7}, TrimMinRates: 5}), 7)
	if !near(m.EPSGrowth.Value, 0.10) || m.EPSRises.Value != 5 {
		t.Fatalf("%+v", m)
	}
}

func TestComputeMetricsNegativePrevious(t *testing.T) {
	t.Parallel()
	in := Inputs{EPS: series(2023, "-1.00", "0.50")}
	m := byHorizon(ComputeMetrics(in, Options{Horizons: []int{2}, TrimMinRates: 5}), 2)
	if !near(m.EPSGrowth.Value, 1.5) || m.EPSRises.Value != 1 {
		t.Fatalf("%+v", m)
	}
}

func TestComputeMetricsZeroPrevious(t *testing.T) {
	t.Parallel()
	in := Inputs{EPS: series(2020, "1.00", "0", "1.00", "1.10", "1.21")}
	m := byHorizon(ComputeMetrics(in, Options{Horizons: []int{5}, TrimMinRates: 5}), 5)
	// taux 1->0 = -1 ; 0->1 ignoré ; 0.1 ; 0.1 -> 3 taux, moyenne simple
	if !m.EPSGrowth.Ok() || !near(m.EPSGrowth.Value, (-1+0.1+0.1)/3) {
		t.Fatalf("%+v", m.EPSGrowth)
	}
	if m.EPSRises.Value != 2 {
		t.Fatalf("hausses %+v", m.EPSRises)
	}
	if len(m.Warnings) != 1 || !strings.Contains(m.Warnings[0], "2021") {
		t.Fatalf("avertissements %v", m.Warnings)
	}
}

func TestComputeMetricsAllRatesIgnored(t *testing.T) {
	t.Parallel()
	in := Inputs{EPS: series(2022, "0", "1")}
	m := byHorizon(ComputeMetrics(in, Options{Horizons: []int{2}, TrimMinRates: 5}), 2)
	if m.EPSGrowth.Status != StatusMissing || m.EPSRises.Status != StatusMissing || len(m.Warnings) == 0 {
		t.Fatalf("%+v", m)
	}
}

func TestComputeMetricsMissingYearInTen(t *testing.T) {
	t.Parallel()
	var s Series
	for y := 2015; y <= 2024; y++ {
		if y == 2019 {
			continue // trou au milieu : 2019 manque
		}
		s = append(s, Point{Year: y, Value: dec("1").Add(decimal.NewFromInt(int64(y - 2015)).Mul(dec("0.1")))})
	}
	// années 2020..2024 : 5 années complètes
	ms := ComputeMetrics(Inputs{EPS: s, DPS: s}, testOpts())
	for _, h := range []int{10, 15, 20} {
		m := byHorizon(ms, h)
		if m.EPSGrowth.Status != StatusInsufficient || m.EPSRises.Status != StatusInsufficient || m.DPSGrowth.Status != StatusInsufficient {
			t.Fatalf("horizon %d : %+v", h, m)
		}
	}
	m5 := byHorizon(ms, 5)
	if !m5.EPSGrowth.Ok() || m5.EPSRises.Value != 4 {
		t.Fatalf("horizon 5 : %+v", m5)
	}
}

func TestComputeMetricsEmptySeriesMissing(t *testing.T) {
	t.Parallel()
	m := byHorizon(ComputeMetrics(Inputs{}, testOpts()), 5)
	for name, st := range map[string]MeasureStatus{
		"eps": m.EPSGrowth.Status, "rises": m.EPSRises.Status, "dps": m.DPSGrowth.Status,
		"payout": m.PayoutRatio.Status, "debt": m.DebtToEquity.Status, "chg": m.DividendChange.Status,
		"shares": m.ShareChange.Status, "pe": m.PE.Status, "pb": m.PB.Status,
		"cy": m.CurrentYield.Status, "ay": m.AverageYield.Status,
	} {
		if st != StatusMissing {
			t.Errorf("%s = %s", name, st)
		}
	}
}

func fullInputs() Inputs {
	return Inputs{
		EPS:         series(2020, "2.00", "2.20", "2.40", "2.60", "4.00"),
		DPS:         series(2020, "0.50", "0.60", "0.70", "0.80", "1.00"),
		AnnualYield: series(2020, "0.02", "0.03", "0.04", "0.03", "0.03"),
		Shares:      series(2020, "100", "99", "98", "97", "95"),
		Snapshot: Snapshot{
			Price: decp("40"), ForwardDividend: decp("1.10"), LongTermDebt: decp("70"), Equity: decp("100"),
			BookValuePerShare: decp("10"),
		},
		CurrentYear: 2025,
	}
}

func TestComputeMetricsStatic(t *testing.T) {
	t.Parallel()
	m := byHorizon(ComputeMetrics(fullInputs(), testOpts()), 5)
	checks := []struct {
		name      string
		got, want float64
	}{
		{"payout", m.PayoutRatio.Value, 0.25},
		{"dette", m.DebtToEquity.Value, 0.70},
		{"variation dividende", m.DividendChange.Value, 0.10},
		{"variation actions", m.ShareChange.Value, 95.0/97.0 - 1},
		{"pe", m.PE.Value, 10},
		{"pb", m.PB.Value, 4},
		{"rendement actuel", m.CurrentYield.Value, 0.0275},
		{"rendement moyen", m.AverageYield.Value, 0.03},
	}
	for _, c := range checks {
		if !near(c.got, c.want) {
			t.Errorf("%s = %v, attendu %v", c.name, c.got, c.want)
		}
	}
	if len(m.Warnings) != 0 {
		t.Fatalf("avertissements %v", m.Warnings)
	}
}

func TestComputeMetricsFX(t *testing.T) {
	t.Parallel()
	in := fullInputs()
	in.Snapshot.FX = decp("1.25") // USD -> CAD
	m := byHorizon(ComputeMetrics(in, testOpts()), 5)
	if !near(m.PE.Value, 40/(4.0*1.25)) || !near(m.PB.Value, 40/(10*1.25)) || !near(m.CurrentYield.Value, 1.10*1.25/40) {
		t.Fatalf("%+v %+v %+v", m.PE, m.PB, m.CurrentYield)
	}
	// les ratios dans une même devise ne bougent pas
	if !near(m.PayoutRatio.Value, 0.25) || !near(m.DebtToEquity.Value, 0.70) {
		t.Fatalf("%+v %+v", m.PayoutRatio, m.DebtToEquity)
	}
}

func TestComputeMetricsDegenerate(t *testing.T) {
	t.Parallel()
	zero := decp("0")
	tests := []struct {
		name   string
		mutate func(*Inputs)
		check  func(Metrics) Measure
		want   MeasureStatus
	}{
		{"BPA nul : P/E", func(in *Inputs) { in.EPS[4].Value = dec("0") }, func(m Metrics) Measure { return m.PE }, StatusMissing},
		{"BPA nul : distribution", func(in *Inputs) { in.EPS[4].Value = dec("0") }, func(m Metrics) Measure { return m.PayoutRatio }, StatusMissing},
		{"BPA négatif : P/E négatif", func(in *Inputs) { in.EPS[4].Value = dec("-1") }, func(m Metrics) Measure { return m.PE }, StatusValue},
		{"capitaux nuls", func(in *Inputs) { in.Snapshot.Equity = zero }, func(m Metrics) Measure { return m.DebtToEquity }, StatusMissing},
		{"capitaux négatifs", func(in *Inputs) { in.Snapshot.Equity = decp("-100") }, func(m Metrics) Measure { return m.DebtToEquity }, StatusValue},
		{"prix nul", func(in *Inputs) { in.Snapshot.Price = zero }, func(m Metrics) Measure { return m.CurrentYield }, StatusMissing},
		{"valeur comptable nulle", func(in *Inputs) { in.Snapshot.BookValuePerShare = zero }, func(m Metrics) Measure { return m.PB }, StatusMissing},
		{"fx nul", func(in *Inputs) { in.Snapshot.FX = zero }, func(m Metrics) Measure { return m.PE }, StatusMissing},
		{"dernier dividende nul", func(in *Inputs) { in.DPS[4].Value = dec("0") }, func(m Metrics) Measure { return m.DividendChange }, StatusMissing},
		{"dividende prévu absent", func(in *Inputs) { in.Snapshot.ForwardDividend = nil }, func(m Metrics) Measure { return m.DividendChange }, StatusMissing},
		{"prix absent", func(in *Inputs) { in.Snapshot.Price = nil }, func(m Metrics) Measure { return m.PB }, StatusMissing},
		{"dividende du dernier exercice absent", func(in *Inputs) { in.DPS = in.DPS[:4] }, func(m Metrics) Measure { return m.PayoutRatio }, StatusMissing},
		{"actions : année précédente absente", func(in *Inputs) { in.Shares = Series{in.Shares[4]} }, func(m Metrics) Measure { return m.ShareChange }, StatusInsufficient},
		{"actions précédentes nulles", func(in *Inputs) { in.Shares[3].Value = dec("0") }, func(m Metrics) Measure { return m.ShareChange }, StatusMissing},
		{"rendement moyen : fenêtre incomplète", func(in *Inputs) { in.AnnualYield = in.AnnualYield[1:] }, func(m Metrics) Measure { return m.AverageYield }, StatusInsufficient},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			in := fullInputs()
			tt.mutate(&in)
			got := tt.check(byHorizon(ComputeMetrics(in, testOpts()), 5))
			if got.Status != tt.want {
				t.Fatalf("statut %s, attendu %s", got.Status, tt.want)
			}
		})
	}
}

func TestComputeMetricsBuybackYearsAndOverflow(t *testing.T) {
	t.Parallel()
	in := fullInputs()
	opts := testOpts()
	opts.BuybackYears = 5
	if m := byHorizon(ComputeMetrics(in, opts), 5); m.ShareChange.Status != StatusInsufficient {
		t.Fatalf("5 ans : %+v", m.ShareChange)
	}
	opts.BuybackYears = 4
	if m := byHorizon(ComputeMetrics(in, opts), 5); !near(m.ShareChange.Value, 95.0/100.0-1) {
		t.Fatalf("4 ans : %+v", m.ShareChange)
	}
	opts.BuybackYears = 0 // valeur invalide : 1 an
	if m := byHorizon(ComputeMetrics(in, opts), 5); !near(m.ShareChange.Value, 95.0/97.0-1) {
		t.Fatalf("0 an : %+v", m.ShareChange)
	}
	in.Snapshot.Price = decp("1e400")
	m := byHorizon(ComputeMetrics(in, opts), 5)
	if m.PE.Status != StatusMissing || len(m.Warnings) == 0 {
		t.Fatalf("débordement : %+v", m.PE)
	}
}

func TestComputeMetricsStaleWarning(t *testing.T) {
	t.Parallel()
	in := fullInputs()
	in.CurrentYear = 2030
	m := byHorizon(ComputeMetrics(in, testOpts()), 5)
	if len(m.Warnings) == 0 {
		t.Fatal("avertissement attendu")
	}
}

func TestOptionsFromRules(t *testing.T) {
	t.Parallel()
	r, err := DefaultRules()
	if err != nil {
		t.Fatal(err)
	}
	o := OptionsFromRules(r)
	if len(o.Horizons) != 4 || o.TrimMinRates != 5 || o.BuybackYears != 1 {
		t.Fatalf("%+v", o)
	}
}
