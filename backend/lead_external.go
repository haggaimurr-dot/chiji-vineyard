package main

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"log"
	"net/http"
	"net/url"
	"os"
	"time"
)

func leadDeliveryID(l Lead) string {
	h := sha256.Sum256([]byte(l.ID + "\n" + l.ReminderAt + "\n" + l.OwnerID))
	return hex.EncodeToString(h[:])
}
func externalLeadDue(l Lead, now time.Time) bool {
	at, e := time.Parse(time.RFC3339Nano, l.ReminderAt)
	return e == nil && !at.After(now) && l.ExternalReminder && l.OwnerID != "" && !l.ReminderDone && l.Status != "WON" && l.Status != "LOST"
}
func leadGatewayURL(raw string) error {
	u, e := url.Parse(raw)
	if e != nil || u.Scheme != "https" || u.Host == "" || u.User != nil || u.Fragment != "" {
		return errors.New("CRM_REMINDER_GATEWAY_URL 必须为 HTTPS 地址")
	}
	return nil
}

// Claims are committed before sending. An ambiguous response or a crashed claim
// is never retried automatically; the receiver deduplicates Idempotency-Key.
func (s *Store) deliverLeadReminder(ctx context.Context, candidate Lead, endpoint, token string, client *http.Client) error {
	id := leadDeliveryID(candidate)
	result, e := s.db.ExecContext(ctx, "INSERT IGNORE INTO partner_lead_deliveries(id,lead_id,status) VALUES(?,?,'SENDING')", id, candidate.ID)
	if e != nil {
		return e
	}
	n, e := result.RowsAffected()
	if e != nil || n == 0 {
		return e
	}
	tx, e := s.db.BeginTx(ctx, nil)
	if e != nil {
		return e
	}
	defer tx.Rollback()
	l, e := s.lead(ctx, tx, candidate.ID, true)
	if e != nil {
		return e
	}
	status := "CANCELLED"
	if l != nil && leadDeliveryID(*l) == id && externalLeadDue(*l, time.Now().UTC()) {
		var active bool
		e = tx.QueryRowContext(ctx, "SELECT active FROM admins WHERE id=? LOCK IN SHARE MODE", l.OwnerID).Scan(&active)
		if e != nil {
			return e
		}
		if active {
			// Send only the routing and reminder information, no customer financial data.
			body, _ := json.Marshal(M{"event": "crm.followup.due", "deliveryId": id, "leadId": l.ID, "organization": l.Organization, "ownerId": l.OwnerID, "ownerName": l.OwnerName, "reminderAt": l.ReminderAt})
			req, e := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(body))
			if e != nil {
				return e
			}
			req.Header.Set("Content-Type", "application/json")
			req.Header.Set("Authorization", "Bearer "+token)
			req.Header.Set("Idempotency-Key", id)
			status = "UNCERTAIN"
			resp, e := client.Do(req)
			if e == nil {
				_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, 4096))
				resp.Body.Close()
				if resp.StatusCode >= 200 && resp.StatusCode < 300 {
					status = "SENT"
				} else {
					status = "FAILED"
				}
			}
		}
	}
	if _, e = tx.ExecContext(ctx, "UPDATE partner_lead_deliveries SET status=?,updated_at=UTC_TIMESTAMP(3) WHERE id=?", status, id); e != nil {
		return e
	}
	return tx.Commit()
}
func (s *Store) runLeadExternalReminders(ctx context.Context) {
	endpoint, token := os.Getenv("CRM_REMINDER_GATEWAY_URL"), os.Getenv("CRM_REMINDER_GATEWAY_TOKEN")
	if endpoint == "" || token == "" {
		return
	}
	if e := leadGatewayURL(endpoint); e != nil {
		log.Print(e)
		return
	}
	client := &http.Client{Timeout: 8 * time.Second, CheckRedirect: func(_ *http.Request, _ []*http.Request) error { return http.ErrUseLastResponse }}
	ticker := time.NewTicker(time.Minute)
	defer ticker.Stop()
	for {
		// Limit each poll to due reminders, without a frontend or an open browser.
		rows, e := s.db.QueryContext(ctx, "SELECT id,status,created_at,payload FROM partner_leads"+leadPendingReminderWhere+" AND "+leadReminderTimeSQL+"<=UTC_TIMESTAMP(6) AND JSON_EXTRACT(payload,'$.externalReminder')=true AND NOT EXISTS (SELECT 1 FROM partner_lead_deliveries d WHERE d.lead_id=partner_leads.id AND d.id=SHA2(CONCAT(partner_leads.id,CHAR(10),"+leadReminderValueSQL+",CHAR(10),JSON_UNQUOTE(JSON_EXTRACT(payload,'$.ownerId'))),256)) ORDER BY "+leadReminderTimeSQL+" LIMIT 100")
		if e == nil {
			items := []Lead{}
			for rows.Next() {
				l, err := scanLead(rows)
				if err != nil {
					e = err
					break
				}
				items = append(items, *l)
			}
			if e == nil {
				e = rows.Err()
			}
			rows.Close()
			for _, l := range items {
				if !externalLeadDue(l, time.Now().UTC()) {
					continue
				}
				if err := s.deliverLeadReminder(ctx, l, endpoint, token, client); err != nil {
					log.Printf("CRM 外部提醒处理失败 lead=%s", l.ID)
				}
			}
		}
		if e != nil {
			log.Print("CRM 外部提醒查询失败，请检查数据库迁移")
		}
		s.heartbeat(ctx, "lead_external_reminders", "")
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
	}
}

func (s *Store) leadDeliveries(ctx context.Context, id string) ([]M, error) {
	if _, e := s.getLead(ctx, id); e != nil {
		return nil, e
	}
	rows, e := s.db.QueryContext(ctx, "SELECT id,status,updated_at FROM partner_lead_deliveries WHERE lead_id=? ORDER BY updated_at DESC LIMIT 100", id)
	if e != nil {
		return nil, e
	}
	defer rows.Close()
	out := []M{}
	for rows.Next() {
		var key, status string
		var at time.Time
		if e = rows.Scan(&key, &status, &at); e != nil {
			return nil, e
		}
		out = append(out, M{"id": key, "status": status, "updatedAt": at.UTC().Format(time.RFC3339Nano)})
	}
	return out, rows.Err()
}
