package main

import (
	"context"
	"database/sql"
	"time"
)

// One row per authenticated user and Beijing day. Retries cannot inflate UV.
const analyticsVisitSQL = `INSERT INTO commerce_activity(activity_date,openid,visited_at)
 VALUES(?,?,?) ON DUPLICATE KEY UPDATE visited_at=COALESCE(visited_at,VALUES(visited_at))`
const analyticsCartSQL = `INSERT INTO commerce_activity(activity_date,openid,visited_at,cart_added_at)
 VALUES(?,?,?,?) ON DUPLICATE KEY UPDATE visited_at=COALESCE(visited_at,VALUES(visited_at)),cart_added_at=COALESCE(cart_added_at,VALUES(cart_added_at))`

func (s *Store) recordCommerceActivity(ctx context.Context, openid string, cart bool) error {
	now := time.Now().UTC()
	day := now.In(dashboardZone).Format("2006-01-02")
	if cart {
		_, err := s.db.ExecContext(ctx, analyticsCartSQL, day, openid, now, now)
		return err
	}
	_, err := s.db.ExecContext(ctx, analyticsVisitSQL, day, openid, now)
	return err
}

// Every downstream stage is a subset of the visited cohort. Direct purchases
// stay in the separate order funnel; they do not imply an add-to-cart event.
const dashboardBehaviorSQL = `SELECT COUNT(*),COALESCE(SUM(a.cart_at IS NOT NULL),0),
 COALESCE(SUM(o.created>0),0),COALESCE(SUM(o.paid>0),0),COALESCE(SUM(o.received>0),0)
 FROM (SELECT openid,MIN(visited_at) visit_at,MIN(cart_added_at) cart_at
 FROM commerce_activity WHERE visited_at>=? AND visited_at<? GROUP BY openid) a
 LEFT JOIN (SELECT o.openid,COUNT(*) created,
 SUM(o.payment_status IN ('PAID','REFUNDING','REFUNDED')) paid,
 SUM(o.payment_status IN ('PAID','REFUNDING','REFUNDED') AND o.fulfillment_status='RECEIVED') received
 FROM orders o JOIN (SELECT openid,MIN(cart_added_at) cart_at FROM commerce_activity
 WHERE visited_at>=? AND visited_at<? GROUP BY openid) c ON c.openid=o.openid
 WHERE o.created_at>=c.cart_at AND o.created_at<? GROUP BY o.openid) o ON o.openid=a.openid`

func dashboardBehavior(ctx context.Context, tx *sql.Tx, start, end time.Time) (M, error) {
	var visitors, cart, created, paid, received int64
	if err := tx.QueryRowContext(ctx, dashboardBehaviorSQL, start, end, start, end, end).Scan(&visitors, &cart, &created, &paid, &received); err != nil {
		return nil, err
	}
	var since time.Time
	if err := tx.QueryRowContext(ctx, "SELECT started_at FROM commerce_analytics_state WHERE id=1").Scan(&since); err != nil {
		return nil, err
	}
	return M{"visitors": visitors, "cartUsers": cart, "createdUsers": created, "paidUsers": paid, "receivedUsers": received, "startedAt": since.UTC().Format(time.RFC3339), "partial": start.Before(since)}, nil
}
