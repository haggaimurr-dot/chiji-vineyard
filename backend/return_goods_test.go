package main

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"encoding/json"
	"errors"
	"strings"
	"testing"
)

type returnFixture struct {
	*ticketFixture
	stock, pendingStock, movements, pendingMovements int
	failMovement                                     bool
	previous                                         *AfterSalesTicket
}
type returnConnector struct{ f *returnFixture }

func (c returnConnector) Connect(context.Context) (driver.Conn, error) { return c.f, nil }
func (c returnConnector) Driver() driver.Driver                        { return returnDriver{c.f} }

type returnDriver struct{ f *returnFixture }

func (d returnDriver) Open(string) (driver.Conn, error) { return d.f, nil }
func (f *returnFixture) Begin() (driver.Tx, error) {
	f.ticketFixture.Begin()
	f.pendingStock = f.stock
	f.pendingMovements = 0
	return f, nil
}
func (f *returnFixture) Commit() error {
	f.stock = f.pendingStock
	f.movements += f.pendingMovements
	return f.ticketFixture.Commit()
}
func (f *returnFixture) Rollback() error { return f.ticketFixture.Rollback() }
func (f *returnFixture) QueryContext(ctx context.Context, q string, a []driver.NamedValue) (driver.Rows, error) {
	switch {
	case strings.HasPrefix(q, "SELECT payload FROM after_sales_tickets WHERE order_id"):
		r := &accountRows{columns: []string{"payload"}}
		if f.previous != nil {
			r.values = [][]driver.Value{{jsonValue(f.previous)}}
		}
		return r, nil
	case strings.HasPrefix(q, "SELECT stock,reserved,sold"):
		return &accountRows{columns: []string{"stock", "reserved", "sold"}, values: [][]driver.Value{{int64(f.pendingStock), int64(0), int64(3)}}}, nil
	case strings.HasPrefix(q, "SELECT s.product_id"):
		return &accountRows{columns: []string{"product", "name", "sku", "stock", "reserved", "sold"}, values: [][]driver.Value{{"p1", "葡萄", "礼盒", int64(f.pendingStock), int64(0), int64(3)}}}, nil
	case strings.HasPrefix(q, "SELECT openid,payload") && !strings.HasSuffix(q, "FOR UPDATE"):
		r := &accountRows{columns: []string{"user", "payload"}}
		if f.ticket != nil {
			r.values = [][]driver.Value{{f.order.OpenID, jsonValue(f.ticket)}}
		}
		return r, nil
	default:
		return f.ticketFixture.QueryContext(ctx, q, a)
	}
}
func (f *returnFixture) ExecContext(ctx context.Context, q string, a []driver.NamedValue) (driver.Result, error) {
	switch {
	case strings.HasPrefix(q, "UPDATE skus SET stock=stock+"):
		f.pendingStock += int(a[0].Value.(int64))
		return driver.RowsAffected(1), nil
	case strings.HasPrefix(q, "INSERT INTO inventory_movements"):
		if f.failMovement {
			return nil, errors.New("ledger failed")
		}
		if a[5].Value != "RETURN_RECEIPT" {
			return nil, errors.New("wrong movement kind")
		}
		f.pendingMovements++
		return driver.RowsAffected(1), nil
	default:
		return f.ticketFixture.ExecContext(ctx, q, a)
	}
}

