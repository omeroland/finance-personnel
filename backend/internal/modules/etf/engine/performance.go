package engine

// Rendement (module 2) : rendement pondéré dans le temps (TWR), TRI par XIRR
// sur flux datés, et formule mensuelle de l'outil pour un compte sans
// transactions (relevés de valeur). Une seule série : le rendement annuel
// se déduit du mensuel (D380, D381).

import (
	"errors"
	"fmt"
	"math"
	"sort"
	"strconv"
	"time"

	"github.com/shopspring/decimal"
)

// Erreurs sentinelles du rendement ; les erreurs détaillées les enveloppent.
var (
	// ErrNoFlows : aucun flux.
	ErrNoFlows = errors.New("aucun flux")
	// ErrSingleFlow : un seul flux, pas de rendement.
	ErrSingleFlow = errors.New("un seul flux")
	// ErrNoSignChange : les flux n'ont pas à la fois une sortie et une entrée
	// (par exemple valeur finale nulle) : le TRI n'existe pas.
	ErrNoSignChange = errors.New("flux sans changement de signe")
	// ErrNoConvergence : le TRI n'a pas été trouvé dans le nombre d'itérations borné.
	ErrNoConvergence = errors.New("TRI non convergent")
	// ErrFlowDate : date de flux absente.
	ErrFlowDate = errors.New("date de flux invalide")
	// ErrStatements : relevés invalides (mois mal formé ou non croissant, montant négatif, trop long).
	ErrStatements = errors.New("relevés invalides")
	// ErrHorizon : comptes d'horizons différents, impossible à consolider (D337).
	ErrHorizon = errors.New("horizons différents")
	// ErrNoPeriod : période vide, pas de rendement.
	ErrNoPeriod = errors.New("période vide")
	// ErrValuations : valeurs datées invalides (non croissantes, négatives).
	ErrValuations = errors.New("valeurs datées invalides")
)

const (
	// maxStatements borne la longueur d'une série de relevés (50 ans).
	maxStatements = 600
	// irrTolerance : précision du TRI (écart de taux).
	irrTolerance = 1e-14
	irrNewtonMax = 60
	irrBisectMax = 400
)

// DatedFlow est un flux daté du point de vue de l'investisseur : négatif
// pour une cotisation (sortie de sa poche), positif pour un retrait et pour
// la valeur finale.
type DatedFlow struct {
	Date   time.Time
	Amount decimal.Decimal
}

// XIRR calcule le TRI annualisé (base 365 jours, comme XIRR du tableur) des
// flux datés. Erreurs typées : ErrNoFlows, ErrSingleFlow, ErrFlowDate,
// ErrNoSignChange, ErrNoConvergence. Jamais de panique ni de NaN.
func XIRR(flows []DatedFlow) (float64, error) {
	if len(flows) == 0 {
		return 0, ErrNoFlows
	}
	if len(flows) == 1 {
		return 0, ErrSingleFlow
	}
	t0 := flows[0].Date
	for _, f := range flows {
		if f.Date.IsZero() {
			return 0, ErrFlowDate
		}
		if f.Date.Before(t0) {
			t0 = f.Date
		}
	}
	amounts := make([]float64, len(flows))
	times := make([]float64, len(flows))
	for i, f := range flows {
		amounts[i] = f.Amount.InexactFloat64()
		times[i] = f.Date.Sub(t0).Hours() / 24 / 365
	}
	return solveRate(amounts, times)
}

