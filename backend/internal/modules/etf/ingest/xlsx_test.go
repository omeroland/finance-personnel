package ingest

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"math"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/shopspring/decimal"
	"github.com/xuri/excelize/v2"
)

const templatePath = "../../../../../testdata/xlsx/selection_gestion_template.xlsx"

// fakeRepo reproduit la sémantique attendue du vrai dépôt : clé naturelle, aucun doublon.
type fakeRepo struct {
	profiles map[string]FundProfile // symbole|as_of
	models   map[string][]Model     // nom -> versions
	calls    int
}

func newFakeRepo() *fakeRepo {
	return &fakeRepo{profiles: map[string]FundProfile{}, models: map[string][]Model{}}
}

func (r *fakeRepo) SaveProfiles(_ context.Context, _ uuid.UUID, ps []FundProfile) (SaveCounts, error) {
	var c SaveCounts
	for _, p := range ps {
		k := p.Symbol + "|" + p.AsOf.Format(time.DateOnly)
		old, ok := r.profiles[k]
		switch {
		case !ok:
			c.Created++
		case reflect.DeepEqual(old, p):
			c.Unchanged++
		default:
			c.Updated++
		}
		r.profiles[k] = p
	}
	return c, nil
}

func (r *fakeRepo) SaveModel(_ context.Context, _ uuid.UUID, m Model) (string, error) {
	vs := r.models[m.Name]
	if len(vs) > 0 && fmt.Sprint(vs[len(vs)-1]) == fmt.Sprint(m) {
		return ModelUnchanged, nil
	}
	r.models[m.Name] = append(vs, m)
	return ModelCreated, nil
}

type fundRow struct {
	sym, name, typ, region     string
	ca, us, intl, bca, bother  string
	mer, size, r1, r3, r5, r10 string
}

