package main

import (
	"bytes"
	"context"
	"database/sql"
	"database/sql/driver"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"sync"
	"testing"
)

// This fixture runs the real HTTP handler, authorization, validation, and SQL
// store methods. Its database/sql driver stores rows in memory; it does not
// claim to validate a MySQL instance or the wx.cloud.callContainer transport.
type crossEndDB struct {
	mu          sync.Mutex
	configs     map[string]string
	versions    map[string]int64
	admins      map[string]Admin
	products    []Product
	articles    []Article
	failWrites  bool
	hideProduct bool
	audits      int
}

type crossEndConnector struct{ state *crossEndDB }
type crossEndDriver struct{ state *crossEndDB }
type crossEndConn struct {
	state           *crossEndDB
	workingConfigs  map[string]string
	workingVersions map[string]int64
	pendingAudits   int
	inTx            bool
}

func (c crossEndConnector) Connect(context.Context) (driver.Conn, error) {
	return &crossEndConn{state: c.state}, nil
}
func (c crossEndConnector) Driver() driver.Driver { return crossEndDriver{c.state} }
func (d crossEndDriver) Open(string) (driver.Conn, error) {
	return &crossEndConn{state: d.state}, nil
}
func (c *crossEndConn) Close() error { return nil }
func (c *crossEndConn) Prepare(string) (driver.Stmt, error) {
	return nil, errors.New("unexpected prepared query")
}
func (c *crossEndConn) Begin() (driver.Tx, error) {
	c.state.mu.Lock()
	defer c.state.mu.Unlock()
	c.workingConfigs = map[string]string{}
	for key, value := range c.state.configs {
		c.workingConfigs[key] = value
	}
	c.workingVersions = map[string]int64{}
	for key, version := range c.state.versions {
		c.workingVersions[key] = version
	}
	c.pendingAudits, c.inTx = 0, true
	return c, nil
}
func (c *crossEndConn) Commit() error {
	c.state.mu.Lock()
	defer c.state.mu.Unlock()
	c.state.configs, c.state.versions = c.workingConfigs, c.workingVersions
	c.state.audits += c.pendingAudits
	c.inTx = false
	return nil
}
func (c *crossEndConn) Rollback() error {
	c.workingConfigs, c.workingVersions = nil, nil
	c.pendingAudits, c.inTx = 0, false
	return nil
}

type crossEndRows struct {
	columns []string
	values  [][]driver.Value
	index   int
}

func (r *crossEndRows) Columns() []string { return r.columns }
func (r *crossEndRows) Close() error      { return nil }
func (r *crossEndRows) Next(out []driver.Value) error {
	if r.index >= len(r.values) {
		return io.EOF
	}
	copy(out, r.values[r.index])
	r.index++
	return nil
}

