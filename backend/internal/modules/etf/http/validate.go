package httpapi

import (
	"regexp"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/shopspring/decimal"

	"github.com/omeroland/finance-privee/backend/internal/api/gen"
	"github.com/omeroland/finance-privee/backend/internal/platform/httpx"
)

var (
	symbolRe  = regexp.MustCompile(`^[A-Z]{2,10}:[A-Z0-9.\-]{1,15}$`)
	decimalRe = regexp.MustCompile(`^[0-9]{1,14}(\.[0-9]{1,6})?$`)
	monthRe   = regexp.MustCompile(`^[0-9]{4}-(0[1-9]|1[0-2])$`)
)

const invalidFields = "Un ou plusieurs champs sont invalides."

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

func (f *fieldErrors) text(field, s string, minLen, maxLen int) {
	if n := utf8.RuneCountInString(strings.TrimSpace(s)); n < minLen || utf8.RuneCountInString(s) > maxLen {
		f.add(field, "Le texte est hors des bornes de longueur.")
	}
}

func (f *fieldErrors) currency(field string, c gen.Currency) {
	if !c.Valid() {
		f.add(field, "La devise doit être CAD ou USD.")
	}
}

// amount lit un décimal positif ou nul (14 chiffres entiers, 6 décimales au plus,
// comme numeric(20,6)) ; strictlyPositive refuse zéro.
func (f *fieldErrors) amount(field string, s *gen.Decimal, strictlyPositive bool) *decimal.Decimal {
	if s == nil {
		return nil
	}
	if !decimalRe.MatchString(*s) {
		f.add(field, "Nombre décimal positif attendu, 6 décimales au plus, par exemple \"152.40\".")
		return nil
	}
	d, err := decimal.NewFromString(*s)
	if err != nil || (strictlyPositive && !d.IsPositive()) {
		f.add(field, "La valeur doit être strictement positive.")
		return nil
	}
	return &d
}

func validDate(t time.Time) bool { return t.Year() >= 1900 && t.Year() <= 2100 }
