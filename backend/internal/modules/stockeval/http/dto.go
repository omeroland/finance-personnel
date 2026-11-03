package httpapi

import (
	"encoding/json"
	"fmt"
	"slices"
	"strings"
	"time"

	"github.com/google/uuid"
	openapi_types "github.com/oapi-codegen/runtime/types"
	"github.com/shopspring/decimal"

	"github.com/omeroland/finance-privee/backend/internal/api/gen"
	"github.com/omeroland/finance-privee/backend/internal/modules/stockeval/store"
)

func ptr[T any](v T) *T { return &v }

// Correspondance entre les mesures du contrat et celles du dépôt.
var metricToStore = map[gen.Metric]string{
	gen.DilutedEps:       store.MetricEPSDiluted,
	gen.DividendPerShare: store.MetricDPS,
	gen.AnnualReturn:     store.MetricDividendYield,
	gen.DilutedShares:    store.MetricSharesDiluted,
}

var metricFromStore = func() map[string]gen.Metric {
	m := make(map[string]gen.Metric, len(metricToStore))
	for k, v := range metricToStore {
		m[v] = k
	}
	return m
}()

// stockDTO représente l'instrument selon le contrat ; faux s'il n'est pas
// représentable (symbole sans bourse reconnue, devise hors CAD/USD).
func stockDTO(i store.Instrument) (gen.Stock, bool) {
	if !validSymbol(i.Symbol) {
		return gen.Stock{}, false
	}
	ex := gen.Exchange(i.Exchange)
	if !ex.Valid() {
		var ok bool
		if ex, ok = exchangeOf(i.Symbol); !ok {
			return gen.Stock{}, false
		}
	}
	q, r := gen.Currency(i.QuoteCurrency), gen.Currency(i.ReportCurrency)
	if !q.Valid() || !r.Valid() {
		return gen.Stock{}, false
	}
	return gen.Stock{
		Symbol: i.Symbol, Name: i.Name, Exchange: ex, QuoteCurrency: q, ReportingCurrency: r,
		IsFinancial: i.IsFinancial, CreatedAt: i.CreatedAt.UTC(),
	}, true
}

func factDTO(f store.Fact, inst store.Instrument) gen.AnnualFact {
	cur := inst.ReportCurrency
	if f.Currency != nil {
		cur = *f.Currency
	}
	dto := gen.AnnualFact{
		FiscalYear: f.FiscalYear, Metric: metricFromStore[f.Metric], Value: f.Value.String(),
		Currency: gen.Currency(cur), Source: f.Source,
	}
	if !f.FetchedAt.IsZero() {
		dto.FetchedAt = ptr(f.FetchedAt.UTC())
	}
	return dto
}

func factsDTO(fs []store.Fact, inst store.Instrument) []gen.AnnualFact {
	out := make([]gen.AnnualFact, 0, len(fs))
	for _, f := range fs {
		out = append(out, factDTO(f, inst))
	}
	return out
}

func nullDec(n decimal.NullDecimal) *string {
	if !n.Valid {
		return nil
	}
	return ptr(n.Decimal.String())
}

// snapshotDTO : le contrat porte une seule devise ; c'est celle de cotation (D200).
func snapshotDTO(s store.Snapshot, inst store.Instrument) gen.Snapshot {
	dto := gen.Snapshot{
		Currency: gen.Currency(inst.QuoteCurrency), Price: nullDec(s.Price), AnnualDividend: nullDec(s.ExpectedDividend),
		LongTermDebt: nullDec(s.LTDebt), Equity: nullDec(s.Equity), BookValuePerShare: nullDec(s.BookValuePerShare),
		Source: ptr(s.Source),
	}
	if s.FXRate.Valid {
		f, _ := s.FXRate.Decimal.Float64()
		dto.FxRate = ptr(float32(f))
	}
	if !s.AsOf.IsZero() {
		dto.AsOf = &openapi_types.Date{Time: s.AsOf.UTC()}
	}
	return dto
}

