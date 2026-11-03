//go:build integration

package store_test

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
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

	"github.com/omeroland/finance-privee/backend/internal/modules/stockeval/store"
	"github.com/omeroland/finance-privee/backend/migrations"
)

// Même image PostgreSQL 16 épinglée que backend/migrations.
const pgImage = "postgres@sha256:0cefaabd882e42bcb2a2f3dc56d3ce4770560ba62fd7f197f1a2e008fd5731ab"

var testPool *pgxpool.Pool

func TestMain(m *testing.M) { os.Exit(run(m)) }

func run(m *testing.M) int {
	ctx := context.Background()
	ctr, err := postgres.Run(ctx, pgImage,
		postgres.WithDatabase("finance"), postgres.WithUsername("owner"), postgres.WithPassword("owner-pw"),
		testcontainers.WithWaitStrategy(wait.ForLog("database system is ready to accept connections").
			WithOccurrence(2).WithStartupTimeout(90*time.Second)),
	)
	if err != nil {
		fmt.Fprintln(os.Stderr, "démarrer PostgreSQL:", err)
		return 1
	}
	defer func() { _ = testcontainers.TerminateContainer(ctr) }()

	adminDSN, err := ctr.ConnectionString(ctx, "sslmode=disable")
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	if err := migrate(ctx, adminDSN); err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	host, _ := ctr.Host(ctx)
	port, _ := ctr.MappedPort(ctx, "5432/tcp")
	appDSN := fmt.Sprintf("postgres://finance_app:test-only-password@%s:%s/finance?sslmode=disable", host, port.Port())
	testPool, err = pgxpool.New(ctx, appDSN)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	defer testPool.Close()
	return m.Run()
}

func migrate(ctx context.Context, adminDSN string) error {
	db, err := goose.OpenDBWithDriver("pgx", adminDSN)
	if err != nil {
		return fmt.Errorf("ouvrir la base: %w", err)
	}
	defer db.Close()
	p, err := goose.NewProvider(goose.DialectPostgres, db, migrations.FS)
	if err != nil {
		return err
	}
	if _, err := p.Up(ctx); err != nil {
		return fmt.Errorf("migrations: %w", err)
	}
	_, err = db.ExecContext(ctx, `ALTER ROLE finance_app LOGIN PASSWORD 'test-only-password'`)
	return err
}

func newStore(t *testing.T) (*store.Store, context.Context) {
	t.Helper()
	return store.New(testPool), store.ContextWithActor(context.Background(), "tester")
}

func auditCount(t *testing.T, owner uuid.UUID) int {
	t.Helper()
	var n int
	if err := testPool.QueryRow(context.Background(), `SELECT count(*) FROM core.audit_log WHERE owner_id = $1`, owner).Scan(&n); err != nil {
		t.Fatal(err)
	}
	return n
}

func addStock(t *testing.T, ctx context.Context, s *store.Store, owner uuid.UUID, symbol string) store.Instrument {
	t.Helper()
	in, err := s.AddToWatchlist(ctx, owner, store.InstrumentInput{
		Symbol: symbol, Name: symbol + " Inc.", Exchange: "TSX", QuoteCurrency: "CAD", ReportCurrency: "CAD",
	})
	if err != nil {
		t.Fatalf("ajouter %s: %v", symbol, err)
	}
	return in
}

func dec(s string) decimal.Decimal { return decimal.RequireFromString(s) }

