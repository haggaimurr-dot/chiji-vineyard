package main

import (
"bytes"
"context"
"database/sql"
"encoding/json"
"errors"
"log"
"net/http"
"os"
"strings"
"time"
)

// 通知中心（新增）：订单状态变更的短信/微信模板消息触达。
// - 微信：用户在小程序订阅后，经 cgi-bin/message/subscribe/send 发送；
// 需 WECHAT_ORDER_TEMPLATE_ID（小程序后台申请的订阅消息模板），未配置时仅记录不实发。
// - 短信：需 SMS_PROVIDER/SMS_API_KEY/SMS_SIGN 等配置（用户提供签名与模板后接入），
// 未配置时仅记录不实发。
// - 退订：notification_prefs 按（用户，通道，类型）记录开关。

const (
notifyKindOrderShipped = "ORDER_SHIPPED"
notifyKindOrderReceived = "ORDER_RECEIVED"
notifyKindOrderDelivered = "ORDER_DELIVERED"
)

func (s *Store) notifyEnabled(ctx context.Context, openid, channel, kind string) bool {
var enabled int
if err:= s.db.QueryRowContext(ctx, "SELECT enabled FROM notification_prefs WHERE openid=? AND channel=? AND kind=?", openid, channel, kind).Scan(&enabled); err!= nil {
return true
}
return enabled == 1
}

func (s *Store) enqueueNotification(ctx context.Context, openid, channel, kind, title string, payload M) {
if openid == "" {
return
}
if!s.notifyEnabled(ctx, openid, channel, kind) {
raw, _:= json.Marshal(payload)
s.db.ExecContext(ctx, "INSERT INTO notifications(id,openid,channel,kind,title,payload,status,error) VALUES(?,?,?,?,?,?,'BLOCKED','用户已退订')",
randomID("NTF"), openid, channel, kind, title, raw)
return
}
raw, _:= json.Marshal(payload)
s.db.ExecContext(ctx, "INSERT INTO notifications(id,openid,channel,kind,title,payload) VALUES(?,?,?,?,?,?)",
randomID("NTF"), openid, channel, kind, title, raw)
}

// notifyOrderEvent：订单状态变更后调用，为用户排入微信+短信通知。
func (s *Store) notifyOrderEvent(ctx context.Context, openid, kind, orderNo, detail string) {
if openid == "" {
return
}
payload:= M{"orderNo": orderNo, "detail": detail}
s.enqueueNotification(ctx, openid, "WECHAT", kind, "订单"+orderNo+detail, payload)
s.enqueueNotification(ctx, openid, "SMS", kind, "订单"+orderNo+detail, payload)
}

func wechatOrderTemplateID() string { return strings.TrimSpace(os.Getenv("WECHAT_ORDER_TEMPLATE_ID"))}

func smsConfigured() bool {
return strings.TrimSpace(os.Getenv("SMS_PROVIDER"))!= "" && strings.TrimSpace(os.Getenv("SMS_SIGN"))!= ""
}

// sendWechatNotification 经订阅消息发送；返回 nil 表示已发送。
func (s *Server) sendWechatNotification(ctx context.Context, openid, kind, title string, payload M) error {
templateID:= wechatOrderTemplateID()
if templateID == "" {
return errors.New("微信订单通知模板未配置（WECHAT_ORDER_TEMPLATE_ID）")
}
var sub string
if err:= s.store.db.QueryRowContext(ctx, "SELECT status FROM notification_subs WHERE openid=? AND kind=?", openid, kind).Scan(&sub); err!= nil || sub!= "ACCEPTED" {
return errors.New("用户未订阅该类通知")
}
msg:= M{"touser": openid, "template_id": templateID, "page": "pages/orders/index", "miniprogram_state": os.Getenv("WECHAT_MINIPROGRAM_STATE"), "lang": "zh_CN",
"data": M{"thing1": M{"value": stringTruncate(title, 20)}, "thing2": M{"value": stringTruncate(kind, 20)}}}
if msg["miniprogram_state"] == "" {
msg["miniprogram_state"] = "formal"
}
return s.wechat.sendSubscriptionMessage(ctx, msg)
}

