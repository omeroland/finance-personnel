package httpapi

import (
	"errors"
	"fmt"
	"slices"
	"strconv"
	"time"

	"github.com/shopspring/decimal"

	"github.com/omeroland/finance-privee/backend/internal/api/gen"
	"github.com/omeroland/finance-privee/backend/internal/modules/stockeval/engine"
	"github.com/omeroland/finance-privee/backend/internal/modules/stockeval/store"
	"github.com/omeroland/finance-privee/backend/internal/platform/httpx"
)

// stockData regroupe ce que les dépôts savent d'une action.
type stockData struct {
	inst  store.Instrument
	facts []store.Fact
	snap  *store.Snapshot
	qual  *store.Qualitative
}

// archivedInputs est ce qui est archivé avec une évaluation : de quoi la
// recalculer à l'identique (l'empreinte en est faite par store/).
type archivedInputs struct {
	Mode      string               `json:"mode"`
	Gate      engine.Gate          `json:"gate"`
	Financial bool                 `json:"financial"`
	Inputs    engine.Inputs        `json:"inputs"`
	Compat    *engine.CompatInputs `json:"compat,omitempty"`
}

// engineInputs traduit les faits et l'instantané en entrées du moteur.
func engineInputs(d stockData, year int) engine.Inputs {
	in := engine.Inputs{CurrentYear: year}
	for _, f := range d.facts {
		p := engine.Point{Year: f.FiscalYear, Value: f.Value}
		switch f.Metric {
		case store.MetricEPSDiluted:
			in.EPS = append(in.EPS, p)
		case store.MetricDPS:
			in.DPS = append(in.DPS, p)
		case store.MetricDividendYield:
			in.AnnualYield = append(in.AnnualYield, p)
		case store.MetricSharesDiluted:
			in.Shares = append(in.Shares, p)
		}
	}
	if s := d.snap; s != nil {
		opt := func(n decimal.NullDecimal) *decimal.Decimal {
			if !n.Valid {
				return nil
			}
			v := n.Decimal
			return &v
		}
		in.Snapshot = engine.Snapshot{
			Price: opt(s.Price), ForwardDividend: opt(s.ExpectedDividend), LongTermDebt: opt(s.LTDebt),
			Equity: opt(s.Equity), BookValuePerShare: opt(s.BookValuePerShare), FX: opt(s.FXRate),
		}
	}
	return in
}

func engineContext(d stockData) engine.Context {
	ctx := engine.Context{Financial: d.inst.IsFinancial}
	if q := d.qual; q != nil {
		for i, b := range []*bool{q.R1, q.R2, q.R3, q.R4} {
			switch {
			case b == nil:
			case *b:
				ctx.Gate[i] = engine.AnswerYes
			default:
				ctx.Gate[i] = engine.AnswerNo
			}
		}
	}
	return ctx
}

func measurePtr(m engine.Measure) *float64 {
	if !m.Ok() {
		return nil
	}
	v := m.Value
	return &v
}

// imported préfère la valeur lue dans la feuille (cellule de l'instantané) à la mesure du moteur.
func imported(n decimal.NullDecimal, m engine.Measure) *float64 {
	if n.Valid {
		f, _ := n.Decimal.Float64()
		return &f
	}
	return measurePtr(m)
}

// compatInputs prépare les entrées du mode compat (D201) : séries en 20 cellules, valeurs de la
// feuille importées si présentes, sinon mesures du moteur sur l'horizon de la feuille.
func (h *Handler) compatInputs(d stockData, in engine.Inputs) engine.CompatInputs {
	opts := engine.OptionsFromRules(h.rules)
	opts.Horizons = []int{h.rules.Compat.Rule5bHorizon}
	var m engine.Metrics
	if ms := engine.ComputeMetrics(in, opts); len(ms) > 0 {
		m = ms[0]
	}
	var s store.Snapshot
	if d.snap != nil {
		s = *d.snap
	}
	ci := engine.CompatInputs{
		EPS: engine.CompatCellsFromSeries(in.EPS), DPS: engine.CompatCellsFromSeries(in.DPS),
		PayoutRatio: imported(s.PayoutRatioImported, m.PayoutRatio), DebtToEquity: imported(s.DebtToEquity, m.DebtToEquity),
		PE: imported(s.PEImported, m.PE), PB: imported(s.PBImported, m.PB),
		DividendChange: measurePtr(m.DividendChange), CurrentYield: measurePtr(m.CurrentYield), AverageYield: measurePtr(m.AverageYield),
	}
	if q := d.qual; q != nil && q.BuybackManual != nil {
		ci.Buyback = "no"
		if *q.BuybackManual {
			ci.Buyback = "yes"
		}
	}
	return ci
}

