package engine

import (
	"errors"
	"fmt"
	"slices"
)

// Status est le statut d'un critère pour un horizon.
type Status string

// Statuts d'un critère.
const (
	StatusPassed      Status = "passed"       // seuil strict atteint
	StatusBorderline  Status = "borderline"   // seuil assoupli atteint seulement
	StatusFailed      Status = "failed"       // seuil assoupli non atteint
	StatusMissingData Status = "missing"      // donnée absente (compte comme un échec)
	StatusNotEnough   Status = "insufficient" // historique insuffisant (compte comme un échec)
	StatusExempt      Status = "exempt"       // exclu du dénominateur (règle 8, D3)
)

// Verdict est le verdict de l'évaluation.
type Verdict string

// Verdicts possibles.
const (
	VerdictExcluded  Verdict = "excluded"
	VerdictCandidate Verdict = "candidate"
	VerdictWatch     Verdict = "watch"
	VerdictRejected  Verdict = "rejected"
)

// Answer est la réponse à l'une des règles qualitatives 1 à 4.
type Answer string

// AnswerYes est la seule réponse qui ouvre la barrière ; toute autre valeur la ferme.
const (
	AnswerYes Answer = "yes"
	AnswerNo  Answer = "no"
)

// Gate regroupe les réponses aux règles qualitatives 1 à 4.
type Gate [4]Answer

// Open indique que les quatre règles sont à « oui ».
func (g Gate) Open() bool {
	for _, a := range g {
		if a != AnswerYes {
			return false
		}
	}
	return true
}

// Context porte les paramètres de l'évaluation propres à l'action.
type Context struct {
	Gate      Gate
	Financial bool // drapeau « financière » : règle 8 exemptée (D3)
}

// ThresholdView est un seuil résolu pour un horizon, pour l'affichage.
type ThresholdView struct {
	Gt        *float64 `json:"gt,omitempty"`
	Ge        *float64 `json:"ge,omitempty"`
	Le        *float64 `json:"le,omitempty"`
	MinRises  *int     `json:"min_rises,omitempty"`
	Direction string   `json:"direction,omitempty"`
}

// CriterionResult est le résultat d'un critère pour un horizon.
type CriterionResult struct {
	ID      string         `json:"id"`
	Status  Status         `json:"status"`
	Value   *float64       `json:"value,omitempty"`   // valeur mesurée (rendement actuel pour 11b)
	Compare *float64       `json:"compare,omitempty"` // 11b : rendement moyen
	Strict  ThresholdView  `json:"strict"`
	Relaxed *ThresholdView `json:"relaxed,omitempty"`
}

// Fraction est un score « réussites sur total ».
type Fraction struct {
	Passed int `json:"passed"`
	Total  int `json:"total"`
}

// ProfileScores regroupe les scores d'un profil de seuils.
type ProfileScores struct {
	Total    Fraction `json:"total"`
	Quality  Fraction `json:"quality"` // 5a à 10
	Price    Fraction `json:"price"`   // 11a à 11c
	Failures int      `json:"failures"`
}

// HorizonResult est le résultat d'un horizon.
type HorizonResult struct {
	Horizon    int               `json:"horizon"`
	Indicative bool              `json:"indicative"` // sous l'horizon minimal de verdict (D9)
	Criteria   []CriterionResult `json:"criteria"`
	Strict     ProfileScores     `json:"strict"`
	Relaxed    ProfileScores     `json:"relaxed"`
	Warnings   []string          `json:"warnings,omitempty"`
}

// Evaluation est le résultat complet, sérialisable en JSON stable.
type Evaluation struct {
	RulesVersion   string          `json:"rules_version"`
	RulesHash      string          `json:"rules_hash"`
	Verdict        Verdict         `json:"verdict"`
	VerdictHorizon *int            `json:"verdict_horizon,omitempty"` // horizon qui a donné candidate ou watch
	Gate           Gate            `json:"gate"`
	Financial      bool            `json:"financial"`
	Horizons       []HorizonResult `json:"horizons"`
}

// Evaluate calcule les mesures puis le pointage des deux profils en une passe.
func Evaluate(in Inputs, r *Rules, ctx Context) (Evaluation, error) {
	if r == nil {
		return Evaluation{}, errors.New("évaluer: référentiel absent")
	}
	return Score(ComputeMetrics(in, OptionsFromRules(r)), r, ctx)
}

// Score pointe des mesures déjà calculées contre le référentiel.
func Score(ms []Metrics, r *Rules, ctx Context) (Evaluation, error) {
	if r == nil {
		return Evaluation{}, errors.New("pointer: référentiel absent")
	}
	ev := Evaluation{
		RulesVersion: r.Version,
		RulesHash:    r.Hash,
		Gate:         ctx.Gate,
		Financial:    ctx.Financial,
		Horizons:     make([]HorizonResult, 0, len(ms)),
	}
	for _, m := range ms {
		hr, err := scoreHorizon(m, r, ctx.Financial)
		if err != nil {
			return Evaluation{}, fmt.Errorf("pointer l'horizon %d: %w", m.Horizon, err)
		}
		hr.Indicative = m.Horizon < r.Parameters.MinVerdictHorizon
		ev.Horizons = append(ev.Horizons, hr)
	}
	ev.Verdict, ev.VerdictHorizon = verdict(ev.Horizons, ctx.Gate)
	return ev, nil
}

