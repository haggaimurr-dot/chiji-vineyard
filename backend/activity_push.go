package main

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"net/url"
	"os"
	"regexp"
	"strings"
	"time"
)

var activityMessageFieldRE = regexp.MustCompile(`^(thing|date|time)[1-9][0-9]*$`)

func activityPushConfig() (shippingMessageConfig, error) {
	c := shippingMessageConfig{TemplateID: strings.TrimSpace(os.Getenv("WECHAT_ACTIVITY_TEMPLATE_ID")), State: os.Getenv("WECHAT_MINIPROGRAM_STATE")}
	if c.TemplateID == "" {
		return c, errors.New("活动微信提醒尚未配置")
	}
	if json.Unmarshal([]byte(os.Getenv("WECHAT_ACTIVITY_TEMPLATE_FIELDS")), &c.Fields) != nil || len(c.Fields) == 0 {
		return c, errors.New("活动模板字段配置无效")
	}
	for key, source := range c.Fields {
		if !activityMessageFieldRE.MatchString(key) || !((strings.HasPrefix(key, "thing") && (source == "title" || source == "location" || source == "remark")) || ((strings.HasPrefix(key, "date") || strings.HasPrefix(key, "time")) && source == "startDate")) {
			return c, errors.New("活动模板仅支持标题、地点、提示及开始日期")
		}
	}
	if c.State == "" {
		c.State = "formal"
	}
	if c.State != "formal" && c.State != "trial" && c.State != "developer" {
		return c, errors.New("小程序版本配置无效")
	}
	return c, nil
}

func activityPushEligible(a HomeActivity, now time.Time) bool {
	return a.ReservationEnabled && homeDateRange(a.StartDate, a.EndDate) && now.In(time.FixedZone("CST", 8*3600)).Format("2006-01-02") == a.StartDate
}

func activityPushMessage(user string, a HomeActivity, c shippingMessageConfig) M {
	location := a.Location
	if strings.TrimSpace(location) == "" {
		location = "详见活动专题"
	}
	values := map[string]string{"title": stringTruncate(a.Title, 20), "location": stringTruncate(location, 20), "remark": "您预约的活动今日开始，请查看安排", "startDate": a.StartDate}
	data := M{}
	for key, source := range c.Fields {
		data[key] = M{"value": values[source]}
	}
	return M{"touser": user, "template_id": c.TemplateID, "page": "pages/activity/index?id=" + url.QueryEscape(a.ID), "miniprogram_state": c.State, "lang": "zh_CN", "data": data}
}

func (s *Server) activityPushSubscription(ctx context.Context, user, id, template string, accept bool) (any, error) {
	c, configErr := activityPushConfig()
	if !accept {
		if configErr != nil {
			return M{"enabled": false, "templateId": "", "status": "NONE"}, nil
		}
		var status string
		err := s.store.db.QueryRowContext(ctx, "SELECT CASE WHEN n.push_status='READY' AND (n.template_id<>? OR NOT EXISTS(SELECT 1 FROM activity_reservations r WHERE r.openid=n.openid AND r.activity_id=n.activity_id AND r.created_at=n.reservation_created_at)) THEN 'NONE' ELSE n.push_status END FROM activity_push_subscriptions n WHERE n.openid=? AND n.activity_id=?", c.TemplateID, user, id).Scan(&status)
		if errors.Is(err, sql.ErrNoRows) {
			status = "NONE"
			err = nil
		}
		return M{"enabled": configErr == nil, "templateId": c.TemplateID, "status": status}, err
	}
	if configErr != nil {
		return nil, configErr
	}
	if !validActivityID(id) || template != c.TemplateID {
		return nil, errors.New("活动模板已变更，请刷新重试")
	}
	tx, err := s.store.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	var payload []byte
	if err = tx.QueryRowContext(ctx, "SELECT payload FROM configs WHERE config_key='home' FOR UPDATE").Scan(&payload); err != nil {
		return nil, err
	}
	var h HomeConfig
	if err = json.Unmarshal(payload, &h); err != nil {
		return nil, err
	}
	if err = reservableActivity(h, id, time.Now()); err != nil {
		return nil, err
	}
	today := time.Now().In(time.FixedZone("CST", 8*3600)).Format("2006-01-02")
	for _, a := range h.Activities {
		if a.ID == id && today > a.StartDate {
			return nil, errors.New("活动已开始，不再订阅开始提醒")
		}
	}
	var created time.Time
	if err = tx.QueryRowContext(ctx, "SELECT created_at FROM activity_reservations WHERE openid=? AND activity_id=? FOR UPDATE", user, id).Scan(&created); err != nil {
		return nil, errors.New("请先预约成功，候补用户请等待转正")
	}
	_, err = tx.ExecContext(ctx, "INSERT INTO activity_push_subscriptions(openid,activity_id,template_id,reservation_created_at,push_status) VALUES(?,?,?,?,'READY') ON DUPLICATE KEY UPDATE template_id=IF(push_status IN ('READY','FAILED','BLOCKED'),VALUES(template_id),template_id),reservation_created_at=IF(push_status IN ('READY','FAILED','BLOCKED'),VALUES(reservation_created_at),reservation_created_at),push_error=IF(push_status IN ('READY','FAILED','BLOCKED'),'',push_error),push_status=IF(push_status IN ('READY','FAILED','BLOCKED'),'READY',push_status)", user, id, c.TemplateID, created)
	if err != nil {
		return nil, err
	}
	var status string
	if err = tx.QueryRowContext(ctx, "SELECT push_status FROM activity_push_subscriptions WHERE openid=? AND activity_id=?", user, id).Scan(&status); err != nil {
		return nil, err
	}
	return M{"status": status}, tx.Commit()
}

