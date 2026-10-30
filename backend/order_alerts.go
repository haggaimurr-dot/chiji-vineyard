package main

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"log"
	"os"
	"strconv"
	"time"
)

// 异常订单自动提醒（新增）：后台每 5 分钟扫描已付款超时未发货的订单，
// 每单每种只提醒一次；订单发货/取消后自动消除。
// ORDER_SHIP_TIMEOUT_HOURS：付款后未发货超时阈值（小时），默认 48。

func orderShipTimeout() time.Duration {
	if v := os.Getenv("ORDER_SHIP_TIMEOUT_HOURS"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 {
			return time.Duration(n) * time.Hour
		}
	}
	return 48 * time.Hour
}

func (s *Store) scanAbnormalOrders(ctx context.Context) error {
	cutoff := time.Now().Add(-orderShipTimeout())
	// 1. 新增：超时未发货
	rows, err := s.db.QueryContext(ctx, `SELECT id,order_no,created_at FROM orders
		WHERE payment_status='PAID' AND fulfillment_status='UNFULFILLED' AND refund_status IN ('NONE','REJECTED') AND created_at<?`,
		cutoff)
	if err != nil {
		return err
	}
	type abnormal struct {
		id, no, created string
	}
	pending := []abnormal{}
	func() {
		defer rows.Close()
		for rows.Next() {
			var a abnormal
			var created time.Time
			if err := rows.Scan(&a.id, &a.no, &created); err != nil {
				return
			}
			a.created = created.Format("2006-01-02 15:04")
			pending = append(pending, a)
		}
	}()
	for _, a := range pending {
		detail := "订单 " + a.no + " 付款后超过 " + strconv.Itoa(int(orderShipTimeout().Hours())) + " 小时未发货（下单 " + a.created + "）"
		if _, err = s.db.ExecContext(ctx, `INSERT IGNORE INTO order_alerts(id,order_id,kind,detail) VALUES(?,?,?,?)`,
			randomID("OAL"), a.id, "SHIP_TIMEOUT", detail); err != nil {
			return err
		}
	}
	// 2. 自动消除：已发货/已取消/已关闭的订单，其未处理提醒标记为已消除
	if _, err = s.db.ExecContext(ctx, `UPDATE order_alerts SET status='RESOLVED',resolved_at=NOW(3) WHERE status='OPEN' AND kind='SHIP_TIMEOUT'
		AND order_id IN (SELECT id FROM orders WHERE fulfillment_status<>'UNFULFILLED' OR order_status IN ('CANCELLED','CLOSED'))`); err != nil {
		return err
	}
	return nil
}

func (s *Store) runOrderAlertScanner(ctx context.Context) {
	// 启动即扫描一次，重启后能补上遗漏的提醒。
	scanOnce := func() {
		job, cancel := context.WithTimeout(ctx, 60*time.Second)
		defer cancel()
		if err := s.scanAbnormalOrders(job); err != nil && ctx.Err() == nil {
			log.Printf("order alert scan: %v", err)
		}
	}
	scanOnce()
	ticker := time.NewTicker(5 * time.Minute)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			scanOnce()
		}
	}
}

type orderAlert struct {
	ID         string `json:"id"`
	OrderID    string `json:"orderId"`
	OrderNo    string `json:"orderNo"`
	Kind       string `json:"kind"`
	Detail     string `json:"detail"`
	Status     string `json:"status"`
	CreatedAt  string `json:"createdAt"`
	ResolvedBy string `json:"resolvedBy"`
}

func (s *Server) orderAlerts(ctx context.Context, raw json.RawMessage) (any, error) {
	var d struct {
		Status string `json:"status"`
		Limit  int    `json:"limit"`
	}
	if err := decode(raw, &d); err != nil {
		return nil, err
	}
	if d.Limit <= 0 || d.Limit > 100 {
		d.Limit = 20
	}
	where, args := " WHERE 1=1", []any{}
	if d.Status == "OPEN" || d.Status == "RESOLVED" {
		where += " AND a.status=?"
		args = append(args, d.Status)
	}
	args = append(args, d.Limit)
	rows, err := s.store.db.QueryContext(ctx, `SELECT a.id,a.order_id,o.order_no,a.kind,a.detail,a.status,a.created_at,a.resolved_by
		FROM order_alerts a LEFT JOIN orders o ON o.id=a.order_id`+where+` ORDER BY a.created_at DESC LIMIT ?`, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := []orderAlert{}
	for rows.Next() {
		var a orderAlert
		var created time.Time
		var orderNo sql.NullString
		if err = rows.Scan(&a.ID, &a.OrderID, &orderNo, &a.Kind, &a.Detail, &a.Status, &created, &a.ResolvedBy); err != nil {
			return nil, err
		}
		a.OrderNo = orderNo.String
		a.CreatedAt = created.Format("2006-01-02 15:04:05")
		items = append(items, a)
	}
	if err = rows.Err(); err != nil {
		return nil, err
	}
	var openCount int
	if err = s.store.db.QueryRowContext(ctx, "SELECT COUNT(*) FROM order_alerts WHERE status='OPEN'").Scan(&openCount); err != nil {
		return nil, err
	}
	return M{"items": items, "openCount": openCount}, nil
}

func (s *Server) resolveOrderAlert(ctx context.Context, actor Claims, raw json.RawMessage) (any, error) {
	var d struct {
		ID string `json:"id"`
	}
	if err := decode(raw, &d); err != nil {
		return nil, err
	}
	if d.ID == "" {
		return nil, errors.New("请选择提醒")
	}
	res, err := s.store.db.ExecContext(ctx, "UPDATE order_alerts SET status='RESOLVED',resolved_at=NOW(3),resolved_by=? WHERE id=? AND status='OPEN'", actor.Username, d.ID)
	if err != nil {
		return nil, err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return nil, errors.New("提醒不存在或已处理")
	}
	s.store.audit(ctx, actor, "ORDER_ALERT_RESOLVE", d.ID, M{})
	return M{"resolved": true}, nil
}
