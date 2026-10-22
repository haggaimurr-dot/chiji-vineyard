package main

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"
	_ "time/tzdata"
)

var managedConfigKeys = []string{"shipping", "coupons", "home", "privacy"}

func validConfigKey(key string) bool {
	for _, allowed := range managedConfigKeys {
		if key == allowed {
			return true
		}
	}
	return false
}

type ConfigVersion struct {
	ID            string `json:"id"`
	Key           string `json:"key"`
	Version       int    `json:"version"`
	Action        string `json:"action"`
	SourceVersion *int   `json:"sourceVersion"`
	Username      string `json:"username"`
	CreatedAt     string `json:"createdAt"`
	Payload       any    `json:"payload"`
}

type ConfigVersionResult struct {
	Key     string `json:"key"`
	Version int    `json:"version"`
	Payload any    `json:"payload"`
}

func defaultConfigValue(key string) any {
	switch key {
	case "shipping":
		return defaultRule()
	case "coupons":
		return CouponConfig{Items: []Coupon{}}
	case "home":
		return defaultHome()
	case "privacy":
		var p PrivacyPolicy
		_ = json.Unmarshal(privacyDefault, &p)
		return p
	}
	return nil
}

func baselineConfigPayload(key string, raw []byte) (string, error) {
	// Home and privacy are loaded over defaults by the public API. Snapshot the
	// resulting effective value when an older row contains only some fields.
	switch key {
	case "home":
		h := defaultHome()
		if err := json.Unmarshal(raw, &h); err != nil {
			return "", err
		}
		return jsonValue(h), nil
	case "privacy":
		p := defaultConfigValue(key).(PrivacyPolicy)
		if err := json.Unmarshal(raw, &p); err != nil {
			return "", err
		}
		return jsonValue(p), nil
	}
	return string(raw), nil
}

// Every writer locks the same config row, including its first creation. The
// current version is a locking read so it sees commits after waiting for a lock.
func lockConfig(ctx context.Context, tx *sql.Tx, key string) ([]byte, int, error) {
	if !validConfigKey(key) {
		return nil, 0, errors.New("配置类型无效")
	}
	// The no-op duplicate update takes an exclusive lock immediately; INSERT
	// IGNORE can acquire shared duplicate locks that deadlock during an upgrade.
	if _, err := tx.ExecContext(ctx, "INSERT INTO configs(config_key,payload) VALUES(?,?) ON DUPLICATE KEY UPDATE config_key=VALUES(config_key)", key, jsonValue(defaultConfigValue(key))); err != nil {
		return nil, 0, err
	}
	var old []byte
	if err := tx.QueryRowContext(ctx, "SELECT payload FROM configs WHERE config_key=? FOR UPDATE", key).Scan(&old); err != nil {
		return nil, 0, err
	}
	var version int
	err := tx.QueryRowContext(ctx, "SELECT version FROM config_versions WHERE config_key=? ORDER BY version DESC LIMIT 1 FOR UPDATE", key).Scan(&version)
	if errors.Is(err, sql.ErrNoRows) {
		err = nil
	}
	return old, version, err
}

func insertConfigVersion(ctx context.Context, tx *sql.Tx, actor Claims, key string, version int, action string, source *int, payload string) error {
	_, err := tx.ExecContext(ctx, "INSERT INTO config_versions(id,config_key,version,action,source_version,admin_id,username,payload) VALUES(?,?,?,?,?,?,?,?)", randomID("CFG"), key, version, action, source, actor.Subject, actor.Username, payload)
	return err
}

func writeConfigVersion(ctx context.Context, tx *sql.Tx, actor Claims, key string, version int, action string, source *int, value any, reason string) error {
	if _, err := tx.ExecContext(ctx, "UPDATE configs SET payload=? WHERE config_key=?", jsonValue(value), key); err != nil {
		return err
	}
	if err := insertConfigVersion(ctx, tx, actor, key, version, action, source, jsonValue(value)); err != nil {
		return err
	}
	detail := M{"key": key, "version": version, "sourceVersion": source}
	if reason != "" {
		detail["reason"] = reason
	}
	_, err := tx.ExecContext(ctx, "INSERT INTO audit_logs(admin_id,username,action,target_id,detail) VALUES(?,?,?,?,?)", actor.Subject, actor.Username, "CONFIG_"+action, key, jsonValue(detail))
	return err
}

