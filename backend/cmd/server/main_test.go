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
	"github.com/omeroland/finance-privee/backend/internal/platform/observability"
)

type fakePinger struct{ err error }

func (p fakePinger) Ping(context.Context) error { return p.err }

// fakeOIDC sert une découverte OIDC et un JWKS vide : l'API est « prête », mais aucun jeton ne passe.
func fakeOIDC(t *testing.T) string {
	t.Helper()
	var srv *httptest.Server
	mux := http.NewServeMux()
	mux.HandleFunc("/.well-known/openid-configuration", func(w http.ResponseWriter, _ *http.Request) {
		_, _ = io.WriteString(w, `{"issuer":"`+srv.URL+`","jwks_uri":"`+srv.URL+`/jwks","id_token_signing_alg_values_supported":["RS256"]}`)
	})
	mux.HandleFunc("/jwks", func(w http.ResponseWriter, _ *http.Request) { _, _ = io.WriteString(w, `{"keys":[]}`) })
	srv = httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	return srv.URL
}

func newTestHandler(t *testing.T, db pinger) http.Handler {
	t.Helper()
	return newTestHandlerIssuer(t, db, fakeOIDC(t))
}

func newTestHandlerIssuer(t *testing.T, db pinger, issuer string) http.Handler {
	t.Helper()
	logger := slog.New(slog.NewJSONHandler(io.Discard, nil))
	v, err := auth.NewVerifier(auth.Config{Issuer: issuer, Audience: "finance-api"}, nil, logger)
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
	return handler(logger, v, api, observability.NewMetrics(), db)
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

// Sans découverte OIDC jamais réussie (JWKS absent du cache), l'API n'est pas prête (D60).
func TestReadyzRequiresJWKS(t *testing.T) {
	t.Parallel()
	h := newTestHandlerIssuer(t, fakePinger{}, "http://127.0.0.1:1/realm")
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest("GET", "/readyz", nil))
	if rec.Code != 503 {
		t.Fatalf("readyz sans JWKS = %d", rec.Code)
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
