package httpapi_test

import (
	"context"
	"sort"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/shopspring/decimal"

	corestore "github.com/omeroland/finance-privee/backend/internal/core/store"
	"github.com/omeroland/finance-privee/backend/internal/modules/etf/ingest"
	etfstore "github.com/omeroland/finance-privee/backend/internal/modules/etf/store"
)

// Entries : écritures du compte, de la plus ancienne à la plus récente (les corrections ne sont pas simulées ici).
func (f *fakeLedger) Entries(_ context.Context, owner, account uuid.UUID, asOf time.Time) ([]corestore.Tx, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls = append(f.calls, owner)
	if _, err := f.account(owner, account); err != nil {
		return nil, err
	}
	var out []corestore.Tx
	for _, t := range f.txs {
		if t.OwnerID == owner && t.AccountID == account && !t.TradeDate.After(asOf) && t.ReversesID == nil {
			out = append(out, t)
		}
	}
	sort.SliceStable(out, func(i, j int) bool { return out[i].TradeDate.Before(out[j].TradeDate) })
	return out, nil
}

func (f *fakeLedger) ListStatements(_ context.Context, owner, account uuid.UUID) ([]corestore.Statement, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls = append(f.calls, owner)
	if _, err := f.account(owner, account); err != nil {
		return nil, err
	}
	var out []corestore.Statement
	for _, s := range f.stmtRows {
		if s.OwnerID == owner && s.AccountID == account {
			out = append(out, s)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Month < out[j].Month })
	return out, nil
}

// fakePortfolio : modèles et prix en mémoire, filtrés par propriétaire.
type fakePortfolio struct {
	mu     sync.Mutex
	models map[uuid.UUID][]etfstore.Model
	prices map[uuid.UUID]map[string]decimal.Decimal
}

func newFakePortfolio() *fakePortfolio {
	return &fakePortfolio{models: map[uuid.UUID][]etfstore.Model{}, prices: map[uuid.UUID]map[string]decimal.Decimal{}}
}

func (p *fakePortfolio) setPrice(owner uuid.UUID, symbol, price string) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.prices[owner] == nil {
		p.prices[owner] = map[string]decimal.Decimal{}
	}
	p.prices[owner][symbol] = decimal.RequireFromString(price)
}

func (p *fakePortfolio) ListModels(_ context.Context, owner uuid.UUID) ([]etfstore.Model, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	return append([]etfstore.Model(nil), p.models[owner]...), nil
}

func (p *fakePortfolio) GetModel(_ context.Context, owner, id uuid.UUID) (etfstore.Model, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	for _, m := range p.models[owner] {
		if m.ID == id {
			return m, nil
		}
	}
	return etfstore.Model{}, &corestore.NotFoundError{Message: "Modèle introuvable."}
}

func build(m etfstore.Model, in ingest.Model, version int) etfstore.Model {
	m.Name, m.Version, m.Targets, m.CashTarget = in.Name, version, in.Targets, in.CashTarget
	m.BandRelative, m.BandCash, m.Holdings = in.BandRelative, in.BandCash, in.Holdings
	m.CreatedAt = fixedNow()
	return m
}

func (p *fakePortfolio) CreateModel(_ context.Context, owner uuid.UUID, in ingest.Model) (etfstore.Model, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	for _, m := range p.models[owner] {
		if m.Name == in.Name {
			return etfstore.Model{}, corestore.ErrConflict
		}
	}
	m := build(etfstore.Model{ID: uuid.New()}, in, 1)
	p.models[owner] = append(p.models[owner], m)
	return m, nil
}

func (p *fakePortfolio) PublishModelVersion(_ context.Context, owner, id uuid.UUID, in ingest.Model) (etfstore.Model, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	for i, m := range p.models[owner] {
		if m.ID == id {
			if m.Name != in.Name {
				return etfstore.Model{}, &corestore.InvalidError{Message: "Le nom d'un modèle ne change pas d'une version à l'autre."}
			}
			p.models[owner][i] = build(m, in, m.Version+1)
			return p.models[owner][i], nil
		}
	}
	return etfstore.Model{}, &corestore.NotFoundError{Message: "Modèle introuvable."}
}

func (p *fakePortfolio) LastPrices(_ context.Context, owner uuid.UUID, symbols []string, _ string, _ time.Time) (map[string]decimal.Decimal, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	out := map[string]decimal.Decimal{}
	for _, s := range symbols {
		if v, ok := p.prices[owner][s]; ok {
			out[s] = v
		}
	}
	return out, nil
}
