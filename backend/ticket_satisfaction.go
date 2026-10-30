package main

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"strings"
)

// 客服满意度评价（新增）：工单 CLOSED 后用户可打 1–5 星并留言；
// 评价记入 ticket_satisfaction（统计用）并回写工单 payload（展示用）。

// rateTicket：用户对已关闭工单打分。
func (s *Server) rateTicket(ctx context.Context, openid string, raw json.RawMessage) (any, error) {
	var d struct {
		TicketID string `json:"ticketId"`
		Rating   int    `json:"rating"`
		Comment  string `json:"comment"`
	}
	if err := decode(raw, &d); err != nil {
		return nil, err
	}
	if d.Rating < 1 || d.Rating > 5 {
		return nil, errors.New("评分须为 1–5 星")
	}
	comment := strings.TrimSpace(d.Comment)
	if len([]rune(comment)) > 200 {
		return nil, errors.New("评价留言最多200字")
	}
	tx, err := s.store.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	var owner, orderID, status string
	var payload []byte
	if err = tx.QueryRowContext(ctx, "SELECT openid,order_id,status,payload FROM after_sales_tickets WHERE id=? FOR UPDATE", d.TicketID).Scan(&owner, &orderID, &status, &payload); err != nil {
		if err == sql.ErrNoRows {
			return nil, errors.New("工单不存在")
		}
		return nil, err
	}
	if owner != openid {
		return nil, errors.New("无权评价该工单")
	}
	if status != "CLOSED" {
		return nil, errors.New("工单关闭后才能评价")
	}
	var exists int
	if err = tx.QueryRowContext(ctx, "SELECT COUNT(*) FROM ticket_satisfaction WHERE ticket_id=?", d.TicketID).Scan(&exists); err != nil {
		return nil, err
	}
	if exists > 0 {
		return nil, errors.New("该工单已评价")
	}
	if _, err = tx.ExecContext(ctx, "INSERT INTO ticket_satisfaction(ticket_id,order_id,openid,rating,comment) VALUES(?,?,?,?,?)", d.TicketID, orderID, openid, d.Rating, comment); err != nil {
		return nil, err
	}
	satis := M{"rating": d.Rating, "comment": comment}
	if _, err = tx.ExecContext(ctx, "UPDATE after_sales_tickets SET payload=JSON_SET(payload,'$.satisfaction',CAST(? AS JSON)) WHERE id=?", jsonValue(satis), d.TicketID); err != nil {
		return nil, err
	}
	if err = tx.Commit(); err != nil {
		return nil, err
	}
	return M{"rated": true}, nil
}

// ticketSatisfactionStats：满意度统计（平均分/评价数/星级分布），默认 30 天。
func (s *Server) ticketSatisfactionStats(ctx context.Context, raw json.RawMessage) (any, error) {
	var d struct {
		Days int `json:"days"`
	}
	if err := decode(raw, &d); err != nil {
		return nil, err
	}
	if d.Days <= 0 {
		d.Days = 30
	}
	if d.Days > 365 {
		d.Days = 365
	}
	var total int
	var avg sql.NullFloat64
	if err := s.store.db.QueryRowContext(ctx, "SELECT COUNT(*),AVG(rating) FROM ticket_satisfaction WHERE created_at>=DATE_SUB(NOW(),INTERVAL ? DAY)", d.Days).Scan(&total, &avg); err != nil {
		return nil, err
	}
	rows, err := s.store.db.QueryContext(ctx, "SELECT rating,COUNT(*) FROM ticket_satisfaction WHERE created_at>=DATE_SUB(NOW(),INTERVAL ? DAY) GROUP BY rating ORDER BY rating", d.Days)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	dist := map[int]int{1: 0, 2: 0, 3: 0, 4: 0, 5: 0}
	for rows.Next() {
		var r, n int
		if err = rows.Scan(&r, &n); err != nil {
			return nil, err
		}
		dist[r] = n
	}
	if err = rows.Err(); err != nil {
		return nil, err
	}
	average := 0.0
	if avg.Valid {
		average = avg.Float64
	}
	return M{"days": d.Days, "total": total, "average": average, "distribution": dist}, nil
}
