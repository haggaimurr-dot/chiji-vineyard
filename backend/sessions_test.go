package main

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"encoding/json"
	"errors"
	"net/http/httptest"
	"sort"
	"strings"
	"testing"
	"time"
)

type storedSession struct {
	loginSession
	subject, kind, credential string
	revoked                   bool
}
type sessionFixture struct {
	*accountFixture
	sessions, pending    map[string]storedSession
	failRead, failInsert bool
}
type sessionConnector struct{ f *sessionFixture }
type sessionDriver struct{ f *sessionFixture }

func (c sessionConnector) Connect(context.Context) (driver.Conn, error) { return c.f, nil }
func (c sessionConnector) Driver() driver.Driver                        { return sessionDriver{c.f} }
func (d sessionDriver) Open(string) (driver.Conn, error)                { return d.f, nil }
func (f *sessionFixture) Begin() (driver.Tx, error) {
	if _, err := f.accountFixture.Begin(); err != nil {
		return nil, err
	}
	f.pending = map[string]storedSession{}
	for k, v := range f.sessions {
		f.pending[k] = v
	}
	return f, nil
}
func (f *sessionFixture) Commit() error {
	if err := f.accountFixture.Commit(); err != nil {
		return err
	}
	f.sessions, f.pending = f.pending, nil
	return nil
}
func (f *sessionFixture) Rollback() error { f.pending = nil; return f.accountFixture.Rollback() }
func (f *sessionFixture) ExecContext(ctx context.Context, q string, args []driver.NamedValue) (driver.Result, error) {
	if strings.HasPrefix(q, "INSERT INTO auth_sessions") {
		if f.failInsert {
			return nil, errors.New("insert unavailable")
		}
		s := storedSession{subject: args[1].Value.(string), kind: args[2].Value.(string), credential: args[3].Value.(string)}
		s.ID, s.UserAgent, s.IPAddress = args[0].Value.(string), args[4].Value.(string), args[5].Value.(string)
		s.CreatedAt, s.ExpiresAt = args[6].Value.(int64), args[7].Value.(int64)
		f.pending[s.ID] = s
		return driver.RowsAffected(1), nil
	}
	if strings.HasPrefix(q, "UPDATE auth_sessions") {
		id := args[1].Value.(string)
		s, ok := f.pending[id]
		if !ok || s.subject != args[2].Value || s.kind != args[3].Value {
			return driver.RowsAffected(0), nil
		}
		s.revoked = true
		f.pending[id] = s
		return driver.RowsAffected(1), nil
	}
	// Audits must use the same transaction as session mutations.
	if strings.HasPrefix(q, "INSERT INTO audit_logs") {
		f.locked = true
	}
	return f.accountFixture.ExecContext(ctx, q, args)
}
func (f *sessionFixture) QueryContext(ctx context.Context, q string, args []driver.NamedValue) (driver.Rows, error) {
	if strings.Contains(q, "FROM auth_sessions") {
		if f.failRead {
			return nil, errors.New("read unavailable")
		}
		if strings.HasPrefix(q, "SELECT COUNT(*)") {
			s, ok := f.sessions[args[0].Value.(string)]
			count := int64(0)
			if ok && s.subject == args[1].Value && s.kind == args[2].Value && !s.revoked && s.ExpiresAt > args[3].Value.(int64) {
				count = 1
			}
			return &accountRows{columns: []string{"count"}, values: [][]driver.Value{{count}}}, nil
		}
		items := []storedSession{}
		for _, s := range f.sessions {
			if s.subject == args[0].Value && s.kind == args[1].Value && s.credential == args[2].Value && !s.revoked && s.ExpiresAt > args[3].Value.(int64) {
				items = append(items, s)
			}
		}
		sort.Slice(items, func(i, j int) bool {
			if items[i].CreatedAt == items[j].CreatedAt {
				return items[i].ID > items[j].ID
			}
			return items[i].CreatedAt > items[j].CreatedAt
		})
		rows := &accountRows{columns: []string{"id", "ua", "ip", "created", "expires"}}
		start := min(len(items), int(args[5].Value.(int64)))
		end := min(len(items), start+int(args[4].Value.(int64)))
		for _, s := range items[start:end] {
			rows.values = append(rows.values, []driver.Value{s.ID, s.UserAgent, s.IPAddress, s.CreatedAt, s.ExpiresAt})
		}
		return rows, nil
	}
	return f.accountFixture.QueryContext(ctx, q, args)
}

