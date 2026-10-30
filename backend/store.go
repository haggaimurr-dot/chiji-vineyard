package main

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"

	_ "github.com/go-sql-driver/mysql"
)

type Store struct{ db *sql.DB }

func openStore(dsn string) (*Store, error) {
	if dsn == "" {
		return nil, errors.New("MYSQL_DSN 未配置")
	}
	db, err := sql.Open("mysql", dsn)
	if err != nil {
		return nil, err
	}
	db.SetMaxOpenConns(20)
	db.SetMaxIdleConns(5)
	db.SetConnMaxLifetime(5 * time.Minute)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err = db.PingContext(ctx); err != nil {
		return nil, err
	}
	return &Store{db: db}, nil
}
func jsonValue(v any) string { b, _ := json.Marshal(v); return string(b) }
func defaultRule() ShippingRule {
	return ShippingRule{Freight: 1200, FreeShippingThreshold: 19900, BlockedProvinces: []string{"新疆维吾尔自治区", "西藏自治区", "青海省"}}
}
func (s *Store) shipping(ctx context.Context) (ShippingRule, error) {
	return readShipping(ctx, s.db)
}
func readShipping(ctx context.Context, q interface {
	QueryRowContext(context.Context, string, ...any) *sql.Row
}) (ShippingRule, error) {
	var raw []byte
	err := q.QueryRowContext(ctx, "SELECT payload FROM configs WHERE config_key='shipping'").Scan(&raw)
	if errors.Is(err, sql.ErrNoRows) {
		return defaultRule(), nil
	}
	if err != nil {
		return ShippingRule{}, err
	}
	var rule ShippingRule
	err = json.Unmarshal(raw, &rule)
	if rule.BlockedProvinces == nil {
		rule.BlockedProvinces = []string{}
	}
	return rule, err
}
func (s *Store) config(ctx context.Context, key string) (M, error) {
	var raw []byte
	err := s.db.QueryRowContext(ctx, "SELECT payload FROM configs WHERE config_key=?", key).Scan(&raw)
	if errors.Is(err, sql.ErrNoRows) {
		return M{}, nil
	}
	if err != nil {
		return nil, err
	}
	var m M
	err = json.Unmarshal(raw, &m)
	return m, err
}
func (s *Store) products(ctx context.Context, category string, includeDraft bool) ([]Product, error) {
	query := "SELECT payload FROM products WHERE 1=1"
	args := []any{}
	if !includeDraft {
		query += " AND status='PUBLISHED'"
	}
	if category != "" && category != "all" {
		query += " AND category=?"
		args = append(args, category)
	}
	query += " ORDER BY sort_order ASC LIMIT 100"
	rows, err := s.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Product{}
	for rows.Next() {
		var raw []byte
		var p Product
		if err = rows.Scan(&raw); err != nil {
			return nil, err
		}
		if err = json.Unmarshal(raw, &p); err != nil {
			return nil, err
		}
		out = append(out, p)
	}
	if err = rows.Err(); err != nil {
		return nil, err
	}
	for i := range out {
		skus, err := s.skus(ctx, out[i].ID)
		if err != nil {
			return nil, err
		}
		out[i].SKUs = skus
	}
	if err = attachProductTagNames(ctx, s.db, out); err != nil {
		return nil, err
	}
	return out, nil
}
func (s *Store) product(ctx context.Context, id string, includeDraft bool) (*Product, error) {
	var raw []byte
	query := "SELECT payload FROM products WHERE id=?"
	if !includeDraft {
		query += " AND status='PUBLISHED'"
	}
	err := s.db.QueryRowContext(ctx, query, id).Scan(&raw)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var p Product
	if err = json.Unmarshal(raw, &p); err != nil {
		return nil, err
	}
	p.SKUs, err = s.skus(ctx, id)
	if err != nil {
		return nil, err
	}
	holder := []Product{p}
	if err = attachProductTagNames(ctx, s.db, holder); err != nil {
		return nil, err
	}
	p = holder[0]
	return &p, nil
}
func (s *Store) skus(ctx context.Context, productID string) ([]SKU, error) {
	rows, err := s.db.QueryContext(ctx, "SELECT id,product_id,name,price,compare_at_price,stock,reserved,sold,limit_per_order,weight_label,active,sort_order FROM skus WHERE product_id=? AND active=1 ORDER BY sort_order", productID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []SKU{}
	for rows.Next() {
		var x SKU
		if err = rows.Scan(&x.ID, &x.ProductID, &x.Name, &x.Price, &x.CompareAtPrice, &x.Stock, &x.Reserved, &x.Sold, &x.LimitPerOrder, &x.WeightLabel, &x.Active, &x.SortOrder); err != nil {
			return nil, err
		}
		out = append(out, x)
	}
	return out, rows.Err()
}
func (s *Store) sku(ctx context.Context, q interface {
	QueryRowContext(context.Context, string, ...any) *sql.Row
}, skuID string, lock bool) (SKU, error) {
	var x SKU
	query := "SELECT id,product_id,name,price,compare_at_price,stock,reserved,sold,limit_per_order,weight_label,active,sort_order FROM skus WHERE id=?"
	if lock {
		query += " FOR UPDATE"
	}
	err := q.QueryRowContext(ctx, query, skuID).Scan(&x.ID, &x.ProductID, &x.Name, &x.Price, &x.CompareAtPrice, &x.Stock, &x.Reserved, &x.Sold, &x.LimitPerOrder, &x.WeightLabel, &x.Active, &x.SortOrder)
	return x, err
}
func (s *Store) articles(ctx context.Context, stage, variety string, includeDraft bool) ([]Article, error) {
	if err := s.publishDueArticles(ctx, time.Now().UTC()); err != nil {
		return nil, err
	}
	query := "SELECT a.payload,a.updated_at,a.status,COALESCE(r.views,0),COALESCE(r.readers,0) FROM articles a LEFT JOIN (SELECT article_id,COUNT(*) AS views,COUNT(DISTINCT reader_hash) AS readers FROM article_reads GROUP BY article_id) r ON r.article_id=a.id WHERE 1=1"
	args := []any{}
	if !includeDraft {
		query += " AND a.status='PUBLISHED'"
	}
	if stage != "" && stage != "全部" {
		query += " AND a.stage=?"
		args = append(args, stage)
	}
	if variety != "" && variety != "全部" {
		query += " AND a.variety=?"
		args = append(args, variety)
	}
	query += " ORDER BY a.published_at DESC,a.id LIMIT 200"
	rows, err := s.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Article{}
	for rows.Next() {
		var raw []byte
		var a Article
		var updated time.Time
		var status string
		var views, readers int
		if err = rows.Scan(&raw, &updated, &status, &views, &readers); err != nil {
			return nil, err
		}
		if err = json.Unmarshal(raw, &a); err != nil {
			return nil, err
		}
		a.Status, a.Version = status, effectiveArticleVersion(a)
		if a.UpdatedAt == "" {
			a.UpdatedAt = updated.UTC().Format(time.RFC3339Nano)
		}
		a.ReadCount, a.ReaderCount = views, readers
		if !includeDraft {
			clearArticleReview(&a)
		}
		out = append(out, a)
	}
	return out, rows.Err()
}
func (s *Store) article(ctx context.Context, id string, includeDraft bool) (*Article, error) {
	if err := s.publishDueArticles(ctx, time.Now().UTC()); err != nil {
		return nil, err
	}
	query := "SELECT payload,updated_at,status FROM articles WHERE id=?"
	if !includeDraft {
		query += " AND status='PUBLISHED'"
	}
	a, _, err := scanStoredArticle(s.db.QueryRowContext(ctx, query, id))
	if err != nil || a == nil {
		return a, err
	}
	a.Version = effectiveArticleVersion(*a)
	err = articleCounters(ctx, s.db, a)
	if !includeDraft {
		clearArticleReview(a)
	}
	return a, err
}
func (s *Store) address(ctx context.Context, openid string) (*Address, error) {
	list, err := s.addresses(ctx, openid)
	if err != nil {
		return nil, err
	}
	for i := range list {
		if list[i].IsDefault {
			return &list[i], nil
		}
	}
	if len(list) > 0 {
		return &list[0], nil
	}
	return nil, nil
}
func (s *Store) cart(ctx context.Context, openid string) ([]CartItem, error) {
	rows, err := s.db.QueryContext(ctx, "SELECT product_id,sku_id,quantity,selected FROM carts WHERE openid=? ORDER BY updated_at DESC LIMIT 100", openid)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []CartItem{}
	for rows.Next() {
		var x CartItem
		if err = rows.Scan(&x.ProductID, &x.SKUID, &x.Quantity, &x.Selected); err != nil {
			return nil, err
		}
		p, err := s.product(ctx, x.ProductID, false)
		if err != nil {
			return nil, err
		}
		x.Product = p
		if p != nil {
			for i := range p.SKUs {
				if p.SKUs[i].ID == x.SKUID {
					x.SKU = &p.SKUs[i]
					break
				}
			}
		}
		out = append(out, x)
	}
	return out, rows.Err()
}
func (s *Store) order(ctx context.Context, q interface {
	QueryRowContext(context.Context, string, ...any) *sql.Row
}, id string, lock bool) (*Order, error) {
	var raw []byte
	var o Order
	var orderStatus, paymentStatus, fulfillmentStatus, refundStatus string
	query := "SELECT order_no,openid,order_status,payment_status,fulfillment_status,refund_status,payload,payment_attempted,transaction_id,expires_at,created_at FROM orders WHERE id=?"
	if lock {
		query += " FOR UPDATE"
	}
	var created time.Time
	err := q.QueryRowContext(ctx, query, id).Scan(&o.OrderNo, &o.OpenID, &orderStatus, &paymentStatus, &fulfillmentStatus, &refundStatus, &raw, &o.PaymentAttempted, &o.TransactionID, &o.ExpiresAt, &created)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	if err = json.Unmarshal(raw, &o); err != nil {
		return nil, err
	}
	// Indexed columns are authoritative for filtering and state guards; legacy
	// payloads may still contain a status from before a transactional update.
	o.OrderStatus, o.PaymentStatus = orderStatus, paymentStatus
	o.FulfillmentStatus, o.RefundStatus = fulfillmentStatus, refundStatus
	var invoiceRaw []byte
	invoiceErr := q.QueryRowContext(ctx, "SELECT payload FROM invoice_requests WHERE order_id=?", id).Scan(&invoiceRaw)
	if invoiceErr == nil {
		var invoice Invoice
		if err = json.Unmarshal(invoiceRaw, &invoice); err != nil {
			return nil, err
		}
		o.Invoice = &invoice
	} else if !errors.Is(invoiceErr, sql.ErrNoRows) {
		return nil, invoiceErr
	}
	o.ID = id
	noticeErr := q.QueryRowContext(ctx, "SELECT push_status,push_error FROM shipping_notifications WHERE order_id=?", id).Scan(&o.ShippingNoticeStatus, &o.ShippingNoticeError)
	if noticeErr != nil && !errors.Is(noticeErr, sql.ErrNoRows) {
		return nil, noticeErr
	}
	o.CreatedAt = created.UTC().Format(time.RFC3339Nano)
	return &o, nil
}
func (s *Store) orderByNo(ctx context.Context, no string) (*Order, error) {
	var id string
	err := s.db.QueryRowContext(ctx, "SELECT id FROM orders WHERE order_no=?", no).Scan(&id)
	if err != nil {
		return nil, err
	}
	return s.order(ctx, s.db, id, false)
}
func (s *Store) orders(ctx context.Context, openid string, limit int) ([]Order, error) {
	return s.searchOrders(ctx, openid, OrderSearch{Limit: limit})
}
func (s *Store) searchOrders(ctx context.Context, openid string, search OrderSearch) ([]Order, error) {
	search, err := normalizeOrderSearch(search, false)
	if err != nil {
		return nil, err
	}
	return s.searchOrdersFrom(ctx, s.db, openid, search)
}

func (s *Store) searchOrdersFrom(ctx context.Context, q interface {
	QueryContext(context.Context, string, ...any) (*sql.Rows, error)
	QueryRowContext(context.Context, string, ...any) *sql.Row
}, openid string, search OrderSearch) ([]Order, error) {
	query, args := orderSearchQuery(openid, search)
	rows, err := q.QueryContext(ctx, query, args...)
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
	if err = rows.Err(); err != nil {
		rows.Close()
		return nil, err
	}
	rows.Close()
	out := []Order{}
	for _, id := range ids {
		o, err := s.order(ctx, q, id, false)
		if err != nil {
			return nil, err
		}
		if o != nil {
			out = append(out, *o)
		}
	}
	return out, nil
}
func (s *Store) refund(ctx context.Context, q interface {
	QueryRowContext(context.Context, string, ...any) *sql.Row
}, id string, lock bool) (*Refund, error) {
	var r Refund
	var created, updated time.Time
	query := "SELECT order_id,order_no,openid,amount,reason,note,status,stock_restored,provider_refund_id,reject_reason,reviewed_by,created_at,updated_at,(SELECT total FROM orders WHERE id=refunds.order_id),(SELECT fulfillment_status FROM orders WHERE id=refunds.order_id) FROM refunds WHERE id=?"
	if lock {
		query += " FOR UPDATE"
	}
	err := q.QueryRowContext(ctx, query, id).Scan(&r.OrderID, &r.OrderNo, &r.OpenID, &r.Amount, &r.Reason, &r.Note, &r.Status, &r.StockRestored, &r.ProviderRefundID, &r.RejectReason, &r.ReviewedBy, &created, &updated, &r.OrderTotal, &r.FulfillmentStatus)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	r.UpdatedAt = updated.UTC().Format(time.RFC3339Nano)
	r.ID = id
	r.CreatedAt = created.UTC().Format(time.RFC3339Nano)
	return &r, nil
}
func (s *Store) refunds(ctx context.Context) ([]Refund, error) {
	rows, err := s.db.QueryContext(ctx, "SELECT id FROM refunds ORDER BY created_at DESC LIMIT 200")
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
	rows.Close()
	out := []Refund{}
	for _, id := range ids {
		r, err := s.refund(ctx, s.db, id, false)
		if err != nil {
			return nil, err
		}
		r.Images, err = s.refundImages(ctx, r.ID)
		if err != nil {
			return nil, err
		}
		out = append(out, *r)
	}
	return out, nil
}
func (s *Store) audit(ctx context.Context, a Claims, action, target string, detail any) {
	_, _ = s.db.ExecContext(ctx, "INSERT INTO audit_logs(admin_id,username,action,target_id,detail) VALUES(?,?,?,?,?)", a.Subject, a.Username, action, target, jsonValue(detail))
}
func (s *Store) resolveItems(ctx context.Context, q interface {
	QueryRowContext(context.Context, string, ...any) *sql.Row
}, raw []CartItem, lock bool) ([]OrderItem, error) {
	if len(raw) == 0 || len(raw) > 30 {
		return nil, errors.New("商品清单无效")
	}
	merged := map[string]int{}
	for _, item := range raw {
		if item.SKUID == "" || item.Quantity < 1 {
			return nil, errors.New("商品数量无效")
		}
		merged[item.SKUID] += item.Quantity
	}
	ids := []string{}
	for id := range merged {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	out := []OrderItem{}
	for _, id := range ids {
		sku, err := s.sku(ctx, q, id, lock)
		if err != nil {
			return nil, errors.New("商品规格已下架")
		}
		if !sku.Active {
			return nil, errors.New("商品规格已下架")
		}
		var payload []byte
		var status string
		query := "SELECT payload,status FROM products WHERE id=?"
		if lock {
			query += " FOR UPDATE"
		}
		if err = q.QueryRowContext(ctx, query, sku.ProductID).Scan(&payload, &status); err != nil {
			return nil, err
		}
		if status != "PUBLISHED" {
			return nil, errors.New("商品已下架")
		}
		var p Product
		if err = json.Unmarshal(payload, &p); err != nil {
			return nil, err
		}
		qty := merged[id]
		if qty > sku.LimitPerOrder || qty > sku.Stock {
			return nil, fmt.Errorf("%s库存或限购数量不足", p.Name)
		}
		small := &Product{ID: p.ID, Name: p.Name, Cover: p.Cover, SaleMode: p.SaleMode, ExpectedShipDate: p.ExpectedShipDate}
		out = append(out, OrderItem{CartItem: CartItem{ProductID: p.ID, SKUID: sku.ID, Quantity: qty, Selected: true, Product: small, SKU: &sku}, Name: p.Name, Cover: p.Cover, SaleMode: p.SaleMode, ExpectedShipDate: p.ExpectedShipDate, SKUName: sku.Name, WeightLabel: sku.WeightLabel, UnitPrice: sku.Price})
	}
	return out, nil
}
func truncate(s string, n int) string {
	if len([]rune(s)) <= n {
		return s
	}
	return string([]rune(s)[:n])
}
func normalizeDate(s string) time.Time {
	for _, format := range []string{time.RFC3339Nano, "2006-01-02T15:04:05.000Z", "2006-01-02"} {
		if t, e := time.Parse(format, s); e == nil {
			return t
		}
	}
	return time.Now().UTC()
}
func normalizeID(s string) string { return strings.TrimSpace(s) }
