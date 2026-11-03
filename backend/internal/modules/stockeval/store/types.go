package store

import (
	"encoding/json"
	"time"

	"github.com/google/uuid"
	"github.com/shopspring/decimal"
)

// Sources de données.
const (
	SourceManual   = "manual"
	SourceXLSX     = "xlsx"
	SourceProvider = "provider"
)

// Mesures des faits annuels.
const (
	MetricEPSDiluted    = "eps_diluted"
	MetricDPS           = "dps"
	MetricDividendYield = "dividend_yield"
	MetricSharesDiluted = "shares_diluted"
)

// Instrument est un titre du socle (core.instrument).
type Instrument struct {
	ID             uuid.UUID `json:"id"`
	Symbol         string    `json:"symbol"`
	Name           string    `json:"name"`
	Exchange       string    `json:"exchange"`
	Kind           string    `json:"kind"`
	QuoteCurrency  string    `json:"quote_currency"`
	ReportCurrency string    `json:"report_currency"`
	IsFinancial    bool      `json:"is_financial"`
	CreatedAt      time.Time `json:"created_at"`
}

// InstrumentInput décrit un titre à suivre.
type InstrumentInput struct {
	Symbol         string
	Name           string
	Exchange       string
	Kind           string // "stock" par défaut
	QuoteCurrency  string
	ReportCurrency string
	IsFinancial    bool
}

// Fact est une valeur annuelle d'une mesure (format étroit).
type Fact struct {
	InstrumentID uuid.UUID       `json:"instrument_id"`
	FiscalYear   int             `json:"fiscal_year"`
	Metric       string          `json:"metric"`
	Value        decimal.Decimal `json:"value"`
	Currency     *string         `json:"currency,omitempty"`
	Source       string          `json:"source"`
	FetchedAt    time.Time       `json:"fetched_at,omitzero"`
}

// FactOutcome dit ce qu'une écriture de fait a produit.
type FactOutcome string

// Issues possibles de WriteFact.
const (
	FactCreated    FactOutcome = "created"  // nouvelle ligne
	FactUpdated    FactOutcome = "updated"  // valeur ou source remplacée
	FactConflicted FactOutcome = "conflict" // valeur manuelle protégée, conflit enregistré
	FactNoop       FactOutcome = "noop"     // identique, ou conflit déjà ouvert
)

// FactConflict est une divergence à trancher par l'utilisateur.
type FactConflict struct {
	ID             uuid.UUID       `json:"id"`
	InstrumentID   uuid.UUID       `json:"instrument_id"`
	FiscalYear     int             `json:"fiscal_year"`
	Metric         string          `json:"metric"`
	ExistingValue  decimal.Decimal `json:"existing_value"`
	ExistingSource string          `json:"existing_source"`
	IncomingValue  decimal.Decimal `json:"incoming_value"`
	IncomingSource string          `json:"incoming_source"`
	CreatedAt      time.Time       `json:"created_at"`
}

// Snapshot regroupe les valeurs courantes d'un titre.
type Snapshot struct {
	InstrumentID        uuid.UUID           `json:"instrument_id"`
	Price               decimal.NullDecimal `json:"price"`
	ExpectedDividend    decimal.NullDecimal `json:"expected_dividend"`
	LTDebt              decimal.NullDecimal `json:"lt_debt"`
	Equity              decimal.NullDecimal `json:"equity"`
	DebtToEquity        decimal.NullDecimal `json:"debt_to_equity"`
	BookValuePerShare   decimal.NullDecimal `json:"book_value_per_share"`
	PEImported          decimal.NullDecimal `json:"pe_imported"`
	PayoutRatioImported decimal.NullDecimal `json:"payout_ratio_imported"`
	PBImported          decimal.NullDecimal `json:"pb_imported"`
	FXRate              decimal.NullDecimal `json:"fx_rate"`
	AsOf                time.Time           `json:"as_of"`
	Source              string              `json:"source"`
}

// Qualitative porte le jugement de l'utilisateur (règles 1 à 4, nil = vide).
type Qualitative struct {
	InstrumentID  uuid.UUID       `json:"instrument_id"`
	R1            *bool           `json:"r1"`
	R2            *bool           `json:"r2"`
	R3            *bool           `json:"r3"`
	R4            *bool           `json:"r4"`
	Notes         json.RawMessage `json:"notes"`
	BuybackManual *bool           `json:"buyback_manual"`
	ReviewedAt    *time.Time      `json:"reviewed_at"`
}

// Evaluation est une ligne de l'archive immuable.
type Evaluation struct {
	ID           uuid.UUID       `json:"id"`
	InstrumentID uuid.UUID       `json:"instrument_id"`
	Mode         string          `json:"mode"`
	RulesVersion string          `json:"rules_version"`
	RulesHash    string          `json:"rules_hash"`
	InputsHash   string          `json:"inputs_hash"`
	Inputs       json.RawMessage `json:"inputs"`
	Result       json.RawMessage `json:"result"`
	CreatedAt    time.Time       `json:"created_at"`
}

// EvaluationInput est ce que http/ fournit pour archiver une évaluation.
type EvaluationInput struct {
	InstrumentID uuid.UUID
	Mode         string
	RulesVersion string
	RulesHash    string
	Inputs       json.RawMessage // l'empreinte en est calculée ici
	Result       json.RawMessage
}

// Statuts et types des jobs d'import.
const (
	JobPending   = "pending"
	JobRunning   = "running"
	JobSucceeded = "succeeded"
	JobFailed    = "failed"

	JobTypeXLSX     = "xlsx"
	JobTypeProvider = "provider"
)

// ImportJob trace un import.
type ImportJob struct {
	ID         uuid.UUID       `json:"id"`
	Type       string          `json:"type"`
	Status     string          `json:"status"`
	Report     json.RawMessage `json:"report"`
	CreatedAt  time.Time       `json:"created_at"`
	FinishedAt *time.Time      `json:"finished_at,omitempty"`
}