func (s *Server) saveVersionedConfig(ctx context.Context, actor Claims, key string, raw json.RawMessage) (ConfigVersionResult, error) {
	var result ConfigVersionResult
	if !validConfigKey(key) {
		return result, errors.New("配置类型无效")
	}
	tx, err := s.store.db.BeginTx(ctx, nil)
	if err != nil {
		return result, err
	}
	defer tx.Rollback()
	old, current, err := lockConfig(ctx, tx, key)
	if err != nil {
		return result, err
	}
	value, err := validateConfigPayload(ctx, tx, key, raw)
	if err != nil {
		return result, err
	}
	if current == 0 {
		// Keep the effective previous value before the first tracked change.
		current = 1
		baseline, e := baselineConfigPayload(key, old)
		if e != nil {
			return result, e
		}
		if err = insertConfigVersion(ctx, tx, actor, key, current, "BASELINE", nil, baseline); err != nil {
			return result, err
		}
	}
	result = ConfigVersionResult{Key: key, Version: current + 1, Payload: value}
	if err = writeConfigVersion(ctx, tx, actor, key, result.Version, "SAVE", nil, value, ""); err != nil {
		return ConfigVersionResult{}, err
	}
	if err = reconcileHomeActivities(ctx, tx, key, old, value); err != nil {
		return ConfigVersionResult{}, err
	}
	return result, tx.Commit()
}

func (s *Server) rollbackConfig(ctx context.Context, actor Claims, raw json.RawMessage) (any, error) {
	var d struct {
		Key             string `json:"key"`
		VersionID       string `json:"versionId"`
		ExpectedVersion *int   `json:"expectedVersion"`
		Reason          string `json:"reason"`
	}
	if err := decode(raw, &d); err != nil {
		return nil, err
	}
	d.Reason = strings.TrimSpace(d.Reason)
	if !validConfigKey(d.Key) || strings.TrimSpace(d.VersionID) == "" || len(d.VersionID) > 80 || d.ExpectedVersion == nil || *d.ExpectedVersion < 0 {
		return nil, errors.New("配置类型、目标版本或当前版本无效")
	}
	if d.Reason == "" || len([]rune(d.Reason)) > 500 {
		return nil, errors.New("请填写 1–500 字的回滚原因")
	}
	tx, err := s.store.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	old, current, err := lockConfig(ctx, tx, d.Key)
	if err != nil {
		return nil, err
	}
	if current != *d.ExpectedVersion {
		return nil, errors.New("配置已变化，请刷新版本列表后重试")
	}
	var payload []byte
	var source int
	err = tx.QueryRowContext(ctx, "SELECT payload,version FROM config_versions WHERE id=? AND config_key=?", d.VersionID, d.Key).Scan(&payload, &source)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, errors.New("目标版本不存在或不属于此配置")
	}
	if err != nil {
		return nil, err
	}
	if source == current {
		return nil, errors.New("所选版本已是当前版本，无需回滚")
	}
	value, err := validateConfigPayload(ctx, tx, d.Key, payload)
	if err != nil {
		return nil, fmt.Errorf("历史配置无法回滚：%w", err)
	}
	if source < 1 || source > current {
		return nil, errors.New("历史版本数据无效")
	}
	result := ConfigVersionResult{Key: d.Key, Version: current + 1, Payload: value}
	if err = writeConfigVersion(ctx, tx, actor, d.Key, result.Version, "ROLLBACK", &source, value, d.Reason); err != nil {
		return nil, err
	}
	if err = reconcileHomeActivities(ctx, tx, d.Key, old, value); err != nil {
		return nil, err
	}
	return result, tx.Commit()
}

// Saved values and historical values share validation and published-reference
// checks. Decode into the allowed schema so unknown input fields cannot persist.
func validateConfigPayload(ctx context.Context, tx *sql.Tx, key string, raw json.RawMessage) (any, error) {
	var object map[string]json.RawMessage
	if err := json.Unmarshal(raw, &object); err != nil || object == nil {
		return nil, errors.New("配置内容必须为有效对象")
	}
	switch key {
	case "shipping":
		var rule ShippingRule
		if err := json.Unmarshal(raw, &rule); err != nil {
			return nil, err
		}
		if len(object["freight"]) == 0 || len(object["freeShippingThreshold"]) == 0 || string(object["freight"]) == "null" || string(object["freeShippingThreshold"]) == "null" || rule.Freight < 0 || rule.FreeShippingThreshold < 0 || rule.Freight > 10000000 || rule.FreeShippingThreshold > 10000000 || len(rule.BlockedProvinces) > 50 {
			return nil, errors.New("运费配置无效")
		}
		seen := map[string]bool{}
		for _, province := range rule.BlockedProvinces {
			if !homeText(province, 30) || seen[province] {
				return nil, errors.New("不配送省份不能为空或重复")
			}
			seen[province] = true
		}
		if rule.BlockedProvinces == nil {
			rule.BlockedProvinces = []string{}
		}
		if err := validateShippingDetails(rule); err != nil {
			return nil, err
		}
		return rule, nil
	case "coupons":
		if len(object["items"]) == 0 || string(object["items"]) == "null" {
			return nil, errors.New("优惠券配置无效")
		}
		var c CouponConfig
		if err := json.Unmarshal(raw, &c); err != nil {
			return nil, err
		}
		if err := validateCoupons(&c); err != nil {
			return nil, err
		}
		return c, nil
	case "privacy":
		var p PrivacyPolicy
		if err := json.Unmarshal(raw, &p); err != nil {
			return nil, err
		}
		return p, validatePrivacy(p)
	case "home":
		var h HomeConfig
		if err := json.Unmarshal(raw, &h); err != nil {
			return nil, err
		}
		if err := validateHome(h); err != nil {
			return nil, err
		}
		if err := validateHomeReferences(ctx, tx, h); err != nil {
			return nil, err
		}
		return h, nil
	}
	return nil, errors.New("配置类型无效")
}

