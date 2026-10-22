package main

import (
	"context"
	"crypto"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha1"
	"crypto/sha256"
	"database/sql"
	"database/sql/driver"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"
)

type paymentDBState struct {
	local            *Order
	missing          []string
	locked, released bool
	status           string
	alerts           map[string]int
	alertFail        bool
}
type paymentConnector struct{ state *paymentDBState }

func (c paymentConnector) Connect(context.Context) (driver.Conn, error) {
	return paymentConnection{c.state}, nil
}
func (c paymentConnector) Driver() driver.Driver { return paymentDriver{} }

type paymentDriver struct{}

func (paymentDriver) Open(string) (driver.Conn, error) { return nil, errors.New("use connector") }

type paymentConnection struct{ state *paymentDBState }

func (c paymentConnection) Prepare(string) (driver.Stmt, error) {
	return nil, errors.New("unexpected prepare")
}
func (c paymentConnection) Close() error { return nil }
func (c paymentConnection) Begin() (driver.Tx, error) {
	return nil, errors.New("unexpected transaction")
}

type paymentRows struct {
	cols   []string
	values [][]driver.Value
}

func (r *paymentRows) Columns() []string { return r.cols }
func (r *paymentRows) Close() error      { return nil }
func (r *paymentRows) Next(out []driver.Value) error {
	if len(r.values) == 0 {
		return io.EOF
	}
	copy(out, r.values[0])
	r.values = r.values[1:]
	return nil
}
func (c paymentConnection) QueryContext(_ context.Context, q string, args []driver.NamedValue) (driver.Rows, error) {
	switch {
	case strings.Contains(q, "GET_LOCK"):
		locked := int64(1)
		if c.state.locked {
			locked = 0
		} else {
			c.state.locked = true
		}
		return &paymentRows{[]string{"lock"}, [][]driver.Value{{locked}}}, nil
	case strings.HasPrefix(q, "SELECT payment_status,total"):
		rows := &paymentRows{cols: []string{"status", "total", "tx"}}
		if o := c.state.local; o != nil {
			rows.values = [][]driver.Value{{o.PaymentStatus, int64(o.Quote.Total), o.TransactionID}}
		}
		return rows, nil
	case strings.HasPrefix(q, "SELECT order_no FROM orders"):
		rows := &paymentRows{cols: []string{"no"}}
		for _, no := range c.state.missing {
			rows.values = append(rows.values, []driver.Value{no})
		}
		return rows, nil
	}
	return nil, fmt.Errorf("unexpected query: %s", q)
}
func (c paymentConnection) ExecContext(_ context.Context, q string, args []driver.NamedValue) (driver.Result, error) {
	switch {
	case strings.Contains(q, "RELEASE_LOCK"):
		c.state.released = true
		c.state.locked = false
	case strings.HasPrefix(q, "INSERT INTO payment_reconciliations"):
		c.state.status = "RUNNING"
	case strings.HasPrefix(q, "UPDATE payment_reconciliations"):
		c.state.status = args[0].Value.(string)
	case strings.HasPrefix(q, "INSERT INTO payment_alerts"):
		if c.state.alertFail {
			return nil, errors.New("alert write failed")
		}
		target := args[1].Value.(string) + ":" + args[2].Value.(string)
		c.state.alerts[target]++
	default:
		return nil, fmt.Errorf("unexpected write: %s", q)
	}
	return driver.RowsAffected(1), nil
}

