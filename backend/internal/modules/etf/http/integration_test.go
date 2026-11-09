//go:build integration

package httpapi_test

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/pressly/goose/v3"
	"github.com/shopspring/decimal"
	"github.com/testcontainers/testcontainers-go"
	"github.com/testcontainers/testcontainers-go/modules/postgres"
	"github.com/testcontainers/testcontainers-go/wait"

	_ "github.com/jackc/pgx/v5/stdlib"

	"github.com/omeroland/finance-privee/backend/internal/api/gen"
	corestore "github.com/omeroland/finance-privee/backend/internal/core/store"
	httpapi "github.com/omeroland/finance-privee/backend/internal/modules/etf/http"
	"github.com/omeroland/finance-privee/backend/internal/modules/etf/ingest"
	etfstore "github.com/omeroland/finance-privee/backend/internal/modules/etf/store"
	"github.com/omeroland/finance-privee/backend/migrations"
)

// Même image PostgreSQL 16 épinglée que backend/migrations.
const pgImage = "postgres@sha256:0cefaabd882e42bcb2a2f3dc56d3ce4770560ba62fd7f197f1a2e008fd5731ab"

var (
	poolOnce sync.Once
	testPool *pgxpool.Pool
	poolErr  error
	ctr      *postgres.PostgresContainer
)

func TestMain(m *testing.M) {
	code := m.Run()
	if ctr != nil {
		_ = testcontainers.TerminateContainer(ctr)
	}
	os.Exit(code)
}

// pool démarre PostgreSQL une fois, applique les migrations et se connecte avec le rôle applicatif.
func pool(t *testing.T) *pgxpool.Pool {
	t.Helper()
	poolOnce.Do(func() {
		ctx := context.Background()
		ctr, poolErr = postgres.Run(ctx, pgImage,
			postgres.WithDatabase("finance"), postgres.WithUsername("owner"), postgres.WithPassword("owner-pw"),
			testcontainers.WithWaitStrategy(wait.ForLog("database system is ready to accept connections").
				WithOccurrence(2).WithStartupTimeout(90*time.Second)),
		)
		if poolErr != nil {
			return
		}
		var dsn string
		if dsn, poolErr = ctr.ConnectionString(ctx, "sslmode=disable"); poolErr != nil {
			return
		}
		db, err := goose.OpenDBWithDriver("pgx", dsn)
		if err != nil {
			poolErr = err
			return
		}
		defer db.Close()
		p, err := goose.NewProvider(goose.DialectPostgres, db, migrations.FS)
		if err != nil {
			poolErr = err
			return
		}
		if _, poolErr = p.Up(ctx); poolErr != nil {
			return
		}
		if _, poolErr = db.ExecContext(ctx, `ALTER ROLE finance_app LOGIN PASSWORD 'test-only-password'`); poolErr != nil {
			return
		}
		host, _ := ctr.Host(ctx)
		port, _ := ctr.MappedPort(ctx, "5432/tcp")
		testPool, poolErr = pgxpool.New(ctx, fmt.Sprintf("postgres://finance_app:test-only-password@%s:%s/finance?sslmode=disable", host, port.Port()))
	})
	if poolErr != nil {
		t.Fatalf("PostgreSQL de test: %v", poolErr)
	}
	return testPool
}

func count(t *testing.T, query string, args ...any) int {
	t.Helper()
	var n int
	if err := testPool.QueryRow(context.Background(), query, args...).Scan(&n); err != nil {
		t.Fatal(err)
	}
	return n
}

func realPair(t *testing.T) (alice, bob *api, core *corestore.Store, etf *etfstore.Store) {
	t.Helper()
	core = corestore.New(pool(t))
	etf = etfstore.New(core)
	h, err := httpapi.New(httpapi.Config{Ledger: core, Catalog: etf, Imports: etf, Now: fixedNow})
	if err != nil {
		t.Fatal(err)
	}
	return newAPI(t, h, uuid.New(), &bytes.Buffer{}), newAPI(t, h, uuid.New(), &bytes.Buffer{}), core, etf
}

