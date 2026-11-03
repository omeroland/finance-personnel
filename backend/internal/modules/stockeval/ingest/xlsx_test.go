package ingest

import (
	"bytes"
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/xuri/excelize/v2"

	"github.com/omeroland/finance-privee/backend/internal/modules/stockeval/store"
)

var fixedNow = func() time.Time { return time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC) }

func doImport(t *testing.T, repos *fakeRepos, data []byte, year int) (Report, error) {
	t.Helper()
	return NewImporter(repos).Import(context.Background(), uuid.New(), bytes.NewReader(data), Options{Year1: year, Now: fixedNow})
}

func TestImportFiveStocks(t *testing.T) {
	t.Parallel()
	repos := newFake()
	data := buildWorkbook(t, fiveStocks())
	rep, err := doImport(t, repos, data, 2025)
	if err != nil {
		t.Fatal(err)
	}
	if rep.Imported != 5 || rep.Ignored != 0 || len(rep.Rows) != 5 {
		t.Fatalf("compte rendu = %+v", rep)
	}
	// Historique de 8 ans : 2018..2025.
	if _, ok := repos.fact("AAA", 2025, store.MetricEPSDiluted); !ok {
		t.Fatal("BPA 2025 de AAA absent")
	}
	if _, ok := repos.fact("AAA", 2017, store.MetricEPSDiluted); ok {
		t.Fatal("BPA 2017 de AAA ne devrait pas exister")
	}
	if rep.Rows[1].Created != 8*3 {
		t.Fatalf("faits créés AAA = %d, attendu 24", rep.Rows[1].Created)
	}
	// 20 ans pour RY : 2006..2025.
	if v, ok := repos.fact("TSX:RY", 2006, store.MetricEPSDiluted); !ok || !v.Value.Equal(d("4")) {
		t.Fatalf("BPA 2006 de RY = %v, %v", v, ok)
	}
	// BPA nul conservé.
	if v, ok := repos.fact("EEE", 2025, store.MetricEPSDiluted); !ok || !v.Value.IsZero() {
		t.Fatalf("BPA nul de EEE = %v, %v", v, ok)
	}
	if v, _ := repos.fact("TSX:RY", 2025, store.MetricEPSDiluted); v.Source != store.SourceXLSX || *v.Currency != "CAD" {
		t.Fatalf("source/devise = %s/%v", v.Source, v.Currency)
	}
	if v, _ := repos.fact("NYSE:KO", 2025, store.MetricDPS); *v.Currency != "USD" {
		t.Fatalf("devise KO = %v", *v.Currency)
	}
	// Instantané et rachat.
	ry := repos.snaps[repos.insts["TSX:RY"].ID]
	if !ry.Price.Decimal.Equal(d("140")) || !ry.PEImported.Decimal.Equal(d("12")) || !ry.ExpectedDividend.Decimal.Equal(d("5.6")) {
		t.Fatalf("instantané RY = %+v", ry)
	}
	if q := repos.quals[repos.insts["TSX:RY"].ID]; q.BuybackManual == nil || !*q.BuybackManual {
		t.Fatal("rachat RY attendu vrai")
	}
	if q := repos.quals[repos.insts["TSX:BNS"].ID]; q.BuybackManual == nil || !*q.BuybackManual {
		t.Fatal("rachat « oui » attendu vrai")
	}
	if !hasWarning(rep.Rows[3], "prix absent") {
		t.Fatalf("avertissement prix absent manquant: %v", rep.Rows[3].Warnings)
	}
	if !strings.Contains(string(rep.JSON()), `"imported":5`) {
		t.Fatalf("JSON = %s", rep.JSON())
	}
}

