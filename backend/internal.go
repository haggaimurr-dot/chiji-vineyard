package main

import (
	"errors"
	"net/http"
	"os"
	"time"
)

func (s *Server) internal(r *http.Request, req Request) (any, error) {
	if os.Getenv("INTERNAL_JOB_SECRET") == "" || r.Header.Get("X-Job-Secret") != os.Getenv("INTERNAL_JOB_SECRET") {
		return nil, errors.New("内部任务鉴权失败")
	}
	if req.Action == "internal.autoInvoices" {
		return M{"processed": true}, s.processAutoInvoices(r.Context())
	}
	if req.Action == "internal.paymentFinanceReconcile" {
		var d struct{ BillDate, Kind string }
		if err := decode(req.Data, &d); err != nil {
			return nil, err
		}
		return s.reconcileFinance(r.Context(), d.BillDate, d.Kind)
	}
	if req.Action == "internal.paymentReconcile" {
		return s.reconcilePayments(r.Context(), mustID(req.Data, "billDate"))
	}
	if req.Action == "internal.restockNotices" {
		return s.sendRestockNotices(r.Context())
	}
	if req.Action == "internal.activityNotices" {
		return s.sendActivityPushes(r.Context())
	}
	if req.Action == "internal.priceDropNotices" {
		return s.sendPriceDropNotices(r.Context())
	}
	if req.Action != "internal.expireOrders" {
		return nil, errors.New("未知内部任务")
	}
	ctx := r.Context()
	rows, err := s.store.db.QueryContext(ctx, "SELECT id FROM orders WHERE order_status='PENDING_PAYMENT' AND payment_status='UNPAID' AND expires_at<NOW(3) ORDER BY expires_at LIMIT 100")
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
	result := M{"scanned": len(ids), "paid": 0, "closed": 0, "failed": 0}
	for _, id := range ids {
		o, e := s.store.order(ctx, s.store.db, id, false)
		if e != nil || o == nil {
			result["failed"] = result["failed"].(int) + 1
			continue
		}
		if o.PaymentAttempted {
			payment, e := s.queryPayment(ctx, o)
			if e != nil {
				result["failed"] = result["failed"].(int) + 1
				continue
			}
			if payment["trade_state"] == "SUCCESS" {
				if s.markVerifiedPayment(ctx, o, payment) == nil {
					result["paid"] = result["paid"].(int) + 1
				} else {
					result["failed"] = result["failed"].(int) + 1
				}
				continue
			}
			if e = s.wechat.close(o.OrderNo); e != nil {
				result["failed"] = result["failed"].(int) + 1
				continue
			}
		}
		if time.Now().Before(o.ExpiresAt) {
			continue
		}
		if e = s.releaseOrder(ctx, id, "CLOSED"); e == nil {
			result["closed"] = result["closed"].(int) + 1
		} else {
			result["failed"] = result["failed"].(int) + 1
		}
	}
	return result, nil
}
