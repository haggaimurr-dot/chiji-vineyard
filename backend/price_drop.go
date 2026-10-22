package main

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"os"
	"regexp"
	"strings"
	"time"
)

// Baseline is the actual price of the chosen SKU, never its compare-at price.
type PriceDrop struct {
	SKUID         string `json:"skuId"`
	SKUName       string `json:"skuName"`
	BaselinePrice int    `json:"baselinePrice"`
	TargetPrice   int    `json:"targetPrice"`
	CurrentPrice  int    `json:"currentPrice"`
	Dropped       bool   `json:"dropped"`
	Available     bool   `json:"available"`
	PushStatus    string `json:"pushStatus"`
}

func priceDropFor(p Product, skuID string, baseline int, status string, targets ...int) *PriceDrop {
	d := &PriceDrop{SKUID: skuID, BaselinePrice: baseline, PushStatus: status}
	if len(targets) > 0 {
		d.TargetPrice = targets[0]
	}
	for _, k := range p.SKUs {
		if k.ID == skuID {
			d.SKUName, d.CurrentPrice = k.Name, k.Price
			d.Available = k.Stock > 0 && k.LimitPerOrder > 0
			d.Dropped = baseline > 0 && k.Price < baseline
			if d.TargetPrice > 0 {
				d.Dropped = k.Price <= d.TargetPrice
			}
			break
		}
	}
	return d
}

func (s *Store) attachPriceDrops(ctx context.Context, openid string, products []Product) error {
	needed := false
	for _, p := range products {
		if p.PriceDropReminder {
			needed = true
			break
		}
	}
	if !needed {
		return nil
	}
	rows, err := s.db.QueryContext(ctx, "SELECT product_id,sku_id,baseline_price,push_status,target_price FROM price_drop_notifications WHERE openid=?", openid)
	if err != nil {
		return err
	}
	defer rows.Close()
	reminders := map[string]*PriceDrop{}
	for rows.Next() {
		var id, skuID, status string
		var baseline, target int
		if err = rows.Scan(&id, &skuID, &baseline, &status, &target); err != nil {
			return err
		}
		reminders[id] = &PriceDrop{SKUID: skuID, BaselinePrice: baseline, PushStatus: status, TargetPrice: target}
	}
	for i := range products {
		if d := reminders[products[i].ID]; d != nil && products[i].PriceDropReminder {
			products[i].PriceDrop = priceDropFor(products[i], d.SKUID, d.BaselinePrice, d.PushStatus, d.TargetPrice)
		}
	}
	return rows.Err()
}

func (s *Store) setPriceDrop(ctx context.Context, openid, productID, skuID string, enabled bool, targets ...int) (any, error) {
	target := 0
	if len(targets) > 0 {
		target = targets[0]
	}
	if target < 0 || target > 100000000 {
		return nil, errors.New("目标价格无效")
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	if !enabled {
		if _, err = tx.ExecContext(ctx, "DELETE FROM product_attention WHERE openid=? AND product_id=? AND kind='pricedrop'", openid, productID); err != nil {
			return nil, err
		}
		if _, err = tx.ExecContext(ctx, "UPDATE price_drop_notifications SET push_status='CANCELLED',push_error='' WHERE openid=? AND product_id=? AND push_status NOT IN ('SENDING','UNCERTAIN')", openid, productID); err != nil {
			return nil, err
		}
		return M{"enabled": false}, tx.Commit()
	}
	// The server reads and locks the baseline; client prices are never trusted.
	var price int
	err = tx.QueryRowContext(ctx, "SELECT k.price FROM skus k JOIN products p ON p.id=k.product_id WHERE p.id=? AND p.status='PUBLISHED' AND k.id=? AND k.active=1 FOR UPDATE", productID, skuID).Scan(&price)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, errors.New("商品规格不存在或已下架")
	}
	if err != nil {
		return nil, err
	}
	if price <= 0 {
		return nil, errors.New("该规格暂不支持降价提醒")
	}
	if target > price {
		return nil, errors.New("目标价格不能高于当前规格价格")
	}
	if _, err = tx.ExecContext(ctx, "INSERT INTO product_attention(openid,product_id,kind) VALUES(?,?,'pricedrop') ON DUPLICATE KEY UPDATE product_id=VALUES(product_id)", openid, productID); err != nil {
		return nil, err
	}
	// Duplicate enables preserve the original SKU and baseline. A cancelled reminder
	// can be renewed, except while a previous send has an unresolved outcome.
	if _, err = tx.ExecContext(ctx, "INSERT INTO price_drop_notifications(openid,product_id,sku_id,baseline_price,target_price,push_status) VALUES(?,?,?,?,?,'NONE') ON DUPLICATE KEY UPDATE sku_id=IF(push_status='CANCELLED',VALUES(sku_id),sku_id),baseline_price=IF(push_status='CANCELLED',VALUES(baseline_price),baseline_price),target_price=IF(push_status='CANCELLED',VALUES(target_price),target_price),template_id=IF(push_status='CANCELLED','',template_id),push_error=IF(push_status='CANCELLED','',push_error),push_status=IF(push_status='CANCELLED','NONE',push_status)", openid, productID, skuID, price, target); err != nil {
		return nil, err
	}
	return M{"enabled": true}, tx.Commit()
}

