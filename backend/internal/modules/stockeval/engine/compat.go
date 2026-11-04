package engine

import (
	"errors"
	"fmt"
	"strings"

	"github.com/shopspring/decimal"
)

// Mode compat : reproduit la feuille Simply Investing au chiffre près, horizon de 20 ans seulement.
// Aucune fonction du mode corrigé n'est réutilisée quand le comportement diffère (D170) : tout ce
// qui suit est séparé et préfixé « compat ». Les formules visées sont celles des onglets
// « EPS Growth Calculation » et « Dividend Growth Calculation » (colonnes B à U, 20 cellules).

// ErrSheetDivZero reproduit le #DIV/0! de la feuille : une valeur précédente nulle casse toute la ligne.
var ErrSheetDivZero = errors.New("#DIV/0! de la feuille: valeur précédente nulle")

// compatCellCount est le nombre de cellules d'une ligne de la feuille (colonnes B à U).
const compatCellCount = 20

// CompatCells est une ligne de la feuille : 20 cellules, de la plus ancienne (B) à la plus récente (U).
// Un nil est une cellule vide.
type CompatCells []*decimal.Decimal

// CompatCellsFromSeries place une série annuelle dans les 20 cellules, la dernière année en U.
// Une année absente donne une cellule vide ; les années plus anciennes que 20 ans sont ignorées.
func CompatCellsFromSeries(s Series) CompatCells {
	cells := make(CompatCells, compatCellCount)
	n := normalize(s)
	if len(n) == 0 {
		return cells
	}
	last := n[len(n)-1].Year
	for _, p := range n {
		if i := compatCellCount - 1 - (last - p.Year); i >= 0 {
			v := p.Value
			cells[i] = &v
		}
	}
	return cells
}

// CompatAverageYield reproduit l'onglet « Average Dividend Yield » : moyenne simple des rendements
// annuels présents dans les 20 cellules (AVERAGE ignore les vides), sans exiger 20 ans. Nil si aucune.
func CompatAverageYield(s Series) *float64 {
	sum, n := decimal.Zero, 0
	for _, c := range CompatCellsFromSeries(s) {
		if c != nil {
			sum = sum.Add(*c)
			n++
		}
	}
	if n == 0 {
		return nil
	}
	f, err := toFloat(sum.Div(decimal.NewFromInt(int64(n))))
	if err != nil {
		return nil
	}
	return &f
}

// CompatGrowth est le résultat d'un onglet de croissance (colonnes V et W de la ligne des taux).
type CompatGrowth struct {
	Blank   bool    // la feuille affiche "" (dernière cellule vide)
	Average float64 // colonne V
	Count   int     // colonne W
}

// compatRate est une cellule de taux : vide ("") ou nombre.
type compatRate struct {
	blank bool
	v     float64
}

// compatRates calcule C7:U7 = IF(B6="","",(C6-B6)/ABS(B6)), la cellule vide valant 0.
func compatRates(cells CompatCells) ([]compatRate, error) {
	if len(cells) != compatCellCount {
		return nil, fmt.Errorf("feuille: %d cellules, attendu %d", len(cells), compatCellCount)
	}
	rs := make([]compatRate, 0, compatCellCount-1)
	for i := 1; i < compatCellCount; i++ {
		prev := cells[i-1]
		if prev == nil {
			rs = append(rs, compatRate{blank: true})
			continue
		}
		if prev.IsZero() {
			return nil, ErrSheetDivZero
		}
		cur := decimal.Zero
		if cells[i] != nil {
			cur = *cells[i]
		}
		f, err := toFloat(cur.Sub(*prev).Div(prev.Abs()))
		if err != nil {
			return nil, fmt.Errorf("taux de la colonne %d: %w", i+2, err)
		}
		rs = append(rs, compatRate{v: f})
	}
	return rs, nil
}

// compatRatesOrBlank applique d'abord IF(U6="","",...) : une dernière cellule vide donne "" même si
// un taux de la ligne serait en erreur.
func compatRatesOrBlank(cells CompatCells) (rs []compatRate, blank bool, err error) {
	if len(cells) != compatCellCount {
		return nil, false, fmt.Errorf("feuille: %d cellules, attendu %d", len(cells), compatCellCount)
	}
	if cells[compatCellCount-1] == nil {
		return nil, true, nil
	}
	rs, err = compatRates(cells)
	return rs, false, err
}

// compatAverage calcule (SUM - MIN - MAX) / diviseur ; SUM, MIN et MAX ignorent les cellules vides
// et valent 0 sans nombre.
func compatAverage(rs []compatRate, divisor int) float64 {
	var sum, lo, hi float64
	n := 0
	for _, r := range rs {
		if r.blank {
			continue
		}
		if n == 0 || r.v < lo {
			lo = r.v
		}
		if n == 0 || r.v > hi {
			hi = r.v
		}
		sum += r.v
		n++
	}
	return (sum - lo - hi) / float64(divisor)
}