func TestPaymentReconciliationPersistsDifferencesAndRetries(t *testing.T) {
	t.Setenv("WECHAT_MCH_ID", "mch")
	t.Setenv("WECHAT_MCH_SERIAL_NO", "serial")
	key, e := rsa.GenerateKey(rand.Reader, 2048)
	if e != nil {
		t.Fatal(e)
	}
	for _, scenario := range []string{"matched", "unknownOrder", "amountMismatch", "localMissing", "providerFailed", "alertFailed", "locked"} {
		t.Run(scenario, func(t *testing.T) {
			date := time.Now().In(paymentZone).AddDate(0, 0, -1).Format("2006-01-02")
			state := &paymentDBState{local: &Order{PaymentStatus: "PAID", TransactionID: "WX123", Quote: Quote{Total: 8990}}, alerts: map[string]int{}}
			if scenario == "unknownOrder" {
				state.local = nil
			}
			if scenario == "amountMismatch" {
				state.local.Quote.Total = 100
			}
			if scenario == "localMissing" {
				state.missing = []string{"CJ-other"}
			}
			if scenario == "alertFailed" {
				state.local = nil
				state.alertFail = true
			}
			if scenario == "locked" {
				state.locked = true
			}
			db := sql.OpenDB(paymentConnector{state})
			defer db.Close()
			db.SetMaxOpenConns(3)
			w := &Wechat{privateKey: key, publicKey: &key.PublicKey}
			w.client = &http.Client{Transport: shippingTransport(func(r *http.Request) (*http.Response, error) {
				if scenario == "locked" {
					t.Fatal("called provider without lock")
				}
				if scenario == "providerFailed" {
					return nil, errors.New("network failed")
				}
				if r.URL.Path == "/v3/bill/tradebill" {
					hash := sha1.Sum([]byte(successBill))
					raw := fmt.Sprintf(`{"download_url":"https://api.mch.weixin.qq.com/v3/billdownload/file?token=test","hash_type":"SHA1","hash_value":"%s"}`, hex.EncodeToString(hash[:]))
					digest := sha256.Sum256([]byte("123\nnonce\n" + raw + "\n"))
					sig, err := rsa.SignPKCS1v15(rand.Reader, key, crypto.SHA256, digest[:])
					if err != nil {
						t.Fatal(err)
					}
					h := http.Header{}
					h.Set("Wechatpay-Timestamp", "123")
					h.Set("Wechatpay-Nonce", "nonce")
					h.Set("Wechatpay-Signature", base64.StdEncoding.EncodeToString(sig))
					return &http.Response{StatusCode: 200, Header: h, Body: io.NopCloser(strings.NewReader(raw))}, nil
				}
				return &http.Response{StatusCode: 200, Header: http.Header{}, Body: io.NopCloser(strings.NewReader(successBill))}, nil
			})}
			server := &Server{store: &Store{db: db}, wechat: w}
			for i := 0; i < 2; i++ {
				result, err := server.reconcilePayments(context.Background(), date)
				if scenario == "locked" {
					if err == nil {
						t.Fatal("concurrent run accepted")
					}
					return
				}
				if err != nil {
					t.Fatal(err)
				}
				run := result.(M)
				want := "DIFFERENCES"
				if scenario == "matched" {
					want = "MATCHED"
				}
				if scenario == "providerFailed" || scenario == "alertFailed" {
					want = "FAILED"
				}
				if run["status"] != want || state.status != want || !state.released {
					t.Fatalf("result=%v state=%+v", run, state)
				}
			}
			if scenario == "unknownOrder" || scenario == "amountMismatch" {
				if state.alerts["BILL_DIFFERENCE:"+date+":CJ123"] != 2 {
					t.Fatal("duplicate alert was not merged", state.alerts)
				}
			}
			if scenario == "localMissing" {
				if state.alerts["BILL_DIFFERENCE:"+date+":CJ-other"] != 2 {
					t.Fatal("reverse difference missed", state.alerts)
				}
			}
			if scenario == "providerFailed" {
				if state.alerts["RECONCILIATION_FAILED:"+date] != 2 {
					t.Fatal("failed task did not alert", state.alerts)
				}
			}
		})
	}
}

const successBill = "交易时间,微信订单号,商户订单号,交易状态,订单金额\n`2026-10-04 12:00:00,`WX123,`CJ123,`SUCCESS,`89.90\n总交易单数,订单总金额\n`1,`89.90\n"

func TestPaymentBillParsing(t *testing.T) {
	result, err := parsePaymentBill([]byte("\ufeff" + successBill))
	if err != nil || len(result) != 1 || result[0].Total != 8990 || result[0].Transaction != "WX123" {
		t.Fatalf("%+v %v", result, err)
	}
	for _, bad := range []string{"", strings.Replace(successBill, "订单金额", "应结订单金额", 1), strings.Replace(successBill, "`SUCCESS", "`REFUND", 1), strings.Replace(successBill, "`89.90", "`89.901", 1), strings.Replace(successBill, "`CJ123", "`", 1), strings.Replace(successBill, "总交易单数", "`time,`WX123,`CJ123,`SUCCESS,`89.90\n总交易单数", 1)} {
		if _, err = parsePaymentBill([]byte(bad)); err == nil {
			t.Fatalf("invalid bill accepted: %s", bad)
		}
	}
	for value, want := range map[string]int{"0.01": 1, "`89.90": 8990, "10": 1000, "1.1": 110} {
		got, e := billMoney(value)
		if e != nil || got != want {
			t.Fatalf("%s=%d %v", value, got, e)
		}
	}
	for _, value := range []string{"-1", "NaN", "1.123", "9999999999", "1.-1", "1.+1", "+1", "1."} {
		if _, e := billMoney(value); e == nil {
			t.Fatalf("bad amount: %s", value)
		}
	}
}

func TestPaymentBillDateUsesBeijing(t *testing.T) {
	now := time.Date(2026, 10, 4, 17, 0, 0, 0, time.UTC)
	got, err := paymentBillDate("", now)
	if err != nil || got != "2026-10-04" {
		t.Fatalf("%s %v", got, err)
	}
	for _, bad := range []string{"2026-10-05", "2026-10-06", "2026-06-01", "2026-09-31", "2026-9-01"} {
		if _, e := paymentBillDate(bad, now); e == nil {
			t.Fatalf("invalid date %s", bad)
		}
	}
}

