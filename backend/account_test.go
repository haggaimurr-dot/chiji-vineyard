package main

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"encoding/json"
	"errors"
	"io"
	"net/http/httptest"
	"sort"
	"strings"
	"testing"
	"time"
)

// This driver checks that account changes and password-safe audit details share
// a transaction, while requireAdmin observes only committed credential changes.
type accountFixture struct {
	configs        map[string]string
	workingConfigs map[string]string
	accounts       map[string]Admin
	working        map[string]Admin
	audits         []string
	pendingAudits  []string
	failAudit      bool
	locked         bool
}

type accountConnector struct{ state *accountFixture }

func (c accountConnector) Connect(context.Context) (driver.Conn, error) { return c.state, nil }
func (c accountConnector) Driver() driver.Driver                        { return accountDriver{c.state} }

type accountDriver struct{ state *accountFixture }

func (d accountDriver) Open(string) (driver.Conn, error) { return d.state, nil }
func (d *accountFixture) Close() error                   { return nil }
func (d *accountFixture) Prepare(string) (driver.Stmt, error) {
	return nil, errors.New("unexpected prepare")
}
func (d *accountFixture) Begin() (driver.Tx, error) {
	d.working = make(map[string]Admin, len(d.accounts))
	for id, a := range d.accounts {
		d.working[id] = a
	}
	d.workingConfigs = map[string]string{}
	for k, v := range d.configs {
		d.workingConfigs[k] = v
	}
	d.pendingAudits = nil
	d.locked = false
	return d, nil
}
func (d *accountFixture) Commit() error {
	d.accounts, d.working = d.working, nil
	d.configs, d.workingConfigs = d.workingConfigs, nil
	d.audits = append(d.audits, d.pendingAudits...)
	d.pendingAudits = nil
	return nil
}
func (d *accountFixture) Rollback() error {
	d.working, d.pendingAudits = nil, nil
	d.workingConfigs = nil
	return nil
}

type accountRows struct {
	columns []string
	values  [][]driver.Value
}

