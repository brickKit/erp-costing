package masterdata

import (
	"context"
	"encoding/json"

	core "github.com/brickKit/erp-costing/v3/internal/core"

	"github.com/brickKit/be-sdk-go/v3/calls"
	"github.com/brickKit/be-sdk-go/v3/errs"
	inventoryv3 "github.com/brickKit/erp-inventory/contract/v3/gen/go/be/erp/inventory/v3"
	currencyv3 "github.com/brickKit/mdm-currency/contract/v3/gen/go/be/mdm/currency/v3"
	orgv3 "github.com/brickKit/mdm-org/contract/v3/gen/go/be/mdm/org/v3"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// Every call here runs outside a transaction (protocol P4.20): evidence first, then the transaction. Unreachable is
// never "does not exist" (protocol 07): it answers DEPENDENCY_UNAVAILABLE.

func Conn(m *core.Base, id string) (*grpc.ClientConn, error) {
	c, ok := m.RT.GRPC(id)
	if !ok {
		return nil, errs.New(errs.ReasonDependencyUnavailable, id+" is not reachable")
	}
	return c, nil
}

// LegalEntity is what costing needs of a legal entity: its base currency is the currency of every value.
type LegalEntity struct {
	ID           string `json:"id"`
	Code         string `json:"code"`
	State        string `json:"state"`
	Timezone     string `json:"timezone"`
	BaseCurrency string `json:"base_currency"`
}

// LegalEntityOf reads a legal entity from mdm/org's source method (any state: movements of a legal entity closed
// since are still valued).
func LegalEntityOf(m *core.Base, ctx context.Context, id string) (LegalEntity, error) {
	c, err := Conn(m, core.Org)
	if err != nil {
		return LegalEntity{}, err
	}
	res, err := orgv3.NewSourceServiceClient(c).Read(ctx, &orgv3.ReadRequest{
		Aggregate: orgv3.Aggregate_AGGREGATE_LEGAL_ENTITY, Ids: []string{id}}, calls.Idempotent())
	if err != nil {
		return LegalEntity{}, err
	}
	var le LegalEntity
	if len(res.GetResults()) != 1 || res.GetResults()[0].GetState() != orgv3.ReadState_READ_STATE_EXISTS {
		return le, core.ErrLegalEntity.New("no legal entity " + id)
	}
	return le, json.Unmarshal(res.GetResults()[0].GetData(), &le)
}

// CurrencyScale is a currency's minor unit (the places an amount is rounded to).
func CurrencyScale(m *core.Base, ctx context.Context, code string) (int32, error) {
	c, err := Conn(m, core.Currency)
	if err != nil {
		return 0, err
	}
	res, err := currencyv3.NewSourceServiceClient(c).Read(ctx, &currencyv3.ReadRequest{
		Aggregate: currencyv3.Aggregate_AGGREGATE_CURRENCY, Ids: []string{code}}, calls.Idempotent())
	if err != nil {
		return 0, err
	}
	if len(res.GetResults()) != 1 || res.GetResults()[0].GetState() != currencyv3.ReadState_READ_STATE_EXISTS {
		return 0, errs.New(errs.ReasonNotFound, "no currency "+code)
	}
	var cur struct {
		MinorUnit int32 `json:"minor_unit"`
	}
	return cur.MinorUnit, json.Unmarshal(res.GetResults()[0].GetData(), &cur)
}

// Movements reads a valuation sequence back from erp/inventory (StockService.Movements, VS03, VS04): the movements of
// the legal entity and product numbered from from on in epoch, at most core.MaxRefetch, and the highest number
// erp/inventory has given in that epoch.
func Movements(m *core.Base, ctx context.Context, le, product, epoch string, from int64) ([][]byte, int64, error) {
	c, err := Conn(m, core.Inventory)
	if err != nil {
		return nil, 0, err
	}
	res, err := inventoryv3.NewStockServiceClient(c).Movements(ctx, &inventoryv3.MovementsRequest{LegalEntityId: le,
		ProductId: product, ValuationEpoch: epoch, FromSeq: from, Limit: core.MaxRefetch}, calls.Idempotent())
	if err != nil {
		return nil, 0, err
	}
	return res.GetMovements(), res.GetLastSeq(), nil
}

// Rate is the rate a source's price was converted at (LY02, LY03).
type Rate struct {
	Rate string `json:"rate"`
	Date string `json:"date"`
	Type string `json:"type"`
}

// RateOf is the spot rate from a currency to the base currency on the date (mdm/currency's RateService); nil when
// they are the same currency. No rate answers CURRENCY_NOT_USABLE.
func RateOf(m *core.Base, ctx context.Context, from, to, date string) (*Rate, error) {
	if from == to {
		return nil, nil
	}
	c, err := Conn(m, core.Currency)
	if err != nil {
		return nil, err
	}
	res, err := currencyv3.NewRateServiceClient(c).EffectiveRate(ctx, &currencyv3.EffectiveRateRequest{RateType: "spot",
		Date: date, FromCurrency: from, ToCurrency: to}, calls.Idempotent())
	if status.Code(err) == codes.NotFound {
		return nil, core.ErrCurrency.New("no spot rate " + from + "/" + to + " on " + date)
	}
	if err != nil {
		return nil, err
	}
	return &Rate{Rate: res.GetRate(), Date: res.GetRateDate(), Type: res.GetRateType()}, nil
}
