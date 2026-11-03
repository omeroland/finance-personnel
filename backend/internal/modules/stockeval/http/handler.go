// Package httpapi contient les handlers de l'API du module d'évaluation
// d'actions : ils traduisent les objets du contrat (DTO) en appels à engine/
// et store/, et inversement. Aucun calcul financier ici.
package httpapi

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"sync"
	"time"

	"github.com/google/uuid"

	"github.com/omeroland/finance-privee/backend/internal/api/gen"
	"github.com/omeroland/finance-privee/backend/internal/modules/stockeval/engine"
	"github.com/omeroland/finance-privee/backend/internal/modules/stockeval/ingest"
	"github.com/omeroland/finance-privee/backend/internal/modules/stockeval/store"
	"github.com/omeroland/finance-privee/backend/internal/platform/auth"
	"github.com/omeroland/finance-privee/backend/internal/platform/httpx"
)

// Limites de corps : 1 Mo partout, sauf l'import (10 Mo de fichier plus l'enveloppe multipart).
const (
	DefaultBodyLimit = 1 << 20
	ImportPath       = "/api/v1/imports/xlsx"
	ImportBodyLimit  = ingest.MaxFileSize + 64<<10
)

// BodyLimit renvoie l'intergiciel de limite de corps de l'API.
func BodyLimit() httpx.Middleware {
	return httpx.BodyLimit(DefaultBodyLimit, map[string]int64{ImportPath: ImportBodyLimit})
}

// Repos regroupe les dépôts utilisés par les handlers (satisfait par *store.Store).
type Repos interface {
	store.WatchlistRepository
	store.FactRepository
	store.SnapshotRepository
	store.QualitativeRepository
	store.EvaluationRepository
	store.ImportJobRepository
	UpdateInstrument(ctx context.Context, owner, instrumentID uuid.UUID, u store.InstrumentUpdate) (store.Instrument, error)
}

// Importer lit un classeur xlsx (satisfait par *ingest.Importer).
type Importer interface {
	Import(ctx context.Context, owner uuid.UUID, r io.Reader, opts ingest.Options) (ingest.Report, error)
}

// Config regroupe les dépendances du Handler, injectées par le constructeur.
type Config struct {
	Repos    Repos
	Importer Importer
	Rules    *engine.Rules
	Logger   *slog.Logger
	// Now fournit l'horloge (défaut : time.Now).
	Now func() time.Time
}

// Handler implémente gen.StrictServerInterface.
type Handler struct {
	repos    Repos
	importer Importer
	rules    *engine.Rules
	log      *slog.Logger
	now      func() time.Time

	jobs sync.WaitGroup
}

var _ gen.StrictServerInterface = (*Handler)(nil)

// New construit le Handler.
func New(cfg Config) (*Handler, error) {
	if cfg.Repos == nil || cfg.Importer == nil || cfg.Rules == nil {
		return nil, errors.New("handlers: dépôts, importeur et règles sont obligatoires")
	}
	h := &Handler{repos: cfg.Repos, importer: cfg.Importer, rules: cfg.Rules, log: cfg.Logger, now: cfg.Now}
	if h.log == nil {
		h.log = slog.Default()
	}
	if h.now == nil {
		h.now = time.Now
	}
	return h, nil
}

// Routes renvoie le routeur de l'API (chemins du contrat). Le JWT est exigé
// en amont (platform/auth) ; chaque handler lit le propriétaire du contexte.
func (h *Handler) Routes() http.Handler {
	mux := http.NewServeMux()
	opts := gen.StrictHTTPServerOptions{
		RequestErrorHandlerFunc:  h.requestError,
		ResponseErrorHandlerFunc: httpx.ErrorHandler(h.log),
	}
	si := gen.NewStrictHandlerWithOptions(h, []gen.StrictMiddlewareFunc{h.scope}, opts)
	gen.HandlerWithOptions(si, gen.StdHTTPServerOptions{BaseRouter: mux, ErrorHandlerFunc: h.requestError})
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		httpx.ErrorHandler(h.log)(w, r, httpx.NotFound("Route inconnue."))
	})
	return mux
}

// Close attend la fin des imports en arrière-plan, ou l'échéance de ctx.
func (h *Handler) Close(ctx context.Context) error {
	done := make(chan struct{})
	go func() { h.jobs.Wait(); close(done) }()
	select {
	case <-done:
		return nil
	case <-ctx.Done():
		return fmt.Errorf("attendre les imports en cours: %w", ctx.Err())
	}
}

// requestError traduit un échec de lecture de la requête (corps, paramètres) en 400,
// ou en 413 si le corps dépasse la limite.
func (h *Handler) requestError(w http.ResponseWriter, r *http.Request, err error) {
	var mbe *http.MaxBytesError
	if errors.As(err, &mbe) {
		httpx.ErrorHandler(h.log)(w, r, err)
		return
	}
	httpx.ErrorHandler(h.log)(w, r, httpx.BadRequest("Corps ou paramètres de la requête illisibles."))
}

type ownerKey struct{}

// scope pose l'acteur d'audit (sub du JWT) et le propriétaire dans le contexte,
// et traduit les erreurs des dépôts en erreurs d'API.
func (h *Handler) scope(next gen.StrictHandlerFunc, _ string) gen.StrictHandlerFunc {
	return func(ctx context.Context, w http.ResponseWriter, r *http.Request, req any) (any, error) {
		sub, ok := auth.OwnerID(ctx)
		if !ok {
			return nil, httpx.Unauthorized("Jeton d'accès absent ou invalide.")
		}
		id, err := uuid.Parse(sub)
		if err != nil {
			h.log.WarnContext(ctx, "sub du jeton qui n'est pas un UUID")
			return nil, httpx.Unauthorized("Jeton d'accès absent ou invalide.")
		}
		ctx = store.ContextWithActor(ctx, sub)
		ctx = context.WithValue(ctx, ownerKey{}, id)
		res, err := next(ctx, w, r, req)
		return res, translate(err)
	}
}

func owner(ctx context.Context) uuid.UUID {
	id, _ := ctx.Value(ownerKey{}).(uuid.UUID)
	return id
}

// translate convertit les erreurs des dépôts en erreurs d'API sans détail interne.
func translate(err error) error {
	var ae *httpx.Error
	switch {
	case err == nil, errors.As(err, &ae):
		return err
	case errors.Is(err, store.ErrNotFound):
		return httpx.NotFound("Ressource introuvable.")
	case errors.Is(err, store.ErrInvalid):
		return httpx.BadRequest("Une valeur est refusée par la validation.")
	}
	return err
}