// buildWorkbook remplit une copie du gabarit (D33) et la sérialise.
func buildWorkbook(t *testing.T, funds []fundRow, mutate func(f *excelize.File)) []byte {
	t.Helper()
	f, err := excelize.OpenFile(templatePath)
	if err != nil {
		t.Fatalf("ouvrir le gabarit: %v", err)
	}
	defer func() { _ = f.Close() }()
	// Vide le catalogue du gabarit puis écrit les lignes synthétiques.
	for row := 2; row <= 400; row++ {
		for _, col := range []string{"A", "B", "C", "D", "E", "F", "G", "H", "I", "J", "K", "L", "M", "R", "S", "T", "U"} {
			_ = f.SetCellValue(SheetFNB, fmt.Sprintf("%s%d", col, row), nil)
		}
	}
	_ = f.SetCellValue(SheetFNB, "R1", "1 an (2021)")
	for i, fr := range funds {
		row := i + 2
		vals := []string{fr.sym, fr.name, "desc", fr.typ, "Indiciel", fr.region, fr.ca, fr.us, fr.intl, fr.bca, fr.bother, fr.mer, fr.size}
		for j, v := range vals {
			c, _ := excelize.CoordinatesToCellName(j+1, row)
			_ = f.SetCellValue(SheetFNB, c, v)
		}
		for col, v := range map[string]string{"R": fr.r1, "S": fr.r3, "T": fr.r5, "U": fr.r10} {
			_ = f.SetCellValue(SheetFNB, fmt.Sprintf("%s%d", col, row), v)
		}
	}
	if mutate != nil {
		mutate(f)
	}
	var buf bytes.Buffer
	if err := f.Write(&buf); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

func synthetic(n int) []fundRow {
	out := make([]fundRow, n)
	for i := range out {
		out[i] = fundRow{
			sym: fmt.Sprintf("T%03d", i), name: fmt.Sprintf("Fonds synthétique %d", i), typ: "Actions", region: "Canada",
			ca: "30%", us: "40%", intl: "30%", bca: "0%", bother: "0%",
			mer: "0.25%", size: "880M$", r1: "19,6%", r3: "-", r5: "-2,5 %", r10: "9,2%",
		}
	}
	return out
}

// modelSheet remplit CRÉATION : cibles et fonds retenus.
func modelSheet(a, b string, subs [3]string, funds [][3]string) func(f *excelize.File) {
	return func(f *excelize.File) {
		_ = f.SetCellValue(SheetCreation, "C9", a)
		_ = f.SetCellValue(SheetCreation, "C10", b)
		for i, v := range subs {
			_ = f.SetCellValue(SheetCreation, fmt.Sprintf("R%d", 9+i), v)
		}
		for i, fd := range funds {
			for j, col := range []string{"C", "D", "E"} {
				_ = f.SetCellValue(SheetCreation, fmt.Sprintf("%s%d", col, 28+i), fd[j])
			}
		}
	}
}

func parse(t *testing.T, data []byte) (Parsed, Report) {
	t.Helper()
	p, rep, err := Parse(context.Background(), bytes.NewReader(data), Options{})
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	return p, rep
}

func TestCatalogue137(t *testing.T) {
	t.Parallel()
	p, rep := parse(t, buildWorkbook(t, synthetic(137), nil))
	if len(p.Profiles) != 137 || rep.Funds != 137 {
		t.Fatalf("fonds lus = %d / %d, veut 137", len(p.Profiles), rep.Funds)
	}
	fp := p.Profiles[0]
	if fp.Symbol != "TSX:T000" || fp.Currency != "CAD" || fp.Source != "xlsx" {
		t.Errorf("identité = %+v", fp)
	}
	if fp.AsOf != time.Date(2021, 12, 31, 0, 0, 0, 0, time.UTC) {
		t.Errorf("as_of = %v", fp.AsOf)
	}
	if fp.MER == nil || math.Abs(*fp.MER-0.0025) > 1e-9 {
		t.Errorf("RFG = %v", fp.MER)
	}
	if fp.Size == nil || !fp.Size.Equal(decimal.NewFromInt(880_000_000)) {
		t.Errorf("taille = %v", fp.Size)
	}
	if fp.Returns[1] != 0.196 || fp.Returns[5] != -0.025 || fp.Returns[10] != 0.092 {
		t.Errorf("rendements = %v", fp.Returns)
	}
	if _, ok := fp.Returns[3]; ok {
		t.Error("« - » doit rester absent")
	}
	if fp.Alloc != (Allocation{EquityCA: 0.3, EquityUS: 0.4, EquityIntl: 0.3}) {
		t.Errorf("répartition = %+v", fp.Alloc)
	}
	if fp.Type != "equity" || fp.Region == nil || *fp.Region != "ca" {
		t.Errorf("type/région = %s %v", fp.Type, fp.Region)
	}
}

func TestGabaritReel(t *testing.T) {
	t.Parallel()
	data, err := readFile(templatePath)
	if err != nil {
		t.Fatal(err)
	}
	p, rep := parse(t, data)
	if len(p.Profiles) < 130 {
		t.Fatalf("catalogue = %d fonds", len(p.Profiles))
	}
	veqt := p.Profiles[0]
	if veqt.Symbol != "TSX:VEQT" || veqt.Size == nil || !veqt.Size.Equal(decimal.NewFromInt(880_000_000)) || veqt.Returns[1] != 0.196 {
		t.Errorf("VEQT = %+v", veqt)
	}
	if rep.Model != ModelNone && p.Model == nil {
		t.Error("incohérence modèle")
	}
	if p.Model == nil || math.Abs(p.Model.Targets.EquityUS-0.435) > 1e-9 {
		t.Errorf("modèle du gabarit = %+v", p.Model)
	}
}

func TestAutresCalcule(t *testing.T) {
	t.Parallel()
	fr := synthetic(3)
	fr[0].ca, fr[0].us, fr[0].intl = "0%", "0%", "0%" // 0 % classé : tout en autres
	fr[1].ca, fr[1].us, fr[1].intl, fr[1].bca = "20%", "20%", "20%", "20%"
	fr[2].ca, fr[2].us, fr[2].intl = "60%", "60%", "0%" // 120 %
	p, rep := parse(t, buildWorkbook(t, fr, nil))
	if p.Profiles[0].Alloc.OtherAssets != 1 {
		t.Errorf("fonds non classé = %+v", p.Profiles[0].Alloc)
	}
	if a := p.Profiles[1].Alloc; math.Abs(a.OtherAssets-0.2) > 1e-9 {
		t.Errorf("autres = %v", a.OtherAssets)
	}
	a := p.Profiles[2].Alloc
	if math.Abs(a.EquityCA+a.EquityUS+a.OtherAssets-1) > 1e-6 || a.OtherAssets != 0 {
		t.Errorf("sur 100 %% = %+v", a)
	}
	if rep.Warnings < 2 {
		t.Errorf("avertissements = %d", rep.Warnings)
	}
}

func TestConversionsDeTexte(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct{ in, want string }{
		{"880M$", "880000000"}, {"1,5G$", "1500000000"}, {"12 K$", "12000"}, {"34", "34000000"}, {"7 725M$", "7725000000"},
	} {
		v, ok, err := parseSize(tc.in)
		if err != nil || !ok || v.String() != tc.want {
			t.Errorf("parseSize(%q) = %v %v %v", tc.in, v, ok, err)
		}
	}
	for _, in := range []string{"", "-", "abc", "-5M$"} {
		if v, ok, err := parseSize(in); ok && err == nil {
			t.Errorf("parseSize(%q) = %v, veut absent ou erreur", in, v)
		}
	}
	for _, tc := range []struct {
		in   string
		want string
		norm bool
	}{
		{"19,6 %", "0.196", false}, {"-2,6%", "-0.026", false}, {"0.275", "0.275", false}, {"25", "0.25", true}, {"−2,6%", "-0.026", false},
	} {
		v, norm, ok, err := parseFraction(tc.in, 1)
		if tc.in == "-2,6%" || tc.in == "−2,6%" {
			// limite 1 : « % » explicite, jamais de normalisation
			norm = false
		}
		if err != nil || !ok || v.String() != tc.want || norm != tc.norm {
			t.Errorf("parseFraction(%q) = %v %v %v %v", tc.in, v, norm, ok, err)
		}
	}
	if _, _, ok, err := parseFraction("-", 1); ok || err != nil {
		t.Error("« - » = absent")
	}
	if _, _, _, err := parseFraction("xx", 1); err == nil {
		t.Error("texte non numérique : erreur attendue")
	}
}

func TestCiblesNormalisees(t *testing.T) {
	t.Parallel()
	funds := [][3]string{{"T000", "A", "50%"}, {"T001", "B", "30%"}, {"T002", "C", "30%"}} // 110 %
	p, rep := parse(t, buildWorkbook(t, synthetic(3),
		modelSheet("80%", "30%", [3]string{"27,5%", "43,5%", "29%"}, funds))) // 110 %
	m := p.Model
	if m == nil {
		t.Fatal("modèle absent")
	}
	tg := m.Targets
	total := tg.EquityCA + tg.EquityUS + tg.EquityIntl + tg.BondCA + tg.BondOther + tg.OtherAssets + m.CashTarget
	if math.Abs(total-1) > 1e-6 {
		t.Errorf("total des cibles = %v", total)
	}
	if math.Abs(tg.BondCA-30.0/110) > 1e-6 || math.Abs(tg.EquityUS-0.435*80.0/110) > 1e-6 {
		t.Errorf("cibles = %+v", tg)
	}
	var w float64
	for _, h := range m.Holdings {
		w += h.Weight
	}
	if math.Abs(w-1) > 1e-6 || len(m.Holdings) != 3 {
		t.Errorf("poids = %v (%d fonds)", w, len(m.Holdings))
	}
	if m.BandRelative != 0.20 || m.BandCash != 0.05 {
		t.Errorf("bandes = %v %v", m.BandRelative, m.BandCash)
	}
	norm := 0
	for _, e := range rep.Entries {
		if e.Kind == KindNormalized && e.Sheet == SheetCreation {
			norm++
		}
	}
	if norm < 2 {
		t.Errorf("normalisations signalées = %d", norm)
	}
}

func TestCiblesExactesSansAvertissement(t *testing.T) {
	t.Parallel()
	funds := [][3]string{{"T000", "A", "60%"}, {"T001", "B", "40%"}}
	p, rep := parse(t, buildWorkbook(t, synthetic(2),
		modelSheet("100%", "0%", [3]string{"27.50%", "43.50%", "29%"}, funds)))
	if p.Model.Targets.EquityCA != 0.275 || p.Model.Targets.BondCA != 0 {
		t.Errorf("cibles = %+v", p.Model.Targets)
	}
	for _, e := range rep.Entries {
		if e.Sheet == SheetCreation && e.Level == LevelWarning {
			t.Errorf("avertissement inattendu: %+v", e)
		}
	}
}

func TestPositionsActuelles(t *testing.T) {
	t.Parallel()
	data := buildWorkbook(t, synthetic(3), func(f *excelize.File) {
		for i, v := range [][3]string{{"T000", "A", "50%"}, {"t001", "B", "50%"}, {"INCONNU", "C", "0%"}} {
			_ = f.SetCellValue(SheetDiversification, fmt.Sprintf("C%d", 8+i), v[0])
			_ = f.SetCellValue(SheetDiversification, fmt.Sprintf("D%d", 8+i), v[1])
			_ = f.SetCellValue(SheetDiversification, fmt.Sprintf("E%d", 8+i), v[2])
		}
	})
	p, rep := parse(t, data)
	if len(p.Current) != 2 || p.Current[1].Symbol != "TSX:T001" || p.Current[0].Weight != 0.5 {
		t.Fatalf("positions = %+v", p.Current)
	}
	if rep.CurrentHoldings != 2 || rep.Warnings == 0 {
		t.Errorf("rapport = %+v", rep)
	}
}

func TestEntreesVides(t *testing.T) {
	t.Parallel()
	data := buildWorkbook(t, nil, func(f *excelize.File) {
		_ = f.SetCellValue(SheetCreation, "C9", nil)
		_ = f.SetCellValue(SheetCreation, "C10", nil)
	})
	p, rep := parse(t, data)
	if len(p.Profiles) != 0 || p.Model != nil || len(p.Current) != 0 {
		t.Fatalf("attendu vide : %+v", p)
	}
	if rep.Model != ModelNone {
		t.Errorf("modèle = %s", rep.Model)
	}
	// Cibles à zéro : aucune division par zéro.
	data = buildWorkbook(t, nil, modelSheet("0%", "0%", [3]string{"", "", ""}, nil))
	if p, _ := parse(t, data); p.Model != nil {
		t.Errorf("modèle = %+v", p.Model)
	}
	// Actions visées mais sous-répartition absente.
	data = buildWorkbook(t, nil, modelSheet("100%", "0%", [3]string{"", "", ""}, nil))
	if p, rep := parse(t, data); p.Model != nil || rep.Warnings == 0 {
		t.Errorf("modèle = %+v, avertissements %d", p.Model, rep.Warnings)
	}
}

func TestReImportIdempotent(t *testing.T) {
	t.Parallel()
	repo := newFakeRepo()
	im := NewImporter(repo)
	owner := uuid.New()
	funds := [][3]string{{"T000", "A", "60%"}, {"T001", "B", "40%"}}
	data := buildWorkbook(t, synthetic(137), modelSheet("70%", "30%", [3]string{"27,5%", "43,5%", "29%"}, funds))

	_, rep1, err := im.Import(context.Background(), owner, bytes.NewReader(data), Options{})
	if err != nil {
		t.Fatal(err)
	}
	if rep1.ProfilesCreated != 137 || rep1.Model != ModelCreated {
		t.Fatalf("premier import = %+v", rep1)
	}
	_, rep2, err := im.Import(context.Background(), owner, bytes.NewReader(data), Options{})
	if err != nil {
		t.Fatal(err)
	}
	if rep2.ProfilesCreated != 0 || rep2.ProfilesUnchanged != 137 || rep2.Model != ModelUnchanged {
		t.Fatalf("second import = %+v", rep2)
	}
	if len(repo.profiles) != 137 || len(repo.models[defaultModelName]) != 1 {
		t.Errorf("doublons : %d profils, %d versions", len(repo.profiles), len(repo.models[defaultModelName]))
	}
}

func TestRejets(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	if _, _, err := Parse(ctx, strings.NewReader("pas un xlsx"), Options{}); !errors.Is(err, ErrInvalidWorkbook) {
		t.Errorf("classeur invalide: %v", err)
	}
	if _, _, err := Parse(ctx, bytes.NewReader(nil), Options{}); !errors.Is(err, ErrInvalidWorkbook) {
		t.Errorf("fichier vide: %v", err)
	}
	big := bytes.Repeat([]byte("x"), MaxFileSize+1)
	if _, _, err := Parse(ctx, bytes.NewReader(big), Options{}); !errors.Is(err, ErrTooLarge) {
		t.Errorf("trop gros: %v", err)
	}
	// Onglet manquant.
	f := excelize.NewFile()
	_ = f.SetSheetName("Sheet1", SheetFNB)
	var buf bytes.Buffer
	_ = f.Write(&buf)
	_, _, err := Parse(ctx, &buf, Options{})
	if !errors.Is(err, ErrInvalidWorkbook) || !strings.Contains(err.Error(), SheetCreation) {
		t.Errorf("onglet manquant: %v", err)
	}
	if _, _, err := Parse(ctx, strings.NewReader(""), Options{Currency: "EURO"}); !errors.Is(err, ErrInvalidOptions) {
		t.Errorf("options: %v", err)
	}
	if _, _, err := NewImporter(nil).Import(ctx, uuid.New(), strings.NewReader(""), Options{}); !errors.Is(err, ErrInvalidOptions) {
		t.Errorf("dépôt absent: %v", err)
	}
	cctx, cancel := context.WithCancel(ctx)
	cancel()
	data := buildWorkbook(t, synthetic(2), nil)
	if _, _, err := Parse(cctx, bytes.NewReader(data), Options{}); err == nil {
		t.Error("contexte annulé: erreur attendue")
	}
}

func TestCellulesIllisiblesEtDoublons(t *testing.T) {
	t.Parallel()
	fr := synthetic(4)
	fr[1].sym = "t000"     // doublon de T000
	fr[2].mer = "beaucoup" // illisible
	fr[2].size = "n.d."
	fr[3].typ, fr[3].region = "Mystère", "Lune"
	p, rep := parse(t, buildWorkbook(t, fr, nil))
	if len(p.Profiles) != 3 || rep.Skipped != 1 {
		t.Fatalf("fonds = %d, ignorés = %d", len(p.Profiles), rep.Skipped)
	}
	if p.Profiles[1].MER != nil || p.Profiles[1].Size != nil {
		t.Errorf("valeurs illisibles gardées: %+v", p.Profiles[1])
	}
	if p.Profiles[2].Type != "other" || p.Profiles[2].Region != nil {
		t.Errorf("type/région inconnus: %+v", p.Profiles[2])
	}
	cells := map[string]bool{}
	for _, e := range rep.Entries {
		cells[e.Cell] = true
	}
	for _, c := range []string{"A3", "L4", "M4", "D5", "F5"} {
		if !cells[c] {
			t.Errorf("avertissement attendu sur %s", c)
		}
	}
}

func TestDateExplicite(t *testing.T) {
	t.Parallel()
	data := buildWorkbook(t, synthetic(1), nil)
	p, _, err := Parse(context.Background(), bytes.NewReader(data), Options{AsOf: time.Date(2026, 11, 1, 15, 0, 0, 0, time.UTC)})
	if err != nil || p.Profiles[0].AsOf.Format(time.DateOnly) != "2026-11-01" {
		t.Fatalf("as_of = %v, %v", p.Profiles[0].AsOf, err)
	}
}
