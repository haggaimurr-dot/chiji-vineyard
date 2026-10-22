package main

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"strings"
	"time"
)

type InventoryMovement struct {
	ID             string `json:"id"`
	ProductID      string `json:"productId"`
	ProductName    string `json:"productName"`
	SKUID          string `json:"skuId"`
	SKUName        string `json:"skuName"`
	Kind           string `json:"kind"`
	Reference      string `json:"reference"`
	Username       string `json:"username"`
	Reason         string `json:"reason"`
	BeforeStock    int    `json:"beforeStock"`
	AfterStock     int    `json:"afterStock"`
	BeforeReserved int    `json:"beforeReserved"`
	AfterReserved  int    `json:"afterReserved"`
	BeforeSold     int    `json:"beforeSold"`
	AfterSold      int    `json:"afterSold"`
	CreatedAt      string `json:"createdAt"`
}

// Lock, mutate and append the movement in the caller's transaction. A failed
// ledger insert rolls back the business operation, including payment/refund.
func applyInventory(ctx context.Context, tx *sql.Tx, skuID, kind, reference, username, reason, query string, args ...any) (sql.Result, error) {
	var beforeStock, beforeReserved, beforeSold int
	beforeQuery := "SELECT stock,reserved,sold FROM skus WHERE id=?"
	if kind == "ADJUST" {
		beforeQuery += " AND active=1"
	}
	err := tx.QueryRowContext(ctx, beforeQuery+" FOR UPDATE", skuID).Scan(&beforeStock, &beforeReserved, &beforeSold)
	missing := errors.Is(err, sql.ErrNoRows)
	if err != nil && !(missing && kind == "PRODUCT_SAVE") {
		return nil, err
	}
	result, err := tx.ExecContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	var productID, productName, skuName string
	var stock, reserved, sold int
	err = tx.QueryRowContext(ctx, "SELECT s.product_id,JSON_UNQUOTE(JSON_EXTRACT(p.payload,'$.name')),s.name,s.stock,s.reserved,s.sold FROM skus s JOIN products p ON p.id=s.product_id WHERE s.id=?", skuID).Scan(&productID, &productName, &skuName, &stock, &reserved, &sold)
	if err != nil {
		return nil, err
	}
	if !missing && stock == beforeStock && reserved == beforeReserved && sold == beforeSold {
		return result, nil
	}
	if stock+reserved < beforeStock+beforeReserved && kind != "LOSS" {
		if err = reconcileInventoryLots(ctx, tx, skuID, stock+reserved, kind, reference, username, reason); err != nil {
			return nil, err
		}
	}
	_, err = tx.ExecContext(ctx, "INSERT INTO inventory_movements(id,product_id,product_name,sku_id,sku_name,kind,reference,username,reason,before_stock,after_stock,before_reserved,after_reserved,before_sold,after_sold) VALUES(?,?,?,?,?,?,?,?,?,?,?,?,?,?,?)", randomID("INV"), productID, productName, skuID, skuName, kind, reference, username, reason, beforeStock, stock, beforeReserved, reserved, beforeSold, sold)
	return result, err
}

