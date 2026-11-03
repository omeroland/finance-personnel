package httpapi_test

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/omeroland/finance-privee/backend/internal/api/gen"
	"github.com/omeroland/finance-privee/backend/internal/modules/stockeval/ingest"
	"github.com/omeroland/finance-privee/backend/internal/modules/stockeval/store"
)

const (
	base = "/api/v1"
	sym  = "TSX:CNR"
)

func (a *api) addStock(symbol string) {
	a.t.Helper()
	r := a.doJSON("POST", base+"/stocks", gen.StockCreate{
		Symbol: symbol, Name: "Compagnie " + symbol, QuoteCurrency: gen.CAD, ReportingCurrency: gen.CAD,
	})
	if r.Status != 201 {
		a.t.Fatalf("ajouter %s: %d %s", symbol, r.Status, r.Body)
	}
}

// fillPerfect remplit une action qui réussit les dix critères au seuil strict sur 20 ans.
func (a *api) fillPerfect(symbol string) {
	a.t.Helper()
	var facts []gen.AnnualFact
	eps, dps, shares := 1.0, 0.4, 1000.0
	for y := 2006; y <= 2025; y++ {
		facts = append(facts,
			gen.AnnualFact{FiscalYear: y, Metric: gen.DilutedEps, Value: fmt.Sprintf("%.6f", eps), Currency: gen.CAD, Source: "manual"},
			gen.AnnualFact{FiscalYear: y, Metric: gen.DividendPerShare, Value: fmt.Sprintf("%.6f", dps), Currency: gen.CAD, Source: "manual"},
			gen.AnnualFact{FiscalYear: y, Metric: gen.AnnualReturn, Value: "0.01", Currency: gen.CAD, Source: "manual"},
			gen.AnnualFact{FiscalYear: y, Metric: gen.DilutedShares, Value: fmt.Sprintf("%.2f", shares), Currency: gen.CAD, Source: "manual"},
		)
		eps, dps, shares = eps*1.12, dps*1.12, shares*0.99
	}
	if r := a.doJSON("PUT", base+"/stocks/"+symbol+"/facts", gen.FactsReplace{Facts: facts}); r.Status != 200 {
		a.t.Fatalf("faits: %d %s", r.Status, r.Body)
	}
	lastEPS := 1.0
	for range 19 {
		lastEPS *= 1.12
	}
	price := fmt.Sprintf("%.2f", 15*lastEPS)
	snap := gen.Snapshot{
		Currency: gen.CAD, Price: &price, AnnualDividend: ptrS(fmt.Sprintf("%.4f", 0.4*lastEPS*1.02)),
		LongTermDebt: ptrS("30"), Equity: ptrS("100"), BookValuePerShare: ptrS(fmt.Sprintf("%.2f", 15*lastEPS/2)),
	}
	if r := a.doJSON("PATCH", base+"/stocks/"+symbol, gen.StockUpdate{Snapshot: &snap}); r.Status != 200 {
		a.t.Fatalf("instantané: %d %s", r.Status, r.Body)
	}
	yes := gen.RuleAnswer{Answer: gen.Yes, Note: ptrS("ok")}
	q := gen.Qualitative{Rule1: yes, Rule2: yes, Rule3: yes, Rule4: yes}
	if r := a.doJSON("PUT", base+"/stocks/"+symbol+"/qualitative", q); r.Status != 200 {
		a.t.Fatalf("qualitatif: %d %s", r.Status, r.Body)
	}
}

func ptrS(s string) *string { return &s }

