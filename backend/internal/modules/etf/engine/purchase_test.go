package engine

import (
	"errors"
	"testing"

	"github.com/shopspring/decimal"
)

func pdec(s string) decimal.Decimal { return decimal.RequireFromString(s) }

func checkPlan(t *testing.T, in PurchaseInput, p PurchasePlan) {
	t.Helper()
	sum := p.Cash
	for _, o := range p.Orders {
		if o.Shares <= 0 {
			t.Errorf("parts non positives: %+v", o)
		}
		if !o.Amount.Equal(o.Price.Mul(decimal.NewFromInt(o.Shares))) {
			t.Errorf("montant incohérent: %+v", o)
		}
		sum = sum.Add(o.Amount)
	}
	if !sum.Equal(in.Amount) {
		t.Errorf("ordres + encaisse = %s, attendu %s", sum, in.Amount)
	}
	if p.Cash.IsNegative() {
		t.Errorf("encaisse négative %s", p.Cash)
	}
}

func TestPlanPurchase(t *testing.T) {
	t.Parallel()
	two := []PurchaseFund{{"AAA", 0.5, pdec("30")}, {"BBB", 0.5, pdec("70")}}
	tests := []struct {
		name   string
		in     PurchaseInput
		shares map[string]int64
		cash   string
	}{
		{"partage sans reliquat", PurchaseInput{Amount: pdec("1000"), Funds: []PurchaseFund{{"AAA", 0.5, pdec("50")}, {"BBB", 0.5, pdec("100")}}}, map[string]int64{"AAA": 10, "BBB": 5}, "0"},
		{"reliquat au plus sous-pondéré", PurchaseInput{Amount: pdec("1000"), Funds: two}, map[string]int64{"AAA": 17, "BBB": 7}, "0"},
		{"encaisse cible", PurchaseInput{Amount: pdec("1000"), Funds: []PurchaseFund{{"AAA", 0.8, pdec("100")}}, CashWeight: 0.2}, map[string]int64{"AAA": 8}, "200"},
		{"prix supérieur au montant", PurchaseInput{Amount: pdec("50"), Funds: []PurchaseFund{{"AAA", 1, pdec("100")}}}, nil, "50"},
		{"montant nul", PurchaseInput{Amount: pdec("0"), Funds: two}, nil, "0"},
		{"un seul fonds", PurchaseInput{Amount: pdec("1000"), Funds: []PurchaseFund{{"AAA", 1, pdec("33")}}}, map[string]int64{"AAA": 30}, "10"},
		{"poids nuls", PurchaseInput{Amount: pdec("500"), Funds: []PurchaseFund{{"AAA", 0, pdec("10")}}, CashWeight: 1}, nil, "500"},
		{"minimum d'ordre", PurchaseInput{Amount: pdec("1000"), Funds: []PurchaseFund{{"AAA", 0.95, pdec("10")}, {"BBB", 0.05, pdec("10")}}, MinOrder: pdec("100")}, map[string]int64{"AAA": 95}, "50"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			p, err := PlanPurchase(tc.in)
			if err != nil {
				t.Fatal(err)
			}
			checkPlan(t, tc.in, p)
			got := map[string]int64{}
			for _, o := range p.Orders {
				got[o.Fund] = o.Shares
			}
			if len(got) != len(tc.shares) {
				t.Fatalf("ordres %v, attendu %v", got, tc.shares)
			}
			for k, v := range tc.shares {
				if got[k] != v {
					t.Errorf("%s: %d parts, attendu %d", k, got[k], v)
				}
			}
			if !p.Cash.Equal(pdec(tc.cash)) {
				t.Errorf("encaisse %s, attendu %s", p.Cash, tc.cash)
			}
		})
	}
}

