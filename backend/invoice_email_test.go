package main

import (
	"bufio"
	"bytes"
	"context"
	"encoding/base64"
	"fmt"
	"io"
	"mime"
	"mime/multipart"
	"net"
	"net/mail"
	"net/smtp"
	"net/textproto"
	"strings"
	"testing"
	"time"
)

func TestInvoicePDFAndApplicantMetadata(t *testing.T) {
	pdf := []byte("%PDF-1.4\n%%EOF")
	encoded := base64.StdEncoding.EncodeToString(pdf)
	got, err := decodeInvoicePDF("电子发票.pdf", encoded)
	if err != nil || !bytes.Equal(got, pdf) {
		t.Fatal(err)
	}
	for _, data := range []struct{ name, body string }{{"invoice.html", encoded}, {"bad\r\n.pdf", encoded}, {"invoice.pdf", "bad"}, {"invoice.pdf", base64.StdEncoding.EncodeToString([]byte("%PDF-missing EOF"))}, {"invoice.pdf", strings.Repeat("A", base64.StdEncoding.EncodedLen(maxInvoicePDF)+1)}} {
		if _, err := decodeInvoicePDF(data.name, data.body); err == nil {
			t.Fatalf("accepted invalid file %s", data.name)
		}
	}
	i, err := validateInvoice(Invoice{Kind: "personal", Title: "张三", Email: "test@example.com", FileName: "stolen.pdf", Number: "fake", EmailStatus: "SENT", SentAt: "fake"})
	if err != nil || i.FileName != "" || i.Number != "" || i.EmailStatus != "" || i.SentAt != "" {
		t.Fatal("client metadata not discarded", i, err)
	}
}

func TestInvoiceMIMEAttachment(t *testing.T) {
	pdf := []byte("%PDF-1.4\n%%EOF")
	i := Invoice{Email: "test@example.com", Title: "测试企业", Number: "0001", Amount: 12345, FileName: "电子发票.pdf", EmailAttempt: "MAIL123"}
	msg, err := mail.ReadMessage(bytes.NewReader(invoiceMessage("sender@example.com", "order1", i, pdf)))
	if err != nil {
		t.Fatal(err)
	}
	if msg.Header.Get("To") != i.Email || msg.Header.Get("Message-ID") != "<MAIL123@example.com>" {
		t.Fatal(msg.Header)
	}
	_, params, err := mime.ParseMediaType(msg.Header.Get("Content-Type"))
	if err != nil {
		t.Fatal(err)
	}
	reader := multipart.NewReader(msg.Body, params["boundary"])
	text, err := reader.NextPart()
	if err != nil {
		t.Fatal(err)
	}
	content, err := io.ReadAll(base64.NewDecoder(base64.StdEncoding, text))
	if err != nil || !strings.Contains(string(content), "123.45") || !strings.Contains(string(content), i.Title) {
		t.Fatal(string(content), err)
	}
	attachment, err := reader.NextPart()
	if err != nil {
		t.Fatal(err)
	}
	_, fileParams, err := mime.ParseMediaType(attachment.Header.Get("Content-Disposition"))
	if err != nil || fileParams["filename"] != i.FileName {
		t.Fatal(fileParams, err)
	}
	got, err := io.ReadAll(base64.NewDecoder(base64.StdEncoding, attachment))
	if err != nil || !bytes.Equal(got, pdf) {
		t.Fatal("attachment changed", err)
	}
}

func TestInvoiceSMTPAcknowledgement(t *testing.T) {
	for _, test := range []struct{ name, reply, want string }{{"accepted", "250 accepted", "SENT"}, {"rejected", "550 refused", "FAILED"}, {"connection lost", "", "UNCERTAIN"}} {
		t.Run(test.name, func(t *testing.T) {
			clientConn, serverConn := net.Pipe()
			defer clientConn.Close()
			clientConn.SetDeadline(time.Now().Add(3 * time.Second))
			done := make(chan error, 1)
			go func() {
				defer serverConn.Close()
				reader := textproto.NewReader(bufio.NewReader(serverConn))
				fmt.Fprint(serverConn, "220 test SMTP\r\n")
				for _, prefix := range []string{"EHLO ", "MAIL FROM:", "RCPT TO:", "DATA"} {
					line, err := reader.ReadLine()
					if err != nil {
						done <- err
						return
					}
					if !strings.HasPrefix(line, prefix) {
						done <- fmt.Errorf("unexpected %s", line)
						return
					}
					if prefix == "DATA" {
						fmt.Fprint(serverConn, "354 proceed\r\n")
					} else {
						fmt.Fprint(serverConn, "250 OK\r\n")
					}
				}
				body, err := io.ReadAll(reader.DotReader())
				if err != nil || !bytes.Contains(body, []byte("application/pdf")) {
					done <- fmt.Errorf("missing PDF: %v", err)
					return
				}
				if test.reply != "" {
					fmt.Fprint(serverConn, test.reply+"\r\n")
				}
				done <- nil
			}()
			client, err := smtp.NewClient(clientConn, "localhost")
			if err != nil {
				t.Fatal(err)
			}
			defer client.Close()
			status, err := submitInvoiceMessage(client, "sender@example.com", "o1", Invoice{Email: "recipient@example.com", FileName: "invoice.pdf", EmailAttempt: "MAIL123"}, []byte("%PDF-1.4\n%%EOF"))
			if status != test.want || (err == nil) != (test.want == "SENT") {
				t.Fatal(status, err)
			}
			if err := <-done; err != nil {
				t.Fatal(err)
			}
		})
	}
}

func TestInvoiceSMTPMissingConfig(t *testing.T) {
	t.Setenv("SMTP_HOST", "")
	status, err := deliverInvoice(context.Background(), "o1", Invoice{}, nil)
	if status != "FAILED" || err == nil {
		t.Fatal(status, err)
	}
}

func TestInvoiceFilters(t *testing.T) {
	for _, filter := range []string{"invoice_pending", "invoice_email"} {
		d, err := normalizeOrderSearch(OrderSearch{Filter: filter}, true)
		if err != nil {
			t.Fatal(err)
		}
		where, _ := orderSearchWhere("", d)
		if !strings.Contains(where, "invoice_requests") || !strings.Contains(where, "order_id=orders.id") {
			t.Fatal(where)
		}
	}
}
