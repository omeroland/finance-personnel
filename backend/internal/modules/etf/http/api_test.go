package httpapi_test

import (
	"bytes"
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/shopspring/decimal"

	"github.com/omeroland/finance-privee/backend/internal/api/gen"
	corestore "github.com/omeroland/finance-privee/backend/internal/core/store"
	httpapi "github.com/omeroland/finance-privee/backend/internal/modules/etf/http"
	etfstore "github.com/omeroland/finance-privee/backend/internal/modules/etf/store"
	"github.com/omeroland/finance-privee/backend/internal/platform/httpx"
)

func sp(s string) *string { return &s }

func catalogRows() []etfstore.Catalog {
	f := func(v float64) *float64 { return &v }
	region := "global"
	yes := true
	return []etfstore.Catalog{
		{
			Symbol: "TSX:XEQT", Name: "iShares Core Equity ETF Portfolio", QuoteCurrency: "CAD", Type: "equity", Region: &region,
			EquityCa: 0.25, EquityUs: 0.45, EquityIntl: 0.3, Mer: f(0.002), Esg: &yes,
			Size: decimal.NullDecimal{Decimal: decimal.NewFromInt(5200000000), Valid: true},
			AsOf: time.Date(2025, 10, 31, 0, 0, 0, 0, time.UTC), Return1y: f(0.142), Return5y: f(0.091),
		},
		{
			Symbol: "TSX:ZAG", Name: "BMO Aggregate Bond Index ETF", QuoteCurrency: "CAD", Type: "fixed_income",
			BondCa: 1, Mer: f(0.0009), AsOf: time.Date(2025, 10, 31, 0, 0, 0, 0, time.UTC),
		},
	}
}

func newPair(t *testing.T, led httpapi.Ledger) (*api, *api) {
	t.Helper()
	h, err := httpapi.New(httpapi.Config{Ledger: led, Catalog: fakeCatalog{rows: catalogRows()}, Portfolio: newFakePortfolio(), Now: fixedNow})
	if err != nil {
		t.Fatal(err)
	}
	return newAPI(t, h, uuid.New(), &bytes.Buffer{}), newAPI(t, h, uuid.New(), &bytes.Buffer{})
}

func (a *api) account(name string) gen.Account {
	a.t.Helper()
	var acc gen.Account
	r := a.doJSON("POST", base+"/accounts", gen.AccountCreate{Name: name, Type: gen.AccountType("celi"), Broker: "Questrade", Currency: gen.CAD})
	if r.Status != 201 {
		a.t.Fatalf("compte: %d %s", r.Status, r.Body)
	}
	r.json(a.t, &acc)
	return acc
}

func TestAccountsAndIsolation(t *testing.T) {
	t.Parallel()
	led := newFakeLedger()
	alice, bob := newPair(t, led)

	acc := alice.account("CELI Alice")
	if r := alice.doJSON("POST", base+"/accounts", gen.AccountCreate{Name: "CELI Alice", Type: "celi", Broker: "x", Currency: gen.CAD}); r.Status != 409 {
		t.Fatalf("doublon: %d", r.Status)
	}
	bad := []gen.AccountCreate{
		{Name: "", Type: "celi", Broker: "x", Currency: gen.CAD},
		{Name: "a", Type: "inconnu", Broker: "x", Currency: gen.CAD},
		{Name: "a", Type: "celi", Broker: "", Currency: gen.CAD},
		{Name: "a", Type: "celi", Broker: "x", Currency: "EUR"},
		{Name: strings.Repeat("é", 101), Type: "celi", Broker: "x", Currency: gen.CAD},
	}
	for _, b := range bad {
		if r := alice.doJSON("POST", base+"/accounts", b); r.Status != 400 {
			t.Fatalf("%+v: %d %s", b, r.Status, r.Body)
		}
	}
	if r := alice.doRaw("POST", base+"/accounts", []byte("{pas du json")); r.Status != 400 {
		t.Fatalf("JSON invalide: %d", r.Status)
	}

	var list gen.AccountList
	alice.doJSON("GET", base+"/accounts", nil).json(t, &list)
	if len(list.Items) != 1 || list.Items[0].Id != acc.Id {
		t.Fatalf("liste d'Alice: %+v", list)
	}
	// Bob ne voit rien et n'atteint aucune route du compte d'Alice.
	bob.doJSON("GET", base+"/accounts", nil).json(t, &list)
	if len(list.Items) != 0 {
		t.Fatalf("fuite entre propriétaires: %+v", list)
	}
	id := acc.Id.String()
	for _, route := range [][2]string{
		{"GET", "/transactions"}, {"GET", "/positions"},
	} {
		if r := bob.doJSON(route[0], base+"/accounts/"+id+route[1], nil); r.Status != 404 {
			t.Fatalf("%s %s chez Bob: %d", route[0], route[1], r.Status)
		}
	}
	if r := bob.doJSON("POST", base+"/accounts/"+id+"/transactions", gen.TransactionCreate{
		Type: gen.TransactionTypeContribution, TradeDate: dateD("2026-01-05"), Currency: gen.CAD, Amount: sp("100"),
	}); r.Status != 404 {
		t.Fatalf("écriture chez Bob: %d", r.Status)
	}
	if r := bob.doJSON("POST", base+"/accounts/"+id+"/statements", gen.StatementCreate{Month: "2026-01", EndValue: "1", Currency: gen.CAD}); r.Status != 404 {
		t.Fatalf("relevé chez Bob: %d", r.Status)
	}
	// Bob peut réutiliser le même nom : l'unicité est par propriétaire.
	bob.account("CELI Alice")
	// Le propriétaire transmis aux dépôts est toujours celui du jeton.
	for i, o := range led.calls {
		if o != alice.owner && o != bob.owner {
			t.Fatalf("appel %d avec un propriétaire inconnu %s", i, o)
		}
	}
}

