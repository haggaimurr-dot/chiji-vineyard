package main

import (
	"context"
	"encoding/json"
	"errors"
	"sort"
	"time"
)

// 公海池：长期未跟进且有负责人的线索自动流入（计算视图，不改状态）；阈值可配。
type publicPoolConfig struct {
	InactiveDays int `json:"inactiveDays"`
}

func defaultPublicPoolConfig() publicPoolConfig { return publicPoolConfig{InactiveDays: 7} }

func (s *Server) readPublicPoolConfig(ctx context.Context) publicPoolConfig {
	cfg := defaultPublicPoolConfig()
	var raw []byte
	if err := s.store.db.QueryRowContext(ctx, "SELECT payload FROM configs WHERE config_key='leads.publicPool'").Scan(&raw); err != nil {
		return cfg
	}
	var c publicPoolConfig
	if err := json.Unmarshal(raw, &c); err != nil {
		return cfg
	}
	if c.InactiveDays >= 1 && c.InactiveDays <= 90 {
		cfg.InactiveDays = c.InactiveDays
	}
	return cfg
}

func (s *Server) savePublicPoolConfig(ctx context.Context, actor Claims, raw json.RawMessage) (any, error) {
	var c publicPoolConfig
	if err := decode(raw, &c); err != nil {
		return nil, err
	}
	if c.InactiveDays < 1 || c.InactiveDays > 90 {
		return nil, errors.New("未跟进天数须在 1–90 天之间")
	}
	if _, err := s.store.db.ExecContext(ctx, "INSERT INTO configs(config_key,payload) VALUES('leads.publicPool',?) ON DUPLICATE KEY UPDATE payload=VALUES(payload)", jsonValue(c)); err != nil {
		return nil, err
	}
	if _, err := s.store.db.ExecContext(ctx, "INSERT INTO audit_logs(admin_id,username,action,target_id,detail) VALUES(?,?,?,?,?)", actor.Subject, actor.Username, "LEAD_PUBLIC_POOL_CONFIG", "", jsonValue(c)); err != nil {
		return nil, err
	}
	return c, nil
}

func leadLastActivity(l Lead) time.Time {
	last, _ := time.Parse(time.RFC3339Nano, l.CreatedAt)
	for _, f := range l.FollowUps {
		if t, err := time.Parse(time.RFC3339Nano, f.CreatedAt); err == nil && t.After(last) {
			last = t
		}
	}
	return last
}

type publicPoolItem struct {
	Lead       Lead `json:"lead"`
	DaysIdle   int  `json:"daysIdle"`
	LastActive string `json:"lastActive"`
}

func (s *Server) leadPublicPool(ctx context.Context) (any, error) {
	cfg := s.readPublicPoolConfig(ctx)
	cutoff := time.Now().AddDate(0, 0, -cfg.InactiveDays)
	rows, err := s.store.db.QueryContext(ctx, "SELECT payload FROM partner_leads WHERE status IN ('NEW','CONTACTED','QUALIFIED')")
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := []publicPoolItem{}
	for rows.Next() {
		var b []byte
		var l Lead
		if err = rows.Scan(&b); err != nil {
			return nil, err
		}
		if err = json.Unmarshal(b, &l); err != nil {
			return nil, err
		}
		if l.OwnerID == "" {
			continue
		}
		last := leadLastActivity(l)
		if last.After(cutoff) {
			continue
		}
		items = append(items, publicPoolItem{Lead: l, DaysIdle: int(time.Since(last).Hours() / 24), LastActive: last.Format(time.RFC3339Nano)})
	}
	if err = rows.Err(); err != nil {
		return nil, err
	}
	sort.Slice(items, func(i, j int) bool { return items[i].DaysIdle > items[j].DaysIdle })
	return M{"config": cfg, "items": items}, nil
}

// 同手机号重复线索提示。
func (s *Server) leadDuplicates(ctx context.Context, raw json.RawMessage) (any, error) {
	id := mustID(raw, "id")
	var hash string
	if err := s.store.db.QueryRowContext(ctx, "SELECT phone_hash FROM partner_leads WHERE id=?", id).Scan(&hash); err != nil {
		return nil, errors.New("线索不存在")
	}
	rows, err := s.store.db.QueryContext(ctx, "SELECT id,payload FROM partner_leads WHERE phone_hash=? AND id<>? ORDER BY created_at DESC LIMIT 20", hash, id)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := []M{}
	for rows.Next() {
		var lid string
		var b []byte
		if err = rows.Scan(&lid, &b); err != nil {
			return nil, err
		}
		var l Lead
		if err = json.Unmarshal(b, &l); err != nil {
			return nil, err
		}
		items = append(items, M{"id": l.ID, "organization": l.Organization, "contact": l.Contact, "status": l.Status, "ownerName": l.OwnerName, "createdAt": l.CreatedAt, "followUpCount": len(l.FollowUps)})
	}
	return M{"items": items}, rows.Err()
}

