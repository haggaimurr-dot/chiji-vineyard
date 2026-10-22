package main

import (
	"context"
	"encoding/json"
	"math"
	"net/http"
	"strings"
	"testing"
)

func TestWaybillSettingsSaveRedactionConflictAndRuntime(t *testing.T) {
	configureWaybillTest(t)
	s, fixture := newWaybillFixture(t)
	ctx := context.Background()
	a := envWaybillAdminSettings()
	a.SenderName = "真实测试寄件人"
	a.Accounts["SF"] = waybillAccount{PartnerID: "private-monthly-id", PartnerKey: "private-password", PartnerSecret: "private-check", TempID: "v2-template", ExpType: "顺丰标快", PayType: "MONTHLY"}
	a.Rates = []waybillRate{{CarrierCode: "JD", FirstWeight: 1, FirstFee: 800, AdditionalWeight: 1, AdditionalFee: 200}}
	saved, err := s.saveWaybillSettings(ctx, Claims{}, json.RawMessage(jsonValue(a)))
	if err != nil {
		t.Fatal(err)
	}
	serialized := jsonValue(saved)
	for _, secret := range []string{"merchant-secret-id", "private-monthly-id", "private-password", "private-check", "waybill-key", "waybill-secret"} {
		if strings.Contains(serialized, secret) {
			t.Fatal("credential leaked in saved settings")
		}
	}
	if saved.(M)["revision"] != 1 {
		t.Fatal(saved)
	}
	if _, err = s.saveWaybillSettings(ctx, Claims{}, json.RawMessage(jsonValue(a))); err == nil {
		t.Fatal("stale revision accepted")
	}
	var edited waybillAdminSettings
	_ = json.Unmarshal([]byte(serialized), &edited)
	edited.SenderAddress = "浙江省杭州市新的测试路1号"
	saved, err = s.saveWaybillSettings(ctx, Claims{}, json.RawMessage(jsonValue(edited)))
	if err != nil {
		t.Fatal(err)
	}
	c, loaded, err := loadWaybillSettings(ctx, s.store.db)
	if err != nil || loaded.Revision != 2 || c.Accounts["SF"].PartnerSecret != "private-check" || c.Accounts["JD"].PartnerID != "merchant-secret-id" {
		t.Fatalf("blank credentials were not preserved: %v", err)
	}
	waybillHTTP = &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
		_ = r.ParseForm()
		var p M
		_ = json.Unmarshal([]byte(r.Form.Get("param")), &p)
		if p["sendMan"].(map[string]any)["printAddr"] != edited.SenderAddress {
			t.Error("runtime ignored saved sender")
		}
		return waybillResponse(successfulWaybillResponse), nil
	})}
	if _, err = s.createWaybill(ctx, Claims{}, testWaybillRequest); err != nil {
		t.Fatal(err)
	}
	if fixture.state.bill.Status != "READY" {
		t.Fatal("saved settings did not produce printable label")
	}
}

func TestWaybillRatesDestinationRoundingMissingAndDisabled(t *testing.T) {
	a := waybillAdminSettings{Accounts: map[string]waybillAccount{}, Rates: []waybillRate{
		{CarrierCode: "JD", FirstWeight: 1, FirstFee: 800, AdditionalWeight: 1, AdditionalFee: 200},
		{CarrierCode: "JD", Province: "浙江省", FirstWeight: 1, FirstFee: 1000, AdditionalWeight: 0.5, AdditionalFee: 150, Surcharge: 100},
		{CarrierCode: "YTO", FirstWeight: 1, FirstFee: 600, AdditionalWeight: 1, AdditionalFee: 100},
	}}
	for _, code := range []string{"JD", "YTO", "EMS", "JT"} {
		a.Accounts[code] = waybillAccount{PartnerID: "id", PartnerKey: "key", TempID: "v2", ExpType: "标准快递"}
	}
	disabled := a.Accounts["JT"]
	disabled.Disabled = true
	a.Accounts["JT"] = disabled
	rows := compareWaybillRates(a, "浙江省", 2)
	if len(rows) != 3 || rows[0]["carrierCode"] != "YTO" || rows[0]["fee"] != int64(700) || rows[1]["fee"] != int64(1400) || rows[2]["available"] != false {
		t.Fatal(rows)
	}
	rows = compareWaybillRates(a, "浙江省", 2.000001)
	if rows[1]["fee"] != int64(1550) {
		t.Fatal("extra gram not charged", rows)
	}
	rows = compareWaybillRates(a, "广东省", 2)
	if rows[1]["fee"] != int64(1000) {
		t.Fatal("nationwide fallback not applied", rows)
	}
	for _, w := range []float64{0, -1, math.NaN(), math.Inf(1), 1001} {
		if validWaybillWeight(w) {
			t.Fatal("invalid weight accepted")
		}
	}
}

func TestWaybillSettingsValidationAndQuotePermissions(t *testing.T) {
	configureWaybillTest(t)
	previous := envWaybillAdminSettings()
	for _, edit := range []func(*waybillAdminSettings){
		func(a *waybillAdminSettings) { a.SenderPhone = "invalid" },
		func(a *waybillAdminSettings) { a.Accounts["UNKNOWN"] = waybillAccount{} },
		func(a *waybillAdminSettings) { a.Accounts["SF"] = waybillAccount{PartnerID: "only-id"} },
		func(a *waybillAdminSettings) {
			a.Rates = []waybillRate{{CarrierCode: "JD", FirstWeight: 1, AdditionalWeight: 0}}
		},
		func(a *waybillAdminSettings) {
			a.Rates = []waybillRate{{CarrierCode: "JD", FirstWeight: 1, AdditionalWeight: 1}, {CarrierCode: "JD", FirstWeight: 2, AdditionalWeight: 1}}
		},
	} {
		a := envWaybillAdminSettings()
		edit(&a)
		if validateWaybillAdminSettings(&a, previous) == nil {
			t.Fatal("invalid settings accepted")
		}
	}
	if actionPermissions["admin.waybill.settings"] != "super" || actionPermissions["admin.waybill.settings.save"] != "super" || actionPermissions["admin.waybill.quote"] != "order.read" {
		t.Fatal("settings or quotes exposed to wrong permission")
	}
	s, _ := newWaybillFixture(t)
	a := previous
	a.Rates = []waybillRate{{CarrierCode: "JD", FirstWeight: 1, FirstFee: 800, AdditionalWeight: 1, AdditionalFee: 200}}
	if _, err := s.saveWaybillSettings(context.Background(), Claims{}, json.RawMessage(jsonValue(a))); err != nil {
		t.Fatal(err)
	}
	quote, err := s.quoteWaybillRates(context.Background(), json.RawMessage(`{"orderId":"o1","weight":2}`))
	if err != nil || quote.(M)["province"] != "浙江省" || quote.(M)["items"].([]M)[0]["fee"] != int64(1000) {
		t.Fatalf("order quote: %v %v", quote, err)
	}
	if _, err = s.quoteWaybillRates(context.Background(), json.RawMessage(`{"orderId":"missing","weight":2}`)); err == nil {
		t.Fatal("missing order accepted")
	}
}
