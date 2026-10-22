package main

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"encoding/json"
	"errors"
	"io"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

type historyRecord struct {
	id, key, action, username, payload string
	version                            int64
	source                             driver.Value
}

// A small transactional driver verifies that snapshots, live configuration and
// audit entries are committed together, without an external MySQL dependency.
type historyDB struct {
	configs, workingConfigs map[string]string
	versions, working       []historyRecord
	audits, pendingAudits   int
	fail                    string
	queries                 []string
	queryArgs               [][]driver.NamedValue
	auditRead               [][]driver.Value
	products, articles      map[string]bool
	role                    string
	commits, rollbacks      int
}

type historyConnector struct{ state *historyDB }
type historyDriver struct{ state *historyDB }

func (c historyConnector) Connect(context.Context) (driver.Conn, error) { return c.state, nil }
func (c historyConnector) Driver() driver.Driver                        { return historyDriver{c.state} }
func (d historyDriver) Open(string) (driver.Conn, error)                { return d.state, nil }
func (d *historyDB) Prepare(string) (driver.Stmt, error) {
	return nil, errors.New("unexpected prepare")
}
func (d *historyDB) Close() error { return nil }
func (d *historyDB) BeginTx(context.Context, driver.TxOptions) (driver.Tx, error) {
	return d.Begin()
}
func (d *historyDB) Begin() (driver.Tx, error) {
	d.workingConfigs = map[string]string{}
	for key, raw := range d.configs {
		d.workingConfigs[key] = raw
	}
	d.working = append([]historyRecord{}, d.versions...)
	d.pendingAudits = 0
	return d, nil
}
func (d *historyDB) Commit() error {
	d.configs, d.versions = d.workingConfigs, d.working
	d.audits += d.pendingAudits
	d.commits++
	return nil
}
func (d *historyDB) Rollback() error {
	d.workingConfigs, d.working = nil, nil
	d.pendingAudits = 0
	d.rollbacks++
	return nil
}

type historyRows struct {
	columns []string
	rows    [][]driver.Value
}

func (r *historyRows) Columns() []string { return r.columns }
func (r *historyRows) Close() error      { return nil }
func (r *historyRows) Next(out []driver.Value) error {
	if len(r.rows) == 0 {
		return io.EOF
	}
	copy(out, r.rows[0])
	r.rows = r.rows[1:]
	return nil
}
func historyRow(columns []string, values ...driver.Value) driver.Rows {
	rows := [][]driver.Value{}
	if len(values) > 0 {
		rows = append(rows, values)
	}
	return &historyRows{columns: columns, rows: rows}
}

