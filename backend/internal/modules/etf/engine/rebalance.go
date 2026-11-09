package engine

import (
	"errors"
	"fmt"
	"math"
	"sort"

	"github.com/shopspring/decimal"
)

// Bandes par défaut de l'outil (D12) : ±20 % relatifs pour les fonds et les
// classes, cible + 5 points pour l'encaisse.
const (
	DefaultRelativeBand = 0.20
	DefaultCashPoints   = 0.05
)

// bandEps absorbe le bruit des flottants aux bornes d'une bande.
const bandEps = 1e-9

// AccountType est le type d'un compte (D310).
type AccountType string

// Les types de compte.
const (
	AccountCELI    AccountType = "celi"
	AccountREER    AccountType = "reer"
	AccountCELIAPP AccountType = "celiapp"
	AccountREEE    AccountType = "reee"
	AccountTaxable AccountType = "taxable"
)

// Registered indique un compte enregistré (vente sans impact fiscal).
func (t AccountType) Registered() bool {
	switch t {
	case AccountCELI, AccountREER, AccountCELIAPP, AccountREEE:
		return true
	}
	return false
}

func (t AccountType) valid() bool { return t.Registered() || t == AccountTaxable }

// RebalSide est le sens d'un ordre.
type RebalSide string

// Les sens d'ordre.
const (
	SideBuy  RebalSide = "buy"
	SideSell RebalSide = "sell"
)

// SkipBelowMinimum est le motif d'un ordre écarté parce qu'il est sous le
// montant minimum (D314).
const SkipBelowMinimum = "below_minimum"

// Erreurs sentinelles du rééquilibrage.
var (
	// ErrRebalanceInput : entrée incohérente (doublon, fonds inconnu, quantité ou encaisse négative, bande invalide, type de compte inconnu).
	ErrRebalanceInput = errors.New("entrée de rééquilibrage invalide")
)

// Bands donne les bandes de tolérance, en fractions.
type Bands struct {
	Relative   float64 // fonds et classes : cible × (1 ± Relative)
	CashPoints float64 // encaisse : au plus cible + CashPoints
}

// DefaultBands renvoie les bandes de D12.
func DefaultBands() Bands {
	return Bands{Relative: DefaultRelativeBand, CashPoints: DefaultCashPoints}
}

// RebalFund est un fonds du modèle (ou détenu hors modèle avec une cible nulle).
type RebalFund struct {
	ID         string
	Price      decimal.Decimal // > 0
	Target     float64         // fraction du portefeuille (avant normalisation)
	Allocation Allocation      // répartition par classe, somme 1
}

// RebalPosition est une position dans un compte.
type RebalPosition struct {
	FundID string
	Shares decimal.Decimal // >= 0 ; seules les parts entières sont vendables
}

// RebalAccount est un compte avec ses positions, son encaisse et sa cotisation.
type RebalAccount struct {
	ID           string
	Type         AccountType
	Cash         decimal.Decimal // encaisse déjà au compte, >= 0
	Contribution decimal.Decimal // nouvelle cotisation à investir, >= 0
	Positions    []RebalPosition
}

// RebalanceInput regroupe les entrées du rééquilibrage.
type RebalanceInput struct {
	Funds      []RebalFund
	Accounts   []RebalAccount
	CashTarget float64         // encaisse cible, fraction
	Bands      *Bands          // nil = DefaultBands
	MinOrder   decimal.Decimal // montant minimum d'un ordre ; 0 = aucun
	// ClassTargets : cibles par classe (fractions, avec CashTarget, total 1).
	// nil = déduites des cibles des fonds et de leur répartition.
	ClassTargets *Allocation
}

// RebalOrder est un ordre en parts entières dans un compte.
type RebalOrder struct {
	AccountID string
	FundID    string
	Side      RebalSide
	Shares    int64
	Price     decimal.Decimal
	Amount    decimal.Decimal // Shares × Price
}

// RebalSkipped est un ordre écarté avec son motif.
type RebalSkipped struct {
	AccountID string
	FundID    string
	Side      RebalSide
	Shares    int64
	Amount    decimal.Decimal
	Reason    string
}