func (r *accountRows) Columns() []string { return r.columns }
func (r *accountRows) Close() error      { return nil }
func (r *accountRows) Next(out []driver.Value) error {
	if len(r.values) == 0 {
		return io.EOF
	}
	copy(out, r.values[0])
	r.values = r.values[1:]
	return nil
}
func (d *accountFixture) QueryContext(_ context.Context, query string, args []driver.NamedValue) (driver.Rows, error) {
	switch query {
	case "SELECT COUNT(*) FROM auth_sessions WHERE id=? AND subject=? AND kind=? AND revoked_at IS NULL AND expires_at>?":
		return &accountRows{columns: []string{"count"}, values: [][]driver.Value{{int64(1)}}}, nil
	case "SELECT id,username,password_hash,role,active FROM admins WHERE username=?":
		r := &accountRows{columns: []string{"id", "username", "password_hash", "role", "active"}}
		for _, a := range d.accounts {
			if strings.EqualFold(a.Username, args[0].Value.(string)) {
				r.values = [][]driver.Value{{a.ID, a.Username, a.PasswordHash, a.Role, a.Active}}
				break
			}
		}
		return r, nil

	case "SELECT attempts,GREATEST(1,CEIL(TIMESTAMPDIFF(MICROSECOND,NOW(3),expires_at)/1000000)) FROM login_limits WHERE bucket_key=?":
		return &accountRows{columns: []string{"attempts", "retry"}, values: [][]driver.Value{{int64(1), int64(900)}}}, nil
	case "SELECT id,username,password_hash,role,active FROM admins WHERE id=? FOR UPDATE":
		if d.working == nil {
			return nil, errors.New("lock outside transaction")
		}
		d.locked = true
		r := &accountRows{columns: []string{"id", "username", "password_hash", "role", "active"}}
		if a, ok := d.working[args[0].Value.(string)]; ok {
			r.values = [][]driver.Value{{a.ID, a.Username, a.PasswordHash, a.Role, a.Active}}
		}
		return r, nil
	case "SELECT payload FROM admin_security WHERE admin_id=? FOR UPDATE":
		if !d.locked {
			return nil, errors.New("security read before account lock")
		}
		r := &accountRows{columns: []string{"payload"}}
		if raw, ok := d.workingConfigs["security:"+args[0].Value.(string)]; ok {
			r.values = [][]driver.Value{{raw}}
		}
		return r, nil

	case "SELECT payload FROM configs WHERE config_key=?":
		r := &accountRows{columns: []string{"payload"}}
		if value, ok := d.configs[args[0].Value.(string)]; ok {
			r.values = [][]driver.Value{{[]byte(value)}}
		}
		return r, nil
	case "SELECT id,username,password_hash,role,active FROM admins ORDER BY id FOR UPDATE":
		if d.working == nil {
			return nil, errors.New("account lock outside transaction")
		}
		d.locked = true
		r := &accountRows{columns: []string{"id", "username", "password_hash", "role", "active"}}
		ids := []string{}
		for id := range d.working {
			ids = append(ids, id)
		}
		sort.Strings(ids)
		for _, id := range ids {
			a := d.working[id]
			r.values = append(r.values, []driver.Value{a.ID, a.Username, a.PasswordHash, a.Role, a.Active})
		}
		return r, nil
	case "SELECT active,role,username,password_hash FROM admins WHERE id=?":
		r := &accountRows{columns: []string{"active", "role", "username", "password_hash"}}
		if a, ok := d.accounts[args[0].Value.(string)]; ok {
			r.values = [][]driver.Value{{a.Active, a.Role, a.Username, a.PasswordHash}}
		}
		return r, nil
	case "SELECT id,username,role,active,created_at,last_login_at FROM admins ORDER BY created_at,id":
		r := &accountRows{columns: []string{"id", "username", "role", "active", "created_at", "last_login_at"}}
		for _, a := range d.accounts {
			r.values = append(r.values, []driver.Value{a.ID, a.Username, a.Role, a.Active, time.Now(), nil})
		}
		return r, nil
	default:
		return nil, errors.New("unexpected account query: " + query)
	}
}
func (d *accountFixture) ExecContext(_ context.Context, query string, args []driver.NamedValue) (driver.Result, error) {
	if strings.HasPrefix(query, "INSERT INTO login_limits") || strings.HasPrefix(query, "DELETE FROM login_limits") {
		return driver.RowsAffected(1), nil
	}

	if d.working == nil || !d.locked {
		return nil, errors.New("account mutation must follow transactional lock")
	}
	switch {
	case strings.HasPrefix(query, "INSERT INTO auth_sessions"):
		// Session persistence is exercised by sessions_test.go.
	case strings.HasPrefix(query, "INSERT INTO admin_security("):
		d.workingConfigs["security:"+args[0].Value.(string)] = args[1].Value.(string)
	case strings.HasPrefix(query, "INSERT INTO configs("):
		d.workingConfigs[args[0].Value.(string)] = args[1].Value.(string)
	case strings.HasPrefix(query, "INSERT INTO admins("):
		a := Admin{ID: args[0].Value.(string), Username: args[1].Value.(string), PasswordHash: args[2].Value.(string), Role: args[3].Value.(string), Active: args[4].Value.(bool)}
		for _, other := range d.working {
			if strings.EqualFold(other.Username, a.Username) {
				return nil, errors.New("duplicate username escaped validation")
			}
		}
		d.working[a.ID] = a
	case strings.HasPrefix(query, "UPDATE admins SET username="):
		id := args[4].Value.(string)
		a, ok := d.working[id]
		if !ok {
			return nil, errors.New("missing account escaped validation")
		}
		a.Username, a.PasswordHash, a.Role, a.Active = args[0].Value.(string), args[1].Value.(string), args[2].Value.(string), args[3].Value.(bool)
		d.working[id] = a
	case strings.HasPrefix(query, "UPDATE admins SET password_hash="):
		id := args[1].Value.(string)
		a, ok := d.working[id]
		if !ok {
			return nil, errors.New("missing password target escaped validation")
		}
		a.PasswordHash = args[0].Value.(string)
		d.working[id] = a
	case strings.HasPrefix(query, "INSERT INTO audit_logs("):
		if d.failAudit {
			return nil, errors.New("audit unavailable")
		}
		d.pendingAudits = append(d.pendingAudits, args[4].Value.(string))
	default:
		return nil, errors.New("unexpected account mutation: " + query)
	}
	return driver.RowsAffected(1), nil
}

