package main

import (
	"context"
	"encoding/json"
	"sync"
	"sync/atomic"
	"testing"
)

func TestInventoryLotValidation(t *testing.T) {
	n := 1
	good := LotChange{InventoryLot: InventoryLot{SKUID: "s", WarehouseID: "w", BatchID: "b", Quantity: 1}, ExpectedStock: &n, Reference: "PO-1", Reason: "采购"}
	if err := validateLotChange(good); err != nil {
		t.Fatal(err)
	}
	for _, edit := range []func(*LotChange){func(d *LotChange) { d.Quantity = 0 }, func(d *LotChange) { d.Quantity = -1 }, func(d *LotChange) { d.Quantity = 2147483648 }, func(d *LotChange) { d.ExpectedStock = nil }, func(d *LotChange) { d.Version = -1 }, func(d *LotChange) { d.BatchID = "" }, func(d *LotChange) { d.Reason = " " }, func(d *LotChange) { d.Reference = "" }} {
		d := good
		edit(&d)
		if validateLotChange(d) == nil {
			t.Fatalf("invalid accepted: %+v", d)
		}
	}
}

// Called by the opt-in isolated MySQL acceptance harness after schema creation.
func inventoryLotsMySQLAcceptance(t *testing.T, ctx context.Context, s *Server, actor Claims) {
	t.Helper()
	db := s.store.db
	raw := func(v any) json.RawMessage { return json.RawMessage(jsonValue(v)) }
	p := Product{ID: "lot-acceptance-product", Name: "分仓验收葡萄", Category: "family", SaleMode: "spot", Cover: "https://example.com/grape.jpg", Status: "PUBLISHED", SKUs: []SKU{{ID: "lot-acceptance-sku", Name: "家庭装", Price: 100, Stock: 20, LimitPerOrder: 100, Active: true}}}
	if _, err := s.saveProduct(ctx, actor, raw(M{"product": p})); err != nil {
		t.Fatal(err)
	}
	wv, err := s.saveWarehouse(ctx, actor, raw(Warehouse{Name: "验收主仓", Active: true}))
	if err != nil {
		t.Fatal(err)
	}
	w := wv.(Warehouse)
	wv, err = s.saveWarehouse(ctx, actor, raw(Warehouse{Name: "验收冷库", Active: true}))
	if err != nil {
		t.Fatal(err)
	}
	w2 := wv.(Warehouse)
	bv, err := s.saveProductBatch(ctx, actor, raw(M{"batch": ProductBatch{ProductID: p.ID, Number: "分仓批次", Orchard: "验收果园", Status: "ACTIVE"}}))
	if err != nil {
		t.Fatal(err)
	}
	b := bv.(ProductBatch)
	stock := 20
	d := LotChange{InventoryLot: InventoryLot{SKUID: p.SKUs[0].ID, WarehouseID: w.ID, BatchID: b.ID, Quantity: 20}, ExpectedStock: &stock, Reference: "OPEN-1", Reason: "期初盘点"}
	if _, err = s.changeInventoryLot(ctx, actor, "admin.inventory.allocate", raw(d)); err != nil {
		t.Fatal(err)
	}
	check := func(wantStock, wantReserved, wantLots int) {
		t.Helper()
		var actual, reserved, total int
		if err := db.QueryRow("SELECT stock,reserved FROM skus WHERE id=?", d.SKUID).Scan(&actual, &reserved); err != nil {
			t.Fatal(err)
		}
		if err := db.QueryRow("SELECT COALESCE(SUM(quantity),0) FROM inventory_lots WHERE sku_id=?", d.SKUID).Scan(&total); err != nil {
			t.Fatal(err)
		}
		if actual != wantStock || reserved != wantReserved || total != wantLots {
			t.Fatalf("stock/reserved/lots=%d/%d/%d want %d/%d/%d", actual, reserved, total, wantStock, wantReserved, wantLots)
		}
	}
	check(20, 0, 20)
	d.Quantity = 5
	d.Version = 1
	d.Reference = "PO-1"
	if _, err = s.changeInventoryLot(ctx, actor, "admin.inventory.receive", raw(d)); err != nil {
		t.Fatal(err)
	}
	check(25, 0, 25)
	stock = 25
	d.Version = 2
	if _, err = s.changeInventoryLot(ctx, actor, "admin.inventory.receive", raw(d)); err == nil {
		t.Fatal("duplicate document accepted")
	}
	check(25, 0, 25)
	d.Reference = "LOSS-1"
	d.Quantity = 3
	if _, err = s.changeInventoryLot(ctx, actor, "admin.inventory.loss", raw(d)); err != nil {
		t.Fatal(err)
	}
	check(22, 0, 22)
	stock = 22
	d.Version = 0
	d.WarehouseID = w2.ID
	d.Quantity = 4
	d.Reference = "PO-2"
	if _, err = s.changeInventoryLot(ctx, actor, "admin.inventory.receive", raw(d)); err != nil {
		t.Fatal(err)
	}
	check(26, 0, 26)
	// Concurrent writes to one SKU are serialized and exactly one stale version wins.
	stock = 26
	d.Version = 1
	d.Quantity = 1
	d.Reference = "LOSS-RACE"
	var successes atomic.Int32
	var wg sync.WaitGroup
	for i := 0; i < 12; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if _, e := s.changeInventoryLot(ctx, actor, "admin.inventory.loss", raw(d)); e == nil {
				successes.Add(1)
			}
		}()
	}
	wg.Wait()
	if successes.Load() != 1 {
		t.Fatalf("race successes=%d", successes.Load())
	}
	check(25, 0, 25)
	input := CreateOrderInput{IdempotencyKey: "lot-acceptance-order", Address: Address{Name: "验收", Phone: "13800138000", Province: "北京市", City: "北京市", District: "朝阳区", Detail: "测试路"}, Items: []CartItem{{SKUID: d.SKUID, Quantity: 2}}}
	out, err := s.createOrder(ctx, "lot-user", raw(input))
	if err != nil {
		t.Fatal(err)
	}
	o := out.(Order)
	check(23, 2, 25)
	if err = s.markPaid(ctx, o.ID, "lot-payment"); err != nil {
		t.Fatal(err)
	}
	check(23, 0, 23)
	// Lot ledger failures must roll back the aggregate SKU and manual receipt too.
	if _, err = db.Exec("CREATE TRIGGER reject_lot_ledger BEFORE INSERT ON inventory_lot_movements FOR EACH ROW SIGNAL SQLSTATE '45000' SET MESSAGE_TEXT='lot ledger unavailable'"); err != nil {
		t.Fatal(err)
	}
	stock = 23
	var version int
	if err = db.QueryRow("SELECT version FROM inventory_lots WHERE sku_id=? AND warehouse_id=? AND batch_id=?", d.SKUID, d.WarehouseID, d.BatchID).Scan(&version); err != nil {
		t.Fatal(err)
	}
	d.Version = version
	d.Reference = "PO-ROLLBACK"
	d.Quantity = 2
	if _, err = s.changeInventoryLot(ctx, actor, "admin.inventory.receive", raw(d)); err == nil {
		t.Fatal("ledger failure accepted")
	}
	check(23, 0, 23)
	if _, err = db.Exec("DROP TRIGGER reject_lot_ledger"); err != nil {
		t.Fatal(err)
	}
	if _, err = s.changeInventoryLot(ctx, actor, "admin.inventory.receive", raw(d)); err != nil {
		t.Fatal(err)
	}
	check(25, 0, 25)
	// Physical reduction by ordinary stock adjustment reconciles both warehouses.
	if _, err = s.batchInventory(ctx, actor, raw(M{"reason": "分仓盘点", "items": []M{{"skuId": d.SKUID, "stock": 1, "expectedStock": 25}}})); err != nil {
		t.Fatal(err)
	}
	check(1, 0, 1)
	w.Active = false
	if _, err = s.saveWarehouse(ctx, actor, raw(w)); err != nil {
		t.Fatal(err)
	}
	stock = 1
	d.WarehouseID = w.ID
	d.Reference = "PO-DISABLED"
	if _, err = s.changeInventoryLot(ctx, actor, "admin.inventory.receive", raw(d)); err == nil {
		t.Fatal("disabled warehouse accepted")
	}
	t.Log("MySQL inventory lots: allocation, two warehouses, purchase/loss, duplicate documents, concurrent versions, order reserve/payment, ledger rollback/retry, adjustment conservation, disabled warehouse passed")
}