func TestWatchlist(t *testing.T) {
	s, ctx := newStore(t)
	owner := uuid.New()

	a := addStock(t, ctx, s, owner, "RY")
	if n := auditCount(t, owner); n != 1 {
		t.Fatalf("audit après ajout = %d, attendu 1", n)
	}
	again := addStock(t, ctx, s, owner, "RY")
	if again.ID != a.ID || auditCount(t, owner) != 1 {
		t.Fatalf("ajout idempotent attendu (audit=%d)", auditCount(t, owner))
	}
	addStock(t, ctx, s, owner, "BNS")
	list, err := s.ListWatchlist(ctx, owner)
	if err != nil || len(list) != 2 || list[0].Symbol != "BNS" {
		t.Fatalf("liste = %+v, err = %v", list, err)
	}
	if _, err := s.AddToWatchlist(ctx, owner, store.InstrumentInput{Symbol: "", QuoteCurrency: "CAD", ReportCurrency: "CAD"}); !errors.Is(err, store.ErrInvalid) {
		t.Fatalf("symbole vide: err = %v", err)
	}

	// Retrait puis réajout du même symbole : l'instrument est réutilisé.
	if err := s.RemoveFromWatchlist(ctx, owner, a.ID); err != nil {
		t.Fatal(err)
	}
	if err := s.RemoveFromWatchlist(ctx, owner, a.ID); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("second retrait: err = %v", err)
	}
	before := auditCount(t, owner)
	back := addStock(t, ctx, s, owner, "RY")
	if back.ID != a.ID || auditCount(t, owner) != before+1 {
		t.Fatalf("réajout: id=%v audit=%d", back.ID, auditCount(t, owner))
	}
	got, err := s.GetInstrument(ctx, owner, a.ID)
	if err != nil || got.Symbol != "RY" {
		t.Fatalf("GetInstrument = %+v, %v", got, err)
	}
}

func TestFactPriority(t *testing.T) {
	s, ctx := newStore(t)
	owner := uuid.New()
	inst := addStock(t, ctx, s, owner, "TD")
	cad := "CAD"
	fact := func(v, src string) store.Fact {
		return store.Fact{InstrumentID: inst.ID, FiscalYear: 2024, Metric: store.MetricEPSDiluted, Value: dec(v), Currency: &cad, Source: src}
	}
	step := func(name string, f store.Fact, want store.FactOutcome, wantAudit int) {
		t.Helper()
		before := auditCount(t, owner)
		got, err := s.WriteFact(ctx, owner, f)
		if err != nil || got != want {
			t.Fatalf("%s: outcome = %q, err = %v (attendu %q)", name, got, err, want)
		}
		if d := auditCount(t, owner) - before; d != wantAudit {
			t.Fatalf("%s: %d lignes d'audit, attendu %d", name, d, wantAudit)
		}
	}

	step("création manuelle", fact("5.00", store.SourceManual), store.FactCreated, 1)
	step("identique", fact("5.00", store.SourceManual), store.FactNoop, 0)
	step("identique, autre échelle", fact("5.0000", store.SourceManual), store.FactNoop, 0)
	step("xlsx identique", fact("5.00", store.SourceXLSX), store.FactNoop, 0)
	step("xlsx différent", fact("6.00", store.SourceXLSX), store.FactConflicted, 1)
	step("xlsx différent répété", fact("6.00", store.SourceXLSX), store.FactNoop, 0)
	step("provider différent", fact("7.00", store.SourceProvider), store.FactConflicted, 1)

	facts, err := s.ListFacts(ctx, owner, inst.ID)
	if err != nil || len(facts) != 1 || !facts[0].Value.Equal(dec("5")) || facts[0].Source != store.SourceManual {
		t.Fatalf("la valeur manuelle doit survivre: %+v, %v", facts, err)
	}
	conflicts, err := s.ListOpenConflicts(ctx, owner, inst.ID)
	if err != nil || len(conflicts) != 2 {
		t.Fatalf("conflits = %+v, %v", conflicts, err)
	}
	c := conflicts[0]
	if !c.ExistingValue.Equal(dec("5")) || c.ExistingSource != store.SourceManual ||
		!c.IncomingValue.Equal(dec("6")) || c.IncomingSource != store.SourceXLSX {
		t.Fatalf("conflit inattendu: %+v", c)
	}

	// Source non manuelle : remplacée par une valeur différente, sans conflit.
	m := fact("1.00", store.SourceXLSX)
	m.FiscalYear = 2023
	step("xlsx créé", m, store.FactCreated, 1)
	m.Value, m.Source = dec("2.00"), store.SourceProvider
	step("provider remplace xlsx", m, store.FactUpdated, 1)
	// Le manuel remplace tout.
	m.Value, m.Source = dec("3.00"), store.SourceManual
	step("manuel remplace", m, store.FactUpdated, 1)
	// Même valeur, source manuelle sur ligne xlsx : la source est promue.
	p := fact("9.00", store.SourceXLSX)
	p.FiscalYear = 2022
	step("xlsx 2022", p, store.FactCreated, 1)
	p.Source = store.SourceManual
	step("promotion en manuel", p, store.FactUpdated, 1)

	for name, bad := range map[string]store.Fact{
		"source":  {InstrumentID: inst.ID, FiscalYear: 2024, Metric: store.MetricDPS, Value: dec("1"), Source: "x"},
		"mesure":  {InstrumentID: inst.ID, FiscalYear: 2024, Metric: "nope", Value: dec("1"), Source: store.SourceManual},
		"année":   {InstrumentID: inst.ID, FiscalYear: 1800, Metric: store.MetricDPS, Value: dec("1"), Source: store.SourceManual},
		"devise":  {InstrumentID: inst.ID, FiscalYear: 2024, Metric: store.MetricDPS, Value: dec("1"), Source: store.SourceManual, Currency: new(string)},
		"inconnu": {InstrumentID: uuid.New(), FiscalYear: 2024, Metric: store.MetricDPS, Value: dec("1"), Source: store.SourceManual},
	} {
		if _, err := s.WriteFact(ctx, owner, bad); err == nil {
			t.Errorf("fait invalide (%s) accepté", name)
		}
	}
}

