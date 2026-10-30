package main

import (
	"context"
	"database/sql"
	"encoding/csv"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strconv"
	"strings"
)

type AdminProductSearch struct {
	ProductSearch
	Status   string `json:"status"`
	SaleMode string `json:"saleMode"`
	TagID    string `json:"tagId"`
}

func (s *Server) searchAdminProducts(ctx context.Context, raw json.RawMessage) (any, error) {
	var d AdminProductSearch
	if err := decode(raw, &d); err != nil {
		return nil, err
	}
	normalized, err := normalizeProductSearch(d.ProductSearch)
	if err != nil {
		return nil, err
	}
	d.ProductSearch = normalized
	if d.Status != "" && d.Status != "DRAFT" && d.Status != "PUBLISHED" {
		return nil, errors.New("商品状态无效")
	}
	if d.SaleMode != "" && d.SaleMode != "spot" && d.SaleMode != "presale" {
		return nil, errors.New("销售模式无效")
	}
	where, args := productSearchWhere(d.ProductSearch)
	where = strings.Replace(where, "p.status='PUBLISHED'", "1=1", 1)
	if d.Status != "" {
		where += " AND p.status=?"
		args = append(args, d.Status)
	}
	if d.SaleMode != "" {
		where += " AND JSON_UNQUOTE(JSON_EXTRACT(p.payload,'$.saleMode'))=?"
		args = append(args, d.SaleMode)
	}
	if d.TagID != "" {
		where += " AND JSON_CONTAINS(JSON_EXTRACT(p.payload,'$.tagIds'), JSON_QUOTE(?))"
		args = append(args, d.TagID)
	}
	tx, err := s.store.db.BeginTx(ctx, &sql.TxOptions{ReadOnly: true, Isolation: sql.LevelRepeatableRead})
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	out := ProductSearchResult{Items: []Product{}, Limit: d.Limit, Offset: d.Offset}
	if err = tx.QueryRowContext(ctx, "SELECT COUNT(*) FROM products p WHERE "+where, args...).Scan(&out.Total); err != nil {
		return nil, err
	}
	rows, err := tx.QueryContext(ctx, "SELECT p.payload FROM products p WHERE "+where+" ORDER BY p.sort_order,p.id LIMIT ? OFFSET ?", append(args, d.Limit, d.Offset)...)
	if err != nil {
		return nil, err
	}
	ids := []any{}
	marks := []string{}
	positions := map[string]int{}
	for rows.Next() {
		var p Product
		var b []byte
		if err = rows.Scan(&b); err == nil {
			err = json.Unmarshal(b, &p)
		}
		if err != nil {
			rows.Close()
			return nil, err
		}
		p.SKUs = []SKU{}
		positions[p.ID] = len(out.Items)
		out.Items = append(out.Items, p)
		ids = append(ids, p.ID)
		marks = append(marks, "?")
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return nil, err
	}
	if len(ids) > 0 {
		rows, err = tx.QueryContext(ctx, "SELECT id,product_id,name,price,compare_at_price,stock,reserved,sold,limit_per_order,weight_label,active,sort_order FROM skus WHERE active=1 AND product_id IN ("+strings.Join(marks, ",")+") ORDER BY product_id,sort_order,id", ids...)
		if err != nil {
			return nil, err
		}
		for rows.Next() {
			var k SKU
			if err = rows.Scan(&k.ID, &k.ProductID, &k.Name, &k.Price, &k.CompareAtPrice, &k.Stock, &k.Reserved, &k.Sold, &k.LimitPerOrder, &k.WeightLabel, &k.Active, &k.SortOrder); err != nil {
				rows.Close()
				return nil, err
			}
			out.Items[positions[k.ProductID]].SKUs = append(out.Items[positions[k.ProductID]].SKUs, k)
		}
		err = rows.Err()
		rows.Close()
		if err != nil {
			return nil, err
		}
	}
	if err = attachProductTagNames(ctx, s.store.db, out.Items); err != nil {
		return nil, err
	}
	return out, tx.Commit()
}

// CSV only creates new drafts. Existing prices and stock cannot be overwritten.
const productCSVHeader = "商品分组,商品名称,副标题,分类,销售模式,预计发货日期,封面,产地,规格名称,售价元,库存,限购,重量说明"

