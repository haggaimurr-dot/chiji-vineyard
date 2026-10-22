package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"log"
	"net/http"
	"net/url"
	"os"
	"strings"
	"time"
)

// Application-owned gateway contract. The gateway must persist idempotency
// keys and return the same invoice on replay, including after network failures.
type AutoInvoiceResult struct {
	Status   string `json:"status"`
	Key      string `json:"requestId"`
	Number   string `json:"number"`
	FileName string `json:"fileName"`
	FileData string `json:"fileData"`
	Amount   int    `json:"amount"`
	Title    string `json:"title"`
	TaxID    string `json:"taxId"`
}

func autoInvoiceConfig() (string, string, error) {
	endpoint, token := strings.TrimSpace(os.Getenv("INVOICE_GATEWAY_URL")), strings.TrimSpace(os.Getenv("INVOICE_GATEWAY_TOKEN"))
	u, err := url.Parse(endpoint)
	if err != nil || u.Scheme != "https" || u.Host == "" || u.User != nil || u.Fragment != "" || token == "" {
		return "", "", errors.New("自动开票服务未配置，请配置 HTTPS 开票网关与密钥")
	}
	return endpoint, token, nil
}
func requestAutoInvoice(ctx context.Context, client *http.Client, endpoint, token string, o Order, i Invoice) (AutoInvoiceResult, string, error) {
	var result AutoInvoiceResult
	body, _ := json.Marshal(M{"requestId": i.AutoKey, "orderId": o.ID, "orderNo": o.OrderNo, "kind": i.Kind, "title": i.Title, "taxId": i.TaxID, "amount": i.Amount, "items": o.Items})
	req, err := http.NewRequestWithContext(ctx, "POST", endpoint, bytes.NewReader(body))
	if err != nil {
		return result, "FAILED", errors.New("开票服务地址无效")
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Idempotency-Key", i.AutoKey)
	response, err := client.Do(req)
	if err != nil {
		return result, "UNCERTAIN", errors.New("开票服务响应中断，结果待核实，请按原请求查询")
	}
	defer response.Body.Close()
	if response.StatusCode != 200 {
		return result, "UNCERTAIN", errors.New("开票服务未返回确认结果，请按原请求查询")
	}
	content, err := io.ReadAll(io.LimitReader(response.Body, (5<<20)+1))
	if err != nil || len(content) > 5<<20 || json.Unmarshal(content, &result) != nil || result.Key != i.AutoKey {
		return result, "UNCERTAIN", errors.New("开票返回格式或请求标识无效，结果待核实")
	}
	// REJECTED guarantees no invoice was issued for this key.
	if result.Status == "REJECTED" {
		return result, "FAILED", errors.New("开票服务拒绝申请，请核对资料或联系服务商")
	}
	if result.Status != "ISSUED" || strings.TrimSpace(result.Number) == "" || len([]rune(result.Number)) > 80 || result.Amount != i.Amount || result.Title != i.Title || result.TaxID != i.TaxID {
		return result, "UNCERTAIN", errors.New("开票结果或发票资料不一致，请核实原请求")
	}
	if _, err = decodeInvoicePDF(result.FileName, result.FileData); err != nil {
		return result, "UNCERTAIN", errors.New("开票服务返回的 PDF 无效，请核实原请求")
	}
	return result, "ISSUED", nil
}
func autoInvoiceBusy(i *Invoice) bool {
	return i != nil && (i.AutoStatus == "PROCESSING" || i.AutoStatus == "UNCERTAIN")
}
func (s *Server) issueAutoInvoice(ctx context.Context, id string, actor *Claims, retry bool) (any, error) {
	endpoint, token, err := autoInvoiceConfig()
	if err != nil {
		return nil, err
	}
	tx, err := s.store.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	o, err := s.store.order(ctx, tx, id, true)
	if err != nil {
		return nil, err
	}
	if o == nil || o.Invoice == nil {
		return nil, errors.New("没有发票申请")
	}
	i := o.Invoice
	if i.Status == "ISSUED" {
		return i, nil
	}
	if i.Status != "APPLIED" {
		return nil, errors.New("没有待处理发票申请")
	}
	if i.AutoStatus == "PROCESSING" {
		started, _ := time.Parse(time.RFC3339, i.AutoStartedAt)
		if time.Since(started) < time.Minute {
			return i, nil
		}
		i.AutoStatus = "UNCERTAIN"
		i.AutoError = "开票任务中断，请按原请求核查结果"
		if _, err = tx.ExecContext(ctx, "UPDATE invoice_requests SET payload=? WHERE order_id=?", jsonValue(i), id); err != nil {
			return nil, err
		}
		return i, tx.Commit()
	}
	if !retry && i.AutoStatus != "" && i.AutoStatus != "READY" {
		return i, nil
	}
	// Uncertain attempts need their original result even after eligibility changes.
	if i.AutoStatus != "UNCERTAIN" && !invoiceEligible(o) {
		i.AutoStatus = "BLOCKED"
		i.AutoError = "订单存在退款，暂停自动开票"
		if _, err = tx.ExecContext(ctx, "UPDATE invoice_requests SET payload=? WHERE order_id=?", jsonValue(i), id); err != nil {
			return nil, err
		}
		return i, tx.Commit()
	}
	if i.AutoKey == "" {
		i.AutoKey = randomID("INV")
	}
	i.AutoStatus = "PROCESSING"
	i.AutoError = ""
	i.AutoStartedAt = nowString()
	if _, err = tx.ExecContext(ctx, "UPDATE invoice_requests SET payload=? WHERE order_id=?", jsonValue(i), id); err != nil {
		return nil, err
	}
	if actor != nil {
		if _, err = tx.ExecContext(ctx, "INSERT INTO audit_logs(admin_id,username,action,target_id,detail) VALUES(?,?,?,?,?)", actor.Subject, actor.Username, "INVOICE_AUTO_REQUEST", id, jsonValue(M{"requestId": i.AutoKey})); err != nil {
			return nil, err
		}
	}
	if err = tx.Commit(); err != nil {
		return nil, err
	}
	client := &http.Client{Timeout: 15 * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	result, status, issueErr := requestAutoInvoice(ctx, client, endpoint, token, *o, *i)
	saveCtx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	save, err := s.store.db.BeginTx(saveCtx, nil)
	if err != nil {
		return nil, err
	}
	defer save.Rollback()
	current, err := s.store.order(saveCtx, save, id, true)
	if err != nil {
		return nil, err
	}
	if current == nil || current.Invoice == nil || current.Invoice.AutoKey != i.AutoKey || current.Invoice.Status != "APPLIED" {
		return nil, errors.New("发票状态已变化，请核实原请求")
	}
	i = current.Invoice
	i.AutoStatus = status
	i.AutoError = ""
	if issueErr != nil {
		i.AutoError = issueErr.Error()
	}
	if status == "ISSUED" {
		pdf, _ := decodeInvoicePDF(result.FileName, result.FileData)
		if _, err = save.ExecContext(saveCtx, "INSERT INTO invoice_files(order_id,content) VALUES(?,?)", id, pdf); err != nil {
			return nil, err
		}
		i.Status = "ISSUED"
		i.Number = result.Number
		i.FileName = result.FileName
		i.IssuedAt = nowString()
		i.EmailStatus = "READY"
	}
	if _, err = save.ExecContext(saveCtx, "UPDATE invoice_requests SET payload=? WHERE order_id=?", jsonValue(i), id); err != nil {
		return nil, err
	}
	if _, err = save.ExecContext(saveCtx, "INSERT INTO audit_logs(admin_id,username,action,target_id,detail) VALUES('SYSTEM','自动开票','INVOICE_AUTO_RESULT',?,?)", id, jsonValue(M{"requestId": i.AutoKey, "status": status})); err != nil {
		return nil, err
	}
	if err = save.Commit(); err != nil {
		return nil, err
	}
	if status == "ISSUED" {
		return s.sendInvoiceEmail(saveCtx, id)
	}
	return i, nil
}
func (s *Server) runAutoInvoices(ctx context.Context) {
	ticker := time.NewTicker(10 * time.Second)
	defer ticker.Stop()
	for {
		if os.Getenv("AUTO_INVOICE_ENABLED") == "true" {
			if err := s.processAutoInvoices(ctx); err != nil {
				log.Printf("auto invoice scan: %v", err)
			}
		}
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
	}
}
func (s *Server) processAutoInvoices(ctx context.Context) error {
	if _, _, err := autoInvoiceConfig(); err != nil {
		return err
	}
	rows, err := s.store.db.QueryContext(ctx, `SELECT order_id FROM invoice_requests WHERE JSON_UNQUOTE(JSON_EXTRACT(payload,'$.status'))='APPLIED' AND (JSON_EXTRACT(payload,'$.autoStatus') IS NULL OR JSON_UNQUOTE(JSON_EXTRACT(payload,'$.autoStatus')) IN ('READY','PROCESSING')) ORDER BY order_id LIMIT 10`)
	if err != nil {
		return err
	}
	ids := []string{}
	for rows.Next() {
		var id string
		if err = rows.Scan(&id); err != nil {
			rows.Close()
			return err
		}
		ids = append(ids, id)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return err
	}
	for _, id := range ids {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		if _, err = s.issueAutoInvoice(ctx, id, nil, false); err != nil {
			log.Printf("auto invoice %s: %v", id, err)
		}
	}
	return nil
}
