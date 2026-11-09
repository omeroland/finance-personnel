package store

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/shopspring/decimal"

	"github.com/omeroland/finance-privee/backend/internal/core/store/coredb"
)

// SourceManual est la source des écritures saisies par l'API.
const SourceManual = "manual"

// TxInput est une écriture à inscrire. Quantités, prix, montants et frais sont
// positifs : le sens vient du type (D321).
type TxInput struct {
	AccountID            uuid.UUID
	Type                 string
	TradeDate            time.Time
	Symbol               string // « BOURSE:SYMBOLE », facultatif selon le type
	Quantity             *decimal.Decimal
	Price                *decimal.Decimal
	Amount               *decimal.Decimal
	Fee                  *decimal.Decimal
	SplitRatio           *decimal.Decimal
	CounterpartAccountID *uuid.UUID
	Currency             string
	ReversesID           *uuid.UUID
	Note                 *string
	Source               string
}

// Tx est une écriture du registre avec le symbole de son instrument.
type Tx struct {
	coredb.CoreTransaction
	Symbol string
}

// ensureInstrument rend l'instrument du symbole, créé au besoin comme FNB (D400).
func ensureInstrument(ctx context.Context, q *coredb.Queries, owner uuid.UUID, symbol, currency string) (coredb.CoreInstrument, error) {
	inst, err := q.GetInstrumentBySymbol(ctx, coredb.GetInstrumentBySymbolParams{OwnerID: owner, Symbol: symbol})
	if err == nil {
		return inst, nil
	}
	if !errors.Is(err, pgx.ErrNoRows) {
		return inst, fmt.Errorf("chercher l'instrument: %w", err)
	}
	exchange, _, _ := strings.Cut(symbol, ":")
	inst, err = q.InsertInstrument(ctx, coredb.InsertInstrumentParams{
		OwnerID: owner, Symbol: symbol, Name: symbol, Exchange: exchange, Kind: "etf",
		QuoteCurrency: currency, ReportCurrency: currency,
	})
	if err != nil {
		return inst, mapPg(fmt.Errorf("créer l'instrument: %w", err))
	}
	if err := Audit(ctx, q, owner, "instrument.create", "core.instrument", nil,
		map[string]any{"id": inst.ID, "symbol": inst.Symbol}); err != nil {
		return inst, err
	}
	return inst, nil
}

// insertTx inscrit une écriture dans la transaction courante, avec son audit.
func insertTx(ctx context.Context, q *coredb.Queries, owner uuid.UUID, in TxInput) (Tx, error) {
	acc, err := q.GetAccount(ctx, coredb.GetAccountParams{OwnerID: owner, ID: in.AccountID})
	if err != nil {
		return Tx{}, notFound(err, "Compte")
	}
	if in.Currency != acc.Currency {
		return Tx{}, &InvalidError{Message: "La devise de la transaction doit être celle du compte."}
	}
	if in.CounterpartAccountID != nil {
		if _, err := q.GetAccount(ctx, coredb.GetAccountParams{OwnerID: owner, ID: *in.CounterpartAccountID}); err != nil {
			if errors.Is(err, pgx.ErrNoRows) {
				return Tx{}, &InvalidError{Message: "Le compte de contrepartie est introuvable."}
			}
			return Tx{}, fmt.Errorf("charger la contrepartie: %w", err)
		}
	}
	if in.ReversesID != nil {
		if _, err := q.GetTransaction(ctx, coredb.GetTransactionParams{OwnerID: owner, ID: *in.ReversesID}); err != nil {
			return Tx{}, notFound(err, "Transaction à inverser")
		}
	}
	var instID *uuid.UUID
	if in.Symbol != "" {
		inst, err := ensureInstrument(ctx, q, owner, in.Symbol, in.Currency)
		if err != nil {
			return Tx{}, err
		}
		instID = &inst.ID
	}
	src := in.Source
	if src == "" {
		src = SourceManual
	}
	row, err := q.InsertTransaction(ctx, coredb.InsertTransactionParams{
		OwnerID: owner, AccountID: in.AccountID, Type: in.Type, TradeDate: in.TradeDate, InstrumentID: instID,
		Quantity: nullDec(in.Quantity), Price: nullDec(in.Price), Amount: nullDec(in.Amount), Fee: nullDec(in.Fee),
		SplitRatio: nullDec(in.SplitRatio), CounterpartAccountID: in.CounterpartAccountID, Currency: in.Currency,
		ReversesID: in.ReversesID, Note: in.Note, Source: src,
	})
	if err != nil {
		return Tx{}, mapPg(fmt.Errorf("inscrire la transaction: %w", err))
	}
	// L'audit ne porte ni quantité ni montant : l'identifiant renvoie au registre.
	if err := Audit(ctx, q, owner, "transaction.create", "core.transaction", nil, map[string]any{
		"id": row.ID, "accountId": row.AccountID, "type": row.Type, "tradeDate": in.TradeDate.Format(time.DateOnly),
		"symbol": in.Symbol, "reversesId": row.ReversesID, "source": row.Source,
	}); err != nil {
		return Tx{}, err
	}
	return Tx{CoreTransaction: row, Symbol: in.Symbol}, nil
}

