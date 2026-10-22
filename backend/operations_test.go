package main

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"encoding/json"
	"errors"
	"io"
	"strings"
	"testing"
)

func TestHomeSectionsValidation(t *testing.T) {
	h := defaultHome()
	h.Activities = []HomeActivity{{Title: "活动", Summary: "简介", Content: "规则", StartDate: "2026-10-01", EndDate: "2026-10-31"}}
	h.Announcements = []HomeAnnouncement{{Title: "公告", Content: "全文", PublishedAt: "2026-10-01"}}
	h.LaunchCalendar = []HomeLaunch{{Variety: "葡萄", StartDate: "2026-11-01", EndDate: "2027-02-28"}}
	if err := validateHome(h); err != nil {
		t.Fatal(err)
	}
	h.Activities[0].EndDate = "2026-09-30"
	if validateHome(h) == nil {
		t.Fatal("reversed activity dates accepted")
	}
	h.Activities = nil
	h.Announcements[0].PublishedAt = "2026-02-30"
	if validateHome(h) == nil {
		t.Fatal("invalid announcement date accepted")
	}
	h.Announcements = nil
	h.LaunchCalendar[0].EndDate = "2033-02-28"
	if validateHome(h) == nil {
		t.Fatal("unbounded season accepted")
	}
	if err := validateHome(defaultHome()); err != nil {
		t.Fatalf("legacy config rejected: %v", err)
	}
}

// Transactional test driver: exercises the service's commit/rollback boundaries
// without requiring an external database in local verification.
type inventoryDB struct {
	stock            map[string]int64
	working          map[string]int64
	audits           int
	pendingAudits    int
	auditFailure     bool
	begun            bool
	locks            []string
	movements        int
	pendingMovements int
	ledgerFailure    bool
}
type inventoryConnector struct{ state *inventoryDB }

func (c inventoryConnector) Connect(context.Context) (driver.Conn, error) { return c.state, nil }
func (c inventoryConnector) Driver() driver.Driver                        { return inventoryDriver{c.state} }

type inventoryDriver struct{ state *inventoryDB }

func (d inventoryDriver) Open(string) (driver.Conn, error) { return d.state, nil }
func (d *inventoryDB) Prepare(string) (driver.Stmt, error) {
	return nil, errors.New("unexpected prepare")
}
func (d *inventoryDB) Close() error { return nil }
func (d *inventoryDB) Begin() (driver.Tx, error) {
	d.begun = true
	d.working = map[string]int64{}
	for id, n := range d.stock {
		d.working[id] = n
	}
	d.pendingAudits = 0
	d.pendingMovements = 0
	return d, nil
}
func (d *inventoryDB) Commit() error {
	d.stock = d.working
	d.audits += d.pendingAudits
	d.movements += d.pendingMovements
	return nil
}
func (d *inventoryDB) Rollback() error {
	d.working = nil
	d.pendingAudits = 0
	d.pendingMovements = 0
	return nil
}
func (d *inventoryDB) QueryContext(_ context.Context, q string, args []driver.NamedValue) (driver.Rows, error) {
	if strings.Contains(q, "FROM inventory_lots") {
		return &shippingRows{cols: []string{"sku_id", "warehouse_id", "batch_id", "quantity", "version"}}, nil
	}
	if strings.HasPrefix(q, "SELECT stock,reserved,sold") {
		return singleShippingRow(d.working[args[0].Value.(string)], int64(0), int64(0)), nil
	}
	if strings.HasPrefix(q, "SELECT s.product_id") {
		return singleShippingRow("p", "葡萄", "家庭装", d.working[args[0].Value.(string)], int64(0), int64(0)), nil
	}
	if !strings.Contains(q, "FOR UPDATE") {
		return nil, errors.New("inventory must lock rows")
	}
	id := args[0].Value.(string)
	d.locks = append(d.locks, id)
	n, ok := d.working[id]
	return &stockRow{stock: n, exists: ok}, nil
}
func (d *inventoryDB) ExecContext(_ context.Context, q string, args []driver.NamedValue) (driver.Result, error) {
	switch {
	case strings.HasPrefix(q, "INSERT INTO inventory_movements"):
		if d.ledgerFailure {
			return nil, errors.New("ledger unavailable")
		}
		d.pendingMovements++
	case strings.HasPrefix(q, "UPDATE skus"):
		d.working[args[1].Value.(string)] = args[0].Value.(int64)
	case strings.HasPrefix(q, "INSERT INTO audit_logs"):
		if d.auditFailure {
			return nil, errors.New("audit unavailable")
		}
		d.pendingAudits++
	default:
		return nil, errors.New("unexpected query")
	}
	return driver.RowsAffected(1), nil
}