func (s *Server) inventoryMovements(ctx context.Context, raw json.RawMessage) (any, error) {
	var d struct {
		SKUID     string `json:"skuId"`
		Kind      string `json:"kind"`
		Reference string `json:"reference"`
		StartDate string `json:"startDate"`
		EndDate   string `json:"endDate"`
		Limit     int    `json:"limit"`
		Offset    int    `json:"offset"`
	}
	if err := decode(raw, &d); err != nil {
		return nil, err
	}
	where, args, err := auditSearchWhere(AuditSearch{StartDate: d.StartDate, EndDate: d.EndDate})
	if err != nil {
		return nil, err
	}
	for _, f := range []struct{ column, value string }{{"sku_id", d.SKUID}, {"kind", d.Kind}, {"reference", d.Reference}} {
		if strings.TrimSpace(f.value) != "" {
			where += " AND " + f.column + "=?"
			args = append(args, strings.TrimSpace(f.value))
		}
	}
	if d.Limit <= 0 || d.Limit > 100 {
		d.Limit = 20
	}
	if d.Offset < 0 {
		return nil, errors.New("分页参数无效")
	}
	var total int
	if err = s.store.db.QueryRowContext(ctx, "SELECT COUNT(*) FROM inventory_movements WHERE "+where, args...).Scan(&total); err != nil {
		return nil, err
	}
	rows, err := s.store.db.QueryContext(ctx, "SELECT id,product_id,product_name,sku_id,sku_name,kind,reference,username,reason,before_stock,after_stock,before_reserved,after_reserved,before_sold,after_sold,created_at FROM inventory_movements WHERE "+where+" ORDER BY created_at DESC,id DESC LIMIT ? OFFSET ?", append(args, d.Limit, d.Offset)...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := []InventoryMovement{}
	for rows.Next() {
		var m InventoryMovement
		var created time.Time
		if err = rows.Scan(&m.ID, &m.ProductID, &m.ProductName, &m.SKUID, &m.SKUName, &m.Kind, &m.Reference, &m.Username, &m.Reason, &m.BeforeStock, &m.AfterStock, &m.BeforeReserved, &m.AfterReserved, &m.BeforeSold, &m.AfterSold, &created); err != nil {
			return nil, err
		}
		m.CreatedAt = created.UTC().Format(time.RFC3339Nano)
		items = append(items, m)
	}
	return M{"items": items, "total": total, "limit": d.Limit, "offset": d.Offset}, rows.Err()
}

func (s *Server) inventoryAlerts(ctx context.Context) (any, error) {
	rows, err := s.store.db.QueryContext(ctx, "SELECT s.id,s.product_id,JSON_UNQUOTE(JSON_EXTRACT(p.payload,'$.name')),s.name,p.status,s.stock,s.reserved,COALESCE(a.threshold,10),COALESCE(a.enabled,1) FROM skus s JOIN products p ON p.id=s.product_id LEFT JOIN inventory_alert_settings a ON a.sku_id=s.id WHERE s.active=1 ORDER BY s.stock,s.id")
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := []M{}
	for rows.Next() {
		var id, pid, pname, name, status string
		var stock, reserved, threshold int
		var enabled bool
		if err = rows.Scan(&id, &pid, &pname, &name, &status, &stock, &reserved, &threshold, &enabled); err != nil {
			return nil, err
		}
		items = append(items, M{"skuId": id, "productId": pid, "productName": pname, "skuName": name, "status": status, "stock": stock, "reserved": reserved, "threshold": threshold, "enabled": enabled, "alert": enabled && status == "PUBLISHED" && stock <= threshold})
	}
	return M{"items": items}, rows.Err()
}

func (s *Server) saveInventoryAlert(ctx context.Context, actor Claims, raw json.RawMessage) (any, error) {
	var d struct {
		SKUID     string `json:"skuId"`
		Threshold *int   `json:"threshold"`
		Enabled   *bool  `json:"enabled"`
	}
	if err := decode(raw, &d); err != nil {
		return nil, err
	}
	if d.SKUID == "" || d.Threshold == nil || *d.Threshold < 0 || *d.Threshold > 2147483647 || d.Enabled == nil {
		return nil, errors.New("请填写非负整数告警阈值及告警状态")
	}
	tx, err := s.store.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	var id string
	if err = tx.QueryRowContext(ctx, "SELECT id FROM skus WHERE id=? AND active=1 FOR UPDATE", d.SKUID).Scan(&id); err != nil {
		return nil, errors.New("规格不存在或已停用")
	}
	if _, err = tx.ExecContext(ctx, "INSERT INTO inventory_alert_settings(sku_id,threshold,enabled) VALUES(?,?,?) ON DUPLICATE KEY UPDATE threshold=VALUES(threshold),enabled=VALUES(enabled)", d.SKUID, *d.Threshold, *d.Enabled); err != nil {
		return nil, err
	}
	if _, err = tx.ExecContext(ctx, "INSERT INTO audit_logs(admin_id,username,action,target_id,detail) VALUES(?,?,?,?,?)", actor.Subject, actor.Username, "INVENTORY_ALERT_SAVE", d.SKUID, jsonValue(d)); err != nil {
		return nil, err
	}
	return M{"saved": true}, tx.Commit()
}
