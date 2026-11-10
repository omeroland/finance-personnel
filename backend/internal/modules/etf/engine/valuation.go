package engine

// Valeur d'un compte avec transactions dans le temps (module 2) : à partir des
// écritures en vigueur du registre, construit les points (valeur avant flux,
// flux net) que TWR attend. Aucun prix de marché n'est stocké en phase 2
// (D402) : un titre est valorisé à son dernier prix d'écriture connu du compte
// (achat, vente ou réinvestissement), D411.

import (
	"fmt"
	"sort"
	"time"

	"github.com/shopspring/decimal"
)

// Types d'écriture du registre (D310) lus par la valorisation.
const (
	EntryBuy          = "buy"
	EntrySell         = "sell"
	EntryDividend     = "dividend"
	EntryDRIP         = "drip"
	EntryContribution = "contribution"
	EntryWithdrawal   = "withdrawal"
	EntryFee          = "fee"
	EntryTransfer     = "transfer"
	EntrySplit        = "split"
)

// Entry est une écriture en vigueur (ni corrigée ni inverse) qui touche un
// compte. Les champs absents valent zéro. Incoming vaut true quand le compte
// est la contrepartie d'un transfert (l'argent et les titres y entrent).
type Entry struct {
	Date       time.Time
	Type       string
	Symbol     string
	Quantity   decimal.Decimal
	Price      decimal.Decimal
	Amount     decimal.Decimal
	Fee        decimal.Decimal
	SplitRatio decimal.Decimal
	Incoming   bool
}

// LedgerAccount regroupe les écritures d'un compte, de la plus ancienne à la plus récente.
type LedgerAccount struct {
	ID      string
	Entries []Entry
}

type ledgerState struct {
	cash   decimal.Decimal
	qty    map[string]decimal.Decimal
	prices map[string]decimal.Decimal
}

func newLedgerState() *ledgerState {
	return &ledgerState{qty: map[string]decimal.Decimal{}, prices: map[string]decimal.Decimal{}}
}

// apply applique une écriture et rend le flux externe net qu'elle représente
// (positif : argent ou titres qui entrent ; négatif : qui sortent).
func (s *ledgerState) apply(e Entry) decimal.Decimal {
	flow := decimal.Zero
	gross := e.Quantity.Mul(e.Price)
	if e.Incoming {
		if e.Type != EntryTransfer {
			return flow
		}
		s.cash = s.cash.Add(e.Amount)
		flow = e.Amount
		if e.Symbol != "" && e.Quantity.IsPositive() {
			s.qty[e.Symbol] = s.qty[e.Symbol].Add(e.Quantity)
			flow = flow.Add(e.Quantity.Mul(s.prices[e.Symbol]))
		}
		return flow
	}
	switch e.Type {
	case EntryContribution:
		s.cash = s.cash.Add(e.Amount)
		flow = e.Amount
	case EntryWithdrawal:
		s.cash = s.cash.Sub(e.Amount)
		flow = e.Amount.Neg()
	case EntryFee:
		s.cash = s.cash.Sub(e.Amount)
	case EntryDividend:
		s.cash = s.cash.Add(e.Amount)
	case EntryBuy:
		s.cash = s.cash.Sub(gross).Sub(e.Fee)
		s.qty[e.Symbol] = s.qty[e.Symbol].Add(e.Quantity)
		s.prices[e.Symbol] = e.Price
	case EntryDRIP:
		s.qty[e.Symbol] = s.qty[e.Symbol].Add(e.Quantity)
		s.prices[e.Symbol] = e.Price
	case EntrySell:
		s.cash = s.cash.Add(gross).Sub(e.Fee)
		s.qty[e.Symbol] = s.qty[e.Symbol].Sub(e.Quantity)
		s.prices[e.Symbol] = e.Price
	case EntrySplit:
		if e.SplitRatio.IsPositive() {
			s.qty[e.Symbol] = s.qty[e.Symbol].Mul(e.SplitRatio)
			if p, ok := s.prices[e.Symbol]; ok { // la valeur du titre ne change pas : le prix se divise
				s.prices[e.Symbol] = p.Div(e.SplitRatio)
			}
		}
	case EntryTransfer:
		s.cash = s.cash.Sub(e.Amount).Sub(e.Fee)
		flow = e.Amount.Neg()
		if e.Symbol != "" && e.Quantity.IsPositive() {
			s.qty[e.Symbol] = s.qty[e.Symbol].Sub(e.Quantity)
			flow = flow.Sub(e.Quantity.Mul(s.prices[e.Symbol]))
		}
	}
	return flow
}

