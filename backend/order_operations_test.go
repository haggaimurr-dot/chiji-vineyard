package main

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"encoding/csv"
	"encoding/json"
	"errors"
	"net/http"
	"reflect"
	"strings"
	"testing"
	"time"
)

func batchShippingRaw(items ...ShippingInput) json.RawMessage {
	return json.RawMessage(jsonValue(M{"items": items}))
}

func readyToShipFixture() *shippingFixture {
	state := shippedFixture()
	state.order.FulfillmentStatus, state.order.Shipment = "PREPARING", nil
	state.status, state.notified = "", false
	return state
}

func batchShippingInput(id, number string) ShippingInput {
	return ShippingInput{OrderID: id, Carrier: "快递", CarrierCode: "SF", TrackingNo: number}
}

func TestBatchShippingRejectsStructuralDuplicatesBeforeWrites(t *testing.T) {
	valid := batchShippingInput("o1", "T123")
	for _, items := range [][]ShippingInput{
		nil,
		make([]ShippingInput, 51),
		{valid, batchShippingInput("", "T456")},
		{valid, batchShippingInput(" o1 ", "T456")},
		{valid, batchShippingInput("o2", "t123")},
	} {
		// A nil store makes any accidental database operation fail the test.
		server := &Server{}
		if _, err := server.batchShipOrders(context.Background(), Claims{}, batchShippingRaw(items...)); err == nil {
			t.Fatalf("invalid batch accepted: %+v", items)
		}
	}
}

func TestBatchShippingPartialSuccessAndIdempotentRetry(t *testing.T) {
	t.Setenv("WECHAT_APP_ID", "")
	t.Setenv("WECHAT_APP_SECRET", "")
	t.Setenv("WECHAT_SHIPPING_TEMPLATE_ID", "")
	state := readyToShipFixture()
	server := shippingTestServer(t, state, func(*http.Request) (*http.Response, error) { t.Fatal("unexpected provider call"); return nil, nil })
	invalid := batchShippingInput("invalid", "X123")
	invalid.Carrier = strings.Repeat("葡", 41)
	raw := batchShippingRaw(batchShippingInput("missing", "T456"), batchShippingInput("o1", "T123"), invalid)
	value, err := server.batchShipOrders(context.Background(), Claims{Subject: "admin"}, raw)
	if err != nil {
		t.Fatal(err)
	}
	result := value.(M)
	rows := result["results"].([]BatchShippingResult)
	if result["succeeded"] != 1 || result["failed"] != 2 || len(rows) != 3 || rows[0].Success || !rows[1].Success || rows[2].Success || rows[1].SyncStatus != "FAILED" {
		t.Fatalf("unexpected partial result: %+v", result)
	}
	if state.order.FulfillmentStatus != "SHIPPED" || !state.notified || state.noticeWrites != 1 || state.reservedOwner != "o1" {
		t.Fatal("valid row did not commit shipment, reservation, and notice together")
	}
	shipment := *state.order.Shipment
	beforeAudits := len(state.auditActions)
	value, err = server.batchShipOrders(context.Background(), Claims{Subject: "admin"}, raw)
	if err != nil || value.(M)["succeeded"] != 1 || state.noticeWrites != 1 || !reflect.DeepEqual(*state.order.Shipment, shipment) || len(state.auditActions) != beforeAudits+1 {
		t.Fatal("retry changed the committed parcel or resent its notice", err)
	}
	state.order.Shipment.SyncStatus, state.order.Shipment.SyncStartedAt = "PENDING", nowString()
	value, err = server.batchShipOrders(context.Background(), Claims{}, batchShippingRaw(batchShippingInput("o1", "T123")))
	if err != nil || value.(M)["succeeded"] != 1 {
		t.Fatal("retry of pending provider sync was not idempotent", err)
	}
	value, err = server.batchShipOrders(context.Background(), Claims{}, batchShippingRaw(batchShippingInput("o1", "CHANGED123")))
	if err != nil || value.(M)["succeeded"] != 0 || !strings.Contains(value.(M)["results"].([]BatchShippingResult)[0].Error, "不能更换") || state.order.Shipment.TrackingNo != "T123" {
		t.Fatal("retry silently replaced an existing shipment", err)
	}
}

