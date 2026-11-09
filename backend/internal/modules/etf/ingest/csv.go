// Import CSV générique de transactions (D10, D390 à D395).
//
// Le format est celui de l'app, sans adapter par courtier (phase 6) : voir
// docs/import-csv.md. ParseCSV est pure (aucune écriture) ; TxImporter.ImportCSV
// y ajoute l'écriture par l'interface TransactionRepository, que les handlers
// (P2-10) implémenteront sur le registre en insertion seule.

package ingest

import (
	"bytes"
	"context"
	"encoding/csv"
	"errors"
	"fmt"
	"io"
	"regexp"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/shopspring/decimal"
)

// Types de transaction (D310).
const (
	TxBuy          = "buy"
	TxSell         = "sell"
	TxDividend     = "dividend"
	TxDrip         = "drip"
	TxContribution = "contribution"
	TxWithdrawal   = "withdrawal"
	TxFee          = "fee"
	TxTransfer     = "transfer"
	TxSplit        = "split"
)

// SourceCSV est la source inscrite au registre (colonne `source`).
const SourceCSV = "csv"

// Catégories et niveau des lignes du compte rendu CSV.
const (
	LevelError    = "error"
	KindDuplicate = "duplicate"
	KindInvalid   = "invalid"
	KindRejected  = "rejected"
)

// Erreurs de l'import CSV.
var (
	ErrEmptyCSV   = errors.New("fichier CSV vide")
	ErrCSVHeader  = errors.New("en-têtes CSV invalides")
	ErrBadOutcome = errors.New("réponse du dépôt incohérente")
)

// Transaction est une ligne valide, prête pour le registre. Quantités, prix,
// montants et frais sont positifs : le sens vient du type (D321).
type Transaction struct {
	Account     string // nom du compte ; résolu par le dépôt
	Type        string
	Date        time.Time
	Symbol      string // « BOURSE:SYMBOLE »
	Quantity    *decimal.Decimal
	Price       *decimal.Decimal
	Amount      *decimal.Decimal
	Fee         *decimal.Decimal
	SplitRatio  *decimal.Decimal
	Counterpart string // nom de l'autre compte d'un transfert
	Currency    string // CAD ou USD
	Note        string
	Source      string // toujours « csv »
	Line        int    // numéro de ligne dans le fichier
	Occurrence  int    // rang (1, 2…) parmi les lignes de même clé du fichier
	NaturalKey  string // clé naturelle, sans le rang
}

// TxParsed est le contenu lu du fichier.
type TxParsed struct {
	Transactions []Transaction
}

// CSVOptions sont les paramètres d'un import CSV.
type CSVOptions struct {
	// Exchange préfixe les symboles nus (défaut « TSX »).
	Exchange string
}

// LineEntry est une entrée du compte rendu, située à la ligne près.
type LineEntry struct {
	Line    int    `json:"line"`
	Level   string `json:"level"` // info, warning, error
	Kind    string `json:"kind,omitempty"`
	Column  string `json:"column,omitempty"`
	Message string `json:"message"`
}

// CSVReport est le compte rendu ligne par ligne.
type CSVReport struct {
	Delimiter  string      `json:"delimiter"`
	Rows       int         `json:"rows"`       // lignes de données lues
	Valid      int         `json:"valid"`      // lignes valides
	Invalid    int         `json:"invalid"`    // lignes en erreur
	Created    int         `json:"created"`    // écritures ajoutées
	Duplicates int         `json:"duplicates"` // déjà au registre
	Rejected   int         `json:"rejected"`   // refusées par le dépôt
	Warnings   int         `json:"warnings"`
	Entries    []LineEntry `json:"entries"`
}

func (r *CSVReport) add(line int, level, kind, column, format string, a ...any) {
	if level == LevelWarning {
		r.Warnings++
	}
	r.Entries = append(r.Entries, LineEntry{Line: line, Level: level, Kind: kind, Column: column, Message: fmt.Sprintf(format, a...)})
}

// Statuts rendus par le dépôt pour chaque écriture.
const (
	OutcomeCreated   = "created"
	OutcomeDuplicate = "duplicate"
	OutcomeRejected  = "rejected"
)

// Outcome est le sort d'une transaction côté dépôt.
type Outcome struct {
	Status  string
	Message string // motif d'un refus (compte inconnu, devise du compte…)
}

// TransactionRepository est le dépôt à injecter. SaveTransactions est
// idempotent et rend un Outcome par transaction, dans l'ordre reçu :
//   - une transaction n'est ajoutée que si le registre compte moins de lignes
//     de même clé naturelle (compte, date, type, symbole, quantité, prix,
//     montant, ratio) que son Occurrence : un ré-import n'ajoute rien, deux
//     achats identiques le même jour dans le fichier restent deux achats ;
//   - un compte inconnu ou une contrepartie inconnue donne « rejected » ;
//   - tout est écrit dans une transaction SQL et audité (backend/CLAUDE.md).
type TransactionRepository interface {
	SaveTransactions(ctx context.Context, owner uuid.UUID, txs []Transaction) ([]Outcome, error)
}

