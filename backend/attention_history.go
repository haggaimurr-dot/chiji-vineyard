package main

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"time"
)

type AttentionHistory struct {
	ID           string `json:"id"`
	ProductID    string `json:"productId"`
	ProductName  string `json:"productName"`
	Kind         string `json:"kind"`
	SKUName      string `json:"skuName"`
	CurrentPrice int    `json:"currentPrice"`
	TargetPrice  int    `json:"targetPrice"`
	Status       string `json:"status"`
	Detail       string `json:"detail"`
	CreatedAt    string `json:"createdAt"`
}

func (s *Store) attentionHistory(ctx context.Context, openid string, raw json.RawMessage) (any, error) {
	var d struct {
		Offset int `json:"offset"`
		Limit  int `json:"limit"`
	}
	if err := decode(raw, &d); err != nil {
		return nil, err
	}
	if d.Limit == 0 {
		d.Limit = 20
	}
	if d.Offset < 0 || d.Offset > 1000000 || d.Limit < 1 || d.Limit > 50 {
		return nil, errors.New("历史分页参数无效")
	}
	rows, err := s.db.QueryContext(ctx, "SELECT id,product_id,product_name,kind,sku_name,current_price,target_price,status,detail,created_at FROM product_reminder_history WHERE openid=? ORDER BY created_at DESC,id DESC LIMIT ? OFFSET ?", openid, d.Limit+1, d.Offset)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := []AttentionHistory{}
	for rows.Next() {
		var h AttentionHistory
		var at time.Time
		if err = rows.Scan(&h.ID, &h.ProductID, &h.ProductName, &h.Kind, &h.SKUName, &h.CurrentPrice, &h.TargetPrice, &h.Status, &h.Detail, &at); err != nil {
			return nil, err
		}
		h.CreatedAt = at.UTC().Format(time.RFC3339Nano)
		items = append(items, h)
	}
	if err = rows.Err(); err != nil {
		return nil, err
	}
	more := len(items) > d.Limit
	if more {
		items = items[:d.Limit]
	}
	return M{"items": items, "hasMore": more, "offset": d.Offset}, nil
}

// Claim and snapshot are committed together before contacting WeChat. A crash
// leaves a visible pending result instead of silently losing the send attempt.
func (s *Store) claimReminder(ctx context.Context, h AttentionHistory, openid, query string, args ...any) (sql.Result, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	result, err := tx.ExecContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	count, err := result.RowsAffected()
	if err != nil {
		return nil, err
	}
	if count > 0 {
		_, err = tx.ExecContext(ctx, "INSERT INTO product_reminder_history(id,openid,product_id,product_name,kind,sku_name,current_price,target_price,status) VALUES(?,?,?,?,?,?,?,?,'SENDING')", h.ID, openid, h.ProductID, h.ProductName, h.Kind, h.SKUName, h.CurrentPrice, h.TargetPrice)
		if err != nil {
			return nil, err
		}
	}
	return result, tx.Commit()
}

func (s *Store) finishReminder(ctx context.Context, historyID, kind, status, detail, openid, productID string) error {
	table := "restock_notifications"
	if kind == "pricedrop" {
		table = "price_drop_notifications"
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if _, err = tx.ExecContext(ctx, "UPDATE "+table+" SET push_status=?,push_error=? WHERE openid=? AND product_id=? AND push_status='SENDING'", status, clean(detail, 240), openid, productID); err != nil {
		return err
	}
	if _, err = tx.ExecContext(ctx, "UPDATE product_reminder_history SET status=?,detail=? WHERE id=? AND openid=? AND status='SENDING'", status, clean(detail, 240), historyID, openid); err != nil {
		return err
	}
	return tx.Commit()
}

func (s *Store) setPriceDropTarget(ctx context.Context, openid string, raw json.RawMessage) (any, error) {
	var d struct {
		ProductID   string `json:"productId"`
		TargetPrice *int   `json:"targetPrice"`
	}
	if err := decode(raw, &d); err != nil {
		return nil, err
	}
	if d.ProductID == "" || d.TargetPrice == nil || *d.TargetPrice < 0 || *d.TargetPrice > 100000000 {
		return nil, errors.New("目标价格无效")
	}
	// Zero explicitly restores the default price-drop condition. Sent grants are
	// never rearmed; unresolved sends cannot have their condition changed.
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	var baseline int
	var status string
	err = tx.QueryRowContext(ctx, "SELECT n.baseline_price,n.push_status FROM price_drop_notifications n JOIN product_attention a ON a.openid=n.openid AND a.product_id=n.product_id AND a.kind='pricedrop' WHERE n.openid=? AND n.product_id=? FOR UPDATE", openid, d.ProductID).Scan(&baseline, &status)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, errors.New("请先设置降价提醒")
	}
	if err != nil {
		return nil, err
	}
	if status == "SENDING" || status == "UNCERTAIN" || status == "CANCELLED" || *d.TargetPrice > baseline {
		return nil, errors.New("提醒不存在、目标价高于关注价或发送结果待核实，请刷新后重试")
	}
	if _, err = tx.ExecContext(ctx, "UPDATE price_drop_notifications SET target_price=? WHERE openid=? AND product_id=?", *d.TargetPrice, openid, d.ProductID); err != nil {
		return nil, err
	}
	return M{"targetPrice": *d.TargetPrice}, tx.Commit()
}
