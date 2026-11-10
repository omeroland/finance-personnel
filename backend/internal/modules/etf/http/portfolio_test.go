package httpapi_test

import (
	"bytes"
	"context"
	"math"
	"strings"
	"testing"

	"github.com/google/uuid"

	"github.com/omeroland/finance-privee/backend/internal/api/gen"
	httpapi "github.com/omeroland/finance-privee/backend/internal/modules/etf/http"
)

// Routes du portefeuille servies par etf/http (P2-11).
func (s srv) ListModels(ctx context.Context, r gen.ListModelsRequestObject) (gen.ListModelsResponseObject, error) {
	return s.h.ListModels(ctx, r)
}

func (s srv) CreateModel(ctx context.Context, r gen.CreateModelRequestObject) (gen.CreateModelResponseObject, error) {
	return s.h.CreateModel(ctx, r)
}

func (s srv) UpdateModel(ctx context.Context, r gen.UpdateModelRequestObject) (gen.UpdateModelResponseObject, error) {
	return s.h.UpdateModel(ctx, r)
}

func (s srv) CreatePurchasePlan(ctx context.Context, r gen.CreatePurchasePlanRequestObject) (gen.CreatePurchasePlanResponseObject, error) {
	return s.h.CreatePurchasePlan(ctx, r)
}

func (s srv) CreateRebalancePlan(ctx context.Context, r gen.CreateRebalancePlanRequestObject) (gen.CreateRebalancePlanResponseObject, error) {
	return s.h.CreateRebalancePlan(ctx, r)
}

func (s srv) GetExposure(ctx context.Context, r gen.GetExposureRequestObject) (gen.GetExposureResponseObject, error) {
	return s.h.GetExposure(ctx, r)
}

func (s srv) GetPerformance(ctx context.Context, r gen.GetPerformanceRequestObject) (gen.GetPerformanceResponseObject, error) {
	return s.h.GetPerformance(ctx, r)
}

func tradeTx(typ gen.TransactionType, date, sym, qty, price string) gen.TransactionCreate {
	return gen.TransactionCreate{Type: typ, TradeDate: dateD(date), Currency: gen.CAD, Symbol: &sym, Quantity: &qty, Price: &price}
}

func f64(v float64) *float64 { return &v }

// modelBody : 80 % XEQT (25/45/30 CA/US/intl) et 20 % ZAG (obligations canadiennes).
func modelBody(name string) gen.ModelWrite {
	return gen.ModelWrite{
		Name: name,
		Targets: []gen.ClassTarget{
			{AssetClass: gen.EquityCa, Target: 0.2},
			{AssetClass: gen.EquityUs, Target: 0.36},
			{AssetClass: gen.EquityIntl, Target: 0.24},
			{AssetClass: gen.BondCa, Target: 0.2},
		},
		Holdings: []gen.ModelHolding{{Symbol: "TSX:XEQT", Weight: 0.8}, {Symbol: "TSX:ZAG", Weight: 0.2}},
	}
}

func newPortfolioPair(t *testing.T) (alice, bob *api, led *fakeLedger, pf *fakePortfolio) {
	t.Helper()
	led, pf = newFakeLedger(), newFakePortfolio()
	h, err := httpapi.New(httpapi.Config{Ledger: led, Catalog: fakeCatalog{rows: catalogRows()}, Portfolio: pf, Now: fixedNow})
	if err != nil {
		t.Fatal(err)
	}
	return newAPI(t, h, uuid.New(), &bytes.Buffer{}), newAPI(t, h, uuid.New(), &bytes.Buffer{}), led, pf
}

func (a *api) want(method, path string, body any, status int, out any) {
	a.t.Helper()
	r := a.doJSON(method, base+path, body)
	if r.Status != status {
		a.t.Fatalf("%s %s : %d (attendu %d) %s", method, path, r.Status, status, r.Body)
	}
	if out != nil {
		r.json(a.t, out)
	}
}

