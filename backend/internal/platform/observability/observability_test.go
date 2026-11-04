package observability_test

import (
	"bytes"
	"context"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/omeroland/finance-privee/backend/internal/platform/httpx"
	"github.com/omeroland/finance-privee/backend/internal/platform/observability"
)

func scrape(t *testing.T, m *observability.Metrics) string {
	t.Helper()
	srv := observability.NewMetricsServer("", m)
	rec := httptest.NewRecorder()
	srv.Handler.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/metrics", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("/metrics = %d", rec.Code)
	}
	return rec.Body.String()
}

func TestFourMetricsAppear(t *testing.T) {
	t.Parallel()
	m := observability.NewMetrics()
	h := m.Middleware(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusNotFound) }))
	h.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, "/api/v1/stocks/TSX:CNR/evaluation", nil))
	h.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, "/inconnu/abc", nil))
	m.EvaluationDone(3 * time.Millisecond)
	m.ImportFinished(observability.ImportFailed)

	out := scrape(t, m)
	for _, want := range []string{
		`http_requests_total{code="4xx",method="GET",route="/api/v1/stocks"} 1`,
		`http_requests_total{code="4xx",method="GET",route="autre"} 1`,
		`http_request_duration_seconds_count{method="GET",route="/api/v1/stocks"} 1`,
		`stockeval_evaluation_duration_seconds_count 1`,
		`import_jobs_total{status="failed"} 1`,
		`import_jobs_total{status="succeeded"} 0`,
	} {
		if !strings.Contains(out, want) {
			t.Errorf("métrique absente : %s", want)
		}
	}
	if strings.Contains(out, "TSX:CNR") {
		t.Error("le symbole ne doit pas devenir une étiquette")
	}
}

func TestMetricsServerOnlyServesMetrics(t *testing.T) {
	t.Parallel()
	srv := observability.NewMetricsServer(":0", observability.NewMetrics())
	rec := httptest.NewRecorder()
	srv.Handler.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/v1/stocks", nil))
	if rec.Code != http.StatusNotFound {
		t.Fatalf("route de l'API sur le port des métriques = %d", rec.Code)
	}
}

func TestMiddlewareDefaultStatus(t *testing.T) {
	t.Parallel()
	m := observability.NewMetrics()
	h := m.Middleware(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { _, _ = io.WriteString(w, "ok") }))
	h.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, "/healthz", nil))
	if out := scrape(t, m); !strings.Contains(out, `http_requests_total{code="2xx",method="GET",route="/healthz"} 1`) {
		t.Fatalf("200 implicite non compté :\n%s", out)
	}
}

func TestReadyz(t *testing.T) {
	t.Parallel()
	logger := slog.New(slog.NewJSONHandler(io.Discard, nil))
	ok := func(context.Context) error { return nil }
	ko := func(context.Context) error { return errors.New("dial tcp 10.0.0.1:5432: refused") }
	cases := []struct {
		name   string
		checks []observability.Check
		want   int
	}{
		{"tout va bien", []observability.Check{ok, ok}, 200},
		{"base arrêtée", []observability.Check{ko, ok}, 503},
		{"JWKS absent", []observability.Check{ok, ko}, 503},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			rec := httptest.NewRecorder()
			observability.Readyz(logger, tc.checks...).ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/readyz", nil))
			if rec.Code != tc.want {
				t.Fatalf("code = %d, attendu %d", rec.Code, tc.want)
			}
			if tc.want == 503 && (rec.Header().Get("Content-Type") != "application/problem+json" || strings.Contains(rec.Body.String(), "10.0.0.1")) {
				t.Fatalf("503 non conforme : %s", rec.Body.String())
			}
		})
	}
	rec := httptest.NewRecorder()
	observability.Healthz(rec, httptest.NewRequest(http.MethodGet, "/healthz", nil))
	if rec.Code != 200 {
		t.Fatalf("healthz = %d", rec.Code)
	}
}

const (
	fakeJWT = "eyJhbGciOiJSUzI1NiJ9.eyJzdWIiOiJhYmMifQ.c2lnbmF0dXJl"
	secret  = "s3cr3t-valeur"
)

// Aucune ligne de journal ne contient l'en-tête Authorization, un jeton, un mot
// de passe ou un montant, même quand l'appelant les passe par erreur.
func TestLogsNeverLeakSecrets(t *testing.T) {
	t.Parallel()
	var buf bytes.Buffer
	logger := observability.NewLogger(&buf, slog.LevelDebug)

	next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Maladresses d'un appelant : en-tête, jeton, mot de passe, montant, erreur qui cite le jeton.
		logger.InfoContext(r.Context(), "maladroit",
			slog.Any("headers", r.Header.Clone()),
			slog.String("Authorization", r.Header.Get("Authorization")),
			slog.String("access_token", fakeJWT),
			slog.String("password", secret),
			slog.String("montant", "12345.67"),
			slog.Group("g", slog.String("portfolio_value", "999")),
			slog.Any("erreur", errors.New("jeton refusé: Bearer "+fakeJWT)),
			slog.String("texte", "reçu "+fakeJWT),
		)
		w.WriteHeader(http.StatusTeapot)
	})
	h := httpx.Chain(next, httpx.RequestID, httpx.AccessLog(logger), httpx.Recover(logger))

	req := httptest.NewRequest(http.MethodGet, "/api/v1/stocks?token="+secret, nil)
	req.Header.Set("Authorization", "Bearer "+fakeJWT)
	req.Header.Set("X-Request-ID", "req-42")
	h.ServeHTTP(httptest.NewRecorder(), req)

	out := buf.String()
	if !strings.Contains(out, `"request_id":"req-42"`) {
		t.Fatalf("identifiant de requête absent :\n%s", out)
	}
	for _, bad := range []string{"Bearer " + fakeJWT, fakeJWT, "eyJ", secret, "12345.67", `"999"`} {
		if strings.Contains(out, bad) {
			t.Errorf("le journal contient %q :\n%s", bad, out)
		}
	}
	// Le nom de l'en-tête lui-même ne doit apparaître comme clé nulle part.
	if strings.Contains(strings.ToLower(out), `"authorization":"bearer`) {
		t.Errorf("en-tête Authorization journalisé :\n%s", out)
	}
	for line := range strings.SplitSeq(strings.TrimSpace(out), "\n") {
		if !strings.HasPrefix(line, "{") {
			t.Errorf("ligne non JSON : %s", line)
		}
	}
}
