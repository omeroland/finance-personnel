package store

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/google/uuid"

	"github.com/omeroland/finance-privee/backend/internal/modules/stockeval/store/storedb"
)

func toJob(r storedb.StockevalImportJob) ImportJob {
	return ImportJob{ID: r.ID, Type: r.Type, Status: r.Status, Report: r.Report, CreatedAt: r.CreatedAt, FinishedAt: r.FinishedAt}
}

// CreateImportJob ouvre un job en statut pending.
func (s *Store) CreateImportJob(ctx context.Context, owner uuid.UUID, jobType string) (ImportJob, error) {
	if jobType != JobTypeXLSX && jobType != JobTypeProvider {
		return ImportJob{}, fmt.Errorf("type de job: %w", ErrInvalid)
	}
	var out ImportJob
	err := s.inTx(ctx, func(q *storedb.Queries) error {
		row, err := q.InsertImportJob(ctx, storedb.InsertImportJobParams{OwnerID: owner, Type: jobType})
		if err != nil {
			return fmt.Errorf("créer le job d'import: %w", err)
		}
		out = toJob(row)
		return audit(ctx, q, owner, "import_job.create", "stockeval.import_job", nil, map[string]any{"id": out.ID, "type": out.Type})
	})
	return out, err
}

// FinishImportJob change le statut d'un job et enregistre le compte rendu.
func (s *Store) FinishImportJob(ctx context.Context, owner, id uuid.UUID, status string, report json.RawMessage, finishedAt *time.Time) (ImportJob, error) {
	switch status {
	case JobPending, JobRunning, JobSucceeded, JobFailed:
	default:
		return ImportJob{}, fmt.Errorf("statut de job: %w", ErrInvalid)
	}
	if len(report) == 0 {
		report = json.RawMessage(`{}`)
	}
	rep, err := canonicalJSON(report)
	if err != nil {
		return ImportJob{}, fmt.Errorf("compte rendu: %w", ErrInvalid)
	}
	var out ImportJob
	err = s.inTx(ctx, func(q *storedb.Queries) error {
		cur, err := q.GetImportJobForUpdate(ctx, storedb.GetImportJobForUpdateParams{OwnerID: owner, ID: id})
		if err != nil {
			return notFound(err, "job "+id.String())
		}
		row, err := q.UpdateImportJob(ctx, storedb.UpdateImportJobParams{
			OwnerID: owner, ID: id, Status: status, Report: rep, FinishedAt: finishedAt,
		})
		if err != nil {
			return fmt.Errorf("mettre à jour le job: %w", err)
		}
		out = toJob(row)
		return audit(ctx, q, owner, "import_job.update", "stockeval.import_job",
			map[string]any{"id": cur.ID, "status": cur.Status}, map[string]any{"id": out.ID, "status": out.Status})
	})
	return out, err
}

// GetImportJob renvoie un job du propriétaire.
func (s *Store) GetImportJob(ctx context.Context, owner, id uuid.UUID) (ImportJob, error) {
	r, err := s.read().GetImportJob(ctx, storedb.GetImportJobParams{OwnerID: owner, ID: id})
	if err != nil {
		return ImportJob{}, notFound(err, "job "+id.String())
	}
	return toJob(r), nil
}

// ListImportJobs renvoie les jobs du propriétaire, le plus récent d'abord.
func (s *Store) ListImportJobs(ctx context.Context, owner uuid.UUID, limit int) ([]ImportJob, error) {
	if limit <= 0 || limit > 500 {
		limit = 50
	}
	rows, err := s.read().ListImportJobs(ctx, storedb.ListImportJobsParams{OwnerID: owner, MaxRows: int32(limit)})
	if err != nil {
		return nil, fmt.Errorf("lister les jobs d'import: %w", err)
	}
	out := make([]ImportJob, 0, len(rows))
	for _, r := range rows {
		out = append(out, toJob(r))
	}
	return out, nil
}