func TestModelsVersionsAndIsolation(t *testing.T) {
	t.Parallel()
	alice, bob, _, _ := newPortfolioPair(t)

	var m gen.Model
	alice.want("POST", "/portfolio/models", modelBody("Croissance"), 201, &m)
	if m.Version != 1 || len(m.Targets) != 6 || m.Bands == nil || *m.Bands.Relative != 0.2 || *m.Bands.CashPoints != 0.05 {
		t.Fatalf("modèle créé : %+v", m)
	}
	alice.want("POST", "/portfolio/models", modelBody("Croissance"), 409, nil)

	bads := map[string]func(b *gen.ModelWrite){
		"cibles à 90 %":        func(b *gen.ModelWrite) { b.Targets[0].Target = 0.1 },
		"poids à 90 %":         func(b *gen.ModelWrite) { b.Holdings[0].Weight = 0.7 },
		"fonds inconnu":        func(b *gen.ModelWrite) { b.Holdings[1].Symbol = "TSX:NOPE" },
		"fonds en double":      func(b *gen.ModelWrite) { b.Holdings[1].Symbol = "TSX:XEQT" },
		"symbole mal formé":    func(b *gen.ModelWrite) { b.Holdings[1].Symbol = "zag" },
		"classe en double":     func(b *gen.ModelWrite) { b.Targets[1].AssetClass = gen.EquityCa },
		"classe inconnue":      func(b *gen.ModelWrite) { b.Targets[1].AssetClass = "or" },
		"bande hors bornes":    func(b *gen.ModelWrite) { b.Bands = &gen.Bands{Relative: f64(1.5)} },
		"nom vide":             func(b *gen.ModelWrite) { b.Name = "" },
		"vingt-et-un fonds":    func(b *gen.ModelWrite) { b.Holdings = make([]gen.ModelHolding, 21) },
		"encaisse sans cibles": func(b *gen.ModelWrite) { b.CashTarget = f64(0.5) },
	}
	for name, mut := range bads {
		b := modelBody("Autre " + name)
		mut(&b)
		r := alice.doJSON("POST", base+"/portfolio/models", b)
		if r.Status != 400 {
			t.Errorf("%s : %d %s", name, r.Status, r.Body)
		}
	}

	// Version suivante : PUT remplace le contenu.
	next := modelBody("Croissance")
	next.CashTarget = f64(0.1)
	next.Targets = []gen.ClassTarget{{AssetClass: gen.EquityCa, Target: 0.18}, {AssetClass: gen.EquityUs, Target: 0.324}, {AssetClass: gen.EquityIntl, Target: 0.216}, {AssetClass: gen.BondCa, Target: 0.18}}
	next.Holdings = []gen.ModelHolding{{Symbol: "TSX:XEQT", Weight: 0.72}, {Symbol: "TSX:ZAG", Weight: 0.18}}
	var v2 gen.Model
	alice.want("PUT", "/portfolio/models/"+m.Id.String(), next, 200, &v2)
	if v2.Version != 2 || v2.Id != m.Id || *v2.CashTarget != 0.1 {
		t.Fatalf("version 2 : %+v", v2)
	}
	renamed := next
	renamed.Name = "Autre nom"
	alice.want("PUT", "/portfolio/models/"+m.Id.String(), renamed, 400, nil)
	alice.want("PUT", "/portfolio/models/"+uuid.New().String(), next, 404, nil)
	var list gen.ModelList
	alice.want("GET", "/portfolio/models", nil, 200, &list)
	if len(list.Items) != 1 || list.Items[0].Version != 2 {
		t.Fatalf("liste : %+v", list)
	}

	// Bob ne voit ni ne modifie le modèle d'Alice, et peut réutiliser le même nom.
	bob.want("GET", "/portfolio/models", nil, 200, &list)
	if len(list.Items) != 0 {
		t.Fatalf("Bob voit %+v", list)
	}
	bob.want("PUT", "/portfolio/models/"+m.Id.String(), next, 404, nil)
	bob.want("POST", "/portfolio/models", modelBody("Croissance"), 201, nil)
}

