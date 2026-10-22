package main

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"
)

type priceFixture struct {
	shippingFixture
	active, published, stocked, stale bool
	price, baseline, target           int
	skuID                             string
	historyWrites                     int
	history                           []AttentionHistory
}
type priceConnector struct{ state *priceFixture }

func (c priceConnector) Connect(context.Context) (driver.Conn, error) { return c.state, nil }
func (c priceConnector) Driver() driver.Driver                        { return priceDriver{c.state} }

type priceDriver struct{ state *priceFixture }

func (d priceDriver) Open(string) (driver.Conn, error) { return d.state, nil }
func (d *priceFixture) QueryContext(_ context.Context, q string, args []driver.NamedValue) (driver.Rows, error) {
	switch {
	case strings.HasPrefix(q, "SELECT n.baseline_price,n.push_status"):
		if args[0].Value != "owner" || args[1].Value != "p1" || !d.active {
			return &shippingRows{cols: make([]string, 2)}, nil
		}
		return singleShippingRow(int64(d.baseline), d.status), nil
	case strings.HasPrefix(q, "SELECT id,product_id,product_name,kind"):
		values := [][]driver.Value{}
		if args[0].Value == "owner" {
			for _, h := range d.history {
				values = append(values, []driver.Value{h.ID, h.ProductID, h.ProductName, h.Kind, h.SKUName, int64(h.CurrentPrice), int64(h.TargetPrice), h.Status, h.Detail, time.Date(2026, 10, 7, 1, 2, 0, 0, time.UTC)})
			}
		}
		offset, limit := int(args[2].Value.(int64)), int(args[1].Value.(int64))
		if offset > len(values) {
			offset = len(values)
		}
		values = values[offset:]
		if len(values) > limit {
			values = values[:limit]
		}
		return &shippingRows{cols: make([]string, 10), values: values}, nil
	case strings.HasPrefix(q, "SELECT a.product_id,a.kind"):
		if args[0].Value != "owner" {
			return &shippingRows{cols: make([]string, 3)}, nil
		}
		return singleShippingRow("p1", "pricedrop", "NONE"), nil
	case strings.HasPrefix(q, "SELECT product_id,sku_id,baseline_price"):
		if args[0].Value != "owner" {
			return nil, errors.New("wrong reminder owner")
		}
		return singleShippingRow("p1", d.skuID, int64(d.baseline), d.status, int64(d.target)), nil
	case strings.HasPrefix(q, "SELECT n.openid,n.product_id"):
		if !strings.Contains(q, "k.id=n.sku_id") || !strings.Contains(q, "k.price<n.baseline_price") {
			return nil, errors.New("missing SKU price guard")
		}
		if d.stale || (d.active && d.published && d.stocked && (d.target == 0 && d.price < d.baseline || d.target > 0 && d.price <= d.target) && (d.status == "READY" || d.status == "FAILED")) {
			return singleShippingRow("owner", "p1", d.skuID, int64(d.baseline), int64(d.target)), nil
		}
		return &shippingRows{cols: make([]string, 5)}, nil
	case strings.HasPrefix(q, "SELECT payload FROM products"):
		if !d.published {
			return &shippingRows{cols: []string{"payload"}}, nil
		}
		raw, _ := json.Marshal(Product{ID: "p1", Name: "阳光玫瑰"})
		return singleShippingRow(raw), nil
	case strings.HasPrefix(q, "SELECT id,product_id,name,price"):
		stock := int64(0)
		if d.stocked {
			stock = 10
		}
		return &shippingRows{cols: make([]string, 12), values: [][]driver.Value{{d.skuID, "p1", "家庭装", int64(d.price), int64(99900), stock, int64(0), int64(0), int64(5), "1kg", true, int64(0)}}}, nil
	case strings.HasPrefix(q, "SELECT k.price"):
		if args[0].Value != "p1" || args[1].Value != "s1" || !d.published {
			return &shippingRows{cols: []string{"price"}}, nil
		}
		return singleShippingRow(int64(d.price)), nil
	case strings.HasPrefix(q, "SELECT product_id FROM product_attention"):
		if !d.active {
			return &shippingRows{cols: []string{"product_id"}}, nil
		}
		return singleShippingRow("p1"), nil
	case strings.HasPrefix(q, "SELECT push_status"):
		return singleShippingRow(d.status), nil
	default:
		return nil, errors.New("unexpected price query: " + q)
	}
}
func (d *priceFixture) ExecContext(_ context.Context, q string, args []driver.NamedValue) (driver.Result, error) {
	switch {
	case strings.HasPrefix(q, "INSERT INTO product_reminder_history"):
		d.historyWrites++
		d.history = append(d.history, AttentionHistory{ID: args[0].Value.(string), ProductID: args[2].Value.(string), ProductName: args[3].Value.(string), Kind: args[4].Value.(string), SKUName: args[5].Value.(string), CurrentPrice: int(args[6].Value.(int64)), TargetPrice: int(args[7].Value.(int64)), Status: "SENDING"})
	case strings.HasPrefix(q, "UPDATE product_reminder_history"):
		d.historyWrites++
		for i := range d.history {
			if d.history[i].ID == args[2].Value && args[3].Value == "owner" {
				d.history[i].Status = args[0].Value.(string)
				d.history[i].Detail = args[1].Value.(string)
			}
		}
	case strings.HasPrefix(q, "UPDATE price_drop_notifications SET target_price=?"):
		d.target = int(args[0].Value.(int64))
	case strings.HasPrefix(q, "UPDATE price_drop_notifications n SET"):
		if !strings.Contains(q, "baseline_price=?") || !strings.Contains(q, "k.price=?") || !strings.Contains(q, "a.kind='pricedrop'") {
			return nil, errors.New("missing atomic price claim guard")
		}
		if !d.active || !d.published || !d.stocked || (d.target == 0 && d.price >= d.baseline || d.target > 0 && d.price > d.target) || args[3].Value != d.skuID || args[4].Value != int64(d.baseline) || args[5].Value != int64(d.target) || args[6].Value != int64(d.price) || (d.status != "READY" && d.status != "FAILED") {
			return driver.RowsAffected(0), nil
		}
		d.status = "SENDING"
	case strings.HasPrefix(q, "UPDATE price_drop_notifications SET push_status=?"):
		d.status = args[0].Value.(string)
	case strings.HasPrefix(q, "INSERT INTO product_attention"):
		d.active = true
	case strings.HasPrefix(q, "DELETE FROM product_attention"):
		d.active = false
	case strings.HasPrefix(q, "UPDATE price_drop_notifications SET push_status='CANCELLED'"):
		if d.status != "SENDING" && d.status != "UNCERTAIN" {
			d.status = "CANCELLED"
		}
	case strings.HasPrefix(q, "INSERT INTO price_drop_notifications"):
		if d.status == "" || d.status == "CANCELLED" {
			d.skuID = args[2].Value.(string)
			d.baseline = int(args[3].Value.(int64))
			d.target = int(args[4].Value.(int64))
			d.status = "NONE"
		}
	case strings.HasPrefix(q, "UPDATE price_drop_notifications SET template_id=?"):
		if d.status == "NONE" || d.status == "BLOCKED" || d.status == "FAILED" {
			d.status = "READY"
		}
	default:
		return nil, errors.New("unexpected price exec: " + q)
	}
	return driver.RowsAffected(1), nil
}