func (c *crossEndConn) QueryContext(_ context.Context, query string, args []driver.NamedValue) (driver.Rows, error) {
	if strings.HasPrefix(query, "SELECT GET_LOCK(") || strings.HasPrefix(query, "SELECT RELEASE_LOCK(") {
		return singleShippingRow(int64(1)), nil
	}
	d := c.state
	d.mu.Lock()
	defer d.mu.Unlock()
	rows := &crossEndRows{}
	switch {
	case strings.HasPrefix(query, "SELECT COUNT(*) FROM auth_sessions"):
		rows.columns = []string{"count"}
		rows.values = [][]driver.Value{{int64(1)}}
	case query == "SELECT active,role,username,password_hash FROM admins WHERE id=?":
		rows.columns = []string{"active", "role", "username", "password_hash"}
		if a, ok := d.admins[args[0].Value.(string)]; ok {
			rows.values = [][]driver.Value{{a.Active, a.Role, a.Username, a.PasswordHash}}
		}
	case strings.HasPrefix(query, "SELECT payload FROM configs"):
		rows.columns = []string{"payload"}
		key := "shipping"
		if len(args) > 0 {
			key = args[0].Value.(string)
		}
		configs := d.configs
		if c.inTx {
			configs = c.workingConfigs
		}
		if value, ok := configs[key]; ok {
			rows.values = [][]driver.Value{{[]byte(value)}}
		}
	case strings.HasPrefix(query, "SELECT version FROM config_versions"):
		rows.columns = []string{"version"}
		if !c.inTx || !strings.HasSuffix(query, "FOR UPDATE") {
			return nil, errors.New("config version validation must lock the head")
		}
		if version := c.workingVersions[args[0].Value.(string)]; version > 0 {
			rows.values = [][]driver.Value{{version}}
		}
	case strings.HasPrefix(query, "SELECT p.id FROM products"):
		rows.columns = []string{"id"}
		if !strings.HasSuffix(query, "FOR UPDATE") || !strings.Contains(query, "p.status='PUBLISHED'") || !strings.Contains(query, "k.active=1") {
			return nil, errors.New("published home product references must be locked")
		}
		for _, p := range d.products {
			if p.ID == args[0].Value.(string) && p.Status == "PUBLISHED" && !d.hideProduct {
				for _, sku := range p.SKUs {
					if sku.Active {
						rows.values = [][]driver.Value{{p.ID}}
						break
					}
				}
			}
		}
	case strings.HasPrefix(query, "SELECT id FROM articles WHERE id=?"):
		rows.columns = []string{"id"}
		if !strings.HasSuffix(query, "FOR UPDATE") || !strings.Contains(query, "status='PUBLISHED'") {
			return nil, errors.New("published home article references must be locked")
		}
		for _, article := range d.articles {
			if article.ID == args[0].Value.(string) && article.Status == "PUBLISHED" {
				rows.values = [][]driver.Value{{article.ID}}
			}
		}
	case strings.HasPrefix(query, "SELECT article_id FROM article_schedules"):
		rows.columns = []string{"article_id"} // This fixture has no scheduled articles.
	case strings.HasPrefix(query, "SELECT payload FROM products WHERE 1=1"):
		if !strings.Contains(query, "status='PUBLISHED'") {
			return nil, errors.New("home validation must only accept published products")
		}
		rows.columns = []string{"payload"}
		for _, p := range d.products {
			if p.Status == "PUBLISHED" && !d.hideProduct {
				rows.values = append(rows.values, []driver.Value{[]byte(jsonValue(p))})
			}
		}
	case strings.HasPrefix(query, "SELECT id,product_id,name,price,"):
		rows.columns = []string{"id", "product_id", "name", "price", "compare_at_price", "stock", "reserved", "sold", "limit_per_order", "weight_label", "active", "sort_order"}
		for _, p := range d.products {
			if p.ID != args[0].Value.(string) {
				continue
			}
			for _, sku := range p.SKUs {
				if sku.Active {
					rows.values = append(rows.values, []driver.Value{sku.ID, p.ID, sku.Name, int64(sku.Price), int64(sku.CompareAtPrice), int64(sku.Stock), int64(sku.Reserved), int64(sku.Sold), int64(sku.LimitPerOrder), sku.WeightLabel, sku.Active, int64(sku.SortOrder)})
				}
			}
		}
	case strings.HasPrefix(query, "SELECT payload FROM articles WHERE 1=1"):
		if !strings.Contains(query, "status='PUBLISHED'") {
			return nil, errors.New("home validation must only accept published articles")
		}
		rows.columns = []string{"payload"}
		for _, article := range d.articles {
			if article.Status == "PUBLISHED" {
				rows.values = append(rows.values, []driver.Value{[]byte(jsonValue(article))})
			}
		}
	default:
		return nil, fmt.Errorf("unexpected cross-end query: %s", query)
	}
	return rows, nil
}

func (c *crossEndConn) ExecContext(_ context.Context, query string, args []driver.NamedValue) (driver.Result, error) {
	d := c.state
	d.mu.Lock()
	defer d.mu.Unlock()
	switch {
	case strings.HasPrefix(query, "INSERT INTO configs(config_key,payload) VALUES(?,?)"):
		if !c.inTx {
			return nil, errors.New("config writes must be transactional")
		}
		key := args[0].Value.(string)
		if _, exists := c.workingConfigs[key]; !exists {
			c.workingConfigs[key] = args[1].Value.(string)
		}
	case strings.HasPrefix(query, "UPDATE configs SET payload=?"):
		if d.failWrites {
			return nil, errors.New("acceptance fixture: config write failed")
		}
		if !c.inTx {
			return nil, errors.New("config writes must be transactional")
		}
		c.workingConfigs[args[1].Value.(string)] = args[0].Value.(string)
	case strings.HasPrefix(query, "INSERT INTO config_versions"):
		if !c.inTx {
			return nil, errors.New("config snapshots must be transactional")
		}
		c.workingVersions[args[1].Value.(string)] = args[2].Value.(int64)
	case strings.HasPrefix(query, "INSERT INTO audit_logs"):
		if c.inTx {
			c.pendingAudits++
		} else {
			d.audits++
		}
	default:
		return nil, fmt.Errorf("unexpected cross-end exec: %s", query)
	}
	return driver.RowsAffected(1), nil
}

type crossEndResponse struct {
	OK    bool            `json:"ok"`
	Data  json.RawMessage `json:"data"`
	Error APIError        `json:"error"`
}

