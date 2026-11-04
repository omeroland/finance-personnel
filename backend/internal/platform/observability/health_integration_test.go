//go:build integration

package observability_test

import (
	"context"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/testcontainers/testcontainers-go"
	"github.com/testcontainers/testcontainers-go/modules/postgres"
	"github.com/testcontainers/testcontainers-go/wait"

	"github.com/omeroland/finance-privee/backend/internal/platform/observability"
)

// Même image PostgreSQL 16 épinglée que backend/migrations.
const pgImage = "postgres@sha256:0cefaabd882e42bcb2a2f3dc56d3ce4770560ba62fd7f197f1a2e008fd5731ab"

// /readyz passe à 503 quand la base est arrêtée, et revient à 200 à son retour.
func TestReadyzFollowsDatabase(t *testing.T) {
	ctx := context.Background()
	ctr, err := postgres.Run(ctx, pgImage,
		postgres.WithDatabase("finance"), postgres.WithUsername("owner"), postgres.WithPassword("owner-pw"),
		testcontainers.WithWaitStrategy(wait.ForLog("database system is ready to accept connections").
			WithOccurrence(2).WithStartupTimeout(90*time.Second)),
	)
	if err != nil {
		t.Fatalf("démarrer PostgreSQL: %v", err)
	}
	t.Cleanup(func() { _ = testcontainers.TerminateContainer(ctr) })
	dsn, err := ctr.ConnectionString(ctx, "sslmode=disable")
	if err != nil {
		t.Fatal(err)
	}
	pool, err := pgxpool.New(ctx, dsn)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(pool.Close)

	jwksCached := func(context.Context) error { return nil } // JWKS déjà en cache
	h := observability.Readyz(slog.New(slog.NewJSONHandler(io.Discard, nil)), pool.Ping, jwksCached)
	get := func() int {
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/readyz", nil))
		return rec.Code
	}

	if got := get(); got != http.StatusOK {
		t.Fatalf("base démarrée : readyz = %d", got)
	}
	stopTimeout := 10 * time.Second
	if err := ctr.Stop(ctx, &stopTimeout); err != nil {
		t.Fatalf("arrêter PostgreSQL: %v", err)
	}
	if got := get(); got != http.StatusServiceUnavailable {
		t.Fatalf("base arrêtée : readyz = %d, attendu 503", got)
	}
}
