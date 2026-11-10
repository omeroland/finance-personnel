package httpapi

import (
	"context"

	"github.com/omeroland/finance-privee/backend/internal/api/gen"
	"github.com/omeroland/finance-privee/backend/internal/platform/httpx"
)

// pendingRoutes porte les routes servies par etf/http (P2-10, D401) : sans
// composition, Routes() les laisse en 501. cmd/server utilise RoutesFor avec le
// handler composé et les sert pour de vrai.
type pendingRoutes struct{ *Handler }

var _ gen.StrictServerInterface = pendingRoutes{}

// Routes au contrat sans handler ici : les modèles, plans, exposition et rendement sont servis par
// etf/http (P2-11). Sans composition, elles répondent 501 au format RFC 9457.

const phase2Pending = "Cette route du module 2 n'est pas encore disponible."

// ListAccounts : servie par etf/http (P2-10) une fois composée dans cmd/server ; 501 sinon.
func (pendingRoutes) ListAccounts(_ context.Context, _ gen.ListAccountsRequestObject) (gen.ListAccountsResponseObject, error) {
	return nil, httpx.NotImplemented(phase2Pending)
}

// CreateAccount : servie par etf/http (P2-10) une fois composée dans cmd/server ; 501 sinon.
func (pendingRoutes) CreateAccount(_ context.Context, _ gen.CreateAccountRequestObject) (gen.CreateAccountResponseObject, error) {
	return nil, httpx.NotImplemented(phase2Pending)
}

// ListTransactions : servie par etf/http (P2-10) une fois composée dans cmd/server ; 501 sinon.
func (pendingRoutes) ListTransactions(_ context.Context, _ gen.ListTransactionsRequestObject) (gen.ListTransactionsResponseObject, error) {
	return nil, httpx.NotImplemented(phase2Pending)
}

// CreateTransaction : servie par etf/http (P2-10) une fois composée dans cmd/server ; 501 sinon.
func (pendingRoutes) CreateTransaction(_ context.Context, _ gen.CreateTransactionRequestObject) (gen.CreateTransactionResponseObject, error) {
	return nil, httpx.NotImplemented(phase2Pending)
}

// CreateStatement : servie par etf/http (P2-10) une fois composée dans cmd/server ; 501 sinon.
func (pendingRoutes) CreateStatement(_ context.Context, _ gen.CreateStatementRequestObject) (gen.CreateStatementResponseObject, error) {
	return nil, httpx.NotImplemented(phase2Pending)
}

// ImportCsv : servie par etf/http (P2-10b, D420) une fois composée dans cmd/server ; 501 sinon.
func (pendingRoutes) ImportCsv(_ context.Context, _ gen.ImportCsvRequestObject) (gen.ImportCsvResponseObject, error) {
	return nil, httpx.NotImplemented(phase2Pending)
}

// ListPositions : servie par etf/http (P2-10) une fois composée dans cmd/server ; 501 sinon.
func (pendingRoutes) ListPositions(_ context.Context, _ gen.ListPositionsRequestObject) (gen.ListPositionsResponseObject, error) {
	return nil, httpx.NotImplemented(phase2Pending)
}

// ListEtfs : servie par etf/http (P2-10) une fois composée dans cmd/server ; 501 sinon.
func (pendingRoutes) ListEtfs(_ context.Context, _ gen.ListEtfsRequestObject) (gen.ListEtfsResponseObject, error) {
	return nil, httpx.NotImplemented(phase2Pending)
}

// ListModels : servie par etf/http (P2-11) une fois composée dans cmd/server ; 501 sinon.
func (pendingRoutes) ListModels(_ context.Context, _ gen.ListModelsRequestObject) (gen.ListModelsResponseObject, error) {
	return nil, httpx.NotImplemented(phase2Pending)
}

// CreateModel : servie par etf/http (P2-11) une fois composée dans cmd/server ; 501 sinon.
func (pendingRoutes) CreateModel(_ context.Context, _ gen.CreateModelRequestObject) (gen.CreateModelResponseObject, error) {
	return nil, httpx.NotImplemented(phase2Pending)
}

// UpdateModel : servie par etf/http (P2-11) une fois composée dans cmd/server ; 501 sinon.
func (pendingRoutes) UpdateModel(_ context.Context, _ gen.UpdateModelRequestObject) (gen.UpdateModelResponseObject, error) {
	return nil, httpx.NotImplemented(phase2Pending)
}

// CreatePurchasePlan : servie par etf/http (P2-11) une fois composée dans cmd/server ; 501 sinon.
func (pendingRoutes) CreatePurchasePlan(_ context.Context, _ gen.CreatePurchasePlanRequestObject) (gen.CreatePurchasePlanResponseObject, error) {
	return nil, httpx.NotImplemented(phase2Pending)
}

// CreateRebalancePlan : servie par etf/http (P2-11) une fois composée dans cmd/server ; 501 sinon.
func (pendingRoutes) CreateRebalancePlan(_ context.Context, _ gen.CreateRebalancePlanRequestObject) (gen.CreateRebalancePlanResponseObject, error) {
	return nil, httpx.NotImplemented(phase2Pending)
}

// GetExposure : servie par etf/http (P2-11) une fois composée dans cmd/server ; 501 sinon.
func (pendingRoutes) GetExposure(_ context.Context, _ gen.GetExposureRequestObject) (gen.GetExposureResponseObject, error) {
	return nil, httpx.NotImplemented(phase2Pending)
}

// GetPerformance : servie par etf/http (P2-11) une fois composée dans cmd/server ; 501 sinon.
func (pendingRoutes) GetPerformance(_ context.Context, _ gen.GetPerformanceRequestObject) (gen.GetPerformanceResponseObject, error) {
	return nil, httpx.NotImplemented(phase2Pending)
}
