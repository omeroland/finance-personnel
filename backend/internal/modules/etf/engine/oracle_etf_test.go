package engine_test

// Rejeu des cas ACHATS et RÉÉQUILIBRAGE de l'oracle (testdata/oracle/etf_cases.json, P2-07) sur
// PlanPurchase et Rebalance. Critère reporté de P2-04 et P2-05.
//
// Règle : un cas « fidèle » doit être égal à l'outil ; un cas « écart » doit donner `attendu_app`.
// Là où les moteurs diffèrent VOLONTAIREMENT de la convention du modèle de l'oracle (D331 à D333 contre
// D360 à D372), l'écart est inscrit dans `knownDivergences` avec son motif et la valeur attendue du
// moteur : le cas n'est pas affaibli, il est épinglé (D415).

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"testing"

	"github.com/shopspring/decimal"

	"github.com/omeroland/finance-privee/backend/internal/modules/etf/engine"
)

const oraclePath = "../../../../../testdata/oracle/etf_cases.json"

// catalogue fictif de l'oracle (tools/oracle/etf_scenarios.py, CATALOGUE) : actions (CA, US, intl),
// obligations (CAD, autres). Le reste, 1 − somme, est « autres actifs » (D340).
type synFund struct{ ca, us, intl, oblCad, oblOther float64 }

var synCatalogue = map[string]synFund{
	"SYN-CA":    {ca: 1},
	"SYN-US":    {us: 1},
	"SYN-INT":   {intl: 1},
	"SYN-OBL":   {oblCad: 1},
	"SYN-OBX":   {oblOther: 1},
	"SYN-BAL":   {ca: 0.15, us: 0.25, intl: 0.20, oblCad: 0.30, oblOther: 0.10},
	"SYN-CROIS": {ca: 0.20, us: 0.40, intl: 0.30, oblCad: 0.10},
	"SYN-GLB":   {oblCad: 0.30, oblOther: 0.70},
	"SYN-OR":    {},
	"SYN-IMM":   {ca: 0.30},
}

func (f synFund) allocation(t *testing.T) engine.Allocation {
	t.Helper()
	a, err := engine.CompleteAllocation(engine.Allocation{
		EquityCA: f.ca, EquityUS: f.us, EquityIntl: f.intl, BondCA: f.oblCad, BondOther: f.oblOther,
	})
	if err != nil {
		t.Fatal(err)
	}
	return a
}

type oracleFile struct {
	Cas []oracleCase `json:"cas"`
}

type oracleCase struct {
	ID          string          `json:"id"`
	Onglet      string          `json:"onglet"`
	Etiquette   string          `json:"etiquette"`
	Ecart       *string         `json:"ecart"`
	Entrees     json.RawMessage `json:"entrees"`
	AttenduApp  json.RawMessage `json:"attendu_app"`
	Comparaison struct {
		Egal      []string `json:"egal"`
		Different []string `json:"different"`
	} `json:"comparaison"`
}

func loadOracle(t *testing.T) []oracleCase {
	t.Helper()
	raw, err := os.ReadFile(oraclePath)
	if err != nil {
		t.Fatal(err)
	}
	var f oracleFile
	dec := json.NewDecoder(bytes.NewReader(raw))
	if err := dec.Decode(&f); err != nil {
		t.Fatal(err)
	}
	return f.Cas
}

func dnum(t *testing.T, n json.Number) decimal.Decimal {
	t.Helper()
	d, err := decimal.NewFromString(n.String())
	if err != nil {
		t.Fatal(err)
	}
	return d
}

// --------------------------------------------------------------------------- ACHATS

type achatsIn struct {
	Montant json.Number `json:"montant"`
	Fonds   []struct {
		Symbole string      `json:"symbole"`
		Pct     float64     `json:"pct"`
		Prix    json.Number `json:"prix"`
	} `json:"fonds"`
}

type achatsOut struct {
	Parts            []int64     `json:"parts"`
	PartsTotal       int64       `json:"parts_total"`
	DepenseTotale    json.Number `json:"depense_totale"`
	EncaisseRestante json.Number `json:"encaisse_restante"`
}

