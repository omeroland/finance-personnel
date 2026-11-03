//go:build integration

package ingest

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/pressly/goose/v3"
	"github.com/testcontainers/testcontainers-go"
	"github.com/testcontainers/testcontainers-go/modules/postgres"
	"github.com/testcontainers/testcontainers-go/wait"

	_ "github.com/jackc/pgx/v5/stdlib"

	"github.com/omeroland/finance-privee/backend/internal/modules/stockeval/store"
	"github.com/omeroland/finance-privee/backend/migrations"
)

// Même image PostgreSQL 16 épinglée que store/.
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
	db, err := goose.OpenDBWithDriver("pgx", adminDSN)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	p, err := goose.NewProvider(goose.DialectPostgres, db, migrations.FS)
	if err == nil {
		_, err = p.Up(ctx)
	}
	if err == nil {
		_, err = db.ExecContext(ctx, `ALTER ROLE finance_app LOGIN PASSWORD 'test-only-password'`)
	}
	_ = db.Close()
	if err != nil {
		fmt.Fprintln(os.Stderr, "migrations:", err)
		return 1
	}
	host, _ := ctr.Host(ctx)
	port, _ := ctr.MappedPort(ctx, "5432/tcp")
	testPool, err = pgxpool.New(ctx, fmt.Sprintf("postgres://finance_app:test-only-password@%s:%s/finance?sslmode=disable", host, port.Port()))
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	defer testPool.Close()
	return m.Run()
}

func auditCount(t *testing.T, owner uuid.UUID) int {
	t.Helper()
	var n int
	if err := testPool.QueryRow(context.Background(), `SELECT count(*) FROM core.audit_log WHERE owner_id = $1`, owner).Scan(&n); err != nil {
		t.Fatal(err)
	}
	return n
}

// TestImportAgainstPostgres prouve, sur les vrais dépôts, l'idempotence (aucun
// fait, conflit ni ligne d'audit au second import) et la protection des
// valeurs manuelles.
func TestImportAgainstPostgres(t *testing.T) {
	st := store.New(testPool)
	ctx := store.ContextWithActor(context.Background(), "tester")
	owner := uuid.New()
	imp := NewImporter(st)
	data := buildWorkbook(t, fiveStocks())
	opts := Options{Year1: 2025}

	rep, err := imp.Import(ctx, owner, bytes.NewReader(data), opts)
	if err != nil {
		t.Fatal(err)
	}
	if rep.Imported != 5 {
		t.Fatalf("importées = %d", rep.Imported)
	}
	list, _ := st.ListWatchlist(ctx, owner)
	if len(list) != 5 {
		t.Fatalf("liste de suivi = %d", len(list))
	}
	after1 := auditCount(t, owner)

	rep2, err := imp.Import(ctx, owner, bytes.NewReader(data), opts)
	if err != nil {
		t.Fatal(err)
	}
	if n := auditCount(t, owner); n != after1 {
		t.Fatalf("audit : %d -> %d au second import", after1, n)
	}
	for _, r := range rep2.Rows {
		if r.Created+r.Updated+r.Conflicts != 0 {
			t.Fatalf("second import a écrit: %+v", r)
		}
	}

	// D31 sur le vrai dépôt.
	var aaa store.Instrument
	for _, i := range list {
		if i.Symbol == "AAA" {
			aaa = i
		}
	}
	snap, err := st.GetSnapshot(ctx, owner, aaa.ID)
	if err != nil || snap.DebtToEquity.Decimal.String() != "1.1792" || snap.Source != store.SourceXLSX {
		t.Fatalf("instantané AAA = %+v, err = %v", snap, err)
	}

	// Saisie manuelle puis réimport : valeur conservée, conflit créé, une seule fois.
	one := "CAD"
	manual := store.Fact{InstrumentID: aaa.ID, FiscalYear: 2025, Metric: store.MetricEPSDiluted, Value: d("42"), Currency: &one, Source: store.SourceManual}
	if _, err := st.WriteFact(ctx, owner, manual); err != nil {
		t.Fatal(err)
	}
	for range 2 {
		if _, err := imp.Import(ctx, owner, bytes.NewReader(data), opts); err != nil {
			t.Fatal(err)
		}
	}
	facts, _ := st.ListFacts(ctx, owner, aaa.ID)
	for _, f := range facts {
		if f.FiscalYear == 2025 && f.Metric == store.MetricEPSDiluted && (f.Source != store.SourceManual || !f.Value.Equal(d("42"))) {
			t.Fatalf("valeur manuelle écrasée: %+v", f)
		}
	}
	conf, _ := st.ListOpenConflicts(ctx, owner, aaa.ID)
	if len(conf) != 1 || !conf[0].IncomingValue.Equal(d("1.7")) {
		t.Fatalf("conflits = %+v", conf)
	}
}
