package engine

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/shopspring/decimal"
)

func val(x float64) Measure { return Measure{Status: StatusValue, Value: x} }

// perfect retourne des mesures qui réussissent les dix critères au seuil strict.
func perfect(h int) Metrics {
	return Metrics{
		Horizon:        h,
		EPSGrowth:      val(0.10),
		EPSRises:       IntMeasure{Status: StatusValue, Value: h},
		DPSGrowth:      val(0.10),
		PayoutRatio:    val(0.50),
		DebtToEquity:   val(0.30),
		DividendChange: val(0.02),
		ShareChange:    val(-0.01),
		PE:             val(15),
		CurrentYield:   val(0.04),
		AverageYield:   val(0.03),
		PB:             val(2),
	}
}

var openGate = Gate{AnswerYes, AnswerYes, AnswerYes, AnswerYes}

func mustRules(t *testing.T) *Rules {
	t.Helper()
	r, err := DefaultRules()
	if err != nil {
		t.Fatal(err)
	}
	return r
}

func statusOf(t *testing.T, h HorizonResult, id string) Status {
	t.Helper()
	for _, c := range h.Criteria {
		if c.ID == id {
			return c.Status
		}
	}
	t.Fatalf("critère %s absent", id)
	return ""
}

func TestCriterionStatuses(t *testing.T) {
	t.Parallel()
	r := mustRules(t)
	tests := []struct {
		name   string
		id     string
		mutate func(*Metrics)
		fin    bool
		want   Status
	}{
		{"parfait", "5a", func(*Metrics) {}, false, StatusPassed},
		{"5a limite", "5a", func(m *Metrics) { m.EPSGrowth = val(0.07) }, false, StatusBorderline},
		{"5a échoue", "5a", func(m *Metrics) { m.EPSGrowth = val(0.05) }, false, StatusFailed},
		{"5b limite", "5b", func(m *Metrics) { m.EPSRises = IntMeasure{Status: StatusValue, Value: 6} }, false, StatusBorderline},
		{"5b échoue", "5b", func(m *Metrics) { m.EPSRises = IntMeasure{Status: StatusValue, Value: 5} }, false, StatusFailed},
		{"5b horizon inconnu", "5b", func(m *Metrics) { m.Horizon = 7 }, false, StatusFailed},
		{"6 limite", "6", func(m *Metrics) { m.DPSGrowth = val(0.06) }, false, StatusBorderline},
		{"7 dividende nul", "7", func(m *Metrics) { m.PayoutRatio = val(0) }, false, StatusFailed},
		{"7 limite", "7", func(m *Metrics) { m.PayoutRatio = val(0.80) }, false, StatusBorderline},
		{"8 sans dette (D2)", "8", func(m *Metrics) { m.DebtToEquity = val(0) }, false, StatusPassed},
		{"8 capitaux négatifs", "8", func(m *Metrics) { m.DebtToEquity = val(-0.5) }, false, StatusFailed},
		{"8 limite", "8", func(m *Metrics) { m.DebtToEquity = val(0.9) }, false, StatusBorderline},
		{"8 financière", "8", func(m *Metrics) { m.DebtToEquity = val(5) }, true, StatusExempt},
		{"9 gel", "9", func(m *Metrics) { m.DividendChange = val(0) }, false, StatusPassed},
		{"9 coupe", "9", func(m *Metrics) { m.DividendChange = val(-0.1) }, false, StatusFailed},
		{"9 manquant", "9", func(m *Metrics) { m.DividendChange = Measure{Status: StatusMissing} }, false, StatusMissingData},
		{"10 pas de rachat", "10", func(m *Metrics) { m.ShareChange = val(0) }, false, StatusFailed},
		{"10 insuffisant", "10", func(m *Metrics) { m.ShareChange = Measure{Status: StatusInsufficient} }, false, StatusNotEnough},
		{"11a BPA négatif", "11a", func(m *Metrics) { m.PE = val(-12) }, false, StatusFailed},
		{"11a limite", "11a", func(m *Metrics) { m.PE = val(30) }, false, StatusBorderline},
		{"11a trop cher", "11a", func(m *Metrics) { m.PE = val(40) }, false, StatusFailed},
		{"11b égalité", "11b", func(m *Metrics) { m.CurrentYield = val(0.03) }, false, StatusFailed},
		{"11b manquant", "11b", func(m *Metrics) { m.CurrentYield = Measure{Status: StatusMissing} }, false, StatusMissingData},
		{"11b manquant l'emporte", "11b", func(m *Metrics) {
			m.CurrentYield = Measure{Status: StatusMissing}
			m.AverageYield = Measure{Status: StatusInsufficient}
		}, false, StatusMissingData},
		{"11b insuffisant", "11b", func(m *Metrics) { m.AverageYield = Measure{Status: StatusInsufficient} }, false, StatusNotEnough},
		{"11c limite", "11c", func(m *Metrics) { m.PB = val(5) }, false, StatusBorderline},
		{"11c négatif", "11c", func(m *Metrics) { m.PB = val(-1) }, false, StatusFailed},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			m := perfect(20)
			tc.mutate(&m)
			ev, err := Score([]Metrics{m}, r, Context{Gate: openGate, Financial: tc.fin})
			if err != nil {
				t.Fatal(err)
			}
			if got := statusOf(t, ev.Horizons[0], tc.id); got != tc.want {
				t.Errorf("critère %s: %s, attendu %s", tc.id, got, tc.want)
			}
		})
	}
}