func TestBatchShippingRefundGuardsAndReservationCollision(t *testing.T) {
	for _, scenario := range []string{"refund applied", "refund in flight", "cancelled", "parcel owned"} {
		t.Run(scenario, func(t *testing.T) {
			state := readyToShipFixture()
			switch scenario {
			case "refund applied":
				state.order.RefundStatus = "APPLIED"
			case "refund in flight":
				state.order.PaymentStatus, state.order.RefundStatus = "REFUNDING", "APPROVED"
			case "cancelled":
				state.order.OrderStatus = "CANCELLED"
			case "parcel owned":
				state.reservedOwner = "other-order"
			}
			server := shippingTestServer(t, state, func(*http.Request) (*http.Response, error) { t.Fatal("rejected row reached provider"); return nil, nil })
			value, err := server.batchShipOrders(context.Background(), Claims{}, batchShippingRaw(batchShippingInput("o1", "T123")))
			if err != nil || value.(M)["failed"] != 1 || state.order.Shipment != nil || state.notified || state.noticeWrites != 0 {
				t.Fatal("restricted row mutated the order", value, err)
			}
		})
	}
}

func TestShipmentInputRejectsOverlongValuesWithoutTruncation(t *testing.T) {
	for _, field := range []string{"orderId", "carrier", "carrierCode", "trackingNo"} {
		d := batchShippingInput("o1", "T123")
		switch field {
		case "orderId":
			d.OrderID = strings.Repeat("O", 81)
		case "carrier":
			d.Carrier = strings.Repeat("葡", 41)
		case "carrierCode":
			d.CarrierCode = strings.Repeat("S", 21)
		case "trackingNo":
			d.TrackingNo = strings.Repeat("1", 51)
		}
		if _, err := validateShippingInput(d); err == nil {
			t.Fatalf("overlong %s was truncated and accepted", field)
		}
	}
}

func TestOrderExportQueryFiltersBeforeUnpaginatedLimit(t *testing.T) {
	query, args, err := orderExportQuery(orderExportRequest{OrderSearch: OrderSearch{Keyword: "CJ123 %_", Filter: "shipping_pending", Limit: 20, Offset: 1000}})
	if err != nil || !strings.Contains(query, "refund_status IN ('NONE','REJECTED','PARTIAL')") || strings.Count(query, "LOCATE(") != 2 || strings.Contains(query, "OFFSET") || !strings.HasSuffix(query, "ORDER BY created_at DESC,id DESC LIMIT ?") || !reflect.DeepEqual(args, []any{"cj123", "%_", 10001}) {
		t.Fatal("export did not apply the full server-side filter", query, args, err)
	}
	ids := []string{"o2", "o1"}
	query, args, err = orderExportQuery(orderExportRequest{OrderSearch: OrderSearch{Filter: "refund"}, OrderIDs: &ids})
	if err != nil || !strings.Contains(query, "id IN (?,?)") || strings.Contains(query, "refund_status<>") || !reflect.DeepEqual(args, []any{"o2", "o1"}) {
		t.Fatal("selected export did not use explicit IDs", query, args, err)
	}
	for _, selected := range [][]string{{}, {"o1", "o1"}, {""}, make([]string, 501)} {
		if _, _, err = orderExportQuery(orderExportRequest{OrderIDs: &selected}); err == nil {
			t.Fatal("invalid selection accepted", selected)
		}
	}
	if _, _, err = orderExportQuery(orderExportRequest{OrderSearch: OrderSearch{Filter: "unknown"}}); err == nil {
		t.Fatal("unknown filter exported all orders")
	}
}

