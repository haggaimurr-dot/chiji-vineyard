package main

import (
	"context"
	"errors"
	"encoding/json"
	"log"
	"os"
	"strings"
	"time"
)

// 直播预告推送：home 直播配置加预告时间/文案；用户订阅后开播前 30 分钟内推送微信订阅消息。
// 推送需配置 WECHAT_LIVE_TEMPLATE_ID（小程序后台申请的订阅消息模板），未配置时仅记录订阅不实发。

func liveNoticeConfig() (shippingMessageConfig, error) {
	c := shippingMessageConfig{TemplateID: strings.TrimSpace(os.Getenv("WECHAT_LIVE_TEMPLATE_ID")), State: os.Getenv("WECHAT_MINIPROGRAM_STATE")}
	if c.TemplateID == "" {
		return c, errors.New("直播预告微信提醒尚未配置")
	}
	if c.State == "" {
		c.State = "formal"
	}
	if c.State != "formal" && c.State != "trial" && c.State != "developer" {
		return c, errors.New("小程序版本配置无效")
	}
	return c, nil
}

func liveNoticeMessage(user, channel, noticeText, noticeAt string, c shippingMessageConfig) M {
	if strings.TrimSpace(noticeText) == "" {
		noticeText = "果园直播即将开始"
	}
	name := map[string]string{"orchard": "果园机位", "stall": "档口机位"}[channel]
	if name == "" {
		name = "果园直播"
	}
	return M{"touser": user, "template_id": c.TemplateID, "page": "pages/orchard-live/index", "miniprogram_state": c.State, "lang": "zh_CN",
		"data": M{"thing1": M{"value": stringTruncate(noticeText, 20)}, "thing2": M{"value": stringTruncate(name, 20)}, "time3": M{"value": noticeAt}}}
}

func validLiveChannel(channel string) bool { return channel == "orchard" || channel == "stall" }

func liveChannelOf(h HomeConfig, channel string) OrchardLiveChannel {
	if h.Live == nil { return OrchardLiveChannel{} }
	if channel == "stall" { return h.Live.Stall }
	return h.Live.Orchard
}

func (s *Server) liveNoticeStatus(ctx context.Context, openid string, raw json.RawMessage) (any, error) {
	var d struct {
		Channel string `json:"channel"`
	}
	if err := decode(raw, &d); err != nil {
		return nil, err
	}
	if !validLiveChannel(d.Channel) {
		return nil, errors.New("直播机位无效")
	}
	var h HomeConfig
	if err := s.store.loadConfig(ctx, "home", &h); err != nil {
		return nil, err
	}
	ch := liveChannelOf(h, d.Channel)
	var status string
	if err := s.store.db.QueryRowContext(ctx, "SELECT push_status FROM live_notice_subscriptions WHERE openid=? AND channel=?", openid, d.Channel).Scan(&status); err != nil {
		status = "NONE"
	}
	c, templateErr := liveNoticeConfig()
	tmpl := ""
	if templateErr == nil { tmpl = c.TemplateID }
	return M{"noticeAt": ch.NoticeAt, "noticeText": ch.NoticeText, "subscribed": status == "READY", "pushConfigured": templateErr == nil, "templateId": tmpl}, nil
}

func (s *Server) liveNoticeSubscribe(ctx context.Context, openid string, raw json.RawMessage) (any, error) {
	var d struct {
		Channel string `json:"channel"`
		Accept  bool   `json:"accept"`
	}
	if err := decode(raw, &d); err != nil {
		return nil, err
	}
	if !validLiveChannel(d.Channel) {
		return nil, errors.New("直播机位无效")
	}
	c, configErr := liveNoticeConfig()
	templateID := ""
	if configErr == nil {
		templateID = c.TemplateID
	}
	var h HomeConfig
	if err := s.store.loadConfig(ctx, "home", &h); err != nil {
		return nil, err
	}
	ch := liveChannelOf(h, d.Channel)
	status := "CANCELLED"
	if d.Accept {
		status = "READY"
	}
	if _, err := s.store.db.ExecContext(ctx, `INSERT INTO live_notice_subscriptions(openid,channel,template_id,notice_at,push_status) VALUES(?,?,?,?,?)
ON DUPLICATE KEY UPDATE template_id=VALUES(template_id),notice_at=VALUES(notice_at),push_status=VALUES(push_status),push_error=''`, openid, d.Channel, templateID, ch.NoticeAt, status); err != nil {
		return nil, err
	}
	return M{"subscribed": d.Accept, "pushConfigured": configErr == nil}, nil
}

func (s *Server) sendLiveNotices(ctx context.Context) (any, error) {
	c, err := liveNoticeConfig()
	if err != nil {
		return nil, err
	}
	var h HomeConfig
	if err = s.store.loadConfig(ctx, "home", &h); err != nil {
		return nil, err
	}
	result := M{"scanned": 0, "sent": 0, "failed": 0, "skipped": 0}
	deadline := time.Now().Add(30 * time.Minute).UTC().Format(time.RFC3339)
	for _, ch := range []struct {
		id  string
		cfg OrchardLiveChannel
	}{{"orchard", liveChannelOf(h, "orchard")}, {"stall", liveChannelOf(h, "stall")}} {
		if ch.cfg.NoticeAt == "" || ch.cfg.NoticeAt > deadline {
			continue
		}
		rows, e := s.store.db.QueryContext(ctx, "SELECT openid FROM live_notice_subscriptions WHERE channel=? AND notice_at=? AND push_status='READY' ORDER BY updated_at LIMIT 100", ch.id, ch.cfg.NoticeAt)
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
			var claimed int64
			r, e := s.store.db.ExecContext(ctx, "UPDATE live_notice_subscriptions SET push_status='SENDING' WHERE openid=? AND channel=? AND notice_at=? AND push_status='READY'", user, ch.id, ch.cfg.NoticeAt)
			if e != nil {
				return nil, e
			}
			claimed, _ = r.RowsAffected()
			if claimed == 0 {
				result["skipped"] = result["skipped"].(int) + 1
				continue
			}
			status, detail := "SENT", ""
			if e = s.wechat.sendSubscriptionMessage(ctx, liveNoticeMessage(user, ch.id, ch.cfg.NoticeText, ch.cfg.NoticeAt, c)); e != nil {
				status, detail = "FAILED", e.Error()
				result["failed"] = result["failed"].(int) + 1
			} else {
				result["sent"] = result["sent"].(int) + 1
			}
			if _, e = s.store.db.ExecContext(ctx, "UPDATE live_notice_subscriptions SET push_status=?,push_error=? WHERE openid=? AND channel=? AND notice_at=?", status, detail, user, ch.id, ch.cfg.NoticeAt); e != nil {
				return nil, e
			}
		}
	}
	return result, nil
}

func (s *Server) runLiveNoticeScanner(ctx context.Context) {
	if _, err := liveNoticeConfig(); err != nil {
		log.Printf("live notice: %v，订阅仅记录不推送", err)
		return
	}
	ticker := time.NewTicker(5 * time.Minute)
	defer ticker.Stop()
	for {
		job, cancel := context.WithTimeout(ctx, 60*time.Second)
		if _, err := s.sendLiveNotices(job); err != nil && ctx.Err() == nil {
			log.Printf("live notice scan: %v", err)
		}
		cancel()
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
	}
}

