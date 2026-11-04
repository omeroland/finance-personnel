package httpapi_test

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/getkin/kin-openapi/openapi3"
	"github.com/getkin/kin-openapi/openapi3filter"
	"github.com/getkin/kin-openapi/routers"
	"github.com/getkin/kin-openapi/routers/legacy"
	"github.com/google/uuid"

	"github.com/omeroland/finance-privee/backend/internal/modules/stockeval/engine"
	httpapi "github.com/omeroland/finance-privee/backend/internal/modules/stockeval/http"
	"github.com/omeroland/finance-privee/backend/internal/platform/auth"
	"github.com/omeroland/finance-privee/backend/internal/platform/httpx"
)

const contractPath = "../../../../../api/openapi.yaml"

var (
	specOnce sync.Once
	specDoc  *openapi3.T
	specRt   routers.Router
	specErr  error
)

// loadSpec charge le contrat une seule fois.
func loadSpec(t testing.TB) (*openapi3.T, routers.Router) {
	t.Helper()
	specOnce.Do(func() {
		path, err := filepath.Abs(contractPath)
		if err != nil {
			specErr = err
			return
		}
		raw, err := os.ReadFile(path)
		if err != nil {
			specErr = err
			return
		}
		// kin-openapi refuse `license.identifier` (OpenAPI 3.1) : on le retire en mémoire, le
		// fichier du contrat reste intact. Spectral contrôle déjà le document (make lint).
		// Idem pour `info.summary`.
		raw = bytes.Replace(raw, []byte("    identifier: LicenseRef-Proprietary\n"), nil, 1)
		raw = regexp.MustCompile(`(?m)^  summary: .*\n`).ReplaceAll(raw, nil)
		if specDoc, specErr = openapi3.NewLoader().LoadFromData(raw); specErr != nil {
			return
		}
		specRt, specErr = legacy.NewRouter(specDoc)
	})
	if specErr != nil {
		t.Fatalf("charger le contrat: %v", specErr)
	}
	return specDoc, specRt
}

// api est un serveur de test dont chaque réponse est validée contre api/openapi.yaml.
type api struct {
	t      testing.TB
	srv    http.Handler
	repos  *fakeRepos
	imp    *fakeImporter
	h      *httpapi.Handler
	logs   *bytes.Buffer
	owner  uuid.UUID
	now    time.Time
	mu     sync.Mutex
	checks int
}

func newAPI(t *testing.T) *api {
	t.Helper()
	repos, imp := newFakeRepos(), &fakeImporter{}
	a := newAPIWith(t, repos, imp, uuid.New())
	a.repos, a.imp = repos, imp
	return a
}

// newAPIWith assemble la pile HTTP au-dessus de dépôts et d'un importeur donnés.
func newAPIWith(t *testing.T, repos httpapi.Repos, imp httpapi.Importer, owner uuid.UUID, obs ...httpapi.Observer) *api {
	t.Helper()
	var observer httpapi.Observer
	if len(obs) > 0 {
		observer = obs[0]
	}
	rules, err := engine.DefaultRules()
	if err != nil {
		t.Fatal(err)
	}
	a := &api{
		t: t, logs: &bytes.Buffer{}, owner: owner,
		now: time.Date(2026, 10, 30, 12, 10, 0, 0, time.UTC),
	}
	h, err := httpapi.New(httpapi.Config{
		Repos: repos, Importer: imp, Rules: rules, Observer: observer, Now: func() time.Time { return a.now },
		Logger: slog.New(slog.NewJSONHandler(&lockedWriter{w: a.logs}, nil)),
	})
	if err != nil {
		t.Fatal(err)
	}
	a.h = h
	inject := func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			next.ServeHTTP(w, r.WithContext(auth.WithOwner(r.Context(), a.owner.String())))
		})
	}
	a.srv = httpx.Chain(h.Routes(), httpx.RequestID, httpx.Recover(slog.Default()), inject, httpapi.BodyLimit())
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = h.Close(ctx)
	})
	return a
}

type lockedWriter struct {
	mu sync.Mutex
	w  io.Writer
}