func configurePriceTest(t *testing.T) {
	t.Helper()
	configureShippingTest(t)
	t.Setenv("WECHAT_PRICE_DROP_TEMPLATE_ID", "price-template")
	t.Setenv("WECHAT_PRICE_DROP_TEMPLATE_FIELDS", `{"thing1":"product","thing2":"sku","amount3":"baselinePrice","amount4":"currentPrice","time5":"detectedAt"}`)
}

func TestPriceDropSameSKU(t *testing.T) {
	p := Product{SKUs: []SKU{{ID: "watched", Price: 9900, Stock: 10, LimitPerOrder: 5}, {ID: "other", Price: 100, Stock: 20, LimitPerOrder: 5}}}
	if priceDropFor(p, "watched", 9900, "NONE").Dropped {
		t.Fatal("other SKU triggered reminder")
	}
	p.SKUs[0].Price = 9800
	if d := priceDropFor(p, "watched", 9900, "NONE"); !d.Dropped || !d.Available {
		t.Fatal(d)
	}
	p.SKUs[0].Stock = 0
	if d := priceDropFor(p, "watched", 9900, "NONE"); !d.Dropped || d.Available {
		t.Fatal(d)
	}
	if d := priceDropFor(p, "removed", 9900, "NONE"); d.Dropped || d.Available {
		t.Fatal(d)
	}
}

