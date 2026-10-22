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

type restockFixture struct {
	shippingFixture
	active, published, stocked bool
	staleCandidate             bool
	historyWrites              int
}
type restockConnector struct{ state *restockFixture }

func (c restockConnector) Connect(context.Context) (driver.Conn, error) { return c.state, nil }
func (c restockConnector) Driver() driver.Driver                        { return restockDriver{c.state} }

type restockDriver struct{ state *restockFixture }

func (d restockDriver) Open(string) (driver.Conn, error) { return d.state, nil }
func (d *restockFixture) QueryContext(_ context.Context, q string, _ []driver.NamedValue) (driver.Rows, error) {
	switch {
	case strings.HasPrefix(q, "SELECT n.openid,n.product_id"):
		if d.staleCandidate || (d.active && d.published && d.stocked && (d.status == "READY" || d.status == "FAILED")) {
			return singleShippingRow("owner", "p1"), nil
		}
		return &shippingRows{cols: []string{"openid", "product_id"}}, nil
	case strings.HasPrefix(q, "SELECT payload FROM products"):
		if !d.published {
			return &shippingRows{cols: []string{"payload"}}, nil
		}
		raw, _ := json.Marshal(Product{ID: "p1", Name: "阳光玫瑰"})
		return singleShippingRow(raw), nil
	case strings.HasPrefix(q, "SELECT id,product_id,name,price"):
		return &shippingRows{cols: make([]string, 12)}, nil
	default:
		return nil, errors.New("unexpected restock query: " + q)
	}
}
func (d *restockFixture) ExecContext(_ context.Context, q string, args []driver.NamedValue) (driver.Result, error) {
	if strings.HasPrefix(q, "INSERT INTO product_reminder_history") || strings.HasPrefix(q, "UPDATE product_reminder_history") {
		d.historyWrites++
	} else if strings.HasPrefix(q, "UPDATE restock_notifications n SET") {
		if !d.active || !d.published || !d.stocked || (d.status != "READY" && d.status != "FAILED") {
			return driver.RowsAffected(0), nil
		}
		d.status = "SENDING"
	} else if strings.HasPrefix(q, "UPDATE restock_notifications SET push_status=?") {
		if d.status != "SENDING" {
			return driver.RowsAffected(0), nil
		}
		d.status = args[0].Value.(string)
	} else {
		return nil, errors.New("unexpected restock exec: " + q)
	}
	return driver.RowsAffected(1), nil
}

func TestRestockDispatchGuardsAndRepeatRuns(t *testing.T) {
	t.Setenv("WECHAT_RESTOCK_TEMPLATE_ID", "restock-template")
	t.Setenv("WECHAT_RESTOCK_TEMPLATE_FIELDS", `{"thing1":"product"}`)
	t.Setenv("WECHAT_MINIPROGRAM_STATE", "trial")
	t.Setenv("WECHAT_APP_ID", "test-app")
	t.Setenv("WECHAT_APP_SECRET", "test-secret")
	for _, tc := range []struct {
		name, status, response, want      string
		active, stocked, published, stale bool
		sends                             int
	}{
		{"successful once", "READY", `{"errcode":0}`, "SENT", true, true, true, false, 1},
		{"explicit failure retries", "READY", `{"errcode":40037}`, "FAILED", true, true, true, false, 2},
		{"refused grant stops", "READY", `{"errcode":43101}`, "BLOCKED", true, true, true, false, 1},
		{"ambiguous response stops", "READY", `{}`, "UNCERTAIN", true, true, true, false, 1},
		{"no authorization", "NONE", `{"errcode":0}`, "NONE", true, true, true, false, 0},
		{"cancelled after scan", "READY", `{"errcode":0}`, "READY", false, true, true, true, 0},
		{"sold out after scan", "READY", `{"errcode":0}`, "READY", true, false, true, true, 0},
		{"unpublished", "READY", `{"errcode":0}`, "READY", true, true, false, true, 0},
		{"already claimed", "SENDING", `{"errcode":0}`, "SENDING", true, true, true, true, 0},
	} {
		t.Run(tc.name, func(t *testing.T) {
			fixture := &restockFixture{shippingFixture: shippingFixture{status: tc.status}, active: tc.active, stocked: tc.stocked, published: tc.published, staleCandidate: tc.stale}
			db := sql.OpenDB(restockConnector{fixture})
			db.SetMaxOpenConns(1)
			defer db.Close()
			sends := 0
			server := &Server{store: &Store{db: db}, wechat: &Wechat{client: &http.Client{Transport: shippingTransport(func(r *http.Request) (*http.Response, error) {
				body := `{"access_token":"test-token"}`
				if !strings.Contains(r.URL.Path, "/token") {
					sends++
					body = tc.response
				}
				return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader(body)), Header: make(http.Header)}, nil
			})}}}
			for i := 0; i < 2; i++ {
				if _, err := server.sendRestockNotices(context.Background()); err != nil {
					t.Fatal(err)
				}
			}
			if fixture.historyWrites != tc.sends*2 {
				t.Fatalf("history writes=%d sends=%d", fixture.historyWrites, sends)
			}
			if sends != tc.sends || fixture.status != tc.want {
				t.Fatalf("sends=%d status=%s", sends, fixture.status)
			}
		})
	}
}

