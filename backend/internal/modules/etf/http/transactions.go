package httpapi

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/omeroland/finance-privee/backend/internal/api/gen"
	corestore "github.com/omeroland/finance-privee/backend/internal/core/store"
	"github.com/omeroland/finance-privee/backend/internal/platform/httpx"
)

func optDec(d interface {
	String() string
}, ok bool,
) *gen.Decimal {
	if !ok {
		return nil
	}
	s := d.String()
	return &s
}

func txDTO(t corestore.Tx) gen.Transaction {
	out := gen.Transaction{
		Id: t.ID, AccountId: t.AccountID, Type: gen.TransactionType(t.Type), TradeDate: dateOf(t.TradeDate),
		Currency: gen.Currency(strings.TrimSpace(t.Currency)), CreatedAt: t.CreatedAt,
		Quantity:   optDec(t.Quantity.Decimal, t.Quantity.Valid),
		Price:      optDec(t.Price.Decimal, t.Price.Valid),
		Amount:     optDec(t.Amount.Decimal, t.Amount.Valid),
		Fee:        optDec(t.Fee.Decimal, t.Fee.Valid),
		SplitRatio: optDec(t.SplitRatio.Decimal, t.SplitRatio.Valid),
		Note:       t.Note, CounterpartAccountId: t.CounterpartAccountID, ReversesId: t.ReversesID,
	}
	if t.Symbol != "" {
		s := t.Symbol
		out.Symbol = &s
	}
	return out
}

// ListTransactions : registre d'un compte (404 si le compte n'est pas au propriétaire).
func (h *Handler) ListTransactions(ctx context.Context, req gen.ListTransactionsRequestObject) (gen.ListTransactionsResponseObject, error) {
	ctx, owner, err := h.begin(ctx)
	if err != nil {
		return nil, err
	}
	var from, to *time.Time
	var fe fieldErrors
	if req.Params.From != nil {
		from = &req.Params.From.Time
		if !validDate(*from) {
			fe.add("from", "Date hors des bornes.")
		}
	}
	if req.Params.To != nil {
		to = &req.Params.To.Time
		if !validDate(*to) {
			fe.add("to", "Date hors des bornes.")
		}
	}
	if from != nil && to != nil && from.After(*to) {
		fe.add("from", "La date de début doit précéder la date de fin.")
	}
	if err := fe.err(); err != nil {
		return nil, err
	}
	rows, err := h.ledger.ListTransactions(ctx, owner, req.Id, from, to)
	if err != nil {
		return nil, translate(fmt.Errorf("lister le registre: %w", err))
	}
	out := gen.TransactionList{Items: make([]gen.Transaction, 0, len(rows))}
	for _, r := range rows {
		out.Items = append(out.Items, txDTO(r))
	}
	return gen.ListTransactions200JSONResponse(out), nil
}

// CreateTransaction : inscrit une écriture ; reversesId inscrit l'écriture inverse.
func (h *Handler) CreateTransaction(ctx context.Context, req gen.CreateTransactionRequestObject) (gen.CreateTransactionResponseObject, error) {
	ctx, owner, err := h.begin(ctx)
	if err != nil {
		return nil, err
	}
	if req.Body == nil {
		return nil, httpx.BadRequest("Corps de requête requis.")
	}
	in, err := parseTx(req.Id, *req.Body)
	if err != nil {
		return nil, err
	}
	t, err := h.ledger.AddTransaction(ctx, owner, in)
	if err != nil {
		return nil, translate(fmt.Errorf("inscrire la transaction: %w", err))
	}
	return gen.CreateTransaction201JSONResponse(txDTO(t)), nil
}

// parseTx valide à la frontière : mêmes champs requis que le CHECK de core.transaction (D393).
func parseTx(account gen.AccountId, b gen.TransactionCreate) (corestore.TxInput, error) {
	var fe fieldErrors
	if !b.Type.Valid() {
		fe.add("type", "Type de transaction inconnu.")
	}
	if !validDate(b.TradeDate.Time) {
		fe.add("tradeDate", "La date doit être comprise entre 1900 et 2100.")
	}
	fe.currency("currency", b.Currency)
	in := corestore.TxInput{
		AccountID: account, Type: string(b.Type), TradeDate: b.TradeDate.Time, Currency: string(b.Currency),
		CounterpartAccountID: b.CounterpartAccountId, ReversesID: b.ReversesId, Source: corestore.SourceManual,
	}
	in.Quantity = fe.amount("quantity", b.Quantity, true)
	in.Price = fe.amount("price", b.Price, false)
	in.Amount = fe.amount("amount", b.Amount, false)
	in.Fee = fe.amount("fee", b.Fee, false)
	in.SplitRatio = fe.amount("splitRatio", b.SplitRatio, true)
	if b.Symbol != nil {
		if !symbolRe.MatchString(*b.Symbol) {
			fe.add("symbol", "Le symbole doit suivre le format BOURSE:SYMBOLE.")
		}
		in.Symbol = *b.Symbol
	}
	if b.Note != nil {
		if len([]rune(*b.Note)) > 500 {
			fe.add("note", "La note compte 500 caractères au plus.")
		}
		in.Note = b.Note
	}
	need := func(field string, ok bool) {
		if !ok {
			fe.add(field, "Champ requis pour ce type de transaction.")
		}
	}
	switch b.Type {
	case gen.TransactionTypeBuy, gen.TransactionTypeSell, gen.TransactionTypeDrip:
		need("symbol", in.Symbol != "")
		need("quantity", in.Quantity != nil)
		need("price", in.Price != nil)
	case gen.TransactionTypeDividend:
		need("symbol", in.Symbol != "")
		need("amount", in.Amount != nil)
	case gen.TransactionTypeSplit:
		need("symbol", in.Symbol != "")
		need("splitRatio", in.SplitRatio != nil)
	case gen.TransactionTypeTransfer:
		need("counterpartAccountId", in.CounterpartAccountID != nil)
		need("amount", in.Amount != nil || (in.Symbol != "" && in.Quantity != nil))
		if in.CounterpartAccountID != nil && *in.CounterpartAccountID == account {
			fe.add("counterpartAccountId", "La contrepartie doit être un autre compte.")
		}
	case gen.TransactionTypeContribution, gen.TransactionTypeWithdrawal, gen.TransactionTypeFee:
		need("amount", in.Amount != nil)
	}
	if b.Type != gen.TransactionTypeTransfer && in.CounterpartAccountID != nil {
		fe.add("counterpartAccountId", "La contrepartie ne s'applique qu'au transfert.")
	}
	if err := fe.err(); err != nil {
		return corestore.TxInput{}, err
	}
	return in, nil
}
