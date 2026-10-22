package main

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"
)

func TestLeadSearchNormalizesAndScopesBeforePagination(t *testing.T) {
	d, err := normalizeLeadSearch(LeadSearch{Keyword: ` CoOp 张三 %_\! `, Status: " WON ", Channel: " 餐饮 ", Region: " 北京 ", Limit: 500, Offset: -2})
	if err != nil || d.Limit != 100 || d.Offset != 0 {
		t.Fatalf("invalid normalized search: %+v, %v", d, err)
	}
	where, args := leadSearchWhere(d)
	if !reflect.DeepEqual(args, []any{"WON", "餐饮", "北京", "coop", "%coop%", "张三", "%张三%", `%_\!`, `%!%!_\!!%`}) {
		t.Fatalf("literal keywords or filters changed: %#v", args)
	}
	if strings.Contains(where, "LIMIT") || strings.Contains(where, "CoOp") || strings.Count(where, "LOCATE(") != 3 || strings.Count(where, "JSON_SEARCH(") != 3 || !strings.Contains(where, "status=?") {
		t.Fatalf("search must be parameterized and applied to count before pagination: %s", where)
	}
	for _, field := range []string{"organization", "contact", "phone", "region", "channel", "volume", "demand", "followUpNote", "followUps[*].note"} {
		if !strings.Contains(where, "$."+field) {
			t.Fatalf("search omits %s", field)
		}
	}
	for _, limit := range []int{0, -1} {
		d, err = normalizeLeadSearch(LeadSearch{Limit: limit})
		if err != nil || d.Limit != 20 {
			t.Fatalf("default pagination: %+v, %v", d, err)
		}
	}
	if _, err = normalizeLeadSearch(LeadSearch{Status: "INVALID"}); err == nil {
		t.Fatal("unknown filter status accepted")
	}
}

func TestLeadHistoryPreservesLegacyAndLatestNote(t *testing.T) {
	l := Lead{ID: "old", Status: "NEW", FollowUpNote: " 历史备注 "}
	ensureLeadHistory(&l)
	ensureLeadHistory(&l)
	if len(l.FollowUps) != 1 || l.FollowUps[0].Note != " 历史备注 " || !l.FollowUps[0].Legacy || l.FollowUps[0].CreatedAt != "" || l.FollowUps[0].Username != "历史记录" {
		t.Fatalf("legacy note or unknown timestamp lost: %+v", l.FollowUps)
	}
	actor := Claims{Username: "operator"}
	if err := appendLeadFollowUp(&l, "CONTACTED", " 已联系 ", actor); err != nil {
		t.Fatal(err)
	}
	if err := appendLeadFollowUp(&l, "QUALIFIED", " \n ", actor); err != nil {
		t.Fatal(err)
	}
	if len(l.FollowUps) != 3 || l.FollowUpNote != "已联系" || l.FollowUps[2].Note != "" || l.FollowUps[2].PreviousStatus != "CONTACTED" || l.FollowUps[2].Status != "QUALIFIED" || l.FollowUps[2].Username != "operator" {
		t.Fatalf("status-only change erased latest note or missing history: %+v", l)
	}
	if _, err := time.Parse(time.RFC3339Nano, l.FollowUps[1].CreatedAt); err != nil {
		t.Fatalf("follow-up timestamp invalid: %v", err)
	}
	if l.FollowUps[1].ID == l.FollowUps[2].ID {
		t.Fatal("history identifiers must be unique")
	}
	for _, request := range []struct{ status, note string }{{"QUALIFIED", " "}, {"INVALID", "备注"}, {"WON", strings.Repeat("葡", 2001)}} {
		before := jsonValue(l)
		if appendLeadFollowUp(&l, request.status, request.note, actor) == nil || jsonValue(l) != before {
			t.Fatal("invalid or empty update mutated history")
		}
	}
	if err := appendLeadFollowUp(&l, "QUALIFIED", strings.Repeat("葡", 2000), actor); err != nil {
		t.Fatalf("2000-character note rejected: %v", err)
	}
	empty := Lead{Status: "NEW"}
	ensureLeadHistory(&empty)
	if empty.FollowUps == nil {
		t.Fatal("new lead history must serialize as []")
	}
}

// Each SQL connection has its own working copy. The fixture holds the lead lock
// until commit/rollback, exercising real concurrent service calls without MySQL.
type leadTestDB struct {
	mu           sync.Mutex
	leads        map[string]Lead
	audits       []string
	history      []LeadHistory
	auditFailure bool
	historyFail  bool
	updateFail   bool
}
type leadTestConnector struct{ state *leadTestDB }
type leadTestDriver struct{ state *leadTestDB }
type leadTestConn struct {
	state    *leadTestDB
	inTx     bool
	locked   bool
	working  *Lead
	username string
	history  []LeadHistory
}

