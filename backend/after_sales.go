package main

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/base64"
	"encoding/json"
	"errors"
	"image"
	_ "image/jpeg"
	_ "image/png"
	"strings"
	"time"
)

type TicketAttachment struct {
	Name    string `json:"name"`
	DataURL string `json:"dataUrl"`
}

func validateTicketAttachments(items []TicketAttachment) error {
	if len(items) > 3 {
		return errors.New("每次最多3张附件")
	}
	total := 0
	for _, item := range items {
		if strings.TrimSpace(item.Name) == "" || len([]rune(item.Name)) > 100 {
			return errors.New("附件名称无效")
		}
		parts := strings.SplitN(item.DataURL, ",", 2)
		if len(parts) != 2 || (parts[0] != "data:image/jpeg;base64" && parts[0] != "data:image/png;base64") {
			return errors.New("附件仅支持JPEG或PNG图片")
		}
		data, err := base64.StdEncoding.DecodeString(parts[1])
		total += len(data)
		if err != nil || len(data) == 0 || total > 512*1024 {
			return errors.New("每次附件合计不得超过512KB")
		}
		config, format, err := image.DecodeConfig(bytes.NewReader(data))
		if err != nil || config.Width <= 0 || config.Height <= 0 || config.Width > 10000 || config.Height > 10000 || config.Width*config.Height > 25000000 || (format == "jpeg" && parts[0] != "data:image/jpeg;base64") || (format == "png" && parts[0] != "data:image/png;base64") {
			return errors.New("附件图片内容无效")
		}
		if _, _, err = image.Decode(bytes.NewReader(data)); err != nil {
			return errors.New("附件图片内容不完整")
		}
	}
	return nil
}

type TicketMessage struct {
	Attachments []TicketAttachment `json:"attachments,omitempty"`
	Role        string             `json:"role"`
	Content     string             `json:"content"`
	CreatedAt   string             `json:"createdAt"`
}
type AfterSalesTicket struct {
	Return    *TicketReturn   `json:"return,omitempty"`
	DueAt     string          `json:"dueAt,omitempty"`
	UserRead  int             `json:"userRead"`
	AdminRead int             `json:"adminRead"`
	ID        string          `json:"id"`
	OrderID   string          `json:"orderId"`
	OrderNo   string          `json:"orderNo"`
	RefundID  string          `json:"refundId,omitempty"`
	Category  string          `json:"category"`
	Status    string          `json:"status"`
	Owner     string          `json:"owner,omitempty"`
	Version   int             `json:"version"`
	CreatedAt string          `json:"createdAt"`
	UpdatedAt string          `json:"updatedAt"`
	Messages  []TicketMessage `json:"messages"`
}

func ticketContent(value string) (string, error) {
	value = strings.TrimSpace(value)
	if value == "" || len([]rune(value)) > 1000 {
		return "", errors.New("请填写问题描述或回复，最多1000字")
	}
	return value, nil
}

