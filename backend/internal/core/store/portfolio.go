package store

import (
	"context"
	"fmt"
	"time"

	"github.com/google/uuid"

	"github.com/omeroland/finance-privee/backend/internal/core/store/coredb"
)

// ListStatements rend les relevés mensuels d'un compte, du plus ancien au plus récent.
func (s *Store) ListStatements(ctx context.Context, owner, account uuid.UUID) ([]Statement, error) {
	q := s.read()
	if _, err := q.GetAccount(ctx, coredb.GetAccountParams{OwnerID: owner, ID: account}); err != nil {
		return nil, notFound(err, "Compte")
	}
	rows, err := q.ListStatements(ctx, coredb.ListStatementsParams{OwnerID: owner, AccountID: account})
	if err != nil {
		return nil, fmt.Errorf("lister les relevés: %w", err)
	}
	return rows, nil
}

// Entries rend les écritures en vigueur (ni corrigées ni inverses) qui touchent
// le compte jusqu'au jour asOf inclus, de la plus ancienne à la plus récente,
// y compris les transferts reçus (le compte est alors la contrepartie).
func (s *Store) Entries(ctx context.Context, owner, account uuid.UUID, asOf time.Time) ([]Tx, error) {
	q := s.read()
	if _, err := q.GetAccount(ctx, coredb.GetAccountParams{OwnerID: owner, ID: account}); err != nil {
		return nil, notFound(err, "Compte")
	}
	rows, err := q.ListEffectiveForAccount(ctx, coredb.ListEffectiveForAccountParams{OwnerID: owner, AccountID: account, AsOf: asOf})
	if err != nil {
		return nil, fmt.Errorf("lister les écritures en vigueur: %w", err)
	}
	symbols := map[uuid.UUID]string{}
	out := make([]Tx, 0, len(rows))
	for _, r := range rows {
		t := Tx{CoreTransaction: r}
		if r.InstrumentID != nil {
			sym, ok := symbols[*r.InstrumentID]
			if !ok {
				inst, err := q.GetInstrument(ctx, coredb.GetInstrumentParams{OwnerID: owner, ID: *r.InstrumentID})
				if err != nil {
					return nil, fmt.Errorf("charger l'instrument: %w", err)
				}
				sym = inst.Symbol
				symbols[*r.InstrumentID] = sym
			}
			t.Symbol = sym
		}
		out = append(out, t)
	}
	return out, nil
}
