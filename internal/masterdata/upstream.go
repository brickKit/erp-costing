package masterdata

import (
	"context"
	"encoding/json"

	core "github.com/brickKit/erp-costing/v3/internal/core"

	"github.com/brickKit/be-sdk-go/v3/calls"
	"github.com/brickKit/be-sdk-go/v3/errs"
	currencyv3 "github.com/brickKit/mdm-currency/contract/v3/gen/go/be/mdm/currency/v3"
	orgv3 "github.com/brickKit/mdm-org/contract/v3/gen/go/be/mdm/org/v3"
	"google.golang.org/grpc"
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

// Deployed says whether an optional dependency is in the assembly (erp/purchase, erp/finance).
func Deployed(m *core.Base, id string) bool {
	_, ok := m.RT.GRPC(id)
	return ok
}