func TestPaymentDifferences(t *testing.T) {
	p := billPayment{No: "CJ123", Transaction: "WX123", Total: 8990}
	order := Order{PaymentStatus: "PAID", TransactionID: "WX123", Quote: Quote{Total: 8990}}
	if paymentDifference(&order, p) != "" {
		t.Fatal("matched payment reported as mismatch")
	}
	if paymentDifference(nil, p) == "" {
		t.Fatal("missing order accepted")
	}
	for _, mutate := range []func(*Order){func(o *Order) { o.Quote.Total++ }, func(o *Order) { o.PaymentStatus = "UNPAID" }, func(o *Order) { o.TransactionID = "other" }} {
		copy := order
		mutate(&copy)
		if paymentDifference(&copy, p) == "" {
			t.Fatal("difference missed")
		}
	}
}

func TestVerifiedPaymentRejectsUnsafeResultsBeforeDB(t *testing.T) {
	t.Setenv("WECHAT_MCH_ID", "mch")
	t.Setenv("WECHAT_APP_ID", "app")
	order := Order{OrderNo: "CJ123", PaymentStatus: "UNPAID", Quote: Quote{Total: 8990}}
	valid := M{"amount": map[string]any{"total": float64(8990), "currency": "CNY"}, "trade_state": "SUCCESS", "transaction_id": "WX123", "success_time": "2026-10-04T12:00:00+08:00", "appid": "app", "mchid": "mch", "out_trade_no": "CJ123"}
	for _, key := range []string{"amount", "transaction_id", "success_time", "appid", "mchid", "out_trade_no", "trade_state"} {
		copy := M{}
		for k, v := range valid {
			copy[k] = v
		}
		delete(copy, key)
		if err := (&Server{}).markVerifiedPayment(context.Background(), &order, copy); err == nil {
			t.Fatalf("missing %s accepted", key)
		}
	}
	valid["amount"] = map[string]any{"total": float64(8990.5)}
	if err := (&Server{}).markVerifiedPayment(context.Background(), &order, valid); err == nil {
		t.Fatal("fractional fen accepted")
	}
}

func TestWechatBillDownloadIntegrityAndHost(t *testing.T) {
	t.Setenv("WECHAT_MCH_ID", "mch")
	t.Setenv("WECHAT_MCH_SERIAL_NO", "serial")
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatal(err)
	}
	hash := sha1.Sum([]byte(successBill))
	for _, scenario := range []string{"valid", "badHash", "badHost", "redirect", "noStatement"} {
		t.Run(scenario, func(t *testing.T) {
			calls := 0
			w := &Wechat{privateKey: key, publicKey: &key.PublicKey}
			w.client = &http.Client{Transport: shippingTransport(func(r *http.Request) (*http.Response, error) {
				calls++
				if !strings.HasPrefix(r.Header.Get("Authorization"), "WECHATPAY2-SHA256-RSA2048") {
					t.Fatal("unsigned request")
				}
				if calls == 1 {
					if r.URL.Query().Get("bill_type") != "SUCCESS" || r.URL.Query().Get("bill_date") != "2026-10-04" {
						t.Fatal("wrong bill query")
					}
					if scenario == "noStatement" {
						return &http.Response{StatusCode: 400, Body: io.NopCloser(strings.NewReader(`{"code":"NO_STATEMENT_EXIST"}`)), Header: http.Header{}}, nil
					}
					host := "api.mch.weixin.qq.com"
					if scenario == "badHost" {
						host = "evil.example"
					}
					expected := hex.EncodeToString(hash[:])
					if scenario == "badHash" {
						expected = strings.Repeat("0", 40)
					}
					raw := fmt.Sprintf(`{"download_url":"https://%s/v3/billdownload/file?token=secret","hash_type":"SHA1","hash_value":"%s"}`, host, expected)
					ts := "123"
					nonce := "nonce"
					digest := sha256.Sum256([]byte(ts + "\n" + nonce + "\n" + raw + "\n"))
					signature, e := rsa.SignPKCS1v15(rand.Reader, key, crypto.SHA256, digest[:])
					if e != nil {
						t.Fatal(e)
					}
					header := http.Header{}
					header.Set("Wechatpay-Timestamp", ts)
					header.Set("Wechatpay-Nonce", nonce)
					header.Set("Wechatpay-Signature", base64.StdEncoding.EncodeToString(signature))
					return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader(raw)), Header: header}, nil
				}
				if calls > 2 {
					t.Fatal("followed bill redirect")
				}
				if scenario == "redirect" {
					return &http.Response{StatusCode: 302, Header: http.Header{"Location": []string{"https://evil.example/bill"}}, Body: io.NopCloser(strings.NewReader(""))}, nil
				}
				return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader(successBill)), Header: http.Header{}}, nil
			})}
			payments, e := w.paymentBill("2026-10-04")
			if scenario == "valid" {
				if e != nil || len(payments) != 1 {
					t.Fatalf("%+v %v", payments, e)
				}
			} else if scenario == "noStatement" {
				if e != nil || len(payments) != 0 || calls != 1 {
					t.Fatal("empty trading day failed", e)
				}
			} else if e == nil {
				t.Fatal("unsafe download accepted")
			}
			if scenario == "badHost" && calls != 1 {
				t.Fatal("contacted unsafe host")
			}
		})
	}
}
