package httpapi_test

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"sort"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/shopspring/decimal"

	"github.com/omeroland/finance-privee/backend/internal/modules/stockeval/ingest"
	"github.com/omeroland/finance-privee/backend/internal/modules/stockeval/store"
)

// fakeRepos est un dépôt en mémoire (un seul propriétaire suffit aux tests de handlers ;
// le filtrage par owner_id est contrôlé par les tests d'intégration de store/).
type fakeRepos struct {
	mu        sync.Mutex
	inst      map[uuid.UUID]store.Instrument
	watch     map[uuid.UUID]bool
	facts     map[string]store.Fact
	snaps     map[uuid.UUID]store.Snapshot
	quals     map[uuid.UUID]store.Qualitative
	evals     []store.Evaluation
	jobs      map[uuid.UUID]store.ImportJob
	conflicts []store.FactConflict
	failList  error
	owners    map[uuid.UUID]bool
}

func newFakeRepos() *fakeRepos {
	return &fakeRepos{
		inst: map[uuid.UUID]store.Instrument{}, watch: map[uuid.UUID]bool{}, facts: map[string]store.Fact{},
		snaps: map[uuid.UUID]store.Snapshot{}, quals: map[uuid.UUID]store.Qualitative{},
		jobs: map[uuid.UUID]store.ImportJob{}, owners: map[uuid.UUID]bool{},
	}
}

func nf(what string) error { return fmt.Errorf("%s: %w", what, store.ErrNotFound) }

func (f *fakeRepos) seen(o uuid.UUID) { f.owners[o] = true }

func (f *fakeRepos) ListWatchlist(_ context.Context, o uuid.UUID) ([]store.Instrument, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.seen(o)
	if f.failList != nil {
		return nil, f.failList
	}
	var out []store.Instrument
	for id, i := range f.inst {
		if f.watch[id] {
			out = append(out, i)
		}
	}
	sort.Slice(out, func(a, b int) bool { return out[a].Symbol < out[b].Symbol })
	return out, nil
}

func (f *fakeRepos) AddToWatchlist(_ context.Context, _ uuid.UUID, in store.InstrumentInput) (store.Instrument, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	for id, i := range f.inst {
		if i.Symbol == in.Symbol {
			f.watch[id] = true
			return i, nil
		}
	}
	i := store.Instrument{
		ID: uuid.New(), Symbol: in.Symbol, Name: in.Name, Exchange: in.Exchange, Kind: "stock",
		QuoteCurrency: in.QuoteCurrency, ReportCurrency: in.ReportCurrency, IsFinancial: in.IsFinancial,
		CreatedAt: time.Date(2026, 3, 2, 14, 5, 0, 0, time.UTC),
	}
	f.inst[i.ID] = i
	f.watch[i.ID] = true
	return i, nil
}

func (f *fakeRepos) RemoveFromWatchlist(_ context.Context, _, id uuid.UUID) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if !f.watch[id] {
		return nf("liste de suivi")
	}
	delete(f.watch, id)
	return nil
}

func (f *fakeRepos) GetInstrument(_ context.Context, _, id uuid.UUID) (store.Instrument, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	i, ok := f.inst[id]
	if !ok {
		return i, nf("instrument")
	}
	return i, nil
}

func (f *fakeRepos) UpdateInstrument(_ context.Context, _, id uuid.UUID, u store.InstrumentUpdate) (store.Instrument, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	i, ok := f.inst[id]
	if !ok {
		return i, nf("instrument")
	}
	i.Name, i.QuoteCurrency, i.ReportCurrency, i.IsFinancial = u.Name, u.QuoteCurrency, u.ReportCurrency, u.IsFinancial
	f.inst[id] = i
	return i, nil
}

func factKey(f store.Fact) string {
	return fmt.Sprintf("%s/%s/%d", f.InstrumentID, f.Metric, f.FiscalYear)
}

func (f *fakeRepos) ListFacts(_ context.Context, _, id uuid.UUID) ([]store.Fact, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	var out []store.Fact
	for _, x := range f.facts {
		if x.InstrumentID == id {
			out = append(out, x)
		}
	}
	sort.Slice(out, func(a, b int) bool {
		if out[a].Metric != out[b].Metric {
			return out[a].Metric < out[b].Metric
		}
		return out[a].FiscalYear < out[b].FiscalYear
	})
	return out, nil
}

func (f *fakeRepos) WriteFact(_ context.Context, _ uuid.UUID, x store.Fact) (store.FactOutcome, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	x.FetchedAt = time.Date(2026, 10, 30, 12, 0, 0, 0, time.UTC)
	f.facts[factKey(x)] = x
	return store.FactCreated, nil
}

