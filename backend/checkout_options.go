package main

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"net/mail"
	"regexp"
	"strings"
	"time"
)

type Coupon struct {
	Code      string `json:"code"`
	Name      string `json:"name"`
	Minimum   int    `json:"minimum"`
	Amount    int    `json:"amount"`
	StartDate string `json:"startDate"`
	EndDate   string `json:"endDate"`
	Active    bool   `json:"active"`
}
type CouponConfig struct {
	Items []Coupon `json:"items"`
}
type Invoice struct {
	AutoStatus     string `json:"autoStatus,omitempty"`
	AutoError      string `json:"autoError,omitempty"`
	AutoKey        string `json:"autoKey,omitempty"`
	AutoStartedAt  string `json:"autoStartedAt,omitempty"`
	Kind           string `json:"kind"`
	Title          string `json:"title"`
	TaxID          string `json:"taxId"`
	Email          string `json:"email"`
	Status         string `json:"status"`
	Number         string `json:"number,omitempty"`
	Reason         string `json:"reason,omitempty"`
	Amount         int    `json:"amount"`
	CreatedAt      string `json:"createdAt"`
	FileName       string `json:"fileName,omitempty"`
	IssuedAt       string `json:"issuedAt,omitempty"`
	EmailStatus    string `json:"emailStatus,omitempty"`
	EmailError     string `json:"emailError,omitempty"`
	SentAt         string `json:"sentAt,omitempty"`
	EmailAttempt   string `json:"emailAttempt,omitempty"`
	EmailStartedAt string `json:"emailStartedAt,omitempty"`
}

var couponRE = regexp.MustCompile(`^[A-Z0-9_-]{1,40}$`)
var taxRE = regexp.MustCompile(`^[A-Z0-9]{15,20}$`)

func chinaDay(now time.Time) string {
	return now.In(time.FixedZone("CST", 8*3600)).Format("2006-01-02")
}
func validateDelivery(date string, items []OrderItem, now time.Time) error {
	if date == "" {
		return nil
	}
	start := chinaDay(now.AddDate(0, 0, 2))
	end := chinaDay(now.AddDate(0, 0, 30))
	for _, i := range items {
		if i.SaleMode == "presale" && i.ExpectedShipDate > start {
			start = i.ExpectedShipDate
		}
	}
	t, e := time.Parse("2006-01-02", date)
	if e != nil || t.Format("2006-01-02") != date || date < start || date > end {
		return errors.New("配送日期须在两天后至30天内，且不早于预售发货日期")
	}
	return nil
}
func discountCoupon(c Coupon, q *Quote, now time.Time) error {
	day := chinaDay(now)
	if !c.Active || day < c.StartDate || day > c.EndDate {
		return errors.New("优惠券未生效或已过期")
	}
	if q.Subtotal < c.Minimum {
		return errors.New("商品金额未达到优惠券使用门槛")
	}
	if c.Amount <= 0 {
		return errors.New("优惠券金额无效")
	}
	q.Discount = c.Amount
	if q.Discount >= q.Subtotal {
		q.Discount = q.Subtotal - 1
	}
	if q.Discount < 0 {
		q.Discount = 0
	}
	q.CouponCode = c.Code
	q.Total = q.Subtotal + q.Freight - q.Discount
	return nil
}
func (s *Server) couponQuote(ctx context.Context, q interface {
	QueryRowContext(context.Context, string, ...any) *sql.Row
}, openid, code string, quote *Quote, lock bool) error {
	code = strings.ToUpper(strings.TrimSpace(code))
	if code == "" {
		return nil
	}
	query := "SELECT payload FROM configs WHERE config_key='coupons'"
	if lock {
		query += " FOR UPDATE"
	}
	var raw []byte
	if e := q.QueryRowContext(ctx, query).Scan(&raw); e != nil {
		if errors.Is(e, sql.ErrNoRows) {
			return errors.New("优惠券不存在")
		}
		return e
	}
	var config CouponConfig
	if e := json.Unmarshal(raw, &config); e != nil {
		return e
	}
	var count int
	where := " FROM orders WHERE openid=? AND (order_status NOT IN ('CANCELLED','CLOSED') OR payment_status<>'UNPAID') AND JSON_UNQUOTE(JSON_EXTRACT(payload,'$.quote.couponCode'))=?"
	if lock {
		// A locking read sees the latest committed order even when shipping reads
		// already established a REPEATABLE READ snapshot before the config lock.
		var id string
		e := q.QueryRowContext(ctx, "SELECT id"+where+" LIMIT 1 FOR UPDATE", openid, code).Scan(&id)
		if e != nil && !errors.Is(e, sql.ErrNoRows) {
			return e
		}
		if e == nil {
			count = 1
		}
	} else if e := q.QueryRowContext(ctx, "SELECT COUNT(*)"+where, openid, code).Scan(&count); e != nil {
		return e
	}
	if count > 0 {
		return errors.New("此优惠券已使用或被待付款订单占用")
	}
	for _, c := range config.Items {
		if c.Code == code {
			return discountCoupon(c, quote, time.Now())
		}
	}
	return errors.New("优惠券不存在")
}
func (s *Server) saveCoupons(ctx context.Context, actor Claims, raw json.RawMessage) (any, error) {
	result, err := s.saveVersionedConfig(ctx, actor, "coupons", raw)
	return result.Payload, err
}