func TestPriceDropUserState(t *testing.T) {
	f := &priceFixture{baseline: 9900, price: 7900, skuID: "s1", shippingFixture: shippingFixture{status: "READY"}}
	db := sql.OpenDB(priceConnector{f})
	db.SetMaxOpenConns(1)
	defer db.Close()
	s := &Store{db: db}
	products := []Product{{ID: "p1", SKUs: []SKU{{ID: "s1", Name: "家庭装", Price: 7900, Stock: 5, LimitPerOrder: 5}}}}
	if err := s.attachAttention(context.Background(), "owner", products); err != nil {
		t.Fatal(err)
	}
	if d := products[0].PriceDrop; !products[0].PriceDropReminder || d == nil || !d.Dropped || d.BaselinePrice != 9900 || d.CurrentPrice != 7900 || d.PushStatus != "READY" {
		t.Fatal(products)
	}
	other := []Product{{ID: "p1"}}
	if err := s.attachAttention(context.Background(), "other-user", other); err != nil {
		t.Fatal(err)
	}
	if other[0].PriceDropReminder || other[0].PriceDrop != nil {
		t.Fatal("another user's reminder leaked")
	}
}

func TestPriceDropSetAndGrant(t *testing.T) {
	configurePriceTest(t)
	f := &priceFixture{published: true, price: 9900, skuID: "s1"}
	db := sql.OpenDB(priceConnector{f})
	db.SetMaxOpenConns(1)
	defer db.Close()
	s := &Server{store: &Store{db: db}}
	ctx := context.Background()
	set := func(enabled bool) {
		t.Helper()
		if _, err := s.store.setPriceDrop(ctx, "owner", "p1", "s1", enabled); err != nil {
			t.Fatal(err)
		}
	}
	set(true)
	if f.baseline != 9900 || f.status != "NONE" {
		t.Fatal(f)
	}
	f.price = 7900
	set(true)
	if f.baseline != 9900 {
		t.Fatal("duplicate enable reset baseline")
	}
	if _, err := s.acceptPriceDropSubscription(ctx, "owner", "p1", "stale-template"); err == nil {
		t.Fatal("stale template accepted")
	}
	if _, err := s.acceptPriceDropSubscription(ctx, "owner", "p1", "price-template"); err != nil || f.status != "READY" {
		t.Fatal(err, f.status)
	}
	f.status = "SENT"
	if _, err := s.acceptPriceDropSubscription(ctx, "owner", "p1", "price-template"); err != nil || f.status != "SENT" {
		t.Fatal("repeat grant reset successful send", err)
	}
	set(false)
	if f.active || f.status != "CANCELLED" {
		t.Fatal(f)
	}
	if _, err := s.acceptPriceDropSubscription(ctx, "owner", "p1", "price-template"); err == nil {
		t.Fatal("cancelled reminder accepted grant")
	}
	set(true)
	if f.baseline != 7900 || f.status != "NONE" {
		t.Fatal("renewal did not require new grant")
	}
	if _, err := s.store.setPriceDrop(ctx, "owner", "p1", "other-sku", true); err == nil {
		t.Fatal("invalid SKU accepted")
	}
	f.status = "UNCERTAIN"
	set(false)
	set(true)
	if f.status != "UNCERTAIN" || f.baseline != 7900 {
		t.Fatal("uncertain send overwritten")
	}
}

