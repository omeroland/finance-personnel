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
	openapi_types "github.com/oapi-codegen/runtime/types"

	"github.com/omeroland/finance-privee/backend/internal/api/gen"
	httpapi "github.com/omeroland/finance-privee/backend/internal/modules/etf/http"
	"github.com/omeroland/finance-privee/backend/internal/platform/auth"
	"github.com/omeroland/finance-privee/backend/internal/platform/httpx"
)

const (
	contractPath = "../../../../../api/openapi.yaml"
	base         = "/api/v1"
)

var (
	specOnce sync.Once
	specDoc  *openapi3.T
	specRt   routers.Router
	specErr  error
)

// loadSpec charge le contrat une seule fois (même retouche en mémoire que la phase 1).
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

// srv complète le sous-ensemble implémenté ici en gen.StrictServerInterface ;
// les autres routes ne sont pas appelées par ces tests.
type srv struct {
	gen.StrictServerInterface
	h *httpapi.Handler
}

func (s srv) ListAccounts(ctx context.Context, r gen.ListAccountsRequestObject) (gen.ListAccountsResponseObject, error) {
	return s.h.ListAccounts(ctx, r)
}

func (s srv) CreateAccount(ctx context.Context, r gen.CreateAccountRequestObject) (gen.CreateAccountResponseObject, error) {
	return s.h.CreateAccount(ctx, r)
}

func (s srv) ListTransactions(ctx context.Context, r gen.ListTransactionsRequestObject) (gen.ListTransactionsResponseObject, error) {
	return s.h.ListTransactions(ctx, r)
}

func (s srv) CreateTransaction(ctx context.Context, r gen.CreateTransactionRequestObject) (gen.CreateTransactionResponseObject, error) {
	return s.h.CreateTransaction(ctx, r)
}

func (s srv) CreateStatement(ctx context.Context, r gen.CreateStatementRequestObject) (gen.CreateStatementResponseObject, error) {
	return s.h.CreateStatement(ctx, r)
}

func (s srv) ListPositions(ctx context.Context, r gen.ListPositionsRequestObject) (gen.ListPositionsResponseObject, error) {
	return s.h.ListPositions(ctx, r)
}

func (s srv) ListEtfs(ctx context.Context, r gen.ListEtfsRequestObject) (gen.ListEtfsResponseObject, error) {
	return s.h.ListEtfs(ctx, r)
}

// api est un client de test dont chaque réponse est validée contre api/openapi.yaml.
type api struct {
	t     testing.TB
	srv   http.Handler
	logs  *bytes.Buffer
	owner uuid.UUID
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

// newAPI assemble la pile HTTP (JWT simulé pour owner) au-dessus de h.
func newAPI(t testing.TB, h *httpapi.Handler, owner uuid.UUID, logs *bytes.Buffer) *api {
	t.Helper()
	log := slog.New(slog.NewJSONHandler(&lockedWriter{w: logs}, nil))
	errH := httpx.ErrorHandler(log)
	bad := func(w http.ResponseWriter, r *http.Request, _ error) {
		errH(w, r, httpx.BadRequest("Corps ou paramètres de la requête illisibles."))
	}
	mux := http.NewServeMux()
	si := gen.NewStrictHandlerWithOptions(srv{h: h}, nil, gen.StrictHTTPServerOptions{
		RequestErrorHandlerFunc: bad, ResponseErrorHandlerFunc: errH,
	})
	gen.HandlerWithOptions(si, gen.StdHTTPServerOptions{BaseRouter: mux, ErrorHandlerFunc: bad})
	inject := func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			next.ServeHTTP(w, r.WithContext(auth.WithOwner(r.Context(), owner.String())))
		})
	}
	return &api{
		t: t, logs: logs, owner: owner,
		srv: httpx.Chain(mux, httpx.RequestID, httpx.Recover(log), inject),
	}
}

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

func (a *api) doJSON(method, path string, v any) resp {
	a.t.Helper()
	var b []byte
	if v != nil {
		var err error
		if b, err = json.Marshal(v); err != nil {
			a.t.Fatal(err)
		}
	}
	return a.doRaw(method, path, b)
}

func (a *api) doRaw(method, path string, body []byte) resp {
	a.t.Helper()
	req := httptest.NewRequest(method, path, bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer test")
	rec := httptest.NewRecorder()
	a.srv.ServeHTTP(rec, req)
	out := resp{Status: rec.Code, Body: rec.Body.Bytes(), Header: rec.Header()}
	a.validate(req, out)
	return out
}

// validate vérifie statut et corps déclarés au contrat ; un statut hors contrat
// doit être un Problem valide.
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
	if route.Operation.Responses.Status(r.Status) == nil {
		a.checkProblem(doc, r)
		return
	}
	in := &openapi3filter.ResponseValidationInput{
		RequestValidationInput: &openapi3filter.RequestValidationInput{Request: req, PathParams: params, Route: route},
		Status:                 r.Status, Header: r.Header, Body: io.NopCloser(bytes.NewReader(r.Body)),
		Options: &openapi3filter.Options{IncludeResponseStatus: true},
	}
	if err := openapi3filter.ValidateResponse(context.Background(), in); err != nil {
		a.t.Fatalf("%s %s -> %d ne respecte pas le contrat: %v\n%s", req.Method, req.URL.Path, r.Status, err, r.Body)
	}
	if r.Status >= 400 {
		a.checkProblem(doc, r)
	}
}

func (a *api) checkProblem(doc *openapi3.T, r resp) {
	a.t.Helper()
	if ct := r.Header.Get("Content-Type"); ct != httpx.ProblemContentType {
		a.t.Fatalf("statut %d avec Content-Type %q", r.Status, ct)
	}
	var v any
	if err := json.Unmarshal(r.Body, &v); err != nil {
		a.t.Fatal(err)
	}
	if err := doc.Components.Schemas["Problem"].Value.VisitJSON(v); err != nil {
		a.t.Fatalf("Problem invalide: %v\n%s", err, r.Body)
	}
	for _, leak := range []string{"goroutine", "SELECT", "INSERT", "pgx", ".go:", "panic", "SQLSTATE", "violates"} {
		if strings.Contains(string(r.Body), leak) {
			a.t.Fatalf("la réponse d'erreur fuit %q: %s", leak, r.Body)
		}
	}
}

func fixedNow() time.Time { return time.Date(2026, 10, 30, 12, 10, 0, 0, time.UTC) }

func dateD(s string) openapi_types.Date {
	t, err := time.Parse(time.DateOnly, s)
	if err != nil {
		panic(err)
	}
	return openapi_types.Date{Time: t}
}
