// Package ingest importe dans le module 2 (portefeuille de FNB) le classeur de
// l'outil SÉLECTION ET GESTION : catalogue (onglet FNB), portefeuille modèle
// (CRÉATION) et positions actuelles (DIVERSIFICATION).
//
// Le classeur est lu en mémoire, en lecture seule : aucune macro n'est exécutée
// et aucune formule n'est recalculée (seules les valeurs en cache sont lues).
// Parse est pure (aucune écriture) ; Importer.Import y ajoute l'écriture par
// l'interface Repository, que les handlers (P2-10) implémenteront sur la base.
package ingest

import (
	"context"
	"errors"
	"fmt"
	"io"
	"math"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/shopspring/decimal"
	"github.com/xuri/excelize/v2"
)

// MaxFileSize est la taille maximale d'un classeur téléversé (10 Mo).
const MaxFileSize = 10 << 20

// Onglets exigés.
const (
	SheetFNB             = "FNB"
	SheetCreation        = "CRÉATION"
	SheetDiversification = "DIVERSIFICATION"
)

// Disposition des onglets.
const (
	firstFundRow     = 2 // FNB : première ligne de fonds (ligne 1 = en-têtes)
	maxFunds         = 2000
	firstModelRow    = 28 // CRÉATION : première ligne du tableau « Choix des fonds »
	maxModelFunds    = 20
	firstCurrentRow  = 8 // DIVERSIFICATION : première ligne de position
	maxCurrentFunds  = 12
	tolerance        = 0.0001 // écart toléré sur un total de 100 % (CHECK du schéma, D323/D324)
	defaultAsOfYear  = 2021   // catalogue figé en 2021 (D353)
	defaultBandRel   = 0.20   // D12
	defaultBandCash  = 0.05   // D12
	defaultModelName = "Portefeuille modèle (outil SÉLECTION ET GESTION)"
)

// Erreurs de l'import.
var (
	ErrTooLarge        = errors.New("fichier trop volumineux (maximum 10 Mo)")
	ErrInvalidWorkbook = errors.New("classeur invalide")
	ErrInvalidOptions  = errors.New("paramètres d'import invalides")
)

// Niveaux et catégories des entrées du compte rendu.
const (
	LevelInfo    = "info"
	LevelWarning = "warning"

	KindImported   = "imported"
	KindSkipped    = "skipped"
	KindNormalized = "normalized"
)

// Issues de l'écriture d'un modèle.
const (
	ModelCreated   = "created"
	ModelUnchanged = "unchanged"
	ModelNone      = "none"
)

// Allocation est la répartition en six classes, en fractions (D310).
type Allocation struct {
	EquityCA    float64 `json:"equity_ca"`
	EquityUS    float64 `json:"equity_us"`
	EquityIntl  float64 `json:"equity_intl"`
	BondCA      float64 `json:"bond_ca"`
	BondOther   float64 `json:"bond_other"`
	OtherAssets float64 `json:"other_assets"`
}

// FundProfile est un FNB du catalogue, prêt pour le dépôt de profils.
// La clé naturelle est (Symbol, AsOf).
type FundProfile struct {
	Symbol   string // « BOURSE:SYMBOLE »
	Exchange string
	Name     string
	Currency string
	Type     string  // equity, fixed_income, balanced, other
	Region   *string // ca, us, intl, global
	Alloc    Allocation
	MER      *float64         // RFG, fraction
	Size     *decimal.Decimal // actif net en dollars
	ESG      *bool
	Returns  map[int]float64 // années (1, 3, 5, 10) -> fraction
	AsOf     time.Time
	Source   string // toujours « xlsx »
	Row      int
}

// ModelHolding est un fonds retenu du modèle, avec son poids (somme 1).
type ModelHolding struct {
	Symbol string
	Name   string
	Weight float64
}

