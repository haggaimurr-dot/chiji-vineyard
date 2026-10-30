package main

import (
	"context"
	"encoding/json"
	"errors"
)

// 提醒触达率统计：按提醒类型聚合发送状态，并统计提醒消息带来的点击回流。
func (s *Store) reminderStats(ctx context.Context) (any, error) {
	type kindStat struct {
		Kind      string         `json:"kind"`
		ByStatus  map[string]int `json:"byStatus"`
		Total     int            `json:"total"`
		Sent      int            `json:"sent"`
		Clicked   int            `json:"clicked"`
		ClickRate float64        `json:"clickRate"`
	}
	stats := []kindStat{}
	for _, k := range []struct {
		kind  string
		table string
	}{{"restock", "restock_notifications"}, {"pricedrop", "price_drop_notifications"}} {
		rows, err := s.db.QueryContext(ctx, "SELECT push_status,COUNT(*),SUM(CASE WHEN push_status='SENT' THEN 1 ELSE 0 END),SUM(CASE WHEN clicked_at IS NOT NULL THEN 1 ELSE 0 END) FROM "+k.table+" GROUP BY push_status")
		if err != nil {
			return nil, err
		}
		st := kindStat{Kind: k.kind, ByStatus: map[string]int{}}
		for rows.Next() {
			var status string
			var n, sent, clicked int
			if err = rows.Scan(&status, &n, &sent, &clicked); err != nil {
				rows.Close()
				return nil, err
			}
			st.ByStatus[status] = n
			st.Total += n
			st.Sent += sent
			st.Clicked += clicked
		}
		if err = rows.Err(); err != nil {
			rows.Close()
			return nil, err
		}
		rows.Close()
		if st.Sent > 0 {
			st.ClickRate = float64(st.Clicked) / float64(st.Sent) * 100
		}
		stats = append(stats, st)
	}
	return M{"items": stats}, nil
}

// 用户打开商品详情时上报（提醒消息场景带 kind+productId），记录点击回流。
func (s *Store) recordReminderClick(ctx context.Context, openid string, raw json.RawMessage) (any, error) {
	var d struct {
		Kind      string `json:"kind"`
		ProductID string `json:"productId"`
	}
	if err := decode(raw, &d); err != nil {
		return nil, err
	}
	d.ProductID = clean(d.ProductID, 80)
	if d.ProductID == "" {
		return nil, errors.New("商品无效")
	}
	table := ""
	switch d.Kind {
	case "restock":
		table = "restock_notifications"
	case "pricedrop":
		table = "price_drop_notifications"
	default:
		return nil, errors.New("提醒类型无效")
	}
	if _, err := s.db.ExecContext(ctx, "UPDATE "+table+" SET clicked_at=COALESCE(clicked_at,NOW(3)) WHERE openid=? AND product_id=?", openid, d.ProductID); err != nil {
		return nil, err
	}
	return M{"ok": true}, nil
}