var priceDropFieldRE = regexp.MustCompile(`^(thing|amount|time)[1-9][0-9]*$`)

func priceDropConfig() (shippingMessageConfig, error) {
	c := shippingMessageConfig{TemplateID: os.Getenv("WECHAT_PRICE_DROP_TEMPLATE_ID"), State: os.Getenv("WECHAT_MINIPROGRAM_STATE")}
	if c.TemplateID == "" {
		return c, errors.New("降价订阅消息尚未配置")
	}
	if err := json.Unmarshal([]byte(os.Getenv("WECHAT_PRICE_DROP_TEMPLATE_FIELDS")), &c.Fields); err != nil || len(c.Fields) == 0 {
		return c, errors.New("降价模板字段配置无效")
	}
	for key, source := range c.Fields {
		if !priceDropFieldRE.MatchString(key) || !((strings.HasPrefix(key, "thing") && (source == "product" || source == "sku" || source == "remark")) || (strings.HasPrefix(key, "amount") && (source == "baselinePrice" || source == "currentPrice")) || (strings.HasPrefix(key, "time") && source == "detectedAt")) {
			return c, errors.New("降价模板字段类型或来源无效")
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

func priceDropSubscriptionConfig() M {
	c, err := priceDropConfig()
	return M{"enabled": err == nil, "templateId": c.TemplateID}
}

func (s *Server) acceptPriceDropSubscription(ctx context.Context, openid, productID, templateID string) (any, error) {
	c, err := priceDropConfig()
	if err != nil {
		return nil, err
	}
	if templateID != c.TemplateID {
		return nil, errors.New("降价模板已更新，请刷新后重新订阅")
	}
	tx, err := s.store.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	var id string
	if err = tx.QueryRowContext(ctx, "SELECT product_id FROM product_attention WHERE openid=? AND product_id=? AND kind='pricedrop' FOR UPDATE", openid, productID).Scan(&id); err != nil {
		return nil, errors.New("请先设置降价提醒")
	}
	_, err = tx.ExecContext(ctx, "UPDATE price_drop_notifications SET template_id=?,push_status='READY',push_error='' WHERE openid=? AND product_id=? AND push_status IN ('NONE','BLOCKED','FAILED')", templateID, openid, productID)
	if err != nil {
		return nil, err
	}
	var status string
	if err = tx.QueryRowContext(ctx, "SELECT push_status FROM price_drop_notifications WHERE openid=? AND product_id=?", openid, productID).Scan(&status); err != nil {
		return nil, err
	}
	if err = tx.Commit(); err != nil {
		return nil, err
	}
	return M{"status": status}, nil
}

func priceDropMessage(openid string, p Product, d PriceDrop, c shippingMessageConfig, now time.Time) M {
	values := map[string]string{"product": stringTruncate(p.Name, 20), "sku": stringTruncate(d.SKUName, 20), "remark": "您关注的规格已降价，欢迎选购", "baselinePrice": fmt.Sprintf("%d.%02d", d.BaselinePrice/100, d.BaselinePrice%100), "currentPrice": fmt.Sprintf("%d.%02d", d.CurrentPrice/100, d.CurrentPrice%100), "detectedAt": now.In(time.FixedZone("CST", 8*3600)).Format("2006-01-02 15:04")}
	if d.TargetPrice > 0 {
		values["remark"] = "您关注的规格已到目标价，欢迎选购"
	}
	data := M{}
	for key, source := range c.Fields {
		data[key] = M{"value": values[source]}
	}
	return M{"touser": openid, "template_id": c.TemplateID, "page": "pages/product/index?id=" + url.QueryEscape(p.ID), "miniprogram_state": c.State, "lang": "zh_CN", "data": data}
}

func (s *Server) sendPriceDropNotices(ctx context.Context) (any, error) {
	c, err := priceDropConfig()
	if err != nil {
		return nil, err
	}
	rows, err := s.store.db.QueryContext(ctx, "SELECT n.openid,n.product_id,n.sku_id,n.baseline_price,n.target_price FROM price_drop_notifications n JOIN product_attention a ON a.openid=n.openid AND a.product_id=n.product_id AND a.kind='pricedrop' JOIN products p ON p.id=n.product_id AND p.status='PUBLISHED' JOIN skus k ON k.product_id=n.product_id AND k.id=n.sku_id AND k.active=1 WHERE n.push_status IN ('READY','FAILED') AND n.template_id=? AND ((n.target_price=0 AND k.price<n.baseline_price) OR (n.target_price>0 AND k.price<=n.target_price)) AND k.stock>0 AND k.limit_per_order>0 ORDER BY n.updated_at,n.openid,n.product_id LIMIT 100", c.TemplateID)
	if err != nil {
		return nil, err
	}
	type target struct {
		openid, productID, skuID string
		baseline, target         int
	}
	targets := []target{}
	for rows.Next() {
		var t target
		if err = rows.Scan(&t.openid, &t.productID, &t.skuID, &t.baseline, &t.target); err != nil {
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
		d := priceDropFor(*p, t.skuID, t.baseline, "READY", t.target)
		if !d.Dropped || !d.Available {
			result["skipped"] = result["skipped"].(int) + 1
			continue
		}
		historyID := randomID("reminder")
		claim, e := s.store.claimReminder(ctx, AttentionHistory{ID: historyID, ProductID: p.ID, ProductName: p.Name, Kind: "pricedrop", SKUName: d.SKUName, CurrentPrice: d.CurrentPrice, TargetPrice: d.TargetPrice}, t.openid, "UPDATE price_drop_notifications n SET push_status='SENDING',push_error='' WHERE openid=? AND product_id=? AND template_id=? AND sku_id=? AND baseline_price=? AND target_price=? AND push_status IN ('READY','FAILED') AND EXISTS(SELECT 1 FROM product_attention a WHERE a.openid=n.openid AND a.product_id=n.product_id AND a.kind='pricedrop') AND EXISTS(SELECT 1 FROM products p JOIN skus k ON k.product_id=p.id WHERE p.id=n.product_id AND p.status='PUBLISHED' AND k.id=n.sku_id AND k.active=1 AND k.price=? AND ((n.target_price=0 AND k.price<n.baseline_price) OR (n.target_price>0 AND k.price<=n.target_price)) AND k.stock>0 AND k.limit_per_order>0)", t.openid, t.productID, c.TemplateID, t.skuID, t.baseline, t.target, d.CurrentPrice)
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
		if e = s.wechat.sendSubscriptionMessage(ctx, priceDropMessage(t.openid, *p, *d, c, time.Now())); e != nil {
			status, detail = "UNCERTAIN", "微信请求结果不确定，请人工核实"
			var rejection *messageRejection
			if errors.As(e, &rejection) {
				status, detail = "FAILED", rejection.Error()
				if rejection.Code == 43101 {
					status = "BLOCKED"
				}
			}
		}
		if e = s.store.finishReminder(ctx, historyID, "pricedrop", status, detail, t.openid, t.productID); e != nil {
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
