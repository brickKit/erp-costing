//go:build integration

package itest

// The costing component against PostgreSQL and the bus over its real surfaces (L2). mdm/org, mdm/currency and
// erp/inventory's StockService are stand-ins that speak their published contracts over mTLS
// (standins_integration_test.go); erp/inventory's movements, erp/purchase's valuation bases and erp/finance's price
// confirmations are published by the test on their streams, as those components' forwarders would. Needs
// BE_SDK_TEST_PG_ADMIN and BE_SDK_TEST_NATS_URL, as be-sdk-go's integration tests. The member listens on 28229 (user)
// and 9229 (system); the stand-ins on 19140-19142.
// Run: go test -tags integration ./itest/ (make test-integration); the index of the steps is itest/AGENTS.md.
//
// Each step is in the file of its business flow; TestCostingEndToEnd runs them in order.

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/http"
	"net/url"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	costing "github.com/brickKit/erp-costing/v3/costing"
	core "github.com/brickKit/erp-costing/v3/internal/core"

	"github.com/brickKit/be-sdk-go/v3/component"
	"github.com/brickKit/be-sdk-go/v3/config"
	"github.com/brickKit/be-sdk-go/v3/events"
	"github.com/brickKit/be-sdk-go/v3/events/bus"
	"github.com/brickKit/be-sdk-go/v3/events/jetstream"
	"github.com/brickKit/be-sdk-go/v3/ids"
	"github.com/brickKit/be-sdk-go/v3/launcher"
	"github.com/brickKit/be-sdk-go/v3/runtime"
	"github.com/brickKit/be-sdk-go/v3/sectest"
	inventoryv3 "github.com/brickKit/erp-inventory/contract/v3/gen/go/be/erp/inventory/v3"
	currencyv3 "github.com/brickKit/mdm-currency/contract/v3/gen/go/be/mdm/currency/v3"
	orgv3 "github.com/brickKit/mdm-org/contract/v3/gen/go/be/mdm/org/v3"
	"github.com/jackc/pgx/v5"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials"
)

const (
	itPort      = "28229"
	itBase      = "http://localhost:" + itPort
	itSystem    = "localhost:9229"
	itOrg       = "localhost:19140"
	itCurrency  = "localhost:19141"
	itInventory = "localhost:19142"
	itSchema    = "erp_costing_it"
)

// fixture is the running member, its stand-ins and the world's IDs, shared by the steps.
type fixture struct {
	t     *testing.T
	ctx   context.Context
	w     *sectest.World
	admin *pgx.Conn
	alice client
	tr    events.Transport
	rt    *runtime.Runtime // the member's runtime

	// two legal entities: Shanghai (CNY, moving average) and Beijing (CNY, FIFO once a step sets it)
	le, le2   string
	warehouse string // erp/inventory's warehouse every movement names (the costing scope ignores it)
	supplier  string // the supplier of erp/finance's price confirmations
	inventory *inventoryStandIn
}

func TestCostingEndToEnd(t *testing.T) {
	fx := start(t)
	fx.notInThisVersion()
	fx.policies()
	fx.sequence()
	fx.unknownKind()
	fx.epochs()
	fx.sources()
	fx.fifo()
	fx.returns()
	fx.policyInUse()
}

// sourceStreams are the streams the member subscribes to; the test publishes on them as their owners would.
var sourceStreams = []events.Stream{{Name: core.InventoryStream, Partitions: 4}, {Name: core.PurchaseStream, Partitions: 4},
	{Name: core.FinanceStream, Partitions: 4}}

