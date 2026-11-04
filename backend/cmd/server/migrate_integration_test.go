//go:build integration

package main

import (
	"context"
	"database/sql"
	"fmt"
	"io"
	"log/slog"
	"net/url"
	"testing"
	"time"

	"github.com/testcontainers/testcontainers-go"
	"github.com/testcontainers/testcontainers-go/modules/postgres"
	"github.com/testcontainers/testcontainers-go/wait"
)

// Image PostgreSQL 16 épinglée par digest (postgres:16-alpine).
const pgImage = "postgres@sha256:0cefaabd882e42bcb2a2f3dc56d3ce4770560ba62fd7f197f1a2e008fd5731ab"

func TestRunMigrate(t *testing.T) {
	ctx := context.Background()
	ctr, err := postgres.Run(ctx, pgImage,
		postgres.WithDatabase("finance"), postgres.WithUsername("owner"), postgres.WithPassword("owner-pw"),
		testcontainers.WithWaitStrategy(wait.ForLog("database system is ready to accept connections").
			WithOccurrence(2).WithStartupTimeout(90*time.Second)))
	if err != nil {
		t.Fatalf("démarrer PostgreSQL: %v", err)
	}
	t.Cleanup(func() { _ = testcontainers.TerminateContainer(ctr) })
	ownerURL, err := ctr.ConnectionString(ctx, "sslmode=disable")
	if err != nil {
		t.Fatal(err)
	}
	host, err := ctr.Host(ctx)
	if err != nil {
		t.Fatal(err)
	}
	port, err := ctr.MappedPort(ctx, "5432/tcp")
	if err != nil {
		t.Fatal(err)
	}

	logger := slog.New(slog.NewJSONHandler(io.Discard, nil))
	// Un mot de passe hostile (apostrophe, guillemet, antislash) doit être posé tel quel.
	const pw = `p'a"ss\w;--ord`
	cfg := migrateConfig{ownerURL: ownerURL, appPassword: pw}
	for i := range 2 { // idempotent
		if err := runMigrate(ctx, logger, cfg); err != nil {
			t.Fatalf("migrate #%d: %v", i+1, err)
		}
	}

	appURL := fmt.Sprintf("postgres://finance_app:%s@%s:%s/finance?sslmode=disable", url.QueryEscape(pw), host, port.Port())
	app, err := sql.Open("pgx", appURL)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = app.Close() })
	var n int
	if err := app.QueryRowContext(ctx, `SELECT count(*) FROM core.instrument`).Scan(&n); err != nil {
		t.Fatalf("finance_app doit pouvoir se connecter et lire: %v", err)
	}

	t.Run("sans mot de passe ou sans URL", func(t *testing.T) {
		if err := runMigrate(ctx, logger, migrateConfig{ownerURL: ownerURL}); err == nil {
			t.Error("mot de passe vide accepté")
		}
		if err := runMigrate(ctx, logger, migrateConfig{appPassword: pw}); err == nil {
			t.Error("URL vide acceptée")
		}
	})
}
