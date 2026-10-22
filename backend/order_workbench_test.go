package main

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
)

func TestPickingSelectionRejectedBeforeRead(t *testing.T) {
	for _, ids := range [][]string{nil, make([]string, 51), {"bad id"}, {"o1", " o1 "}} {
		server := &Server{}
		raw := json.RawMessage(jsonValue(M{"orderIds": ids}))
		if _, err := server.pickingOrders(context.Background(), raw); err == nil {
			t.Fatalf("invalid selection accepted: %#v", ids)
		}
	}
}

func TestExceptionOrderSearch(t *testing.T) {
	for filter, expected := range map[string]string{
		"exceptions": " OR ", "sync_failed": "$.shipment.syncStatus",
		"notice_exception": "shipping_notifications", "refund_failed": "refund_status='FAILED'",
		"invoice_exception": "invoice_requests",
	} {
		search, err := normalizeOrderSearch(OrderSearch{Filter: filter, Keyword: "CJ123", Limit: 20}, true)
		if err != nil {
			t.Fatal(err)
		}
		query, args := orderSearchQuery("", search)
		if !strings.Contains(query, expected) || !strings.Contains(query, "LOCATE") || !strings.Contains(query, "LIMIT ? OFFSET ?") || len(args) != 3 {
			t.Fatalf("%s: %s %#v", filter, query, args)
		}
	}
	if actionPermissions["admin.order.picking"] != "order.read" {
		t.Fatal("picking must require order.read")
	}
}