func (c leadTestConnector) Connect(context.Context) (driver.Conn, error) {
	return &leadTestConn{state: c.state}, nil
}
func (c leadTestConnector) Driver() driver.Driver { return leadTestDriver{c.state} }
func (d leadTestDriver) Open(string) (driver.Conn, error) {
	return &leadTestConn{state: d.state}, nil
}
func (c *leadTestConn) Prepare(string) (driver.Stmt, error) {
	return nil, errors.New("unexpected prepare")
}
func (c *leadTestConn) Close() error { return nil }
func (c *leadTestConn) Begin() (driver.Tx, error) {
	c.inTx = true
	return c, nil
}
func (c *leadTestConn) Commit() error {
	if c.working != nil {
		c.state.leads[c.working.ID] = *c.working
		c.state.audits = append(c.state.audits, c.username)
		c.state.history = append(c.state.history, c.history...)
	}
	return c.Rollback()
}
func (c *leadTestConn) Rollback() error {
	c.working, c.username, c.inTx = nil, "", false
	c.history = nil
	if c.locked {
		c.locked = false
		c.state.mu.Unlock()
	}
	return nil
}

type leadTestRows struct {
	columns []string
	values  [][]driver.Value
}

func (r *leadTestRows) Columns() []string { return r.columns }
func (r *leadTestRows) Close() error      { return nil }
func (r *leadTestRows) Next(out []driver.Value) error {
	if len(r.values) == 0 {
		return io.EOF
	}
	copy(out, r.values[0])
	r.values = r.values[1:]
	return nil
}
func leadTestRow(l Lead) []driver.Value {
	return []driver.Value{l.ID, l.Status, time.Date(2026, 10, 4, 1, 0, 0, 0, time.UTC), []byte(jsonValue(l))}
}
func (c *leadTestConn) QueryContext(_ context.Context, q string, args []driver.NamedValue) (driver.Rows, error) {
	if !strings.HasPrefix(q, "SELECT id,status,created_at,payload FROM partner_leads WHERE id=?") || !strings.HasSuffix(q, " FOR UPDATE") || !c.inTx {
		return nil, errors.New("lead updates must lock their row in a transaction")
	}
	c.state.mu.Lock()
	c.locked = true
	rows := &leadTestRows{columns: []string{"id", "status", "created_at", "payload"}}
	if lead, ok := c.state.leads[args[0].Value.(string)]; ok {
		rows.values = [][]driver.Value{leadTestRow(lead)}
	}
	return rows, nil
}
func (c *leadTestConn) ExecContext(_ context.Context, q string, args []driver.NamedValue) (driver.Result, error) {
	if !c.locked || !c.inTx {
		return nil, errors.New("lead writes must retain row lock")
	}
	switch {
	case strings.HasPrefix(q, "UPDATE partner_leads SET status=?,payload=? WHERE id=?"):
		if c.state.updateFail {
			return nil, errors.New("update unavailable")
		}
		var lead Lead
		if err := json.Unmarshal([]byte(args[1].Value.(string)), &lead); err != nil {
			return nil, err
		}
		if lead.Status != args[0].Value || lead.ID != args[2].Value {
			return nil, errors.New("row and payload disagree")
		}
		c.working = &lead
	case strings.HasPrefix(q, "INSERT INTO partner_lead_history"):
		if c.state.historyFail {
			return nil, errors.New("history unavailable")
		}
		var entry LeadHistory
		if err := json.Unmarshal([]byte(args[3].Value.(string)), &entry); err != nil {
			return nil, err
		}
		c.history = append(c.history, entry)
	case strings.HasPrefix(q, "INSERT INTO audit_logs"):
		if c.state.auditFailure {
			return nil, errors.New("audit unavailable")
		}
		c.username = args[1].Value.(string)
	default:
		return nil, errors.New("unexpected write: " + q)
	}
	return driver.RowsAffected(1), nil
}

