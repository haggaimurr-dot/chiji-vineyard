package main

import (
	"context"
	"crypto/md5"
	"database/sql"
	"database/sql/driver"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

const waybillTestPNG = "iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAQAAAC1HAwCAAAAC0lEQVR42mP8/x8AAwMCAO+aQ1cAAAAASUVORK5CYII="

func configureWaybillTest(t *testing.T) {
	t.Helper()
	t.Setenv("KUAIDI100_KEY", "waybill-key")
	t.Setenv("KUAIDI100_SECRET", "waybill-secret")
	t.Setenv("KUAIDI100_SENDER_NAME", "寄件人")
	t.Setenv("KUAIDI100_SENDER_PHONE", "13800000000")
	t.Setenv("KUAIDI100_SENDER_ADDRESS", "浙江省杭州市西湖区果园路1号")
	t.Setenv("KUAIDI100_WAYBILL_ACCOUNTS", `{"JD":{"partnerId":"merchant-secret-id","tempId":"jd-v2-template","expType":"标准快递","payType":"MONTHLY"}}`)
	oldHTTP, oldImageHTTP := waybillHTTP, waybillImageHTTP
	t.Cleanup(func() { waybillHTTP, waybillImageHTTP = oldHTTP, oldImageHTTP })
	waybillImageHTTP = &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
		if r.Method != "GET" || r.URL.String() != "https://api.kuaidi100.com/label/example" {
			t.Errorf("unexpected image request: %s %s", r.Method, r.URL)
		}
		content, _ := base64.StdEncoding.DecodeString(waybillTestPNG)
		return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader(string(content))), Header: make(http.Header)}, nil
	})}
}

func waybillTestOrder() Order {
	return Order{ID: "o1", OrderNo: "CJ123", OpenID: "owner", OrderStatus: "CONFIRMED", PaymentStatus: "PAID", FulfillmentStatus: "PREPARING", RefundStatus: "NONE", Address: Address{Name: "收件人", Phone: "13800001234", Province: "浙江省", City: "杭州市", District: "西湖区", Detail: "测试路1号"}}
}

func waybillResponse(body string) *http.Response {
	return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader(body)), Header: make(http.Header)}
}

const successfulWaybillResponse = `{"success":true,"code":200,"data":{"kuaidinum":"JD12345678","taskId":"provider-task","label":"http://api.kuaidi100.com/label/example"}}`

func TestWaybillProviderV2Contract(t *testing.T) {
	configureWaybillTest(t)
	waybillHTTP = &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
		if r.Method != "POST" || r.URL.String() != "https://api.kuaidi100.com/label/order" {
			t.Fatal("wrong provider endpoint")
		}
		if err := r.ParseForm(); err != nil {
			t.Fatal(err)
		}
		param := r.Form.Get("param")
		sum := md5.Sum([]byte(param + r.Form.Get("t") + "waybill-key" + "waybill-secret"))
		if r.Form.Get("method") != "order" || r.Form.Get("key") != "waybill-key" || r.Form.Get("sign") != strings.ToUpper(hex.EncodeToString(sum[:])) {
			t.Fatal("incorrect provider signature")
		}
		var p M
		if err := json.Unmarshal([]byte(param), &p); err != nil {
			t.Fatal(err)
		}
		if p["printType"] != "IMAGE" || p["orderId"] != "CJ123" || p["reorder"] != false || p["count"] != float64(1) || p["weight"] != 2.5 || p["kuaidicom"] != "jd" || p["tempId"] != "jd-v2-template" {
			t.Fatalf("wrong parameters: %+v", p)
		}
		receiver := p["recMan"].(map[string]any)
		if receiver["mobile"] != "13800001234" || receiver["printAddr"] != "浙江省杭州市西湖区测试路1号" {
			t.Fatal("recipient snapshot lost")
		}
		if _, ok := p["openid"]; ok {
			t.Fatal("unexpected identity sent to provider")
		}
		return waybillResponse(successfulWaybillResponse), nil
	})}
	c, _ := waybillSettingsFromEnv()
	carrier, _ := carrierForWaybill("JD")
	result := createProviderWaybill(context.Background(), c, carrier, waybillTestOrder(), 2.5)
	if result.Status != "UNCERTAIN" || result.TrackingNo != "JD12345678" || result.LabelURL != "https://api.kuaidi100.com/label/example" {
		t.Fatal(result)
	}
	// It is READY only after the actual image is durably saved.
}

