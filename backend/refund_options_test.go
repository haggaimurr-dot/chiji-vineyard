package main

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"io"
	"testing"
)

type evidenceReadDB struct{ refundExecDB }
type evidenceConnector struct{ state *evidenceReadDB }

func (c evidenceConnector) Connect(context.Context) (driver.Conn, error) { return c.state, nil }
func (c evidenceConnector) Driver() driver.Driver                        { return refundExecDriver{&c.state.refundExecDB} }

type evidenceRow struct{ done bool }

func (*evidenceRow) Columns() []string { return []string{"openid", "mime", "content"} }
func (*evidenceRow) Close() error      { return nil }
func (r *evidenceRow) Next(values []driver.Value) error {
	if r.done {
		return io.EOF
	}
	r.done = true
	values[0] = "owner"
	values[1] = "image/jpeg"
	values[2] = []byte{0xff, 0xd8, 0xff}
	return nil
}
func (*evidenceReadDB) QueryContext(context.Context, string, []driver.NamedValue) (driver.Rows, error) {
	return &evidenceRow{}, nil
}

func TestEvidenceReadRequiresOwnerOrAuthenticatedAdmin(t *testing.T) {
	db := sql.OpenDB(evidenceConnector{&evidenceReadDB{}})
	defer db.Close()
	s := &Server{store: &Store{db: db}}
	raw := []byte(`{"id":"image"}`)
	if result, err := s.readRefundEvidence(context.Background(), "another-user", false, raw); err == nil || result != nil {
		t.Fatal("another user read private evidence")
	}
	for _, identity := range []struct {
		openid string
		admin  bool
	}{{"owner", false}, {"", true}} {
		result, err := s.readRefundEvidence(context.Background(), identity.openid, identity.admin, raw)
		if err != nil {
			t.Fatal(err)
		}
		if result.(M)["dataUrl"] != "data:image/jpeg;base64,/9j/" {
			t.Fatal("owner/admin preview unavailable")
		}
	}
}

func TestAfterSalesValidation(t *testing.T) {
	o := Order{PaymentStatus: "PAID", FulfillmentStatus: "SHIPPED", RefundStatus: "NONE", Quote: Quote{Total: 10000}, RefundedAmount: 2000}
	if validateRefund(o, 2000, "坏果", []string{"evidence"}) != nil {
		t.Fatal("valid shipped partial refund rejected")
	}
	for _, amount := range []int{-1, 0, 8001} {
		if validateRefund(o, amount, "坏果", []string{"evidence"}) == nil {
			t.Fatalf("accepted amount %d", amount)
		}
	}
	if validateRefund(o, 2000, "坏果", nil) == nil {
		t.Fatal("shipped refund accepted without evidence")
	}
	if validateRefund(o, 2000, "", []string{"evidence"}) == nil {
		t.Fatal("empty reason accepted")
	}
	if validateRefund(o, 2000, "坏果", []string{"a", "a"}) == nil {
		t.Fatal("duplicate evidence accepted")
	}
	if validateRefund(o, 2000, "坏果", []string{"1", "2", "3", "4", "5", "6", "7"}) == nil {
		t.Fatal("too many images accepted")
	}
	for _, state := range []string{"APPLIED", "APPROVED", "FAILED", "SUCCEEDED"} {
		o.RefundStatus = state
		if canRefund(o) {
			t.Fatalf("accepted state %s", state)
		}
	}
	for _, state := range []string{"NONE", "REJECTED", "PARTIAL"} {
		o.RefundStatus = state
		if !canRefund(o) {
			t.Fatalf("rejected state %s", state)
		}
	}
	o.FulfillmentStatus = "RECEIVED"
	if validateRefund(o, 8000, "坏果", []string{"a"}) != nil {
		t.Fatal("received order rejected")
	}
	o.FulfillmentStatus = "PREPARING"
	if validateRefund(o, 8000, "取消", nil) != nil {
		t.Fatal("unshipped refund requires evidence")
	}
	if !canShip(o) {
		t.Fatal("partially refunded order cannot ship")
	}
}

func TestPartialRefundPersistenceAndCumulativeLimit(t *testing.T) {
	for _, fulfillment := range []string{"PREPARING", "SHIPPED", "RECEIVED"} {
		t.Run(fulfillment, func(t *testing.T) {
			state := &refundExecDB{}
			db := sql.OpenDB(refundExecConnector{state})
			defer db.Close()
			o := Order{ID: "o", PaymentStatus: "REFUNDING", OrderStatus: "CONFIRMED", FulfillmentStatus: fulfillment, Quote: Quote{Total: 10000}, Items: []OrderItem{{CartItem: CartItem{SKUID: "sku", Quantity: 2}}}}
			apply := func(rf *Refund) error {
				tx, e := db.Begin()
				if e != nil {
					return e
				}
				defer tx.Rollback()
				if e = persistRefundResult(context.Background(), tx, rf, &o, "SUCCESS", "wx"); e != nil {
					return e
				}
				return tx.Commit()
			}
			first := Refund{Status: "APPROVED", Amount: 2000}
			if err := apply(&first); err != nil {
				t.Fatal(err)
			}
			if o.RefundedAmount != 2000 || o.PaymentStatus != "PAID" || o.RefundStatus != "PARTIAL" || o.FulfillmentStatus != fulfillment || state.stock != 0 {
				t.Fatal("partial refund changed fulfillment or inventory")
			}
			if err := apply(&first); err != nil {
				t.Fatal(err)
			}
			if o.RefundedAmount != 2000 {
				t.Fatal("duplicate refund added amount twice")
			}
			excessive := Refund{Status: "APPROVED", Amount: 8001}
			if apply(&excessive) == nil {
				t.Fatal("cumulative excess accepted")
			}
			final := Refund{Status: "APPROVED", Amount: 8000}
			if err := apply(&final); err != nil {
				t.Fatal(err)
			}
			if o.RefundedAmount != 10000 || o.PaymentStatus != "REFUNDED" {
				t.Fatal("remaining refund not finalized")
			}
			expected := 0
			if fulfillment == "PREPARING" {
				expected = 2
				if o.OrderStatus != "CLOSED" {
					t.Fatal("unshipped full refund did not close order")
				}
			}
			if state.committed != expected {
				t.Fatalf("restored %d stock for %s", state.committed, fulfillment)
			}
			if err := apply(&final); err != nil {
				t.Fatal(err)
			}
			if state.committed != expected {
				t.Fatal("duplicate restored stock")
			}
		})
	}
}