// CompatEPSGrowth reproduit l'onglet « EPS Growth Calculation » : moyenne divisée par divisor
// (18 dans la feuille) et nombre de hausses sur les transitions 2 à 19 (la première est ignorée).
func CompatEPSGrowth(cells CompatCells, divisor int) (CompatGrowth, error) {
	rs, blank, err := compatRatesOrBlank(cells)
	if err != nil || blank {
		return CompatGrowth{Blank: blank}, err
	}
	g := CompatGrowth{Average: compatAverage(rs, divisor)}
	for _, r := range rs[1:] {
		if !r.blank && r.v > 0 {
			g.Count++
		}
	}
	return g, nil
}

// CompatDividendGrowth reproduit l'onglet « Dividend Growth Calculation » : même moyenne, mais le
// compte est celui des hausses consécutives jusqu'à la dernière année. Une cellule de taux vide
// vaut « hausse » (dans Calc, "" > 0 % est vrai).
func CompatDividendGrowth(cells CompatCells, divisor int) (CompatGrowth, error) {
	rs, blank, err := compatRatesOrBlank(cells)
	if err != nil || blank {
		return CompatGrowth{Blank: blank}, err
	}
	g := CompatGrowth{Average: compatAverage(rs, divisor)}
	run := true // le drapeau de la colonne suivante est non nul
	for i := len(rs) - 1; i >= 0; i-- {
		run = run && (rs[i].blank || rs[i].v > 0)
		if run {
			g.Count++
		}
	}
	return g, nil
}

// CompatMetrics regroupe les valeurs que la feuille compare aux seuils. Un nil est une cellule vide.
// Les ratios sont ceux saisis dans la feuille, utilisés tels quels (fractions, sauf P/E et P/B).
type CompatMetrics struct {
	EPSGrowth      *float64
	EPSRises       *int
	DPSGrowth      *float64
	PayoutRatio    *float64
	DebtToEquity   *float64
	DividendChange *float64
	Buyback        string // oui/non saisi, casse ignorée
	PE             *float64
	CurrentYield   *float64
	AverageYield   *float64
	PB             *float64
	Warnings       []string
}

// CompatInputs regroupe les entrées du mode compat.
type CompatInputs struct {
	EPS CompatCells // BPA, 20 cellules (vide = tout vide)
	DPS CompatCells // dividende par action, 20 cellules (vide = tout vide)
	// Valeurs saisies dans la feuille.
	PayoutRatio    *float64
	DebtToEquity   *float64
	DividendChange *float64
	Buyback        string
	PE             *float64
	CurrentYield   *float64
	AverageYield   *float64
	PB             *float64
}

// CompatCompute calcule les trois valeurs issues des onglets de croissance et reprend le reste tel quel.
// Une erreur de la feuille (ErrSheetDivZero) est retournée, enveloppée avec l'onglet concerné.
func CompatCompute(in CompatInputs, r *Rules) (CompatMetrics, error) {
	if r == nil {
		return CompatMetrics{}, errors.New("compat: référentiel absent")
	}
	m := CompatMetrics{
		PayoutRatio: in.PayoutRatio, DebtToEquity: in.DebtToEquity, DividendChange: in.DividendChange,
		Buyback: in.Buyback, PE: in.PE, CurrentYield: in.CurrentYield, AverageYield: in.AverageYield, PB: in.PB,
	}
	eps, dps := in.EPS, in.DPS
	if len(eps) == 0 {
		eps = make(CompatCells, compatCellCount)
	}
	if len(dps) == 0 {
		dps = make(CompatCells, compatCellCount)
	}
	e, err := CompatEPSGrowth(eps, r.Compat.Divisor)
	if err != nil {
		return CompatMetrics{}, fmt.Errorf("compat: onglet BPA: %w", err)
	}
	if !e.Blank {
		m.EPSGrowth, m.EPSRises = &e.Average, &e.Count
	}
	d, err := CompatDividendGrowth(dps, r.Compat.Divisor)
	if err != nil {
		return CompatMetrics{}, fmt.Errorf("compat: onglet dividendes: %w", err)
	}
	if !d.Blank {
		m.DPSGrowth = &d.Average
	}
	return m, nil
}

// CompatEvaluate calcule puis pointe, comme la feuille : un seul horizon (compat.rule_5b_horizon).
func CompatEvaluate(in CompatInputs, r *Rules, ctx Context) (Evaluation, error) {
	m, err := CompatCompute(in, r)
	if err != nil {
		return Evaluation{}, err
	}
	return CompatScore(m, r, ctx)
}

