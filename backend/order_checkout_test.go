package main

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"encoding/json"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"
)

type checkoutState struct {
	mu            sync.Mutex
	keys          map[string][2]string
	orders        map[string]Order
	stock, writes int
	failInsert    bool
}
type checkoutConnector struct{ state *checkoutState }
type checkoutDriver struct{}

func (checkoutDriver) Open(string) (driver.Conn, error) { return nil, errors.New("use connector") }
func (c checkoutConnector) Driver() driver.Driver       { return checkoutDriver{} }
func (c checkoutConnector) Connect(context.Context) (driver.Conn, error) {
	return &checkoutConn{state: c.state}, nil
}

type checkoutConn struct {
	state    *checkoutState
	snapshot checkoutState
	inTx     bool
}

func (c *checkoutConn) Prepare(string) (driver.Stmt, error) {
	return nil, errors.New("unexpected prepare")
}
func (c *checkoutConn) Close() error { return nil }
func (c *checkoutConn) Begin() (driver.Tx, error) {
	c.state.mu.Lock()
	c.inTx = true
	c.snapshot = checkoutState{keys: map[string][2]string{}, orders: map[string]Order{}, stock: c.state.stock, writes: c.state.writes}
	for k, v := range c.state.keys {
		c.snapshot.keys[k] = v
	}
	for k, v := range c.state.orders {
		c.snapshot.orders[k] = v
	}
	return c, nil
}
func (c *checkoutConn) Commit() error { c.inTx = false; c.state.mu.Unlock(); return nil }
func (c *checkoutConn) Rollback() error {
	c.state.keys = c.snapshot.keys
	c.state.orders = c.snapshot.orders
	c.state.stock = c.snapshot.stock
	c.state.writes = c.snapshot.writes
	c.inTx = false
	c.state.mu.Unlock()
	return nil
}
func keyArgs(args []driver.NamedValue, offset int) string {
	return args[offset].Value.(string) + "/" + args[offset+1].Value.(string)
}
func (c *checkoutConn) QueryContext(_ context.Context, q string, args []driver.NamedValue) (driver.Rows, error) {
	if strings.Contains(q, "config_key='shipping'") {
		return singleShippingRow(jsonValue(defaultRule())), nil
	}
	if !c.inTx {
		c.state.mu.Lock()
		defer c.state.mu.Unlock()
	}
	switch {
	case strings.HasPrefix(q, "SELECT stock,reserved,sold"):
		return singleShippingRow(int64(c.state.stock), int64(0), int64(0)), nil
	case strings.HasPrefix(q, "SELECT s.product_id"):
		return singleShippingRow("p1", "葡萄", "家庭装", int64(c.state.stock), int64(1), int64(0)), nil
	case strings.HasPrefix(q, "SELECT stock,reserved,sold"):
		return singleShippingRow(int64(c.state.stock), int64(0), int64(0)), nil
	case strings.HasPrefix(q, "SELECT s.product_id"):
		return singleShippingRow("p1", "葡萄", "家庭装", int64(c.state.stock), int64(0), int64(0)), nil
	case strings.HasPrefix(q, "SELECT request_hash"):
		v := c.state.keys[keyArgs(args, 0)]
		return singleShippingRow(v[0], v[1]), nil
	case strings.HasPrefix(q, "SELECT order_id FROM order_idempotency"):
		v, ok := c.state.keys[keyArgs(args, 0)]
		if !ok {
			return &shippingRows{cols: []string{"order_id"}}, nil
		}
		return singleShippingRow(v[1]), nil
	case strings.Contains(q, "FROM skus WHERE id="):
		return singleShippingRow("s1", "p1", "家庭装", int64(10000), int64(0), int64(c.state.stock), int64(0), int64(0), int64(10), "2kg", true, int64(1)), nil
	case strings.Contains(q, "SELECT payload,status FROM products"):
		return singleShippingRow(jsonValue(Product{ID: "p1", Name: "葡萄"}), "PUBLISHED"), nil
	case strings.Contains(q, "config_key='coupons'"):
		return singleShippingRow(`{"items":[{"code":"SAVE","name":"测试券","minimum":9900,"amount":1000,"startDate":"2020-01-01","endDate":"2099-01-01","active":true}]}`), nil
	case strings.HasPrefix(q, "SELECT COUNT(*) FROM orders"):
		count := 0
		for _, o := range c.state.orders {
			if o.OpenID == args[0].Value && o.Quote.CouponCode == args[1].Value {
				count++
			}
		}
		return singleShippingRow(int64(count)), nil
	case strings.HasPrefix(q, "SELECT id FROM orders"):
		for _, o := range c.state.orders {
			if o.OpenID == args[0].Value && o.Quote.CouponCode == args[1].Value && (o.PaymentStatus != "UNPAID" || (o.OrderStatus != "CANCELLED" && o.OrderStatus != "CLOSED")) {
				return singleShippingRow(o.ID), nil
			}
		}
		return &shippingRows{cols: []string{"id"}}, nil
	case strings.Contains(q, "FROM orders WHERE id="):
		o := c.state.orders[args[0].Value.(string)]
		return singleShippingRow(o.OrderNo, o.OpenID, o.OrderStatus, o.PaymentStatus, o.FulfillmentStatus, o.RefundStatus, jsonValue(o), false, "", o.ExpiresAt, time.Now()), nil
	case strings.Contains(q, "FROM invoice_requests"), strings.Contains(q, "FROM shipping_notifications"):
		return &shippingRows{cols: []string{"empty"}}, nil
	}
	return nil, errors.New("unexpected checkout query: " + q)
}
func (c *checkoutConn) ExecContext(_ context.Context, q string, args []driver.NamedValue) (driver.Result, error) {
	if !c.inTx {
		return nil, errors.New("write outside transaction")
	}
	switch {
	case strings.HasPrefix(q, "INSERT INTO order_idempotency"):
		key := keyArgs(args, 0)
		if _, ok := c.state.keys[key]; !ok {
			c.state.keys[key] = [2]string{args[2].Value.(string), ""}
		}
	case strings.HasPrefix(q, "UPDATE order_idempotency"):
		key := keyArgs(args, 1)
		v := c.state.keys[key]
		v[1] = args[0].Value.(string)
		c.state.keys[key] = v
	case strings.HasPrefix(q, "UPDATE skus"):
		c.state.stock -= int(args[0].Value.(int64))
	case strings.HasPrefix(q, "INSERT INTO orders"):
		if c.state.failInsert {
			return nil, errors.New("injected insert failure")
		}
		var o Order
		if err := json.Unmarshal([]byte(args[8].Value.(string)), &o); err != nil {
			return nil, err
		}
		o.OpenID = args[2].Value.(string)
		c.state.orders[o.ID] = o
		c.state.writes++
	case strings.HasPrefix(q, "DELETE FROM carts"):
	case strings.HasPrefix(q, "INSERT INTO inventory_movements"):
	case strings.HasPrefix(q, "INSERT INTO inventory_movements"):
	default:
		return nil, errors.New("unexpected checkout write: " + q)
	}
	return driver.RowsAffected(1), nil
}
func TestCheckoutIdempotencyTransactions(t *testing.T) {
	state := &checkoutState{keys: map[string][2]string{}, orders: map[string]Order{}, stock: 10}
	db := sql.OpenDB(checkoutConnector{state})
	defer db.Close()
	s := &Server{store: &Store{db: db}}
	d := CreateOrderInput{IdempotencyKey: "checkout_test_0001", CouponCode: "SAVE", Items: []CartItem{{ProductID: "p1", SKUID: "s1", Quantity: 1}}, Address: Address{Name: "测试", Phone: "13800000000", Province: "浙江省", City: "杭州市", Detail: "1号"}}
	var wg sync.WaitGroup
	ids := make(chan string, 8)
	errs := make(chan error, 8)
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			v, e := s.createOrder(context.Background(), "owner", json.RawMessage(jsonValue(d)))
			if e != nil {
				errs <- e
				return
			}
			switch o := v.(type) {
			case Order:
				ids <- o.ID
			case *Order:
				ids <- o.ID
			}
		}()
	}
	wg.Wait()
	close(ids)
	close(errs)
	for e := range errs {
		t.Fatal(e)
	}
	first := ""
	for id := range ids {
		if first == "" {
			first = id
		}
		if id != first {
			t.Fatal("duplicate order", id, first)
		}
	}
	if state.writes != 1 || state.stock != 9 {
		t.Fatalf("writes=%d stock=%d", state.writes, state.stock)
	}
	// Replay must work even if coupon is occupied and inventory is now unavailable.
	state.stock = 0
	if _, e := s.createOrder(context.Background(), "owner", json.RawMessage(jsonValue(d))); e != nil {
		t.Fatal(e)
	}
	d.Note = "changed"
	if _, e := s.createOrder(context.Background(), "owner", json.RawMessage(jsonValue(d))); e == nil || !strings.Contains(e.Error(), "其他结算") {
		t.Fatal("key reuse accepted", e)
	}
	d.Note = ""
	d.CouponCode = ""
	state.stock = 5
	if _, e := s.createOrder(context.Background(), "other", json.RawMessage(jsonValue(d))); e != nil {
		t.Fatal("users share key namespace", e)
	}
	d.IdempotencyKey = "checkout_rollback_0001"
	state.failInsert = true
	if _, e := s.createOrder(context.Background(), "owner", json.RawMessage(jsonValue(d))); e == nil {
		t.Fatal("insert failure accepted")
	}
	if state.stock != 4 || len(state.keys) != 2 {
		t.Fatal("failed purchase did not rollback", state.stock, state.keys)
	}
	state.failInsert = false
	if _, e := s.createOrder(context.Background(), "owner", json.RawMessage(jsonValue(d))); e != nil {
		t.Fatal("failed attempt cannot retry", e)
	}
	recovered, e := s.recoverOrder(context.Background(), "owner", d.IdempotencyKey)
	if e != nil || recovered == nil {
		t.Fatal("lost response recovery failed", e)
	}
	missing, e := s.recoverOrder(context.Background(), "stranger", d.IdempotencyKey)
	if e != nil || missing != nil {
		t.Fatal("recovery leaked other user's order", e)
	}
}