// 合并重复线索：来源线索的跟进记录并入目标，来源标记为已关闭（可追溯）。
func (s *Server) mergeLeads(ctx context.Context, actor Claims, raw json.RawMessage) (any, error) {
	var d struct {
		TargetID  string   `json:"targetId"`
		SourceIDs []string `json:"sourceIds"`
	}
	if err := decode(raw, &d); err != nil {
		return nil, err
	}
	if d.TargetID == "" || len(d.SourceIDs) == 0 || len(d.SourceIDs) > 20 {
		return nil, errors.New("请选择目标线索与 1–20 条来源线索")
	}
	for _, sid := range d.SourceIDs {
		if sid == d.TargetID {
			return nil, errors.New("来源线索不能包含目标线索")
		}
	}
	tx, err := s.store.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	load := func(id string) (*Lead, error) {
		var b []byte
		if err := tx.QueryRowContext(ctx, "SELECT payload FROM partner_leads WHERE id=? FOR UPDATE", id).Scan(&b); err != nil {
			return nil, errors.New("线索不存在：" + id)
		}
		var l Lead
		if err := json.Unmarshal(b, &l); err != nil {
			return nil, err
		}
		return &l, nil
	}
	target, err := load(d.TargetID)
	if err != nil {
		return nil, err
	}
	now := time.Now()
	merged := 0
	for _, sid := range d.SourceIDs {
		src, err := load(sid)
		if err != nil {
			return nil, err
		}
		if src.Status == "WON" {
			return nil, errors.New("已成交线索不能被合并：" + sid)
		}
		seen := map[string]bool{}
		for _, f := range target.FollowUps {
			seen[f.ID] = true
		}
		for _, f := range src.FollowUps {
			if !seen[f.ID] {
				target.FollowUps = append(target.FollowUps, f)
				seen[f.ID] = true
			}
		}
		sort.Slice(target.FollowUps, func(i, j int) bool { return target.FollowUps[i].CreatedAt < target.FollowUps[j].CreatedAt })
		target.FollowUps = append(target.FollowUps, LeadFollowUp{ID: randomID("LF"), Note: "合并线索 " + src.ID + "（" + src.Organization + "）的跟进记录", Status: target.Status, PreviousStatus: target.Status, CreatedAt: now.Format(time.RFC3339Nano), Username: actor.Username})
		src.Status = "LOST"
		src.OwnerID, src.OwnerName = "", ""
		src.FollowUpNote = "已合并至线索 " + target.ID
		src.Version++
		if _, err = tx.ExecContext(ctx, "UPDATE partner_leads SET status=?,payload=? WHERE id=?", src.Status, jsonValue(src), src.ID); err != nil {
			return nil, err
		}
		hist := LeadHistory{ID: randomID("LH"), LeadID: src.ID, ActorID: actor.Subject, ActorName: actor.Username, CreatedAt: now.Format(time.RFC3339Nano), Note: "合并至线索 " + target.ID, Status: src.Status, Changes: []string{"mergedInto:" + target.ID}}
		if _, err = tx.ExecContext(ctx, "INSERT INTO partner_lead_history(id,lead_id,created_at,payload) VALUES(?,?,?,?)", hist.ID, hist.LeadID, now, jsonValue(hist)); err != nil {
			return nil, err
		}
		merged++
	}
	target.Version++
	if _, err = tx.ExecContext(ctx, "UPDATE partner_leads SET payload=? WHERE id=?", jsonValue(target), target.ID); err != nil {
		return nil, err
	}
	if _, err = tx.ExecContext(ctx, "INSERT INTO audit_logs(admin_id,username,action,target_id,detail) VALUES(?,?,?,?,?)", actor.Subject, actor.Username, "LEAD_MERGE", target.ID, jsonValue(M{"sources": d.SourceIDs, "merged": merged})); err != nil {
		return nil, err
	}
	if err = tx.Commit(); err != nil {
		return nil, err
	}
	return M{"targetId": target.ID, "merged": merged, "followUpCount": len(target.FollowUps)}, nil
}
