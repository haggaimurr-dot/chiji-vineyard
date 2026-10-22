package main

import (
	"context"
	"encoding/json"
	"net/http"
	"reflect"
	"strings"
	"testing"
	"time"
)

func TestShipmentValidation(t *testing.T) {
	valid := ShipmentInput{Carrier: " 顺丰速运 ", CarrierCode: " sf ", TrackingNo: " SF123456 "}
	got, err := validateShipment(valid)
	if err != nil || got != (ShipmentInput{Carrier: "顺丰速运", CarrierCode: "SF", TrackingNo: "SF123456"}) {
		t.Fatalf("normalization: %+v %v", got, err)
	}
	for _, tc := range []ShipmentInput{
		{Carrier: "", CarrierCode: "SF", TrackingNo: "T123"},
		{Carrier: strings.Repeat("葡", 41), CarrierCode: "SF", TrackingNo: "T123"},
		{Carrier: "快递\n公司", CarrierCode: "SF", TrackingNo: "T123"},
		{Carrier: "<快递>", CarrierCode: "SF", TrackingNo: "T123"},
		{Carrier: "快递", CarrierCode: "顺丰", TrackingNo: "T123"},
		{Carrier: "快递", CarrierCode: strings.Repeat("S", 21), TrackingNo: "T123"},
		{Carrier: "快递", CarrierCode: "SF", TrackingNo: "123"},
		{Carrier: "快递", CarrierCode: "SF", TrackingNo: strings.Repeat("T", 51)},
		{Carrier: "快递", CarrierCode: "SF", TrackingNo: "T 123"},
		{Carrier: "快递", CarrierCode: "SF", TrackingNo: "T<script>"},
	} {
		if _, err := validateShipment(tc); err == nil {
			t.Fatalf("invalid shipment accepted: %+v", tc)
		}
	}
}

func shipmentUpdateRaw(state *shippingFixture) json.RawMessage {
	raw, _ := json.Marshal(M{"orderId": state.order.ID, "carrier": "中通快递", "carrierCode": " zto ", "trackingNo": "NEW12345", "expectedShipment": shipmentInput(*state.order.Shipment)})
	return raw
}

func editableShipmentFixture() *shippingFixture {
	state := shippedFixture()
	state.order.Shipment.CarrierCode = "SF"
	state.order.Shipment.SyncStatus = "SUCCESS"
	return state
}

func TestShipmentCorrectionGuards(t *testing.T) {
	for _, name := range []string{"missing snapshot", "changed snapshot", "refund applied", "refund approved", "refund failed", "fully refunded", "received", "cancelled", "syncing"} {
		t.Run(name, func(t *testing.T) {
			state := editableShipmentFixture()
			raw := shipmentUpdateRaw(state)
			switch name {
			case "missing snapshot":
				raw = []byte(`{"orderId":"o1","carrier":"中通快递","carrierCode":"ZTO","trackingNo":"NEW12345"}`)
			case "changed snapshot":
				state.order.Shipment.TrackingNo = "OTHER123"
			case "refund applied":
				state.order.RefundStatus = "APPLIED"
			case "refund approved":
				state.order.RefundStatus, state.order.PaymentStatus = "APPROVED", "REFUNDING"
			case "refund failed":
				state.order.RefundStatus, state.order.PaymentStatus = "FAILED", "REFUNDING"
			case "fully refunded":
				state.order.RefundStatus, state.order.PaymentStatus = "SUCCEEDED", "REFUNDED"
			case "received":
				state.order.OrderStatus, state.order.FulfillmentStatus = "COMPLETED", "RECEIVED"
			case "cancelled":
				state.order.OrderStatus = "CANCELLED"
			case "syncing":
				state.order.Shipment.SyncStatus, state.order.Shipment.SyncStartedAt = "PENDING", nowString()
			}
			before := *state.order.Shipment
			s := shippingTestServer(t, state, func(*http.Request) (*http.Response, error) {
				t.Fatal("rejected correction reached provider")
				return nil, nil
			})
			if _, err := s.changeShipment(context.Background(), Claims{Subject: "admin"}, raw, "update"); err == nil {
				t.Fatal("invalid correction accepted")
			}
			if !reflect.DeepEqual(*state.order.Shipment, before) || len(state.auditActions) != 0 || state.noticeWrites != 0 {
				t.Fatal("rejected correction changed order or notice")
			}
		})
	}
}

