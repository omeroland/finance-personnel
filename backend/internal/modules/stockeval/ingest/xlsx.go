// Package ingest importe des données dans le module d'évaluation d'actions.
//
// xlsx.go lit un classeur au format de la feuille Simply Investing (excelize) et
// écrit les faits par les dépôts de store/, avec source = xlsx. Le classeur est
// lu en lecture seule, en mémoire : aucune macro n'est exécutée et aucune
// formule n'est recalculée (seules les valeurs en cache sont lues).
package ingest

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/shopspring/decimal"
	"github.com/xuri/excelize/v2"

	"github.com/omeroland/finance-privee/backend/internal/modules/stockeval/store"
)

// MaxFileSize est la taille maximale d'un classeur téléversé (10 Mo).
const MaxFileSize = 10 << 20

// Onglets exigés.
const (
	SheetMain     = "SI Worksheet"
	SheetEPS      = "EPS Growth Calculation"
	SheetDividend = "Dividend Growth Calculation"
	SheetYield    = "Average Dividend Yield"
)

// Disposition de la feuille.
const (
	firstStockRow = 7  // SI Worksheet : première ligne d'action
	firstBlockRow = 6  // onglets de séries : première ligne du premier bloc
	yearsPerBlock = 20 // colonnes B à U
	maxStocks     = 500
	// Seuils de D31.
	maxRatioFraction = 5.0
	maxYieldFraction = 1.0
	// Rupture de plausibilité du BPA d'une année à l'autre.
	epsJumpFactor = 10
)

// Erreurs de l'import.
var (
	ErrTooLarge        = errors.New("fichier trop volumineux (maximum 10 Mo)")
	ErrInvalidWorkbook = errors.New("classeur invalide")
	ErrInvalidOptions  = errors.New("paramètres d'import invalides")
)

// Repos regroupe les dépôts nécessaires à l'import (satisfait par *store.Store).
type Repos interface {
	store.WatchlistRepository
	store.FactRepository
	store.SnapshotRepository
	store.QualitativeRepository
}

// Options sont les paramètres d'un import.
type Options struct {
	// Year1 est l'année fiscale de « Year 1 » (obligatoire).
	Year1 int
	// Currency est la devise des titres sans préfixe de bourse américaine (« CAD » par défaut).
	Currency string
	// Now fournit l'horloge (défaut : time.Now), pour les tests.
	Now func() time.Time
}

// Statuts d'une ligne du compte rendu.
const (
	RowImported = "imported"
	RowIgnored  = "ignored"
)

// Niveaux et catégories (kind) des entrées du compte rendu ; les valeurs de
// kind sont celles de JobEntry.kind du contrat (D160, D161). Un kind vide
// signifie une entrée sans catégorie.
const (
	LevelInfo    = "info"
	LevelWarning = "warning"
	LevelError   = "error"

	KindImported   = "imported"
	KindSkipped    = "skipped"
	KindConflict   = "conflict"
	KindNormalized = "normalized"
	KindProtected  = "protected"
)

// Entry est une entrée du compte rendu, au format de JobEntry.
type Entry struct {
	Level   string `json:"level"`
	Kind    string `json:"kind,omitempty"`
	Line    int    `json:"line,omitempty"`
	Symbol  string `json:"symbol,omitempty"`
	Message string `json:"message"`
}

// RowReport est le compte rendu d'une ligne de « SI Worksheet ».
type RowReport struct {
	Row       int      `json:"row"`
	Symbol    string   `json:"symbol,omitempty"`
	Status    string   `json:"status"`
	Created   int      `json:"facts_created"`
	Updated   int      `json:"facts_updated"`
	Unchanged int      `json:"facts_unchanged"`
	Conflicts int      `json:"conflicts"`
	Snapshot  string   `json:"snapshot,omitempty"` // written, unchanged, protected, none
	Warnings  []string `json:"warnings,omitempty"`
	Entries   []Entry  `json:"entries,omitempty"`
}

// note ajoute une entrée à la ligne ; les avertissements restent aussi dans Warnings.
func (rr *RowReport) note(level, kind, format string, a ...any) {
	msg := fmt.Sprintf(format, a...)
	if level == LevelWarning {
		rr.Warnings = append(rr.Warnings, msg)
	}
	rr.Entries = append(rr.Entries, Entry{Level: level, Kind: kind, Line: rr.Row, Symbol: rr.Symbol, Message: msg})
}

// Report est le compte rendu complet d'un import.
type Report struct {
	Year1    int         `json:"year1"`
	Rows     []RowReport `json:"rows"`
	Entries  []Entry     `json:"entries"`
	Imported int         `json:"imported"`
	Ignored  int         `json:"ignored"`
	Warnings int         `json:"warnings"`
}