func TestComplexDeliveryRules(t *testing.T) {
	now := time.Date(2026, 10, 5, 0, 0, 0, 0, time.UTC)
	items := []OrderItem{{CartItem: CartItem{Quantity: 3, SKU: &SKU{Stock: 10, LimitPerOrder: 10}}, UnitPrice: 5000}}
	rule := ShippingRule{Freight: 1200, FreeShippingThreshold: 19900, Regions: []ShippingRegion{
		{Name: "省", Province: "浙江省", Freight: 2000, FreeShippingThreshold: 20000, BaseQuantity: 1, ExtraFreight: 500, Surcharge: 300},
		{Name: "市", Province: "浙江省", City: "杭州市", Freight: 1000, FreeShippingThreshold: 20000, BaseQuantity: 2, ExtraFreight: 200, Surcharge: 400},
		{Name: "区", Province: "浙江省", City: "杭州市", District: "西湖区", Blocked: true, BaseQuantity: 1},
	}, BlockedDates: []string{"2026-10-10"}}
	if e := validateShippingDetails(rule); e != nil {
		t.Fatal(e)
	}
	for _, tc := range []struct {
		city, district, date string
		freight              int
		name                 string
		blocked              bool
	}{
		{"宁波市", "", "", 3300, "省", false}, {"杭州市", "余杭区", "", 1600, "市", false}, {"杭州市", "西湖区", "", 0, "区", true}, {"杭州市", "余杭区", "2026-10-10", 1600, "市", true},
	} {
		q, e := calculateAddressQuote(items, rule, Address{Province: "浙江省", City: tc.city, District: tc.district}, tc.date, now)
		if e != nil || q.Freight != tc.freight || q.ShippingRuleName != tc.name || (q.BlockedReason != "") != tc.blocked {
			t.Fatalf("%+v: %+v %v", tc, q, e)
		}
	}
	items[0].UnitPrice = 10000
	q, e := calculateAddressQuote(items, rule, Address{Province: "浙江省", City: "杭州市"}, "", now)
	if e != nil || q.Freight != 400 {
		t.Fatal("free shipping must retain surcharge", q, e)
	}
	if e := discountCoupon(Coupon{Active: true, StartDate: "2020-01-01", EndDate: "2099-01-01", Minimum: 30000, Amount: 29999}, &q, now); e != nil || q.Freight != 400 || q.Total != 401 {
		t.Fatal("coupon changed freight", q, e)
	}
	rule.BlockedProvinces = []string{"浙江省"}
	q, _ = calculateAddressQuote(items, rule, Address{Province: "浙江省", City: "杭州市"}, "", now)
	if q.BlockedReason == "" {
		t.Fatal("province ban overridden")
	}
	rule.Regions = append(rule.Regions, rule.Regions[0])
	if validateShippingDetails(rule) == nil {
		t.Fatal("duplicate region accepted")
	}
	rule.Regions = nil
	rule.BlockedDates = []string{"2026-02-30"}
	if validateShippingDetails(rule) == nil {
		t.Fatal("invalid calendar date accepted")
	}
}
