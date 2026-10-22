package main

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"encoding/base64"
	"encoding/json"
	"errors"
	"strings"
	"testing"
)

type reviewFeatureState struct {
	payload []byte
	pending []byte
}
type reviewFeatureConnector struct{ state *reviewFeatureState }
type reviewFeatureConn struct{ state *reviewFeatureState }
type reviewFeatureTx struct{ state *reviewFeatureState }

func (c reviewFeatureConnector) Connect(context.Context) (driver.Conn, error) {
	return &reviewFeatureConn{c.state}, nil
}
func (reviewFeatureConnector) Driver() driver.Driver { return reviewPageDriver{} }
func (*reviewFeatureConn) Prepare(string) (driver.Stmt, error) {
	return nil, errors.New("unexpected prepare")
}
func (*reviewFeatureConn) Close() error { return nil }
func (c *reviewFeatureConn) Begin() (driver.Tx, error) {
	c.state.pending = nil
	return reviewFeatureTx{c.state}, nil
}
func (t reviewFeatureTx) Commit() error   { t.state.payload = t.state.pending; return nil }
func (t reviewFeatureTx) Rollback() error { t.state.pending = nil; return nil }
func (c *reviewFeatureConn) QueryContext(_ context.Context, query string, args []driver.NamedValue) (driver.Rows, error) {
	if strings.HasPrefix(query, "SELECT id,product_id,openid") {
		if !strings.HasSuffix(query, "FOR UPDATE") {
			return nil, errors.New("mutation must lock review")
		}
		if args[0].Value != "r1" {
			return &shippingRows{cols: []string{"id"}}, nil
		}
		return singleShippingRow("r1", "p1", "owner", int64(5), "鲜甜", "2026-10-05"), nil
	}
	if strings.HasPrefix(query, "SELECT payload FROM review_details") {
		if c.state.payload == nil {
			return &shippingRows{cols: []string{"payload"}}, nil
		}
		return singleShippingRow(c.state.payload), nil
	}
	if strings.HasPrefix(query, "SELECT review_id,payload") {
		return &shippingRows{cols: []string{"review_id", "payload"}, values: [][]driver.Value{{"r1", c.state.payload}}}, nil
	}
	return nil, errors.New("unexpected query: " + query)
}
func (c *reviewFeatureConn) ExecContext(_ context.Context, query string, args []driver.NamedValue) (driver.Result, error) {
	if strings.HasPrefix(query, "INSERT INTO review_details") {
		c.state.pending = append([]byte{}, args[1].Value.([]byte)...)
		return driver.RowsAffected(1), nil
	}
	if strings.HasPrefix(query, "INSERT INTO media") || strings.HasPrefix(query, "INSERT INTO audit_logs") {
		return driver.RowsAffected(1), nil
	}
	return nil, errors.New("unexpected write: " + query)
}
func TestReviewAppendReplyOwnershipAndPreservation(t *testing.T) {
	state := &reviewFeatureState{}
	db := sql.OpenDB(reviewFeatureConnector{state})
	defer db.Close()
	s := &Server{store: &Store{db: db}}
	ctx := context.Background()
	raw := []byte(`{"id":"r1","content":"放了两天仍然新鲜"}`)
	if _, err := s.appendReview(ctx, "other", raw); err == nil {
		t.Fatal("another buyer can append")
	}
	result, err := s.appendReview(ctx, "owner", raw)
	if err != nil {
		t.Fatal(err)
	}
	review := result.(Review)
	if review.AppendContent == "" || review.Rating != 5 || review.Content != "鲜甜" {
		t.Fatal(review)
	}
	if _, err = s.appendReview(ctx, "owner", raw); err == nil {
		t.Fatal("duplicate append accepted")
	}
	result, err = s.replyReview(ctx, Claims{Subject: "admin", Username: "运营"}, []byte(`{"id":"r1","content":"谢谢支持"}`))
	if err != nil {
		t.Fatal(err)
	}
	review = result.(Review)
	if review.Reply != "谢谢支持" || review.AppendContent == "" {
		t.Fatal("reply erased append", review)
	}
	if _, err = s.replyReview(ctx, Claims{Subject: "admin"}, raw); err == nil {
		t.Fatal("duplicate reply accepted")
	}
	items := []Review{{ID: "r1"}}
	if err = s.enrichReviews(ctx, items); err != nil {
		t.Fatal(err)
	}
	if items[0].Reply != "谢谢支持" || items[0].AppendContent == "" {
		t.Fatal("public list lost details")
	}
	// Reverse the mutation order to verify an append also preserves a reply.
	original := Review{ID: "r1", ProductID: "p1", Rating: 5, Content: "鲜甜", Reply: "谢谢", ReplyAt: "2026-10-05"}
	state.payload, _ = json.Marshal(original)
	result, err = s.appendReview(ctx, "owner", raw)
	if err != nil {
		t.Fatal(err)
	}
	if result.(Review).Reply != "谢谢" {
		t.Fatal("append erased reply")
	}
}
func TestReviewImageAndTextValidation(t *testing.T) {
	if err := validateReviewText(strings.Repeat("果", 500)); err != nil {
		t.Fatal(err)
	}
	for _, text := range []string{"  ", strings.Repeat("果", 501)} {
		if validateReviewText(text) == nil {
			t.Fatal("invalid text accepted")
		}
	}
	png := "data:image/png;base64,iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAQAAAC1HAwCAAAAC0lEQVR42mP8/x8AAwMCAO+jWZkAAAAASUVORK5CYII="
	if err := validateReviewImages([]string{png}); err != nil {
		t.Fatal(err)
	}
	for _, images := range [][]string{make([]string, 7), {"https://example.com/image.jpg"}, {"data:image/png;base64,aGVsbG8="}, {"data:image/jpeg;base64," + strings.Split(png, ",")[1]}, {"data:image/png;base64," + base64.StdEncoding.EncodeToString(make([]byte, 513<<10))}} {
		if validateReviewImages(images) == nil {
			t.Fatal("invalid images accepted")
		}
	}
	t.Setenv("PUBLIC_BASE_URL", "https://example.com")
	state := &reviewFeatureState{}
	db := sql.OpenDB(reviewFeatureConnector{state})
	defer db.Close()
	s := &Server{store: &Store{db: db}}
	raw, _ := json.Marshal(M{"id": "r1", "content": "追加晒图", "images": []string{png}})
	result, err := s.appendReview(context.Background(), "owner", raw)
	if err != nil {
		t.Fatal(err)
	}
	images := result.(Review).AppendImages
	if len(images) != 1 || !strings.HasPrefix(images[0], "https://example.com/media/REVIMG") {
		t.Fatal(images)
	}
}

