package httpx_test

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/omeroland/finance-privee/backend/internal/platform/httpx"
)

func discard() *slog.Logger { return slog.New(slog.NewJSONHandler(io.Discard, nil)) }

func decode(t *testing.T, rec *httptest.ResponseRecorder) httpx.Problem {
	t.Helper()
	if ct := rec.Header().Get("Content-Type"); ct != httpx.ProblemContentType {
		t.Fatalf("Content-Type = %q", ct)
	}
	var p httpx.Problem
	if err := json.Unmarshal(rec.Body.Bytes(), &p); err != nil {
		t.Fatal(err)
	}
	return p
}

func TestProblemOf(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name   string
		err    error
		status int
		logged bool
	}{
		{"validation", httpx.BadRequest("x", httpx.FieldError{Field: "a", Message: "b"}), 400, false},
		{"401", httpx.Unauthorized("x"), 401, false},
		{"404", httpx.NotFound("x"), 404, false},
		{"409", httpx.Conflict("x"), 409, false},
		{"501", httpx.NotImplemented("x"), 501, true},
		{"413 explicite", httpx.PayloadTooLarge("x"), 413, false},
		{"413 MaxBytes", &http.MaxBytesError{Limit: 1}, 413, false},
		{"délai", context.DeadlineExceeded, 503, true},
		{"inconnue", errors.New("pq: relation \"secret\" does not exist"), 500, true},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			p, cause := httpx.ProblemOf(tc.err, "/x")
			if p.Status != tc.status || (cause != nil) != tc.logged {
				t.Fatalf("status=%d cause=%v", p.Status, cause)
			}
			if strings.Contains(p.Detail+p.Title, "secret") || strings.Contains(p.Detail+p.Title, "pq:") {
				t.Fatalf("fuite interne: %+v", p)
			}
		})
	}
}

func TestErrorHandlerEtRecover(t *testing.T) {
	t.Parallel()
	var logs bytes.Buffer
	logger := slog.New(slog.NewJSONHandler(&logs, nil))

	rec := httptest.NewRecorder()
	httpx.ErrorHandler(logger)(rec, httptest.NewRequest("GET", "/a", nil), errors.New("SELECT * FROM t: boom"))
	p := decode(t, rec)
	if rec.Code != 500 || strings.Contains(rec.Body.String(), "SELECT") || p.Instance != "/a" {
		t.Fatalf("réponse = %d %s", rec.Code, rec.Body.String())
	}
	if !strings.Contains(logs.String(), "boom") {
		t.Fatal("la cause doit être journalisée")
	}

	h := httpx.Chain(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { panic("kaboom") }),
		httpx.RequestID, httpx.Recover(logger))
	rec = httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest("GET", "/p", nil))
	decode(t, rec)
	if rec.Code != 500 || strings.Contains(rec.Body.String(), "kaboom") || rec.Header().Get(httpx.RequestIDHeader) == "" {
		t.Fatalf("panique: %d %s", rec.Code, rec.Body.String())
	}
}

func TestRequestID(t *testing.T) {
	t.Parallel()
	var seen string
	h := httpx.RequestID(http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) { seen = httpx.RequestIDFrom(r.Context()) }))

	req := httptest.NewRequest("GET", "/", nil)
	req.Header.Set(httpx.RequestIDHeader, "abc-123")
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if seen != "abc-123" || rec.Header().Get(httpx.RequestIDHeader) != "abc-123" {
		t.Fatalf("identifiant repris = %q", seen)
	}

	req = httptest.NewRequest("GET", "/", nil)
	req.Header.Set(httpx.RequestIDHeader, "mauvais id\n")
	h.ServeHTTP(httptest.NewRecorder(), req)
	if seen == "" || strings.ContainsAny(seen, " \n") {
		t.Fatalf("identifiant généré = %q", seen)
	}
}

func TestAccessLogSansJeton(t *testing.T) {
	t.Parallel()
	var logs bytes.Buffer
	h := httpx.AccessLog(slog.New(slog.NewJSONHandler(&logs, nil)))(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusTeapot)
		_, _ = w.Write([]byte("ok"))
	}))
	req := httptest.NewRequest("GET", "/api/v1/stocks?secret=1", nil)
	req.Header.Set("Authorization", "Bearer TOKEN-SECRET")
	h.ServeHTTP(httptest.NewRecorder(), req)
	out := logs.String()
	if strings.Contains(out, "TOKEN-SECRET") || strings.Contains(out, "secret=1") {
		t.Fatalf("journal indiscret: %s", out)
	}
	if !strings.Contains(out, `"status":418`) || !strings.Contains(out, `"path":"/api/v1/stocks"`) {
		t.Fatalf("journal incomplet: %s", out)
	}
}

func TestBodyLimit(t *testing.T) {
	t.Parallel()
	read := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if _, err := io.ReadAll(r.Body); err != nil {
			httpx.ErrorHandler(discard())(w, r, err)
			return
		}
		w.WriteHeader(http.StatusNoContent)
	})
	h := httpx.BodyLimit(10, map[string]int64{"/big": 100})(read)

	do := func(path string, n int, chunked bool) int {
		req := httptest.NewRequest("POST", path, bytes.NewReader(make([]byte, n)))
		if chunked {
			req.ContentLength = -1
		}
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, req)
		if rec.Code == 413 {
			decode(t, rec)
		}
		return rec.Code
	}
	if c := do("/x", 5, false); c != 204 {
		t.Fatalf("sous la limite = %d", c)
	}
	if c := do("/x", 11, false); c != 413 {
		t.Fatalf("Content-Length trop grand = %d", c)
	}
	if c := do("/x", 11, true); c != 413 {
		t.Fatalf("lecture au-delà = %d", c)
	}
	if c := do("/big", 50, false); c != 204 {
		t.Fatalf("limite propre au chemin = %d", c)
	}
}

func TestTimeout(t *testing.T) {
	t.Parallel()
	h := httpx.Timeout(time.Hour)(http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
		if _, ok := r.Context().Deadline(); !ok {
			t.Error("échéance absente")
		}
	}))
	h.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest("GET", "/", nil))
}
