package main

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"sort"
	"strings"
	"unicode/utf8"
)

type cartBatchRequest struct {
	Operation string   `json:"operation"`
	SKUIDs    []string `json:"skuIds"`
	Selected  *bool    `json:"selected"`
}

func (d cartBatchRequest) validate() error {
	if d.Operation != "delete" && d.Operation != "favorite" && d.Operation != "select" {
		return errors.New("购物袋批量操作类型无效")
	}
	if len(d.SKUIDs) == 0 || len(d.SKUIDs) > 100 {
		return errors.New("每次请选择1–100项购物袋商品")
	}
	if d.Operation == "select" && d.Selected == nil {
		return errors.New("请指定购物袋勾选状态")
	}
	seen := map[string]bool{}
	for _, id := range d.SKUIDs {
		if strings.TrimSpace(id) == "" || utf8.RuneCountInString(id) > 80 || seen[id] {
			return errors.New("购物袋规格编号无效或重复")
		}
		seen[id] = true
	}
	return nil
}

func (s *Store) batchCart(ctx context.Context, openid string, raw json.RawMessage) ([]CartItem, error) {
	var d cartBatchRequest
	if err := decode(raw, &d); err != nil {
		return nil, err
	}
	if err := d.validate(); err != nil {
		return nil, err
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	// Use a stable lock order and derive product IDs from the user's own cart.
	sort.Strings(d.SKUIDs)
	products := map[string]bool{}
	for _, id := range d.SKUIDs {
		var productID string
		err = tx.QueryRowContext(ctx, "SELECT product_id FROM carts WHERE openid=? AND sku_id=? FOR UPDATE", openid, id).Scan(&productID)
		if errors.Is(err, sql.ErrNoRows) {
			continue // Repeating an already completed request is safe.
		}
		if err != nil {
			return nil, err
		}
		if d.Operation == "favorite" {
			products[productID] = true
		}
	}
	productIDs := make([]string, 0, len(products))
	for id := range products {
		productIDs = append(productIDs, id)
	}
	sort.Strings(productIDs)
	for _, id := range productIDs {
		var status string
		err = tx.QueryRowContext(ctx, "SELECT status FROM products WHERE id=? FOR UPDATE", id).Scan(&status)
		if errors.Is(err, sql.ErrNoRows) || (err == nil && status != "PUBLISHED") {
			return nil, errors.New("商品不存在或已下架，未移入收藏")
		}
		if err != nil {
			return nil, err
		}
		if _, err = tx.ExecContext(ctx, "INSERT INTO product_attention(openid,product_id,kind) VALUES(?,?,'favorite') ON DUPLICATE KEY UPDATE product_id=VALUES(product_id)", openid, id); err != nil {
			return nil, err
		}
	}
	for _, id := range d.SKUIDs {
		if d.Operation == "select" {
			_, err = tx.ExecContext(ctx, "UPDATE carts SET selected=? WHERE openid=? AND sku_id=?", *d.Selected, openid, id)
		} else {
			_, err = tx.ExecContext(ctx, "DELETE FROM carts WHERE openid=? AND sku_id=?", openid, id)
		}
		if err != nil {
			return nil, err
		}
	}
	if err = tx.Commit(); err != nil {
		return nil, err
	}
	return s.cart(ctx, openid)
}
