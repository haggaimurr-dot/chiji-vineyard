package main

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"encoding/json"
	"errors"
	"reflect"
	"strings"
	"testing"
)

func TestProductSearchBoundsAndLiteralFilters(t *testing.T) {
	d, err := normalizeProductSearch(ProductSearch{Offset: -1, Limit: 500, Category: " family ", Keyword: " 葡萄 %_ 2KG ", Sort: "priceDesc"})
	if err != nil || d.Offset != 0 || d.Limit != 50 || d.Category != "family" {
		t.Fatalf("bounds: %+v %v", d, err)
	}
	where, args := productSearchWhere(d)
	if !reflect.DeepEqual(args, []any{"family", "葡萄", "葡萄", "%_", "%_", "2kg", "2kg"}) || strings.Count(where, "LOCATE(") != 6 || strings.Contains(where, " LIKE ") || !strings.Contains(where, "k.active=1") {
		t.Fatalf("literal filters: %s %#v", where, args)
	}
	if !strings.Contains(where, "p.status='PUBLISHED'") {
		t.Fatal("drafts exposed")
	}
	for _, d := range []ProductSearch{{Category: "invalid"}, {Sort: "price; DROP TABLE"}, {Keyword: strings.Repeat("葡", 61)}} {
		if _, err := normalizeProductSearch(d); err == nil {
			t.Fatalf("invalid search accepted: %+v", d)
		}
	}
	d, err = normalizeProductSearch(ProductSearch{})
	if err != nil || d.Limit != 20 || d.Sort != "default" || d.Category != "all" {
		t.Fatal(d, err)
	}
	for _, sort := range []string{"default", "priceAsc", "priceDesc", "sales"} {
		order := productSearchOrder(sort)
		if !strings.HasSuffix(order, "p.sort_order ASC,p.id ASC") {
			t.Fatal("unstable pagination", order)
		}
		if strings.HasPrefix(sort, "price") && !strings.HasPrefix(order, "min_price IS NULL ASC") {
			t.Fatal("missing SKU must sort last", order)
		}
	}
}

func TestProductSearchCombinedConditions(t *testing.T) {
	min, max := int64(5025), int64(10000)
	d, err := normalizeProductSearch(ProductSearch{Category: "family", Keyword: "葡萄", SaleMode: "spot", Stock: "available", MinPrice: &min, MaxPrice: &max})
	if err != nil {
		t.Fatal(err)
	}
	where, args := productSearchWhere(d)
	if !reflect.DeepEqual(args, []any{"spot", min, max, "family", "葡萄", "葡萄"}) || !strings.Contains(where, "stock_filter.stock>0 AND stock_filter.limit_per_order>0") || !strings.Contains(where, "active=1)>=?") || !strings.Contains(where, "active=1)<=?") {
		t.Fatal(where, args)
	}
	d.Stock = "soldOut"
	where, _ = productSearchWhere(d)
	if !strings.Contains(where, "NOT EXISTS") {
		t.Fatal(where)
	}
	negative, large := int64(-1), int64(100000001)
	for _, bad := range []ProductSearch{{SaleMode: "invalid"}, {Stock: "invalid"}, {MinPrice: &negative}, {MaxPrice: &large}, {MinPrice: &max, MaxPrice: &min}} {
		if _, err := normalizeProductSearch(bad); err == nil {
			t.Fatal("invalid filters accepted", bad)
		}
	}
}

type productPageFixture struct {
	active, committed bool
	queries           int
	where             string
	args              []driver.NamedValue
	sort              string
	failSKU           bool
	empty             bool
}
type productPageConnector struct{ state *productPageFixture }
type productPageDriver struct{ state *productPageFixture }

