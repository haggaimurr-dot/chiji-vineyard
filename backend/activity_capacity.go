package main

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"time"
)

func addActivityNotice(ctx context.Context, tx *sql.Tx, user string, a HomeActivity, kind string) error {
	_, err := tx.ExecContext(ctx, "INSERT INTO activity_notices(id,openid,activity_id,title,start_date,end_date,kind) VALUES(?,?,?,?,?,?,?)", randomID("an"), user, a.ID, a.Title, a.StartDate, a.EndDate, kind)
	return err
}

// Call only while holding the home config lock. Queue IDs represent admission order.
func promoteActivity(ctx context.Context, tx *sql.Tx, a HomeActivity) error {
	if !a.ReservationEnabled || !homeDateRange(a.StartDate, a.EndDate) || time.Now().In(time.FixedZone("CST", 8*3600)).Format("2006-01-02") > a.EndDate {
		return nil
	}
	var count int
	if err := tx.QueryRowContext(ctx, "SELECT COUNT(*) FROM activity_reservations WHERE activity_id=?", a.ID).Scan(&count); err != nil {
		return err
	}
	for a.Capacity == 0 || count < a.Capacity {
		var user string
		err := tx.QueryRowContext(ctx, "SELECT openid FROM activity_waitlist WHERE activity_id=? ORDER BY id LIMIT 1 FOR UPDATE", a.ID).Scan(&user)
		if errors.Is(err, sql.ErrNoRows) {
			break
		}
		if err != nil {
			return err
		}
		if _, err = tx.ExecContext(ctx, "INSERT IGNORE INTO activity_reservations(openid,activity_id) VALUES(?,?)", user, a.ID); err != nil {
			return err
		}
		if _, err = tx.ExecContext(ctx, "DELETE FROM activity_waitlist WHERE openid=? AND activity_id=?", user, a.ID); err != nil {
			return err
		}
		if err = addActivityNotice(ctx, tx, user, a, "PROMOTED"); err != nil {
			return err
		}
		count++
	}
	return nil
}

func (s *Store) setActivityReservation(ctx context.Context, openid string, raw json.RawMessage) (any, error) {
	var d struct {
		ActivityID string `json:"activityId"`
		Enabled    *bool  `json:"enabled"`
	}
	if err := decode(raw, &d); err != nil {
		return nil, err
	}
	if !validActivityID(d.ActivityID) || d.Enabled == nil {
		return nil, errors.New("预约参数无效")
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	// Serialize joins, cancellations and admin configuration changes on the same row.
	var payload []byte
	err = tx.QueryRowContext(ctx, "SELECT payload FROM configs WHERE config_key='home' FOR UPDATE").Scan(&payload)
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return nil, err
	}
	var h HomeConfig
	if len(payload) > 0 {
		if err = json.Unmarshal(payload, &h); err != nil {
			return nil, err
		}
	}
	var a HomeActivity
	for _, item := range h.Activities {
		if item.ID == d.ActivityID {
			a = item
			break
		}
	}
	if !*d.Enabled {
		if _, err = tx.ExecContext(ctx, "DELETE FROM activity_reservations WHERE openid=? AND activity_id=?", openid, d.ActivityID); err != nil {
			return nil, err
		}
		if _, err = tx.ExecContext(ctx, "DELETE FROM activity_waitlist WHERE openid=? AND activity_id=?", openid, d.ActivityID); err != nil {
			return nil, err
		}
		if a.ID != "" {
			if err = promoteActivity(ctx, tx, a); err != nil {
				return nil, err
			}
		}
		return M{"enabled": false, "status": "NONE"}, tx.Commit()
	}
	if err = reservableActivity(h, d.ActivityID, time.Now()); err != nil {
		return nil, err
	}
	// Honor existing candidates before newcomers, including after a capacity increase.
	if err = promoteActivity(ctx, tx, a); err != nil {
		return nil, err
	}
	var exists int
	if err = tx.QueryRowContext(ctx, "SELECT COUNT(*) FROM activity_reservations WHERE openid=? AND activity_id=?", openid, d.ActivityID).Scan(&exists); err != nil {
		return nil, err
	}
	if exists > 0 {
		return M{"enabled": true, "status": "RESERVED"}, tx.Commit()
	}
	if err = tx.QueryRowContext(ctx, "SELECT COUNT(*) FROM activity_waitlist WHERE openid=? AND activity_id=?", openid, d.ActivityID).Scan(&exists); err != nil {
		return nil, err
	}
	if exists > 0 {
		return M{"enabled": true, "status": "WAITLISTED"}, tx.Commit()
	}
	var count int
	if err = tx.QueryRowContext(ctx, "SELECT COUNT(*) FROM activity_reservations WHERE activity_id=?", d.ActivityID).Scan(&count); err != nil {
		return nil, err
	}
	status := "RESERVED"
	if a.Capacity > 0 && count >= a.Capacity {
		if !a.WaitlistEnabled {
			return nil, errors.New("活动名额已满，暂未开放候补")
		}
		status = "WAITLISTED"
		_, err = tx.ExecContext(ctx, "INSERT INTO activity_waitlist(openid,activity_id) VALUES(?,?)", openid, d.ActivityID)
	} else {
		_, err = tx.ExecContext(ctx, "INSERT IGNORE INTO activity_reservations(openid,activity_id) VALUES(?,?)", openid, d.ActivityID)
	}
	if err != nil {
		return nil, err
	}
	if err = addActivityNotice(ctx, tx, openid, a, status); err != nil {
		return nil, err
	}
	return M{"enabled": true, "status": status}, tx.Commit()
}

