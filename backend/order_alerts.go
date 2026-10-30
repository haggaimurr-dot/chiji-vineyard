package main

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"log"
	"os"
	"strings"
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
	// 3. 物流异常（停滞/无揽收）
	if err = s.scanLogisticsAlerts(ctx); err != nil {
		return err
	}
	// 4. 物流提醒自动消除：已收货/已取消/已关闭，或轨迹已恢复更新
	if _, err = s.db.ExecContext(ctx, `UPDATE order_alerts SET status='RESOLVED',resolved_at=NOW(3) WHERE status='OPEN' AND kind IN ('TRACKING_STUCK','NO_TRACKING')
		AND order_id IN (SELECT id FROM orders WHERE fulfillment_status='RECEIVED' OR order_status IN ('CANCELLED','CLOSED','COMPLETED'))`); err != nil {
		return err
	}
	return nil
}

// logisticsStuckHours：最新轨迹超过该时长视为停滞，默认 48 小时。
func logisticsStuckHours() time.Duration {
	if v := os.Getenv("LOGISTICS_STUCK_HOURS"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 {
			return time.Duration(n) * time.Hour
		}
	}
	return 48 * time.Hour
}

func parseTrackingTime(s string) (time.Time, bool) {
	for _, layout := range []string{time.RFC3339Nano, time.RFC3339, "2006-01-02 15:04:05", "2006-01-02 15:04", "2006/01/02 15:04:05", "2006/01/02 15:04", "2006-01-02"} {
		if t, err := time.ParseInLocation(layout, strings.TrimSpace(s), time.Local); err == nil {
			return t, true
		}
	}
	return time.Time{}, false
}

// scanLogisticsAlerts：已发货订单的物流停滞/无揽收预警。
func (s *Store) scanLogisticsAlerts(ctx context.Context) error {
	stuckCutoff := time.Now().Add(-logisticsStuckHours())
	noTrackCutoff := time.Now().Add(-24 * time.Hour)
	rows, err := s.db.QueryContext(ctx, `SELECT o.id,o.order_no,o.created_at,JSON_UNQUOTE(JSON_EXTRACT(o.payload,'$.shipment.shippedAt')),n.carrier_code,n.tracking_no,t.payload
		FROM orders o JOIN shipment_numbers n ON n.order_id=o.id
		LEFT JOIN shipment_tracking t ON t.cache_key=CONCAT(n.carrier_code,':',n.tracking_no)
		WHERE o.fulfillment_status='SHIPPED' AND o.order_status NOT IN ('CANCELLED','CLOSED','COMPLETED')`)
	if err != nil {
		return err
	}
	type shipped struct {
		id, no, created, shippedAt, trackingNo string
		raw                                   sql.NullString
	}
	list := []shipped{}
	func() {
		defer rows.Close()
		for rows.Next() {
			var sh shipped
			var created time.Time
			var shippedAt, carrier, no sql.NullString
			if err := rows.Scan(&sh.id, &no, &created, &shippedAt, &carrier, &sh.trackingNo, &sh.raw); err != nil {
				return
			}
			sh.no = no.String
			sh.created = created.Format("2006-01-02")
			sh.shippedAt = shippedAt.String
			list = append(list, sh)
		}
	}()
	for _, sh := range list {
		var latest time.Time
		var latestText string
		if sh.raw.Valid && sh.raw.String != "" && sh.raw.String != "{}" {
			var tr Tracking
			if json.Unmarshal([]byte(sh.raw.String), &tr) == nil {
				for _, e := range tr.Events {
					if t, ok := parseTrackingTime(e.Time); ok && t.After(latest) {
						latest, latestText = t, e.Time
					}
				}
			}
		}
		switch {
		case latest.IsZero():
			// 暂无轨迹：发货超 24 小时未揽收才预警（以发货时间/建单时间为准）。
			base := sh.shippedAt
			if base == "" {
				base = sh.created
			}
			if t, ok := parseTrackingTime(base); !ok || t.After(noTrackCutoff) {
				continue
			}
			if _, err = s.db.ExecContext(ctx, `INSERT IGNORE INTO order_alerts(id,order_id,kind,detail) VALUES(?,?,?,?)`,
				randomID("OAL"), sh.id, "NO_TRACKING", "订单 "+sh.no+" 发货超过 24 小时暂无揽收轨迹（"+sh.trackingNo+"），请联系承运商核实"); err != nil {
				return err
			}
		case latest.Before(stuckCutoff):
			if _, err = s.db.ExecContext(ctx, `INSERT IGNORE INTO order_alerts(id,order_id,kind,detail) VALUES(?,?,?,?)`,
				randomID("OAL"), sh.id, "TRACKING_STUCK", "订单 "+sh.no+" 物流轨迹超过 "+strconv.Itoa(int(logisticsStuckHours().Hours()))+" 小时未更新（最新："+latestText+"），请核实配送进度"); err != nil {
				return err
			}
		default:
			// 轨迹已恢复：消除该单的物流提醒。
			if _, err = s.db.ExecContext(ctx, `UPDATE order_alerts SET status='RESOLVED',resolved_at=NOW(3) WHERE status='OPEN' AND kind IN ('TRACKING_STUCK','NO_TRACKING') AND order_id=?`, sh.id); err != nil {
				return err
			}
		}
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
		s.heartbeat(ctx, "order_alert_scanner", "")
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