func TestPurchasePlan(t *testing.T) {
	t.Parallel()
	alice, bob, _, pf := newPortfolioPair(t)
	var m gen.Model
	alice.want("POST", "/portfolio/models", modelBody("Croissance"), 201, &m)
	path := "/portfolio/models/" + m.Id.String() + "/purchase-plan"
	req := gen.PurchasePlanRequest{Amount: "10000", Currency: gen.CAD}

	alice.want("POST", path, req, 422, nil) // aucun prix connu
	pf.setPrice(alice.owner, "TSX:XEQT", "30")
	alice.want("POST", path, req, 422, nil) // il manque encore ZAG
	pf.setPrice(alice.owner, "TSX:ZAG", "20")
	var plan gen.PurchasePlan
	alice.want("POST", path, req, 200, &plan)
	// 8000 / 30 = 266 parts (7980 $), 2000 / 20 = 100 parts ; reste 20 $.
	if len(plan.Orders) != 2 || plan.Orders[0].Symbol != "TSX:XEQT" || plan.Orders[0].Shares != 266 ||
		plan.Orders[1].Shares != 100 || plan.Invested != "9980" || plan.CashRemainder != "20" || plan.ModelVersion != 1 {
		t.Fatalf("plan : %+v", plan)
	}
	for _, bad := range []gen.PurchasePlanRequest{{Amount: "0", Currency: gen.CAD}, {Amount: "-5", Currency: gen.CAD}, {Amount: "abc", Currency: gen.CAD}, {Amount: "5", Currency: "EUR"}} {
		if r := alice.doJSON("POST", base+path, bad); r.Status != 400 {
			t.Errorf("%+v : %d %s", bad, r.Status, r.Body)
		}
	}
	alice.want("POST", "/portfolio/models/"+uuid.New().String()+"/purchase-plan", req, 404, nil)
	pf.setPrice(bob.owner, "TSX:XEQT", "30")
	bob.want("POST", path, req, 404, nil) // le modèle d'Alice n'existe pas pour Bob
}

func TestRebalancePlan(t *testing.T) {
	t.Parallel()
	alice, bob, _, pf := newPortfolioPair(t)
	acc := alice.account("CELI")
	var m gen.Model
	alice.want("POST", "/portfolio/models", modelBody("Croissance"), 201, &m)
	req := gen.RebalancePlanRequest{ModelId: m.Id, Currency: gen.CAD, AccountIds: &[]uuid.UUID{acc.Id}}

	alice.want("POST", "/portfolio/rebalance-plans", req, 422, nil) // prix manquants
	pf.setPrice(alice.owner, "TSX:XEQT", "30")
	pf.setPrice(alice.owner, "TSX:ZAG", "20")
	// Positions simulées : 10 XEQT (300 $), 5 ZAG (100 $), encaisse 100,50 $ -> total 500,50 $.
	var plan gen.RebalancePlan
	alice.want("POST", "/portfolio/rebalance-plans", req, 200, &plan)
	if plan.TotalValue == nil || *plan.TotalValue != "500.5" || len(plan.Classes) != 6 || plan.ModelVersion != 1 {
		t.Fatalf("plan : %+v", plan)
	}
	oob := 0
	for _, c := range plan.Classes {
		if c.OutOfBand {
			oob++
		}
	}
	if oob == 0 || len(plan.Orders) == 0 {
		t.Fatalf("un portefeuille à 80 pour cent d encaisse doit être déséquilibré : %+v", plan)
	}
	for _, o := range plan.Orders {
		if o.AccountId == nil || *o.AccountId != acc.Id || o.Side != gen.OrderSideBuy {
			t.Fatalf("ordre : %+v", o)
		}
	}
	// Poids avant/après et statut par fonds (D460) : tous du moteur, en fraction.
	if len(plan.Funds) < 2 || len(plan.AccountCash) != 1 || plan.AccountCash[0].AccountId != acc.Id || plan.AccountCash[0].Cash != plan.CashAfter {
		t.Fatalf("fonds ou encaisse par compte : %+v", plan)
	}
	oobBefore := 0
	for _, f := range plan.Funds {
		if f.Symbol == "" || f.Before < 0 || f.Before > 1 || f.After < 0 || f.After > 1 {
			t.Fatalf("fonds : %+v", f)
		}
		if f.OutOfBandBefore {
			oobBefore++
		}
	}
	if oobBefore == 0 {
		t.Fatalf("un fonds doit être hors bande avant : %+v", plan.Funds)
	}
	// Cotisation et minimum d'ordre ; tous les comptes (sans accountIds).
	req2 := gen.RebalancePlanRequest{ModelId: m.Id, Currency: gen.CAD, Contribution: sp("1000"), MinOrderAmount: sp("100000"), Bands: &gen.Bands{Relative: f64(0.1)}}
	alice.want("POST", "/portfolio/rebalance-plans", req2, 200, &plan)
	if len(plan.Orders) != 0 || len(plan.Skipped) == 0 || plan.CashAfter != "1100.5" {
		t.Fatalf("minimum d'ordre : %+v", plan)
	}
	// Une autre devise : comptes ignorés en vue « tous », refus si demandés explicitement.
	req3 := gen.RebalancePlanRequest{ModelId: m.Id, Currency: gen.USD}
	alice.want("POST", "/portfolio/rebalance-plans", req3, 200, &plan)
	if len(plan.Orders) != 0 {
		t.Fatalf("aucun compte USD : %+v", plan)
	}
	req3.AccountIds = &[]uuid.UUID{acc.Id}
	alice.want("POST", "/portfolio/rebalance-plans", req3, 400, nil)

	bad := []gen.RebalancePlanRequest{
		{ModelId: m.Id, Currency: gen.CAD, Contribution: sp("-1")},
		{ModelId: m.Id, Currency: gen.CAD, Bands: &gen.Bands{CashPoints: f64(2)}},
		{ModelId: m.Id, Currency: "EUR"},
	}
	for _, b := range bad {
		if r := alice.doJSON("POST", base+"/portfolio/rebalance-plans", b); r.Status != 400 {
			t.Errorf("%+v : %d %s", b, r.Status, r.Body)
		}
	}
	alice.want("POST", "/portfolio/rebalance-plans", gen.RebalancePlanRequest{ModelId: uuid.New(), Currency: gen.CAD}, 404, nil)
	alice.want("POST", "/portfolio/rebalance-plans", gen.RebalancePlanRequest{ModelId: m.Id, Currency: gen.CAD, AccountIds: &[]uuid.UUID{uuid.New()}}, 404, nil)
	// Bob : ni le modèle ni le compte d'Alice.
	bob.want("POST", "/portfolio/rebalance-plans", req, 404, nil)
	bobAcc := bob.account("CELI Bob")
	bob.want("POST", "/portfolio/rebalance-plans", gen.RebalancePlanRequest{ModelId: m.Id, Currency: gen.CAD, AccountIds: &[]uuid.UUID{bobAcc.Id, acc.Id}}, 404, nil)
}

