package main

import (
	"context"
	"encoding/json"
	"errors"
	"net/url"
	"strings"
	"time"
)

type ProductBatch struct {
	ExternalTrace *ExternalTrace `json:"externalTrace,omitempty"`
	Code          string         `json:"code"`
	ID            string         `json:"id"`
	ProductID     string         `json:"productId"`
	Number        string         `json:"number"`
	Orchard       string         `json:"orchard"`
	HarvestedAt   string         `json:"harvestedAt"`
	PackedAt      string         `json:"packedAt"`
	ExpiresAt     string         `json:"expiresAt"`
	Note          string         `json:"note"`
	Status        string         `json:"status"`
	Version       int            `json:"version"`
}

type ExternalTrace struct {
	Platform string `json:"platform"`
	URL      string `json:"url"`
	Code     string `json:"code"`
}

func validateExternalTrace(t *ExternalTrace) error {
	if t == nil {
		return nil
	}
	u, err := url.Parse(t.URL)
	if !homeText(t.Platform, 80) || len([]rune(t.Code)) > 120 || len(t.URL) > 1000 || strings.ContainsAny(t.URL, "\r\n\t ") || err != nil || u.Scheme != "https" || u.Hostname() == "" || u.User != nil {
		return errors.New("外部溯源须填写平台名称和有效 HTTPS 链接，平台编码最多 120 字")
	}
	return nil
}

func validateManagedBatch(b ProductBatch) error {
	if e := validateExternalTrace(b.ExternalTrace); e != nil {
		return e
	}
	if !homeText(b.ProductID, 80) || !homeText(b.Number, 120) || !homeText(b.Orchard, 200) || len([]rune(b.Note)) > 1000 || (b.Status != "ACTIVE" && b.Status != "ARCHIVED") {
		return errors.New("请填写商品、批次号、果园及有效状态")
	}
	if e := validateProductBatch(&Traceability{HarvestedAt: b.HarvestedAt, PackedAt: b.PackedAt}); e != nil {
		return e
	}
	if b.ExpiresAt != "" {
		if _, e := time.Parse("2006-01-02", b.ExpiresAt); e != nil {
			return errors.New("有效期日期无效")
		}
		if b.ExpiresAt < b.HarvestedAt || b.ExpiresAt < b.PackedAt {
			return errors.New("有效期不能早于采收或包装日期")
		}
	}
	return nil
}
func (s *Server) listProductBatches(ctx context.Context, raw json.RawMessage) (any, error) {
	var d struct {
		ProductID string `json:"productId"`
		Keyword   string `json:"keyword"`
		Status    string `json:"status"`
		Offset    int    `json:"offset"`
		Limit     int    `json:"limit"`
	}
	if e := decode(raw, &d); e != nil {
		return nil, e
	}
	if d.Offset < 0 {
		return nil, errors.New("分页无效")
	}
	if d.Limit < 1 || d.Limit > 50 {
		d.Limit = 20
	}
	where := "1=1"
	args := []any{}
	if d.ProductID != "" {
		where += " AND product_id=?"
		args = append(args, d.ProductID)
	}
	if d.Status != "" {
		where += " AND status=?"
		args = append(args, d.Status)
	}
	if strings.TrimSpace(d.Keyword) != "" {
		where += " AND LOCATE(?,LOWER(CONCAT(batch_number,' ',payload)))>0"
		args = append(args, strings.ToLower(strings.TrimSpace(d.Keyword)))
	}
	var total int
	if e := s.store.db.QueryRowContext(ctx, "SELECT COUNT(*) FROM product_batches WHERE "+where, args...).Scan(&total); e != nil {
		return nil, e
	}
	rows, e := s.store.db.QueryContext(ctx, "SELECT payload,version FROM product_batches WHERE "+where+" ORDER BY created_at DESC,id DESC LIMIT ? OFFSET ?", append(args, d.Limit, d.Offset)...)
	if e != nil {
		return nil, e
	}
	defer rows.Close()
	items := []ProductBatch{}
	for rows.Next() {
		var b ProductBatch
		var data []byte
		var version int
		if e = rows.Scan(&data, &version); e == nil {
			e = json.Unmarshal(data, &b)
		}
		if e != nil {
			return nil, e
		}
		b.Code = b.ID
		b.Version = version
		items = append(items, b)
	}
	return M{"items": items, "total": total, "offset": d.Offset, "limit": d.Limit}, rows.Err()
}
func (s *Server) saveProductBatch(ctx context.Context, actor Claims, raw json.RawMessage) (any, error) {
	var d struct {
		Batch ProductBatch `json:"batch"`
	}
	if e := decode(raw, &d); e != nil {
		return nil, e
	}
	b := d.Batch
	b.Number = strings.TrimSpace(b.Number)
	b.Orchard = strings.TrimSpace(b.Orchard)
	if b.ExternalTrace != nil {
		b.ExternalTrace.Platform = strings.TrimSpace(b.ExternalTrace.Platform)
		b.ExternalTrace.URL = strings.TrimSpace(b.ExternalTrace.URL)
		b.ExternalTrace.Code = strings.TrimSpace(b.ExternalTrace.Code)
	}
	if e := validateManagedBatch(b); e != nil {
		return nil, e
	}
	tx, e := s.store.db.BeginTx(ctx, nil)
	if e != nil {
		return nil, e
	}
	defer tx.Rollback()
	var productID string
	if e = tx.QueryRowContext(ctx, "SELECT id FROM products WHERE id=? FOR UPDATE", b.ProductID).Scan(&productID); e != nil {
		return nil, errors.New("商品不存在")
	}
	if b.ID == "" {
		b.ID = randomID("BAT")
		b.Code = b.ID
		b.Version = 1
		if _, e = tx.ExecContext(ctx, "INSERT INTO product_batches(id,product_id,batch_number,status,version,payload) VALUES(?,?,?,?,?,?)", b.ID, b.ProductID, b.Number, b.Status, b.Version, jsonValue(b)); e != nil {
			return nil, errors.New("批次号重复或批次保存失败")
		}
	} else {
		var owner string
		var version int
		if e = tx.QueryRowContext(ctx, "SELECT product_id,version FROM product_batches WHERE id=? FOR UPDATE", b.ID).Scan(&owner, &version); e != nil {
			return nil, e
		}
		if owner != b.ProductID || version != b.Version {
			return nil, errors.New("批次已变化，请刷新；商品归属不可修改")
		}
		b.Code = b.ID
		b.Version++
		if _, e = tx.ExecContext(ctx, "UPDATE product_batches SET batch_number=?,status=?,version=?,payload=? WHERE id=?", b.Number, b.Status, b.Version, jsonValue(b), b.ID); e != nil {
			return nil, errors.New("批次号重复或批次保存失败")
		}
	}
	if _, e = tx.ExecContext(ctx, "INSERT INTO audit_logs(admin_id,username,action,target_id,detail) VALUES(?,?,?,?,?)", actor.Subject, actor.Username, "PRODUCT_BATCH_SAVE", b.ID, jsonValue(b)); e != nil {
		return nil, e
	}
	return b, tx.Commit()
}
