//go:build integration

package httpapi_test

import (
	"context"
	"fmt"
	"os"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/pressly/goose/v3"
	"github.com/testcontainers/testcontainers-go"
	"github.com/testcontainers/testcontainers-go/modules/postgres"
	"github.com/testcontainers/testcontainers-go/wait"

	_ "github.com/jackc/pgx/v5/stdlib"

	"github.com/omeroland/finance-privee/backend/internal/api/gen"
	"github.com/omeroland/finance-privee/backend/internal/modules/stockeval/ingest"
	"github.com/omeroland/finance-privee/backend/internal/modules/stockeval/store"
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

// Pile complète (HTTP, store, ingest, PostgreSQL) : réponses validées contre le contrat, acteur d'audit
// issu du JWT, et isolation entre propriétaires.
func TestFullStackWithPostgres(t *testing.T) {
	p := pool(t)
	st := store.New(p)
	ownerA, ownerB := uuid.New(), uuid.New()
	a := newAPIWith(t, st, ingest.NewImporter(st), ownerA)
	b := newAPIWith(t, st, ingest.NewImporter(st), ownerB)

	a.addStock(sym)
	a.fillPerfect(sym)

	var list gen.StockList
	a.doJSON("GET", base+"/stocks?horizon=20", nil).json(t, &list)
	if len(list.Items) != 1 || list.Items[0].Score.PerfectHorizon == nil || *list.Items[0].Score.PerfectHorizon != 20 {
		t.Fatalf("liste: %+v", list)
	}
	var rec gen.EvaluationRecord
	r := a.doJSON("POST", base+"/stocks/"+sym+"/evaluations", gen.EvaluationCreate{})
	r.json(t, &rec)
	if r.Status != 201 || len(rec.InputsFingerprint) != 64 {
		t.Fatalf("archive: %d %s", r.Status, r.Body)
	}
	var hist gen.EvaluationRecordList
	a.doJSON("GET", base+"/stocks/"+sym+"/evaluations", nil).json(t, &hist)
	if len(hist.Items) != 1 || hist.Items[0].Id != rec.Id {
		t.Fatalf("historique: %s", r.Body)
	}
	if r := a.doJSON("GET", base+"/stocks/"+sym+"/evaluation?mode=compat", nil); r.Status != 200 {
		t.Fatalf("compat: %d %s", r.Status, r.Body)
	}
	if r := a.doJSON("PATCH", base+"/stocks/"+sym, gen.StockUpdate{Name: ptrS("Autre nom")}); r.Status != 200 {
		t.Fatalf("patch: %d %s", r.Status, r.Body)
	}

	// L'acteur de l'audit est le sub du JWT (P1-13), jamais « system ».
	rows, err := p.Query(context.Background(), `SELECT DISTINCT actor FROM core.audit_log WHERE owner_id = $1`, ownerA)
	if err != nil {
		t.Fatal(err)
	}
	var actors []string
	for rows.Next() {
		var s string
		if err := rows.Scan(&s); err != nil {
			t.Fatal(err)
		}
		actors = append(actors, s)
	}
	rows.Close()
	if len(actors) != 1 || actors[0] != ownerA.String() {
		t.Fatalf("acteurs d'audit = %v, attendu [%s]", actors, ownerA)
	}

	// Un autre propriétaire ne voit rien et peut suivre le même symbole.
	b.doJSON("GET", base+"/stocks", nil).json(t, &list)
	if len(list.Items) != 0 {
		t.Fatalf("fuite entre propriétaires: %+v", list)
	}
	if r := b.doJSON("GET", base+"/stocks/"+sym, nil); r.Status != 404 {
		t.Fatalf("fiche d'un autre propriétaire: %d", r.Status)
	}
	if r := b.doJSON("GET", base+"/stocks/"+sym+"/evaluations", nil); r.Status != 404 {
		t.Fatalf("historique d'un autre propriétaire: %d", r.Status)
	}
	b.addStock(sym)

	// Import du gabarit vide : le job se termine, visible seulement de son propriétaire.
	file := mustRead(t, "../../../../../testdata/xlsx/simply_investing_template.xlsx")
	body, ct := multipartBody(t, "2006", file)
	var job gen.Job
	r = a.do("POST", base+"/imports/xlsx", ct, body)
	r.json(t, &job)
	if r.Status != 202 {
		t.Fatalf("import: %d %s", r.Status, r.Body)
	}
	if done := a.waitJob(job.Id.String()); done.Status != gen.JobStatusSucceeded {
		t.Fatalf("job: %+v", done)
	}
	if r := b.doJSON("GET", base+"/imports/"+job.Id.String(), nil); r.Status != 404 {
		t.Fatalf("job d'un autre propriétaire: %d", r.Status)
	}
}

func mustRead(t *testing.T, path string) []byte {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return b
}
