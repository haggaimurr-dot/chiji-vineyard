package main

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/base64"
	"encoding/json"
	"image"
	"image/png"
	"testing"
	"time"
)

func TestAfterSalesEvidenceDeadlineAndReadReceipts(t *testing.T) {
	f := &ticketFixture{order: Order{ID: "order1", OrderNo: "CJ1", OpenID: "user1", PaymentStatus: "PAID"}}
	db := sql.OpenDB(ticketConnector{f})
	defer db.Close()
	db.SetMaxOpenConns(1)
	s := &Server{store: &Store{db: db}}
	ctx := context.Background()
	actor := Claims{Subject: "admin", Username: "operator"}
	raw := func(v any) json.RawMessage { return json.RawMessage(jsonValue(v)) }
	var picture bytes.Buffer
	png.Encode(&picture, image.NewRGBA(image.Rect(0, 0, 2, 2)))
	evidence := []TicketAttachment{{Name: "品质问题.png", DataURL: "data:image/png;base64," + base64.StdEncoding.EncodeToString(picture.Bytes())}}
	if _, err := s.createTicket(ctx, "user1", nil, raw(M{"orderId": "order1", "category": "QUALITY", "content": "请查看图片", "attachments": evidence})); err != nil {
		t.Fatal(err)
	}
	if len(f.ticket.Messages[0].Attachments) != 1 || f.ticket.UserRead != 1 || f.ticket.AdminRead != 0 {
		t.Fatal("evidence or receipt missing")
	}
	deadline, err := time.Parse(time.RFC3339, f.ticket.DueAt)
	if err != nil || time.Until(deadline) < 47*time.Hour {
		t.Fatal("48 hour deadline missing")
	}
	id := f.ticket.ID
	read := M{"id": id, "version": 1, "readThrough": 1, "status": "CLOSED", "owner": "attacker"}
	if _, err = s.readTicket(ctx, "stranger", nil, raw(read)); err == nil {
		t.Fatal("foreign user read evidence")
	}
	if _, err = s.readTicket(ctx, "", &actor, raw(read)); err != nil {
		t.Fatal(err)
	}
	if f.ticket.Status != "OPEN" || f.ticket.Owner != "" || f.ticket.Version != 1 || f.ticket.AdminRead != 1 {
		t.Fatal("read endpoint changed ticket or version")
	}
	before := cloneTicket(f.ticket)
	for _, value := range []string{"invalid", time.Now().Add(-time.Hour).Format(time.RFC3339), time.Now().Add(31 * 24 * time.Hour).Format(time.RFC3339)} {
		if _, err = s.updateTicket(ctx, "", &actor, raw(M{"id": id, "version": 1, "dueAt": value})); err == nil {
			t.Fatal("invalid deadline accepted")
		}
	}
	if f.ticket.Version != before.Version || f.ticket.DueAt != before.DueAt {
		t.Fatal("failed update committed")
	}
	due := time.Now().Add(72 * time.Hour).UTC().Format(time.RFC3339)
	if _, err = s.updateTicket(ctx, "user1", nil, raw(M{"id": id, "version": 1, "dueAt": due, "content": "spoof"})); err == nil {
		t.Fatal("user changed deadline")
	}
	if _, err = s.updateTicket(ctx, "", &actor, raw(M{"id": id, "version": 1, "dueAt": due, "content": "正在核实", "attachments": evidence})); err != nil {
		t.Fatal(err)
	}
	if f.ticket.DueAt != due || len(f.ticket.Messages) != 2 || f.ticket.UserRead != 1 {
		t.Fatal("reply incorrectly read by user")
	}
	if _, err = s.readTicket(ctx, "user1", nil, raw(M{"id": id, "version": 1, "readThrough": 2})); err == nil {
		t.Fatal("stale acknowledgement accepted")
	}
	if _, err = s.readTicket(ctx, "user1", nil, raw(M{"id": id, "version": 2, "readThrough": 3})); err == nil {
		t.Fatal("future acknowledgement accepted")
	}
	if _, err = s.readTicket(ctx, "user1", nil, raw(M{"id": id, "version": 2, "readThrough": 2})); err != nil {
		t.Fatal(err)
	}
	if f.ticket.UserRead != 2 || f.ticket.Version != 2 {
		t.Fatal("read receipt not persisted")
	}
	for _, items := range [][]TicketAttachment{
		{{Name: "spoof.png", DataURL: "data:image/png;base64," + base64.StdEncoding.EncodeToString([]byte("not an image"))}},
		{{Name: "script.svg", DataURL: "data:image/svg+xml;base64,PHN2Zz4="}},
		{evidence[0], evidence[0], evidence[0], evidence[0]},
		{{Name: "large.png", DataURL: "data:image/png;base64," + base64.StdEncoding.EncodeToString(make([]byte, 512*1024+1))}},
	} {
		if err := validateTicketAttachments(items); err == nil {
			t.Fatal("unsafe evidence accepted")
		}
	}
}

func TestRefundProviderNumberAvailableForAcceptance(t *testing.T) {
	var result map[string]any
	json.Unmarshal([]byte(jsonValue(Refund{ProviderRefundID: "WX123", OpenID: "private-user"})), &result)
	if result["providerRefundId"] != "WX123" {
		t.Fatal("provider number missing")
	}
	if _, ok := result["openid"]; ok {
		t.Fatal("private owner exposed")
	}
}