func TestWatchlistCrud(t *testing.T) {
	t.Parallel()
	a := newAPI(t)

	if r := a.doJSON("GET", base+"/stocks", nil); r.Status != 200 || !strings.Contains(string(r.Body), `"items":[]`) {
		t.Fatalf("liste vide: %d %s", r.Status, r.Body)
	}
	a.addStock(sym)
	if r := a.doJSON("POST", base+"/stocks", gen.StockCreate{Symbol: sym, Name: "x", QuoteCurrency: gen.CAD, ReportingCurrency: gen.CAD}); r.Status != 409 {
		t.Fatalf("doublon: %d", r.Status)
	}
	bad := []gen.StockCreate{
		{Symbol: "cnr", Name: "x", QuoteCurrency: gen.CAD, ReportingCurrency: gen.CAD},
		{Symbol: "LSE:VOD", Name: "x", QuoteCurrency: gen.CAD, ReportingCurrency: gen.CAD},
		{Symbol: "TSX:RY", Name: "", QuoteCurrency: "EUR", ReportingCurrency: gen.CAD},
	}
	for _, b := range bad {
		r := a.doJSON("POST", base+"/stocks", b)
		var p httpProblem
		r.json(t, &p)
		if r.Status != 400 || len(p.Errors) == 0 {
			t.Fatalf("%+v: %d %s", b, r.Status, r.Body)
		}
	}
	if r := a.do("POST", base+"/stocks", "application/json", []byte("{pas du json")); r.Status != 400 {
		t.Fatalf("JSON invalide: %d", r.Status)
	}

	var d gen.StockDetail
	r := a.doJSON("GET", base+"/stocks/"+sym, nil)
	r.json(t, &d)
	if r.Status != 200 || d.Stock.Symbol != sym || d.Qualitative.Rule1.Answer != gen.Unanswered || d.Snapshot != nil {
		t.Fatalf("fiche: %d %s", r.Status, r.Body)
	}
	if r := a.doJSON("GET", base+"/stocks/TSX:NOPE", nil); r.Status != 404 {
		t.Fatalf("inconnue: %d", r.Status)
	}
	if r := a.doJSON("GET", base+"/stocks/bad", nil); r.Status != 400 {
		t.Fatalf("symbole invalide: %d", r.Status)
	}

	// Modification des métadonnées.
	name, fin := "Nouveau nom", true
	r = a.doJSON("PATCH", base+"/stocks/"+sym, gen.StockUpdate{Name: &name, IsFinancial: &fin})
	r.json(t, &d)
	if r.Status != 200 || d.Stock.Name != name || !d.Stock.IsFinancial {
		t.Fatalf("patch: %d %s", r.Status, r.Body)
	}
	if r := a.doJSON("PATCH", base+"/stocks/"+sym, map[string]any{}); r.Status != 400 {
		t.Fatalf("patch vide: %d", r.Status)
	}
	empty := ""
	if r := a.doJSON("PATCH", base+"/stocks/"+sym, gen.StockUpdate{Name: &empty}); r.Status != 400 {
		t.Fatalf("nom vide: %d", r.Status)
	}
	badSnap := gen.Snapshot{Currency: gen.USD, Price: ptrS("1.5")}
	if r := a.doJSON("PATCH", base+"/stocks/"+sym, gen.StockUpdate{Snapshot: &badSnap}); r.Status != 400 {
		t.Fatalf("devise de l'instantané: %d", r.Status)
	}
	badSnap = gen.Snapshot{Currency: gen.CAD, Price: ptrS("abc")}
	if r := a.doJSON("PATCH", base+"/stocks/"+sym, gen.StockUpdate{Snapshot: &badSnap}); r.Status != 400 {
		t.Fatalf("prix invalide: %d", r.Status)
	}
	if r := a.doJSON("PATCH", base+"/stocks/TSX:NOPE", gen.StockUpdate{Name: &name}); r.Status != 404 {
		t.Fatalf("patch inconnue: %d", r.Status)
	}

	if r := a.doJSON("DELETE", base+"/stocks/"+sym, nil); r.Status != 204 {
		t.Fatalf("suppression: %d", r.Status)
	}
	if r := a.doJSON("DELETE", base+"/stocks/"+sym, nil); r.Status != 404 {
		t.Fatalf("seconde suppression: %d", r.Status)
	}
	if r := a.doJSON("GET", base+"/stocks/"+sym, nil); r.Status != 404 {
		t.Fatalf("après suppression: %d", r.Status)
	}
	// Garde-fou : la validation du contrat a bien tourné sur les réponses déclarées.
	if a.checks < 10 {
		t.Fatalf("seulement %d réponses validées contre le contrat", a.checks)
	}
}

