package httpapi_test

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/shopspring/decimal"

	corestore "github.com/omeroland/finance-privee/backend/internal/core/store"
	"github.com/omeroland/finance-privee/backend/internal/core/store/coredb"
	etfstore "github.com/omeroland/finance-privee/backend/internal/modules/etf/store"
)

// fakeLedger est un dépôt en mémoire qui respecte l'isolement par propriétaire :
// la logique SQL (positions, inverses, audit) est éprouvée par les tests d'intégration.
type fakeLedger struct {
	mu       sync.Mutex
	accounts []corestore.Account
	txs      []corestore.Tx
	stmts    map[string]bool
	stmtRows []corestore.Statement
	asOf     time.Time
	calls    []uuid.UUID // propriétaires reçus
}

func newFakeLedger() *fakeLedger { return &fakeLedger{stmts: map[string]bool{}} }

func (f *fakeLedger) account(owner, id uuid.UUID) (corestore.Account, error) {
	for _, a := range f.accounts {
		if a.OwnerID == owner && a.ID == id {
			return a, nil
		}
	}
	return corestore.Account{}, &corestore.NotFoundError{Message: "Compte introuvable."}
}

func (f *fakeLedger) CreateAccount(_ context.Context, owner uuid.UUID, in corestore.AccountInput) (corestore.Account, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls = append(f.calls, owner)
	for _, a := range f.accounts {
		if a.OwnerID == owner && a.Name == in.Name {
			return corestore.Account{}, corestore.ErrConflict
		}
	}
	a := corestore.Account{ID: uuid.New(), OwnerID: owner, Name: in.Name, Type: in.Type, Broker: in.Broker, Currency: in.Currency, CreatedAt: fixedNow()}
	f.accounts = append(f.accounts, a)
	return a, nil
}

func (f *fakeLedger) ListAccounts(_ context.Context, owner uuid.UUID) ([]corestore.Account, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls = append(f.calls, owner)
	var out []corestore.Account
	for _, a := range f.accounts {
		if a.OwnerID == owner {
			out = append(out, a)
		}
	}
	return out, nil
}

func (f *fakeLedger) AddTransaction(_ context.Context, owner uuid.UUID, in corestore.TxInput) (corestore.Tx, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls = append(f.calls, owner)
	acc, err := f.account(owner, in.AccountID)
	if err != nil {
		return corestore.Tx{}, err
	}
	if in.Currency != acc.Currency {
		return corestore.Tx{}, &corestore.InvalidError{Message: "La devise de la transaction doit être celle du compte."}
	}
	nd := func(d *decimal.Decimal) decimal.NullDecimal {
		if d == nil {
			return decimal.NullDecimal{}
		}
		return decimal.NullDecimal{Decimal: *d, Valid: true}
	}
	t := corestore.Tx{CoreTransaction: coredb.CoreTransaction{
		ID: uuid.New(), OwnerID: owner, AccountID: in.AccountID, Type: in.Type, TradeDate: in.TradeDate,
		Quantity: nd(in.Quantity), Price: nd(in.Price), Amount: nd(in.Amount), Fee: nd(in.Fee), SplitRatio: nd(in.SplitRatio),
		CounterpartAccountID: in.CounterpartAccountID, Currency: in.Currency, ReversesID: in.ReversesID, Note: in.Note,
		Source: "manual", CreatedAt: fixedNow(),
	}, Symbol: in.Symbol}
	f.txs = append(f.txs, t)
	return t, nil
}

func (f *fakeLedger) ListTransactions(_ context.Context, owner, account uuid.UUID, _, _ *time.Time) ([]corestore.Tx, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls = append(f.calls, owner)
	if _, err := f.account(owner, account); err != nil {
		return nil, err
	}
	var out []corestore.Tx
	for _, t := range f.txs {
		if t.OwnerID == owner && t.AccountID == account {
			out = append(out, t)
		}
	}
	return out, nil
}

func (f *fakeLedger) CreateStatement(_ context.Context, owner uuid.UUID, in corestore.StatementInput) (corestore.Statement, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls = append(f.calls, owner)
	if _, err := f.account(owner, in.AccountID); err != nil {
		return corestore.Statement{}, err
	}
	key := in.AccountID.String() + in.Month
	if f.stmts[key] {
		return corestore.Statement{}, corestore.ErrConflict
	}
	f.stmts[key] = true
	s := corestore.Statement{ID: uuid.New(), OwnerID: owner, AccountID: in.AccountID, Month: in.Month, EndValue: in.EndValue, CreatedAt: fixedNow()}
	if in.Contribution != nil {
		s.Contribution = decimal.NullDecimal{Decimal: *in.Contribution, Valid: true}
	}
	f.stmtRows = append(f.stmtRows, s)
	return s, nil
}

func (f *fakeLedger) Holdings(_ context.Context, owner, account uuid.UUID, asOf time.Time) (corestore.Holdings, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls = append(f.calls, owner)
	acc, err := f.account(owner, account)
	if err != nil {
		return corestore.Holdings{}, err
	}
	f.asOf = asOf
	avg := decimal.RequireFromString("152.4")
	return corestore.Holdings{Account: acc, AsOf: asOf, Cash: decimal.RequireFromString("100.5"), Positions: []corestore.Position{
		{Symbol: "TSX:XEQT", Name: "XEQT", Quantity: decimal.NewFromInt(10), AverageCost: &avg},
		{Symbol: "TSX:ZAG", Name: "ZAG", Quantity: decimal.NewFromInt(5)},
	}}, nil
}

type fakeCatalog struct{ rows []etfstore.Catalog }

func (c fakeCatalog) ListCatalog(_ context.Context, _ uuid.UUID, f etfstore.CatalogFilter) ([]etfstore.Catalog, error) {
	var out []etfstore.Catalog
	for _, r := range c.rows {
		if f.Type != "" && r.Type != f.Type {
			continue
		}
		if f.MaxMER != nil && (r.Mer == nil || *r.Mer > *f.MaxMER) {
			continue
		}
		out = append(out, r)
	}
	return out, nil
}

var errBoom = fmt.Errorf("pgx: SELECT secret FROM core.account failed at db.go:42")