func TestLeadUpdateAtomicHistoryAndAudit(t *testing.T) {
	for _, scenario := range []string{"commit", "status only", "update failure", "history failure", "audit failure", "missing", "empty", "forged actor", "stale version"} {
		t.Run(scenario, func(t *testing.T) {
			state := &leadTestDB{leads: map[string]Lead{"lead": {ID: "lead", Status: "NEW", FollowUpNote: "旧备注"}}, updateFail: scenario == "update failure", historyFail: scenario == "history failure", auditFailure: scenario == "audit failure"}
			db := sql.OpenDB(leadTestConnector{state})
			defer db.Close()
			s := Server{store: &Store{db: db}}
			id, note := "lead", "新备注"
			if scenario == "missing" {
				id = "missing"
			}
			if scenario == "empty" {
				note = " "
			}
			input := M{"leadId": id, "status": "NEW", "followUpNote": note, "expectedVersion": 0}
			if scenario == "status only" {
				input["status"], input["followUpNote"] = "CONTACTED", " "
			}
			if scenario == "stale version" {
				input["expectedVersion"] = 1
			}
			if scenario == "forged actor" {
				input["username"] = "forged"
			}
			result, err := s.updateLead(context.Background(), Claims{Subject: "admin", Username: "张三"}, []byte(jsonValue(input)))
			if scenario == "commit" {
				if err != nil || result == nil || len(result.FollowUps) != 2 || result.FollowUps[1].Username != "张三" || result.FollowUpNote != "新备注" || result.CreatedAt != "2026-10-04T01:00:00Z" || !reflect.DeepEqual(state.audits, []string{"张三"}) || len(state.history) != 1 {
					t.Fatalf("history and authenticated audit not committed: %+v, %v", result, err)
				}
			} else if scenario == "status only" {
				if err != nil || result == nil || result.Status != "CONTACTED" || result.FollowUpNote != "旧备注" || len(result.FollowUps) != 2 || result.FollowUps[1].Note != "" || result.FollowUps[1].PreviousStatus != "NEW" || len(state.audits) != 1 || len(state.history) != 1 {
					t.Fatalf("status-only workflow erased notes/history: %+v, %v", result, err)
				}
			} else if scenario == "empty" {
				if err != nil || result == nil || len(state.audits) != 0 || len(state.history) != 0 || state.leads["lead"].FollowUpNote != "旧备注" {
					t.Fatalf("no-op must leave persistent data unchanged: %+v, %v", state, err)
				}
			} else if err == nil || state.leads["lead"].FollowUpNote != "旧备注" || len(state.leads["lead"].FollowUps) != 0 || len(state.audits) != 0 || len(state.history) != 0 {
				t.Fatalf("failed update escaped rollback: %+v, %v", state, err)
			}
		})
	}
}

func TestConcurrentLeadUpdatesDoNotLoseHistory(t *testing.T) {
	state := &leadTestDB{leads: map[string]Lead{"lead": {ID: "lead", Status: "NEW", FollowUpNote: "原备注"}}}
	db := sql.OpenDB(leadTestConnector{state})
	defer db.Close()
	db.SetMaxOpenConns(8)
	s := Server{store: &Store{db: db}}
	const writers = 24
	var wg sync.WaitGroup
	errorsOut := make(chan error, writers)
	for i := 0; i < writers; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			for retries := 0; retries < writers*2; retries++ {
				state.mu.Lock()
				version := state.leads["lead"].Version
				state.mu.Unlock()
				_, err := s.updateLead(context.Background(), Claims{Subject: "admin", Username: "operator"}, []byte(jsonValue(M{"leadId": "lead", "status": "CONTACTED", "followUpNote": fmt.Sprintf("跟进 %d", i), "expectedVersion": version})))
				if err != nil && strings.Contains(err.Error(), "版本已被其他管理员更新") {
					continue
				}
				errorsOut <- err
				return
			}
			errorsOut <- errors.New("concurrent update retries exhausted")
		}(i)
	}
	wg.Wait()
	close(errorsOut)
	for err := range errorsOut {
		if err != nil {
			t.Fatal(err)
		}
	}
	lead := state.leads["lead"]
	if len(lead.FollowUps) != writers+1 || len(state.audits) != writers || len(state.history) != writers || lead.Version != writers {
		t.Fatalf("concurrent writes lost entries: %d follow-ups, %d audits", len(lead.FollowUps), len(state.audits))
	}
	notes := map[string]bool{}
	for _, entry := range lead.FollowUps {
		notes[entry.Note] = true
	}
	if len(notes) != writers+1 || !lead.FollowUps[0].Legacy || lead.FollowUpNote != lead.FollowUps[len(lead.FollowUps)-1].Note {
		t.Fatal("history or latest note corrupted")
	}
}

type leadListConnector struct{}
type leadListDriver struct{}
type leadListConn struct{}