func TestTransactions(t *testing.T) {
	t.Parallel()
	led := newFakeLedger()
	a, _ := newPair(t, led)
	acc := a.account("CELI")
	path := base + "/accounts/" + acc.Id.String() + "/transactions"

	buy := gen.TransactionCreate{
		Type: gen.TransactionTypeBuy, TradeDate: dateD("2026-02-03"), Currency: gen.CAD,
		Symbol: sp("TSX:XEQT"), Quantity: sp("10.5"), Price: sp("30.25"), Fee: sp("4.95"), Note: sp("premier achat"),
	}
	var tx gen.Transaction
	r := a.doJSON("POST", path, buy)
	r.json(t, &tx)
	if r.Status != 201 || tx.Symbol == nil || *tx.Symbol != "TSX:XEQT" || *tx.Quantity != "10.5" {
		t.Fatalf("achat: %d %s", r.Status, r.Body)
	}
	// Écriture inverse : le dépôt reçoit reversesId.
	rev := buy
	rev.ReversesId = &tx.Id
	var inv gen.Transaction
	r = a.doJSON("POST", path, rev)
	r.json(t, &inv)
	if r.Status != 201 || inv.ReversesId == nil || *inv.ReversesId != tx.Id {
		t.Fatalf("inverse: %d %s", r.Status, r.Body)
	}
	var list gen.TransactionList
	a.doJSON("GET", path+"?from=2026-01-01&to=2026-12-31", nil).json(t, &list)
	if len(list.Items) != 2 {
		t.Fatalf("registre: %+v", list)
	}
	if r := a.doJSON("GET", path+"?from=2026-12-31&to=2026-01-01", nil); r.Status != 400 {
		t.Fatalf("bornes inversées: %d", r.Status)
	}
	if r := a.doJSON("GET", base+"/accounts/"+uuid.NewString()+"/transactions", nil); r.Status != 404 {
		t.Fatalf("compte inconnu: %d", r.Status)
	}
	if r := a.doJSON("POST", path, func() gen.TransactionCreate { b := buy; b.Currency = gen.USD; return b }()); r.Status != 400 {
		t.Fatalf("devise du compte: %d", r.Status)
	}

	d := dateD("2026-02-03")
	other := uuid.New()
	cases := map[string]gen.TransactionCreate{
		"type inconnu":       {Type: "x", TradeDate: d, Currency: gen.CAD},
		"achat sans prix":    {Type: gen.TransactionTypeBuy, TradeDate: d, Currency: gen.CAD, Symbol: sp("TSX:XEQT"), Quantity: sp("1")},
		"quantité nulle":     {Type: gen.TransactionTypeBuy, TradeDate: d, Currency: gen.CAD, Symbol: sp("TSX:XEQT"), Quantity: sp("0"), Price: sp("1")},
		"quantité négative":  {Type: gen.TransactionTypeBuy, TradeDate: d, Currency: gen.CAD, Symbol: sp("TSX:XEQT"), Quantity: sp("-1"), Price: sp("1")},
		"7 décimales":        {Type: gen.TransactionTypeContribution, TradeDate: d, Currency: gen.CAD, Amount: sp("1.1234567")},
		"trop grand":         {Type: gen.TransactionTypeContribution, TradeDate: d, Currency: gen.CAD, Amount: sp("123456789012345")},
		"texte":              {Type: gen.TransactionTypeContribution, TradeDate: d, Currency: gen.CAD, Amount: sp("1e3")},
		"symbole invalide":   {Type: gen.TransactionTypeDividend, TradeDate: d, Currency: gen.CAD, Symbol: sp("xeqt"), Amount: sp("1")},
		"dividende sans mt":  {Type: gen.TransactionTypeDividend, TradeDate: d, Currency: gen.CAD, Symbol: sp("TSX:XEQT")},
		"split sans ratio":   {Type: gen.TransactionTypeSplit, TradeDate: d, Currency: gen.CAD, Symbol: sp("TSX:XEQT")},
		"cotisation sans mt": {Type: gen.TransactionTypeContribution, TradeDate: d, Currency: gen.CAD},
		"transfert vide":     {Type: gen.TransactionTypeTransfer, TradeDate: d, Currency: gen.CAD},
		"contrepartie seule": {Type: gen.TransactionTypeContribution, TradeDate: d, Currency: gen.CAD, Amount: sp("1"), CounterpartAccountId: &other},
		"auto-transfert":     {Type: gen.TransactionTypeTransfer, TradeDate: d, Currency: gen.CAD, Amount: sp("1"), CounterpartAccountId: &acc.Id},
		"note trop longue":   {Type: gen.TransactionTypeFee, TradeDate: d, Currency: gen.CAD, Amount: sp("1"), Note: sp(strings.Repeat("n", 501))},
		"date hors bornes":   {Type: gen.TransactionTypeFee, TradeDate: dateD("1800-01-01"), Currency: gen.CAD, Amount: sp("1")},
		"devise inconnue":    {Type: gen.TransactionTypeFee, TradeDate: d, Currency: "EUR", Amount: sp("1")},
	}
	for name, c := range cases {
		if r := a.doJSON("POST", path, c); r.Status != 400 {
			t.Errorf("%s: %d %s", name, r.Status, r.Body)
		}
	}
	// Types sans symbole ni quantité.
	ok := []gen.TransactionCreate{
		{Type: gen.TransactionTypeContribution, TradeDate: d, Currency: gen.CAD, Amount: sp("1000")},
		{Type: gen.TransactionTypeSplit, TradeDate: d, Currency: gen.CAD, Symbol: sp("TSX:XEQT"), SplitRatio: sp("2")},
		{Type: gen.TransactionTypeTransfer, TradeDate: d, Currency: gen.CAD, Amount: sp("10"), CounterpartAccountId: &other},
	}
	for _, c := range ok {
		if r := a.doJSON("POST", path, c); r.Status != 201 {
			t.Errorf("%s: %d %s", c.Type, r.Status, r.Body)
		}
	}
}

