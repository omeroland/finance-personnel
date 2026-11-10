package engine_test

import (
	"errors"
	"math"
	"testing"
	"time"

	"github.com/shopspring/decimal"

	"github.com/omeroland/finance-privee/backend/internal/modules/etf/engine"
)

func dd(s string) decimal.Decimal { return decimal.RequireFromString(s) }

func day(s string) time.Time {
	t, err := time.Parse(time.DateOnly, s)
	if err != nil {
		panic(err)
	}
	return t
}

func TestCheckTargetsAndWeights(t *testing.T) {
	t.Parallel()
	ok := engine.Allocation{EquityCA: 0.4, EquityUS: 0.4, BondCA: 0.1}
	if err := engine.CheckTargets(ok, 0.1); err != nil {
		t.Fatal(err)
	}
	for name, err := range map[string]error{
		"total court":    engine.CheckTargets(ok, 0.2),
		"cash négatif":   engine.CheckTargets(ok, -0.1),
		"cible > 1":      engine.CheckTargets(engine.Allocation{EquityCA: 1.5}, 0),
		"poids NaN":      engine.CheckWeights([]float64{math.NaN()}, 0),
		"poids + cash":   engine.CheckWeights([]float64{0.5, 0.4}, 0),
		"tolérance 1e-5": engine.CheckWeights([]float64{0.5, 0.49999}, 0),
	} {
		var te *engine.TargetsError
		if !errors.As(err, &te) || !errors.Is(err, engine.ErrTargets) {
			t.Errorf("%s : %v", name, err)
		}
	}
	if err := engine.CheckWeights([]float64{0.6, 0.3}, 0.1); err != nil {
		t.Fatal(err)
	}
	if err := engine.CheckWeights([]float64{0.5, 0.4999995}, 0); err != nil {
		t.Fatalf("écart de 5e-7 toléré: %v", err)
	}
}

func TestDeviation(t *testing.T) {
	t.Parallel()
	d := engine.Deviation(engine.Allocation{EquityCA: 0.31, BondCA: 0.1}, engine.Allocation{EquityCA: 0.22, BondCA: 0.2})
	if math.Abs(d.EquityCA-0.09) > 1e-12 || math.Abs(d.BondCA+0.1) > 1e-12 {
		t.Fatalf("%+v", d)
	}
}

func TestExposureOfHoldings(t *testing.T) {
	t.Parallel()
	mer := 0.002
	res, err := engine.ExposureOfHoldings([]engine.Holding{
		{Symbol: "A", Quantity: dd("10"), Price: dd("50"), Allocation: engine.Allocation{EquityCA: 1}, MER: &mer},
		{Symbol: "B", Quantity: dd("10"), Price: dd("50"), Allocation: engine.Allocation{BondCA: 1}},
	})
	if err != nil || res.Empty || !res.Value.Equal(dd("1000")) || res.Exposure.EquityCA != 0.5 || res.Exposure.BondCA != 0.5 {
		t.Fatalf("%+v %v", res, err)
	}
	if !res.HasMER || res.MER != mer { // RFG pondéré sur les fonds qui en ont un
		t.Fatalf("RFG: %+v", res)
	}
	none, err := engine.ExposureOfHoldings([]engine.Holding{{Symbol: "B", Quantity: dd("1"), Price: dd("1"), Allocation: engine.Allocation{BondCA: 1}}})
	if err != nil || none.HasMER {
		t.Fatalf("sans RFG: %+v %v", none, err)
	}
	if e, err := engine.ExposureOfHoldings(nil); err != nil || !e.Empty {
		t.Fatalf("vide: %+v %v", e, err)
	}
	if _, err := engine.ExposureOfHoldings([]engine.Holding{{Symbol: "X", Quantity: dd("1"), Price: dd("1"), Allocation: engine.Allocation{EquityCA: 0.5}}}); !errors.Is(err, engine.ErrAllocation) {
		t.Fatalf("répartition incomplète: %v", err)
	}
}

