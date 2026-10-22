package main

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/go-sql-driver/mysql"
)

// Opt-in: creates and removes its own database, never uses a business database.
func TestInventoryMySQLAcceptance(t *testing.T) {
	dsn := os.Getenv("MYSQL_TEST_DSN")
	if dsn == "" {
		t.Skip("set MYSQL_TEST_DSN to run real MySQL acceptance")
	}
	cfg, e := mysql.ParseDSN(dsn)
	if e != nil {
		t.Fatal(e)
	}
	cfg.DBName = ""
	cfg.ParseTime = true
	cfg.MultiStatements = true
	cfg.Loc = time.UTC
	root, e := sql.Open("mysql", cfg.FormatDSN())
	if e != nil {
		t.Fatal(e)
	}
	defer root.Close()
	name := fmt.Sprintf("chiji_inventory_test_%d", time.Now().UnixNano())
	if _, e = root.Exec("CREATE DATABASE `" + name + "` CHARACTER SET utf8mb4"); e != nil {
		t.Fatal(e)
	}
	defer func() {
		if _, e := root.Exec("DROP DATABASE `" + name + "`"); e != nil {
			t.Error(e)
		}
	}()
	cfg.DBName = name
	db, e := sql.Open("mysql", cfg.FormatDSN())
	if e != nil {
		t.Fatal(e)
	}
	defer db.Close()
	db.SetMaxOpenConns(40)
	schema, e := os.ReadFile("schema.sql")
	if e != nil {
		t.Fatal(e)
	}
	if _, e = db.Exec(string(schema)); e != nil {
		t.Fatal(e)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()
	s := Server{store: &Store{db: db}}
	actor := Claims{Subject: "qa", Username: "并发验收"}
	p := Product{ID: "acceptance-product", Name: "验收葡萄", Category: "family", SaleMode: "spot", Cover: "https://example.com/grape.jpg", Status: "PUBLISHED", SKUs: []SKU{{ID: "a-acceptance-sku", Name: "家庭装", Price: 100, Stock: 17, LimitPerOrder: 100, Active: true}}}
	raw := func(v any) json.RawMessage { return json.RawMessage(jsonValue(v)) }
	if _, e = s.saveProduct(ctx, actor, raw(M{"product": p})); e != nil {
		t.Fatal(e)
	}
	parallel := func(n int, fn func(int)) {
		var wg sync.WaitGroup
		gate := make(chan struct{})
		for i := 0; i < n; i++ {
			wg.Add(1)
			go func(i int) { defer wg.Done(); <-gate; fn(i) }(i)
		}
		close(gate)
		wg.Wait()
	}
	counts := func() (int, int, int) {
		var a, b, c int
		if e := db.QueryRow("SELECT stock,reserved,sold FROM skus WHERE id=?", p.SKUs[0].ID).Scan(&a, &b, &c); e != nil {
			t.Fatal(e)
		}
		return a, b, c
	}
	assertCounts := func(a, b, c int) {
		t.Helper()
		x, y, z := counts()
		if x != a || y != b || z != c {
			t.Fatalf("stock/reserved/sold=%d/%d/%d want %d/%d/%d", x, y, z, a, b, c)
		}
	}
	orders := make(chan Order, 60)
	var insufficient atomic.Int32
	parallel(60, func(i int) {
		input := CreateOrderInput{IdempotencyKey: fmt.Sprintf("acceptance-order-%03d", i), Address: Address{Name: "验收", Phone: "13800138000", Province: "北京市", City: "北京市", District: "朝阳区", Detail: "测试路 1 号"}, Items: []CartItem{{SKUID: p.SKUs[0].ID, Quantity: 1}}}
		result, e := s.createOrder(ctx, fmt.Sprintf("qa-%d", i), raw(input))
		if e != nil {
			if strings.Contains(e.Error(), "库存") {
				insufficient.Add(1)
			} else {
				t.Errorf("unexpected create error: %v", e)
			}
			return
		}
		orders <- result.(Order)
	})
	close(orders)
	successful := []Order{}
	for o := range orders {
		successful = append(successful, o)
	}
	if len(successful) != 17 || insufficient.Load() != 43 {
		t.Fatalf("success=%d insufficient=%d", len(successful), insufficient.Load())
	}
	assertCounts(0, 17, 0)
	parallel(12, func(i int) {
		if e := s.releaseOrder(ctx, successful[0].ID, "CANCELLED"); e != nil {
			t.Error(e)
		}
	})
	assertCounts(1, 16, 0)
	parallel(12, func(i int) {
		if e := s.markPaid(ctx, successful[1].ID, "qa-transaction"); e != nil {
			t.Error(e)
		}
	})
	assertCounts(1, 15, 1)
	// Cancel and payment contend for the same order lock; exactly one terminal state wins.
	parallel(2, func(i int) {
		if i == 0 {
			if e := s.releaseOrder(ctx, successful[2].ID, "CANCELLED"); e != nil {
				t.Error(e)
			}
		} else {
			if e := s.markPaid(ctx, successful[2].ID, "qa-race"); e != nil && !strings.Contains(e.Error(), "已关闭") {
				t.Error(e)
			}
		}
	})
	a, b, c := counts()
	if b != 14 || a+c != 3 {
		t.Fatalf("payment/cancel race violated conservation: %d/%d/%d", a, b, c)
	}
	// Duplicate successful refund notifications restore an unshipped order once.
	paidOrder := successful[1]
	if _, e = db.Exec("INSERT INTO refunds(id,order_id,order_no,openid,amount,reason,status) VALUES(?,?,?,?,?,?,?)", "qa-refund", paidOrder.ID, paidOrder.OrderNo, paidOrder.OpenID, paidOrder.Quote.Total, "验收退款", "APPROVED"); e != nil {
		t.Fatal(e)
	}
	parallel(12, func(i int) {
		if e := s.recordRefundResult(ctx, "qa-refund", "SUCCESS", "qa-provider-refund"); e != nil {
			t.Error(e)
		}
	})
	assertCounts(a+1, b, c-1)
	a++
	c--
	for _, check := range []struct{ kind, ref string }{{"RELEASE", successful[0].OrderNo}, {"PAY", successful[1].OrderNo}, {"REFUND", successful[1].OrderNo}} {
		var n int
		if e = db.QueryRow("SELECT COUNT(*) FROM inventory_movements WHERE kind=? AND reference=?", check.kind, check.ref).Scan(&n); e != nil || n != 1 {
			t.Fatalf("duplicate ledger %s: %d %v", check.kind, n, e)
		}
	}
	// Concurrent admin adjustments use the same expected stock; only one can win.
	var adjustments atomic.Int32
	parallel(20, func(i int) {
		_, e := s.batchInventory(ctx, actor, raw(M{"reason": "并发盘点", "items": []M{{"skuId": p.SKUs[0].ID, "stock": 50 + i, "expectedStock": a}}}))
		if e == nil {
			adjustments.Add(1)
		} else if !strings.Contains(e.Error(), "库存已变化") {
			t.Error(e)
		}
	})
	if adjustments.Load() != 1 {
		t.Fatal("adjustment winners", adjustments.Load())
	}
	stock, reserved, sold := counts()
	if reserved != b || sold != c {
		t.Fatal("adjustments changed reserved/sold")
	}
	_, e = s.batchInventory(ctx, actor, raw(M{"reason": "整批回滚", "items": []M{{"skuId": p.SKUs[0].ID, "stock": 2, "expectedStock": stock}, {"skuId": "z-missing", "stock": 0, "expectedStock": 0}}}))
	if e == nil {
		t.Fatal("missing SKU accepted")
	}
	assertCounts(stock, reserved, sold)
	// Stale product edits cannot undo an order or stock adjustment.
	stale := p
	old := 17
	stale.SKUs[0].ExpectedStock = &old
	if _, e = s.saveProduct(ctx, actor, raw(M{"product": stale})); e == nil {
		t.Fatal("stale edit accepted")
	}
	assertCounts(stock, reserved, sold)
	// Ledger failure must roll back all products and all SKU changes in an import.
	if _, e = db.Exec("CREATE TRIGGER reject_acceptance_ledger BEFORE INSERT ON inventory_movements FOR EACH ROW BEGIN IF NEW.after_stock=777 THEN SIGNAL SQLSTATE '45000' SET MESSAGE_TEXT='acceptance ledger failure'; END IF; END"); e != nil {
		t.Fatal(e)
	}
	csv := productCSVHeader + "\nA,导入一,,family,spot,,https://example.com/a.jpg,果园,装,10,8,1,1kg\nB,导入二,,family,spot,,https://example.com/b.jpg,果园,装,10,777,1,1kg\n"
	if _, e = s.importProducts(ctx, actor, raw(M{"content": csv})); e == nil {
		t.Fatal("ledger failure import succeeded")
	}
	var n int
	if e = db.QueryRow("SELECT COUNT(*) FROM products").Scan(&n); e != nil || n != 1 {
		t.Fatal("partial import", n, e)
	}
	if _, e = db.Exec("DROP TRIGGER reject_acceptance_ledger"); e != nil {
		t.Fatal(e)
	}
	if _, e = s.importProducts(ctx, actor, raw(M{"content": strings.Replace(csv, ",777,", ",7,", 1)})); e != nil {
		t.Fatal(e)
	}
	// More than 100 products remain searchable and page-hydrated.
	tx, e := db.BeginTx(ctx, nil)
	if e != nil {
		t.Fatal(e)
	}
	for i := 0; i < 125; i++ {
		p.ID = fmt.Sprintf("bulk-%03d", i)
		p.Name = fmt.Sprintf("批量商品%03d", i)
		p.SKUs[0].ID = p.ID + "-sku"
		if _, e = s.saveProductTx(ctx, tx, actor, p); e != nil {
			tx.Rollback()
			t.Fatal(e)
		}
	}
	if e = tx.Commit(); e != nil {
		t.Fatal(e)
	}
	result, e := s.searchAdminProducts(ctx, raw(M{"keyword": "批量商品", "offset": 100, "limit": 20}))
	if e != nil {
		t.Fatal(e)
	}
	page := result.(ProductSearchResult)
	if page.Total != 125 || len(page.Items) != 20 || len(page.Items[0].SKUs) != 1 {
		t.Fatal(page)
	}
	batch := ProductBatch{ProductID: p.ID, Number: "B01", Orchard: "测试果园", Status: "ACTIVE"}
	saved, e := s.saveProductBatch(ctx, actor, raw(M{"batch": batch}))
	if e != nil {
		t.Fatal(e)
	}
	batch = saved.(ProductBatch)
	batch.Status = "ARCHIVED"
	if _, e = s.saveProductBatch(ctx, actor, raw(M{"batch": batch})); e != nil {
		t.Fatal(e)
	}
	if _, e = s.saveProductBatch(ctx, actor, raw(M{"batch": batch})); e == nil {
		t.Fatal("stale batch accepted")
	}
	t.Run("batch warehouses purchase loss", func(t *testing.T) { inventoryLotsMySQLAcceptance(t, ctx, &s, actor) })
	t.Logf("MySQL: 60 concurrent orders -> 17 successes/43 insufficient; duplicate release/payment/refund x12; payment/cancel race; adjustment race x20 -> one winner; batch rollback; stale edit; atomic import/ledger rollback; 125-product pagination; batch version protection passed")
}