// CompatScore pointe les valeurs contre les seuils de la feuille. Il n'y a pas de seuil assoupli en
// compat : le profil assoupli reprend le strict. Une donnée absente réussit, avec avertissement.
func CompatScore(m CompatMetrics, r *Rules, ctx Context) (Evaluation, error) {
	if r == nil {
		return Evaluation{}, errors.New("compat: référentiel absent")
	}
	h := r.Compat.Rule5bHorizon
	hr := HorizonResult{Horizon: h, Indicative: h < r.Parameters.MinVerdictHorizon}
	warns := append([]string(nil), m.Warnings...)
	allEmpty := true
	for i, id := range CriterionIDs {
		cr, absent := compatCriterion(id, m, r)
		if absent {
			warns = append(warns, fmt.Sprintf("règle %s: donnée absente, réussite comme dans la feuille", id))
		} else {
			allEmpty = false
		}
		hr.Criteria = append(hr.Criteria, cr)
		quality := i < 7
		tally(&hr.Strict, cr.Status, quality, false)
		tally(&hr.Relaxed, cr.Status, quality, true)
	}
	if allEmpty {
		warns = append(warns, "ligne entièrement vide: 0 échec, comme dans la feuille")
	}
	hr.Warnings = warns
	ev := Evaluation{
		RulesVersion: r.Version, RulesHash: r.Hash, Gate: ctx.Gate, Financial: ctx.Financial,
		Horizons: []HorizonResult{hr},
	}
	ev.Verdict, ev.VerdictHorizon = verdict(ev.Horizons, ctx.Gate)
	return ev, nil
}

// compatCriterion pointe un critère ; absent vaut vrai si la donnée manque (le critère réussit alors).
func compatCriterion(id string, m CompatMetrics, r *Rules) (cr CriterionResult, absent bool) {
	c := r.Criteria[id]
	cr = CriterionResult{ID: id, Status: StatusPassed, Strict: compatView(id, c.Strict, r)}
	fail := false
	set := func(v *float64, failed func(float64) bool) {
		if v == nil {
			absent = true
			return
		}
		x := *v
		cr.Value = &x
		fail = failed(x)
	}
	switch id {
	case "5a":
		set(m.EPSGrowth, func(x float64) bool { return x < *c.Strict.Ge })
	case "5b":
		var v *float64
		if m.EPSRises != nil {
			f := float64(*m.EPSRises)
			v = &f
		}
		set(v, func(x float64) bool { return x < float64(r.Compat.Rule5bMinRises) })
	case "6":
		set(m.DPSGrowth, func(x float64) bool { return x < *c.Strict.Ge })
	case "7":
		set(m.PayoutRatio, func(x float64) bool { return x > *c.Strict.Le })
	case "8":
		set(m.DebtToEquity, func(x float64) bool { return x > *c.Strict.Le })
	case "9":
		set(m.DividendChange, func(x float64) bool {
			if r.Compat.Rule9Operator == ">" {
				return !(x > 0)
			}
			return x < 0
		})
	case "10":
		switch strings.ToLower(strings.TrimSpace(m.Buyback)) {
		case "":
			absent = true
		case "no":
			fail = true
		}
	case "11a":
		set(m.PE, func(x float64) bool { return x > *c.Strict.Le })
	case "11b":
		if m.CurrentYield == nil || m.AverageYield == nil {
			absent = true
			break
		}
		cur, avg := *m.CurrentYield, *m.AverageYield
		cr.Value, cr.Compare = &cur, &avg
		fail = cur < avg // la feuille: L < M échoue, l'égalité réussit
	case "11c":
		set(m.PB, func(x float64) bool { return x > *c.Strict.Le })
	}
	if absent {
		cr.Value, cr.Compare = nil, nil
		return cr, true
	}
	if fail {
		cr.Status = StatusFailed
	}
	return cr, false
}

// compatView est le seuil affiché en compat : sans borne basse (7, 8, 11a, 11c), sans seuil assoupli.
func compatView(id string, t Threshold, r *Rules) ThresholdView {
	v := view(t, r.Compat.Rule5bHorizon)
	switch id {
	case "5b":
		n := r.Compat.Rule5bMinRises
		v.MinRises = &n
	case "7", "8", "11a", "11c":
		v.Gt, v.Ge = nil, nil
	case "9":
		zero := 0.0
		v.Gt, v.Ge = nil, nil
		if r.Compat.Rule9Operator == ">" {
			v.Gt = &zero
		} else {
			v.Ge = &zero
		}
	}
	return v
}
