package main

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"sort"
	"strings"
	"time"
)

func chinaDateString() string {
	return time.Now().In(time.FixedZone("Asia/Shanghai", 8*3600)).Format("2006-01-02")
}

func loadAllocationBatches(ctx context.Context, tx *sql.Tx, ids map[string]bool) (map[string]ProductBatch, error) {
	keys := []string{}
	for id := range ids {
		keys = append(keys, id)
	}
	sort.Strings(keys)
	out := map[string]ProductBatch{}
	for _, id := range keys {
		var data []byte
		var b ProductBatch
		if err := tx.QueryRowContext(ctx, "SELECT payload FROM product_batches WHERE id=? FOR UPDATE", id).Scan(&data); err != nil {
			return nil, err
		}
		if err := json.Unmarshal(data, &b); err != nil {
			return nil, err
		}
		b.Code = b.ID
		out[id] = b
	}
	return out, nil
}

func requireOrderBatches(ctx context.Context, tx *sql.Tx, o Order) error {
	if len(o.Batches) > 0 {
		if err := validateOrderBatches(o, o.Batches); err != nil {
			return err
		}
		ids := map[string]bool{}
		for _, a := range o.Batches {
			ids[a.BatchID] = true
		}
		batches, err := loadAllocationBatches(ctx, tx, ids)
		if err != nil {
			return err
		}
		for _, b := range batches {
			if b.Status != "ACTIVE" || (b.ExpiresAt != "" && b.ExpiresAt < chinaDateString()) {
				return errors.New("订单批次已归档或过期，请重新登记后发货")
			}
		}
		return nil
	}
	// Existing products with no managed batches retain their original shipping flow.
	for _, item := range o.Items {
		var count int
		if err := tx.QueryRowContext(ctx, "SELECT COUNT(*) FROM product_batches WHERE product_id=?", item.ProductID).Scan(&count); err != nil {
			return err
		}
		if count > 0 {
			return errors.New("请先登记订单实际批次和数量，再发货")
		}
	}
	return nil
}

// Batch is a snapshot: later edits and archival never rewrite order history.
type OrderBatch struct {
	SKUID    string       `json:"skuId"`
	BatchID  string       `json:"batchId"`
	Quantity int          `json:"quantity"`
	Batch    ProductBatch `json:"batch"`
}

func validateOrderBatches(o Order, allocations []OrderBatch) error {
	if len(allocations) == 0 || len(allocations) > 200 {
		return errors.New("请登记 1–200 条订单批次")
	}
	quantities := map[string]int{}
	products := map[string]string{}
	for _, item := range o.Items {
		quantities[item.SKUID] += item.Quantity
		products[item.SKUID] = item.ProductID
	}
	seen := map[string]bool{}
	for _, a := range allocations {
		key := a.SKUID + "\x00" + a.BatchID
		if a.Quantity <= 0 || a.Quantity > quantities[a.SKUID] || seen[key] || a.BatchID != a.Batch.ID || a.Batch.ProductID != products[a.SKUID] {
			return errors.New("批次归属、数量或重复记录无效")
		}
		seen[key] = true
		quantities[a.SKUID] -= a.Quantity
	}
	for _, remaining := range quantities {
		if remaining != 0 {
			return errors.New("每个规格的批次数量合计须等于订单数量")
		}
	}
	return nil
}

func (s *Server) saveOrderBatches(ctx context.Context, actor Claims, raw json.RawMessage) (any, error) {
	var d struct {
		OrderID         string       `json:"orderId"`
		ExpectedVersion int          `json:"expectedVersion"`
		Items           []OrderBatch `json:"items"`
	}
	if err := decode(raw, &d); err != nil {
		return nil, err
	}
	if len(d.Items) == 0 || len(d.Items) > 200 {
		return nil, errors.New("请登记 1–200 条订单批次")
	}
	tx, err := s.store.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	o, err := s.store.order(ctx, tx, d.OrderID, true)
	if err != nil {
		return nil, err
	}
	if o == nil {
		return nil, errors.New("订单不存在")
	}
	if !canShip(*o) || o.OrderStatus != "CONFIRMED" {
		return nil, errors.New("仅待发货订单可登记批次，发货后保留记录")
	}
	if o.BatchVersion != d.ExpectedVersion {
		return nil, errors.New("订单批次已变化，请刷新后重试")
	}
	// Lock batches in a stable order across concurrent order allocations.
	ids := map[string]bool{}
	for _, a := range d.Items {
		ids[a.BatchID] = true
	}
	batches, err := loadAllocationBatches(ctx, tx, ids)
	if err != nil {
		return nil, err
	}
	for i := range d.Items {
		b := batches[d.Items[i].BatchID]
		if b.Status != "ACTIVE" || (b.ExpiresAt != "" && b.ExpiresAt < chinaDateString()) {
			return nil, errors.New("批次不存在、已归档或已过期")
		}
		d.Items[i].Batch = b
	}
	if err = validateOrderBatches(*o, d.Items); err != nil {
		return nil, err
	}
	o.Batches = d.Items
	o.BatchVersion++
	if _, err = tx.ExecContext(ctx, "UPDATE orders SET payload=JSON_SET(payload,'$.batches',CAST(? AS JSON),'$.batchVersion',?) WHERE id=?", jsonValue(o.Batches), o.BatchVersion, o.ID); err != nil {
		return nil, err
	}
	if _, err = tx.ExecContext(ctx, "INSERT INTO audit_logs(admin_id,username,action,target_id,detail) VALUES(?,?,?,?,?)", actor.Subject, actor.Username, "ORDER_BATCH_SAVE", o.ID, jsonValue(o.Batches)); err != nil {
		return nil, err
	}
	return o, tx.Commit()
}

func (s *Server) queryTraceCode(ctx context.Context, raw json.RawMessage) (any, error) {
	var d struct {
		Code string `json:"code"`
	}
	if err := decode(raw, &d); err != nil {
		return nil, err
	}
	d.Code = strings.TrimSpace(d.Code)
	if !homeText(d.Code, 120) {
		return nil, errors.New("请输入有效溯源码")
	}
	var data []byte
	err := s.store.db.QueryRowContext(ctx, "SELECT payload FROM product_batches WHERE id=?", d.Code).Scan(&data)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, errors.New("未找到该溯源码，请核对包装上的溯源码")
	}
	if err != nil {
		return nil, err
	}
	var b ProductBatch
	if err = json.Unmarshal(data, &b); err != nil {
		return nil, err
	}
	b.Code = b.ID
	// Public fields deliberately exclude internal notes and customer/order data.
	result := M{"code": b.Code, "number": b.Number, "orchard": b.Orchard, "harvestedAt": b.HarvestedAt, "packedAt": b.PackedAt, "expiresAt": b.ExpiresAt, "status": b.Status}
	// 关联商品公开信息（名称/品种/封面），其余商品字段不泄露。
	if b.ProductID != "" {
		var prodPayload []byte
		if err := s.store.db.QueryRowContext(ctx, "SELECT payload FROM products WHERE id=? AND status='PUBLISHED'", b.ProductID).Scan(&prodPayload); err == nil {
			var prod Product
			if json.Unmarshal(prodPayload, &prod) == nil {
				result["productName"] = prod.Name
				result["variety"] = prod.Variety
				result["cover"] = prod.Cover
			}
		}
	}
	if b.ExternalTrace != nil && validateExternalTrace(b.ExternalTrace) == nil {
		result["externalTrace"] = b.ExternalTrace
	}
	return result, nil
}
