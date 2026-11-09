package store

import (
	"context"
	"fmt"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/shopspring/decimal"

	"github.com/omeroland/finance-privee/backend/internal/core/store/coredb"
)

// Account est une ligne de core.account.
type Account = coredb.CoreAccount

// Statement est une ligne de core.statement.
type Statement = coredb.CoreStatement

// AccountInput est la création d'un compte.
type AccountInput struct {
	Name, Type, Broker, Currency string
}

// StatementInput est la saisie d'un relevé mensuel.
type StatementInput struct {
	AccountID    uuid.UUID
	Month        string
	EndValue     decimal.Decimal
	Contribution *decimal.Decimal
	Currency     string
}

// CreateAccount crée un compte ; un nom déjà pris donne ErrConflict.
func (s *Store) CreateAccount(ctx context.Context, owner uuid.UUID, in AccountInput) (Account, error) {
	var out Account
	err := s.InTx(ctx, func(tx pgx.Tx) error {
		q := coredb.New(tx)
		row, err := q.CreateAccount(ctx, coredb.CreateAccountParams{
			OwnerID: owner, Name: in.Name, Type: in.Type, Broker: in.Broker, Currency: in.Currency,
		})
		if err != nil {
			return mapPg(fmt.Errorf("créer le compte: %w", err))
		}
		out = row
		return Audit(ctx, q, owner, "account.create", "core.account", nil,
			map[string]any{"id": row.ID, "name": row.Name, "type": row.Type, "currency": row.Currency})
	})
	return out, err
}

// ListAccounts renvoie les comptes du propriétaire.
func (s *Store) ListAccounts(ctx context.Context, owner uuid.UUID) ([]Account, error) {
	rows, err := s.read().ListAccounts(ctx, owner)
	if err != nil {
		return nil, fmt.Errorf("lister les comptes: %w", err)
	}
	return rows, nil
}

// GetAccount renvoie un compte du propriétaire.
func (s *Store) GetAccount(ctx context.Context, owner, id uuid.UUID) (Account, error) {
	row, err := s.read().GetAccount(ctx, coredb.GetAccountParams{OwnerID: owner, ID: id})
	if err != nil {
		return Account{}, notFound(err, "Compte")
	}
	return row, nil
}

// CreateStatement enregistre un relevé ; un doublon (compte, mois) donne ErrConflict.
func (s *Store) CreateStatement(ctx context.Context, owner uuid.UUID, in StatementInput) (Statement, error) {
	var out Statement
	err := s.InTx(ctx, func(tx pgx.Tx) error {
		q := coredb.New(tx)
		acc, err := q.GetAccount(ctx, coredb.GetAccountParams{OwnerID: owner, ID: in.AccountID})
		if err != nil {
			return notFound(err, "Compte")
		}
		if acc.Currency != in.Currency {
			return &InvalidError{Message: "La devise du relevé doit être celle du compte."}
		}
		row, err := q.CreateStatement(ctx, coredb.CreateStatementParams{
			OwnerID: owner, AccountID: in.AccountID, Month: in.Month, EndValue: in.EndValue,
			Contribution: nullDec(in.Contribution),
		})
		if err != nil {
			return mapPg(fmt.Errorf("créer le relevé: %w", err))
		}
		out = row
		return Audit(ctx, q, owner, "statement.create", "core.statement", nil,
			map[string]any{"id": row.ID, "accountId": row.AccountID, "month": row.Month})
	})
	return out, err
}

func nullDec(d *decimal.Decimal) decimal.NullDecimal {
	if d == nil {
		return decimal.NullDecimal{}
	}
	return decimal.NullDecimal{Decimal: d.Round(6), Valid: true}
}
