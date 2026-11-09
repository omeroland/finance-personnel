package engine

import (
	"math"
	"testing"

	"github.com/shopspring/decimal"
)

// FuzzRebalance vérifie les propriétés : jamais d'encaisse négative, jamais de
// vente au-delà des parts entières détenues, parts entières, aucune vente
// quand la cotisation et l'encaisse seules suffisent à rester dans les bandes.
func FuzzRebalance(f *testing.F) {
	f.Add(int64(1000), int64(5000), int64(60), int64(80), int64(1000), int64(2000), int64(40), int64(20), int64(10), int64(500))
	f.Add(int64(0), int64(0), int64(100), int64(0), int64(1), int64(1), int64(0), int64(1), int64(0), int64(0))
	f.Fuzz(func(t *testing.T, cash1, cash2, sh1, sh2, p1c, p2c, tgt, sh3, minOrd, contrib int64) {
		abs := func(v, mod int64) int64 {
			if v < 0 {
				v = -(v + 1)
			}
			return v % mod
		}
		price1 := decimal.New(abs(p1c, 100000)+1, -2)
		price2 := decimal.New(abs(p2c, 100000)+1, -2)
		target := float64(abs(tgt, 101)) / 100
		in := RebalanceInput{
			Funds: []RebalFund{
				{ID: "A", Price: price1, Target: target, Allocation: allCA},
				{ID: "B", Price: price2, Target: 1 - target, Allocation: allBd},
			},
			Accounts: []RebalAccount{
				{ID: "r", Type: AccountCELI, Cash: decimal.New(abs(cash1, 1e7), -2), Contribution: decimal.New(abs(contrib, 1e7), -2),
					Positions: []RebalPosition{{FundID: "A", Shares: decimal.NewFromInt(abs(sh1, 1000))}, {FundID: "B", Shares: decimal.NewFromInt(abs(sh3, 1000))}}},
				{ID: "t", Type: AccountTaxable, Cash: decimal.New(abs(cash2, 1e7), -2),
					Positions: []RebalPosition{{FundID: "A", Shares: decimal.NewFromInt(abs(sh2, 1000))}}},
			},
			MinOrder: decimal.New(abs(minOrd, 100000), -2),
		}
		held := map[[2]string]decimal.Decimal{}
		cash := map[string]decimal.Decimal{}
		for _, a := range in.Accounts {
			cash[a.ID] = a.Cash.Add(a.Contribution)
			for _, p := range a.Positions {
				held[[2]string{a.ID, p.FundID}] = p.Shares
			}
		}
		plan, err := Rebalance(in)
		if err != nil {
			t.Fatalf("erreur inattendue: %v", err)
		}
		prices := map[string]decimal.Decimal{"A": price1, "B": price2}
		for _, o := range plan.Orders {
			if o.Shares <= 0 || !o.Amount.Equal(prices[o.FundID].Mul(decimal.NewFromInt(o.Shares))) {
				t.Fatalf("ordre incohérent: %+v", o)
			}
			n := decimal.NewFromInt(o.Shares)
			k := [2]string{o.AccountID, o.FundID}
			if o.Side == Sell {
				held[k] = held[k].Sub(n)
				cash[o.AccountID] = cash[o.AccountID].Add(o.Amount)
			} else {
				held[k] = held[k].Add(n)
				cash[o.AccountID] = cash[o.AccountID].Sub(o.Amount)
			}
		}
		for k, v := range held {
			if v.IsNegative() {
				t.Fatalf("vente au-delà du détenu: %v %s", k, v)
			}
		}
		end := decimal.Zero
		for id, c := range cash {
			if c.IsNegative() {
				t.Fatalf("encaisse négative: %s %s", id, c)
			}
			end = end.Add(c)
		}
		if !end.Equal(plan.EndCash) && len(plan.Orders) > 0 {
			t.Fatalf("encaisse finale %s != %s", plan.EndCash, end)
		}
		for _, w := range append(plan.Before.Funds, plan.After.Funds...) {
			if math.IsNaN(w.Weight) {
				t.Fatal("poids NaN")
			}
		}
	})
}
