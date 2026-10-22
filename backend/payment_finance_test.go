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
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"
)

const refundFinanceBill = "商户订单号,商户退款单号,微信退款单号,退款状态,申请退款金额,退款金额,手续费\n`CJ123,`RF123,`WXRF123,`SUCCESS,`89.90,`89.90,`-0.54\n总交易单数,退款总金额,手续费总金额,申请退款总金额\n`1,`89.90,`-0.54,`89.90\n"
const fundsFinanceBill = "记账时间,资金流水单号,业务类型,收支类型,收支金额(元),账户结余(元)\n`2026-10-07 10:00:00,`F1,`交易,`收入,`100.00,`100.00\n`2026-10-07 10:00:01,`F2,`扣除交易手续费,`支出,`0.60,`99.40\n`2026-10-07 10:00:02,`F3,`退款,`支出,`50.00,`49.40\n`2026-10-07 10:00:03,`F4,`退还交易手续费,`收入,`0.30,`49.70\n资金流水总笔数,收入笔数,收入金额,支出笔数,支出金额\n`4,`2,`100.30,`2,`50.60\n"

func TestPaymentFinanceParsingAndTotals(t *testing.T) {
	for _, kind := range []string{"REFUND", "BASIC"} {
		raw := refundFinanceBill
		if kind == "BASIC" {
			raw = fundsFinanceBill
		}
		rows, err := parseFinanceBill([]byte("\ufeff"+raw), kind)
		if err != nil {
			t.Fatal(err)
		}
		totals := financeTotals(rows, kind)
		if kind == "REFUND" {
			if totals["netFees"] != -54 || totals["refundRequested"] != 8990 {
				t.Fatal(totals)
			}
		} else if totals["net"] != 4970 || totals["settlementNet"] != 4970 || totals["netFees"] != 30 {
			t.Fatal(totals)
		}
		for _, bad := range []string{strings.Replace(raw, "`1,`89.90", "`2,`89.90", 1), strings.Replace(raw, "`89.90,`-0.54,`89.90", "`90.00,`-0.54,`89.90", 1), strings.Replace(raw, "`50.60", "`50.61", 1), strings.Replace(raw, "收入金额", "未知金额", 1), strings.Replace(raw, "申请退款总金额", "未知金额", 1), raw + "extra\n", strings.Split(raw, "\n总")[0], strings.Replace(raw, "89.90", "89.901", 1), strings.Replace(raw, "`支出", "`未知", 1), strings.Replace(raw, "`SUCCESS", "`UNKNOWN", 1)} {
			if bad == raw {
				continue
			}
			if _, e := parseFinanceBill([]byte(bad), kind); e == nil {
				t.Fatalf("accepted malformed %s: %s", kind, bad)
			}
		}
	}
	if _, err := parseFinanceBill([]byte(strings.Replace(fundsFinanceBill, "`F4", "`F1", 1)), "BASIC"); err == nil {
		t.Fatal("duplicate accepted")
	}
	rows, e := parseFinanceBill(nil, "REFUND")
	if e != nil || len(rows) != 0 {
		t.Fatal(rows, e)
	}
}
func TestPaymentFinanceRefundSnapshot(t *testing.T) {
	rows, _ := parseFinanceBill([]byte(refundFinanceBill), "REFUND")
	row := rows[0]
	local := &Refund{OrderNo: "CJ123", Amount: 8990, ProviderRefundID: "WXRF123", Status: "SUCCEEDED"}
	if refundBillDifference(local, row) != "" {
		t.Fatal("matched refund rejected")
	}
	row["退款状态"] = "PROCESSING"
	if refundBillDifference(local, row) != "" {
		t.Fatal("later local success rejected")
	}
	row["退款状态"] = "SUCCESS"
	local.Status = "APPROVED"
	if refundBillDifference(local, row) == "" {
		t.Fatal("missed status mismatch")
	}
	row["退款状态"] = "CLOSED"
	if refundBillDifference(local, row) == "" {
		t.Fatal("missed closed state mismatch")
	}
	local.Status = "FAILED"
	if refundBillDifference(local, row) != "" {
		t.Fatal("matched failed state rejected")
	}
	row["退款状态"] = "SUCCESS"
	local.Status = "SUCCEEDED"
	local.Amount = 1
	if refundBillDifference(local, row) == "" {
		t.Fatal("missed amount mismatch")
	}
	if refundBillDifference(nil, row) == "" {
		t.Fatal("unknown refund missed")
	}
	for _, status := range []string{"REFUNDING", "REFUNDED"} {
		if paymentDifference(&Order{PaymentStatus: status, TransactionID: "WX", Quote: Quote{Total: 100}}, billPayment{No: "CJ", Transaction: "WX", Total: 100}) != "" {
			t.Fatal("refund state hid original payment")
		}
	}
}

type financeDBState struct {
	paymentDBState
	refund         *Refund
	missingRefunds []string
	payload        M
}
type financeConnector struct{ state *financeDBState }

func (c financeConnector) Connect(context.Context) (driver.Conn, error) {
	return financeConnection{c.state}, nil
}
func (c financeConnector) Driver() driver.Driver { return paymentDriver{} }

type financeConnection struct{ state *financeDBState }