func TestAccountTimelines(t *testing.T) {
	t.Parallel()
	entries := []engine.Entry{
		{Date: day("2026-01-05"), Type: engine.EntryContribution, Amount: dd("1000")},
		{Date: day("2026-01-10"), Type: engine.EntryBuy, Symbol: "X", Quantity: dd("10"), Price: dd("50"), Fee: dd("0")},
		{Date: day("2026-02-10"), Type: engine.EntryBuy, Symbol: "X", Quantity: dd("2"), Price: dd("55")},
		{Date: day("2026-02-15"), Type: engine.EntryDividend, Amount: dd("5")},
		{Date: day("2026-02-20"), Type: engine.EntrySplit, Symbol: "X", SplitRatio: dd("2")},
		{Date: day("2026-03-01"), Type: engine.EntryFee, Amount: dd("1")},
		{Date: day("2026-03-05"), Type: engine.EntryWithdrawal, Amount: dd("100")},
	}
	other := []engine.Entry{
		{Date: day("2026-01-03"), Type: engine.EntryContribution, Amount: dd("200")},
		{Date: day("2026-03-20"), Type: engine.EntryTransfer, Amount: dd("50")},
		{Date: day("2026-03-21"), Type: engine.EntryTransfer, Amount: dd("50"), Incoming: true},
		{Date: day("2026-03-22"), Type: engine.EntryBuy, Symbol: "Y", Quantity: dd("1"), Price: dd("10")},
		{Date: day("2026-03-23"), Type: engine.EntryDRIP, Symbol: "Y", Quantity: dd("1"), Price: dd("11")},
		{Date: day("2026-03-24"), Type: engine.EntrySell, Symbol: "Y", Quantity: dd("1"), Price: dd("12")},
		{Date: day("2026-03-25"), Type: engine.EntryTransfer, Symbol: "Y", Quantity: dd("1"), Amount: dd("0")},
		{Date: day("2026-03-26"), Type: engine.EntryTransfer, Symbol: "Y", Quantity: dd("1"), Incoming: true},
		{Date: day("2026-03-27"), Type: engine.EntryDividend, Amount: dd("1"), Incoming: true}, // sans effet
	}
	accts := []engine.LedgerAccount{{ID: "A", Entries: entries}, {ID: "B", Entries: other}}
	first, ok := engine.FirstEntryDate(accts)
	if !ok || !first.Equal(day("2026-01-03")) {
		t.Fatalf("première date: %v %v", first, ok)
	}
	if _, ok := engine.FirstEntryDate(nil); ok {
		t.Fatal("aucune écriture")
	}
	tl, err := engine.AccountTimelines(accts, day("2026-01-01"), day("2026-03-31"))
	if err != nil {
		t.Fatal(err)
	}
	a, b := tl["A"], tl["B"]
	if len(a) != len(b) {
		t.Fatalf("dates communes : %d et %d points", len(a), len(b))
	}
	for i := range a {
		if !a[i].Date.Equal(b[i].Date) || (i > 0 && !a[i].Date.After(a[i-1].Date)) {
			t.Fatalf("point %d : dates non communes ou non croissantes", i)
		}
	}
	// Compte A : valeur avant flux à la cotisation du 5 janvier = 0 ; à la fin de mars,
	// 1000 − 500 (achat) − 110 (achat) + 5 − 1 − 100 = 294 d'encaisse + 24 parts à 27,5 (après le fractionnement).
	last := a[len(a)-1]
	if !last.Date.Equal(day("2026-03-31")) || !last.Value.Equal(dd("294").Add(dd("24").Mul(dd("27.5")))) || !last.Flow.IsZero() {
		t.Fatalf("dernier point de A : %+v", last)
	}
	var sawContribution bool
	for _, p := range a {
		if p.Date.Equal(day("2026-01-05")) {
			sawContribution = p.Flow.Equal(dd("1000")) && p.Value.IsZero()
		}
	}
	if !sawContribution {
		t.Fatalf("point de la cotisation : %+v", a)
	}
	if _, err := engine.TWR(a); err != nil {
		t.Fatal(err)
	}
	if _, err := engine.MergeValuations([][]engine.Valuation{a, b}); err != nil {
		t.Fatalf("les comptes se consolident : %v", err)
	}
	// from après les premières écritures : l'état de départ compte, les flux antérieurs non.
	late, err := engine.AccountTimelines(accts[:1], day("2026-02-01"), day("2026-02-28"))
	if err != nil {
		t.Fatal(err)
	}
	if p := late["A"][0]; !p.Date.Equal(day("2026-02-01")) || !p.Flow.IsZero() || !p.Value.Equal(dd("500").Add(dd("500"))) {
		t.Fatalf("départ tardif : %+v", p)
	}
	if _, err := engine.AccountTimelines(accts, day("2026-03-31"), day("2026-01-01")); !errors.Is(err, engine.ErrNoPeriod) {
		t.Fatalf("période inversée : %v", err)
	}
	// Mois de décembre à janvier.
	if tl, err := engine.AccountTimelines(accts[:1], day("2025-12-15"), day("2026-01-31")); err != nil || len(tl["A"]) < 3 {
		t.Fatalf("passage d'année : %v %v", tl, err)
	}
}
