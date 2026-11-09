package engine

import (
	"math"
	"testing"

	"github.com/shopspring/decimal"
)

func FuzzExposure(f *testing.F) {
	f.Add(100.0, 0.5, 0.5, 0.0, 0.002, 50.0, 0.0, 0.0, 1.0, 0.01)
	f.Add(0.0, 1.0, 0.0, 0.0, 0.0, 0.0, 0.0, 0.0, 1.0, 0.0)
	f.Fuzz(func(t *testing.T, w1, a1, b1, c1, m1, w2, a2, b2, c2, m2 float64) {
		mk := func(w, a, b, c, m float64) Fund {
			if math.IsNaN(w) || math.IsInf(w, 0) {
				w = 0
			}
			return Fund{
				ID:         "f",
				Weight:     decimal.NewFromFloat(math.Abs(math.Mod(w, 1e9))),
				Allocation: Allocation{EquityCA: a, EquityUS: b, BondCA: c, OtherAssets: 1 - a - b - c},
				MER:        m,
			}
		}
		r, err := Exposure([]Fund{mk(w1, a1, b1, c1, m1), mk(w2, a2, b2, c2, m2)})
		if err != nil {
			return
		}
		if r.Empty {
			if r.Exposure != (Allocation{}) || r.MER != 0 {
				t.Fatalf("résultat vide non nul: %+v", r)
			}
			return
		}
		if math.Abs(r.Exposure.Sum()-1) > 1e-9 || math.IsNaN(r.MER) {
			t.Fatalf("incohérent: %+v", r)
		}
	})
}

func FuzzNormalizeTargets(f *testing.F) {
	f.Add(0.3, 0.3, 0.3, 0.1, 0.0)
	f.Fuzz(func(t *testing.T, a, b, c, d, cash float64) {
		got, err := NormalizeTargets(Allocation{EquityCA: a, EquityUS: b, EquityIntl: c, BondCA: d}, cash)
		if err != nil {
			return
		}
		if math.Abs(got.Sum()+cash-1) > 1e-9 {
			t.Fatalf("total %v", got.Sum()+cash)
		}
	})
}