type ProductImportPreview struct {
	Products []Product `json:"products"`
	Errors   []string  `json:"errors"`
	Rows     int       `json:"rows"`
}

func parseProductCSV(content string) ProductImportPreview {
	out := ProductImportPreview{Products: []Product{}, Errors: []string{}}
	fail := func(line int, why string) { out.Errors = append(out.Errors, fmt.Sprintf("第 %d 行：%s", line, why)) }
	if len(content) > 1024*1024 {
		fail(1, "文件不能超过 1MB")
		return out
	}
	reader := csv.NewReader(strings.NewReader(strings.TrimPrefix(content, "\ufeff")))
	reader.FieldsPerRecord = 13
	header, err := reader.Read()
	if err != nil || strings.Join(header, ",") != productCSVHeader {
		fail(1, "请使用下载的 CSV 模板，保留表头")
		return out
	}
	groups := map[string]int{}
	for line := 2; ; line++ {
		row, e := reader.Read()
		if e == io.EOF {
			break
		}
		out.Rows++
		if out.Rows > 500 {
			fail(line, "一次最多导入 500 个规格")
			break
		}
		if e != nil {
			fail(line, "CSV 列数或引号格式无效")
			continue
		}
		for i := range row {
			row[i] = strings.TrimSpace(row[i])
		}
		price, e := strconv.ParseFloat(row[9], 64)
		stock, e2 := strconv.Atoi(row[10])
		limit, e3 := strconv.Atoi(row[11])
		// Parse monetary values as decimal digits, avoiding rounding imported prices.
		cents, e4 := csvPrice(row[9])
		if row[0] == "" || !homeText(row[1], 80) || !homeText(row[6], 500) || !homeText(row[8], 60) || len([]rune(row[0])) > 80 || len([]rune(row[2])) > 200 || len([]rune(row[7])) > 200 || len([]rune(row[12])) > 80 || e != nil || price <= 0 || e2 != nil || stock < 0 || stock > 2147483647 || e3 != nil || limit < 1 || limit > 2147483647 || e4 != nil || (row[3] != "family" && row[3] != "gift") || (row[4] != "spot" && row[4] != "presale") {
			fail(line, "名称、封面、分类、模式、售价、库存或限购无效")
			continue
		}
		if row[4] == "presale" && !homeDateRange(row[5], row[5]) {
			fail(line, "预售需要有效发货日期")
			continue
		}
		index, ok := groups[row[0]]
		if !ok {
			index = len(out.Products)
			groups[row[0]] = index
			out.Products = append(out.Products, Product{Name: row[1], Subtitle: row[2], Category: row[3], SaleMode: row[4], ExpectedShipDate: row[5], Cover: row[6], Origin: row[7], Status: "DRAFT", SortOrder: 100, Gallery: []string{}, Badges: []string{}, Highlights: []string{}, SKUs: []SKU{}})
		}
		p := &out.Products[index]
		if p.Name != row[1] || p.Subtitle != row[2] || p.Category != row[3] || p.SaleMode != row[4] || p.ExpectedShipDate != row[5] || p.Cover != row[6] || p.Origin != row[7] {
			fail(line, "同一商品分组的商品资料必须一致")
			continue
		}
		duplicate := false
		for _, k := range p.SKUs {
			if k.Name == row[8] {
				duplicate = true
			}
		}
		if duplicate {
			fail(line, "同一商品内规格名称重复")
			continue
		}
		p.SKUs = append(p.SKUs, SKU{Name: row[8], Price: cents, Stock: stock, LimitPerOrder: limit, WeightLabel: row[12], Active: true})
	}
	if out.Rows == 0 {
		fail(2, "没有商品数据")
	}
	return out
}
func csvPrice(value string) (int, error) {
	parts := strings.Split(value, ".")
	if len(parts) > 2 || len(parts[0]) == 0 {
		return 0, errors.New("售价无效")
	}
	fraction := "00"
	if len(parts) == 2 {
		if len(parts[1]) < 1 || len(parts[1]) > 2 {
			return 0, errors.New("售价最多两位小数")
		}
		fraction = (parts[1] + "0")[:2]
	}
	for _, c := range parts[0] + fraction {
		if c < '0' || c > '9' {
			return 0, errors.New("售价无效")
		}
	}
	n, e := strconv.ParseInt(parts[0]+fraction, 10, 32)
	if e != nil || n < 1 {
		return 0, errors.New("售价无效")
	}
	return int(n), nil
}
func (s *Server) importProducts(ctx context.Context, actor Claims, raw json.RawMessage) (any, error) {
	var d struct {
		Content string `json:"content"`
		Preview bool   `json:"preview"`
	}
	if e := decode(raw, &d); e != nil {
		return nil, e
	}
	out := parseProductCSV(d.Content)
	if d.Preview {
		return out, nil
	}
	if len(out.Errors) > 0 {
		return nil, errors.New(strings.Join(out.Errors, "；"))
	}
	tx, e := s.store.db.BeginTx(ctx, nil)
	if e != nil {
		return nil, e
	}
	defer tx.Rollback()
	for _, p := range out.Products {
		if _, e = s.saveProductTx(ctx, tx, actor, p); e != nil {
			return nil, e
		}
	}
	if _, e = tx.ExecContext(ctx, "INSERT INTO audit_logs(admin_id,username,action,target_id,detail) VALUES(?,?,?,?,?)", actor.Subject, actor.Username, "PRODUCT_IMPORT", "csv", jsonValue(M{"products": len(out.Products), "skus": out.Rows})); e != nil {
		return nil, e
	}
	if e = tx.Commit(); e != nil {
		return nil, e
	}
	return M{"created": len(out.Products), "skus": out.Rows}, nil
}

