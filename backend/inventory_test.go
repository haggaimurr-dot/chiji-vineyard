package main

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"
)

type inventoryReadDB struct {
	queries   []string
	arguments [][]driver.NamedValue
}
type inventoryReadConnector struct{ state *inventoryReadDB }
type inventoryReadDriver struct{ state *inventoryReadDB }

func (c inventoryReadConnector) Connect(context.Context) (driver.Conn, error) { return c.state, nil }
func (c inventoryReadConnector) Driver() driver.Driver                        { return inventoryReadDriver{c.state} }
func (d inventoryReadDriver) Open(string) (driver.Conn, error)                { return d.state, nil }
func (d *inventoryReadDB) Prepare(string) (driver.Stmt, error) {
	return nil, errors.New("unexpected prepare")
}
func (d *inventoryReadDB) Close() error { return nil }
func (d *inventoryReadDB) Begin() (driver.Tx, error) {
	return nil, errors.New("unexpected transaction")
}
func (d *inventoryReadDB) QueryContext(_ context.Context, q string, args []driver.NamedValue) (driver.Rows, error) {
	d.queries = append(d.queries, q)
	d.arguments = append(d.arguments, args)
	if strings.HasPrefix(q, "SELECT s.id,s.product_id") {
		return &shippingRows{cols: []string{"sku_id", "product_id", "product_name", "sku_name", "status", "stock", "reserved", "threshold", "enabled"}, values: [][]driver.Value{
			{"low", "p", "葡萄", "家庭装", "PUBLISHED", int64(10), int64(9), int64(10), true},
			{"zero", "p", "葡萄", "礼盒", "PUBLISHED", int64(0), int64(0), int64(0), true},
			{"enough", "p", "葡萄", "分享装", "PUBLISHED", int64(11), int64(0), int64(10), true},
			{"off", "p", "葡萄", "已关闭", "PUBLISHED", int64(0), int64(0), int64(10), false},
			{"draft", "p", "葡萄", "草稿", "DRAFT", int64(0), int64(0), int64(10), true},
		}}, nil
	}
	if strings.HasPrefix(q, "SELECT COUNT(*) FROM inventory_movements") {
		return singleShippingRow(int64(1)), nil
	}
	if strings.HasPrefix(q, "SELECT id,product_id,product_name") {
		return singleShippingRow("inv", "p", "历史商品名称", "s", "历史规格名称", "ADJUST", "", "运营", "盘点", int64(20), int64(10), int64(2), int64(2), int64(1), int64(1), time.Date(2026, 10, 4, 16, 0, 0, 0, time.UTC)), nil
	}
	return nil, errors.New("unexpected inventory query: " + q)
}

func TestInventoryAlertsUseAvailableStockAndPublication(t *testing.T) {
	state := &inventoryReadDB{}
	db := sql.OpenDB(inventoryReadConnector{state})
	defer db.Close()
	s := Server{store: &Store{db: db}}
	result, err := s.inventoryAlerts(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	items := result.(M)["items"].([]M)
	for i, want := range []bool{true, true, false, false, false} {
		if items[i]["alert"] != want {
			t.Fatalf("row %d: %+v", i, items[i])
		}
	}
	if !strings.Contains(state.queries[0], "s.active=1") || !strings.Contains(state.queries[0], "COALESCE(a.threshold,10)") {
		t.Fatal("missing inactive exclusion or default threshold")
	}
}

func TestInventoryMovementFiltersAndHistoricalSnapshot(t *testing.T) {
	state := &inventoryReadDB{}
	db := sql.OpenDB(inventoryReadConnector{state})
	defer db.Close()
	s := Server{store: &Store{db: db}}
	result, err := s.inventoryMovements(context.Background(), json.RawMessage(`{"skuId":"s' OR 1=1","kind":"ADJUST","startDate":"2026-10-05","endDate":"2026-10-05","limit":1,"offset":0}`))
	if err != nil {
		t.Fatal(err)
	}
	page := result.(M)
	items := page["items"].([]InventoryMovement)
	if len(items) != 1 || items[0].ProductName != "历史商品名称" || items[0].BeforeReserved != items[0].AfterReserved {
		t.Fatal(items)
	}
	if strings.Contains(state.queries[0], "s' OR") || state.arguments[0][2].Value != "s' OR 1=1" {
		t.Fatal("filters must use placeholders")
	}
	if state.arguments[0][0].Value.(time.Time) != time.Date(2026, 10, 4, 16, 0, 0, 0, time.UTC) || state.arguments[0][1].Value.(time.Time) != time.Date(2026, 10, 5, 16, 0, 0, 0, time.UTC) {
		t.Fatal("incorrect Beijing date boundaries")
	}
	for _, raw := range []string{`{"startDate":"2026-02-30"}`, `{"startDate":"2026-10-05","endDate":"2026-10-04"}`, `{"offset":-1}`} {
		if _, err = s.inventoryMovements(context.Background(), json.RawMessage(raw)); err == nil {
			t.Fatal("invalid filter accepted", raw)
		}
	}
}

func TestInventoryAlertRejectsInvalidSettings(t *testing.T) {
	s := Server{}
	for _, raw := range []string{`{}`, `{"skuId":"s","threshold":-1,"enabled":true}`, `{"skuId":"s","threshold":2147483648,"enabled":true}`, `{"skuId":"s","threshold":1.5,"enabled":true}`, `{"skuId":"s","threshold":10}`} {
		if _, err := s.saveInventoryAlert(context.Background(), Claims{}, json.RawMessage(raw)); err == nil {
			t.Fatal("invalid threshold accepted", raw)
		}
	}
}

func TestInventoryNoOpDoesNotAppendMovement(t *testing.T) {
	state := &inventoryDB{stock: map[string]int64{"a": 10}}
	db := sql.OpenDB(inventoryConnector{state})
	defer db.Close()
	s := Server{store: &Store{db: db}}
	_, err := s.batchInventory(context.Background(), Claims{Subject: "admin", Username: "运营"}, json.RawMessage(`{"reason":"确认盘点","items":[{"skuId":"a","stock":10,"expectedStock":10}]}`))
	if err != nil {
		t.Fatal(err)
	}
	if state.movements != 0 || state.stock["a"] != 10 || state.audits != 1 {
		t.Fatalf("unexpected no-op effects: %+v", state)
	}
}
