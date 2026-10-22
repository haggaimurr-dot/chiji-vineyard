package main

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"encoding/json"
	"strings"
	"testing"
)

func TestOrderBatchAllocationValidation(t *testing.T) {
	o := Order{Items: []OrderItem{{CartItem: CartItem{ProductID: "p1", SKUID: "s1", Quantity: 3}}, {CartItem: CartItem{ProductID: "p2", SKUID: "s2", Quantity: 1}}}}
	a := OrderBatch{SKUID: "s1", BatchID: "b1", Quantity: 2, Batch: ProductBatch{ID: "b1", ProductID: "p1"}}
	b := OrderBatch{SKUID: "s1", BatchID: "b2", Quantity: 1, Batch: ProductBatch{ID: "b2", ProductID: "p1"}}
	c := OrderBatch{SKUID: "s2", BatchID: "b3", Quantity: 1, Batch: ProductBatch{ID: "b3", ProductID: "p2"}}
	if err := validateOrderBatches(o, []OrderBatch{a, b, c}); err != nil {
		t.Fatal(err)
	}
	for name, items := range map[string][]OrderBatch{"empty": {}, "incomplete": {a, b}, "duplicate": {a, b, b, c}, "overallocated": {a, a, c}} {
		t.Run(name, func(t *testing.T) {
			if validateOrderBatches(o, items) == nil {
				t.Fatal("accepted invalid quantities")
			}
		})
	}
	for _, change := range []func(*OrderBatch){func(a *OrderBatch) { a.Batch.ProductID = "p2" }, func(a *OrderBatch) { a.SKUID = "unknown" }, func(a *OrderBatch) { a.Quantity = 0 }, func(a *OrderBatch) { a.Quantity = -1 }, func(a *OrderBatch) { a.Batch.ID = "other" }} {
		bad := a
		change(&bad)
		if validateOrderBatches(o, []OrderBatch{bad, b, c}) == nil {
			t.Fatal("accepted invalid allocation")
		}
	}
}

type batchFixture struct {
	*shippingFixture
	batches   map[string]ProductBatch
	failAudit bool
}
type batchConnector struct{ state *batchFixture }

func (c batchConnector) Connect(context.Context) (driver.Conn, error) { return c.state, nil }
func (c batchConnector) Driver() driver.Driver                        { return batchDriver{c.state} }

type batchDriver struct{ state *batchFixture }