// achatsDivergence épingle un écart voulu : D361 répartit le reliquat sur des parts entières
// supplémentaires (une part à la fois, au fonds le plus sous-pondéré), l'oracle (D331) laisse le
// reste de chaque ligne en encaisse sans redistribution.
type achatsDivergence struct {
	Parts []int64 // parts par fonds, dans l'ordre des lignes du cas
	Cash  string  // encaisse restante du moteur
}

var achatsDivergences = map[string]achatsDivergence{
	"achats-fidele-04":       {Parts: []int64{158, 134, 187, 0}, Cash: "22.36"},
	"achats-ecart-parts-02":  {Parts: []int64{155, 141, 170, 202}, Cash: "23.96"},
	"achats-ecart-parts-03":  {Parts: []int64{1, 1}, Cash: "50"},
	"achats-ecart-totaux-02": {Parts: []int64{337, 248, 304}, Cash: "6.7"},
}

func TestOracleAchats(t *testing.T) {
	t.Parallel()
	replayed := 0
	for _, c := range loadOracle(t) {
		if c.Onglet != "ACHATS" {
			continue
		}
		replayed++
		t.Run(c.ID, func(t *testing.T) {
			var in achatsIn
			var want achatsOut
			mustDecode(t, c.Entrees, &in)
			mustDecode(t, c.AttenduApp, &want)

			sum := 0.0
			pin := engine.PurchaseInput{Amount: dnum(t, in.Montant)}
			for _, f := range in.Fonds {
				sum += f.Pct
				pin.Funds = append(pin.Funds, engine.PurchaseFund{ID: f.Symbole, Weight: f.Pct, Price: dnum(t, f.Prix)})
			}
			if sum < 1 {
				pin.CashWeight = 1 - sum
			}
			plan, err := engine.PlanPurchase(pin)
			if err != nil {
				t.Fatal(err)
			}
			shares := map[string]int64{}
			var total int64
			for _, o := range plan.Orders {
				shares[o.Fund] = o.Shares
				total += o.Shares
			}
			got := make([]int64, len(in.Fonds))
			for i, f := range in.Fonds {
				got[i] = shares[f.Symbole]
			}
			// Invariant de tous les cas : le plan boucle (investi + encaisse = montant).
			if !plan.Invested.Add(plan.Cash).Equal(pin.Amount) {
				t.Fatalf("investi %s + encaisse %s != montant %s", plan.Invested, plan.Cash, pin.Amount)
			}
			if d, ok := achatsDivergences[c.ID]; ok {
				if fmt.Sprint(got) != fmt.Sprint(d.Parts) || plan.Cash.String() != d.Cash {
					t.Fatalf("écart épinglé : parts %v encaisse %s, attendu %v / %s", got, plan.Cash, d.Parts, d.Cash)
				}
				// L'écart va dans un seul sens : jamais moins de parts, jamais plus d'encaisse que l'oracle.
				for i := range got {
					if got[i] < want.Parts[i] {
						t.Fatalf("fonds %d : %d parts < %d (oracle)", i, got[i], want.Parts[i])
					}
				}
				if plan.Cash.GreaterThan(dnum(t, want.EncaisseRestante)) {
					t.Fatalf("encaisse %s > oracle %s", plan.Cash, want.EncaisseRestante)
				}
				return
			}
			if fmt.Sprint(got) != fmt.Sprint(want.Parts) || total != want.PartsTotal {
				t.Fatalf("parts %v (total %d, encaisse %s), attendu %v (total %d)", got, total, plan.Cash, want.Parts, want.PartsTotal)
			}
			if !plan.Invested.Equal(dnum(t, want.DepenseTotale)) {
				t.Fatalf("dépense %s, attendu %s", plan.Invested, want.DepenseTotale)
			}
			if !plan.Cash.Equal(dnum(t, want.EncaisseRestante)) {
				t.Fatalf("encaisse %s, attendu %s", plan.Cash, want.EncaisseRestante)
			}
		})
	}
	if replayed != 9 {
		t.Fatalf("%d cas ACHATS rejoués, 9 attendus", replayed)
	}
	t.Cleanup(func() {
		t.Logf("ACHATS : %d cas, %d écarts voulus épinglés (D415)", replayed, len(achatsDivergences))
	})
}

// --------------------------------------------------------------------------- RÉÉQUILIBRAGE

