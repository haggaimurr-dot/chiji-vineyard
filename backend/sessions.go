package main

import (
	"crypto/rand"
	"encoding/hex"
	"errors"
	"net/http"
	"strings"
	"time"
)

// Session IDs are independent of credentials and never contain the bearer token.
func (s *Server) issueSession(r *http.Request, c Claims) (string, error) {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	c.SessionID = hex.EncodeToString(b)
	token, err := signToken(c)
	if err != nil {
		return "", err
	}
	tx, err := s.store.db.BeginTx(r.Context(), nil)
	if err != nil {
		return "", err
	}
	defer tx.Rollback()
	if c.Kind == "admin" {
		a, e := lockPersonalAccount(r.Context(), tx, c.Subject)
		if e != nil {
			return "", e
		}
		if c.Credential != adminCredential(a.PasswordHash) {
			return "", errors.New("登录凭据已变更，请重新登录")
		}
	}
	_, err = tx.ExecContext(r.Context(), "INSERT INTO auth_sessions(id,subject,kind,credential,user_agent,ip_address,created_at,expires_at) VALUES(?,?,?,?,?,?,?,?)", c.SessionID, c.Subject, c.Kind, c.Credential, clean(r.UserAgent(), 300), clean(loginPeer(r), 64), time.Now().Unix(), c.Exp)
	if err != nil {
		return "", err
	}
	if err = tx.Commit(); err != nil {
		return "", err
	}
	return token, nil
}

func (s *Store) checkSession(r *http.Request, c Claims) error {
	if c.SessionID == "" {
		return errors.New("登录会话已升级，请重新登录")
	}
	var count int
	query := "SELECT COUNT(*) FROM auth_sessions WHERE id=? AND subject=? AND kind=? AND revoked_at IS NULL AND expires_at>?"
	args := []any{c.SessionID, c.Subject, c.Kind, time.Now().Unix()}
	if c.Kind == "mini" {
		query += " AND NOT EXISTS (SELECT 1 FROM account_closures WHERE subject_hash=?)"
		args = append(args, articleReaderHash(c.Subject))
	}
	err := s.db.QueryRowContext(r.Context(), query, args...).Scan(&count)
	if err != nil || count != 1 {
		return errors.New("登录会话已失效，请重新登录")
	}
	return nil
}

type loginSession struct {
	ID        string `json:"id"`
	UserAgent string `json:"userAgent"`
	IPAddress string `json:"ipAddress"`
	CreatedAt int64  `json:"createdAt"`
	ExpiresAt int64  `json:"expiresAt"`
	Current   bool   `json:"current"`
}

func (s *Server) personalSessions(r *http.Request, actor Claims, req Request) (any, error) {
	ctx := r.Context()
	if strings.HasSuffix(req.Action, ".list") {
		var d struct {
			Limit  int `json:"limit"`
			Offset int `json:"offset"`
		}
		if err := decode(req.Data, &d); err != nil {
			return nil, err
		}
		if d.Limit == 0 {
			d.Limit = 20
		}
		if d.Limit < 1 || d.Limit > 100 || d.Offset < 0 || d.Offset > 1000000 {
			return nil, errors.New("会话分页参数无效")
		}
		rows, err := s.store.db.QueryContext(ctx, "SELECT id,user_agent,ip_address,created_at,expires_at FROM auth_sessions WHERE subject=? AND kind=? AND credential=? AND revoked_at IS NULL AND expires_at>? ORDER BY created_at DESC,id DESC LIMIT ? OFFSET ?", actor.Subject, actor.Kind, actor.Credential, time.Now().Unix(), d.Limit+1, d.Offset)
		if err != nil {
			return nil, err
		}
		defer rows.Close()
		items := []loginSession{}
		for rows.Next() {
			var item loginSession
			if err = rows.Scan(&item.ID, &item.UserAgent, &item.IPAddress, &item.CreatedAt, &item.ExpiresAt); err != nil {
				return nil, err
			}
			item.Current = item.ID == actor.SessionID
			items = append(items, item)
		}
		if err = rows.Err(); err != nil {
			return nil, err
		}
		hasMore := len(items) > d.Limit
		if hasMore {
			items = items[:d.Limit]
		}
		return M{"items": items, "hasMore": hasMore}, nil
	}
	id := actor.SessionID
	if strings.HasSuffix(req.Action, ".revoke") {
		var d struct {
			SessionID string `json:"sessionId"`
		}
		if err := decode(req.Data, &d); err != nil {
			return nil, err
		}
		decoded, err := hex.DecodeString(d.SessionID)
		if err != nil || len(decoded) != 32 {
			return nil, errors.New("会话标识无效")
		}
		id = d.SessionID
	}
	tx, err := s.store.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	result, err := tx.ExecContext(ctx, "UPDATE auth_sessions SET revoked_at=COALESCE(revoked_at,?) WHERE id=? AND subject=? AND kind=?", time.Now().Unix(), id, actor.Subject, actor.Kind)
	if err != nil {
		return nil, err
	}
	// Do not disclose whether a session belongs to another account.
	if _, err = result.RowsAffected(); err != nil {
		return nil, err
	}
	if actor.Kind == "admin" {
		_, err = tx.ExecContext(ctx, "INSERT INTO audit_logs(admin_id,username,action,target_id,detail) VALUES(?,?,?,?,?)", actor.Subject, actor.Username, req.Action, id, jsonValue(M{"current": id == actor.SessionID}))
		if err != nil {
			return nil, err
		}
	}
	if err = tx.Commit(); err != nil {
		return nil, err
	}
	return M{"sessionRevoked": id == actor.SessionID}, nil
}
