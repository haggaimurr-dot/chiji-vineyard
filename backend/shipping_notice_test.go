package main

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"reflect"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func configureShippingTest(t *testing.T) {
	t.Helper()
	t.Setenv("WECHAT_SHIPPING_TEMPLATE_ID", "test-template")
	t.Setenv("WECHAT_SHIPPING_TEMPLATE_FIELDS", `{"character_string1":"orderNo","thing2":"carrier","character_string3":"trackingNo","time4":"shippedAt","thing5":"product"}`)
	t.Setenv("WECHAT_MINIPROGRAM_STATE", "trial")
	t.Setenv("WECHAT_APP_ID", "test-app")
	t.Setenv("WECHAT_APP_SECRET", "test-secret")
}

func TestShippingMessageConfiguration(t *testing.T) {
	configureShippingTest(t)
	c, err := shippingConfig()
	if err != nil {
		t.Fatal(err)
	}
	o := Order{ID: "o1", OpenID: "owner", OrderNo: "CJ123", Shipment: &Shipment{Carrier: "顺丰速运", TrackingNo: "SF123", ShippedAt: "2026-10-04T01:02:00Z"}, Items: []OrderItem{{Name: strings.Repeat("葡", 30)}}}
	body := shippingMessage(o, c)
	data := body["data"].(M)
	if body["touser"] != "owner" || body["page"] != "pages/order-detail/index?id=o1" || body["miniprogram_state"] != "trial" {
		t.Fatal(body)
	}
	if data["time4"].(M)["value"] != "2026-10-04 09:02" || len([]rune(data["thing5"].(M)["value"].(string))) != 20 {
		t.Fatal(data)
	}
	for _, raw := range []string{`{}`, `{"thing1":"orderNo"}`, `{"time1":"carrier"}`, `{"thing1":"unknown"}`, `{"number1":"trackingNo"}`} {
		t.Setenv("WECHAT_SHIPPING_TEMPLATE_FIELDS", raw)
		if _, err = shippingConfig(); err == nil {
			t.Fatalf("invalid mapping accepted: %s", raw)
		}
	}
}

func TestOrderSearchScopesBeforePagination(t *testing.T) {
	query, args := orderSearchQuery("owner", OrderSearch{Keyword: " CJ123  阳光 %_ ", Filter: "paid", Limit: 20, Offset: 100})
	if !strings.Contains(query, "openid=?") || !strings.Contains(query, "payment_status='PAID'") || strings.Count(query, "LOCATE(") != 3 || !strings.HasSuffix(query, "LIMIT ? OFFSET ?") {
		t.Fatal(query)
	}
	if !reflect.DeepEqual(args, []any{"owner", "cj123", "阳光", "%_", 20, 100}) {
		t.Fatal(args)
	}
	_, args = orderSearchQuery("owner", OrderSearch{Limit: 999, Offset: -5})
	if args[len(args)-2] != 100 || args[len(args)-1] != 0 {
		t.Fatal(args)
	}
}

// In-memory SQL driver validates transaction boundaries and the send claim.
// Each connection uses the same fixture; MaxOpenConns(1) serializes SQL operations.
type shippingFixture struct {
	order                       Order
	status, template, pushError string
	notified                    bool
	failNotice                  bool
	noticeWrites                int
	auditActions                []string
	reservedOwner               string
	snapshot                    *shippingFixture
}
type shippingConnector struct{ state *shippingFixture }

func (c shippingConnector) Connect(context.Context) (driver.Conn, error) { return c.state, nil }
func (c shippingConnector) Driver() driver.Driver                        { return shippingDriver{c.state} }

type shippingDriver struct{ state *shippingFixture }

func (d shippingDriver) Open(string) (driver.Conn, error) { return d.state, nil }
func (d *shippingFixture) Close() error                   { return nil }
func (d *shippingFixture) Prepare(string) (driver.Stmt, error) {
	return nil, errors.New("unexpected prepare")
}
func (d *shippingFixture) Begin() (driver.Tx, error) {
	saved := *d
	saved.snapshot = nil
	d.snapshot = &saved
	return d, nil
}
func (d *shippingFixture) Commit() error { d.snapshot = nil; return nil }
func (d *shippingFixture) Rollback() error {
	if d.snapshot != nil {
		saved := *d.snapshot
		*d = saved
	}
	return nil
}

type shippingRows struct {
	cols   []string
	values [][]driver.Value
}

