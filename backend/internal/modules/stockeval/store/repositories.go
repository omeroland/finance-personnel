package store

import (
	"context"
	"encoding/json"
	"time"

	"github.com/google/uuid"
)

// Interfaces consommées par http/. Toutes prennent le propriétaire explicitement.

// WatchlistRepository gère la liste de suivi.
type WatchlistRepository interface {
	ListWatchlist(ctx context.Context, owner uuid.UUID) ([]Instrument, error)
	// AddToWatchlist crée l'instrument au besoin et l'ajoute ; idempotent.
	AddToWatchlist(ctx context.Context, owner uuid.UUID, in InstrumentInput) (Instrument, error)
	RemoveFromWatchlist(ctx context.Context, owner, instrumentID uuid.UUID) error
	GetInstrument(ctx context.Context, owner, instrumentID uuid.UUID) (Instrument, error)
}

// FactRepository gère les faits annuels et leurs conflits.
type FactRepository interface {
	ListFacts(ctx context.Context, owner, instrumentID uuid.UUID) ([]Fact, error)
	WriteFact(ctx context.Context, owner uuid.UUID, f Fact) (FactOutcome, error)
	ListOpenConflicts(ctx context.Context, owner, instrumentID uuid.UUID) ([]FactConflict, error)
}

// SnapshotRepository gère les valeurs courantes.
type SnapshotRepository interface {
	GetSnapshot(ctx context.Context, owner, instrumentID uuid.UUID) (Snapshot, error)
	// SaveSnapshot renvoie false si une valeur manuelle protège la ligne.
	SaveSnapshot(ctx context.Context, owner uuid.UUID, s Snapshot) (written bool, err error)
}

// QualitativeRepository gère les règles 1 à 4.
type QualitativeRepository interface {
	GetQualitative(ctx context.Context, owner, instrumentID uuid.UUID) (Qualitative, error)
	SaveQualitative(ctx context.Context, owner uuid.UUID, q Qualitative) error
}

// EvaluationRepository gère l'archive en insertion seule.
type EvaluationRepository interface {
	ArchiveEvaluation(ctx context.Context, owner uuid.UUID, in EvaluationInput) (Evaluation, error)
	GetEvaluation(ctx context.Context, owner, id uuid.UUID) (Evaluation, error)
	ListEvaluations(ctx context.Context, owner, instrumentID uuid.UUID, limit int) ([]Evaluation, error)
}

// ImportJobRepository gère la traçabilité des imports.
type ImportJobRepository interface {
	CreateImportJob(ctx context.Context, owner uuid.UUID, jobType string) (ImportJob, error)
	FinishImportJob(ctx context.Context, owner, id uuid.UUID, status string, report json.RawMessage, finishedAt *time.Time) (ImportJob, error)
	GetImportJob(ctx context.Context, owner, id uuid.UUID) (ImportJob, error)
	ListImportJobs(ctx context.Context, owner uuid.UUID, limit int) ([]ImportJob, error)
}

var (
	_ WatchlistRepository   = (*Store)(nil)
	_ FactRepository        = (*Store)(nil)
	_ SnapshotRepository    = (*Store)(nil)
	_ QualitativeRepository = (*Store)(nil)
	_ EvaluationRepository  = (*Store)(nil)
	_ ImportJobRepository   = (*Store)(nil)
)
