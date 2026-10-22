package main

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"encoding/json"
	"errors"
	"reflect"
	"strings"
	"testing"
	"time"
)

func TestOrderSearchFiltersAndBounds(t *testing.T) {
	for filter, clause := range map[string]string{
		"all": "1=1", "pending": "order_status='PENDING_PAYMENT' AND payment_status='UNPAID'",
		"paid": "refund_status IN ('NONE','REJECTED','PARTIAL')", "shipping_pending": "fulfillment_status IN ('UNFULFILLED','PREPARING')",
		"shipping": "fulfillment_status='SHIPPED'", "shipped": "order_status='CONFIRMED'",
		"received": "fulfillment_status='RECEIVED'", "completed": "order_status='COMPLETED'",
		"cancelled": "order_status='CANCELLED'", "closed": "order_status='CLOSED'",
		"refund": "(refund_status<>'NONE' OR payment_status IN ('REFUNDING','REFUNDED'))",
	} {
		search, err := normalizeOrderSearch(OrderSearch{Filter: filter}, true)
		if err != nil {
			t.Fatal(err)
		}
		where, _ := orderSearchWhere("", search)
		if !strings.Contains(where, clause) || search.Limit != 20 || search.Offset != 0 {
			t.Fatalf("%s: %s %+v", filter, where, search)
		}
	}
	search, err := normalizeOrderSearch(OrderSearch{Limit: 501, Offset: -1, Filter: " SHIPPED ", RefundStatus: " partial "}, true)
	if err != nil || search.Limit != 100 || search.Offset != 0 || search.Filter != "shipped" || search.RefundStatus != "PARTIAL" {
		t.Fatalf("page bounds: %+v %v", search, err)
	}
	for _, invalid := range []OrderSearch{
		{Filter: "not-a-status"}, {OrderStatus: "FAILED"}, {OrderStatus: "CONFIRMED COMPLETED"}, {PaymentStatus: "SUCCESS"},
		{FulfillmentStatus: "CANCELLED"}, {RefundStatus: "PROCESSING"},
	} {
		if _, err := normalizeOrderSearch(invalid, true); err == nil {
			t.Fatalf("invalid filter accepted: %+v", invalid)
		}
	}
}

func TestOrderSearchIndependentStatesAndLiteralWords(t *testing.T) {
	search := OrderSearch{Keyword: " CJ123  葡萄 %_ ", OrderStatus: "CONFIRMED", PaymentStatus: "PAID", FulfillmentStatus: "SHIPPED", RefundStatus: "PARTIAL", Limit: 50, Offset: 100}
	query, args := orderSearchQuery("owner", search)
	wantArgs := []any{"owner", "CONFIRMED", "PAID", "SHIPPED", "PARTIAL", "cj123", "葡萄", "%_", 50, 100}
	if !reflect.DeepEqual(args, wantArgs) || strings.Count(query, "LOCATE(") != 3 || strings.Contains(query, " LIKE ") {
		t.Fatalf("query/args: %s %#v", query, args)
	}
	for _, fragment := range []string{"openid=?", "order_status=?", "payment_status=?", "fulfillment_status=?", "refund_status=?", "$.address.name", "$.address.phone", "$.shipment.trackingNo", "ORDER BY created_at DESC,id DESC LIMIT ? OFFSET ?"} {
		if !strings.Contains(query, fragment) {
			t.Fatalf("query missing %s", fragment)
		}
	}
}

type orderPageFixture struct {
	active, committed, snapshot bool
	countWhere                  string
	countArgs                   []driver.NamedValue
	pageArgs                    []driver.NamedValue
}

type orderPageConnector struct{ state *orderPageFixture }
type orderPageDriver struct{ state *orderPageFixture }