// JSON sérialise le compte rendu pour store.ImportJob.
func (r Report) JSON() json.RawMessage {
	b, err := json.Marshal(r)
	if err != nil {
		return json.RawMessage(`{}`)
	}
	return b
}

// Importer lit des classeurs et écrit dans les dépôts.
type Importer struct {
	repos Repos
}

// NewImporter construit l'importeur.
func NewImporter(repos Repos) *Importer { return &Importer{repos: repos} }

// Import lit le classeur r et écrit les faits du propriétaire. Les écritures
// sont faites une à une (chacune transactionnelle) : un échec d'écriture
// interrompt l'import, les lignes déjà écrites restent valables et un nouvel
// import les reconnaît (idempotence).
func (im *Importer) Import(ctx context.Context, owner uuid.UUID, r io.Reader, opts Options) (Report, error) {
	if opts.Year1 < 1920 || opts.Year1 > 2200 {
		return Report{}, fmt.Errorf("année de « Year 1 » %d: %w", opts.Year1, ErrInvalidOptions)
	}
	if opts.Currency == "" {
		opts.Currency = "CAD"
	}
	if len(opts.Currency) != 3 {
		return Report{}, fmt.Errorf("devise %q: %w", opts.Currency, ErrInvalidOptions)
	}
	if opts.Now == nil {
		opts.Now = time.Now
	}
	data, err := io.ReadAll(io.LimitReader(r, MaxFileSize+1))
	if err != nil {
		return Report{}, fmt.Errorf("lire le fichier: %w", err)
	}
	if len(data) > MaxFileSize {
		return Report{}, ErrTooLarge
	}
	f, err := excelize.OpenReader(strings.NewReader(string(data)))
	if err != nil {
		return Report{}, fmt.Errorf("ouvrir le classeur (format .xlsx attendu): %w", ErrInvalidWorkbook)
	}
	defer func() { _ = f.Close() }()

	if missing := missingSheets(f); len(missing) > 0 {
		return Report{}, fmt.Errorf("onglets manquants : %s: %w", strings.Join(missing, ", "), ErrInvalidWorkbook)
	}

	rep := Report{Year1: opts.Year1}
	seen := map[string]bool{}
	for row := firstStockRow; row < firstStockRow+maxStocks; row++ {
		if err := ctx.Err(); err != nil {
			return rep, fmt.Errorf("import interrompu: %w", err)
		}
		sym := strings.TrimSpace(cell(f, SheetMain, "C", row))
		if sym == "" {
			break
		}
		rr := RowReport{Row: row, Symbol: sym}
		if seen[strings.ToUpper(sym)] {
			rr.Status = RowIgnored
			rr.note(LevelWarning, KindSkipped, "symbole en double dans la feuille : ligne ignorée")
		} else {
			seen[strings.ToUpper(sym)] = true
			if err := im.importRow(ctx, owner, f, row-firstStockRow, &rr, opts); err != nil {
				return rep, fmt.Errorf("ligne %d (%s): %w", row, sym, err)
			}
		}
		rep.add(rr)
	}
	return rep, nil
}

func (r *Report) add(rr RowReport) {
	if rr.Status == RowImported {
		r.Imported++
	} else {
		r.Ignored++
	}
	r.Warnings += len(rr.Warnings)
	r.Entries = append(r.Entries, rr.Entries...)
	r.Rows = append(r.Rows, rr)
}

func missingSheets(f *excelize.File) []string {
	have := map[string]bool{}
	for _, s := range f.GetSheetList() {
		have[s] = true
	}
	var out []string
	for _, s := range []string{SheetMain, SheetEPS, SheetDividend, SheetYield} {
		if !have[s] {
			out = append(out, s)
		}
	}
	return out
}

func cell(f *excelize.File, sheet, col string, row int) string {
	v, err := f.GetCellValue(sheet, col+strconv.Itoa(row), excelize.Options{RawCellValue: true})
	if err != nil {
		return ""
	}
	return strings.TrimSpace(v)
}

// number lit une cellule numérique. ok=false si vide ; err si illisible.
func number(raw string) (v decimal.Decimal, ok bool, err error) {
	s := strings.ReplaceAll(strings.TrimSpace(raw), " ", "")
	if s == "" {
		return decimal.Zero, false, nil
	}
	pct := strings.HasSuffix(s, "%")
	s = strings.TrimSuffix(s, "%")
	s = strings.ReplaceAll(s, ",", ".")
	d, e := decimal.NewFromString(s)
	if e != nil {
		return decimal.Zero, false, fmt.Errorf("valeur non numérique %q", raw)
	}
	if pct {
		d = d.Div(decimal.NewFromInt(100))
	}
	return d, true, nil
}

