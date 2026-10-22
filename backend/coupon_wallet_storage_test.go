package main

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"encoding/json"
	"errors"
	"io"
	"strings"
	"testing"
)

type walletFixture struct {
	config         string
	owned          map[string]string
	used, reserved bool
	failWrite      bool
	lastQuery      string
}
type walletConnector struct{ state *walletFixture }
type walletDriver struct{}

func (walletDriver) Open(string) (driver.Conn, error) { return nil, errors.New("use connector") }
func (c walletConnector) Driver() driver.Driver       { return walletDriver{} }
func (c walletConnector) Connect(context.Context) (driver.Conn, error) {
	return &walletConn{state: c.state}, nil
}

type walletConn struct{ state *walletFixture }

func (c *walletConn) Prepare(string) (driver.Stmt, error) {
	return nil, errors.New("unexpected prepare")
}
func (c *walletConn) Close() error              { return nil }
func (c *walletConn) Begin() (driver.Tx, error) { return c, nil }
func (c *walletConn) Commit() error             { return nil }
func (c *walletConn) Rollback() error           { return nil }

type walletRows struct {
	values [][]driver.Value
	cols   []string
}

func (r *walletRows) Columns() []string { return r.cols }
func (r *walletRows) Close() error      { return nil }
func (r *walletRows) Next(dest []driver.Value) error {
	if len(r.values) == 0 {
		return io.EOF
	}
	copy(dest, r.values[0])
	r.values = r.values[1:]
	return nil
}
func (c *walletConn) QueryContext(_ context.Context, q string, args []driver.NamedValue) (driver.Rows, error) {
	c.state.lastQuery = q
	if strings.Contains(q, "FROM user_coupons") {
		rows := &walletRows{cols: []string{"code", "snapshot", "current", "used", "reserved"}}
		prefix := args[0].Value.(string) + "/"
		for key, snapshot := range c.state.owned {
			if strings.HasPrefix(key, prefix) {
				rows.values = append(rows.values, []driver.Value{strings.TrimPrefix(key, prefix), snapshot, c.state.config, c.state.used, c.state.reserved})
			}
		}
		return rows, nil
	}
	if strings.HasPrefix(q, "SELECT payload FROM configs") {
		return &walletRows{cols: []string{"payload"}, values: [][]driver.Value{{c.state.config}}}, nil
	}
	if strings.HasPrefix(q, "SELECT id FROM orders") {
		r := &walletRows{cols: []string{"id"}}
		if c.state.used || c.state.reserved {
			r.values = [][]driver.Value{{"occupied-order"}}
		}
		return r, nil
	}
	if strings.HasPrefix(q, "SELECT COUNT(*) FROM orders") {
		count := int64(0)
		if c.state.used || c.state.reserved {
			count = 1
		}
		return &walletRows{cols: []string{"count"}, values: [][]driver.Value{{count}}}, nil
	}
	return nil, errors.New("unexpected query: " + q)
}
func (c *walletConn) ExecContext(_ context.Context, q string, args []driver.NamedValue) (driver.Result, error) {
	if c.state.failWrite {
		return nil, errors.New("write failed")
	}
	if !strings.HasPrefix(q, "INSERT INTO user_coupons") {
		return nil, errors.New("unexpected write")
	}
	key := args[0].Value.(string) + "/" + args[1].Value.(string)
	if _, ok := c.state.owned[key]; !ok {
		c.state.owned[key] = string(args[2].Value.([]byte))
	}
	return driver.RowsAffected(1), nil
}
func TestCouponWalletStorage(t *testing.T) {
	state := &walletFixture{config: `{"items":[{"code":"SAVE","name":"测试券","minimum":9900,"amount":1000,"startDate":"2020-01-01","endDate":"2099-12-31","active":true}]}`, owned: map[string]string{}}
	db := sql.OpenDB(walletConnector{state: state})
	defer db.Close()
	server := &Server{store: &Store{db: db}}
	ctx := context.Background()
	for i := 0; i < 2; i++ {
		if _, err := server.redeemCoupon(ctx, "alice", json.RawMessage(`{"code":" save "}`)); err != nil {
			t.Fatal(err)
		}
	}
	if len(state.owned) != 1 {
		t.Fatal("duplicate redemption", state.owned)
	}
	other, err := server.couponWallet(ctx, "bob")
	if err != nil || len(other) != 0 {
		t.Fatal("ownership leak", other, err)
	}
	list, err := server.couponWallet(ctx, "alice")
	if err != nil || len(list) != 1 || list[0].Status != "AVAILABLE" {
		t.Fatal(list, err)
	}
	state.reserved = true
	list, err = server.couponWallet(ctx, "alice")
	if err != nil || list[0].Status != "RESERVED" {
		t.Fatal(list, err)
	}
	quote := Quote{Subtotal: 19900, Freight: 1200}
	if err = server.couponQuote(ctx, db, "alice", "SAVE", &quote, true); err == nil {
		t.Fatal("occupied coupon accepted")
	}
	if !strings.HasSuffix(state.lastQuery, "FOR UPDATE") {
		t.Fatal("occupancy did not use a current locking read")
	}
	state.reserved = false
	if err = server.couponQuote(ctx, db, "alice", "SAVE", &quote, true); err != nil || quote.Discount != 1000 {
		t.Fatal(quote, err)
	}
	state.used = true
	list, err = server.couponWallet(ctx, "alice")
	if err != nil || list[0].Status != "USED" {
		t.Fatal(list, err)
	}
	state.used = false
	state.config = `{"items":[]}`
	list, err = server.couponWallet(ctx, "alice")
	if err != nil || list[0].Status != "DISABLED" || list[0].Name != "测试券" {
		t.Fatal("removed rule should retain snapshot", list, err)
	}
	if _, err = server.redeemCoupon(ctx, "alice", json.RawMessage(`{"code":"SAVE"}`)); err == nil {
		t.Fatal("removed coupon redeemed")
	}
	state.config = `{"items":[{"code":"SAVE","active":true,"endDate":"2099-12-31"}]}`
	state.failWrite = true
	if _, err = server.redeemCoupon(ctx, "bob", json.RawMessage(`{"code":"SAVE"}`)); err == nil {
		t.Fatal("write failure swallowed")
	}
	if len(state.owned) != 1 {
		t.Fatal("failed write changed ownership")
	}
}
