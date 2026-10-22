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
	"sync"
	"testing"
	"time"
)

func workflowPatch(t *testing.T, fields M) leadUpdate {
	t.Helper()
	fields["leadId"], fields["expectedVersion"] = "lead", 0
	d, err := decodeLeadUpdate(json.RawMessage(jsonValue(fields)))
	if err != nil {
		t.Fatal(err)
	}
	return d
}

func TestLeadWorkflowPartialUpdatesAndReminders(t *testing.T) {
	now := time.Date(2026, 10, 4, 10, 0, 0, 0, time.UTC)
	original := Lead{ID: "lead", Status: "CONTACTED", FollowUpNote: "已有备注", OwnerID: "old", OwnerName: "旧负责人", ReminderAt: now.Add(-time.Hour).Format(time.RFC3339), Version: 3}
	got, changes, note, err := applyLeadUpdate(original, workflowPatch(t, M{"ownerId": "new"}), "新负责人", now)
	if err != nil || got.Status != original.Status || got.FollowUpNote != original.FollowUpNote || got.ReminderAt != original.ReminderAt || got.OwnerName != "新负责人" || strings.Join(changes, ",") != "owner" || note != "" {
		t.Fatalf("assignment overwrote unrelated fields: %+v %v %v", got, changes, err)
	}
	got, changes, _, err = applyLeadUpdate(original, workflowPatch(t, M{"reminderDone": true}), original.OwnerName, now)
	if err != nil || !got.ReminderDone || strings.Join(changes, ",") != "reminder" {
		t.Fatalf("could not complete overdue reminder: %+v %v %v", got, changes, err)
	}
	got, changes, _, err = applyLeadUpdate(original, workflowPatch(t, M{"status": "WON"}), original.OwnerName, now)
	if err != nil || !got.ReminderDone || strings.Join(changes, ",") != "status,reminder" {
		t.Fatalf("closing did not complete reminder: %+v %v %v", got, changes, err)
	}
	for _, fields := range []M{
		{"reminderAt": now.Add(-time.Minute).Format(time.RFC3339)},
		{"reminderAt": "2026-99-04T00:00:00Z"},
		{"reminderAt": "2026-10-04T11:00:00"},
		{"status": "LOST", "reminderAt": now.Add(time.Hour).Format(time.RFC3339)},
	} {
		if _, _, _, err := applyLeadUpdate(original, workflowPatch(t, fields), original.OwnerName, now); err == nil {
			t.Fatalf("invalid reminder accepted: %v", fields)
		}
	}
	completed := original
	completed.ReminderDone = true
	if _, _, _, err := applyLeadUpdate(completed, workflowPatch(t, M{"reminderDone": false}), original.OwnerName, now); err == nil {
		t.Fatal("reopened overdue reminder")
	}
	got, _, _, err = applyLeadUpdate(completed, workflowPatch(t, M{"reminderAt": ""}), original.OwnerName, now)
	if err != nil || got.ReminderAt != "" || got.ReminderDone {
		t.Fatalf("clear did not clear completion: %+v %v", got, err)
	}
	if _, _, _, err := applyLeadUpdate(Lead{Status: "NEW"}, workflowPatch(t, M{"reminderDone": true}), "", now); err == nil {
		t.Fatal("completed a nonexistent reminder")
	}
	got, changes, note, err = applyLeadUpdate(original, workflowPatch(t, M{"followUpNote": original.FollowUpNote}), original.OwnerName, now)
	if err != nil || got.FollowUpNote != original.FollowUpNote || strings.Join(changes, ",") != "note" || note != original.FollowUpNote {
		t.Fatal("repeated real activity was discarded")
	}
	got, changes, _, err = applyLeadUpdate(original, workflowPatch(t, M{"followUpNote": " ", "status": original.Status}), original.OwnerName, now)
	if err != nil || len(changes) != 0 || got.FollowUpNote != original.FollowUpNote {
		t.Fatal("no-op cleared previous note")
	}
	got, _, _, err = applyLeadUpdate(original, workflowPatch(t, M{"reminderAt": "2026-10-04T20:00:00+08:00"}), original.OwnerName, now)
	if err != nil || got.ReminderAt != "2026-10-04T12:00:00Z" || got.ReminderDone {
		t.Fatalf("timestamp not normalized: %+v %v", got, err)
	}
}

