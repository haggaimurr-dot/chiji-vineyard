package main

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"strings"
	"time"
)

type AuditSearch struct {
	Action    string `json:"action"`
	Username  string `json:"username"`
	TargetID  string `json:"targetId"`
	StartDate string `json:"startDate"`
	EndDate   string `json:"endDate"`
	Limit     int    `json:"limit"`
	Offset    int    `json:"offset"`
}

type AuditLog struct {
	ID        string `json:"id"`
	AdminID   string `json:"adminId"`
	Username  string `json:"username"`
	Action    string `json:"action"`
	TargetID  string `json:"targetId"`
	Detail    any    `json:"detail"`
	CreatedAt string `json:"createdAt"`
}

func auditSearchWhere(d AuditSearch) (string, []any, error) {
	clauses := []string{"1=1"}
	args := []any{}
	for _, f := range []struct{ column, value string }{{"action", d.Action}, {"username", d.Username}, {"target_id", d.TargetID}} {
		if strings.TrimSpace(f.value) != "" {
			if f.column == "action" {
				clauses = append(clauses, f.column+"=?")
			} else {
				// Literal substring matching also treats %, _ and quotes as text.
				clauses = append(clauses, "LOCATE(?, "+f.column+")>0")
			}
			args = append(args, strings.TrimSpace(f.value))
		}
	}
	zone, err := time.LoadLocation("Asia/Shanghai")
	if err != nil {
		return "", nil, err
	}
	var start, end time.Time
	if d.StartDate != "" {
		start, err = time.ParseInLocation("2006-01-02", d.StartDate, zone)
		if err != nil || start.Format("2006-01-02") != d.StartDate {
			return "", nil, errors.New("开始日期无效")
		}
		clauses = append(clauses, "created_at>=?")
		args = append(args, start.UTC())
	}
	if d.EndDate != "" {
		end, err = time.ParseInLocation("2006-01-02", d.EndDate, zone)
		if err != nil || end.Format("2006-01-02") != d.EndDate {
			return "", nil, errors.New("结束日期无效")
		}
		if !start.IsZero() && end.Before(start) {
			return "", nil, errors.New("结束日期不能早于开始日期")
		}
		clauses = append(clauses, "created_at<?")
		args = append(args, end.AddDate(0, 0, 1).UTC())
	}
	return strings.Join(clauses, " AND "), args, nil
}

// Protect older audit entries too, including arbitrary nested request data.
// Invalid JSON is replaced rather than exposed as an unredacted string.
func safeAuditDetail(raw []byte) any {
	var detail any
	if err := json.Unmarshal(raw, &detail); err != nil {
		return M{"message": "日志详情格式无效"}
	}
	return redactAuditDetail(detail)
}

func redactAuditDetail(value any) any {
	switch v := value.(type) {
	case map[string]any:
		for key, item := range v {
			lower := strings.ToLower(strings.NewReplacer("_", "", "-", "", " ", "").Replace(key))
			sensitive := strings.Contains(lower, "password") || strings.Contains(lower, "token") || strings.Contains(lower, "secret") || strings.Contains(lower, "credential") || lower == "authorization" || lower == "cookie" || lower == "hash" || strings.HasSuffix(lower, "passwordhash")
			// A boolean event flag does not contain a password or credential.
			if lower == "passwordchanged" {
				_, isFlag := item.(bool)
				sensitive = !isFlag
			}
			if sensitive {
				v[key] = "[已隐藏]"
			} else {
				v[key] = redactAuditDetail(item)
			}
		}
	case []any:
		for i, item := range v {
			v[i] = redactAuditDetail(item)
		}
	}
	return value
}

func (s *Store) auditLogs(ctx context.Context, raw json.RawMessage) (any, error) {
	return s.readAuditLogs(ctx, raw, false)
}

func (s *Store) readAuditLogs(ctx context.Context, raw json.RawMessage, export bool) (any, error) {
	var d AuditSearch
	if err := decode(raw, &d); err != nil {
		return nil, err
	}
	d.Limit, d.Offset = historyPagination(d.Limit, d.Offset)
	if export {
		d.Limit, d.Offset = 10000, 0
	}
	where, args, err := auditSearchWhere(d)
	if err != nil {
		return nil, err
	}
	tx, err := s.db.BeginTx(ctx, &sql.TxOptions{ReadOnly: true, Isolation: sql.LevelRepeatableRead})
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	var total int
	if err = tx.QueryRowContext(ctx, "SELECT COUNT(*) FROM audit_logs WHERE "+where, args...).Scan(&total); err != nil {
		return nil, err
	}
	if export && total > 10000 {
		return nil, errors.New("筛选结果超过 10000 条日志，请缩小筛选范围后导出")
	}
	listArgs := append(append([]any{}, args...), d.Limit, d.Offset)
	rows, err := tx.QueryContext(ctx, "SELECT CAST(id AS CHAR),admin_id,username,action,target_id,detail,created_at FROM audit_logs WHERE "+where+" ORDER BY created_at DESC,id DESC LIMIT ? OFFSET ?", listArgs...)
	if err != nil {
		return nil, err
	}
	items := []AuditLog{}
	for rows.Next() {
		var item AuditLog
		var detail []byte
		var created time.Time
		if err = rows.Scan(&item.ID, &item.AdminID, &item.Username, &item.Action, &item.TargetID, &detail, &created); err != nil {
			rows.Close()
			return nil, err
		}
		item.Detail = safeAuditDetail(detail)
		item.CreatedAt = created.UTC().Format(time.RFC3339Nano)
		items = append(items, item)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return nil, err
	}
	if err = tx.Commit(); err != nil {
		return nil, err
	}
	return M{"items": items, "total": total, "limit": d.Limit, "offset": d.Offset}, nil
}