// normalize applique D31 : au-delà du seuil, la valeur est un pourcentage.
func normalize(v decimal.Decimal, limit float64, label string, rr *RowReport) decimal.Decimal {
	if v.GreaterThan(decimal.NewFromFloat(limit)) {
		n := v.Div(decimal.NewFromInt(100))
		rr.note(LevelWarning, KindNormalized, "%s %s lu comme un pourcentage : converti en %s", label, v, n)
		return n
	}
	return v
}

func (im *Importer) importRow(ctx context.Context, owner uuid.UUID, f *excelize.File, k int, rr *RowReport, opts Options) error {
	warn := func(format string, a ...any) { rr.note(LevelWarning, "", format, a...) }

	input, err := instrumentInput(rr.Symbol, cell(f, SheetMain, "B", firstStockRow+k), opts.Currency)
	if err != nil {
		rr.Status = RowIgnored
		rr.note(LevelWarning, KindSkipped, "%v", err)
		return nil
	}
	inst, err := im.repos.AddToWatchlist(ctx, owner, input)
	if err != nil {
		return fmt.Errorf("ajouter à la liste de suivi: %w", err)
	}
	rr.Status = RowImported
	rr.note(LevelInfo, KindImported, "ligne importée")
	row := firstStockRow + k

	read := func(col, label string) (decimal.Decimal, bool) {
		v, ok, err := number(cell(f, SheetMain, col, row))
		if err != nil {
			warn("%s : %v (ignoré)", label, err)
			return decimal.Zero, false
		}
		return v, ok
	}

	// Instantané : on part de l'existant pour ne pas effacer les champs absents du classeur.
	snap := store.Snapshot{InstrumentID: inst.ID, Source: store.SourceXLSX}
	cur, curErr := im.repos.GetSnapshot(ctx, owner, inst.ID)
	hasCur := curErr == nil
	if curErr != nil && !errors.Is(curErr, store.ErrNotFound) {
		return fmt.Errorf("lire l'instantané: %w", curErr)
	}
	if hasCur {
		snap = cur
		snap.Source = store.SourceXLSX
	}
	changed := false
	set := func(dst *decimal.NullDecimal, v decimal.Decimal) {
		if !dst.Valid || !dst.Decimal.Equal(v) {
			changed = true
		}
		*dst = decimal.NullDecimal{Decimal: v, Valid: true}
	}
	found := false
	if v, ok := read("G", "ratio de distribution"); ok {
		set(&snap.PayoutRatioImported, normalize(v, maxRatioFraction, "ratio de distribution", rr))
		found = true
	}
	if v, ok := read("H", "dette LT / capitaux propres"); ok {
		set(&snap.DebtToEquity, normalize(v, maxRatioFraction, "dette LT / capitaux propres", rr))
		found = true
	}
	if v, ok := read("K", "P/E"); ok {
		set(&snap.PEImported, v)
		found = true
	}
	if v, ok := read("O", "P/B"); ok {
		set(&snap.PBImported, v)
		found = true
	}
	if v, ok := read("P", "dividende prévu"); ok {
		set(&snap.ExpectedDividend, v)
		found = true
	}
	if v, ok := read("Q", "prix"); ok {
		set(&snap.Price, v)
		found = true
	} else if cell(f, SheetMain, "Q", row) == "" {
		warn("prix absent (formule sans valeur en cache)")
	}
	switch {
	case !found:
		rr.Snapshot = "none"
	case hasCur && cur.Source == store.SourceManual:
		rr.Snapshot = "protected"
		rr.note(LevelWarning, KindProtected, "instantané saisi à la main : valeurs de la feuille non appliquées")
	case hasCur && !changed && cur.Source == store.SourceXLSX:
		rr.Snapshot = "unchanged"
	default:
		snap.AsOf = opts.Now()
		written, err := im.repos.SaveSnapshot(ctx, owner, snap)
		if err != nil {
			return fmt.Errorf("écrire l'instantané: %w", err)
		}
		rr.Snapshot = "written"
		if !written {
			rr.Snapshot = "protected"
			rr.note(LevelWarning, KindProtected, "instantané protégé : valeurs de la feuille non appliquées")
		}
	}

	if err := im.importBuyback(ctx, owner, inst.ID, strings.ToLower(cell(f, SheetMain, "J", row)), rr); err != nil {
		return err
	}

	cur2 := input.QuoteCurrency
	series := []struct {
		sheet, metric string
		row           int
		currency      *string
		limit         float64
	}{
		{SheetEPS, store.MetricEPSDiluted, firstBlockRow + 4*k, &cur2, 0},
		{SheetDividend, store.MetricDPS, firstBlockRow + 4*k, &cur2, 0},
		{SheetYield, store.MetricDividendYield, firstBlockRow + 2*k, nil, maxYieldFraction},
	}
	for _, s := range series {
		if err := im.importSeries(ctx, owner, f, inst.ID, s.sheet, s.metric, s.row, s.currency, s.limit, opts.Year1, rr); err != nil {
			return err
		}
	}
	return nil
}