// Une réponse hors contrat doit être détectée par le harnais lui-même.
func TestContractHarnessDetectsViolations(t *testing.T) {
	t.Parallel()
	bad := resp{Status: 200, Header: http.Header{"Content-Type": {"application/json"}}, Body: []byte(`{"items":[{"stock":{"symbol":"x"}}]}`)}
	req := httptest.NewRequest("GET", base+"/stocks", nil)
	ft := &failRecorder{TB: t}
	a2 := &api{t: ft}
	func() {
		defer func() { _ = recover() }()
		a2.validate(req, bad)
	}()
	if !ft.failed {
		t.Fatal("la violation du contrat n'a pas été détectée")
	}
}

// failRecorder capture Fatalf au lieu d'arrêter le test.
type failRecorder struct {
	testing.TB
	failed bool
}

func (f *failRecorder) Fatalf(string, ...any) { f.failed = true; panic("fatal") }

type httpProblem struct {
	Status int `json:"status"`
	Errors []struct {
		Field string `json:"field"`
	} `json:"errors"`
}

func TestFactsAndQualitative(t *testing.T) {
	t.Parallel()
	a := newAPI(t)
	a.addStock(sym)
	path := base + "/stocks/" + sym

	facts := gen.FactsReplace{Facts: []gen.AnnualFact{
		{FiscalYear: 2024, Metric: gen.DilutedEps, Value: "2.50", Currency: gen.CAD, Source: "ignorée"},
		{FiscalYear: 2024, Metric: gen.AnnualReturn, Value: "0.031", Currency: gen.CAD, Source: "x"},
	}}
	var out gen.FactList
	r := a.doJSON("PUT", path+"/facts", facts)
	r.json(t, &out)
	if r.Status != 200 || len(out.Facts) != 2 || out.Facts[0].Source != "manual" {
		t.Fatalf("faits: %d %s", r.Status, r.Body)
	}
	for _, bad := range []gen.FactsReplace{
		{Facts: nil},
		{Facts: []gen.AnnualFact{{FiscalYear: 1800, Metric: gen.DilutedEps, Value: "1", Currency: gen.CAD}}},
		{Facts: []gen.AnnualFact{{FiscalYear: 2020, Metric: "inconnue", Value: "1", Currency: gen.CAD}}},
		{Facts: []gen.AnnualFact{{FiscalYear: 2020, Metric: gen.DilutedEps, Value: "1,5", Currency: gen.CAD}}},
		{Facts: []gen.AnnualFact{{FiscalYear: 2020, Metric: gen.DilutedEps, Value: "1", Currency: "EUR"}}},
		{Facts: []gen.AnnualFact{
			{FiscalYear: 2020, Metric: gen.DilutedEps, Value: "1", Currency: gen.CAD},
			{FiscalYear: 2020, Metric: gen.DilutedEps, Value: "2", Currency: gen.CAD},
		}},
	} {
		if r := a.doJSON("PUT", path+"/facts", bad); r.Status != 400 {
			t.Fatalf("%+v: %d", bad, r.Status)
		}
	}
	if r := a.doJSON("PUT", base+"/stocks/TSX:NOPE/facts", facts); r.Status != 404 {
		t.Fatalf("faits inconnue: %d", r.Status)
	}

	// Un conflit ouvert apparaît dans la fiche et comme avertissement de l'évaluation.
	for _, x := range a.repos.facts {
		a.repos.conflicts = append(a.repos.conflicts, store.FactConflict{
			InstrumentID: x.InstrumentID, FiscalYear: 2024, Metric: store.MetricEPSDiluted,
			ExistingValue: dec("2.50"), ExistingSource: "manual", IncomingValue: dec("2.60"), IncomingSource: "xlsx",
		})
		break
	}
	var d gen.StockDetail
	r = a.doJSON("GET", path, nil)
	r.json(t, &d)
	if len(d.Conflicts) != 1 || len(d.Sources) != 1 || d.Sources[0].FactCount != 2 {
		t.Fatalf("fiche: %s", r.Body)
	}
	var ev gen.Evaluation
	r = a.doJSON("GET", path+"/evaluation", nil)
	r.json(t, &ev)
	if ev.Warnings == nil || !strings.Contains(strings.Join(*ev.Warnings, "|"), "Conflit de sources") {
		t.Fatalf("avertissement de conflit absent: %s", r.Body)
	}

	note := strings.Repeat("é", 2001)
	q := gen.Qualitative{
		Rule1: gen.RuleAnswer{Answer: gen.Yes}, Rule2: gen.RuleAnswer{Answer: gen.No, Note: ptrS("non")},
		Rule3: gen.RuleAnswer{Answer: gen.Unanswered}, Rule4: gen.RuleAnswer{Answer: gen.Yes}, ManualBuyback: new(bool),
	}
	var got gen.Qualitative
	r = a.doJSON("PUT", path+"/qualitative", q)
	r.json(t, &got)
	if r.Status != 200 || got.Rule2.Answer != gen.No || got.Rule2.Note == nil || *got.Rule2.Note != "non" ||
		got.Rule3.Answer != gen.Unanswered || got.ReviewedAt == nil || !got.ReviewedAt.Equal(a.now) {
		t.Fatalf("qualitatif: %d %s", r.Status, r.Body)
	}
	q.Rule1 = gen.RuleAnswer{Answer: "peut-être"}
	if r := a.doJSON("PUT", path+"/qualitative", q); r.Status != 400 {
		t.Fatalf("réponse invalide: %d", r.Status)
	}
	q.Rule1 = gen.RuleAnswer{Answer: gen.Yes, Note: &note}
	if r := a.doJSON("PUT", path+"/qualitative", q); r.Status != 400 {
		t.Fatalf("note trop longue: %d", r.Status)
	}
	if r := a.doJSON("PUT", base+"/stocks/TSX:NOPE/qualitative", q); r.Status != 404 {
		t.Fatalf("qualitatif inconnue: %d", r.Status)
	}
}