type rebalIn struct {
	CibleEncaisse json.Number `json:"cible_encaisse"`
	Encaisse      json.Number `json:"encaisse"`
	Fonds         []struct {
		Symbole  string       `json:"symbole"`
		PctCible float64      `json:"pct_cible"`
		Valeur   json.Number  `json:"valeur"`
		Prix     *json.Number `json:"prix"`
	} `json:"fonds"`
}

type rebalStatus struct {
	Cible  float64 `json:"cible"`
	Pct    float64 `json:"pct"`
	Statut string  `json:"statut"`
	Ecart  float64 `json:"ecart"`
}

type rebalOut struct {
	Total    float64       `json:"total"`
	Encaisse rebalStatus   `json:"encaisse"`
	Fonds    []rebalStatus `json:"fonds"`
	Parts    []*int64      `json:"parts"`
	Actions  rebalStatus   `json:"actions"`
	Obl      rebalStatus   `json:"obligations"`
	Autres   rebalStatus   `json:"autres_actifs"`
}

const aEquilibrer = "À équilibrer"

// rebalDivergence épingle ce que le moteur fait autrement que l'oracle.
type rebalDivergence struct {
	// Classes : statut de bande des classes (actions, obligations, autres) que le moteur donne
	// quand il diffère de l'oracle. D332 (oracle) : ±5 points absolus ; D370 (moteur) : ±20 %
	// relatifs de la cible (bandes configurables de D12).
	ClassStatus map[string]string
	// Parts : net des ordres (positif = achat) par ligne du cas, quand il diffère de
	// trunc(−écart / prix) de l'oracle (D333). Le moteur n'achète qu'avec le produit des ventes
	// (D371/D372), arrondi à la part entière.
	Parts []int64
}

var rebalDivergences = map[string]rebalDivergence{
	"reequilibrage-fidele-02":      {ClassStatus: map[string]string{"actions": aEquilibrer}},
	"reequilibrage-fidele-03":      {ClassStatus: map[string]string{"obligations": "OK"}},
	"reequilibrage-fidele-05":      {ClassStatus: map[string]string{"actions": "OK", "obligations": "OK"}},
	"reequilibrage-ecart-parts-01": {ClassStatus: map[string]string{"actions": aEquilibrer}, Parts: []int64{-16, 29, 0}},
	"reequilibrage-ecart-parts-02": {Parts: []int64{0, 0, 0, 0}},
}