func (c financeConnection) Prepare(string) (driver.Stmt, error) {
	return nil, errors.New("unexpected prepare")
}
func (c financeConnection) Close() error { return nil }
func (c financeConnection) Begin() (driver.Tx, error) {
	return nil, errors.New("unexpected transaction")
}
func (c financeConnection) QueryContext(ctx context.Context, q string, args []driver.NamedValue) (driver.Rows, error) {
	if strings.HasPrefix(q, "SELECT order_no,amount,status") {
		rows := &paymentRows{cols: []string{"order", "amount", "status", "provider"}}
		if r := c.state.refund; r != nil {
			rows.values = [][]driver.Value{{r.OrderNo, int64(r.Amount), r.Status, r.ProviderRefundID}}
		}
		return rows, nil
	}
	if strings.HasPrefix(q, "SELECT id FROM refunds WHERE accepted_at") {
		rows := &paymentRows{cols: []string{"id"}}
		for _, id := range c.state.missingRefunds {
			rows.values = append(rows.values, []driver.Value{id})
		}
		return rows, nil
	}
	return (paymentConnection{&c.state.paymentDBState}).QueryContext(ctx, q, args)
}
func (c financeConnection) ExecContext(ctx context.Context, q string, args []driver.NamedValue) (driver.Result, error) {
	if strings.HasPrefix(q, "INSERT INTO payment_finance_runs") {
		c.state.status = "RUNNING"
		return driver.RowsAffected(1), nil
	}
	if strings.HasPrefix(q, "UPDATE payment_finance_runs") {
		c.state.status = args[0].Value.(string)
		if err := json.Unmarshal([]byte(args[1].Value.(string)), &c.state.payload); err != nil {
			return nil, err
		}
		return driver.RowsAffected(1), nil
	}
	return (paymentConnection{&c.state.paymentDBState}).ExecContext(ctx, q, args)
}
func TestPaymentFinancePersistsRetriesAndFailures(t *testing.T) {
	key, e := rsa.GenerateKey(rand.Reader, 2048)
	if e != nil {
		t.Fatal(e)
	}
	t.Setenv("WECHAT_MCH_ID", "mch")
	t.Setenv("WECHAT_MCH_SERIAL_NO", "serial")
	for _, scenario := range []string{"matched", "amount", "missing", "providerFailed", "alertFailed", "locked", "funds", "empty"} {
		t.Run(scenario, func(t *testing.T) {
			state := &financeDBState{paymentDBState: paymentDBState{alerts: map[string]int{}}, refund: &Refund{OrderNo: "CJ123", Amount: 8990, ProviderRefundID: "WXRF123", Status: "SUCCEEDED"}}
			kind := "REFUND"
			raw := refundFinanceBill
			switch scenario {
			case "amount":
				state.refund.Amount = 1
			case "missing":
				state.missingRefunds = []string{"RFOTHER"}
			case "alertFailed":
				state.refund = nil
				state.alertFail = true
			case "locked":
				state.locked = true
			case "funds":
				kind = "BASIC"
				raw = fundsFinanceBill
			}
			db := sql.OpenDB(financeConnector{state})
			defer db.Close()
			db.SetMaxOpenConns(3)
			w := &Wechat{privateKey: key, publicKey: &key.PublicKey}
			w.client = &http.Client{Transport: shippingTransport(func(r *http.Request) (*http.Response, error) {
				if scenario == "locked" {
					t.Fatal("provider called without lock")
				}
				if scenario == "providerFailed" {
					return nil, errors.New("network")
				}
				if strings.HasSuffix(r.URL.Path, "billdownload/file") {
					return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader(raw))}, nil
				}
				if kind == "REFUND" && r.URL.Query().Get("bill_type") != "REFUND" || kind == "BASIC" && r.URL.Query().Get("account_type") != "BASIC" {
					t.Fatal(r.URL)
				}
				hash := sha1.Sum([]byte(raw))
				body := fmt.Sprintf(`{"download_url":"https://api.mch.weixin.qq.com/v3/billdownload/file","hash_type":"SHA1","hash_value":"%s"}`, hex.EncodeToString(hash[:]))
				code := 200
				if scenario == "empty" {
					code = 400
					body = `{"code":"NO_STATEMENT_EXIST","message":"none"}`
				}
				digest := sha256.Sum256([]byte("123\nnonce\n" + body + "\n"))
				sig, e := rsa.SignPKCS1v15(rand.Reader, key, crypto.SHA256, digest[:])
				if e != nil {
					t.Fatal(e)
				}
				headers := http.Header{}
				headers.Set("Wechatpay-Timestamp", "123")
				headers.Set("Wechatpay-Nonce", "nonce")
				headers.Set("Wechatpay-Signature", base64.StdEncoding.EncodeToString(sig))
				return &http.Response{StatusCode: code, Header: headers, Body: io.NopCloser(strings.NewReader(body))}, nil
			})}
			s := &Server{store: &Store{db: db}, wechat: w}
			date := time.Now().In(paymentZone).AddDate(0, 0, -1).Format("2006-01-02")
			for i := 0; i < 2; i++ {
				result, err := s.reconcileFinance(context.Background(), date, kind)
				if scenario == "locked" {
					if err == nil {
						t.Fatal("lock ignored")
					}
					return
				}
				if err != nil {
					t.Fatal(err)
				}
				run := result.(M)
				want := "MATCHED"
				if scenario == "amount" || scenario == "missing" {
					want = "DIFFERENCES"
				}
				if scenario == "providerFailed" || scenario == "alertFailed" {
					want = "FAILED"
				}
				if scenario == "funds" {
					want = "COMPLETED"
				}
				if scenario == "empty" {
					want = "MATCHED"
				}
				if run["status"] != want || state.status != want || !state.released {
					t.Fatal(run, state)
				}
				if want == "FAILED" {
					if _, ok := run["totals"]; ok {
						t.Fatal("failed totals published")
					}
				} else {
					if _, ok := run["totals"]; !ok {
						t.Fatal("totals missing")
					}
				}
			}
			if scenario == "missing" && state.alerts["REFUND_BILL_DIFFERENCE:"+date+":RFOTHER"] != 2 {
				t.Fatal("missing refund not alerted")
			}
		})
	}
}
