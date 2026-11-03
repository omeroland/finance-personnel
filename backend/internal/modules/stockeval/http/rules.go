package httpapi

import (
	"context"
	"fmt"
	"slices"
	"strings"

	"github.com/omeroland/finance-privee/backend/internal/api/gen"
	"github.com/omeroland/finance-privee/backend/internal/modules/stockeval/engine"
)

// criterionMeta est le texte d'affichage d'un critère (docs/rules.md) ; les seuils viennent du référentiel.
var criterionMeta = map[string]struct{ label, formula, period string }{
	"5a":  {"Croissance du BPA", "Moyenne tronquée des taux de croissance du BPA", "Au moins 10 ans"},
	"5b":  {"Hausses du BPA", "Nombre de taux de croissance positifs dans la fenêtre", "5, 10, 15, 20 ans"},
	"6":   {"Croissance du dividende", "Moyenne tronquée des taux de croissance du dividende par action", "Au moins 10 ans"},
	"7":   {"Ratio de distribution", "Dividende annuel par action / BPA", "Dernier exercice"},
	"8":   {"Dette", "Dette à long terme / capitaux propres", "Dernier bilan"},
	"9":   {"Coupe de dividende", "Dividende prévu comparé au dernier dividende", "1 an"},
	"10":  {"Rachats d'actions", "Actions en circulation : année la plus récente contre année précédente", "1 an"},
	"11a": {"P/E", "Prix de l'action / BPA", "Courant"},
	"11b": {"Rendement vs moyenne", "Rendement actuel contre rendement moyen", "Fenêtre de l'horizon"},
	"11c": {"P/B", "Prix de l'action / valeur comptable par action", "Courant"},
}

func thrOfRule(t engine.Threshold) thr {
	return thr{gt: t.Gt, ge: t.Ge, le: t.Le, direction: t.Direction}
}

// GetRules : référentiel des règles pour les libellés des clients.
func (h *Handler) GetRules(_ context.Context, _ gen.GetRulesRequestObject) (gen.GetRulesResponseObject, error) {
	out := gen.Rules{Version: h.rules.Version, Criteria: make([]gen.RuleDefinition, 0, len(engine.CriterionIDs))}
	for _, id := range engine.CriterionIDs {
		c, ok := h.rules.Criterion(id)
		if !ok {
			return nil, fmt.Errorf("critère %s absent du référentiel", id)
		}
		meta := criterionMeta[id]
		def := gen.RuleDefinition{Id: gen.CriterionId(id), Label: meta.label, Formula: meta.formula, Period: meta.period}
		def.Strict = ruleThreshold(id, c.Strict)
		if c.Relaxed != nil {
			def.Relaxed = ptr(ruleThreshold(id, *c.Relaxed))
		}
		if len(c.Strict.MinRises) > 0 {
			by := []gen.HorizonThreshold{}
			for _, hz := range h.rules.Parameters.Horizons {
				if _, ok := c.Strict.MinRises[hz]; !ok {
					continue
				}
				ht := gen.HorizonThreshold{Horizon: gen.Horizon(hz), Strict: ruleThresholdAt(id, c.Strict, hz)}
				if c.Relaxed != nil {
					ht.Relaxed = ptr(ruleThresholdAt(id, *c.Relaxed, hz))
				}
				by = append(by, ht)
			}
			def.ByHorizon = &by
		}
		out.Criteria = append(out.Criteria, def)
	}
	return gen.GetRules200JSONResponse(out), nil
}

// ruleThreshold décrit un seuil sans horizon ; pour 5b, la liste des seuils selon l'horizon.
func ruleThreshold(id string, t engine.Threshold) gen.Threshold {
	if len(t.MinRises) == 0 {
		return thresholdDTO(id, thrOfRule(t))
	}
	hz := make([]int, 0, len(t.MinRises))
	for k := range t.MinRises {
		hz = append(hz, k)
	}
	slices.Sort(hz)
	parts := make([]string, 0, len(hz))
	for _, k := range hz {
		parts = append(parts, fmt.Sprint(t.MinRises[k]))
	}
	return gen.Threshold{Description: "≥ " + strings.Join(parts, ", ") + " hausses selon l'horizon"}
}

func ruleThresholdAt(id string, t engine.Threshold, horizon int) gen.Threshold {
	n := t.MinRises[horizon]
	return thresholdDTO(id, thr{minRises: &n})
}