func TestStatements(t *testing.T) {
	t.Parallel()
	a, _ := newPair(t, newFakeLedger())
	acc := a.account("REEE")
	path := base + "/accounts/" + acc.Id.String() + "/statements"
	var st gen.Statement
	r := a.doJSON("POST", path, gen.StatementCreate{Month: "2026-03", EndValue: "1520.50", Currency: gen.CAD, Contribution: sp("-100")})
	r.json(t, &st)
	if r.Status != 201 || st.Contribution == nil || *st.Contribution != "-100" || st.EndValue != "1520.5" {
		t.Fatalf("relevé: %d %s", r.Status, r.Body)
	}
	if r := a.doJSON("POST", path, gen.StatementCreate{Month: "2026-03", EndValue: "1", Currency: gen.CAD}); r.Status != 409 {
		t.Fatalf("doublon: %d", r.Status)
	}
	for _, b := range []gen.StatementCreate{
		{Month: "2026-13", EndValue: "1", Currency: gen.CAD},
		{Month: "2026-04", EndValue: "-1", Currency: gen.CAD},
		{Month: "2026-04", EndValue: "abc", Currency: gen.CAD},
		{Month: "2026-04", EndValue: "1", Currency: "EUR"},
		{Month: "2026-04", EndValue: "1", Currency: gen.CAD, Contribution: sp("x")},
	} {
		if r := a.doJSON("POST", path, b); r.Status != 400 {
			t.Fatalf("%+v: %d", b, r.Status)
		}
	}
}