func TestOracleReequilibrage(t *testing.T) {
	t.Parallel()
	replayed := 0
	for _, c := range loadOracle(t) {
		if c.Onglet != "RÉÉQUILIBRAGE" {
			continue
		}
		replayed++
		t.Run(c.ID, func(t *testing.T) {
			var in rebalIn
			var want rebalOut
			mustDecode(t, c.Entrees, &in)
			mustDecode(t, c.AttenduApp, &want)

			cashTarget, _ := in.CibleEncaisse.Float64()
			rin := engine.RebalanceInput{CashTarget: cashTarget}
			cash := dnum(t, in.Encaisse)
			rin.Accounts = []engine.RebalAccount{{ID: "A", Type: engine.AccountCELI, Cash: cash}}
			for _, f := range in.Fonds {
				// Convention de saisie de l'outil (D332) : les cibles de fonds totalisent 100 % de la partie
				// investie ; elles sont ramenées à 1 − encaisse cible avant le moteur (voir Target ci-dessous).
				price := decimal.NewFromInt(1) // sans prix dans le cas : 1 $ la part, parts = valeur
				if f.Prix != nil {
					price = dnum(t, *f.Prix)
				}
				value := dnum(t, f.Valeur)
				// Parts exactes : valeur / prix à 20 décimales (le total reste égal à l'oracle à 1e-9).
				shares := value.DivRound(price, 20)
				rin.Funds = append(rin.Funds, engine.RebalFund{
					ID: f.Symbole, Price: price, Target: f.PctCible * (1 - cashTarget), Allocation: synCatalogue[f.Symbole].allocation(t),
				})
				rin.Accounts[0].Positions = append(rin.Accounts[0].Positions, engine.RebalPosition{FundID: f.Symbole, Shares: shares})
			}
			plan, err := engine.Rebalance(rin)
			if err != nil {
				t.Fatal(err)
			}
			div := rebalDivergences[c.ID]

			total, _ := plan.Total.Float64()
			near(t, "total", total, want.Total)
			near(t, "encaisse.pct", plan.Before.Cash, want.Encaisse.Pct)
			if (want.Encaisse.Statut == aEquilibrer) != hasBand(plan, "cash", "") {
				t.Fatalf("statut de l'encaisse : %v, attendu %s", plan.OutOfBand, want.Encaisse.Statut)
			}
			byFund := map[string]engine.FundWeight{}
			for _, fw := range plan.Before.Funds {
				byFund[fw.FundID] = fw
			}
			for i, f := range in.Fonds {
				w, exp := byFund[f.Symbole], want.Fonds[i]
				near(t, f.Symbole+".pct", w.Weight, exp.Pct)
				near(t, f.Symbole+".cible", w.Target, exp.Cible)
				near(t, f.Symbole+".ecart", (w.Weight-w.Target)*total, exp.Ecart)
				if (exp.Statut == aEquilibrer) != hasBand(plan, "fund", f.Symbole) {
					t.Fatalf("statut de %s : %v, attendu %s", f.Symbole, plan.OutOfBand, exp.Statut)
				}
			}
			cls := []struct {
				name  string
				exp   rebalStatus
				value float64
				ids   []engine.Class
			}{
				{"actions", want.Actions, 0, []engine.Class{engine.EquityCA, engine.EquityUS, engine.EquityIntl}},
				{"obligations", want.Obl, 0, []engine.Class{engine.BondCA, engine.BondOther}},
				{"autres_actifs", want.Autres, 0, []engine.Class{engine.OtherAssets}},
			}
			for _, k := range cls {
				pct, outCount := 0.0, 0
				for _, id := range k.ids {
					pct += plan.Before.Classes.Get(id)
					if hasBand(plan, "class", string(id)) {
						outCount++
					}
				}
				near(t, k.name+".pct", pct, k.exp.Pct)
				// Le statut d'une classe de l'oracle regroupe ses sous-classes ; le moteur les évalue une à une.
				got := "OK"
				if outCount > 0 {
					got = aEquilibrer
				}
				expect := k.exp.Statut
				if v, ok := div.ClassStatus[k.name]; ok {
					expect = v
				}
				if got != expect {
					t.Errorf("statut de la classe %s : %s, attendu %s", k.name, got, expect)
				}
			}

			// Parts à bouger (net des ordres), seulement quand le cas porte des prix.
			if len(want.Parts) > 0 && want.Parts[0] != nil {
				net := map[string]int64{}
				for _, o := range plan.Orders {
					if o.Side == engine.SideSell {
						net[o.FundID] -= o.Shares
					} else {
						net[o.FundID] += o.Shares
					}
				}
				got := make([]int64, len(in.Fonds))
				exp := make([]int64, len(in.Fonds))
				for i, f := range in.Fonds {
					got[i] = net[f.Symbole]
					exp[i] = *want.Parts[i]
				}
				if div.Parts != nil {
					exp = div.Parts
				}
				if fmt.Sprint(got) != fmt.Sprint(exp) {
					t.Errorf("parts %v, attendu %v", got, exp)
				}
			}
		})
	}
	if replayed != 11 {
		t.Fatalf("%d cas RÉÉQUILIBRAGE rejoués, 11 attendus", replayed)
	}
	t.Cleanup(func() {
		t.Logf("RÉÉQUILIBRAGE : %d cas, %d écarts voulus épinglés (D415)", replayed, len(rebalDivergences))
	})
}

func hasBand(p engine.RebalancePlan, kind, id string) bool {
	for _, b := range p.OutOfBand {
		if b.Kind == kind && b.ID == id {
			return true
		}
	}
	return false
}

func near(t *testing.T, what string, got, want float64) {
	t.Helper()
	if d := got - want; d > 1e-9 || d < -1e-9 {
		t.Fatalf("%s = %.12f, attendu %.12f", what, got, want)
	}
}

func mustDecode(t *testing.T, raw json.RawMessage, v any) {
	t.Helper()
	d := json.NewDecoder(bytes.NewReader(raw))
	d.UseNumber()
	if err := d.Decode(v); err != nil {
		t.Fatal(err)
	}
}
