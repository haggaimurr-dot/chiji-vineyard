package main

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"encoding/json"
	"errors"
	"fmt"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"
)

type limitBucket struct {
	attempts int64
	expiry   time.Time
}
type limitFixture struct {
	mu              sync.Mutex
	buckets         map[string]limitBucket
	fail            bool
	account         *Admin
	passwordQueries int
}
type limitConnector struct{ state *limitFixture }

func (c limitConnector) Connect(context.Context) (driver.Conn, error) { return c.state, nil }
func (c limitConnector) Driver() driver.Driver                        { return limitDriver{c.state} }

type limitDriver struct{ state *limitFixture }

func (d limitDriver) Open(string) (driver.Conn, error) { return d.state, nil }
func (*limitFixture) Close() error                     { return nil }
func (*limitFixture) Prepare(string) (driver.Stmt, error) {
	return nil, errors.New("unexpected prepare")
}
func (*limitFixture) Begin() (driver.Tx, error) { return nil, errors.New("unexpected transaction") }
func (d *limitFixture) ExecContext(_ context.Context, query string, args []driver.NamedValue) (driver.Result, error) {
	d.mu.Lock()
	defer d.mu.Unlock()
	if d.fail {
		return nil, errors.New("database unavailable")
	}
	switch {
	case strings.HasPrefix(query, "INSERT INTO login_limits"):
		key := args[0].Value.(string)
		b := d.buckets[key]
		if !b.expiry.After(time.Now()) {
			b = limitBucket{expiry: time.Now().Add(time.Duration(args[1].Value.(int64)) * time.Second)}
		}
		b.attempts = min(b.attempts+1, args[2].Value.(int64))
		d.buckets[key] = b
	case strings.HasPrefix(query, "DELETE FROM login_limits"):
		for k, b := range d.buckets {
			if !b.expiry.After(time.Now()) {
				delete(d.buckets, k)
			}
		}
	default:
		return nil, fmt.Errorf("unexpected limiter mutation: %s", query)
	}
	return driver.RowsAffected(1), nil
}
func (d *limitFixture) QueryContext(_ context.Context, query string, args []driver.NamedValue) (driver.Rows, error) {
	d.mu.Lock()
	defer d.mu.Unlock()
	if d.fail {
		return nil, errors.New("database unavailable")
	}
	if strings.HasPrefix(query, "SELECT attempts,") {
		b := d.buckets[args[0].Value.(string)]
		return &accountRows{columns: []string{"attempts", "retry"}, values: [][]driver.Value{{b.attempts, int64(max(1, int(time.Until(b.expiry).Seconds())+1))}}}, nil
	}
	if strings.HasPrefix(query, "SELECT id,username,password_hash,role,active FROM admins WHERE username=") {
		d.passwordQueries++
		rows := &accountRows{columns: []string{"id", "username", "password_hash", "role", "active"}}
		if d.account != nil {
			a := d.account
			rows.values = [][]driver.Value{{a.ID, a.Username, a.PasswordHash, a.Role, a.Active}}
		}
		return rows, nil
	}
	return nil, fmt.Errorf("unexpected limiter read: %s", query)
}
func limitServer(t *testing.T) (*Server, *limitFixture) {
	t.Helper()
	f := &limitFixture{buckets: map[string]limitBucket{}}
	db := sql.OpenDB(limitConnector{f})
	t.Cleanup(func() { db.Close() })
	return &Server{store: &Store{db: db}}, f
}
func identityPost(s *Server, action, username, ip string) *httptest.ResponseRecorder {
	body := jsonValue(M{"action": action, "data": M{"username": username, "password": "wrong-password"}})
	r := httptest.NewRequest("POST", "/api", strings.NewReader(body))
	r.RemoteAddr = ip
	w := httptest.NewRecorder()
	s.api(w, r)
	return w
}