func (leadListConnector) Connect(context.Context) (driver.Conn, error) { return leadListConn{}, nil }
func (leadListConnector) Driver() driver.Driver                        { return leadListDriver{} }
func (leadListDriver) Open(string) (driver.Conn, error)                { return leadListConn{}, nil }
func (leadListConn) Prepare(string) (driver.Stmt, error) {
	return nil, errors.New("unexpected prepare")
}
func (leadListConn) Close() error              { return nil }
func (leadListConn) Begin() (driver.Tx, error) { return nil, errors.New("unexpected transaction") }
func (leadListConn) QueryContext(_ context.Context, q string, args []driver.NamedValue) (driver.Rows, error) {
	if strings.HasPrefix(q, "SELECT COUNT(*)") {
		if !strings.Contains(q, "status=?") || strings.Contains(q, "LIMIT") || len(args) != 1 || args[0].Value != "NEW" {
			return nil, errors.New("count omitted active filters")
		}
		return &leadTestRows{columns: []string{"count"}, values: [][]driver.Value{{int64(701)}}}, nil
	}
	if strings.HasPrefix(q, "SELECT id,status,created_at,payload") {
		if !strings.HasSuffix(q, "ORDER BY created_at DESC,id DESC LIMIT ? OFFSET ?") || !reflect.DeepEqual([]any{args[0].Value, args[1].Value, args[2].Value}, []any{"NEW", int64(20), int64(600)}) {
			return nil, errors.New("page ordering or bounds incorrect")
		}
		return &leadTestRows{columns: []string{"id", "status", "created_at", "payload"}, values: [][]driver.Value{leadTestRow(Lead{ID: "older", Status: "NEW", Channel: "餐饮", Region: "北京", FollowUpNote: "历史备注"})}}, nil
	}
	if strings.HasPrefix(q, "SELECT DISTINCT") {
		if len(args) != 0 || strings.Contains(q, "status=?") || !strings.HasSuffix(q, "ORDER BY value") {
			return nil, errors.New("global facets were scoped to current page")
		}
		if strings.Contains(q, "$.channel") {
			return &leadTestRows{columns: []string{"value"}, values: [][]driver.Value{{"电商"}, {"餐饮"}}}, nil
		}
		return &leadTestRows{columns: []string{"value"}, values: [][]driver.Value{{"北京"}, {"上海"}}}, nil
	}
	return nil, errors.New("unexpected query: " + q)
}

func TestLeadListRetainsCountGlobalFacetsAndLegacyBeyond500(t *testing.T) {
	db := sql.OpenDB(leadListConnector{})
	defer db.Close()
	db.SetMaxOpenConns(1)
	store := Store{db: db}
	result, err := store.searchLeads(context.Background(), LeadSearch{Status: "NEW", Offset: 600})
	if err != nil || result.Total != 701 || result.Limit != 20 || result.Offset != 600 || len(result.Items) != 1 || result.Items[0].ID != "older" || len(result.Items[0].FollowUps) != 1 || !reflect.DeepEqual(result.Channels, []string{"电商", "餐饮"}) || !reflect.DeepEqual(result.Regions, []string{"北京", "上海"}) {
		t.Fatalf("paged list lost count/facets/history: %+v, %v", result, err)
	}
}

type leadSubmitConnector struct{ saved *Lead }
type leadSubmitDriver struct{ saved *Lead }
type leadSubmitConn struct {
	leadListConn
	saved *Lead
}

func (c leadSubmitConnector) Connect(context.Context) (driver.Conn, error) {
	return leadSubmitConn{saved: c.saved}, nil
}
func (c leadSubmitConnector) Driver() driver.Driver { return leadSubmitDriver{c.saved} }
func (d leadSubmitDriver) Open(string) (driver.Conn, error) {
	return leadSubmitConn{saved: d.saved}, nil
}
func (leadSubmitConn) QueryContext(_ context.Context, q string, args []driver.NamedValue) (driver.Rows, error) {
	if !strings.HasPrefix(q, "SELECT id FROM partner_leads WHERE phone_hash=?") || len(args) != 1 {
		return nil, errors.New("unexpected duplicate check")
	}
	return &leadTestRows{columns: []string{"id"}}, nil
}
func (c leadSubmitConn) ExecContext(_ context.Context, q string, args []driver.NamedValue) (driver.Result, error) {
	if !strings.HasPrefix(q, "INSERT INTO partner_leads(") {
		return nil, errors.New("unexpected submit write")
	}
	if err := json.Unmarshal([]byte(args[3].Value.(string)), c.saved); err != nil {
		return nil, err
	}
	return driver.RowsAffected(1), nil
}

func TestPublicLeadSubmitStartsEmptyHistory(t *testing.T) {
	var saved Lead
	db := sql.OpenDB(leadSubmitConnector{saved: &saved})
	defer db.Close()
	s := Server{store: &Store{db: db}}
	raw := []byte(`{"organization":"合作方","contact":"联系人","phone":"13800000000","region":"北京","volume":"100箱","consent":true,"status":"WON","followUpNote":"forged","followUps":[{"note":"forged","username":"admin"}]}`)
	if _, err := s.submitLead(context.Background(), raw); err != nil {
		t.Fatal(err)
	}
	if saved.Status != "NEW" || saved.FollowUpNote != "" || saved.FollowUps == nil || len(saved.FollowUps) != 0 {
		t.Fatalf("public input forged follow-up history: %+v", saved)
	}
}
