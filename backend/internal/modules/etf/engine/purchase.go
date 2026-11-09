package engine

import (
	"errors"
	"fmt"
	"math"
	"sort"

	"github.com/shopspring/decimal"
)

// ErrPrice : prix manquant, nul ou négatif (le contrat renvoie 422).
var ErrPrice = errors.New("prix invalide")

// ErrAmount : montant à investir ou montant minimum négatif.
var ErrAmount = errors.New("montant invalide")

// ErrFund : identifiant de fonds vide ou en double.
var ErrFund = errors.New("fonds invalide")

// PriceError décrit un prix rejeté.
type PriceError struct {
	Fund   string
	Reason string
}

func (e *PriceError) Error() string { return fmt.Sprintf("fonds %q: %s", e.Fund, e.Reason) }

// Unwrap permet errors.Is(err, ErrPrice).
func (e *PriceError) Unwrap() error { return ErrPrice }

// PurchaseFund est un fonds retenu avec son poids cible (fraction) et son prix.
type PurchaseFund struct {
	ID     string
	Weight float64
	Price  decimal.Decimal
}

// PurchaseInput regroupe les entrées du plan d'achat initial.
// Les poids des fonds plus CashWeight totalisent 1 (à Tolerance près).
type PurchaseInput struct {
	Amount     decimal.Decimal // montant à investir (>= 0)
	Funds      []PurchaseFund
	CashWeight float64         // cible d'encaisse (fraction)
	MinOrder   decimal.Decimal // montant minimum d'un ordre ; 0 = aucun
}

// Order est un ordre d'achat en parts entières.
type Order struct {
	Fund   string
	Shares int64
	Price  decimal.Decimal
	Amount decimal.Decimal // Shares × Price
}

// PurchasePlan est le plan d'achat. Somme des ordres + Cash = Amount.
type PurchasePlan struct {
	Orders   []Order // par identifiant ; fonds sans part exclus
	Cash     decimal.Decimal
	Invested decimal.Decimal
	// Weights : poids réels obtenus par fonds (fraction du montant) ;
	// CashWeight pour l'encaisse. Tout à 0 si le montant est nul.
	Weights    map[string]float64
	CashWeight float64
}

// PlanPurchase calcule le plan d'achat initial : parts = plancher(montant ×
// poids ÷ prix) ; le reliquat va, une part à la fois, au fonds le plus
// sous-pondéré (départage par identifiant) tant qu'une part est payable ; les
// ordres sous MinOrder sont ignorés ; le reste est en encaisse (D360 à D363).
func PlanPurchase(in PurchaseInput) (PurchasePlan, error) {
	if in.Amount.IsNegative() {
		return PurchasePlan{}, fmt.Errorf("%w: montant négatif %s", ErrAmount, in.Amount)
	}
	if in.MinOrder.IsNegative() {
		return PurchasePlan{}, fmt.Errorf("%w: minimum négatif %s", ErrAmount, in.MinOrder)
	}
	if !validPart(in.CashWeight) {
		return PurchasePlan{}, &TargetsError{Sum: math.NaN(), Reason: "cible d'encaisse hors de [0, 1]"}
	}
	funds := append([]PurchaseFund(nil), in.Funds...)
	sort.Slice(funds, func(i, j int) bool { return funds[i].ID < funds[j].ID })
	total := in.CashWeight
	for i, f := range funds {
		if f.ID == "" || (i > 0 && funds[i-1].ID == f.ID) {
			return PurchasePlan{}, fmt.Errorf("%w: %q", ErrFund, f.ID)
		}
		if !f.Price.IsPositive() {
			return PurchasePlan{}, &PriceError{Fund: f.ID, Reason: "prix manquant ou non positif"}
		}
		if !validPart(f.Weight) {
			return PurchasePlan{}, fmt.Errorf("%w: fonds %q, poids %v", ErrWeight, f.ID, f.Weight)
		}
		total += f.Weight
	}
	if math.Abs(total-1) > Tolerance {
		return PurchasePlan{}, &TargetsError{Sum: total, Reason: "poids des fonds et encaisse"}
	}

	plan := PurchasePlan{Weights: make(map[string]float64, len(funds)), Cash: in.Amount}
	for _, f := range funds {
		plan.Weights[f.ID] = 0
	}
	if !in.Amount.IsPositive() {
		return plan, nil
	}

	shares := make([]int64, len(funds))
	spent := make([]decimal.Decimal, len(funds))
	targets := make([]decimal.Decimal, len(funds)) // montant cible par fonds
	remaining := in.Amount
	for i, f := range funds {
		targets[i] = in.Amount.Mul(decimal.NewFromFloat(f.Weight))
		if f.Weight == 0 {
			continue
		}
		q, _ := targets[i].QuoRem(f.Price, 0) // quotient entier tronqué, exact
		n := q.IntPart()
		cost := f.Price.Mul(decimal.NewFromInt(n))
		for n > 0 && cost.GreaterThan(remaining) { // poids totalisant un peu plus de 1
			n--
			cost = f.Price.Mul(decimal.NewFromInt(n))
		}
		shares[i], spent[i] = n, cost
		remaining = remaining.Sub(cost)
	}
	// Reliquat : une part à la fois au fonds le plus sous-pondéré (écart > 0).
	for {
		best := -1
		var bestGap decimal.Decimal
		for i, f := range funds {
			if f.Price.GreaterThan(remaining) {
				continue
			}
			gap := targets[i].Sub(spent[i])
			if !gap.IsPositive() {
				continue
			}
			if best < 0 || gap.GreaterThan(bestGap) { // égalité : plus petit identifiant
				best, bestGap = i, gap
			}
		}
		if best < 0 {
			break
		}
		shares[best]++
		spent[best] = spent[best].Add(funds[best].Price)
		remaining = remaining.Sub(funds[best].Price)
	}
	// Ordres sous le minimum : ignorés, montant rendu à l'encaisse.
	for i, f := range funds {
		if shares[i] == 0 {
			continue
		}
		if in.MinOrder.IsPositive() && spent[i].LessThan(in.MinOrder) {
			remaining = remaining.Add(spent[i])
			continue
		}
		plan.Orders = append(plan.Orders, Order{Fund: f.ID, Shares: shares[i], Price: f.Price, Amount: spent[i]})
		plan.Weights[f.ID] = spent[i].Div(in.Amount).InexactFloat64()
	}
	plan.Cash = remaining
	plan.Invested = in.Amount.Sub(remaining)
	plan.CashWeight = remaining.Div(in.Amount).InexactFloat64()
	return plan, nil
}
