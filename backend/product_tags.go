package main

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"strings"
)

// 商品标签体系 / 规格模板（新增）。
// 标签改名只改 product_tags 的一行，商品通过 tagIds 引用，改名全局生效。

type ProductTag struct {
	ID   string `json:"id"`
	Name string `json:"name"`
	Sort int    `json:"sort"`
}

type SpecTemplate struct {
	ID    string `json:"id"`
	Name  string `json:"name"`
	Specs []Spec `json:"specs"`
}

type Spec struct {
	Name   string   `json:"name"`
	Values []string `json:"values"`
}

func tagNameMap(ctx context.Context, db *sql.DB) (map[string]string, error) {
	rows, err := db.QueryContext(ctx, "SELECT id,name FROM product_tags")
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[string]string{}
	for rows.Next() {
		var id, name string
		if err = rows.Scan(&id, &name); err != nil {
			return nil, err
		}
		out[id] = name
	}
	return out, rows.Err()
}

// attachProductTagNames 把商品的 tagIds 解析为展示用的标签名。
func attachProductTagNames(ctx context.Context, db *sql.DB, products []Product) error {
	need := map[string]bool{}
	for _, p := range products {
		for _, id := range p.TagIDs {
			need[id] = true
		}
	}
	if len(need) == 0 {
		return nil
	}
	names, err := tagNameMap(ctx, db)
	if err != nil {
		return err
	}
	for i := range products {
		products[i].Tags = nil
		for _, id := range products[i].TagIDs {
			if name := names[id]; name != "" {
				products[i].Tags = append(products[i].Tags, name)
			}
		}
	}
	return nil
}

func validateTagIDs(ctx context.Context, q queryer, ids []string) ([]string, error) {
	seen := map[string]bool{}
	out := []string{}
	for _, id := range ids {
		id = strings.TrimSpace(id)
		if id == "" || seen[id] {
			continue
		}
		seen[id] = true
		out = append(out, id)
	}
	if len(out) > 10 {
		return nil, errors.New("商品标签最多 10 个")
	}
	if len(out) == 0 {
		return out, nil
	}
	marks := make([]string, len(out))
	args := make([]any, len(out))
	for i, id := range out {
		marks[i] = "?"
		args[i] = id
	}
	var count int
	if err := q.QueryRowContext(ctx, "SELECT COUNT(*) FROM product_tags WHERE id IN ("+strings.Join(marks, ",")+")", args...).Scan(&count); err != nil {
		return nil, err
	}
	if count != len(out) {
		return nil, errors.New("部分商品标签不存在")
	}
	return out, nil
}

type queryer interface {
	QueryRowContext(ctx context.Context, query string, args ...any) *sql.Row
}

func (s *Server) productTags(ctx context.Context, raw json.RawMessage) (any, error) {
	tags := []ProductTag{}
	rows, err := s.store.db.QueryContext(ctx, "SELECT id,name,sort FROM product_tags ORDER BY sort,id")
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var t ProductTag
		if err = rows.Scan(&t.ID, &t.Name, &t.Sort); err != nil {
			return nil, err
		}
		tags = append(tags, t)
	}
	return M{"items": tags}, rows.Err()
}

func (s *Server) saveProductTag(ctx context.Context, actor Claims, raw json.RawMessage) (any, error) {
	var d struct {
		ID   string `json:"id"`
		Name string `json:"name"`
		Sort *int   `json:"sort"`
	}
	if err := decode(raw, &d); err != nil {
		return nil, err
	}
	name := strings.TrimSpace(d.Name)
	if name == "" || len([]rune(name)) > 20 {
		return nil, errors.New("标签名称不能为空且不超过 20 字")
	}
	tx, err := s.store.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	if d.ID == "" {
		d.ID = randomID("PTG")
		if _, err = tx.ExecContext(ctx, "INSERT INTO product_tags(id,name,sort) VALUES(?,?,?)", d.ID, name, intValue(d.Sort)); err != nil {
			if isDupError(err) {
				return nil, errors.New("标签名称已存在")
			}
			return nil, err
		}
	} else {
		res, err := tx.ExecContext(ctx, "UPDATE product_tags SET name=?,sort=? WHERE id=?", name, intValue(d.Sort), d.ID)
		if err != nil {
			if isDupError(err) {
				return nil, errors.New("标签名称已存在")
			}
			return nil, err
		}
		if n, _ := res.RowsAffected(); n == 0 {
			return nil, errors.New("标签不存在")
		}
	}
	if _, err = tx.ExecContext(ctx, "INSERT INTO audit_logs(admin_id,username,action,target_id,detail) VALUES(?,?,?,?,?)", actor.Subject, actor.Username, "PRODUCT_TAG_SAVE", d.ID, jsonValue(M{"name": name})); err != nil {
		return nil, err
	}
	if err = tx.Commit(); err != nil {
		return nil, err
	}
	return M{"id": d.ID, "name": name}, nil
}

