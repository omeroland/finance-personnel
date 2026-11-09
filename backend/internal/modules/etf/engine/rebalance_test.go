package engine

import (
	"errors"
	"reflect"
	"testing"

	"github.com/shopspring/decimal"
)

func d(s string) decimal.Decimal { return decimal.RequireFromString(s) }

var (
	allCA = Allocation{EquityCA: 1}
	allUS = Allocation{EquityUS: 1}
	allBd = Allocation{BondCA: 1}
)

func twoFunds() []RebalFund {
	return []RebalFund{
		{ID: "XEQT", Price: d("10"), Target: 0.6, Allocation: allCA},
		{ID: "XBB", Price: d("20"), Target: 0.4, Allocation: allBd},
	}
}

func pos(id, shares string) RebalPosition { return RebalPosition{FundID: id, Shares: d(shares)} }

func TestRebalanceInBandsEmptyPlan(t *testing.T) {
	t.Parallel()
	// 600 en XEQT, 400 en XBB : exactement sur la cible.
	p, err := Rebalance(RebalanceInput{
		Funds:    twoFunds(),
		Accounts: []RebalAccount{{ID: "a", Type: AccountCELI, Positions: []RebalPosition{pos("XEQT", "60"), pos("XBB", "20")}}},
	})
	if err != nil || len(p.Orders) != 0 || len(p.OutOfBand) != 0 {
		t.Fatalf("plan vide attendu: %+v %v", p, err)
	}
}

func TestRebalanceContributionBuysNoSell(t *testing.T) {
	t.Parallel()
	// Cible 60/40, détenu 500/500, cotisation 200 : total 1200, cibles 720/480.
	// Déficits 220 (XEQT) / -20 : tout va à XEQT, rien n'est vendu.
	p, err := Rebalance(RebalanceInput{
		Funds: twoFunds(),
		Accounts: []RebalAccount{{ID: "a", Type: AccountCELI, Contribution: d("200"),
			Positions: []RebalPosition{pos("XEQT", "50"), pos("XBB", "25")}}},
	})
	if err != nil {
		t.Fatal(err)
	}
	for _, o := range p.Orders {
		if o.Side == Sell {
			t.Fatalf("vente inattendue: %+v", o)
		}
	}
	if len(p.Orders) != 1 || p.Orders[0].FundID != "XEQT" || p.Orders[0].Shares != 20 || !p.Orders[0].Amount.Equal(d("200")) {
		t.Fatalf("ordres: %+v", p.Orders)
	}
	if !p.EndCash.IsZero() {
		t.Fatalf("encaisse finale: %s", p.EndCash)
	}
}