func TestPositions(t *testing.T) {
	t.Parallel()
	led := newFakeLedger()
	a, _ := newPair(t, led)
	acc := a.account("CELI")
	path := base + "/accounts/" + acc.Id.String() + "/positions"
	var pl gen.PositionList
	a.doJSON("GET", path, nil).json(t, &pl)
	if pl.AsOf.Format("2006-01-02") != "2026-10-30" || pl.Cash != "100.5" || len(pl.Positions) != 2 ||
		pl.TotalValue != nil || pl.Positions[0].Price != nil || pl.Positions[0].AverageCost == nil || pl.Positions[1].AverageCost != nil {
		t.Fatalf("positions: %+v", pl)
	}
	a.doJSON("GET", path+"?asOf=2025-06-30", nil)
	if led.asOf.Format("2006-01-02") != "2025-06-30" {
		t.Fatalf("asOf transmis: %v", led.asOf)
	}
	if r := a.doJSON("GET", path+"?asOf=1700-01-01", nil); r.Status != 400 {
		t.Fatalf("date hors bornes: %d", r.Status)
	}
	if r := a.doJSON("GET", path+"?asOf=pas-une-date", nil); r.Status != 400 {
		t.Fatalf("date invalide: %d", r.Status)
	}
}

func TestCatalog(t *testing.T) {
	t.Parallel()
	a, _ := newPair(t, newFakeLedger())
	var l gen.EtfList
	a.doJSON("GET", base+"/portfolio/etfs", nil).json(t, &l)
	if len(l.Items) != 2 || len(l.Items[0].Allocation) != 3 || l.Items[0].Returns == nil || len(*l.Items[0].Returns) != 2 ||
		l.Items[0].Size == nil || *l.Items[0].Size != "5200000000" || l.Items[1].Region != nil {
		t.Fatalf("catalogue: %+v", l)
	}
	a.doJSON("GET", base+"/portfolio/etfs?type=fixed_income", nil).json(t, &l)
	if len(l.Items) != 1 || l.Items[0].Symbol != "TSX:ZAG" {
		t.Fatalf("type: %+v", l)
	}
	a.doJSON("GET", base+"/portfolio/etfs?maxMer=0.001", nil).json(t, &l)
	if len(l.Items) != 1 || l.Items[0].Symbol != "TSX:ZAG" {
		t.Fatalf("maxMer: %+v", l)
	}
	a.doJSON("GET", base+"/portfolio/etfs?region=us", nil)
	for _, q := range []string{"type=zzz", "region=zzz", "maxMer=2", "maxMer=-0.1", "maxMer=abc"} {
		if r := a.doJSON("GET", base+"/portfolio/etfs?"+q, nil); r.Status != 400 {
			t.Fatalf("%s: %d", q, r.Status)
		}
	}
}

type failLedger struct{ *fakeLedger }

func (failLedger) ListAccounts(context.Context, uuid.UUID) ([]corestore.Account, error) {
	return nil, errBoom
}

func TestInternalErrorsLeakNothing(t *testing.T) {
	t.Parallel()
	a, _ := newPair(t, failLedger{newFakeLedger()})
	r := a.doJSON("GET", base+"/accounts", nil)
	if r.Status != 500 {
		t.Fatalf("statut %d", r.Status)
	}
	if strings.Contains(string(r.Body), "secret") || strings.Contains(string(r.Body), "db.go") {
		t.Fatalf("fuite: %s", r.Body)
	}
}

func TestMissingOwnerIs401(t *testing.T) {
	t.Parallel()
	h, err := httpapi.New(httpapi.Config{Ledger: newFakeLedger(), Catalog: fakeCatalog{}, Portfolio: newFakePortfolio()})
	if err != nil {
		t.Fatal(err)
	}
	_, err = h.ListAccounts(context.Background(), gen.ListAccountsRequestObject{})
	var ae *httpx.Error
	if !errors.As(err, &ae) || ae.Status != 401 {
		t.Fatalf("erreur = %v", err)
	}
	if _, err := httpapi.New(httpapi.Config{}); err == nil {
		t.Fatal("dépôts obligatoires")
	}
}