func TestExposure(t *testing.T) {
	t.Parallel()
	alice, bob, _, pf := newPortfolioPair(t)
	acc := alice.account("CELI")
	var m gen.Model
	alice.want("POST", "/portfolio/models", modelBody("Croissance"), 201, &m)
	q := "?accounts=" + acc.Id.String()

	var ex gen.Exposure
	alice.want("GET", "/portfolio/exposure"+q, nil, 200, &ex)
	if len(ex.Classes) != 0 || ex.TotalValue != nil { // aucun prix connu : positions écartées
		t.Fatalf("sans prix : %+v", ex)
	}
	pf.setPrice(alice.owner, "TSX:XEQT", "30")
	pf.setPrice(alice.owner, "TSX:ZAG", "20")
	alice.want("GET", "/portfolio/exposure"+q+"&modelId="+m.Id.String(), nil, 200, &ex)
	// 10 XEQT à 30 + 5 ZAG à 20 = 400 $ ; XEQT 75 % : CA 18,75 %, US 33,75 %, intl 22,5 % ; ZAG 25 %.
	if ex.TotalValue == nil || *ex.TotalValue != "400" || len(ex.Classes) != 6 || ex.ModelId == nil || *ex.ModelId != m.Id {
		t.Fatalf("exposition : %+v", ex)
	}
	if ex.WeightedMer == nil || math.Abs(*ex.WeightedMer-(0.75*0.002+0.25*0.0009)) > 1e-12 {
		t.Fatalf("RFG : %v", ex.WeightedMer)
	}
	ca := ex.Classes[0]
	if ca.AssetClass != gen.EquityCa || math.Abs(ca.Actual-0.1875) > 1e-12 || ca.Target == nil || *ca.Target != 0.2 ||
		ca.Deviation == nil || math.Abs(*ca.Deviation-(0.1875-0.2)) > 1e-12 {
		t.Fatalf("classe CA : %+v", ca)
	}
	ex = gen.Exposure{}
	alice.want("GET", "/portfolio/exposure"+q, nil, 200, &ex) // sans modèle : ni cible ni écart
	if ex.ModelId != nil || ex.Classes[0].Target != nil || ex.Classes[0].Deviation != nil {
		t.Fatalf("sans modèle : %+v", ex)
	}
	alice.want("GET", "/portfolio/exposure?accounts="+uuid.New().String(), nil, 404, nil)
	alice.want("GET", "/portfolio/exposure?modelId="+uuid.New().String(), nil, 404, nil)
	// Bob : ni le compte ni le modèle d'Alice.
	bob.want("GET", "/portfolio/exposure"+q, nil, 404, nil)
	bob.account("CELI Bob")
	bob.want("GET", "/portfolio/exposure?modelId="+m.Id.String(), nil, 404, nil)
	ex = gen.Exposure{}
	bob.want("GET", "/portfolio/exposure", nil, 200, &ex)
	if len(ex.Classes) != 0 {
		t.Fatalf("Bob : %+v", ex)
	}
	// Devises mélangées : refus.
	alice.want("POST", "/accounts", gen.AccountCreate{Name: "USD", Type: "taxable", Broker: "x", Currency: gen.USD}, 201, nil)
	alice.want("GET", "/portfolio/exposure", nil, 400, nil)
}

