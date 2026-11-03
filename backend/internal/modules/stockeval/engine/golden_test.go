package engine

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"testing"
)

type goldenRow struct {
	Symbol string `json:"symbol"`
	Row    int    `json:"row"`
	Inputs struct {
		EPSGrowth      float64 `json:"eps_growth_avg"`
		EPSIncreases   float64 `json:"eps_increases"`
		DPSGrowth      float64 `json:"dps_growth_avg"`
		Payout         float64 `json:"payout_ratio"`
		DebtToEquity   float64 `json:"lt_debt_to_equity_raw"`
		DividendChange float64 `json:"dividend_change"`
		Buyback        string  `json:"buyback"`
		PE             float64 `json:"pe"`
		CurrentYield   float64 `json:"current_yield"`
		AvgYield       float64 `json:"avg_yield"`
		PB             float64 `json:"pb"`
	} `json:"inputs"`
	Strict       map[string]int `json:"expected_strict_failures"`
	StrictCount  int            `json:"expected_strict_failure_count"`
	Relaxed      map[string]int `json:"expected_relaxed_failures"`
	RelaxedCount int            `json:"expected_relaxed_failure_count"`
}

func (g goldenRow) metrics() Metrics {
	v := func(x float64) Measure { return Measure{Status: StatusValue, Value: x} }
	i := g.Inputs
	share := 0.0 // « no » : aucun rachat
	if i.Buyback == "yes" {
		share = -0.01
	}
	return Metrics{
		Horizon:        20,
		EPSGrowth:      v(i.EPSGrowth),
		EPSRises:       IntMeasure{Status: StatusValue, Value: int(i.EPSIncreases)},
		DPSGrowth:      v(i.DPSGrowth),
		PayoutRatio:    v(i.Payout),
		DebtToEquity:   v(i.DebtToEquity),
		DividendChange: v(i.DividendChange),
		ShareChange:    v(share),
		PE:             v(i.PE),
		CurrentYield:   v(i.CurrentYield),
		AverageYield:   v(i.AvgYield),
		PB:             v(i.PB),
	}
}

func TestGoldenSamples(t *testing.T) {
	t.Parallel()
	raw, err := os.ReadFile(filepath.Join("..", "..", "..", "..", "..", "testdata", "golden", "simply_investing_samples.json"))
	if err != nil {
		t.Fatalf("lecture du JSON de référence: %v", err)
	}
	var doc struct {
		Rows []goldenRow `json:"rows"`
	}
	if err := json.Unmarshal(raw, &doc); err != nil {
		t.Fatalf("décodage: %v", err)
	}
	if len(doc.Rows) != 15 {
		t.Fatalf("lignes de référence: %d, attendu 15", len(doc.Rows))
	}
	rules, err := DefaultRules()
	if err != nil {
		t.Fatal(err)
	}
	for _, row := range doc.Rows {
		t.Run(fmt.Sprintf("%s_ligne%d", row.Symbol, row.Row), func(t *testing.T) {
			t.Parallel()
			ev, err := Score([]Metrics{row.metrics()}, rules, Context{})
			if err != nil {
				t.Fatal(err)
			}
			h := ev.Horizons[0]
			for _, c := range h.Criteria {
				if got, want := c.Status == StatusPassed, row.Strict[c.ID] == 0; got != want {
					t.Errorf("strict %s: statut %s, échecs attendus %d", c.ID, c.Status, row.Strict[c.ID])
				}
				if got, want := c.Status == StatusPassed || c.Status == StatusBorderline, row.Relaxed[c.ID] == 0; got != want {
					t.Errorf("assoupli %s: statut %s, échecs attendus %d", c.ID, c.Status, row.Relaxed[c.ID])
				}
			}
			if h.Strict.Failures != row.StrictCount {
				t.Errorf("compte strict %d, attendu %d", h.Strict.Failures, row.StrictCount)
			}
			if h.Relaxed.Failures != row.RelaxedCount {
				t.Errorf("compte assoupli %d, attendu %d", h.Relaxed.Failures, row.RelaxedCount)
			}
		})
	}
}
