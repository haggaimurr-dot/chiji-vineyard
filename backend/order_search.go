package main

import (
	"context"
	"database/sql"
	"errors"
	"strings"
)

type OrderSearch struct {
	Keyword           string `json:"keyword"`
	Filter            string `json:"filter"`
	Limit             int    `json:"limit"`
	Offset            int    `json:"offset"`
	OrderStatus       string `json:"orderStatus"`
	PaymentStatus     string `json:"paymentStatus"`
	FulfillmentStatus string `json:"fulfillmentStatus"`
	RefundStatus      string `json:"refundStatus"`
}

type OrderPage struct {
	Items  []Order `json:"items"`
	Total  int     `json:"total"`
	Offset int     `json:"offset"`
	Limit  int     `json:"limit"`
}

func normalizeOrderSearch(d OrderSearch, paginated bool) (OrderSearch, error) {
	d.Filter = strings.ToLower(strings.TrimSpace(d.Filter))
	switch d.Filter {
	case "", "all", "pending", "paid", "shipping_pending", "shipping", "shipped", "received", "completed", "cancelled", "closed", "refund", "invoice_pending", "invoice_email", "exceptions", "sync_failed", "notice_exception", "refund_failed", "invoice_exception":
	default:
		return d, errors.New("订单筛选状态无效")
	}
	for _, field := range []struct {
		value *string
		valid string
	}{
		{&d.OrderStatus, " PENDING_PAYMENT CONFIRMED COMPLETED CANCELLED CLOSED "},
		{&d.PaymentStatus, " UNPAID PAID REFUNDING REFUNDED "},
		{&d.FulfillmentStatus, " UNFULFILLED PREPARING SHIPPED RECEIVED "},
		{&d.RefundStatus, " NONE APPLIED APPROVED REJECTED FAILED PARTIAL SUCCEEDED "},
	} {
		*field.value = strings.ToUpper(strings.TrimSpace(*field.value))
		valid := *field.value == ""
		for _, value := range strings.Fields(field.valid) {
			valid = valid || *field.value == value
		}
		if !valid {
			return d, errors.New("订单筛选状态无效")
		}
	}
	if paginated {
		if d.Limit <= 0 {
			d.Limit = 20
		} else if d.Limit > 100 {
			d.Limit = 100
		}
	} else if d.Limit <= 0 || d.Limit > 500 {
		d.Limit = 100
	}
	if d.Offset < 0 {
		d.Offset = 0
	}
	return d, nil
}