func newAccountServer(t *testing.T) (*Server, *accountFixture, Claims) {
	t.Helper()
	t.Setenv("TOKEN_SECRET", strings.Repeat("account-test-secret", 3))
	state := &accountFixture{accounts: map[string]Admin{
		"super":    {ID: "super", Username: "alice", PasswordHash: "seed-super-hash", Role: "SUPER_ADMIN", Active: true},
		"operator": {ID: "operator", Username: "bob", PasswordHash: "seed-operator-hash", Role: "OPERATOR", Active: true},
	}}
	db := sql.OpenDB(accountConnector{state})
	db.SetMaxOpenConns(1)
	t.Cleanup(func() { _ = db.Close() })
	actor := Claims{Subject: "super", Username: "alice", Role: "SUPER_ADMIN", Credential: adminCredential(state.accounts["super"].PasswordHash)}
	return &Server{store: &Store{db: db}}, state, actor
}

func TestAccountSaveRejectsUnsafeChanges(t *testing.T) {
	tests := []struct {
		name    string
		account M
		message string
	}{
		{"duplicate create", M{"username": "ALICE", "password": "new-password-123", "role": "OPERATOR", "active": true}, "用户名已存在"},
		{"duplicate rename", M{"id": "operator", "username": "alice", "role": "OPERATOR", "active": true}, "用户名已存在"},
		{"missing explicit ID", M{"id": "missing", "username": "charlie", "password": "new-password-123", "role": "OPERATOR", "active": true}, "账号不存在"},
		{"self deactivation", M{"id": "super", "username": "alice", "role": "SUPER_ADMIN", "active": false}, "不能停用当前账号"},
		{"last super demotion", M{"id": "super", "username": "alice", "role": "OPERATOR", "active": true}, "保留至少一个"},
		{"short password", M{"username": "charlie", "password": "short", "role": "OPERATOR", "active": true}, "至少 10 位"},
		{"new account needs password", M{"username": "charlie", "role": "OPERATOR", "active": true}, "必须设置"},
		{"unsupported role", M{"username": "charlie", "password": "new-password-123", "role": "UNKNOWN", "active": true}, "角色无效"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			s, state, actor := newAccountServer(t)
			state.accounts["inactive-super"] = Admin{ID: "inactive-super", Username: "disabled", Role: "SUPER_ADMIN", Active: false}
			_, err := s.saveAccount(context.Background(), actor, json.RawMessage(jsonValue(M{"account": tc.account})))
			if err == nil || !strings.Contains(err.Error(), tc.message) {
				t.Fatalf("expected %q, got %v", tc.message, err)
			}
			if len(state.accounts) != 3 || state.accounts["super"].Role != "SUPER_ADMIN" || !state.accounts["super"].Active || state.accounts["operator"].Username != "bob" || len(state.audits) != 0 {
				t.Fatalf("rejected operation changed state: %+v", state)
			}
		})
	}
}