// solveRate trouve r tel que somme(a_i / (1+r)^t_i) = 0 : Newton, puis
// bissection si Newton échoue. Le taux est borné à ]-1, +inf[.
func solveRate(a, t []float64) (float64, error) {
	hasNeg, hasPos := false, false
	for _, x := range a {
		if math.IsNaN(x) || math.IsInf(x, 0) {
			return 0, fmt.Errorf("montant non fini: %w", ErrNoConvergence)
		}
		hasNeg = hasNeg || x < 0
		hasPos = hasPos || x > 0
	}
	if !hasNeg || !hasPos {
		return 0, ErrNoSignChange
	}
	npv := func(r float64) float64 {
		s := 0.0
		for i := range a {
			s += a[i] * math.Pow(1+r, -t[i])
		}
		return s
	}
	dnpv := func(r float64) float64 {
		s := 0.0
		for i := range a {
			s -= t[i] * a[i] * math.Pow(1+r, -t[i]-1)
		}
		return s
	}
	finite := func(x float64) bool { return !math.IsNaN(x) && !math.IsInf(x, 0) }

	// Newton depuis 10 %.
	r := 0.1
	for i := 0; i < irrNewtonMax; i++ {
		f, d := npv(r), dnpv(r)
		if d == 0 || !finite(f) || !finite(d) {
			break
		}
		next := r - f/d
		if !finite(next) || next <= -1 {
			break
		}
		if math.Abs(next-r) < irrTolerance {
			if finite(npv(next)) {
				return next, nil
			}
			break
		}
		r = next
	}

	// Repli : bissection sur un intervalle qui change de signe.
	lo, hi := -1+1e-12, 1.0
	flo := npv(lo)
	fhi := npv(hi)
	for i := 0; i < 80 && finite(flo) && finite(fhi) && flo*fhi > 0; i++ {
		hi = hi*2 + 1
		fhi = npv(hi)
	}
	if !finite(flo) || !finite(fhi) || flo*fhi > 0 {
		return 0, ErrNoConvergence
	}
	if flo == 0 {
		return lo, nil
	}
	for i := 0; i < irrBisectMax; i++ {
		mid := (lo + hi) / 2
		fm := npv(mid)
		if !finite(fm) {
			return 0, ErrNoConvergence
		}
		if fm == 0 || (hi-lo)/2 < irrTolerance*(1+math.Abs(mid)) {
			return mid, nil
		}
		if flo*fm < 0 {
			hi = mid
		} else {
			lo, flo = mid, fm
		}
	}
	return 0, ErrNoConvergence
}

// periodicIRR : TRI par période de flux espacés régulièrement (période 0, 1, 2…).
func periodicIRR(flows []float64) (float64, error) {
	t := make([]float64, len(flows))
	for i := range t {
		t[i] = float64(i)
	}
	return solveRate(flows, t)
}

// Valuation est un point de la ligne du temps d'un compte avec transactions.
// Value est la valeur du compte à cette date juste avant le flux ; Flow est le
// flux externe net à cette date (positif = cotisation, négatif = retrait).
type Valuation struct {
	Date  time.Time
	Value decimal.Decimal
	Flow  decimal.Decimal
}

// MonthReturn est le rendement d'un mois. Defined est faux quand le mois n'a
// pas de rendement (valeur de départ nulle) ; Return vaut alors 0. Month est
// au format AAAA-MM.
type MonthReturn struct {
	Month   string
	Return  float64
	Defined bool
	// AACJ est le rendement cumulé sur k mois, (1+TRI mensuel)^k − 1, quand
	// HasAACJ (relevés de valeur seulement, D337).
	AACJ    float64
	HasAACJ bool
}

// YearReturn est le rendement d'une année civile, déduit des rendements mensuels.
type YearReturn struct {
	Year   int
	Return float64
	// Months est le nombre de mois définis dans l'année (moins de 12 = année partielle).
	Months int
}

// Performance est la série de rendement d'un compte ou d'un consolidé. Les
// rendements annuels ne sont jamais stockés : Annual() les déduit de Monthly.
type Performance struct {
	Monthly []MonthReturn
	// Cumulative : rendement de la période (produit des rendements mensuels − 1).
	Cumulative    float64
	HasCumulative bool
	// IRR : TRI cumulé sur la période (relevés : (1+TRI mensuel)^n − 1 ; transactions : XIRR annualisé).
	IRR    float64
	HasIRR bool
}

// Annual déduit les rendements annuels de la série mensuelle (produit des
// rendements définis de chaque année civile). Une seule série (D381).
func (p Performance) Annual() []YearReturn {
	return AnnualFromMonthly(p.Monthly)
}

// AnnualFromMonthly regroupe des rendements mensuels par année civile.
func AnnualFromMonthly(monthly []MonthReturn) []YearReturn {
	var out []YearReturn
	idx := map[int]int{}
	for _, m := range monthly {
		if !m.Defined || len(m.Month) < 4 {
			continue
		}
		y, err := strconv.Atoi(m.Month[:4])
		if err != nil {
			continue
		}
		i, seen := idx[y]
		if !seen {
			i = len(out)
			idx[y] = i
			out = append(out, YearReturn{Year: y, Return: 1})
		}
		out[i].Return *= 1 + m.Return
		out[i].Months++
	}
	for i := range out {
		out[i].Return--
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Year < out[j].Year })
	return out
}

