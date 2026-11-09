package ingest

import (
	"bytes"
	"context"
	"errors"
	"sort"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/shopspring/decimal"
)

const csvFixture = "../../../../../testdata/fixtures/transactions_generic.csv"

// txRepo reproduit la sémantique attendue du vrai dépôt (D391) : une ligne
// n'est ajoutée que si le registre compte moins de lignes de même clé que son rang.
type txRepo struct {
	rows     []Transaction
	accounts map[string]bool // nil = tous connus
	err      error
}

func (r *txRepo) SaveTransactions(_ context.Context, _ uuid.UUID, txs []Transaction) ([]Outcome, error) {
	if r.err != nil {
		return nil, r.err
	}
	outs := make([]Outcome, len(txs))
	for i, t := range txs {
		if r.accounts != nil && !r.accounts[t.Account] {
			outs[i] = Outcome{Status: OutcomeRejected, Message: "compte inconnu"}
			continue
		}
		n := 0
		for _, e := range r.rows {
			if e.NaturalKey == t.NaturalKey {
				n++
			}
		}
		if n >= t.Occurrence {
			outs[i] = Outcome{Status: OutcomeDuplicate}
			continue
		}
		r.rows = append(r.rows, t)
		outs[i] = Outcome{Status: OutcomeCreated}
	}
	return outs, nil
}

func readFixture(t *testing.T) []byte {
	t.Helper()
	b, err := readFile(csvFixture)
	if err != nil {
		t.Fatalf("fixture: %v", err)
	}
	return b
}

// positions rebâtit les quantités par (compte, symbole) : achats et DRIP
// ajoutent, ventes retirent, fractionnements multiplient, dans l'ordre des dates.
func positions(txs []Transaction) map[string]decimal.Decimal {
	sorted := append([]Transaction(nil), txs...)
	sort.SliceStable(sorted, func(i, j int) bool { return sorted[i].Date.Before(sorted[j].Date) })
	pos := map[string]decimal.Decimal{}
	for _, t := range sorted {
		k := t.Account + "|" + t.Symbol
		switch t.Type {
		case TxBuy, TxDrip:
			pos[k] = pos[k].Add(*t.Quantity)
		case TxSell:
			pos[k] = pos[k].Sub(*t.Quantity)
		case TxSplit:
			pos[k] = pos[k].Mul(*t.SplitRatio)
		}
	}
	return pos
}