// BandStatus décrit un fonds, une classe ou l'encaisse hors de sa bande.
type BandStatus struct {
	Kind      string // "fund", "class" ou "cash"
	ID        string // identifiant du fonds, nom de la classe, "" pour l'encaisse
	Weight    float64
	Target    float64
	Low, High float64
	Over      bool // true : au-dessus de la bande ; false : en dessous
}

// FundWeight est le poids d'un fonds dans le portefeuille.
type FundWeight struct {
	FundID string
	Weight float64
	Target float64 // cible normalisée
}

// Weights donne les poids d'un état du portefeuille (cotisations incluses dans le total).
type Weights struct {
	Funds   []FundWeight
	Cash    float64
	Classes Allocation
}

// AccountCash est l'encaisse finale d'un compte.
type AccountCash struct {
	AccountID string
	Cash      decimal.Decimal
}

// RebalancePlan est le résultat du rééquilibrage. Un plan sans ordre est vide.
type RebalancePlan struct {
	Orders      []RebalOrder
	Skipped     []RebalSkipped
	EndCash     decimal.Decimal
	AccountCash []AccountCash
	Before      Weights
	After       Weights
	OutOfBand   []BandStatus // constat de départ
}

type rbAccount struct {
	id   string
	typ  AccountType
	cash decimal.Decimal
	held map[string]decimal.Decimal
}

type rbFund struct {
	id     string
	price  decimal.Decimal
	target float64
	alloc  Allocation
}

type orderKey struct {
	acc, fund string
	side      RebalSide
}

type rebalancer struct {
	funds      []rbFund // trié par ID
	byID       map[string]int
	accts      []*rbAccount // ordre d'achat : par ID
	sellOrder  []*rbAccount // enregistrés d'abord, puis taxable
	total      decimal.Decimal
	cashTarget float64
	bands      Bands
	classT     Allocation
	orders     map[orderKey]int64
	skipped    []RebalSkipped
}

func badInput(format string, a ...any) error {
	return fmt.Errorf("%w: %s", ErrRebalanceInput, fmt.Sprintf(format, a...))
}

func validRatio(v float64) bool { return !math.IsNaN(v) && !math.IsInf(v, 0) && v >= 0 }

// Rebalance construit le plan de rééquilibrage dans l'ordre de l'architecture :
// détecter, acheter avec l'encaisse et les cotisations, vendre si c'est encore
// hors bande (comptes enregistrés d'abord), arrondir en parts entières,
// écarter les ordres sous le minimum. Aucun impact fiscal n'est calculé.
// Sans fonds, ou sans valeur, le plan est vide et sans erreur.
func Rebalance(in RebalanceInput) (RebalancePlan, error) {
	r, err := newRebalancer(in)
	if err != nil {
		return RebalancePlan{}, err
	}
	if r == nil || !r.total.IsPositive() {
		return RebalancePlan{}, nil
	}
	plan := RebalancePlan{Before: r.weights()}
	plan.OutOfBand = r.detect()
	if len(plan.OutOfBand) == 0 {
		plan.After = plan.Before
		plan.EndCash, plan.AccountCash = r.cashSummary()
		return plan, nil
	}
	r.buyPass()
	if r.anyFundOver() {
		if r.sellPass(in.MinOrder) {
			r.buyPass()
		}
	}
	r.dropSmallBuys(in.MinOrder)
	plan.Orders = r.orderList()
	plan.Skipped = r.skipped
	plan.After = r.weights()
	plan.EndCash, plan.AccountCash = r.cashSummary()
	return plan, nil
}