func TestLeadWorkflowRejectsInvalidInputs(t *testing.T) {
	for _, raw := range []string{
		`{}`, `{"leadId":"lead"}`, `{"leadId":"lead","expectedVersion":null}`,
		`{"leadId":"lead","expectedVersion":-1}`, `{"leadId":"lead","expectedVersion":1.5}`,
		`{"leadId":"lead","expectedVersion":"0"}`, `{"leadId":"lead","expectedVersion":0,"status":"OTHER"}`,
		`{"leadId":"lead","expectedVersion":0,"ownerId":null}`, `{"leadId":"lead","expectedVersion":0,"reminderDone":"true"}`,
		`{"leadId":"lead","expectedVersion":0,"ownerName":"forged"}`, `null`,
		jsonValue(M{"leadId": "lead", "expectedVersion": 0, "followUpNote": strings.Repeat("字", 2001)}),
	} {
		if _, err := decodeLeadUpdate(json.RawMessage(raw)); err == nil {
			t.Fatalf("accepted invalid update: %s", raw)
		}
	}
	if _, err := decodeLeadUpdate(json.RawMessage(jsonValue(M{"leadId": "lead", "expectedVersion": 0, "followUpNote": strings.Repeat("字", 2000)}))); err != nil {
		t.Fatal("valid Unicode note rejected:", err)
	}
	if _, err := normalizeLeadSearch(LeadSearch{Reminder: "unknown"}); err == nil {
		t.Fatal("invalid reminder filter accepted")
	}
}

// This driver models row locking and transaction visibility so a stale version
// or an audit/history failure cannot publish a partial update.
type workflowDB struct {
	mu             sync.Mutex
	leads          map[string]Lead
	owners         map[string]Admin
	history        []LeadHistory
	audits         int
	historyFailure bool
	auditFailure   bool
	queries        []string
}
type workflowConnector struct{ state *workflowDB }
type workflowDriver struct{ state *workflowDB }
type workflowConn struct {
	state          *workflowDB
	working        map[string]Lead
	pendingHistory []LeadHistory
	pendingAudits  int
	locked         bool
}