// batchProductStatus publishes or unpublishes multiple products at once.
func (s *Server) batchProductStatus(ctx context.Context, actor Claims, raw json.RawMessage) (any, error) {
	var d struct {
		IDs    []string `json:"ids"`
		Status string   `json:"status"`
	}
	if err := decode(raw, &d); err != nil {
		return nil, err
	}
	if d.Status != "DRAFT" && d.Status != "PUBLISHED" {
		return nil, errors.New("状态无效")
	}
	if len(d.IDs) == 0 || len(d.IDs) > 200 {
		return nil, errors.New("请选择 1–200 个商品")
	}
	tx, err := s.store.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	for _, id := range d.IDs {
		res, err := tx.ExecContext(ctx, "UPDATE products SET status=?,payload=JSON_SET(payload,'$.status',?) WHERE id=?", d.Status, d.Status, id)
		if err != nil {
			return nil, err
		}
		n, err := res.RowsAffected()
		if err != nil {
			return nil, err
		}
		if n == 0 {
			return nil, errors.New("商品不存在")
		}
	}
	if _, err = tx.ExecContext(ctx, "INSERT INTO audit_logs(admin_id,username,action,target_id,detail) VALUES(?,?,?,?,?)", actor.Subject, actor.Username, "PRODUCT_BATCH_STATUS", "", jsonValue(M{"ids": d.IDs, "status": d.Status})); err != nil {
		return nil, err
	}
	if err = tx.Commit(); err != nil {
		return nil, err
	}
	return M{"updated": len(d.IDs)}, nil
}

// duplicateProduct copies a product (SKUs, media, content) as a DRAFT with zero stock.
func (s *Server) duplicateProduct(ctx context.Context, actor Claims, raw json.RawMessage) (any, error) {
	var d struct {
		ID string `json:"id"`
	}
	if err := decode(raw, &d); err != nil {
		return nil, err
	}
	p, err := s.store.product(ctx, d.ID, true)
	if err != nil || p == nil {
		return nil, errors.New("商品不存在")
	}
	p.ID = ""
	p.Name = clean(p.Name+"（副本）", 80)
	p.Status = "DRAFT"
	for i := range p.SKUs {
		p.SKUs[i].ID = ""
		p.SKUs[i].ProductID = ""
		p.SKUs[i].Stock = 0
		p.SKUs[i].ExpectedStock = nil
	}
	tx, err := s.store.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	np, err := s.saveProductTx(ctx, tx, actor, *p)
	if err != nil {
		return nil, err
	}
	if _, err = tx.ExecContext(ctx, "INSERT INTO audit_logs(admin_id,username,action,target_id,detail) VALUES(?,?,?,?,?)", actor.Subject, actor.Username, "PRODUCT_DUPLICATE", np.ID, jsonValue(M{"name": np.Name, "sourceId": d.ID})); err != nil {
		return nil, err
	}
	if err = tx.Commit(); err != nil {
		return nil, err
	}
	return s.store.product(ctx, np.ID, true)
}
