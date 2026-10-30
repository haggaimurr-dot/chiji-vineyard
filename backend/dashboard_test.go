package main

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"
)

func TestDashboardChinaRange(t *testing.T) {
	now := time.Date(2026, 10, 5, 16, 0, 0, 0, time.UTC)
	d, start, end, err := normalizeDashboardRange(dashboardRange{}, now)
	if err != nil || d.EndDate != "2026-10-06" || d.StartDate != "2026-09-07" || start.Format(time.RFC3339) != "2026-09-06T16:00:00Z" || end.Format(time.RFC3339) != "2026-10-06T16:00:00Z" {
		t.Fatalf("wrong range: %+v %v %v %v", d, start, end, err)
	}
	for _, d := range []dashboardRange{{StartDate: "2026-02-30"}, {EndDate: "2026-10-7"}, {EndDate: "2026-10-07"}, {StartDate: "2026-10-06", EndDate: "2026-10-05"}, {StartDate: "2025-01-01"}} {
		if _, _, _, err := normalizeDashboardRange(d, now); err == nil {
			t.Fatalf("accepted invalid range: %+v", d)
		}
	}
}

type dashboardFixture struct {
	orderPageFixture
	empty, fail, failBehavior bool
}
type dashboardConnector struct{ fixture *dashboardFixture }

func (c dashboardConnector) Connect(context.Context) (driver.Conn, error) { return c.fixture, nil }
func (c dashboardConnector) Driver() driver.Driver                        { return orderPageDriver{} }
func (d *dashboardFixture) QueryContext(_ context.Context, q string, args []driver.NamedValue) (driver.Rows, error) {
	if !d.active {
		return nil, errors.New("query outside snapshot")
	}
	if q == dashboardBehaviorSQL {
		if d.failBehavior {
			return nil, errors.New("analytics unavailable")
		}
		if len(args) != 5 || args[0].Value.(time.Time).Format(time.RFC3339) != "2026-09-17T16:00:00Z" || args[4].Value.(time.Time).Format(time.RFC3339) != "2026-09-20T16:00:00Z" {
			return nil, errors.New("wrong behavior boundaries")
		}
		values := []driver.Value{int64(4), int64(3), int64(2), int64(2), int64(1)}
		if d.empty {
			values = []driver.Value{int64(0), int64(0), int64(0), int64(0), int64(0)}
		}
		return &shippingRows{cols: []string{"visitors", "cart", "created", "paid", "received"}, values: [][]driver.Value{values}}, nil
	}
	if q == "SELECT started_at FROM commerce_analytics_state WHERE id=1" {
		return singleShippingRow(time.Date(2026, 9, 18, 0, 0, 0, 0, time.UTC)), nil
	}
	if q == dashboardCohortSQL {
		if args[0].Value.(time.Time).Format(time.RFC3339) != "2026-09-17T16:00:00Z" || args[1].Value.(time.Time).Format(time.RFC3339) != "2026-09-20T16:00:00Z" {
			return nil, errors.New("wrong date boundaries")
		}
		if d.fail {
			return nil, errors.New("database unavailable")
		}
		rows := &shippingRows{cols: []string{"day", "buyer", "created", "paid", "received", "gross", "refunded", "valid"}}
		if !d.empty {
			rows.values = [][]driver.Value{
				{"2026-09-18", "buyer-a", int64(2), int64(2), int64(1), int64(20000), int64(3000), int64(2)},
				{"2026-09-19", "buyer-b", int64(3), int64(1), int64(0), int64(10000), int64(10000), int64(0)},
				{"2026-09-19", "buyer-c", int64(1), int64(1), int64(1), int64(5000), int64(0), int64(1)},
			}
		}
		return rows, nil
	}
	if strings.HasPrefix(q, "SELECT COUNT(*)") {
		return singleShippingRow(int64(2)), nil
	}
	if strings.HasPrefix(q, "SELECT COALESCE(SUM(CASE WHEN o.payment_status") {
		if len(args) != 2 || args[0].Value.(time.Time).Format(time.RFC3339) != "2026-09-14T16:00:00Z" || args[1].Value.(time.Time).Format(time.RFC3339) != "2026-09-17T16:00:00Z" {
			return nil, errors.New("wrong previous-period boundaries")
		}
		return singleShippingRow(int64(10000), int64(1000), int64(3)), nil
	}
	return nil, errors.New("unexpected query")
}

