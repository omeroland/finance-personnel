package httpapi

import (
	"context"

	"github.com/omeroland/finance-privee/backend/internal/api/gen"
	"github.com/omeroland/finance-privee/backend/internal/platform/httpx"
)

// Routes du socle et du module 2 (P2-01) : au contrat, pas encore
// implémentées. Les handlers arrivent avec P2-10 (comptes, registre,
// catalogue) et P2-11 (modèles, plans, exposition, rendement) ; d'ici là,
// elles répondent 501 au format RFC 9457.

const phase2Pending = "Cette route du module 2 n'est pas encore disponible."

// ListAccounts : contrat P2-01, implémentation en P2-10 ou P2-11.
func (h *Handler) ListAccounts(_ context.Context, _ gen.ListAccountsRequestObject) (gen.ListAccountsResponseObject, error) {
	return nil, httpx.NotImplemented(phase2Pending)
}

// CreateAccount : contrat P2-01, implémentation en P2-10 ou P2-11.
func (h *Handler) CreateAccount(_ context.Context, _ gen.CreateAccountRequestObject) (gen.CreateAccountResponseObject, error) {
	return nil, httpx.NotImplemented(phase2Pending)
}

// ListTransactions : contrat P2-01, implémentation en P2-10 ou P2-11.
func (h *Handler) ListTransactions(_ context.Context, _ gen.ListTransactionsRequestObject) (gen.ListTransactionsResponseObject, error) {
	return nil, httpx.NotImplemented(phase2Pending)
}

// CreateTransaction : contrat P2-01, implémentation en P2-10 ou P2-11.
func (h *Handler) CreateTransaction(_ context.Context, _ gen.CreateTransactionRequestObject) (gen.CreateTransactionResponseObject, error) {
	return nil, httpx.NotImplemented(phase2Pending)
}

// CreateStatement : contrat P2-01, implémentation en P2-10 ou P2-11.
func (h *Handler) CreateStatement(_ context.Context, _ gen.CreateStatementRequestObject) (gen.CreateStatementResponseObject, error) {
	return nil, httpx.NotImplemented(phase2Pending)
}

// ImportCsv : contrat P2-01, implémentation en P2-10 ou P2-11.
func (h *Handler) ImportCsv(_ context.Context, _ gen.ImportCsvRequestObject) (gen.ImportCsvResponseObject, error) {
	return nil, httpx.NotImplemented(phase2Pending)
}

// ListPositions : contrat P2-01, implémentation en P2-10 ou P2-11.
func (h *Handler) ListPositions(_ context.Context, _ gen.ListPositionsRequestObject) (gen.ListPositionsResponseObject, error) {
	return nil, httpx.NotImplemented(phase2Pending)
}

// ListEtfs : contrat P2-01, implémentation en P2-10 ou P2-11.
func (h *Handler) ListEtfs(_ context.Context, _ gen.ListEtfsRequestObject) (gen.ListEtfsResponseObject, error) {
	return nil, httpx.NotImplemented(phase2Pending)
}

// ListModels : contrat P2-01, implémentation en P2-10 ou P2-11.
func (h *Handler) ListModels(_ context.Context, _ gen.ListModelsRequestObject) (gen.ListModelsResponseObject, error) {
	return nil, httpx.NotImplemented(phase2Pending)
}

// CreateModel : contrat P2-01, implémentation en P2-10 ou P2-11.
func (h *Handler) CreateModel(_ context.Context, _ gen.CreateModelRequestObject) (gen.CreateModelResponseObject, error) {
	return nil, httpx.NotImplemented(phase2Pending)
}

// UpdateModel : contrat P2-01, implémentation en P2-10 ou P2-11.
func (h *Handler) UpdateModel(_ context.Context, _ gen.UpdateModelRequestObject) (gen.UpdateModelResponseObject, error) {
	return nil, httpx.NotImplemented(phase2Pending)
}

// CreatePurchasePlan : contrat P2-01, implémentation en P2-10 ou P2-11.
func (h *Handler) CreatePurchasePlan(_ context.Context, _ gen.CreatePurchasePlanRequestObject) (gen.CreatePurchasePlanResponseObject, error) {
	return nil, httpx.NotImplemented(phase2Pending)
}

// CreateRebalancePlan : contrat P2-01, implémentation en P2-10 ou P2-11.
func (h *Handler) CreateRebalancePlan(_ context.Context, _ gen.CreateRebalancePlanRequestObject) (gen.CreateRebalancePlanResponseObject, error) {
	return nil, httpx.NotImplemented(phase2Pending)
}

// GetExposure : contrat P2-01, implémentation en P2-10 ou P2-11.
func (h *Handler) GetExposure(_ context.Context, _ gen.GetExposureRequestObject) (gen.GetExposureResponseObject, error) {
	return nil, httpx.NotImplemented(phase2Pending)
}

// GetPerformance : contrat P2-01, implémentation en P2-10 ou P2-11.
func (h *Handler) GetPerformance(_ context.Context, _ gen.GetPerformanceRequestObject) (gen.GetPerformanceResponseObject, error) {
	return nil, httpx.NotImplemented(phase2Pending)
}