func orderSearchWhere(openid string, d OrderSearch) (string, []any) {
	clauses := []string{"1=1"}
	args := []any{}
	if openid != "" {
		clauses = append(clauses, "openid=?")
		args = append(args, openid)
	}
	switch d.Filter {
	case "sync_failed":
		clauses = append(clauses, "JSON_UNQUOTE(JSON_EXTRACT(payload,'$.shipment.syncStatus'))='FAILED'")
	case "notice_exception":
		clauses = append(clauses, "EXISTS (SELECT 1 FROM shipping_notifications WHERE order_id=orders.id AND push_status IN ('FAILED','UNCERTAIN'))")
	case "refund_failed":
		clauses = append(clauses, "refund_status='FAILED'")
	case "invoice_exception":
		clauses = append(clauses, "EXISTS (SELECT 1 FROM invoice_requests WHERE order_id=orders.id AND JSON_UNQUOTE(JSON_EXTRACT(invoice_requests.payload,'$.status'))='ISSUED' AND JSON_UNQUOTE(JSON_EXTRACT(invoice_requests.payload,'$.emailStatus')) IN ('FAILED','UNCERTAIN'))")
	case "exceptions":
		clauses = append(clauses, "((JSON_UNQUOTE(JSON_EXTRACT(payload,'$.shipment.syncStatus'))='FAILED') OR (EXISTS (SELECT 1 FROM shipping_notifications WHERE order_id=orders.id AND push_status IN ('FAILED','UNCERTAIN'))) OR (refund_status='FAILED') OR (EXISTS (SELECT 1 FROM invoice_requests WHERE order_id=orders.id AND JSON_UNQUOTE(JSON_EXTRACT(invoice_requests.payload,'$.status'))='ISSUED' AND JSON_UNQUOTE(JSON_EXTRACT(invoice_requests.payload,'$.emailStatus')) IN ('FAILED','UNCERTAIN'))))")
	case "pending":
		clauses = append(clauses, "order_status='PENDING_PAYMENT' AND payment_status='UNPAID'")
	case "paid", "shipping_pending":
		clauses = append(clauses, "order_status='CONFIRMED' AND payment_status='PAID' AND fulfillment_status IN ('UNFULFILLED','PREPARING') AND refund_status IN ('NONE','REJECTED','PARTIAL')")
	case "shipping", "shipped":
		clauses = append(clauses, "order_status='CONFIRMED' AND payment_status='PAID' AND fulfillment_status='SHIPPED' AND refund_status IN ('NONE','REJECTED','PARTIAL')")
	case "received":
		clauses = append(clauses, "fulfillment_status='RECEIVED'")
	case "completed":
		clauses = append(clauses, "order_status='COMPLETED'")
	case "cancelled":
		clauses = append(clauses, "order_status='CANCELLED'")
	case "closed":
		clauses = append(clauses, "order_status='CLOSED'")
	case "refund":
		clauses = append(clauses, "(refund_status<>'NONE' OR payment_status IN ('REFUNDING','REFUNDED'))")
	case "invoice_pending":
		clauses = append(clauses, "EXISTS (SELECT 1 FROM invoice_requests WHERE order_id=orders.id AND JSON_UNQUOTE(JSON_EXTRACT(invoice_requests.payload,'$.status'))='APPLIED')")
	case "invoice_email":
		clauses = append(clauses, "EXISTS (SELECT 1 FROM invoice_requests WHERE order_id=orders.id AND JSON_UNQUOTE(JSON_EXTRACT(invoice_requests.payload,'$.status'))='ISSUED' AND JSON_UNQUOTE(JSON_EXTRACT(invoice_requests.payload,'$.emailStatus')) IN ('READY','FAILED','SENDING','UNCERTAIN'))")
	}
	for _, field := range []struct{ column, value string }{
		{"order_status", d.OrderStatus}, {"payment_status", d.PaymentStatus},
		{"fulfillment_status", d.FulfillmentStatus}, {"refund_status", d.RefundStatus},
	} {
		if field.value != "" {
			clauses = append(clauses, field.column+"=?")
			args = append(args, field.value)
		}
	}
	// LOCATE treats %, _ and backslash as literal search text; each word must match.
	for _, word := range strings.Fields(strings.ToLower(clean(d.Keyword, 80))) {
		clauses = append(clauses, "LOCATE(?, LOWER(CONCAT(order_no,' ',COALESCE(JSON_EXTRACT(payload,'$.items[*].name'),'[]'),' ',COALESCE(JSON_EXTRACT(payload,'$.items[*].skuName'),'[]'),' ',COALESCE(JSON_EXTRACT(payload,'$.address.name'),''),' ',COALESCE(JSON_EXTRACT(payload,'$.address.phone'),''),' ',COALESCE(JSON_EXTRACT(payload,'$.shipment.trackingNo'),''))))>0")
		args = append(args, word)
	}
	return strings.Join(clauses, " AND "), args
}

func orderSearchQuery(openid string, d OrderSearch) (string, []any) {
	if d.Limit <= 0 || d.Limit > 500 {
		d.Limit = 100
	}
	if d.Offset < 0 {
		d.Offset = 0
	}
	where, args := orderSearchWhere(openid, d)
	args = append(args, d.Limit, d.Offset)
	return "SELECT id FROM orders WHERE " + where + " ORDER BY created_at DESC,id DESC LIMIT ? OFFSET ?", args
}

func (s *Store) searchOrdersPage(ctx context.Context, openid string, search OrderSearch) (OrderPage, error) {
	search, err := normalizeOrderSearch(search, true)
	page := OrderPage{Items: []Order{}, Offset: search.Offset, Limit: search.Limit}
	if err != nil {
		return page, err
	}
	// Count and rows share a snapshot so concurrent checkout/refund writes cannot
	// change the total halfway through a page response.
	tx, err := s.db.BeginTx(ctx, &sql.TxOptions{ReadOnly: true, Isolation: sql.LevelRepeatableRead})
	if err != nil {
		return page, err
	}
	defer tx.Rollback()
	where, args := orderSearchWhere(openid, search)
	if err = tx.QueryRowContext(ctx, "SELECT COUNT(*) FROM orders WHERE "+where, args...).Scan(&page.Total); err != nil {
		return page, err
	}
	page.Items, err = s.searchOrdersFrom(ctx, tx, openid, search)
	if err != nil {
		return page, err
	}
	return page, tx.Commit()
}
