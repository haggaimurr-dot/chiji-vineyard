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

type activityPushFixture struct {
	shippingFixture
	home                   HomeConfig
	active, stale, changed bool
}
type activityPushConnector struct{ f *activityPushFixture }
type activityPushDriver struct{ f *activityPushFixture }

func (c activityPushConnector) Connect(context.Context) (driver.Conn, error) { return c.f, nil }
func (c activityPushConnector) Driver() driver.Driver                        { return activityPushDriver{c.f} }
func (d activityPushDriver) Open(string) (driver.Conn, error)                { return d.f, nil }
func (f *activityPushFixture) QueryContext(_ context.Context, q string, _ []driver.NamedValue) (driver.Rows, error) {
	if strings.HasPrefix(q, "SELECT payload FROM configs") {
		h := f.home
		if f.changed && strings.Contains(q, "FOR UPDATE") {
			h.Activities = nil
		}
		raw, _ := json.Marshal(h)
		return singleShippingRow(raw), nil
	}
	if strings.HasPrefix(q, "SELECT n.openid") {
		if f.stale || (f.active && f.status == "READY") {
			return singleShippingRow("owner"), nil
		}
		return &shippingRows{cols: []string{"openid"}}, nil
	}
	return nil, errors.New("unexpected query: " + q)
}
func (f *activityPushFixture) ExecContext(_ context.Context, q string, args []driver.NamedValue) (driver.Result, error) {
	if strings.HasPrefix(q, "UPDATE activity_push_subscriptions n") {
		if !strings.Contains(q, "r.created_at=n.reservation_created_at") || !strings.Contains(q, "openid=?") {
			return nil, errors.New("missing reservation or ownership guard")
		}
		if !f.active || f.status != "READY" {
			return driver.RowsAffected(0), nil
		}
		f.status = "SENDING"
	} else if strings.HasPrefix(q, "UPDATE activity_push_subscriptions SET") {
		f.status = args[0].Value.(string)
	} else {
		return nil, errors.New("unexpected exec: " + q)
	}
	return driver.RowsAffected(1), nil
}
func configureActivityPush(t *testing.T) {
	t.Helper()
	t.Setenv("WECHAT_ACTIVITY_TEMPLATE_ID", "activity-template")
	t.Setenv("WECHAT_ACTIVITY_TEMPLATE_FIELDS", `{"thing1":"title","thing2":"location","date3":"startDate"}`)
	t.Setenv("WECHAT_MINIPROGRAM_STATE", "trial")
	t.Setenv("WECHAT_APP_ID", "app")
	t.Setenv("WECHAT_APP_SECRET", "secret")
}
func TestActivityPushDispatch(t *testing.T) {
	configureActivityPush(t)
	today := time.Now().In(time.FixedZone("CST", 8*3600)).Format("2006-01-02")
	for _, tc := range []struct {
		name, status, response, want string
		active, stale, changed       bool
		sends                        int
	}{
		{"success once", "READY", `{"errcode":0}`, "SENT", true, false, false, 1},
		{"explicit rejection waits for user", "READY", `{"errcode":40037}`, "FAILED", true, false, false, 1},
		{"refused", "READY", `{"errcode":43101}`, "BLOCKED", true, false, false, 1},
		{"unknown outcome", "READY", `{}`, "UNCERTAIN", true, false, false, 1},
		{"cancelled or rejoined", "READY", `{"errcode":0}`, "READY", false, true, false, 0},
		{"concurrent claim", "SENDING", `{"errcode":0}`, "SENDING", true, true, false, 0},
		{"activity removed after scan", "READY", `{"errcode":0}`, "READY", true, true, true, 0},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f := &activityPushFixture{shippingFixture: shippingFixture{status: tc.status}, home: HomeConfig{Activities: []HomeActivity{{ID: "a1", Title: "葡萄采摘", StartDate: today, EndDate: today, ReservationEnabled: true}}}, active: tc.active, stale: tc.stale, changed: tc.changed}
			db := sql.OpenDB(activityPushConnector{f})
			db.SetMaxOpenConns(1)
			defer db.Close()
			sends := 0
			s := &Server{store: &Store{db: db}, wechat: &Wechat{client: &http.Client{Transport: shippingTransport(func(r *http.Request) (*http.Response, error) {
				body := `{"access_token":"token"}`
				if !strings.Contains(r.URL.Path, "/token") {
					sends++
					body = tc.response
					var message M
					if err := json.NewDecoder(r.Body).Decode(&message); err != nil || message["touser"] != "owner" || message["page"] != "pages/activity/index?id=a1" {
						t.Errorf("invalid message %v", message)
					}
				}
				return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader(body)), Header: make(http.Header)}, nil
			})}}}
			for i := 0; i < 2; i++ {
				if _, err := s.sendActivityPushes(context.Background()); err != nil {
					t.Fatal(err)
				}
			}
			if sends != tc.sends || f.status != tc.want {
				t.Fatalf("sends %d status %s", sends, f.status)
			}
		})
	}
}
func TestActivityPushConfigurationAndDate(t *testing.T) {
	configureActivityPush(t)
	c, err := activityPushConfig()
	if err != nil {
		t.Fatal(err)
	}
	a := HomeActivity{ID: "a1", Title: strings.Repeat("葡", 30), StartDate: "2026-10-08", EndDate: "2026-10-09", ReservationEnabled: true}
	body := activityPushMessage("owner", a, c)
	data := body["data"].(M)
	if len([]rune(data["thing1"].(M)["value"].(string))) != 20 || data["thing2"].(M)["value"] != "详见活动专题" {
		t.Fatal(body)
	}
	for _, raw := range []string{`{}`, `{"thing1":"startDate"}`, `{"date1":"title"}`, `{"thing0":"title"}`, `{"number1":"title"}`} {
		t.Setenv("WECHAT_ACTIVITY_TEMPLATE_FIELDS", raw)
		if _, err := activityPushConfig(); err == nil {
			t.Fatalf("accepted %s", raw)
		}
	}
	if !activityPushEligible(a, time.Date(2026, 10, 7, 16, 0, 0, 0, time.UTC)) || activityPushEligible(a, time.Date(2026, 10, 7, 15, 59, 0, 0, time.UTC)) || activityPushEligible(a, time.Date(2026, 10, 8, 16, 0, 0, 0, time.UTC)) {
		t.Fatal("Beijing date boundary")
	}
	a.ReservationEnabled = false
	if activityPushEligible(a, time.Date(2026, 10, 8, 0, 0, 0, 0, time.UTC)) {
		t.Fatal("closed activity")
	}
}