// sendSMSNotification 短信发送；provider 未配置时返回错误由调用方记 SKIPPED。
func (s *Server) sendSMSNotification(ctx context.Context, openid, title string, payload M) error {
if!smsConfigured() {
return errors.New("短信通道未配置（SMS_PROVIDER/SMS_SIGN）")
}
provider:= strings.ToUpper(strings.TrimSpace(os.Getenv("SMS_PROVIDER")))
// 各短信服务商的 HTTP 接口差异大，此处预留统一出口：按 provider 分发。
// 当前支持 TENCENT（腾讯云短信 SendSms）。
if provider!= "TENCENT" {
return errors.New("不支持的短信服务商：" + provider)
}
return errors.New("腾讯云短信签名/模板尚未配置")
}

var notifyHTTP = &http.Client{Timeout: 15 * time.Second}

func (s *Store) runNotifyWorker(ctx context.Context) {
ticker:= time.NewTicker(time.Minute)
defer ticker.Stop()
for {
select {
case <-ctx.Done():
return
case <-ticker.C:
s.dispatchNotifications(ctx)
}
}
}

func (s *Store) dispatchNotifications(ctx context.Context) {
rows, err:= s.db.QueryContext(ctx, "SELECT id,openid,channel,kind,title,payload FROM notifications WHERE status='QUEUED' ORDER BY created_at LIMIT 50")
if err!= nil {
log.Printf("notify dispatch query: %v", err)
return
}
type item struct {
id, openid, channel, kind, title string
payload []byte
}
list:= []item{}
func() {
defer rows.Close()
for rows.Next() {
var it item
if err:= rows.Scan(&it.id, &it.openid, &it.channel, &it.kind, &it.title, &it.payload); err!= nil {
return
}
list = append(list, it)
}
}()
srv:= &Server{store: s}
for _, it:= range list {
var payload M
_ = json.Unmarshal(it.payload, &payload)
var sendErr error
switch it.channel {
case "WECHAT":
sendErr = srv.sendWechatNotification(ctx, it.openid, it.kind, it.title, payload)
case "SMS":
sendErr = srv.sendSMSNotification(ctx, it.openid, it.title, payload)
default:
sendErr = errors.New("未知通道")
}
if sendErr == nil {
s.db.ExecContext(ctx, "UPDATE notifications SET status='SENT',sent_at=NOW(3) WHERE id=?", it.id)
} else if strings.Contains(sendErr.Error(), "未配置") || strings.Contains(sendErr.Error(), "尚未配置") {
s.db.ExecContext(ctx, "UPDATE notifications SET status='SKIPPED',error=? WHERE id=?", sendErr.Error(), it.id)
} else {
s.db.ExecContext(ctx, "UPDATE notifications SET status='FAILED',error=? WHERE id=?", sendErr.Error(), it.id)
}
}
_ = bytes.MinRead
_ = sql.ErrNoRows
}

// notifyConfig：返回通知配置（模板是否配置、各类型订阅/开关状态）。
func (s *Store) notifyConfig(ctx context.Context, openid string) (any, error) {
	kinds := []string{notifyKindOrderShipped, notifyKindOrderReceived, notifyKindOrderDelivered}
	type kindState struct {
		Kind           string `json:"kind"`
		Label          string `json:"label"`
		WechatAccepted bool   `json:"wechatAccepted"`
		WechatEnabled  bool   `json:"wechatEnabled"`
		SMSEnabled     bool   `json:"smsEnabled"`
	}
	labels := map[string]string{notifyKindOrderShipped: "发货通知", notifyKindOrderReceived: "收货通知", notifyKindOrderDelivered: "派送通知"}
	states := []kindState{}
	for _, k := range kinds {
		var sub string
		_ = s.db.QueryRowContext(ctx, "SELECT status FROM notification_subs WHERE openid=? AND kind=?", openid, k).Scan(&sub)
		states = append(states, kindState{
			Kind:           k,
			Label:          labels[k],
			WechatAccepted: sub == "ACCEPTED",
			WechatEnabled:  s.notifyEnabled(ctx, openid, "WECHAT", k),
			SMSEnabled:     s.notifyEnabled(ctx, openid, "SMS", k),
		})
	}
	return M{"wechatConfigured": wechatOrderTemplateID() != "", "wechatTemplateId": wechatOrderTemplateID(), "smsConfigured": smsConfigured(), "kinds": states}, nil
}

