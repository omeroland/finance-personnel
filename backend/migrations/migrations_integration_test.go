//go:build integration

package migrations_test

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
	"testing"
	"time"

	_ "github.com/jackc/pgx/v5/stdlib"
	"github.com/pressly/goose/v3"
	"github.com/testcontainers/testcontainers-go"
	"github.com/testcontainers/testcontainers-go/modules/postgres"
	"github.com/testcontainers/testcontainers-go/wait"

	"github.com/omeroland/finance-privee/backend/migrations"
)

// Image PostgreSQL 16 épinglée par digest (postgres:16-alpine).
const pgImage = "postgres@sha256:0cefaabd882e42bcb2a2f3dc56d3ce4770560ba62fd7f197f1a2e008fd5731ab"

const appPassword = "test-only-password"

func startPostgres(t *testing.T) (adminDSN, appDSN string) {
	t.Helper()
	ctx := context.Background()
	ctr, err := postgres.Run(ctx, pgImage,
		postgres.WithDatabase("finance"),
		postgres.WithUsername("owner"),
		postgres.WithPassword("owner-pw"),
		testcontainers.WithWaitStrategy(
			wait.ForLog("database system is ready to accept connections").
				WithOccurrence(2).WithStartupTimeout(90*time.Second)),
	)
	if err != nil {
		t.Fatalf("démarrer PostgreSQL: %v", err)
	}
	t.Cleanup(func() { _ = testcontainers.TerminateContainer(ctr) })

	adminDSN, err = ctr.ConnectionString(ctx, "sslmode=disable")
	if err != nil {
		t.Fatalf("chaîne de connexion: %v", err)
	}
	host, err := ctr.Host(ctx)
	if err != nil {
		t.Fatal(err)
	}
	port, err := ctr.MappedPort(ctx, "5432/tcp")
	if err != nil {
		t.Fatal(err)
	}
	appDSN = fmt.Sprintf("postgres://finance_app:%s@%s:%s/finance?sslmode=disable", appPassword, host, port.Port())
	return adminDSN, appDSN
}

func openDB(t *testing.T, dsn string) *sql.DB {
	t.Helper()
	db, err := sql.Open("pgx", dsn)
	if err != nil {
		t.Fatalf("ouvrir %q: %v", "db", err)
	}
	t.Cleanup(func() { _ = db.Close() })
	return db
}

func newProvider(t *testing.T, db *sql.DB) *goose.Provider {
	t.Helper()
	p, err := goose.NewProvider(goose.DialectPostgres, db, migrations.FS)
	if err != nil {
		t.Fatalf("fournisseur goose: %v", err)
	}
	return p
}

func TestMigrations(t *testing.T) {
	ctx := context.Background()
	adminDSN, appDSN := startPostgres(t)
	admin := openDB(t, adminDSN)
	p := newProvider(t, admin)

	if _, err := p.Up(ctx); err != nil {
		t.Fatalf("up: %v", err)
	}
	if _, err := p.DownTo(ctx, 0); err != nil {
		t.Fatalf("down: %v", err)
	}
	var n int
	if err := admin.QueryRowContext(ctx,
		`SELECT count(*) FROM information_schema.schemata WHERE schema_name IN ('core','stockeval','etf')`).Scan(&n); err != nil {
		t.Fatal(err)
	}
	if n != 0 {
		t.Fatalf("après down, %d schémas subsistent", n)
	}
	if _, err := p.Up(ctx); err != nil {
		t.Fatalf("second up: %v", err)
	}

	t.Run("owner_id NOT NULL partout", func(t *testing.T) {
		rows, err := admin.QueryContext(ctx, `
			SELECT t.table_schema || '.' || t.table_name,
			       coalesce(c.is_nullable, 'ABSENT')
			FROM information_schema.tables t
			LEFT JOIN information_schema.columns c
			  ON c.table_schema = t.table_schema AND c.table_name = t.table_name AND c.column_name = 'owner_id'
			WHERE t.table_schema IN ('core', 'stockeval', 'etf') AND t.table_type = 'BASE TABLE'`)
		if err != nil {
			t.Fatal(err)
		}
		defer rows.Close()
		count := 0
		for rows.Next() {
			var name, nullable string
			if err := rows.Scan(&name, &nullable); err != nil {
				t.Fatal(err)
			}
			count++
			if nullable != "NO" {
				t.Errorf("%s: owner_id %s (attendu NOT NULL)", name, nullable)
			}
		}
		if err := rows.Err(); err != nil {
			t.Fatal(err)
		}
		if count != 16 {
			t.Errorf("%d tables trouvées, 16 attendues", count)
		}
	})

	t.Run("finance_app insertion seule", func(t *testing.T) {
		if _, err := admin.ExecContext(ctx, `ALTER ROLE finance_app LOGIN PASSWORD '`+appPassword+`'`); err != nil {
			t.Fatal(err)
		}
		const owner = "00000000-0000-0000-0000-000000000001"
		var instrument string
		if err := admin.QueryRowContext(ctx, `
			INSERT INTO core.instrument (owner_id, symbol, name, exchange, kind, quote_currency, report_currency)
			VALUES ($1, 'TEST.TO', 'Test', 'TSX', 'stock', 'CAD', 'CAD') RETURNING id`, owner).Scan(&instrument); err != nil {
			t.Fatal(err)
		}

		app := openDB(t, appDSN)
		if _, err := app.ExecContext(ctx, `
			INSERT INTO stockeval.evaluation (owner_id, instrument_id, mode, rules_version, rules_hash, inputs_hash, inputs, result)
			VALUES ($1, $2, 'strict', 'v1', 'h', 'h', '{}', '{}')`, owner, instrument); err != nil {
			t.Fatalf("INSERT evaluation: %v", err)
		}
		if _, err := app.ExecContext(ctx, `
			INSERT INTO core.audit_log (owner_id, actor, action, entity) VALUES ($1, 'a', 'create', 'evaluation')`, owner); err != nil {
			t.Fatalf("INSERT audit_log: %v", err)
		}
		for _, q := range []string{
			`UPDATE stockeval.evaluation SET mode = 'x'`,
			`DELETE FROM stockeval.evaluation`,
			`UPDATE core.audit_log SET actor = 'x'`,
			`DELETE FROM core.audit_log`,
		} {
			_, err := app.ExecContext(ctx, q)
			if err == nil || !strings.Contains(err.Error(), "permission denied") {
				t.Errorf("%s: erreur %v, « permission denied » attendu", q, err)
			}
		}
		// Les autres tables restent modifiables.
		if _, err := app.ExecContext(ctx, `UPDATE core.instrument SET name = 'Autre'`); err != nil {
			t.Errorf("UPDATE instrument: %v", err)
		}
	})
}