func TestPerformanceStatementsAndTransactions(t *testing.T) {
	t.Parallel()
	alice, bob, _, _ := newPortfolioPair(t)
	stmtAcc, txAcc := alice.account("Relevés"), alice.account("Registre")
	st := "/accounts/" + stmtAcc.Id.String() + "/statements"
	for _, s := range []gen.StatementCreate{
		{Month: "2025-12", EndValue: "1000", Currency: gen.CAD},
		{Month: "2026-01", EndValue: "1100", Currency: gen.CAD},
		{Month: "2026-02", EndValue: "1210", Currency: gen.CAD},
	} {
		alice.want("POST", st, s, 201, nil)
	}
	var perf gen.Performance
	alice.want("GET", "/portfolio/performance?accounts="+stmtAcc.Id.String(), nil, 200, &perf)
	s := perf.Accounts[0]
	if *s.Method != gen.Statements || len(s.Monthly) != 2 || math.Abs(*s.Twr-0.21) > 1e-9 ||
		math.Abs(*s.Monthly[0].Twr-0.1) > 1e-9 || s.Monthly[1].Irr == nil || len(s.Annual) != 1 {
		t.Fatalf("relevés : %+v", s)
	}
	// TRI cumulé (D337, D413) : (1 + 10 %)^2 − 1 après deux mois, pas un taux annuel.
	if math.Abs(*s.Irr-0.21) > 1e-6 || perf.Consolidated.Method == nil || *perf.Consolidated.Method != gen.Statements {
		t.Fatalf("TRI cumulé : irr=%v consolidé=%+v", *s.Irr, perf.Consolidated)
	}
	if s.IrrBasis == nil || *s.IrrBasis != gen.Cumulative {
		t.Fatalf("base du TRI des relevés : %v", s.IrrBasis)
	}
	if perf.From.String() != "2026-01-01" && perf.From.String() != "2025-12-01" {
		t.Fatalf("début : %v", perf.From)
	}

	// Compte avec transactions : contribution, achat, puis le prix monte (vente partielle à un prix plus haut).
	tx := "/accounts/" + txAcc.Id.String() + "/transactions"
	alice.want("POST", tx, gen.TransactionCreate{Type: gen.TransactionTypeContribution, TradeDate: dateD("2026-01-05"), Currency: gen.CAD, Amount: sp("1000")}, 201, nil)
	alice.want("POST", tx, tradeTx(gen.TransactionTypeBuy, "2026-01-06", "TSX:XEQT", "10", "100"), 201, nil)
	alice.want("POST", tx, tradeTx(gen.TransactionTypeSell, "2026-03-10", "TSX:XEQT", "5", "120"), 201, nil)
	perf = gen.Performance{}
	alice.want("GET", "/portfolio/performance?accounts="+txAcc.Id.String()+"&from=2026-01-01&to=2026-04-30", nil, 200, &perf)
	a := perf.Accounts[0]
	if *a.Method != gen.Transactions || a.Twr == nil || a.Irr == nil || len(a.Annual) != 1 || a.Annual[0].Period != "2026" ||
		math.Abs(*a.Annual[0].Twr-*a.Twr) > 1e-12 || perf.Consolidated.Method == nil || *perf.Consolidated.Method != gen.Transactions {
		t.Fatalf("registre : %+v", a)
	}
	if a.IrrBasis == nil || *a.IrrBasis != gen.Annualized {
		t.Fatalf("base du TRI du registre : %v", a.IrrBasis)
	}
	// Mars : la vente à 120 $ marque le titre à 120 $ (D411) : 1000 $ -> 1200 $, soit +20 %.
	var march *gen.PerformancePoint
	for i := range a.Monthly {
		if a.Monthly[i].Period == "2026-03" {
			march = &a.Monthly[i]
		}
	}
	if march == nil || march.Twr == nil || math.Abs(*march.Twr-0.2) > 1e-9 {
		t.Fatalf("mars : %+v", a.Monthly)
	}
	// Méthodes mixtes : le consolidé est vide (D413).
	perf = gen.Performance{}
	alice.want("GET", "/portfolio/performance", nil, 200, &perf)
	if len(perf.Accounts) != 2 || perf.Consolidated.Method != nil || len(perf.Consolidated.Monthly) != 0 {
		t.Fatalf("consolidé mixte : %+v", perf.Consolidated)
	}
	// Période invalide, compte étranger.
	alice.want("GET", "/portfolio/performance?from=2026-05-01&to=2026-01-01", nil, 400, nil)
	alice.want("GET", "/portfolio/performance?from=1800-01-01", nil, 400, nil)
	alice.want("GET", "/portfolio/performance?accounts="+uuid.New().String(), nil, 404, nil)
	bob.want("GET", "/portfolio/performance?accounts="+txAcc.Id.String(), nil, 404, nil)
	perf = gen.Performance{}
	bob.want("GET", "/portfolio/performance", nil, 200, &perf)
	if len(perf.Accounts) != 0 || perf.Consolidated.Method != nil {
		t.Fatalf("Bob : %+v", perf)
	}
}

