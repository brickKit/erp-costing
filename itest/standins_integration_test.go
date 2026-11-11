//go:build integration

package itest

// The stand-ins of the integration test: mdm/org (legal entities), mdm/currency (CNY and USD) and erp/inventory's
// StockService.Movements (the valuation sequence read back, VS03), each speaking its published contract over mTLS.

import (
	"context"
	"encoding/json"
	"sort"
	"sync"

	inventoryv3 "github.com/brickKit/erp-inventory/contract/v3/gen/go/be/erp/inventory/v3"
	currencyv3 "github.com/brickKit/mdm-currency/contract/v3/gen/go/be/mdm/currency/v3"
	orgv3 "github.com/brickKit/mdm-org/contract/v3/gen/go/be/mdm/org/v3"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// --- mdm/org: legal entities ---

type orgStandIn struct {
	orgv3.UnimplementedSourceServiceServer
	mu  sync.Mutex
	les map[string]map[string]any
}

var orgStamp = &orgv3.Stamp{Instance: "org-it", Epoch: "1.1"}

func (o *orgStandIn) Read(_ context.Context, in *orgv3.ReadRequest) (*orgv3.ReadResponse, error) {
	o.mu.Lock()
	defer o.mu.Unlock()
	out := &orgv3.ReadResponse{Stamp: orgStamp}
	for _, id := range in.GetIds() {
		r := &orgv3.ReadResult{Id: id, State: orgv3.ReadState_READ_STATE_PURGED}
		if v, ok := o.les[id]; ok && in.GetAggregate() == orgv3.Aggregate_AGGREGATE_LEGAL_ENTITY {
			data, _ := json.Marshal(v)
			r = &orgv3.ReadResult{Id: id, State: orgv3.ReadState_READ_STATE_EXISTS, Version: v["version"].(int64), Data: data}
		}
		out.Results = append(out.Results, r)
	}
	return out, nil
}

// --- mdm/currency: CNY and USD ---

type currencyStandIn struct {
	currencyv3.UnimplementedSourceServiceServer
}

func (currencyStandIn) Read(_ context.Context, in *currencyv3.ReadRequest) (*currencyv3.ReadResponse, error) {
	out := &currencyv3.ReadResponse{Stamp: &currencyv3.Stamp{}}
	for _, code := range in.GetIds() {
		r := &currencyv3.ReadResult{Id: code, State: currencyv3.ReadState_READ_STATE_PURGED}
		if code == "CNY" || code == "USD" {
			data, _ := json.Marshal(map[string]any{"code": code, "minor_unit": 2, "active": true})
			r = &currencyv3.ReadResult{Id: code, State: currencyv3.ReadState_READ_STATE_EXISTS, Version: 1, Data: data}
		}
		out.Results = append(out.Results, r)
	}
	return out, nil
}

// rateStandIn answers USD to CNY at 7.2 on any date; any other pair has no rate.
type rateStandIn struct {
	currencyv3.UnimplementedRateServiceServer
}

func (rateStandIn) EffectiveRate(_ context.Context, in *currencyv3.EffectiveRateRequest) (*currencyv3.EffectiveRateResponse, error) {
	if in.GetFromCurrency() != "USD" || in.GetToCurrency() != "CNY" {
		return nil, status.Error(codes.NotFound, "no rate")
	}
	return &currencyv3.EffectiveRateResponse{Rate: "7.2", RateDate: in.GetDate(), RateType: in.GetRateType()}, nil
}

// --- erp/inventory: the numbered movements, read back by valuation sequence ---

// inventoryStandIn keeps every movement the test numbers (published or held back) by legal entity, product and
// epoch; Movements answers them from a sequence number on, as erp/inventory 3.0.39 does. reads counts the calls.
type inventoryStandIn struct {
	inventoryv3.UnimplementedStockServiceServer
	mu    sync.Mutex
	moves map[string][]map[string]any // "<legal entity>/<product>/<epoch>" → movements by sequence
	reads int
}

func newInventoryStandIn() *inventoryStandIn {
	return &inventoryStandIn{moves: map[string][]map[string]any{}}
}

func (s *inventoryStandIn) key(le, product, epoch string) string {
	return le + "/" + product + "/" + epoch
}

// add keeps a numbered movement.
func (s *inventoryStandIn) add(m map[string]any) {
	s.mu.Lock()
	defer s.mu.Unlock()
	seq := m["valuation_seq"].(map[string]any)
	k := s.key(m["legal_entity_id"].(string), m["product_id"].(string), seq["epoch"].(string))
	s.moves[k] = append(s.moves[k], m)
	sort.Slice(s.moves[k], func(i, j int) bool { return seqOf(s.moves[k][i]) < seqOf(s.moves[k][j]) })
}

func seqOf(m map[string]any) int64 {
	switch n := m["valuation_seq"].(map[string]any)["seq"].(type) {
	case int64:
		return n
	case int:
		return int64(n)
	case float64:
		return int64(n)
	}
	return 0
}

func (s *inventoryStandIn) Movements(_ context.Context, in *inventoryv3.MovementsRequest) (*inventoryv3.MovementsResponse, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.reads++
	out := &inventoryv3.MovementsResponse{}
	all := s.moves[s.key(in.GetLegalEntityId(), in.GetProductId(), in.GetValuationEpoch())]
	limit := int(in.GetLimit())
	if limit <= 0 {
		limit = 200
	}
	for _, m := range all {
		out.LastSeq = max(out.LastSeq, seqOf(m))
		if seqOf(m) >= in.GetFromSeq() && len(out.Movements) < limit {
			b, _ := json.Marshal(m)
			out.Movements = append(out.Movements, b)
		}
	}
	return out, nil
}

// count is how many times Movements was called.
func (s *inventoryStandIn) count() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.reads
}