func (s *ledgerState) value() decimal.Decimal {
	v := s.cash
	for sym, q := range s.qty {
		v = v.Add(q.Mul(s.prices[sym]))
	}
	return v
}

func day(t time.Time) time.Time {
	y, m, d := t.UTC().Date()
	return time.Date(y, m, d, 0, 0, 0, 0, time.UTC)
}

// FirstEntryDate rend la date de la plus ancienne écriture des comptes, ou
// false s'il n'y en a aucune.
func FirstEntryDate(accounts []LedgerAccount) (time.Time, bool) {
	var first time.Time
	found := false
	for _, a := range accounts {
		for _, e := range a.Entries {
			if d := day(e.Date); !found || d.Before(first) {
				first, found = d, true
			}
		}
	}
	return first, found
}

// AccountTimelines construit, pour chaque compte, la ligne du temps de TWR sur
// [from, to] : un point à from, à to, à la fin de chaque mois entre les deux et
// à chaque date où l'un des comptes a un flux externe. Tous les comptes
// partagent les mêmes dates, ce qui permet MergeValuations (D337). La valeur
// d'un point est celle d'avant le flux de la date (elle comprend les écritures
// du jour) ; les écritures antérieures à from ne comptent que pour l'état de
// départ. Erreur : ErrNoPeriod si to précède from.
func AccountTimelines(accounts []LedgerAccount, from, to time.Time) (map[string][]Valuation, error) {
	from, to = day(from), day(to)
	if to.Before(from) {
		return nil, fmt.Errorf("fin avant le début: %w", ErrNoPeriod)
	}
	sorted := make([][]Entry, len(accounts))
	dates := map[time.Time]bool{from: true, to: true}
	for i, a := range accounts {
		es := append([]Entry(nil), a.Entries...)
		sort.SliceStable(es, func(x, y int) bool { return day(es[x].Date).Before(day(es[y].Date)) })
		sorted[i] = es
		st := newLedgerState()
		for _, e := range es {
			f := st.apply(e)
			if d := day(e.Date); !f.IsZero() && !d.Before(from) && !d.After(to) {
				dates[d] = true
			}
		}
	}
	year, month, _ := from.Date()
	for {
		end := time.Date(year, month+1, 0, 0, 0, 0, 0, time.UTC) // dernier jour du mois
		if end.After(to) {
			break
		}
		if end.After(from) {
			dates[end] = true
		}
		if month == time.December {
			year, month = year+1, time.January
		} else {
			month++
		}
	}
	checkpoints := make([]time.Time, 0, len(dates))
	for d := range dates {
		checkpoints = append(checkpoints, d)
	}
	sort.Slice(checkpoints, func(i, j int) bool { return checkpoints[i].Before(checkpoints[j]) })

	out := make(map[string][]Valuation, len(accounts))
	for i, a := range accounts {
		st := newLedgerState()
		es := sorted[i]
		next := 0
		points := make([]Valuation, 0, len(checkpoints))
		for _, cp := range checkpoints {
			flow := decimal.Zero
			for next < len(es) && !day(es[next].Date).After(cp) {
				f := st.apply(es[next])
				if !day(es[next].Date).Before(from) {
					flow = flow.Add(f)
				}
				next++
			}
			points = append(points, Valuation{Date: cp, Value: st.value().Sub(flow), Flow: flow})
		}
		out[a.ID] = points
	}
	return out, nil
}