func (d batchDriver) Open(string) (driver.Conn, error) { return d.state, nil }
func (d *batchFixture) QueryContext(ctx context.Context, q string, args []driver.NamedValue) (driver.Rows, error) {
	if strings.Contains(q, "FROM product_batches") {
		if strings.Contains(q, "COUNT(*)") {
			var count int64
			for _, b := range d.batches {
				if b.ProductID == args[0].Value {
					count++
				}
			}
			return singleShippingRow(count), nil
		}
		if b, ok := d.batches[args[0].Value.(string)]; ok {
			return singleShippingRow(jsonValue(b)), nil
		}
		return &shippingRows{cols: []string{"payload"}}, nil
	}
	return d.shippingFixture.QueryContext(ctx, q, args)
}
func (d *batchFixture) ExecContext(ctx context.Context, q string, args []driver.NamedValue) (driver.Result, error) {
	if strings.Contains(q, "'$.batches'") {
		if err := json.Unmarshal([]byte(args[0].Value.(string)), &d.order.Batches); err != nil {
			return nil, err
		}
		d.order.BatchVersion = int(args[1].Value.(int64))
		return driver.RowsAffected(1), nil
	}
	if d.failAudit && strings.Contains(q, "INSERT INTO audit_logs") {
		return nil, sql.ErrConnDone
	}
	return d.shippingFixture.ExecContext(ctx, q, args)
}
func TestOrderBatchPersistenceAndTrace(t *testing.T) {
	ctx := context.Background()
	state := &batchFixture{shippingFixture: readyToShipFixture(), batches: map[string]ProductBatch{
		"b1": {ID: "b1", ProductID: "p1", Number: "FIRST", Orchard: "原果园", Status: "ACTIVE", Note: "内部资料"},
		"b2": {ID: "b2", ProductID: "p1", Number: "SECOND", Orchard: "另一果园", Status: "ACTIVE"},
	}}
	state.order.Items = []OrderItem{{CartItem: CartItem{ProductID: "p1", SKUID: "s1", Quantity: 2}}}
	db := sql.OpenDB(batchConnector{state})
	defer db.Close()
	db.SetMaxOpenConns(1)
	s := &Server{store: &Store{db: db}}
	checkShip := func() error {
		tx, err := db.BeginTx(ctx, nil)
		if err != nil {
			return err
		}
		defer tx.Rollback()
		return requireOrderBatches(ctx, tx, state.order)
	}
	if checkShip() == nil {
		t.Fatal("unallocated managed product can ship")
	}
	raw := json.RawMessage(`{"orderId":"o1","expectedVersion":0,"items":[{"skuId":"s1","batchId":"b1","quantity":1,"batch":{"orchard":"forged"}},{"skuId":"s1","batchId":"b2","quantity":1}]}`)
	state.failAudit = true
	if _, err := s.saveOrderBatches(ctx, Claims{}, raw); err == nil || state.order.BatchVersion != 0 || len(state.order.Batches) != 0 {
		t.Fatal("audit failure did not roll back", err)
	}
	state.failAudit = false
	if _, err := s.saveOrderBatches(ctx, Claims{}, raw); err != nil {
		t.Fatal(err)
	}
	if state.order.BatchVersion != 1 || len(state.order.Batches) != 2 || state.order.Batches[0].Batch.Orchard != "原果园" || state.order.Batches[0].Batch.Code != "b1" {
		t.Fatal("snapshot/version/code not saved correctly")
	}
	if _, err := s.saveOrderBatches(ctx, Claims{}, raw); err == nil {
		t.Fatal("stale allocation overwrote history")
	}
	if err := checkShip(); err != nil {
		t.Fatal(err)
	}
	b := state.batches["b1"]
	b.ExternalTrace = &ExternalTrace{Platform: "验收平台", URL: "https://trace.example.com/b1", Code: "EXT-001"}
	b.Orchard = "新果园"
	b.Status = "ARCHIVED"
	state.batches["b1"] = b
	if checkShip() == nil {
		t.Fatal("archived batch can ship")
	}
	if state.order.Batches[0].Batch.Orchard != "原果园" {
		t.Fatal("batch edit rewrote order history")
	}
	result, err := s.queryTraceCode(ctx, json.RawMessage(`{"code":" b1 "}`))
	if err != nil {
		t.Fatal(err)
	}
	trace := result.(M)
	if trace["externalTrace"] == nil || state.order.Batches[0].Batch.ExternalTrace != nil {
		t.Fatal("external trace missing or order snapshot rewritten")
	}
	if trace["status"] != "ARCHIVED" || trace["orchard"] != "新果园" || trace["note"] != nil || trace["orderId"] != nil {
		t.Fatal("public trace leaked internal fields or lost archived history", trace)
	}
	if _, err = s.queryTraceCode(ctx, json.RawMessage(`{"code":"unknown"}`)); err == nil {
		t.Fatal("unknown trace accepted")
	}
	b.Status = "ACTIVE"
	b.ExpiresAt = "2020-01-01"
	state.batches["b1"] = b
	if checkShip() == nil {
		t.Fatal("expired batch can ship")
	}
	state.order.FulfillmentStatus = "SHIPPED"
	if _, err = s.saveOrderBatches(ctx, Claims{}, json.RawMessage(strings.ReplaceAll(string(raw), `"expectedVersion":0`, `"expectedVersion":1`))); err == nil {
		t.Fatal("shipped order batches edited")
	}
}
