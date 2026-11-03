package engine

import (
	"encoding/json"
	"errors"
	"math"
	"os"
	"path/filepath"
	"testing"

	"github.com/shopspring/decimal"
)

type oracleExpect struct {
	Average *float64 `json:"moyenne"`
	Rises   *int     `json:"hausses"`
	Streak  *int     `json:"hausses_consecutives"`
	Error   *string  `json:"erreur"`
}

type oracleCase struct {
	ID        string       `json:"id"`
	Scenario  string       `json:"scenario"`
	EPS       []*float64   `json:"eps"`
	Dividends []*float64   `json:"dividends"`
	WantEPS   oracleExpect `json:"attendu_eps"`
	WantDPS   oracleExpect `json:"attendu_dividendes"`
}

func oracleCells(xs []*float64) CompatCells {
	cells := make(CompatCells, len(xs))
	for i, x := range xs {
		if x != nil {
			d := decimal.NewFromFloat(*x)
			cells[i] = &d
		}
	}
	return cells
}

func checkOracle(t *testing.T, name string, got CompatGrowth, err error, want oracleExpect, count *int) {
	t.Helper()
	if want.Error != nil {
		if *want.Error != "#DIV/0!" {
			t.Fatalf("%s: erreur d'oracle inattendue %q", name, *want.Error)
		}
		if !errors.Is(err, ErrSheetDivZero) {
			t.Errorf("%s: erreur %v, attendu ErrSheetDivZero", name, err)
		}
		return
	}
	if err != nil {
		t.Fatalf("%s: erreur inattendue %v", name, err)
	}
	if want.Average == nil {
		if !got.Blank {
			t.Errorf("%s: attendu une cellule vide, obtenu %+v", name, got)
		}
		return
	}
	if got.Blank {
		t.Fatalf("%s: cellule vide, attendu %v", name, *want.Average)
	}
	if math.Abs(got.Average-*want.Average) > 1e-9 {
		t.Errorf("%s: moyenne %v, attendu %v", name, got.Average, *want.Average)
	}
	if count == nil || got.Count != *count {
		t.Errorf("%s: compte %d, attendu %v", name, got.Count, count)
	}
}

// TestCompatOracle rejoue les 200 cas calculés par LibreOffice sur la vraie feuille (P1-07).
func TestCompatOracle(t *testing.T) {
	t.Parallel()
	raw, err := os.ReadFile(filepath.Join("..", "..", "..", "..", "..", "testdata", "oracle", "cases.json"))
	if err != nil {
		t.Fatalf("lecture de l'oracle: %v", err)
	}
	var doc struct {
		Cases []oracleCase `json:"cas"`
	}
	if err := json.Unmarshal(raw, &doc); err != nil {
		t.Fatalf("décodage: %v", err)
	}
	if len(doc.Cases) != 200 {
		t.Fatalf("cas de l'oracle: %d, attendu 200", len(doc.Cases))
	}
	rules, err := DefaultRules()
	if err != nil {
		t.Fatal(err)
	}
	div := rules.Compat.Divisor
	divZero := 0
	for _, c := range doc.Cases {
		eps, errE := CompatEPSGrowth(oracleCells(c.EPS), div)
		checkOracle(t, c.ID+"/BPA", eps, errE, c.WantEPS, c.WantEPS.Rises)
		dps, errD := CompatDividendGrowth(oracleCells(c.Dividends), div)
		checkOracle(t, c.ID+"/dividendes", dps, errD, c.WantDPS, c.WantDPS.Streak)
		if c.WantEPS.Error != nil {
			divZero++
		}
	}
	if divZero == 0 {
		t.Error("aucun cas #DIV/0! dans l'oracle")
	}
	t.Logf("%d cas reproduits dont %d en #DIV/0!", len(doc.Cases), divZero)
}
