package main

import (
	"context"
	"encoding/json"
	"errors"
)

// 常见问题快捷回复：客服维护 FAQ 短语，工单回复时一键插入。
type faq struct {
	ID        string `json:"id"`
	Title     string `json:"title"`
	Content   string `json:"content"`
	Sort      int    `json:"sort"`
	Active    bool   `json:"active"`
	UpdatedAt string `json:"updatedAt"`
}

func (s *Store) faqList(ctx context.Context, includeInactive bool) (any, error) {
	where := " WHERE active=TRUE"
	if includeInactive {
		where = ""
	}
	rows, err := s.db.QueryContext(ctx, "SELECT id,title,content,sort,active,updated_at FROM faqs"+where+" ORDER BY sort,id LIMIT 200")
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := []faq{}
	for rows.Next() {
		var f faq
		var active int
		if err = rows.Scan(&f.ID, &f.Title, &f.Content, &f.Sort, &active, &f.UpdatedAt); err != nil {
			return nil, err
		}
		f.Active = active != 0
		items = append(items, f)
	}
	return M{"items": items}, rows.Err()
}

func (s *Server) saveFaq(ctx context.Context, actor Claims, raw json.RawMessage) (any, error) {
	var d struct {
		ID      string `json:"id"`
		Title   string `json:"title"`
		Content string `json:"content"`
		Sort    int    `json:"sort"`
		Active  *bool  `json:"active"`
	}
	if err := decode(raw, &d); err != nil {
		return nil, err
	}
	title := clean(d.Title, 60)
	content := clean(d.Content, 2000)
	if title == "" || content == "" {
		return nil, errors.New("标题与内容不能为空")
	}
	if d.Sort < 0 || d.Sort > 9999 {
		return nil, errors.New("排序无效")
	}
	active := true
	if d.Active != nil {
		active = *d.Active
	}
	id := d.ID
	if id == "" {
		id = randomID("FAQ")
	}
	if _, err := s.store.db.ExecContext(ctx, "INSERT INTO faqs(id,title,content,sort,active) VALUES(?,?,?,?,?) ON DUPLICATE KEY UPDATE title=VALUES(title),content=VALUES(content),sort=VALUES(sort),active=VALUES(active)",
		id, title, content, d.Sort, boolToInt(active)); err != nil {
		return nil, err
	}
	if _, err := s.store.db.ExecContext(ctx, "INSERT INTO audit_logs(admin_id,username,action,target_id,detail) VALUES(?,?,?,?,?)",
		actor.Subject, actor.Username, "FAQ_SAVE", id, jsonValue(M{"title": title})); err != nil {
		return nil, err
	}
	return M{"id": id}, nil
}

func (s *Server) deleteFaq(ctx context.Context, actor Claims, raw json.RawMessage) (any, error) {
	id := mustID(raw, "id")
	if id == "" {
		return nil, errors.New("快捷回复无效")
	}
	if _, err := s.store.db.ExecContext(ctx, "DELETE FROM faqs WHERE id=?", id); err != nil {
		return nil, err
	}
	if _, err := s.store.db.ExecContext(ctx, "INSERT INTO audit_logs(admin_id,username,action,target_id,detail) VALUES(?,?,?,?,?)",
		actor.Subject, actor.Username, "FAQ_DELETE", id, jsonValue(M{})); err != nil {
		return nil, err
	}
	return M{"id": id}, nil
}

func boolToInt(b bool) int {
	if b {
		return 1
	}
	return 0
}