func (c workflowConnector) Connect(context.Context) (driver.Conn, error) {
	return &workflowConn{state: c.state}, nil
}
func (c workflowConnector) Driver() driver.Driver { return workflowDriver{c.state} }
func (d workflowDriver) Open(string) (driver.Conn, error) {
	return &workflowConn{state: d.state}, nil
}
func (c *workflowConn) Prepare(string) (driver.Stmt, error) {
	return nil, errors.New("unexpected prepare")
}
func (c *workflowConn) Close() error { return nil }
func (c *workflowConn) Begin() (driver.Tx, error) {
	c.state.mu.Lock()
	c.locked = true
	c.working = map[string]Lead{}
	for id, lead := range c.state.leads {
		c.working[id] = lead
	}
	c.pendingHistory, c.pendingAudits = nil, 0
	return c, nil
}
func (c *workflowConn) Commit() error {
	c.state.leads = c.working
	c.state.history = append(c.state.history, c.pendingHistory...)
	c.state.audits += c.pendingAudits
	return c.Rollback()
}
func (c *workflowConn) Rollback() error {
	c.working, c.pendingHistory, c.pendingAudits = nil, nil, 0
	if c.locked {
		c.locked = false
		c.state.mu.Unlock()
	}
	return nil
}
func (c *workflowConn) QueryContext(_ context.Context, query string, args []driver.NamedValue) (driver.Rows, error) {
	if !c.locked {
		c.state.mu.Lock()
		defer c.state.mu.Unlock()
	}
	c.state.queries = append(c.state.queries, query)
	leads := c.state.leads
	if c.locked {
		leads = c.working
	}
	switch {
	case strings.HasPrefix(query, "SELECT id,status,created_at,payload FROM partner_leads WHERE id=?"):
		if c.locked && !strings.HasSuffix(query, "FOR UPDATE") {
			return nil, errors.New("lead update must lock its row")
		}
		rows := &workflowRows{columns: []string{"id", "status", "created_at", "payload"}}
		if lead, ok := leads[args[0].Value.(string)]; ok {
			created, _ := time.Parse(time.RFC3339Nano, lead.CreatedAt)
			rows.values = append(rows.values, []driver.Value{lead.ID, lead.Status, created, []byte(jsonValue(lead))})
		}
		return rows, nil
	case strings.HasPrefix(query, "SELECT username,active FROM admins"):
		rows := &workflowRows{columns: []string{"username", "active"}}
		if owner, ok := c.state.owners[args[0].Value.(string)]; ok {
			rows.values = append(rows.values, []driver.Value{owner.Username, owner.Active})
		}
		return rows, nil
	case strings.HasPrefix(query, "SELECT id,username,role,active FROM admins"):
		rows := &workflowRows{columns: []string{"id", "username", "role", "active"}}
		for _, owner := range c.state.owners {
			if owner.Active {
				rows.values = append(rows.values, []driver.Value{owner.ID, owner.Username, owner.Role, owner.Active})
			}
		}
		return rows, nil
	case query == "SELECT payload FROM configs WHERE config_key=?":
		return &workflowRows{columns: []string{"payload"}}, nil
	case strings.HasPrefix(query, "SELECT COUNT(*) FROM auth_sessions"):
		return &workflowRows{columns: []string{"count"}, values: [][]driver.Value{{int64(1)}}}, nil
	case strings.HasPrefix(query, "SELECT active,role,username,password_hash FROM admins"):
		rows := &workflowRows{columns: []string{"active", "role", "username", "password_hash"}}
		if owner, ok := c.state.owners[args[0].Value.(string)]; ok {
			rows.values = append(rows.values, []driver.Value{owner.Active, owner.Role, owner.Username, owner.PasswordHash})
		}
		return rows, nil
	case strings.HasPrefix(query, "SELECT payload FROM partner_lead_history"):
		rows := &workflowRows{columns: []string{"payload"}}
		for _, entry := range c.state.history {
			if entry.LeadID == args[0].Value.(string) {
				rows.values = append(rows.values, []driver.Value{[]byte(jsonValue(entry))})
			}
		}
		return rows, nil
	case strings.HasPrefix(query, "SELECT id,status,created_at,payload FROM partner_leads"):
		if strings.Contains(query, "LIMIT") || !strings.Contains(query, leadPendingReminderCondition) {
			return nil, errors.New("reminders must query all open pending leads")
		}
		pending := []Lead{}
		for _, lead := range leads {
			if lead.ReminderAt != "" && !lead.ReminderDone && lead.Status != "WON" && lead.Status != "LOST" {
				pending = append(pending, lead)
			}
		}
		sort.Slice(pending, func(i, j int) bool { return pending[i].ReminderAt < pending[j].ReminderAt })
		rows := &workflowRows{columns: []string{"id", "status", "created_at", "payload"}}
		for _, lead := range pending {
			created, _ := time.Parse(time.RFC3339Nano, lead.CreatedAt)
			rows.values = append(rows.values, []driver.Value{lead.ID, lead.Status, created, []byte(jsonValue(lead))})
		}
		return rows, nil
	case strings.HasPrefix(query, "SELECT id FROM partner_leads WHERE phone_hash"):
		return &workflowRows{columns: []string{"id"}}, nil
	}
	return nil, errors.New("unexpected query: " + query)
}
func (c *workflowConn) ExecContext(_ context.Context, query string, args []driver.NamedValue) (driver.Result, error) {
	if strings.HasPrefix(query, "INSERT INTO partner_leads") {
		c.state.mu.Lock()
		defer c.state.mu.Unlock()
		var lead Lead
		if err := json.Unmarshal([]byte(args[3].Value.(string)), &lead); err != nil {
			return nil, err
		}
		c.state.leads[lead.ID] = lead
		return driver.RowsAffected(1), nil
	}
	if !c.locked {
		return nil, errors.New("update, history and audit must use the same transaction")
	}
	switch {
	case strings.HasPrefix(query, "UPDATE partner_leads"):
		var lead Lead
		if err := json.Unmarshal([]byte(args[1].Value.(string)), &lead); err != nil {
			return nil, err
		}
		c.working[lead.ID] = lead
	case strings.HasPrefix(query, "INSERT INTO partner_lead_history"):
		if c.state.historyFailure {
			return nil, errors.New("history unavailable")
		}
		var entry LeadHistory
		if err := json.Unmarshal([]byte(args[3].Value.(string)), &entry); err != nil {
			return nil, err
		}
		c.pendingHistory = append(c.pendingHistory, entry)
	case strings.HasPrefix(query, "INSERT INTO audit_logs"):
		if c.state.auditFailure {
			return nil, errors.New("audit unavailable")
		}
		c.pendingAudits++
	default:
		return nil, errors.New("unexpected mutation: " + query)
	}
	return driver.RowsAffected(1), nil
}

