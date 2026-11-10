package engine

import (
	"github.com/shopspring/decimal"
)

// Holding est une position valorisée : quantité, prix, répartition complète
// (CompleteAllocation, D340) et RFG s'il est connu. Plusieurs lignes d'un même
// symbole (comptes différents) s'additionnent naturellement.
type Holding struct {
	Symbol     string
	Quantity   decimal.Decimal
	Price      decimal.Decimal
	Allocation Allocation
	MER        *float64
}

// HoldingsExposure est l'exposition d'un ensemble de positions.
type HoldingsExposure struct {
	// Empty : aucune valeur à exposer.
	Empty bool
	// Value est la valeur de marché des positions (quantité × prix).
	Value    decimal.Decimal
	Exposure Allocation
	// MER est le RFG pondéré sur les positions qui ont un RFG ; HasMER est faux
	// si aucune n'en a.
	MER    float64
	HasMER bool
}

// ExposureOfHoldings valorise les positions (quantité × prix) puis calcule
// l'exposition par transparence et le RFG pondéré (voir Exposure).
func ExposureOfHoldings(hs []Holding) (HoldingsExposure, error) {
	all := make([]Fund, 0, len(hs))
	withMER := make([]Fund, 0, len(hs))
	for _, h := range hs {
		f := Fund{ID: h.Symbol, Weight: h.Quantity.Mul(h.Price), Allocation: h.Allocation}
		all = append(all, f)
		if h.MER != nil {
			f.MER = *h.MER
			withMER = append(withMER, f)
		}
	}
	res, err := Exposure(all)
	if err != nil {
		return HoldingsExposure{}, err
	}
	if res.Empty {
		return HoldingsExposure{Empty: true}, nil
	}
	out := HoldingsExposure{Value: res.Total, Exposure: res.Exposure}
	if m, err := Exposure(withMER); err != nil {
		return HoldingsExposure{}, err
	} else if !m.Empty {
		out.MER, out.HasMER = m.MER, true
	}
	return out, nil
}
