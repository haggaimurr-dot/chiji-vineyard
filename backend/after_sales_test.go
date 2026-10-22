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

type ticketFixture struct {
	ticket        *AfterSalesTicket
	working       *AfterSalesTicket
	order         Order
	failAudit     bool
	audits        int
	pendingAudits int
}
type ticketConnector struct{ f *ticketFixture }

func (c ticketConnector) Connect(context.Context) (driver.Conn, error) { return c.f, nil }
func (c ticketConnector) Driver() driver.Driver                        { return ticketDriver{c.f} }

type ticketDriver struct{ f *ticketFixture }

func (d ticketDriver) Open(string) (driver.Conn, error) { return d.f, nil }
func (f *ticketFixture) Close() error                   { return nil }
func (f *ticketFixture) Prepare(string) (driver.Stmt, error) {
	return nil, errors.New("unexpected prepare")
}
func cloneTicket(t *AfterSalesTicket) *AfterSalesTicket {
	if t == nil {
		return nil
	}
	var clone AfterSalesTicket
	json.Unmarshal([]byte(jsonValue(t)), &clone)
	return &clone
}
func (f *ticketFixture) Begin() (driver.Tx, error) {
	f.working = cloneTicket(f.ticket)
	f.pendingAudits = 0
	return f, nil
}
func (f *ticketFixture) Commit() error {
	f.ticket = f.working
	f.working = nil
	f.audits += f.pendingAudits
	return nil
}
func (f *ticketFixture) Rollback() error { f.working = nil; f.pendingAudits = 0; return nil }
func (f *ticketFixture) QueryContext(_ context.Context, q string, args []driver.NamedValue) (driver.Rows, error) {
	switch {
	case strings.HasPrefix(q, "SELECT order_no,openid"):
		if !strings.HasSuffix(q, "FOR UPDATE") {
			return nil, errors.New("order must be locked")
		}
		r := &accountRows{columns: []string{"no", "user", "order", "pay", "ship", "refund", "payload", "attempt", "transaction", "expire", "created"}}
		if args[0].Value == f.order.ID {
			r.values = [][]driver.Value{{f.order.OrderNo, f.order.OpenID, f.order.OrderStatus, f.order.PaymentStatus, f.order.FulfillmentStatus, f.order.RefundStatus, jsonValue(f.order), false, "", time.Now().Add(time.Hour), time.Now()}}
		}
		return r, nil
	case strings.HasPrefix(q, "SELECT payload FROM invoice_requests"):
		return &accountRows{columns: []string{"payload"}}, nil
	case strings.HasPrefix(q, "SELECT push_status,push_error"):
		return &accountRows{columns: []string{"status", "error"}}, nil
	case strings.HasPrefix(q, "SELECT id FROM after_sales_tickets"):
		r := &accountRows{columns: []string{"id"}}
		if f.working != nil && f.working.Status != "CLOSED" {
			r.values = [][]driver.Value{{f.working.ID}}
		}
		return r, nil
	case strings.HasPrefix(q, "SELECT order_id FROM refunds"):
		r := &accountRows{columns: []string{"order_id"}}
		if args[0].Value == "refund1" {
			r.values = [][]driver.Value{{f.order.ID}}
		}
		return r, nil
	case strings.HasPrefix(q, "SELECT openid,payload FROM after_sales_tickets"):
		r := &accountRows{columns: []string{"user", "payload"}}
		if f.working != nil && args[0].Value == f.working.ID {
			r.values = [][]driver.Value{{f.order.OpenID, jsonValue(f.working)}}
		}
		return r, nil
	case strings.HasPrefix(q, "SELECT COUNT(*) FROM admins"):
		n := int64(0)
		if args[0].Value == "operator" {
			n = 1
		}
		return &accountRows{columns: []string{"count"}, values: [][]driver.Value{{n}}}, nil
	case strings.HasPrefix(q, "SELECT payload FROM after_sales_tickets"):
		r := &accountRows{columns: []string{"payload"}}
		if !strings.Contains(q, "openid=?") {
			return nil, errors.New("user list must filter ownership")
		}
		if f.ticket != nil && args[0].Value == f.order.OpenID {
			r.values = [][]driver.Value{{jsonValue(f.ticket)}}
		}
		return r, nil
	}
	return nil, errors.New("unexpected query: " + q)
}
func (f *ticketFixture) ExecContext(_ context.Context, q string, args []driver.NamedValue) (driver.Result, error) {
	var raw string
	switch {
	case strings.HasPrefix(q, "INSERT INTO after_sales_tickets"):
		raw = args[4].Value.(string)
	case strings.HasPrefix(q, "UPDATE after_sales_tickets"):
		raw = args[1].Value.(string)
	case strings.HasPrefix(q, "INSERT INTO audit_logs"):
		if f.failAudit {
			return nil, errors.New("audit failure")
		}
		f.pendingAudits++
		return driver.RowsAffected(1), nil
	default:
		return nil, errors.New("unexpected exec: " + q)
	}
	var ticket AfterSalesTicket
	if err := json.Unmarshal([]byte(raw), &ticket); err != nil {
		return nil, err
	}
	f.working = &ticket
	return driver.RowsAffected(1), nil
}
func TestAfterSalesOwnershipLifecycleAndConflict(t *testing.T) {
	f := &ticketFixture{order: Order{ID: "order1", OrderNo: "CJ1", OpenID: "user1", PaymentStatus: "PAID", OrderStatus: "CONFIRMED", FulfillmentStatus: "SHIPPED", RefundStatus: "APPLIED"}}
	db := sql.OpenDB(ticketConnector{f})
	defer db.Close()
	db.SetMaxOpenConns(1)
	s := &Server{store: &Store{db: db}}
	ctx := context.Background()
	actor := Claims{Subject: "admin1", Username: "operator"}
	raw := func(v any) json.RawMessage { return json.RawMessage(jsonValue(v)) }
	create := raw(M{"orderId": "order1", "category": "QUALITY", "content": "坏果问题", "refundId": "refund1"})
	if _, err := s.createTicket(ctx, "stranger", nil, create); err == nil {
		t.Fatal("other user created ticket")
	}
	wrongRefund := raw(M{"orderId": "order1", "category": "QUALITY", "content": "问题", "refundId": "wrong"})
	if _, err := s.createTicket(ctx, "user1", nil, wrongRefund); err == nil {
		t.Fatal("unrelated refund linked")
	}
	if _, err := s.createTicket(ctx, "user1", nil, create); err != nil {
		t.Fatal(err)
	}
	if _, err := s.createTicket(ctx, "user1", nil, create); err == nil {
		t.Fatal("duplicate active ticket")
	}
	id := f.ticket.ID
	update := func(user string, a *Claims, data M) error {
		data["id"] = id
		_, err := s.updateTicket(ctx, user, a, raw(data))
		return err
	}
	if err := update("stranger", nil, M{"version": 1, "content": "foreign"}); err == nil {
		t.Fatal("other user updated ticket")
	}
	if err := update("user1", nil, M{"version": 1, "status": "CLOSED", "content": "bad"}); err == nil {
		t.Fatal("user changed status")
	}
	if err := update("", &actor, M{"version": 1, "owner": "unknown"}); err == nil {
		t.Fatal("unknown owner accepted")
	}
	if err := update("", &actor, M{"version": 1, "owner": "operator", "status": "PROCESSING", "content": "正在处理"}); err != nil {
		t.Fatal(err)
	}
	if f.ticket.Version != 2 || f.audits != 1 {
		t.Fatal("update or audit missing")
	}
	if err := update("user1", nil, M{"version": 1, "content": "stale"}); err == nil {
		t.Fatal("stale update accepted")
	}
	if err := update("", &actor, M{"version": 2, "status": "CLOSED", "content": "close"}); err == nil {
		t.Fatal("skipped resolution")
	}
	if err := update("", &actor, M{"version": 2, "status": "RESOLVED"}); err == nil {
		t.Fatal("missing resolution text")
	}
	f.failAudit = true
	if err := update("", &actor, M{"version": 2, "status": "RESOLVED", "content": "已处理"}); err == nil {
		t.Fatal("audit failure ignored")
	}
	if f.ticket.Version != 2 || f.ticket.Status != "PROCESSING" {
		t.Fatal("audit failure committed update")
	}
	f.failAudit = false
	if err := update("", &actor, M{"version": 2, "status": "RESOLVED", "content": "已处理"}); err != nil {
		t.Fatal(err)
	}
	if err := update("user1", nil, M{"version": 3, "content": "仍有问题"}); err != nil {
		t.Fatal(err)
	}
	if f.ticket.Status != "PROCESSING" {
		t.Fatal("followup did not reopen resolved ticket")
	}
	if err := update("", &actor, M{"version": 4, "status": "RESOLVED", "content": "再次处理"}); err != nil {
		t.Fatal(err)
	}
	if err := update("", &actor, M{"version": 5, "status": "CLOSED", "content": "处理完成"}); err != nil {
		t.Fatal(err)
	}
	if err := update("user1", nil, M{"version": 6, "content": "late"}); err == nil {
		t.Fatal("closed ticket editable")
	}
	items, err := s.listTickets(ctx, "stranger", raw(M{}))
	if err != nil || len(items.([]AfterSalesTicket)) != 0 {
		t.Fatal("foreign tickets exposed", err)
	}
	items, err = s.listTickets(ctx, "user1", raw(M{}))
	if err != nil || len(items.([]AfterSalesTicket)) != 1 || items.([]AfterSalesTicket)[0].Owner != "" {
		t.Fatal("list or owner redaction failed", err)
	}
	if _, err = s.createTicket(ctx, "user1", nil, create); err != nil {
		t.Fatal("new ticket after closure failed", err)
	}
}