// sheetError : le mode compat reproduit une erreur de la feuille (#DIV/0!). C'est une requête
// 400 du contrat (D202), pas une erreur du serveur.
func sheetError(err error) error {
	return &httpx.Error{
		Status: 400, Kind: "sheet-error", Title: "Erreur de la feuille reproduite",
		Detail: "Le mode compat reproduit une erreur de la feuille (#DIV/0!) : un BPA ou un dividende précédent nul casse la ligne. Utilise le mode corrigé.",
		Cause:  err,
	}
}

// evaluate appelle le moteur dans le mode demandé.
func (h *Handler) evaluate(d stockData, mode gen.Mode) (engine.Evaluation, archivedInputs, error) {
	in := engineInputs(d, h.now().Year())
	ctx := engineContext(d)
	arch := archivedInputs{Mode: string(mode), Gate: ctx.Gate, Financial: ctx.Financial, Inputs: in}
	if mode == gen.Compat {
		ci := h.compatInputs(d, in)
		arch.Compat = &ci
		ev, err := engine.CompatEvaluate(ci, h.rules, ctx)
		if errors.Is(err, engine.ErrSheetDivZero) {
			return engine.Evaluation{}, arch, sheetError(err)
		}
		if err != nil {
			return engine.Evaluation{}, arch, fmt.Errorf("évaluer %s en compat: %w", d.inst.Symbol, err)
		}
		return ev, arch, nil
	}
	ev, err := engine.Evaluate(in, h.rules, ctx)
	if err != nil {
		return engine.Evaluation{}, arch, fmt.Errorf("évaluer %s: %w", d.inst.Symbol, err)
	}
	return ev, arch, nil
}

// ---- Seuils -------------------------------------------------------------

// thr est un seuil, vu du moteur (ThresholdView ou Threshold).
type thr struct {
	gt, ge, le *float64
	minRises   *int
	direction  string
}

func thrOfView(v engine.ThresholdView) thr {
	return thr{gt: v.Gt, ge: v.Ge, le: v.Le, minRises: v.MinRises, direction: v.Direction}
}

// pctCriteria : critères dont la valeur est une fraction, affichée en pourcentage.
var pctCriteria = map[string]bool{"5a": true, "6": true, "7": true, "8": true, "9": true}

func fmtNum(v float64, pct bool) string {
	if pct {
		v *= 100
	}
	return strconv.FormatFloat(v, 'f', -1, 64)
}

func thresholdDTO(id string, t thr) gen.Threshold {
	switch {
	case t.direction == engine.DirectionDecrease:
		return gen.Threshold{Description: "Actions en circulation en baisse"}
	case t.direction == engine.DirectionGreater:
		return gen.Threshold{Description: "Rendement actuel supérieur au rendement moyen"}
	case t.minRises != nil:
		return gen.Threshold{
			Description: fmt.Sprintf("≥ %d hausses", *t.minRises),
			Min:         ptr(float32(*t.minRises)), MinInclusive: ptr(true),
		}
	}
	pct := pctCriteria[id]
	unit := ""
	if pct {
		unit = " %"
	}
	out := gen.Threshold{}
	var lo float64
	var hasLo bool
	switch {
	case t.gt != nil:
		lo, hasLo = *t.gt, true
		out.Min, out.MinInclusive = ptr(float32(lo)), ptr(false)
	case t.ge != nil:
		lo, hasLo = *t.ge, true
		out.Min, out.MinInclusive = ptr(float32(lo)), ptr(true)
	}
	if t.le != nil {
		out.Max, out.MaxInclusive = ptr(float32(*t.le)), ptr(true)
	}
	switch {
	case hasLo && t.le != nil:
		op := "≤"
		if t.gt != nil {
			op = "<"
		}
		out.Description = fmt.Sprintf("%s %s x ≤ %s%s", fmtNum(lo, pct), op, fmtNum(*t.le, pct), unit)
	case hasLo:
		op := "≥"
		if t.gt != nil {
			op = ">"
		}
		out.Description = fmt.Sprintf("%s %s%s", op, fmtNum(lo, pct), unit)
	case t.le != nil:
		out.Description = fmt.Sprintf("≤ %s%s", fmtNum(*t.le, pct), unit)
	default:
		out.Description = "Aucun seuil"
	}
	return out
}

// ---- Évaluation -> DTO ---------------------------------------------------