func newRebalancer(in RebalanceInput) (*rebalancer, error) {
	bands := DefaultBands()
	if in.Bands != nil {
		bands = *in.Bands
	}
	if !validRatio(bands.Relative) || bands.Relative > 1 || !validRatio(bands.CashPoints) || bands.CashPoints > 1 {
		return nil, badInput("bandes hors de [0, 1]")
	}
	if !validRatio(in.CashTarget) || in.CashTarget > 1 {
		return nil, &TargetsError{Sum: math.NaN(), Reason: "encaisse hors de [0, 1]"}
	}
	if in.MinOrder.IsNegative() {
		return nil, badInput("montant minimum négatif")
	}
	if len(in.Funds) == 0 {
		return nil, nil
	}
	r := &rebalancer{byID: map[string]int{}, cashTarget: in.CashTarget, bands: bands, orders: map[orderKey]int64{}}

	sum := 0.0
	seenFund := map[string]bool{}
	for _, f := range in.Funds {
		if seenFund[f.ID] {
			return nil, badInput("fonds %q en double", f.ID)
		}
		seenFund[f.ID] = true
		if !f.Price.IsPositive() {
			return nil, &PriceError{Fund: f.ID, Reason: "prix manquant ou non positif"}
		}
		if !validPart(f.Target) {
			return nil, &TargetsError{Sum: math.NaN(), Reason: fmt.Sprintf("cible du fonds %q hors de [0, 1]", f.ID)}
		}
		if err := validateAllocation(f.ID, f.Allocation); err != nil {
			return nil, err
		}
		sum += f.Target
		r.funds = append(r.funds, rbFund{id: f.ID, price: f.Price, target: f.Target, alloc: f.Allocation})
	}
	if math.Abs(sum+in.CashTarget-1) > Tolerance {
		return nil, &TargetsError{Sum: sum + in.CashTarget, Reason: "les cibles et l'encaisse ne totalisent pas 100 %"}
	}
	sort.Slice(r.funds, func(i, j int) bool { return r.funds[i].id < r.funds[j].id })
	for i, f := range r.funds {
		r.byID[f.id] = i
	}
	if sum > 0 {
		k := (1 - in.CashTarget) / sum
		for i := range r.funds {
			r.funds[i].target *= k
		}
	}
	if in.ClassTargets != nil {
		ct, err := NormalizeTargets(*in.ClassTargets, in.CashTarget)
		if err != nil {
			return nil, err
		}
		r.classT = ct
	} else {
		var acc [6]float64
		for _, f := range r.funds {
			p := f.alloc.parts()
			s := f.alloc.Sum()
			for i := range acc {
				acc[i] += f.target * p[i] / s
			}
		}
		r.classT = fromParts(acc)
	}

	seen := map[string]bool{}
	r.total = decimal.Zero
	for _, a := range in.Accounts {
		if seen[a.ID] {
			return nil, badInput("compte %q en double", a.ID)
		}
		seen[a.ID] = true
		if !a.Type.valid() {
			return nil, badInput("compte %q: type %q inconnu", a.ID, a.Type)
		}
		if a.Cash.IsNegative() || a.Contribution.IsNegative() {
			return nil, badInput("compte %q: encaisse ou cotisation négative", a.ID)
		}
		acc := &rbAccount{id: a.ID, typ: a.Type, cash: a.Cash.Add(a.Contribution), held: map[string]decimal.Decimal{}}
		r.total = r.total.Add(acc.cash)
		for _, p := range a.Positions {
			i, ok := r.byID[p.FundID]
			if !ok {
				return nil, badInput("compte %q: fonds %q absent de la liste des fonds", a.ID, p.FundID)
			}
			if p.Shares.IsNegative() {
				return nil, badInput("compte %q: parts négatives pour %q", a.ID, p.FundID)
			}
			acc.held[p.FundID] = acc.held[p.FundID].Add(p.Shares)
			r.total = r.total.Add(p.Shares.Mul(r.funds[i].price))
		}
		r.accts = append(r.accts, acc)
	}
	sort.Slice(r.accts, func(i, j int) bool { return r.accts[i].id < r.accts[j].id })
	r.sellOrder = append([]*rbAccount(nil), r.accts...)
	sort.SliceStable(r.sellOrder, func(i, j int) bool {
		return r.sellOrder[i].typ.Registered() && !r.sellOrder[j].typ.Registered()
	})
	return r, nil
}

func (r *rebalancer) fundValue(f rbFund) decimal.Decimal {
	s := decimal.Zero
	for _, a := range r.accts {
		s = s.Add(a.held[f.id])
	}
	return s.Mul(f.price)
}

func (r *rebalancer) totalCash() decimal.Decimal {
	c := decimal.Zero
	for _, a := range r.accts {
		c = c.Add(a.cash)
	}
	return c
}

func (r *rebalancer) ratio(v decimal.Decimal) float64 {
	x, _ := v.Div(r.total).Float64()
	return x
}

func (r *rebalancer) targetValue(f rbFund) decimal.Decimal {
	return r.total.Mul(decimal.NewFromFloat(f.target))
}

