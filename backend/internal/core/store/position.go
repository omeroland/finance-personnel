package store

import (
	"context"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/shopspring/decimal"

	"github.com/omeroland/finance-privee/backend/internal/core/store/coredb"
)

// Position est une ligne ouverte d'un compte, dérivée du registre (D322).
type Position struct {
	Symbol   string
	Name     string
	Quantity decimal.Decimal
	// AverageCost est le coût moyen par part, frais d'achat compris ; nil si
	// inconnu (titres reçus par transfert). Aucun prix de marché n'est stocké en phase 2 (D402).
	AverageCost *decimal.Decimal
}

// Holdings est l'état d'un compte à une date.
type Holdings struct {
	Account   Account
	AsOf      time.Time
	Cash      decimal.Decimal
	Positions []Position
}

type lot struct {
	qty, cost decimal.Decimal
	known     bool
}

// Holdings dérive encaisse, positions et coûts moyens du registre au jour asOf.
// Les quantités viennent de l'agrégat SQL (D322) ; l'encaisse et le coût moyen
// sont recalculés des écritures en vigueur.
func (s *Store) Holdings(ctx context.Context, owner, account uuid.UUID, asOf time.Time) (Holdings, error) {
	q := s.read()
	acc, err := q.GetAccount(ctx, coredb.GetAccountParams{OwnerID: owner, ID: account})
	if err != nil {
		return Holdings{}, notFound(err, "Compte")
	}
	pos, err := q.ListPositionsAt(ctx, coredb.ListPositionsAtParams{OwnerID: owner, AccountID: account, AsOf: asOf})
	if err != nil {
		return Holdings{}, fmt.Errorf("lire les positions: %w", err)
	}
	eff, err := q.ListEffectiveForAccount(ctx, coredb.ListEffectiveForAccountParams{OwnerID: owner, AccountID: account, AsOf: asOf})
	if err != nil {
		return Holdings{}, fmt.Errorf("lire les écritures en vigueur: %w", err)
	}
	cash, lots := fold(account, eff)
	out := Holdings{Account: acc, AsOf: asOf, Cash: cash, Positions: []Position{}}
	for _, p := range pos {
		if p.InstrumentID == nil {
			continue
		}
		inst, err := q.GetInstrument(ctx, coredb.GetInstrumentParams{OwnerID: owner, ID: *p.InstrumentID})
		if err != nil {
			return Holdings{}, fmt.Errorf("charger l'instrument: %w", err)
		}
		position := Position{Symbol: p.Symbol, Name: inst.Name, Quantity: p.Quantity}
		if l, ok := lots[*p.InstrumentID]; ok && l.known && l.qty.IsPositive() {
			avg := l.cost.DivRound(l.qty, 6)
			position.AverageCost = &avg
		}
		out.Positions = append(out.Positions, position)
	}
	return out, nil
}

func val(d decimal.NullDecimal) decimal.Decimal {
	if d.Valid {
		return d.Decimal
	}
	return decimal.Zero
}

// fold applique les écritures en vigueur dans l'ordre : encaisse et lots.
func fold(account uuid.UUID, eff []coredb.CoreTransaction) (decimal.Decimal, map[uuid.UUID]*lot) {
	cash := decimal.Zero
	lots := map[uuid.UUID]*lot{}
	get := func(id uuid.UUID) *lot {
		l, ok := lots[id]
		if !ok {
			l = &lot{known: true}
			lots[id] = l
		}
		return l
	}
	for _, t := range eff {
		incoming := t.AccountID != account // le compte est la contrepartie d'un transfert
		if incoming {
			if t.Type == "transfer" {
				if t.Amount.Valid {
					cash = cash.Add(t.Amount.Decimal)
				}
				if t.InstrumentID != nil && t.Quantity.Valid {
					l := get(*t.InstrumentID)
					l.qty = l.qty.Add(t.Quantity.Decimal)
					l.known = false
				}
			}
			continue
		}
		gross := val(t.Quantity).Mul(val(t.Price))
		fee := val(t.Fee)
		switch t.Type {
		case "contribution":
			cash = cash.Add(val(t.Amount))
		case "withdrawal":
			cash = cash.Sub(val(t.Amount))
		case "fee":
			cash = cash.Sub(val(t.Amount))
		case "dividend":
			cash = cash.Add(val(t.Amount))
		case "buy":
			cash = cash.Sub(gross).Sub(fee)
			l := get(*t.InstrumentID)
			l.qty = l.qty.Add(t.Quantity.Decimal)
			l.cost = l.cost.Add(gross).Add(fee)
		case "drip": // réinvestissement : aucun effet sur l'encaisse
			l := get(*t.InstrumentID)
			l.qty = l.qty.Add(t.Quantity.Decimal)
			l.cost = l.cost.Add(gross)
		case "sell":
			cash = cash.Add(gross).Sub(fee)
			reduce(get(*t.InstrumentID), t.Quantity.Decimal)
		case "split":
			l := get(*t.InstrumentID)
			l.qty = l.qty.Mul(t.SplitRatio.Decimal)
		case "transfer":
			cash = cash.Sub(val(t.Amount)).Sub(fee)
			if t.InstrumentID != nil && t.Quantity.Valid {
				reduce(get(*t.InstrumentID), t.Quantity.Decimal)
			}
		}
	}
	return cash, lots
}

// reduce retire des parts au coût moyen courant.
func reduce(l *lot, qty decimal.Decimal) {
	if l.qty.IsPositive() {
		avg := l.cost.DivRound(l.qty, 12)
		l.cost = l.cost.Sub(avg.Mul(qty))
	}
	l.qty = l.qty.Sub(qty)
	if !l.qty.IsPositive() {
		l.qty, l.cost = decimal.Zero, decimal.Zero
	}
}
