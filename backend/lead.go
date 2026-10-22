package main

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"strings"
	"time"
	"unicode/utf8"
)

type LeadSearch struct {
	Keyword  string `json:"keyword"`
	Status   string `json:"status"`
	Channel  string `json:"channel"`
	Region   string `json:"region"`
	OwnerID  string `json:"ownerId"`
	Reminder string `json:"reminder"`
	Limit    int    `json:"limit"`
	Offset   int    `json:"offset"`
}

type LeadListResult struct {
	Items    []Lead   `json:"items"`
	Total    int      `json:"total"`
	Channels []string `json:"channels"`
	Regions  []string `json:"regions"`
	Limit    int      `json:"limit"`
	Offset   int      `json:"offset"`
}

func validLeadStatus(status string) bool {
	switch status {
	case "NEW", "CONTACTED", "QUALIFIED", "WON", "LOST":
		return true
	default:
		return false
	}
}

func normalizeLeadSearch(d LeadSearch) (LeadSearch, error) {
	d.Keyword = strings.TrimSpace(d.Keyword)
	d.Status = strings.TrimSpace(d.Status)
	d.Channel = strings.TrimSpace(d.Channel)
	d.Region = strings.TrimSpace(d.Region)
	d.OwnerID = strings.TrimSpace(d.OwnerID)
	d.Reminder = strings.TrimSpace(d.Reminder)
	if d.Status != "" && !validLeadStatus(d.Status) {
		return d, errors.New("线索状态无效")
	}
	if utf8.RuneCountInString(d.OwnerID) > 80 {
		return d, errors.New("负责人编号无效")
	}
	switch d.Reminder {
	case "", "due", "upcoming", "done", "none":
	default:
		return d, errors.New("提醒筛选条件无效")
	}
	if d.Limit <= 0 {
		d.Limit = 20
	} else if d.Limit > 100 {
		d.Limit = 100
	}
	if d.Offset < 0 {
		d.Offset = 0
	}
	return d, nil
}

func leadSearchWhere(d LeadSearch) (string, []any) {
	clauses := []string{"1=1"}
	args := []any{}
	if d.Status != "" {
		clauses = append(clauses, "status=?")
		args = append(args, d.Status)
	}
	if d.OwnerID == "__unassigned__" {
		clauses = append(clauses, "COALESCE(JSON_UNQUOTE(JSON_EXTRACT(payload,'$.ownerId')),'')=''")
	} else if d.OwnerID != "" {
		clauses = append(clauses, "JSON_UNQUOTE(JSON_EXTRACT(payload,'$.ownerId'))=?")
		args = append(args, d.OwnerID)
	}
	switch d.Reminder {
	case "due", "upcoming":
		clauses = append(clauses, leadPendingReminderCondition)
		comparison := "<="
		if d.Reminder == "upcoming" {
			comparison = ">"
		}
		clauses = append(clauses, leadReminderTimeSQL+comparison+"UTC_TIMESTAMP(6)")
	case "done":
		clauses = append(clauses, leadReminderValueSQL+"<>''", leadReminderDoneSQL+"='true'")
	case "none":
		clauses = append(clauses, leadReminderValueSQL+"=''")
	}
	for _, filter := range []struct{ field, value string }{{"channel", d.Channel}, {"region", d.Region}} {
		if filter.value != "" {
			clauses = append(clauses, "TRIM(JSON_UNQUOTE(JSON_EXTRACT(payload,'$."+filter.field+"')))=?")
			args = append(args, filter.value)
		}
	}
	fields := []string{"organization", "contact", "phone", "region", "channel", "volume", "demand", "followUpNote", "ownerName", "customerProfile.email", "customerProfile.address", "customerProfile.industry", "customerProfile.notes", "contacts", "contracts", "receipts"}
	values := make([]string, 0, len(fields))
	for _, field := range fields {
		values = append(values, "COALESCE(JSON_UNQUOTE(JSON_EXTRACT(payload,'$."+field+"')),'')")
	}
	text := "LOWER(CONCAT_WS(' '," + strings.Join(values, ",") + "))"
	for _, word := range strings.Fields(strings.ToLower(d.Keyword)) {
		// LOCATE treats %, _ and backslash literally. JSON_SEARCH decodes history
		// strings and uses an explicit escape so these characters stay literal too.
		clauses = append(clauses, "(LOCATE(?,"+text+")>0 OR JSON_SEARCH(LOWER(COALESCE(JSON_EXTRACT(payload,'$.followUps[*].note'),JSON_ARRAY())),'one',?,'!') IS NOT NULL)")
		pattern := "%" + strings.NewReplacer("!", "!!", "%", "!%", "_", "!_").Replace(word) + "%"
		args = append(args, word, pattern)
	}
	return " WHERE " + strings.Join(clauses, " AND "), args
}

