package main

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"errors"
	"strings"
	"testing"
)

type cartBatchFixture struct {
	cart, savedCart            map[string]string
	favorites, savedFavorites  map[string]bool
	down, failWrite            string
	writes, commits, rollbacks int
	includeInvalidRows         bool
}
type cartBatchConnector struct{ f *cartBatchFixture }
type cartBatchDriver struct{ f *cartBatchFixture }

func (c cartBatchConnector) Connect(context.Context) (driver.Conn, error) { return c.f, nil }
func (c cartBatchConnector) Driver() driver.Driver                        { return cartBatchDriver{c.f} }
func (d cartBatchDriver) Open(string) (driver.Conn, error)                { return d.f, nil }
func (f *cartBatchFixture) Prepare(string) (driver.Stmt, error) {
	return nil, errors.New("unexpected prepare")
}
func (f *cartBatchFixture) Close() error { return nil }
func copyCartMap[V any](m map[string]V) map[string]V {
	n := map[string]V{}
	for k, v := range m {
		n[k] = v
	}
	return n
}
func (f *cartBatchFixture) Begin() (driver.Tx, error) {
	f.savedCart = copyCartMap(f.cart)
	f.savedFavorites = copyCartMap(f.favorites)
	return f, nil
}
func (f *cartBatchFixture) Commit() error { f.commits++; return nil }
func (f *cartBatchFixture) Rollback() error {
	f.rollbacks++
	f.cart, f.favorites = f.savedCart, f.savedFavorites
	return nil
}
func (f *cartBatchFixture) QueryContext(_ context.Context, q string, args []driver.NamedValue) (driver.Rows, error) {
	if strings.Contains(q, "FROM carts") {
		if args[0].Value != "user" {
			return nil, errors.New("missing user scope")
		}
		if strings.Contains(q, "FOR UPDATE") {
			if id := f.cart[args[1].Value.(string)]; id != "" {
				return singleShippingRow(id), nil
			}
			return &shippingRows{cols: []string{"product_id"}}, nil
		}
		if f.includeInvalidRows {
			rows := &shippingRows{cols: []string{"product_id", "sku_id", "quantity", "selected"}}
			for skuID, productID := range f.cart {
				rows.values = append(rows.values, []driver.Value{productID, skuID, int64(1), true})
			}
			return rows, nil
		}
		// Return no visible products so the test focuses on persisted mutations.
		return &shippingRows{cols: []string{"product_id", "sku_id", "quantity", "selected"}}, nil
	}
	if strings.Contains(q, "FROM products") {
		if f.includeInvalidRows && strings.Contains(q, "SELECT payload") {
			if args[0].Value == "gone" {
				return &shippingRows{cols: []string{"payload"}}, nil
			}
			return singleShippingRow(`{"id":"p1","name":"葡萄"}`), nil
		}
		status := "PUBLISHED"
		if args[0].Value == f.down {
			status = "DRAFT"
		}
		return singleShippingRow(status), nil
	}
	if f.includeInvalidRows && strings.Contains(q, "FROM skus") {
		return &shippingRows{cols: []string{"id", "product_id", "name", "price", "compare_at_price", "stock", "reserved", "sold", "limit_per_order", "weight_label", "active", "sort_order"}}, nil
	}
	return nil, errors.New("unexpected query: " + q)
}