func TestOrderCSVPreservesTextAndEscapesFormulas(t *testing.T) {
	for _, value := range []string{"=SUM(1,2)", "+cmd", "-1+cmd", "@SUM(1)", " \t=1", "\r\n=1", "\ufeff=1", "000123", "12345678901234567890", "1e10"} {
		if safeCSVText(value) != "'"+value {
			t.Fatal("unsafe or numeric text cell was not protected", value)
		}
	}
	o := Order{ID: "o1", OrderNo: "0001234567890123456789", CreatedAt: "2026-10-04T01:02:03Z", PaymentStatus: "PAID", FulfillmentStatus: "SHIPPED", RefundStatus: "NONE", Address: Address{Name: "=HYPERLINK(\"evil\")", Phone: "00123456789", Detail: "楼栋, \"一号\"\n二层"}, Note: "@SUM(1)", Quote: Quote{Subtotal: 12345, Freight: 500, Discount: 45, Total: 12800}, Invoice: &Invoice{Kind: "company", Title: "公司", TaxID: "001234567890123456", Status: "APPLIED"}, Shipment: &Shipment{Carrier: "顺丰", CarrierCode: "SF", TrackingNo: "00000123456789012345"}, Items: []OrderItem{{Name: "葡萄", SKUName: "家庭装", CartItem: CartItem{Quantity: 2}, UnitPrice: 6000}}}
	content, err := ordersCSV([]Order{o})
	if err != nil || !strings.HasPrefix(content, "\ufeff") || !strings.Contains(content, "\r\n") {
		t.Fatal("CSV encoding/BOM invalid", err)
	}
	records, err := csv.NewReader(strings.NewReader(strings.TrimPrefix(content, "\ufeff"))).ReadAll()
	if err != nil || len(records) != 2 || len(records[0]) != 32 || len(records[1]) != 32 {
		t.Fatal("CSV did not roundtrip", records, err)
	}
	row := records[1]
	if row[0] != "'"+o.OrderNo || row[2] != "2026-10-04 09:02:03" || row[7] != "123.45" || row[10] != "128.00" || row[13] != "'"+o.Address.Name || row[14] != "'"+o.Address.Phone || row[20] != "'@SUM(1)" || row[23] != "'"+o.Invoice.TaxID || row[29] != "'"+o.Shipment.TrackingNo || !strings.Contains(row[12], "× 2") {
		t.Fatal("CSV lost values or formula protection", row)
	}
}

// Export fixture verifies that all rows and invoice snapshots come from one
// read-only transaction; audit must occur after it commits without PII.
type orderExportFixture struct {
	rows         [][]driver.Value
	active       bool
	committed    bool
	rolledBack   bool
	options      driver.TxOptions
	query        string
	args         []driver.NamedValue
	auditDetails string
}
type orderExportConnector struct{ state *orderExportFixture }
type orderExportDriver struct{ state *orderExportFixture }

func (c orderExportConnector) Connect(context.Context) (driver.Conn, error) { return c.state, nil }
func (c orderExportConnector) Driver() driver.Driver                        { return orderExportDriver{c.state} }
func (d orderExportDriver) Open(string) (driver.Conn, error)                { return d.state, nil }
func (d *orderExportFixture) Close() error                                  { return nil }
func (d *orderExportFixture) Prepare(string) (driver.Stmt, error) {
	return nil, errors.New("unexpected prepare")
}
func (d *orderExportFixture) Begin() (driver.Tx, error) {
	return d.BeginTx(context.Background(), driver.TxOptions{})
}
func (d *orderExportFixture) BeginTx(_ context.Context, options driver.TxOptions) (driver.Tx, error) {
	d.active, d.options = true, options
	return d, nil
}
func (d *orderExportFixture) Commit() error   { d.active, d.committed = false, true; return nil }
func (d *orderExportFixture) Rollback() error { d.active, d.rolledBack = false, true; return nil }
func (d *orderExportFixture) QueryContext(_ context.Context, q string, args []driver.NamedValue) (driver.Rows, error) {
	if !d.active || !d.options.ReadOnly || d.options.Isolation != driver.IsolationLevel(sql.LevelRepeatableRead) || !strings.HasPrefix(q, "SELECT id,order_no,") {
		return nil, errors.New("export must read a consistent transaction snapshot")
	}
	d.query, d.args = q, args
	return &shippingRows{cols: make([]string, 9), values: d.rows}, nil
}
func (d *orderExportFixture) ExecContext(_ context.Context, q string, args []driver.NamedValue) (driver.Result, error) {
	if d.active || !d.committed || !strings.HasPrefix(q, "INSERT INTO audit_logs") || args[2].Value != "ORDER_EXPORT" {
		return nil, errors.New("unexpected export mutation")
	}
	d.auditDetails = args[4].Value.(string)
	return driver.RowsAffected(1), nil
}