func TestDashboardAggregatesAndSnapshot(t *testing.T) {
	for _, empty := range []bool{false, true} {
		state := &dashboardFixture{empty: empty}
		db := sql.OpenDB(dashboardConnector{state})
		result, err := (&Server{store: &Store{db: db}}).dashboard(context.Background(), json.RawMessage(`{"startDate":"2026-09-18","endDate":"2026-09-20"}`))
		db.Close()
		if err != nil {
			t.Fatal(err)
		}
		m := result.(M)
		if !state.committed || !state.snapshot || m["pendingShipment"] != 2 {
			t.Fatalf("wrong snapshot/backlog: %+v", m)
		}
		trend := m["trend"].([]dashboardDay)
		behavior := m["behaviorFunnel"].(M)
		if behavior["partial"] != true || behavior["startedAt"] != "2026-09-18T00:00:00Z" {
			t.Fatal("missing partial coverage")
		}
		if empty && behavior["visitors"] != int64(0) {
			t.Fatal("nonzero empty visitors")
		}
		if !empty && (behavior["visitors"] != int64(4) || behavior["cartUsers"] != int64(3) || behavior["createdUsers"] != int64(2) || behavior["paidUsers"] != int64(2) || behavior["receivedUsers"] != int64(1)) {
			t.Fatal("wrong behavior funnel")
		}
		if len(trend) != 3 || trend[2].NetRevenue != 0 {
			t.Fatal("missing zero-sale days")
		}
		if empty {
			if m["repurchaseRate"] != 0.0 || m["revenue"] != int64(0) || m["buyers"] != 0 {
				t.Fatal("empty range is not zero")
			}
		} else {
			if m["revenue"] != int64(22000) || m["grossRevenue"] != int64(35000) || m["refundedAmount"] != int64(13000) || m["buyers"] != 2 || m["repeatBuyers"] != 1 || m["repurchaseRate"] != 50.0 {
				t.Fatalf("wrong totals: %+v", m)
			}
			f := m["funnel"].(M)
			if f["created"] != int64(6) || f["paid"] != int64(4) || f["received"] != int64(2) || trend[0].NetRevenue+trend[1].NetRevenue != m["revenue"] {
				t.Fatal("funnel or daily totals differ")
			}
			prev := m["previous"].(M)
			if prev["grossRevenue"] != int64(10000) || prev["refundedAmount"] != int64(1000) || prev["netRevenue"] != int64(9000) || prev["paidOrders"] != int64(3) {
				t.Fatalf("wrong previous-period comparison: %+v", prev)
			}
		}
	}
	state := &dashboardFixture{fail: true}
	db := sql.OpenDB(dashboardConnector{state})
	defer db.Close()
	if _, err := (&Server{store: &Store{db: db}}).dashboard(context.Background(), json.RawMessage(`{"startDate":"2026-09-18","endDate":"2026-09-20"}`)); err == nil || state.committed || state.active {
		t.Fatal("failed dashboard did not roll back")
	}
	behaviorState := &dashboardFixture{failBehavior: true}
	behaviorDB := sql.OpenDB(dashboardConnector{behaviorState})
	defer behaviorDB.Close()
	if _, err := (&Server{store: &Store{db: behaviorDB}}).dashboard(context.Background(), json.RawMessage(`{"startDate":"2026-09-18","endDate":"2026-09-20"}`)); err == nil || behaviorState.committed || behaviorState.active {
		t.Fatal("analytics failure did not roll back snapshot")
	}
}