type workflowRows struct {
	columns []string
	values  [][]driver.Value
}

func (r *workflowRows) Columns() []string { return r.columns }
func (r *workflowRows) Close() error      { return nil }
func (r *workflowRows) Next(out []driver.Value) error {
	if len(r.values) == 0 {
		return io.EOF
	}
	copy(out, r.values[0])
	r.values = r.values[1:]
	return nil
}

func workflowServer(t *testing.T) (*Server, *workflowDB) {
	t.Helper()
	state := &workflowDB{leads: map[string]Lead{"lead": {ID: "lead", Status: "NEW", CreatedAt: "2026-10-01T00:00:00Z", FollowUpNote: "历史备注"}}, owners: map[string]Admin{"admin": {ID: "admin", Username: "运营", Role: "OPERATOR", Active: true, PasswordHash: "hash"}, "disabled": {ID: "disabled", Username: "停用", Active: false}}}
	db := sql.OpenDB(workflowConnector{state})
	t.Cleanup(func() { db.Close() })
	return &Server{store: &Store{db: db}}, state
}

func TestLeadWorkflowAtomicHistoryAuditAndNoOp(t *testing.T) {
	actor := Claims{Subject: "admin", Username: "运营"}
	for _, failure := range []string{"history", "audit", "none"} {
		t.Run(failure, func(t *testing.T) {
			s, state := workflowServer(t)
			state.historyFailure, state.auditFailure = failure == "history", failure == "audit"
			lead, err := s.updateLead(context.Background(), actor, json.RawMessage(`{"leadId":"lead","expectedVersion":0,"ownerId":"admin","followUpNote":"再次沟通"}`))
			if failure != "none" {
				if err == nil || state.leads["lead"].Version != 0 || state.leads["lead"].OwnerID != "" || len(state.history) != 0 || state.audits != 0 {
					t.Fatalf("partial action escaped rollback: %+v %+v %v", lead, state, err)
				}
				return
			}
			if err != nil || lead.Version != 1 || lead.OwnerName != "运营" || len(state.history) != 1 || state.audits != 1 || state.history[0].ActorID != actor.Subject || strings.Join(state.history[0].Changes, ",") != "owner,note" {
				t.Fatalf("action was not committed together: %+v %+v %v", lead, state, err)
			}
			entries, err := s.store.leadHistory(context.Background(), "lead")
			if err != nil || len(entries) != 2 || entries[0].ID != state.history[0].ID || entries[1].Note != "历史备注" {
				t.Fatalf("legacy history lost or mirror duplicated: %+v %v", entries, err)
			}
			_, err = s.updateLead(context.Background(), actor, json.RawMessage(`{"leadId":"lead","expectedVersion":1,"status":"NEW","followUpNote":""}`))
			if err != nil || state.leads["lead"].Version != 1 || len(state.history) != 1 || state.audits != 1 {
				t.Fatal("no-op created history or changed version", err)
			}
			_, err = s.updateLead(context.Background(), actor, json.RawMessage(`{"leadId":"lead","expectedVersion":1,"followUpNote":"再次沟通"}`))
			if err != nil || state.leads["lead"].Version != 2 || len(state.history) != 2 || state.history[0].OwnerName != "运营" {
				t.Fatal("repeat follow-up lost, or prior snapshot changed", err)
			}
			_, err = s.updateLead(context.Background(), actor, json.RawMessage(`{"leadId":"lead","expectedVersion":2,"ownerId":""}`))
			if err != nil || state.leads["lead"].OwnerName != "" || state.history[0].OwnerName != "运营" || len(state.history) != 3 {
				t.Fatal("reassignment changed an earlier immutable snapshot", err)
			}
		})
	}
}

