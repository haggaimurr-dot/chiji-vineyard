package main

import (
	"testing"
	"time"
)

func TestBestWalletCoupon(t *testing.T) {
	now := time.Date(2026, 10, 8, 0, 0, 0, 0, time.UTC)
	makeCoupon := func(code string, minimum, amount int, end string) WalletCoupon {
		return WalletCoupon{Coupon: Coupon{Code: code, Minimum: minimum, Amount: amount, Active: true, StartDate: "2026-01-01", EndDate: end}, Status: "AVAILABLE"}
	}
	wallet := []WalletCoupon{
		makeCoupon("EARLY", 0, 1000, "2026-10-09"),
		makeCoupon("LARGER", 19900, 2000, "2026-12-31"),
		makeCoupon("LATE", 0, 1000, "2026-11-30"),
	}
	for _, tc := range []struct {
		subtotal int
		want     string
	}{{9900, "EARLY"}, {19900, "LARGER"}, {1, ""}} {
		if got := bestWalletCoupon(wallet, Quote{Subtotal: tc.subtotal, Freight: 1200}, now); got != tc.want {
			t.Fatalf("subtotal %d: got %s want %s", tc.subtotal, got, tc.want)
		}
	}
	wallet[0].Status = "RESERVED"
	if got := bestWalletCoupon(wallet, Quote{Subtotal: 9900}, now); got != "LATE" {
		t.Fatal(got)
	}
	wallet[2].Status = "USED"
	if got := bestWalletCoupon(wallet, Quote{Subtotal: 9900}, now); got != "" {
		t.Fatal(got)
	}
	// Both coupons are capped to subtotal-1: consume the earlier expiry, not the larger face value.
	wallet = []WalletCoupon{makeCoupon("BIG", 0, 2000, "2026-12-31"), makeCoupon("SMALL", 0, 1000, "2026-10-09")}
	if got := bestWalletCoupon(wallet, Quote{Subtotal: 500}, now); got != "SMALL" {
		t.Fatal(got)
	}
	wallet[0].EndDate = "2026-10-07"
	wallet[1].Active = false
	if got := bestWalletCoupon(wallet, Quote{Subtotal: 500}, now); got != "" {
		t.Fatal("invalid rule accepted", got)
	}
}

func TestWalletCouponStatus(t *testing.T) {
	now := time.Date(2026, 10, 8, 16, 0, 0, 0, time.UTC) // Oct 9 in Beijing.
	c := Coupon{Active: true, StartDate: "2026-10-09", EndDate: "2026-10-09"}
	cases := []struct {
		used, reserved bool
		want           string
	}{{false, false, "AVAILABLE"}, {false, true, "RESERVED"}, {true, true, "USED"}}
	for _, tc := range cases {
		if status, _ := walletStatus(c, tc.used, tc.reserved, now); status != tc.want {
			t.Fatal(status, tc.want)
		}
	}
	if status, _ := walletStatus(c, false, false, now.AddDate(0, 0, 1)); status != "EXPIRED" {
		t.Fatal(status)
	}
	if status, _ := walletStatus(c, false, false, now.AddDate(0, 0, -1)); status != "UPCOMING" {
		t.Fatal(status)
	}
	c.Active = false
	if status, _ := walletStatus(c, false, false, now); status != "DISABLED" {
		t.Fatal(status)
	}
}