func (r *rebalancer) weights() Weights {
	w := Weights{Cash: r.ratio(r.totalCash())}
	var cls [6]float64
	for _, f := range r.funds {
		fw := r.ratio(r.fundValue(f))
		w.Funds = append(w.Funds, FundWeight{FundID: f.id, Weight: fw, Target: f.target})
		p := f.alloc.parts()
		s := f.alloc.Sum()
		for i := range cls {
			cls[i] += fw * p[i] / s
		}
	}
	w.Classes = fromParts(cls)
	return w
}

func (r *rebalancer) fundBand(f rbFund) (lo, hi float64) {
	return f.target * (1 - r.bands.Relative), f.target * (1 + r.bands.Relative)
}

// detect liste ce qui est hors bande dans l'état courant.
func (r *rebalancer) detect() []BandStatus {
	var out []BandStatus
	w := r.weights()
	for i, f := range r.funds {
		lo, hi := r.fundBand(f)
		x := w.Funds[i].Weight
		if x < lo-bandEps || x > hi+bandEps {
			out = append(out, BandStatus{Kind: "fund", ID: f.id, Weight: x, Target: f.target, Low: lo, High: hi, Over: x > hi})
		}
	}
	for _, c := range Classes {
		t := r.classT.Get(c)
		lo, hi := t*(1-r.bands.Relative), t*(1+r.bands.Relative)
		x := w.Classes.Get(c)
		if x < lo-bandEps || x > hi+bandEps {
			out = append(out, BandStatus{Kind: "class", ID: string(c), Weight: x, Target: t, Low: lo, High: hi, Over: x > hi})
		}
	}
	hi := r.cashTarget + r.bands.CashPoints
	if w.Cash > hi+bandEps {
		out = append(out, BandStatus{Kind: "cash", Weight: w.Cash, Target: r.cashTarget, Low: 0, High: hi, Over: true})
	}
	return out
}

func (r *rebalancer) anyFundOver() bool {
	for _, b := range r.detect() {
		if b.Kind == "fund" && b.Over {
			return true
		}
	}
	return false
}

func (r *rebalancer) buyShares(a *rbAccount, f rbFund, n int64) {
	a.cash = a.cash.Sub(f.price.Mul(decimal.NewFromInt(n)))
	a.held[f.id] = a.held[f.id].Add(decimal.NewFromInt(n))
	r.orders[orderKey{a.id, f.id, SideBuy}] += n
}

// buyPass achète les fonds sous-pondérés avec l'encaisse au-delà de la cible
// d'encaisse : parts entières arrondies vers le bas, puis le reste une part à
// la fois au fonds le plus sous-pondéré. Aucun achat ne fait dépasser la bande haute.
func (r *rebalancer) buyPass() {
	budget := r.totalCash().Sub(r.total.Mul(decimal.NewFromFloat(r.cashTarget)))
	if !budget.IsPositive() {
		return
	}
	deficit := make([]decimal.Decimal, len(r.funds))
	sumDef := decimal.Zero
	for i, f := range r.funds {
		if d := r.targetValue(f).Sub(r.fundValue(f)); d.IsPositive() {
			deficit[i] = d
			sumDef = sumDef.Add(d)
		}
	}
	if !sumDef.IsPositive() {
		return
	}
	spend := decimal.Min(budget, sumDef)
	for i, f := range r.funds {
		if !deficit[i].IsPositive() {
			continue
		}
		want := deficit[i].Mul(spend).Div(sumDef).Div(f.price).Floor().IntPart()
		for _, a := range r.accts {
			if want <= 0 {
				break
			}
			n := min(want, a.cash.Div(f.price).Floor().IntPart(), budget.Div(f.price).Floor().IntPart())
			if n <= 0 {
				continue
			}
			r.buyShares(a, f, n)
			budget = budget.Sub(f.price.Mul(decimal.NewFromInt(n)))
			want -= n
		}
	}
	// Reste : une part à la fois au fonds le plus sous-pondéré.
	for {
		best := -1
		bestRatio := math.Inf(1)
		var bestAcc *rbAccount
		for i, f := range r.funds {
			if f.target <= 0 || f.price.GreaterThan(budget) {
				continue
			}
			val := r.fundValue(f)
			if !r.targetValue(f).Sub(val).IsPositive() {
				continue
			}
			if r.ratio(val.Add(f.price)) > f.target*(1+r.bands.Relative)+bandEps {
				continue
			}
			var acc *rbAccount
			for _, a := range r.accts {
				if a.cash.GreaterThanOrEqual(f.price) {
					acc = a
					break
				}
			}
			if acc == nil {
				continue
			}
			if ratio := r.ratio(val) / f.target; ratio < bestRatio {
				best, bestRatio, bestAcc = i, ratio, acc
			}
		}
		if best < 0 {
			return
		}
		f := r.funds[best]
		r.buyShares(bestAcc, f, 1)
		budget = budget.Sub(f.price)
	}
}

