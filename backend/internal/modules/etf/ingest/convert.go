package ingest

import (
	"fmt"
	"strings"

	"github.com/shopspring/decimal"
)

// Seuils de D351 : au-delà, une valeur est lue comme un pourcentage.
const (
	maxShareFraction  = 1.0 // parts de classe, RFG, cibles
	maxReturnFraction = 5.0 // rendements (500 %)
)

var (
	hundred = decimal.NewFromInt(100)
	million = decimal.NewFromInt(1_000_000)
)

// cleanText retire les espaces (dont insécables) et normalise le signe moins.
func cleanText(raw string) string {
	r := strings.NewReplacer(
		" ", "", " ", "", " ", "", "\t", "",
		"−", "-", "–", "-", "—", "-",
	)
	return r.Replace(strings.TrimSpace(raw))
}

// isBlank est vrai pour une cellule vide ou un tiret (« - » : valeur absente dans l'outil).
func isBlank(raw string) bool {
	s := cleanText(raw)
	return s == "" || s == "-" || strings.EqualFold(s, "n/a") || strings.EqualFold(s, "nd")
}

// parseNumber lit « 0.196 », « 19,6 % », « -2,6% » ou « 25 ». Le drapeau pct
// indique la présence du signe « % ». ok=false si la cellule est vide ou « - ».
func parseNumber(raw string) (v decimal.Decimal, pct, ok bool, err error) {
	if isBlank(raw) {
		return decimal.Zero, false, false, nil
	}
	s := cleanText(raw)
	if strings.HasSuffix(s, "%") {
		pct = true
		s = strings.TrimSuffix(s, "%")
	}
	s = strings.ReplaceAll(s, ",", ".")
	d, e := decimal.NewFromString(s)
	if e != nil {
		return decimal.Zero, false, false, fmt.Errorf("valeur non numérique %q", strings.TrimSpace(raw))
	}
	return d, pct, true, nil
}

// parseFraction convertit une cellule en fraction (0.196 pour « 19,6 % »).
// Une valeur sans « % » dont la valeur absolue dépasse limit est lue comme
// un pourcentage (D351) ; normalized l'indique à l'appelant pour l'avertissement.
func parseFraction(raw string, limit float64) (v decimal.Decimal, normalized, ok bool, err error) {
	d, pct, ok, err := parseNumber(raw)
	if err != nil || !ok {
		return decimal.Zero, false, false, err
	}
	switch {
	case pct:
		return d.Div(hundred), false, true, nil
	case d.Abs().GreaterThan(decimal.NewFromFloat(limit)):
		return d.Div(hundred), true, true, nil
	}
	return d, false, true, nil
}

// parseSize convertit « 880M$ » en dollars. Suffixes : K, M, G (ou B) ;
// sans suffixe, la valeur est en millions, comme l'en-tête « GROSSEUR (M$) » (D352).
func parseSize(raw string) (v decimal.Decimal, ok bool, err error) {
	if isBlank(raw) {
		return decimal.Zero, false, nil
	}
	s := strings.ToUpper(cleanText(raw))
	s = strings.TrimSuffix(s, "$")
	s = strings.TrimSuffix(s, "CAD")
	mult := million
	switch {
	case strings.HasSuffix(s, "K"):
		mult, s = decimal.NewFromInt(1_000), strings.TrimSuffix(s, "K")
	case strings.HasSuffix(s, "M"):
		s = strings.TrimSuffix(s, "M")
	case strings.HasSuffix(s, "G"), strings.HasSuffix(s, "B"):
		mult, s = decimal.NewFromInt(1_000_000_000), s[:len(s)-1]
	}
	s = strings.ReplaceAll(s, ",", ".")
	d, e := decimal.NewFromString(s)
	if e != nil {
		return decimal.Zero, false, fmt.Errorf("taille non numérique %q", strings.TrimSpace(raw))
	}
	if d.IsNegative() {
		return decimal.Zero, false, fmt.Errorf("taille négative %q", strings.TrimSpace(raw))
	}
	return d.Mul(mult), true, nil
}

// mapType traduit le type de l'outil vers l'énumération du contrat (D311).
func mapType(raw string) (string, bool) {
	switch strings.ToLower(strings.TrimSpace(raw)) {
	case "actions", "actions privilégiées", "actions privilegiees":
		return "equity", true
	case "obligations":
		return "fixed_income", true
	case "tout-en-un":
		return "balanced", true
	case "cryptomonnaies", "métaux", "metaux", "immobilier":
		return "other", true
	}
	return "other", false
}

// mapRegion traduit la région de l'outil ; un fonds tout-en-un « International » est global.
func mapRegion(raw, fundType string) (string, bool) {
	switch strings.ToLower(strings.TrimSpace(raw)) {
	case "canada":
		return "ca", true
	case "états-unis", "etats-unis", "États-Unis", "usa":
		return "us", true
	case "international", "chine", "inde", "europe", "asie", "marchés émergents":
		if fundType == "balanced" && strings.EqualFold(strings.TrimSpace(raw), "international") {
			return "global", true
		}
		return "intl", true
	case "global", "mondial":
		return "global", true
	}
	return "", false
}
