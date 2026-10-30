package main

import (
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/json"
	"errors"
	"strings"
	"time"
)

// 活动现场签到核销（新增）：预约用户领取签到码（二维码内容），
// 管理员在签到页输入/扫码核销；已签到重复核销提示，不可重复。

const checkinCodeAlphabet = "ABCDEFGHJKMNPQRSTUVWXYZ23456789"

func newCheckinCode() string {
	b := make([]byte, 8)
	if _, err := rand.Read(b); err != nil {
		return ""
	}
	var sb strings.Builder
	for _, v := range b {
		sb.WriteByte(checkinCodeAlphabet[int(v)%len(checkinCodeAlphabet)])
	}
	return sb.String()
}

func (s *Store) ensureCheckinCode(ctx context.Context, openid, activityID string) (string, error) {
	var exists int
	if err := s.db.QueryRowContext(ctx, "SELECT COUNT(*) FROM activity_reservations WHERE openid=? AND activity_id=?", openid, activityID).Scan(&exists); err != nil {
		return "", err
	}
	if exists == 0 {
		return "", errors.New("请先预约该活动")
	}
	var code string
	if err := s.db.QueryRowContext(ctx, "SELECT code FROM activity_checkins WHERE activity_id=? AND openid=?", activityID, openid).Scan(&code); err == nil {
		return code, nil
	} else if !errors.Is(err, sql.ErrNoRows) {
		return "", err
	}
	for i := 0; i < 5; i++ {
		code = newCheckinCode()
		if code == "" {
			return "", errors.New("签到码生成失败")
		}
		if _, err := s.db.ExecContext(ctx, "INSERT INTO activity_checkins(activity_id,openid,code) VALUES(?,?,?)", activityID, openid, code); err == nil {
			return code, nil
		}
	}
	return "", errors.New("签到码生成失败，请重试")
}

func (s *Store) activityCheckinCode(ctx context.Context, openid string, raw json.RawMessage) (any, error) {
	var d struct {
		ActivityID string `json:"activityId"`
	}
	if err := decode(raw, &d); err != nil {
		return nil, err
	}
	if !validActivityID(d.ActivityID) {
		return nil, errors.New("活动参数无效")
	}
	code, err := s.ensureCheckinCode(ctx, openid, d.ActivityID)
	if err != nil {
		return nil, err
	}
	return M{"code": code}, nil
}

type checkinResult struct {
	Code       string `json:"code"`
	Checked    bool   `json:"checked"`
	ActivityID string `json:"activityId"`
	OpenID     string `json:"openid"`
	CheckedAt  string `json:"checkedAt"`
	CheckedBy  string `json:"checkedBy"`
	Message    string `json:"message"`
}

func (s *Server) activityCheckin(ctx context.Context, actor Claims, raw json.RawMessage) (any, error) {
	var d struct {
		Code string `json:"code"`
	}
	if err := decode(raw, &d); err != nil {
		return nil, err
	}
	code := strings.ToUpper(strings.TrimSpace(d.Code))
	if code == "" {
		return nil, errors.New("请输入签到码")
	}
	var r checkinResult
	var checkedAt sql.NullTime
	var status string
	err := s.store.db.QueryRowContext(ctx, "SELECT code,activity_id,openid,status,checked_in_at,checked_by FROM activity_checkins WHERE code=?", code).
		Scan(&r.Code, &r.ActivityID, &r.OpenID, &status, &checkedAt, &r.CheckedBy)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, errors.New("签到码无效，请核对")
		}
		return nil, err
	}
	if status == "CHECKED" {
		r.Checked = true
		if checkedAt.Valid {
			r.CheckedAt = checkedAt.Time.Format("2006-01-02 15:04:05")
		}
		r.Message = "该签到码已核销，请勿重复签到"
		return r, nil
	}
	res, err := s.store.db.ExecContext(ctx, "UPDATE activity_checkins SET status='CHECKED',checked_in_at=NOW(3),checked_by=? WHERE code=? AND status='PENDING'", actor.Username, code)
	if err != nil {
		return nil, err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		r.Checked = true
		r.Message = "该签到码已核销，请勿重复签到"
		return r, nil
	}
	r.Checked = true
	r.CheckedBy = actor.Username
	r.CheckedAt = time.Now().Format("2006-01-02 15:04:05")
	r.Message = "签到成功"
	s.store.audit(ctx, actor, "ACTIVITY_CHECKIN", code, M{"activityId": r.ActivityID})
	return r, nil
}

func (s *Server) activityCheckinStats(ctx context.Context, raw json.RawMessage) (any, error) {
	var d struct {
		ActivityID string `json:"activityId"`
	}
	if err := decode(raw, &d); err != nil {
		return nil, err
	}
	if d.ActivityID != "" && !validActivityID(d.ActivityID) {
		return nil, errors.New("活动参数无效")
	}
	cond, args := "1=1", []any{}
	if d.ActivityID != "" {
		cond, args = "activity_id=?", []any{d.ActivityID}
	}
	var reserved, checked int
	if err := s.store.db.QueryRowContext(ctx, "SELECT COUNT(*) FROM activity_reservations WHERE "+cond, args...).Scan(&reserved); err != nil {
		return nil, err
	}
	if err := s.store.db.QueryRowContext(ctx, "SELECT COUNT(*) FROM activity_checkins WHERE "+cond+" AND status='CHECKED'", args...).Scan(&checked); err != nil {
		return nil, err
	}
	return M{"reserved": reserved, "checked": checked}, nil
}
