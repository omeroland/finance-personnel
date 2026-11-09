package httpapi

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"regexp"
	"strings"

	"github.com/google/uuid"

	"github.com/omeroland/finance-privee/backend/internal/api/gen"
	"github.com/omeroland/finance-privee/backend/internal/modules/etf/ingest"
	"github.com/omeroland/finance-privee/backend/internal/platform/httpx"
)

// Limites de l'import CSV (10 Mo de fichier, plus l'enveloppe multipart).
const (
	MaxCSVSize      = 10 << 20
	CSVBodyLimit    = MaxCSVSize + 64<<10
	genericBroker   = "generic" // D315 : seul adapter avant la phase 6
	csvTooLargeText = "Le fichier dépasse 10 Mo."
)

// CSVPath reconnaît le chemin de la route d'import (son id varie : la limite
// de corps ne peut pas être une entrée exacte de httpx.BodyLimit).
var CSVPath = regexp.MustCompile(`^/api/v1/accounts/[^/]+/imports/csv$`)

// scopedRepo restreint l'import au compte de la route : une ligne d'un autre
// compte est refusée sans toucher au registre (D420).
type scopedRepo struct {
	inner   ingest.TransactionRepository
	account string
}

func (s scopedRepo) SaveTransactions(ctx context.Context, owner uuid.UUID, txs []ingest.Transaction) ([]ingest.Outcome, error) {
	out := make([]ingest.Outcome, len(txs))
	var keep []ingest.Transaction
	var idx []int
	for i, t := range txs {
		if !strings.EqualFold(t.Account, s.account) {
			out[i] = ingest.Outcome{Status: ingest.OutcomeRejected, Message: "le compte de la ligne n'est pas celui de la route"}
			continue
		}
		keep = append(keep, t)
		idx = append(idx, i)
	}
	if len(keep) == 0 {
		return out, nil
	}
	res, err := s.inner.SaveTransactions(ctx, owner, keep)
	if err != nil {
		return nil, err
	}
	if len(res) != len(keep) {
		return nil, ingest.ErrBadOutcome
	}
	for j, i := range idx {
		out[i] = res[j]
	}
	return out, nil
}

// ImportCsv : import synchrone d'un CSV vers le registre d'un compte (D420).
func (h *Handler) ImportCsv(ctx context.Context, req gen.ImportCsvRequestObject) (gen.ImportCsvResponseObject, error) {
	ctx, owner, err := h.begin(ctx)
	if err != nil {
		return nil, err
	}
	if h.imports == nil {
		return nil, httpx.NotImplemented("L'import CSV n'est pas configuré.")
	}
	if req.Params.Broker != genericBroker {
		return nil, httpx.BadRequest("Adapter de courtier inconnu.", httpx.FieldError{Field: "broker", Message: "Seul l'adapter « generic » est disponible."})
	}
	accts, err := h.ledger.ListAccounts(ctx, owner)
	if err != nil {
		return nil, translate(fmt.Errorf("lister les comptes: %w", err))
	}
	name := ""
	for _, a := range accts {
		if a.ID == req.Id {
			name = a.Name
		}
	}
	if name == "" {
		return nil, httpx.NotFound("Compte introuvable.")
	}

	var data []byte
	found := false
	for req.Body != nil {
		part, err := req.Body.NextPart()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return nil, multipartError(err)
		}
		if part.FormName() != "file" {
			continue
		}
		data, err = io.ReadAll(io.LimitReader(part, MaxCSVSize+1))
		if err != nil {
			return nil, multipartError(err)
		}
		if len(data) > MaxCSVSize {
			return nil, httpx.PayloadTooLarge(csvTooLargeText)
		}
		found = true
	}
	if !found {
		return nil, httpx.BadRequest("Le fichier CSV est requis.", httpx.FieldError{Field: "file", Message: "Champ « file » absent."})
	}

	imp := ingest.NewTxImporter(scopedRepo{inner: h.imports, account: name})
	_, rep, err := imp.ImportCSV(ctx, owner, strings.NewReader(string(data)), ingest.CSVOptions{})
	switch {
	case errors.Is(err, ingest.ErrEmptyCSV):
		return nil, httpx.BadRequest("Le fichier CSV est vide.", httpx.FieldError{Field: "file", Message: "Fichier vide."})
	case errors.Is(err, ingest.ErrCSVHeader):
		return nil, httpx.BadRequest("En-têtes CSV invalides.", httpx.FieldError{Field: "file", Message: "Les en-têtes date, compte et type sont obligatoires."})
	case err != nil:
		return nil, translate(fmt.Errorf("importer le CSV: %w", err))
	}
	return gen.ImportCsv200JSONResponse(reportDTO(rep)), nil
}

func multipartError(err error) error {
	var mbe *http.MaxBytesError
	if errors.As(err, &mbe) {
		return err // devient un 413
	}
	return httpx.BadRequest("Corps multipart illisible.")
}

func reportDTO(r ingest.CSVReport) gen.CsvImportReport {
	out := gen.CsvImportReport{
		RowsRead: r.Rows, Valid: r.Valid, Invalid: r.Invalid, Created: r.Created,
		Duplicates: r.Duplicates, Rejected: r.Rejected, Warnings: r.Warnings,
		Entries: make([]gen.CsvImportEntry, 0, len(r.Entries)),
	}
	for _, e := range r.Entries {
		d := gen.CsvImportEntry{Line: e.Line, Level: e.Level, Message: e.Message}
		if e.Kind != "" {
			k := e.Kind
			d.Kind = &k
		}
		if e.Column != "" {
			c := e.Column
			d.Column = &c
		}
		out.Entries = append(out.Entries, d)
	}
	return out
}