// Model est le portefeuille modèle : cibles normalisées (somme 1 avec l'encaisse).
type Model struct {
	Name         string
	Targets      Allocation
	CashTarget   float64
	BandRelative float64
	BandCash     float64
	Holdings     []ModelHolding
}

// CurrentHolding est un fonds de l'onglet DIVERSIFICATION. L'onglet ne porte
// que des poids (somme 1), pas de quantités : voir D354.
type CurrentHolding struct {
	Symbol string
	Name   string
	Weight float64
	Row    int
}

// Parsed est le contenu lu du classeur.
type Parsed struct {
	Profiles []FundProfile
	Model    *Model // nil si l'onglet CRÉATION est vide
	Current  []CurrentHolding
}

// Options sont les paramètres d'un import.
type Options struct {
	// AsOf date le catalogue. Défaut : 31 décembre de l'année de l'en-tête « 1 an (AAAA) », sinon 2021-12-31.
	AsOf time.Time
	// Exchange préfixe les symboles nus (défaut « TSX »).
	Exchange string
	// Currency est la devise des fonds (défaut « CAD »).
	Currency string
	// ModelName nomme le portefeuille modèle.
	ModelName string
}

// Entry est une entrée du compte rendu, située à la cellule près.
type Entry struct {
	Level   string `json:"level"`
	Kind    string `json:"kind,omitempty"`
	Sheet   string `json:"sheet,omitempty"`
	Cell    string `json:"cell,omitempty"`
	Symbol  string `json:"symbol,omitempty"`
	Message string `json:"message"`
}

// Report est le compte rendu d'un import.
type Report struct {
	AsOf              string  `json:"as_of"`
	Funds             int     `json:"funds"`
	Skipped           int     `json:"skipped"`
	ProfilesCreated   int     `json:"profiles_created"`
	ProfilesUpdated   int     `json:"profiles_updated"`
	ProfilesUnchanged int     `json:"profiles_unchanged"`
	Model             string  `json:"model"`
	ModelHoldings     int     `json:"model_holdings"`
	CurrentHoldings   int     `json:"current_holdings"`
	Warnings          int     `json:"warnings"`
	Entries           []Entry `json:"entries"`
}

func (r *Report) add(level, kind, sheet, cell, symbol, format string, a ...any) {
	if level == LevelWarning {
		r.Warnings++
	}
	r.Entries = append(r.Entries, Entry{Level: level, Kind: kind, Sheet: sheet, Cell: cell, Symbol: symbol,
		Message: fmt.Sprintf(format, a...)})
}

func (r *Report) warn(sheet, cell, symbol, format string, a ...any) {
	r.add(LevelWarning, "", sheet, cell, symbol, format, a...)
}

// SaveCounts résume l'écriture des profils.
type SaveCounts struct{ Created, Updated, Unchanged int }

// Repository est le dépôt à injecter. Les implémentations sont idempotentes :
//   - SaveProfiles crée l'instrument (symbole, nom, devise) s'il manque, puis
//     écrit le profil sous la clé (propriétaire, instrument, as_of) ; un profil
//     identique est « unchanged », sans doublon ;
//   - SaveModel publie une nouvelle version, sauf si la dernière version du
//     modèle de même nom a exactement le même contenu (« unchanged »).
type Repository interface {
	SaveProfiles(ctx context.Context, owner uuid.UUID, profiles []FundProfile) (SaveCounts, error)
	SaveModel(ctx context.Context, owner uuid.UUID, m Model) (string, error)
}

// Importer lit des classeurs et écrit dans le dépôt.
type Importer struct{ repo Repository }

// NewImporter construit l'importeur.
func NewImporter(repo Repository) *Importer { return &Importer{repo: repo} }

