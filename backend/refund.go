package main

import (
	"context"
	"database/sql"
	"errors"
	"net/url"
	"os"
	"time"
)

func (w *Wechat) queryRefund(id string) (M, error) {
	return w.request("GET", "/v3/refund/domestic/refunds/"+url.PathEscape(id), nil)
}

func (s *Server) userRefunds(ctx context.Context, openid, orderID string, refresh bool) ([]Refund, error) {
	o, err := s.store.order(ctx, s.store.db, orderID, false)
	if err != nil {
		return nil, err
	}
	if o == nil || o.OpenID != openid {
		return nil, errors.New("订单不存在")
	}
	rows, err := s.store.db.QueryContext(ctx, "SELECT id FROM refunds WHERE order_id=? AND openid=? ORDER BY created_at DESC,id DESC", orderID, openid)
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
	result := []Refund{}
	for i, id := range ids {
		rf, e := s.store.refund(ctx, s.store.db, id, false)
		if e != nil {
			return nil, e
		}
		if rf == nil {
			continue
		}
		if refresh && i == 0 && (rf.Status == "APPROVED" || rf.Status == "FAILED") {
			if _, e = s.syncRefund(ctx, id); e != nil {
				return nil, e
			}
			rf, e = s.store.refund(ctx, s.store.db, id, false)
			if e != nil {
				return nil, e
			}
		}
		rf.Images, e = s.store.refundImages(ctx, rf.ID)
		if e != nil {
			return nil, e
		}
		result = append(result, *rf)
	}
	return result, nil
}

// Validate the nominal refund amount, not payer_refund (which excludes payment coupons).
func validateRefundResult(r Refund, p M, callback bool) error {
	amount, ok := p["amount"].(map[string]any)
	if !ok {
		return errors.New("退款金额缺失")
	}
	refund, ok := amount["refund"].(float64)
	if !ok || refund != float64(r.Amount) || p["out_refund_no"] != r.ID || p["out_trade_no"] != r.OrderNo {
		return errors.New("退款单号、订单或金额不匹配")
	}
	if callback && (os.Getenv("WECHAT_MCH_ID") == "" || p["mchid"] != os.Getenv("WECHAT_MCH_ID")) {
		return errors.New("退款商户信息不匹配")
	}
	if id, ok := p["refund_id"].(string); !ok || id == "" {
		return errors.New("微信退款单号缺失")
	}
	return nil
}

func (s *Server) syncRefund(ctx context.Context, id string) (any, error) {
	rf, err := s.store.refund(ctx, s.store.db, id, false)
	if err != nil {
		return nil, err
	}
	if rf == nil {
		return nil, errors.New("退款申请不存在")
	}
	if rf.Status != "APPROVED" && rf.Status != "FAILED" {
		return rf, nil
	}
	p, err := s.wechat.queryRefund(id)
	if err != nil {
		return nil, err
	}
	if err = validateRefundResult(*rf, p, false); err != nil {
		return nil, err
	}
	status, _ := p["status"].(string)
	providerID, _ := p["refund_id"].(string)
	if err = s.recordRefundResult(ctx, id, status, providerID, p); err != nil {
		return nil, err
	}
	return s.store.refund(ctx, s.store.db, id, false)
}

func (s *Server) recordRefundResult(ctx context.Context, id, status, providerID string, provider ...M) error {
	tx, err := s.store.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	rf, err := s.store.refund(ctx, tx, id, true)
	if err != nil {
		return err
	}
	if rf == nil {
		return errors.New("退款申请不存在")
	}
	o, err := s.store.order(ctx, tx, rf.OrderID, true)
	if err != nil {
		return err
	}
	if o == nil {
		return errors.New("订单不存在")
	}
	if err = persistRefundResult(ctx, tx, rf, o, status, providerID); err != nil {
		return err
	}
	if len(provider) > 0 {
		if value, ok := provider[0]["create_time"].(string); ok && value != "" {
			accepted, e := time.Parse(time.RFC3339, value)
			if e != nil {
				return errors.New("退款受理时间无效")
			}
			if _, e = tx.ExecContext(ctx, "UPDATE refunds SET accepted_at=COALESCE(accepted_at,?) WHERE id=?", accepted.UTC(), id); e != nil {
				return e
			}
		}
	}
	return tx.Commit()
}

func persistRefundResult(ctx context.Context, tx *sql.Tx, rf *Refund, o *Order, status, providerID string) error {
	if rf.StockRestored || rf.Status == "SUCCEEDED" {
		return nil
	}
	if rf.Status != "APPROVED" && rf.Status != "FAILED" {
		return errors.New("退款未审批，不能更新资金状态")
	}
	// Out-of-order processing responses must not hide an abnormal/closed result.
	if rf.Status == "FAILED" && status == "PROCESSING" {
		return nil
	}
	switch status {
	case "SUCCESS":
		updated, restore, err := refundResult(*o, rf.Amount)
		if err != nil {
			return err
		}
		*o = updated
		if o.PaymentStatus == "REFUNDED" {
			for itemIndex, item := range o.Items {
				stock := 0
				if restore {
					stock = item.Quantity
					for _, p := range o.Packages {
						if p.Shipment != nil {
							for _, allocated := range p.Items {
								if allocated.ItemIndex == itemIndex {
									stock -= allocated.Quantity
								}
							}
						}
					}
				}
				if _, err = applyInventory(ctx, tx, item.SKUID, "REFUND", o.OrderNo, "系统", "退款完成："+rf.ID, "UPDATE skus SET stock=stock+?,sold=GREATEST(0,sold-?) WHERE id=?", stock, item.Quantity, item.SKUID); err != nil {
					return err
				}
			}
		}
		rf.Status = "SUCCEEDED"
		rf.StockRestored = true
	case "PROCESSING":
		rf.Status = "APPROVED"
		o.PaymentStatus = "REFUNDING"
	case "CLOSED", "ABNORMAL":
		rf.Status = "FAILED"
		o.PaymentStatus = "REFUNDING"
	default:
		return errors.New("未知微信退款状态")
	}
	if status != "SUCCESS" {
		o.RefundStatus = rf.Status
	}
	appendOrderEvent(o, "REFUND_"+rf.Status, "退款状态更新："+rf.Status, "系统", "")
	if _, err := tx.ExecContext(ctx, "UPDATE orders SET refund_status=?,payment_status=?,order_status=?,payload=? WHERE id=?", o.RefundStatus, o.PaymentStatus, o.OrderStatus, jsonValue(o), o.ID); err != nil {
		return err
	}
	_, err := tx.ExecContext(ctx, "UPDATE refunds SET status=?,stock_restored=?,provider_refund_id=? WHERE id=?", rf.Status, rf.StockRestored, providerID, rf.ID)
	return err
}