func TestParseCSV_FixtureLineReport(t *testing.T) {
	t.Parallel()
	p, rep, err := ParseCSV(context.Background(), bytes.NewReader(readFixture(t)), CSVOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if rep.Delimiter != ";" || rep.Rows != 11 || rep.Valid != 8 || rep.Invalid != 3 {
		t.Fatalf("rapport inattendu: %+v", rep)
	}
	badLines := map[int]string{}
	for _, e := range rep.Entries {
		if e.Level == LevelError {
			badLines[e.Line] = e.Column
		}
	}
	want := map[int]string{10: "date", 11: "type", 12: "quantité"}
	if len(badLines) != 3 {
		t.Fatalf("erreurs: %v", badLines)
	}
	for l, c := range want {
		if badLines[l] != c {
			t.Errorf("ligne %d: colonne %q, attendu %q", l, badLines[l], c)
		}
	}
	if d := p.Transactions[6]; d.Type != TxDividend || d.Symbol != "TSX:VFV" || d.Amount.String() != "12.34" {
		t.Errorf("dividende: %+v", d)
	}
}

func TestImportCSV_IdempotentAndPositions(t *testing.T) {
	t.Parallel()
	repo := &txRepo{}
	im := NewTxImporter(repo)
	owner := uuid.New()
	data := readFixture(t)

	_, rep, err := im.ImportCSV(context.Background(), owner, bytes.NewReader(data), CSVOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if rep.Created != 8 || rep.Duplicates != 0 || len(repo.rows) != 8 {
		t.Fatalf("premier import: %+v", rep)
	}
	got := positions(repo.rows)
	want := map[string]string{"CELI|TSX:XEQT": "121.5", "CELI|TSX:VFV": "10.5", "REER|TSX:XEQT": "5"}
	for k, w := range want {
		if g := got[k]; g.String() != w {
			t.Errorf("position %s = %s, attendu %s", k, g, w)
		}
	}
	// Ré-import du même fichier : zéro ajout.
	_, rep, err = im.ImportCSV(context.Background(), owner, bytes.NewReader(data), CSVOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if rep.Created != 0 || rep.Duplicates != 8 || len(repo.rows) != 8 {
		t.Fatalf("ré-import: %+v lignes=%d", rep, len(repo.rows))
	}
	// Variante virgule + BOM + en-têtes en majuscules : mêmes clés, rien d'ajouté.
	alt := "\xef\xbb\xbfDATE,COMPTE,TYPE,SYMBOLE,QUANTITE,PRIX,DEVISE\n2024-01-15,CELI,buy,XEQT,100,\"30,50\",CAD\n"
	_, rep, err = im.ImportCSV(context.Background(), owner, strings.NewReader(alt), CSVOptions{})
	if err != nil || rep.Created != 0 || rep.Duplicates != 1 || rep.Delimiter != "," {
		t.Fatalf("variante: %v %+v", err, rep)
	}
}

func TestImportCSV_SameDayIdenticalBuysKept(t *testing.T) {
	t.Parallel()
	repo := &txRepo{}
	im := NewTxImporter(repo)
	in := "date,compte,type,symbole,quantité,prix,devise\n2024-01-02,CELI,buy,XEQT,1,10,CAD\n2024-01-02,CELI,buy,XEQT,1,10,CAD\n"
	_, rep, _ := im.ImportCSV(context.Background(), uuid.New(), strings.NewReader(in), CSVOptions{})
	if rep.Created != 2 {
		t.Fatalf("%+v", rep)
	}
	_, rep, _ = im.ImportCSV(context.Background(), uuid.New(), strings.NewReader(in), CSVOptions{})
	if rep.Created != 0 || rep.Duplicates != 2 {
		t.Fatalf("%+v", rep)
	}
}

func TestImportCSV_RepoRejectionAndErrors(t *testing.T) {
	t.Parallel()
	in := "date,compte,type,montant\n2024-01-02,CELI,contribution,100\n2024-01-03,AUTRE,fee,5\n"
	repo := &txRepo{accounts: map[string]bool{"CELI": true}}
	_, rep, err := NewTxImporter(repo).ImportCSV(context.Background(), uuid.New(), strings.NewReader(in), CSVOptions{})
	if err != nil || rep.Created != 1 || rep.Rejected != 1 {
		t.Fatalf("%v %+v", err, rep)
	}
	boom := errors.New("boom")
	_, _, err = NewTxImporter(&txRepo{err: boom}).ImportCSV(context.Background(), uuid.New(), strings.NewReader(in), CSVOptions{})
	if !errors.Is(err, boom) {
		t.Fatalf("erreur du dépôt non enveloppée: %v", err)
	}
	if _, _, err = NewTxImporter(nil).ImportCSV(context.Background(), uuid.New(), strings.NewReader(in), CSVOptions{}); !errors.Is(err, ErrInvalidOptions) {
		t.Fatalf("dépôt absent: %v", err)
	}
}

func TestParseCSV_FileErrors(t *testing.T) {
	t.Parallel()
	cancelled, cancel := context.WithCancel(context.Background())
	cancel()
	cases := []struct {
		name string
		ctx  context.Context
		in   string
		want error
	}{
		{"vide", context.Background(), "", ErrEmptyCSV},
		{"espaces", context.Background(), " \n\n", ErrEmptyCSV},
		{"BOM seul", context.Background(), "\xef\xbb\xbf", ErrEmptyCSV},
		{"sans en-têtes reconnus", context.Background(), "a,b,c\n1,2,3\n", ErrCSVHeader},
		{"colonne manquante", context.Background(), "date,compte\n", ErrCSVHeader},
		{"colonne en double", context.Background(), "date,date,compte,type\n", ErrCSVHeader},
		{"trop gros", context.Background(), "date,compte,type\n" + strings.Repeat("x", MaxFileSize), ErrTooLarge},
		{"contexte annulé", cancelled, "date,compte,type\n", context.Canceled},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()
			_, _, err := ParseCSV(c.ctx, strings.NewReader(c.in), CSVOptions{})
			if !errors.Is(err, c.want) {
				t.Fatalf("err = %v, attendu %v", err, c.want)
			}
		})
	}
	// En-têtes seuls : aucune ligne, pas d'erreur.
	p, rep, err := ParseCSV(context.Background(), strings.NewReader("date,compte,type\n"), CSVOptions{})
	if err != nil || len(p.Transactions) != 0 || rep.Rows != 0 {
		t.Fatalf("en-têtes seuls: %v %+v", err, rep)
	}
}

func TestParseCSV_RowValidation(t *testing.T) {
	t.Parallel()
	hdr := "date,compte,type,symbole,quantité,prix,devise,montant,ratio,contrepartie\n"
	rows := []struct {
		row     string
		invalid bool
		col     string // colonne de la première entrée ; "" = non vérifiée
	}{
		{"2024-01-02,CELI,buy,XEQT,1,10,CAD,,,", false, ""},
		{"2024-01-02,CELI,buy,XEQT,1,10,EUR,,,", true, "devise"},
		{"2024-01-02,CELI,buy,xe qt,1,10,CAD,,,", true, "symbole"},
		{"2024-01-02,CELI,buy,XEQT,1,,CAD,,,", true, ""},
		{"2024-01-02,CELI,buy,XEQT,0,10,CAD,,,", true, "quantité"},
		{"2024-01-02,CELI,buy,XEQT,1e3,10,CAD,,,", true, "quantité"},
		{"2024-01-02,CELI,dividend,XEQT,,,CAD,,,", true, ""},
		{"2024-01-02,CELI,split,XEQT,,,CAD,,0,", true, "ratio"},
		{"2024-01-02,CELI,transfer,,,,CAD,50,,CELI", true, "contrepartie"},
		{"2024-01-02,CELI,transfer,,,,CAD,50,,REER", false, ""},
		{"2024-01-02,,fee,,,,CAD,5,,", true, "compte"},
		{"2024-01-02,CELI,fee,,,,,5,,", false, "devise"},
	}
	for i, c := range rows {
		_, rep, err := ParseCSV(context.Background(), strings.NewReader(hdr+c.row+"\n"), CSVOptions{})
		if err != nil {
			t.Fatalf("ligne %d: %v", i, err)
		}
		if (rep.Invalid == 1) != c.invalid {
			t.Errorf("ligne %d (%s): invalid=%d (%+v)", i, c.row, rep.Invalid, rep.Entries)
		}
		if c.col != "" && (len(rep.Entries) == 0 || rep.Entries[0].Column != c.col) {
			t.Errorf("ligne %d: entrées %+v, colonne attendue %s", i, rep.Entries, c.col)
		}
	}
}

func TestParseDecimal(t *testing.T) {
	t.Parallel()
	ok := map[string]string{
		"12,5": "12.5", "12.5": "12.5", "1 234,56": "1234.56", "1 234,56": "1234.56",
		"1,234.56": "1234.56", "1.234,56": "1234.56", "$100": "100", "0,1234567": "0.123457",
	}
	for in, want := range ok {
		d, _, err := parseDecimal(in)
		if err != nil || d.String() != want {
			t.Errorf("%q = %v (%v), attendu %s", in, d, err, want)
		}
	}
	for _, in := range []string{"", "abc", "1,2,3", "1e5", "--1", "NaN", "123456789012345678"} {
		if _, _, err := parseDecimal(in); err == nil {
			t.Errorf("%q devrait échouer", in)
		}
	}
	a, _, _ := parseDecimal("0,1")
	b, _, _ := parseDecimal("0,2")
	if a.Add(b).String() != "0.3" {
		t.Error("addition inexacte")
	}
}

func TestParseCSV_MalformedQuoteDoesNotStopOthers(t *testing.T) {
	t.Parallel()
	in := "date,compte,type,montant\n2024-01-02,CELI,fee,1\n2024-01-03,\"CE\"LI,fee,2\n2024-01-04,CELI,fee,3\n"
	p, rep, err := ParseCSV(context.Background(), strings.NewReader(in), CSVOptions{})
	if err != nil || len(p.Transactions) < 1 || rep.Invalid < 1 {
		t.Fatalf("%v %+v", err, rep)
	}
}
