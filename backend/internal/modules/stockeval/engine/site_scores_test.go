package engine

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

// D304 : 42 titres (TSX et États-Unis) relevés sur le site Simply Investing. Le site ne donne que des agrégats
// (croissance moyenne, hausses, ratios), pas les séries annuelles : ce test valide donc les seuils de rules.yaml
// et le pointage strict, pas le calcul des moyennes tronquées.

type siteScore struct {
	Stocks []struct {
		Symbol   string `json:"symbol"`
		Snapshot struct {
			PayoutPct       float64 `json:"payoutRatioPct"`
			LTDebtEquityPct float64 `json:"longTermDebtToEquityPct"`
			DividendChgPct  float64 `json:"annualDividendChangePct"`
			SharesCurrent   float64 `json:"sharesCurrentYear"`
			SharesPrevious  float64 `json:"sharesPreviousYear"`
			PE              float64 `json:"peRatio"`
			PB              float64 `json:"pbRatio"`
		} `json:"snapshot"`
		Horizons []struct {
			Years            int     `json:"years"`
			EpsGrowthPct     float64 `json:"averageEpsGrowthPct"`
			EpsIncreases     int     `json:"epsIncreases"`
			DividendGrowth   float64 `json:"averageDividendGrowthPct"`
			YieldVsAveragePc float64 `json:"dividendYieldVsAveragePct"`
			SiScore          int     `json:"siScore"`
		} `json:"horizons"`
	} `json:"stocks"`
}

// passes applique un seuil strict (gt, ge, le) à une valeur.
func passes(t Threshold, v float64) bool {
	if t.Gt != nil && !(v > *t.Gt) {
		return false
	}
	if t.Ge != nil && !(v >= *t.Ge) {
		return false
	}
	if t.Le != nil && !(v <= *t.Le) {
		return false
	}
	return true
}

func TestSiteScoresStrict(t *testing.T) {
	t.Parallel()
	rules, err := DefaultRules()
	if err != nil {
		t.Fatal(err)
	}
	strict := func(id string) Threshold {
		c, ok := rules.Criterion(id)
		if !ok {
			t.Fatalf("critère %s absent de rules.yaml", id)
		}
		return c.Strict
	}
	raw, err := os.ReadFile(filepath.Join("..", "..", "..", "..", "..", "testdata", "fixtures", "simply_investing_site_scores.json"))
	if err != nil {
		t.Fatal(err)
	}
	var doc siteScore
	if err := json.Unmarshal(raw, &doc); err != nil {
		t.Fatal(err)
	}
	if len(doc.Stocks) != 42 {
		t.Fatalf("42 titres attendus, %d lus", len(doc.Stocks))
	}
	for _, s := range doc.Stocks {
		for _, h := range s.Horizons {
			fails := 0
			check := func(ok bool) {
				if !ok {
					fails++
				}
			}
			check(passes(strict("5a"), h.EpsGrowthPct/100))
			check(h.EpsIncreases >= strict("5b").MinRises[h.Years])
			check(passes(strict("6"), h.DividendGrowth/100))
			check(passes(strict("7"), s.Snapshot.PayoutPct/100))
			check(passes(strict("8"), s.Snapshot.LTDebtEquityPct/100))
			check(passes(strict("9"), s.Snapshot.DividendChgPct/100))
			check(s.Snapshot.SharesCurrent < s.Snapshot.SharesPrevious) // règle 10 : en baisse
			check(passes(strict("11a"), s.Snapshot.PE))
			check(h.YieldVsAveragePc > 0) // règle 11b : rendement actuel strictement supérieur à la moyenne
			check(passes(strict("11c"), s.Snapshot.PB))
			if got := 10 - fails; got != h.SiScore {
				t.Errorf("%s %d ans : score %d, site %d", s.Symbol, h.Years, got, h.SiScore)
			}
		}
	}
}