// Les notes des règles qualitatives sont rangées dans store.Qualitative.Notes : {"rule1": "...", ...}.
func qualitativeDTO(q *store.Qualitative) gen.Qualitative {
	if q == nil {
		q = &store.Qualitative{}
	}
	notes := map[string]string{}
	if len(q.Notes) > 0 {
		_ = json.Unmarshal(q.Notes, &notes) // notes illisibles : ignorées
	}
	answer := func(b *bool, key string) gen.RuleAnswer {
		a := gen.RuleAnswer{Answer: gen.Unanswered}
		if b != nil {
			a.Answer = gen.No
			if *b {
				a.Answer = gen.Yes
			}
		}
		if n := notes[key]; n != "" {
			a.Note = ptr(n)
		}
		return a
	}
	dto := gen.Qualitative{
		Rule1: answer(q.R1, "rule1"), Rule2: answer(q.R2, "rule2"), Rule3: answer(q.R3, "rule3"), Rule4: answer(q.R4, "rule4"),
		ManualBuyback: q.BuybackManual,
	}
	if q.ReviewedAt != nil {
		dto.ReviewedAt = ptr(q.ReviewedAt.UTC())
	}
	return dto
}

func answerToStore(a gen.Answer) *bool {
	switch a {
	case gen.Yes:
		return ptr(true)
	case gen.No:
		return ptr(false)
	}
	return nil
}

// qualitativeToStore convertit la saisie ; reviewedAt est fixé par le serveur.
func qualitativeToStore(in gen.Qualitative, instrumentID uuid.UUID, now time.Time) (store.Qualitative, error) {
	notes := map[string]string{}
	for k, r := range map[string]gen.RuleAnswer{"rule1": in.Rule1, "rule2": in.Rule2, "rule3": in.Rule3, "rule4": in.Rule4} {
		if r.Note != nil && *r.Note != "" {
			notes[k] = *r.Note
		}
	}
	raw, err := json.Marshal(notes)
	if err != nil {
		return store.Qualitative{}, fmt.Errorf("sérialiser les notes: %w", err)
	}
	return store.Qualitative{
		InstrumentID: instrumentID,
		R1:           answerToStore(in.Rule1.Answer), R2: answerToStore(in.Rule2.Answer),
		R3: answerToStore(in.Rule3.Answer), R4: answerToStore(in.Rule4.Answer),
		Notes: raw, BuybackManual: in.ManualBuyback, ReviewedAt: &now,
	}, nil
}

// sourcesDTO résume les faits par source (nombre de valeurs, dernière récupération).
func sourcesDTO(fs []store.Fact) []gen.SourceSummary {
	by := map[string]*gen.SourceSummary{}
	for _, f := range fs {
		s := by[f.Source]
		if s == nil {
			s = &gen.SourceSummary{Source: f.Source}
			by[f.Source] = s
		}
		s.FactCount++
		if !f.FetchedAt.IsZero() && (s.LastFetchedAt == nil || f.FetchedAt.After(*s.LastFetchedAt)) {
			s.LastFetchedAt = ptr(f.FetchedAt.UTC())
		}
	}
	out := make([]gen.SourceSummary, 0, len(by))
	for _, s := range by {
		out = append(out, *s)
	}
	slices.SortFunc(out, func(a, b gen.SourceSummary) int { return strings.Compare(a.Source, b.Source) })
	return out
}

func conflictsDTO(cs []store.FactConflict) []gen.SourceConflict {
	out := make([]gen.SourceConflict, 0, len(cs))
	for _, c := range cs {
		out = append(out, gen.SourceConflict{
			FiscalYear: c.FiscalYear, Metric: metricFromStore[c.Metric],
			Values: []gen.SourceValue{
				{Source: c.ExistingSource, Value: c.ExistingValue.String()},
				{Source: c.IncomingSource, Value: c.IncomingValue.String()},
			},
		})
	}
	return out
}