type stockRow struct {
	stock  int64
	exists bool
	read   bool
}

func (r *stockRow) Columns() []string { return []string{"stock"} }
func (r *stockRow) Close() error      { return nil }
func (r *stockRow) Next(out []driver.Value) error {
	if r.read || !r.exists {
		return io.EOF
	}
	r.read = true
	out[0] = r.stock
	return nil
}
func TestInventoryAtomicity(t *testing.T) {
	cases := []struct {
		name          string
		expected      int
		missing       bool
		auditFailure  bool
		wantError     bool
		ledgerFailure bool
	}{
		{name: "commit all", expected: 20}, {name: "conflict rolls back first update", expected: 19, wantError: true}, {name: "missing SKU rolls back first update", expected: 20, missing: true, wantError: true}, {name: "audit failure rolls back stock", expected: 20, auditFailure: true, wantError: true},
		{name: "ledger failure rolls back stock", expected: 20, ledgerFailure: true, wantError: true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			state := &inventoryDB{stock: map[string]int64{"a": 10, "b": 20}, auditFailure: tc.auditFailure, ledgerFailure: tc.ledgerFailure}
			if tc.missing {
				delete(state.stock, "b")
			}
			db := sql.OpenDB(inventoryConnector{state})
			defer db.Close()
			db.SetMaxOpenConns(1)
			server := Server{store: &Store{db: db}}
			raw := json.RawMessage(jsonValue(M{"reason": "盘点", "items": []M{{"skuId": "b", "stock": 25, "expectedStock": tc.expected}, {"skuId": "a", "stock": 0, "expectedStock": 10}}}))
			_, err := server.batchInventory(context.Background(), Claims{Subject: "admin", Username: "test"}, raw)
			if (err != nil) != tc.wantError {
				t.Fatalf("error: %v", err)
			}
			if !tc.ledgerFailure && strings.Join(state.locks, ",") != "a,b" {
				t.Fatalf("locks must have stable order: %v", state.locks)
			}
			if tc.wantError {
				if state.stock["a"] != 10 || state.audits != 0 || state.movements != 0 {
					t.Fatalf("partial changes escaped rollback: %+v", state)
				}
			} else if state.stock["a"] != 0 || state.stock["b"] != 25 || state.audits != 1 || state.movements != 2 {
				t.Fatalf("stock and audit not committed: %+v", state)
			}
		})
	}
}
func TestInventoryRejectsInvalidBatch(t *testing.T) {
	n := 1
	for _, items := range [][]InventoryChange{nil, {{SKUID: "a", Stock: -1, ExpectedStock: &n}}, {{SKUID: "a", Stock: 1}}, {{SKUID: "a", Stock: 1, ExpectedStock: &n}, {SKUID: "a", Stock: 2, ExpectedStock: &n}}} {
		if validateInventory(items) == nil {
			t.Fatalf("accepted invalid batch %+v", items)
		}
	}
}
func TestPrivacyPublicationRequiresIdentity(t *testing.T) {
	var p PrivacyPolicy
	if err := json.Unmarshal(privacyDefault, &p); err != nil {
		t.Fatal(err)
	}
	if err := validatePrivacy(p); err != nil {
		t.Fatal(err)
	}
	p.Published = true
	if validatePrivacy(p) == nil {
		t.Fatal("published unidentified operator")
	}
	p.Operator = "公司"
	p.Contact = "privacy@example.com"
	p.Address = "联系地址"
	if err := validatePrivacy(p); err != nil {
		t.Fatal(err)
	}
	p.Content[0] = " "
	if validatePrivacy(p) == nil {
		t.Fatal("published empty section")
	}
}
func TestHomeRejectsUnsafeOrDuplicateTargets(t *testing.T) {
	h := defaultHome()
	if err := validateHome(h); err != nil {
		t.Fatal(err)
	}
	h.Banners[0].Target = "https://external.example"
	if validateHome(h) == nil {
		t.Fatal("accepted arbitrary target")
	}
	h = defaultHome()
	h.ProductIDs = []string{"a", "a"}
	if validateHome(h) == nil {
		t.Fatal("accepted duplicate recommendations")
	}
	h = defaultHome()
	h.Banners[0].Target = "product"
	if validateHome(h) == nil {
		t.Fatal("accepted missing product ID")
	}
}
