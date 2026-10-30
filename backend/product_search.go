package main

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"strings"
)

type ProductSearch struct {
	SaleMode string `json:"saleMode"`
	Stock    string `json:"stock"`
	MinPrice *int64 `json:"minPrice"`
	MaxPrice *int64 `json:"maxPrice"`
	Category string `json:"category"`
	Keyword  string `json:"keyword"`
	Sort     string `json:"sort"`
	Offset   int    `json:"offset"`
	Limit    int    `json:"limit"`
}
type ProductSearchResult struct {
	Items  []Product `json:"items"`
	Total  int       `json:"total"`
	Offset int       `json:"offset"`
	Limit  int       `json:"limit"`
}

func normalizeProductSearch(d ProductSearch) (ProductSearch, error) {
	if d.SaleMode != "" && d.SaleMode != "all" && d.SaleMode != "spot" && d.SaleMode != "presale" {
		return d, errors.New("发货方式无效")
	}
	if d.Stock != "" && d.Stock != "all" && d.Stock != "available" && d.Stock != "soldOut" {
		return d, errors.New("库存筛选无效")
	}
	if (d.MinPrice != nil && (*d.MinPrice < 0 || *d.MinPrice > 100000000)) || (d.MaxPrice != nil && (*d.MaxPrice < 0 || *d.MaxPrice > 100000000)) || (d.MinPrice != nil && d.MaxPrice != nil && *d.MinPrice > *d.MaxPrice) {
		return d, errors.New("价格区间无效")
	}
	d.Category = strings.TrimSpace(d.Category)
	if d.Category == "" {
		d.Category = "all"
	}
	if d.Category != "all" && d.Category != "family" && d.Category != "gift" {
		return d, errors.New("商品分类无效")
	}
	if d.Sort == "" {
		d.Sort = "default"
	}
	if d.Sort != "default" && d.Sort != "priceAsc" && d.Sort != "priceDesc" && d.Sort != "sales" {
		return d, errors.New("商品排序无效")
	}
	d.Keyword = strings.TrimSpace(d.Keyword)
	if len([]rune(d.Keyword)) > 60 {
		return d, errors.New("搜索关键词最多60字")
	}
	if d.Offset < 0 {
		d.Offset = 0
	}
	if d.Limit <= 0 {
		d.Limit = 20
	}
	if d.Limit > 50 {
		d.Limit = 50
	}
	return d, nil
}

func productSearchWhere(d ProductSearch) (string, []any) {
	where := "p.status='PUBLISHED'"
	args := []any{}
	if d.SaleMode != "" && d.SaleMode != "all" {
		where += " AND JSON_UNQUOTE(JSON_EXTRACT(p.payload,'$.saleMode'))=?"
		args = append(args, d.SaleMode)
	}
	if d.Stock == "available" || d.Stock == "soldOut" {
		where += " AND "
		if d.Stock == "soldOut" {
			where += "NOT "
		}
		where += "EXISTS (SELECT 1 FROM skus stock_filter WHERE stock_filter.product_id=p.id AND stock_filter.active=1 AND stock_filter.stock>0 AND stock_filter.limit_per_order>0)"
	}
	if d.MinPrice != nil {
		where += " AND (SELECT MIN(price) FROM skus price_filter WHERE price_filter.product_id=p.id AND price_filter.active=1)>=?"
		args = append(args, *d.MinPrice)
	}
	if d.MaxPrice != nil {
		where += " AND (SELECT MIN(price) FROM skus price_filter WHERE price_filter.product_id=p.id AND price_filter.active=1)<=?"
		args = append(args, *d.MaxPrice)
	}
	if d.Category != "all" {
		where += " AND p.category=?"
		args = append(args, d.Category)
	}
	for _, word := range strings.Fields(strings.ToLower(d.Keyword)) {
		where += ` AND (LOCATE(?,LOWER(CONCAT_WS(' ',JSON_UNQUOTE(JSON_EXTRACT(p.payload,'$.name')),JSON_UNQUOTE(JSON_EXTRACT(p.payload,'$.subtitle')),JSON_UNQUOTE(JSON_EXTRACT(p.payload,'$.origin')),JSON_UNQUOTE(JSON_EXTRACT(p.payload,'$.badges')))))>0 OR EXISTS (SELECT 1 FROM skus k WHERE k.product_id=p.id AND k.active=1 AND LOCATE(?,LOWER(CONCAT_WS(' ',k.name,k.weight_label)))>0))`
		args = append(args, word, word)
	}
	return where, args
}

