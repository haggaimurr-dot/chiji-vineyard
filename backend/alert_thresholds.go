package main

import (
	"context"
	"encoding/json"
	"errors"
	"log"
	"strconv"
	"time"
)

// 关键指标告警阈值可配：阈值存 configs('alert.thresholds')，定时扫描超标即写入 payment_alerts。
type alertThresholds struct {
	RefundStuckHours     int `json:"refundStuckHours"`
	InvoiceStuckHours    int `json:"invoiceStuckHours"`
	OpenAlertCount       int `json:"openAlertCount"`
	ReminderFailureCount int `json:"reminderFailureCount"`
}

func defaultAlertThresholds() alertThresholds {
	return alertThresholds{RefundStuckHours: 24, InvoiceStuckHours: 48, OpenAlertCount: 50, ReminderFailureCount: 100}
}

func (s *Server) getAlertThresholds(ctx context.Context) (alertThresholds, error) {
	t := defaultAlertThresholds()
	var raw []byte
	if err := s.store.db.QueryRowContext(ctx, "SELECT payload FROM configs WHERE config_key='alert.thresholds'").Scan(&raw); err == nil {
		_ = json.Unmarshal(raw, &t)
	}
	if t.RefundStuckHours < 1 || t.RefundStuckHours > 720 {
		t.RefundStuckHours = 24
	}
	if t.InvoiceStuckHours < 1 || t.InvoiceStuckHours > 720 {
		t.InvoiceStuckHours = 48
	}
	if t.OpenAlertCount < 1 || t.OpenAlertCount > 100000 {
		t.OpenAlertCount = 50
	}
	if t.ReminderFailureCount < 1 || t.ReminderFailureCount > 100000 {
		t.ReminderFailureCount = 100
	}
	return t, nil
}

func (s *Server) saveAlertThresholds(ctx context.Context, actor Claims, raw json.RawMessage) (any, error) {
	var t alertThresholds
	if err := decode(raw, &t); err != nil {
		return nil, err
	}
	if t.RefundStuckHours < 1 || t.RefundStuckHours > 720 || t.InvoiceStuckHours < 1 || t.InvoiceStuckHours > 720 ||
		t.OpenAlertCount < 1 || t.OpenAlertCount > 100000 || t.ReminderFailureCount < 1 || t.ReminderFailureCount > 100000 {
		return nil, errors.New("阈值超出允许范围（时长 1–720 小时，数量 1–100000）")
	}
	if _, err := s.store.db.ExecContext(ctx, "INSERT INTO configs(config_key,payload) VALUES('alert.thresholds',?) ON DUPLICATE KEY UPDATE payload=VALUES(payload)", jsonValue(t)); err != nil {
		return nil, err
	}
	if _, err := s.store.db.ExecContext(ctx, "INSERT INTO audit_logs(admin_id,username,action,target_id,detail) VALUES(?,?,?,?,?)",
		actor.Subject, actor.Username, "ALERT_THRESHOLDS_SAVE", "alert.thresholds", jsonValue(t)); err != nil {
		return nil, err
	}
	return t, nil
}

func (s *Server) scanAlertThresholds(ctx context.Context) error {
	t, err := s.getAlertThresholds(ctx)
	if err != nil {
		return err
	}
	// 1. 退款超时未处理
	var stuckRefunds int
	if err = s.store.db.QueryRowContext(ctx, "SELECT COUNT(*) FROM refunds WHERE status IN ('APPROVED','PROCESSING') AND created_at < NOW(3) - INTERVAL ? HOUR", t.RefundStuckHours).Scan(&stuckRefunds); err != nil {
		return err
	}
	if stuckRefunds > 0 {
		if err = s.paymentAlert(ctx, "THRESHOLD_REFUND_STUCK", "refunds", strconv.Itoa(stuckRefunds)+" 笔退款超过 "+strconv.Itoa(t.RefundStuckHours)+" 小时未处理"); err != nil {
			return err
		}
	}
	// 2. 发票超时未开
	rows, err := s.store.db.QueryContext(ctx, "SELECT JSON_UNQUOTE(JSON_EXTRACT(payload,'$.createdAt')) FROM invoice_requests WHERE JSON_UNQUOTE(JSON_EXTRACT(payload,'$.status'))='APPLIED'")
	if err != nil {
		return err
	}
	cutoff := time.Now().UTC().Add(-time.Duration(t.InvoiceStuckHours) * time.Hour)
	stuckInvoices := 0
	for rows.Next() {
		var created string
		if err = rows.Scan(&created); err != nil {
			rows.Close()
			return err
		}
		if ts, e := time.Parse(time.RFC3339Nano, created); e == nil && ts.Before(cutoff) {
			stuckInvoices++
		}
	}
	rows.Close()
	if stuckInvoices > 0 {
		if err = s.paymentAlert(ctx, "THRESHOLD_INVOICE_STUCK", "invoices", strconv.Itoa(stuckInvoices)+" 张发票超过 "+strconv.Itoa(t.InvoiceStuckHours)+" 小时未开具"); err != nil {
			return err
		}
	}
	// 3. 告警积压
	var openAlerts int
	if err = s.store.db.QueryRowContext(ctx, "SELECT COUNT(*) FROM payment_alerts WHERE status='OPEN'").Scan(&openAlerts); err != nil {
		return err
	}
	if openAlerts > t.OpenAlertCount {
		if err = s.paymentAlert(ctx, "THRESHOLD_ALERT_BACKLOG", "alerts", "未处理告警 "+strconv.Itoa(openAlerts)+" 条，超过阈值 "+strconv.Itoa(t.OpenAlertCount)); err != nil {
			return err
		}
	}
	// 4. 提醒失败积压
	var reminderFailures int
	if err = s.store.db.QueryRowContext(ctx, "SELECT (SELECT COUNT(*) FROM restock_notifications WHERE push_status IN ('FAILED','BLOCKED'))+(SELECT COUNT(*) FROM price_drop_notifications WHERE push_status IN ('FAILED','BLOCKED'))").Scan(&reminderFailures); err != nil {
		return err
	}
	if reminderFailures > t.ReminderFailureCount {
		if err = s.paymentAlert(ctx, "THRESHOLD_REMINDER_FAILURES", "reminders", "提醒发送失败积压 "+strconv.Itoa(reminderFailures)+" 条，超过阈值 "+strconv.Itoa(t.ReminderFailureCount)); err != nil {
			return err
		}
	}
	return nil
}

func (s *Server) runAlertThresholdScanner(ctx context.Context) {
	job, cancel := context.WithTimeout(ctx, 60*time.Second)
	if err := s.scanAlertThresholds(job); err != nil && ctx.Err() == nil {
		log.Printf("alert threshold scan: %v", err)
	}
	cancel()
	ticker := time.NewTicker(15 * time.Minute)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			job, cancel := context.WithTimeout(ctx, 60*time.Second)
			if err := s.scanAlertThresholds(job); err != nil && ctx.Err() == nil {
				log.Printf("alert threshold scan: %v", err)
			}
			cancel()
		}
	}
}
