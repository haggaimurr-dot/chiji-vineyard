package main

import (
	"context"
	"encoding/json"
	"errors"
	"time"
)

// 配置变更审批：普通管理员提交变更单（PENDING），SUPER_ADMIN 审批后生效并记版本。
type configChangeOrder struct {
	ID        string `json:"id"`
	ConfigKey string `json:"configKey"`
	Payload   any    `json:"payload"`
	Status    string `json:"status"`
	CreatedBy string `json:"createdBy"`
	CreatedAt string `json:"createdAt"`
	DecidedBy string `json:"decidedBy,omitempty"`
	DecidedAt string `json:"decidedAt,omitempty"`
	Note      string `json:"note,omitempty"`
}

func (s *Server) submitConfigChange(ctx context.Context, actor Claims, raw json.RawMessage) (any, error) {
	var d struct {
		Key     string          `json:"key"`
		Payload json.RawMessage `json:"payload"`
	}
	if err := decode(raw, &d); err != nil {
		return nil, err
	}
	if !validConfigKey(d.Key) || len(d.Payload) == 0 {
		return nil, errors.New("配置类型或内容无效")
	}
	tx, err := s.store.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	value, err := validateConfigPayload(ctx, tx, d.Key, d.Payload)
	if err != nil {
		return nil, err
	}
	id := randomID("CCO")
	now := time.Now().UTC().Format(time.RFC3339Nano)
	if _, err = tx.ExecContext(ctx, "INSERT INTO config_change_orders(id,config_key,payload,status,created_by,created_at) VALUES(?,?,?,?,?,?)",
		id, d.Key, jsonValue(value), "PENDING", actor.Username, now); err != nil {
		return nil, err
	}
	if _, err = tx.ExecContext(ctx, "INSERT INTO audit_logs(admin_id,username,action,target_id,detail) VALUES(?,?,?,?,?)",
		actor.Subject, actor.Username, "CONFIG_CHANGE_SUBMIT", id, jsonValue(M{"key": d.Key})); err != nil {
		return nil, err
	}
	if err = tx.Commit(); err != nil {
		return nil, err
	}
	return configChangeOrder{ID: id, ConfigKey: d.Key, Payload: value, Status: "PENDING", CreatedBy: actor.Username, CreatedAt: now}, nil
}

func (s *Server) configChangeOrders(ctx context.Context, raw json.RawMessage) (any, error) {
	var d struct {
		Status string `json:"status"`
	}
	_ = decode(raw, &d)
	where := ""
	if d.Status == "PENDING" || d.Status == "APPROVED" || d.Status == "REJECTED" {
		where = " WHERE status='" + d.Status + "'"
	}
	rows, err := s.store.db.QueryContext(ctx, "SELECT id,config_key,payload,status,created_by,created_at,decided_by,decided_at,note FROM config_change_orders"+where+" ORDER BY created_at DESC LIMIT 200")
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := []configChangeOrder{}
	for rows.Next() {
		var o configChangeOrder
		var payload []byte
		if err = rows.Scan(&o.ID, &o.ConfigKey, &payload, &o.Status, &o.CreatedBy, &o.CreatedAt, &o.DecidedBy, &o.DecidedAt, &o.Note); err != nil {
			return nil, err
		}
		_ = json.Unmarshal(payload, &o.Payload)
		items = append(items, o)
	}
	return M{"items": items}, rows.Err()
}

func (s *Server) decideConfigChange(ctx context.Context, actor Claims, raw json.RawMessage) (any, error) {
	var d struct {
		ID      string `json:"id"`
		Approve bool   `json:"approve"`
		Note    string `json:"note"`
	}
	if err := decode(raw, &d); err != nil {
		return nil, err
	}
	if d.ID == "" {
		return nil, errors.New("变更单无效")
	}
	var key string
	var payload []byte
	var status string
	if err := s.store.db.QueryRowContext(ctx, "SELECT config_key,payload,status FROM config_change_orders WHERE id=?", d.ID).Scan(&key, &payload, &status); err != nil {
		return nil, errors.New("变更单不存在")
	}
	if status != "PENDING" {
		return nil, errors.New("变更单已处理")
	}
	now := time.Now().UTC().Format(time.RFC3339Nano)
	if d.Approve {
		result, err := s.saveVersionedConfig(ctx, actor, key, payload)
		if err != nil {
			return nil, err
		}
		if _, err = s.store.db.ExecContext(ctx, "UPDATE config_change_orders SET status='APPROVED',decided_by=?,decided_at=?,note=? WHERE id=? AND status='PENDING'",
			actor.Username, now, clean(d.Note, 500), d.ID); err != nil {
			return nil, err
		}
		return M{"id": d.ID, "status": "APPROVED", "version": result.Version}, nil
	}
	if _, err := s.store.db.ExecContext(ctx, "UPDATE config_change_orders SET status='REJECTED',decided_by=?,decided_at=?,note=? WHERE id=? AND status='PENDING'",
		actor.Username, now, clean(d.Note, 500), d.ID); err != nil {
		return nil, err
	}
	return M{"id": d.ID, "status": "REJECTED"}, nil
}