func TestNegativeEPSFails11aInBothProfiles(t *testing.T) {
	t.Parallel()
	m := perfect(20)
	m.PE = val(-8)
	ev, err := Score([]Metrics{m}, mustRules(t), Context{Gate: openGate})
	if err != nil {
		t.Fatal(err)
	}
	h := ev.Horizons[0]
	if h.Strict.Failures != 1 || h.Relaxed.Failures != 1 {
		t.Errorf("échecs strict/assoupli = %d/%d, attendu 1/1", h.Strict.Failures, h.Relaxed.Failures)
	}
	if h.Strict.Price.Passed != 2 || h.Relaxed.Price.Passed != 2 {
		t.Errorf("sous-score prix %v / %v, attendu 2/3", h.Strict.Price, h.Relaxed.Price)
	}
}

func TestScores(t *testing.T) {
	t.Parallel()
	m := perfect(20)
	m.EPSGrowth = val(0.07) // limite
	m.PE = val(30)          // limite
	m.PB = val(10)          // échoue
	m.DebtToEquity = val(9) // échoue, mais exempté
	m.CurrentYield = Measure{Status: StatusMissing}
	ev, err := Score([]Metrics{m}, mustRules(t), Context{Gate: openGate, Financial: true})
	if err != nil {
		t.Fatal(err)
	}
	h := ev.Horizons[0]
	want := map[string]ProfileScores{
		"strict":   {Total: Fraction{5, 9}, Quality: Fraction{5, 6}, Price: Fraction{0, 3}, Failures: 4},
		"assoupli": {Total: Fraction{7, 9}, Quality: Fraction{6, 6}, Price: Fraction{1, 3}, Failures: 2},
	}
	if h.Strict != want["strict"] {
		t.Errorf("strict = %+v, attendu %+v", h.Strict, want["strict"])
	}
	if h.Relaxed != want["assoupli"] {
		t.Errorf("assoupli = %+v, attendu %+v", h.Relaxed, want["assoupli"])
	}
}

func TestVerdict(t *testing.T) {
	t.Parallel()
	r := mustRules(t)
	relaxedOnly := func(h int) Metrics {
		m := perfect(h)
		m.PE = val(30)
		return m
	}
	failing := func(h int) Metrics {
		m := perfect(h)
		m.PB = val(50)
		return m
	}
	tests := []struct {
		name    string
		gate    Gate
		ms      []Metrics
		want    Verdict
		horizon int
	}{
		{"candidate", openGate, []Metrics{perfect(5), perfect(10)}, VerdictCandidate, 10},
		{"candidate sur le 20 ans", openGate, []Metrics{failing(10), perfect(20)}, VerdictCandidate, 20},
		{"5 ans parfait seul : rejetée", openGate, []Metrics{perfect(5), failing(10)}, VerdictRejected, 0},
		{"watch", openGate, []Metrics{perfect(5), relaxedOnly(10)}, VerdictWatch, 10},
		{"candidate l'emporte sur watch", openGate, []Metrics{relaxedOnly(10), perfect(20)}, VerdictCandidate, 20},
		{"rejected", openGate, []Metrics{failing(10), failing(20)}, VerdictRejected, 0},
		{"une règle à non : excluded", Gate{AnswerYes, AnswerNo, AnswerYes, AnswerYes}, []Metrics{perfect(20)}, VerdictExcluded, 0},
		{"règle sans réponse : excluded", Gate{AnswerYes, AnswerYes, AnswerYes, ""}, []Metrics{perfect(20)}, VerdictExcluded, 0},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			ev, err := Score(tc.ms, r, Context{Gate: tc.gate})
			if err != nil {
				t.Fatal(err)
			}
			if ev.Verdict != tc.want {
				t.Fatalf("verdict %s, attendu %s", ev.Verdict, tc.want)
			}
			if tc.horizon == 0 && ev.VerdictHorizon != nil {
				t.Errorf("horizon de verdict inattendu: %d", *ev.VerdictHorizon)
			}
			if tc.horizon != 0 && (ev.VerdictHorizon == nil || *ev.VerdictHorizon != tc.horizon) {
				t.Errorf("horizon de verdict = %v, attendu %d", ev.VerdictHorizon, tc.horizon)
			}
		})
	}
}

