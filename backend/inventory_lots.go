package main

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"strings"
	"time"
)

type Warehouse struct {
	ID      string `json:"id"`
	Name    string `json:"name"`
	Active  bool   `json:"active"`
	Version int    `json:"version"`
}
type InventoryLot struct {
	SKUID       string `json:"skuId"`
	WarehouseID string `json:"warehouseId"`
	BatchID     string `json:"batchId"`
	Quantity    int    `json:"quantity"`
	Version     int    `json:"version"`
}
type LotChange struct {
	InventoryLot
	ExpectedStock *int   `json:"expectedStock"`
	Reference     string `json:"reference"`
	Reason        string `json:"reason"`
}

func validateLotChange(d LotChange) error {
	if d.SKUID == "" || d.WarehouseID == "" || d.BatchID == "" || d.Quantity <= 0 || d.Quantity > 2147483647 || d.Version < 0 || d.ExpectedStock == nil || *d.ExpectedStock < 0 || strings.TrimSpace(d.Reference) == "" || len([]rune(d.Reference)) > 128 || strings.TrimSpace(d.Reason) == "" || len([]rune(d.Reason)) > 200 {
		return errors.New("请选择规格、仓库和批次，填写正整数数量、单号和 1–200 字原因")
	}
	return nil
}
func (s *Server) inventoryLocations(ctx context.Context) (any, error) {
	rows, err := s.store.db.QueryContext(ctx, "SELECT id,name,active,version FROM inventory_warehouses ORDER BY name,id")
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := []Warehouse{}
	for rows.Next() {
		var w Warehouse
		if err = rows.Scan(&w.ID, &w.Name, &w.Active, &w.Version); err != nil {
			return nil, err
		}
		items = append(items, w)
	}
	return M{"items": items}, rows.Err()
}
func (s *Server) saveWarehouse(ctx context.Context, actor Claims, raw json.RawMessage) (any, error) {
	var w Warehouse
	if err := decode(raw, &w); err != nil {
		return nil, err
	}
	w.Name = strings.TrimSpace(w.Name)
	if w.Name == "" || len([]rune(w.Name)) > 80 {
		return nil, errors.New("仓库名称须为 1–80 字")
	}
	tx, err := s.store.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	if w.ID == "" {
		w.ID = randomID("WH")
		w.Version = 1
		_, err = tx.ExecContext(ctx, "INSERT INTO inventory_warehouses(id,name,active,version) VALUES(?,?,?,1)", w.ID, w.Name, w.Active)
	} else {
		var result sql.Result
		result, err = tx.ExecContext(ctx, "UPDATE inventory_warehouses SET name=?,active=?,version=version+1 WHERE id=? AND version=?", w.Name, w.Active, w.ID, w.Version)
		if err == nil {
			n, e := result.RowsAffected()
			err = e
			if n != 1 {
				return nil, errors.New("仓库已变化，请刷新重试")
			}
			w.Version++
		}
	}
	if err != nil {
		return nil, err
	}
	_, err = tx.ExecContext(ctx, "INSERT INTO audit_logs(admin_id,username,action,target_id,detail) VALUES(?,?,?,?,?)", actor.Subject, actor.Username, "INVENTORY_WAREHOUSE_SAVE", w.ID, jsonValue(w))
	if err != nil {
		return nil, err
	}
	return w, tx.Commit()
}
func (s *Server) inventoryLots(ctx context.Context, raw json.RawMessage) (any, error) {
	var d struct {
		SKUID string `json:"skuId"`
	}
	if err := decode(raw, &d); err != nil {
		return nil, err
	}
	if d.SKUID == "" {
		return nil, errors.New("请选择规格")
	}
	tx, err := s.store.db.BeginTx(ctx, &sql.TxOptions{ReadOnly: true, Isolation: sql.LevelRepeatableRead})
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	var stock, reserved int
	if err = tx.QueryRowContext(ctx, "SELECT stock,reserved FROM skus WHERE id=?", d.SKUID).Scan(&stock, &reserved); err != nil {
		return nil, err
	}
	rows, err := tx.QueryContext(ctx, "SELECT sku_id,warehouse_id,batch_id,quantity,version FROM inventory_lots WHERE sku_id=? ORDER BY warehouse_id,batch_id", d.SKUID)
	if err != nil {
		return nil, err
	}
	items := []InventoryLot{}
	total := 0
	for rows.Next() {
		var l InventoryLot
		if err = rows.Scan(&l.SKUID, &l.WarehouseID, &l.BatchID, &l.Quantity, &l.Version); err != nil {
			rows.Close()
			return nil, err
		}
		total += l.Quantity
		items = append(items, l)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return nil, err
	}
	if err = tx.Commit(); err != nil {
		return nil, err
	}
	return M{"items": items, "stock": stock, "reserved": reserved, "unallocated": stock + reserved - total}, nil
}
func appendLotMovement(ctx context.Context, tx *sql.Tx, l InventoryLot, before int, kind, reference, username, reason string) error {
	_, err := tx.ExecContext(ctx, "INSERT INTO inventory_lot_movements(id,sku_id,warehouse_id,batch_id,kind,reference,username,reason,before_quantity,after_quantity) VALUES(?,?,?,?,?,?,?,?,?,?)", randomID("LOT"), l.SKUID, l.WarehouseID, l.BatchID, kind, reference, username, reason, before, l.Quantity)
	return err
}
func (s *Server) changeInventoryLot(ctx context.Context, actor Claims, action string, raw json.RawMessage) (any, error) {
	var d LotChange
	if err := decode(raw, &d); err != nil {
		return nil, err
	}
	if err := validateLotChange(d); err != nil {
		return nil, err
	}
	d.Reference = strings.TrimSpace(d.Reference)
	d.Reason = strings.TrimSpace(d.Reason)
	kind := "PURCHASE"
	if action == "admin.inventory.loss" {
		kind = "LOSS"
	} else if action == "admin.inventory.allocate" {
		kind = "ALLOCATE"
	} else if action != "admin.inventory.receive" {
		return nil, errors.New("库存操作无效")
	}
	tx, err := s.store.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	var stock, reserved int
	var pid string
	if err = tx.QueryRowContext(ctx, "SELECT product_id,stock,reserved FROM skus WHERE id=? AND active=1 FOR UPDATE", d.SKUID).Scan(&pid, &stock, &reserved); err != nil {
		return nil, err
	}
	if stock != *d.ExpectedStock {
		return nil, errors.New("库存已变化，请刷新重试")
	}
	var active bool
	if err = tx.QueryRowContext(ctx, "SELECT active FROM inventory_warehouses WHERE id=? FOR UPDATE", d.WarehouseID).Scan(&active); err != nil {
		return nil, err
	}
	if !active {
		return nil, errors.New("仓库已停用")
	}
	var bpid, status string
	if err = tx.QueryRowContext(ctx, "SELECT product_id,status FROM product_batches WHERE id=? FOR UPDATE", d.BatchID).Scan(&bpid, &status); err != nil {
		return nil, err
	}
	if bpid != pid || (status != "ACTIVE" && kind != "LOSS") {
		return nil, errors.New("批次不属于该商品或已归档")
	}
	var quantity, version int
	err = tx.QueryRowContext(ctx, "SELECT quantity,version FROM inventory_lots WHERE sku_id=? AND warehouse_id=? AND batch_id=? FOR UPDATE", d.SKUID, d.WarehouseID, d.BatchID).Scan(&quantity, &version)
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return nil, err
	}
	if version != d.Version {
		return nil, errors.New("批次库存已变化，请刷新重试")
	}
	delta := d.Quantity
	if kind == "LOSS" {
		delta = -delta
		if quantity < d.Quantity || stock < d.Quantity {
			return nil, errors.New("可售库存或该批次库存不足，不能损耗预占库存")
		}
	}
	if int64(quantity)+int64(delta) > 2147483647 || (kind == "PURCHASE" && int64(stock)+int64(delta) > 2147483647) {
		return nil, errors.New("库存超出上限")
	}
	if kind == "ALLOCATE" {
		var total int64
		if err = tx.QueryRowContext(ctx, "SELECT COALESCE(SUM(quantity),0) FROM inventory_lots WHERE sku_id=?", d.SKUID).Scan(&total); err != nil {
			return nil, err
		}
		if total+int64(delta) > int64(stock)+int64(reserved) {
			return nil, errors.New("未分配库存不足")
		}
	}
	_, err = tx.ExecContext(ctx, "INSERT INTO inventory_lots(sku_id,warehouse_id,batch_id,quantity,version) VALUES(?,?,?,?,1) ON DUPLICATE KEY UPDATE quantity=VALUES(quantity),version=version+1", d.SKUID, d.WarehouseID, d.BatchID, quantity+delta)
	if err != nil {
		return nil, err
	}
	if kind != "ALLOCATE" {
		_, err = applyInventory(ctx, tx, d.SKUID, kind, d.Reference, actor.Username, d.Reason, "UPDATE skus SET stock=stock+? WHERE id=?", delta, d.SKUID)
		if err != nil {
			return nil, err
		}
	}
	if _, err = tx.ExecContext(ctx, "INSERT INTO inventory_lot_documents(sku_id,warehouse_id,batch_id,kind,reference) VALUES(?,?,?,?,?)", d.SKUID, d.WarehouseID, d.BatchID, kind, d.Reference); err != nil {
		return nil, errors.New("该单号已登记或登记失败，请刷新并核对流水")
	}
	l := InventoryLot{SKUID: d.SKUID, WarehouseID: d.WarehouseID, BatchID: d.BatchID, Quantity: quantity + delta, Version: version + 1}
	if err = appendLotMovement(ctx, tx, l, quantity, kind, d.Reference, actor.Username, d.Reason); err != nil {
		return nil, err
	}
	_, err = tx.ExecContext(ctx, "INSERT INTO audit_logs(admin_id,username,action,target_id,detail) VALUES(?,?,?,?,?)", actor.Subject, actor.Username, "INVENTORY_"+kind, d.SKUID, jsonValue(d))
	if err != nil {
		return nil, err
	}
	return l, tx.Commit()
}