// start makes the world, the stand-ins and the source streams, then migrates and runs the member.
func start(t *testing.T) *fixture {
	adminURL, natsURL := os.Getenv("BE_SDK_TEST_PG_ADMIN"), os.Getenv("BE_SDK_TEST_NATS_URL")
	if adminURL == "" || natsURL == "" {
		t.Fatal("BE_SDK_TEST_PG_ADMIN and BE_SDK_TEST_NATS_URL are needed")
	}
	ctx := context.Background()
	admin, err := pgx.Connect(ctx, adminURL)
	require.NoError(t, err)
	t.Cleanup(func() { admin.Close(ctx) })
	_, err = admin.Exec(ctx, "DROP SCHEMA IF EXISTS "+itSchema+" CASCADE; CREATE SCHEMA "+itSchema)
	require.NoError(t, err)
	u, err := url.Parse(adminURL)
	require.NoError(t, err)
	pw, _ := u.User.Password()
	w := sectest.New(t)
	w.GrantAll(sectest.Alice, core.Definitions())
	fx := &fixture{t: t, ctx: ctx, w: w, admin: admin, le: ids.New().String(), le2: ids.New().String(),
		warehouse: ids.New().String(), supplier: ids.New().String()}
	org := &orgStandIn{les: map[string]map[string]any{
		fx.le: {"id": fx.le, "code": "SH", "state": "active", "timezone": "Asia/Shanghai", "base_currency": "CNY", "version": int64(1)},
		fx.le2: {"id": fx.le2, "code": "BJ", "state": "active", "timezone": "Asia/Shanghai", "base_currency": "CNY",
			"version": int64(1)}}}
	portFree(t, itPort, itSystem, itOrg, itCurrency, itInventory)
	serve(t, itOrg, w, core.Org, func(s *grpc.Server) { orgv3.RegisterSourceServiceServer(s, org) })
	serve(t, itCurrency, w, core.Currency, func(s *grpc.Server) {
		currencyv3.RegisterSourceServiceServer(s, currencyStandIn{})
		currencyv3.RegisterRateServiceServer(s, rateStandIn{})
	})
	fx.inventory = newInventoryStandIn()
	serve(t, itInventory, w, core.Inventory, func(s *grpc.Server) { inventoryv3.RegisterStockServiceServer(s, fx.inventory) })

	// clean streams each run (earlier runs and cells leave events on the shared NATS)
	js, err := jetstream.Connect(natsURL, "costing-it-clean")
	require.NoError(t, err)
	for _, s := range append(sourceStreams, events.Stream{Name: core.Stream}) {
		_ = js.DeleteStream(ctx, s.Name) // not there: nothing to clean
	}
	_ = js.Close()
	fx.tr, err = bus.Open(ctx, natsURL, "costing-it-sources", slog.Default())
	require.NoError(t, err)
	t.Cleanup(func() { fx.tr.Close() })
	for _, s := range sourceStreams {
		require.NoError(t, fx.tr.EnsureStream(ctx, s))
	}

	env := w.Env(t, core.ID)
	for k, v := range map[string]string{"COMPONENT_ID": core.ID, "COMPONENT_VERSION": costing.Version, "PORT": itPort,
		"STOP_DRAIN_DELAY": "50ms", "PG_HOST": u.Hostname(), "PG_PORT": u.Port(), "PG_DATABASE": strings.TrimPrefix(u.Path, "/"),
		"PG_USER": u.User.Username(), "PG_PASSWORD": pw, "PG_SCHEMA": itSchema, "PG_OWNER_USER": u.User.Username(),
		"PG_OWNER_PASSWORD": pw, "BUS_URL": natsURL, "IMPLEMENTATION_INSTANCE": ids.New().String(),
		"MDM_ORG_GRPC_ENDPOINT": "http://" + itOrg, "MDM_CURRENCY_GRPC_ENDPOINT": "http://" + itCurrency,
		"ERP_INVENTORY_GRPC_ENDPOINT": "http://" + itInventory,
		// the waits short, so a step sees a gap read and a policy price within seconds
		core.KeyGapWait: "2s", core.KeyBasisWait: "2s"} {
		env[k] = v
	}
	var logs bytes.Buffer
	var logMu sync.Mutex
	t.Cleanup(func() {
		if t.Failed() {
			logMu.Lock()
			t.Log(logs.String())
			logMu.Unlock()
		}
	})
	out := &lockedWriter{w: &logs, mu: &logMu}
	opts := launcher.Options{Stdout: out, Stderr: out,
		Env: config.Env{LookupEnv: func(k string) (string, bool) { v, ok := env[k]; return v, ok }, Read: os.ReadFile}}
	migrate := opts
	migrate.Args = []string{"migrate"}
	def := costing.Definition()
	newModule := def.New
	def.New = func(rt *runtime.Runtime) (component.Module, error) { fx.rt = rt; return newModule(rt) }
	require.Equal(t, launcher.ExitOK, launcher.Run(ctx, def, migrate))
	sctx, stop := context.WithCancel(ctx)
	done := make(chan int, 1)
	go func() { done <- launcher.Run(sctx, def, opts) }()
	t.Cleanup(func() { stop(); <-done })
	require.Eventually(t, func() bool {
		r, err := http.Get(itBase + "/readyz")
		if err == nil {
			r.Body.Close()
		}
		return err == nil && r.StatusCode == 200
	}, 20*time.Second, 100*time.Millisecond)
	fx.alice = client{t, w.Bearer(sectest.Alice)}
	return fx
}

