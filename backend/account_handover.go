package main

import (
	"database/sql"
	"context"
	"encoding/json"
	"errors"
	"time"
)

// 离职交接：把离职管理员名下的合作线索批量转给另一位在职管理员，并记交接历史。
// 仅 SUPER_ADMIN 可操作；停用账号本身走 admin.account.save（停用即撤销全部会话）。
func (s *Server) accountHandover(ctx context.Context, actor Claims, raw json.RawMessage) (any, error) {
	if actor.Role != "SUPER_ADMIN" {
		return nil, errors.New("仅超级管理员可以操作")
	}
	var d struct {
		FromAdminID string `json:"fromAdminId"`
		ToAdminID   string `json:"toAdminId"`
	}
	if err := decode(raw, &d); err != nil {
		return nil, err
	}
	if d.FromAdminID == "" || d.ToAdminID == "" {
		return nil, errors.New("请选择交接双方账号")
	}
	if d.FromAdminID == d.ToAdminID {
		return nil, errors.New("交接双方不能是同一账号")
	}
	tx, err := s.store.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	var fromName, toName string
	var toActive bool
	err = tx.QueryRowContext(ctx, "SELECT username FROM admins WHERE id=?", d.FromAdminID).Scan(&fromName)
	if err != nil {
		return nil, errors.New("交出方账号不存在")
	}
	err = tx.QueryRowContext(ctx, "SELECT username,active FROM admins WHERE id=?", d.ToAdminID).Scan(&toName, &toActive)
	if err != nil {
		return nil, errors.New("接收方账号不存在")
	}
	if !toActive {
		return nil, errors.New("接收方账号已停用")
	}
	rows, err := tx.QueryContext(ctx, "SELECT id,payload FROM partner_leads WHERE JSON_UNQUOTE(JSON_EXTRACT(payload,'$.ownerId'))=?", d.FromAdminID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	now := time.Now()
	transferred := 0
	for rows.Next() {
		var id string
		var payload []byte
		if err = rows.Scan(&id, &payload); err != nil {
			return nil, err
		}
		var lead Lead
		if err = json.Unmarshal(payload, &lead); err != nil {
			return nil, err
		}
		lead.OwnerID = d.ToAdminID
		lead.OwnerName = toName
		lead.Version++
		entry := LeadHistory{ID: randomID("LH"), LeadID: id, ActorID: actor.Subject, ActorName: actor.Username, CreatedAt: now.Format(time.RFC3339Nano), Note: "离职交接：" + fromName + " → " + toName, Status: lead.Status, OwnerID: lead.OwnerID, OwnerName: lead.OwnerName, Changes: []string{"ownerId", "ownerName"}}
		if _, err = tx.ExecContext(ctx, "UPDATE partner_leads SET payload=? WHERE id=?", jsonValue(lead), id); err != nil {
			return nil, err
		}
		if _, err = tx.ExecContext(ctx, "INSERT INTO partner_lead_history(id,lead_id,created_at,payload) VALUES(?,?,?,?)", entry.ID, entry.LeadID, now, jsonValue(entry)); err != nil {
			return nil, err
		}
		transferred++
	}
	if err = rows.Err(); err != nil {
		return nil, err
	}
	if _, err = tx.ExecContext(ctx, "INSERT INTO audit_logs(admin_id,username,action,target_id,detail) VALUES(?,?,?,?,?)", actor.Subject, actor.Username, "ADMIN_ACCOUNT_HANDOVER", d.FromAdminID, jsonValue(M{"from": fromName, "to": toName, "toAdminId": d.ToAdminID, "transferred": transferred})); err != nil {
		return nil, err
	}
	if err = tx.Commit(); err != nil {
		return nil, err
	}
	return M{"transferred": transferred, "from": fromName, "to": toName}, nil
}

// revokeAdminSessions 撤销某管理员的全部会话（停用/离职时调用）。
func revokeAdminSessions(ctx context.Context, tx *sql.Tx, adminID string) error {
	_, err := tx.ExecContext(ctx, "UPDATE auth_sessions SET revoked_at=COALESCE(revoked_at,?) WHERE subject=? AND kind='admin' AND revoked_at IS NULL", time.Now().Unix(), adminID)
	return err
}