func (r *shippingRows) Columns() []string { return r.cols }
func (r *shippingRows) Close() error      { return nil }
func (r *shippingRows) Next(out []driver.Value) error {
	if len(r.values) == 0 {
		return io.EOF
	}
	copy(out, r.values[0])
	r.values = r.values[1:]
	return nil
}
func singleShippingRow(values ...driver.Value) driver.Rows {
	cols := make([]string, len(values))
	for i := range cols {
		cols[i] = "value"
	}
	return &shippingRows{cols: cols, values: [][]driver.Value{values}}
}
func (d *shippingFixture) QueryContext(_ context.Context, q string, args []driver.NamedValue) (driver.Rows, error) {
	switch {
	case strings.HasPrefix(q, "SELECT COUNT(*) FROM product_batches"):
		return singleShippingRow(int64(0)), nil
	case strings.Contains(q, "FROM order_waybills"):
		return &shippingRows{cols: make([]string, 13)}, nil
	case strings.HasPrefix(q, "SELECT order_id FROM shipment_numbers"):
		return singleShippingRow(d.reservedOwner), nil
	case strings.HasPrefix(q, "SELECT id FROM orders WHERE id<>"):
		return &shippingRows{cols: []string{"id"}}, nil
	case strings.Contains(q, "FROM invoice_requests"):
		return &shippingRows{cols: []string{"payload"}}, nil
	case strings.HasPrefix(q, "SELECT order_no,openid"):
		if args[0].Value != d.order.ID {
			return &shippingRows{cols: make([]string, 11)}, nil
		}
		raw, _ := json.Marshal(d.order)
		return singleShippingRow(d.order.OrderNo, d.order.OpenID, d.order.OrderStatus, d.order.PaymentStatus, d.order.FulfillmentStatus, d.order.RefundStatus, raw, false, "", time.Now().Add(time.Hour), time.Now()), nil
	case strings.HasPrefix(q, "SELECT push_status,push_error"):
		if d.status == "" {
			return &shippingRows{cols: []string{"status", "error"}}, nil
		}
		return singleShippingRow(d.status, d.pushError), nil
	case strings.HasPrefix(q, "SELECT push_status"):
		if d.status == "" {
			return &shippingRows{cols: []string{"status"}}, nil
		}
		return singleShippingRow(d.status), nil
	default:
		return nil, errors.New("unexpected query: " + q)
	}
}
func (d *shippingFixture) ExecContext(_ context.Context, q string, args []driver.NamedValue) (driver.Result, error) {
	switch {
	case strings.HasPrefix(q, "INSERT IGNORE INTO shipment_numbers"):
		if d.snapshot == nil {
			return nil, errors.New("parcel reservation must be transactional")
		}
		if d.reservedOwner == "" {
			d.reservedOwner = args[2].Value.(string)
		}
	case strings.HasPrefix(q, "UPDATE orders SET fulfillment_status='SHIPPED'"):
		if d.snapshot == nil {
			return nil, errors.New("shipping must be transactional")
		}
		var shipment Shipment
		if err := json.Unmarshal([]byte(args[0].Value.(string)), &shipment); err != nil {
			return nil, err
		}
		d.order.FulfillmentStatus = "SHIPPED"
		d.order.Shipment = &shipment
	case strings.HasPrefix(q, "INSERT INTO shipping_notifications"):
		if d.failNotice {
			return nil, errors.New("notice insert failed")
		}
		if d.snapshot == nil {
			return nil, errors.New("notice must be transactional")
		}
		d.noticeWrites++
		if strings.Contains(q, "notified_at") {
			d.notified = true
			if d.status == "" {
				d.status = "NONE"
			}
		} else {
			d.status = "READY"
			d.template = args[2].Value.(string)
		}
	case strings.HasPrefix(q, "UPDATE orders SET payload=JSON_SET"):
		if strings.Contains(q, "AND JSON_UNQUOTE") && (d.order.Shipment == nil || d.order.Shipment.SyncAttempt != args[2].Value) {
			return driver.RowsAffected(0), nil
		}
		var shipment Shipment
		if err := json.Unmarshal([]byte(args[0].Value.(string)), &shipment); err != nil {
			return nil, err
		}
		d.order.Shipment = &shipment
	case strings.HasPrefix(q, "UPDATE shipping_notifications SET push_status='SENDING'"):
		if !d.notified || args[1].Value != d.template || (d.status != "READY" && d.status != "FAILED") {
			return driver.RowsAffected(0), nil
		}
		d.status = "SENDING"
	case strings.HasPrefix(q, "UPDATE shipping_notifications SET push_status=?"):
		d.status = args[0].Value.(string)
		d.pushError = args[1].Value.(string)
	case strings.HasPrefix(q, "INSERT INTO audit_logs"):
		d.auditActions = append(d.auditActions, args[2].Value.(string))
	default:
		return nil, errors.New("unexpected exec: " + q)
	}
	return driver.RowsAffected(1), nil
}

type shippingTransport func(*http.Request) (*http.Response, error)