func TestSnapshotAndQualitative(t *testing.T) {
	s, ctx := newStore(t)
	owner := uuid.New()
	inst := addStock(t, ctx, s, owner, "ENB")
	asOf := time.Date(2025, 1, 31, 15, 4, 5, 123456000, time.UTC)

	if _, err := s.GetSnapshot(ctx, owner, inst.ID); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("instantané absent: %v", err)
	}
	snap := store.Snapshot{
		InstrumentID: inst.ID, Price: decimal.NewNullDecimal(dec("50.25")),
		PayoutRatioImported: decimal.NewNullDecimal(dec("0.70")), AsOf: asOf, Source: store.SourceManual,
	}
	steps := []struct {
		name    string
		mut     func(*store.Snapshot)
		written bool
		audit   int
	}{
		{"création", func(*store.Snapshot) {}, true, 1},
		{"identique", func(*store.Snapshot) {}, true, 0},
		{"xlsx ne touche pas le manuel", func(x *store.Snapshot) { x.Source = store.SourceXLSX; x.Price = decimal.NewNullDecimal(dec("1")) }, false, 0},
		{"manuel modifié", func(x *store.Snapshot) { x.Price = decimal.NewNullDecimal(dec("51")) }, true, 1},
	}
	for _, st := range steps {
		st.mut(&snap)
		before := auditCount(t, owner)
		w, err := s.SaveSnapshot(ctx, owner, snap)
		if err != nil || w != st.written || auditCount(t, owner)-before != st.audit {
			t.Fatalf("%s: written=%v err=%v audit=%d", st.name, w, err, auditCount(t, owner)-before)
		}
		if !st.written {
			snap.Source, snap.Price = store.SourceManual, decimal.NewNullDecimal(dec("51"))
		}
	}
	got, err := s.GetSnapshot(ctx, owner, inst.ID)
	if err != nil || !got.Price.Decimal.Equal(dec("51")) || got.PayoutRatioImported.Decimal.String() != "0.7" || got.Equity.Valid {
		t.Fatalf("instantané relu = %+v, %v", got, err)
	}
	if _, err := s.SaveSnapshot(ctx, owner, store.Snapshot{InstrumentID: inst.ID, Source: "x", AsOf: asOf}); !errors.Is(err, store.ErrInvalid) {
		t.Fatalf("source invalide: %v", err)
	}

	// Qualitatif.
	if _, err := s.GetQualitative(ctx, owner, inst.ID); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("qualitatif absent: %v", err)
	}
	yes := true
	q := store.Qualitative{InstrumentID: inst.ID, R1: &yes, Notes: json.RawMessage(`{"r1": "ok", "a": 1}`), ReviewedAt: &asOf}
	before := auditCount(t, owner)
	if err := s.SaveQualitative(ctx, owner, q); err != nil {
		t.Fatal(err)
	}
	q.Notes = json.RawMessage(`{"a":1,"r1":"ok"}`) // même contenu, autre ordre
	if err := s.SaveQualitative(ctx, owner, q); err != nil {
		t.Fatal(err)
	}
	if d := auditCount(t, owner) - before; d != 1 {
		t.Fatalf("audit qualitatif = %d, attendu 1", d)
	}
	no := false
	q.R2 = &no
	if err := s.SaveQualitative(ctx, owner, q); err != nil {
		t.Fatal(err)
	}
	if d := auditCount(t, owner) - before; d != 2 {
		t.Fatalf("audit qualitatif après modification = %d, attendu 2", d)
	}
	gq, err := s.GetQualitative(ctx, owner, inst.ID)
	if err != nil || gq.R1 == nil || !*gq.R1 || gq.R2 == nil || *gq.R2 || gq.R3 != nil {
		t.Fatalf("qualitatif relu = %+v, %v", gq, err)
	}
	if err := s.SaveQualitative(ctx, owner, store.Qualitative{InstrumentID: inst.ID, Notes: json.RawMessage(`{`)}); !errors.Is(err, store.ErrInvalid) {
		t.Fatalf("notes invalides: %v", err)
	}
	// Notes vides : défaut {}.
	if err := s.SaveQualitative(ctx, owner, store.Qualitative{InstrumentID: inst.ID}); err != nil {
		t.Fatal(err)
	}
}

