package main

import (
	"context"
	"database/sql"
	"encoding/csv"
	"encoding/json"
	"errors"
	"strings"
	"time"
)

func (s *Store) compareConfigs(ctx context.Context, raw json.RawMessage) (any, error) {
	var d struct {
		Key         string `json:"key"`
		FromVersion int    `json:"fromVersion"`
		ToVersion   int    `json:"toVersion"`
	}
	if err := decode(raw, &d); err != nil {
		return nil, err
	}
	if !validConfigKey(d.Key) || d.FromVersion < 1 || d.ToVersion < 1 {
		return nil, errors.New("配置类型或对比版本无效")
	}
	result := M{"key": d.Key, "fromVersion": d.FromVersion, "toVersion": d.ToVersion}
	// Version snapshots are immutable; both reads always refer to saved versions.
	for name, version := range map[string]int{"before": d.FromVersion, "after": d.ToVersion} {
		var payload []byte
		err := s.db.QueryRowContext(ctx, "SELECT payload FROM config_versions WHERE config_key=? AND version=?", d.Key, version).Scan(&payload)
		if errors.Is(err, sql.ErrNoRows) {
			return nil, errors.New("对比版本不存在或不属于此配置")
		}
		if err != nil {
			return nil, err
		}
		var value any
		if err = json.Unmarshal(payload, &value); err != nil {
			return nil, errors.New("历史配置数据无效")
		}
		result[name] = value
	}
	return result, nil
}

func auditCSV(items []AuditLog) (string, error) {
	var out strings.Builder
	out.WriteString("\ufeff")
	writer := csv.NewWriter(&out)
	writer.UseCRLF = true
	if err := writer.Write([]string{"日志 ID", "管理员 ID", "操作人", "操作类型", "目标 ID", "时间（北京时间）", "操作详情"}); err != nil {
		return "", err
	}
	for _, item := range items {
		created, err := time.Parse(time.RFC3339Nano, item.CreatedAt)
		if err != nil {
			return "", err
		}
		row := []string{item.ID, item.AdminID, item.Username, item.Action, item.TargetID, created.In(time.FixedZone("CST", 8*3600)).Format("2006-01-02 15:04:05"), jsonValue(safeAuditDetail([]byte(jsonValue(item.Detail))))}
		for i := range row {
			row[i] = safeCSVText(row[i])
		}
		if err := writer.Write(row); err != nil {
			return "", err
		}
	}
	writer.Flush()
	return out.String(), writer.Error()
}

func (s *Server) exportAudit(ctx context.Context, actor Claims, raw json.RawMessage) (any, error) {
	// Use the same consistent count/list snapshot and filters as the log browser.
	result, err := s.store.readAuditLogs(ctx, raw, true)
	if err != nil {
		return nil, err
	}
	items := result.(M)["items"].([]AuditLog)
	content, err := auditCSV(items)
	if err != nil {
		return nil, err
	}
	if _, err = s.store.db.ExecContext(ctx, "INSERT INTO audit_logs(admin_id,username,action,target_id,detail) VALUES(?,?,?,?,?)", actor.Subject, actor.Username, "AUDIT_EXPORT", "", jsonValue(M{"count": len(items)})); err != nil {
		return nil, errors.New("导出日志保存失败，请稍后重试")
	}
	return M{"filename": "操作日志_" + time.Now().In(time.FixedZone("CST", 8*3600)).Format("20060102_150405") + ".csv", "content": content, "count": len(items)}, nil
}
