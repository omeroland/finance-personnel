package engine

import (
	"errors"
	"math"
	"testing"

	"github.com/shopspring/decimal"
)

func dec(s string) decimal.Decimal { return decimal.RequireFromString(s) }

func near(a, b float64) bool { return math.Abs(a-b) < 1e-9 }

var (
	equityCA = Allocation{EquityCA: 1}
	equityUS = Allocation{EquityUS: 1}
	bondCA   = Allocation{BondCA: 1}
	gold     = Allocation{OtherAssets: 1}
	balanced = Allocation{EquityUS: 0.4, EquityIntl: 0.2, BondCA: 0.4}
)

func TestExposure(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name  string
		funds []Fund
		want  Allocation
		mer   float64
		empty bool
	}{
		{"nil", nil, Allocation{}, 0, true},
		{"vide", []Fund{}, Allocation{}, 0, true},
		{"poids tous nuls", []Fund{{ID: "a", Allocation: equityCA, MER: 0.01}, {ID: "b", Weight: dec("0"), Allocation: bondCA}}, Allocation{}, 0, true},
		{"un fonds", []Fund{{ID: "a", Weight: dec("100"), Allocation: balanced, MER: 0.002}}, balanced, 0.002, false},
		{"deux fonds 75/25", []Fund{
			{ID: "a", Weight: dec("7500"), Allocation: equityUS, MER: 0.002},
			{ID: "b", Weight: dec("2500"), Allocation: bondCA, MER: 0.006},
		}, Allocation{EquityUS: 0.75, BondCA: 0.25}, 0.003, false},
		{"or ne disparait pas", []Fund{
			{ID: "a", Weight: dec("50"), Allocation: equityCA},
			{ID: "or", Weight: dec("50"), Allocation: gold, MER: 0.004},
		}, Allocation{EquityCA: 0.5, OtherAssets: 0.5}, 0.002, false},
		{"poids nul ignore", []Fund{
			{ID: "a", Weight: dec("10"), Allocation: equityCA, MER: 0.01},
			{ID: "z", Weight: dec("0"), Allocation: gold, MER: 0.5},
		}, equityCA, 0.01, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			r, err := Exposure(tt.funds)
			if err != nil {
				t.Fatal(err)
			}
			if r.Empty != tt.empty {
				t.Fatalf("Empty = %v", r.Empty)
			}
			for _, c := range Classes {
				if !near(r.Exposure.Get(c), tt.want.Get(c)) {
					t.Errorf("%s = %v, attendu %v", c, r.Exposure.Get(c), tt.want.Get(c))
				}
			}
			if !near(r.MER, tt.mer) {
				t.Errorf("MER = %v, attendu %v", r.MER, tt.mer)
			}
			if !tt.empty && !near(r.Exposure.Sum(), 1) {
				t.Errorf("somme = %v", r.Exposure.Sum())
			}
		})
	}
}

func TestExposureErrors(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name  string
		funds []Fund
		is    error
	}{
		{"somme 0.9", []Fund{{ID: "a", Weight: dec("1"), Allocation: Allocation{EquityCA: 0.9}}}, ErrAllocation},
		{"somme 1.1", []Fund{{ID: "a", Weight: dec("1"), Allocation: Allocation{EquityCA: 1, BondCA: 0.1}}}, ErrAllocation},
		{"part negative", []Fund{{ID: "a", Weight: dec("1"), Allocation: Allocation{EquityCA: 1.5, BondCA: -0.5}}}, ErrAllocation},
		{"NaN", []Fund{{ID: "a", Weight: dec("1"), Allocation: Allocation{EquityCA: math.NaN()}}}, ErrAllocation},
		{"fonds a poids nul invalide", []Fund{{ID: "a", Allocation: Allocation{}}}, ErrAllocation},
		{"poids negatif", []Fund{{ID: "a", Weight: dec("-1"), Allocation: equityCA}}, ErrWeight},
		{"RFG negatif", []Fund{{ID: "a", Weight: dec("1"), Allocation: equityCA, MER: -0.1}}, ErrMER},
		{"RFG NaN", []Fund{{ID: "a", Weight: dec("1"), Allocation: equityCA, MER: math.NaN()}}, ErrMER},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			_, err := Exposure(tt.funds)
			if !errors.Is(err, tt.is) {
				t.Fatalf("err = %v, attendu %v", err, tt.is)
			}
		})
	}
	var ae *AllocationError
	_, err := Exposure([]Fund{{ID: "xyz", Weight: dec("1"), Allocation: Allocation{EquityCA: 0.9}}})
	if !errors.As(err, &ae) || ae.Fund != "xyz" {
		t.Fatalf("erreur typée attendue, reçu %v", err)
	}
}

