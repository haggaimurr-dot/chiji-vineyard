package main

import (
	"context"
	"database/sql/driver"
	"encoding/json"
	"errors"
	"time"
)

// Hold a connection-scoped MySQL lock across each mini request, including login.
// This keeps an in-flight write from recreating personal data after deletion.
func (s *Store) lockMiniAccount(ctx context.Context, openid string) (func(), error) {
	conn, err := s.db.Conn(ctx)
	if err != nil {
		return nil, err
	}
	name := articleReaderHash(openid)
	var acquired int
	if err = conn.QueryRowContext(ctx, "SELECT GET_LOCK(?,1)", name).Scan(&acquired); err != nil || acquired != 1 {
		if err != nil {
			conn.Raw(func(any) error { return driver.ErrBadConn })
		}
		conn.Close()
		return nil, errors.New("账号操作处理中，请稍后重试")
	}
	return func() {
		cleanup, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		var released int
		if err := conn.QueryRowContext(cleanup, "SELECT RELEASE_LOCK(?)", name).Scan(&released); err != nil || released != 1 {
			conn.Raw(func(any) error { return driver.ErrBadConn })
		}
		conn.Close()
	}, nil
}

var personalDataTables = []string{"addresses", "carts", "product_attention", "restock_notifications", "price_drop_notifications", "product_reminder_history", "article_favorites", "classroom_progress", "classroom_video_progress", "classroom_questions", "activity_notices", "activity_push_subscriptions", "shipping_notifications", "commerce_activity"}

func (s *Store) personalData(ctx context.Context, actor Claims, action string, raw json.RawMessage) (any, error) {
	var d struct {
		Confirm string `json:"confirm"`
		Scope   string `json:"scope"`
	}
	if err := decode(raw, &d); err != nil {
		return nil, err
	}
	if action == "account.data.delete" && (d.Confirm != "删除个人数据" || d.Scope != "addresses" && d.Scope != "usage") {
		return nil, errors.New("请确认删除范围")
	}
	if action == "account.close" && d.Confirm != "注销账号" {
		return nil, errors.New("请确认注销账号")
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	// Serialize deletion attempts and re-check the current session in the transaction.
	var session string
	if err = tx.QueryRowContext(ctx, "SELECT id FROM auth_sessions WHERE id=? AND subject=? AND kind='mini' AND revoked_at IS NULL AND expires_at>? FOR UPDATE", actor.SessionID, actor.Subject, time.Now().Unix()).Scan(&session); err != nil {
		return nil, errors.New("登录会话已失效，请重新登录")
	}
	if action == "account.status" || action == "account.close" {
		var blocked int
		err = tx.QueryRowContext(ctx, "SELECT (SELECT COUNT(*) FROM orders WHERE openid=? AND (order_status NOT IN ('COMPLETED','CANCELLED','CLOSED') OR refund_status IN ('APPLIED','APPROVED','PROCESSING','FAILED') OR payment_status IN ('PAYING','UNKNOWN'))) + (SELECT COUNT(*) FROM after_sales_tickets WHERE openid=? AND status NOT IN ('RESOLVED','CLOSED')) + (SELECT COUNT(*) FROM refunds WHERE openid=? AND status NOT IN ('SUCCEEDED','REJECTED','CLOSED'))", actor.Subject, actor.Subject, actor.Subject).Scan(&blocked)
		if err != nil {
			return nil, err
		}
		if action == "account.status" {
			return M{"canClose": blocked == 0, "blockingCount": blocked}, nil
		}
		if blocked > 0 {
			return nil, errors.New("请先处理未完成订单、退款或售后，再注销账号")
		}
	}
	tables := personalDataTables
	if action == "account.data.delete" {
		if d.Scope == "addresses" {
			tables = []string{"addresses"}
		} else {
			tables = personalDataTables[1:]
		}
	}
	for _, table := range tables {
		if _, err = tx.ExecContext(ctx, "DELETE FROM "+table+" WHERE openid=?", actor.Subject); err != nil {
			return nil, err
		}
	}
	if action == "account.close" || d.Scope == "usage" {
		if _, err = tx.ExecContext(ctx, "DELETE FROM article_reads WHERE reader_hash=?", articleReaderHash(actor.Subject)); err != nil {
			return nil, err
		}
	}
	if action == "account.close" {
		// Transaction records keep their snapshots, but a new account cannot inherit them.
		detached := randomID("CLOSED")
		for _, table := range []string{"orders", "refunds", "after_sales_tickets", "refund_evidence", "user_coupons", "order_idempotency", "product_reviews"} {
			if _, err = tx.ExecContext(ctx, "UPDATE "+table+" SET openid=? WHERE openid=?", detached, actor.Subject); err != nil {
				return nil, err
			}
		}
		for _, table := range []string{"activity_reservations", "activity_waitlist", "users"} {
			if _, err = tx.ExecContext(ctx, "DELETE FROM "+table+" WHERE openid=?", actor.Subject); err != nil {
				return nil, err
			}
		}
		if _, err = tx.ExecContext(ctx, "INSERT INTO account_closures(subject_hash) VALUES(?) ON DUPLICATE KEY UPDATE closed_at=CURRENT_TIMESTAMP(3)", articleReaderHash(actor.Subject)); err != nil {
			return nil, err
		}
		if _, err = tx.ExecContext(ctx, "DELETE FROM auth_sessions WHERE subject=? AND kind='mini'", actor.Subject); err != nil {
			return nil, err
		}
	}
	if err = tx.Commit(); err != nil {
		return nil, err
	}
	return M{"deleted": true, "closed": action == "account.close"}, nil
}

func (s *Store) closedAccount(ctx context.Context, openid string) (bool, error) {
	var count int
	err := s.db.QueryRowContext(ctx, "SELECT COUNT(*) FROM account_closures WHERE subject_hash=?", articleReaderHash(openid)).Scan(&count)
	return count > 0, err
}