func TestReturnGoodsLifecycleAtomicReceipt(t *testing.T) {
	f := &returnFixture{ticketFixture: &ticketFixture{order: Order{ID: "order1", OrderNo: "CJ1", OpenID: "user1", PaymentStatus: "PAID", FulfillmentStatus: "SHIPPED", Items: []OrderItem{{CartItem: CartItem{SKUID: "s1", Quantity: 3}, Name: "葡萄", SKUName: "礼盒"}}}}, stock: 10}
	db := sql.OpenDB(returnConnector{f})
	defer db.Close()
	db.SetMaxOpenConns(1)
	s := &Server{store: &Store{db: db}}
	ctx := context.Background()
	actor := Claims{Subject: "admin", Username: "operator"}
	raw := func(v any) json.RawMessage { return json.RawMessage(jsonValue(v)) }
	if _, err := s.createTicket(ctx, "user1", nil, raw(M{"orderId": "order1", "category": "QUALITY", "content": "申请退回"})); err != nil {
		t.Fatal(err)
	}
	update := func(user string, a *Claims, d M) error {
		d["id"] = f.ticket.ID
		d["version"] = f.ticket.Version
		_, err := s.updateTicket(ctx, user, a, raw(d))
		return err
	}
	auth := ReturnAction{Action: "AUTHORIZE", Address: Address{Name: "退货仓", Phone: "13800138000", Province: "浙江省", City: "杭州市", Detail: "验收仓1号"}, Items: []ReturnItem{{SKUID: "s1", Quantity: 3}}}
	if err := update("user1", nil, M{"return": auth}); err == nil {
		t.Fatal("user authorized own return")
	}
	bad := auth
	bad.Items = []ReturnItem{{SKUID: "foreign", Quantity: 1}}
	if err := update("", &actor, M{"return": bad}); err == nil {
		t.Fatal("foreign SKU accepted")
	}
	f.previous = &AfterSalesTicket{Return: &TicketReturn{Items: []ReturnItem{{SKUID: "s1", Quantity: 1}}}}
	if err := update("", &actor, M{"return": auth}); err == nil {
		t.Fatal("cross-ticket over-return accepted")
	}
	f.previous = nil
	if err := update("", &actor, M{"return": auth}); err != nil {
		t.Fatal(err)
	}
	if f.ticket.Return.Status != "AWAITING_SHIPMENT" || f.ticket.Status != "PROCESSING" {
		t.Fatal("authorization not saved")
	}
	if err := update("", &actor, M{"return": auth}); err == nil {
		t.Fatal("address overwritten")
	}
	if err := update("", &actor, M{"status": "RESOLVED", "content": "close early"}); err == nil {
		t.Fatal("unreceived return resolved")
	}
	receive := ReturnAction{Action: "RECEIVE", Items: []ReturnItem{{SKUID: "s1", Quantity: 3, Restock: 2}}, Inspection: "2盒可售，1盒破损隔离"}
	if err := update("", &actor, M{"return": receive}); err == nil {
		t.Fatal("unshipped return received")
	}
	ship := ReturnAction{Action: "SHIP", Shipment: ShipmentInput{Carrier: "顺丰速运", CarrierCode: "SF", TrackingNo: "SF123456"}}
	if err := update("stranger", nil, M{"return": ship}); err == nil {
		t.Fatal("foreign user shipped")
	}
	if err := update("user1", nil, M{"return": ship}); err != nil {
		t.Fatal(err)
	}
	if f.stock != 10 {
		t.Fatal("shipping changed stock")
	}
	if _, err := s.returnTracking(ctx, "stranger", raw(M{"id": f.ticket.ID})); err == nil {
		t.Fatal("foreign user read return tracking")
	}
	if err := update("user1", nil, M{"return": receive}); err == nil {
		t.Fatal("user received goods")
	}
	stale := f.ticket.Version - 1
	if _, err := s.updateTicket(ctx, "", &actor, raw(M{"id": f.ticket.ID, "version": stale, "return": receive})); err == nil {
		t.Fatal("stale receipt accepted")
	}
	f.failMovement = true
	if err := update("", &actor, M{"return": receive}); err == nil {
		t.Fatal("ledger failure ignored")
	}
	f.failMovement = false
	if f.stock != 10 || f.movements != 0 || f.ticket.Return.Status != "SHIPPED" {
		t.Fatal("ledger failure committed")
	}
	f.failAudit = true
	if err := update("", &actor, M{"return": receive}); err == nil {
		t.Fatal("audit failure ignored")
	}
	f.failAudit = false
	if f.stock != 10 || f.movements != 0 {
		t.Fatal("audit failure changed stock")
	}
	if err := update("", &actor, M{"return": receive}); err != nil {
		t.Fatal(err)
	}
	if f.stock != 12 || f.movements != 1 || f.ticket.Return.Status != "RECEIVED" || f.ticket.Return.Items[0].Restock != 2 {
		t.Fatal("incorrect inventory receipt")
	}
	if err := update("", &actor, M{"return": receive}); err == nil {
		t.Fatal("duplicate stock receipt")
	}
	if err := update("user1", nil, M{"return": ship}); err == nil {
		t.Fatal("shipment rewritten after receipt")
	}
	if err := update("", &actor, M{"status": "RESOLVED", "content": "验收处理完成"}); err != nil {
		t.Fatal(err)
	}
	if f.stock != 12 || f.movements != 1 {
		t.Fatal("resolution restored stock again")
	}
}

func TestReturnReceiptQuantityValidation(t *testing.T) {
	for _, items := range [][]ReturnItem{
		{{SKUID: "s1", Quantity: 0}}, {{SKUID: "s1", Quantity: 4}}, {{SKUID: "s1", Quantity: 3, Restock: 4}},
		{{SKUID: "s1", Quantity: 1}, {SKUID: "s1", Quantity: 1}}, {{SKUID: "foreign", Quantity: 1}},
	} {
		if validateReturnItems(items, map[string]int{"s1": 3}, true) == nil {
			t.Fatalf("invalid items accepted: %+v", items)
		}
	}
}