// importBuyback range « rachat oui/non » dans le jugement, sans écraser un choix existant.
func (im *Importer) importBuyback(ctx context.Context, owner, id uuid.UUID, v string, rr *RowReport) error {
	var val bool
	switch v {
	case "":
		return nil
	case "yes", "oui", "y", "true", "1":
		val = true
	case "no", "non", "n", "false", "0":
		val = false
	default:
		rr.note(LevelWarning, "", "rachat %q non reconnu (oui/non attendu) : ignoré", v)
		return nil
	}
	q, err := im.repos.GetQualitative(ctx, owner, id)
	if err != nil && !errors.Is(err, store.ErrNotFound) {
		return fmt.Errorf("lire le qualitatif: %w", err)
	}
	if err == nil && q.BuybackManual != nil {
		return nil // un choix existe déjà : il prime
	}
	q.InstrumentID = id
	q.BuybackManual = &val
	if err := im.repos.SaveQualitative(ctx, owner, q); err != nil {
		return fmt.Errorf("écrire le rachat: %w", err)
	}
	return nil
}

func (im *Importer) importSeries(ctx context.Context, owner uuid.UUID, f *excelize.File, id uuid.UUID,
	sheet, metric string, row int, currency *string, limit float64, year1 int, rr *RowReport,
) error {
	type pt struct {
		year int
		v    decimal.Decimal
	}
	var pts []pt
	for i := range yearsPerBlock {
		col, _ := excelize.ColumnNumberToName(2 + i)
		year := year1 - (yearsPerBlock - 1) + i
		v, ok, err := number(cell(f, sheet, col, row))
		if err != nil {
			rr.note(LevelWarning, "", "%s %s %d : %v (ignoré)", sheet, metric, year, err)
			continue
		}
		if !ok {
			continue
		}
		if limit > 0 {
			v = normalize(v, limit, fmt.Sprintf("rendement %d", year), rr)
		}
		pts = append(pts, pt{year, v})
	}
	for i, p := range pts {
		if i > 0 {
			prev := pts[i-1]
			if p.year-prev.year > 1 {
				rr.note(LevelWarning, "", "%s : année(s) manquante(s) entre %d et %d", metric, prev.year, p.year)
			}
			if metric == store.MetricEPSDiluted && prev.v.IsPositive() &&
				p.v.GreaterThan(prev.v.Mul(decimal.NewFromInt(epsJumpFactor))) {
				rr.note(LevelWarning, "", "BPA %d : plus de %dx celui de %d", p.year, epsJumpFactor, prev.year)
			}
		}
		out, err := im.repos.WriteFact(ctx, owner, store.Fact{
			InstrumentID: id, FiscalYear: p.year, Metric: metric, Value: p.v, Currency: currency, Source: store.SourceXLSX,
		})
		if err != nil {
			return fmt.Errorf("écrire %s %d: %w", metric, p.year, err)
		}
		switch out {
		case store.FactCreated:
			rr.Created++
		case store.FactUpdated:
			rr.Updated++
		case store.FactConflicted:
			rr.Conflicts++
			rr.note(LevelWarning, KindConflict, "%s %d : valeur saisie à la main conservée, conflit créé", metric, p.year)
		default:
			rr.Unchanged++
		}
	}
	return nil
}

// instrumentInput déduit l'instrument du symbole (« BOURSE:SYMBOLE » ou symbole nu).
func instrumentInput(symbol, name, defaultCurrency string) (store.InstrumentInput, error) {
	if len(symbol) > 32 {
		return store.InstrumentInput{}, errors.New("symbole trop long : ligne ignorée")
	}
	exchange := ""
	cur := defaultCurrency
	if ex, _, ok := strings.Cut(symbol, ":"); ok {
		exchange = strings.ToUpper(ex)
		switch exchange {
		case "NYSE", "NASDAQ", "AMEX", "NYSEARCA":
			cur = "USD"
		case "TSX", "TSXV":
			cur = "CAD"
		}
	}
	if name == "" {
		name = symbol
	}
	if len(name) > 200 {
		name = string([]rune(name)[:min(200, len([]rune(name)))])
	}
	return store.InstrumentInput{
		Symbol: symbol, Name: name, Exchange: exchange, QuoteCurrency: cur, ReportCurrency: cur,
	}, nil
}