func TestRebalanceSellsRegisteredFirst(t *testing.T) {
	t.Parallel()
	// XEQT pèse 800/1000 (cible 600, bande haute 720) : vente de 20 parts, puis rachat de XBB.
	p, err := Rebalance(RebalanceInput{
		Funds: twoFunds(),
		Accounts: []RebalAccount{
			{ID: "a-taxable", Type: AccountTaxable, Positions: []RebalPosition{pos("XEQT", "40")}},
			{ID: "z-celi", Type: AccountCELI, Positions: []RebalPosition{pos("XEQT", "40"), pos("XBB", "10")}},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	var sells, buys []Order
	for _, o := range p.Orders {
		if o.Side == Sell {
			sells = append(sells, o)
		} else {
			buys = append(buys, o)
		}
	}
	if len(sells) != 1 || sells[0].AccountID != "z-celi" || sells[0].Shares != 20 {
		t.Fatalf("ventes: %+v", sells)
	}
	if len(buys) != 1 || buys[0].FundID != "XBB" || buys[0].AccountID != "z-celi" || buys[0].Shares != 10 {
		t.Fatalf("achats: %+v", buys)
	}
	if len(p.OutOfBand) == 0 {
		t.Fatal("hors bande attendu")
	}
	for _, w := range p.After.Funds {
		if w.Weight < w.Target*0.8-1e-9 || w.Weight > w.Target*1.2+1e-9 {
			t.Fatalf("encore hors bande: %+v", w)
		}
	}
}

func TestRebalanceSellsTaxableWhenRegisteredShort(t *testing.T) {
	t.Parallel()
	p, err := Rebalance(RebalanceInput{
		Funds: twoFunds(),
		Accounts: []RebalAccount{
			{ID: "celi", Type: AccountCELI, Positions: []RebalPosition{pos("XEQT", "5"), pos("XBB", "10")}},
			{ID: "tax", Type: AccountTaxable, Positions: []RebalPosition{pos("XEQT", "70")}},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	got := map[string]int64{}
	for _, o := range p.Orders {
		if o.Side == Sell {
			got[o.AccountID] += o.Shares
		}
	}
	if got["celi"] != 5 || got["tax"] == 0 {
		t.Fatalf("ventes par compte: %v", got)
	}
}

func TestRebalanceCashTargetBand(t *testing.T) {
	t.Parallel()
	funds := []RebalFund{{ID: "A", Price: d("10"), Target: 0.9, Allocation: allCA}}
	in := RebalanceInput{
		Funds: funds, CashTarget: 0.1,
		Accounts: []RebalAccount{{ID: "a", Type: AccountCELI, Cash: d("140"), Positions: []RebalPosition{pos("A", "86")}}},
	}
	// encaisse 140/1000 = 14 % <= 15 % : dans la bande, plan vide.
	p, _ := Rebalance(in)
	if len(p.Orders) != 0 {
		t.Fatalf("plan vide attendu: %+v", p.Orders)
	}
	// encaisse 200/1000 = 20 % > 15 % : achat jusqu'à la cible d'encaisse.
	in.Accounts[0].Cash = d("200")
	in.Accounts[0].Positions = []RebalPosition{pos("A", "80")}
	p, _ = Rebalance(in)
	if len(p.Orders) != 1 || p.Orders[0].Shares != 10 || !p.EndCash.Equal(d("100")) {
		t.Fatalf("plan: %+v cash %s", p.Orders, p.EndCash)
	}
	// bande d'encaisse élargie à 15 points : dans la bande.
	in.Bands = &Bands{Relative: 0.2, CashPoints: 0.15}
	p, _ = Rebalance(in)
	if len(p.Orders) != 0 {
		t.Fatalf("bande configurable ignorée: %+v", p.Orders)
	}
}

func TestRebalanceConfigurableRelativeBand(t *testing.T) {
	t.Parallel()
	in := RebalanceInput{
		Funds:    twoFunds(),
		Accounts: []RebalAccount{{ID: "a", Type: AccountCELI, Positions: []RebalPosition{pos("XEQT", "65"), pos("XBB", "17")}}},
	}
	// 650/990 = 65,7 % vs 60 % : écart relatif 9 % ; dans ±20 %, hors ±5 %.
	if p, _ := Rebalance(in); len(p.Orders) != 0 {
		t.Fatalf("±20 %%: plan vide attendu: %+v", p.Orders)
	}
	in.Bands = &Bands{Relative: 0.05, CashPoints: 0.05}
	p, err := Rebalance(in)
	if err != nil || len(p.Orders) == 0 {
		t.Fatalf("±5 %%: ordres attendus: %+v %v", p, err)
	}
}

func TestRebalanceMinOrder(t *testing.T) {
	t.Parallel()
	in := RebalanceInput{
		Funds:    twoFunds(),
		Accounts: []RebalAccount{{ID: "a", Type: AccountCELI, Contribution: d("1000"), Positions: []RebalPosition{pos("XEQT", "60"), pos("XBB", "20")}}},
		MinOrder: d("500"),
	}
	// Cotisation 1000 : 600 XEQT / 400 XBB ; l'achat de 400 est écarté.
	p, err := Rebalance(in)
	if err != nil {
		t.Fatal(err)
	}
	if len(p.Orders) != 1 || p.Orders[0].FundID != "XEQT" {
		t.Fatalf("ordres: %+v", p.Orders)
	}
	if len(p.Skipped) != 1 || p.Skipped[0].FundID != "XBB" || p.Skipped[0].Reason != SkipBelowMinimum {
		t.Fatalf("écartés: %+v", p.Skipped)
	}
	if !p.EndCash.Equal(d("400")) {
		t.Fatalf("encaisse: %s", p.EndCash)
	}
}

func TestRebalanceMinOrderSell(t *testing.T) {
	t.Parallel()
	p, err := Rebalance(RebalanceInput{
		Funds:    twoFunds(),
		Accounts: []RebalAccount{{ID: "a", Type: AccountCELI, Positions: []RebalPosition{pos("XEQT", "80"), pos("XBB", "10")}}},
		MinOrder: d("1000"),
	})
	if err != nil || len(p.Orders) != 0 || len(p.Skipped) == 0 {
		t.Fatalf("plan: %+v %v", p, err)
	}
}

func TestRebalanceWholeSharesAndRemainder(t *testing.T) {
	t.Parallel()
	funds := []RebalFund{
		{ID: "A", Price: d("30"), Target: 0.5, Allocation: allCA},
		{ID: "B", Price: d("70"), Target: 0.5, Allocation: allUS},
	}
	p, err := Rebalance(RebalanceInput{Funds: funds, Accounts: []RebalAccount{{ID: "a", Type: AccountCELI, Cash: d("1000")}}})
	if err != nil {
		t.Fatal(err)
	}
	spent := decimal.Zero
	for _, o := range p.Orders {
		spent = spent.Add(o.Amount)
		if !o.Amount.Equal(o.Price.Mul(decimal.NewFromInt(o.Shares))) {
			t.Fatalf("montant incohérent: %+v", o)
		}
	}
	if !p.EndCash.Add(spent).Equal(d("1000")) || p.EndCash.IsNegative() {
		t.Fatalf("encaisse %s dépensé %s", p.EndCash, spent)
	}
	// 500 chacun : 16 parts de A (480), 7 de B (490) ; reste 30 -> une part de A.
	if len(p.Orders) != 2 || p.Orders[0].Shares != 17 || p.Orders[1].Shares != 7 {
		t.Fatalf("ordres: %+v", p.Orders)
	}
}

func TestRebalanceErrors(t *testing.T) {
	t.Parallel()
	acc := []RebalAccount{{ID: "a", Type: AccountCELI, Cash: d("100")}}
	bad := twoFunds()
	bad[0].Price = decimal.Zero
	tests := []struct {
		name string
		in   RebalanceInput
		is   error
	}{
		{"prix nul", RebalanceInput{Funds: bad, Accounts: acc}, ErrPrice},
		{"cibles", RebalanceInput{Funds: []RebalFund{{ID: "A", Price: d("1"), Target: 0.5, Allocation: allCA}}, Accounts: acc}, ErrTargets},
		{"fonds inconnu", RebalanceInput{Funds: twoFunds(), Accounts: []RebalAccount{{ID: "a", Type: AccountCELI, Positions: []RebalPosition{pos("ZZZ", "1")}}}}, ErrRebalanceInput},
		{"type inconnu", RebalanceInput{Funds: twoFunds(), Accounts: []RebalAccount{{ID: "a", Type: "x"}}}, ErrRebalanceInput},
		{"parts négatives", RebalanceInput{Funds: twoFunds(), Accounts: []RebalAccount{{ID: "a", Type: AccountCELI, Positions: []RebalPosition{pos("XBB", "-1")}}}}, ErrRebalanceInput},
		{"encaisse négative", RebalanceInput{Funds: twoFunds(), Accounts: []RebalAccount{{ID: "a", Type: AccountCELI, Cash: d("-1")}}}, ErrRebalanceInput},
		{"compte en double", RebalanceInput{Funds: twoFunds(), Accounts: []RebalAccount{{ID: "a", Type: AccountCELI}, {ID: "a", Type: AccountCELI}}}, ErrRebalanceInput},
		{"fonds en double", RebalanceInput{Funds: append(twoFunds(), twoFunds()[0]), Accounts: acc}, ErrRebalanceInput},
		{"bande invalide", RebalanceInput{Funds: twoFunds(), Accounts: acc, Bands: &Bands{Relative: -1}}, ErrRebalanceInput},
		{"minimum négatif", RebalanceInput{Funds: twoFunds(), Accounts: acc, MinOrder: d("-1")}, ErrRebalanceInput},
		{"encaisse cible", RebalanceInput{Funds: twoFunds(), Accounts: acc, CashTarget: 2}, ErrTargets},
		{"cible de fonds", RebalanceInput{Funds: []RebalFund{{ID: "A", Price: d("1"), Target: 2, Allocation: allCA}}, Accounts: acc}, ErrTargets},
		{"répartition", RebalanceInput{Funds: []RebalFund{{ID: "A", Price: d("1"), Target: 1, Allocation: Allocation{}}}, Accounts: acc}, ErrAllocation},
		{"cibles de classes", RebalanceInput{Funds: twoFunds(), Accounts: acc, ClassTargets: &Allocation{EquityCA: 0.5}}, ErrTargets},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if _, err := Rebalance(tc.in); !errors.Is(err, tc.is) {
				t.Fatalf("err = %v, attendu %v", err, tc.is)
			}
		})
	}
	var pe *PriceError
	_, err := Rebalance(RebalanceInput{Funds: bad, Accounts: acc})
	if !errors.As(err, &pe) || pe.Fund != "XEQT" || pe.Error() == "" {
		t.Fatalf("PriceError attendue: %v", err)
	}
}

func TestRebalanceEmpty(t *testing.T) {
	t.Parallel()
	for _, in := range []RebalanceInput{
		{},
		{Funds: twoFunds()},
		{Funds: twoFunds(), Accounts: []RebalAccount{{ID: "a", Type: AccountCELI}}},
	} {
		p, err := Rebalance(in)
		if err != nil || len(p.Orders) != 0 {
			t.Fatalf("plan vide attendu: %+v %v", p, err)
		}
	}
}

func TestRebalanceClassTargetsAndDeterminism(t *testing.T) {
	t.Parallel()
	ct := Allocation{EquityCA: 0.6, BondCA: 0.4}
	in := RebalanceInput{
		Funds: twoFunds(), ClassTargets: &ct,
		Accounts: []RebalAccount{
			{ID: "b", Type: AccountTaxable, Cash: d("123.45"), Positions: []RebalPosition{pos("XEQT", "30")}},
			{ID: "a", Type: AccountREER, Contribution: d("300"), Positions: []RebalPosition{pos("XBB", "9.5")}},
		},
	}
	first, err := Rebalance(in)
	if err != nil {
		t.Fatal(err)
	}
	for range 20 {
		again, _ := Rebalance(in)
		if !reflect.DeepEqual(first, again) {
			t.Fatal("plan non déterministe")
		}
	}
	hasClass := false
	for _, b := range first.OutOfBand {
		if b.Kind == "class" {
			hasClass = true
		}
	}
	if !hasClass {
		t.Fatalf("classe hors bande attendue: %+v", first.OutOfBand)
	}
}

func TestAccountTypeAndDefaults(t *testing.T) {
	t.Parallel()
	for _, ty := range []AccountType{AccountCELI, AccountREER, AccountCELIAPP, AccountREEE} {
		if !ty.Registered() {
			t.Fatal(ty)
		}
	}
	if AccountTaxable.Registered() || DefaultBands().Relative != 0.20 || DefaultBands().CashPoints != 0.05 {
		t.Fatal("défauts")
	}
}