// TxImporter lit des CSV et écrit dans le dépôt.
type TxImporter struct{ repo TransactionRepository }

// NewTxImporter construit l'importeur de transactions.
func NewTxImporter(repo TransactionRepository) *TxImporter { return &TxImporter{repo: repo} }

// ImportCSV lit r puis écrit les lignes valides ; les lignes invalides sont
// signalées sans empêcher les autres.
func (im *TxImporter) ImportCSV(ctx context.Context, owner uuid.UUID, r io.Reader, opts CSVOptions) (TxParsed, CSVReport, error) {
	if im.repo == nil {
		return TxParsed{}, CSVReport{}, fmt.Errorf("dépôt absent: %w", ErrInvalidOptions)
	}
	p, rep, err := ParseCSV(ctx, r, opts)
	if err != nil || len(p.Transactions) == 0 {
		return p, rep, err
	}
	if err := ctx.Err(); err != nil {
		return p, rep, fmt.Errorf("import interrompu: %w", err)
	}
	outs, err := im.repo.SaveTransactions(ctx, owner, p.Transactions)
	if err != nil {
		return p, rep, fmt.Errorf("écrire les transactions: %w", err)
	}
	if len(outs) != len(p.Transactions) {
		return p, rep, fmt.Errorf("%d résultats pour %d transactions: %w", len(outs), len(p.Transactions), ErrBadOutcome)
	}
	for i, o := range outs {
		line := p.Transactions[i].Line
		switch o.Status {
		case OutcomeCreated:
			rep.Created++
		case OutcomeDuplicate:
			rep.Duplicates++
			rep.add(line, LevelInfo, KindDuplicate, "", "déjà au registre, ignorée")
		case OutcomeRejected:
			rep.Rejected++
			rep.add(line, LevelError, KindRejected, "", "refusée: %s", o.Message)
		default:
			return p, rep, fmt.Errorf("statut %q: %w", o.Status, ErrBadOutcome)
		}
	}
	return p, rep, nil
}

// Colonnes reconnues : en-têtes sans accent, en minuscules, vers le nom interne.
var csvAliases = map[string]string{
	"date":   "date",
	"compte": "account", "account": "account",
	"type":    "type",
	"symbole": "symbol", "symbol": "symbol",
	"quantite": "quantity", "quantity": "quantity",
	"prix": "price", "price": "price",
	"devise": "currency", "currency": "currency",
	"frais": "fee", "fee": "fee", "fees": "fee",
	"montant": "amount", "amount": "amount",
	"ratio": "ratio", "split_ratio": "ratio",
	"contrepartie": "counterpart", "counterpart": "counterpart",
	"note": "note",
}

var accentFold = strings.NewReplacer("é", "e", "è", "e", "ê", "e", "à", "a", "â", "a", "ô", "o", "î", "i", "ï", "i", "ç", "c", "û", "u", "ù", "u")

func canonHeader(s string) string {
	return accentFold.Replace(strings.ToLower(strings.TrimSpace(s)))
}

var (
	symbolRe  = regexp.MustCompile(`^[A-Z]{2,10}:[A-Z0-9.\-]{1,15}$`)
	bareRe    = regexp.MustCompile(`^[A-Z0-9][A-Z0-9.\-]{0,14}$`)
	numberRe  = regexp.MustCompile(`^[0-9]{1,14}(\.[0-9]+)?$`)
	validType = map[string]bool{
		TxBuy: true, TxSell: true, TxDividend: true, TxDrip: true, TxContribution: true,
		TxWithdrawal: true, TxFee: true, TxTransfer: true, TxSplit: true,
	}
)

