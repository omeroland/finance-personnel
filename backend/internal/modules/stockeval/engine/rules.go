// Package engine contient le moteur de règles pur du module 1 : référentiel, mesures et pointage.
package engine

import (
	"bytes"
	"crypto/sha256"
	_ "embed"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"slices"

	"go.yaml.in/yaml/v3"
)

//go:embed rules.yaml
var embeddedRules []byte

// CriterionIDs liste les critères, dans l'ordre du référentiel.
var CriterionIDs = []string{"5a", "5b", "6", "7", "8", "9", "10", "11a", "11b", "11c"}

// relaxedIDs sont les sept critères qui ont un seuil assoupli.
var relaxedIDs = map[string]bool{"5a": true, "5b": true, "6": true, "7": true, "8": true, "11a": true, "11c": true}

// Directions admises pour les critères comparatifs (10 et 11b).
const (
	DirectionDecrease = "decrease" // règle 10 : baisse
	DirectionGreater  = "greater"  // règle 11b : strictement supérieur
)

// Threshold est un seuil. Les ratios sont des fractions (0.70 = 70 %).
type Threshold struct {
	Gt        *float64    `yaml:"gt"`
	Ge        *float64    `yaml:"ge"`
	Le        *float64    `yaml:"le"`
	MinRises  map[int]int `yaml:"min_rises"` // règle 5b : hausses minimales par horizon
	Direction string      `yaml:"direction"` // règles 10 et 11b
}

// Criterion est un critère avec son seuil strict et, s'il existe, son seuil assoupli.
type Criterion struct {
	ID      string     `yaml:"-"`
	Strict  Threshold  `yaml:"strict"`
	Relaxed *Threshold `yaml:"relaxed"`
}

// Parameters regroupe les paramètres transversaux.
type Parameters struct {
	Horizons           []int `yaml:"horizons"`
	TrimMinRates       int   `yaml:"trim_min_rates"`
	MinVerdictHorizon  int   `yaml:"min_verdict_horizon"`
	BuybackPeriodYears int   `yaml:"buyback_period_years"`
}

// Compat décrit les comportements de la feuille reproduits en mode compat.
type Compat struct {
	Divisor                   int    `yaml:"divisor"`
	Rule5bHorizon             int    `yaml:"rule_5b_horizon"`
	Rule5bMinRises            int    `yaml:"rule_5b_min_rises"`
	Rule5bSkipFirstTransition bool   `yaml:"rule_5b_skip_first_transition"`
	LowerBoundsChecked        bool   `yaml:"lower_bounds_checked"`
	EmptyDataPasses           bool   `yaml:"empty_data_passes"`
	EmptyCellAsZero           bool   `yaml:"empty_cell_as_zero"`
	Rule9Operator             string `yaml:"rule_9_operator"`
}

// Rules est le référentiel chargé et validé.
type Rules struct {
	Version    string
	Hash       string // empreinte SHA-256 (hexadécimal) des octets du fichier
	Parameters Parameters
	Compat     Compat
	Criteria   map[string]Criterion
}

type rawRules struct {
	Version    string               `yaml:"version"`
	Parameters Parameters           `yaml:"parameters"`
	Criteria   map[string]Criterion `yaml:"criteria"`
	Compat     Compat               `yaml:"compat"`
}

// Criterion retourne le critère d'identifiant id (5a, 5b, 6, 7, 8, 9, 10, 11a, 11b, 11c).
func (r *Rules) Criterion(id string) (Criterion, bool) {
	c, ok := r.Criteria[id]
	return c, ok
}

// DefaultRules charge le référentiel embarqué.
func DefaultRules() (*Rules, error) { return LoadRules(embeddedRules) }

// LoadRules analyse et valide un fichier rules.yaml.
func LoadRules(data []byte) (*Rules, error) {
	dec := yaml.NewDecoder(bytes.NewReader(data))
	dec.KnownFields(true)
	var raw rawRules
	if err := dec.Decode(&raw); err != nil {
		if errors.Is(err, io.EOF) {
			return nil, errors.New("rules.yaml: fichier vide")
		}
		return nil, fmt.Errorf("rules.yaml: lecture: %w", err)
	}
	sum := sha256.Sum256(data)
	r := &Rules{
		Version:    raw.Version,
		Hash:       hex.EncodeToString(sum[:]),
		Parameters: raw.Parameters,
		Compat:     raw.Compat,
		Criteria:   make(map[string]Criterion, len(raw.Criteria)),
	}
	for id, c := range raw.Criteria {
		c.ID = id
		r.Criteria[id] = c
	}
	if err := r.validate(); err != nil {
		return nil, fmt.Errorf("rules.yaml: %w", err)
	}
	return r, nil
}

