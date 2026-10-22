package main

import (
	"testing"
	"time"
)

func TestCouponPricing(t *testing.T) {
	now := time.Date(2026, 10, 4, 0, 0, 0, 0, time.UTC)
	c := Coupon{Code: "SAVE10", Minimum: 9900, Amount: 1000, StartDate: "2026-10-04", EndDate: "2026-10-04", Active: true}
	q := Quote{Subtotal: 9900, Freight: 1200, Total: 11100}
	if e := discountCoupon(c, &q, now); e != nil || q.Total != 10100 || q.Discount != 1000 || q.Freight != 1200 || q.CouponCode != "SAVE10" {
		t.Fatalf("invalid discount: %+v %v", q, e)
	}
	q = Quote{Subtotal: 9899}
	if discountCoupon(c, &q, now) == nil {
		t.Fatal("below threshold accepted")
	}
	q.Subtotal = 9900
	if discountCoupon(c, &q, now.AddDate(0, 0, 1)) == nil {
		t.Fatal("expired coupon accepted")
	}
	c.Active = false
	if discountCoupon(c, &q, now) == nil {
		t.Fatal("disabled coupon accepted")
	}
	c.Active = true
	c.Minimum = 0
	c.Amount = 99999
	q = Quote{Subtotal: 500, Freight: 1200}
	if e := discountCoupon(c, &q, now); e != nil || q.Total != 1201 || q.Discount != 499 {
		t.Fatalf("must preserve positive product payment: %+v %v", q, e)
	}
}
func TestDeliveryDates(t *testing.T) {
	now := time.Date(2026, 10, 4, 16, 1, 0, 0, time.UTC)
	for _, d := range []string{"", "2026-10-07", "2026-11-04"} {
		if e := validateDelivery(d, nil, now); e != nil {
			t.Fatalf("%s: %v", d, e)
		}
	}
	for _, d := range []string{"2026-10-06", "2026-11-05", "2026-10-40", "2026-1-07"} {
		if validateDelivery(d, nil, now) == nil {
			t.Fatalf("accepted %s", d)
		}
	}
	items := []OrderItem{{SaleMode: "presale", ExpectedShipDate: "2026-10-20"}}
	if validateDelivery("2026-10-19", items, now) == nil {
		t.Fatal("before presale accepted")
	}
	if validateDelivery("2026-10-20", items, now) != nil {
		t.Fatal("presale date rejected")
	}
}
func TestInvoiceValidation(t *testing.T) {
	i := Invoice{Kind: "company", Title: " 公司 ", TaxID: "91310000DEMO123456", Email: "test@example.com"}
	x, e := validateInvoice(i)
	if e != nil || x.Title != "公司" {
		t.Fatal(x, e)
	}
	i.TaxID = ""
	if _, e = validateInvoice(i); e == nil {
		t.Fatal("missing tax id accepted")
	}
	i.Kind = "personal"
	i.TaxID = "unused"
	x, e = validateInvoice(i)
	if e != nil || x.TaxID != "" {
		t.Fatal(x, e)
	}
	i.Email = "bad"
	if _, e = validateInvoice(i); e == nil {
		t.Fatal("invalid email accepted")
	}
	for _, o := range []*Order{nil, {PaymentStatus: "UNPAID", RefundStatus: "NONE"}, {PaymentStatus: "PAID", RefundStatus: "APPLIED"}, {PaymentStatus: "REFUNDED", RefundStatus: "SUCCEEDED"}} {
		if invoiceEligible(o) {
			t.Fatal("invalid invoice eligibility")
		}
	}
	if !invoiceEligible(&Order{PaymentStatus: "PAID", RefundStatus: "REJECTED"}) {
		t.Fatal("rejected refund should permit invoice")
	}
}