func TestPortfolioNeedsOwner(t *testing.T) {
	t.Parallel()
	h, err := httpapi.New(httpapi.Config{Ledger: newFakeLedger(), Catalog: fakeCatalog{}, Portfolio: newFakePortfolio()})
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	for name, call := range map[string]func() error{
		"modèles":    func() error { _, err := h.ListModels(ctx, gen.ListModelsRequestObject{}); return err },
		"création":   func() error { _, err := h.CreateModel(ctx, gen.CreateModelRequestObject{}); return err },
		"achat":      func() error { _, err := h.CreatePurchasePlan(ctx, gen.CreatePurchasePlanRequestObject{}); return err },
		"équilibre":  func() error { _, err := h.CreateRebalancePlan(ctx, gen.CreateRebalancePlanRequestObject{}); return err },
		"exposition": func() error { _, err := h.GetExposure(ctx, gen.GetExposureRequestObject{}); return err },
		"rendement":  func() error { _, err := h.GetPerformance(ctx, gen.GetPerformanceRequestObject{}); return err },
	} {
		if err := call(); err == nil || !strings.Contains(err.Error(), "Authentification") {
			t.Errorf("%s : %v", name, err)
		}
	}
}

// Prix saisis (D440) : ils remplacent le registre et couvrent l'absence de prix.
func TestPurchasePlanManualPrices(t *testing.T) {
	t.Parallel()
	alice, _, _, pf := newPortfolioPair(t)
	var m gen.Model
	alice.want("POST", "/portfolio/models", modelBody("Croissance"), 201, &m)
	path := "/portfolio/models/" + m.Id.String() + "/purchase-plan"
	prices := gen.ManualPrices{"TSX:XEQT": "30"}
	req := gen.PurchasePlanRequest{Amount: "10000", Currency: gen.CAD, Prices: &prices}

	// Un seul prix saisi : 422 qui nomme le fonds sans prix.
	r := alice.doJSON("POST", base+path, req)
	if r.Status != 422 || !strings.Contains(string(r.Body), "TSX:ZAG") || strings.Contains(string(r.Body), "TSX:XEQT,") {
		t.Fatalf("422 attendu : %d %s", r.Status, r.Body)
	}
	prices["TSX:ZAG"] = "20"
	var plan gen.PurchasePlan
	alice.want("POST", path, req, 200, &plan)
	if len(plan.Orders) != 2 || plan.Orders[0].Shares != 266 || plan.Orders[1].Shares != 100 {
		t.Fatalf("plan : %+v", plan)
	}
	// Le prix saisi l'emporte sur celui du registre.
	pf.setPrice(alice.owner, "TSX:XEQT", "60")
	alice.want("POST", path, req, 200, &plan)
	if plan.Orders[0].Shares != 266 {
		t.Fatalf("le prix saisi doit gagner : %+v", plan)
	}
	// Sans prix saisi pour XEQT, le registre sert (60 $ : 8000 / 60 = 133 parts).
	alice.want("POST", path, gen.PurchasePlanRequest{Amount: "10000", Currency: gen.CAD, Prices: &gen.ManualPrices{"TSX:ZAG": "20"}}, 200, &plan)
	if plan.Orders[0].Shares != 133 {
		t.Fatalf("prix du registre : %+v", plan)
	}
	for name, bad := range map[string]gen.ManualPrices{"zéro": {"TSX:XEQT": "0"}, "texte": {"TSX:XEQT": "abc"}, "symbole": {"xeqt": "30"}} {
		b := bad
		if r := alice.doJSON("POST", base+path, gen.PurchasePlanRequest{Amount: "10000", Currency: gen.CAD, Prices: &b}); r.Status != 400 {
			t.Errorf("%s : %d %s", name, r.Status, r.Body)
		}
	}
}

