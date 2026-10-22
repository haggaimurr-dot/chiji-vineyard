package main

import (
	"context"
	"encoding/json"
	"errors"
	"github.com/go-sql-driver/mysql"
	"strings"
	"unicode/utf8"
)

type TraceStep struct {
	Title  string `json:"title"`
	Detail string `json:"detail"`
}
type Traceability struct {
	Orchard     string      `json:"orchard"`
	Batch       string      `json:"batch"`
	Code        string      `json:"code,omitempty"`
	HarvestedAt string      `json:"harvestedAt"`
	PackedAt    string      `json:"packedAt,omitempty"`
	Packing     string      `json:"packing"`
	Steps       []TraceStep `json:"steps"`
	Demo        bool        `json:"demo,omitempty"`
}
type GiftCustomization struct {
	Enabled         bool     `json:"enabled"`
	Description     string   `json:"description"`
	Options         []string `json:"options"`
	MinimumQuantity int      `json:"minimumQuantity"`
	LeadTime        string   `json:"leadTime"`
}
type Pairing struct {
	Title       string `json:"title"`
	Description string `json:"description"`
	ProductID   string `json:"productId,omitempty"`
}
type Review struct {
	Status        string   `json:"status,omitempty"`
	RejectReason  string   `json:"rejectReason,omitempty"`
	ModeratedAt   string   `json:"moderatedAt,omitempty"`
	ID            string   `json:"id"`
	ProductID     string   `json:"productId"`
	Rating        int      `json:"rating"`
	Content       string   `json:"content"`
	CreatedAt     string   `json:"createdAt"`
	Images        []string `json:"images"`
	AppendContent string   `json:"appendContent,omitempty"`
	AppendImages  []string `json:"appendImages,omitempty"`
	AppendAt      string   `json:"appendAt,omitempty"`
	Reply         string   `json:"reply,omitempty"`
	ReplyAt       string   `json:"replyAt,omitempty"`
}

type ReviewPageRequest struct {
	ProductID string `json:"productId"`
	Page      int    `json:"page"`
	PageSize  int    `json:"pageSize"`
}

func parseReviewPage(raw json.RawMessage) (ReviewPageRequest, error) {
	var d ReviewPageRequest
	if err := decode(raw, &d); err != nil {
		return d, err
	}
	if d.Page == 0 {
		d.Page = 1
	}
	if d.PageSize == 0 {
		d.PageSize = 20
	}
	if d.ProductID == "" || d.Page < 1 || d.Page > 1000000 || d.PageSize < 1 || d.PageSize > 100 {
		return d, errors.New("评价分页参数无效")
	}
	return d, nil
}

func (s *Server) productReviews(ctx context.Context, raw json.RawMessage) (any, error) {
	d, err := parseReviewPage(raw)
	if err != nil {
		return nil, err
	}
	id := d.ProductID
	p, err := s.store.product(ctx, id, false)
	if err != nil {
		return nil, err
	}
	if p == nil {
		return nil, errors.New("商品不存在或已下架")
	}
	var total int
	var average float64
	err = s.store.db.QueryRowContext(ctx, "SELECT COUNT(*),COALESCE(AVG(rating),0) FROM product_reviews WHERE product_id=? AND "+approvedReviewSQL, id).Scan(&total, &average)
	if err != nil {
		return nil, err
	}
	offset := (d.Page - 1) * d.PageSize
	rows, err := s.store.db.QueryContext(ctx, "SELECT id,rating,content,created_at FROM product_reviews WHERE product_id=? AND "+approvedReviewSQL+" ORDER BY created_at DESC,id DESC LIMIT ? OFFSET ?", id, d.PageSize, offset)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := []Review{}
	for rows.Next() {
		r := Review{ProductID: id}
		if err = rows.Scan(&r.ID, &r.Rating, &r.Content, &r.CreatedAt); err != nil {
			return nil, err
		}
		items = append(items, r)
	}
	if err = rows.Err(); err != nil {
		return nil, err
	}
	rows.Close()
	if err = s.enrichReviews(ctx, items); err != nil {
		return nil, err
	}
	return M{"total": total, "average": average, "items": items, "page": d.Page, "pageSize": d.PageSize, "hasMore": offset+len(items) < total}, rows.Err()
}

func reviewEligibility(o *Order, openid, productID string, rating int, content string) error {
	if rating < 1 || rating > 5 || strings.TrimSpace(content) == "" || utf8.RuneCountInString(content) > 500 {
		return errors.New("请填写 1–5 星评分和 1–500 字评价")
	}
	if o == nil || o.OpenID != openid {
		return errors.New("订单不存在")
	}
	if o.OrderStatus != "COMPLETED" || o.FulfillmentStatus != "RECEIVED" || o.PaymentStatus != "PAID" {
		return errors.New("确认收货后才能评价")
	}
	for _, item := range o.Items {
		if item.ProductID == productID {
			return nil
		}
	}
	return errors.New("该商品不在此订单中")
}

func (s *Server) createReview(ctx context.Context, openid string, raw json.RawMessage) (any, error) {
	var d struct {
		OrderID   string   `json:"orderId"`
		ProductID string   `json:"productId"`
		Rating    int      `json:"rating"`
		Content   string   `json:"content"`
		Images    []string `json:"images"`
	}
	if err := decode(raw, &d); err != nil {
		return nil, err
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
	d.Content = strings.TrimSpace(d.Content)
	if err = reviewEligibility(o, openid, d.ProductID, d.Rating, d.Content); err != nil {
		return nil, err
	}
	r := Review{ID: randomID("REV"), ProductID: d.ProductID, Rating: d.Rating, Content: d.Content, CreatedAt: nowString(), Status: "PENDING"}
	_, err = tx.ExecContext(ctx, "INSERT INTO product_reviews(id,order_id,product_id,openid,rating,content,created_at) VALUES(?,?,?,?,?,?,?)", r.ID, d.OrderID, r.ProductID, openid, r.Rating, r.Content, r.CreatedAt)
	var mysqlErr *mysql.MySQLError
	if errors.As(err, &mysqlErr) && mysqlErr.Number == 1062 {
		return nil, errors.New("此订单中的商品已评价")
	}
	if err != nil {
		return nil, err
	}
	r.Images, err = saveReviewImages(ctx, tx, d.Images)
	if err != nil {
		return nil, err
	}
	payload, _ := json.Marshal(r)
	if _, err = tx.ExecContext(ctx, "INSERT INTO review_details(review_id,payload) VALUES(?,?)", r.ID, payload); err != nil {
		return nil, err
	}
	return r, tx.Commit()
}