func TestPriceDropDispatch(t *testing.T) {
	configurePriceTest(t)
	for _, tc := range []struct {
		name, status, response, want      string
		active, published, stocked, stale bool
		price, sends                      int
	}{
		{"success once", "READY", `{"errcode":0}`, "SENT", true, true, true, false, 7900, 1},
		{"explicit failure retries", "READY", `{"errcode":40037}`, "FAILED", true, true, true, false, 7900, 2},
		{"refused grant stops", "READY", `{"errcode":43101}`, "BLOCKED", true, true, true, false, 7900, 1},
		{"ambiguous stops", "READY", `{}`, "UNCERTAIN", true, true, true, false, 7900, 1},
		{"no grant", "NONE", `{"errcode":0}`, "NONE", true, true, true, false, 7900, 0},
		{"cancelled after scan", "READY", `{"errcode":0}`, "READY", false, true, true, true, 7900, 0},
		{"price restored", "READY", `{"errcode":0}`, "READY", true, true, true, true, 9900, 0},
		{"price increased", "READY", `{"errcode":0}`, "READY", true, true, true, true, 10900, 0},
		{"sold out", "READY", `{"errcode":0}`, "READY", true, true, false, true, 7900, 0},
		{"unpublished", "READY", `{"errcode":0}`, "READY", true, false, true, true, 7900, 0},
		{"already claimed", "SENDING", `{"errcode":0}`, "SENDING", true, true, true, true, 7900, 0},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f := &priceFixture{shippingFixture: shippingFixture{status: tc.status}, active: tc.active, published: tc.published, stocked: tc.stocked, stale: tc.stale, price: tc.price, baseline: 9900, skuID: "s1"}
			db := sql.OpenDB(priceConnector{f})
			db.SetMaxOpenConns(1)
			defer db.Close()
			sends := 0
			s := &Server{store: &Store{db: db}, wechat: &Wechat{client: &http.Client{Transport: shippingTransport(func(r *http.Request) (*http.Response, error) {
				body := `{"access_token":"token"}`
				if !strings.Contains(r.URL.Path, "/token") {
					sends++
					body = tc.response
				}
				return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader(body)), Header: make(http.Header)}, nil
			})}}}
			for i := 0; i < 2; i++ {
				if _, err := s.sendPriceDropNotices(context.Background()); err != nil {
					t.Fatal(err)
				}
			}
			if f.historyWrites != tc.sends*2 {
				t.Fatalf("history writes=%d sends=%d", f.historyWrites, sends)
			}
			if sends != tc.sends || f.status != tc.want {
				t.Fatalf("sends=%d status=%s", sends, f.status)
			}
		})
	}
}

func TestPriceDropConfigurationAndMessage(t *testing.T) {
	configurePriceTest(t)
	c, err := priceDropConfig()
	if err != nil {
		t.Fatal(err)
	}
	body := priceDropMessage("owner", Product{ID: "a&b", Name: strings.Repeat("葡", 30)}, PriceDrop{SKUName: "家庭装", BaselinePrice: 9901, CurrentPrice: 7900}, c, time.Date(2026, 10, 5, 1, 2, 0, 0, time.UTC))
	data := body["data"].(M)
	if body["page"] != "pages/product/index?id=a%26b" || data["amount3"].(M)["value"] != "99.01" || data["amount4"].(M)["value"] != "79.00" || data["time5"].(M)["value"] != "2026-10-05 09:02" || len([]rune(data["thing1"].(M)["value"].(string))) != 20 {
		t.Fatal(body)
	}
	for _, raw := range []string{`{}`, `null`, `{"thing1":"currentPrice"}`, `{"amount1":"sku"}`, `{"time1":"product"}`, `{"thing1":"unknown"}`} {
		t.Setenv("WECHAT_PRICE_DROP_TEMPLATE_FIELDS", raw)
		if _, err = priceDropConfig(); err == nil {
			t.Fatal("invalid mapping accepted", raw)
		}
	}
	t.Setenv("WECHAT_PRICE_DROP_TEMPLATE_ID", "")
	if priceDropSubscriptionConfig()["enabled"] != false {
		t.Fatal("missing config must disable push")
	}
}
