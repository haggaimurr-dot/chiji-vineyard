package main

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"sort"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"
)

const leadReminderValueSQL = "COALESCE(JSON_UNQUOTE(JSON_EXTRACT(payload,'$.reminderAt')),'')"
const leadReminderDoneSQL = "COALESCE(JSON_UNQUOTE(JSON_EXTRACT(payload,'$.reminderDone')),'false')"
const leadReminderTimeSQL = "CAST(REPLACE(REPLACE(" + leadReminderValueSQL + ",'T',' '),'Z','') AS DATETIME(6))"
const leadPendingReminderCondition = "status IN ('NEW','CONTACTED','QUALIFIED') AND " + leadReminderValueSQL + "<>'' AND " + leadReminderDoneSQL + "='false'"
const leadPendingReminderWhere = " WHERE " + leadPendingReminderCondition

// LeadHistory is an immutable snapshot of a single administrator action.
type LeadHistory struct {
	Contacts          []LeadContact   `json:"contacts"`
	Contracts         []LeadContract  `json:"contracts"`
	Receipts          []LeadReceipt   `json:"receipts"`
	ExternalReminder  bool            `json:"externalReminder"`
	CustomerProfile   CustomerProfile `json:"customerProfile"`
	OpportunityAmount int64           `json:"opportunityAmount"`
	ID                string          `json:"id"`
	LeadID            string          `json:"leadId"`
	ActorID           string          `json:"actorId"`
	ActorName         string          `json:"actorName"`
	CreatedAt         string          `json:"createdAt"`
	Note              string          `json:"note"`
	Status            string          `json:"status"`
	OwnerID           string          `json:"ownerId"`
	OwnerName         string          `json:"ownerName"`
	ReminderAt        string          `json:"reminderAt"`
	ReminderDone      bool            `json:"reminderDone"`
	Changes           []string        `json:"changes"`
}

type LeadOwner struct {
	ID       string `json:"id"`
	Username string `json:"username"`
	Role     string `json:"role"`
	Active   bool   `json:"active"`
}

type leadUpdate struct {
	Contacts          *[]LeadContact   `json:"contacts"`
	Contracts         *[]LeadContract  `json:"contracts"`
	Receipts          *[]LeadReceipt   `json:"receipts"`
	ExternalReminder  *bool            `json:"externalReminder"`
	CustomerProfile   *CustomerProfile `json:"customerProfile"`
	OpportunityAmount *int64           `json:"opportunityAmount"`
	LeadID            string           `json:"leadId"`
	ExpectedVersion   *int             `json:"expectedVersion"`
	Status            *string          `json:"status"`
	FollowUpNote      *string          `json:"followUpNote"`
	OwnerID           *string          `json:"ownerId"`
	ReminderAt        *string          `json:"reminderAt"`
	ReminderDone      *bool            `json:"reminderDone"`
}