func (c productPageConnector) Connect(context.Context) (driver.Conn, error) { return c.state, nil }
func (c productPageConnector) Driver() driver.Driver                        { return productPageDriver{c.state} }
func (d productPageDriver) Open(string) (driver.Conn, error)                { return d.state, nil }
func (d *productPageFixture) Close() error                                  { return nil }
func (d *productPageFixture) Prepare(string) (driver.Stmt, error) {
	return nil, errors.New("unexpected prepare")
}
func (d *productPageFixture) Begin() (driver.Tx, error) {
	return nil, errors.New("snapshot options required")
}
func (d *productPageFixture) BeginTx(_ context.Context, opts driver.TxOptions) (driver.Tx, error) {
	if !opts.ReadOnly || opts.Isolation != driver.IsolationLevel(sql.LevelRepeatableRead) {
		return nil, errors.New("expected read snapshot")
	}
	d.active = true
	return d, nil
}
func (d *productPageFixture) Commit() error   { d.active = false; d.committed = true; return nil }
func (d *productPageFixture) Rollback() error { d.active = false; return nil }
func (d *productPageFixture) QueryContext(_ context.Context, q string, args []driver.NamedValue) (driver.Rows, error) {
	if !d.active {
		return nil, errors.New("query outside snapshot")
	}
	d.queries++
	switch {
	case strings.HasPrefix(q, "SELECT COUNT(*) FROM products p WHERE "):
		d.where = strings.TrimPrefix(q, "SELECT COUNT(*) FROM products p WHERE ")
		d.args = append([]driver.NamedValue{}, args...)
		return singleShippingRow(int64(137)), nil
	case strings.HasPrefix(q, "SELECT p.payload FROM products p"):
		if !strings.Contains(q, " WHERE "+d.where+" ORDER BY "+productSearchOrder(d.sort)+" LIMIT ? OFFSET ?") || !reflect.DeepEqual(args[:len(args)-2], d.args) {
			return nil, errors.New("page/count filters differ")
		}
		if args[len(args)-2].Value != int64(20) || args[len(args)-1].Value != int64(120) {
			return nil, errors.New("missing SQL pagination beyond 100")
		}
		if d.sort != "default" && (!strings.Contains(q, "MIN(price)") || !strings.Contains(q, "SUM(GREATEST(sold,0))") || !strings.Contains(q, "WHERE active=1 GROUP BY product_id")) {
			return nil, errors.New("ranking missing active SKU metrics")
		}
		rows := &shippingRows{cols: []string{"payload"}}
		if !d.empty {
			for _, id := range []string{"p120", "p121"} {
				raw, _ := json.Marshal(Product{ID: id, Name: "葡萄", Description: strings.Repeat("详情", 100), Gallery: []string{"large-image"}, SKUs: []SKU{{ID: "stale", Stock: 999}}, Favorite: true, RestockReminder: true})
				rows.values = append(rows.values, []driver.Value{raw})
			}
		}
		return rows, nil
	case strings.HasPrefix(q, "SELECT id,product_id,name,price"):
		if d.failSKU {
			return nil, errors.New("SKU query failed")
		}
		if !strings.Contains(q, "active=1 AND product_id IN (?,?)") || len(args) != 2 || args[0].Value != "p120" || args[1].Value != "p121" {
			return nil, errors.New("SKU hydration must be batched and page scoped")
		}
		return &shippingRows{cols: []string{"id", "product_id", "name", "price", "compare_at_price", "stock", "reserved", "sold", "limit_per_order", "weight_label", "active", "sort_order"}, values: [][]driver.Value{{"k120", "p120", "家庭装", int64(8990), int64(0), int64(2), int64(0), int64(100), int64(5), "2kg", true, int64(1)}}}, nil
	case strings.HasPrefix(q, "SELECT product_id,kind FROM product_attention"):
		if !strings.Contains(q, "openid=? AND product_id IN (?,?)") || len(args) != 3 || args[0].Value != "owner" {
			return nil, errors.New("attention must be owner/page scoped")
		}
		return &shippingRows{cols: []string{"product_id", "kind"}, values: [][]driver.Value{{"p120", "favorite"}, {"p121", "restock"}}}, nil
	default:
		return nil, errors.New("unexpected query: " + q)
	}
}

func TestProductPageBeyond100AndBatchHydration(t *testing.T) {
	for _, sort := range []string{"default", "priceAsc", "priceDesc", "sales"} {
		t.Run(sort, func(t *testing.T) {
			state := &productPageFixture{sort: sort}
			db := sql.OpenDB(productPageConnector{state})
			db.SetMaxOpenConns(1)
			defer db.Close()
			out, err := (&Store{db: db}).searchProducts(context.Background(), "owner", ProductSearch{Category: "family", Keyword: "葡萄 2kg", Sort: sort, Limit: 20, Offset: 120})
			if err != nil {
				t.Fatal(err)
			}
			if out.Total != 137 || out.Offset != 120 || len(out.Items) != 2 || state.queries != 4 || !state.committed {
				t.Fatalf("page: %+v fixture: %+v", out, state)
			}
			a, b := out.Items[0], out.Items[1]
			if len(a.SKUs) != 1 || a.SKUs[0].Stock != 2 || len(b.SKUs) != 0 || !a.Favorite || a.RestockReminder || b.Favorite || !b.RestockReminder {
				t.Fatal("stale payload overrides live SKUs/attention", out.Items)
			}
			if a.Description != "" || len(a.Gallery) != 0 {
				t.Fatal("detail payload leaked into cards")
			}
		})
	}
}
func TestProductPageEmptyAndHydrationFailure(t *testing.T) {
	for _, empty := range []bool{false, true} {
		state := &productPageFixture{sort: "default", empty: empty, failSKU: !empty}
		db := sql.OpenDB(productPageConnector{state})
		db.SetMaxOpenConns(1)
		_, err := (&Store{db: db}).searchProducts(context.Background(), "owner", ProductSearch{Offset: 120, Limit: 20})
		db.Close()
		if empty {
			if err != nil || !state.committed || state.queries != 2 {
				t.Fatal("empty page must skip hydration", state, err)
			}
		} else if err == nil || state.committed || state.active {
			t.Fatal("hydration failure must rollback", state, err)
		}
	}
}
