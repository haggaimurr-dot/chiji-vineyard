package main

import (
	"context"
	"database/sql"
	"time"
)

// 健康检查聚合页（新增）：各后台任务写心跳，管理端一屏总览服务状态。

// heartbeat 记录组件心跳。
func (s *Store) heartbeat(ctx context.Context, component, detail string) {
	if len(detail) > 200 {
		detail = detail[:200]
	}
	_, _ = s.db.ExecContext(ctx, "INSERT INTO runtime_heartbeats(component,last_run,detail) VALUES(?,NOW(3),?) ON DUPLICATE KEY UPDATE last_run=NOW(3),detail=VALUES(detail)", component, detail)
}

// runtimeStatus 聚合各组件健康状态。
func (s *Server) runtimeStatus(ctx context.Context) (any, error) {
	type component struct {
		name      string
		label     string
		maxStale  time.Duration
	}
	components := []component{
		{"article_publisher", "定时发布", 10 * time.Minute},
		{"review_alert_scanner", "差评提醒扫描", 10 * time.Minute},
		{"order_alert_scanner", "异常订单扫描", 15 * time.Minute},
		{"notify_worker", "通知分发", 5 * time.Minute},
		{"lead_external_reminders", "线索外部提醒", 30 * time.Minute},
	}
	rows, err := s.store.db.QueryContext(ctx, "SELECT component,TIMESTAMPDIFF(SECOND,last_run,NOW()),detail FROM runtime_heartbeats")
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	beats := map[string]int64{}
	details := map[string]string{}
	for rows.Next() {
		var c string
		var agoSec sql.NullInt64
		var d string
		if err = rows.Scan(&c, &agoSec, &d); err != nil {
			return nil, err
		}
		if agoSec.Valid {
			beats[c] = agoSec.Int64
		}
		details[c] = d
	}
	if err = rows.Err(); err != nil {
		return nil, err
	}
	now := time.Now()
	items := []M{}
	allOK := true
	for _, c := range components {
		agoSec, ok := beats[c.name]
		status := "UNKNOWN"
		ago := ""
		if ok {
			ago = (time.Duration(agoSec) * time.Second).Round(time.Second).String()
			switch {
			case details[c.name] == "DISABLED":
				status = "DISABLED"
			case time.Duration(agoSec)*time.Second <= c.maxStale:
				status = "OK"
			default:
				status = "STALE"
				allOK = false
			}
		} else {
			allOK = false
		}
		items = append(items, M{"component": c.name, "label": c.label, "status": status, "lastRunAgo": ago, "detail": details[c.name]})
	}
	// 数据库连通性
	dbStatus := "OK"
	if err := s.store.db.PingContext(ctx); err != nil {
		dbStatus = "DOWN"
		allOK = false
	}
	items = append([]M{{"component": "api", "label": "后端 API", "status": "OK", "lastRunAgo": "—", "detail": "本接口可达即正常"}, {"component": "db", "label": "数据库", "status": dbStatus, "lastRunAgo": "—", "detail": ""}}, items...)
	return M{"healthy": allOK, "items": items, "checkedAt": now.Format("2006-01-02 15:04:05")}, nil
}