func TestWaybillProviderFailuresDoNotFabricateSuccess(t *testing.T) {
	configureWaybillTest(t)
	c, _ := waybillSettingsFromEnv()
	carrier, _ := carrierForWaybill("JD")
	cases := []struct {
		body   string
		status string
	}{
		{`{"success":false,"code":30004,"message":"credential-echo-waybill-secret"}`, "FAILED"},
		{`{"success":false,"code":30005}`, "FAILED"},
		{`{"success":false,"code":-1}`, "UNCERTAIN"},
		{`{"success":true,"code":200,"data":{}}`, "UNCERTAIN"},
		{`{"success":true,"code":200,"data":{"kuaidinum":"JD1234","taskId":"task","label":"https://evil.test/image"}}`, "UNCERTAIN"},
		{`not-json`, "UNCERTAIN"},
	}
	for _, tc := range cases {
		waybillHTTP = &http.Client{Transport: roundTripFunc(func(*http.Request) (*http.Response, error) { return waybillResponse(tc.body), nil })}
		result := createProviderWaybill(context.Background(), c, carrier, waybillTestOrder(), 1)
		if result.Status != tc.status || strings.Contains(result.Error, "waybill-secret") {
			t.Fatal(result)
		}
	}
	waybillHTTP = &http.Client{Transport: roundTripFunc(func(*http.Request) (*http.Response, error) { return nil, context.DeadlineExceeded })}
	if result := createProviderWaybill(context.Background(), c, carrier, waybillTestOrder(), 1); result.Status != "UNCERTAIN" {
		t.Fatal(result)
	}
}

func TestWaybillConfigurationAndProtectedImages(t *testing.T) {
	configureWaybillTest(t)
	config := waybillConfig()
	if config["configured"] != true || len(config["carriers"].([]waybillCarrier)) != 1 || strings.Contains(jsonValue(config), "merchant-secret") || strings.Contains(jsonValue(config), "13800000000") {
		t.Fatal(config)
	}
	t.Setenv("KUAIDI100_SECRET", "")
	if waybillConfig()["configured"] != false {
		t.Fatal("missing electronic label credentials accepted")
	}
	for _, value := range []string{"https://api.kuaidi100.com.evil.test/label/x", "https://user:pass@api.kuaidi100.com/label/x", "https://api.kuaidi100.com:8080/label/x", "file:///image", "https://127.0.0.1/x", "https://api.kuaidi100.com/label/a,https://api.kuaidi100.com/label/b"} {
		if _, err := verifiedWaybillURL(value); err == nil {
			t.Fatalf("unsafe label URL accepted: %s", value)
		}
	}
	waybillImageHTTP = &http.Client{Transport: roundTripFunc(func(*http.Request) (*http.Response, error) { return waybillResponse("<html>not a label</html>"), nil })}
	if _, _, err := downloadWaybillImage(context.Background(), "https://api.kuaidi100.com/label/x"); err == nil {
		t.Fatal("HTML executed as authoritative label")
	}
	s := &Server{}
	response := httptest.NewRecorder()
	s.waybillLabelHTTP(response, httptest.NewRequest("GET", "/admin/waybills/x/label", nil))
	if response.Code != 401 {
		t.Fatal("private label accessible without admin login")
	}
}

// A small transactional SQL fixture exercises the durable creation claim,
// rollback, provider result persistence and shipment number reservation.
type waybillFixtureState struct {
	settings         string
	order            Order
	bill             *waybillRecord
	reservation      string
	created, updated time.Time
}
type waybillFixture struct {
	mu        sync.Mutex
	state     waybillFixtureState
	snapshot  *waybillFixtureState
	failClaim bool
}
type waybillConnector struct{ fixture *waybillFixture }

func (c waybillConnector) Connect(context.Context) (driver.Conn, error) { return c.fixture, nil }
func (c waybillConnector) Driver() driver.Driver                        { return waybillDriver{c.fixture} }