func TestEvaluationAndListPerfectHorizon(t *testing.T) {
	t.Parallel()
	a := newAPI(t)
	a.addStock(sym)
	a.addStock("NYSE:KO")
	a.fillPerfect(sym)
	path := base + "/stocks/" + sym

	var ev gen.Evaluation
	r := a.doJSON("GET", path+"/evaluation", nil)
	r.json(t, &ev)
	if r.Status != 200 || len(ev.Horizons) != 4 || !ev.QualitativeGatePassed || ev.Mode != gen.Corrected {
		t.Fatalf("évaluation: %d %s", r.Status, r.Body)
	}
	for _, h := range ev.Horizons {
		if h.FailureCount != 0 || h.Strict != 10 || h.OutOf != 10 || len(h.Criteria) != 10 {
			t.Fatalf("horizon %d: %+v", h.Horizon, h)
		}
		if (h.Horizon == 5) != (h.Verdict == nil) {
			t.Fatalf("verdict à %d ans: %v", h.Horizon, h.Verdict)
		}
		if h.Verdict != nil && *h.Verdict != gen.Candidate {
			t.Fatalf("verdict = %s", *h.Verdict)
		}
	}

	var list gen.StockList
	r = a.doJSON("GET", base+"/stocks?horizon=15", nil)
	r.json(t, &list)
	if r.Status != 200 || len(list.Items) != 2 || list.Items[0].Stock.Symbol != "NYSE:KO" {
		t.Fatalf("liste: %d %s", r.Status, r.Body)
	}
	cnr, ko := list.Items[1].Score, list.Items[0].Score
	if cnr.Horizon != 15 || cnr.PerfectHorizon == nil || *cnr.PerfectHorizon != 20 || cnr.Verdict == nil || *cnr.Verdict != gen.Candidate {
		t.Fatalf("score CNR: %+v", cnr)
	}
	if ko.PerfectHorizon != nil || ko.Verdict == nil || *ko.Verdict != gen.Excluded {
		t.Fatalf("score KO (sans données, exclue): %+v", ko)
	}

	// Compat : horizon fixé à 20 ans.
	r = a.doJSON("GET", base+"/stocks?mode=compat", nil)
	r.json(t, &list)
	if r.Status != 200 || list.Items[1].Score.Horizon != 20 || list.Items[1].Score.Mode != gen.Compat {
		t.Fatalf("liste compat: %d %s", r.Status, r.Body)
	}
	r = a.doJSON("GET", path+"/evaluation?mode=compat", nil)
	r.json(t, &ev)
	if r.Status != 200 || len(ev.Horizons) != 1 || ev.Horizons[0].Horizon != 20 {
		t.Fatalf("évaluation compat: %d %s", r.Status, r.Body)
	}
	for _, q := range []string{"/stocks?mode=compat&horizon=10", "/stocks?mode=x", "/stocks?horizon=7", "/stocks?horizon=abc"} {
		if r := a.doJSON("GET", base+q, nil); r.Status != 400 {
			t.Fatalf("%s: %d", q, r.Status)
		}
	}
	if r := a.doJSON("GET", path+"/evaluation?mode=x", nil); r.Status != 400 {
		t.Fatalf("mode invalide: %d", r.Status)
	}
	if r := a.doJSON("GET", base+"/stocks/TSX:NOPE/evaluation", nil); r.Status != 404 {
		t.Fatalf("évaluation inconnue: %d", r.Status)
	}
}

