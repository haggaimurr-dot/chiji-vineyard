package main

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"time"
)

var dashboardZone = time.FixedZone("Asia/Shanghai", 8*60*60)

type dashboardRange struct {
	StartDate string `json:"startDate"`
	EndDate   string `json:"endDate"`
}

func normalizeDashboardRange(d dashboardRange, now time.Time) (dashboardRange, time.Time, time.Time, error) {
	today := now.In(dashboardZone).Format("2006-01-02")
	if d.EndDate == "" {
		d.EndDate = today
	}
	end, err := time.ParseInLocation("2006-01-02", d.EndDate, dashboardZone)
	if err != nil || end.Format("2006-01-02") != d.EndDate {
		return d, time.Time{}, time.Time{}, errors.New("结束日期无效")
	}
	if d.StartDate == "" {
		d.StartDate = end.AddDate(0, 0, -29).Format("2006-01-02")
	}
	start, err := time.ParseInLocation("2006-01-02", d.StartDate, dashboardZone)
	if err != nil || start.Format("2006-01-02") != d.StartDate {
		return d, start, end, errors.New("开始日期无效")
	}
	if end.Before(start) || end.Sub(start) > 365*24*time.Hour || d.EndDate > today {
		return d, start, end, errors.New("请选择不超过366天且不晚于今天的日期范围")
	}
	return d, start.UTC(), end.AddDate(0, 0, 1).UTC(), nil
}

type dashboardDay struct {
	Date           string `json:"date"`
	GrossRevenue   int64  `json:"grossRevenue"`
	RefundedAmount int64  `json:"refundedAmount"`
	NetRevenue     int64  `json:"netRevenue"`
	PaidOrders     int64  `json:"paidOrders"`
}

// Aggregate by cohort (order creation date) and buyer. Refunds are summed before
// joining so multiple refund requests cannot multiply order amounts or counts.
const dashboardCohortSQL = `SELECT DATE_FORMAT(DATE_ADD(o.created_at, INTERVAL 8 HOUR),'%Y-%m-%d'), o.openid,
 COUNT(*),
 SUM(CASE WHEN o.payment_status IN ('PAID','REFUNDING','REFUNDED') THEN 1 ELSE 0 END),
 SUM(CASE WHEN o.payment_status IN ('PAID','REFUNDING','REFUNDED') AND o.fulfillment_status='RECEIVED' THEN 1 ELSE 0 END),
 SUM(CASE WHEN o.payment_status IN ('PAID','REFUNDING','REFUNDED') THEN o.total ELSE 0 END),
 SUM(CASE WHEN o.payment_status IN ('PAID','REFUNDING','REFUNDED') THEN COALESCE(r.amount,0) ELSE 0 END),
 SUM(CASE WHEN o.payment_status IN ('PAID','REFUNDING','REFUNDED') AND o.total>COALESCE(r.amount,0) THEN 1 ELSE 0 END)
 FROM orders o LEFT JOIN (SELECT order_id,SUM(amount) amount FROM refunds WHERE status='SUCCEEDED' GROUP BY order_id) r ON r.order_id=o.id
 WHERE o.created_at>=? AND o.created_at<?
 GROUP BY DATE_FORMAT(DATE_ADD(o.created_at, INTERVAL 8 HOUR),'%Y-%m-%d'),o.openid`