func (s *Server) deleteProductTag(ctx context.Context, actor Claims, raw json.RawMessage) (any, error) {
	var d struct {
		ID string `json:"id"`
	}
	if err := decode(raw, &d); err != nil {
		return nil, err
	}
	if d.ID == "" {
		return nil, errors.New("请选择标签")
	}
	tx, err := s.store.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	rows, err := tx.QueryContext(ctx, "SELECT id,payload FROM products WHERE JSON_CONTAINS(JSON_EXTRACT(payload,'$.tagIds'), JSON_QUOTE(?))", d.ID)
	if err != nil {
		return nil, err
	}
	type pending struct {
		id      string
		payload []byte
	}
	pend := []pending{}
	func() {
		defer rows.Close()
		for rows.Next() {
			var p pending
			if err := rows.Scan(&p.id, &p.payload); err != nil {
				return
			}
			pend = append(pend, p)
		}
	}()
	for _, p := range pend {
		var prod Product
		if err := json.Unmarshal(p.payload, &prod); err != nil {
			return nil, err
		}
		kept := []string{}
		for _, tid := range prod.TagIDs {
			if tid != d.ID {
				kept = append(kept, tid)
			}
		}
		prod.TagIDs = kept
		if _, err = tx.ExecContext(ctx, "UPDATE products SET payload=? WHERE id=?", jsonValue(prod), p.id); err != nil {
			return nil, err
		}
	}
	res, err := tx.ExecContext(ctx, "DELETE FROM product_tags WHERE id=?", d.ID)
	if err != nil {
		return nil, err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return nil, errors.New("标签不存在")
	}
	if _, err = tx.ExecContext(ctx, "INSERT INTO audit_logs(admin_id,username,action,target_id,detail) VALUES(?,?,?,?,?)", actor.Subject, actor.Username, "PRODUCT_TAG_DELETE", d.ID, jsonValue(M{})); err != nil {
		return nil, err
	}
	return M{"deleted": true}, tx.Commit()
}

func (s *Server) specTemplates(ctx context.Context) (any, error) {
	rows, err := s.store.db.QueryContext(ctx, "SELECT id,name,payload FROM spec_templates ORDER BY name")
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []SpecTemplate{}
	for rows.Next() {
		var t SpecTemplate
		var payload []byte
		if err = rows.Scan(&t.ID, &t.Name, &payload); err != nil {
			return nil, err
		}
		var body struct {
			Specs []Spec `json:"specs"`
		}
		if err = json.Unmarshal(payload, &body); err != nil {
			return nil, err
		}
		t.Specs = body.Specs
		out = append(out, t)
	}
	return M{"items": out}, rows.Err()
}

func (s *Server) saveSpecTemplate(ctx context.Context, actor Claims, raw json.RawMessage) (any, error) {
	var d struct {
		ID    string `json:"id"`
		Name  string `json:"name"`
		Specs []Spec `json:"specs"`
	}
	if err := decode(raw, &d); err != nil {
		return nil, err
	}
	name := strings.TrimSpace(d.Name)
	if name == "" || len([]rune(name)) > 30 {
		return nil, errors.New("模板名称不能为空且不超过 30 字")
	}
	if len(d.Specs) == 0 || len(d.Specs) > 4 {
		return nil, errors.New("规格维度为 1-4 个")
	}
	seen := map[string]bool{}
	for i := range d.Specs {
		d.Specs[i].Name = strings.TrimSpace(d.Specs[i].Name)
		if d.Specs[i].Name == "" || seen[d.Specs[i].Name] {
			return nil, errors.New("规格名称不能为空且不能重复")
		}
		seen[d.Specs[i].Name] = true
		vals := []string{}
		vseen := map[string]bool{}
		for _, v := range d.Specs[i].Values {
			v = strings.TrimSpace(v)
			if v == "" || vseen[v] {
				continue
			}
			vseen[v] = true
			vals = append(vals, v)
		}
		if len(vals) == 0 || len(vals) > 20 {
			return nil, errors.New("每个规格 1-20 个取值")
		}
		d.Specs[i].Values = vals
	}
	if d.ID == "" {
		d.ID = randomID("SPT")
	}
	_, err := s.store.db.ExecContext(ctx, "INSERT INTO spec_templates(id,name,payload) VALUES(?,?,?) ON DUPLICATE KEY UPDATE name=VALUES(name),payload=VALUES(payload)", d.ID, name, jsonValue(M{"specs": d.Specs}))
	if err != nil {
		return nil, err
	}
	s.store.audit(ctx, actor, "SPEC_TEMPLATE_SAVE", d.ID, M{"name": name})
	return M{"id": d.ID, "name": name}, nil
}

func (s *Server) deleteSpecTemplate(ctx context.Context, actor Claims, raw json.RawMessage) (any, error) {
	var d struct {
		ID string `json:"id"`
	}
	if err := decode(raw, &d); err != nil {
		return nil, err
	}
	if d.ID == "" {
		return nil, errors.New("请选择模板")
	}
	res, err := s.store.db.ExecContext(ctx, "DELETE FROM spec_templates WHERE id=?", d.ID)
	if err != nil {
		return nil, err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return nil, errors.New("模板不存在")
	}
	s.store.audit(ctx, actor, "SPEC_TEMPLATE_DELETE", d.ID, M{})
	return M{"deleted": true}, nil
}

func intValue(p *int) int {
	if p == nil {
		return 0
	}
	return *p
}

func isDupError(err error) bool {
	return err != nil && strings.Contains(err.Error(), "Duplicate entry")
}