// Compound compose des rendements successifs : produit(1+r) − 1. Vide : (0, false).
func Compound(returns []float64) (float64, bool) {
	if len(returns) == 0 {
		return 0, false
	}
	p := 1.0
	for _, r := range returns {
		p *= 1 + r
	}
	if math.IsNaN(p) || math.IsInf(p, 0) {
		return 0, false
	}
	return p - 1, true
}

// Annualize donne le rendement annualisé (moyenne géométrique) de rendements
// annuels. Vide : ErrNoPeriod.
func Annualize(annual []float64) (float64, error) {
	if len(annual) == 0 {
		return 0, ErrNoPeriod
	}
	p := 1.0
	for _, r := range annual {
		if math.IsNaN(r) || math.IsInf(r, 0) || r < -1 {
			return 0, fmt.Errorf("rendement annuel %v: %w", r, ErrStatements)
		}
		p *= 1 + r
	}
	return math.Pow(p, 1/float64(len(annual))) - 1, nil
}

// TWR calcule le rendement pondéré dans le temps d'un compte avec
// transactions : chaînage des sous-périodes entre deux flux, puis regroupement
// mensuel (par mois de la date de fin de la sous-période). Les points doivent
// être en ordre strictement croissant de date. Une sous-période dont la valeur
// de départ (valeur + flux) est nulle n'a pas de rendement et est ignorée.
// Le TRI (XIRR) est calculé sur les flux changés de signe plus la valeur finale.
func TWR(points []Valuation) (Performance, error) {
	if len(points) < 2 {
		return Performance{}, ErrNoPeriod
	}
	for i, p := range points {
		if p.Date.IsZero() {
			return Performance{}, fmt.Errorf("point %d: %w", i, ErrFlowDate)
		}
		if p.Value.IsNegative() {
			return Performance{}, fmt.Errorf("point %d: valeur négative: %w", i, ErrValuations)
		}
		if i > 0 && !p.Date.After(points[i-1].Date) {
			return Performance{}, fmt.Errorf("point %d: dates non croissantes: %w", i, ErrValuations)
		}
	}
	type acc struct {
		prod    float64
		defined bool
	}
	var months []string
	byMonth := map[string]*acc{}
	for i := 1; i < len(points); i++ {
		start := points[i-1].Value.Add(points[i-1].Flow)
		label := points[i].Date.Format("2006-01")
		a, ok := byMonth[label]
		if !ok {
			a = &acc{prod: 1}
			byMonth[label] = a
			months = append(months, label)
		}
		if !start.IsPositive() {
			continue
		}
		r := points[i].Value.Div(start).InexactFloat64() - 1
		if math.IsNaN(r) || math.IsInf(r, 0) {
			continue
		}
		a.prod *= 1 + r
		a.defined = true
	}
	var perf Performance
	var all []float64
	for _, label := range months {
		a := byMonth[label]
		m := MonthReturn{Month: label}
		if a.defined {
			m.Return, m.Defined = a.prod-1, true
			all = append(all, m.Return)
		}
		perf.Monthly = append(perf.Monthly, m)
	}
	perf.Cumulative, perf.HasCumulative = Compound(all)

	flows := make([]DatedFlow, 0, len(points)+1)
	for _, p := range points {
		if !p.Flow.IsZero() {
			flows = append(flows, DatedFlow{Date: p.Date, Amount: p.Flow.Neg()})
		}
	}
	last := points[len(points)-1]
	flows = append(flows, DatedFlow{Date: last.Date, Amount: last.Value})
	if r, err := XIRR(flows); err == nil {
		perf.IRR, perf.HasIRR = r, true
	}
	return perf, nil
}

// MergeValuations consolide des comptes avec transactions : mêmes dates
// exactement, valeurs et flux additionnés. Sinon ErrHorizon (D337).
func MergeValuations(accounts [][]Valuation) ([]Valuation, error) {
	if len(accounts) == 0 {
		return nil, ErrNoPeriod
	}
	out := make([]Valuation, len(accounts[0]))
	copy(out, accounts[0])
	for _, acct := range accounts[1:] {
		if len(acct) != len(out) {
			return nil, ErrHorizon
		}
		for i, p := range acct {
			if !p.Date.Equal(out[i].Date) {
				return nil, ErrHorizon
			}
			out[i].Value = out[i].Value.Add(p.Value)
			out[i].Flow = out[i].Flow.Add(p.Flow)
		}
	}
	return out, nil
}

