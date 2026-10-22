package main

import (
	"context"
	"database/sql"
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"testing"
)

func TestTargetPriceDispatchAndHistory(t *testing.T) {
	configurePriceTest(t)
	for _, price := range []int{8001, 8000, 7999} {
		f := &priceFixture{shippingFixture: shippingFixture{status: "READY"}, active: true, published: true, stocked: true, price: price, baseline: 9900, target: 8000, skuID: "s1"}
		db := sql.OpenDB(priceConnector{f})
		db.SetMaxOpenConns(1)
		sends := 0
		s := &Server{store: &Store{db: db}, wechat: &Wechat{client: &http.Client{Transport: shippingTransport(func(r *http.Request) (*http.Response, error) {
			body := `{"access_token":"token"}`
			if !strings.Contains(r.URL.Path, "/token") {
				sends++
				body = `{"errcode":0}`
			}
			return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader(body)), Header: make(http.Header)}, nil
		})}}}
		for i := 0; i < 2; i++ {
			if _, err := s.sendPriceDropNotices(context.Background()); err != nil {
				t.Fatal(err)
			}
		}
		want := 1
		if price > 8000 {
			want = 0
		}
		if sends != want || len(f.history) != want {
			t.Fatalf("price=%d sends=%d history=%d", price, sends, len(f.history))
		}
		if want > 0 && (f.history[0].Status != "SENT" || f.history[0].TargetPrice != 8000 || f.history[0].CurrentPrice != price) {
			t.Fatalf("snapshot: %#v", f.history)
		}
		db.Close()
	}
}

func TestTargetPriceBoundaryAndSKU(t *testing.T) {
	p := Product{SKUs: []SKU{{ID: "s1", Price: 8000, Stock: 1, LimitPerOrder: 1}, {ID: "other", Price: 1, Stock: 1, LimitPerOrder: 1}}}
	if priceDropFor(p, "s1", 9900, "READY", 7900).Dropped {
		t.Fatal("a drop above the target triggered")
	}
	if !priceDropFor(p, "s1", 9900, "READY", 8000).Dropped {
		t.Fatal("equal target did not trigger")
	}
	if priceDropFor(p, "removed", 9900, "READY", 8000).Dropped {
		t.Fatal("removed SKU triggered")
	}
	p.SKUs[0].Stock = 0
	if priceDropFor(p, "s1", 9900, "READY", 8000).Available {
		t.Fatal("sold out SKU available")
	}
}

func TestTargetPriceSetValidationAndGrant(t *testing.T) {
	f := &priceFixture{shippingFixture: shippingFixture{status: "READY"}, active: true, published: true, price: 9900, baseline: 9900, skuID: "s1"}
	db := sql.OpenDB(priceConnector{f})
	defer db.Close()
	s := &Store{db: db}
	ctx := context.Background()
	for _, raw := range []string{`{}`, `{"productId":"p1","targetPrice":-1}`, `{"productId":"p1","targetPrice":1.1}`, `{"productId":"p1","targetPrice":10000}`, `{"productId":"p1","targetPrice":null}`} {
		if _, err := s.setPriceDropTarget(ctx, "owner", json.RawMessage(raw)); err == nil {
			t.Fatalf("accepted %s", raw)
		}
	}
	if _, err := s.setPriceDropTarget(ctx, "other", json.RawMessage(`{"productId":"p1","targetPrice":7900}`)); err == nil {
		t.Fatal("other user edited target")
	}
	for _, status := range []string{"READY", "SENT"} {
		f.status = status
		for _, target := range []string{"7900", "7900", "0"} {
			if _, err := s.setPriceDropTarget(ctx, "owner", json.RawMessage(`{"productId":"p1","targetPrice":`+target+`}`)); err != nil {
				t.Fatal(err)
			}
			if f.status != status {
				t.Fatal("grant rearmed")
			}
		}
	}
	for _, status := range []string{"SENDING", "UNCERTAIN", "CANCELLED"} {
		f.status = status
		if _, err := s.setPriceDropTarget(ctx, "owner", json.RawMessage(`{"productId":"p1","targetPrice":7900}`)); err == nil {
			t.Fatal("unresolved send overwritten")
		}
	}
	f.status = "CANCELLED"
	if _, err := s.setPriceDrop(ctx, "owner", "p1", "s1", true, 7900); err != nil {
		t.Fatal(err)
	}
	if f.target != 7900 {
		t.Fatal("initial target not saved")
	}
}

func TestReminderHistorySnapshotOwnershipAndPagination(t *testing.T) {
	f := &priceFixture{history: []AttentionHistory{{ID: "h1", ProductID: "deleted", ProductName: "原商品名", Kind: "pricedrop", SKUName: "原规格", CurrentPrice: 7900, TargetPrice: 8000, Status: "SENT"}, {ID: "h2", ProductID: "p2", Kind: "restock", Status: "FAILED"}}}
	db := sql.OpenDB(priceConnector{f})
	defer db.Close()
	s := &Store{db: db}
	ctx := context.Background()
	result, err := s.attentionHistory(ctx, "owner", json.RawMessage(`{"limit":1}`))
	if err != nil {
		t.Fatal(err)
	}
	page := result.(M)
	items := page["items"].([]AttentionHistory)
	if len(items) != 1 || !page["hasMore"].(bool) || items[0].ProductName != "原商品名" || items[0].TargetPrice != 8000 || items[0].CreatedAt != "2026-10-07T01:02:00Z" {
		t.Fatalf("snapshot: %#v", page)
	}
	result, err = s.attentionHistory(ctx, "owner", json.RawMessage(`{"offset":1,"limit":1}`))
	if err != nil {
		t.Fatal(err)
	}
	if result.(M)["hasMore"].(bool) || result.(M)["items"].([]AttentionHistory)[0].Status != "FAILED" {
		t.Fatal("pagination")
	}
	result, err = s.attentionHistory(ctx, "other", json.RawMessage(`{}`))
	if err != nil {
		t.Fatal(err)
	}
	if len(result.(M)["items"].([]AttentionHistory)) != 0 {
		t.Fatal("history leaked across users")
	}
	for _, raw := range []string{`{"offset":-1}`, `{"limit":51}`, `{"limit":1.2}`} {
		if _, err = s.attentionHistory(ctx, "owner", json.RawMessage(raw)); err == nil {
			t.Fatal("invalid pagination accepted")
		}
	}
}