func (d *historyDB) QueryContext(_ context.Context, q string, args []driver.NamedValue) (driver.Rows, error) {
	d.queries = append(d.queries, q)
	d.queryArgs = append(d.queryArgs, append([]driver.NamedValue{}, args...))
	switch {
	case strings.HasPrefix(q, "SELECT active,role"):
		return historyRow([]string{"active", "role", "username", "password_hash"}, true, d.role, "tester", "hash"), nil
	case strings.HasPrefix(q, "SELECT COUNT(*) FROM auth_sessions"):
		return historyRow([]string{"count"}, int64(1)), nil
	case strings.HasPrefix(q, "SELECT payload FROM configs"):
		if !strings.HasSuffix(q, "FOR UPDATE") {
			return nil, errors.New("configuration must be locked")
		}
		key := args[0].Value.(string)
		return historyRow([]string{"payload"}, d.workingConfigs[key]), nil
	case strings.HasPrefix(q, "SELECT version FROM config_versions"):
		if !strings.HasSuffix(q, "FOR UPDATE") {
			return nil, errors.New("head version must be a current read")
		}
		var head int64
		for _, v := range d.working {
			if v.key == args[0].Value.(string) && v.version > head {
				head = v.version
			}
		}
		if head == 0 {
			return historyRow([]string{"version"}), nil
		}
		return historyRow([]string{"version"}, head), nil
	case strings.HasPrefix(q, "SELECT payload,version FROM config_versions"):
		for _, v := range d.working {
			if v.id == args[0].Value && v.key == args[1].Value {
				return historyRow([]string{"payload", "version"}, v.payload, v.version), nil
			}
		}
		return historyRow([]string{"payload", "version"}), nil
	case strings.HasPrefix(q, "SELECT p.id FROM products"):
		if d.products[args[0].Value.(string)] {
			return historyRow([]string{"id"}, args[0].Value), nil
		}
		return historyRow([]string{"id"}), nil
	case strings.HasPrefix(q, "SELECT id FROM articles"):
		if d.articles[args[0].Value.(string)] {
			return historyRow([]string{"id"}, args[0].Value), nil
		}
		return historyRow([]string{"id"}), nil
	case strings.HasPrefix(q, "SELECT COUNT(*) FROM config_versions"):
		var count int64
		for _, v := range d.working {
			if len(args) == 0 || v.key == args[0].Value {
				count++
			}
		}
		return historyRow([]string{"count"}, count), nil
	case strings.HasPrefix(q, "SELECT id,config_key,version"):
		rows := &historyRows{columns: []string{"id", "key", "version", "action", "source", "username", "created", "payload"}}
		for i := len(d.working) - 1; i >= 0; i-- {
			v := d.working[i]
			if len(args) == 2 || v.key == args[0].Value {
				rows.rows = append(rows.rows, []driver.Value{v.id, v.key, v.version, v.action, v.source, v.username, time.Date(2026, 10, 4, 0, 0, 0, 0, time.UTC), v.payload})
			}
		}
		offset := int(args[len(args)-1].Value.(int64))
		limit := int(args[len(args)-2].Value.(int64))
		if offset > len(rows.rows) {
			offset = len(rows.rows)
		}
		rows.rows = rows.rows[offset:]
		if len(rows.rows) > limit {
			rows.rows = rows.rows[:limit]
		}
		return rows, nil
	case strings.HasPrefix(q, "SELECT config_key,MAX(version)"):
		heads := map[string]int64{}
		for _, v := range d.working {
			if v.version > heads[v.key] {
				heads[v.key] = v.version
			}
		}
		rows := &historyRows{columns: []string{"key", "version"}}
		for key, version := range heads {
			rows.rows = append(rows.rows, []driver.Value{key, version})
		}
		return rows, nil
	case strings.HasPrefix(q, "SELECT payload FROM config_versions"):
		for _, v := range d.versions {
			if v.key == args[0].Value && v.version == args[1].Value {
				return historyRow([]string{"payload"}, v.payload), nil
			}
		}
		return &historyRows{columns: []string{"payload"}}, nil
	case strings.HasPrefix(q, "SELECT COUNT(*) FROM audit_logs"):
		return historyRow([]string{"count"}, int64(len(d.auditRead))), nil
	case strings.HasPrefix(q, "SELECT CAST(id AS CHAR),admin_id"):
		rows := append([][]driver.Value{}, d.auditRead...)
		offset := int(args[len(args)-1].Value.(int64))
		limit := int(args[len(args)-2].Value.(int64))
		if offset > len(rows) {
			offset = len(rows)
		}
		rows = rows[offset:]
		if len(rows) > limit {
			rows = rows[:limit]
		}
		return &historyRows{columns: []string{"id", "admin", "username", "action", "target", "detail", "created"}, rows: rows}, nil
	}
	return nil, errors.New("unexpected query: " + q)
}

func (d *historyDB) ExecContext(_ context.Context, q string, args []driver.NamedValue) (driver.Result, error) {
	switch {
	case strings.HasPrefix(q, "INSERT INTO configs"):
		key := args[0].Value.(string)
		if _, exists := d.workingConfigs[key]; !exists {
			d.workingConfigs[key] = args[1].Value.(string)
		}
	case strings.HasPrefix(q, "UPDATE configs"):
		if d.fail == "config" {
			return nil, errors.New("config unavailable")
		}
		d.workingConfigs[args[1].Value.(string)] = args[0].Value.(string)
	case strings.HasPrefix(q, "INSERT INTO config_versions"):
		if d.fail == "version" {
			return nil, errors.New("version unavailable")
		}
		v := historyRecord{id: args[0].Value.(string), key: args[1].Value.(string), version: args[2].Value.(int64), action: args[3].Value.(string), source: args[4].Value, username: args[6].Value.(string), payload: args[7].Value.(string)}
		d.working = append(d.working, v)
	case strings.HasPrefix(q, "INSERT INTO audit_logs"):
		if d.fail == "audit" {
			return nil, errors.New("audit unavailable")
		}
		if args[2].Value != "CONFIG_SAVE" && args[2].Value != "CONFIG_ROLLBACK" && args[2].Value != "AUDIT_EXPORT" {
			return nil, errors.New("unexpected audit action")
		}
		d.pendingAudits++
	default:
		return nil, errors.New("unexpected exec: " + q)
	}
	return driver.RowsAffected(1), nil
}

func historyServer(t *testing.T, state *historyDB) *Server {
	t.Helper()
	if state.configs == nil {
		state.configs = map[string]string{}
	}
	db := sql.OpenDB(historyConnector{state})
	db.SetMaxOpenConns(1)
	t.Cleanup(func() { db.Close() })
	return &Server{store: &Store{db: db}}
}

