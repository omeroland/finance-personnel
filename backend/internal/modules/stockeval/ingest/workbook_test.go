package ingest

import (
	"bytes"
	"math"
	"strconv"
	"testing"

	"github.com/xuri/excelize/v2"
)

const templatePath = "../../../../../testdata/xlsx/simply_investing_template.xlsx"

// stockSpec décrit une action à écrire dans une copie du gabarit (D33).
type stockSpec struct {
	Symbol, Name string
	Payout, Debt *float64
	Buyback      string
	PE, PB       *float64
	Div, Price   *float64
	EPS, DPS     map[int]float64 // clé : indice de colonne 0..19 (0 = B, 19 = U)
	Yield        map[int]float64
}

func fp(v float64) *float64 { return &v }

// buildWorkbook remplit une copie du gabarit et renvoie les octets du classeur.
func buildWorkbook(t testing.TB, specs []stockSpec) []byte {
	t.Helper()
	f, err := excelize.OpenFile(templatePath)
	if err != nil {
		t.Fatalf("ouvrir le gabarit: %v", err)
	}
	defer func() { _ = f.Close() }()
	set := func(sheet, col string, row int, v any) {
		if err := f.SetCellValue(sheet, col+strconv.Itoa(row), v); err != nil {
			t.Fatalf("écrire %s!%s%d: %v", sheet, col, row, err)
		}
	}
	setF := func(sheet, col string, row int, v *float64) {
		if v != nil {
			set(sheet, col, row, *v)
		}
	}
	for k, s := range specs {
		r := firstStockRow + k
		set(SheetMain, "B", r, s.Name)
		set(SheetMain, "C", r, s.Symbol)
		setF(SheetMain, "G", r, s.Payout)
		setF(SheetMain, "H", r, s.Debt)
		if s.Buyback != "" {
			set(SheetMain, "J", r, s.Buyback)
		}
		setF(SheetMain, "K", r, s.PE)
		setF(SheetMain, "O", r, s.PB)
		setF(SheetMain, "P", r, s.Div)
		setF(SheetMain, "Q", r, s.Price)
		for i, v := range s.EPS {
			c, _ := excelize.ColumnNumberToName(2 + i)
			set(SheetEPS, c, firstBlockRow+4*k, v)
		}
		for i, v := range s.DPS {
			c, _ := excelize.ColumnNumberToName(2 + i)
			set(SheetDividend, c, firstBlockRow+4*k, v)
		}
		for i, v := range s.Yield {
			c, _ := excelize.ColumnNumberToName(2 + i)
			set(SheetYield, c, firstBlockRow+2*k, v)
		}
	}
	var buf bytes.Buffer
	if err := f.Write(&buf); err != nil {
		t.Fatalf("écrire le classeur: %v", err)
	}
	return buf.Bytes()
}

// run8 donne une série croissante sur les 8 dernières années.
func run8(start, step float64) map[int]float64 {
	m := map[int]float64{}
	for i := 12; i < 20; i++ {
		m[i] = math.Round((start+step*float64(i-12))*1e4) / 1e4
	}
	return m
}

func run20(start, step float64) map[int]float64 {
	m := map[int]float64{}
	for i := range 20 {
		m[i] = math.Round((start+step*float64(i))*1e4) / 1e4
	}
	return m
}

// fiveStocks est le classeur de test : 5 actions dont une avec 8 ans d'historique
// (AAA) et une avec un BPA nul (EEE).
func fiveStocks() []stockSpec {
	return []stockSpec{
		{
			Symbol: "TSX:RY", Name: "Banque Royale", Payout: fp(0.45), Debt: fp(1.2), Buyback: "yes", PE: fp(12), PB: fp(1.8),
			Div: fp(5.6), Price: fp(140), EPS: run20(4, 0.5), DPS: run20(1, 0.2), Yield: run20(0.03, 0.001),
		},
		{
			Symbol: "AAA", Name: "Historique court", Payout: fp(0.5), Debt: fp(117.92), Buyback: "no", PE: fp(15), PB: fp(2),
			Div: fp(1), Price: fp(50), EPS: run8(1, 0.1), DPS: run8(0.2, 0.02), Yield: run8(0.02, 0.001),
		},
		{
			Symbol: "NYSE:KO", Name: "Boisson", Payout: fp(68.4), Debt: fp(0.7), PE: fp(24), Price: fp(60),
			EPS: run20(1, 0.05), DPS: run20(0.5, 0.03), Yield: map[int]float64{18: 3.1, 19: 3.2},
		},
		{
			Symbol: "TSX:BNS", Name: "Banque Nova", Payout: fp(0.55), Debt: fp(2), Buyback: "oui",
			EPS: run20(3, 0.2), DPS: run20(1, 0.1),
		},
		{
			Symbol: "EEE", Name: "BPA nul", Payout: fp(0), Debt: fp(0.3), Price: fp(10),
			EPS: map[int]float64{16: 0.5, 17: 0.6, 18: 0.7, 19: 0}, DPS: map[int]float64{19: 0.1},
		},
	}
}