func (l *lockedWriter) Write(p []byte) (int, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.w.Write(p)
}

// resp est une réponse déjà validée.
type resp struct {
	Status int
	Body   []byte
	Header http.Header
}

func (r resp) json(t testing.TB, v any) {
	t.Helper()
	if err := json.Unmarshal(r.Body, v); err != nil {
		t.Fatalf("JSON illisible: %v\n%s", err, r.Body)
	}
}

// do envoie une requête et valide la réponse contre le contrat.
func (a *api) do(method, path, contentType string, body []byte) resp {
	a.t.Helper()
	return a.doWith(a.srv, method, path, contentType, body)
}

func (a *api) doJSON(method, path string, v any) resp {
	a.t.Helper()
	var b []byte
	if v != nil {
		var err error
		if b, err = json.Marshal(v); err != nil {
			a.t.Fatal(err)
		}
	}
	return a.do(method, path, "application/json", b)
}

func (a *api) doWith(srv http.Handler, method, path, contentType string, body []byte) resp {
	a.t.Helper()
	req := httptest.NewRequest(method, path, bytes.NewReader(body))
	if contentType != "" {
		req.Header.Set("Content-Type", contentType)
	}
	req.Header.Set("Authorization", "Bearer test")
	rec := httptest.NewRecorder()
	srv.ServeHTTP(rec, req)
	out := resp{Status: rec.Code, Body: rec.Body.Bytes(), Header: rec.Header()}
	a.validate(req, out)
	return out
}

// validate vérifie la réponse : statut et corps déclarés au contrat. Les statuts que le
// contrat ne déclare pas (413, 500, 503) doivent au moins être un Problem valide.
func (a *api) validate(req *http.Request, r resp) {
	a.t.Helper()
	doc, rt := loadSpec(a.t)
	route, params, err := rt.FindRoute(req)
	if err != nil {
		if r.Status == http.StatusNotFound {
			a.checkProblem(doc, r)
			return
		}
		a.t.Fatalf("route %s %s absente du contrat: %v", req.Method, req.URL.Path, err)
	}
	in := &openapi3filter.ResponseValidationInput{
		RequestValidationInput: &openapi3filter.RequestValidationInput{Request: req, PathParams: params, Route: route},
		Status:                 r.Status,
		Header:                 r.Header,
		Body:                   io.NopCloser(bytes.NewReader(r.Body)),
		Options:                &openapi3filter.Options{IncludeResponseStatus: true},
	}
	declared := route.Operation.Responses.Status(r.Status) != nil
	if !declared {
		a.checkProblem(doc, r)
		return
	}
	if err := openapi3filter.ValidateResponse(context.Background(), in); err != nil {
		a.t.Fatalf("%s %s -> %d ne respecte pas le contrat: %v\n%s", req.Method, req.URL.Path, r.Status, err, r.Body)
	}
	a.mu.Lock()
	a.checks++
	a.mu.Unlock()
}

func (a *api) checkProblem(doc *openapi3.T, r resp) {
	a.t.Helper()
	if ct := r.Header.Get("Content-Type"); ct != httpx.ProblemContentType {
		a.t.Fatalf("statut %d hors contrat avec Content-Type %q", r.Status, ct)
	}
	var v any
	if err := json.Unmarshal(r.Body, &v); err != nil {
		a.t.Fatal(err)
	}
	if err := doc.Components.Schemas["Problem"].Value.VisitJSON(v); err != nil {
		a.t.Fatalf("Problem invalide: %v\n%s", err, r.Body)
	}
	body := string(r.Body)
	for _, leak := range []string{"goroutine", "SELECT", "pgx", ".go:", "panic"} {
		if strings.Contains(body, leak) {
			a.t.Fatalf("la réponse d'erreur fuit %q: %s", leak, body)
		}
	}
}

func injectOwner(next http.Handler, sub string) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		next.ServeHTTP(w, r.WithContext(auth.WithOwner(r.Context(), sub)))
	})
}

func newHandlerNoDeps() (*httpapi.Handler, error) { return httpapi.New(httpapi.Config{}) }
