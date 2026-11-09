package httpapi

import (
	"context"
	"fmt"
	"strings"
	"time"

	openapi_types "github.com/oapi-codegen/runtime/types"

	"github.com/omeroland/finance-privee/backend/internal/api/gen"
	corestore "github.com/omeroland/finance-privee/backend/internal/core/store"
	"github.com/omeroland/finance-privee/backend/internal/platform/httpx"
)

func accountDTO(a corestore.Account) gen.Account {
	return gen.Account{
		Id: a.ID, Name: a.Name, Type: gen.AccountType(a.Type), Broker: a.Broker,
		Currency: gen.Currency(strings.TrimSpace(a.Currency)), CreatedAt: a.CreatedAt,
	}
}

// ListAccounts : comptes du propriétaire.
func (h *Handler) ListAccounts(ctx context.Context, _ gen.ListAccountsRequestObject) (gen.ListAccountsResponseObject, error) {
	ctx, owner, err := h.begin(ctx)
	if err != nil {
		return nil, err
	}
	rows, err := h.ledger.ListAccounts(ctx, owner)
	if err != nil {
		return nil, translate(fmt.Errorf("lister les comptes: %w", err))
	}
	out := gen.AccountList{Items: make([]gen.Account, 0, len(rows))}
	for _, r := range rows {
		out.Items = append(out.Items, accountDTO(r))
	}
	return gen.ListAccounts200JSONResponse(out), nil
}

// CreateAccount : crée un compte (409 si le nom est pris).
func (h *Handler) CreateAccount(ctx context.Context, req gen.CreateAccountRequestObject) (gen.CreateAccountResponseObject, error) {
	ctx, owner, err := h.begin(ctx)
	if err != nil {
		return nil, err
	}
	if req.Body == nil {
		return nil, httpx.BadRequest("Corps de requête requis.")
	}
	b := *req.Body
	var fe fieldErrors
	fe.text("name", b.Name, 1, 100)
	fe.text("broker", b.Broker, 1, 100)
	if !b.Type.Valid() {
		fe.add("type", "Le type doit être celi, reer, celiapp, reee ou taxable.")
	}
	fe.currency("currency", b.Currency)
	if err := fe.err(); err != nil {
		return nil, err
	}
	acc, err := h.ledger.CreateAccount(ctx, owner, corestore.AccountInput{
		Name: strings.TrimSpace(b.Name), Type: string(b.Type), Broker: strings.TrimSpace(b.Broker), Currency: string(b.Currency),
	})
	if err != nil {
		return nil, translate(fmt.Errorf("créer le compte: %w", err))
	}
	return gen.CreateAccount201JSONResponse(accountDTO(acc)), nil
}

func dec(d interface{ String() string }) gen.Decimal { return d.String() }

// CreateStatement : relevé mensuel d'un compte (409 si le mois existe).
func (h *Handler) CreateStatement(ctx context.Context, req gen.CreateStatementRequestObject) (gen.CreateStatementResponseObject, error) {
	ctx, owner, err := h.begin(ctx)
	if err != nil {
		return nil, err
	}
	if req.Body == nil {
		return nil, httpx.BadRequest("Corps de requête requis.")
	}
	b := *req.Body
	var fe fieldErrors
	if !monthRe.MatchString(b.Month) {
		fe.add("month", "Le mois doit suivre le format AAAA-MM.")
	}
	fe.currency("currency", b.Currency)
	end := fe.amount("endValue", &b.EndValue, false)
	contribution := b.Contribution
	neg := false
	if contribution != nil && strings.HasPrefix(*contribution, "-") {
		neg = true
		trimmed := strings.TrimPrefix(*contribution, "-")
		contribution = &trimmed
	}
	contrib := fe.amount("contribution", contribution, false)
	if err := fe.err(); err != nil {
		return nil, err
	}
	if neg && contrib != nil {
		n := contrib.Neg()
		contrib = &n
	}
	st, err := h.ledger.CreateStatement(ctx, owner, corestore.StatementInput{
		AccountID: req.Id, Month: b.Month, EndValue: *end, Contribution: contrib, Currency: string(b.Currency),
	})
	if err != nil {
		return nil, translate(fmt.Errorf("créer le relevé: %w", err))
	}
	out := gen.Statement{
		Id: st.ID, AccountId: st.AccountID, Month: st.Month, EndValue: dec(st.EndValue), Currency: b.Currency,
	}
	if st.Contribution.Valid {
		c := dec(st.Contribution.Decimal)
		out.Contribution = &c
	}
	return gen.CreateStatement201JSONResponse(out), nil
}

func dateOf(t time.Time) openapi_types.Date { return openapi_types.Date{Time: t} }
