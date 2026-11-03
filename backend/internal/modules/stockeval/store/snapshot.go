package store

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/omeroland/finance-privee/backend/internal/modules/stockeval/store/storedb"
)

func toSnapshot(r storedb.StockevalSnapshot) Snapshot {
	return Snapshot{
		InstrumentID: r.InstrumentID, Price: r.Price, ExpectedDividend: r.ExpectedDividend, LTDebt: r.LtDebt,
		Equity: r.Equity, DebtToEquity: r.DebtToEquity, BookValuePerShare: r.BookValuePerShare,
		PEImported: r.PeImported, PayoutRatioImported: r.PayoutRatioImported, PBImported: r.PbImported,
		FXRate: r.FxRate, AsOf: r.AsOf, Source: r.Source,
	}
}

// normTime ramène une date à la précision de PostgreSQL pour comparer.
func normTime(t time.Time) time.Time { return t.UTC().Truncate(time.Microsecond) }

// GetSnapshot renvoie les valeurs courantes d'un instrument.
func (s *Store) GetSnapshot(ctx context.Context, owner, instrumentID uuid.UUID) (Snapshot, error) {
	r, err := s.read().GetSnapshot(ctx, storedb.GetSnapshotParams{OwnerID: owner, InstrumentID: instrumentID})
	if err != nil {
		return Snapshot{}, notFound(err, "instantané de "+instrumentID.String())
	}
	return toSnapshot(r), nil
}

// SaveSnapshot écrit l'instantané. Une ligne manuelle n'est pas écrasée par une
// source non manuelle (written=false). Une écriture identique n'a aucun effet.
func (s *Store) SaveSnapshot(ctx context.Context, owner uuid.UUID, in Snapshot) (bool, error) {
	if !validSource(in.Source) || in.AsOf.IsZero() {
		return false, fmt.Errorf("source ou date de l'instantané: %w", ErrInvalid)
	}
	written := false
	err := s.inTx(ctx, func(q *storedb.Queries) error {
		if err := requireInstrument(ctx, q, owner, in.InstrumentID); err != nil {
			return err
		}
		cur, err := q.GetSnapshotForUpdate(ctx, storedb.GetSnapshotForUpdateParams{OwnerID: owner, InstrumentID: in.InstrumentID})
		var before any
		switch {
		case errors.Is(err, pgx.ErrNoRows):
		case err != nil:
			return fmt.Errorf("charger l'instantané: %w", err)
		default:
			c := toSnapshot(cur)
			if cur.Source == SourceManual && in.Source != SourceManual {
				return nil
			}
			a, b := c, in
			a.AsOf, b.AsOf = normTime(a.AsOf), normTime(b.AsOf)
			ja, _ := json.Marshal(a)
			jb, _ := json.Marshal(b)
			if bytes.Equal(ja, jb) {
				written = true // la donnée est bien en place ; rien à écrire
				return nil
			}
			before = c
		}
		if err := q.UpsertSnapshot(ctx, storedb.UpsertSnapshotParams{
			OwnerID: owner, InstrumentID: in.InstrumentID, Price: in.Price, ExpectedDividend: in.ExpectedDividend,
			LtDebt: in.LTDebt, Equity: in.Equity, DebtToEquity: in.DebtToEquity,
			BookValuePerShare: in.BookValuePerShare, PeImported: in.PEImported,
			PayoutRatioImported: in.PayoutRatioImported, PbImported: in.PBImported, FxRate: in.FXRate,
			AsOf: in.AsOf, Source: in.Source,
		}); err != nil {
			return fmt.Errorf("écrire l'instantané: %w", err)
		}
		written = true
		return audit(ctx, q, owner, "snapshot.save", "stockeval.snapshot", before, in)
	})
	return written, err
}

func toQualitative(r storedb.StockevalQualitative) Qualitative {
	return Qualitative{
		InstrumentID: r.InstrumentID, R1: r.R1, R2: r.R2, R3: r.R3, R4: r.R4,
		Notes: r.Notes, BuybackManual: r.BuybackManual, ReviewedAt: r.ReviewedAt,
	}
}

// GetQualitative renvoie le jugement qualitatif d'un instrument.
func (s *Store) GetQualitative(ctx context.Context, owner, instrumentID uuid.UUID) (Qualitative, error) {
	r, err := s.read().GetQualitative(ctx, storedb.GetQualitativeParams{OwnerID: owner, InstrumentID: instrumentID})
	if err != nil {
		return Qualitative{}, notFound(err, "qualitatif de "+instrumentID.String())
	}
	return toQualitative(r), nil
}

// SaveQualitative écrit le jugement ; une écriture identique n'a aucun effet.
func (s *Store) SaveQualitative(ctx context.Context, owner uuid.UUID, in Qualitative) error {
	notes := in.Notes
	if len(notes) == 0 {
		notes = json.RawMessage(`{}`)
	}
	canon, err := canonicalJSON(notes)
	if err != nil {
		return fmt.Errorf("notes: %w", ErrInvalid)
	}
	in.Notes = canon
	return s.inTx(ctx, func(q *storedb.Queries) error {
		if err := requireInstrument(ctx, q, owner, in.InstrumentID); err != nil {
			return err
		}
		cur, err := q.GetQualitativeForUpdate(ctx, storedb.GetQualitativeForUpdateParams{OwnerID: owner, InstrumentID: in.InstrumentID})
		var before any
		switch {
		case errors.Is(err, pgx.ErrNoRows):
		case err != nil:
			return fmt.Errorf("charger le qualitatif: %w", err)
		default:
			c := toQualitative(cur)
			if c.Notes, err = canonicalJSON(c.Notes); err != nil {
				return err
			}
			a, b := c, in
			if a.ReviewedAt != nil {
				t := normTime(*a.ReviewedAt)
				a.ReviewedAt = &t
			}
			if b.ReviewedAt != nil {
				t := normTime(*b.ReviewedAt)
				b.ReviewedAt = &t
			}
			ja, _ := json.Marshal(a)
			jb, _ := json.Marshal(b)
			if bytes.Equal(ja, jb) {
				return nil
			}
			before = c
		}
		if err := q.UpsertQualitative(ctx, storedb.UpsertQualitativeParams{
			OwnerID: owner, InstrumentID: in.InstrumentID, R1: in.R1, R2: in.R2, R3: in.R3, R4: in.R4,
			Notes: in.Notes, BuybackManual: in.BuybackManual, ReviewedAt: in.ReviewedAt,
		}); err != nil {
			return fmt.Errorf("écrire le qualitatif: %w", err)
		}
		return audit(ctx, q, owner, "qualitative.save", "stockeval.qualitative", before, in)
	})
}
