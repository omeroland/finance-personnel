package engine

import (
	"math"
	"testing"

	"github.com/shopspring/decimal"
)

func clampExp(e int8) int32 { return int32(e) % 20 }

func FuzzGrowth(f *testing.F) {
	f.Add(int64(100), int8(-2), int64(110), int8(-2))
	f.Add(int64(0), int8(0), int64(5), int8(0))
	f.Add(int64(-100), int8(0), int64(50), int8(0))
	f.Add(int64(math.MaxInt64), int8(19), int64(math.MinInt64), int8(-19))
	f.Fuzz(func(t *testing.T, pm int64, pe int8, cm int64, ce int8) {
		prev := decimal.New(pm, clampExp(pe))
		cur := decimal.New(cm, clampExp(ce))
		g, err := Growth(prev, cur)
		if err != nil {
			return
		}
		if math.IsNaN(g) || math.IsInf(g, 0) {
			t.Fatalf("sortie non finie : %v", g)
		}
	})
}

func FuzzTrimmedMean(f *testing.F) {
	f.Add(0.1, 0.2, 0.3, 0.4, 0.5, 5)
	f.Add(-1.0, 1e308, 1e308, 0.0, 3.0, 3)
	f.Add(math.NaN(), 1.0, 2.0, 3.0, 4.0, 0)
	f.Fuzz(func(t *testing.T, a, b, c, d, e float64, minCount int) {
		xs := []float64{a, b, c, d, e}
		for n := 0; n <= len(xs); n++ {
			m, err := TrimmedMean(xs[:n], minCount)
			if err != nil {
				continue
			}
			if math.IsNaN(m) || math.IsInf(m, 0) {
				t.Fatalf("sortie non finie : %v", m)
			}
		}
	})
}

func FuzzComputeMetrics(f *testing.F) {
	f.Add(int64(100), int64(110), int64(0), int64(121), int64(5), int8(2))
	f.Fuzz(func(t *testing.T, a, b, c, d, price int64, gap int8) {
		mk := func(vals ...int64) Series {
			var s Series
			y := 2000
			for i, v := range vals {
				y++
				if i == 2 {
					y += int(gap) % 3
				}
				s = append(s, Point{Year: y, Value: decimal.New(v, -2)})
			}
			return s
		}
		p := decimal.New(price, 0)
		in := Inputs{
			EPS: mk(a, b, c, d), DPS: mk(d, c, b, a), AnnualYield: mk(a, b, c, d), Shares: mk(a, b, c, d),
			Snapshot: Snapshot{Price: &p, ForwardDividend: &p, LongTermDebt: &p, Equity: &p, BookValuePerShare: &p, FX: &p},
		}
		for _, m := range ComputeMetrics(in, Options{Horizons: []int{2, 3, 4}, TrimMinRates: 5, BuybackYears: 1}) {
			for _, x := range []Measure{m.EPSGrowth, m.DPSGrowth, m.PayoutRatio, m.DebtToEquity, m.DividendChange, m.ShareChange, m.PE, m.PB, m.CurrentYield, m.AverageYield} {
				if x.Ok() && (math.IsNaN(x.Value) || math.IsInf(x.Value, 0)) {
					t.Fatalf("mesure non finie : %+v", m)
				}
			}
		}
	})
}