func TestPersonalSessions(t *testing.T) {
	t.Setenv("TOKEN_SECRET", strings.Repeat("s", 32))
	f := &sessionFixture{accountFixture: &accountFixture{accounts: map[string]Admin{"a": {ID: "a", Username: "alice", Active: true, Role: "OPERATOR", PasswordHash: "hash"}}}, sessions: map[string]storedSession{}}
	db := sql.OpenDB(sessionConnector{f})
	db.SetMaxOpenConns(1)
	defer db.Close()
	s := &Server{store: &Store{db: db}}
	r := httptest.NewRequest("POST", "/api", nil)
	r.RemoteAddr = "192.0.2.1:123"
	r.Header.Set("User-Agent", "test device")
	mint := func(kind, subject string) (string, Claims) {
		t.Helper()
		c := Claims{Subject: subject, Kind: kind, Exp: time.Now().Add(time.Hour).Unix()}
		if kind == "admin" {
			c.Credential = adminCredential("hash")
			c.Username = "alice"
		}
		token, err := s.issueSession(r, c)
		if err != nil {
			t.Fatal(err)
		}
		c, err = verifyToken(token, kind)
		if err != nil {
			t.Fatal(err)
		}
		return token, c
	}
	first, a := mint("admin", "a")
	_, b := mint("admin", "a")
	_, other := mint("mini", "a")
	_, foreign := mint("mini", "another")
	f.accounts["another"] = Admin{ID: "another", Username: "bob", Active: true, Role: "OPERATOR", PasswordHash: "hash"}
	_, foreignAdmin := mint("admin", "another")
	call := func(actor Claims, action string, data M) (any, error) {
		return s.personalSessions(r, actor, Request{Action: action, Data: json.RawMessage(jsonValue(data))})
	}
	listed, err := call(a, "admin.session.list", M{"limit": 1})
	if err != nil {
		t.Fatal(err)
	}
	page := listed.(M)
	if !page["hasMore"].(bool) || len(page["items"].([]loginSession)) != 1 {
		t.Fatalf("bad pagination: %v", page)
	}
	listed, err = call(a, "admin.session.list", M{})
	if err != nil {
		t.Fatal(err)
	}
	items := listed.(M)["items"].([]loginSession)
	current := 0
	for _, item := range items {
		if item.Current {
			current++
		}
		if item.UserAgent != "test device" || item.IPAddress != "192.0.2.1" {
			t.Fatalf("bad metadata: %v", item)
		}
	}
	if len(items) != 2 || current != 1 {
		t.Fatalf("owner/kind/current isolation: %v", items)
	}
	if _, err = call(a, "admin.session.revoke", M{"sessionId": foreign.SessionID}); err != nil {
		t.Fatal(err)
	}
	if err = s.store.checkSession(r, foreign); err != nil {
		t.Fatal("foreign session revoked", err)
	}
	if _, err = call(a, "admin.session.revoke", M{"sessionId": foreignAdmin.SessionID}); err != nil {
		t.Fatal(err)
	}
	if err = s.store.checkSession(r, foreignAdmin); err != nil {
		t.Fatal("another admin's session revoked", err)
	}
	f.failAudit = true
	if _, err = call(a, "admin.session.revoke", M{"sessionId": b.SessionID}); err == nil {
		t.Fatal("audit failure accepted")
	}
	if err = s.store.checkSession(r, b); err != nil {
		t.Fatal("audit rollback lost session", err)
	}
	f.failAudit = false
	if _, err = call(a, "admin.session.revoke", M{"sessionId": b.SessionID}); err != nil {
		t.Fatal(err)
	}
	if err = s.store.checkSession(r, b); err == nil {
		t.Fatal("revoked session accepted")
	}
	r.Header.Set("Authorization", first)
	if _, err = s.store.requireAdmin(r); err != nil {
		t.Fatal("other device affected", err)
	}
	if _, err = call(a, "admin.logout", M{"sessionId": other.SessionID}); err != nil {
		t.Fatal(err)
	}
	if _, err = s.store.requireAdmin(r); err == nil {
		t.Fatal("logout token accepted")
	}
	if err = s.store.checkSession(r, other); err != nil {
		t.Fatal("logout affected mini session", err)
	}
	if _, err = call(other, "auth.logout", M{}); err != nil {
		t.Fatal(err)
	}
	if err = s.store.checkSession(r, other); err == nil {
		t.Fatal("mini logout ignored")
	}
	_, live := mint("mini", "live")
	f.failRead = true
	if err = s.store.checkSession(r, live); err == nil {
		t.Fatal("DB outage bypassed validation")
	}
	f.failRead = false
	expired := f.sessions[live.SessionID]
	expired.ExpiresAt = time.Now().Unix()
	f.sessions[live.SessionID] = expired
	if err = s.store.checkSession(r, live); err == nil {
		t.Fatal("expired session accepted")
	}
	if err = s.store.checkSession(r, Claims{}); err == nil {
		t.Fatal("legacy token accepted")
	}
	if _, err = call(a, "admin.session.revoke", M{"sessionId": "invalid"}); err == nil {
		t.Fatal("invalid session ID accepted")
	}
	if token, err := s.issueSession(r, Claims{Subject: "a", Kind: "admin", Credential: "obsolete", Exp: time.Now().Add(time.Hour).Unix()}); err == nil || token != "" {
		t.Fatal("obsolete admin credentials issued a session")
	}
	f.failInsert = true
	if token, err := s.issueSession(r, Claims{Subject: "new", Kind: "mini", Exp: time.Now().Add(time.Hour).Unix()}); err == nil || token != "" {
		t.Fatal("session insert failure issued token")
	}
	if _, err = call(a, "admin.session.list", M{"limit": 101}); err == nil {
		t.Fatal("unbounded pagination accepted")
	}
}