func TestCompatSheetErrorIsProblem(t *testing.T) {
	t.Parallel()
	a := newAPI(t)
	a.addStock(sym)
	a.addStock("TSX:RY")
	// BPA nul suivi d'une valeur : #DIV/0! de la feuille.
	facts := gen.FactsReplace{Facts: []gen.AnnualFact{
		{FiscalYear: 2023, Metric: gen.DilutedEps, Value: "0", Currency: gen.CAD},
		{FiscalYear: 2024, Metric: gen.DilutedEps, Value: "1.5", Currency: gen.CAD},
	}}
	if r := a.doJSON("PUT", base+"/stocks/"+sym+"/facts", facts); r.Status != 200 {
		t.Fatalf("faits: %d", r.Status)
	}
	r := a.doJSON("GET", base+"/stocks/"+sym+"/evaluation?mode=compat", nil)
	var p struct{ Type, Detail string }
	r.json(t, &p)
	if r.Status != 400 || !strings.HasSuffix(p.Type, "/sheet-error") || !strings.Contains(p.Detail, "#DIV/0!") {
		t.Fatalf("erreur de la feuille: %d %s", r.Status, r.Body)
	}
	if r := a.doJSON("POST", base+"/stocks/"+sym+"/evaluations", gen.EvaluationCreate{Mode: ptrMode(gen.Compat)}); r.Status != 400 {
		t.Fatalf("archivage compat: %d", r.Status)
	}
	// La liste compat omet l'action en erreur mais garde les autres ; le mode corrigé les garde toutes.
	var list gen.StockList
	a.doJSON("GET", base+"/stocks?mode=compat", nil).json(t, &list)
	if len(list.Items) != 1 || list.Items[0].Stock.Symbol != "TSX:RY" {
		t.Fatalf("liste compat: %+v", list.Items)
	}
	a.doJSON("GET", base+"/stocks", nil).json(t, &list)
	if len(list.Items) != 2 {
		t.Fatalf("liste corrigée: %d", len(list.Items))
	}
}

func ptrMode(m gen.Mode) *gen.Mode { return &m }