func TestEvaluationArchive(t *testing.T) {
	s, ctx := newStore(t)
	owner := uuid.New()
	inst := addStock(t, ctx, s, owner, "CNQ")

	in := store.EvaluationInput{
		InstrumentID: inst.ID, Mode: "strict", RulesVersion: "1", RulesHash: "abc",
		Inputs: json.RawMessage(`{ "b": [1, 2.50], "a": "é<" }`), Result: json.RawMessage(`{"verdict":"pass"}`),
	}
	before := auditCount(t, owner)
	ev, err := s.ArchiveEvaluation(ctx, owner, in)
	if err != nil {
		t.Fatal(err)
	}
	if auditCount(t, owner)-before != 1 {
		t.Fatal("une ligne d'audit attendue")
	}
	sum := sha256.Sum256([]byte(`{"a":"é<","b":[1,2.50]}`))
	if want := hex.EncodeToString(sum[:]); ev.InputsHash != want {
		t.Fatalf("empreinte = %s, attendu %s", ev.InputsHash, want)
	}
	if h, err := store.InputsHash([]byte(`{"b":[1,2.50],"a":"é<"}`)); err != nil || h != ev.InputsHash {
		t.Fatalf("InputsHash = %s, %v", h, err)
	}
	if _, err := store.InputsHash([]byte(`{} {}`)); err == nil {
		t.Fatal("JSON avec reste accepté")
	}
	got, err := s.GetEvaluation(ctx, owner, ev.ID)
	if err != nil || got.InputsHash != ev.InputsHash {
		t.Fatalf("relecture: %+v, %v", got, err)
	}
	if _, err := s.ArchiveEvaluation(ctx, owner, in); err != nil {
		t.Fatal(err)
	}
	list, err := s.ListEvaluations(ctx, owner, inst.ID, 0)
	if err != nil || len(list) != 2 {
		t.Fatalf("liste = %d, %v", len(list), err)
	}
	bad := in
	bad.Inputs = json.RawMessage(`nope`)
	if _, err := s.ArchiveEvaluation(ctx, owner, bad); !errors.Is(err, store.ErrInvalid) {
		t.Fatalf("entrées invalides: %v", err)
	}
	bad = in
	bad.Mode = ""
	if _, err := s.ArchiveEvaluation(ctx, owner, bad); !errors.Is(err, store.ErrInvalid) {
		t.Fatalf("mode vide: %v", err)
	}

	// L'archive est immuable pour le rôle applicatif.
	if _, err := testPool.Exec(context.Background(), `UPDATE stockeval.evaluation SET mode = 'x' WHERE id = $1`, ev.ID); err == nil {
		t.Fatal("UPDATE sur l'archive accepté")
	}
	if _, err := testPool.Exec(context.Background(), `DELETE FROM stockeval.evaluation WHERE id = $1`, ev.ID); err == nil {
		t.Fatal("DELETE sur l'archive accepté")
	}
}