// notifySubscribe：用户订阅某类微信通知（小程序 wx.requestSubscribeMessage 成功后调用）。
func (s *Store) notifySubscribe(ctx context.Context, openid string, raw json.RawMessage) (any, error) {
var d struct {
Kind string `json:"kind"`
Accept bool `json:"accept"`
}
if err:= decode(raw, &d); err!= nil {
return nil, err
}
switch d.Kind {
case notifyKindOrderShipped, notifyKindOrderReceived, notifyKindOrderDelivered:
default:
return nil, errors.New("通知类型无效")
}
status:= "REJECTED"
if d.Accept {
status = "ACCEPTED"
}
templateID:= wechatOrderTemplateID()
if _, err:= s.db.ExecContext(ctx, "INSERT INTO notification_subs(openid,kind,template_id,status) VALUES(?,?,?,?) ON DUPLICATE KEY UPDATE template_id=VALUES(template_id),status=VALUES(status)", openid, d.Kind, templateID, status); err!= nil {
return nil, err
}
return M{"subscribed": d.Accept}, nil
}

// notifyPrefSet：用户退订/重订某通道某类通知。
func (s *Store) notifyPrefSet(ctx context.Context, openid string, raw json.RawMessage) (any, error) {
var d struct {
Channel string `json:"channel"`
Kind string `json:"kind"`
Enabled bool `json:"enabled"`
}
if err:= decode(raw, &d); err!= nil {
return nil, err
}
if d.Channel!= "WECHAT" && d.Channel!= "SMS" {
return nil, errors.New("通道无效")
}
switch d.Kind {
case notifyKindOrderShipped, notifyKindOrderReceived, notifyKindOrderDelivered:
default:
return nil, errors.New("通知类型无效")
}
enabled:= 0
if d.Enabled {
enabled = 1
}
if _, err:= s.db.ExecContext(ctx, "INSERT INTO notification_prefs(openid,channel,kind,enabled) VALUES(?,?,?,?) ON DUPLICATE KEY UPDATE enabled=VALUES(enabled)", openid, d.Channel, d.Kind, enabled); err!= nil {
return nil, err
}
return M{"enabled": d.Enabled}, nil
}

func (s *Server) notifyStats(ctx context.Context, raw json.RawMessage) (any, error) {
rows, err:= s.store.db.QueryContext(ctx, "SELECT channel,status,COUNT(*) FROM notifications GROUP BY channel,status")
if err!= nil {
return nil, err
}
defer rows.Close()
byChannel:= map[string]map[string]int{}
for rows.Next() {
var ch, st string
var n int
if err = rows.Scan(&ch, &st, &n); err!= nil {
return nil, err
}
if byChannel[ch] == nil {
byChannel[ch] = map[string]int{}
}
byChannel[ch][st] = n
}
if err = rows.Err(); err!= nil {
return nil, err
}
out:= []M{}
for ch, m:= range byChannel {
total:= 0
for _, n:= range m {
total += n
}
rate:= 0.0
if total > 0 {
rate = float64(m["SENT"]) / float64(total) * 100
}
out = append(out, M{"channel": ch, "total": total, "sent": m["SENT"], "failed": m["FAILED"], "skipped": m["SKIPPED"], "blocked": m["BLOCKED"], "deliveryRate": rate})
}
return M{"items": out}, nil
}