func TestShipmentCorrectionPreservesShippingAndNotice(t *testing.T) {
	configureShippingTest(t)
	for _, reply := range []string{`{"errcode":0}`, `{"errcode":40001,"errmsg":"upload rejected"}`, `{}`} {
		state := editableShipmentFixture()
		state.order.RefundStatus = "PARTIAL"
		state.status = "SENT"
		shippedAt := state.order.Shipment.ShippedAt
		uploads := 0
		s := shippingTestServer(t, state, func(r *http.Request) (*http.Response, error) {
			switch r.URL.Path {
			case "/cgi-bin/token":
				return shippingResponse(`{"access_token":"test-token"}`), nil
			case "/wxa/sec/order/upload_shipping_info":
				uploads++
				return shippingResponse(reply), nil
			default:
				t.Fatal("correction sent a second shipping notice", r.URL.Path)
				return nil, nil
			}
		})
		value, err := s.changeShipment(context.Background(), Claims{Subject: "admin"}, shipmentUpdateRaw(state), "update")
		if err != nil {
			t.Fatal(err)
		}
		shipment := value.(M)["shipment"].(Shipment)
		wantStatus := "SUCCESS"
		if reply != `{"errcode":0}` {
			wantStatus = "FAILED"
		}
		if shipment.CarrierCode != "ZTO" || shipment.TrackingNo != "NEW12345" || shipment.ShippedAt != shippedAt || shipment.SyncStatus != wantStatus || !reflect.DeepEqual(*state.order.Shipment, shipment) {
			t.Fatalf("correction not saved accurately: %+v", shipment)
		}
		if state.status != "SENT" || !state.notified || state.noticeWrites != 0 || uploads != 1 || len(state.auditActions) != 1 || state.auditActions[0] != "ORDER_SHIPMENT_UPDATE" {
			t.Fatalf("notice/audit changed: %+v uploads=%d", state, uploads)
		}
	}
}

func TestShipmentSyncClaimAndStaleResult(t *testing.T) {
	configureShippingTest(t)
	state := editableShipmentFixture()
	state.order.Shipment.SyncStatus = "PENDING"
	state.order.Shipment.SyncStartedAt = time.Now().Add(-3 * time.Minute).UTC().Format(time.RFC3339Nano)
	var s *Server
	var newer Shipment
	s = shippingTestServer(t, state, func(r *http.Request) (*http.Response, error) {
		if r.URL.Path == "/cgi-bin/token" {
			return shippingResponse(`{"access_token":"test-token"}`), nil
		}
		if _, err := s.changeShipment(context.Background(), Claims{Subject: "admin"}, shipmentUpdateRaw(state), "update"); err == nil || !strings.Contains(err.Error(), "正在同步") {
			t.Fatal("concurrent correction did not see claimed upload", err)
		}
		newer = *state.order.Shipment
		newer.TrackingNo, newer.SyncAttempt, newer.SyncStatus = "LATEST123", "newer-attempt", "SUCCESS"
		state.order.Shipment = &newer
		return shippingResponse(`{"errcode":0}`), nil
	})
	if _, err := s.shipOrder(context.Background(), Claims{Subject: "admin"}, []byte(`{"orderId":"o1"}`), true); err == nil || !strings.Contains(err.Error(), "已更新") {
		t.Fatal("stale provider response not rejected", err)
	}
	if !reflect.DeepEqual(*state.order.Shipment, newer) || state.status != "READY" || state.noticeWrites != 0 {
		t.Fatal("stale result overwrote correction or resent notice")
	}
}

func TestShipmentCorrectionNoChange(t *testing.T) {
	state := editableShipmentFixture()
	input := shipmentInput(*state.order.Shipment)
	raw, _ := json.Marshal(M{"orderId": state.order.ID, "carrier": input.Carrier, "carrierCode": input.CarrierCode, "trackingNo": input.TrackingNo, "expectedShipment": input})
	s := shippingTestServer(t, state, func(*http.Request) (*http.Response, error) {
		t.Fatal("unchanged correction reached provider")
		return nil, nil
	})
	if _, err := s.changeShipment(context.Background(), Claims{Subject: "admin"}, raw, "update"); err != nil {
		t.Fatal(err)
	}
	if len(state.auditActions) != 0 || state.noticeWrites != 0 {
		t.Fatal("unchanged correction modified state")
	}
}

func TestShipmentSyncResultSurvivesRequestCancellation(t *testing.T) {
	configureShippingTest(t)
	state := editableShipmentFixture()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	s := shippingTestServer(t, state, func(r *http.Request) (*http.Response, error) {
		if r.URL.Path == "/cgi-bin/token" {
			return shippingResponse(`{"access_token":"test-token"}`), nil
		}
		cancel()
		return shippingResponse(`{"errcode":0}`), nil
	})
	if _, err := s.changeShipment(ctx, Claims{Subject: "admin"}, shipmentUpdateRaw(state), "update"); err != nil {
		t.Fatal(err)
	}
	if state.order.Shipment.SyncStatus != "SUCCESS" {
		t.Fatal("committed edit left pending after request cancellation")
	}
}
