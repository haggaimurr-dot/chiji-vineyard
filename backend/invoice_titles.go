package main

import (
	"context"
	"database/sql"
	"encoding/json"
)

// 开票抬头记忆：按客户（openid）存档用过的开票抬头，再次开票自动带出上次抬头，可切换历史。
type invoiceTitle struct {
	Kind       string `json:"kind"`
	Title      string `json:"title"`
	TaxID      string `json:"taxId"`
	Email      string `json:"email"`
	LastUsedAt string `json:"lastUsedAt"`
}

func recordInvoiceTitle(ctx context.Context, tx *sql.Tx, openid string, i Invoice) error {
	title := clean(i.Title, 100)
	if title == "" {
		return nil
	}
	kind := i.Kind
	if kind != "company" {
		kind = "personal"
	}
	_, err := tx.ExecContext(ctx, `INSERT INTO invoice_titles(openid,kind,title,tax_id,email,last_used_at)
VALUES(?,?,?,?,?,NOW(3)) ON DUPLICATE KEY UPDATE tax_id=VALUES(tax_id),email=VALUES(email),last_used_at=NOW(3)`,
		openid, kind, title, clean(i.TaxID, 20), clean(i.Email, 200))
	return err
}

func (s *Store) invoiceTitles(ctx context.Context, openid string) (any, error) {
	rows, err := s.db.QueryContext(ctx, "SELECT kind,title,tax_id,email,last_used_at FROM invoice_titles WHERE openid=? ORDER BY last_used_at DESC LIMIT 20", openid)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := []invoiceTitle{}
	for rows.Next() {
		var t invoiceTitle
		if err = rows.Scan(&t.Kind, &t.Title, &t.TaxID, &t.Email, &t.LastUsedAt); err != nil {
			return nil, err
		}
		items = append(items, t)
	}
	if err = rows.Err(); err != nil {
		return nil, err
	}
	var last any
	if len(items) > 0 {
		last = items[0]
	}
	return M{"last": last, "items": items}, nil
}

func (s *Server) miniInvoiceTitles(ctx context.Context, openid string, _ json.RawMessage) (any, error) {
	return s.store.invoiceTitles(ctx, openid)
}