// Statement est un relevé de valeur de fin de mois d'un compte sans
// transactions : cotisation du mois (négative = retrait), versée en fin de
// mois (D15), et valeur de fin de mois.
type Statement struct {
	Month        string // AAAA-MM
	Contribution decimal.Decimal
	EndValue     decimal.Decimal
}

// StatementPerformance applique la formule mensuelle de l'outil :
// r = (fin − cotisation) / début − 1, le début d'un mois étant la fin du
// précédent (le premier mois part de start). L'AACJ du mois k est
// (1+TRI mensuel)^k − 1 pour les flux : −start, −cotisations des mois
// précédents, et (fin_k − cotisation_k) au mois k (D337). Un mois de départ
// nul n'a pas de rendement (Defined faux) ; il est ignoré du chaînage.
func StatementPerformance(start decimal.Decimal, statements []Statement) (Performance, error) {
	if len(statements) == 0 {
		return Performance{}, ErrNoPeriod
	}
	if len(statements) > maxStatements {
		return Performance{}, fmt.Errorf("plus de %d relevés: %w", maxStatements, ErrStatements)
	}
	if start.IsNegative() {
		return Performance{}, fmt.Errorf("valeur de départ négative: %w", ErrStatements)
	}
	var prev time.Time
	for i, s := range statements {
		m, err := time.Parse("2006-01", s.Month)
		if err != nil {
			return Performance{}, fmt.Errorf("relevé %d: mois %q: %w", i, s.Month, ErrStatements)
		}
		if i > 0 && !m.After(prev) {
			return Performance{}, fmt.Errorf("relevé %d: mois non croissants: %w", i, ErrStatements)
		}
		if s.EndValue.IsNegative() {
			return Performance{}, fmt.Errorf("relevé %d: valeur négative: %w", i, ErrStatements)
		}
		prev = m
	}

	var perf Performance
	var all []float64
	begin := start
	flows := []float64{start.Neg().InexactFloat64()}
	for k, s := range statements {
		m := MonthReturn{Month: s.Month}
		net := s.EndValue.Sub(s.Contribution)
		if begin.IsPositive() {
			r := net.Div(begin).InexactFloat64() - 1
			if !math.IsNaN(r) && !math.IsInf(r, 0) {
				m.Return, m.Defined = r, true
				all = append(all, r)
			}
		}
		trial := append(append([]float64(nil), flows...), net.InexactFloat64())
		if i, err := periodicIRR(trial); err == nil {
			if v := math.Pow(1+i, float64(k+1)) - 1; !math.IsNaN(v) && !math.IsInf(v, 0) {
				m.AACJ, m.HasAACJ = v, true
			}
		}
		perf.Monthly = append(perf.Monthly, m)
		flows = append(flows, s.Contribution.Neg().InexactFloat64())
		begin = s.EndValue
	}
	perf.Cumulative, perf.HasCumulative = Compound(all)
	if n := len(perf.Monthly); perf.Monthly[n-1].HasAACJ {
		perf.IRR, perf.HasIRR = perf.Monthly[n-1].AACJ, true
	}
	return perf, nil
}

// StatementAccount est un compte sans transactions : départ et relevés.
type StatementAccount struct {
	Start      decimal.Decimal
	Statements []Statement
}

// ConsolidateStatements additionne départs, cotisations et valeurs de fin de
// comptes de même horizon (mêmes mois), puis applique la formule mensuelle
// (D337). Horizons différents : ErrHorizon.
func ConsolidateStatements(accounts []StatementAccount) (Performance, error) {
	if len(accounts) == 0 {
		return Performance{}, ErrNoPeriod
	}
	total := StatementAccount{Start: accounts[0].Start, Statements: append([]Statement(nil), accounts[0].Statements...)}
	for _, a := range accounts[1:] {
		if len(a.Statements) != len(total.Statements) {
			return Performance{}, ErrHorizon
		}
		total.Start = total.Start.Add(a.Start)
		for i, s := range a.Statements {
			if s.Month != total.Statements[i].Month {
				return Performance{}, ErrHorizon
			}
			total.Statements[i].Contribution = total.Statements[i].Contribution.Add(s.Contribution)
			total.Statements[i].EndValue = total.Statements[i].EndValue.Add(s.EndValue)
		}
	}
	return StatementPerformance(total.Start, total.Statements)
}