func TestLeadWorkflowConcurrencyRejectsStaleVersion(t *testing.T) {
	s, state := workflowServer(t)
	actor := Claims{Subject: "admin", Username: "运营"}
	results := make(chan error, 2)
	for _, note := range []string{"第一人", "第二人"} {
		go func(note string) {
			_, err := s.updateLead(context.Background(), actor, json.RawMessage(jsonValue(M{"leadId": "lead", "expectedVersion": 0, "followUpNote": note})))
			results <- err
		}(note)
	}
	passed, conflicts := 0, 0
	for range 2 {
		err := <-results
		if err == nil {
			passed++
		} else if strings.Contains(err.Error(), "版本") {
			conflicts++
		} else {
			t.Fatal(err)
		}
	}
	if passed != 1 || conflicts != 1 || state.leads["lead"].Version != 1 || len(state.history) != 1 || state.audits != 1 {
		t.Fatalf("stale writer published: passed=%d conflicts=%d state=%+v", passed, conflicts, state)
	}
}

func TestLeadWorkflowOwnerValidationAndOperatorAccess(t *testing.T) {
	s, state := workflowServer(t)
	for _, owner := range []string{"disabled", "missing"} {
		if _, err := s.updateLead(context.Background(), Claims{}, json.RawMessage(jsonValue(M{"leadId": "lead", "expectedVersion": 0, "ownerId": owner}))); err == nil {
			t.Fatal("inactive or absent owner accepted")
		}
	}
	if state.leads["lead"].Version != 0 || len(state.history) != 0 {
		t.Fatal("invalid owner changed the lead")
	}
	t.Setenv("TOKEN_SECRET", strings.Repeat("workflow-secret", 3))
	token, err := adminToken(state.owners["admin"])
	if err != nil {
		t.Fatal(err)
	}
	r := httptest.NewRequest("POST", "/api", nil)
	r.Header.Set("Authorization", "Bearer "+token)
	result, err := s.admin(r, Request{Action: "admin.lead.owners", Data: json.RawMessage(`{}`)})
	if err != nil || len(result.([]LeadOwner)) != 1 || result.([]LeadOwner)[0].ID != "admin" {
		t.Fatalf("operators cannot assign active owners: %v %v", result, err)
	}
}

func TestLeadWorkflowPublicSubmitCannotInjectAdminFields(t *testing.T) {
	s, state := workflowServer(t)
	_, err := s.submitLead(context.Background(), json.RawMessage(`{"organization":"企业","contact":"张三","phone":"13800000000","region":"杭州","volume":"100斤","consent":true,"status":"WON","ownerId":"admin","ownerName":"伪造","reminderAt":"2099-01-01T00:00:00Z","reminderDone":true,"version":999,"followUpNote":"伪造记录","followUps":[{"note":"伪造历史"}]}`))
	if err != nil {
		t.Fatal(err)
	}
	for id, lead := range state.leads {
		if id == "lead" {
			continue
		}
		if lead.Status != "NEW" || lead.OwnerID != "" || lead.OwnerName != "" || lead.ReminderAt != "" || lead.ReminderDone || lead.Version != 0 || lead.FollowUpNote != "" || len(lead.FollowUps) != 0 {
			t.Fatalf("public client injected administrator fields: %+v", lead)
		}
		return
	}
	t.Fatal("lead not inserted")
}

func TestLeadWorkflowReminderListHasNoPaginationCap(t *testing.T) {
	s, state := workflowServer(t)
	for i := range 600 {
		id := jsonValue(i)
		state.leads[id] = Lead{ID: id, Status: "CONTACTED", CreatedAt: "2026-10-01T00:00:00Z", ReminderAt: time.Now().Add(time.Duration(i) * time.Hour).UTC().Format(time.RFC3339Nano)}
	}
	state.leads["closed"] = Lead{ID: "closed", Status: "WON", ReminderAt: "2099-01-01T00:00:00Z"}
	state.leads["done"] = Lead{ID: "done", Status: "NEW", ReminderAt: "2099-01-01T00:00:00Z", ReminderDone: true}
	items, err := s.store.leadReminders(context.Background())
	if err != nil || len(items) != 600 {
		t.Fatalf("reminders truncated or closed/completed leads included: %d %v", len(items), err)
	}
	where, args := leadSearchWhere(LeadSearch{OwnerID: "admin", Reminder: "due"})
	if len(args) != 1 || args[0] != "admin" || !strings.Contains(where, leadPendingReminderCondition) || !strings.Contains(where, "<=UTC_TIMESTAMP(6)") {
		t.Fatalf("owner/due filters absent: %s %+v", where, args)
	}
}
