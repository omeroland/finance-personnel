// Package engine calcule l'exposition par transparence d'un ensemble de FNB
// et le RFG pondéré (module 2). Package pur : aucune entrée-sortie, aucun
// import de platform/ ni de store/.
//
// Conventions : l'argent (poids = valeur de marché) est en decimal ; les
// ratios (parts, RFG, cibles) sont des float64 en fraction (0.70 = 70 %).
package engine

import (
	"errors"
	"fmt"
	"math"

	"github.com/shopspring/decimal"
)

// Tolerance est l'écart admis sur une somme de parts ou de cibles qui doit
// valoir 1 (même valeur que les CHECK SQL, D323 et D324).
const Tolerance = 0.0001

// Class est l'une des six classes d'actifs (D310).
type Class string

// Les six classes d'actifs.
const (
	EquityCA    Class = "equity_ca"
	EquityUS    Class = "equity_us"
	EquityIntl  Class = "equity_intl"
	BondCA      Class = "bond_ca"
	BondOther   Class = "bond_other"
	OtherAssets Class = "other_assets"
)

// Classes liste les six classes dans l'ordre canonique.
var Classes = [6]Class{EquityCA, EquityUS, EquityIntl, BondCA, BondOther, OtherAssets}

// Allocation donne une part (fraction) par classe d'actifs.
type Allocation struct {
	EquityCA    float64
	EquityUS    float64
	EquityIntl  float64
	BondCA      float64
	BondOther   float64
	OtherAssets float64
}

// Get renvoie la part d'une classe (0 si la classe est inconnue).
func (a Allocation) Get(c Class) float64 {
	switch c {
	case EquityCA:
		return a.EquityCA
	case EquityUS:
		return a.EquityUS
	case EquityIntl:
		return a.EquityIntl
	case BondCA:
		return a.BondCA
	case BondOther:
		return a.BondOther
	case OtherAssets:
		return a.OtherAssets
	}
	return 0
}

func (a Allocation) parts() [6]float64 {
	return [6]float64{a.EquityCA, a.EquityUS, a.EquityIntl, a.BondCA, a.BondOther, a.OtherAssets}
}

func fromParts(p [6]float64) Allocation {
	return Allocation{p[0], p[1], p[2], p[3], p[4], p[5]}
}

// Sum renvoie la somme des six parts.
func (a Allocation) Sum() float64 {
	s := 0.0
	for _, p := range a.parts() {
		s += p
	}
	return s
}

// Erreurs sentinelles ; les erreurs détaillées les enveloppent (errors.Is).
var (
	// ErrAllocation : répartition d'un fonds invalide (part hors [0,1], NaN, somme différente de 1).
	ErrAllocation = errors.New("répartition de fonds invalide")
	// ErrWeight : poids négatif.
	ErrWeight = errors.New("poids de fonds invalide")
	// ErrTargets : cibles invalides (part hors [0,1] ou total différent de 100 %).
	ErrTargets = errors.New("cibles invalides")
	// ErrMER : RFG négatif ou non fini.
	ErrMER = errors.New("RFG invalide")
)

// AllocationError décrit une répartition de fonds rejetée.
type AllocationError struct {
	Fund   string  // identifiant du fonds (libre)
	Sum    float64 // somme des six parts (NaN si une part est invalide)
	Reason string
}

func (e *AllocationError) Error() string {
	return fmt.Sprintf("fonds %q: %s (somme %.6f)", e.Fund, e.Reason, e.Sum)
}

// Unwrap permet errors.Is(err, ErrAllocation).
func (e *AllocationError) Unwrap() error { return ErrAllocation }

// TargetsError décrit des cibles rejetées.
type TargetsError struct {
	Sum    float64 // somme des cibles et de l'encaisse (NaN si une valeur est invalide)
	Reason string
}

func (e *TargetsError) Error() string {
	return fmt.Sprintf("%s (total %.6f, attendu 1)", e.Reason, e.Sum)
}

// Unwrap permet errors.Is(err, ErrTargets).
func (e *TargetsError) Unwrap() error { return ErrTargets }

func validPart(p float64) bool { return !math.IsNaN(p) && !math.IsInf(p, 0) && p >= 0 && p <= 1 }

// CompleteAllocation range dans « autres actifs » la part non classée d'un
// fonds partiellement classé : le total revient à 1 et le fonds ne disparaît
// pas du total (D340). Une somme au-dessus de 1 + Tolerance est rejetée.
func CompleteAllocation(a Allocation) (Allocation, error) {
	sum := 0.0
	for _, v := range a.parts() {
		if !validPart(v) {
			return Allocation{}, &AllocationError{Sum: math.NaN(), Reason: "part hors de [0, 1]"}
		}
		sum += v
	}
	if sum > 1+Tolerance {
		return Allocation{}, &AllocationError{Sum: sum, Reason: "somme au-dessus de 1"}
	}
	if rest := 1 - sum; rest > 0 {
		a.OtherAssets += rest
	}
	return a, nil
}