// sellPass vend les fonds au-dessus de leur bande haute jusqu'à leur cible
// (parts entières vers le bas), comptes enregistrés d'abord. Renvoie true si
// au moins un ordre de vente a été retenu.
func (r *rebalancer) sellPass(minOrder decimal.Decimal) bool {
	sold := false
	for _, f := range r.funds {
		_, hi := r.fundBand(f)
		val := r.fundValue(f)
		if r.ratio(val) <= hi+bandEps {
			continue
		}
		left := val.Sub(r.targetValue(f)).Div(f.price).Floor().IntPart()
		for _, a := range r.sellOrder {
			if left <= 0 {
				break
			}
			n := min(left, a.held[f.id].Floor().IntPart())
			if n <= 0 {
				continue
			}
			amount := f.price.Mul(decimal.NewFromInt(n))
			left -= n
			if amount.LessThan(minOrder) {
				r.skipped = append(r.skipped, RebalSkipped{AccountID: a.id, FundID: f.id, Side: SideSell, Shares: n, Amount: amount, Reason: SkipBelowMinimum})
				continue
			}
			a.held[f.id] = a.held[f.id].Sub(decimal.NewFromInt(n))
			a.cash = a.cash.Add(amount)
			r.orders[orderKey{a.id, f.id, SideSell}] += n
			sold = true
		}
	}
	return sold
}

// dropSmallBuys écarte les achats sous le minimum et rend leur montant à l'encaisse.
func (r *rebalancer) dropSmallBuys(minOrder decimal.Decimal) {
	if minOrder.IsPositive() {
		for k, n := range r.orders {
			if k.side != SideBuy {
				continue
			}
			f := r.funds[r.byID[k.fund]]
			amount := f.price.Mul(decimal.NewFromInt(n))
			if !amount.LessThan(minOrder) {
				continue
			}
			for _, a := range r.accts {
				if a.id == k.acc {
					a.cash = a.cash.Add(amount)
					a.held[k.fund] = a.held[k.fund].Sub(decimal.NewFromInt(n))
				}
			}
			delete(r.orders, k)
			r.skipped = append(r.skipped, RebalSkipped{AccountID: k.acc, FundID: k.fund, Side: SideBuy, Shares: n, Amount: amount, Reason: SkipBelowMinimum})
		}
	}
	sort.Slice(r.skipped, func(i, j int) bool {
		a, b := r.skipped[i], r.skipped[j]
		if a.AccountID != b.AccountID {
			return a.AccountID < b.AccountID
		}
		if a.FundID != b.FundID {
			return a.FundID < b.FundID
		}
		return a.Side < b.Side
	})
}

func (r *rebalancer) orderList() []RebalOrder {
	out := make([]RebalOrder, 0, len(r.orders))
	for k, n := range r.orders {
		p := r.funds[r.byID[k.fund]].price
		out = append(out, RebalOrder{AccountID: k.acc, FundID: k.fund, Side: k.side, Shares: n, Price: p, Amount: p.Mul(decimal.NewFromInt(n))})
	}
	sort.Slice(out, func(i, j int) bool {
		a, b := out[i], out[j]
		if a.AccountID != b.AccountID {
			return a.AccountID < b.AccountID
		}
		if a.Side != b.Side {
			return a.Side == SideSell // ventes avant achats
		}
		return a.FundID < b.FundID
	})
	return out
}

func (r *rebalancer) cashSummary() (decimal.Decimal, []AccountCash) {
	ac := make([]AccountCash, 0, len(r.accts))
	for _, a := range r.accts {
		ac = append(ac, AccountCash{AccountID: a.id, Cash: a.cash})
	}
	return r.totalCash(), ac
}
