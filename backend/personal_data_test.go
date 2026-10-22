package main

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"encoding/json"
	"errors"
	"net/http/httptest"
	"strings"
	"testing"
)

type personalFixture struct {
	blocked                            int64
	fail                               string
	pending, writes                    []string
	active, committed, closed, revoked bool
}
type personalConnector struct{ f *personalFixture }
type personalDriver struct{}

func (c personalConnector) Connect(context.Context) (driver.Conn, error) { return c.f, nil }
func (c personalConnector) Driver() driver.Driver                        { return personalDriver{} }
func (personalDriver) Open(string) (driver.Conn, error)                  { return nil, errors.New("use connector") }
func (f *personalFixture) Prepare(string) (driver.Stmt, error) {
	return nil, errors.New("not supported")
}
func (f *personalFixture) Close() error              { return nil }
func (f *personalFixture) Begin() (driver.Tx, error) { f.active = true; f.pending = nil; return f, nil }
func (f *personalFixture) Commit() error {
	f.writes = append(f.writes, f.pending...)
	f.committed = true
	f.active = false
	for _, q := range f.pending {
		if strings.HasPrefix(q, "INSERT INTO account_closures") {
			f.closed = true
		}
		if strings.HasPrefix(q, "DELETE FROM auth_sessions") {
			f.revoked = true
		}
	}
	return nil
}
func (f *personalFixture) Rollback() error { f.pending = nil; f.active = false; return nil }
func (f *personalFixture) QueryContext(_ context.Context, q string, args []driver.NamedValue) (driver.Rows, error) {
	switch {
	case strings.Contains(q, "GET_LOCK") || strings.Contains(q, "RELEASE_LOCK"):
		return singleShippingRow(int64(1)), nil
	case strings.HasPrefix(q, "SELECT id FROM auth_sessions"):
		if args[1].Value != "owner" || f.revoked {
			return &shippingRows{cols: []string{"id"}}, nil
		}
		return &shippingRows{cols: []string{"id"}, values: [][]driver.Value{{"session"}}}, nil
	case strings.HasPrefix(q, "SELECT (SELECT COUNT"):
		for _, a := range args {
			if a.Value != "owner" {
				return nil, errors.New("wrong subject")
			}
		}
		return singleShippingRow(f.blocked), nil
	case strings.HasPrefix(q, "SELECT COUNT(*) FROM auth_sessions"):
		n := int64(1)
		if f.revoked || f.closed && strings.Contains(q, "account_closures") {
			n = 0
		}
		return singleShippingRow(n), nil
	case strings.HasPrefix(q, "SELECT COUNT(*) FROM account_closures"):
		n := int64(0)
		if f.closed {
			n = 1
		}
		return singleShippingRow(n), nil
	}
	return nil, errors.New("unexpected query: " + q)
}
func (f *personalFixture) ExecContext(_ context.Context, q string, args []driver.NamedValue) (driver.Result, error) {
	if !f.active {
		return nil, errors.New("write outside transaction")
	}
	if strings.Contains(q, f.fail) && f.fail != "" {
		return nil, errors.New("storage unavailable")
	}
	if strings.Contains(q, "WHERE openid=?") && args[len(args)-1].Value != "owner" {
		return nil, errors.New("cross-account write")
	}
	f.pending = append(f.pending, q)
	return driver.RowsAffected(1), nil
}
func TestPersonalDataDeletionAndClosure(t *testing.T) {
	cases := []struct {
		name, action, payload, fail string
		blocked                     int64
		wantError, closed           bool
	}{
		{name: "address only", action: "account.data.delete", payload: `{"scope":"addresses","confirm":"删除个人数据","openid":"victim"}`},
		{name: "usage", action: "account.data.delete", payload: `{"scope":"usage","confirm":"删除个人数据"}`},
		{name: "invalid scope", action: "account.data.delete", payload: `{"scope":"all","confirm":"删除个人数据"}`, wantError: true},
		{name: "confirmation required", action: "account.close", payload: `{}`, wantError: true},
		{name: "blocked", action: "account.close", payload: `{"confirm":"注销账号"}`, blocked: 1, wantError: true},
		{name: "rollback", action: "account.close", payload: `{"confirm":"注销账号"}`, fail: "account_closures", wantError: true},
		{name: "close", action: "account.close", payload: `{"confirm":"注销账号"}`, closed: true},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			f := &personalFixture{blocked: c.blocked, fail: c.fail}
			db := sql.OpenDB(personalConnector{f})
			defer db.Close()
			s := &Store{db: db}
			_, err := s.personalData(context.Background(), Claims{Subject: "owner", SessionID: "session"}, c.action, json.RawMessage(c.payload))
			if (err != nil) != c.wantError {
				t.Fatalf("unexpected error %v", err)
			}
			if c.wantError && len(f.writes) != 0 {
				t.Fatal("failed operation committed writes")
			}
			if f.closed != c.closed || f.revoked != c.closed {
				t.Fatal("closure and revocation must commit together")
			}
			if c.name == "address only" && (len(f.writes) != 1 || !strings.HasPrefix(f.writes[0], "DELETE FROM addresses")) {
				t.Fatal("address deletion changed unrelated data")
			}
			if c.closed {
				if err := s.checkSession(httptest.NewRequest("POST", "/api", nil), Claims{Subject: "owner", Kind: "mini", SessionID: "session"}); err == nil {
					t.Fatal("closed account session accepted")
				}
				if _, err = s.personalData(context.Background(), Claims{Subject: "owner", SessionID: "session"}, "account.close", json.RawMessage(c.payload)); err == nil {
					t.Fatal("revoked session accepted")
				}
				found, err := s.closedAccount(context.Background(), "owner")
				if err != nil || !found {
					t.Fatal("missing durable closure")
				}
			}
		})
	}
}