func decodeLeadUpdate(raw json.RawMessage) (leadUpdate, error) {
	var d leadUpdate
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&d); err != nil {
		return d, errors.New("线索更新参数无效")
	}
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(raw, &fields); err != nil {
		return d, errors.New("线索更新参数无效")
	}
	for _, key := range []string{"status", "followUpNote", "ownerId", "reminderAt", "reminderDone", "customerProfile", "opportunityAmount", "contacts", "contracts", "receipts", "externalReminder"} {
		if value, ok := fields[key]; ok && bytes.Equal(bytes.TrimSpace(value), []byte("null")) {
			return d, errors.New("线索更新字段不能为 null")
		}
	}
	if d.CustomerProfile != nil {
		if err := validateCustomerProfile(d.CustomerProfile); err != nil {
			return d, err
		}
	}
	if d.OpportunityAmount != nil && (*d.OpportunityAmount < 0 || *d.OpportunityAmount > 100000000000) {
		return d, errors.New("商机金额必须为 0–10亿元，最多两位小数")
	}
	d.LeadID = strings.TrimSpace(d.LeadID)
	if d.LeadID == "" || utf8.RuneCountInString(d.LeadID) > 80 {
		return d, errors.New("线索编号无效")
	}
	if d.ExpectedVersion == nil || *d.ExpectedVersion < 0 {
		return d, errors.New("请提供有效的线索版本")
	}
	if d.Status != nil {
		*d.Status = strings.TrimSpace(*d.Status)
		if !validLeadStatus(*d.Status) {
			return d, errors.New("线索状态无效")
		}
	}
	if d.FollowUpNote != nil {
		*d.FollowUpNote = strings.TrimSpace(*d.FollowUpNote)
		if utf8.RuneCountInString(*d.FollowUpNote) > 2000 {
			return d, errors.New("跟进备注不能超过 2000 字")
		}
	}
	if d.OwnerID != nil {
		*d.OwnerID = strings.TrimSpace(*d.OwnerID)
		if utf8.RuneCountInString(*d.OwnerID) > 80 {
			return d, errors.New("负责人编号无效")
		}
	}
	return d, nil
}

func sameReminderTime(a, b string) bool {
	if a == b {
		return true
	}
	x, e1 := time.Parse(time.RFC3339Nano, a)
	y, e2 := time.Parse(time.RFC3339Nano, b)
	return e1 == nil && e2 == nil && x.Equal(y)
}

func applyLeadUpdate(current Lead, d leadUpdate, ownerName string, now time.Time) (Lead, []string, string, error) {
	next := current
	crmChanges, err := applyLeadCRM(&next, current, d)
	if err != nil {
		return current, nil, "", err
	}
	if d.CustomerProfile != nil {
		next.CustomerProfile = *d.CustomerProfile
	}
	if d.OpportunityAmount != nil {
		next.OpportunityAmount = *d.OpportunityAmount
	}
	if d.Status != nil {
		next.Status = *d.Status
	}
	if d.OwnerID != nil {
		next.OwnerID, next.OwnerName = *d.OwnerID, ownerName
	}
	note := ""
	if d.FollowUpNote != nil && *d.FollowUpNote != "" {
		note = *d.FollowUpNote
		next.FollowUpNote = note
	}
	reminderChanged := false
	if d.ReminderAt != nil {
		value := strings.TrimSpace(*d.ReminderAt)
		if value == "" {
			next.ReminderAt, next.ReminderDone = "", false
		} else {
			at, err := time.Parse(time.RFC3339Nano, value)
			if err != nil {
				return current, nil, "", errors.New("提醒时间必须为有效的 ISO 时间")
			}
			value = at.UTC().Format(time.RFC3339Nano)
			reminderChanged = !sameReminderTime(current.ReminderAt, value)
			if reminderChanged {
				next.ReminderDone = false
			}
			next.ReminderAt = value
		}
	}
	if d.ReminderDone != nil {
		if next.ReminderAt == "" && *d.ReminderDone {
			return current, nil, "", errors.New("请先设置提醒时间")
		}
		next.ReminderDone = *d.ReminderDone
	}
	newPendingReminder := next.ReminderAt != "" && !next.ReminderDone && (reminderChanged || current.ReminderDone)
	if next.Status == "WON" || next.Status == "LOST" {
		if newPendingReminder {
			return current, nil, "", errors.New("已成交或已关闭的线索不能设置待办提醒")
		}
		if next.ReminderAt != "" {
			next.ReminderDone = true
		}
	} else if newPendingReminder {
		at, err := time.Parse(time.RFC3339Nano, next.ReminderAt)
		if err != nil || !at.After(now) {
			return current, nil, "", errors.New("新设置的提醒时间必须晚于当前时间")
		}
	}
	changes := crmChanges
	if next.CustomerProfile != current.CustomerProfile {
		changes = append(changes, "profile")
	}
	if next.OpportunityAmount != current.OpportunityAmount {
		changes = append(changes, "amount")
	}
	if next.OwnerID != current.OwnerID || next.OwnerName != current.OwnerName {
		changes = append(changes, "owner")
	}
	if next.Status != current.Status {
		changes = append(changes, "status")
	}
	if note != "" {
		changes = append(changes, "note")
	}
	if next.ReminderAt != current.ReminderAt || next.ReminderDone != current.ReminderDone {
		changes = append(changes, "reminder")
	}
	return next, changes, note, nil
}

