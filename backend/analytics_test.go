package main

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"errors"
	"testing"
	"time"
)

type commerceFixture struct {
	orderPageFixture
	calls int
	fail  bool
}
type commerceConnector struct{ fixture *commerceFixture }

func (c commerceConnector) Connect(context.Context) (driver.Conn, error) { return c.fixture, nil }
func (c commerceConnector) Driver() driver.Driver                        { return orderPageDriver{} }
func (f *commerceFixture) ExecContext(_ context.Context, query string, args []driver.NamedValue) (driver.Result, error) {
	f.calls++
	if query != analyticsVisitSQL && query != analyticsCartSQL {
		return nil, errors.New("unexpected event query")
	}
	if len(args) < 3 || args[1].Value != "authenticated-user" {
		return nil, errors.New("identity differs")
	}
	now := args[2].Value.(time.Time)
	if now.Location() != time.UTC || args[0].Value != now.In(dashboardZone).Format("2006-01-02") {
		return nil, errors.New("event uses incorrect clock or day")
	}
	if query == analyticsCartSQL && (len(args) != 4 || args[3].Value != args[2].Value) {
		return nil, errors.New("cart does not ensure same-time visit")
	}
	if f.fail {
		return nil, errors.New("database unavailable")
	}
	return driver.RowsAffected(1), nil
}
func TestCommerceActivityWrites(t *testing.T) {
	f := &commerceFixture{}
	db := sql.OpenDB(commerceConnector{f})
	defer db.Close()
	store := &Store{db: db}
	for _, cart := range []bool{false, true} {
		if err := store.recordCommerceActivity(context.Background(), "authenticated-user", cart); err != nil {
			t.Fatal(err)
		}
	}
	if f.calls != 2 {
		t.Fatal("missing visit or add event")
	}
	f.fail = true
	if err := store.recordCommerceActivity(context.Background(), "authenticated-user", false); err == nil {
		t.Fatal("swallowed failed write")
	}
}