func TestImportJobs(t *testing.T) {
	s, ctx := newStore(t)
	owner := uuid.New()

	if _, err := s.CreateImportJob(ctx, owner, "csv"); !errors.Is(err, store.ErrInvalid) {
		t.Fatalf("type invalide: %v", err)
	}
	j, err := s.CreateImportJob(ctx, owner, store.JobTypeXLSX)
	if err != nil || j.Status != store.JobPending {
		t.Fatalf("création: %+v, %v", j, err)
	}
	now := time.Now()
	done, err := s.FinishImportJob(ctx, owner, j.ID, store.JobSucceeded, json.RawMessage(`{"rows":3}`), &now)
	if err != nil || done.Status != store.JobSucceeded || done.FinishedAt == nil {
		t.Fatalf("fin: %+v, %v", done, err)
	}
	if n := auditCount(t, owner); n != 2 {
		t.Fatalf("audit = %d, attendu 2", n)
	}
	if _, err := s.FinishImportJob(ctx, owner, j.ID, "weird", nil, nil); !errors.Is(err, store.ErrInvalid) {
		t.Fatalf("statut invalide: %v", err)
	}
	if _, err := s.FinishImportJob(ctx, owner, j.ID, store.JobFailed, json.RawMessage(`{`), nil); !errors.Is(err, store.ErrInvalid) {
		t.Fatalf("rapport invalide: %v", err)
	}
	if _, err := s.FinishImportJob(ctx, owner, uuid.New(), store.JobFailed, nil, nil); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("job inconnu: %v", err)
	}
	if got, err := s.GetImportJob(ctx, owner, j.ID); err != nil || got.ID != j.ID {
		t.Fatalf("lecture: %+v, %v", got, err)
	}
	if list, err := s.ListImportJobs(ctx, owner, 10); err != nil || len(list) != 1 {
		t.Fatalf("liste: %d, %v", len(list), err)
	}
}

