package httpapi

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"

	corestore "github.com/omeroland/finance-privee/backend/internal/core/store"
	"github.com/omeroland/finance-privee/backend/internal/modules/etf/engine"
	etfstore "github.com/omeroland/finance-privee/backend/internal/modules/etf/store"
	"github.com/omeroland/finance-privee/backend/internal/platform/httpx"
)

const maxAccountsParam = 50

// today est le jour de valorisation par défaut (UTC).
func (h *Handler) today() time.Time { return h.now().UTC().Truncate(24 * time.Hour) }

// resolveAccounts rend les comptes demandés (tous si ids est vide), dans
// l'ordre de la demande sans doublon. Un compte inconnu ou d'un autre
// propriétaire donne 404 : la réponse ne distingue pas les deux cas.
func (h *Handler) resolveAccounts(ctx context.Context, owner uuid.UUID, ids []uuid.UUID) ([]corestore.Account, error) {
	all, err := h.ledger.ListAccounts(ctx, owner)
	if err != nil {
		return nil, translate(fmt.Errorf("lister les comptes: %w", err))
	}
	if len(ids) == 0 {
		return all, nil
	}
	if len(ids) > maxAccountsParam {
		return nil, httpx.BadRequest(invalidFields, httpx.FieldError{Field: "accounts", Message: "50 comptes au plus."})
	}
	byID := make(map[uuid.UUID]corestore.Account, len(all))
	for _, a := range all {
		byID[a.ID] = a
	}
	seen := map[uuid.UUID]bool{}
	out := make([]corestore.Account, 0, len(ids))
	for _, id := range ids {
		if seen[id] {
			continue
		}
		seen[id] = true
		a, ok := byID[id]
		if !ok {
			return nil, httpx.NotFound("Compte introuvable.")
		}
		out = append(out, a)
	}
	return out, nil
}

// profiles rend le dernier profil de chaque FNB du catalogue, par symbole.
func (h *Handler) profiles(ctx context.Context, owner uuid.UUID) (map[string]etfstore.Catalog, error) {
	rows, err := h.catalog.ListCatalog(ctx, owner, etfstore.CatalogFilter{})
	if err != nil {
		return nil, translate(fmt.Errorf("lister le catalogue: %w", err))
	}
	out := make(map[string]etfstore.Catalog, len(rows))
	for _, r := range rows {
		out[r.Symbol] = r
	}
	return out, nil
}

// fullAllocation traduit la répartition d'un profil et range la part non
// classée dans « autres actifs » avant tout calcul (D340).
func fullAllocation(c etfstore.Catalog) (engine.Allocation, error) {
	a, err := engine.CompleteAllocation(engine.Allocation{
		EquityCA: c.EquityCa, EquityUS: c.EquityUs, EquityIntl: c.EquityIntl,
		BondCA: c.BondCa, BondOther: c.BondOther, OtherAssets: c.OtherAssets,
	})
	if err != nil {
		return engine.Allocation{}, fmt.Errorf("répartition du fonds %s: %w", c.Symbol, err)
	}
	return a, nil
}

// planError traduit une erreur d'un moteur de plan en erreur d'API, sans détail interne.
func planError(err error) error {
	var (
		pe *engine.PriceError
		te *engine.TargetsError
	)
	switch {
	case errors.As(err, &pe):
		return httpx.Unprocessable("Le prix du fonds " + pe.Fund + " est manquant ou invalide.")
	case errors.As(err, &te):
		return httpx.Unprocessable("Les cibles du modèle sont incohérentes.")
	case errors.Is(err, engine.ErrRebalanceInput), errors.Is(err, engine.ErrAllocation),
		errors.Is(err, engine.ErrFund), errors.Is(err, engine.ErrWeight), errors.Is(err, engine.ErrAmount):
		return httpx.Unprocessable("Les positions, l'encaisse ou le modèle sont incohérents.")
	}
	return err
}

// missingList énumère des symboles pour un message.
func missingList(symbols []string) string { return strings.Join(symbols, ", ") }