func (s *Server) dashboard(ctx context.Context, raw json.RawMessage) (any, error) {
	var d dashboardRange
	if err := decode(raw, &d); err != nil {
		return nil, err
	}
	d, start, end, err := normalizeDashboardRange(d, time.Now())
	if err != nil {
		return nil, err
	}
	tx, err := s.store.db.BeginTx(ctx, &sql.TxOptions{ReadOnly: true, Isolation: sql.LevelRepeatableRead})
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	trend := []dashboardDay{}
	indices := map[string]int{}
	for date := start.In(dashboardZone); date.Before(end); date = date.AddDate(0, 0, 1) {
		key := date.Format("2006-01-02")
		indices[key] = len(trend)
		trend = append(trend, dashboardDay{Date: key})
	}
	rows, err := tx.QueryContext(ctx, dashboardCohortSQL, start, end)
	if err != nil {
		return nil, err
	}
	var created, paid, received, gross, refunded int64
	buyers := map[string]int64{}
	for rows.Next() {
		var date, buyer string
		var c, p, r, g, f, valid int64
		if err = rows.Scan(&date, &buyer, &c, &p, &r, &g, &f, &valid); err != nil {
			rows.Close()
			return nil, err
		}
		created += c
		paid += p
		received += r
		gross += g
		refunded += f
		if buyer != "" && valid > 0 {
			buyers[buyer] += valid
		}
		if i, ok := indices[date]; ok {
			trend[i].GrossRevenue += g
			trend[i].RefundedAmount += f
			trend[i].NetRevenue += g - f
			trend[i].PaidOrders += p
		}
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return nil, err
	}
	repeat := 0
	for _, count := range buyers {
		if count >= 2 {
			repeat++
		}
	}
	var shipping, refunds, leads, products, articles int
	for _, q := range []struct {
		query string
		out   *int
	}{
		{"SELECT COUNT(*) FROM orders WHERE payment_status='PAID' AND fulfillment_status IN ('UNFULFILLED','PREPARING') AND refund_status IN ('NONE','REJECTED','PARTIAL')", &shipping},
		{"SELECT COUNT(*) FROM refunds WHERE status='APPLIED'", &refunds},
		{"SELECT COUNT(*) FROM partner_leads WHERE status='NEW'", &leads},
		{"SELECT COUNT(*) FROM products", &products},
		{"SELECT COUNT(*) FROM articles WHERE status='PUBLISHED'", &articles},
	} {
		if err = tx.QueryRowContext(ctx, q.query).Scan(q.out); err != nil {
			return nil, err
		}
	}
	behavior, err := dashboardBehavior(ctx, tx, start, end)
	if err != nil {
		return nil, err
	}
	// Previous-period comparison: same length, immediately before the range.
	prevEnd := start
	prevStart := start.Add(-end.Sub(start))
	var prevGross, prevRefunded, prevPaid int64
	if err = tx.QueryRowContext(ctx, `SELECT COALESCE(SUM(CASE WHEN o.payment_status IN ('PAID','REFUNDING','REFUNDED') THEN o.total ELSE 0 END),0),
		COALESCE(SUM(CASE WHEN o.payment_status IN ('PAID','REFUNDING','REFUNDED') THEN COALESCE(r.amount,0) ELSE 0 END),0),
		COALESCE(SUM(CASE WHEN o.payment_status IN ('PAID','REFUNDING','REFUNDED') THEN 1 ELSE 0 END),0)
		FROM orders o LEFT JOIN (SELECT order_id,SUM(amount) amount FROM refunds WHERE status='SUCCEEDED' GROUP BY order_id) r ON r.order_id=o.id
		WHERE o.created_at>=? AND o.created_at<?`, prevStart, prevEnd).Scan(&prevGross, &prevRefunded, &prevPaid); err != nil {
		return nil, err
	}
	if err = tx.Commit(); err != nil {
		return nil, err
	}
	rate := 0.0
	if len(buyers) > 0 {
		rate = float64(repeat) / float64(len(buyers)) * 100
	}
	return M{"startDate": d.StartDate, "endDate": d.EndDate, "revenue": gross - refunded, "grossRevenue": gross, "refundedAmount": refunded, "paidOrders": paid,
		"previous": M{"grossRevenue": prevGross, "refundedAmount": prevRefunded, "netRevenue": prevGross - prevRefunded, "paidOrders": prevPaid},
		"pendingShipment": shipping, "pendingRefund": refunds, "newLeads": leads, "products": products, "articles": articles,
		"buyers": len(buyers), "repeatBuyers": repeat, "repurchaseRate": rate, "trend": trend, "behaviorFunnel": behavior,
		"funnel": M{"created": created, "paid": paid, "received": received}}, nil
}
