package store

import (
	"context"
	"errors"
	"fmt"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/omeroland/finance-privee/backend/internal/modules/stockeval/store/storedb"
)

func validSource(s string) bool {
	return s == SourceManual || s == SourceXLSX || s == SourceProvider
}

func validMetric(m string) bool {
	switch m {
	case MetricEPSDiluted, MetricDPS, MetricDividendYield, MetricSharesDiluted:
		return true
	}
	return false
}

func toFact(r storedb.StockevalAnnualFact) Fact {
	return Fact{
		InstrumentID: r.InstrumentID, FiscalYear: int(r.FiscalYear), Metric: r.Metric,
		Value: r.Value, Currency: r.Currency, Source: r.Source, FetchedAt: r.FetchedAt,
	}
}

// ListFacts renvoie les faits annuels d'un instrument, triés par mesure puis année.
func (s *Store) ListFacts(ctx context.Context, owner, instrumentID uuid.UUID) ([]Fact, error) {
	rows, err := s.read().ListAnnualFacts(ctx, storedb.ListAnnualFactsParams{OwnerID: owner, InstrumentID: instrumentID})
	if err != nil {
		return nil, fmt.Errorf("lister les faits de %s: %w", instrumentID, err)
	}
	out := make([]Fact, 0, len(rows))
	for _, r := range rows {
		out = append(out, toFact(r))
	}
	return out, nil
}

// ListOpenConflicts renvoie les conflits non résolus d'un instrument.
func (s *Store) ListOpenConflicts(ctx context.Context, owner, instrumentID uuid.UUID) ([]FactConflict, error) {
	rows, err := s.read().ListOpenConflicts(ctx, storedb.ListOpenConflictsParams{OwnerID: owner, InstrumentID: instrumentID})
	if err != nil {
		return nil, fmt.Errorf("lister les conflits de %s: %w", instrumentID, err)
	}
	out := make([]FactConflict, 0, len(rows))
	for _, r := range rows {
		out = append(out, FactConflict{
			ID: r.ID, InstrumentID: r.InstrumentID, FiscalYear: int(r.FiscalYear), Metric: r.Metric,
			ExistingValue: r.ExistingValue, ExistingSource: r.ExistingSource,
			IncomingValue: r.IncomingValue, IncomingSource: r.IncomingSource, CreatedAt: r.CreatedAt,
		})
	}
	return out, nil
}

// WriteFact écrit un fait en respectant la priorité des sources :
//   - absent : créé ;
//   - identique (valeur, devise, source) : aucun effet, aucune ligne d'audit ;
//   - existant manuel et entrant non manuel de valeur différente : la valeur
//     manuelle est conservée et un conflit est créé (une seule fois par
//     divergence) ;
//   - même valeur mais entrant manuel sur une source non manuelle : la source
//     devient manuelle ;
//   - sinon (entrant manuel, ou existant non manuel) : remplacé.
func (s *Store) WriteFact(ctx context.Context, owner uuid.UUID, f Fact) (FactOutcome, error) {
	if !validSource(f.Source) || !validMetric(f.Metric) {
		return "", fmt.Errorf("source ou mesure: %w", ErrInvalid)
	}
	if f.FiscalYear < 1900 || f.FiscalYear > 2200 {
		return "", fmt.Errorf("année fiscale: %w", ErrInvalid)
	}
	if f.Currency != nil && len(*f.Currency) != 3 {
		return "", fmt.Errorf("devise: %w", ErrInvalid)
	}
	year := int32(f.FiscalYear) //nolint:gosec // bornée à 1900..2200 ci-dessus
	outcome := FactNoop
	err := s.inTx(ctx, func(q *storedb.Queries) error {
		if err := requireInstrument(ctx, q, owner, f.InstrumentID); err != nil {
			return err
		}
		key := storedb.GetAnnualFactForUpdateParams{
			OwnerID: owner, InstrumentID: f.InstrumentID, FiscalYear: year, Metric: f.Metric,
		}
		cur, err := q.GetAnnualFactForUpdate(ctx, key)
		exists := true
		if errors.Is(err, pgx.ErrNoRows) {
			exists = false
		} else if err != nil {
			return fmt.Errorf("charger le fait %s/%d: %w", f.Metric, f.FiscalYear, err)
		}

		upsert := func() error {
			return q.UpsertAnnualFact(ctx, storedb.UpsertAnnualFactParams{
				OwnerID: owner, InstrumentID: f.InstrumentID, FiscalYear: year, Metric: f.Metric,
				Value: f.Value, Currency: f.Currency, Source: f.Source,
			})
		}
		if !exists {
			if err := upsert(); err != nil {
				return fmt.Errorf("créer le fait: %w", err)
			}
			outcome = FactCreated
			return audit(ctx, q, owner, "fact.create", "stockeval.annual_fact", nil, f)
		}

		same := cur.Value.Equal(f.Value)
		if same && cur.Source == f.Source && sameCurrency(cur.Currency, f.Currency) {
			return nil
		}
		protected := cur.Source == SourceManual && f.Source != SourceManual
		if protected && same {
			return nil // même valeur, source moins fiable : rien à changer
		}
		if protected {
			ex := storedb.FindOpenConflictParams{
				OwnerID: owner, InstrumentID: f.InstrumentID, FiscalYear: year, Metric: f.Metric,
				IncomingValue: f.Value, IncomingSource: f.Source, ExistingValue: cur.Value, ExistingSource: cur.Source,
			}
			if _, err := q.FindOpenConflict(ctx, ex); err == nil {
				return nil // conflit identique déjà ouvert
			} else if !errors.Is(err, pgx.ErrNoRows) {
				return fmt.Errorf("chercher un conflit ouvert: %w", err)
			}
			c, err := q.InsertFactConflict(ctx, storedb.InsertFactConflictParams{
				OwnerID: owner, InstrumentID: f.InstrumentID, FiscalYear: year, Metric: f.Metric,
				ExistingValue: cur.Value, ExistingSource: cur.Source, IncomingValue: f.Value, IncomingSource: f.Source,
			})
			if err != nil {
				return fmt.Errorf("créer le conflit: %w", err)
			}
			outcome = FactConflicted
			return audit(ctx, q, owner, "fact.conflict", "stockeval.fact_conflict", nil, c)
		}
		if err := upsert(); err != nil {
			return fmt.Errorf("mettre à jour le fait: %w", err)
		}
		outcome = FactUpdated
		return audit(ctx, q, owner, "fact.update", "stockeval.annual_fact", toFact(cur), f)
	})
	if err != nil {
		return "", err
	}
	return outcome, nil
}

func sameCurrency(a, b *string) bool {
	if a == nil || b == nil {
		return a == b
	}
	return *a == *b
}