func TestArchiveEvaluation(t *testing.T) {
	t.Parallel()
	a := newAPI(t)
	a.addStock(sym)
	a.fillPerfect(sym)
	path := base + "/stocks/" + sym + "/evaluations"

	var hist gen.EvaluationRecordList
	if r := a.doJSON("GET", path, nil); r.Status != 200 {
		t.Fatal(r.Status)
	} else {
		r.json(t, &hist)
	}
	if len(hist.Items) != 0 {
		t.Fatalf("historique initial: %d", len(hist.Items))
	}
	var rec gen.EvaluationRecord
	r := a.doJSON("POST", path, gen.EvaluationCreate{})
	r.json(t, &rec)
	if r.Status != 201 || rec.Mode != gen.Corrected || len(rec.InputsFingerprint) != 64 || rec.RulesVersion == "" || len(rec.Evaluation.Horizons) != 4 {
		t.Fatalf("archive: %d %s", r.Status, r.Body)
	}
	// GET sans archivage : l'historique ne change pas.
	a.doJSON("GET", base+"/stocks/"+sym+"/evaluation", nil)
	a.doJSON("POST", path, gen.EvaluationCreate{Mode: ptrMode(gen.Compat)})
	r = a.doJSON("GET", path, nil)
	r.json(t, &hist)
	if len(hist.Items) != 2 || hist.Items[0].Mode != gen.Compat {
		t.Fatalf("historique: %s", r.Body)
	}
	// Les entrées archivées permettent de recalculer à l'identique : même empreinte pour mêmes entrées.
	var again gen.EvaluationRecord
	a.doJSON("POST", path, gen.EvaluationCreate{}).json(t, &again)
	if again.InputsFingerprint != rec.InputsFingerprint {
		t.Fatalf("empreinte instable: %s / %s", again.InputsFingerprint, rec.InputsFingerprint)
	}
	if r := a.do("POST", path, "application/json", []byte(`{"mode":"zzz"}`)); r.Status != 400 {
		t.Fatalf("mode invalide: %d", r.Status)
	}
	if r := a.doJSON("POST", base+"/stocks/TSX:NOPE/evaluations", gen.EvaluationCreate{}); r.Status != 404 {
		t.Fatalf("archive inconnue: %d", r.Status)
	}
	if r := a.doJSON("GET", base+"/stocks/TSX:NOPE/evaluations", nil); r.Status != 404 {
		t.Fatalf("historique inconnue: %d", r.Status)
	}
}

func TestRulesAndRefresh(t *testing.T) {
	t.Parallel()
	a := newAPI(t)
	var rules gen.Rules
	r := a.doJSON("GET", base+"/rules", nil)
	r.json(t, &rules)
	if r.Status != 200 || len(rules.Criteria) != 10 || rules.Version == "" {
		t.Fatalf("règles: %d %s", r.Status, r.Body)
	}
	for _, c := range rules.Criteria {
		if c.Id == gen.CriterionIdN5b && (c.ByHorizon == nil || len(*c.ByHorizon) != 4) {
			t.Fatalf("5b par horizon: %+v", c)
		}
		if c.Id == gen.CriterionIdN7 && (c.Strict.Description != "0 < x ≤ 75 %" || c.Relaxed == nil || c.Relaxed.Description != "0 < x ≤ 85 %") {
			t.Fatalf("règle 7: %+v", c)
		}
	}
	a.addStock(sym)
	r = a.doJSON("POST", base+"/stocks/"+sym+"/refresh", nil)
	var p struct{ Status int }
	r.json(t, &p)
	if r.Status != 501 || p.Status != 501 || r.Header.Get("Content-Type") != "application/problem+json" {
		t.Fatalf("refresh: %d %s", r.Status, r.Body)
	}
	if r := a.doJSON("POST", base+"/stocks/TSX:NOPE/refresh", nil); r.Status != 404 {
		t.Fatalf("refresh inconnue: %d", r.Status)
	}
}

// multipartBody construit le corps d'un import ; year vide = champ absent.
func multipartBody(t *testing.T, year string, file []byte) ([]byte, string) {
	t.Helper()
	var buf bytes.Buffer
	w := multipart.NewWriter(&buf)
	if year != "" {
		_ = w.WriteField("year1", year)
	}
	if file != nil {
		fw, err := w.CreateFormFile("file", "feuille.xlsx")
		if err != nil {
			t.Fatal(err)
		}
		_, _ = fw.Write(file)
	}
	_ = w.Close()
	return buf.Bytes(), w.FormDataContentType()
}

func (a *api) waitJob(id string) gen.Job {
	a.t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for {
		var j gen.Job
		r := a.doJSON("GET", base+"/imports/"+id, nil)
		r.json(a.t, &j)
		if r.Status != 200 {
			a.t.Fatalf("job: %d %s", r.Status, r.Body)
		}
		if j.Status == gen.JobStatusSucceeded || j.Status == gen.JobStatusFailed {
			return j
		}
		if time.Now().After(deadline) {
			a.t.Fatalf("job jamais terminé: %s", r.Body)
		}
		time.Sleep(5 * time.Millisecond)
	}
}