func (s *Server) updateLeadWorkflow(ctx context.Context, actor Claims, raw json.RawMessage) (*Lead, error) {
	d, err := decodeLeadUpdate(raw)
	if err != nil {
		return nil, err
	}
	tx, err := s.store.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	current, err := s.store.lead(ctx, tx, d.LeadID, true)
	if err != nil {
		return nil, err
	}
	if current == nil {
		return nil, errors.New("线索不存在")
	}
	if current.Version != *d.ExpectedVersion {
		return nil, errors.New("线索版本已被其他管理员更新，请刷新后重试")
	}
	ownerName := current.OwnerName
	if d.OwnerID != nil {
		ownerName = ""
		if *d.OwnerID != "" {
			var active bool
			err = tx.QueryRowContext(ctx, "SELECT username,active FROM admins WHERE id=? LOCK IN SHARE MODE", *d.OwnerID).Scan(&ownerName, &active)
			if errors.Is(err, sql.ErrNoRows) || (err == nil && !active) {
				return nil, errors.New("负责人不存在或账号已停用")
			}
			if err != nil {
				return nil, err
			}
		}
	}
	now := time.Now().UTC()
	next, changes, note, err := applyLeadUpdate(*current, d, ownerName, now)
	if err != nil {
		return nil, err
	}
	if len(changes) == 0 {
		return current, nil
	}
	next.Version++
	entry := LeadHistory{Contacts: next.Contacts, Contracts: next.Contracts, Receipts: next.Receipts, ExternalReminder: next.ExternalReminder, ID: randomID("LH"), LeadID: next.ID, ActorID: actor.Subject, ActorName: actor.Username, CreatedAt: now.Format(time.RFC3339Nano), Note: note, Status: next.Status, OwnerID: next.OwnerID, OwnerName: next.OwnerName, ReminderAt: next.ReminderAt, ReminderDone: next.ReminderDone, CustomerProfile: next.CustomerProfile, OpportunityAmount: next.OpportunityAmount, Changes: changes}
	// Keep the old read/search contract while the immutable table remains the
	// source of truth. Identical IDs let history reads avoid duplicate records.
	next.FollowUps = append(append([]LeadFollowUp{}, next.FollowUps...), LeadFollowUp{ID: entry.ID, Note: note, Status: next.Status, PreviousStatus: current.Status, CreatedAt: entry.CreatedAt, Username: actor.Username})
	if _, err = tx.ExecContext(ctx, "UPDATE partner_leads SET status=?,payload=? WHERE id=?", next.Status, jsonValue(next), next.ID); err != nil {
		return nil, err
	}
	if _, err = tx.ExecContext(ctx, "INSERT INTO partner_lead_history(id,lead_id,created_at,payload) VALUES(?,?,?,?)", entry.ID, entry.LeadID, now, jsonValue(entry)); err != nil {
		return nil, err
	}
	if _, err = tx.ExecContext(ctx, "INSERT INTO audit_logs(admin_id,username,action,target_id,detail) VALUES(?,?,?,?,?)", actor.Subject, actor.Username, "LEAD_UPDATE", next.ID, jsonValue(M{"historyId": entry.ID, "changes": changes, "previousVersion": current.Version, "version": next.Version, "status": next.Status, "ownerId": next.OwnerID, "reminderAt": next.ReminderAt, "reminderDone": next.ReminderDone})); err != nil {
		return nil, err
	}
	if err = tx.Commit(); err != nil {
		return nil, err
	}
	return &next, nil
}