func crossEndPost(t *testing.T, service *httptest.Server, action, token string, data any, wantStatus int) crossEndResponse {
	t.Helper()
	request, err := http.NewRequest(http.MethodPost, service.URL, bytes.NewBufferString(jsonValue(M{"action": action, "data": data})))
	if err != nil {
		t.Fatal(err)
	}
	request.Header.Set("Content-Type", "application/json")
	if token != "" {
		request.Header.Set("Authorization", "Bearer "+token)
	}
	response, err := service.Client().Do(request)
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	var payload crossEndResponse
	if err = json.NewDecoder(response.Body).Decode(&payload); err != nil {
		t.Fatal(err)
	}
	if response.StatusCode != wantStatus || payload.OK != (wantStatus == http.StatusOK) {
		t.Fatalf("%s: status=%d ok=%t error=%q; want status=%d", action, response.StatusCode, payload.OK, payload.Error.Message, wantStatus)
	}
	return payload
}

func crossEndReadHome(t *testing.T, response crossEndResponse) HomeConfig {
	t.Helper()
	var home HomeConfig
	if err := json.Unmarshal(response.Data, &home); err != nil {
		t.Fatal(err)
	}
	return home
}

func TestCrossEndOperationalUpdatesHTTP(t *testing.T) {
	t.Setenv("TOKEN_SECRET", "cross-end-test-secret-at-least-32-bytes")
	super := Admin{ID: "acceptance-super", Username: "super", Role: "SUPER_ADMIN", Active: true, PasswordHash: "fixture-super-hash"}
	staff := Admin{ID: "acceptance-staff", Username: "staff", Role: "OPERATOR", Active: true, PasswordHash: "fixture-staff-hash"}
	state := &crossEndDB{
		configs:  map[string]string{"shipping": jsonValue(defaultRule())},
		admins:   map[string]Admin{super.ID: super, staff.ID: staff},
		products: []Product{{ID: "p-acceptance", Name: "验收商品", Status: "PUBLISHED", SKUs: []SKU{{ID: "s-acceptance", ProductID: "p-acceptance", Name: "验收规格", Price: 1000, Stock: 5, LimitPerOrder: 5, Active: true}}}},
		articles: []Article{{ID: "a-acceptance", Title: "验收文章", Status: "PUBLISHED"}},
	}
	db := sql.OpenDB(crossEndConnector{state})
	defer db.Close()
	server := &Server{store: &Store{db: db}}
	service := httptest.NewServer(http.HandlerFunc(server.api))
	defer service.Close()
	superToken, err := adminToken(super)
	if err != nil {
		t.Fatal(err)
	}
	staffToken, err := adminToken(staff)
	if err != nil {
		t.Fatal(err)
	}
	mini, err := miniToken("acceptance-mini-user")
	if err != nil {
		t.Fatal(err)
	}
	home := defaultHome()
	home.Banners = []Banner{{Title: "验收保存的轮播", Copy: "验收文案", Button: "查看商品", Target: "product", TargetID: "p-acceptance"}}
	home.ProductIDs = []string{"p-acceptance"}
	home.ArticleIDs = []string{"a-acceptance"}
	home.Activities = []HomeActivity{{Title: "验收活动", Summary: "活动简介", Content: "完整规则", StartDate: "2026-10-01", EndDate: "2026-10-31", ProductID: "p-acceptance"}}
	home.Announcements = []HomeAnnouncement{{Title: "验收公告", Content: "公告正文", PublishedAt: "2026-10-04"}}
	home.LaunchCalendar = []HomeLaunch{{Variety: "验收品种", StartDate: "2026-11-20", EndDate: "2027-02-10", Note: "跨年上市", ProductID: "p-acceptance"}}
	var policy PrivacyPolicy
	if err := json.Unmarshal(privacyDefault, &policy); err != nil {
		t.Fatal(err)
	}
	policy.Published, policy.Operator, policy.Contact, policy.Address = true, "验收主体", "privacy@example.com", "验收地址"
	policy.Version = "acceptance-v1"

	t.Run("authorization rejects writes without changing store", func(t *testing.T) {
		crossEndPost(t, service, "admin.home.save", "", M{"home": home}, http.StatusUnauthorized)
		crossEndPost(t, service, "admin.privacy.save", "", M{"privacy": policy}, http.StatusUnauthorized)
		for _, action := range []string{"admin.home.save", "admin.privacy.save"} {
			response := crossEndPost(t, service, action, staffToken, M{"home": home, "privacy": policy}, http.StatusForbidden)
			if !strings.Contains(response.Error.Message, "超级管理员") {
				t.Fatalf("unexpected rejection: %s", response.Error.Message)
			}
		}
		state.mu.Lock()
		configCount, audits := len(state.configs), state.audits
		state.mu.Unlock()
		if configCount != 1 || audits != 0 {
			t.Fatal("an unauthorized request changed config storage")
		}
		crossEndPost(t, service, "home.get", "", M{}, http.StatusUnauthorized)
	})

	t.Run("home save is returned identically to admin and mini readers", func(t *testing.T) {
		saved := crossEndReadHome(t, crossEndPost(t, service, "admin.home.save", superToken, M{"home": home}, http.StatusOK))
		public := crossEndReadHome(t, crossEndPost(t, service, "home.get", mini, M{}, http.StatusOK))
		var config struct {
			Home     HomeConfig   `json:"home"`
			Shipping ShippingRule `json:"shipping"`
		}
		response := crossEndPost(t, service, "admin.config.get", superToken, M{}, http.StatusOK)
		if err := json.Unmarshal(response.Data, &config); err != nil {
			t.Fatal(err)
		}
		if !reflect.DeepEqual(home, saved) || !reflect.DeepEqual(home, public) || !reflect.DeepEqual(home, config.Home) || !reflect.DeepEqual(defaultRule(), config.Shipping) {
			t.Fatal("saved home differs across readers or overwrote shipping")
		}
		freshServer := &Server{store: &Store{db: db}}
		freshService := httptest.NewServer(http.HandlerFunc(freshServer.api))
		defer freshService.Close()
		if fresh := crossEndReadHome(t, crossEndPost(t, freshService, "home.get", mini, M{}, http.StatusOK)); !reflect.DeepEqual(home, fresh) {
			t.Fatal("a new service instance did not read the saved store value")
		}
	})

	t.Run("failed validation and storage writes retain last saved home", func(t *testing.T) {
		invalid := home
		invalid.ProductIDs = []string{"missing-product"}
		crossEndPost(t, service, "admin.home.save", superToken, M{"home": invalid}, http.StatusBadRequest)
		state.mu.Lock()
		state.failWrites = true
		state.mu.Unlock()
		changed := home
		changed.StoryTitle = "不应保存的标题"
		crossEndPost(t, service, "admin.home.save", superToken, M{"home": changed}, http.StatusBadRequest)
		state.mu.Lock()
		state.failWrites = false
		state.hideProduct = true
		state.mu.Unlock()
		crossEndPost(t, service, "admin.home.save", superToken, M{"home": home}, http.StatusBadRequest)
		state.mu.Lock()
		state.hideProduct = false
		state.mu.Unlock()
		if got := crossEndReadHome(t, crossEndPost(t, service, "home.get", mini, M{}, http.StatusOK)); !reflect.DeepEqual(home, got) {
			t.Fatal("failed save overwrote previously published home")
		}
		state.mu.Lock()
		audits := state.audits
		state.mu.Unlock()
		if audits != 1 {
			t.Fatalf("failed saves generated audit writes: %d", audits)
		}
	})

	t.Run("policy publication update and withdrawal use the same stored config", func(t *testing.T) {
		crossEndPost(t, service, "privacy.get", "", M{}, http.StatusBadRequest)
		crossEndPost(t, service, "admin.privacy.save", superToken, M{"privacy": policy}, http.StatusOK)
		for _, version := range []string{"acceptance-v1", "acceptance-v2"} {
			policy.Version = version
			crossEndPost(t, service, "admin.privacy.save", superToken, M{"privacy": policy}, http.StatusOK)
			var public PrivacyPolicy
			response := crossEndPost(t, service, "privacy.get", "", M{}, http.StatusOK)
			if err := json.Unmarshal(response.Data, &public); err != nil {
				t.Fatal(err)
			}
			if !reflect.DeepEqual(policy, public) {
				t.Fatal("public policy did not match the newly saved policy")
			}
		}
		invalid := policy
		invalid.Operator = ""
		crossEndPost(t, service, "admin.privacy.save", superToken, M{"privacy": invalid}, http.StatusBadRequest)
		policy.Published = false
		crossEndPost(t, service, "admin.privacy.save", superToken, M{"privacy": policy}, http.StatusOK)
		withdrawn := crossEndPost(t, service, "privacy.get", "", M{}, http.StatusBadRequest)
		if !strings.Contains(withdrawn.Error.Message, "正在完善") || len(withdrawn.Data) != 0 {
			t.Fatal("withdrawn policy remained publicly readable")
		}
		var config struct {
			Home    HomeConfig    `json:"home"`
			Privacy PrivacyPolicy `json:"privacy"`
		}
		response := crossEndPost(t, service, "admin.config.get", superToken, M{}, http.StatusOK)
		if err := json.Unmarshal(response.Data, &config); err != nil {
			t.Fatal(err)
		}
		if !reflect.DeepEqual(home, config.Home) || !reflect.DeepEqual(policy, config.Privacy) {
			t.Fatal("policy save overwrote home or draft is not available to administrators")
		}
	})
}
