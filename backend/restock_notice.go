package main

import (
	"context"
	"encoding/json"
	"errors"
	"net/url"
	"os"
	"strings"
	"time"
)

func restockConfig() (shippingMessageConfig, error) {
	c := shippingMessageConfig{TemplateID: os.Getenv("WECHAT_RESTOCK_TEMPLATE_ID"), State: os.Getenv("WECHAT_MINIPROGRAM_STATE")}
	if c.TemplateID == "" {
		return c, errors.New("到货订阅消息尚未配置")
	}
	if err := json.Unmarshal([]byte(os.Getenv("WECHAT_RESTOCK_TEMPLATE_FIELDS")), &c.Fields); err != nil || len(c.Fields) == 0 {
		return c, errors.New("到货模板字段配置无效")
	}
	for key, source := range c.Fields {
		if !messageFieldRE.MatchString(key) || !((strings.HasPrefix(key, "thing") && (source == "product" || source == "remark")) || (strings.HasPrefix(key, "time") && source == "restockedAt")) {
			return c, errors.New("到货模板仅支持商品、提示的 thing 字段和到货时间的 time 字段")
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

func restockSubscriptionConfig() M {
	c, err := restockConfig()
	return M{"enabled": err == nil, "templateId": c.TemplateID}
}

func (s *Server) acceptRestockSubscription(ctx context.Context, openid, productID, templateID string) (any, error) {
	c, err := restockConfig()
	if err != nil {
		return nil, err
	}
	if c.TemplateID != templateID {
		return nil, errors.New("到货模板已更新，请刷新后重新订阅")
	}
	tx, err := s.store.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	var id string
	if err = tx.QueryRowContext(ctx, "SELECT product_id FROM product_attention WHERE openid=? AND product_id=? AND kind='restock' FOR UPDATE", openid, productID).Scan(&id); err != nil {
		return nil, errors.New("请先设置到货提醒")
	}
	// One grant per active reminder; repeated taps never reset an in-flight send.
	_, err = tx.ExecContext(ctx, "INSERT INTO restock_notifications(openid,product_id,template_id,push_status) VALUES(?,?,?,'READY') ON DUPLICATE KEY UPDATE template_id=IF(push_status IN ('CANCELLED','BLOCKED','FAILED'),VALUES(template_id),template_id),push_error=IF(push_status IN ('CANCELLED','BLOCKED','FAILED'),'',push_error),push_status=IF(push_status IN ('CANCELLED','BLOCKED','FAILED'),'READY',push_status)", openid, productID, c.TemplateID)
	if err != nil {
		return nil, err
	}
	var status string
	if err = tx.QueryRowContext(ctx, "SELECT push_status FROM restock_notifications WHERE openid=? AND product_id=?", openid, productID).Scan(&status); err != nil {
		return nil, err
	}
	if err = tx.Commit(); err != nil {
		return nil, err
	}
	return M{"status": status}, nil
}

func restockMessage(openid string, p Product, c shippingMessageConfig, now time.Time) M {
	values := map[string]string{"product": stringTruncate(p.Name, 20), "remark": "您关注的商品已到货，欢迎选购", "restockedAt": now.In(time.FixedZone("CST", 8*3600)).Format("2006-01-02 15:04")}
	data := M{}
	for key, source := range c.Fields {
		data[key] = M{"value": values[source]}
	}
	return M{"touser": openid, "template_id": c.TemplateID, "page": "pages/product/index?id=" + url.QueryEscape(p.ID), "miniprogram_state": c.State, "lang": "zh_CN", "data": data}
}

func (s *Server) sendRestockNotices(ctx context.Context) (any, error) {
	c, err := restockConfig()
	if err != nil {
		return nil, err
	}
	rows, err := s.store.db.QueryContext(ctx, "SELECT n.openid,n.product_id FROM restock_notifications n JOIN product_attention a ON a.openid=n.openid AND a.product_id=n.product_id AND a.kind='restock' JOIN products p ON p.id=n.product_id AND p.status='PUBLISHED' WHERE n.push_status IN ('READY','FAILED') AND n.template_id=? AND EXISTS(SELECT 1 FROM skus k WHERE k.product_id=n.product_id AND k.active=1 AND k.stock>0 AND k.limit_per_order>0) ORDER BY n.updated_at,n.openid,n.product_id LIMIT 100", c.TemplateID)
	if err != nil {
		return nil, err
	}
	type target struct{ openid, productID string }
	targets := []target{}
	for rows.Next() {
		var t target
		if err = rows.Scan(&t.openid, &t.productID); err != nil {
			rows.Close()
			return nil, err
		}
		targets = append(targets, t)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return nil, err
	}
	result := M{"scanned": len(targets), "sent": 0, "failed": 0, "skipped": 0}
	for _, t := range targets {
		p, e := s.store.product(ctx, t.productID, false)
		if e != nil {
			return nil, e
		}
		if p == nil {
			result["skipped"] = result["skipped"].(int) + 1
			continue
		}
		// Recheck availability and active reminder while atomically claiming the send.
		historyID := randomID("reminder")
		claim, e := s.store.claimReminder(ctx, AttentionHistory{ID: historyID, ProductID: p.ID, ProductName: p.Name, Kind: "restock"}, t.openid, "UPDATE restock_notifications n SET push_status='SENDING',push_error='' WHERE openid=? AND product_id=? AND template_id=? AND push_status IN ('READY','FAILED') AND EXISTS(SELECT 1 FROM product_attention a WHERE a.openid=n.openid AND a.product_id=n.product_id AND a.kind='restock') AND EXISTS(SELECT 1 FROM products p JOIN skus k ON k.product_id=p.id WHERE p.id=n.product_id AND p.status='PUBLISHED' AND k.active=1 AND k.stock>0 AND k.limit_per_order>0)", t.openid, t.productID, c.TemplateID)
		if e != nil {
			return nil, e
		}
		count, e := claim.RowsAffected()
		if e != nil {
			return nil, e
		}
		if count == 0 {
			result["skipped"] = result["skipped"].(int) + 1
			continue
		}
		status, detail := "SENT", ""
		if e = s.wechat.sendSubscriptionMessage(ctx, restockMessage(t.openid, *p, c, time.Now())); e != nil {
			status, detail = "UNCERTAIN", "微信请求结果不确定，请人工核实"
			var rejection *messageRejection
			if errors.As(e, &rejection) {
				status, detail = "FAILED", rejection.Error()
				if rejection.Code == 43101 {
					status = "BLOCKED"
				}
			}
		}
		e = s.store.finishReminder(ctx, historyID, "restock", status, detail, t.openid, t.productID)
		if e != nil {
			return nil, e
		}
		key := "failed"
		if status == "SENT" {
			key = "sent"
		}
		result[key] = result[key].(int) + 1
	}
	return result, nil
}