func TestReviewModerationPreservesDetails(t *testing.T) {
	original := Review{ID: "r1", ProductID: "p1", Rating: 5, Content: "鲜甜", Status: "PENDING", Images: []string{"https://example.com/photo"}, AppendContent: "仍然新鲜", AppendAt: "2026-10-05", Reply: "感谢支持", ReplyAt: "2026-10-05"}
	state := &reviewFeatureState{}
	state.payload, _ = json.Marshal(original)
	db := sql.OpenDB(reviewFeatureConnector{state})
	defer db.Close()
	s := &Server{store: &Store{db: db}}
	ctx := context.Background()
	actor := Claims{Subject: "admin", Username: "运营"}
	for _, raw := range []string{`{"id":"r1"}`, `{"id":"r1","status":"PENDING"}`, `{"id":"r1","status":"REJECTED","reason":" "}`} {
		if _, err := s.moderateReview(ctx, actor, []byte(raw)); err == nil {
			t.Fatalf("accepted invalid moderation: %s", raw)
		}
	}
	if _, err := s.changeReview(ctx, "owner", nil, []byte(`{"id":"r1","status":"APPROVED"}`)); err == nil {
		t.Fatal("buyer can approve")
	}
	result, err := s.moderateReview(ctx, actor, []byte(`{"id":"r1","status":"REJECTED","reason":"包含个人信息"}`))
	if err != nil {
		t.Fatal(err)
	}
	r := result.(Review)
	if r.Status != "REJECTED" || r.RejectReason != "包含个人信息" || r.ModeratedAt == "" || r.Reply != original.Reply || r.AppendContent != original.AppendContent || len(r.Images) != 1 {
		t.Fatal(r)
	}
	if _, err = s.moderateReview(ctx, actor, []byte(`{"id":"r1","status":"REJECTED","reason":"重复"}`)); err == nil {
		t.Fatal("duplicate moderation accepted")
	}
	result, err = s.moderateReview(ctx, actor, []byte(`{"id":"r1","status":"APPROVED"}`))
	if err != nil {
		t.Fatal(err)
	}
	r = result.(Review)
	if r.Status != "APPROVED" || r.RejectReason != "" || r.Reply != original.Reply {
		t.Fatal(r)
	}
	items := []Review{{ID: "r1"}}
	if err = s.enrichReviews(ctx, items); err != nil {
		t.Fatal(err)
	}
	if items[0].Status != "APPROVED" || items[0].AppendContent != original.AppendContent {
		t.Fatal(items)
	}
	if _, err = s.moderateReview(ctx, actor, []byte(`{"id":"missing","status":"APPROVED"}`)); err == nil {
		t.Fatal("missing review accepted")
	}
	// Appending a new piece of content must re-enter moderation and preserve replies.
	original.Status = "APPROVED"
	original.AppendAt = ""
	original.AppendContent = ""
	state.payload, _ = json.Marshal(original)
	result, err = s.appendReview(ctx, "owner", []byte(`{"id":"r1","content":"追评"}`))
	if err != nil {
		t.Fatal(err)
	}
	r = result.(Review)
	if r.Status != "PENDING" || r.ModeratedAt != "" || r.Reply != original.Reply {
		t.Fatal(r)
	}
}
