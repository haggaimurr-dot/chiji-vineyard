package main

import (
	"context"
	"database/sql"
	"encoding/base64"
	"encoding/json"
	"errors"
	"net/http"
)

// Evidence is private: it is never exposed through the public /media route.
func (s *Server) uploadRefundEvidence(ctx context.Context, openid string, raw json.RawMessage) (any, error) {
	var d struct {
		OrderID string `json:"orderId"`
		DataURL string `json:"dataUrl"`
	}
	if err := decode(raw, &d); err != nil {
		return nil, err
	}
	mime, content, err := decodeDataURL(d.DataURL)
	if err != nil {
		return nil, err
	}
	if len(content) == 0 || len(content) > 512<<10 || http.DetectContentType(content) != mime {
		return nil, errors.New("请上传真实图片，单张不超过512KB")
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
	if o == nil || o.OpenID != openid || !canRefund(*o) {
		return nil, errors.New("当前订单不能上传售后举证")
	}
	var count int
	if _, err = tx.ExecContext(ctx, "DELETE FROM refund_evidence WHERE order_id=? AND refund_id='' AND created_at<DATE_SUB(NOW(3),INTERVAL 1 DAY)", o.ID); err != nil {
		return nil, err
	}
	if err = tx.QueryRowContext(ctx, "SELECT COUNT(*) FROM refund_evidence WHERE order_id=? AND refund_id=''", o.ID).Scan(&count); err != nil {
		return nil, err
	}
	if count >= 30 {
		return nil, errors.New("未提交的图片过多，请先提交售后申请")
	}
	id := randomID("EVI")
	_, err = tx.ExecContext(ctx, "INSERT INTO refund_evidence(id,order_id,openid,mime,content) VALUES(?,?,?,?,?)", id, o.ID, openid, mime, content)
	if err != nil {
		return nil, err
	}
	return M{"id": id}, tx.Commit()
}

func (s *Server) deleteRefundEvidence(ctx context.Context, openid string, raw json.RawMessage) (any, error) {
	_, err := s.store.db.ExecContext(ctx, "DELETE FROM refund_evidence WHERE id=? AND openid=? AND refund_id=''", mustID(raw, "id"), openid)
	return M{"deleted": err == nil}, err
}

func (s *Store) refundImages(ctx context.Context, id string) ([]string, error) {
	rows, err := s.db.QueryContext(ctx, "SELECT id FROM refund_evidence WHERE refund_id=? ORDER BY created_at,id", id)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	ids := []string{}
	for rows.Next() {
		var imageID string
		if err = rows.Scan(&imageID); err != nil {
			return nil, err
		}
		ids = append(ids, imageID)
	}
	return ids, rows.Err()
}

func (s *Server) readRefundEvidence(ctx context.Context, openid string, admin bool, raw json.RawMessage) (any, error) {
	id := mustID(raw, "id")
	var owner, mime string
	var content []byte
	err := s.store.db.QueryRowContext(ctx, "SELECT openid,mime,content FROM refund_evidence WHERE id=?", id).Scan(&owner, &mime, &content)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, errors.New("举证图片不存在")
	}
	if err != nil {
		return nil, err
	}
	if !admin && owner != openid {
		return nil, errors.New("举证图片不存在")
	}
	return M{"dataUrl": "data:" + mime + ";base64," + base64.StdEncoding.EncodeToString(content)}, nil
}

func validateRefund(o Order, amount int, reason string, images []string) error {
	if !canRefund(o) {
		return errors.New("当前订单不能申请退款")
	}
	if amount <= 0 || amount > o.Quote.Total-o.RefundedAmount {
		return errors.New("退款金额须大于0且不超过剩余可退金额")
	}
	if clean(reason, 80) == "" {
		return errors.New("请选择售后原因")
	}
	if len(images) > 6 {
		return errors.New("举证图片最多6张")
	}
	if (o.FulfillmentStatus == "SHIPPED" || o.FulfillmentStatus == "RECEIVED") && len(images) == 0 {
		return errors.New("已发货售后请上传举证图片")
	}
	seen := map[string]bool{}
	for _, id := range images {
		if id == "" || seen[id] {
			return errors.New("举证图片无效或重复")
		}
		seen[id] = true
	}
	return nil
}

// A monetary partial refund does not identify returned units. Restore inventory
// only when the entire payment has been refunded before shipment.
func refundResult(o Order, amount int) (Order, bool, error) {
	if amount <= 0 || amount > o.Quote.Total-o.RefundedAmount {
		return o, false, errors.New("退款累计金额异常")
	}
	o.RefundedAmount += amount
	full := o.RefundedAmount == o.Quote.Total
	restore := full && (o.FulfillmentStatus == "UNFULFILLED" || o.FulfillmentStatus == "PREPARING")
	o.RefundStatus = "PARTIAL"
	o.PaymentStatus = "PAID"
	if full {
		o.PaymentStatus = "REFUNDED"
		o.RefundStatus = "SUCCEEDED"
		if restore {
			o.OrderStatus = "CLOSED"
		}
	}
	return o, restore, nil
}