type waybillDriver struct{ fixture *waybillFixture }

func (d waybillDriver) Open(string) (driver.Conn, error) { return d.fixture, nil }
func (d *waybillFixture) Close() error                   { return nil }
func (d *waybillFixture) Prepare(string) (driver.Stmt, error) {
	return nil, errors.New("unexpected prepare")
}
func (d *waybillFixture) Begin() (driver.Tx, error) {
	d.mu.Lock()
	defer d.mu.Unlock()
	snapshot := d.state
	if snapshot.bill != nil {
		clone := *snapshot.bill
		snapshot.bill = &clone
	}
	d.snapshot = &snapshot
	return d, nil
}
func (d *waybillFixture) Commit() error {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.snapshot = nil
	return nil
}
func (d *waybillFixture) Rollback() error {
	d.mu.Lock()
	defer d.mu.Unlock()
	if d.snapshot != nil {
		d.state = *d.snapshot
		d.snapshot = nil
	}
	return nil
}
func (d *waybillFixture) QueryContext(_ context.Context, query string, args []driver.NamedValue) (driver.Rows, error) {
	d.mu.Lock()
	defer d.mu.Unlock()
	s := &d.state
	switch {
	case strings.Contains(query, "FROM configs WHERE config_key='waybill'"):
		if s.settings != "" {
			return singleShippingRow(s.settings), nil
		}
		return &shippingRows{cols: []string{"payload"}}, nil
	case strings.Contains(query, "FROM invoice_requests"), strings.Contains(query, "FROM shipping_notifications"):
		return &shippingRows{cols: []string{"value"}}, nil
	case strings.HasPrefix(query, "SELECT order_no,openid"):
		if args[0].Value != s.order.ID {
			return &shippingRows{cols: make([]string, 11)}, nil
		}
		raw, _ := json.Marshal(s.order)
		return singleShippingRow(s.order.OrderNo, s.order.OpenID, s.order.OrderStatus, s.order.PaymentStatus, s.order.FulfillmentStatus, s.order.RefundStatus, raw, false, "", time.Now().Add(time.Hour), time.Now()), nil
	case strings.Contains(query, "FROM order_waybills"):
		columnCount := 13
		if strings.Contains(query, ",content") {
			columnCount++
		}
		if s.bill == nil {
			return &shippingRows{cols: make([]string, columnCount)}, nil
		}
		b := s.bill
		values := []driver.Value{b.ID, b.OrderID, b.OrderNo, b.Carrier, b.CarrierCode, b.TrackingNo, b.Status, b.Error, b.ProviderTaskID, b.ProviderURL, b.MIME, s.created, s.updated}
		if columnCount == 14 {
			values = append(values, b.Content)
		}
		return singleShippingRow(values...), nil
	case strings.Contains(query, "FROM shipment_numbers"):
		return singleShippingRow(s.reservation), nil
	case strings.Contains(query, "FROM orders"):
		// No other historical order owns this mocked parcel.
		return &shippingRows{cols: []string{"id"}}, nil
	default:
		return nil, errors.New("unexpected query: " + query)
	}
}
func (d *waybillFixture) ExecContext(_ context.Context, query string, args []driver.NamedValue) (driver.Result, error) {
	d.mu.Lock()
	defer d.mu.Unlock()
	s := &d.state
	switch {
	case strings.HasPrefix(query, "INSERT IGNORE INTO configs"):
		if s.settings == "" {
			s.settings = args[0].Value.(string)
		}
	case strings.HasPrefix(query, "UPDATE configs SET payload=? WHERE config_key='waybill'"):
		s.settings = args[0].Value.(string)
	case strings.HasPrefix(query, "INSERT INTO order_waybills"):
		if d.snapshot == nil {
			return nil, errors.New("missing transaction creation claim")
		}
		if d.failClaim {
			return nil, errors.New("claim failed")
		}
		s.bill = &waybillRecord{Waybill: Waybill{ID: args[0].Value.(string), OrderID: args[1].Value.(string), OrderNo: args[2].Value.(string), Carrier: args[3].Value.(string), CarrierCode: args[4].Value.(string), Status: "CREATING"}}
		s.created, s.updated = time.Now().UTC(), time.Now().UTC()
	case strings.HasPrefix(query, "INSERT IGNORE INTO shipment_numbers"):
		if d.snapshot == nil {
			return nil, errors.New("reservation outside transaction")
		}
		if s.reservation == "" {
			s.reservation = args[2].Value.(string)
		}
	case strings.HasPrefix(query, "UPDATE order_waybills SET status=?,error=?,tracking_no=?"):
		if d.snapshot == nil {
			return nil, errors.New("provider result outside transaction")
		}
		s.bill.Status, s.bill.Error, s.bill.TrackingNo, s.bill.ProviderTaskID, s.bill.ProviderURL = args[0].Value.(string), args[1].Value.(string), args[2].Value.(string), args[3].Value.(string), args[4].Value.(string)
		s.updated = time.Now().UTC()
	case strings.HasPrefix(query, "UPDATE order_waybills SET status='READY'"):
		if s.bill.Status == "UNCERTAIN" {
			s.bill.Status, s.bill.Error, s.bill.MIME, s.bill.Content = "READY", "", args[0].Value.(string), args[1].Value.([]byte)
			s.updated = time.Now().UTC()
		}
	case strings.HasPrefix(query, "UPDATE order_waybills SET error=?"):
		if s.bill.Status == "UNCERTAIN" {
			s.bill.Error = args[0].Value.(string)
			s.updated = time.Now().UTC()
		}
	case strings.HasPrefix(query, "UPDATE order_waybills SET status='UNCERTAIN'"):
		if s.bill != nil && s.bill.Status == "CREATING" && time.Since(s.updated) > 2*time.Minute {
			s.bill.Status = "UNCERTAIN"
			s.bill.Error = "创建过程已中断"
			s.updated = time.Now().UTC()
		}
	case strings.HasPrefix(query, "UPDATE order_waybills SET status='CREATING'"):
		s.bill.Status, s.bill.Error = "CREATING", ""
		s.updated = time.Now().UTC()
	case strings.HasPrefix(query, "UPDATE order_waybills SET status=?,error=?"):
		s.bill.Status, s.bill.Error = args[0].Value.(string), args[1].Value.(string)
		s.updated = time.Now().UTC()
	case strings.HasPrefix(query, "INSERT INTO audit_logs"):
	default:
		return nil, errors.New("unexpected exec: " + query)
	}
	return driver.RowsAffected(1), nil
}

