package ingest

import (
	"context"
	"fmt"

	"github.com/google/uuid"
	"github.com/shopspring/decimal"

	"github.com/omeroland/finance-privee/backend/internal/modules/stockeval/store"
)

// fakeRepos reproduit en mémoire la sémantique de D100/D101 pour les tests unitaires.
type fakeRepos struct {
	insts     map[string]store.Instrument
	facts     map[string]store.Fact
	conflicts int
	snaps     map[uuid.UUID]store.Snapshot
	quals     map[uuid.UUID]store.Qualitative
	writes    int // écritures effectives
	failFact  bool
}

func newFake() *fakeRepos {
	return &fakeRepos{
		insts: map[string]store.Instrument{}, facts: map[string]store.Fact{},
		snaps: map[uuid.UUID]store.Snapshot{}, quals: map[uuid.UUID]store.Qualitative{},
	}
}

func (f *fakeRepos) ListWatchlist(context.Context, uuid.UUID) ([]store.Instrument, error) {
	return nil, nil
}
func (f *fakeRepos) RemoveFromWatchlist(context.Context, uuid.UUID, uuid.UUID) error { return nil }
func (f *fakeRepos) GetInstrument(context.Context, uuid.UUID, uuid.UUID) (store.Instrument, error) {
	return store.Instrument{}, store.ErrNotFound
}

func (f *fakeRepos) AddToWatchlist(_ context.Context, _ uuid.UUID, in store.InstrumentInput) (store.Instrument, error) {
	if i, ok := f.insts[in.Symbol]; ok {
		return i, nil
	}
	i := store.Instrument{ID: uuid.New(), Symbol: in.Symbol, Name: in.Name, QuoteCurrency: in.QuoteCurrency}
	f.insts[in.Symbol] = i
	f.writes++
	return i, nil
}

func (f *fakeRepos) ListFacts(context.Context, uuid.UUID, uuid.UUID) ([]store.Fact, error) {
	return nil, nil
}

func (f *fakeRepos) ListOpenConflicts(context.Context, uuid.UUID, uuid.UUID) ([]store.FactConflict, error) {
	return nil, nil
}

func (f *fakeRepos) WriteFact(_ context.Context, _ uuid.UUID, in store.Fact) (store.FactOutcome, error) {
	if f.failFact {
		return "", fmt.Errorf("panne simulée")
	}
	k := fmt.Sprintf("%s/%d/%s", in.InstrumentID, in.FiscalYear, in.Metric)
	cur, ok := f.facts[k]
	switch {
	case !ok:
		f.facts[k] = in
		f.writes++
		return store.FactCreated, nil
	case cur.Value.Equal(in.Value) && cur.Source == in.Source:
		return store.FactNoop, nil
	case cur.Source == store.SourceManual && in.Source != store.SourceManual:
		if !cur.Value.Equal(in.Value) {
			f.conflicts++
			return store.FactConflicted, nil
		}
		return store.FactNoop, nil
	}
	f.facts[k] = in
	f.writes++
	return store.FactUpdated, nil
}

func (f *fakeRepos) GetSnapshot(_ context.Context, _ uuid.UUID, id uuid.UUID) (store.Snapshot, error) {
	s, ok := f.snaps[id]
	if !ok {
		return store.Snapshot{}, store.ErrNotFound
	}
	return s, nil
}

func (f *fakeRepos) SaveSnapshot(_ context.Context, _ uuid.UUID, s store.Snapshot) (bool, error) {
	if c, ok := f.snaps[s.InstrumentID]; ok && c.Source == store.SourceManual && s.Source != store.SourceManual {
		return false, nil
	}
	f.snaps[s.InstrumentID] = s
	f.writes++
	return true, nil
}

func (f *fakeRepos) GetQualitative(_ context.Context, _ uuid.UUID, id uuid.UUID) (store.Qualitative, error) {
	q, ok := f.quals[id]
	if !ok {
		return store.Qualitative{}, store.ErrNotFound
	}
	return q, nil
}

func (f *fakeRepos) SaveQualitative(_ context.Context, _ uuid.UUID, q store.Qualitative) error {
	f.quals[q.InstrumentID] = q
	f.writes++
	return nil
}

func (f *fakeRepos) fact(sym string, year int, metric string) (store.Fact, bool) {
	i := f.insts[sym]
	v, ok := f.facts[fmt.Sprintf("%s/%d/%s", i.ID, year, metric)]
	return v, ok
}

func d(s string) decimal.Decimal { return decimal.RequireFromString(s) }
