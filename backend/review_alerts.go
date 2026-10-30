package main

import (
	"context"
	"encoding/json"
	"errors"
	"log"
	"time"
)

// 差评自动提醒：后台每 5 分钟扫描新增的低分评价（≤2 星），每条评价只提醒一次。
func (s *Store) scanLowRatingReviews(ctx context.Context) error {
	rows, err := s.db.QueryContext(ctx, `SELECT id,product_id,order_id,rating,content,created_at FROM product_reviews
		WHERE rating<=2 AND created_at>=? AND id NOT IN (SELECT id FROM review_alerts)`,
		time.Now().UTC().Add(-10*time.Minute).Format(time.RFC3339Nano))
	if err != nil {
		return err
	}
	defer rows.Close()
	for rows.Next() {
		var id, productID, orderID, content, createdAt string
		var rating int
		if err = rows.Scan(&id, &productID, &orderID, &rating, &content, &createdAt); err != nil {
			return err
		}
		if _, err = s.db.ExecContext(ctx, `INSERT IGNORE INTO review_alerts(id,product_id,order_id,rating,content,created_at) VALUES(?,?,?,?,?,?)`,
			id, productID, orderID, rating, content, createdAt); err != nil {
			return err
		}
	}
	return rows.Err()
}

func (s *Store) runReviewAlertScanner(ctx context.Context) {
	ticker := time.NewTicker(5 * time.Minute)
	defer ticker.Stop()
	for {
		job, cancel := context.WithTimeout(ctx, 60*time.Second)
		if err := s.scanLowRatingReviews(job); err != nil && ctx.Err() == nil {
			log.Printf("review alert scan: %v", err)
		}
		cancel()
		s.heartbeat(ctx, "review_alert_scanner", "")
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
	}
}

type reviewAlert struct {
	ID        string `json:"id"`
	ProductID string `json:"productId"`
	OrderID   string `json:"orderId"`
	Rating    int    `json:"rating"`
	Content   string `json:"content"`
	Status    string `json:"status"`
	CreatedAt string `json:"createdAt"`
}

func (s *Server) reviewAlerts(ctx context.Context, raw json.RawMessage) (any, error) {
	var d struct {
		Status string `json:"status"`
	}
	_ = decode(raw, &d)
	where := " WHERE status='OPEN'"
	if d.Status == "RESOLVED" {
		where = " WHERE status='RESOLVED'"
	} else if d.Status == "ALL" {
		where = ""
	}
	rows, err := s.store.db.QueryContext(ctx, "SELECT id,product_id,order_id,rating,content,status,created_at FROM review_alerts"+where+" ORDER BY created_at DESC LIMIT 200")
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := []reviewAlert{}
	for rows.Next() {
		var a reviewAlert
		if err = rows.Scan(&a.ID, &a.ProductID, &a.OrderID, &a.Rating, &a.Content, &a.Status, &a.CreatedAt); err != nil {
			return nil, err
		}
		items = append(items, a)
	}
	return M{"items": items}, rows.Err()
}

func (s *Server) reviewAlertCount(ctx context.Context) (any, error) {
	var n int
	if err := s.store.db.QueryRowContext(ctx, "SELECT COUNT(*) FROM review_alerts WHERE status='OPEN'").Scan(&n); err != nil {
		return nil, err
	}
	return M{"count": n}, nil
}

func (s *Server) resolveReviewAlert(ctx context.Context, actor Claims, raw json.RawMessage) (any, error) {
	id := mustID(raw, "id")
	res, err := s.store.db.ExecContext(ctx, "UPDATE review_alerts SET status='RESOLVED',resolved_by=? WHERE id=? AND status='OPEN'", actor.Username, id)
	if err != nil {
		return nil, err
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return nil, errors.New("提醒不存在或已处理")
	}
	return M{"id": id}, nil
}