// sourceEpoch is the epoch every source stream's events carry (the stand-ins' databases never change it).
var sourceEpoch = events.Epoch{Generation: 1, Timeline: 1}

// publish publishes an incremental fact on a source's stream, as that component's forwarder would.
func (fx *fixture) publish(stream events.Stream, source, typ, id string, data any) {
	fx.t.Helper()
	payload, err := json.Marshal(data)
	require.NoError(fx.t, err)
	row := events.Row{EventID: ids.New().String(), Stream: stream.Name, Partition: stream.PartitionOf(id), Type: typ,
		OccurredAt: time.Now(), RecordID: id, Version: 1, Projection: events.Full, Payload: payload,
		Seq: time.Now().UnixNano(), Epoch: sourceEpoch}
	row.Digest = events.Digest(row.EventID, payload)
	require.NoError(fx.t, fx.tr.Publish(fx.ctx, events.Message{Subject: events.Subject(stream.Name, row.Partition, row.Type),
		Headers: row.Headers(source, source+"-it"), Data: payload}))
}

// fact reads the payload of the incremental fact with the ID from the outbox.
func (fx *fixture) fact(id, typ string) map[string]any {
	fx.t.Helper()
	var payload []byte
	require.NoError(fx.t, fx.admin.QueryRow(fx.ctx, "SELECT payload FROM "+itSchema+".be_outbox WHERE type = $1 AND "+
		"convert_from(payload, 'UTF8')::jsonb->>'fact_id' = $2", typ, id).Scan(&payload), "%s %s", typ, id)
	var out map[string]any
	require.NoError(fx.t, json.Unmarshal(payload, &out))
	return out
}

// --- helpers ---

type client struct {
	t     *testing.T
	token string
}

// do calls the user surface; a POST or PUT carries a new Idempotency-Key unless key gives one.
func (c client) do(method, path string, body any, key ...string) (int, map[string]any) {
	c.t.Helper()
	var r io.Reader
	if body != nil {
		b, _ := json.Marshal(body)
		r = bytes.NewReader(b)
	}
	req, _ := http.NewRequest(method, itBase+path, r)
	req.Header.Set("Authorization", c.token)
	req.Header.Set("Content-Type", "application/json")
	if method == "POST" || method == "PUT" {
		k := ids.New().String()
		if len(key) > 0 {
			k = key[0]
		}
		req.Header.Set("Idempotency-Key", k)
	}
	resp, err := http.DefaultClient.Do(req)
	require.NoError(c.t, err)
	defer resp.Body.Close()
	var out map[string]any
	raw, _ := io.ReadAll(resp.Body)
	_ = json.Unmarshal(raw, &out)
	return resp.StatusCode, out
}

func serve(t *testing.T, addr string, w *sectest.World, as string, register func(*grpc.Server)) {
	ln, err := net.Listen("tcp", addr)
	require.NoError(t, err)
	srv := grpc.NewServer(grpc.Creds(credentials.NewTLS(w.TLS(t, as).ServerConfig())))
	register(srv)
	go func() { _ = srv.Serve(ln) }()
	t.Cleanup(srv.Stop)
}

// at reads a value along a path of keys and indexes from a decoded JSON object.
func at(v any, path ...any) any {
	for _, p := range path {
		switch k := p.(type) {
		case string:
			m, _ := v.(map[string]any)
			v = m[k]
		case int:
			a, _ := v.([]any)
			if k >= len(a) {
				return nil
			}
			v = a[k]
		}
	}
	return v
}

func (fx *fixture) get(path string) map[string]any {
	fx.t.Helper()
	code, out := fx.alice.do("GET", path, nil)
	require.Equal(fx.t, 200, code, out)
	return out
}

type lockedWriter struct {
	w  io.Writer
	mu *sync.Mutex
}

func (l *lockedWriter) Write(p []byte) (int, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.w.Write(p)
}

// portFree fails at once when a port the member needs is taken: a running cell or another member would answer in
// its place, and the test would assert against another process.
func portFree(t *testing.T, addrs ...string) {
	for _, a := range addrs {
		if !strings.Contains(a, ":") {
			a = "localhost:" + a
		}
		ln, err := net.Listen("tcp", a)
		require.NoError(t, err, fmt.Sprintf("port %s is taken: is a cell or another member running?", a))
		_ = ln.Close()
	}
}
