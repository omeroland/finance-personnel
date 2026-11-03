package engine

import (
	"errors"
	"fmt"
	"math"
	"slices"

	"github.com/shopspring/decimal"
)

// Erreurs typées du calcul des mesures.
var (
	// ErrZeroBase : la valeur précédente est nulle, le taux de croissance n'existe pas.
	ErrZeroBase = errors.New("valeur précédente nulle")
	// ErrNotFinite : le résultat n'est pas un nombre fini.
	ErrNotFinite = errors.New("résultat non fini")
	// ErrNoData : aucune valeur à moyenner.
	ErrNoData = errors.New("aucune valeur")
)

// MeasureStatus indique si une mesure a une valeur.
type MeasureStatus string

// Statuts d'une mesure.
const (
	StatusValue        MeasureStatus = "value"        // valeur calculée
	StatusMissing      MeasureStatus = "missing"      // donnée absente ou inutilisable
	StatusInsufficient MeasureStatus = "insufficient" // historique insuffisant
)

// Measure est une mesure : une valeur (ratio en fraction) ou un marqueur.
type Measure struct {
	Status MeasureStatus
	Value  float64
}

// Ok indique que la mesure a une valeur.
func (m Measure) Ok() bool { return m.Status == StatusValue }

// IntMeasure est une mesure entière (nombre de hausses).
type IntMeasure struct {
	Status MeasureStatus
	Value  int
}

// Ok indique que la mesure a une valeur.
func (m IntMeasure) Ok() bool { return m.Status == StatusValue }

// Point est une valeur annuelle (année fiscale).
type Point struct {
	Year  int
	Value decimal.Decimal
}

// Series est une série annuelle. L'ordre est libre ; une année absente est un trou.
type Series []Point

// Snapshot est l'instantané courant. Un champ nil est une donnée manquante.
// Price est dans la devise de cotation ; les autres montants sont dans la devise de déclaration.
type Snapshot struct {
	Price             *decimal.Decimal
	ForwardDividend   *decimal.Decimal // dividende annuel prévu par action
	LongTermDebt      *decimal.Decimal
	Equity            *decimal.Decimal
	BookValuePerShare *decimal.Decimal
	// FX convertit la devise de déclaration vers la devise de cotation ; nil = même devise.
	FX *decimal.Decimal
}

// Inputs regroupe les entrées du calcul des mesures.
type Inputs struct {
	EPS         Series // BPA dilué
	DPS         Series // dividende par action ordinaire
	AnnualYield Series // rendement annuel, en fraction (0.03 = 3 %)
	Shares      Series // actions en circulation diluées
	Snapshot    Snapshot
	CurrentYear int // année courante, fournie par l'appelant (aucune horloge)
}

// Options règle le calcul ; elle vient de rules.yaml (voir OptionsFromRules).
type Options struct {
	Horizons     []int
	TrimMinRates int
	BuybackYears int
}

// OptionsFromRules extrait les paramètres de calcul du référentiel.
func OptionsFromRules(r *Rules) Options {
	return Options{
		Horizons:     slices.Clone(r.Parameters.Horizons),
		TrimMinRates: r.Parameters.TrimMinRates,
		BuybackYears: r.Parameters.BuybackPeriodYears,
	}
}

// Metrics regroupe les mesures d'un horizon.
type Metrics struct {
	Horizon        int
	EPSGrowth      Measure    // règle 5a : moyenne (tronquée) des taux du BPA
	EPSRises       IntMeasure // règle 5b : nombre de taux > 0
	DPSGrowth      Measure    // règle 6
	PayoutRatio    Measure    // règle 7 : dividende / BPA du dernier exercice
	DebtToEquity   Measure    // règle 8
	DividendChange Measure    // règle 9 : dividende prévu / dernier dividende - 1
	ShareChange    Measure    // règle 10 : actions récentes / actions d'il y a n ans - 1
	PE             Measure    // règle 11a
	CurrentYield   Measure    // règle 11b : dividende prévu / prix
	AverageYield   Measure    // règle 11b : moyenne du rendement annuel sur la fenêtre
	PB             Measure    // règle 11c
	Warnings       []string
}

// Growth retourne (cur - prev) / |prev| ; ErrZeroBase si prev vaut 0.
func Growth(prev, cur decimal.Decimal) (float64, error) {
	if prev.IsZero() {
		return 0, ErrZeroBase
	}
	return toFloat(cur.Sub(prev).Div(prev.Abs()))
}

