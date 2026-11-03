package store

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/omeroland/finance-privee/backend/internal/modules/stockeval/store/storedb"
)

func toInstrument(r storedb.CoreInstrument) Instrument {
	return Instrument{
		ID: r.ID, Symbol: r.Symbol, Name: r.Name, Exchange: r.Exchange, Kind: r.Kind,
		QuoteCurrency: r.QuoteCurrency, ReportCurrency: r.ReportCurrency,
		IsFinancial: r.IsFinancial, CreatedAt: r.CreatedAt,
	}
}

func validateInstrument(in *InstrumentInput) error {
	in.Symbol = strings.TrimSpace(in.Symbol)
	if in.Symbol == "" || len(in.Symbol) > 32 {
		return fmt.Errorf("symbole: %w", ErrInvalid)
	}
	if in.Kind == "" {
		in.Kind = "stock"
	}
	if in.Kind != "stock" && in.Kind != "etf" && in.Kind != "cash" {
		return fmt.Errorf("type d'instrument: %w", ErrInvalid)
	}
	if len(in.QuoteCurrency) != 3 || len(in.ReportCurrency) != 3 {
		return fmt.Errorf("devise: %w", ErrInvalid)
	}
	if len(in.Name) > 200 || len(in.Exchange) > 32 {
		return fmt.Errorf("nom ou bourse trop long: %w", ErrInvalid)
	}
	return nil
}

// ListWatchlist renvoie la liste de suivi du propriétaire, triée par symbole.
func (s *Store) ListWatchlist(ctx context.Context, owner uuid.UUID) ([]Instrument, error) {
	rows, err := s.read().ListWatchlist(ctx, owner)
	if err != nil {
		return nil, fmt.Errorf("lister la liste de suivi: %w", err)
	}
	out := make([]Instrument, 0, len(rows))
	for _, r := range rows {
		out = append(out, toInstrument(r))
	}
	return out, nil
}

// GetInstrument renvoie un instrument du propriétaire.
func (s *Store) GetInstrument(ctx context.Context, owner, instrumentID uuid.UUID) (Instrument, error) {
	r, err := s.read().GetInstrument(ctx, storedb.GetInstrumentParams{OwnerID: owner, ID: instrumentID})
	if err != nil {
		return Instrument{}, notFound(err, "instrument "+instrumentID.String())
	}
	return toInstrument(r), nil
}

// AddToWatchlist crée l'instrument au besoin et l'ajoute à la liste. Une seule
// ligne d'audit par ajout effectif ; rien si l'instrument y est déjà.
func (s *Store) AddToWatchlist(ctx context.Context, owner uuid.UUID, in InstrumentInput) (Instrument, error) {
	if err := validateInstrument(&in); err != nil {
		return Instrument{}, err
	}
	var out Instrument
	err := s.inTx(ctx, func(q *storedb.Queries) error {
		row, err := q.GetInstrumentBySymbol(ctx, storedb.GetInstrumentBySymbolParams{OwnerID: owner, Symbol: in.Symbol})
		created := false
		switch {
		case errors.Is(err, pgx.ErrNoRows):
			row, err = q.InsertInstrument(ctx, storedb.InsertInstrumentParams{
				OwnerID: owner, Symbol: in.Symbol, Name: in.Name, Exchange: in.Exchange, Kind: in.Kind,
				QuoteCurrency: in.QuoteCurrency, ReportCurrency: in.ReportCurrency, IsFinancial: in.IsFinancial,
			})
			if err != nil {
				return fmt.Errorf("créer l'instrument %s: %w", in.Symbol, err)
			}
			created = true
		case err != nil:
			return fmt.Errorf("charger l'instrument %s: %w", in.Symbol, err)
		}
		out = toInstrument(row)
		if !created {
			on, err := q.IsOnWatchlist(ctx, storedb.IsOnWatchlistParams{OwnerID: owner, InstrumentID: row.ID})
			if err != nil {
				return fmt.Errorf("lire la liste de suivi: %w", err)
			}
			if on {
				return nil
			}
		}
		if err := q.AddToWatchlist(ctx, storedb.AddToWatchlistParams{OwnerID: owner, InstrumentID: row.ID}); err != nil {
			return fmt.Errorf("ajouter %s à la liste de suivi: %w", in.Symbol, err)
		}
		return audit(ctx, q, owner, "watchlist.add", "watchlist", nil, out)
	})
	if err != nil {
		return Instrument{}, err
	}
	return out, nil
}

// RemoveFromWatchlist retire l'instrument de la liste (l'instrument et ses
// données restent). ErrNotFound s'il n'y figure pas.
func (s *Store) RemoveFromWatchlist(ctx context.Context, owner, instrumentID uuid.UUID) error {
	return s.inTx(ctx, func(q *storedb.Queries) error {
		inst, err := q.GetInstrument(ctx, storedb.GetInstrumentParams{OwnerID: owner, ID: instrumentID})
		if err != nil {
			return notFound(err, "instrument "+instrumentID.String())
		}
		n, err := q.RemoveFromWatchlist(ctx, storedb.RemoveFromWatchlistParams{OwnerID: owner, InstrumentID: instrumentID})
		if err != nil {
			return fmt.Errorf("retirer de la liste de suivi: %w", err)
		}
		if n == 0 {
			return fmt.Errorf("liste de suivi %s: %w", instrumentID, ErrNotFound)
		}
		return audit(ctx, q, owner, "watchlist.remove", "watchlist", toInstrument(inst), nil)
	})
}

// InstrumentUpdate remplace les métadonnées modifiables d'un instrument.
type InstrumentUpdate struct {
	Name           string
	QuoteCurrency  string
	ReportCurrency string
	IsFinancial    bool
}

// UpdateInstrument modifie les métadonnées d'un instrument du propriétaire ;
// une modification identique n'écrit rien (ni audit).
func (s *Store) UpdateInstrument(ctx context.Context, owner, instrumentID uuid.UUID, u InstrumentUpdate) (Instrument, error) {
	in := InstrumentInput{Symbol: "-", Name: u.Name, QuoteCurrency: u.QuoteCurrency, ReportCurrency: u.ReportCurrency}
	if err := validateInstrument(&in); err != nil {
		return Instrument{}, err
	}
	var out Instrument
	err := s.inTx(ctx, func(q *storedb.Queries) error {
		cur, err := q.GetInstrument(ctx, storedb.GetInstrumentParams{OwnerID: owner, ID: instrumentID})
		if err != nil {
			return notFound(err, "instrument "+instrumentID.String())
		}
		before := toInstrument(cur)
		if before.Name == u.Name && before.QuoteCurrency == u.QuoteCurrency &&
			before.ReportCurrency == u.ReportCurrency && before.IsFinancial == u.IsFinancial {
			out = before
			return nil
		}
		row, err := q.UpdateInstrument(ctx, storedb.UpdateInstrumentParams{
			OwnerID: owner, ID: instrumentID, Name: u.Name, QuoteCurrency: u.QuoteCurrency,
			ReportCurrency: u.ReportCurrency, IsFinancial: u.IsFinancial,
		})
		if err != nil {
			return fmt.Errorf("modifier l'instrument %s: %w", instrumentID, err)
		}
		out = toInstrument(row)
		return audit(ctx, q, owner, "instrument.update", "core.instrument", before, out)
	})
	return out, err
}