func TestConfigSaveAtomicity(t *testing.T) {
	old := ShippingRule{Freight: 1200, FreeShippingThreshold: 19900, BlockedProvinces: []string{"上海市"}}
	next := ShippingRule{Freight: 500, FreeShippingThreshold: 9900, BlockedProvinces: []string{}}
	for _, failure := range []string{"", "config", "version", "audit"} {
		t.Run(failure, func(t *testing.T) {
			state := &historyDB{configs: map[string]string{"shipping": jsonValue(old)}, fail: failure}
			server := historyServer(t, state)
			result, err := server.saveVersionedConfig(context.Background(), Claims{Subject: "a", Username: "tester"}, "shipping", json.RawMessage(jsonValue(next)))
			if failure != "" {
				if err == nil || state.configs["shipping"] != jsonValue(old) || len(state.versions) != 0 || state.audits != 0 || state.rollbacks != 1 {
					t.Fatalf("partial transaction escaped: %+v; %v", state, err)
				}
				return
			}
			if err != nil || result.Version != 2 || len(state.versions) != 2 || state.audits != 1 || state.configs["shipping"] != jsonValue(next) {
				t.Fatalf("failed save: %+v; %v", state, err)
			}
			if state.versions[0].action != "BASELINE" || state.versions[0].payload != jsonValue(old) || state.versions[1].action != "SAVE" {
				t.Fatalf("missing previous-value baseline: %+v", state.versions)
			}
		})
	}
}

func TestConfigDefaultBaselineAndMonotonicRollback(t *testing.T) {
	state := &historyDB{}
	server := historyServer(t, state)
	ctx := context.Background()
	actor := Claims{Subject: "a", Username: "tester"}
	_, err := server.saveVersionedConfig(ctx, actor, "shipping", json.RawMessage(jsonValue(ShippingRule{Freight: 100, FreeShippingThreshold: 200})))
	if err != nil || state.versions[0].payload != jsonValue(defaultRule()) {
		t.Fatalf("default baseline missing: %+v %v", state.versions, err)
	}
	_, err = server.saveVersionedConfig(ctx, actor, "shipping", json.RawMessage(jsonValue(ShippingRule{Freight: 300, FreeShippingThreshold: 400})))
	if err != nil || state.versions[2].version != 3 {
		t.Fatal("save must advance version", err)
	}
	request := M{"key": "shipping", "versionId": state.versions[0].id, "expectedVersion": 3, "reason": "恢复原运费"}
	result, err := server.rollbackConfig(ctx, actor, json.RawMessage(jsonValue(request)))
	if err != nil || result.(ConfigVersionResult).Version != 4 || state.configs["shipping"] != jsonValue(defaultRule()) || state.audits != 3 {
		t.Fatalf("rollback did not create new current version: %+v %v", state, err)
	}
	last := state.versions[3]
	if last.action != "ROLLBACK" || last.source != int64(1) || len(state.versions) != 4 {
		t.Fatalf("history overwritten or missing source: %+v", state.versions)
	}
	_, err = server.rollbackConfig(ctx, actor, json.RawMessage(jsonValue(request)))
	if err == nil || !strings.Contains(err.Error(), "配置已变化") || len(state.versions) != 4 || state.audits != 3 {
		t.Fatalf("stale rollback accepted: %v %+v", err, state)
	}
}

func TestRollbackRejectsInvalidHistoryAndCrossKey(t *testing.T) {
	home := defaultHome()
	home.ProductIDs = []string{"removed-product"}
	var privacy PrivacyPolicy
	_ = json.Unmarshal(privacyDefault, &privacy)
	privacy.Published = true
	for _, tc := range []struct{ key, payload string }{
		{"shipping", `{"freight":-1,"freeShippingThreshold":100}`},
		{"shipping", `{}`}, {"coupons", `{"items":[{"code":"BAD"}]}`},
		{"privacy", jsonValue(privacy)}, {"home", jsonValue(home)},
	} {
		t.Run(tc.key+tc.payload[:min(len(tc.payload), 20)], func(t *testing.T) {
			state := &historyDB{configs: map[string]string{tc.key: jsonValue(defaultConfigValue(tc.key))}, versions: []historyRecord{{id: "old", key: tc.key, version: 1, payload: tc.payload}, {id: "current", key: tc.key, version: 2, payload: jsonValue(defaultConfigValue(tc.key))}}}
			server := historyServer(t, state)
			_, err := server.rollbackConfig(context.Background(), Claims{}, json.RawMessage(jsonValue(M{"key": tc.key, "versionId": "old", "expectedVersion": 2, "reason": "恢复"})))
			if err == nil || !strings.Contains(err.Error(), "历史配置无法回滚") || len(state.versions) != 2 || state.audits != 0 || state.commits != 0 {
				t.Fatalf("invalid history accepted: %v %+v", err, state)
			}
		})
	}
	state := &historyDB{versions: []historyRecord{{id: "coupon-version", key: "coupons", version: 1, payload: `{"items":[]}`}}}
	server := historyServer(t, state)
	_, err := server.rollbackConfig(context.Background(), Claims{}, json.RawMessage(`{"key":"shipping","versionId":"coupon-version","expectedVersion":0,"reason":"恢复"}`))
	if err == nil || !strings.Contains(err.Error(), "不属于") || state.configs["shipping"] != "" {
		t.Fatalf("cross-key rollback accepted or placeholder escaped: %v %+v", err, state)
	}
}