func criterionDTO(c engine.CriterionResult) gen.Criterion {
	meta := criterionMeta[c.ID]
	dto := gen.Criterion{
		Id: gen.CriterionId(c.ID), Label: ptr(meta.label), Status: gen.CriterionStatus(c.Status),
		StrictThreshold: thresholdDTO(c.ID, thrOfView(c.Strict)),
	}
	if c.Value != nil {
		dto.Value = ptr(float32(*c.Value))
	}
	if c.Relaxed != nil {
		dto.RelaxedThreshold = ptr(thresholdDTO(c.ID, thrOfView(*c.Relaxed)))
	}
	switch c.Status {
	case engine.StatusMissingData:
		dto.Note = ptr("Donnée manquante.")
	case engine.StatusNotEnough:
		dto.Note = ptr("Historique insuffisant pour l'horizon.")
	case engine.StatusExempt:
		dto.Note = ptr("Critère exempté.")
	default:
		if c.Compare != nil {
			dto.Note = ptr("Rendement moyen : " + strconv.FormatFloat(*c.Compare, 'f', 4, 64) + " (fraction).")
		}
	}
	return dto
}

func subScore(strict, relaxed engine.Fraction) gen.SubScore {
	return gen.SubScore{Strict: strict.Passed, Relaxed: relaxed.Passed, OutOf: strict.Total}
}

func insufficient(hr engine.HorizonResult) bool {
	return slices.ContainsFunc(hr.Criteria, func(c engine.CriterionResult) bool { return c.Status == engine.StatusNotEnough })
}

func horizonDTO(hr engine.HorizonResult, gate engine.Gate) gen.HorizonResult {
	dto := gen.HorizonResult{
		Horizon: gen.Horizon(hr.Horizon), Indicative: hr.Indicative, InsufficientHistory: insufficient(hr),
		Strict: hr.Strict.Total.Passed, Relaxed: hr.Relaxed.Total.Passed, OutOf: hr.Strict.Total.Total,
		FailureCount: hr.Strict.Failures,
		Quality:      subScore(hr.Strict.Quality, hr.Relaxed.Quality), Price: subScore(hr.Strict.Price, hr.Relaxed.Price),
		Criteria: make([]gen.Criterion, 0, len(hr.Criteria)),
	}
	if v, ok := engine.HorizonVerdict(hr, gate); ok {
		dto.Verdict = ptr(gen.Verdict(v))
	}
	for _, c := range hr.Criteria {
		dto.Criteria = append(dto.Criteria, criterionDTO(c))
	}
	return dto
}

func evaluationDTO(symbol string, mode gen.Mode, ev engine.Evaluation, at time.Time, extra []string) gen.Evaluation {
	dto := gen.Evaluation{
		Symbol: symbol, Mode: mode, RulesVersion: ev.RulesVersion, ComputedAt: at.UTC(),
		QualitativeGatePassed: ev.Gate.Open(), Horizons: make([]gen.HorizonResult, 0, len(ev.Horizons)),
	}
	seen := map[string]bool{}
	var warns []string
	add := func(w string) {
		if !seen[w] {
			seen[w] = true
			warns = append(warns, w)
		}
	}
	for _, w := range extra {
		add(w)
	}
	for _, hr := range ev.Horizons {
		dto.Horizons = append(dto.Horizons, horizonDTO(hr, ev.Gate))
		for _, w := range hr.Warnings {
			add(w)
		}
	}
	if len(warns) > 0 {
		dto.Warnings = &warns
	}
	return dto
}

// summaryDTO est la ligne de score de la liste de suivi pour l'horizon demandé ;
// perfectHorizon est le plus long horizon à 10/10 strict, calculé ici côté serveur (D160).
func summaryDTO(mode gen.Mode, ev engine.Evaluation, horizon int) (gen.ScoreSummary, bool) {
	var perfect *gen.Horizon
	var sel *engine.HorizonResult
	for i := range ev.Horizons {
		hr := &ev.Horizons[i]
		if hr.Horizon == horizon {
			sel = hr
		}
		if hr.Strict.Failures == 0 && (perfect == nil || int(*perfect) < hr.Horizon) {
			perfect = ptr(gen.Horizon(hr.Horizon))
		}
	}
	if sel == nil {
		return gen.ScoreSummary{}, false
	}
	hd := horizonDTO(*sel, ev.Gate)
	return gen.ScoreSummary{
		Horizon: hd.Horizon, Mode: mode, Strict: hd.Strict, Relaxed: hd.Relaxed, OutOf: hd.OutOf,
		FailureCount: hd.FailureCount, Quality: hd.Quality, Price: hd.Price, Verdict: hd.Verdict,
		InsufficientHistory: ptr(hd.InsufficientHistory), PerfectHorizon: perfect,
	}, true
}

func conflictWarnings(cs []store.FactConflict) []string {
	out := make([]string, 0, len(cs))
	for _, c := range cs {
		out = append(out, fmt.Sprintf("Conflit de sources non résolu : %s %d (%s et %s).",
			metricFromStore[c.Metric], c.FiscalYear, c.ExistingSource, c.IncomingSource))
	}
	return out
}
