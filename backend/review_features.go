package main

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"net/http"
	"os"
	"strings"
	"unicode/utf8"
)

// Legacy reviews without a status remain approved. Apply the same predicate to
// the public list and its totals so hidden reviews cannot affect the rating.
const reviewStatusSQL = "COALESCE((SELECT JSON_UNQUOTE(JSON_EXTRACT(payload,'$.status')) FROM review_details WHERE review_id=product_reviews.id),'APPROVED')"
const approvedReviewSQL = reviewStatusSQL + "='APPROVED'"

func validReviewStatus(status string) bool {
	return status == "PENDING" || status == "APPROVED" || status == "REJECTED"
}

func validateReviewText(text string) error {
	if strings.TrimSpace(text) == "" || utf8.RuneCountInString(text) > 500 {
		return errors.New("请填写1–500字内容")
	}
	return nil
}
func validateReviewImages(images []string) error {
	if len(images) > 6 {
		return errors.New("晒图最多6张")
	}
	for _, image := range images {
		mime, data, err := decodeDataURL(image)
		if err != nil || len(data) == 0 || len(data) > 512<<10 || http.DetectContentType(data) != mime {
			return errors.New("请上传真实图片，单张不超过512KB")
		}
	}
	return nil
}
func saveReviewImages(ctx context.Context, tx *sql.Tx, images []string) ([]string, error) {
	if err := validateReviewImages(images); err != nil {
		return nil, err
	}
	urls := []string{}
	base := strings.TrimRight(os.Getenv("PUBLIC_BASE_URL"), "/")
	if len(images) > 0 && !strings.HasPrefix(base, "https://") {
		return nil, errors.New("请配置HTTPS PUBLIC_BASE_URL后使用晒图")
	}
	for _, image := range images {
		mime, data, _ := decodeDataURL(image)
		id := randomID("REVIMG")
		if _, err := tx.ExecContext(ctx, "INSERT INTO media(id,mime,content) VALUES(?,?,?)", id, mime, data); err != nil {
			return nil, err
		}
		urls = append(urls, base+"/media/"+id)
	}
	return urls, nil
}
func (s *Server) enrichReviews(ctx context.Context, items []Review) error {
	if len(items) == 0 {
		return nil
	}
	marks := []string{}
	args := []any{}
	indexes := map[string]int{}
	for i, r := range items {
		marks = append(marks, "?")
		args = append(args, r.ID)
		indexes[r.ID] = i
		items[i].Images = []string{}
	}
	rows, err := s.store.db.QueryContext(ctx, "SELECT review_id,payload FROM review_details WHERE review_id IN ("+strings.Join(marks, ",")+")", args...)
	if err != nil {
		return err
	}
	defer rows.Close()
	for rows.Next() {
		var id string
		var payload []byte
		if err = rows.Scan(&id, &payload); err != nil {
			return err
		}
		var r Review
		if err = json.Unmarshal(payload, &r); err != nil {
			return err
		}
		items[indexes[id]] = r
	}
	return rows.Err()
}
func (s *Server) orderReviews(ctx context.Context, openid string, raw json.RawMessage) (any, error) {
	var d struct {
		OrderID string `json:"orderId"`
	}
	if err := decode(raw, &d); err != nil {
		return nil, err
	}
	rows, err := s.store.db.QueryContext(ctx, "SELECT id,product_id,rating,content,created_at FROM product_reviews WHERE order_id=? AND openid=? ORDER BY created_at,id", d.OrderID, openid)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := []Review{}
	for rows.Next() {
		var r Review
		if err = rows.Scan(&r.ID, &r.ProductID, &r.Rating, &r.Content, &r.CreatedAt); err != nil {
			return nil, err
		}
		items = append(items, r)
	}
	if err = rows.Err(); err != nil {
		return nil, err
	}
	rows.Close()
	err = s.enrichReviews(ctx, items)
	return items, err
}
func (s *Server) adminReviews(ctx context.Context, raw json.RawMessage) (any, error) {
	var d struct {
		Page   int    `json:"page"`
		Status string `json:"status"`
	}
	if err := decode(raw, &d); err != nil {
		return nil, err
	}
	if d.Page == 0 {
		d.Page = 1
	}
	if d.Page < 1 || d.Page > 1000000 {
		return nil, errors.New("分页参数无效")
	}
	var total int
	where := ""
	args := []any{}
	if d.Status != "" {
		if !validReviewStatus(d.Status) {
			return nil, errors.New("评价状态无效")
		}
		where = " WHERE " + reviewStatusSQL + "=?"
		args = append(args, d.Status)
	}
	if err := s.store.db.QueryRowContext(ctx, "SELECT COUNT(*) FROM product_reviews"+where, args...).Scan(&total); err != nil {
		return nil, err
	}
	args = append(args, (d.Page-1)*20)
	rows, err := s.store.db.QueryContext(ctx, "SELECT id,product_id,rating,content,created_at FROM product_reviews"+where+" ORDER BY created_at DESC,id DESC LIMIT 20 OFFSET ?", args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := []Review{}
	for rows.Next() {
		var r Review
		if err = rows.Scan(&r.ID, &r.ProductID, &r.Rating, &r.Content, &r.CreatedAt); err != nil {
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
	return M{"items": items, "total": total, "hasMore": d.Page*20 < total}, nil
}
func (s *Server) appendReview(ctx context.Context, openid string, raw json.RawMessage) (any, error) {
	return s.changeReview(ctx, openid, nil, raw)
}
func (s *Server) replyReview(ctx context.Context, actor Claims, raw json.RawMessage) (any, error) {
	var d struct {
		Status string `json:"status"`
	}
	if err := json.Unmarshal(raw, &d); err != nil {
		return nil, err
	}
	if d.Status != "" {
		return nil, errors.New("回复参数无效")
	}
	return s.changeReview(ctx, "", &actor, raw)
}
func (s *Server) moderateReview(ctx context.Context, actor Claims, raw json.RawMessage) (any, error) {
	var d struct {
		Status string `json:"status"`
	}
	if err := json.Unmarshal(raw, &d); err != nil {
		return nil, err
	}
	if d.Status != "APPROVED" && d.Status != "REJECTED" {
		return nil, errors.New("审核状态无效")
	}
	return s.changeReview(ctx, "", &actor, raw)
}
func (s *Server) changeReview(ctx context.Context, openid string, actor *Claims, raw json.RawMessage) (any, error) {
	var d struct {
		ID      string   `json:"id"`
		Content string   `json:"content"`
		Images  []string `json:"images"`
		Status  string   `json:"status"`
		Reason  string   `json:"reason"`
	}
	if err := decode(raw, &d); err != nil {
		return nil, err
	}
	d.Content = strings.TrimSpace(d.Content)
	d.Reason = strings.TrimSpace(d.Reason)
	if d.Status != "" {
		if actor == nil || (d.Status != "APPROVED" && d.Status != "REJECTED") {
			return nil, errors.New("审核状态无效")
		}
		if d.Status == "REJECTED" && (d.Reason == "" || utf8.RuneCountInString(d.Reason) > 500) {
			return nil, errors.New("请填写1–500字驳回原因")
		}
	} else {
		if err := validateReviewText(d.Content); err != nil {
			return nil, err
		}
	}
	tx, err := s.store.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	var r Review
	var owner string
	err = tx.QueryRowContext(ctx, "SELECT id,product_id,openid,rating,content,created_at FROM product_reviews WHERE id=? FOR UPDATE", d.ID).Scan(&r.ID, &r.ProductID, &owner, &r.Rating, &r.Content, &r.CreatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, errors.New("评价不存在")
	}
	if err != nil {
		return nil, err
	}
	if actor == nil && owner != openid {
		return nil, errors.New("评价不存在")
	}
	var payload []byte
	err = tx.QueryRowContext(ctx, "SELECT payload FROM review_details WHERE review_id=?", r.ID).Scan(&payload)
	if err == nil {
		if err = json.Unmarshal(payload, &r); err != nil {
			return nil, err
		}
	} else if !errors.Is(err, sql.ErrNoRows) {
		return nil, err
	}
	if d.Status != "" {
		if r.Status == d.Status {
			return nil, errors.New("评价已处于该审核状态")
		}
		r.Status = d.Status
		r.RejectReason = ""
		if d.Status == "REJECTED" {
			r.RejectReason = d.Reason
		}
		r.ModeratedAt = nowString()
	} else if actor == nil {
		if r.AppendAt != "" {
			return nil, errors.New("每条评价仅可追评一次")
		}
		r.AppendImages, err = saveReviewImages(ctx, tx, d.Images)
		if err != nil {
			return nil, err
		}
		r.AppendContent = d.Content
		r.AppendAt = nowString()
		r.Status = "PENDING"
		r.RejectReason = ""
		r.ModeratedAt = ""
	} else {
		if r.ReplyAt != "" {
			return nil, errors.New("该评价已回复")
		}
		r.Reply = d.Content
		r.ReplyAt = nowString()
	}
	payload, _ = json.Marshal(r)
	if _, err = tx.ExecContext(ctx, "INSERT INTO review_details(review_id,payload) VALUES(?,?) ON DUPLICATE KEY UPDATE payload=VALUES(payload)", r.ID, payload); err != nil {
		return nil, err
	}
	if err = tx.Commit(); err != nil {
		return nil, err
	}
	if actor != nil {
		if d.Status != "" {
			s.store.audit(ctx, *actor, "REVIEW_MODERATE", r.ID, M{"status": d.Status, "reason": r.RejectReason})
		} else {
			s.store.audit(ctx, *actor, "REVIEW_REPLY", r.ID, M{"content": d.Content})
		}
	}
	return r, nil
}
