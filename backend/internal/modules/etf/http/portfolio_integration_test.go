//go:build integration

package httpapi_test

import (
	"context"
	"math"
	"testing"

	"github.com/google/uuid"

	"github.com/omeroland/finance-privee/backend/internal/api/gen"
	"github.com/omeroland/finance-privee/backend/internal/modules/etf/ingest"
)

func fundProfile(sym, name string, a ingest.Allocation, mer float64) ingest.FundProfile {
	p := profile(sym, name, "equity", mer, "2025-10-31")
	p.Alloc = a
	return p
}

// Pile complète (HTTP, dépôts, PostgreSQL) : modèles versionnés, plans, exposition et rendement
// sur le registre réel, isolement entre deux utilisateurs et audit sans montant.
func TestPortfolioFullStackWithPostgres(t *testing.T) {
	alice, bob, _, etf := realPair(t)
	ctx := context.Background()
	if _, err := etf.SaveProfiles(ctx, alice.owner, []ingest.FundProfile{
		fundProfile("TSX:XEQT", "Equity", ingest.Allocation{EquityCA: 0.25, EquityUS: 0.45, EquityIntl: 0.3}, 0.002),
		fundProfile("TSX:ZAG", "Bond", ingest.Allocation{BondCA: 1}, 0.001),
		fundProfile("TSX:VGRO", "Sans prix", ingest.Allocation{EquityUS: 1}, 0.002),
	}); err != nil {
		t.Fatal(err)
	}

	var celi, taxable gen.Account
	alice.post("/accounts", gen.AccountCreate{Name: "CELI", Type: "celi", Broker: "Q", Currency: gen.CAD}, 201).json(t, &celi)
	alice.post("/accounts", gen.AccountCreate{Name: "Taxable", Type: "taxable", Broker: "Q", Currency: gen.CAD}, 201).json(t, &taxable)
	tx := "/accounts/" + celi.Id.String() + "/transactions"
	alice.post(tx, gen.TransactionCreate{Type: gen.TransactionTypeContribution, TradeDate: dateD("2026-01-05"), Currency: gen.CAD, Amount: sp("10000")}, 201)
	alice.post(tx, trade(gen.TransactionTypeBuy, "2026-01-06", "TSX:XEQT", "100", "30"), 201)
	alice.post(tx, trade(gen.TransactionTypeBuy, "2026-01-06", "TSX:ZAG", "50", "20"), 201)

	var m gen.Model
	alice.post("/portfolio/models", modelBody("Croissance"), 201).json(t, &m)
	alice.post("/portfolio/models", modelBody("Croissance"), 409)
	bad := modelBody("Hors catalogue")
	bad.Holdings[1].Symbol = "TSX:NOPE"
	alice.post("/portfolio/models", bad, 400)
	var v2 gen.Model
	alice.doJSON("PUT", base+"/portfolio/models/"+m.Id.String(), modelBody("Croissance")).json(t, &v2)
	if v2.Version != 2 {
		t.Fatalf("version %d", v2.Version)
	}
	if n := count(t, `SELECT count(*) FROM etf.model_version WHERE owner_id = $1`, alice.owner); n != 2 {
		t.Fatalf("%d versions", n)
	}

	// Plan d'achat : prix = dernier prix du registre (D410) ; un fonds sans prix donne 422.
	var plan gen.PurchasePlan
	alice.post("/portfolio/models/"+m.Id.String()+"/purchase-plan", gen.PurchasePlanRequest{Amount: "10000", Currency: gen.CAD}, 200).json(t, &plan)
	if len(plan.Orders) != 2 || plan.Orders[0].Shares != 266 || plan.Orders[1].Shares != 100 || plan.CashRemainder != "20" || plan.ModelVersion != 2 {
		t.Fatalf("plan d'achat : %+v", plan)
	}
	noPrice := modelBody("Sans prix")
	noPrice.Holdings = []gen.ModelHolding{{Symbol: "TSX:VGRO", Weight: 1}}
	noPrice.Targets = []gen.ClassTarget{{AssetClass: gen.EquityUs, Target: 1}}
	var m2 gen.Model
	alice.post("/portfolio/models", noPrice, 201).json(t, &m2)
	alice.post("/portfolio/models/"+m2.Id.String()+"/purchase-plan", gen.PurchasePlanRequest{Amount: "100", Currency: gen.CAD}, 422)

	// Rééquilibrage : 6000 $ d'encaisse à investir.
	var rb gen.RebalancePlan
	alice.post("/portfolio/rebalance-plans", gen.RebalancePlanRequest{ModelId: m.Id, Currency: gen.CAD, AccountIds: &[]uuid.UUID{celi.Id}}, 200).json(t, &rb)
	if rb.TotalValue == nil || *rb.TotalValue != "10000" || len(rb.Orders) == 0 || len(rb.Classes) != 6 {
		t.Fatalf("rééquilibrage : %+v", rb)
	}
	alice.post("/portfolio/rebalance-plans", gen.RebalancePlanRequest{ModelId: m2.Id, Currency: gen.CAD}, 422) // prix manquant

	// Exposition : 100 XEQT à 30 + 50 ZAG à 20 = 4000 $.
	var ex gen.Exposure
	alice.doJSON("GET", base+"/portfolio/exposure?modelId="+m.Id.String(), nil).json(t, &ex)
	if ex.TotalValue == nil || *ex.TotalValue != "4000" || ex.WeightedMer == nil ||
		math.Abs(*ex.WeightedMer-(0.75*0.002+0.25*0.001)) > 1e-9 || math.Abs(ex.Classes[0].Actual-0.1875) > 1e-9 {
		t.Fatalf("exposition : %+v", ex)
	}

	// Rendement : relevés sur le compte taxable, registre sur le CELI.
	st := "/accounts/" + taxable.Id.String() + "/statements"
	alice.post(st, gen.StatementCreate{Month: "2026-01", EndValue: "1000", Currency: gen.CAD}, 201)
	alice.post(st, gen.StatementCreate{Month: "2026-02", EndValue: "1100", Currency: gen.CAD}, 201)
	var perf gen.Performance
	alice.doJSON("GET", base+"/portfolio/performance", nil).json(t, &perf)
	if len(perf.Accounts) != 2 {
		t.Fatalf("comptes : %+v", perf)
	}
	methods := map[gen.PerformanceMethod]bool{}
	for _, a := range perf.Accounts {
		if a.Method != nil {
			methods[*a.Method] = true
		}
	}
	if !methods[gen.Statements] || !methods[gen.Transactions] {
		t.Fatalf("méthodes : %+v", perf.Accounts)
	}

	// Bob : aucune fuite, en lecture comme en écriture.
	if r := bob.doJSON("PUT", base+"/portfolio/models/"+m.Id.String(), modelBody("Croissance")); r.Status != 404 && r.Status != 400 { // 400 : les fonds d'Alice sont absents du catalogue de Bob
		t.Fatalf("Bob modifie le modèle : %d", r.Status)
	}
	bob.post("/portfolio/models/"+m.Id.String()+"/purchase-plan", gen.PurchasePlanRequest{Amount: "10000", Currency: gen.CAD}, 404)
	bob.post("/portfolio/rebalance-plans", gen.RebalancePlanRequest{ModelId: m.Id, Currency: gen.CAD}, 404)
	for _, p := range []string{"/portfolio/exposure?accounts=" + celi.Id.String(), "/portfolio/exposure?modelId=" + m.Id.String(), "/portfolio/performance?accounts=" + celi.Id.String()} {
		if r := bob.doJSON("GET", base+p, nil); r.Status != 404 {
			t.Fatalf("Bob lit %s : %d", p, r.Status)
		}
	}
	var bl gen.ModelList
	bob.doJSON("GET", base+"/portfolio/models", nil).json(t, &bl)
	if len(bl.Items) != 0 {
		t.Fatalf("Bob voit %+v", bl)
	}
	// Bob ne peut pas bâtir un modèle avec les fonds d'Alice (catalogue vide chez lui).
	bob.post("/portfolio/models", modelBody("Copie"), 400)

	// Audit : écritures de modèle sans poids ni montant, acteur = sub du jeton.
	if n := count(t, `SELECT count(*) FROM core.audit_log WHERE owner_id = $1 AND action IN ('model.create','model.version')`, alice.owner); n != 5 {
		t.Fatalf("%d lignes d'audit de modèle", n)
	}
	if n := count(t, `SELECT count(*) FROM core.audit_log WHERE owner_id = $1 AND action LIKE 'model.%' AND (after::text ~* '(weight|amount|price|target)' OR actor <> $2)`, alice.owner, alice.owner.String()); n != 0 {
		t.Fatalf("%d lignes d'audit fautives", n)
	}
}
