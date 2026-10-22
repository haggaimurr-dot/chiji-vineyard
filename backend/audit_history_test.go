package main

import (
	"context"
	"database/sql/driver"
	"encoding/json"
	"strings"
	"testing"
	"time"
)

func TestAuditListReturnsPageAndSafeDetails(t *testing.T) {
	created := time.Date(2026, 10, 4, 0, 0, 0, 0, time.UTC)
	state := &historyDB{auditRead: [][]driver.Value{
		{"2", "a", "tester", "CONFIG_SAVE", "shipping", `{"version":3}`, created},
		{"1", "a", "tester", "CONFIG_SAVE", "shipping", `{"token":"private-value","version":2}`, created},
	}}
	server := historyServer(t, state)
	result, err := server.store.auditLogs(context.Background(), json.RawMessage(`{"action":"CONFIG_SAVE","username":"tester","targetId":"shipping","startDate":"2026-10-04","endDate":"2026-10-04","limit":1,"offset":1}`))
	if err != nil {
		t.Fatal(err)
	}
	data := result.(M)
	items := data["items"].([]AuditLog)
	if data["total"] != 2 || data["limit"] != 1 || data["offset"] != 1 || len(items) != 1 || items[0].ID != "1" || items[0].AdminID != "a" || items[0].CreatedAt != "2026-10-04T00:00:00Z" {
		t.Fatalf("bad audit page: %+v", data)
	}
	if strings.Contains(jsonValue(data), "private-value") || len(state.queryArgs) != 2 || len(state.queryArgs[0]) != 5 || len(state.queryArgs[1]) != 7 {
		t.Fatalf("bad redaction or count/list filters: %+v", data)
	}
	for i := range state.queryArgs[0] {
		if state.queryArgs[0][i].Value != state.queryArgs[1][i].Value {
			t.Fatal("count and page filters must agree")
		}
	}
}

func TestAuditFiltersUseChinaDatesAndLiteralParameters(t *testing.T) {
	d := AuditSearch{Action: "CONFIG_ROLLBACK", Username: "user' OR 1=1", TargetID: "shipping", StartDate: "2026-10-04", EndDate: "2026-10-04"}
	where, args, err := auditSearchWhere(d)
	if err != nil {
		t.Fatal(err)
	}
	if where != "1=1 AND action=? AND LOCATE(?, username)>0 AND LOCATE(?, target_id)>0 AND created_at>=? AND created_at<?" || args[1] != d.Username {
		t.Fatalf("filters must remain parameterized: %s %+v", where, args)
	}
	if args[3].(time.Time).Format(time.RFC3339) != "2026-10-03T16:00:00Z" || args[4].(time.Time).Format(time.RFC3339) != "2026-10-04T16:00:00Z" {
		t.Fatalf("wrong China-day boundaries: %+v", args)
	}
	for _, invalid := range []AuditSearch{{StartDate: "2026-02-30"}, {EndDate: "2026-1-01"}, {StartDate: "2026-10-05", EndDate: "2026-10-04"}} {
		if _, _, err := auditSearchWhere(invalid); err == nil {
			t.Fatalf("invalid date accepted: %+v", invalid)
		}
	}
}

func TestAuditDetailsHideNestedCredentials(t *testing.T) {
	raw := []byte(`{"password":"plain","password_hash":"digest","nested":[{"access_token":"private-token","authorization":"Bearer abc","secret":"private-secret"}],"passwordChanged":true,"reason":"回滚旧配置","version":3}`)
	safe := jsonValue(safeAuditDetail(raw))
	for _, leaked := range []string{"plain", "digest", "private-token", "Bearer abc", "private-secret"} {
		if strings.Contains(safe, leaked) {
			t.Fatalf("credential leaked: %s", safe)
		}
	}
	var detail M
	_ = json.Unmarshal([]byte(safe), &detail)
	if detail["passwordChanged"] != true || detail["reason"] != "回滚旧配置" || detail["version"] != float64(3) {
		t.Fatalf("non-sensitive details lost: %+v", detail)
	}
	if strings.Contains(jsonValue(safeAuditDetail([]byte(`password:secret-value`))), "secret-value") {
		t.Fatal("malformed detail leaked")
	}
}

func TestHistoryPaginationBounds(t *testing.T) {
	for _, tc := range []struct{ limit, offset, wantLimit, wantOffset int }{{0, -1, 20, 0}, {1000, 4, 20, 4}, {100, 30, 100, 30}} {
		limit, offset := historyPagination(tc.limit, tc.offset)
		if limit != tc.wantLimit || offset != tc.wantOffset {
			t.Fatalf("bad pagination: %d %d", limit, offset)
		}
	}
}