func validateHomeReferences(ctx context.Context, tx *sql.Tx, h HomeConfig) error {
	products, articles := map[string]bool{}, map[string]bool{}
	for _, id := range h.ProductIDs {
		products[id] = true
	}
	for _, id := range h.ArticleIDs {
		articles[id] = true
	}
	for _, b := range h.Banners {
		if b.Target == "product" {
			products[b.TargetID] = true
		} else if b.Target == "article" {
			articles[b.TargetID] = true
		}
	}
	for _, a := range h.Activities {
		if a.ProductID != "" {
			products[a.ProductID] = true
		}
	}
	for _, a := range h.LaunchCalendar {
		if a.ProductID != "" {
			products[a.ProductID] = true
		}
	}
	for i, ids := range []map[string]bool{products, articles} {
		ordered := make([]string, 0, len(ids))
		for id := range ids {
			ordered = append(ordered, id)
		}
		sort.Strings(ordered)
		for _, id := range ordered {
			query := "SELECT p.id FROM products p JOIN skus k ON k.product_id=p.id WHERE p.id=? AND p.status='PUBLISHED' AND k.active=1 LIMIT 1 FOR UPDATE"
			message := "首页关联商品不存在、未上架或无可用规格"
			if i == 1 {
				query = "SELECT id FROM articles WHERE id=? AND status='PUBLISHED' FOR UPDATE"
				message = "首页关联文章不存在或未发布"
			}
			var found string
			if err := tx.QueryRowContext(ctx, query, id).Scan(&found); err != nil {
				if errors.Is(err, sql.ErrNoRows) {
					return errors.New(message)
				}
				return err
			}
		}
	}
	return nil
}

func historyPagination(limit, offset int) (int, int) {
	if limit <= 0 || limit > 100 {
		limit = 20
	}
	if offset < 0 {
		offset = 0
	}
	return limit, offset
}

func (s *Store) configVersions(ctx context.Context, raw json.RawMessage) (any, error) {
	var d struct {
		Key    string `json:"key"`
		Limit  int    `json:"limit"`
		Offset int    `json:"offset"`
	}
	if err := decode(raw, &d); err != nil {
		return nil, err
	}
	if d.Key != "" && !validConfigKey(d.Key) {
		return nil, errors.New("配置类型无效")
	}
	d.Limit, d.Offset = historyPagination(d.Limit, d.Offset)
	tx, err := s.db.BeginTx(ctx, &sql.TxOptions{ReadOnly: true, Isolation: sql.LevelRepeatableRead})
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	where := "config_key IN ('shipping','coupons','home','privacy')"
	args := []any{}
	if d.Key != "" {
		where = "config_key=?"
		args = append(args, d.Key)
	}
	var total int
	if err = tx.QueryRowContext(ctx, "SELECT COUNT(*) FROM config_versions WHERE "+where, args...).Scan(&total); err != nil {
		return nil, err
	}
	listArgs := append(append([]any{}, args...), d.Limit, d.Offset)
	rows, err := tx.QueryContext(ctx, "SELECT id,config_key,version,action,source_version,username,created_at,payload FROM config_versions WHERE "+where+" ORDER BY created_at DESC,version DESC,id DESC LIMIT ? OFFSET ?", listArgs...)
	if err != nil {
		return nil, err
	}
	items := []ConfigVersion{}
	for rows.Next() {
		var v ConfigVersion
		var created time.Time
		var source sql.NullInt64
		var payload []byte
		if err = rows.Scan(&v.ID, &v.Key, &v.Version, &v.Action, &source, &v.Username, &created, &payload); err != nil {
			rows.Close()
			return nil, err
		}
		if source.Valid {
			n := int(source.Int64)
			v.SourceVersion = &n
		}
		if err = json.Unmarshal(payload, &v.Payload); err != nil {
			rows.Close()
			return nil, errors.New("历史配置数据无效")
		}
		v.CreatedAt = created.UTC().Format(time.RFC3339Nano)
		items = append(items, v)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return nil, err
	}
	current := map[string]int{}
	for _, key := range managedConfigKeys {
		current[key] = 0
	}
	rows, err = tx.QueryContext(ctx, "SELECT config_key,MAX(version) FROM config_versions WHERE config_key IN ('shipping','coupons','home','privacy') GROUP BY config_key")
	if err != nil {
		return nil, err
	}
	for rows.Next() {
		var key string
		var version int
		if err = rows.Scan(&key, &version); err != nil {
			rows.Close()
			return nil, err
		}
		current[key] = version
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return nil, err
	}
	if err = tx.Commit(); err != nil {
		return nil, err
	}
	return M{"items": items, "total": total, "limit": d.Limit, "offset": d.Offset, "currentVersions": current}, nil
}