// Import lit le classeur r et écrit catalogue et modèle. Les positions actuelles
// sont rendues dans Parsed.Current, sans écriture (D354).
func (im *Importer) Import(ctx context.Context, owner uuid.UUID, r io.Reader, opts Options) (Parsed, Report, error) {
	if im.repo == nil {
		return Parsed{}, Report{}, fmt.Errorf("dépôt absent: %w", ErrInvalidOptions)
	}
	p, rep, err := Parse(ctx, r, opts)
	if err != nil {
		return p, rep, err
	}
	if len(p.Profiles) > 0 {
		c, err := im.repo.SaveProfiles(ctx, owner, p.Profiles)
		if err != nil {
			return p, rep, fmt.Errorf("écrire le catalogue: %w", err)
		}
		rep.ProfilesCreated, rep.ProfilesUpdated, rep.ProfilesUnchanged = c.Created, c.Updated, c.Unchanged
	}
	if p.Model != nil {
		if err := ctx.Err(); err != nil {
			return p, rep, fmt.Errorf("import interrompu: %w", err)
		}
		out, err := im.repo.SaveModel(ctx, owner, *p.Model)
		if err != nil {
			return p, rep, fmt.Errorf("écrire le modèle: %w", err)
		}
		rep.Model = out
	}
	return p, rep, nil
}

// Parse lit le classeur sans rien écrire.
func Parse(ctx context.Context, r io.Reader, opts Options) (Parsed, Report, error) {
	if opts.Exchange == "" {
		opts.Exchange = "TSX"
	}
	if opts.Currency == "" {
		opts.Currency = "CAD"
	}
	if opts.ModelName == "" {
		opts.ModelName = defaultModelName
	}
	if len(opts.Currency) != 3 || len(opts.Exchange) > 16 || len([]rune(opts.ModelName)) > 100 {
		return Parsed{}, Report{}, fmt.Errorf("devise, bourse ou nom du modèle: %w", ErrInvalidOptions)
	}
	if r == nil {
		return Parsed{}, Report{}, fmt.Errorf("fichier absent: %w", ErrInvalidWorkbook)
	}
	data, err := io.ReadAll(io.LimitReader(r, MaxFileSize+1))
	if err != nil {
		return Parsed{}, Report{}, fmt.Errorf("lire le fichier: %w", err)
	}
	if len(data) > MaxFileSize {
		return Parsed{}, Report{}, ErrTooLarge
	}
	f, err := excelize.OpenReader(strings.NewReader(string(data)),
		excelize.Options{UnzipSizeLimit: 100 << 20, UnzipXMLSizeLimit: 32 << 20})
	if err != nil {
		return Parsed{}, Report{}, fmt.Errorf("ouvrir le classeur (format .xlsx attendu): %w", ErrInvalidWorkbook)
	}
	defer func() { _ = f.Close() }()
	if missing := missingSheets(f); len(missing) > 0 {
		return Parsed{}, Report{}, fmt.Errorf("onglets manquants : %s: %w", strings.Join(missing, ", "), ErrInvalidWorkbook)
	}

	rep := Report{Model: ModelNone}
	var p Parsed
	asOf := resolveAsOf(f, opts, &rep)
	rep.AsOf = asOf.Format(time.DateOnly)

	if err := ctx.Err(); err != nil {
		return p, rep, fmt.Errorf("import interrompu: %w", err)
	}
	p.Profiles = parseCatalog(f, opts, asOf, &rep)
	known := map[string]bool{}
	for _, fp := range p.Profiles {
		known[fp.Symbol] = true
	}
	if err := ctx.Err(); err != nil {
		return p, rep, fmt.Errorf("import interrompu: %w", err)
	}
	p.Model = parseModel(f, opts, known, &rep)
	if p.Model != nil {
		rep.ModelHoldings = len(p.Model.Holdings)
	}
	p.Current = parseCurrent(f, opts, known, &rep)
	rep.CurrentHoldings = len(p.Current)
	return p, rep, nil
}