func (c orderPageConnector) Connect(context.Context) (driver.Conn, error) { return c.state, nil }
func (c orderPageConnector) Driver() driver.Driver                        { return orderPageDriver{c.state} }
func (d orderPageDriver) Open(string) (driver.Conn, error)                { return d.state, nil }
func (d *orderPageFixture) Close() error                                  { return nil }
func (d *orderPageFixture) Prepare(string) (driver.Stmt, error) {
	return nil, errors.New("unexpected prepare")
}
func (d *orderPageFixture) Begin() (driver.Tx, error) {
	return nil, errors.New("snapshot options required")
}
func (d *orderPageFixture) BeginTx(_ context.Context, opts driver.TxOptions) (driver.Tx, error) {
	if !opts.ReadOnly || opts.Isolation != driver.IsolationLevel(sql.LevelRepeatableRead) {
		return nil, errors.New("page must share a read-only repeatable-read snapshot")
	}
	d.active, d.snapshot = true, true
	return d, nil
}
func (d *orderPageFixture) Commit() error   { d.active, d.committed = false, true; return nil }
func (d *orderPageFixture) Rollback() error { d.active = false; return nil }
func (d *orderPageFixture) QueryContext(_ context.Context, q string, args []driver.NamedValue) (driver.Rows, error) {
	if !d.active {
		return nil, errors.New("query outside page snapshot")
	}
	switch {
	case strings.HasPrefix(q, "SELECT COUNT(*) FROM orders WHERE "):
		d.countWhere = strings.TrimPrefix(q, "SELECT COUNT(*) FROM orders WHERE ")
		d.countArgs = append([]driver.NamedValue{}, args...)
		return singleShippingRow(int64(25)), nil
	case strings.HasPrefix(q, "SELECT id FROM orders WHERE "):
		where := strings.Split(strings.TrimPrefix(q, "SELECT id FROM orders WHERE "), " ORDER BY ")[0]
		if where != d.countWhere || !reflect.DeepEqual(args[:len(args)-2], d.countArgs) {
			return nil, errors.New("count and page use different filters")
		}
		d.pageArgs = append([]driver.NamedValue{}, args...)
		return &shippingRows{cols: []string{"id"}, values: [][]driver.Value{{"o21"}, {"o22"}, {"o23"}}}, nil
	case strings.HasPrefix(q, "SELECT order_no,openid"):
		id := args[0].Value.(string)
		// An old JSON status must not override indexed state used for filtering.
		raw, _ := json.Marshal(Order{ID: id, OrderStatus: "PENDING_PAYMENT", PaymentStatus: "UNPAID", FulfillmentStatus: "UNFULFILLED", RefundStatus: "NONE"})
		return singleShippingRow("CJ"+id, "owner", "CONFIRMED", "PAID", "SHIPPED", "PARTIAL", raw, false, "", time.Now(), time.Now()), nil
	case strings.Contains(q, "FROM invoice_requests"):
		return &shippingRows{cols: []string{"payload"}}, nil
	case strings.HasPrefix(q, "SELECT push_status,push_error"):
		return &shippingRows{cols: []string{"push_status", "push_error"}}, nil
	default:
		return nil, errors.New("unexpected query: " + q)
	}
}

func TestOrderPageCountScopeSnapshotAndIndexedStatuses(t *testing.T) {
	state := &orderPageFixture{}
	db := sql.OpenDB(orderPageConnector{state})
	db.SetMaxOpenConns(1)
	defer db.Close()
	page, err := (&Store{db: db}).searchOrdersPage(context.Background(), "owner", OrderSearch{Keyword: "葡萄", Filter: "shipped", Limit: 3, Offset: 20})
	if err != nil {
		t.Fatal(err)
	}
	if page.Total != 25 || page.Offset != 20 || page.Limit != 3 || len(page.Items) != 3 || !state.snapshot || !state.committed {
		t.Fatalf("incomplete page or snapshot: %+v %+v", page, state)
	}
	if !strings.Contains(state.countWhere, "openid=?") || len(state.countArgs) != 2 || state.countArgs[0].Value != "owner" || state.countArgs[1].Value != "葡萄" {
		t.Fatalf("count escaped owner/keyword scope: %+v", state)
	}
	if state.pageArgs[len(state.pageArgs)-2].Value != int64(3) || state.pageArgs[len(state.pageArgs)-1].Value != int64(20) {
		t.Fatal("pagination missing from SQL", state.pageArgs)
	}
	for _, o := range page.Items {
		if o.OrderStatus != "CONFIRMED" || o.PaymentStatus != "PAID" || o.FulfillmentStatus != "SHIPPED" || o.RefundStatus != "PARTIAL" {
			t.Fatalf("payload overrode indexed state: %+v", o)
		}
	}
}
