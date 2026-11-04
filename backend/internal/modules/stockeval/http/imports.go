package httpapi

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/omeroland/finance-privee/backend/internal/api/gen"
	"github.com/omeroland/finance-privee/backend/internal/modules/stockeval/ingest"
	"github.com/omeroland/finance-privee/backend/internal/modules/stockeval/store"
	"github.com/omeroland/finance-privee/backend/internal/platform/httpx"
)

const (
	importTimeout = 5 * time.Minute
	finishTimeout = 30 * time.Second
)

// jobReport est le compte rendu rangé dans store.ImportJob.Report : celui de ingest/ et,
// si le job échoue, le message d'erreur destiné à l'utilisateur.
type jobReport struct {
	ingest.Report
	Error string `json:"error,omitempty"`
}

// ImportXlsx : reçoit le classeur, crée un job (202) et importe en arrière-plan.
func (h *Handler) ImportXlsx(ctx context.Context, req gen.ImportXlsxRequestObject) (gen.ImportXlsxResponseObject, error) {
	var data []byte
	year, haveFile, haveYear := 0, false, false
	var fe fieldErrors
	for {
		part, err := req.Body.NextPart()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return nil, multipartError(err)
		}
		switch part.FormName() {
		case "file":
			data, err = io.ReadAll(io.LimitReader(part, ingest.MaxFileSize+1))
			if err != nil {
				return nil, multipartError(err)
			}
			if len(data) > ingest.MaxFileSize {
				return nil, httpx.PayloadTooLarge("Le fichier dépasse 10 Mo.")
			}
			haveFile = true
		case "year1":
			b, err := io.ReadAll(io.LimitReader(part, 16))
			if err != nil {
				return nil, multipartError(err)
			}
			year, err = strconv.Atoi(strings.TrimSpace(string(b)))
			if err != nil || year < 1980 || year > 2100 {
				fe.add("year1", "L'année de « Year 1 » doit être comprise entre 1980 et 2100.")
			}
			haveYear = true
		}
	}
	if !haveFile || len(data) == 0 {
		fe.add("file", "Le fichier xlsx est requis.")
	}
	if !haveYear {
		fe.add("year1", "L'année de « Year 1 » est requise.")
	}
	if err := fe.err(); err != nil {
		return nil, err
	}

	o := owner(ctx)
	job, err := h.repos.CreateImportJob(ctx, o, store.JobTypeXLSX)
	if err != nil {
		return nil, fmt.Errorf("créer le job d'import: %w", err)
	}
	h.jobs.Add(1)
	// Le traitement survit à la requête mais garde l'acteur et le propriétaire du contexte.
	go h.runImport(context.WithoutCancel(ctx), o, job.ID, data, year)
	return gen.ImportXlsx202JSONResponse(h.jobDTO(job)), nil
}

func multipartError(err error) error {
	var mbe *http.MaxBytesError
	if errors.As(err, &mbe) {
		return err // devient un 413
	}
	return httpx.BadRequest("Corps multipart illisible.")
}

// runImport exécute l'import et consigne l'issue du job.
func (h *Handler) runImport(base context.Context, o, jobID uuid.UUID, data []byte, year int) {
	defer h.jobs.Done()
	ctx, cancel := context.WithTimeout(base, importTimeout)
	defer cancel()

	var rep ingest.Report
	var runErr error
	func() {
		defer func() {
			if v := recover(); v != nil {
				runErr = fmt.Errorf("panique pendant l'import: %v", v)
			}
		}()
		if _, err := h.repos.FinishImportJob(ctx, o, jobID, store.JobRunning, nil, nil); err != nil {
			runErr = fmt.Errorf("passer le job en cours: %w", err)
			return
		}
		rep, runErr = h.importer.Import(ctx, o, bytes.NewReader(data), ingest.Options{Year1: year, Now: h.now})
	}()

	jr := jobReport{Report: rep}
	status := store.JobSucceeded
	if runErr != nil {
		status = store.JobFailed
		jr.Error = userMessage(runErr)
		h.log.ErrorContext(base, "import xlsx en échec", "job", jobID.String(), "erreur", runErr.Error())
	}
	h.obs.ImportFinished(string(status))
	raw, err := json.Marshal(jr)
	if err != nil {
		raw = json.RawMessage(`{}`)
	}
	fctx, fcancel := context.WithTimeout(context.WithoutCancel(base), finishTimeout)
	defer fcancel()
	now := h.now().UTC()
	if _, err := h.repos.FinishImportJob(fctx, o, jobID, status, raw, &now); err != nil {
		h.log.ErrorContext(base, "consigner l'issue du job", "job", jobID.String(), "erreur", err.Error())
	}
}

// userMessage ne montre à l'utilisateur que les erreurs de saisie de l'import.
func userMessage(err error) string {
	switch {
	case errors.Is(err, ingest.ErrInvalidWorkbook), errors.Is(err, ingest.ErrInvalidOptions), errors.Is(err, ingest.ErrTooLarge):
		return err.Error()
	}
	return "L'import a échoué ; consulte le journal du serveur."
}

// GetImport : état et compte rendu d'un job.
func (h *Handler) GetImport(ctx context.Context, req gen.GetImportRequestObject) (gen.GetImportResponseObject, error) {
	job, err := h.repos.GetImportJob(ctx, owner(ctx), req.Id)
	if err != nil {
		return nil, err
	}
	return gen.GetImport200JSONResponse(h.jobDTO(job)), nil
}

// RefreshStock : le rafraîchissement par fournisseur arrive en phase 3 (D32).
func (h *Handler) RefreshStock(ctx context.Context, req gen.RefreshStockRequestObject) (gen.RefreshStockResponseObject, error) {
	if _, err := h.find(ctx, req.Symbol); err != nil {
		return nil, err
	}
	return nil, httpx.NotImplemented("Le rafraîchissement par fournisseur arrive en phase 3.")
}

func (h *Handler) jobDTO(j store.ImportJob) gen.Job {
	dto := gen.Job{Id: j.ID, Type: gen.JobType(j.Type), CreatedAt: j.CreatedAt.UTC(), Status: jobStatus(j.Status)}
	if j.FinishedAt != nil {
		dto.FinishedAt = ptr(j.FinishedAt.UTC())
	}
	if dto.Status != gen.JobStatusSucceeded && dto.Status != gen.JobStatusFailed {
		return dto
	}
	var jr jobReport
	if err := json.Unmarshal(j.Report, &jr); err != nil {
		h.log.Warn("compte rendu de job illisible", "job", j.ID.String())
	}
	entries := make([]gen.JobEntry, 0, len(jr.Entries)+1)
	for _, e := range jr.Entries {
		je := gen.JobEntry{Level: gen.JobEntryLevel(e.Level), Message: e.Message}
		if k := gen.JobEntryKind(e.Kind); k.Valid() {
			je.Kind = &k
		}
		if e.Line > 0 {
			je.Line = ptr(e.Line)
		}
		if e.Symbol != "" {
			je.Symbol = ptr(e.Symbol)
		}
		entries = append(entries, je)
	}
	if jr.Error != "" {
		entries = append(entries, gen.JobEntry{Level: gen.Error, Message: jr.Error})
	}
	dto.Entries = &entries
	dto.StocksImported = ptr(jr.Imported)
	return dto
}

func jobStatus(s string) gen.JobStatus {
	if s == store.JobPending {
		return gen.JobStatusQueued
	}
	return gen.JobStatus(s)
}
