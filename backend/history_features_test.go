package main

import (
	"context"
	"database/sql/driver"
	"encoding/csv"
	"encoding/json"
	"strings"
	"testing"
	"time"
)

func TestConfigCompareUsesSavedVersionsWithinKey(t *testing.T) {
	state := &historyDB{versions: []historyRecord{
		{id: "old", key: "shipping", version: 1, payload: `{"freight":100}`},
		{id: "new", key: "shipping", version: 2, payload: `{"freight":200}`},
		{id: "other", key: "home", version: 3, payload: `{}`},
	}}
	server := historyServer(t, state)
	result, err := server.store.compareConfigs(context.Background(), json.RawMessage(`{"key":"shipping","fromVersion":1,"toVersion":2}`))
	if err != nil {
		t.Fatal(err)
	}
	data := result.(M)
	if jsonValue(data["before"]) != `{"freight":100}` || jsonValue(data["after"]) != `{"freight":200}` {
		t.Fatalf("bad comparison: %+v", data)
	}
	for _, raw := range []string{`{"key":"shipping","fromVersion":1,"toVersion":3}`, `{"key":"shipping","fromVersion":0,"toVersion":2}`, `{"key":"credentials","fromVersion":1,"toVersion":2}`} {
		if _, err := server.store.compareConfigs(context.Background(), json.RawMessage(raw)); err == nil {
			t.Fatalf("invalid comparison accepted: %s", raw)
		}
	}
}

func TestAuditExportIgnoresPaginationAndRedactsCSV(t *testing.T) {
	state := &historyDB{auditRead: [][]driver.Value{
		{"2", "a", "=HYPERLINK(\"bad\")", "CONFIG_SAVE", "001", `{"token":"never-export","nested":{"password":"private"},"reason":"包含逗号,和引号\""}`, time.Date(2026, 10, 4, 16, 0, 0, 0, time.UTC)},
		{"1", "a", "owner", "CONFIG_SAVE", "shipping", `{}`, time.Date(2026, 10, 4, 0, 0, 0, 0, time.UTC)},
	}}
	server := historyServer(t, state)
	result, err := server.exportAudit(context.Background(), Claims{Subject: "a", Username: "owner"}, json.RawMessage(`{"action":"CONFIG_SAVE","limit":1,"offset":1}`))
	if err != nil {
		t.Fatal(err)
	}
	data := result.(M)
	content := data["content"].(string)
	if data["count"] != 2 || !strings.HasPrefix(content, "\ufeff") || strings.Contains(content, "never-export") || strings.Contains(content, "private") {
		t.Fatalf("unsafe export: %+v", data)
	}
	rows, err := csv.NewReader(strings.NewReader(strings.TrimPrefix(content, "\ufeff"))).ReadAll()
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 3 || rows[1][2] != "'=HYPERLINK(\"bad\")" || rows[1][4] != "'001" || rows[1][5] != "2026-10-05 00:00:00" {
		t.Fatalf("bad CSV: %+v", rows)
	}
	if state.queryArgs[1][0].Value != "CONFIG_SAVE" || state.queryArgs[1][1].Value != int64(10000) || state.queryArgs[1][2].Value != int64(0) {
		t.Fatalf("bad export filters: %+v", state.queryArgs)
	}
	state.fail = "audit"
	if _, err := server.exportAudit(context.Background(), Claims{}, json.RawMessage(`{}`)); err == nil {
		t.Fatal("export returned despite audit failure")
	}
}

func TestAuditExportRejectsOversizedResult(t *testing.T) {
	state := &historyDB{auditRead: make([][]driver.Value, 10001)}
	server := historyServer(t, state)
	if _, err := server.exportAudit(context.Background(), Claims{}, json.RawMessage(`{}`)); err == nil || !strings.Contains(err.Error(), "10000") {
		t.Fatalf("missing export limit: %v", err)
	}
}