// Unallocated stock is consumed first. When physical total falls below allocated
// total, consume allocated rows in stable warehouse/batch order under the SKU lock.
// Returns/refunds increase unallocated stock; the original lot is not guessed.
func reconcileInventoryLots(ctx context.Context, tx *sql.Tx, skuID string, physical int, kind, reference, username, reason string) error {
	rows, err := tx.QueryContext(ctx, "SELECT sku_id,warehouse_id,batch_id,quantity,version FROM inventory_lots WHERE sku_id=? ORDER BY warehouse_id,batch_id FOR UPDATE", skuID)
	if err != nil {
		return err
	}
	lots := []InventoryLot{}
	total := 0
	for rows.Next() {
		var l InventoryLot
		if err = rows.Scan(&l.SKUID, &l.WarehouseID, &l.BatchID, &l.Quantity, &l.Version); err != nil {
			rows.Close()
			return err
		}
		lots = append(lots, l)
		total += l.Quantity
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return err
	}
	excess := total - physical
	for _, l := range lots {
		if excess <= 0 {
			break
		}
		delta := l.Quantity
		if delta > excess {
			delta = excess
		}
		if delta == 0 {
			continue
		}
		before := l.Quantity
		l.Quantity -= delta
		excess -= delta
		if _, err = tx.ExecContext(ctx, "UPDATE inventory_lots SET quantity=?,version=version+1 WHERE sku_id=? AND warehouse_id=? AND batch_id=?", l.Quantity, l.SKUID, l.WarehouseID, l.BatchID); err != nil {
			return err
		}
		if err = appendLotMovement(ctx, tx, l, before, kind, reference, username, reason); err != nil {
			return err
		}
	}
	return nil
}
func (s *Server) inventoryLotMovements(ctx context.Context, raw json.RawMessage) (any, error) {
	var d struct {
		SKUID       string `json:"skuId"`
		WarehouseID string `json:"warehouseId"`
		BatchID     string `json:"batchId"`
		Offset      int    `json:"offset"`
	}
	if err := decode(raw, &d); err != nil {
		return nil, err
	}
	if d.Offset < 0 {
		return nil, errors.New("分页参数无效")
	}
	where := "1=1"
	args := []any{}
	for _, f := range []struct{ column, value string }{{"sku_id", d.SKUID}, {"warehouse_id", d.WarehouseID}, {"batch_id", d.BatchID}} {
		if f.value != "" {
			where += " AND " + f.column + "=?"
			args = append(args, f.value)
		}
	}
	var total int
	if err := s.store.db.QueryRowContext(ctx, "SELECT COUNT(*) FROM inventory_lot_movements WHERE "+where, args...).Scan(&total); err != nil {
		return nil, err
	}
	rows, err := s.store.db.QueryContext(ctx, "SELECT id,sku_id,warehouse_id,batch_id,kind,reference,username,reason,before_quantity,after_quantity,created_at FROM inventory_lot_movements WHERE "+where+" ORDER BY created_at DESC,id DESC LIMIT 20 OFFSET ?", append(args, d.Offset)...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := []M{}
	for rows.Next() {
		var id, sku, wh, batch, kind, reference, user, reason string
		var before, after int
		var created time.Time
		if err = rows.Scan(&id, &sku, &wh, &batch, &kind, &reference, &user, &reason, &before, &after, &created); err != nil {
			return nil, err
		}
		items = append(items, M{"id": id, "skuId": sku, "warehouseId": wh, "batchId": batch, "kind": kind, "reference": reference, "username": user, "reason": reason, "beforeQuantity": before, "afterQuantity": after, "createdAt": created.UTC().Format(time.RFC3339Nano)})
	}
	return M{"items": items, "total": total, "offset": d.Offset, "limit": 20}, rows.Err()
}

// resolveScan resolves a scanned batch QR code to its batch/product/SKUs for fast stock operations.
func (s *Server) resolveScan(ctx context.Context, raw json.RawMessage) (any, error) {
	var d struct {
		Code string `json:"code"`
	}
	if err := decode(raw, &d); err != nil {
		return nil, err
	}
	code := strings.TrimSpace(d.Code)
	if code == "" {
		return nil, errors.New("请扫描或输入批次码")
	}
	var batchID, productID, number, status string
	err := s.store.db.QueryRowContext(ctx, "SELECT id,product_id,batch_number,status FROM product_batches WHERE id=? OR JSON_UNQUOTE(JSON_EXTRACT(payload,'$.code'))=? LIMIT 1", code, code).Scan(&batchID, &productID, &number, &status)
	if err != nil {
		return nil, errors.New("未找到该批次码")
	}
	var productName string
	_ = s.store.db.QueryRowContext(ctx, "SELECT JSON_UNQUOTE(JSON_EXTRACT(payload,'$.name')) FROM products WHERE id=?", productID).Scan(&productName)
	rows, err := s.store.db.QueryContext(ctx, "SELECT id,name FROM skus WHERE product_id=? AND active=1 ORDER BY sort_order,id", productID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	skus := []M{}
	for rows.Next() {
		var id, name string
		if err := rows.Scan(&id, &name); err != nil {
			return nil, err
		}
		skus = append(skus, M{"skuId": id, "skuName": name})
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return M{"batchId": batchID, "batchNumber": number, "status": status, "productId": productID, "productName": productName, "skus": skus}, nil
}