func TestUnitNormalizationD31(t *testing.T) {
	t.Parallel()
	repos := newFake()
	rep, err := doImport(t, repos, buildWorkbook(t, fiveStocks()), 2025)
	if err != nil {
		t.Fatal(err)
	}
	aaa := repos.snaps[repos.insts["AAA"].ID]
	if !aaa.DebtToEquity.Decimal.Equal(d("1.1792")) {
		t.Fatalf("dette/capitaux = %s, attendu 1.1792", aaa.DebtToEquity.Decimal)
	}
	if !hasWarning(rep.Rows[1], "pourcentage") {
		t.Fatalf("avertissement manquant: %v", rep.Rows[1].Warnings)
	}
	ko := repos.snaps[repos.insts["NYSE:KO"].ID]
	if !ko.PayoutRatioImported.Decimal.Equal(d("0.684")) {
		t.Fatalf("ratio de distribution KO = %s", ko.PayoutRatioImported.Decimal)
	}
	if v, _ := repos.fact("NYSE:KO", 2025, store.MetricDividendYield); !v.Value.Equal(d("0.032")) {
		t.Fatalf("rendement KO = %s", v.Value)
	}
	if v := repos.snaps[repos.insts["TSX:RY"].ID]; !v.PayoutRatioImported.Decimal.Equal(d("0.45")) {
		t.Fatalf("ratio RY = %s", v.PayoutRatioImported.Decimal)
	}
}

func TestSecondImportIsNoop(t *testing.T) {
	t.Parallel()
	repos := newFake()
	data := buildWorkbook(t, fiveStocks())
	if _, err := doImport(t, repos, data, 2025); err != nil {
		t.Fatal(err)
	}
	before := repos.writes
	rep, err := doImport(t, repos, data, 2025)
	if err != nil {
		t.Fatal(err)
	}
	if repos.writes != before {
		t.Fatalf("%d écritures au second import", repos.writes-before)
	}
	for _, r := range rep.Rows {
		if r.Created+r.Updated+r.Conflicts != 0 || r.Unchanged == 0 {
			t.Fatalf("ligne %d: %+v", r.Row, r)
		}
		if r.Snapshot == "written" {
			t.Fatalf("instantané réécrit: %+v", r)
		}
	}
}

func ptr[T any](v T) *T { return &v }

func TestManualValueNotOverwritten(t *testing.T) {
	t.Parallel()
	repos := newFake()
	data := buildWorkbook(t, fiveStocks())
	if _, err := doImport(t, repos, data, 2025); err != nil {
		t.Fatal(err)
	}
	id := repos.insts["TSX:RY"].ID
	k := id.String() + "/2025/eps_diluted"
	f := repos.facts[k]
	f.Value, f.Source = d("99"), store.SourceManual
	repos.facts[k] = f
	sk := repos.snaps[id]
	sk.Source = store.SourceManual
	sk.Price.Decimal = d("1")
	repos.snaps[id] = sk
	repos.quals[id] = store.Qualitative{InstrumentID: id, BuybackManual: ptr(false)}

	rep, err := doImport(t, repos, data, 2025)
	if err != nil {
		t.Fatal(err)
	}
	if !repos.facts[k].Value.Equal(d("99")) || repos.facts[k].Source != store.SourceManual {
		t.Fatal("valeur manuelle écrasée")
	}
	if repos.conflicts != 1 || rep.Rows[0].Conflicts != 1 || !hasWarning(rep.Rows[0], "conflit") {
		t.Fatalf("conflits = %d, ligne = %+v", repos.conflicts, rep.Rows[0])
	}
	if rep.Rows[0].Snapshot != "protected" || !repos.snaps[id].Price.Decimal.Equal(d("1")) {
		t.Fatalf("instantané manuel non protégé: %+v", rep.Rows[0])
	}
	if *repos.quals[id].BuybackManual {
		t.Fatal("rachat manuel écrasé")
	}
}

func TestMissingSheetsRefused(t *testing.T) {
	t.Parallel()
	f := excelize.NewFile()
	var buf bytes.Buffer
	if err := f.Write(&buf); err != nil {
		t.Fatal(err)
	}
	_, err := doImport(t, newFake(), buf.Bytes(), 2025)
	if !errors.Is(err, ErrInvalidWorkbook) || !strings.Contains(err.Error(), SheetEPS) {
		t.Fatalf("err = %v", err)
	}
	g, err := excelize.OpenFile(templatePath)
	if err != nil {
		t.Fatal(err)
	}
	if err := g.DeleteSheet(SheetYield); err != nil {
		t.Fatal(err)
	}
	buf.Reset()
	if err := g.Write(&buf); err != nil {
		t.Fatal(err)
	}
	_, err = doImport(t, newFake(), buf.Bytes(), 2025)
	if !errors.Is(err, ErrInvalidWorkbook) || !strings.Contains(err.Error(), SheetYield) || strings.Contains(err.Error(), SheetEPS) {
		t.Fatalf("err = %v", err)
	}
}

