package main

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestAutoInvoiceGatewayResults(t *testing.T) {
	i := Invoice{AutoKey: "INV1", Title: "测试企业", TaxID: "91310000123456789A", Amount: 12345}
	good := AutoInvoiceResult{Status: "ISSUED", Key: i.AutoKey, Title: i.Title, TaxID: i.TaxID, Amount: i.Amount, Number: "123", FileName: "invoice.pdf", FileData: base64.StdEncoding.EncodeToString([]byte("%PDF-1.4\n%%EOF"))}
	result := good
	httpStatus := 200
	calls := 0
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		if r.Header.Get("Idempotency-Key") != "INV1" || r.Header.Get("Authorization") != "Bearer secret" {
			t.Error("missing gateway authentication/idempotency")
		}
		var request map[string]any
		json.NewDecoder(r.Body).Decode(&request)
		if request["requestId"] != "INV1" || request["title"] != i.Title || request["amount"] != float64(i.Amount) {
			t.Error("invalid gateway request")
		}
		w.WriteHeader(httpStatus)
		json.NewEncoder(w).Encode(result)
	}))
	defer server.Close()
	call := func(want string) {
		t.Helper()
		_, status, err := requestAutoInvoice(context.Background(), server.Client(), server.URL, "secret", Order{ID: "order1"}, i)
		if status != want || (err == nil) != (want == "ISSUED") {
			t.Fatalf("status=%s err=%v want=%s", status, err, want)
		}
	}
	call("ISSUED")
	call("ISSUED")
	if calls != 2 {
		t.Fatal("replay missing")
	}
	for _, change := range []func(*AutoInvoiceResult){func(r *AutoInvoiceResult) { r.Key = "foreign" }, func(r *AutoInvoiceResult) { r.Amount++ }, func(r *AutoInvoiceResult) { r.Title = "other" }, func(r *AutoInvoiceResult) { r.TaxID = "other" }, func(r *AutoInvoiceResult) { r.FileData = "invalid" }, func(r *AutoInvoiceResult) { r.Status = "PENDING" }} {
		result = good
		change(&result)
		call("UNCERTAIN")
	}
	result = AutoInvoiceResult{Key: "INV1", Status: "REJECTED"}
	call("FAILED")
	result = good
	httpStatus = 500
	call("UNCERTAIN")
	cancelled, cancel := context.WithCancel(context.Background())
	cancel()
	_, status, err := requestAutoInvoice(cancelled, server.Client(), server.URL, "secret", Order{}, i)
	if status != "UNCERTAIN" || err == nil {
		t.Fatal("network failure not uncertain")
	}
}
func TestAutoInvoiceConfigAndGuards(t *testing.T) {
	t.Setenv("INVOICE_GATEWAY_TOKEN", "secret")
	for _, endpoint := range []string{"", "http://example.com", "https://user:pass@example.com", "https://example.com/#fragment"} {
		t.Setenv("INVOICE_GATEWAY_URL", endpoint)
		if _, _, err := autoInvoiceConfig(); err == nil {
			t.Fatal("unsafe endpoint accepted", endpoint)
		}
	}
	t.Setenv("INVOICE_GATEWAY_URL", "https://example.com/invoice")
	if _, _, err := autoInvoiceConfig(); err != nil {
		t.Fatal(err)
	}
	if autoInvoiceBusy(nil) || autoInvoiceBusy(&Invoice{AutoStatus: "FAILED"}) || !autoInvoiceBusy(&Invoice{AutoStatus: "PROCESSING"}) || !autoInvoiceBusy(&Invoice{AutoStatus: "UNCERTAIN"}) {
		t.Fatal("wrong issuing guards")
	}
	applicant, err := validateInvoice(Invoice{Kind: "personal", Title: "张三", Email: "test@example.com", AutoKey: "fake", AutoStatus: "ISSUED", AutoError: "fake"})
	if err != nil || applicant.AutoKey != "" || applicant.AutoStatus != "" || applicant.AutoError != "" {
		t.Fatal("applicant spoofed auto metadata", applicant, err)
	}
}