type ActivityReservationState struct {
	ActivityID string `json:"activityId"`
	Status     string `json:"status"`
	Confirmed  int    `json:"confirmed"`
	Waiting    int    `json:"waiting"`
	Position   int    `json:"position"`
}

func (s *Store) activityReservationStatus(ctx context.Context, openid string) (any, error) {
	tx, err := s.db.BeginTx(ctx, &sql.TxOptions{ReadOnly: true, Isolation: sql.LevelRepeatableRead})
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	states := map[string]*ActivityReservationState{}
	get := func(id string) *ActivityReservationState {
		if states[id] == nil {
			states[id] = &ActivityReservationState{ActivityID: id, Status: "NONE"}
		}
		return states[id]
	}
	var payload []byte
	err = tx.QueryRowContext(ctx, "SELECT payload FROM configs WHERE config_key='home'").Scan(&payload)
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return nil, err
	}
	var h HomeConfig
	if len(payload) > 0 {
		if err = json.Unmarshal(payload, &h); err != nil {
			return nil, err
		}
	}
	for _, a := range h.Activities {
		if a.ID != "" {
			get(a.ID)
		}
	}
	rows, err := tx.QueryContext(ctx, "SELECT activity_id,COUNT(*),MAX(openid=?) FROM activity_reservations GROUP BY activity_id", openid)
	if err != nil {
		return nil, err
	}
	for rows.Next() {
		var id string
		var count, own int
		if err = rows.Scan(&id, &count, &own); err != nil {
			rows.Close()
			return nil, err
		}
		v := get(id)
		v.Confirmed = count
		if own > 0 {
			v.Status = "RESERVED"
		}
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return nil, err
	}
	rows, err = tx.QueryContext(ctx, "SELECT w.activity_id,COUNT(*),MAX(w.openid=?),COALESCE(MAX(CASE WHEN w.openid=? THEN (SELECT COUNT(*) FROM activity_waitlist q WHERE q.activity_id=w.activity_id AND q.id<=w.id) END),0) FROM activity_waitlist w GROUP BY w.activity_id", openid, openid)
	if err != nil {
		return nil, err
	}
	for rows.Next() {
		var id string
		var count, own, position int
		if err = rows.Scan(&id, &count, &own, &position); err != nil {
			rows.Close()
			return nil, err
		}
		v := get(id)
		v.Waiting = count
		if own > 0 {
			v.Status = "WAITLISTED"
			v.Position = position
		}
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return nil, err
	}
	items := []ActivityReservationState{}
	for _, v := range states {
		if v.Status != "NONE" {
			items = append(items, *v)
			continue
		}
		for _, a := range h.Activities {
			if a.ID == v.ActivityID {
				items = append(items, *v)
				break
			}
		}
	}
	return M{"items": items}, tx.Commit()
}

// Saves and rollbacks refill vacancies atomically, without evicting confirmed users.
func reconcileHomeActivities(ctx context.Context, tx *sql.Tx, key string, old []byte, value any) error {
	if key != "home" {
		return nil
	}
	var before HomeConfig
	if err := json.Unmarshal(old, &before); err != nil {
		return err
	}
	after, ok := value.(HomeConfig)
	if !ok {
		return errors.New("首页配置类型无效")
	}
	tracked := map[string]bool{}
	for _, a := range before.Activities {
		if a.Capacity > 0 || a.WaitlistEnabled {
			tracked[a.ID] = true
		}
	}
	for _, a := range after.Activities {
		if tracked[a.ID] || a.Capacity > 0 || a.WaitlistEnabled {
			if err := promoteActivity(ctx, tx, a); err != nil {
				return err
			}
		}
	}
	return nil
}

// 超售检测：逐个活动比对预约数与容量，容量调整后超出的也能发现。
type activityOversellRow struct {
	ActivityID string `json:"activityId"`
	Title      string `json:"title"`
	Capacity   int    `json:"capacity"`
	Reserved   int    `json:"reserved"`
	Waitlisted int    `json:"waitlisted"`
	OverBy     int    `json:"overBy"`
}

func (s *Server) activityOversellCheck(ctx context.Context) (any, error) {
	var payload []byte
	if err := s.store.db.QueryRowContext(ctx, "SELECT payload FROM configs WHERE config_key='home'").Scan(&payload); err != nil && !errors.Is(err, sql.ErrNoRows) {
		return nil, err
	}
	var h HomeConfig
	if len(payload) > 0 {
		if err := json.Unmarshal(payload, &h); err != nil {
			return nil, err
		}
	}
	rows := []activityOversellRow{}
	for _, a := range h.Activities {
		if a.Capacity <= 0 {
			continue
		}
		var reserved, waitlisted int
		if err := s.store.db.QueryRowContext(ctx, "SELECT COUNT(*) FROM activity_reservations WHERE activity_id=?", a.ID).Scan(&reserved); err != nil {
			return nil, err
		}
		if err := s.store.db.QueryRowContext(ctx, "SELECT COUNT(*) FROM activity_waitlist WHERE activity_id=?", a.ID).Scan(&waitlisted); err != nil {
			return nil, err
		}
		row := activityOversellRow{ActivityID: a.ID, Title: a.Title, Capacity: a.Capacity, Reserved: reserved, Waitlisted: waitlisted}
		if reserved > a.Capacity {
			row.OverBy = reserved - a.Capacity
		}
		rows = append(rows, row)
	}
	return M{"items": rows}, nil
}
