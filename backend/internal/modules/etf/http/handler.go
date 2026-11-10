// Package httpapi contient les handlers du module 2 (FNB) et du registre :
// comptes, transactions, relevés, positions et catalogue. Ils traduisent les
// objets du contrat (DTO) en appels aux dépôts, et inversement ; aucun calcul
// financier ici (les positions et l'encaisse viennent du dépôt).
package httpapi

import (
	"context"
	"errors"
	"log/slog"
	"time"

	"github.com/google/uuid"
	"github.com/shopspring/decimal"

	"github.com/omeroland/finance-privee/backend/internal/api/gen"
	corestore "github.com/omeroland/finance-privee/backend/internal/core/store"
	"github.com/omeroland/finance-privee/backend/internal/modules/etf/ingest"
	etfstore "github.com/omeroland/finance-privee/backend/internal/modules/etf/store"
	"github.com/omeroland/finance-privee/backend/internal/platform/auth"
	"github.com/omeroland/finance-privee/backend/internal/platform/httpx"
)

// Ledger regroupe les dépôts du socle utilisés ici (satisfait par *corestore.Store).
type Ledger interface {
	CreateAccount(ctx context.Context, owner uuid.UUID, in corestore.AccountInput) (corestore.Account, error)
	ListAccounts(ctx context.Context, owner uuid.UUID) ([]corestore.Account, error)
	AddTransaction(ctx context.Context, owner uuid.UUID, in corestore.TxInput) (corestore.Tx, error)
	ListTransactions(ctx context.Context, owner, account uuid.UUID, from, to *time.Time) ([]corestore.Tx, error)
	CreateStatement(ctx context.Context, owner uuid.UUID, in corestore.StatementInput) (corestore.Statement, error)
	Holdings(ctx context.Context, owner, account uuid.UUID, asOf time.Time) (corestore.Holdings, error)
	ListStatements(ctx context.Context, owner, account uuid.UUID) ([]corestore.Statement, error)
	Entries(ctx context.Context, owner, account uuid.UUID, asOf time.Time) ([]corestore.Tx, error)
}

// Portfolio regroupe les dépôts de modèles et de prix (satisfait par *etfstore.Store).
type Portfolio interface {
	ListModels(ctx context.Context, owner uuid.UUID) ([]etfstore.Model, error)
	GetModel(ctx context.Context, owner, id uuid.UUID) (etfstore.Model, error)
	CreateModel(ctx context.Context, owner uuid.UUID, in ingest.Model) (etfstore.Model, error)
	PublishModelVersion(ctx context.Context, owner, id uuid.UUID, in ingest.Model) (etfstore.Model, error)
	LastPrices(ctx context.Context, owner uuid.UUID, symbols []string, currency string, asOf time.Time) (map[string]decimal.Decimal, error)
}

// Catalog lit le catalogue de FNB (satisfait par *etfstore.Store).
type Catalog interface {
	ListCatalog(ctx context.Context, owner uuid.UUID, f etfstore.CatalogFilter) ([]etfstore.Catalog, error)
}

// Config regroupe les dépendances, injectées par le constructeur.
type Config struct {
	Ledger  Ledger
	Catalog Catalog
	// Portfolio : modèles et derniers prix (P2-11).
	Portfolio Portfolio
	Logger    *slog.Logger
	// Now fournit l'horloge (défaut : time.Now).
	Now func() time.Time
}

// Handler implémente les routes comptes, registre, relevés, positions et catalogue de
// gen.StrictServerInterface. Il se compose avec le handler du module 1 dans cmd/server.
type Handler struct {
	ledger  Ledger
	catalog Catalog
	pf      Portfolio
	log     *slog.Logger
	now     func() time.Time
}

// New construit le Handler.
func New(cfg Config) (*Handler, error) {
	if cfg.Ledger == nil || cfg.Catalog == nil || cfg.Portfolio == nil {
		return nil, errors.New("handlers: dépôts obligatoires")
	}
	h := &Handler{ledger: cfg.Ledger, catalog: cfg.Catalog, pf: cfg.Portfolio, log: cfg.Logger, now: cfg.Now}
	if h.log == nil {
		h.log = slog.Default()
	}
	if h.now == nil {
		h.now = time.Now
	}
	return h, nil
}

// begin lit le propriétaire du jeton (jamais d'un paramètre de la requête) et
// pose l'acteur d'audit dans le contexte.
func (h *Handler) begin(ctx context.Context) (context.Context, uuid.UUID, error) {
	sub, ok := auth.OwnerID(ctx)
	if !ok {
		return ctx, uuid.Nil, httpx.Unauthorized("Jeton d'accès absent ou invalide.")
	}
	id, err := uuid.Parse(sub)
	if err != nil {
		h.log.WarnContext(ctx, "sub du jeton qui n'est pas un UUID")
		return ctx, uuid.Nil, httpx.Unauthorized("Jeton d'accès absent ou invalide.")
	}
	return corestore.ContextWithActor(ctx, sub), id, nil
}

// translate convertit les erreurs des dépôts en erreurs d'API sans détail interne.
func translate(err error) error {
	var (
		ae  *httpx.Error
		inv *corestore.InvalidError
		nf  *corestore.NotFoundError
	)
	switch {
	case err == nil, errors.As(err, &ae):
		return err
	case errors.As(err, &nf):
		return httpx.NotFound(nf.Message)
	case errors.Is(err, corestore.ErrNotFound):
		return httpx.NotFound("Ressource introuvable.")
	case errors.As(err, &inv):
		return httpx.BadRequest(inv.Message)
	case errors.Is(err, corestore.ErrInvalid):
		return httpx.BadRequest("Une valeur est refusée par la validation.")
	case errors.Is(err, corestore.ErrConflict):
		return httpx.Conflict("La ressource existe déjà ou l'écriture a déjà été inversée.")
	}
	return err
}

var _ = gen.StrictServerInterface(nil)