func newWaybillFixture(t *testing.T) (*Server, *waybillFixture) {
	t.Helper()
	fixture := &waybillFixture{state: waybillFixtureState{order: waybillTestOrder()}}
	db := sql.OpenDB(waybillConnector{fixture})
	db.SetMaxOpenConns(1)
	t.Cleanup(func() { db.Close() })
	return &Server{store: &Store{db: db}}, fixture
}

var testWaybillRequest = json.RawMessage(`{"orderId":"o1","carrierCode":"JD","weight":2.5}`)

func TestWaybillDurableClaimConcurrencyAndReprint(t *testing.T) {
	configureWaybillTest(t)
	s, fixture := newWaybillFixture(t)
	started, release := make(chan struct{}), make(chan struct{})
	var calls atomic.Int32
	waybillHTTP = &http.Client{Transport: roundTripFunc(func(*http.Request) (*http.Response, error) {
		calls.Add(1)
		fixture.mu.Lock()
		claimed := fixture.snapshot == nil && fixture.state.bill != nil && fixture.state.bill.Status == "CREATING"
		fixture.mu.Unlock()
		if !claimed {
			t.Error("provider called before claim committed")
		}
		close(started)
		<-release
		return waybillResponse(successfulWaybillResponse), nil
	})}
	done := make(chan error, 1)
	go func() { _, err := s.createWaybill(context.Background(), Claims{}, testWaybillRequest); done <- err }()
	<-started
	second, err := s.createWaybill(context.Background(), Claims{}, testWaybillRequest)
	if err != nil || second.(Waybill).Status != "CREATING" {
		t.Fatalf("concurrent claim %v %v", second, err)
	}
	close(release)
	if err = <-done; err != nil {
		t.Fatal(err)
	}
	fixture.mu.Lock()
	fixture.state.order.FulfillmentStatus = "SHIPPED"
	fixture.mu.Unlock()
	ready, err := s.createWaybill(context.Background(), Claims{}, testWaybillRequest)
	if err != nil || ready.(Waybill).Status != "READY" || calls.Load() != 1 {
		t.Fatalf("repeat created another bill: %v %v", ready, err)
	}
	if strings.Contains(jsonValue(ready), "api.kuaidi100.com") {
		t.Fatal("provider label URL exposed")
	}
	label, err := s.readWaybillLabel(context.Background(), "o1")
	if err != nil || label.(M)["dataUrl"] != "data:image/png;base64,"+waybillTestPNG {
		t.Fatalf("saved image missing: %v %v", label, err)
	}
	if _, err = s.createWaybill(context.Background(), Claims{}, json.RawMessage(`{"orderId":"o1","carrierCode":"SF","weight":1}`)); err == nil {
		t.Fatal("existing carrier changed")
	}
	fixture.mu.Lock()
	if fixture.state.order.Shipment != nil {
		t.Error("creating label marked order shipped")
	}
	fixture.mu.Unlock()
}

