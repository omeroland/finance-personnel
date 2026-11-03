package httpapi

import (
	"regexp"
	"strconv"
	"strings"
	"unicode/utf8"

	"github.com/shopspring/decimal"

	"github.com/omeroland/finance-privee/backend/internal/api/gen"
	"github.com/omeroland/finance-privee/backend/internal/platform/httpx"
)

var (
	symbolRe  = regexp.MustCompile(`^[A-Z]{2,10}:[A-Z0-9.\-]{1,15}$`)
	decimalRe = regexp.MustCompile(`^-?[0-9]+(\.[0-9]+)?$`)
)

const invalidFields = "Un ou plusieurs champs sont invalides."

// fieldErrors accumule les erreurs de validation d'une requête.
type fieldErrors []httpx.FieldError

func (f *fieldErrors) add(field, msg string) {
	*f = append(*f, httpx.FieldError{Field: field, Message: msg})
}

func (f fieldErrors) err() error {
	if len(f) == 0 {
		return nil
	}
	return httpx.BadRequest(invalidFields, f...)
}

func validSymbol(s string) bool { return symbolRe.MatchString(s) }

// checkSymbol valide le symbole d'un chemin.
func checkSymbol(s string) error {
	if !validSymbol(s) {
		return httpx.BadRequest(invalidFields, httpx.FieldError{Field: "symbol", Message: "Le symbole doit suivre le format BOURSE:SYMBOLE."})
	}
	return nil
}

// exchangeOf déduit la bourse du préfixe du symbole canonique.
func exchangeOf(symbol string) (gen.Exchange, bool) {
	ex, _, _ := strings.Cut(symbol, ":")
	e := gen.Exchange(ex)
	return e, e.Valid()
}

func checkName(f *fieldErrors, name string) {
	if n := utf8.RuneCountInString(strings.TrimSpace(name)); n < 1 || n > 200 {
		f.add("name", "Le nom doit compter de 1 à 200 caractères.")
	}
}

func checkCurrency(f *fieldErrors, field string, c gen.Currency) {
	if !c.Valid() {
		f.add(field, "La devise doit être CAD ou USD.")
	}
}

// parseDecimal valide et lit une chaîne décimale du contrat.
func parseDecimal(f *fieldErrors, field, s string) (decimal.Decimal, bool) {
	if !decimalRe.MatchString(s) || len(s) > 40 {
		f.add(field, "Nombre décimal attendu, par exemple \"152.40\".")
		return decimal.Decimal{}, false
	}
	d, err := decimal.NewFromString(s)
	if err != nil {
		f.add(field, "Nombre décimal attendu, par exemple \"152.40\".")
		return decimal.Decimal{}, false
	}
	return d, true
}

// float32Decimal lit un nombre du contrat sans l'erreur de représentation de float32.
func float32Decimal(v float32) decimal.Decimal {
	d, err := decimal.NewFromString(strconv.FormatFloat(float64(v), 'f', -1, 32))
	if err != nil {
		return decimal.Zero
	}
	return d
}

// modeAndHorizon valide les paramètres de la liste. En mode compat, seul l'horizon
// de la feuille est offert (D141) ; un autre horizon explicite est refusé.
func (h *Handler) modeAndHorizon(m *gen.Mode, hz *gen.Horizon) (gen.Mode, int, error) {
	mode, err := parseMode(m)
	if err != nil {
		return "", 0, err
	}
	horizon := 10
	if hz != nil {
		if !hz.Valid() {
			return "", 0, httpx.BadRequest(invalidFields, httpx.FieldError{Field: "horizon", Message: "L'horizon doit valoir 5, 10, 15 ou 20."})
		}
		horizon = int(*hz)
	}
	if mode == gen.Compat {
		fixed := h.rules.Compat.Rule5bHorizon
		if hz != nil && horizon != fixed {
			return "", 0, httpx.BadRequest(invalidFields, httpx.FieldError{
				Field: "horizon", Message: "Le mode compat ne porte que sur l'horizon de " + strconv.Itoa(fixed) + " ans.",
			})
		}
		horizon = fixed
	}
	return mode, horizon, nil
}

func parseMode(m *gen.Mode) (gen.Mode, error) {
	if m == nil {
		return gen.Corrected, nil
	}
	if !m.Valid() {
		return "", httpx.BadRequest(invalidFields, httpx.FieldError{Field: "mode", Message: "Le mode doit être corrected ou compat."})
	}
	return *m, nil
}