func TestAccountCreateAndStatusPreserveCredentials(t *testing.T) {
	s, state, actor := newAccountServer(t)
	raw := json.RawMessage(jsonValue(M{"account": M{"username": "charlie", "password": "new-password-123", "role": "OPERATOR"}}))
	result, err := s.saveAccount(context.Background(), actor, raw)
	if err != nil {
		t.Fatal(err)
	}
	id := result.(M)["id"].(string)
	a := state.accounts[id]
	if !a.Active || a.PasswordHash == "new-password-123" || !verifyPassword("new-password-123", a.PasswordHash) {
		t.Fatalf("invalid persisted account: %+v", a)
	}
	hash := a.PasswordHash
	_, err = s.saveAccount(context.Background(), actor, json.RawMessage(jsonValue(M{"account": M{"id": id, "username": "charlie", "role": "OPERATOR", "active": false}})))
	if err != nil || state.accounts[id].Active || state.accounts[id].PasswordHash != hash {
		t.Fatalf("status update changed credentials: %v", err)
	}
	list, err := s.accounts(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	for _, value := range []string{jsonValue(result), jsonValue(list), strings.Join(state.audits, "")} {
		if strings.Contains(value, "new-password-123") || strings.Contains(value, hash) || strings.Contains(value, "password_hash") {
			t.Fatalf("credentials leaked: %s", value)
		}
	}
}

func TestAccountCanDeactivateAnotherSuperWhenOneRemains(t *testing.T) {
	s, state, actor := newAccountServer(t)
	state.accounts["second"] = Admin{ID: "second", Username: "second", Role: "SUPER_ADMIN", Active: true, PasswordHash: "second-hash"}
	_, err := s.saveAccount(context.Background(), actor, json.RawMessage(jsonValue(M{"account": M{"id": "second", "username": "second", "role": "SUPER_ADMIN", "active": false}})))
	if err != nil || state.accounts["second"].Active || !state.accounts["super"].Active {
		t.Fatalf("valid deactivation failed: %v", err)
	}
}

func TestAccountPasswordResetRevokesAllOldTokens(t *testing.T) {
	for _, id := range []string{"operator", "super"} {
		t.Run(id, func(t *testing.T) {
			s, state, actor := newAccountServer(t)
			previous := state.accounts[id]
			oldToken, err := adminToken(previous)
			if err != nil {
				t.Fatal(err)
			}
			r := httptest.NewRequest("POST", "/api", nil)
			r.Header.Set("Authorization", oldToken)
			if _, err := s.store.requireAdmin(r); err != nil {
				t.Fatalf("initial session invalid: %v", err)
			}
			result, err := s.resetAccountPassword(context.Background(), actor, json.RawMessage(jsonValue(M{"accountId": id, "password": "replacement-password"})))
			if err != nil {
				t.Fatal(err)
			}
			if result.(M)["sessionRevoked"] != true || result.(M)["currentSessionRevoked"] != (id == actor.Subject) {
				t.Fatal(result)
			}
			updated := state.accounts[id]
			if updated.Username != previous.Username || updated.Role != previous.Role || updated.Active != previous.Active || !verifyPassword("replacement-password", updated.PasswordHash) {
				t.Fatal("reset changed identity/status or did not hash password")
			}
			if _, err := s.store.requireAdmin(r); err == nil {
				t.Fatal("old session survived password reset")
			}
			newToken, _ := adminToken(updated)
			r.Header.Set("Authorization", newToken)
			if _, err := s.store.requireAdmin(r); err != nil {
				t.Fatalf("new session invalid: %v", err)
			}
			if strings.Contains(jsonValue(result), updated.PasswordHash) || strings.Contains(strings.Join(state.audits, ""), "replacement-password") || strings.Contains(newToken, updated.PasswordHash) {
				t.Fatal("reset leaked credentials")
			}
		})
	}
}

func TestAccountAuditFailureRollsBackSaveAndReset(t *testing.T) {
	for _, operation := range []string{"create", "reset"} {
		t.Run(operation, func(t *testing.T) {
			s, state, actor := newAccountServer(t)
			state.failAudit = true
			oldHash := state.accounts["operator"].PasswordHash
			var err error
			if operation == "create" {
				_, err = s.saveAccount(context.Background(), actor, json.RawMessage(jsonValue(M{"account": M{"username": "charlie", "password": "new-password-123", "role": "OPERATOR", "active": true}})))
			} else {
				_, err = s.resetAccountPassword(context.Background(), actor, json.RawMessage(jsonValue(M{"accountId": "operator", "password": "replacement-password"})))
			}
			if err == nil || len(state.accounts) != 2 || state.accounts["operator"].PasswordHash != oldHash || len(state.audits) != 0 {
				t.Fatalf("partial change escaped rollback: %v %+v", err, state)
			}
		})
	}
}

func TestAccountResetKeepsInactiveAccountDisabled(t *testing.T) {
	s, state, actor := newAccountServer(t)
	account := state.accounts["operator"]
	account.Active = false
	state.accounts[account.ID] = account
	_, err := s.resetAccountPassword(context.Background(), actor, json.RawMessage(jsonValue(M{"accountId": account.ID, "password": "replacement-password"})))
	if err != nil || state.accounts[account.ID].Active || !verifyPassword("replacement-password", state.accounts[account.ID].PasswordHash) {
		t.Fatalf("reset must preserve deactivation: %v", err)
	}
	token, _ := adminToken(state.accounts[account.ID])
	r := httptest.NewRequest("POST", "/api", nil)
	r.Header.Set("Authorization", token)
	if _, err = s.store.requireAdmin(r); err == nil {
		t.Fatal("inactive account gained access through reset")
	}
}

func TestAccountMutationRechecksActorUnderLock(t *testing.T) {
	for _, change := range []string{"inactive", "demoted", "reset"} {
		t.Run(change, func(t *testing.T) {
			s, state, actor := newAccountServer(t)
			a := state.accounts["super"]
			switch change {
			case "inactive":
				a.Active = false
			case "demoted":
				a.Role = "OPERATOR"
			case "reset":
				a.PasswordHash = "new-super-hash"
			}
			state.accounts[a.ID] = a
			_, err := s.resetAccountPassword(context.Background(), actor, json.RawMessage(jsonValue(M{"accountId": "operator", "password": "replacement-password"})))
			if err == nil || len(state.audits) != 0 || state.accounts["operator"].PasswordHash != "seed-operator-hash" {
				t.Fatalf("stale actor authorized: %v", err)
			}
		})
	}
}

func TestAccountPasswordBoundariesAndMissingResetTarget(t *testing.T) {
	for _, password := range []string{"", "123456789", strings.Repeat("长", 129)} {
		if _, err := hashPassword(password); err == nil {
			t.Fatal("invalid password accepted")
		}
	}
	for _, password := range []string{"1234567890", strings.Repeat("长", 128)} {
		hash, err := hashPassword(password)
		if err != nil || !verifyPassword(password, hash) {
			t.Fatalf("valid password rejected: %v", err)
		}
	}
	s, state, actor := newAccountServer(t)
	for _, id := range []string{"", "missing"} {
		if _, err := s.resetAccountPassword(context.Background(), actor, json.RawMessage(jsonValue(M{"accountId": id, "password": "replacement-password"}))); err == nil {
			t.Fatal("missing reset target accepted")
		}
	}
	if len(state.accounts) != 2 || len(state.audits) != 0 {
		t.Fatal("missing reset target changed state")
	}
}

func TestAdminSessionWithoutCredentialIsRejected(t *testing.T) {
	s, _, _ := newAccountServer(t)
	oldToken, err := signToken(Claims{Subject: "super", Kind: "admin", Role: "SUPER_ADMIN", Exp: time.Now().Add(time.Hour).Unix()})
	if err != nil {
		t.Fatal(err)
	}
	r := httptest.NewRequest("POST", "/api", nil)
	r.Header.Set("Authorization", oldToken)
	if _, err := s.store.requireAdmin(r); err == nil {
		t.Fatal("legacy token without a credential binding accepted")
	}
}
