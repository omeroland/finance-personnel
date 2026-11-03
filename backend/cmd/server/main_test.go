package main

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/google/uuid"

	"github.com/omeroland/finance-privee/backend/internal/modules/stockeval/engine"
	httpapi "github.com/omeroland/finance-privee/backend/internal/modules/stockeval/http"
	"github.com/omeroland/finance-privee/backend/internal/modules/stockeval/ingest"
	"github.com/omeroland/finance-privee/backend/internal/modules/stockeval/store"
	"github.com/omeroland/finance-privee/backend/internal/platform/auth"
)

type fakePinger struct{ err error }

func (p fakePinger) Ping(context.Context) error { return p.err }

func newTestHandler(t *testing.T, db pinger) http.Handler {
	t.Helper()
	logger := slog.New(slog.NewJSONHandler(io.Discard, nil))
	// L'issuer est injoignable : la découverte échoue, donc tout jeton est refusé (fermé par défaut, D60).
	v, err := auth.NewVerifier(auth.Config{Issuer: "http://127.0.0.1:1/realm", Audience: "finance-api"}, nil, logger)
	if err != nil {
		t.Fatal(err)
	}
	rules, err := engine.DefaultRules()
	if err != nil {
		t.Fatal(err)
	}
	var st *store.Store // jamais appelé : le JWT est refusé avant les handlers
	api, err := httpapi.New(httpapi.Config{Repos: st, Importer: ingest.NewImporter(st), Rules: rules, Logger: logger})
	if err != nil {
		t.Fatal(err)
	}
	return handler(logger, v, api, db)
}

func TestHandlerPublicRoutesAndJWT(t *testing.T) {
	t.Parallel()
	h := newTestHandler(t, fakePinger{})
	do := func(method, path, auth string) *httptest.ResponseRecorder {
		req := httptest.NewRequest(method, path, nil)
		if auth != "" {
			req.Header.Set("Authorization", auth)
		}
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, req)
		return rec
	}
	for _, p := range []string{"/healthz", "/readyz"} {
		if r := do("GET", p, ""); r.Code != 200 || r.Header().Get("X-Request-ID") == "" {
			t.Fatalf("%s = %d", p, r.Code)
		}
	}
	// Toute route de l'API exige un JWT valide.
	routes := [][2]string{
		{"GET", "/api/v1/stocks"},
		{"POST", "/api/v1/stocks"},
		{"GET", "/api/v1/stocks/TSX:CNR"},
		{"PATCH", "/api/v1/stocks/TSX:CNR"},
		{"DELETE", "/api/v1/stocks/TSX:CNR"},
		{"PUT", "/api/v1/stocks/TSX:CNR/facts"},
		{"PUT", "/api/v1/stocks/TSX:CNR/qualitative"},
		{"GET", "/api/v1/stocks/TSX:CNR/evaluation"},
		{"GET", "/api/v1/stocks/TSX:CNR/evaluations"},
		{"POST", "/api/v1/stocks/TSX:CNR/evaluations"},
		{"POST", "/api/v1/stocks/TSX:CNR/refresh"},
		{"POST", "/api/v1/imports/xlsx"},
		{"GET", "/api/v1/imports/" + uuid.NewString()},
		{"GET", "/api/v1/rules"},
	}
	for _, rt := range routes {
		for _, authz := range []string{"", "Bearer faux.jeton.ici"} {
			r := do(rt[0], rt[1], authz)
			if r.Code != 401 || r.Header().Get("Content-Type") != "application/problem+json" {
				t.Fatalf("%s %s (%q) = %d", rt[0], rt[1], authz, r.Code)
			}
		}
	}
}

func TestReadyzReportsDatabase(t *testing.T) {
	t.Parallel()
	h := newTestHandler(t, fakePinger{err: errors.New("dial tcp 10.0.0.1:5432: refused")})
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest("GET", "/readyz", nil))
	if rec.Code != 503 || rec.Header().Get("Content-Type") != "application/problem+json" {
		t.Fatalf("readyz = %d", rec.Code)
	}
	if body := rec.Body.String(); strings.Contains(body, "10.0.0.1") {
		t.Fatalf("fuite de l'adresse de la base: %s", body)
	}
}

func TestRunRefusesWithoutConfig(t *testing.T) {
	t.Parallel()
	logger := slog.New(slog.NewJSONHandler(io.Discard, nil))
	if err := run(logger, config{databaseURL: "postgres://x", auth: auth.Config{Audience: "a"}}); err == nil {
		t.Fatal("OIDC_ISSUER manquant : erreur attendue")
	}
	err := run(logger, config{auth: auth.Config{Issuer: "http://x", Audience: "a"}})
	if err == nil {
		t.Fatal("DATABASE_URL manquant : erreur attendue")
	}
}