// Read permission only acknowledges the messages the caller actually displayed.
func (s *Server) readTicket(ctx context.Context, openid string, actor *Claims, raw json.RawMessage) (any, error) {
	var d struct {
		ID          string
		Version     int
		ReadThrough *int
	}
	if err := decode(raw, &d); err != nil {
		return nil, err
	}
	if d.ReadThrough == nil {
		return nil, errors.New("请提供已读位置")
	}
	clean, _ := json.Marshal(d)
	return s.updateTicket(ctx, openid, actor, clean)
}
func ticketTransition(from, to string) bool {
	return from == to || from == "OPEN" && to == "PROCESSING" || (from == "OPEN" || from == "PROCESSING") && to == "RESOLVED" || from == "RESOLVED" && to == "CLOSED"
}
func (s *Server) createTicket(ctx context.Context, openid string, actor *Claims, raw json.RawMessage) (any, error) {
	var d struct {
		OrderID, RefundID, Category, Content string
		Attachments                          []TicketAttachment
	}
	if err := decode(raw, &d); err != nil {
		return nil, err
	}
	if err := validateTicketAttachments(d.Attachments); err != nil {
		return nil, err
	}
	content, err := ticketContent(d.Content)
	if err != nil {
		return nil, err
	}
	if d.Category != "QUALITY" && d.Category != "DELIVERY" && d.Category != "INVOICE" && d.Category != "OTHER" {
		return nil, errors.New("请选择售后问题类型")
	}
	tx, err := s.store.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	o, err := s.store.order(ctx, tx, d.OrderID, true)
	if err != nil {
		return nil, err
	}
	if o == nil || actor == nil && o.OpenID != openid {
		return nil, errors.New("订单不存在")
	}
	if o.PaymentStatus != "PAID" && o.PaymentStatus != "REFUNDING" && o.PaymentStatus != "REFUNDED" {
		return nil, errors.New("请支付订单后提交售后工单")
	}
	if d.RefundID != "" {
		var orderID string
		if err = tx.QueryRowContext(ctx, "SELECT order_id FROM refunds WHERE id=?", d.RefundID).Scan(&orderID); err != nil || orderID != o.ID {
			return nil, errors.New("退款申请与订单不匹配")
		}
	}
	// The order lock serializes submissions, including retries and multiple instances.
	var existing string
	err = tx.QueryRowContext(ctx, "SELECT id FROM after_sales_tickets WHERE order_id=? AND status IN ('OPEN','PROCESSING','RESOLVED') LIMIT 1", o.ID).Scan(&existing)
	if err == nil {
		return nil, errors.New("此订单已有未关闭工单，请在原工单补充说明")
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return nil, err
	}
	role := "USER"
	if actor != nil {
		role = "ADMIN"
	}
	t := AfterSalesTicket{ID: randomID("AS"), OrderID: o.ID, OrderNo: o.OrderNo, RefundID: d.RefundID, Category: d.Category, Status: "OPEN", Version: 1, CreatedAt: nowString(), UpdatedAt: nowString(), Messages: []TicketMessage{{Role: role, Content: content, CreatedAt: nowString(), Attachments: d.Attachments}}}
	t.DueAt = time.Now().UTC().Add(48 * time.Hour).Format(time.RFC3339)
	if actor == nil {
		t.UserRead = 1
	} else {
		t.AdminRead = 1
	}
	if _, err = tx.ExecContext(ctx, "INSERT INTO after_sales_tickets(id,order_id,openid,status,payload) VALUES(?,?,?,?,?)", t.ID, o.ID, o.OpenID, t.Status, jsonValue(t)); err != nil {
		return nil, err
	}
	if actor != nil {
		if err = ticketAudit(ctx, tx, *actor, "TICKET_CREATE", t); err != nil {
			return nil, err
		}
	}
	return t, tx.Commit()
}
func ticketAudit(ctx context.Context, tx *sql.Tx, actor Claims, action string, t AfterSalesTicket) error {
	_, err := tx.ExecContext(ctx, "INSERT INTO audit_logs(admin_id,username,action,target_id,detail) VALUES(?,?,?,?,?)", actor.Subject, actor.Username, action, t.ID, jsonValue(M{"status": t.Status, "owner": t.Owner, "version": t.Version}))
	return err
}
func (s *Server) listTickets(ctx context.Context, openid string, raw json.RawMessage) (any, error) {
	var d struct {
		OrderID, Status string
		Offset          int
	}
	if err := decode(raw, &d); err != nil {
		return nil, err
	}
	if d.Offset < 0 {
		return nil, errors.New("分页参数无效")
	}
	query := "SELECT payload FROM after_sales_tickets WHERE 1=1"
	args := []any{}
	if openid != "" {
		query += " AND openid=?"
		args = append(args, openid)
	}
	if d.OrderID != "" {
		query += " AND order_id=?"
		args = append(args, d.OrderID)
	}
	if d.Status != "" {
		query += " AND status=?"
		args = append(args, d.Status)
	}
	query += " ORDER BY created_at DESC,id DESC LIMIT 50 OFFSET ?"
	args = append(args, d.Offset)
	rows, err := s.store.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := []AfterSalesTicket{}
	for rows.Next() {
		var raw []byte
		var t AfterSalesTicket
		if err = rows.Scan(&raw); err != nil {
			return nil, err
		}
		if err = json.Unmarshal(raw, &t); err != nil {
			return nil, err
		}
		if openid != "" {
			t.Owner = ""
		}
		items = append(items, t)
	}
	return items, rows.Err()
}
func (s *Server) updateTicket(ctx context.Context, openid string, actor *Claims, raw json.RawMessage) (any, error) {
	var d struct {
		ID, Content, Status string
		Return              *ReturnAction
		Owner               *string
		Version             int
		Attachments         []TicketAttachment
		DueAt               *string
		ReadThrough         *int
	}
	if err := decode(raw, &d); err != nil {
		return nil, err
	}
	if err := validateTicketAttachments(d.Attachments); err != nil {
		return nil, err
	}
	content := strings.TrimSpace(d.Content)
	if content != "" {
		var err error
		content, err = ticketContent(content)
		if err != nil {
			return nil, err
		}
	}
	tx, err := s.store.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	var stored []byte
	var user string
	if err = tx.QueryRowContext(ctx, "SELECT openid,payload FROM after_sales_tickets WHERE id=? FOR UPDATE", d.ID).Scan(&user, &stored); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, errors.New("工单不存在")
		}
		return nil, err
	}
	if actor == nil && user != openid {
		return nil, errors.New("工单不存在")
	}
	var t AfterSalesTicket
	if err = json.Unmarshal(stored, &t); err != nil {
		return nil, err
	}
	if t.Version != d.Version {
		return nil, errors.New("工单已更新，请刷新后重试")
	}
	if d.ReadThrough != nil {
		if d.Return != nil || d.Content != "" || d.Owner != nil || d.DueAt != nil || d.Status != "" || len(d.Attachments) > 0 || *d.ReadThrough < 0 || *d.ReadThrough > len(t.Messages) {
			return nil, errors.New("已读参数无效")
		}
		if actor == nil {
			if *d.ReadThrough > t.UserRead {
				t.UserRead = *d.ReadThrough
			}
		} else {
			if *d.ReadThrough > t.AdminRead {
				t.AdminRead = *d.ReadThrough
			}
		}
		if _, err = tx.ExecContext(ctx, "UPDATE after_sales_tickets SET status=?,payload=? WHERE id=?", t.Status, jsonValue(t), t.ID); err != nil {
			return nil, err
		}
		if err = tx.Commit(); err != nil {
			return nil, err
		}
		if actor == nil {
			t.Owner = ""
		}
		return t, nil
	}
	if t.Status == "CLOSED" {
		return nil, errors.New("工单已关闭，如有新问题请新建工单")
	}
	if len(t.Messages) >= 200 {
		return nil, errors.New("工单记录已达上限，请联系商家处理")
	}
	if d.Return != nil {
		if d.Status != "" || d.Owner != nil || d.DueAt != nil || content != "" || len(d.Attachments) > 0 {
			return nil, errors.New("退货操作请单独提交")
		}
		content, err = s.applyTicketReturn(ctx, tx, &t, actor, *d.Return)
		if err != nil {
			return nil, err
		}
	}
	if actor == nil {
		if d.Status != "" || d.Owner != nil || d.DueAt != nil || content == "" {
			return nil, errors.New("仅可补充问题说明")
		}
		if t.Status == "RESOLVED" {
			t.Status = "PROCESSING"
			t.DueAt = time.Now().UTC().Add(48 * time.Hour).Format(time.RFC3339)
		}
	} else {
		if d.Status != "" {
			if (d.Status == "RESOLVED" || d.Status == "CLOSED") && t.Return != nil && t.Return.Status != "RECEIVED" {
				return nil, errors.New("请完成退货验收入库后结单")
			}
			if !ticketTransition(t.Status, d.Status) {
				return nil, errors.New("工单状态不能如此变更")
			}
			if (d.Status == "RESOLVED" || d.Status == "CLOSED") && content == "" {
				return nil, errors.New("请填写处理结果后结单")
			}
			t.Status = d.Status
		}
		if d.DueAt != nil {
			deadline, e := time.Parse(time.RFC3339, *d.DueAt)
			if e != nil || !deadline.After(time.Now()) || deadline.After(time.Now().Add(30*24*time.Hour)) {
				return nil, errors.New("处理期限须在未来30天以内")
			}
			t.DueAt = deadline.UTC().Format(time.RFC3339)
		}
		if d.Owner != nil {
			owner := strings.TrimSpace(*d.Owner)
			if len([]rune(owner)) > 40 {
				return nil, errors.New("负责人最多40字")
			}
			if owner != "" {
				var count int
				if err = tx.QueryRowContext(ctx, "SELECT COUNT(*) FROM admins WHERE username=? AND active=TRUE", owner).Scan(&count); err != nil {
					return nil, err
				}
				if count == 0 {
					return nil, errors.New("负责人须为有效管理员账号")
				}
			}
			t.Owner = owner
		}
		if content == "" && d.Status == "" && d.Owner == nil && d.DueAt == nil {
			return nil, errors.New("请填写回复或选择处理操作")
		}
	}
	if content == "" && len(d.Attachments) > 0 {
		return nil, errors.New("请填写附件说明")
	}
	if content != "" {
		role := "USER"
		if actor != nil {
			role = "ADMIN"
		}
		t.Messages = append(t.Messages, TicketMessage{Role: role, Content: content, CreatedAt: nowString(), Attachments: d.Attachments})
	}
	if actor == nil {
		t.UserRead = len(t.Messages)
	} else {
		t.AdminRead = len(t.Messages)
	}
	// Bound private inline evidence per ticket as well as per message.
	size := 0
	for _, m := range t.Messages {
		for _, a := range m.Attachments {
			size += len(a.DataURL)
		}
	}
	if size > 8*1024*1024 {
		return nil, errors.New("工单附件累计已达6MB，请联系商家处理")
	}
	t.Version++
	t.UpdatedAt = nowString()
	if _, err = tx.ExecContext(ctx, "UPDATE after_sales_tickets SET status=?,payload=? WHERE id=?", t.Status, jsonValue(t), t.ID); err != nil {
		return nil, err
	}
	if actor != nil {
		if err = ticketAudit(ctx, tx, *actor, "TICKET_UPDATE", t); err != nil {
			return nil, err
		}
	}
	if err = tx.Commit(); err != nil {
		return nil, err
	}
	if actor == nil {
		t.Owner = ""
	}
	return t, nil
}
