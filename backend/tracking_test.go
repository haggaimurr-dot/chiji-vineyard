package main

import (
	"context"
	"crypto/md5"
	"encoding/hex"
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"testing"
)

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }
func TestTrackingProviderContract(t *testing.T) {
	t.Setenv("KUAIDI100_CUSTOMER", "test-customer")
	t.Setenv("KUAIDI100_KEY", "test-key")
	old := trackingHTTP
	t.Cleanup(func() { trackingHTTP = old })
	trackingHTTP = &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
		if r.URL.String() != "https://poll.kuaidi100.com/poll/query.do" || r.Method != "POST" {
			t.Fatal("unexpected endpoint")
		}
		if err := r.ParseForm(); err != nil {
			t.Fatal(err)
		}
		param := r.Form.Get("param")
		sum := md5.Sum([]byte(param + "test-key" + "test-customer"))
		if r.Form.Get("sign") != strings.ToUpper(hex.EncodeToString(sum[:])) {
			t.Fatal("wrong provider signature")
		}
		var p map[string]string
		_ = json.Unmarshal([]byte(param), &p)
		if p["phone"] != "1234" || p["com"] != "shunfeng" || p["num"] != "SF123" {
			t.Fatalf("incorrect or excessive data %+v", p)
		}
		if len(p) != 4 {
			t.Fatalf("unexpected personal data %+v", p)
		}
		return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader(`{"status":"200","state":"3","data":[{"time":"2026-10-01 10:00:00","context":"已签收"}]}`)), Header: make(http.Header)}, nil
	})}
	result, err := queryTracking(context.Background(), Order{Address: Address{Phone: "13800001234"}, Shipment: &Shipment{CarrierCode: "SF", TrackingNo: "SF123"}})
	if err != nil || result.Status != "SUCCESS" || result.State != "3" || result.Message != "已签收" || len(result.Events) != 1 {
		t.Fatalf("unexpected result %+v %v", result, err)
	}
}
func TestTrackingWithoutCredentials(t *testing.T) {
	t.Setenv("KUAIDI100_KEY", "")
	result, err := queryTracking(context.Background(), Order{Shipment: &Shipment{CarrierCode: "SF"}})
	if err != nil || result.Status != "UNAVAILABLE" || len(result.Events) != 0 {
		t.Fatalf("must not fabricate tracking: %+v %v", result, err)
	}
}
func TestTrackingMalformedResponse(t *testing.T) {
	t.Setenv("KUAIDI100_CUSTOMER", "test")
	t.Setenv("KUAIDI100_KEY", "test")
	old := trackingHTTP
	t.Cleanup(func() { trackingHTTP = old })
	trackingHTTP = &http.Client{Transport: roundTripFunc(func(*http.Request) (*http.Response, error) {
		return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader("invalid-json"))}, nil
	})}
	result, err := queryTracking(context.Background(), Order{Shipment: &Shipment{CarrierCode: "SF"}})
	if err != nil || result.Status != "UNAVAILABLE" {
		t.Fatalf("unexpected result %+v %v", result, err)
	}
}