func (f *fakeRepos) ListOpenConflicts(_ context.Context, _, id uuid.UUID) ([]store.FactConflict, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	var out []store.FactConflict
	for _, c := range f.conflicts {
		if c.InstrumentID == id {
			out = append(out, c)
		}
	}
	return out, nil
}

func (f *fakeRepos) ResolveConflicts(_ context.Context, _, id uuid.UUID, year int, metric string) (int, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	kept, n := f.conflicts[:0], 0
	for _, c := range f.conflicts {
		if c.InstrumentID == id && c.FiscalYear == year && c.Metric == metric {
			n++
			continue
		}
		kept = append(kept, c)
	}
	f.conflicts = kept
	return n, nil
}

func (f *fakeRepos) GetSnapshot(_ context.Context, _, id uuid.UUID) (store.Snapshot, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	s, ok := f.snaps[id]
	if !ok {
		return s, nf("instantané")
	}
	return s, nil
}

func (f *fakeRepos) SaveSnapshot(_ context.Context, _ uuid.UUID, s store.Snapshot) (bool, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.snaps[s.InstrumentID] = s
	return true, nil
}

func (f *fakeRepos) GetQualitative(_ context.Context, _, id uuid.UUID) (store.Qualitative, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	q, ok := f.quals[id]
	if !ok {
		return q, nf("qualitatif")
	}
	return q, nil
}

func (f *fakeRepos) SaveQualitative(_ context.Context, _ uuid.UUID, q store.Qualitative) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.quals[q.InstrumentID] = q
	return nil
}

func (f *fakeRepos) ArchiveEvaluation(_ context.Context, _ uuid.UUID, in store.EvaluationInput) (store.Evaluation, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	h, err := store.InputsHash(in.Inputs)
	if err != nil {
		return store.Evaluation{}, err
	}
	e := store.Evaluation{
		ID: uuid.New(), InstrumentID: in.InstrumentID, Mode: in.Mode, RulesVersion: in.RulesVersion, RulesHash: in.RulesHash,
		InputsHash: h, Inputs: in.Inputs, Result: in.Result, CreatedAt: time.Date(2026, 10, 30, 12, 10, 0, 0, time.UTC),
	}
	f.evals = append([]store.Evaluation{e}, f.evals...)
	return e, nil
}

func (f *fakeRepos) GetEvaluation(_ context.Context, _, id uuid.UUID) (store.Evaluation, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	for _, e := range f.evals {
		if e.ID == id {
			return e, nil
		}
	}
	return store.Evaluation{}, nf("évaluation")
}

func (f *fakeRepos) ListEvaluations(_ context.Context, _, id uuid.UUID, _ int) ([]store.Evaluation, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	var out []store.Evaluation
	for _, e := range f.evals {
		if e.InstrumentID == id {
			out = append(out, e)
		}
	}
	return out, nil
}

func (f *fakeRepos) CreateImportJob(_ context.Context, _ uuid.UUID, jobType string) (store.ImportJob, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	j := store.ImportJob{
		ID: uuid.New(), Type: jobType, Status: store.JobPending, Report: json.RawMessage(`{}`),
		CreatedAt: time.Date(2026, 10, 30, 12, 0, 0, 0, time.UTC),
	}
	f.jobs[j.ID] = j
	return j, nil
}

func (f *fakeRepos) FinishImportJob(_ context.Context, _, id uuid.UUID, status string, report json.RawMessage, at *time.Time) (store.ImportJob, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	j, ok := f.jobs[id]
	if !ok {
		return j, nf("job")
	}
	j.Status, j.FinishedAt = status, at
	if len(report) > 0 {
		j.Report = report
	}
	f.jobs[id] = j
	return j, nil
}

func (f *fakeRepos) GetImportJob(_ context.Context, _, id uuid.UUID) (store.ImportJob, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	j, ok := f.jobs[id]
	if !ok {
		return j, nf("job")
	}
	return j, nil
}

func (f *fakeRepos) ListImportJobs(context.Context, uuid.UUID, int) ([]store.ImportJob, error) {
	return nil, nil
}

// fakeImporter rend un compte rendu fixe ou une erreur.
type fakeImporter struct {
	report ingest.Report
	err    error
	panics bool
	got    []byte
	year   int
}

func (i *fakeImporter) Import(_ context.Context, _ uuid.UUID, r io.Reader, o ingest.Options) (ingest.Report, error) {
	if i.panics {
		panic("boum")
	}
	i.got, _ = io.ReadAll(r)
	i.year = o.Year1
	return i.report, i.err
}

func dec(s string) decimal.Decimal { return decimal.RequireFromString(s) }
