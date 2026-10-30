package main

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"hash/fnv"
	"time"
)

// 配置灰度发布（新增）：高危配置（当前仅首页 home）可按用户比例灰度验证。
// 命中规则：fnv(openid) % 100 < percent。同一用户在一次灰度中结果稳定。
// 灰度中的配置：命中用户看到灰度版，未命中看到正式版；全量后灰度版写入正式配置。

var grayEligibleConfigs = map[string]bool{"home": true}

func grayBucket(openid string) int {
	h := fnv.New32a()
	h.Write([]byte("config-gray:v1\x00" + openid))
	return int(h.Sum32() % 100)
}

type configRollout struct {
	ID        string `json:"id"`
	ConfigKey string `json:"configKey"`
	Percent   int    `json:"percent"`
	Status    string `json:"status"`
	CreatedBy string `json:"createdBy"`
	CreatedAt string `json:"createdAt"`
}

// grayPayload 返回 openid 命中的灰度 payload（nil 表示未命中/无灰度）。
func (s *Store) grayPayload(ctx context.Context, configKey, openid string) []byte {
	if !grayEligibleConfigs[configKey] || openid == "" {
		return nil
	}
	var payload []byte
	var percent int
	err := s.db.QueryRowContext(ctx, "SELECT payload,percent FROM config_rollouts WHERE config_key=? AND status='GRAY' ORDER BY created_at DESC LIMIT 1", configKey).Scan(&payload, &percent)
	if err != nil {
		return nil
	}
	if grayBucket(openid) < percent {
		return payload
	}
	return nil
}

func (s *Store) homeFor(ctx context.Context, openid string) (HomeConfig, error) {
	if err := s.publishDueArticles(ctx, time.Now().UTC()); err != nil {
		return HomeConfig{}, err
	}
	h := defaultHome()
	if payload := s.grayPayload(ctx, "home", openid); payload != nil {
		if err := json.Unmarshal(payload, &h); err != nil {
			return HomeConfig{}, err
		}
		return h, nil
	}
	err := s.loadConfig(ctx, "home", &h)
	return h, err
}

func (s *Server) rolloutList(ctx context.Context) (any, error) {
	rows, err := s.store.db.QueryContext(ctx, "SELECT id,config_key,percent,status,created_by,created_at FROM config_rollouts ORDER BY created_at DESC LIMIT 50")
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := []configRollout{}
	for rows.Next() {
		var r configRollout
		var created time.Time
		if err = rows.Scan(&r.ID, &r.ConfigKey, &r.Percent, &r.Status, &r.CreatedBy, &created); err != nil {
			return nil, err
		}
		r.CreatedAt = created.Format("2006-01-02 15:04:05")
		items = append(items, r)
	}
	return M{"items": items}, rows.Err()
}

func (s *Server) rolloutSave(ctx context.Context, actor Claims, raw json.RawMessage) (any, error) {
	var d struct {
		ConfigKey string          `json:"configKey"`
		Payload   json.RawMessage `json:"payload"`
		Percent   int             `json:"percent"`
	}
	if err := decode(raw, &d); err != nil {
		return nil, err
	}
	if !grayEligibleConfigs[d.ConfigKey] {
		return nil, errors.New("该配置暂不支持灰度（当前仅支持首页 home）")
	}
	if d.Percent < 1 || d.Percent > 100 {
		return nil, errors.New("灰度比例须为 1–100")
	}
	if len(d.Payload) == 0 {
		return nil, errors.New("灰度配置内容不能为空")
	}
	// 灰度内容须通过正式配置的校验器。
	if d.ConfigKey == "home" {
		var h HomeConfig
		if err := json.Unmarshal(d.Payload, &h); err != nil {
			return nil, errors.New("灰度配置不是有效的首页配置")
		}
		if err := validateHome(h); err != nil {
			return nil, err
		}
	}
	var active int
	if err := s.store.db.QueryRowContext(ctx, "SELECT COUNT(*) FROM config_rollouts WHERE config_key=? AND status='GRAY'", d.ConfigKey).Scan(&active); err != nil {
		return nil, err
	}
	if active > 0 {
		return nil, errors.New("该配置已有进行中的灰度，请先全量或回滚")
	}
	id := randomID("ROL")
	if _, err := s.store.db.ExecContext(ctx, "INSERT INTO config_rollouts(id,config_key,payload,percent,created_by) VALUES(?,?,?,?,?)", id, d.ConfigKey, d.Payload, d.Percent, actor.Username); err != nil {
		return nil, err
	}
	s.store.audit(ctx, actor, "CONFIG_ROLLOUT_CREATE", id, M{"configKey": d.ConfigKey, "percent": d.Percent})
	return M{"id": id}, nil
}

func (s *Server) rolloutFull(ctx context.Context, actor Claims, raw json.RawMessage) (any, error) {
	id := mustID(raw, "id")
	var key string
	var payload []byte
	err := s.store.db.QueryRowContext(ctx, "SELECT config_key,payload FROM config_rollouts WHERE id=? AND status='GRAY'", id).Scan(&key, &payload)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, errors.New("灰度不存在或已结束")
	}
	if err != nil {
		return nil, err
	}
	if _, err = s.store.db.ExecContext(ctx, "UPDATE config_rollouts SET status='FULL' WHERE id=?", id); err != nil {
		return nil, err
	}
	if _, err = s.saveVersionedConfig(ctx, actor, key, payload); err != nil {
		return nil, err
	}
	s.store.audit(ctx, actor, "CONFIG_ROLLOUT_FULL", id, M{"configKey": key})
	return M{"full": true}, nil
}

func (s *Server) rolloutRollback(ctx context.Context, actor Claims, raw json.RawMessage) (any, error) {
	id := mustID(raw, "id")
	res, err := s.store.db.ExecContext(ctx, "UPDATE config_rollouts SET status='ROLLED_BACK' WHERE id=? AND status='GRAY'", id)
	if err != nil {
		return nil, err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return nil, errors.New("灰度不存在或已结束")
	}
	s.store.audit(ctx, actor, "CONFIG_ROLLOUT_ROLLBACK", id, M{})
	return M{"rolledBack": true}, nil
}