func TestRestockConfigurationAndMessage(t *testing.T) {
	t.Setenv("WECHAT_RESTOCK_TEMPLATE_ID", "restock-template")
	t.Setenv("WECHAT_RESTOCK_TEMPLATE_FIELDS", `{"thing1":"product","thing2":"remark","time3":"restockedAt"}`)
	t.Setenv("WECHAT_MINIPROGRAM_STATE", "trial")
	c, err := restockConfig()
	if err != nil {
		t.Fatal(err)
	}
	body := restockMessage("owner", Product{ID: "a&b", Name: strings.Repeat("葡", 30)}, c, time.Date(2026, 10, 5, 1, 2, 0, 0, time.UTC))
	data := body["data"].(M)
	if body["touser"] != "owner" || body["template_id"] != "restock-template" || body["page"] != "pages/product/index?id=a%26b" || body["miniprogram_state"] != "trial" {
		t.Fatal(body)
	}
	if len([]rune(data["thing1"].(M)["value"].(string))) != 20 || data["time3"].(M)["value"] != "2026-10-05 09:02" {
		t.Fatal(data)
	}
	for _, raw := range []string{`{}`, `null`, `{"thing1":"restockedAt"}`, `{"time1":"product"}`, `{"number1":"product"}`, `{"thing1":"unknown"}`} {
		t.Setenv("WECHAT_RESTOCK_TEMPLATE_FIELDS", raw)
		if _, err = restockConfig(); err == nil {
			t.Fatalf("accepted invalid mapping %s", raw)
		}
	}
	t.Setenv("WECHAT_RESTOCK_TEMPLATE_ID", "")
	if restockSubscriptionConfig()["enabled"] != false {
		t.Fatal("missing template must disable authorization")
	}
}

func TestRestockWechatResponseClassification(t *testing.T) {
	t.Setenv("WECHAT_APP_ID", "test-app")
	t.Setenv("WECHAT_APP_SECRET", "test-secret")
	for _, tc := range []struct {
		name, response string
		network        bool
		code           int
		success        bool
	}{
		{name: "success", response: `{"errcode":0}`, success: true},
		{name: "grant denied", response: `{"errcode":43101,"errmsg":"refused"}`, code: 43101},
		{name: "invalid template", response: `{"errcode":40037,"errmsg":"invalid template"}`, code: 40037},
		{name: "ambiguous response", response: `{}`},
		{name: "ambiguous network", network: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			w := &Wechat{client: &http.Client{Transport: shippingTransport(func(r *http.Request) (*http.Response, error) {
				response := tc.response
				if strings.Contains(r.URL.Path, "/token") {
					response = `{"access_token":"test-token"}`
				} else if tc.network {
					return nil, errors.New("connection lost")
				}
				return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader(response)), Header: make(http.Header)}, nil
			})}}
			err := w.sendSubscriptionMessage(context.Background(), M{"touser": "owner"})
			if (err == nil) != tc.success {
				t.Fatal(err)
			}
			var rejection *messageRejection
			rejected := errors.As(err, &rejection)
			if rejected != (tc.code != 0) || (rejected && rejection.Code != tc.code) {
				t.Fatalf("unexpected classification: %v", err)
			}
		})
	}
}