func (s *Store) leadOwners(ctx context.Context) ([]LeadOwner, error) {
	rows, err := s.db.QueryContext(ctx, "SELECT id,username,role,active FROM admins WHERE active=1 ORDER BY username,id")
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	owners := []LeadOwner{}
	for rows.Next() {
		var owner LeadOwner
		if err = rows.Scan(&owner.ID, &owner.Username, &owner.Role, &owner.Active); err != nil {
			return nil, err
		}
		owners = append(owners, owner)
	}
	return owners, rows.Err()
}

func mergeLeadHistory(l Lead, persisted []LeadHistory) []LeadHistory {
	entries := append([]LeadHistory{}, persisted...)
	seen := map[string]bool{}
	for _, entry := range entries {
		seen[entry.ID] = true
	}
	ensureLeadHistory(&l)
	for i, legacy := range l.FollowUps {
		id := legacy.ID
		if id == "" {
			id = "legacy-" + l.ID + "-" + strconv.Itoa(i)
		}
		if seen[id] {
			continue
		}
		changes := []string{}
		if legacy.PreviousStatus != "" && legacy.PreviousStatus != legacy.Status {
			changes = append(changes, "status")
		}
		if legacy.Note != "" {
			changes = append(changes, "note")
		}
		entries = append(entries, LeadHistory{ID: id, LeadID: l.ID, ActorName: legacy.Username, CreatedAt: legacy.CreatedAt, Note: legacy.Note, Status: legacy.Status, Changes: changes})
		seen[id] = true
	}
	sort.SliceStable(entries, func(i, j int) bool {
		a, ae := time.Parse(time.RFC3339Nano, entries[i].CreatedAt)
		b, be := time.Parse(time.RFC3339Nano, entries[j].CreatedAt)
		if ae != nil {
			return false
		}
		if be != nil {
			return true
		}
		if a.Equal(b) {
			return entries[i].ID > entries[j].ID
		}
		return a.After(b)
	})
	return entries
}

func (s *Store) getLead(ctx context.Context, id string) (*Lead, error) {
	id = strings.TrimSpace(id)
	if id == "" || utf8.RuneCountInString(id) > 80 {
		return nil, errors.New("线索编号无效")
	}
	l, err := s.lead(ctx, s.db, id, false)
	if err != nil {
		return nil, err
	}
	if l == nil {
		return nil, errors.New("线索不存在")
	}
	return l, nil
}

func (s *Store) leadHistory(ctx context.Context, id string) ([]LeadHistory, error) {
	l, err := s.getLead(ctx, id)
	if err != nil {
		return nil, err
	}
	id = l.ID
	rows, err := s.db.QueryContext(ctx, "SELECT payload FROM partner_lead_history WHERE lead_id=? ORDER BY created_at DESC,id DESC", id)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	entries := []LeadHistory{}
	for rows.Next() {
		var raw []byte
		var entry LeadHistory
		if err = rows.Scan(&raw); err != nil {
			return nil, err
		}
		if err = json.Unmarshal(raw, &entry); err != nil {
			return nil, err
		}
		entries = append(entries, entry)
	}
	if err = rows.Err(); err != nil {
		return nil, err
	}
	return mergeLeadHistory(*l, entries), nil
}

func (s *Store) leadReminders(ctx context.Context) ([]Lead, error) {
	rows, err := s.db.QueryContext(ctx, "SELECT id,status,created_at,payload FROM partner_leads"+leadPendingReminderWhere+" ORDER BY "+leadReminderTimeSQL+",id")
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := []Lead{}
	for rows.Next() {
		lead, err := scanLead(rows)
		if err != nil {
			return nil, err
		}
		items = append(items, *lead)
	}
	return items, rows.Err()
}