func TestIndicativeHorizon(t *testing.T) {
	t.Parallel()
	ev, err := Score([]Metrics{perfect(5), perfect(10)}, mustRules(t), Context{Gate: openGate})
	if err != nil {
		t.Fatal(err)
	}
	if !ev.Horizons[0].Indicative || ev.Horizons[1].Indicative {
		t.Errorf("indicatif: 5 ans %v, 10 ans %v", ev.Horizons[0].Indicative, ev.Horizons[1].Indicative)
	}
}

func TestNilRules(t *testing.T) {
	t.Parallel()
	if _, err := Score(nil, nil, Context{}); err == nil {
		t.Error("Score: erreur attendue")
	}
	if _, err := Evaluate(Inputs{}, nil, Context{}); err == nil {
		t.Error("Evaluate: erreur attendue")
	}
}

func TestScoreMissingCriterion(t *testing.T) {
	t.Parallel()
	r := mustRules(t)
	delete(r.Criteria, "9")
	if _, err := Score([]Metrics{perfect(20)}, r, Context{}); err == nil {
		t.Error("erreur attendue pour un critère absent du référentiel")
	}
}

func yearly(vals ...float64) Series {
	var s Series
	for i, v := range vals {
		s = append(s, Point{Year: 2000 + i, Value: decimal.NewFromFloat(v)})
	}
	return s
}

func TestEvaluateEndToEnd(t *testing.T) {
	t.Parallel()
	r := mustRules(t)
	price, fwd := decimal.NewFromInt(30), decimal.NewFromFloat(1.2)
	debt, eq, bv := decimal.NewFromInt(10), decimal.NewFromInt(100), decimal.NewFromInt(15)
	in := Inputs{
		EPS:         yearly(1, 1.1, 1.2, 1.3, 1.4, 1.5, 1.6, 1.7, 1.8, 1.9, 2.0),
		DPS:         yearly(0.5, 0.55, 0.6, 0.65, 0.7, 0.75, 0.8, 0.85, 0.9, 0.95, 1.0),
		AnnualYield: yearly(0.02, 0.02, 0.02, 0.02, 0.02, 0.02, 0.02, 0.02, 0.02, 0.02, 0.02),
		Shares:      yearly(110, 109, 108, 107, 106, 105, 104, 103, 102, 101, 100),
		Snapshot: Snapshot{Price: &price, ForwardDividend: &fwd, LongTermDebt: &debt, Equity: &eq,
			BookValuePerShare: &bv},
		CurrentYear: 2011,
	}
	ev, err := Evaluate(in, r, Context{Gate: openGate})
	if err != nil {
		t.Fatal(err)
	}
	if len(ev.Horizons) != 4 {
		t.Fatalf("horizons = %d, attendu 4", len(ev.Horizons))
	}
	h5, h10, h15 := ev.Horizons[0], ev.Horizons[1], ev.Horizons[2]
	if statusOf(t, h5, "5a") != StatusFailed && statusOf(t, h5, "5a") != StatusBorderline && statusOf(t, h5, "5a") != StatusPassed {
		t.Errorf("5 ans : statut 5a inattendu")
	}
	if got := statusOf(t, h10, "5b"); got != StatusPassed {
		t.Errorf("10 ans 5b = %s", got)
	}
	if got := statusOf(t, h15, "5a"); got != StatusNotEnough {
		t.Errorf("15 ans 5a = %s, attendu insufficient", got)
	}
	if ev.Verdict == VerdictExcluded {
		t.Error("barrière ouverte : verdict exclu inattendu")
	}
	if ev.RulesVersion != r.Version || ev.RulesHash != r.Hash {
		t.Error("version ou empreinte du référentiel non reportée")
	}
}

func TestJSONStable(t *testing.T) {
	t.Parallel()
	ev, err := Score([]Metrics{perfect(10)}, mustRules(t), Context{Gate: openGate})
	if err != nil {
		t.Fatal(err)
	}
	a, err := json.Marshal(ev)
	if err != nil {
		t.Fatal(err)
	}
	ev2, err := Score([]Metrics{perfect(10)}, mustRules(t), Context{Gate: openGate})
	if err != nil {
		t.Fatal(err)
	}
	b, _ := json.Marshal(ev2)
	if string(a) != string(b) {
		t.Fatal("sérialisation non déterministe")
	}
	s := string(a)
	order := []string{`"rules_version"`, `"rules_hash"`, `"verdict"`, `"verdict_horizon"`, `"gate"`, `"financial"`, `"horizons"`}
	pos := -1
	for _, k := range order {
		i := strings.Index(s, k)
		if i <= pos {
			t.Fatalf("champ %s hors d'ordre dans %s", k, s)
		}
		pos = i
	}
	if !strings.Contains(s, `"status":"passed"`) || !strings.Contains(s, `"min_rises":4`) {
		t.Errorf("contenu inattendu: %s", s)
	}
}

func TestGateOpen(t *testing.T) {
	t.Parallel()
	if !openGate.Open() || (Gate{}).Open() {
		t.Error("Gate.Open incorrect")
	}
}