func TestCompleteAllocation(t *testing.T) {
	t.Parallel()
	got, err := CompleteAllocation(Allocation{EquityUS: 0.6, BondCA: 0.3})
	if err != nil {
		t.Fatal(err)
	}
	if !near(got.OtherAssets, 0.1) || !near(got.Sum(), 1) {
		t.Fatalf("got %+v", got)
	}
	// Le fonds complété garde sa part dans le total.
	r, err := Exposure([]Fund{{ID: "a", Weight: dec("100"), Allocation: got}})
	if err != nil || !near(r.Exposure.OtherAssets, 0.1) || !near(r.Exposure.Sum(), 1) {
		t.Fatalf("r=%+v err=%v", r, err)
	}
	// Totalement non classé : tout dans autres actifs.
	got, err = CompleteAllocation(Allocation{})
	if err != nil || got.OtherAssets != 1 {
		t.Fatalf("got %+v err=%v", got, err)
	}
	if _, err := CompleteAllocation(Allocation{EquityCA: 0.8, BondCA: 0.4}); !errors.Is(err, ErrAllocation) {
		t.Fatalf("err = %v", err)
	}
	if _, err := CompleteAllocation(Allocation{EquityCA: -0.1}); !errors.Is(err, ErrAllocation) {
		t.Fatalf("err = %v", err)
	}
}

func TestNormalizeTargets(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name    string
		targets Allocation
		cash    float64
		wantSum float64
		bad     bool
	}{
		{"100 %", Allocation{EquityCA: 0.275 * 0.7, EquityUS: 0.435 * 0.7, EquityIntl: 0.29 * 0.7, BondCA: 0.3}, 0, 1, false},
		{"avec encaisse", Allocation{EquityUS: 0.6, BondCA: 0.35}, 0.05, 0.95, false},
		{"dans la tolerance", Allocation{EquityUS: 0.6, BondCA: 0.40005}, 0, 1, false},
		{"99 %", Allocation{EquityUS: 0.6, BondCA: 0.39}, 0, 0, true},
		{"101 %", Allocation{EquityUS: 0.6, BondCA: 0.41}, 0, 0, true},
		{"encaisse non comptee", Allocation{EquityUS: 0.6, BondCA: 0.4}, 0.05, 0, true},
		{"part negative", Allocation{EquityUS: 1.2, BondCA: -0.2}, 0, 0, true},
		{"encaisse negative", Allocation{EquityUS: 1}, -0.1, 0, true},
		{"NaN", Allocation{EquityUS: math.NaN()}, 0, 0, true},
		{"tout en encaisse", Allocation{}, 1, 0, false},
		{"rien", Allocation{}, 0, 0, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got, err := NormalizeTargets(tt.targets, tt.cash)
			if tt.bad {
				var te *TargetsError
				if !errors.Is(err, ErrTargets) || !errors.As(err, &te) {
					t.Fatalf("erreur typée attendue, reçu %v", err)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			if !near(got.Sum(), tt.wantSum) {
				t.Errorf("somme = %v, attendu %v", got.Sum(), tt.wantSum)
			}
		})
	}
}

// Propriété : pour toute entrée valide non vide, la somme des expositions vaut 1
// et le RFG pondéré reste entre le min et le max des RFG des fonds pesants.
func TestExposureProperty(t *testing.T) {
	t.Parallel()
	seed := uint64(42)
	next := func() float64 { // xorshift déterministe
		seed ^= seed << 13
		seed ^= seed >> 7
		seed ^= seed << 17
		return float64(seed%1_000_000) / 1_000_000
	}
	for range 500 {
		n := int(next()*8) + 1
		funds := make([]Fund, n)
		lo, hi := math.Inf(1), math.Inf(-1)
		anyW := false
		for i := range funds {
			var p [6]float64
			s := 0.0
			for j := range p {
				p[j] = next()
				s += p[j]
			}
			for j := range p {
				p[j] /= s
			}
			w := decimal.NewFromFloat(next() * 1000).Round(2)
			if next() < 0.2 {
				w = decimal.Zero
			}
			m := next() * 0.02
			if w.IsPositive() {
				anyW = true
				lo, hi = math.Min(lo, m), math.Max(hi, m)
			}
			funds[i] = Fund{ID: "f", Weight: w, Allocation: fromParts(p), MER: m}
		}
		r, err := Exposure(funds)
		if err != nil {
			t.Fatal(err)
		}
		if !anyW {
			if !r.Empty {
				t.Fatal("résultat vide attendu")
			}
			continue
		}
		if r.Empty || !near(r.Exposure.Sum(), 1) {
			t.Fatalf("somme = %v", r.Exposure.Sum())
		}
		if r.MER < lo-1e-12 || r.MER > hi+1e-12 {
			t.Fatalf("RFG %v hors [%v, %v]", r.MER, lo, hi)
		}
	}
}