func TestImportXlsx(t *testing.T) {
	t.Parallel()
	a := newAPI(t)
	a.imp.report = ingest.Report{
		Year1: 2006, Imported: 1, Warnings: 1,
		Entries: []ingest.Entry{
			{Level: "info", Kind: "imported", Line: 7, Symbol: sym, Message: "Ligne importée."},
			{Level: "warning", Kind: "normalized", Line: 7, Symbol: sym, Message: "Ratio lu comme un pourcentage."},
			{Level: "warning", Message: "sans catégorie"},
		},
	}
	body, ct := multipartBody(t, "2006", []byte("contenu xlsx"))
	r := a.do("POST", base+"/imports/xlsx", ct, body)
	var job gen.Job
	r.json(t, &job)
	if r.Status != 202 || job.Type != gen.Xlsx || (job.Status != gen.JobStatusQueued && job.Status != gen.JobStatusRunning && job.Status != gen.JobStatusSucceeded) {
		t.Fatalf("import: %d %s", r.Status, r.Body)
	}
	done := a.waitJob(job.Id.String())
	if done.Status != gen.JobStatusSucceeded || done.FinishedAt == nil || done.StocksImported == nil || *done.StocksImported != 1 {
		t.Fatalf("job terminé: %+v", done)
	}
	e := *done.Entries
	if len(e) != 3 || e[1].Kind == nil || *e[1].Kind != gen.JobEntryKindNormalized || e[1].Line == nil || *e[1].Line != 7 ||
		e[1].Symbol == nil || e[2].Kind != nil || e[0].Level != gen.Info {
		t.Fatalf("entrées: %+v", e)
	}
	if string(a.imp.got) != "contenu xlsx" || a.imp.year != 2006 {
		t.Fatalf("importeur: %q %d", a.imp.got, a.imp.year)
	}

	// Échec : le message utilisateur remonte comme entrée « error », sans détail interne.
	a.imp.err = fmt.Errorf("onglets manquants : X: %w", ingest.ErrInvalidWorkbook)
	a.imp.report = ingest.Report{}
	r = a.do("POST", base+"/imports/xlsx", ct, body)
	r.json(t, &job)
	done = a.waitJob(job.Id.String())
	e = *done.Entries
	if done.Status != gen.JobStatusFailed || len(e) != 1 || e[0].Level != gen.Error || !strings.Contains(e[0].Message, "onglets manquants") {
		t.Fatalf("job en échec: %+v", done)
	}
	a.imp.err = errors.New("pq: SELECT secret FROM t")
	r = a.do("POST", base+"/imports/xlsx", ct, body)
	r.json(t, &job)
	done = a.waitJob(job.Id.String())
	if done.Status != gen.JobStatusFailed || strings.Contains((*done.Entries)[0].Message, "SELECT") {
		t.Fatalf("fuite interne: %+v", done)
	}
	if !strings.Contains(a.logs.String(), "SELECT secret") {
		t.Fatal("la cause doit rester dans le journal")
	}

	// Panique de l'importeur : le job échoue, le serveur survit.
	a.imp.panics = true
	r = a.do("POST", base+"/imports/xlsx", ct, body)
	r.json(t, &job)
	if done = a.waitJob(job.Id.String()); done.Status != gen.JobStatusFailed {
		t.Fatalf("panique: %+v", done)
	}
}

