package main

import (
	"context"
	"database/sql"
	"encoding/json"
)

// 内容与商品互相引流：文章关联商品（article.productIds），两端互相展示入口。
type linkedProduct struct {
	ID    string `json:"id"`
	Name  string `json:"name"`
	Cover string `json:"cover"`
	Price int    `json:"price"`
}
type linkedArticle struct {
	ID    string `json:"id"`
	Title string `json:"title"`
	Cover string `json:"cover"`
}

func scanLinkedProducts(rows *sql.Rows) ([]linkedProduct, error) {
	items := []linkedProduct{}
	for rows.Next() {
		var p linkedProduct
		var payload []byte
		if err := rows.Scan(&p.ID, &payload); err != nil {
			return nil, err
		}
		var full struct {
			Name  string `json:"name"`
			Cover string `json:"cover"`
			Price int    `json:"price"`
		}
		_ = json.Unmarshal(payload, &full)
		p.Name, p.Cover, p.Price = full.Name, full.Cover, full.Price
		items = append(items, p)
	}
	return items, rows.Err()
}

// 文章详情附带关联商品（仅上架商品）。
func (s *Store) articleLinkedProducts(ctx context.Context, ids []string) ([]linkedProduct, error) {
	if len(ids) == 0 {
		return []linkedProduct{}, nil
	}
	place := ""
	args := []any{}
	for i, id := range ids {
		if i > 0 {
			place += ","
		}
		place += "?"
		args = append(args, id)
	}
	rows, err := s.db.QueryContext(ctx, "SELECT id,payload FROM products WHERE id IN ("+place+") AND status='PUBLISHED'", args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return scanLinkedProducts(rows)
}

// 商品详情附带关联文章（仅已发布文章）。
func (s *Store) productLinkedArticles(ctx context.Context, productID string) ([]linkedArticle, error) {
	rows, err := s.db.QueryContext(ctx, "SELECT id,payload FROM articles WHERE status='PUBLISHED' AND JSON_CONTAINS(JSON_EXTRACT(payload,'$.productIds'), JSON_QUOTE(?)) ORDER BY published_at DESC LIMIT 10", productID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := []linkedArticle{}
	for rows.Next() {
		var a linkedArticle
		var payload []byte
		if err = rows.Scan(&a.ID, &payload); err != nil {
			return nil, err
		}
		var full struct {
			Title string `json:"title"`
			Cover string `json:"cover"`
		}
		_ = json.Unmarshal(payload, &full)
		a.Title, a.Cover = full.Title, full.Cover
		items = append(items, a)
	}
	return items, rows.Err()
}