func TestPlanPurchaseErrors(t *testing.T) {
	t.Parallel()
	ok := PurchaseFund{"AAA", 1, pdec("10")}
	tests := []struct {
		name string
		in   PurchaseInput
		want error
	}{
		{"prix nul", PurchaseInput{Amount: pdec("100"), Funds: []PurchaseFund{{"AAA", 1, pdec("0")}}}, ErrPrice},
		{"prix manquant", PurchaseInput{Amount: pdec("100"), Funds: []PurchaseFund{{ID: "AAA", Weight: 1}}}, ErrPrice},
		{"prix négatif", PurchaseInput{Amount: pdec("100"), Funds: []PurchaseFund{{"AAA", 1, pdec("-1")}}}, ErrPrice},
		{"montant négatif", PurchaseInput{Amount: pdec("-1"), Funds: []PurchaseFund{ok}}, ErrAmount},
		{"minimum négatif", PurchaseInput{Amount: pdec("1"), Funds: []PurchaseFund{ok}, MinOrder: pdec("-1")}, ErrAmount},
		{"total différent de 1", PurchaseInput{Amount: pdec("1"), Funds: []PurchaseFund{{"AAA", 0.5, pdec("1")}}}, ErrTargets},
		{"poids négatif", PurchaseInput{Amount: pdec("1"), Funds: []PurchaseFund{{"AAA", -0.5, pdec("1")}, {"BBB", 1.5, pdec("1")}}}, ErrWeight},
		{"doublon", PurchaseInput{Amount: pdec("1"), Funds: []PurchaseFund{{"AAA", 0.5, pdec("1")}, {"AAA", 0.5, pdec("1")}}}, ErrFund},
		{"identifiant vide", PurchaseInput{Amount: pdec("1"), Funds: []PurchaseFund{{"", 1, pdec("1")}}}, ErrFund},
		{"encaisse invalide", PurchaseInput{Amount: pdec("1"), Funds: []PurchaseFund{ok}, CashWeight: 2}, ErrTargets},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if _, err := PlanPurchase(tc.in); !errors.Is(err, tc.want) {
				t.Errorf("erreur %v, attendu %v", err, tc.want)
			}
		})
	}
}

func TestPlanPurchaseDeterministic(t *testing.T) {
	t.Parallel()
	in := PurchaseInput{Amount: pdec("1234.56"), Funds: []PurchaseFund{{"B", 0.4, pdec("41.17")}, {"A", 0.4, pdec("41.17")}, {"C", 0.2, pdec("17.3")}}}
	first, err := PlanPurchase(in)
	if err != nil {
		t.Fatal(err)
	}
	checkPlan(t, in, first)
	for i := 0; i < 20; i++ {
		in.Funds[0], in.Funds[1] = in.Funds[1], in.Funds[0]
		p, _ := PlanPurchase(in)
		if len(p.Orders) != len(first.Orders) || !p.Cash.Equal(first.Cash) {
			t.Fatalf("non déterministe: %+v vs %+v", p, first)
		}
		for j := range p.Orders {
			if p.Orders[j].Fund != first.Orders[j].Fund || p.Orders[j].Shares != first.Orders[j].Shares {
				t.Fatalf("non déterministe: %+v vs %+v", p, first)
			}
		}
	}
}

func FuzzPlanPurchase(f *testing.F) {
	f.Add(int64(100000), 0.5, 0.3, int64(3050), int64(1234), int64(0))
	f.Add(int64(0), 0.5, 0.5, int64(100), int64(100), int64(0))
	f.Add(int64(500), 0.0, 0.0, int64(1), int64(1), int64(100))
	f.Fuzz(func(t *testing.T, cents int64, w1, w2 float64, p1, p2, minOrd int64) {
		in := PurchaseInput{
			Amount:     decimal.New(cents, -2),
			Funds:      []PurchaseFund{{"A", w1, decimal.New(p1, -2)}, {"B", w2, decimal.New(p2, -2)}},
			CashWeight: 1 - w1 - w2,
			MinOrder:   decimal.New(minOrd, -2),
		}
		p, err := PlanPurchase(in)
		if err != nil {
			return
		}
		checkPlan(t, in, p)
		if p.Invested.GreaterThan(in.Amount) {
			t.Fatalf("dépassement: %s > %s", p.Invested, in.Amount)
		}
	})
}