func TestWaybillUnknownOutcomeCannotRetry(t *testing.T) {
	configureWaybillTest(t)
	s, _ := newWaybillFixture(t)
	var calls atomic.Int32
	waybillHTTP = &http.Client{Transport: roundTripFunc(func(*http.Request) (*http.Response, error) { calls.Add(1); return nil, context.DeadlineExceeded })}
	for i := 0; i < 2; i++ {
		result, err := s.createWaybill(context.Background(), Claims{}, testWaybillRequest)
		if err != nil || result.(Waybill).Status != "UNCERTAIN" {
			t.Fatalf("unknown request: %v %v", result, err)
		}
	}
	if calls.Load() != 1 {
		t.Fatal("ambiguous provider request automatically repeated")
	}
	if _, err := s.syncWaybill(context.Background(), Claims{}, "o1"); err == nil {
		t.Fatal("unknown request cannot download an unknown label")
	}
	tx, _ := s.store.db.BeginTx(context.Background(), nil)
	defer tx.Rollback()
	if err := validateWaybillShipment(context.Background(), tx, "o1", Shipment{CarrierCode: "JD", TrackingNo: "JD12345678"}); err == nil {
		t.Fatal("unconfirmed electronic bill bypassed manually")
	}
}

func TestWaybillRollbackAndEligibility(t *testing.T) {
	configureWaybillTest(t)
	s, fixture := newWaybillFixture(t)
	var calls atomic.Int32
	waybillHTTP = &http.Client{Transport: roundTripFunc(func(*http.Request) (*http.Response, error) {
		calls.Add(1)
		return waybillResponse(successfulWaybillResponse), nil
	})}
	fixture.failClaim = true
	if _, err := s.createWaybill(context.Background(), Claims{}, testWaybillRequest); err == nil {
		t.Fatal("failed claim accepted")
	}
	if fixture.state.bill != nil || calls.Load() != 0 {
		t.Fatal("provider called after failed transaction")
	}
	fixture.failClaim = false
	for _, status := range []string{"UNPAID", "REFUNDED"} {
		fixture.state.order.PaymentStatus = status
		if _, err := s.createWaybill(context.Background(), Claims{}, testWaybillRequest); err == nil {
			t.Fatalf("%s label created", status)
		}
	}
	fixture.state.order.PaymentStatus = "PAID"
	for _, status := range []string{"APPLIED", "PROCESSING", "REFUNDED"} {
		fixture.state.order.RefundStatus = status
		if _, err := s.createWaybill(context.Background(), Claims{}, testWaybillRequest); err == nil {
			t.Fatalf("%s refund label created", status)
		}
	}
	fixture.state.order.RefundStatus = "NONE"
	fixture.state.order.FulfillmentStatus = "SHIPPED"
	if _, err := s.createWaybill(context.Background(), Claims{}, testWaybillRequest); err == nil {
		t.Fatal("new label created for shipped order")
	}
	if calls.Load() != 0 {
		t.Fatal("ineligible order reached provider")
	}
}
