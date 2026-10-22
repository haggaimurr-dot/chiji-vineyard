package main

import (
	"bytes"
	"context"
	"crypto/tls"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"mime"
	"mime/multipart"
	"net"
	"net/mail"
	"net/smtp"
	"net/textproto"
	"os"
	"strings"
	"time"
)

const maxInvoicePDF = 3 << 20

func decodeInvoicePDF(name, data string) ([]byte, error) {
	if !strings.HasSuffix(strings.ToLower(strings.TrimSpace(name)), ".pdf") || len([]rune(name)) > 120 || strings.ContainsAny(name, "\r\n/\\") {
		return nil, errors.New("请上传 PDF 发票（文件名最多120个字符）")
	}
	if len(data) > base64.StdEncoding.EncodedLen(maxInvoicePDF) {
		return nil, errors.New("发票文件不能超过3MB")
	}
	content, err := base64.StdEncoding.DecodeString(data)
	if err != nil || len(content) > maxInvoicePDF || !bytes.HasPrefix(content, []byte("%PDF-")) || !bytes.Contains(content, []byte("%%EOF")) {
		return nil, errors.New("请上传有效的 PDF 发票，大小不超过3MB")
	}
	return content, nil
}

// Base64 with RFC 2045 line lengths, including CRLF for SMTP.
func mailBase64(data []byte) string {
	encoded := base64.StdEncoding.EncodeToString(data)
	var result strings.Builder
	for len(encoded) > 76 {
		result.WriteString(encoded[:76] + "\r\n")
		encoded = encoded[76:]
	}
	result.WriteString(encoded + "\r\n")
	return result.String()
}

func invoiceMessage(from, orderID string, i Invoice, pdf []byte) []byte {
	var body bytes.Buffer
	writer := multipart.NewWriter(&body)
	text, _ := writer.CreatePart(textproto.MIMEHeader{"Content-Type": {"text/plain; charset=UTF-8"}, "Content-Transfer-Encoding": {"base64"}})
	text.Write([]byte(mailBase64([]byte(fmt.Sprintf("您好，您申请的电子普通发票已开具。\n抬头：%s\n发票号码：%s\n金额：¥%.2f\n请查收附件。", i.Title, i.Number, float64(i.Amount)/100)))))
	file, _ := writer.CreatePart(textproto.MIMEHeader{"Content-Type": {"application/pdf"}, "Content-Transfer-Encoding": {"base64"}, "Content-Disposition": {mime.FormatMediaType("attachment", map[string]string{"filename": i.FileName})}})
	file.Write([]byte(mailBase64(pdf)))
	writer.Close()
	headers := fmt.Sprintf("From: %s\r\nTo: %s\r\nSubject: %s\r\nDate: %s\r\nMessage-ID: <%s@%s>\r\nMIME-Version: 1.0\r\nContent-Type: multipart/mixed; boundary=%q\r\n\r\n", from, i.Email, mime.BEncoding.Encode("UTF-8", "迟记葡萄牙电子发票"), time.Now().Format(time.RFC1123Z), i.EmailAttempt, strings.Split(from, "@")[1], writer.Boundary())
	return append([]byte(headers), body.Bytes()...)
}

// Only a positive DATA acknowledgement means accepted by the SMTP server.
// An interrupted final acknowledgement is uncertain and must not auto-retry.
func deliverInvoice(ctx context.Context, orderID string, i Invoice, pdf []byte) (string, error) {
	host, port, user, password, from := os.Getenv("SMTP_HOST"), os.Getenv("SMTP_PORT"), os.Getenv("SMTP_USERNAME"), os.Getenv("SMTP_PASSWORD"), os.Getenv("SMTP_FROM")
	a, err := mail.ParseAddress(from)
	if host == "" || user == "" || password == "" || err != nil || a.Address != from || strings.ContainsAny(from, "\r\n") {
		return "FAILED", errors.New("发票邮箱服务未配置，请配置 SMTP 环境变量后重试")
	}
	if port == "" {
		port = "465"
	}
	mode := os.Getenv("SMTP_TLS_MODE")
	if mode == "" {
		mode = "tls"
	}
	if mode != "tls" && mode != "starttls" {
		return "FAILED", errors.New("SMTP_TLS_MODE 须为 tls 或 starttls")
	}
	deadline := time.Now().Add(15 * time.Second)
	if d, ok := ctx.Deadline(); ok && d.Before(deadline) {
		deadline = d
	}
	conn, err := (&net.Dialer{Timeout: 5 * time.Second}).DialContext(ctx, "tcp", net.JoinHostPort(host, port))
	if err != nil {
		return "FAILED", errors.New("邮件服务器连接失败，请检查配置或网络后重试")
	}
	defer conn.Close()
	conn.SetDeadline(deadline)
	config := &tls.Config{ServerName: host, MinVersion: tls.VersionTLS12}
	if mode == "tls" {
		secure := tls.Client(conn, config)
		if err = secure.HandshakeContext(ctx); err != nil {
			return "FAILED", errors.New("邮件服务器 TLS 连接失败")
		}
		conn = secure
	}
	client, err := smtp.NewClient(conn, host)
	if err != nil {
		return "FAILED", errors.New("邮件服务器连接失败")
	}
	defer client.Close()
	if mode == "starttls" {
		if err = client.StartTLS(config); err != nil {
			return "FAILED", errors.New("邮件服务器 STARTTLS 连接失败")
		}
	}
	if err = client.Auth(smtp.PlainAuth("", user, password, host)); err != nil {
		return "FAILED", errors.New("邮箱认证失败，请检查 SMTP 账号与授权码")
	}
	return submitInvoiceMessage(client, from, orderID, i, pdf)
}