func (s *Store) searchLeads(ctx context.Context, search LeadSearch) (LeadListResult, error) {
	d, err := normalizeLeadSearch(search)
	if err != nil {
		return LeadListResult{}, err
	}
	result := LeadListResult{Items: []Lead{}, Channels: []string{}, Regions: []string{}, Limit: d.Limit, Offset: d.Offset}
	where, args := leadSearchWhere(d)
	if err = s.db.QueryRowContext(ctx, "SELECT COUNT(*) FROM partner_leads"+where, args...).Scan(&result.Total); err != nil {
		return result, err
	}
	pageArgs := append(append([]any{}, args...), d.Limit, d.Offset)
	rows, err := s.db.QueryContext(ctx, "SELECT id,status,created_at,payload FROM partner_leads"+where+" ORDER BY created_at DESC,id DESC LIMIT ? OFFSET ?", pageArgs...)
	if err != nil {
		return result, err
	}
	for rows.Next() {
		lead, e := scanLead(rows)
		if e != nil {
			rows.Close()
			return result, e
		}
		result.Items = append(result.Items, *lead)
	}
	if err = rows.Err(); err != nil {
		rows.Close()
		return result, err
	}
	rows.Close()
	if result.Channels, err = s.leadFilterValues(ctx, "channel"); err != nil {
		return result, err
	}
	result.Regions, err = s.leadFilterValues(ctx, "region")
	return result, err
}

func (s *Store) leadFilterValues(ctx context.Context, field string) ([]string, error) {
	// field is selected only by the two fixed internal callers above.
	expression := "JSON_EXTRACT(payload,'$." + field + "')"
	rows, err := s.db.QueryContext(ctx, "SELECT DISTINCT TRIM(JSON_UNQUOTE("+expression+")) AS value FROM partner_leads WHERE JSON_TYPE("+expression+")='STRING' AND TRIM(JSON_UNQUOTE("+expression+"))<>'' ORDER BY value")
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	values := []string{}
	for rows.Next() {
		var value string
		if err = rows.Scan(&value); err != nil {
			return nil, err
		}
		values = append(values, value)
	}
	return values, rows.Err()
}

func ensureLeadHistory(l *Lead) {
	if len(l.FollowUps) == 0 && strings.TrimSpace(l.FollowUpNote) != "" {
		l.FollowUps = []LeadFollowUp{{ID: "legacy-" + l.ID, Note: l.FollowUpNote, Status: l.Status, PreviousStatus: l.Status, CreatedAt: "", Username: "历史记录", Legacy: true}}
	}
	if l.FollowUps == nil {
		l.FollowUps = []LeadFollowUp{}
	}
}

func scanLead(row interface{ Scan(...any) error }) (*Lead, error) {
	var raw []byte
	var id, status string
	var created time.Time
	if err := row.Scan(&id, &status, &created, &raw); err != nil {
		return nil, err
	}
	var l Lead
	if err := json.Unmarshal(raw, &l); err != nil {
		return nil, err
	}
	l.ID, l.Status, l.CreatedAt = id, status, created.UTC().Format(time.RFC3339Nano)
	ensureLeadHistory(&l)
	return &l, nil
}

func (s *Store) lead(ctx context.Context, q interface {
	QueryRowContext(context.Context, string, ...any) *sql.Row
}, id string, lock bool) (*Lead, error) {
	query := "SELECT id,status,created_at,payload FROM partner_leads WHERE id=?"
	if lock {
		query += " FOR UPDATE"
	}
	l, err := scanLead(q.QueryRowContext(ctx, query, id))
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	return l, err
}

func appendLeadFollowUp(l *Lead, status, note string, actor Claims) error {
	status, note = strings.TrimSpace(status), strings.TrimSpace(note)
	if !validLeadStatus(status) {
		return errors.New("线索状态无效")
	}
	if utf8.RuneCountInString(note) > 2000 {
		return errors.New("跟进备注不能超过 2000 字")
	}
	if status == l.Status && note == "" {
		return errors.New("请输入跟进备注或修改线索状态")
	}
	ensureLeadHistory(l)
	l.FollowUps = append(l.FollowUps, LeadFollowUp{ID: randomID("FOLLOWUP"), Note: note, Status: status, PreviousStatus: l.Status, CreatedAt: nowString(), Username: actor.Username})
	l.Status = status
	if note != "" {
		l.FollowUpNote = note
	}
	return nil
}

func (s *Server) updateLead(ctx context.Context, actor Claims, raw json.RawMessage) (*Lead, error) {
	return s.updateLeadWorkflow(ctx, actor, raw)
}