// ParseCSV lit un CSV de transactions sans rien écrire. Une ligne invalide est
// signalée (erreur à la ligne) et n'empêche pas les suivantes. Un fichier vide,
// trop gros, sans en-têtes ou sans colonnes obligatoires est une erreur.
func ParseCSV(ctx context.Context, r io.Reader, opts CSVOptions) (TxParsed, CSVReport, error) {
	var rep CSVReport
	if err := ctx.Err(); err != nil {
		return TxParsed{}, rep, fmt.Errorf("import interrompu: %w", err)
	}
	data, err := io.ReadAll(io.LimitReader(r, MaxFileSize+1))
	if err != nil {
		return TxParsed{}, rep, fmt.Errorf("lire le CSV: %w", err)
	}
	if len(data) > MaxFileSize {
		return TxParsed{}, rep, ErrTooLarge
	}
	data = bytes.TrimPrefix(data, []byte{0xEF, 0xBB, 0xBF})
	if len(bytes.TrimSpace(data)) == 0 {
		return TxParsed{}, rep, ErrEmptyCSV
	}
	exchange := strings.ToUpper(strings.TrimSpace(opts.Exchange))
	if exchange == "" {
		exchange = "TSX"
	}

	delim := detectDelimiter(data)
	rep.Delimiter = string(delim)
	cr := csv.NewReader(bytes.NewReader(data))
	cr.Comma = delim
	cr.FieldsPerRecord = -1
	cr.LazyQuotes = true

	cols, err := readHeader(cr)
	if err != nil {
		return TxParsed{}, rep, err
	}

	var out TxParsed
	seen := map[string]int{}
	for {
		if err := ctx.Err(); err != nil {
			return out, rep, fmt.Errorf("import interrompu: %w", err)
		}
		rec, err := cr.Read()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			line := 0
			var pe *csv.ParseError
			if errors.As(err, &pe) {
				line = pe.Line
			}
			rep.Rows++
			rep.Invalid++
			rep.add(line, LevelError, KindInvalid, "", "ligne illisible: %v", err)
			continue
		}
		if blankRecord(rec) {
			continue
		}
		line, _ := cr.FieldPos(0)
		rep.Rows++
		tx, ok := parseRow(rec, cols, exchange, line, &rep)
		if !ok {
			rep.Invalid++
			continue
		}
		seen[tx.NaturalKey]++
		tx.Occurrence = seen[tx.NaturalKey]
		out.Transactions = append(out.Transactions, tx)
		rep.Valid++
	}
	return out, rep, nil
}

func readHeader(cr *csv.Reader) (map[string]int, error) {
	for {
		rec, err := cr.Read()
		if errors.Is(err, io.EOF) {
			return nil, ErrEmptyCSV
		}
		if err != nil {
			return nil, fmt.Errorf("lire les en-têtes (%v): %w", err, ErrCSVHeader)
		}
		if blankRecord(rec) {
			continue
		}
		cols := map[string]int{}
		for i, h := range rec {
			name, ok := csvAliases[canonHeader(h)]
			if !ok {
				continue
			}
			if _, dup := cols[name]; dup {
				return nil, fmt.Errorf("colonne %q en double: %w", name, ErrCSVHeader)
			}
			cols[name] = i
		}
		var missing []string
		for _, req := range []string{"date", "account", "type"} {
			if _, ok := cols[req]; !ok {
				missing = append(missing, req)
			}
		}
		if len(missing) > 0 {
			return nil, fmt.Errorf("colonnes obligatoires absentes (%s): %w", strings.Join(missing, ", "), ErrCSVHeader)
		}
		return cols, nil
	}
}

func blankRecord(rec []string) bool {
	for _, f := range rec {
		if strings.TrimSpace(f) != "" {
			return false
		}
	}
	return true
}

// detectDelimiter compte virgules et points-virgules de la première ligne
// non vide, hors guillemets.
func detectDelimiter(data []byte) rune {
	commas, semis := 0, 0
	inQ := false
	for _, b := range data {
		if b == '"' {
			inQ = !inQ
			continue
		}
		if inQ {
			continue
		}
		if b == '\n' && commas+semis > 0 {
			break
		}
		switch b {
		case ',':
			commas++
		case ';':
			semis++
		}
	}
	if semis > commas {
		return ';'
	}
	return ','
}