func (a *api) post(path string, body any, want int) resp {
	a.t.Helper()
	r := a.doJSON("POST", base+path, body)
	if r.Status != want {
		a.t.Fatalf("POST %s: %d (attendu %d) %s", path, r.Status, want, r.Body)
	}
	return r
}

func (a *api) positions(acc gen.Account) gen.PositionList {
	a.t.Helper()
	var pl gen.PositionList
	r := a.doJSON("GET", base+"/accounts/"+acc.Id.String()+"/positions", nil)
	r.json(a.t, &pl)
	if r.Status != 200 {
		a.t.Fatalf("positions: %d %s", r.Status, r.Body)
	}
	return pl
}

func trade(typ gen.TransactionType, date, sym, qty, price string) gen.TransactionCreate {
	return gen.TransactionCreate{Type: typ, TradeDate: dateD(date), Currency: gen.CAD, Symbol: &sym, Quantity: &qty, Price: &price}
}

// Pile complète (HTTP, dépôts, PostgreSQL) : registre en insertion seule, écriture
// inverse, positions dérivées, isolement entre deux utilisateurs, audit.
func TestLedgerFullStackWithPostgres(t *testing.T) {
	alice, bob, _, _ := realPair(t)

	var celi, reer gen.Account
	alice.post("/accounts", gen.AccountCreate{Name: "CELI", Type: "celi", Broker: "Questrade", Currency: gen.CAD}, 201).json(t, &celi)
	alice.post("/accounts", gen.AccountCreate{Name: "REER", Type: "reer", Broker: "Questrade", Currency: gen.CAD}, 201).json(t, &reer)
	alice.post("/accounts", gen.AccountCreate{Name: "CELI", Type: "celi", Broker: "x", Currency: gen.CAD}, 409)

	tx := func(acc gen.Account) string { return "/accounts/" + acc.Id.String() + "/transactions" }
	alice.post(tx(celi), gen.TransactionCreate{Type: gen.TransactionTypeContribution, TradeDate: dateD("2026-01-05"), Currency: gen.CAD, Amount: sp("10000")}, 201)
	first := trade(gen.TransactionTypeBuy, "2026-01-10", "TSX:XEQT", "100", "30.5")
	first.Fee = sp("9.99")
	alice.post(tx(celi), first, 201)
	alice.post(tx(celi), trade(gen.TransactionTypeBuy, "2026-02-10", "TSX:XEQT", "10", "31"), 201)
	var sell gen.Transaction
	alice.post(tx(celi), trade(gen.TransactionTypeSell, "2026-03-10", "TSX:XEQT", "40", "32"), 201).json(t, &sell)

	pl := alice.positions(celi)
	if len(pl.Positions) != 1 || pl.Positions[0].Quantity != "70" || pl.Cash != "7910.01" ||
		pl.Positions[0].AverageCost == nil || *pl.Positions[0].AverageCost != "30.636273" || pl.TotalValue != nil {
		t.Fatalf("positions après la vente: %+v", pl)
	}

	// Correction : l'écriture inverse annule la vente ; le registre garde les deux lignes.
	inv := trade(gen.TransactionTypeSell, "2026-03-11", "TSX:XEQT", "40", "32")
	inv.ReversesId = &sell.Id
	alice.post(tx(celi), inv, 201)
	alice.post(tx(celi), inv, 409) // déjà inversée
	var reversal gen.Transaction
	var list gen.TransactionList
	alice.doJSON("GET", base+tx(celi), nil).json(t, &list)
	if len(list.Items) != 5 {
		t.Fatalf("registre: %d lignes", len(list.Items))
	}
	reversal = list.Items[4]
	rr := inv
	rr.ReversesId = &reversal.Id
	alice.post(tx(celi), rr, 400) // une inverse ne s'inverse pas
	bad := inv
	bad.Quantity = sp("41")
	bad.ReversesId = &list.Items[1].Id
	alice.post(tx(celi), bad, 400) // ne recopie pas l'originale
	alice.post(tx(celi), func() gen.TransactionCreate { b := inv; id := uuid.New(); b.ReversesId = &id; return b }(), 404)

	pl = alice.positions(celi)
	if pl.Positions[0].Quantity != "110" || pl.Cash != "6630.01" {
		t.Fatalf("positions après l'inverse: %+v", pl)
	}
	// Date antérieure : seule la cotisation et le premier achat comptent.
	var early gen.PositionList
	alice.doJSON("GET", base+"/accounts/"+celi.Id.String()+"/positions?asOf=2026-01-31", nil).json(t, &early)
	if early.Positions[0].Quantity != "100" || early.Cash != "6940.01" {
		t.Fatalf("positions au 31 janvier: %+v", early)
	}

	// Transfert d'espèces et de titres entre comptes d'Alice.
	cp := reer.Id
	alice.post(tx(celi), gen.TransactionCreate{Type: gen.TransactionTypeTransfer, TradeDate: dateD("2026-04-01"), Currency: gen.CAD, Amount: sp("500"), CounterpartAccountId: &cp}, 201)
	alice.post(tx(celi), gen.TransactionCreate{Type: gen.TransactionTypeTransfer, TradeDate: dateD("2026-04-02"), Currency: gen.CAD, Symbol: sp("TSX:XEQT"), Quantity: sp("10"), CounterpartAccountId: &cp}, 201)
	if r := alice.positions(reer); r.Cash != "500" || len(r.Positions) != 1 || r.Positions[0].Quantity != "10" || r.Positions[0].AverageCost != nil {
		t.Fatalf("REER: %+v", r)
	}
	if pl = alice.positions(celi); pl.Cash != "6130.01" || pl.Positions[0].Quantity != "100" {
		t.Fatalf("CELI après transfert: %+v", pl)
	}
	// Un compte vide : encaisse nulle et valeur totale présente.
	var empty gen.Account
	alice.post("/accounts", gen.AccountCreate{Name: "REEE", Type: "reee", Broker: "x", Currency: gen.CAD}, 201).json(t, &empty)
	if pl = alice.positions(empty); pl.Cash != "0" || pl.TotalValue == nil || *pl.TotalValue != "0" {
		t.Fatalf("compte vide: %+v", pl)
	}

	// Relevés : un par compte et par mois.
	st := "/accounts/" + empty.Id.String() + "/statements"
	alice.post(st, gen.StatementCreate{Month: "2026-03", EndValue: "1500.25", Currency: gen.CAD, Contribution: sp("-50")}, 201)
	alice.post(st, gen.StatementCreate{Month: "2026-03", EndValue: "1", Currency: gen.CAD}, 409)

	// Bob (autre utilisateur) : aucune fuite, aucun accès en lecture ni en écriture.
	var bl gen.AccountList
	bob.doJSON("GET", base+"/accounts", nil).json(t, &bl)
	if len(bl.Items) != 0 {
		t.Fatalf("Bob voit des comptes: %+v", bl)
	}
	for _, p := range []string{"/transactions", "/positions"} {
		if r := bob.doJSON("GET", base+"/accounts/"+celi.Id.String()+p, nil); r.Status != 404 {
			t.Fatalf("Bob lit %s: %d", p, r.Status)
		}
	}
	bob.post(tx(celi), trade(gen.TransactionTypeBuy, "2026-01-10", "TSX:XEQT", "1", "1"), 404)
	bob.post(st, gen.StatementCreate{Month: "2026-04", EndValue: "1", Currency: gen.CAD}, 404)
	var bobAcc gen.Account
	bob.post("/accounts", gen.AccountCreate{Name: "CELI", Type: "celi", Broker: "x", Currency: gen.CAD}, 201).json(t, &bobAcc)
	bob.post(tx(bobAcc), gen.TransactionCreate{Type: gen.TransactionTypeContribution, TradeDate: dateD("2026-01-05"), Currency: gen.CAD, Amount: sp("1")}, 201)
	// Bob ne peut ni inverser une écriture d'Alice ni la prendre pour contrepartie.
	rev := trade(gen.TransactionTypeSell, "2026-03-11", "TSX:XEQT", "40", "32")
	rev.ReversesId = &sell.Id
	bob.post(tx(bobAcc), rev, 404)
	bob.post(tx(bobAcc), gen.TransactionCreate{Type: gen.TransactionTypeTransfer, TradeDate: dateD("2026-04-01"), Currency: gen.CAD, Amount: sp("1"), CounterpartAccountId: &celi.Id}, 400)
	if pl = bob.positions(bobAcc); pl.Cash != "1" || len(pl.Positions) != 0 {
		t.Fatalf("Bob: %+v", pl)
	}

	// Audit : une ligne par écriture, acteur = sub du jeton, aucun montant.
	aliceN := count(t, `SELECT count(*) FROM core.audit_log WHERE owner_id = $1`, alice.owner)
	if aliceN < 12 {
		t.Fatalf("lignes d'audit d'Alice = %d", aliceN)
	}
	if n := count(t, `SELECT count(*) FROM core.audit_log WHERE owner_id = $1 AND actor <> $2`, alice.owner, alice.owner.String()); n != 0 {
		t.Fatalf("%d lignes d'audit avec un autre acteur", n)
	}
	if n := count(t, `SELECT count(*) FROM core.audit_log WHERE owner_id = $1 AND (after::text ~* '(quantity|price|amount)')`, alice.owner); n != 0 {
		t.Fatalf("%d lignes d'audit portent des montants", n)
	}
	if n := count(t, `SELECT count(*) FROM core.transaction WHERE owner_id = $1`, alice.owner); n != 7 {
		t.Fatalf("registre d'Alice: %d lignes", n)
	}
}