// ValidateAllocation vérifie que les six parts sont dans [0,1] et totalisent 1 à Tolerance près.
func ValidateAllocation(a Allocation) error {
	return validateAllocation("", a)
}

func validateAllocation(fund string, a Allocation) error {
	sum := 0.0
	for _, v := range a.parts() {
		if !validPart(v) {
			return &AllocationError{Fund: fund, Sum: math.NaN(), Reason: "part hors de [0, 1]"}
		}
		sum += v
	}
	if math.Abs(sum-1) > Tolerance {
		return &AllocationError{Fund: fund, Sum: sum, Reason: "la répartition ne totalise pas 1"}
	}
	return nil
}

// Fund est un fonds détenu : son poids (valeur de marché), sa répartition et son RFG.
type Fund struct {
	ID         string // libre, sert aux messages d'erreur
	Weight     decimal.Decimal
	Allocation Allocation
	MER        float64 // fraction (0.0020 = 0,20 %)
}

// Result est l'exposition d'un ensemble de fonds.
type Result struct {
	// Empty vaut true quand il n'y a rien à exposer (aucun fonds ou poids tous nuls) ;
	// les autres champs sont alors à zéro.
	Empty bool
	// Total est la somme des poids.
	Total decimal.Decimal
	// Exposure est E_c par classe ; la somme vaut 1 quand Empty est false.
	Exposure Allocation
	// MER est le RFG pondéré, en fraction.
	MER float64
}

// Exposure calcule E_c = somme_f(w_f × a_f,c) / somme_f(w_f) et le RFG
// pondéré de la même façon. Une entrée vide ou des poids tous nuls donnent un
// résultat vide, sans erreur. Un poids négatif, une répartition qui ne
// totalise pas 1 ou un RFG invalide sont rejetés par une erreur typée.
func Exposure(funds []Fund) (Result, error) {
	total := decimal.Zero
	for _, f := range funds {
		if f.Weight.IsNegative() {
			return Result{}, fmt.Errorf("fonds %q: %w: %s", f.ID, ErrWeight, f.Weight)
		}
		if err := validateAllocation(f.ID, f.Allocation); err != nil {
			return Result{}, err
		}
		if math.IsNaN(f.MER) || math.IsInf(f.MER, 0) || f.MER < 0 {
			return Result{}, fmt.Errorf("fonds %q: %w: %v", f.ID, ErrMER, f.MER)
		}
		total = total.Add(f.Weight)
	}
	if !total.IsPositive() {
		return Result{Empty: true}, nil
	}
	var acc [6]float64
	mer := 0.0
	for _, f := range funds {
		if f.Weight.IsZero() {
			continue
		}
		// w_f / somme(w) en decimal, puis en float64 (ratio).
		share, _ := f.Weight.Div(total).Float64()
		// Renormalise la répartition du fonds (somme tolérée à 1e-4) pour que le total reste 1.
		p := f.Allocation.parts()
		sum := f.Allocation.Sum()
		for i := range acc {
			acc[i] += share * p[i] / sum
		}
		mer += share * f.MER
	}
	return Result{Total: total, Exposure: fromParts(acc), MER: mer}, nil
}

// NormalizeTargets valide des cibles par classe et l'encaisse cible : chaque
// valeur dans [0,1] et total (cibles + encaisse) égal à 1 à Tolerance près.
// Elle renvoie les cibles remises à l'échelle pour que leur somme vaille
// exactement 1 - cash. Sinon : *TargetsError (errors.Is(err, ErrTargets)).
func NormalizeTargets(targets Allocation, cash float64) (Allocation, error) {
	if !validPart(cash) {
		return Allocation{}, &TargetsError{Sum: math.NaN(), Reason: "encaisse hors de [0, 1]"}
	}
	sum := 0.0
	for _, v := range targets.parts() {
		if !validPart(v) {
			return Allocation{}, &TargetsError{Sum: math.NaN(), Reason: "cible hors de [0, 1]"}
		}
		sum += v
	}
	if math.Abs(sum+cash-1) > Tolerance {
		return Allocation{}, &TargetsError{Sum: sum + cash, Reason: "les cibles et l'encaisse ne totalisent pas 100 %"}
	}
	if sum == 0 {
		return targets, nil // cibles toutes nulles et encaisse 100 %
	}
	k := (1 - cash) / sum
	p := targets.parts()
	for i := range p {
		p[i] *= k
	}
	return fromParts(p), nil
}
