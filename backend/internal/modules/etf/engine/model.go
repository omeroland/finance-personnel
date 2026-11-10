package engine

import (
	"math"
)

// ModelTolerance est l'écart admis par l'API sur le total d'un modèle (contrat
// ModelWrite : 1e-6). La base tolère 1e-4 (D324) ; l'API est plus stricte et
// ne corrige jamais en silence.
const ModelTolerance = 1e-6

// CheckTargets vérifie que chaque cible et l'encaisse sont dans [0, 1] et que
// leur total vaut 1 à ModelTolerance près. Sinon : *TargetsError.
func CheckTargets(targets Allocation, cash float64) error {
	p := targets.parts()
	return checkTotal(p[:], cash, "les cibles par classe et l'encaisse ne totalisent pas 100 %")
}

// CheckWeights vérifie que chaque poids de fonds et l'encaisse sont dans [0, 1]
// et que leur total vaut 1 à ModelTolerance près. Sinon : *TargetsError.
func CheckWeights(weights []float64, cash float64) error {
	return checkTotal(weights, cash, "les poids des fonds et l'encaisse ne totalisent pas 100 %")
}

func checkTotal(parts []float64, cash float64, reason string) error {
	if !validPart(cash) {
		return &TargetsError{Sum: math.NaN(), Reason: "encaisse hors de [0, 1]"}
	}
	sum := cash
	for _, p := range parts {
		if !validPart(p) {
			return &TargetsError{Sum: math.NaN(), Reason: "valeur hors de [0, 1]"}
		}
		sum += p
	}
	if math.Abs(sum-1) > ModelTolerance {
		return &TargetsError{Sum: sum, Reason: reason}
	}
	return nil
}

// Deviation renvoie réel − cible pour chaque classe (en points de fraction).
func Deviation(actual, target Allocation) Allocation {
	a, t := actual.parts(), target.parts()
	var d [6]float64
	for i := range d {
		d[i] = a[i] - t[i]
	}
	return fromParts(d)
}
