package main

import (
	"context"
	"encoding/json"
	"errors"
	"time"
)

type ActivityNotice struct {
	Kind       string `json:"kind"`
	ID         string `json:"id"`
	ActivityID string `json:"activityId"`
	Title      string `json:"title"`
	StartDate  string `json:"startDate"`
	EndDate    string `json:"endDate"`
	CreatedAt  string `json:"createdAt"`
	Read       bool   `json:"read"`
}

func (s *Store) activityNotices(ctx context.Context, openid string, raw json.RawMessage) (any, error) {
	var d struct {
		Offset int `json:"offset"`
		Limit  int `json:"limit"`
	}
	if len(raw) > 0 {
		if err := decode(raw, &d); err != nil {
			return nil, err
		}
	}
	if d.Limit == 0 {
		d.Limit = 20
	}
	if d.Offset < 0 || d.Offset > 1000000 || d.Limit < 1 || d.Limit > 50 {
		return nil, errors.New("通知分页参数无效")
	}
	var unread int
	if err := s.db.QueryRowContext(ctx, "SELECT COUNT(*) FROM activity_notices WHERE openid=? AND is_read=0", openid).Scan(&unread); err != nil {
		return nil, err
	}
	rows, err := s.db.QueryContext(ctx, "SELECT id,activity_id,title,start_date,end_date,created_at,is_read,kind FROM activity_notices WHERE openid=? ORDER BY created_at DESC,id DESC LIMIT ? OFFSET ?", openid, d.Limit+1, d.Offset)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := []ActivityNotice{}
	for rows.Next() {
		var n ActivityNotice
		var created time.Time
		if err := rows.Scan(&n.ID, &n.ActivityID, &n.Title, &n.StartDate, &n.EndDate, &created, &n.Read, &n.Kind); err != nil {
			return nil, err
		}
		n.CreatedAt = created.UTC().Format(time.RFC3339)
		items = append(items, n)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	more := len(items) > d.Limit
	if more {
		items = items[:d.Limit]
	}
	return M{"items": items, "unread": unread, "hasMore": more}, nil
}

func (s *Store) readActivityNotice(ctx context.Context, openid string, raw json.RawMessage) (any, error) {
	var d struct {
		ID string `json:"id"`
	}
	if err := decode(raw, &d); err != nil {
		return nil, err
	}
	if d.ID == "" || len(d.ID) > 80 {
		return nil, errors.New("通知标识无效")
	}
	_, err := s.db.ExecContext(ctx, "UPDATE activity_notices SET is_read=1 WHERE id=? AND openid=?", d.ID, openid)
	return M{"ok": true}, err
}