func toFloat(d decimal.Decimal) (float64, error) {
	f, _ := d.Float64()
	if math.IsNaN(f) || math.IsInf(f, 0) {
		return 0, ErrNotFinite
	}
	return f, nil
}

// normalize trie par année et garde la dernière valeur d'une année répétée.
func normalize(s Series) Series {
	out := slices.Clone(s)
	slices.SortStableFunc(out, func(a, b Point) int { return a.Year - b.Year })
	res := out[:0]
	for _, p := range out {
		if n := len(res); n > 0 && res[n-1].Year == p.Year {
			res[n-1] = p
			continue
		}
		res = append(res, p)
	}
	return res
}

// Window retourne les h dernières années consécutives de la série, en ordre croissant.
// Elle retourne false si la série n'a pas h années consécutives se terminant à sa dernière année.
func Window(s Series, h int) (Series, bool) {
	if h < 1 {
		return nil, false
	}
	n := normalize(s)
	if len(n) < h {
		return nil, false
	}
	w := n[len(n)-h:]
	if w[h-1].Year-w[0].Year != h-1 {
		return nil, false
	}
	return slices.Clone(w), true
}

// TrimmedMean retire le min et le max si len(xs) >= minCount (et au moins 3 valeurs),
// sinon fait la moyenne simple.
func TrimmedMean(xs []float64, minCount int) (float64, error) {
	if len(xs) == 0 {
		return 0, ErrNoData
	}
	v := slices.Clone(xs)
	for _, x := range v {
		if math.IsNaN(x) || math.IsInf(x, 0) {
			return 0, ErrNotFinite
		}
	}
	if len(v) >= minCount && len(v) >= 3 {
		slices.Sort(v)
		v = v[1 : len(v)-1]
	}
	sum := 0.0
	for _, x := range v {
		sum += x
	}
	m := sum / float64(len(v))
	if math.IsNaN(m) || math.IsInf(m, 0) {
		return 0, ErrNotFinite
	}
	return m, nil
}

// CountIncreases compte les taux strictement positifs.
func CountIncreases(rates []float64) int {
	n := 0
	for _, r := range rates {
		if r > 0 {
			n++
		}
	}
	return n
}

// rates calcule les taux d'une fenêtre ; les taux impossibles sont ignorés avec un avertissement.
func rates(label string, w Series) (rs []float64, warns []string) {
	for i := 1; i < len(w); i++ {
		g, err := Growth(w[i-1].Value, w[i].Value)
		if err != nil {
			warns = append(warns, fmt.Sprintf("%s: taux %d-%d ignoré (%v)", label, w[i-1].Year, w[i].Year, err))
			continue
		}
		rs = append(rs, g)
	}
	return rs, warns
}

func (s Series) at(year int) (decimal.Decimal, bool) {
	for _, p := range s {
		if p.Year == year {
			return p.Value, true
		}
	}
	return decimal.Decimal{}, false
}

func missing() Measure      { return Measure{Status: StatusMissing} }
func insufficient() Measure { return Measure{Status: StatusInsufficient} }

func value(d decimal.Decimal, warns *[]string, label string) Measure {
	f, err := toFloat(d)
	if err != nil {
		*warns = append(*warns, fmt.Sprintf("%s: %v", label, err))
		return missing()
	}
	return Measure{Status: StatusValue, Value: f}
}

// ratio calcule num/den ; un dénominateur nul donne missing et un avertissement.
func ratio(num, den decimal.Decimal, warns *[]string, label string) Measure {
	if den.IsZero() {
		*warns = append(*warns, fmt.Sprintf("%s: dénominateur nul", label))
		return missing()
	}
	return value(num.Div(den), warns, label)
}

// growthMeasures calcule la moyenne (tronquée) des taux et le nombre de hausses d'une série.
func growthMeasures(label string, s Series, h, trimMin int, warns *[]string) (Measure, IntMeasure) {
	if len(s) == 0 {
		return missing(), IntMeasure{Status: StatusMissing}
	}
	w, ok := Window(s, h)
	if !ok {
		return insufficient(), IntMeasure{Status: StatusInsufficient}
	}
	rs, ws := rates(label, w)
	*warns = append(*warns, ws...)
	if len(rs) == 0 {
		return missing(), IntMeasure{Status: StatusMissing}
	}
	m, err := TrimmedMean(rs, trimMin)
	if err != nil {
		*warns = append(*warns, fmt.Sprintf("%s: %v", label, err))
		return missing(), IntMeasure{Status: StatusMissing}
	}
	return Measure{Status: StatusValue, Value: m}, IntMeasure{Status: StatusValue, Value: CountIncreases(rs)}
}

