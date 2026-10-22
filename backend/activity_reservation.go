package main

import (
	"context"
	"errors"
	"regexp"
	"time"
)

var activityIDPattern = regexp.MustCompile(`^[a-zA-Z0-9_-]{1,80}$`)

func validActivityID(id string) bool { return activityIDPattern.MatchString(id) }

func reservableActivity(h HomeConfig, id string, now time.Time) error {
	today := now.In(time.FixedZone("CST", 8*3600)).Format("2006-01-02")
	for _, a := range h.Activities {
		if a.ID == id && a.ReservationEnabled {
			if !homeDateRange(a.StartDate, a.EndDate) || today > a.EndDate {
				return errors.New("活动已结束，不能预约")
			}
			return nil
		}
	}
	return errors.New("活动已下线或未开放预约")
}

func (s *Store) activityReservations(ctx context.Context, openid string) ([]string, error) {
	rows, err := s.db.QueryContext(ctx, "SELECT activity_id FROM activity_reservations WHERE openid=? ORDER BY created_at DESC,activity_id", openid)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	ids := []string{}
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		ids = append(ids, id)
	}
	return ids, rows.Err()
}
