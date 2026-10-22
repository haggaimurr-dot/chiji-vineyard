package main

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"regexp"
	"strings"
	"time"
)

type ShippingNotice struct {
	Parcels    []Shipment `json:"parcels,omitempty"`
	OrderID    string     `json:"orderId"`
	OrderNo    string     `json:"orderNo"`
	Carrier    string     `json:"carrier"`
	TrackingNo string     `json:"trackingNo"`
	ShippedAt  string     `json:"shippedAt"`
	Read       bool       `json:"read"`
}

type shippingMessageConfig struct {
	TemplateID string
	Fields     map[string]string
	State      string
}

var messageFieldRE = regexp.MustCompile(`^(thing|character_string|time)[1-9][0-9]*$`)

func shippingConfig() (shippingMessageConfig, error) {
	c := shippingMessageConfig{TemplateID: os.Getenv("WECHAT_SHIPPING_TEMPLATE_ID"), State: os.Getenv("WECHAT_MINIPROGRAM_STATE")}
	if c.TemplateID == "" {
		return c, errors.New("发货订阅消息尚未配置")
	}
	if err := json.Unmarshal([]byte(os.Getenv("WECHAT_SHIPPING_TEMPLATE_FIELDS")), &c.Fields); err != nil {
		return c, errors.New("发货订阅模板字段配置无效")
	}
	if len(c.Fields) == 0 {
		return c, errors.New("发货订阅模板字段不能为空")
	}
	for key, source := range c.Fields {
		if !messageFieldRE.MatchString(key) {
			return c, errors.New("发货模板仅支持 thing、character_string、time 字段")
		}
		switch source {
		case "product", "carrier", "orderNo", "trackingNo", "shippedAt":
		default:
			return c, errors.New("发货模板数据来源无效")
		}
		if source == "shippedAt" && !strings.HasPrefix(key, "time") {
			return c, errors.New("发货时间需映射到 time 字段")
		}
		if (source == "carrier" || source == "product") && !strings.HasPrefix(key, "thing") {
			return c, errors.New("商品和承运商需映射到 thing 字段")
		}
		if (source == "orderNo" || source == "trackingNo") && !strings.HasPrefix(key, "character_string") {
			return c, errors.New("订单号和运单号需映射到 character_string 字段")
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

func (s *Server) shippingSubscription(ctx context.Context, openid, id string, accept bool) (any, error) {
	tx, err := s.store.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	o, err := s.store.order(ctx, tx, id, true)
	if err != nil {
		return nil, err
	}
	if o == nil || o.OpenID != openid {
		return nil, errors.New("订单不存在")
	}
	c, configErr := shippingConfig()
	var status string
	err = tx.QueryRowContext(ctx, "SELECT push_status FROM shipping_notifications WHERE order_id=?", id).Scan(&status)
	if errors.Is(err, sql.ErrNoRows) {
		status = "NONE"
	} else if err != nil {
		return nil, err
	}
	if accept {
		if configErr != nil {
			return nil, configErr
		}
		if o.PaymentStatus != "PAID" || (o.FulfillmentStatus != "PREPARING" && o.FulfillmentStatus != "UNFULFILLED" && o.FulfillmentStatus != "SHIPPED") || (o.RefundStatus != "NONE" && o.RefundStatus != "REJECTED" && o.RefundStatus != "PARTIAL") {
			return nil, errors.New("当前订单不能订阅发货通知")
		}
		if status == "NONE" || status == "FAILED" || status == "BLOCKED" {
			_, err = tx.ExecContext(ctx, "INSERT INTO shipping_notifications(order_id,openid,template_id,push_status) VALUES(?,?,?,'READY') ON DUPLICATE KEY UPDATE template_id=VALUES(template_id),push_status='READY',push_error=''", id, openid, c.TemplateID)
			if err != nil {
				return nil, err
			}
			status = "READY"
		}
	}
	if err = tx.Commit(); err != nil {
		return nil, err
	}
	if accept && o.Shipment != nil {
		result, sendErr := s.sendShippingNotice(ctx, id)
		if sendErr == nil {
			status = result["status"].(string)
		}
	}
	return M{"enabled": configErr == nil, "templateId": c.TemplateID, "status": status}, nil
}

func (s *Server) shippingNotices(ctx context.Context, openid string) (any, error) {
	var unread int
	if err := s.store.db.QueryRowContext(ctx, "SELECT COUNT(*) FROM shipping_notifications WHERE openid=? AND notified_at IS NOT NULL AND read_at IS NULL", openid).Scan(&unread); err != nil {
		return nil, err
	}
	rows, err := s.store.db.QueryContext(ctx, "SELECT n.order_id,o.order_no,o.payload,n.read_at FROM shipping_notifications n JOIN orders o ON o.id=n.order_id WHERE n.openid=? AND n.notified_at IS NOT NULL ORDER BY (n.read_at IS NULL) DESC,n.notified_at DESC,n.order_id DESC LIMIT 200", openid)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	notices := []ShippingNotice{}
	for rows.Next() {
		var n ShippingNotice
		var raw []byte
		var read sql.NullTime
		var o Order
		if err = rows.Scan(&n.OrderID, &n.OrderNo, &raw, &read); err != nil {
			return nil, err
		}
		if err = json.Unmarshal(raw, &o); err != nil {
			return nil, err
		}
		if o.Shipment == nil {
			continue
		}
		n.Parcels = o.Shipment.Parcels
		n.Carrier = o.Shipment.Carrier
		n.TrackingNo = o.Shipment.TrackingNo
		n.ShippedAt = o.Shipment.ShippedAt
		n.Read = read.Valid
		notices = append(notices, n)
	}
	return M{"items": notices, "unread": unread}, rows.Err()
}

func (s *Server) readShippingNotice(ctx context.Context, openid, id string) (any, error) {
	_, err := s.store.db.ExecContext(ctx, "UPDATE shipping_notifications SET read_at=COALESCE(read_at,NOW(3)) WHERE order_id=? AND openid=? AND notified_at IS NOT NULL", id, openid)
	return M{"read": true}, err
}

// A compare-and-set claims one send. Ambiguous network failures stay UNCERTAIN:
// automatically retrying those could consume another grant or send twice.
func (s *Server) sendShippingNotice(ctx context.Context, id string) (M, error) {
	o, err := s.store.order(ctx, s.store.db, id, false)
	if err != nil {
		return nil, err
	}
	if o == nil || o.Shipment == nil {
		return nil, errors.New("订单尚未发货")
	}
	c, err := shippingConfig()
	if err != nil {
		return nil, err
	}
	result, err := s.store.db.ExecContext(ctx, "UPDATE shipping_notifications SET push_status='SENDING',push_error='' WHERE order_id=? AND template_id=? AND push_status IN ('READY','FAILED') AND notified_at IS NOT NULL", id, c.TemplateID)
	if err != nil {
		return nil, err
	}
	n, err := result.RowsAffected()
	if err != nil {
		return nil, err
	}
	if n == 0 {
		var status string
		err = s.store.db.QueryRowContext(ctx, "SELECT push_status FROM shipping_notifications WHERE order_id=?", id).Scan(&status)
		return M{"status": status}, err
	}
	status, detail := "SENT", ""
	if err = s.wechat.sendShippingMessage(ctx, *o, c); err != nil {
		status, detail = "UNCERTAIN", "微信请求结果不确定，请人工核实"
		var rejection *messageRejection
		if errors.As(err, &rejection) {
			status, detail = "FAILED", rejection.Error()
			if rejection.Code == 43101 {
				status = "BLOCKED"
			}
		}
	}
	_, updateErr := s.store.db.ExecContext(ctx, "UPDATE shipping_notifications SET push_status=?,push_error=? WHERE order_id=? AND push_status='SENDING'", status, clean(detail, 240), id)
	return M{"status": status, "error": detail}, updateErr
}

type messageRejection struct {
	Code    int
	Message string
}

func (e *messageRejection) Error() string {
	return fmt.Sprintf("微信消息发送失败：%s (%d)", e.Message, e.Code)
}

func shippingMessage(o Order, c shippingMessageConfig) M {
	values := map[string]string{"orderNo": o.OrderNo, "carrier": o.Shipment.Carrier, "trackingNo": o.Shipment.TrackingNo}
	if len(o.Items) > 0 {
		values["product"] = o.Items[0].Name
	}
	shipped, _ := time.Parse(time.RFC3339Nano, o.Shipment.ShippedAt)
	values["shippedAt"] = shipped.In(time.FixedZone("CST", 8*3600)).Format("2006-01-02 15:04")
	data := M{}
	for key, source := range c.Fields {
		value := values[source]
		if len(key) >= 5 && key[:5] == "thing" {
			value = stringTruncate(value, 20)
		}
		data[key] = M{"value": value}
	}
	return M{"touser": o.OpenID, "template_id": c.TemplateID, "page": "pages/order-detail/index?id=" + o.ID, "miniprogram_state": c.State, "lang": "zh_CN", "data": data}
}

func stringTruncate(v string, count int) string {
	chars := []rune(v)
	if len(chars) > count {
		return string(chars[:count])
	}
	return v
}
