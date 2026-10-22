package main

import (
	"context"
	"crypto"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"database/sql"
	"database/sql/driver"
	"encoding/base64"
	"errors"
	"io"
	"net/http"
	"strconv"
	"strings"
	"testing"
	"time"
)

type refundTransport func(*http.Request) (*http.Response, error)

func (f refundTransport) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

func TestRefundQueryUsesMerchantNumberAndVerifiesResponse(t *testing.T) {
	t.Setenv("WECHAT_MCH_ID", "merchant")
	t.Setenv("WECHAT_MCH_SERIAL_NO", "serial")
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatal(err)
	}
	valid := true
	w := &Wechat{privateKey: key, publicKey: &key.PublicKey}
	w.client = &http.Client{Transport: refundTransport(func(req *http.Request) (*http.Response, error) {
		if req.Method != "GET" || req.URL.Path != "/v3/refund/domestic/refunds/RF_test" || !strings.Contains(req.Header.Get("Authorization"), `mchid="merchant"`) {
			t.Fatal("wrong refund query or authentication")
		}
		body := `{"out_refund_no":"RF_test","status":"SUCCESS"}`
		stamp := strconv.FormatInt(time.Now().Unix(), 10)
		nonce := "nonce"
		digest := sha256.Sum256([]byte(stamp + "\n" + nonce + "\n" + body + "\n"))
		sig, e := rsa.SignPKCS1v15(rand.Reader, key, crypto.SHA256, digest[:])
		if e != nil {
			return nil, e
		}
		header := http.Header{}
		header.Set("Wechatpay-Timestamp", stamp)
		header.Set("Wechatpay-Nonce", nonce)
		header.Set("Wechatpay-Signature", base64.StdEncoding.EncodeToString(sig))
		if !valid {
			body = `{"out_refund_no":"RF_test","status":"PROCESSING"}`
		}
		return &http.Response{StatusCode: 200, Header: header, Body: io.NopCloser(strings.NewReader(body))}, nil
	})}
	result, err := w.queryRefund("RF_test")
	if err != nil || result["status"] != "SUCCESS" {
		t.Fatalf("signed query failed: %v", err)
	}
	valid = false
	if _, err = w.queryRefund("RF_test"); err == nil {
		t.Fatal("tampered response accepted")
	}
}

func TestRefundResultIdentityAndCouponAmount(t *testing.T) {
	t.Setenv("WECHAT_MCH_ID", "merchant")
	r := Refund{ID: "rf", OrderNo: "order", Amount: 100}
	p := M{"out_refund_no": "rf", "out_trade_no": "order", "refund_id": "wx-rf", "mchid": "merchant", "amount": M{"refund": float64(100), "payer_refund": float64(90)}}
	if err := validateRefundResult(r, p, true); err != nil {
		t.Fatal(err)
	}
	for _, key := range []string{"out_refund_no", "out_trade_no", "refund_id", "mchid"} {
		saved := p[key]
		p[key] = ""
		if validateRefundResult(r, p, true) == nil {
			t.Fatalf("accepted invalid %s", key)
		}
		p[key] = saved
	}
	for _, amount := range []any{nil, M{}, M{"refund": 99.5}, M{"refund": float64(99)}} {
		p["amount"] = amount
		if validateRefundResult(r, p, false) == nil {
			t.Fatal("accepted invalid amount")
		}
	}
}

// This driver records transaction effects so rollback and exactly-once inventory
// restoration are exercised without a live merchant or database.
type refundExecDB struct {
	pending, committed, stock int
	failOrder                 bool
}
type refundExecConnector struct{ state *refundExecDB }
type refundExecDriver struct{ state *refundExecDB }