func productSearchOrder(sort string) string {
	switch sort {
	case "priceAsc":
		return "min_price IS NULL ASC,min_price ASC,p.sort_order ASC,p.id ASC"
	case "priceDesc":
		return "min_price IS NULL ASC,min_price DESC,p.sort_order ASC,p.id ASC"
	case "sales":
		return "sales DESC,p.sort_order ASC,p.id ASC"
	default:
		return "p.sort_order ASC,p.id ASC"
	}
}

// Filtering and ranking happen before LIMIT; only the current page is hydrated.
func (s *Store) searchProducts(ctx context.Context, openid string, d ProductSearch) (ProductSearchResult, error) {
	d, err := normalizeProductSearch(d)
	out := ProductSearchResult{Items: []Product{}, Offset: d.Offset, Limit: d.Limit}
	if err != nil {
		return out, err
	}
	tx, err := s.db.BeginTx(ctx, &sql.TxOptions{ReadOnly: true, Isolation: sql.LevelRepeatableRead})
	if err != nil {
		return out, err
	}
	defer tx.Rollback()
	where, args := productSearchWhere(d)
	if err = tx.QueryRowContext(ctx, "SELECT COUNT(*) FROM products p WHERE "+where, args...).Scan(&out.Total); err != nil {
		return out, err
	}
	query := "SELECT p.payload FROM products p"
	if d.Sort != "default" {
		query += " LEFT JOIN (SELECT product_id,MIN(price) AS min_price,SUM(GREATEST(sold,0)) AS sales FROM skus WHERE active=1 GROUP BY product_id) metrics ON metrics.product_id=p.id"
	}
	query += " WHERE " + where + " ORDER BY " + productSearchOrder(d.Sort) + " LIMIT ? OFFSET ?"
	pageArgs := append(append([]any{}, args...), d.Limit, d.Offset)
	rows, err := tx.QueryContext(ctx, query, pageArgs...)
	if err != nil {
		return out, err
	}
	for rows.Next() {
		var raw []byte
		var p Product
		if err = rows.Scan(&raw); err == nil {
			err = json.Unmarshal(raw, &p)
		}
		if err != nil {
			rows.Close()
			return out, err
		}
		// The list needs card information only; details remain on product.detail.
		p.Description = ""
		p.Gallery = []string{}
		p.Highlights = []string{}
		p.Traceability = nil
		p.Pairings = nil
		p.SKUs = []SKU{}
		p.Favorite = false
		p.RestockReminder = false
		out.Items = append(out.Items, p)
	}
	err = rows.Err()
	rows.Close()
	if err == nil {
		err = attachProductTagNames(ctx, s.db, out.Items)
	}
	if err != nil || len(out.Items) == 0 {
		if err == nil {
			err = tx.Commit()
		}
		return out, err
	}
	placeholders := make([]string, len(out.Items))
	ids := make([]any, len(out.Items))
	positions := map[string]int{}
	for i, p := range out.Items {
		placeholders[i] = "?"
		ids[i] = p.ID
		positions[p.ID] = i
	}
	in := strings.Join(placeholders, ",")
	rows, err = tx.QueryContext(ctx, "SELECT id,product_id,name,price,compare_at_price,stock,reserved,sold,limit_per_order,weight_label,active,sort_order FROM skus WHERE active=1 AND product_id IN ("+in+") ORDER BY product_id,sort_order,id", ids...)
	if err != nil {
		return out, err
	}
	for rows.Next() {
		var k SKU
		if err = rows.Scan(&k.ID, &k.ProductID, &k.Name, &k.Price, &k.CompareAtPrice, &k.Stock, &k.Reserved, &k.Sold, &k.LimitPerOrder, &k.WeightLabel, &k.Active, &k.SortOrder); err != nil {
			rows.Close()
			return out, err
		}
		if i, ok := positions[k.ProductID]; ok {
			out.Items[i].SKUs = append(out.Items[i].SKUs, k)
		}
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return out, err
	}
	rows, err = tx.QueryContext(ctx, "SELECT product_id,kind FROM product_attention WHERE openid=? AND product_id IN ("+in+")", append([]any{openid}, ids...)...)
	if err != nil {
		return out, err
	}
	defer rows.Close()
	for rows.Next() {
		var id, kind string
		if err = rows.Scan(&id, &kind); err != nil {
			return out, err
		}
		if i, ok := positions[id]; ok {
			if kind == "favorite" {
				out.Items[i].Favorite = true
			}
			if kind == "restock" {
				out.Items[i].RestockReminder = true
			}
			if kind == "pricedrop" {
				out.Items[i].PriceDropReminder = true
			}
		}
	}
	err = rows.Err()
	rows.Close()
	if err == nil {
		err = tx.Commit()
	}
	return out, err
}