// TestOwnerIsolation : le propriétaire B ne lit ni n'écrit rien du propriétaire A,
// sur chaque requête de lecture et chaque écriture visant un instrument.
func TestOwnerIsolation(t *testing.T) {
	s, ctx := newStore(t)
	a, b := uuid.New(), uuid.New()
	inst := addStock(t, ctx, s, a, "SHOP")
	cad := "CAD"
	if _, err := s.WriteFact(ctx, a, store.Fact{InstrumentID: inst.ID, FiscalYear: 2024, Metric: store.MetricDPS, Value: dec("1"), Currency: &cad, Source: store.SourceManual}); err != nil {
		t.Fatal(err)
	}
	if _, err := s.WriteFact(ctx, a, store.Fact{InstrumentID: inst.ID, FiscalYear: 2024, Metric: store.MetricDPS, Value: dec("2"), Source: store.SourceXLSX}); err != nil {
		t.Fatal(err)
	}
	if _, err := s.SaveSnapshot(ctx, a, store.Snapshot{InstrumentID: inst.ID, AsOf: time.Now(), Source: store.SourceManual}); err != nil {
		t.Fatal(err)
	}
	if err := s.SaveQualitative(ctx, a, store.Qualitative{InstrumentID: inst.ID}); err != nil {
		t.Fatal(err)
	}
	ev, err := s.ArchiveEvaluation(ctx, a, store.EvaluationInput{
		InstrumentID: inst.ID, Mode: "strict", RulesVersion: "1", RulesHash: "h",
		Inputs: json.RawMessage(`{}`), Result: json.RawMessage(`{}`),
	})
	if err != nil {
		t.Fatal(err)
	}
	job, err := s.CreateImportJob(ctx, a, store.JobTypeXLSX)
	if err != nil {
		t.Fatal(err)
	}

	// Lectures de B : vides ou introuvables.
	if l, err := s.ListWatchlist(ctx, b); err != nil || len(l) != 0 {
		t.Errorf("ListWatchlist(B) = %v, %v", l, err)
	}
	if _, err := s.GetInstrument(ctx, b, inst.ID); !errors.Is(err, store.ErrNotFound) {
		t.Errorf("GetInstrument(B): %v", err)
	}
	if l, err := s.ListFacts(ctx, b, inst.ID); err != nil || len(l) != 0 {
		t.Errorf("ListFacts(B) = %v, %v", l, err)
	}
	if l, err := s.ListOpenConflicts(ctx, b, inst.ID); err != nil || len(l) != 0 {
		t.Errorf("ListOpenConflicts(B) = %v, %v", l, err)
	}
	if _, err := s.GetSnapshot(ctx, b, inst.ID); !errors.Is(err, store.ErrNotFound) {
		t.Errorf("GetSnapshot(B): %v", err)
	}
	if _, err := s.GetQualitative(ctx, b, inst.ID); !errors.Is(err, store.ErrNotFound) {
		t.Errorf("GetQualitative(B): %v", err)
	}
	if _, err := s.GetEvaluation(ctx, b, ev.ID); !errors.Is(err, store.ErrNotFound) {
		t.Errorf("GetEvaluation(B): %v", err)
	}
	if l, err := s.ListEvaluations(ctx, b, inst.ID, 10); err != nil || len(l) != 0 {
		t.Errorf("ListEvaluations(B) = %v, %v", l, err)
	}
	if _, err := s.GetImportJob(ctx, b, job.ID); !errors.Is(err, store.ErrNotFound) {
		t.Errorf("GetImportJob(B): %v", err)
	}
	if l, err := s.ListImportJobs(ctx, b, 10); err != nil || len(l) != 0 {
		t.Errorf("ListImportJobs(B) = %v, %v", l, err)
	}

	// Écritures de B sur l'instrument de A : refusées, sans audit.
	if _, err := s.WriteFact(ctx, b, store.Fact{InstrumentID: inst.ID, FiscalYear: 2024, Metric: store.MetricDPS, Value: dec("9"), Source: store.SourceManual}); !errors.Is(err, store.ErrNotFound) {
		t.Errorf("WriteFact(B): %v", err)
	}
	if _, err := s.SaveSnapshot(ctx, b, store.Snapshot{InstrumentID: inst.ID, AsOf: time.Now(), Source: store.SourceManual}); !errors.Is(err, store.ErrNotFound) {
		t.Errorf("SaveSnapshot(B): %v", err)
	}
	if err := s.SaveQualitative(ctx, b, store.Qualitative{InstrumentID: inst.ID}); !errors.Is(err, store.ErrNotFound) {
		t.Errorf("SaveQualitative(B): %v", err)
	}
	if _, err := s.ArchiveEvaluation(ctx, b, store.EvaluationInput{InstrumentID: inst.ID, Mode: "m", RulesVersion: "1", RulesHash: "h", Inputs: json.RawMessage(`{}`), Result: json.RawMessage(`{}`)}); !errors.Is(err, store.ErrNotFound) {
		t.Errorf("ArchiveEvaluation(B): %v", err)
	}
	if err := s.RemoveFromWatchlist(ctx, b, inst.ID); !errors.Is(err, store.ErrNotFound) {
		t.Errorf("RemoveFromWatchlist(B): %v", err)
	}
	if _, err := s.FinishImportJob(ctx, b, job.ID, store.JobFailed, nil, nil); !errors.Is(err, store.ErrNotFound) {
		t.Errorf("FinishImportJob(B): %v", err)
	}
	if n := auditCount(t, b); n != 0 {
		t.Errorf("audit de B = %d, attendu 0", n)
	}

	// B peut avoir le même symbole : unicité par propriétaire (D40).
	other := addStock(t, ctx, s, b, "SHOP")
	if other.ID == inst.ID {
		t.Error("B partage l'instrument de A")
	}
	if l, _ := s.ListWatchlist(ctx, a); len(l) != 1 {
		t.Errorf("la liste de A a changé: %d", len(l))
	}
}

func TestActorAudited(t *testing.T) {
	s := store.New(testPool)
	owner := uuid.New()
	ctx := store.ContextWithActor(context.Background(), "sub-123")
	addStock(t, ctx, s, owner, "XYZ")
	if _, err := s.AddToWatchlist(context.Background(), owner, store.InstrumentInput{Symbol: "NOACTOR", QuoteCurrency: "CAD", ReportCurrency: "CAD"}); err != nil {
		t.Fatal(err)
	}
	rows, err := testPool.Query(context.Background(), `SELECT actor FROM core.audit_log WHERE owner_id = $1 ORDER BY id`, owner)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	var actors []string
	for rows.Next() {
		var a string
		if err := rows.Scan(&a); err != nil {
			t.Fatal(err)
		}
		actors = append(actors, a)
	}
	if len(actors) != 2 || actors[0] != "sub-123" || actors[1] != "system" {
		t.Fatalf("acteurs = %v", actors)
	}
}