// ComputeMetrics calcule les mesures de chaque horizon d'opts.Horizons, dans l'ordre.
func ComputeMetrics(in Inputs, opts Options) []Metrics {
	eps, dps, ay, sh := normalize(in.EPS), normalize(in.DPS), normalize(in.AnnualYield), normalize(in.Shares)
	out := make([]Metrics, 0, len(opts.Horizons))
	for _, h := range opts.Horizons {
		m := Metrics{Horizon: h}
		w := &m.Warnings
		m.EPSGrowth, m.EPSRises = growthMeasures("BPA", eps, h, opts.TrimMinRates, w)
		m.DPSGrowth, _ = growthMeasures("dividende", dps, h, opts.TrimMinRates, w)
		staticMeasures(&m, in, eps, dps, sh, opts)
		m.AverageYield = averageYield(ay, h, w)
		if n := len(eps); in.CurrentYear > 0 && n > 0 && eps[n-1].Year < in.CurrentYear-2 {
			*w = append(*w, fmt.Sprintf("BPA: dernier exercice %d très ancien pour %d", eps[n-1].Year, in.CurrentYear))
		}
		out = append(out, m)
	}
	return out
}

func averageYield(ay Series, h int, warns *[]string) Measure {
	if len(ay) == 0 {
		return missing()
	}
	w, ok := Window(ay, h)
	if !ok {
		return insufficient()
	}
	sum := decimal.Zero
	for _, p := range w {
		sum = sum.Add(p.Value)
	}
	return value(sum.Div(decimal.NewFromInt(int64(len(w)))), warns, "rendement moyen")
}

func staticMeasures(m *Metrics, in Inputs, eps, dps, sh Series, opts Options) {
	w := &m.Warnings
	s := in.Snapshot
	fx := decimal.NewFromInt(1)
	if s.FX != nil {
		fx = *s.FX
	}
	m.PayoutRatio, m.DividendChange, m.PE, m.PB = missing(), missing(), missing(), missing()
	m.DebtToEquity, m.ShareChange, m.CurrentYield = missing(), missing(), missing()

	var lastEPS, lastDPS decimal.Decimal
	haveEPS, haveDPS := false, false
	if n := len(eps); n > 0 {
		lastEPS, haveEPS = eps[n-1].Value, true
		lastDPS, haveDPS = dps.at(eps[n-1].Year)
	}
	if haveEPS && haveDPS {
		m.PayoutRatio = ratio(lastDPS, lastEPS, w, "ratio de distribution")
	}
	if s.LongTermDebt != nil && s.Equity != nil {
		m.DebtToEquity = ratio(*s.LongTermDebt, *s.Equity, w, "dette sur capitaux propres")
	}
	if s.ForwardDividend != nil && len(dps) > 0 {
		last := dps[len(dps)-1].Value
		if last.IsZero() {
			*w = append(*w, "variation du dividende: dernier dividende nul")
		} else {
			m.DividendChange = value(s.ForwardDividend.Div(last).Sub(decimal.NewFromInt(1)), w, "variation du dividende")
		}
	}
	m.ShareChange = shareChange(sh, opts.BuybackYears, w)
	if s.Price != nil {
		if haveEPS {
			m.PE = ratio(*s.Price, lastEPS.Mul(fx), w, "P/E")
		}
		if s.BookValuePerShare != nil {
			m.PB = ratio(*s.Price, s.BookValuePerShare.Mul(fx), w, "P/B")
		}
		if s.ForwardDividend != nil {
			m.CurrentYield = ratio(s.ForwardDividend.Mul(fx), *s.Price, w, "rendement actuel")
		}
	}
}

func shareChange(sh Series, years int, warns *[]string) Measure {
	if len(sh) == 0 {
		return missing()
	}
	if years < 1 {
		years = 1
	}
	last := sh[len(sh)-1]
	prev, ok := sh.at(last.Year - years)
	if !ok {
		return insufficient()
	}
	if prev.IsZero() {
		*warns = append(*warns, "variation des actions: valeur précédente nulle")
		return missing()
	}
	return value(last.Value.Div(prev).Sub(decimal.NewFromInt(1)), warns, "variation des actions")
}