func exportFixtureRow(id string) []driver.Value {
	o := Order{ID: id, OrderNo: "payload-no", PaymentStatus: "UNPAID", Address: Address{Name: "客户私密名称", Phone: "13912345678"}, Invoice: &Invoice{Title: "stale"}}
	return []driver.Value{id, "DB-" + id, "CONFIRMED", "PAID", "PREPARING", "NONE", []byte(jsonValue(o)), time.Date(2026, 10, 4, 1, 2, 3, 0, time.UTC), []byte(jsonValue(Invoice{Title: "latest invoice", Status: "ISSUED"}))}
}

func exportTestServer(t *testing.T, state *orderExportFixture) *Server {
	t.Helper()
	db := sql.OpenDB(orderExportConnector{state})
	db.SetMaxOpenConns(1)
	t.Cleanup(func() { db.Close() })
	return &Server{store: &Store{db: db}}
}

func TestExportOrdersUsesSnapshotBeyondListPageAndAuditsWithoutPII(t *testing.T) {
	state := &orderExportFixture{}
	for i := 0; i < 510; i++ {
		state.rows = append(state.rows, exportFixtureRow("o1"))
	}
	server := exportTestServer(t, state)
	value, err := server.exportOrders(context.Background(), Claims{Subject: "admin"}, []byte(`{"keyword":"客户私密名称","filter":"shipping_pending","limit":20,"offset":1000}`))
	if err != nil {
		t.Fatal(err)
	}
	result := value.(M)
	if result["count"] != 510 || !state.committed || state.rolledBack || strings.Contains(state.query, "OFFSET") || !strings.HasSuffix(result["filename"].(string), ".csv") {
		t.Fatal("export was truncated or did not commit its snapshot", result["count"], state)
	}
	rows, err := csv.NewReader(strings.NewReader(strings.TrimPrefix(result["content"].(string), "\ufeff"))).ReadAll()
	if err != nil || len(rows) != 511 || rows[1][0] != "DB-o1" || rows[1][4] != "已付款" || rows[1][22] != "latest invoice" {
		t.Fatal("export did not use authoritative status/invoice snapshot", err)
	}
	if !strings.Contains(state.auditDetails, `"count":510`) || strings.Contains(state.auditDetails, "客户") || strings.Contains(state.auditDetails, "13912345678") || strings.Contains(state.auditDetails, "latest invoice") {
		t.Fatal("export audit includes sensitive data", state.auditDetails)
	}
}

func TestExportOrdersRefusesOverflowAndMissingSelection(t *testing.T) {
	for _, name := range []string{"overflow", "missing selected"} {
		t.Run(name, func(t *testing.T) {
			state := &orderExportFixture{}
			raw := json.RawMessage(`{}`)
			if name == "overflow" {
				row := exportFixtureRow("o1")
				for i := 0; i < 10001; i++ {
					state.rows = append(state.rows, row)
				}
			} else {
				state.rows = [][]driver.Value{exportFixtureRow("o1")}
				raw = []byte(`{"orderIds":["o1","missing"]}`)
			}
			if _, err := exportTestServer(t, state).exportOrders(context.Background(), Claims{}, raw); err == nil || state.committed || !state.rolledBack || state.auditDetails != "" {
				t.Fatal("oversized or incomplete export was silently accepted", err)
			}
		})
	}
}
