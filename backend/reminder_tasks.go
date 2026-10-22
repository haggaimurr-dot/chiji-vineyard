package main

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"time"
)

type ReminderTask struct {
	OpenID      string `json:"openid"`
	ProductID   string `json:"productId"`
	ProductName string `json:"productName"`
	Kind        string `json:"kind"`
	Status      string `json:"status"`
	Detail      string `json:"detail"`
	UpdatedAt   string `json:"updatedAt"`
}

const reminderTasksSQL = `(SELECT n.openid,n.product_id,COALESCE(p.name,n.product_id) product_name,'restock' kind,n.push_status status,n.push_error detail,n.updated_at FROM restock_notifications n LEFT JOIN products p ON p.id=n.product_id UNION ALL SELECT n.openid,n.product_id,COALESCE(p.name,n.product_id),'pricedrop',n.push_status,n.push_error,n.updated_at FROM price_drop_notifications n LEFT JOIN products p ON p.id=n.product_id) t`

func (s *Store) reminderTasks(ctx context.Context, raw json.RawMessage) (any, error) {
	var d struct {
		Kind   string `json:"kind"`
		Status string `json:"status"`
		Offset int    `json:"offset"`
		Limit  int    `json:"limit"`
	}
	if err := decode(raw, &d); err != nil {
		return nil, err
	}
	if d.Limit == 0 {
		d.Limit = 20
	}
	if d.Offset < 0 || d.Offset > 1000000 || d.Limit < 1 || d.Limit > 50 || (d.Kind != "" && d.Kind != "restock" && d.Kind != "pricedrop") {
		return nil, errors.New("任务筛选或分页参数无效")
	}
	valid := map[string]bool{"NONE": true, "READY": true, "SENDING": true, "SENT": true, "FAILED": true, "BLOCKED": true, "UNCERTAIN": true, "CANCELLED": true, "issues": true}
	if d.Status != "" && !valid[d.Status] {
		return nil, errors.New("任务状态无效")
	}
	counts := M{}
	rows, err := s.db.QueryContext(ctx, "SELECT status,COUNT(*) FROM "+reminderTasksSQL+" GROUP BY status")
	if err != nil {
		return nil, err
	}
	for rows.Next() {
		var status string
		var count int
		if err = rows.Scan(&status, &count); err != nil {
			rows.Close()
			return nil, err
		}
		counts[status] = count
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return nil, err
	}
	where, args := " WHERE 1=1", []any{}
	if d.Kind != "" {
		where += " AND kind=?"
		args = append(args, d.Kind)
	}
	if d.Status == "issues" {
		where += " AND status IN ('FAILED','BLOCKED','UNCERTAIN','SENDING')"
	} else if d.Status != "" {
		where += " AND status=?"
		args = append(args, d.Status)
	}
	args = append(args, d.Limit+1, d.Offset)
	rows, err = s.db.QueryContext(ctx, "SELECT openid,product_id,product_name,kind,status,detail,updated_at FROM "+reminderTasksSQL+where+" ORDER BY updated_at DESC,kind,openid,product_id LIMIT ? OFFSET ?", args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := []ReminderTask{}
	for rows.Next() {
		var t ReminderTask
		var at time.Time
		if err = rows.Scan(&t.OpenID, &t.ProductID, &t.ProductName, &t.Kind, &t.Status, &t.Detail, &at); err != nil {
			return nil, err
		}
		t.UpdatedAt = at.UTC().Format(time.RFC3339Nano)
		items = append(items, t)
	}
	if err = rows.Err(); err != nil {
		return nil, err
	}
	more := len(items) > d.Limit
	if more {
		items = items[:d.Limit]
	}
	return M{"items": items, "counts": counts, "hasMore": more}, nil
}

func reminderResolution(status, action string) (string, error) {
	switch action {
	case "retry":
		if status == "FAILED" {
			return "READY", nil
		}
	case "sent":
		if status == "UNCERTAIN" {
			return "SENT", nil
		}
	case "stop":
		if status == "FAILED" || status == "BLOCKED" || status == "UNCERTAIN" {
			return "CANCELLED", nil
		}
	}
	return "", errors.New("任务状态已变化或不支持此处置；发送中不能操作，授权失效需用户重新订阅")
}

func (s *Store) resolveReminderTask(ctx context.Context, actor Claims, raw json.RawMessage) (any, error) {
	var d struct {
		OpenID    string `json:"openid"`
		ProductID string `json:"productId"`
		Kind      string `json:"kind"`
		Status    string `json:"status"`
		Action    string `json:"action"`
		Note      string `json:"note"`
	}
	if err := decode(raw, &d); err != nil {
		return nil, err
	}
	d.Note = strings.TrimSpace(d.Note)
	if d.OpenID == "" || d.ProductID == "" || (d.Kind != "restock" && d.Kind != "pricedrop") || d.Note == "" || len([]rune(d.Note)) > 240 {
		return nil, errors.New("请选择任务并填写 1–240 字处置备注")
	}
	table := "restock_notifications"
	if d.Kind == "pricedrop" {
		table = "price_drop_notifications"
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	var status string
	if err = tx.QueryRowContext(ctx, "SELECT push_status FROM "+table+" WHERE openid=? AND product_id=? FOR UPDATE", d.OpenID, d.ProductID).Scan(&status); err != nil {
		return nil, err
	}
	if status != d.Status {
		return nil, errors.New("任务状态已变化，请刷新后处置")
	}
	next, err := reminderResolution(status, d.Action)
	if err != nil {
		return nil, err
	}
	if next == "READY" {
		var active int
		if err = tx.QueryRowContext(ctx, "SELECT COUNT(*) FROM product_attention WHERE openid=? AND product_id=? AND kind=?", d.OpenID, d.ProductID, d.Kind).Scan(&active); err != nil {
			return nil, err
		}
		if active == 0 {
			return nil, errors.New("用户已取消提醒，不能重新排队")
		}
	}
	if _, err = tx.ExecContext(ctx, "UPDATE "+table+" SET push_status=?,push_error=? WHERE openid=? AND product_id=?", next, clean("人工处置："+d.Note, 240), d.OpenID, d.ProductID); err != nil {
		return nil, err
	}
	// Preserve failed attempts; only annotate an unresolved send after verification.
	if status == "UNCERTAIN" {
		if _, err = tx.ExecContext(ctx, "UPDATE product_reminder_history SET status=?,detail=? WHERE openid=? AND product_id=? AND kind=? AND status IN ('SENDING','UNCERTAIN')", next, clean("人工核实："+d.Note, 240), d.OpenID, d.ProductID, d.Kind); err != nil {
			return nil, err
		}
	}
	if _, err = tx.ExecContext(ctx, "INSERT INTO audit_logs(admin_id,username,action,target_id,detail) VALUES(?,?,?,?,?)", actor.Subject, actor.Username, "REMINDER_RESOLVE", d.ProductID, jsonValue(M{"openid": d.OpenID, "kind": d.Kind, "before": status, "after": next, "note": d.Note})); err != nil {
		return nil, err
	}
	return M{"status": next}, tx.Commit()
}