func TestInvalidInputs(t *testing.T) {
	t.Parallel()
	imp := NewImporter(newFake())
	ctx := context.Background()
	owner := uuid.New()
	for _, o := range []Options{{Year1: 0}, {Year1: 2025, Currency: "XX"}} {
		if _, err := imp.Import(ctx, owner, bytes.NewReader(nil), o); !errors.Is(err, ErrInvalidOptions) {
			t.Fatalf("options %+v: err = %v", o, err)
		}
	}
	if _, err := imp.Import(ctx, owner, strings.NewReader("pas un xlsx"), Options{Year1: 2025}); !errors.Is(err, ErrInvalidWorkbook) {
		t.Fatalf("err = %v", err)
	}
	big := bytes.NewReader(make([]byte, MaxFileSize+1))
	if _, err := imp.Import(ctx, owner, big, Options{Year1: 2025}); !errors.Is(err, ErrTooLarge) {
		t.Fatalf("err = %v", err)
	}
	cctx, cancel := context.WithCancel(ctx)
	cancel()
	if _, err := imp.Import(cctx, owner, bytes.NewReader(buildWorkbook(t, fiveStocks())), Options{Year1: 2025}); err == nil {
		t.Fatal("annulation ignorée")
	}
}

func TestWarningsAndEdgeRows(t *testing.T) {
	t.Parallel()
	specs := []stockSpec{
		{Symbol: "DUP", Name: "A", EPS: map[int]float64{17: 1, 18: 20, 19: 21}},
		{Symbol: "DUP", Name: "B"},
		{Symbol: strings.Repeat("X", 40)},
		{Symbol: "GAP", EPS: map[int]float64{10: 1, 15: 2}, Buyback: "peut-être"},
	}
	f, err := excelize.OpenReader(bytes.NewReader(buildWorkbook(t, specs)))
	if err != nil {
		t.Fatal(err)
	}
	// Texte dans le BPA de la 4e action (k=3) et dans son ratio de distribution.
	_ = f.SetCellValue(SheetEPS, "V18", "abc")
	_ = f.SetCellValue(SheetEPS, "U18", "abc")
	_ = f.SetCellValue(SheetMain, "G10", "n/a")
	var buf bytes.Buffer
	if err := f.Write(&buf); err != nil {
		t.Fatal(err)
	}

	rep, err := doImport(t, newFake(), buf.Bytes(), 2025)
	if err != nil {
		t.Fatal(err)
	}
	if rep.Imported != 2 || rep.Ignored != 2 {
		t.Fatalf("rapport = %+v", rep)
	}
	if !hasWarning(rep.Rows[0], "plus de 10x") {
		t.Fatalf("saut de BPA: %v", rep.Rows[0].Warnings)
	}
	if !hasWarning(rep.Rows[1], "en double") || !hasWarning(rep.Rows[2], "trop long") {
		t.Fatalf("lignes ignorées: %+v %+v", rep.Rows[1], rep.Rows[2])
	}
	g := rep.Rows[3]
	for _, w := range []string{"manquante", "rachat", "non numérique"} {
		if !hasWarning(g, w) {
			t.Fatalf("avertissement %q manquant: %v", w, g.Warnings)
		}
	}
}

func TestNumberParsingAndWriteFailure(t *testing.T) {
	t.Parallel()
	if v, ok, err := number("47%"); err != nil || !ok || !v.Equal(d("0.47")) {
		t.Fatalf("47%% -> %v %v %v", v, ok, err)
	}
	if v, _, _ := number("1,5"); !v.Equal(d("1.5")) {
		t.Fatalf("virgule: %v", v)
	}
	if _, ok, err := number(" "); ok || err != nil {
		t.Fatal("vide")
	}
	repos := newFake()
	repos.failFact = true
	if _, err := doImport(t, repos, buildWorkbook(t, fiveStocks()), 2025); err == nil || !strings.Contains(err.Error(), "panne simulée") {
		t.Fatalf("err = %v", err)
	}
}

func hasWarning(r RowReport, sub string) bool {
	for _, w := range r.Warnings {
		if strings.Contains(w, sub) {
			return true
		}
	}
	return false
}