func verdict(hs []HorizonResult, g Gate) (Verdict, *int) {
	if !g.Open() {
		return VerdictExcluded, nil
	}
	var watch *int
	for _, h := range hs {
		if h.Indicative {
			continue
		}
		hz := h.Horizon
		if h.Strict.Failures == 0 {
			return VerdictCandidate, &hz
		}
		if h.Relaxed.Failures == 0 && watch == nil {
			watch = &hz
		}
	}
	if watch != nil {
		return VerdictWatch, watch
	}
	return VerdictRejected, nil
}

func scoreHorizon(m Metrics, r *Rules, financial bool) (HorizonResult, error) {
	hr := HorizonResult{Horizon: m.Horizon, Warnings: slices.Clone(m.Warnings)}
	for i, id := range CriterionIDs {
		c, ok := r.Criteria[id]
		if !ok {
			return hr, fmt.Errorf("critère %s absent du référentiel", id)
		}
		cr := scoreCriterion(c, m, financial)
		hr.Criteria = append(hr.Criteria, cr)
		quality := i < 7 // 5a, 5b, 6, 7, 8, 9, 10
		tally(&hr.Strict, cr.Status, quality, false)
		tally(&hr.Relaxed, cr.Status, quality, true)
	}
	return hr, nil
}

// tally ajoute un critère aux scores d'un profil ; un exempté sort du dénominateur.
func tally(p *ProfileScores, st Status, quality, relaxed bool) {
	if st == StatusExempt {
		return
	}
	ok := st == StatusPassed || (relaxed && st == StatusBorderline)
	sub := &p.Price
	if quality {
		sub = &p.Quality
	}
	for _, f := range []*Fraction{&p.Total, sub} {
		f.Total++
		if ok {
			f.Passed++
		}
	}
	if !ok {
		p.Failures++
	}
}

// subject est la mesure à pointer pour un critère.
type subject struct {
	status  MeasureStatus
	value   float64
	compare float64
	hasCmp  bool
}

func subjectOf(id string, m Metrics) subject {
	fromM := func(x Measure) subject { return subject{status: x.Status, value: x.Value} }
	switch id {
	case "5a":
		return fromM(m.EPSGrowth)
	case "5b":
		return subject{status: m.EPSRises.Status, value: float64(m.EPSRises.Value)}
	case "6":
		return fromM(m.DPSGrowth)
	case "7":
		return fromM(m.PayoutRatio)
	case "8":
		return fromM(m.DebtToEquity)
	case "9":
		return fromM(m.DividendChange)
	case "10":
		return fromM(m.ShareChange)
	case "11a":
		return fromM(m.PE)
	case "11c":
		return fromM(m.PB)
	}
	// 11b : deux mesures ; une donnée manquante l'emporte sur un historique insuffisant.
	cur, avg := m.CurrentYield, m.AverageYield
	s := subject{value: cur.Value, compare: avg.Value, hasCmp: true}
	switch {
	case cur.Status == StatusMissing || avg.Status == StatusMissing:
		s.status = StatusMissing
	case !cur.Ok() || !avg.Ok():
		s.status = StatusInsufficient
	default:
		s.status = StatusValue
	}
	return s
}

func scoreCriterion(c Criterion, m Metrics, financial bool) CriterionResult {
	cr := CriterionResult{ID: c.ID, Strict: view(c.Strict, m.Horizon)}
	if c.Relaxed != nil {
		v := view(*c.Relaxed, m.Horizon)
		cr.Relaxed = &v
	}
	s := subjectOf(c.ID, m)
	switch {
	case c.ID == "8" && financial:
		cr.Status = StatusExempt
		return cr
	case s.status == StatusMissing:
		cr.Status = StatusMissingData
		return cr
	case s.status == StatusInsufficient:
		cr.Status = StatusNotEnough
		return cr
	}
	v := s.value
	cr.Value = &v
	if s.hasCmp {
		cmp := s.compare
		cr.Compare = &cmp
	}
	relaxed := c.Strict // sans seuil assoupli, le seuil strict s'applique
	if c.Relaxed != nil {
		relaxed = *c.Relaxed
	}
	switch {
	case meets(c.ID, c.Strict, s, m.Horizon):
		cr.Status = StatusPassed
	case meets(c.ID, relaxed, s, m.Horizon):
		cr.Status = StatusBorderline
	default:
		cr.Status = StatusFailed
	}
	return cr
}

// meets indique si la mesure respecte le seuil ; un seuil inapplicable à l'horizon n'est pas respecté.
func meets(id string, t Threshold, s subject, horizon int) bool {
	switch id {
	case "5b":
		n, ok := t.MinRises[horizon]
		return ok && s.value >= float64(n)
	case "10":
		return s.value < 0
	case "11b":
		return s.value > s.compare
	}
	if t.Gt != nil && !(s.value > *t.Gt) {
		return false
	}
	if t.Ge != nil && !(s.value >= *t.Ge) {
		return false
	}
	return t.Le == nil || s.value <= *t.Le
}

func view(t Threshold, horizon int) ThresholdView {
	v := ThresholdView{Gt: t.Gt, Ge: t.Ge, Le: t.Le, Direction: t.Direction}
	if n, ok := t.MinRises[horizon]; ok {
		v.MinRises = &n
	}
	return v
}