func TestImportCSVRouteWithPostgres(t *testing.T) {
	alice, bob, _, _ := realPair(t)
	var celi gen.Account
	alice.post("/accounts", gen.AccountCreate{Name: "CELI", Type: "celi", Broker: "x", Currency: gen.CAD}, 201).json(t, &celi)
	alice.post("/accounts", gen.AccountCreate{Name: "REER", Type: "reer", Broker: "x", Currency: gen.CAD}, 201)
	csvData, err := os.ReadFile("../../../../../testdata/fixtures/transactions_generic.csv")
	if err != nil {
		t.Fatal(err)
	}
	where := `owner_id = $1 AND source = 'csv'`

	var rep gen.CsvImportReport
	r := alice.importCSV(celi.Id, "generic", csvData)
	if r.Status != 200 {
		t.Fatalf("import: %d %s", r.Status, r.Body)
	}
	r.json(t, &rep)
	// La ligne du compte REER est refusée : la route importe dans le CELI seulement.
	if rep.Created < 6 || rep.Rejected != 1 || rep.Invalid == 0 {
		t.Fatalf("rapport: %+v", rep)
	}
	rows := count(t, `SELECT count(*) FROM core.transaction WHERE `+where, alice.owner)
	if rows != rep.Created {
		t.Fatalf("registre %d, rapport %d", rows, rep.Created)
	}

	// Ré-import : zéro création.
	alice.importCSV(celi.Id, "generic", csvData).json(t, &rep)
	if rep.Created != 0 || rep.Duplicates != rows {
		t.Fatalf("ré-import: %+v", rep)
	}
	if n := count(t, `SELECT count(*) FROM core.transaction WHERE `+where, alice.owner); n != rows {
		t.Fatalf("le ré-import a ajouté des lignes: %d -> %d", rows, n)
	}

	// Audit : une ligne par écriture, sans quantité, prix ni montant.
	if n := count(t, `SELECT count(*) FROM core.audit_log WHERE owner_id = $1 AND action = 'transaction.create'`, alice.owner); n < rows {
		t.Fatalf("audit: %d lignes pour %d écritures", n, rows)
	}
	if n := count(t, `SELECT count(*) FROM core.audit_log WHERE owner_id = $1 AND after::text ~ '(quantity|price|amount|5000|30\.5)'`, alice.owner); n != 0 {
		t.Fatalf("%d lignes d'audit portent un montant", n)
	}

	// Deux utilisateurs : Bob ne peut pas importer dans le compte d'Alice.
	if r := bob.importCSV(celi.Id, "generic", csvData); r.Status != 404 {
		t.Fatalf("Bob: %d %s", r.Status, r.Body)
	}
	if n := count(t, `SELECT count(*) FROM core.transaction WHERE owner_id = $1`, bob.owner); n != 0 {
		t.Fatalf("Bob a %d lignes", n)
	}
	if pl := alice.positions(celi); len(pl.Positions) == 0 {
		t.Fatalf("positions: %+v", pl)
	}
}

