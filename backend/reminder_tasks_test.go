package main

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"encoding/json"
	"errors"
	"io"
	"strings"
	"testing"
)

type taskFixture struct {
	status, snapshot string
	active           int64
	audits, history  int
	failAudit        bool
}
type taskConnector struct{ f *taskFixture }

func (c taskConnector) Connect(context.Context) (driver.Conn, error) { return c.f, nil }
func (c taskConnector) Driver() driver.Driver                        { return taskDriver{c.f} }

type taskDriver struct{ f *taskFixture }

func (d taskDriver) Open(string) (driver.Conn, error) { return d.f, nil }
func (f *taskFixture) Prepare(string) (driver.Stmt, error) {
	return nil, errors.New("unexpected prepare")
}
func (f *taskFixture) Close() error              { return nil }
func (f *taskFixture) Begin() (driver.Tx, error) { f.snapshot = f.status; return f, nil }
func (f *taskFixture) Commit() error             { f.snapshot = ""; return nil }
func (f *taskFixture) Rollback() error {
	if f.snapshot != "" {
		f.status = f.snapshot
		f.snapshot = ""
	}
	return nil
}

type taskRows struct {
	value driver.Value
	done  bool
}

func (r *taskRows) Columns() []string { return []string{"value"} }
func (r *taskRows) Close() error      { return nil }
func (r *taskRows) Next(out []driver.Value) error {
	if r.done {
		return io.EOF
	}
	out[0] = r.value
	r.done = true
	return nil
}
func (f *taskFixture) QueryContext(_ context.Context, q string, _ []driver.NamedValue) (driver.Rows, error) {
	if strings.HasPrefix(q, "SELECT push_status") && strings.HasSuffix(q, "FOR UPDATE") {
		return &taskRows{value: f.status}, nil
	}
	if strings.HasPrefix(q, "SELECT COUNT(*) FROM product_attention") {
		return &taskRows{value: f.active}, nil
	}
	return nil, errors.New("unexpected query: " + q)
}
func (f *taskFixture) ExecContext(_ context.Context, q string, args []driver.NamedValue) (driver.Result, error) {
	switch {
	case strings.HasPrefix(q, "UPDATE restock_notifications"), strings.HasPrefix(q, "UPDATE price_drop_notifications"):
		f.status = args[0].Value.(string)
	case strings.HasPrefix(q, "UPDATE product_reminder_history"):
		f.history++
	case strings.HasPrefix(q, "INSERT INTO audit_logs"):
		if f.failAudit {
			return nil, errors.New("audit failed")
		}
		f.audits++
	default:
		return nil, errors.New("unexpected exec: " + q)
	}
	return driver.RowsAffected(1), nil
}
func TestReminderResolutionGuards(t *testing.T) {
	for _, status := range []string{"NONE", "READY", "SENDING", "SENT", "FAILED", "BLOCKED", "UNCERTAIN", "CANCELLED"} {
		for _, action := range []string{"retry", "sent", "stop", "invalid"} {
			next, err := reminderResolution(status, action)
			allowed := status == "FAILED" && (action == "retry" || action == "stop") || status == "BLOCKED" && action == "stop" || status == "UNCERTAIN" && (action == "sent" || action == "stop")
			if (err == nil) != allowed || (!allowed && next != "") {
				t.Fatalf("%s/%s: %s %v", status, action, next, err)
			}
		}
	}
}
func TestReminderResolveTransactions(t *testing.T) {
	for _, tc := range []struct {
		status, expected, action, want string
		active                         int64
		failAudit                      bool
		wantErr                        bool
	}{
		{"FAILED", "FAILED", "retry", "READY", 1, false, false},
		{"FAILED", "FAILED", "retry", "FAILED", 0, false, true},
		{"SENT", "FAILED", "retry", "SENT", 1, false, true},
		{"UNCERTAIN", "UNCERTAIN", "sent", "SENT", 1, false, false},
		{"BLOCKED", "BLOCKED", "retry", "BLOCKED", 1, false, true},
		{"SENDING", "SENDING", "stop", "SENDING", 1, false, true},
		{"FAILED", "FAILED", "stop", "FAILED", 1, true, true},
	} {
		for _, kind := range []string{"restock", "pricedrop"} {
			f := &taskFixture{status: tc.status, active: tc.active, failAudit: tc.failAudit}
			db := sql.OpenDB(taskConnector{f})
			raw, _ := json.Marshal(M{"openid": "user", "productId": "product", "kind": kind, "status": tc.expected, "action": tc.action, "note": "已核实"})
			_, err := (&Store{db: db}).resolveReminderTask(context.Background(), Claims{Subject: "admin", Username: "operator"}, raw)
			db.Close()
			if (err != nil) != tc.wantErr || f.status != tc.want {
				t.Fatalf("%s %#v status=%s err=%v", kind, tc, f.status, err)
			}
			if !tc.wantErr && f.audits != 1 {
				t.Fatal("missing audit")
			}
			if !tc.wantErr && tc.status == "UNCERTAIN" && f.history != 1 {
				t.Fatal("missing history resolution")
			}
		}
	}
}