func TestRollbackAtomicityAndCurrentVersionRejection(t *testing.T) {
	for _, failure := range []string{"config", "version", "audit", "current"} {
		t.Run(failure, func(t *testing.T) {
			old := ShippingRule{Freight: 100, FreeShippingThreshold: 200}
			current := ShippingRule{Freight: 300, FreeShippingThreshold: 400}
			state := &historyDB{configs: map[string]string{"shipping": jsonValue(current)}, versions: []historyRecord{{id: "old", key: "shipping", version: 1, payload: jsonValue(old)}, {id: "current", key: "shipping", version: 2, payload: jsonValue(current)}}, fail: failure}
			server := historyServer(t, state)
			id := "old"
			if failure == "current" {
				id = "current"
			}
			_, err := server.rollbackConfig(context.Background(), Claims{}, json.RawMessage(jsonValue(M{"key": "shipping", "versionId": id, "expectedVersion": 2, "reason": "恢复"})))
			if err == nil || len(state.versions) != 2 || state.audits != 0 || state.configs["shipping"] != jsonValue(current) || state.rollbacks != 1 {
				t.Fatalf("failed rollback changed persisted state: %+v %v", state, err)
			}
			if failure == "current" && !strings.Contains(err.Error(), "当前版本") {
				t.Fatal("current version rollback should be rejected explicitly", err)
			}
		})
	}
}

func TestHistoryListingAndMergedBaseline(t *testing.T) {
	state := &historyDB{configs: map[string]string{"home": `{"storyTitle":"旧标题"}`}}
	server := historyServer(t, state)
	_, err := server.saveOperationalConfig(context.Background(), Claims{Username: "tester"}, json.RawMessage(jsonValue(M{"home": defaultHome()})), "home")
	if err != nil {
		t.Fatal(err)
	}
	var baseline HomeConfig
	_ = json.Unmarshal([]byte(state.versions[0].payload), &baseline)
	if baseline.StoryTitle != "旧标题" || len(baseline.Banners) != 1 || validateHome(baseline) != nil {
		t.Fatalf("baseline did not capture effective config: %+v", baseline)
	}
	result, err := server.store.configVersions(context.Background(), json.RawMessage(`{"key":"home","limit":1,"offset":1}`))
	if err != nil {
		t.Fatal(err)
	}
	data := result.(M)
	items := data["items"].([]ConfigVersion)
	if data["total"] != 2 || len(items) != 1 || items[0].Action != "BASELINE" || data["currentVersions"].(map[string]int)["home"] != 2 || data["currentVersions"].(map[string]int)["shipping"] != 0 {
		t.Fatalf("bad history page: %+v", data)
	}
	if items[0].SourceVersion != nil || items[0].CreatedAt != "2026-10-04T00:00:00Z" {
		t.Fatalf("bad API fields: %+v", items)
	}
}

func TestHistoryActionsRequireSuperAdmin(t *testing.T) {
	t.Setenv("TOKEN_SECRET", strings.Repeat("x", 32))
	state := &historyDB{role: "OPERATOR"}
	server := historyServer(t, state)
	token, err := signToken(Claims{SessionID: randomNonce() + randomNonce(), Subject: "a", Kind: "admin", Credential: adminCredential("hash"), Exp: time.Now().Add(time.Hour).Unix()})
	if err != nil {
		t.Fatal(err)
	}
	for _, action := range []string{"admin.config.versions", "admin.config.rollback", "admin.audit.list", "admin.config.compare", "admin.audit.export"} {
		request := httptest.NewRequest("POST", "/api", nil)
		request.Header.Set("Authorization", "Bearer "+token)
		_, err := server.admin(request, Request{Action: action, Data: json.RawMessage(`{}`)})
		if err == nil || !strings.Contains(err.Error(), "超级管理员") || state.commits != 0 || state.rollbacks != 0 {
			t.Fatalf("operator accessed %s: %v", action, err)
		}
	}
}