func (f shippingTransport) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }
func shippingResponse(raw string) *http.Response {
	return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader(raw)), Header: make(http.Header)}
}
func shippingTestServer(t *testing.T, state *shippingFixture, transport shippingTransport) *Server {
	t.Helper()
	db := sql.OpenDB(shippingConnector{state})
	db.SetMaxOpenConns(1)
	t.Cleanup(func() { db.Close() })
	return &Server{store: &Store{db: db}, wechat: &Wechat{client: &http.Client{Transport: transport, Timeout: time.Second}}}
}
func shippedFixture() *shippingFixture {
	return &shippingFixture{order: Order{ID: "o1", OrderNo: "CJ1", OpenID: "owner", OrderStatus: "CONFIRMED", PaymentStatus: "PAID", FulfillmentStatus: "SHIPPED", RefundStatus: "NONE", Shipment: &Shipment{Carrier: "快递", TrackingNo: "T123", ShippedAt: nowString()}}, status: "READY", template: "test-template", notified: true}
}

func TestShippingPushClaimAndFailureHandling(t *testing.T) {
	configureShippingTest(t)
	for _, scenario := range []struct {
		name, reply, want string
		network           bool
	}{{"success", `{"errcode":0}`, "SENT", false}, {"rejected", `{"errcode":40037,"errmsg":"invalid template"}`, "FAILED", false}, {"permission", `{"errcode":43101,"errmsg":"not subscribed"}`, "BLOCKED", false}, {"timeout", "", "UNCERTAIN", true}, {"missing code", `{}`, "UNCERTAIN", false}} {
		t.Run(scenario.name, func(t *testing.T) {
			state := shippedFixture()
			var sends atomic.Int32
			s := shippingTestServer(t, state, func(r *http.Request) (*http.Response, error) {
				if r.URL.Path == "/cgi-bin/token" {
					return shippingResponse(`{"access_token":"test-token"}`), nil
				}
				sends.Add(1)
				if scenario.network {
					return nil, errors.New("network lost")
				}
				return shippingResponse(scenario.reply), nil
			})
			result, err := s.sendShippingNotice(context.Background(), "o1")
			if err != nil || result["status"] != scenario.want {
				t.Fatalf("%v %v", result, err)
			}
			if state.status != scenario.want || sends.Load() != 1 {
				t.Fatalf("status=%s sends=%d", state.status, sends.Load())
			}
			if scenario.want != "FAILED" {
				_, err = s.sendShippingNotice(context.Background(), "o1")
				if err != nil || sends.Load() != 1 {
					t.Fatal("duplicate send", err)
				}
			}
		})
	}
	state := shippedFixture()
	var sends atomic.Int32
	s := shippingTestServer(t, state, func(r *http.Request) (*http.Response, error) {
		if r.URL.Path == "/cgi-bin/token" {
			return shippingResponse(`{"access_token":"token"}`), nil
		}
		sends.Add(1)
		time.Sleep(20 * time.Millisecond)
		return shippingResponse(`{"errcode":0}`), nil
	})
	var wg sync.WaitGroup
	for range 4 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if _, err := s.sendShippingNotice(context.Background(), "o1"); err != nil {
				t.Error(err)
			}
		}()
	}
	wg.Wait()
	if sends.Load() != 1 {
		t.Fatalf("concurrent sends: %d", sends.Load())
	}
}

func TestShippingNotificationAtomicityAndOwnership(t *testing.T) {
	t.Setenv("WECHAT_SHIPPING_TEMPLATE_ID", "")
	t.Setenv("WECHAT_APP_ID", "")
	t.Setenv("WECHAT_APP_SECRET", "")
	state := shippedFixture()
	state.order.Shipment = nil
	state.order.FulfillmentStatus = "PREPARING"
	state.status = ""
	state.notified = false
	state.failNotice = true
	s := shippingTestServer(t, state, func(*http.Request) (*http.Response, error) { t.Fatal("unexpected provider call"); return nil, nil })
	raw := json.RawMessage(`{"orderId":"o1","carrier":"快递","carrierCode":"SF","trackingNo":"T123"}`)
	if _, err := s.shipOrder(context.Background(), Claims{Subject: "admin"}, raw, false); err == nil {
		t.Fatal("notice failure accepted")
	}
	if state.order.Shipment != nil || state.order.FulfillmentStatus != "PREPARING" || state.notified {
		t.Fatal("partial shipping persisted")
	}
	state.failNotice = false
	if _, err := s.shipOrder(context.Background(), Claims{Subject: "admin"}, raw, false); err != nil {
		t.Fatal(err)
	}
	if !state.notified || state.order.FulfillmentStatus != "SHIPPED" || state.order.Shipment.SyncStatus != "FAILED" {
		t.Fatal("in-app notice lost on provider failure")
	}
	if _, err := s.shipOrder(context.Background(), Claims{Subject: "admin"}, raw, false); err == nil {
		t.Fatal("repeat shipping accepted")
	}
	if _, err := s.shippingSubscription(context.Background(), "other", "o1", true); err == nil {
		t.Fatal("other user's subscription accepted")
	}
}
