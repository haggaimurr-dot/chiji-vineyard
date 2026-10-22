package main

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"errors"
	"fmt"
	"strings"
	"testing"
)

type reviewPageConnector struct{}
type reviewPageDriver struct{}
type reviewPageConn struct{}

func (reviewPageConnector) Connect(context.Context) (driver.Conn, error) {
	return reviewPageConn{}, nil
}
func (reviewPageConnector) Driver() driver.Driver         { return reviewPageDriver{} }
func (reviewPageDriver) Open(string) (driver.Conn, error) { return reviewPageConn{}, nil }
func (reviewPageConn) Prepare(string) (driver.Stmt, error) {
	return nil, errors.New("unexpected prepare")
}
func (reviewPageConn) Close() error              { return nil }
func (reviewPageConn) Begin() (driver.Tx, error) { return nil, errors.New("unexpected transaction") }
func (reviewPageConn) QueryContext(_ context.Context, query string, args []driver.NamedValue) (driver.Rows, error) {
	if strings.HasPrefix(query, "SELECT review_id,payload") {
		return &shippingRows{cols: []string{"review_id", "payload"}}, nil
	}
	if args[0].Value != "p1" {
		return nil, errors.New("product scope missing")
	}
	switch {
	case strings.HasPrefix(query, "SELECT payload FROM products"):
		if !strings.Contains(query, "status='PUBLISHED'") {
			return nil, errors.New("unpublished product exposed")
		}
		return singleShippingRow([]byte(`{"id":"p1"}`)), nil
	case strings.Contains(query, "FROM skus"):
		return &shippingRows{cols: []string{"id"}}, nil
	case strings.HasPrefix(query, "SELECT COUNT(*),COALESCE(AVG(rating),0)"):
		if !strings.Contains(query, approvedReviewSQL) {
			return nil, errors.New("summary includes unapproved reviews")
		}
		if len(args) != 1 || strings.Contains(query, "LIMIT") {
			return nil, errors.New("summary must cover every review")
		}
		return singleShippingRow(int64(45), float64(4)), nil
	case strings.HasPrefix(query, "SELECT id,rating,content,created_at"):
		if !strings.Contains(query, approvedReviewSQL) {
			return nil, errors.New("public page includes unapproved reviews")
		}
		if !strings.HasSuffix(query, "ORDER BY created_at DESC,id DESC LIMIT ? OFFSET ?") || len(args) != 3 {
			return nil, errors.New("invalid paging query")
		}
		size, offset := int(args[1].Value.(int64)), int(args[2].Value.(int64))
		values := [][]driver.Value{}
		for i := offset; i < offset+size && i < 45; i++ {
			values = append(values, []driver.Value{fmt.Sprintf("r%02d", i), int64(4), "评价", "2026-10-05T01:00:00Z"})
		}
		return &shippingRows{cols: []string{"id", "rating", "content", "created_at"}, values: values}, nil
	}
	return nil, errors.New("unexpected query: " + query)
}

func TestReviewPagination(t *testing.T) {
	db := sql.OpenDB(reviewPageConnector{})
	defer db.Close()
	s := Server{store: &Store{db: db}}
	seen := map[string]bool{}
	for page := 1; page <= 4; page++ {
		raw := []byte(fmt.Sprintf(`{"productId":"p1","page":%d}`, page))
		if page == 1 {
			raw = []byte(`{"productId":"p1"}`)
		}
		result, err := s.productReviews(context.Background(), raw)
		if err != nil {
			t.Fatal(err)
		}
		summary := result.(M)
		items := summary["items"].([]Review)
		expected := []int{20, 20, 5, 0}[page-1]
		if len(items) != expected || summary["total"] != 45 || summary["average"] != float64(4) || summary["page"] != page || summary["pageSize"] != 20 || summary["hasMore"] != (page < 3) {
			t.Fatalf("incorrect page %d: %+v", page, summary)
		}
		for _, item := range items {
			if seen[item.ID] {
				t.Fatal("repeated review", item.ID)
			}
			seen[item.ID] = true
		}
	}
	if len(seen) != 45 {
		t.Fatal("missing older reviews")
	}
	result, err := s.productReviews(context.Background(), []byte(`{"productId":"p1","page":2,"pageSize":7}`))
	if err != nil {
		t.Fatal(err)
	}
	if items := result.(M)["items"].([]Review); len(items) != 7 || items[0].ID != "r07" {
		t.Fatal("custom page size ignored")
	}
	for _, raw := range []string{`{}`, `{"productId":"p1","page":-1}`, `{"productId":"p1","page":1.5}`, `{"productId":"p1","page":1000001}`, `{"productId":"p1","pageSize":101}`, `{"productId":"p1","pageSize":-1}`} {
		if _, err := parseReviewPage([]byte(raw)); err == nil {
			t.Fatal("invalid pagination accepted", raw)
		}
	}
}
