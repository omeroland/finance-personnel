package httpapi

import (
	"time"

	"github.com/google/uuid"
	"github.com/shopspring/decimal"

	"github.com/omeroland/finance-privee/backend/internal/api/gen"
	"github.com/omeroland/finance-privee/backend/internal/modules/stockeval/store"
)

// mergeSnapshot applique une saisie manuelle sur l'instantané existant : seules les
// propriétés fournies changent (les valeurs importées de la feuille restent). La
// devise du contrat est celle de cotation (D200). Les erreurs vont dans fe.
func mergeSnapshot(fe *fieldErrors, cur *store.Snapshot, in gen.Snapshot, id uuid.UUID, quoteCurrency string, now time.Time) *store.Snapshot {
	out := store.Snapshot{InstrumentID: id}
	if cur != nil {
		out = *cur
	}
	if string(in.Currency) != quoteCurrency {
		fe.add("snapshot.currency", "La devise de l'instantané doit être la devise de cotation ("+quoteCurrency+").")
	}
	set := func(field string, v *string, dst *decimal.NullDecimal) {
		if v == nil {
			return
		}
		if d, ok := parseDecimal(fe, "snapshot."+field, *v); ok {
			*dst = decimal.NewNullDecimal(d)
		}
	}
	set("price", in.Price, &out.Price)
	set("annualDividend", in.AnnualDividend, &out.ExpectedDividend)
	set("longTermDebt", in.LongTermDebt, &out.LTDebt)
	set("equity", in.Equity, &out.Equity)
	set("bookValuePerShare", in.BookValuePerShare, &out.BookValuePerShare)
	if in.FxRate != nil {
		if *in.FxRate < 0 {
			fe.add("snapshot.fxRate", "Le taux de change ne peut pas être négatif.")
		} else {
			out.FXRate = decimal.NewNullDecimal(float32Decimal(*in.FxRate))
		}
	}
	out.AsOf = now.UTC()
	if in.AsOf != nil {
		out.AsOf = in.AsOf.UTC()
	}
	out.Source = store.SourceManual
	return &out
}