func TestCartRetainsInvalidItemsForCleanup(t *testing.T) {
	f := &cartBatchFixture{cart: map[string]string{"missing-product": "gone", "missing-sku": "p1"}, includeInvalidRows: true}
	db := sql.OpenDB(cartBatchConnector{f})
	defer db.Close()
	store := &Store{db: db}
	items, err := store.cart(context.Background(), "user")
	if err != nil || len(items) != 2 {
		t.Fatalf("invalid items must remain visible: items=%v error=%v", items, err)
	}
	for _, item := range items {
		if item.SKU != nil || (item.SKUID == "missing-product" && item.Product != nil) || (item.SKUID == "missing-sku" && item.Product == nil) {
			t.Fatalf("unexpected invalid item hydration: %+v", item)
		}
	}
}
func (f *cartBatchFixture) ExecContext(_ context.Context, q string, args []driver.NamedValue) (driver.Result, error) {
	if strings.HasPrefix(q, "INSERT") {
		if args[0].Value != "user" {
			return nil, errors.New("missing favorite scope")
		}
		f.writes++
		f.favorites[args[1].Value.(string)] = true
	} else if strings.HasPrefix(q, "DELETE") {
		if args[0].Value != "user" {
			return nil, errors.New("missing delete scope")
		}
		id := args[1].Value.(string)
		if id == f.failWrite {
			return nil, errors.New("write failed")
		}
		delete(f.cart, id)
	} else if strings.HasPrefix(q, "UPDATE") {
		if args[1].Value != "user" || args[0].Value != false {
			return nil, errors.New("invalid selection or scope")
		}
	} else {
		return nil, errors.New("unexpected write")
	}
	return driver.RowsAffected(1), nil
}

func TestCartBatchTransactions(t *testing.T) {
	for _, scenario := range []string{"favorite", "delete", "select", "down", "writeFailure"} {
		t.Run(scenario, func(t *testing.T) {
			f := &cartBatchFixture{cart: map[string]string{"s1": "p1", "s2": "p1", "s3": "p2"}, favorites: map[string]bool{}}
			db := sql.OpenDB(cartBatchConnector{f})
			defer db.Close()
			s := Store{db: db}
			operation := scenario
			if scenario == "down" {
				operation = "favorite"
				f.down = "p2"
			}
			if scenario == "writeFailure" {
				operation = "favorite"
				f.failWrite = "s2"
			}
			raw := []byte(`{"operation":"` + operation + `","skuIds":["s3","s2","s1","other-user-sku"],"selected":false}`)
			_, err := s.batchCart(context.Background(), "user", raw)
			if scenario == "down" || scenario == "writeFailure" {
				if err == nil || len(f.cart) != 3 || len(f.favorites) != 0 || f.rollbacks != 1 || f.commits != 0 {
					t.Fatalf("failed batch not rolled back: %+v %v", f, err)
				}
				return
			}
			if err != nil || f.commits != 1 {
				t.Fatalf("batch failed: %+v %v", f, err)
			}
			if scenario == "select" {
				if len(f.cart) != 3 || len(f.favorites) != 0 {
					t.Fatal("select changed cart membership")
				}
			} else if len(f.cart) != 0 {
				t.Fatal("items not removed")
			}
			if scenario == "favorite" && (f.writes != 2 || len(f.favorites) != 2) {
				t.Fatal("favorites not deduplicated")
			}
			if _, err = s.batchCart(context.Background(), "user", raw); err != nil {
				t.Fatal("retry failed", err)
			}
			if scenario == "favorite" && f.writes != 2 {
				t.Fatal("retry repeated favorites")
			}
		})
	}
}

func TestCartBatchValidation(t *testing.T) {
	for _, raw := range []string{
		`{}`, `{"operation":"unknown","skuIds":["s"]}`, `{"operation":"delete","skuIds":[]}`,
		`{"operation":"delete","skuIds":["s","s"]}`, `{"operation":"delete","skuIds":[" "]}`,
		`{"operation":"select","skuIds":["s"]}`, `{"operation":"select","skuIds":["s"],"selected":"false"}`,
		`{"operation":"delete","skuIds":[1]}`,
	} {
		// Invalid input must fail before accessing a database.
		s := Store{}
		if _, err := s.batchCart(context.Background(), "user", []byte(raw)); err == nil {
			t.Fatalf("accepted %s", raw)
		}
	}
	ids := make([]string, 101)
	if (cartBatchRequest{Operation: "delete", SKUIDs: ids}).validate() == nil {
		t.Fatal("over limit accepted")
	}
}