func TestImportValidationAndLimits(t *testing.T) {
	t.Parallel()
	a := newAPI(t)
	cases := map[string]struct {
		year string
		file []byte
	}{
		"sans fichier":     {"2006", nil},
		"fichier vide":     {"2006", []byte{}},
		"sans année":       {"", []byte("x")},
		"année hors borne": {"1700", []byte("x")},
		"année texte":      {"abc", []byte("x")},
	}
	for name, c := range cases {
		body, ct := multipartBody(t, c.year, c.file)
		if r := a.do("POST", base+"/imports/xlsx", ct, body); r.Status != 400 {
			t.Fatalf("%s: %d %s", name, r.Status, r.Body)
		}
	}
	if r := a.do("POST", base+"/imports/xlsx", "application/json", []byte("{}")); r.Status != 400 {
		t.Fatalf("pas multipart: %d", r.Status)
	}

	// 11 Mo : 413 (Content-Length connu), et aussi sans Content-Length (lecture bornée).
	body, ct := multipartBody(t, "2006", bytes.Repeat([]byte("a"), 11<<20))
	r := a.do("POST", base+"/imports/xlsx", ct, body)
	if r.Status != 413 {
		t.Fatalf("11 Mo: %d", r.Status)
	}
	req, _ := http.NewRequest("POST", base+"/imports/xlsx", bytes.NewReader(body))
	req.ContentLength = -1
	req.Header.Set("Content-Type", ct)
	rec := httptest.NewRecorder()
	a.srv.ServeHTTP(rec, req)
	if rec.Code != 413 {
		t.Fatalf("11 Mo sans Content-Length: %d %s", rec.Code, rec.Body.String())
	}
	// Un fichier d'exactement 10 Mo est accepté.
	body, ct = multipartBody(t, "2006", bytes.Repeat([]byte("a"), ingest.MaxFileSize))
	if r := a.do("POST", base+"/imports/xlsx", ct, body); r.Status != 202 {
		t.Fatalf("10 Mo: %d %s", r.Status, r.Body)
	}
	// Ailleurs, la limite est de 1 Mo.
	big, _ := json.Marshal(gen.StockCreate{Symbol: sym, Name: strings.Repeat("a", 2<<20), QuoteCurrency: gen.CAD, ReportingCurrency: gen.CAD})
	if r := a.do("POST", base+"/stocks", "application/json", big); r.Status != 413 {
		t.Fatalf("2 Mo hors import: %d", r.Status)
	}

	if r := a.doJSON("GET", base+"/imports/00000000-0000-4000-8000-000000000000", nil); r.Status != 404 {
		t.Fatalf("job inconnu: %d", r.Status)
	}
	if r := a.doJSON("GET", base+"/imports/pas-un-uuid", nil); r.Status != 400 {
		t.Fatalf("id invalide: %d", r.Status)
	}
}

func TestInternalErrorHasNoLeak(t *testing.T) {
	t.Parallel()
	a := newAPI(t)
	a.repos.failList = errors.New(`ERROR: relation "stockeval.secret_table" does not exist (SQLSTATE 42P01) at pgx/conn.go:42`)
	for _, path := range []string{"/stocks", "/stocks/" + sym, "/stocks/" + sym + "/evaluation"} {
		r := a.doJSON("GET", base+path, nil)
		if r.Status != 500 || strings.Contains(string(r.Body), "secret_table") || strings.Contains(string(r.Body), "SQLSTATE") {
			t.Fatalf("%s: %d %s", path, r.Status, r.Body)
		}
	}
	if !strings.Contains(a.logs.String(), "secret_table") {
		t.Fatal("la cause doit être journalisée")
	}
	// Route inconnue : problem+json aussi.
	if r := a.doJSON("GET", "/api/v1/inconnue", nil); r.Status != 404 {
		t.Fatalf("route inconnue: %d", r.Status)
	}
}

func TestAuthRequiredAndOwnerMustBeUUID(t *testing.T) {
	t.Parallel()
	a := newAPI(t)
	// Sans propriétaire dans le contexte (jeton non vérifié en amont) : 401.
	bare := a.h.Routes()
	if r := a.doWith(bare, "GET", base+"/stocks", "", nil); r.Status != 401 {
		t.Fatalf("sans propriétaire: %d", r.Status)
	}
	bad := injectOwner(bare, "pas-un-uuid")
	if r := a.doWith(bad, "GET", base+"/stocks", "", nil); r.Status != 401 {
		t.Fatalf("sub non UUID: %d", r.Status)
	}
	if len(a.repos.owners) != 0 {
		t.Fatal("aucun accès aux dépôts sans propriétaire valide")
	}
	a.doJSON("GET", base+"/stocks", nil)
	if !a.repos.owners[a.owner] || len(a.repos.owners) != 1 {
		t.Fatalf("propriétaire utilisé = %v", a.repos.owners)
	}
}

func TestNewRequiresDependencies(t *testing.T) {
	t.Parallel()
	if _, err := newHandlerNoDeps(); err == nil {
		t.Fatal("erreur attendue sans dépendances")
	}
}