// AddTransaction inscrit une écriture (ou son inverse si ReversesID est posé).
// Une originale déjà inversée donne ErrConflict.
func (s *Store) AddTransaction(ctx context.Context, owner uuid.UUID, in TxInput) (Tx, error) {
	var out Tx
	err := s.InTx(ctx, func(tx pgx.Tx) error {
		var err error
		out, err = insertTx(ctx, coredb.New(tx), owner, in)
		return err
	})
	return out, err
}

// ListTransactions rend le registre d'un compte, de la plus ancienne à la plus récente.
func (s *Store) ListTransactions(ctx context.Context, owner, account uuid.UUID, from, to *time.Time) ([]Tx, error) {
	q := s.read()
	if _, err := q.GetAccount(ctx, coredb.GetAccountParams{OwnerID: owner, ID: account}); err != nil {
		return nil, notFound(err, "Compte")
	}
	rows, err := q.ListTransactionsBetween(ctx, coredb.ListTransactionsBetweenParams{
		OwnerID: owner, AccountID: account, FromDate: from, ToDate: to,
	})
	if err != nil {
		return nil, fmt.Errorf("lister le registre: %w", err)
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

// ImportRow est une ligne d'import prête à inscrire (compte désigné par son nom).
type ImportRow struct {
	Account     string
	Counterpart string
	Type        string
	Date        time.Time
	Symbol      string
	Quantity    *decimal.Decimal
	Price       *decimal.Decimal
	Amount      *decimal.Decimal
	Fee         *decimal.Decimal
	SplitRatio  *decimal.Decimal
	Currency    string
	Note        string
	Source      string
	Occurrence  int
}

// Statuts d'une ligne importée.
const (
	ImportCreated   = "created"
	ImportDuplicate = "duplicate"
	ImportRejected  = "rejected"
)

// ImportOutcome est le sort d'une ligne.
type ImportOutcome struct{ Status, Message string }

func round6(d *decimal.Decimal) *decimal.Decimal {
	if d == nil {
		return nil
	}
	r := d.Round(6)
	return &r
}

// ImportTransactions inscrit des lignes dans une seule transaction SQL (D392) :
// la ligne n'est ajoutée que si le registre compte moins d'originales de même
// clé naturelle que son rang. Un refus (compte inconnu, devise, contrainte) est
// rendu ligne par ligne, sans interrompre les autres.
func (s *Store) ImportTransactions(ctx context.Context, owner uuid.UUID, rows []ImportRow) ([]ImportOutcome, error) {
	outs := make([]ImportOutcome, len(rows))
	err := s.InTx(ctx, func(tx pgx.Tx) error {
		q := coredb.New(tx)
		for i, r := range rows {
			if err := ctx.Err(); err != nil {
				return fmt.Errorf("import interrompu: %w", err)
			}
			o, err := importOne(ctx, tx, q, owner, r)
			if err != nil {
				return err
			}
			outs[i] = o
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	return outs, nil
}

func importOne(ctx context.Context, tx pgx.Tx, q *coredb.Queries, owner uuid.UUID, r ImportRow) (ImportOutcome, error) {
	acc, err := q.GetAccountByName(ctx, coredb.GetAccountByNameParams{OwnerID: owner, Name: r.Account})
	if errors.Is(err, pgx.ErrNoRows) {
		return ImportOutcome{Status: ImportRejected, Message: "compte inconnu"}, nil
	}
	if err != nil {
		return ImportOutcome{}, fmt.Errorf("chercher le compte: %w", err)
	}
	if r.Currency != acc.Currency {
		return ImportOutcome{Status: ImportRejected, Message: "devise différente de celle du compte"}, nil
	}
	in := TxInput{
		AccountID: acc.ID, Type: r.Type, TradeDate: r.Date, Symbol: r.Symbol,
		Quantity: round6(r.Quantity), Price: round6(r.Price), Amount: round6(r.Amount), Fee: round6(r.Fee),
		SplitRatio: round6(r.SplitRatio), Currency: r.Currency, Source: r.Source,
	}
	if r.Note != "" {
		in.Note = &r.Note
	}
	if r.Counterpart != "" {
		cp, err := q.GetAccountByName(ctx, coredb.GetAccountByNameParams{OwnerID: owner, Name: r.Counterpart})
		if errors.Is(err, pgx.ErrNoRows) {
			return ImportOutcome{Status: ImportRejected, Message: "contrepartie inconnue"}, nil
		}
		if err != nil {
			return ImportOutcome{}, fmt.Errorf("chercher la contrepartie: %w", err)
		}
		in.CounterpartAccountID = &cp.ID
	}
	// Clé naturelle : sans instrument connu, aucune écriture existante ne correspond.
	var instID *uuid.UUID
	if r.Symbol != "" {
		inst, err := q.GetInstrumentBySymbol(ctx, coredb.GetInstrumentBySymbolParams{OwnerID: owner, Symbol: r.Symbol})
		if err == nil {
			instID = &inst.ID
		} else if !errors.Is(err, pgx.ErrNoRows) {
			return ImportOutcome{}, fmt.Errorf("chercher l'instrument: %w", err)
		}
	}
	if r.Symbol == "" || instID != nil {
		n, err := q.CountNaturalKey(ctx, coredb.CountNaturalKeyParams{
			OwnerID: owner, AccountID: acc.ID, Type: r.Type, TradeDate: r.Date, InstrumentID: instID,
			Quantity: nullDec(in.Quantity), Price: nullDec(in.Price), Amount: nullDec(in.Amount),
			SplitRatio: nullDec(in.SplitRatio),
		})
		if err != nil {
			return ImportOutcome{}, fmt.Errorf("compter la clé naturelle: %w", err)
		}
		if int(n) >= max(r.Occurrence, 1) {
			return ImportOutcome{Status: ImportDuplicate}, nil
		}
	}
	// Point de sauvegarde : une contrainte violée refuse la ligne sans annuler le lot.
	sp, err := tx.Begin(ctx)
	if err != nil {
		return ImportOutcome{}, fmt.Errorf("ouvrir le point de sauvegarde: %w", err)
	}
	_, err = insertTx(ctx, coredb.New(sp), owner, in)
	if err != nil {
		_ = sp.Rollback(ctx)
		var inv *InvalidError
		if errors.As(err, &inv) {
			return ImportOutcome{Status: ImportRejected, Message: inv.Message}, nil
		}
		if errors.Is(err, ErrConflict) {
			return ImportOutcome{Status: ImportRejected, Message: "conflit avec une écriture existante"}, nil
		}
		return ImportOutcome{}, err
	}
	if err := sp.Commit(ctx); err != nil {
		return ImportOutcome{}, fmt.Errorf("libérer le point de sauvegarde: %w", err)
	}
	return ImportOutcome{Status: ImportCreated}, nil
}