func profile(sym, name, typ string, mer float64, asOf string) ingest.FundProfile {
	d, _ := time.Parse(time.DateOnly, asOf)
	region := "global"
	size := decimal.RequireFromString("5200000000")
	return ingest.FundProfile{
		Symbol: sym, Exchange: "TSX", Name: name, Currency: "CAD", Type: typ, Region: &region,
		Alloc: ingest.Allocation{EquityCA: 0.5, EquityUS: 0.5}, MER: &mer, Size: &size,
		Returns: map[int]float64{1: 0.1, 5: 0.07}, AsOf: d, Source: "xlsx",
	}
}

func ptrF(f float64) *float64 { return &f }

// Catalogue, modèle (dépôt de l'import du classeur) et import CSV, avec isolement entre propriétaires.
func TestCatalogModelAndCSVWithPostgres(t *testing.T) {
	alice, bob, _, etf := realPair(t)
	ctx := context.Background()

	// Dépôt de l'import du classeur : idempotent, un profil par (instrument, as_of).
	profiles := []ingest.FundProfile{
		profile("TSX:XEQT", "iShares Core Equity", "equity", 0.002, "2025-10-31"),
		profile("TSX:ZAG", "BMO Aggregate", "fixed_income", 0.0009, "2025-10-31"),
	}
	c, err := etf.SaveProfiles(ctx, alice.owner, profiles)
	if err != nil || c.Created != 2 {
		t.Fatalf("création: %+v %v", c, err)
	}
	if c, err = etf.SaveProfiles(ctx, alice.owner, profiles); err != nil || c.Unchanged != 2 || c.Created != 0 {
		t.Fatalf("idempotence: %+v %v", c, err)
	}
	profiles[0].MER = ptrF(0.0025)
	if c, err = etf.SaveProfiles(ctx, alice.owner, profiles); err != nil || c.Updated != 1 || c.Unchanged != 1 {
		t.Fatalf("mise à jour: %+v %v", c, err)
	}
	var l gen.EtfList
	alice.doJSON("GET", base+"/portfolio/etfs?type=equity&maxMer=0.003", nil).json(t, &l)
	if len(l.Items) != 1 || l.Items[0].Symbol != "TSX:XEQT" || *l.Items[0].Mer != 0.0025 || l.Items[0].Name != "iShares Core Equity" {
		t.Fatalf("catalogue d'Alice: %+v", l)
	}
	alice.doJSON("GET", base+"/portfolio/etfs?maxMer=0.001", nil).json(t, &l)
	if len(l.Items) != 1 || l.Items[0].Symbol != "TSX:ZAG" {
		t.Fatalf("maxMer: %+v", l)
	}
	bob.doJSON("GET", base+"/portfolio/etfs", nil).json(t, &l)
	if len(l.Items) != 0 {
		t.Fatalf("Bob voit le catalogue d'Alice: %+v", l)
	}

	model := ingest.Model{
		Name: "Modèle test", Targets: ingest.Allocation{EquityCA: 0.4, EquityUS: 0.6}, BandRelative: 0.2, BandCash: 0.05,
		Holdings: []ingest.ModelHolding{{Symbol: "TSX:XEQT", Weight: 0.7}, {Symbol: "TSX:ZAG", Weight: 0.3}},
	}
	if out, err := etf.SaveModel(ctx, alice.owner, model); err != nil || out != ingest.ModelCreated {
		t.Fatalf("modèle: %q %v", out, err)
	}
	if out, err := etf.SaveModel(ctx, alice.owner, model); err != nil || out != ingest.ModelUnchanged {
		t.Fatalf("modèle identique: %q %v", out, err)
	}
	model.Holdings = []ingest.ModelHolding{{Symbol: "TSX:XEQT", Weight: 0.6}, {Symbol: "TSX:ZAG", Weight: 0.4}}
	if out, err := etf.SaveModel(ctx, alice.owner, model); err != nil || out != ingest.ModelCreated {
		t.Fatalf("nouvelle version: %q %v", out, err)
	}
	if n := count(t, `SELECT max(version) FROM etf.model_version WHERE owner_id = $1`, alice.owner); n != 2 {
		t.Fatalf("version = %d", n)
	}
	model.Holdings = []ingest.ModelHolding{{Symbol: "TSX:INCONNU", Weight: 1}}
	if _, err := etf.SaveModel(ctx, alice.owner, model); err == nil {
		t.Fatal("un fonds hors catalogue doit être refusé")
	}
	// Chez Bob, les fonds d'Alice n'existent pas.
	model.Holdings = []ingest.ModelHolding{{Symbol: "TSX:XEQT", Weight: 1}}
	if _, err := etf.SaveModel(ctx, bob.owner, model); err == nil {
		t.Fatal("Bob ne doit pas atteindre les fonds d'Alice")
	}

	// Import CSV générique (route encore en 501, D400) : dépôt prêt, idempotent.
	for _, n := range []string{"CELI", "REER"} {
		alice.post("/accounts", gen.AccountCreate{Name: n, Type: "celi", Broker: "x", Currency: gen.CAD}, 201)
	}
	csvData, err := os.ReadFile("../../../../../testdata/fixtures/transactions_generic.csv")
	if err != nil {
		t.Fatal(err)
	}
	imp := ingest.NewTxImporter(etf)
	_, rep, err := imp.ImportCSV(ctx, alice.owner, bytes.NewReader(csvData), ingest.CSVOptions{})
	if err != nil || rep.Created < 7 || rep.Rejected != 0 {
		t.Fatalf("import: %+v %v", rep, err)
	}
	rows := count(t, `SELECT count(*) FROM core.transaction WHERE owner_id = $1 AND source = 'csv'`, alice.owner)
	if rows != rep.Created {
		t.Fatalf("registre %d lignes, rapport %d", rows, rep.Created)
	}
	_, rep2, err := imp.ImportCSV(ctx, alice.owner, bytes.NewReader(csvData), ingest.CSVOptions{})
	if err != nil || rep2.Created != 0 || rep2.Duplicates != rep.Created {
		t.Fatalf("ré-import: %+v %v", rep2, err)
	}
	if n := count(t, `SELECT count(*) FROM core.transaction WHERE owner_id = $1 AND source = 'csv'`, alice.owner); n != rows {
		t.Fatalf("ré-import a ajouté des lignes: %d -> %d", rows, n)
	}
	// Chez Bob, les comptes d'Alice sont inconnus : tout est refusé, rien n'est écrit.
	_, rep3, err := imp.ImportCSV(ctx, bob.owner, bytes.NewReader(csvData), ingest.CSVOptions{})
	if err != nil || rep3.Created != 0 || rep3.Rejected == 0 {
		t.Fatalf("import chez Bob: %+v %v", rep3, err)
	}
	if n := count(t, `SELECT count(*) FROM core.transaction WHERE owner_id = $1`, bob.owner); n != 0 {
		t.Fatalf("Bob a %d lignes", n)
	}
	// Les écritures importées alimentent les positions.
	var accs gen.AccountList
	alice.doJSON("GET", base+"/accounts", nil).json(t, &accs)
	for _, a := range accs.Items {
		if a.Name == "CELI" {
			if pl := alice.positions(a); len(pl.Positions) == 0 {
				t.Fatalf("positions importées: %+v", pl)
			}
		}
	}
}