func missingSheets(f *excelize.File) []string {
	have := map[string]bool{}
	for _, s := range f.GetSheetList() {
		have[s] = true
	}
	var out []string
	for _, s := range []string{SheetFNB, SheetCreation, SheetDiversification} {
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

func addr(col string, row int) string { return col + strconv.Itoa(row) }

var yearRe = regexp.MustCompile(`(?:19|20)\d{2}`)

// resolveAsOf détermine la date du catalogue (D353).
func resolveAsOf(f *excelize.File, opts Options, rep *Report) time.Time {
	if !opts.AsOf.IsZero() {
		return time.Date(opts.AsOf.Year(), opts.AsOf.Month(), opts.AsOf.Day(), 0, 0, 0, 0, time.UTC)
	}
	if ys := yearRe.FindAllString(cell(f, SheetFNB, "R", 1), -1); len(ys) > 0 {
		y, _ := strconv.Atoi(ys[len(ys)-1])
		rep.add(LevelInfo, "", SheetFNB, "R1", "", "date du catalogue déduite de l'en-tête : 31 décembre %d", y)
		return time.Date(y, 12, 31, 0, 0, 0, 0, time.UTC)
	}
	rep.warn(SheetFNB, "R1", "", "aucune année dans l'en-tête des rendements : catalogue daté du 31 décembre %d", defaultAsOfYear)
	return time.Date(defaultAsOfYear, 12, 31, 0, 0, 0, 0, time.UTC)
}

// canonical donne « BOURSE:SYMBOLE » en majuscules.
func canonical(symbol, exchange string) (string, string, error) {
	s := strings.ToUpper(cleanText(symbol))
	if s == "" {
		return "", "", errors.New("symbole vide")
	}
	ex := strings.ToUpper(exchange)
	if before, _, ok := strings.Cut(s, ":"); ok {
		ex = before
	} else {
		s = ex + ":" + s
	}
	if len(s) > 32 {
		return "", "", errors.New("symbole trop long")
	}
	return s, ex, nil
}

func round9(v float64) float64 { return math.Round(v*1e9) / 1e9 }

func parseCatalog(f *excelize.File, opts Options, asOf time.Time, rep *Report) []FundProfile {
	var out []FundProfile
	seen := map[string]bool{}
	retCols := []struct {
		col   string
		years int
	}{{"R", 1}, {"S", 3}, {"T", 5}, {"U", 10}}
	classCols := []string{"G", "H", "I", "J", "K"}

	for row := firstFundRow; row < firstFundRow+maxFunds; row++ {
		raw := cell(f, SheetFNB, "A", row)
		if raw == "" {
			break
		}
		sym, ex, err := canonical(raw, opts.Exchange)
		if err != nil {
			rep.add(LevelWarning, KindSkipped, SheetFNB, addr("A", row), raw, "%v : ligne ignorée", err)
			rep.Skipped++
			continue
		}
		if seen[sym] {
			rep.add(LevelWarning, KindSkipped, SheetFNB, addr("A", row), sym, "symbole en double : ligne ignorée")
			rep.Skipped++
			continue
		}
		seen[sym] = true
		warnCell := func(col, format string, a ...any) { rep.warn(SheetFNB, addr(col, row), sym, format, a...) }

		name := cell(f, SheetFNB, "B", row)
		if name == "" {
			name = sym
		}
		if r := []rune(name); len(r) > 200 {
			name = string(r[:200])
		}
		fp := FundProfile{
			Symbol: sym, Exchange: ex, Name: name, Currency: opts.Currency,
			AsOf: asOf, Source: "xlsx", Row: row, Returns: map[int]float64{},
		}

		var known bool
		fp.Type, known = mapType(cell(f, SheetFNB, "D", row))
		if !known {
			warnCell("D", "type %q inconnu : classé « other »", cell(f, SheetFNB, "D", row))
		}
		if rg := cell(f, SheetFNB, "F", row); rg != "" {
			if v, ok := mapRegion(rg, fp.Type); ok {
				fp.Region = &v
			} else {
				warnCell("F", "région %q inconnue : ignorée", rg)
			}
		}

		// Répartition : cinq classes lues, « autres » calculé (écart de l'outil sur les fonds partiellement classés).
		vals := make([]decimal.Decimal, 5)
		sum := decimal.Zero
		for i, col := range classCols {
			v, norm, ok, err := parseFraction(cell(f, SheetFNB, col, row), maxShareFraction)
			if err != nil {
				warnCell(col, "%v (lu comme 0)", err)
				continue
			}
			if !ok {
				continue
			}
			if norm {
				rep.add(LevelWarning, KindNormalized, SheetFNB, addr(col, row), sym, "%s lu comme un pourcentage : converti en %s", cell(f, SheetFNB, col, row), v)
			}
			if v.IsNegative() {
				warnCell(col, "part négative %s (lue comme 0)", v)
				continue
			}
			vals[i] = v
			sum = sum.Add(v)
		}
		one := decimal.NewFromInt(1)
		switch {
		case sum.IsZero():
			warnCell("G", "répartition absente : tout en « autres actifs »")
			fp.Alloc.OtherAssets = 1
		case sum.GreaterThan(one.Add(decimal.NewFromFloat(tolerance))):
			warnCell("G", "répartition de %s au-delà de 100 %% : ramenée à 100 %%", sum.Mul(hundred))
			for i := range vals {
				vals[i] = vals[i].Div(sum)
			}
			sum = one
			fallthrough
		default:
			f64 := func(d decimal.Decimal) float64 { v, _ := d.Float64(); return round9(v) }
			fp.Alloc = Allocation{
				EquityCA: f64(vals[0]), EquityUS: f64(vals[1]), EquityIntl: f64(vals[2]),
				BondCA: f64(vals[3]), BondOther: f64(vals[4]),
			}
			if rest := one.Sub(sum); rest.GreaterThan(decimal.NewFromFloat(tolerance)) {
				fp.Alloc.OtherAssets = f64(rest)
			}
		}

		if v, norm, ok, err := parseFraction(cell(f, SheetFNB, "L", row), maxShareFraction); err != nil {
			warnCell("L", "RFG : %v (ignoré)", err)
		} else if ok {
			if norm {
				rep.add(LevelWarning, KindNormalized, SheetFNB, addr("L", row), sym, "RFG %s lu comme un pourcentage : converti en %s", cell(f, SheetFNB, "L", row), v)
			}
			if fv, _ := v.Float64(); fv < 0 || fv > 1 {
				warnCell("L", "RFG hors de [0 %%, 100 %%] (ignoré)")
			} else {
				fp.MER = &fv
			}
		}
		if v, ok, err := parseSize(cell(f, SheetFNB, "M", row)); err != nil {
			warnCell("M", "%v (ignorée)", err)
		} else if ok {
			fp.Size = &v
		}
		esg := strings.Contains(strings.ToUpper(name+" "+cell(f, SheetFNB, "C", row)), "ESG")
		fp.ESG = &esg

		for _, rc := range retCols {
			v, norm, ok, err := parseFraction(cell(f, SheetFNB, rc.col, row), maxReturnFraction)
			if err != nil {
				warnCell(rc.col, "rendement %d an(s) : %v (ignoré)", rc.years, err)
				continue
			}
			if !ok {
				continue
			}
			if norm {
				rep.add(LevelWarning, KindNormalized, SheetFNB, addr(rc.col, row), sym, "rendement %s lu comme un pourcentage : converti en %s", cell(f, SheetFNB, rc.col, row), v)
			}
			fv, _ := v.Float64()
			fp.Returns[rc.years] = round9(fv)
		}
		out = append(out, fp)
		rep.Funds++
	}
	if len(out) == 0 {
		rep.add(LevelInfo, "", SheetFNB, "", "", "catalogue vide : aucun fonds importé")
	}
	return out
}

// targets lit un champ fraction de CRÉATION ; absent -> ok=false.
func fractionAt(f *excelize.File, sheet, col string, row int, rep *Report, label string) (decimal.Decimal, bool) {
	raw := cell(f, sheet, col, row)
	v, norm, ok, err := parseFraction(raw, maxShareFraction)
	if err != nil {
		rep.warn(sheet, addr(col, row), "", "%s : %v (ignoré)", label, err)
		return decimal.Zero, false
	}
	if ok && norm {
		rep.add(LevelWarning, KindNormalized, sheet, addr(col, row), "", "%s %s lu comme un pourcentage : converti en %s", label, raw, v)
	}
	if ok && v.IsNegative() {
		rep.warn(sheet, addr(col, row), "", "%s négatif (ignoré)", label)
		return decimal.Zero, false
	}
	return v, ok
}

func near1(d decimal.Decimal) bool {
	return d.Sub(decimal.NewFromInt(1)).Abs().LessThanOrEqual(decimal.NewFromFloat(tolerance))
}

func toF(d decimal.Decimal) float64 { v, _ := d.Float64(); return round9(v) }

// parseModel lit CRÉATION. Les cibles sont normalisées pour que le total vaille 100 %
// (écart « cibles au-delà de 100 % » de l'outil). Obligations : une seule cible dans
// l'outil, affectée à bond_ca (D350).
func parseModel(f *excelize.File, opts Options, catalog map[string]bool, rep *Report) *Model {
	const sh = SheetCreation
	a, aok := fractionAt(f, sh, "C", 9, rep, "portion risquée")
	b, bok := fractionAt(f, sh, "C", 10, rep, "portion moins risquée")
	switch {
	case !aok && !bok:
		rep.add(LevelInfo, "", sh, "C9", "", "cibles vides : aucun portefeuille modèle")
		return nil
	case !aok:
		a = decimal.NewFromInt(1).Sub(b)
	case !bok:
		b = decimal.NewFromInt(1).Sub(a)
	}
	if a.IsNegative() || b.IsNegative() {
		rep.warn(sh, "C9", "", "portions risquée et moins risquée incohérentes : aucun modèle")
		return nil
	}
	total := a.Add(b)
	if total.IsZero() {
		rep.warn(sh, "C9", "", "portions risquée et moins risquée nulles : aucun modèle")
		return nil
	}
	if !near1(total) {
		rep.add(LevelWarning, KindNormalized, sh, "C9", "", "portions risquée + moins risquée = %s %% : normalisées à 100 %%", total.Mul(hundred))
		a, b = a.Div(total), b.Div(total)
	}

	var subs [3]decimal.Decimal
	subSum := decimal.Zero
	for i, row := range []int{9, 10, 11} {
		if v, ok := fractionAt(f, sh, "R", row, rep, "sous-répartition des actions"); ok {
			subs[i] = v
			subSum = subSum.Add(v)
		}
	}
	if a.IsPositive() && subSum.IsZero() {
		rep.warn(sh, "R9", "", "sous-répartition des actions absente : aucun modèle")
		return nil
	}
	if !subSum.IsZero() && !near1(subSum) {
		rep.add(LevelWarning, KindNormalized, sh, "R9", "", "sous-répartition des actions = %s %% : normalisée à 100 %%", subSum.Mul(hundred))
	}
	if v, ok := fractionAt(f, sh, "R", 14, rep, "cible d'obligations"); ok && !near1(v) {
		rep.warn(sh, "R14", "", "cible d'obligations %s %% : toute la portion moins risquée va à « bond_ca »", v.Mul(hundred))
	}
	m := &Model{Name: opts.ModelName, BandRelative: defaultBandRel, BandCash: defaultBandCash}
	m.Targets.BondCA = toF(b)
	if !subSum.IsZero() {
		m.Targets.EquityCA = toF(a.Mul(subs[0]).Div(subSum))
		m.Targets.EquityUS = toF(a.Mul(subs[1]).Div(subSum))
		m.Targets.EquityIntl = toF(a.Mul(subs[2]).Div(subSum))
	}

	// Fonds retenus.
	type h struct {
		sym, name string
		w         decimal.Decimal
		cell      string
	}
	var hs []h
	seen := map[string]bool{}
	wsum := decimal.Zero
	for row := firstModelRow; row < firstModelRow+maxModelFunds; row++ {
		raw := cell(f, sh, "C", row)
		if raw == "" {
			continue
		}
		sym, _, err := canonical(raw, opts.Exchange)
		if err != nil {
			rep.add(LevelWarning, KindSkipped, sh, addr("C", row), raw, "%v : ligne ignorée", err)
			continue
		}
		if seen[sym] {
			rep.add(LevelWarning, KindSkipped, sh, addr("C", row), sym, "fonds en double : ligne ignorée")
			continue
		}
		seen[sym] = true
		w, ok := fractionAt(f, sh, "E", row, rep, "% alloué")
		if !ok || w.IsZero() {
			rep.add(LevelWarning, KindSkipped, sh, addr("E", row), sym, "%% alloué absent ou nul : fonds ignoré")
			continue
		}
		if !catalog[sym] {
			rep.warn(sh, addr("C", row), sym, "fonds absent du catalogue de ce classeur")
		}
		hs = append(hs, h{sym, cell(f, sh, "D", row), w, addr("E", row)})
		wsum = wsum.Add(w)
	}
	if len(hs) == 0 {
		rep.warn(sh, addr("C", firstModelRow), "", "aucun fonds retenu : modèle sans fonds")
	} else if !near1(wsum) {
		rep.add(LevelWarning, KindNormalized, sh, addr("E", firstModelRow), "", "%% alloués = %s %% : normalisés à 100 %%", wsum.Mul(hundred))
	}
	for _, x := range hs {
		m.Holdings = append(m.Holdings, ModelHolding{Symbol: x.sym, Name: x.name, Weight: toF(x.w.Div(wsum))})
	}
	return m
}

// parseCurrent lit DIVERSIFICATION : fonds et poids. L'onglet divise sans protection
// quand il est vide (écart de l'outil) ; ici, vide = aucune position.
func parseCurrent(f *excelize.File, opts Options, catalog map[string]bool, rep *Report) []CurrentHolding {
	const sh = SheetDiversification
	var hs []CurrentHolding
	seen := map[string]bool{}
	sum := decimal.Zero
	var ws []decimal.Decimal
	for row := firstCurrentRow; row < firstCurrentRow+maxCurrentFunds; row++ {
		raw := cell(f, sh, "C", row)
		if raw == "" {
			continue
		}
		sym, _, err := canonical(raw, opts.Exchange)
		if err != nil {
			rep.add(LevelWarning, KindSkipped, sh, addr("C", row), raw, "%v : ligne ignorée", err)
			continue
		}
		if seen[sym] {
			rep.add(LevelWarning, KindSkipped, sh, addr("C", row), sym, "fonds en double : ligne ignorée")
			continue
		}
		seen[sym] = true
		w, ok := fractionAt(f, sh, "E", row, rep, "% portefeuille")
		if !ok || w.IsZero() {
			rep.add(LevelWarning, KindSkipped, sh, addr("E", row), sym, "%% portefeuille absent ou nul : fonds ignoré")
			continue
		}
		if !catalog[sym] {
			rep.warn(sh, addr("C", row), sym, "fonds absent du catalogue de ce classeur")
		}
		hs = append(hs, CurrentHolding{Symbol: sym, Name: cell(f, sh, "D", row), Row: row})
		ws = append(ws, w)
		sum = sum.Add(w)
	}
	if len(hs) == 0 {
		rep.add(LevelInfo, "", sh, "", "", "aucune position actuelle")
		return nil
	}
	if !near1(sum) {
		rep.add(LevelWarning, KindNormalized, sh, addr("E", firstCurrentRow), "", "%% portefeuille = %s %% : normalisés à 100 %%", sum.Mul(hundred))
	}
	for i := range hs {
		hs[i].Weight = toF(ws[i].Div(sum))
	}
	return hs
}
