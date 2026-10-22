package main

import (
	"context"
	"encoding/json"
	"errors"
)

func attentionKind(kind string) bool {
	return kind == "favorite" || kind == "restock" || kind == "pricedrop"
}

func (s *Store) attachAttention(ctx context.Context, openid string, products []Product) error {
	rows, err := s.db.QueryContext(ctx, "SELECT a.product_id,a.kind,COALESCE(n.push_status,'NONE') FROM product_attention a LEFT JOIN restock_notifications n ON n.openid=a.openid AND n.product_id=a.product_id AND a.kind='restock' WHERE a.openid=?", openid)
	if err != nil {
		return err
	}
	defer rows.Close()
	flags := map[string]map[string]bool{}
	statuses := map[string]string{}
	for rows.Next() {
		var id, kind, status string
		if err = rows.Scan(&id, &kind, &status); err != nil {
			return err
		}
		if flags[id] == nil {
			flags[id] = map[string]bool{}
		}
		flags[id][kind] = true
		if kind == "restock" {
			statuses[id] = status
		}
	}
	for i := range products {
		products[i].Favorite = flags[products[i].ID]["favorite"]
		products[i].RestockReminder = flags[products[i].ID]["restock"]
		products[i].RestockPushStatus = statuses[products[i].ID]
		products[i].PriceDropReminder = flags[products[i].ID]["pricedrop"]
	}
	if err = rows.Err(); err != nil {
		return err
	}
	rows.Close()
	return s.attachPriceDrops(ctx, openid, products)
}

func (s *Store) setAttention(ctx context.Context, openid string, raw json.RawMessage) (any, error) {
	var d struct {
		ProductID   string `json:"productId"`
		SKUID       string `json:"skuId"`
		Kind        string `json:"kind"`
		Enabled     *bool  `json:"enabled"`
		TargetPrice *int   `json:"targetPrice"`
	}
	if err := decode(raw, &d); err != nil {
		return nil, err
	}
	if !attentionKind(d.Kind) || d.Enabled == nil || d.ProductID == "" {
		return nil, errors.New("关注参数无效")
	}
	if d.Kind == "pricedrop" {
		if d.TargetPrice != nil && (*d.TargetPrice <= 0 || *d.TargetPrice > 100000000) {
			return nil, errors.New("目标价格无效")
		}
		target := 0
		if d.TargetPrice != nil {
			target = *d.TargetPrice
		}
		return s.setPriceDrop(ctx, openid, d.ProductID, d.SKUID, *d.Enabled, target)
	}
	if *d.Enabled {
		p, err := s.product(ctx, d.ProductID, false)
		if err != nil {
			return nil, err
		}
		if p == nil {
			return nil, errors.New("商品不存在或已下架")
		}
		if d.Kind == "restock" {
			for _, sku := range p.SKUs {
				if sku.Stock > 0 && sku.LimitPerOrder > 0 {
					return nil, errors.New("商品已有货，可直接购买")
				}
			}
		}
		_, err = s.db.ExecContext(ctx, "INSERT INTO product_attention(openid,product_id,kind) VALUES(?,?,?) ON DUPLICATE KEY UPDATE product_id=VALUES(product_id)", openid, d.ProductID, d.Kind)
		return M{"enabled": true}, err
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	_, err = tx.ExecContext(ctx, "DELETE FROM product_attention WHERE openid=? AND product_id=? AND kind=?", openid, d.ProductID, d.Kind)
	if err != nil {
		return nil, err
	}
	if d.Kind == "restock" {
		_, err = tx.ExecContext(ctx, "UPDATE restock_notifications SET push_status='CANCELLED',push_error='' WHERE openid=? AND product_id=? AND push_status NOT IN ('SENDING','UNCERTAIN')", openid, d.ProductID)
		if err != nil {
			return nil, err
		}
	}
	return M{"enabled": false}, tx.Commit()
}

func (s *Store) attentionProducts(ctx context.Context, openid, kind string) ([]Product, error) {
	if !attentionKind(kind) {
		return nil, errors.New("关注类型无效")
	}
	rows, err := s.db.QueryContext(ctx, "SELECT product_id FROM product_attention WHERE openid=? AND kind=? ORDER BY created_at DESC,product_id", openid, kind)
	if err != nil {
		return nil, err
	}
	ids := []string{}
	for rows.Next() {
		var id string
		if err = rows.Scan(&id); err != nil {
			rows.Close()
			return nil, err
		}
		ids = append(ids, id)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return nil, err
	}
	products := []Product{}
	for _, id := range ids {
		p, err := s.product(ctx, id, false)
		if err != nil {
			return nil, err
		}
		if p != nil {
			products = append(products, *p)
		}
	}
	err = s.attachAttention(ctx, openid, products)
	return products, err
}