func (r *Rules) validate() error {
	if r.Version == "" {
		return errors.New("champ version manquant")
	}
	p := r.Parameters
	if len(p.Horizons) == 0 || slices.Min(p.Horizons) < 2 || !slices.IsSorted(p.Horizons) ||
		len(slices.Compact(slices.Clone(p.Horizons))) != len(p.Horizons) {
		return errors.New("parameters.horizons: liste croissante d'au moins 2 ans requise")
	}
	if !slices.Contains(p.Horizons, p.MinVerdictHorizon) {
		return fmt.Errorf("parameters.min_verdict_horizon %d: doit être l'un des horizons", p.MinVerdictHorizon)
	}
	if p.TrimMinRates < 3 {
		return errors.New("parameters.trim_min_rates: au moins 3 requis")
	}
	if p.BuybackPeriodYears < 1 {
		return errors.New("parameters.buyback_period_years: au moins 1 requis")
	}
	c := r.Compat
	if c.Divisor <= 0 {
		return errors.New("compat.divisor: doit être positif")
	}
	if !slices.Contains(p.Horizons, c.Rule5bHorizon) || c.Rule5bMinRises < 0 {
		return errors.New("compat.rule_5b_horizon / rule_5b_min_rises: valeurs invalides")
	}
	if !slices.Contains([]string{">=", ">"}, c.Rule9Operator) {
		return fmt.Errorf("compat.rule_9_operator %q: attendu >= ou >", c.Rule9Operator)
	}
	for _, id := range CriterionIDs {
		cr, ok := r.Criteria[id]
		if !ok {
			return fmt.Errorf("critère %s manquant", id)
		}
		if err := r.validateCriterion(cr); err != nil {
			return fmt.Errorf("critère %s: %w", id, err)
		}
	}
	for id := range r.Criteria {
		if !slices.Contains(CriterionIDs, id) {
			return fmt.Errorf("critère %s inconnu", id)
		}
	}
	return nil
}

func (r *Rules) validateCriterion(c Criterion) error {
	if err := r.validateShape(c.ID, c.Strict, "strict"); err != nil {
		return err
	}
	if !relaxedIDs[c.ID] {
		if c.Relaxed != nil {
			return errors.New("seuil assoupli non prévu pour ce critère")
		}
		return nil
	}
	if c.Relaxed == nil {
		return errors.New("seuil assoupli manquant")
	}
	if err := r.validateShape(c.ID, *c.Relaxed, "assoupli"); err != nil {
		return err
	}
	s, x := c.Strict, *c.Relaxed
	if x.Ge != nil && s.Ge != nil && *x.Ge > *s.Ge {
		return fmt.Errorf("seuil assoupli (ge %v) plus strict que le seuil strict (ge %v)", *x.Ge, *s.Ge)
	}
	if x.Le != nil && s.Le != nil && *x.Le < *s.Le {
		return fmt.Errorf("seuil assoupli (le %v) plus strict que le seuil strict (le %v)", *x.Le, *s.Le)
	}
	// Les bornes basses d'un intervalle ne s'assouplissent pas (docs/rules.md).
	if s.Le != nil && (!equalPtr(x.Gt, s.Gt) || !equalPtr(x.Ge, s.Ge)) {
		return errors.New("la borne basse (gt, ge) du seuil assoupli doit égaler celle du seuil strict")
	}
	for h, n := range s.MinRises {
		if x.MinRises[h] > n {
			return fmt.Errorf("seuil assoupli (%d hausses sur %d ans) plus strict que le seuil strict (%d)", x.MinRises[h], h, n)
		}
	}
	return nil
}

func (r *Rules) validateShape(id string, t Threshold, label string) error {
	switch id {
	case "5b":
		if len(t.MinRises) != len(r.Parameters.Horizons) {
			return fmt.Errorf("seuil %s: min_rises doit couvrir exactement les horizons %v", label, r.Parameters.Horizons)
		}
		for _, h := range r.Parameters.Horizons {
			n, ok := t.MinRises[h]
			if !ok || n < 0 || n > h-1 {
				return fmt.Errorf("seuil %s: min_rises[%d] doit être entre 0 et %d", label, h, h-1)
			}
		}
	case "10":
		if t.Direction != DirectionDecrease {
			return fmt.Errorf("seuil %s: direction %q, attendu %q", label, t.Direction, DirectionDecrease)
		}
	case "11b":
		if t.Direction != DirectionGreater {
			return fmt.Errorf("seuil %s: direction %q, attendu %q", label, t.Direction, DirectionGreater)
		}
	default:
		if t.Gt == nil && t.Ge == nil && t.Le == nil {
			return fmt.Errorf("seuil %s: aucune borne (gt, ge, le)", label)
		}
		if t.Gt != nil && t.Ge != nil {
			return fmt.Errorf("seuil %s: gt et ge exclusifs", label)
		}
		lo := t.Gt
		if lo == nil {
			lo = t.Ge
		}
		if lo != nil && t.Le != nil && *lo > *t.Le {
			return fmt.Errorf("seuil %s: borne basse au-dessus de la borne haute", label)
		}
	}
	return nil
}

func equalPtr(a, b *float64) bool {
	if a == nil || b == nil {
		return a == b
	}
	return *a == *b
}