func submitInvoiceMessage(client *smtp.Client, from, orderID string, i Invoice, pdf []byte) (string, error) {
	if err := client.Mail(from); err != nil {
		return "FAILED", errors.New("邮件服务器拒绝发件人")
	}
	if err := client.Rcpt(i.Email); err != nil {
		return "FAILED", errors.New("邮件服务器拒绝收件人，请核对申请邮箱")
	}
	data, err := client.Data()
	if err != nil {
		return "FAILED", errors.New("邮件服务器拒绝接收发票邮件")
	}
	if _, err = data.Write(invoiceMessage(from, orderID, i, pdf)); err != nil {
		return "FAILED", errors.New("邮件内容传输失败，请重试")
	}
	if err = data.Close(); err != nil {
		var response *textproto.Error
		if errors.As(err, &response) {
			return "FAILED", errors.New("邮件服务器未接受发票邮件，请重试")
		}
		return "UNCERTAIN", errors.New("邮件发送结果待核实，请检查发件服务记录后登记结果")
	}
	return "SENT", nil
}

func (s *Server) sendInvoiceEmail(ctx context.Context, id string) (any, error) {
	tx, err := s.store.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	o, err := s.store.order(ctx, tx, id, true)
	if err != nil {
		return nil, err
	}
	if o == nil || o.Invoice == nil || o.Invoice.Status != "ISSUED" || o.Invoice.FileName == "" {
		return nil, errors.New("请先上传发票并登记开票")
	}
	i := o.Invoice
	if i.EmailStatus == "SENT" || i.EmailStatus == "UNCERTAIN" {
		return i, nil
	}
	if i.EmailStatus == "SENDING" {
		started, _ := time.Parse(time.RFC3339, i.EmailStartedAt)
		if time.Since(started) < time.Minute {
			return i, nil
		}
		i.EmailStatus = "UNCERTAIN"
		i.EmailError = "邮件发送中断，结果待核实，请检查发件服务记录"
		if _, err = tx.ExecContext(ctx, "UPDATE invoice_requests SET payload=? WHERE order_id=?", jsonValue(i), id); err != nil {
			return nil, err
		}
		return i, tx.Commit()
	}
	if !invoiceEligible(o) {
		return nil, errors.New("订单退款状态已变化，不能发送发票")
	}
	var pdf []byte
	if err = tx.QueryRowContext(ctx, "SELECT content FROM invoice_files WHERE order_id=?", id).Scan(&pdf); err != nil {
		return nil, err
	}
	i.EmailStatus = "SENDING"
	i.EmailError = ""
	i.EmailAttempt = randomID("MAIL")
	i.EmailStartedAt = nowString()
	if _, err = tx.ExecContext(ctx, "UPDATE invoice_requests SET payload=? WHERE order_id=?", jsonValue(i), id); err != nil {
		return nil, err
	}
	if err = tx.Commit(); err != nil {
		return nil, err
	}
	status, sendErr := deliverInvoice(ctx, id, *i, pdf)
	i.EmailStatus = status
	if sendErr != nil {
		i.EmailError = sendErr.Error()
	}
	if status == "SENT" {
		i.SentAt = nowString()
	}
	// Persist independently of browser disconnects and the API request deadline.
	saveCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	_, err = s.store.db.ExecContext(saveCtx, "UPDATE invoice_requests SET payload=? WHERE order_id=? AND JSON_UNQUOTE(JSON_EXTRACT(payload,'$.emailAttempt'))=? AND JSON_UNQUOTE(JSON_EXTRACT(payload,'$.emailStatus'))='SENDING'", jsonValue(i), id, i.EmailAttempt)
	return i, err
}

func (s *Server) invoiceFile(ctx context.Context, id, openid string) (any, error) {
	o, err := s.store.order(ctx, s.store.db, id, false)
	if err != nil {
		return nil, err
	}
	if o == nil || (openid != "" && o.OpenID != openid) || o.Invoice == nil || o.Invoice.FileName == "" {
		return nil, errors.New("发票文件不存在")
	}
	var pdf []byte
	if err = s.store.db.QueryRowContext(ctx, "SELECT content FROM invoice_files WHERE order_id=?", id).Scan(&pdf); err != nil {
		return nil, err
	}
	return M{"fileName": o.Invoice.FileName, "fileData": base64.StdEncoding.EncodeToString(pdf)}, nil
}

func (s *Server) resolveInvoiceEmail(ctx context.Context, actor Claims, raw json.RawMessage) (any, error) {
	var d struct{ OrderID, Status, Reason string }
	if err := decode(raw, &d); err != nil {
		return nil, err
	}
	d.Reason = clean(d.Reason, 200)
	if (d.Status != "SENT" && d.Status != "FAILED") || d.Reason == "" {
		return nil, errors.New("请核对邮件服务记录并填写核实说明")
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
	if o == nil || o.Invoice == nil || o.Invoice.EmailStatus != "UNCERTAIN" {
		return nil, errors.New("仅发送结果待核实的发票可登记核实结果")
	}
	i := o.Invoice
	i.EmailStatus = d.Status
	i.EmailError = ""
	if d.Status == "SENT" {
		i.SentAt = nowString()
	} else {
		i.EmailError = "已核实未发送，可重试"
	}
	if _, err = tx.ExecContext(ctx, "UPDATE invoice_requests SET payload=? WHERE order_id=?", jsonValue(i), o.ID); err != nil {
		return nil, err
	}
	if err = tx.Commit(); err != nil {
		return nil, err
	}
	s.store.audit(ctx, actor, "INVOICE_EMAIL_RESOLVE", o.ID, M{"status": d.Status, "reason": d.Reason})
	return i, nil
}