func TestLoginLimitsAccountSourceExpiryAndFailure(t *testing.T) {
	t.Run("account aliases and expiry", func(t *testing.T) {
		s, f := limitServer(t)
		for i := 0; i < 5; i++ {
			if w := identityPost(s, "admin.login", " Owner ", "192.0.2.1:123"); w.Code != 400 {
				t.Fatalf("attempt %d: %d %s", i, w.Code, w.Body)
			}
		}
		w := identityPost(s, "admin.login", "OWNER", "192.0.2.2:123")
		if w.Code != 429 || w.Header().Get("Retry-After") == "" || !strings.Contains(w.Body.String(), "LOGIN_RATE_LIMITED") || f.passwordQueries != 5 {
			t.Fatalf("limit failed: %d %s queries=%d", w.Code, w.Body, f.passwordQueries)
		}
		key := limitKey("admin-account", "owner")
		b := f.buckets[key]
		b.expiry = time.Now().Add(-time.Second)
		f.buckets[key] = b
		if w := identityPost(s, "admin.login", "owner", "192.0.2.2:123"); w.Code != 400 {
			t.Fatalf("expiry did not reset: %d", w.Code)
		}
	})
	t.Run("distributed source and forged forwarding", func(t *testing.T) {
		s, f := limitServer(t)
		for i := 0; i < 30; i++ {
			identityPost(s, "admin.login", fmt.Sprintf("user%d", i), "192.0.2.1:123")
		}
		w := identityPost(s, "admin.login", "different-user", "192.0.2.1:456")
		if w.Code != 429 || f.passwordQueries != 30 {
			t.Fatalf("source limit bypassed: %d queries=%d", w.Code, f.passwordQueries)
		}
		r := httptest.NewRequest("POST", "/api", nil)
		r.RemoteAddr = "192.0.2.1:123"
		r.Header.Set("X-Forwarded-For", "198.51.100.1")
		t.Setenv("LOGIN_TRUSTED_PROXY_CIDRS", "")
		if loginPeer(r) != "192.0.2.1" {
			t.Fatal("untrusted forwarded header used")
		}
		t.Setenv("LOGIN_TRUSTED_PROXY_CIDRS", "192.0.2.0/24")
		if loginPeer(r) != "198.51.100.1" {
			t.Fatal("trusted proxy not supported")
		}
		r.Header.Set("X-Forwarded-For", "203.0.113.1, 198.51.100.1")
		if loginPeer(r) != "198.51.100.1" {
			t.Fatal("forged leftmost header bypassed")
		}
	})
	t.Run("database fails closed", func(t *testing.T) {
		s, f := limitServer(t)
		f.fail = true
		w := identityPost(s, "admin.login", "owner", "192.0.2.1:123")
		if w.Code != 503 || f.passwordQueries != 0 {
			t.Fatalf("database failure bypassed limiter: %d", w.Code)
		}
	})
	t.Run("database collation aliases share resolved account", func(t *testing.T) {
		s, f := limitServer(t)
		f.account = &Admin{ID: "same-id", Username: "owner", Active: true}
		for i := 0; i < 5; i++ {
			identityPost(s, "admin.login", fmt.Sprintf("alias%d", i), fmt.Sprintf("192.0.2.%d:123", i+1))
		}
		w := identityPost(s, "admin.login", "another-alias", "192.0.2.9:123")
		if w.Code != 429 {
			t.Fatalf("resolved account limiter bypassed: %d", w.Code)
		}
	})
	t.Run("mini and bootstrap gates", func(t *testing.T) {
		s, _ := limitServer(t)
		r := httptest.NewRequest("POST", "/api", nil)
		r.RemoteAddr = "192.0.2.1:123"
		for _, action := range []string{"auth.login", "admin.bootstrap"} {
			for i := 0; i < 30; i++ {
				if e := s.checkLoginLimit(r, Request{Action: action}); e != nil {
					t.Fatal(e)
				}
			}
			var limit *loginLimitError
			if !errors.As(s.checkLoginLimit(r, Request{Action: action}), &limit) {
				t.Fatalf("%s unthrottled", action)
			}
		}
	})
}

func TestPermissionsRevokeLiveSessionAndProtectedRoutes(t *testing.T) {
	t.Setenv("TOKEN_SECRET", strings.Repeat("x", 32))
	s, f, actor := newAccountServer(t)
	staff := f.accounts["operator"]
	token, err := adminToken(staff)
	if err != nil {
		t.Fatal(err)
	}
	r := httptest.NewRequest("POST", "/api", nil)
	r.Header.Set("Authorization", token)
	c, err := s.store.requireAdmin(r)
	if err != nil {
		t.Fatal(err)
	}
	if err = s.store.authorizeAdmin(r, c, "admin.order.export"); err != nil {
		t.Fatalf("legacy grants lost: %v", err)
	}
	save := func(grants []string) error {
		_, e := s.saveAccountPermissions(context.Background(), actor, json.RawMessage(jsonValue(M{"accountId": staff.ID, "permissions": grants})))
		return e
	}
	if err = save([]string{"order.read"}); err != nil {
		t.Fatal(err)
	}
	if err = s.store.authorizeAdmin(r, c, "admin.order.list"); err != nil {
		t.Fatal(err)
	}
	for _, action := range []string{"admin.order.export", "admin.order.ship", "admin.refund.approve", "admin.media.upload", "admin.future.action"} {
		if !errors.Is(s.store.authorizeAdmin(r, c, action), errPermission) {
			t.Fatalf("revoked action allowed: %s", action)
		}
	}
	if err = save([]string{"product.write"}); err != nil {
		t.Fatal(err)
	}
	if !errors.Is(s.store.authorizeAdmin(r, c, "admin.product.save"), errPermission) {
		t.Fatal("product save bypasses inventory grant")
	}
	if err = save([]string{}); err != nil {
		t.Fatal(err)
	}
	for _, path := range []string{"/api/media/upload", "/admin/waybills/id/label"} {
		request := httptest.NewRequest("POST", path, nil)
		if strings.Contains(path, "waybills") {
			request.Method = "GET"
		}
		request.Header.Set("Authorization", token)
		w := httptest.NewRecorder()
		s.routes().ServeHTTP(w, request)
		if w.Code != 403 {
			t.Fatalf("protected route %s returned %d: %s", path, w.Code, w.Body)
		}
	}
	request := httptest.NewRequest("POST", "/api", strings.NewReader(`{"action":"admin.order.export","data":{}}`))
	request.Header.Set("Authorization", token)
	w := httptest.NewRecorder()
	s.api(w, request)
	if w.Code != 403 || !strings.Contains(w.Body.String(), "PERMISSION_DENIED") {
		t.Fatalf("bad denial: %d %s", w.Code, w.Body)
	}
	if err = save([]string{"order.read"}); err != nil {
		t.Fatal(err)
	}
	f.failAudit = true
	if err = save([]string{"order.export"}); err == nil {
		t.Fatal("audit failure committed grants")
	}
	grants, err := s.store.adminPermissions(context.Background(), staff.ID, staff.Role)
	if err != nil || len(grants) != 1 || grants[0] != "order.read" {
		t.Fatalf("rollback lost grants: %v %v", grants, err)
	}
	if err = save([]string{"super"}); err == nil {
		t.Fatal("invalid grant accepted")
	}
}