func parseRow(rec []string, cols map[string]int, exchange string, line int, rep *CSVReport) (Transaction, bool) {
	get := func(name string) string {
		i, ok := cols[name]
		if !ok || i >= len(rec) {
			return ""
		}
		return strings.TrimSpace(rec[i])
	}
	failed := false
	fail := func(col, format string, a ...any) {
		failed = true
		rep.add(line, LevelError, KindInvalid, col, format, a...)
	}
	tx := Transaction{Source: SourceCSV, Line: line}

	if s := get("date"); s == "" {
		fail("date", "date absente")
	} else if d, err := time.Parse("2006-01-02", s); err != nil {
		fail("date", "date %q illisible (format AAAA-MM-JJ)", s)
	} else {
		tx.Date = d
	}
	tx.Account = get("account")
	switch {
	case tx.Account == "":
		fail("compte", "compte absent")
	case len([]rune(tx.Account)) > 100:
		fail("compte", "nom de compte trop long")
	}
	tx.Type = strings.ToLower(get("type"))
	if !validType[tx.Type] {
		fail("type", "type %q inconnu", get("type"))
	}
	if s := strings.ToUpper(get("symbol")); s != "" {
		switch {
		case symbolRe.MatchString(s):
			tx.Symbol = s
		case bareRe.MatchString(s):
			tx.Symbol = exchange + ":" + s
		default:
			fail("symbole", "symbole %q invalide", get("symbol"))
		}
	}
	num := func(col, name string, strictPositive bool) *decimal.Decimal {
		s := get(col)
		if s == "" {
			return nil
		}
		d, rounded, err := parseDecimal(s)
		switch {
		case err != nil:
			fail(name, "%s %q invalide", name, s)
			return nil
		case d.IsNegative():
			fail(name, "%s ne peut pas être négatif (le sens vient du type)", name)
			return nil
		case strictPositive && d.IsZero():
			fail(name, "%s doit être strictement positif", name)
			return nil
		}
		if rounded {
			rep.add(line, LevelWarning, "", name, "%s arrondi à 6 décimales", name)
		}
		return &d
	}
	tx.Quantity = num("quantity", "quantité", true)
	tx.Price = num("price", "prix", false)
	tx.Amount = num("amount", "montant", false)
	tx.Fee = num("fee", "frais", false)
	tx.SplitRatio = num("ratio", "ratio", true)
	tx.Counterpart = get("counterpart")
	if tx.Counterpart != "" && strings.EqualFold(tx.Counterpart, tx.Account) {
		fail("contrepartie", "la contrepartie doit être un autre compte")
	}
	tx.Note = get("note")
	if len([]rune(tx.Note)) > 500 {
		fail("note", "note trop longue (500 caractères au plus)")
	}
	switch tx.Currency = strings.ToUpper(get("currency")); tx.Currency {
	case "":
		tx.Currency = "CAD"
		rep.add(line, LevelWarning, "", "devise", "devise absente, CAD retenue")
	case "CAD", "USD":
	default:
		fail("devise", "devise %q non prise en charge (CAD ou USD)", get("currency"))
	}

	if validType[tx.Type] {
		need := func(ok bool, what string) {
			if !ok && !failed { // une erreur de format a déjà été signalée
				fail("", "type %s: %s requis", tx.Type, what)
			}
		}
		switch tx.Type {
		case TxBuy, TxSell, TxDrip:
			need(tx.Symbol != "", "symbole")
			need(tx.Quantity != nil, "quantité")
			need(tx.Price != nil, "prix")
		case TxDividend:
			need(tx.Symbol != "", "symbole")
			need(tx.Amount != nil, "montant")
		case TxSplit:
			need(tx.Symbol != "", "symbole")
			need(tx.SplitRatio != nil, "ratio")
		case TxTransfer:
			need(tx.Counterpart != "", "contrepartie")
			need(tx.Amount != nil || (tx.Symbol != "" && tx.Quantity != nil), "montant, ou symbole et quantité")
		default: // contribution, withdrawal, fee
			need(tx.Amount != nil, "montant")
		}
	}
	if failed {
		return tx, false
	}
	tx.NaturalKey = naturalKey(tx)
	return tx, true
}

// parseDecimal lit « 1 234,56 », « 1,234.56 », « 12,5 » ou « 12.5 » sans
// jamais passer par un flottant. Le second résultat indique un arrondi à 6
// décimales (numeric(20,6)).
func parseDecimal(s string) (decimal.Decimal, bool, error) {
	s = strings.NewReplacer(" ", "", " ", "", " ", "", "$", "", "'", "").Replace(s)
	lastDot, lastComma := strings.LastIndex(s, "."), strings.LastIndex(s, ",")
	switch {
	case lastDot >= 0 && lastComma >= 0:
		if lastComma > lastDot { // 1.234,56
			s = strings.ReplaceAll(s, ".", "")
			s = strings.Replace(s, ",", ".", 1)
		} else { // 1,234.56
			s = strings.ReplaceAll(s, ",", "")
		}
	case lastComma >= 0:
		if strings.Count(s, ",") > 1 {
			return decimal.Decimal{}, false, errors.New("séparateurs multiples")
		}
		s = strings.Replace(s, ",", ".", 1)
	}
	if !numberRe.MatchString(s) {
		return decimal.Decimal{}, false, errors.New("nombre invalide")
	}
	d, err := decimal.NewFromString(s)
	if err != nil {
		return decimal.Decimal{}, false, fmt.Errorf("nombre invalide: %w", err)
	}
	rd := d.Round(6)
	return rd, !rd.Equal(d), nil
}

func dstr(d *decimal.Decimal) string {
	if d == nil {
		return ""
	}
	return d.String()
}

// naturalKey est la clé de dédoublonnage (D391) : compte, date, type, symbole,
// quantité, prix, plus montant et ratio, faute de quoi deux dividendes ou deux
// cotisations du même jour se confondraient.
func naturalKey(t Transaction) string {
	return strings.Join([]string{
		strings.ToLower(t.Account), t.Date.Format("2006-01-02"), t.Type, t.Symbol,
		dstr(t.Quantity), dstr(t.Price), dstr(t.Amount), dstr(t.SplitRatio),
	}, "|")
}