// Rééquilibrage : prix saisis (D440) et cotisation par compte (D443).
func TestRebalancePlanManualPricesAndContributions(t *testing.T) {
	t.Parallel()
	alice, bob, _, _ := newPortfolioPair(t)
	a1, a2 := alice.account("CELI"), alice.account("REER")
	var m gen.Model
	alice.want("POST", "/portfolio/models", modelBody("Croissance"), 201, &m)
	prices := gen.ManualPrices{"TSX:XEQT": "30"}
	req := gen.RebalancePlanRequest{ModelId: m.Id, Currency: gen.CAD, Prices: &prices}

	r := alice.doJSON("POST", base+"/portfolio/rebalance-plans", req)
	if r.Status != 422 || !strings.Contains(string(r.Body), "TSX:ZAG") {
		t.Fatalf("422 attendu : %d %s", r.Status, r.Body)
	}
	prices["TSX:ZAG"] = "20"
	var plan gen.RebalancePlan
	alice.want("POST", "/portfolio/rebalance-plans", req, 200, &plan)

	// Une cotisation par compte ; un minimum d'ordre très haut garde l'effet visible dans l'encaisse.
	var base1, both gen.RebalancePlan
	req.MinOrderAmount = sp("100000")
	alice.want("POST", "/portfolio/rebalance-plans", req, 200, &base1)
	req.Contributions = &[]gen.ContributionByAccount{{AccountId: a1.Id, Amount: "1000"}, {AccountId: a2.Id, Amount: "500"}}
	alice.want("POST", "/portfolio/rebalance-plans", req, 200, &both)
	if base1.CashAfter == both.CashAfter {
		t.Fatalf("les cotisations doivent changer l'encaisse : %s / %s", base1.CashAfter, both.CashAfter)
	}
	// Ancien champ : un seul compte reçoit 1000 ; combiner les deux champs est refusé.
	req.Contributions = nil
	req.Contribution = sp("1000")
	var legacy gen.RebalancePlan
	alice.want("POST", "/portfolio/rebalance-plans", req, 200, &legacy)
	req.Contributions = &[]gen.ContributionByAccount{{AccountId: a1.Id, Amount: "1000"}}
	alice.want("POST", "/portfolio/rebalance-plans", req, 400, nil)
	var single gen.RebalancePlan
	req.Contribution = nil
	alice.want("POST", "/portfolio/rebalance-plans", req, 200, &single)
	if single.CashAfter != legacy.CashAfter {
		t.Fatalf("contributions à un compte = ancien champ : %s / %s", single.CashAfter, legacy.CashAfter)
	}
	// Doublon, montant négatif, compte inconnu, compte d'un autre propriétaire, compte hors plan.
	bobAcc := bob.account("CELI Bob")
	for name, c := range map[string][]gen.ContributionByAccount{
		"doublon":   {{AccountId: a1.Id, Amount: "1"}, {AccountId: a1.Id, Amount: "2"}},
		"négatif":   {{AccountId: a1.Id, Amount: "-1"}},
		"inconnu":   {{AccountId: uuid.New(), Amount: "1"}},
		"étranger":  {{AccountId: bobAcc.Id, Amount: "1"}},
		"hors plan": {{AccountId: a2.Id, Amount: "1"}},
	} {
		cc := c
		bad := gen.RebalancePlanRequest{ModelId: m.Id, Currency: gen.CAD, Prices: &prices, Contributions: &cc}
		if name == "hors plan" {
			bad.AccountIds = &[]uuid.UUID{a1.Id}
		}
		if r := alice.doJSON("POST", base+"/portfolio/rebalance-plans", bad); r.Status != 400 {
			t.Errorf("%s : %d %s", name, r.Status, r.Body)
		}
	}
}