func (s *Server) sendActivityPushes(ctx context.Context) (any, error) {
	c, err := activityPushConfig()
	if err != nil {
		return nil, err
	}
	var h HomeConfig
	if err = s.store.loadConfig(ctx, "home", &h); err != nil {
		return nil, err
	}
	result := M{"scanned": 0, "sent": 0, "failed": 0, "skipped": 0}
	for _, a := range h.Activities {
		if !activityPushEligible(a, time.Now()) {
			continue
		}
		remaining := 100 - result["scanned"].(int)
		if remaining <= 0 {
			break
		}
		rows, e := s.store.db.QueryContext(ctx, "SELECT n.openid FROM activity_push_subscriptions n JOIN activity_reservations r ON r.openid=n.openid AND r.activity_id=n.activity_id AND r.created_at=n.reservation_created_at WHERE n.activity_id=? AND n.template_id=? AND n.push_status='READY' ORDER BY n.updated_at,n.openid LIMIT ?", a.ID, c.TemplateID, remaining)
		if e != nil {
			return nil, e
		}
		users := []string{}
		for rows.Next() {
			var user string
			if e = rows.Scan(&user); e != nil {
				rows.Close()
				return nil, e
			}
			users = append(users, user)
		}
		e = rows.Err()
		rows.Close()
		if e != nil {
			return nil, e
		}
		for _, user := range users {
			result["scanned"] = result["scanned"].(int) + 1
			current, claimed, e := s.claimActivityPush(ctx, user, a.ID, c.TemplateID)
			if e != nil {
				return nil, e
			}
			if !claimed {
				result["skipped"] = result["skipped"].(int) + 1
				continue
			}
			status, detail := "SENT", ""
			if e = s.wechat.sendSubscriptionMessage(ctx, activityPushMessage(user, current, c)); e != nil {
				status, detail = "UNCERTAIN", "微信请求结果不确定，请人工核实"
				var rejection *messageRejection
				if errors.As(e, &rejection) {
					status, detail = "FAILED", rejection.Error()
					if rejection.Code == 43101 {
						status = "BLOCKED"
					}
				}
			}
			// Use a fresh bounded context so an expired job still persists the send outcome.
			finishCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
			_, e = s.store.db.ExecContext(finishCtx, "UPDATE activity_push_subscriptions SET push_status=?,push_error=? WHERE openid=? AND activity_id=? AND push_status='SENDING'", status, stringTruncate(detail, 500), user, a.ID)
			cancel()
			if e != nil {
				return nil, e
			}
			key := "failed"
			if status == "SENT" {
				key = "sent"
			}
			result[key] = result[key].(int) + 1
		}
	}
	return result, nil
}

func (s *Server) claimActivityPush(ctx context.Context, user, id, template string) (HomeActivity, bool, error) {
	var a HomeActivity
	tx, err := s.store.db.BeginTx(ctx, nil)
	if err != nil {
		return a, false, err
	}
	defer tx.Rollback()
	var payload []byte
	if err = tx.QueryRowContext(ctx, "SELECT payload FROM configs WHERE config_key='home' FOR UPDATE").Scan(&payload); err != nil {
		return a, false, err
	}
	var h HomeConfig
	if err = json.Unmarshal(payload, &h); err != nil {
		return a, false, err
	}
	for _, item := range h.Activities {
		if item.ID == id {
			a = item
			break
		}
	}
	if !activityPushEligible(a, time.Now()) {
		return a, false, nil
	}
	claim, err := tx.ExecContext(ctx, "UPDATE activity_push_subscriptions n SET push_status='SENDING',push_error='' WHERE openid=? AND activity_id=? AND template_id=? AND push_status='READY' AND EXISTS(SELECT 1 FROM activity_reservations r WHERE r.openid=n.openid AND r.activity_id=n.activity_id AND r.created_at=n.reservation_created_at)", user, id, template)
	if err != nil {
		return a, false, err
	}
	count, err := claim.RowsAffected()
	if err != nil {
		return a, false, err
	}
	return a, count == 1, tx.Commit()
}