func (c refundExecConnector) Connect(context.Context) (driver.Conn, error) { return c.state, nil }
func (c refundExecConnector) Driver() driver.Driver                        { return refundExecDriver{c.state} }
func (d refundExecDriver) Open(string) (driver.Conn, error)                { return d.state, nil }
func (d *refundExecDB) Prepare(string) (driver.Stmt, error) {
	return nil, errors.New("unexpected prepare")
}
func (d *refundExecDB) Close() error              { return nil }
func (d *refundExecDB) Begin() (driver.Tx, error) { d.pending = 0; return d, nil }
func (d *refundExecDB) Commit() error             { d.committed += d.pending; d.pending = 0; return nil }
func (d *refundExecDB) Rollback() error           { d.pending = 0; return nil }
func (d *refundExecDB) QueryContext(_ context.Context, q string, args []driver.NamedValue) (driver.Rows, error) {
	if strings.HasPrefix(q, "SELECT stock,reserved,sold") {
		return singleShippingRow(int64(d.committed+d.pending), int64(0), int64(2)), nil
	}
	if strings.HasPrefix(q, "SELECT s.product_id") {
		return singleShippingRow("p", "葡萄", "家庭装", int64(d.committed+d.pending), int64(0), int64(0)), nil
	}
	return nil, errors.New("unexpected inventory query: " + q)
}
func (d *refundExecDB) ExecContext(_ context.Context, q string, args []driver.NamedValue) (driver.Result, error) {
	if d.failOrder && strings.HasPrefix(q, "UPDATE orders") {
		return nil, errors.New("order write failed")
	}
	if strings.HasPrefix(q, "UPDATE skus") {
		d.pending += int(args[0].Value.(int64))
		d.stock++
	}
	return driver.RowsAffected(1), nil
}

func TestRefundProgressTransitionsAndIdempotency(t *testing.T) {
	state := &refundExecDB{}
	db := sql.OpenDB(refundExecConnector{state})
	defer db.Close()
	rf := Refund{ID: "rf", Status: "APPROVED", Amount: 100}
	o := Order{ID: "order", PaymentStatus: "REFUNDING", RefundStatus: "APPROVED", FulfillmentStatus: "PREPARING", Quote: Quote{Total: 100}, Items: []OrderItem{{CartItem: CartItem{SKUID: "sku", Quantity: 2}}}}
	apply := func(status string) error {
		tx, e := db.Begin()
		if e != nil {
			return e
		}
		defer tx.Rollback()
		if e = persistRefundResult(context.Background(), tx, &rf, &o, status, "wx-rf"); e != nil {
			return e
		}
		return tx.Commit()
	}
	if e := apply("ABNORMAL"); e != nil {
		t.Fatal(e)
	}
	if rf.Status != "FAILED" || o.PaymentStatus != "REFUNDING" || state.stock != 0 {
		t.Fatal("abnormal result returned stock or released payment")
	}
	if e := apply("PROCESSING"); e != nil {
		t.Fatal(e)
	}
	if rf.Status != "FAILED" {
		t.Fatal("stale processing hid abnormal result")
	}
	if e := apply("SUCCESS"); e != nil {
		t.Fatal(e)
	}
	if rf.Status != "SUCCEEDED" || o.PaymentStatus != "REFUNDED" || state.committed != 2 {
		t.Fatal("refund not finalized")
	}
	for _, status := range []string{"SUCCESS", "PROCESSING", "ABNORMAL", "CLOSED"} {
		if e := apply(status); e != nil {
			t.Fatal(e)
		}
	}
	if state.stock != 1 || state.committed != 2 || o.PaymentStatus != "REFUNDED" {
		t.Fatal("duplicate or stale result changed settled refund")
	}
}

func TestRefundProgressRollbackAndUnapprovedProtection(t *testing.T) {
	state := &refundExecDB{failOrder: true}
	db := sql.OpenDB(refundExecConnector{state})
	defer db.Close()
	rf := Refund{ID: "rf", Status: "APPROVED", Amount: 100}
	o := Order{FulfillmentStatus: "PREPARING", Quote: Quote{Total: 100}, Items: []OrderItem{{CartItem: CartItem{SKUID: "sku", Quantity: 2}}}}
	tx, _ := db.Begin()
	if persistRefundResult(context.Background(), tx, &rf, &o, "SUCCESS", "wx-rf") == nil {
		t.Fatal("failed write accepted")
	}
	tx.Rollback()
	if state.committed != 0 || state.pending != 0 {
		t.Fatal("rollback leaked stock")
	}
	for _, status := range []string{"APPLIED", "REJECTED"} {
		rf = Refund{Status: status}
		if persistRefundResult(context.Background(), nil, &rf, &o, "SUCCESS", "wx-rf") == nil {
			t.Fatal("unapproved refund finalized")
		}
	}
	rf = Refund{Status: "APPROVED"}
	if persistRefundResult(context.Background(), nil, &rf, &o, "UNKNOWN", "wx-rf") == nil {
		t.Fatal("unknown status accepted")
	}
}