func validateCoupons(c *CouponConfig) error {
	if len(c.Items) > 100 {
		return errors.New("最多配置100张优惠券")
	}
	seen := map[string]bool{}
	for i := range c.Items {
		x := &c.Items[i]
		x.Code = strings.ToUpper(strings.TrimSpace(x.Code))
		x.Name = clean(x.Name, 60)
		_, a := time.Parse("2006-01-02", x.StartDate)
		_, b := time.Parse("2006-01-02", x.EndDate)
		if !couponRE.MatchString(x.Code) || seen[x.Code] || x.Name == "" || x.Amount < 1 || x.Amount > 10000000 || x.Minimum < 0 || x.Minimum > 10000000 || a != nil || b != nil || x.StartDate > x.EndDate {
			return errors.New("优惠券编码、金额或有效期无效（编码不可重复）")
		}
		seen[x.Code] = true
	}
	return nil
}
func validateInvoice(i Invoice) (Invoice, error) {
	i.Title = clean(i.Title, 100)
	i.Email = strings.TrimSpace(i.Email)
	i.TaxID = strings.ToUpper(strings.TrimSpace(i.TaxID))
	a, e := mail.ParseAddress(i.Email)
	if i.Title == "" || len(i.Email) > 200 || e != nil || a.Address != i.Email || (i.Kind != "personal" && i.Kind != "company") || (i.Kind == "company" && !taxRE.MatchString(i.TaxID)) {
		return i, errors.New("请填写发票抬头、有效邮箱及企业税号")
	}
	if i.Kind == "personal" {
		i.TaxID = ""
	}
	// Accept only applicant fields; operational metadata is server-owned.
	return Invoice{Kind: i.Kind, Title: i.Title, TaxID: i.TaxID, Email: i.Email}, nil
}
func invoiceEligible(o *Order) bool {
	return o != nil && o.PaymentStatus == "PAID" && (o.RefundStatus == "NONE" || o.RefundStatus == "REJECTED")
}
func (s *Server) applyInvoice(ctx context.Context, openid string, raw json.RawMessage) (any, error) {
	var d struct {
		OrderID string  `json:"orderId"`
		Invoice Invoice `json:"invoice"`
	}
	if e := decode(raw, &d); e != nil {
		return nil, e
	}
	i, e := validateInvoice(d.Invoice)
	if e != nil {
		return nil, e
	}
	tx, e := s.store.db.BeginTx(ctx, nil)
	if e != nil {
		return nil, e
	}
	defer tx.Rollback()
	o, e := s.store.order(ctx, tx, d.OrderID, true)
	if e != nil {
		return nil, e
	}
	if o == nil || o.OpenID != openid {
		return nil, errors.New("订单不存在")
	}
	if !invoiceEligible(o) {
		return nil, errors.New("仅已支付且无进行中退款的订单可申请发票")
	}
	if o.Invoice != nil && o.Invoice.Status != "REJECTED" {
		return nil, errors.New("此订单已申请发票")
	}
	i.Status = "APPLIED"
	i.CreatedAt = nowString()
	i.Amount = o.Quote.Total
	_, e = tx.ExecContext(ctx, "INSERT INTO invoice_requests(order_id,payload) VALUES(?,?) ON DUPLICATE KEY UPDATE payload=VALUES(payload)", o.ID, jsonValue(i))
	if e != nil {
		return nil, e
	}
	if e = recordInvoiceTitle(ctx, tx, openid, i); e != nil {
		return nil, e
	}
	return i, tx.Commit()
}
func (s *Server) updateInvoice(ctx context.Context, raw json.RawMessage) (any, error) {
	var d struct{ OrderID, Status, Number, Reason, FileName, FileData string }
	if e := decode(raw, &d); e != nil {
		return nil, e
	}
	tx, e := s.store.db.BeginTx(ctx, nil)
	if e != nil {
		return nil, e
	}
	defer tx.Rollback()
	o, e := s.store.order(ctx, tx, d.OrderID, true)
	if e != nil {
		return nil, e
	}
	if o == nil || o.Invoice == nil || o.Invoice.Status != "APPLIED" {
		return nil, errors.New("没有待处理发票申请")
	}
	i := o.Invoice
	if autoInvoiceBusy(i) {
		return nil, errors.New("自动开票结果待确认，请先核实原请求")
	}
	if d.Status == "ISSUED" {
		if !invoiceEligible(o) {
			return nil, errors.New("订单退款状态已变化，不能开票")
		}
		i.Number = clean(d.Number, 80)
		if i.Number == "" {
			return nil, errors.New("请输入发票号码")
		}
		content, err := decodeInvoicePDF(d.FileName, d.FileData)
		if err != nil {
			return nil, err
		}
		i.AutoStatus = ""
		i.AutoError = ""
		i.FileName = clean(d.FileName, 120)
		i.IssuedAt = nowString()
		i.EmailStatus = "READY"
		if _, err = tx.ExecContext(ctx, "INSERT INTO invoice_files(order_id,content) VALUES(?,?)", o.ID, content); err != nil {
			return nil, err
		}
	} else if d.Status == "REJECTED" {
		i.Reason = clean(d.Reason, 200)
		if i.Reason == "" {
			return nil, errors.New("请输入拒绝原因")
		}
	} else {
		return nil, errors.New("无效发票状态")
	}
	i.Status = d.Status
	_, e = tx.ExecContext(ctx, "UPDATE invoice_requests SET payload=? WHERE order_id=?", jsonValue(i), o.ID)
	if e != nil {
		return nil, e
	}
	if e = tx.Commit(); e != nil {
		return nil, e
	}
	if i.Status == "ISSUED" {
		return s.sendInvoiceEmail(ctx, o.ID)
	}
	return i, nil
}
