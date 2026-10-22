package main

import (
	"context"
	"crypto/sha1"
	"crypto/sha256"
	"database/sql"
	"encoding/csv"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/url"
	"os"
	"regexp"
	"strconv"
	"strings"
	"time"
)

var paymentZone = time.FixedZone("Asia/Shanghai", 8*3600)
var billMoneyPattern = regexp.MustCompile(`^[0-9]+(?:\.[0-9]{1,2})?$`)

func (s *Server) queryPayment(ctx context.Context, o *Order) (M, error) {
	result, err := s.wechat.query(o.OrderNo)
	if err != nil {
		s.notePaymentAlert(ctx, "QUERY_FAILED", o.OrderNo, "微信支付查单失败，请核查支付配置或网络")
	}
	return result, err
}

func (s *Server) markVerifiedPayment(ctx context.Context, o *Order, payment M) error {
	amount, _ := payment["amount"].(map[string]any)
	total, ok := amount["total"].(float64)
	tx, _ := payment["transaction_id"].(string)
	success, _ := payment["success_time"].(string)
	paid, err := time.Parse(time.RFC3339, success)
	if !ok || total != float64(o.Quote.Total) || amount["currency"] != "CNY" || payment["trade_state"] != "SUCCESS" || payment["mchid"] != os.Getenv("WECHAT_MCH_ID") || payment["appid"] != os.Getenv("WECHAT_APP_ID") || payment["out_trade_no"] != o.OrderNo || tx == "" || err != nil || o.PaymentStatus == "PAID" && o.TransactionID != tx {
		s.notePaymentAlert(ctx, "PAYMENT_MISMATCH", o.OrderNo, "支付金额、交易号、时间或商户信息校验失败")
		return errors.New("支付结果校验失败")
	}
	err = s.markPaid(ctx, o.ID, tx, paid)
	if err != nil {
		s.notePaymentAlert(ctx, "PAYMENT_RECORD_FAILED", o.OrderNo, "微信已支付，本地入账失败，请人工核查")
	}
	return err
}

type PaymentAlert struct {
	ID          string `json:"id"`
	Kind        string `json:"kind"`
	Target      string `json:"target"`
	Message     string `json:"message"`
	Status      string `json:"status"`
	Occurrences int    `json:"occurrences"`
	LastSeen    string `json:"lastSeen"`
	Note        string `json:"note"`
}

func (s *Server) paymentAlert(ctx context.Context, kind, target, message string) error {
	key := sha256.Sum256([]byte(kind + ":" + target))
	_, err := s.store.db.ExecContext(ctx, `INSERT INTO payment_alerts(id,kind,target,message) VALUES(?,?,?,?) ON DUPLICATE KEY UPDATE occurrences=occurrences+1,last_seen=NOW(3),status='OPEN',message=VALUES(message),resolution_note='',resolved_by=''`, hex.EncodeToString(key[:]), kind, clean(target, 80), clean(message, 240))
	if err != nil {
		log.Printf("payment alert persistence failed kind=%s: %v", kind, err)
	}
	return err
}

// Untrusted callback contents and provider errors must never be stored in alerts.
func (s *Server) notePaymentAlert(ctx context.Context, kind, target, message string) {
	if s.store != nil {
		_ = s.paymentAlert(ctx, kind, target, message)
	}
}

func paymentBillDate(value string, now time.Time) (string, error) {
	if value == "" {
		value = now.In(paymentZone).AddDate(0, 0, -1).Format("2006-01-02")
	}
	date, err := time.ParseInLocation("2006-01-02", value, paymentZone)
	today := now.In(paymentZone).Format("2006-01-02")
	boundary, _ := time.ParseInLocation("2006-01-02", today, paymentZone)
	if err != nil || value >= today || date.Before(boundary.AddDate(0, -3, 0)) {
		return "", errors.New("请选择近三个月内、今天之前的账单日期")
	}
	return value, nil
}

type billPayment struct {
	No, Transaction string
	Total           int
}

func billMoney(value string) (int, error) {
	value = strings.TrimPrefix(strings.TrimSpace(value), "`")
	if !billMoneyPattern.MatchString(value) {
		return 0, errors.New("账单金额格式无效")
	}
	parts := strings.Split(value, ".")
	if len(parts) > 2 || len(parts) == 2 && len(parts[1]) > 2 {
		return 0, errors.New("账单金额格式无效")
	}
	whole, err := strconv.Atoi(parts[0])
	if err != nil || whole < 0 || whole > 20000000 {
		return 0, errors.New("账单金额无效")
	}
	fraction := 0
	if len(parts) == 2 {
		digits := parts[1] + strings.Repeat("0", 2-len(parts[1]))
		fraction, err = strconv.Atoi(digits)
		if err != nil || fraction < 0 {
			return 0, errors.New("账单金额无效")
		}
	}
	return whole*100 + fraction, nil
}

func parsePaymentBill(raw []byte) ([]billPayment, error) {
	reader := csv.NewReader(strings.NewReader(strings.TrimPrefix(string(raw), "\ufeff")))
	reader.FieldsPerRecord = -1
	header, err := reader.Read()
	if err != nil {
		return nil, errors.New("交易账单为空")
	}
	cols := map[string]int{}
	for i, v := range header {
		cols[strings.TrimSpace(v)] = i
	}
	for _, key := range []string{"商户订单号", "微信订单号", "交易状态", "订单金额"} {
		if _, ok := cols[key]; !ok {
			return nil, fmt.Errorf("账单缺少字段：%s", key)
		}
	}
	out := []billPayment{}
	seen := map[string]bool{}
	summarySeen := false
	for {
		row, e := reader.Read()
		if e == io.EOF {
			break
		}
		if e != nil {
			return nil, e
		}
		if len(row) > 0 && strings.HasPrefix(strings.TrimSpace(row[0]), "总交易单数") {
			summary, e := reader.Read()
			if e != nil || len(summary) != len(row) {
				return nil, errors.New("账单汇总无效")
			}
			count, e := strconv.Atoi(strings.TrimPrefix(strings.TrimSpace(summary[0]), "`"))
			if e != nil || count != len(out) {
				return nil, errors.New("账单交易笔数不一致")
			}
			summarySeen = true
			break
		}
		if len(row) != len(header) {
			return nil, errors.New("账单行字段数量无效")
		}
		get := func(key string) string { return strings.TrimPrefix(strings.TrimSpace(row[cols[key]]), "`") }
		// SUCCESS bills contain payment transactions only; refunds are reconciled separately.
		if get("交易状态") != "SUCCESS" {
			return nil, errors.New("成功交易账单含有非成功记录")
		}
		no, tx := get("商户订单号"), get("微信订单号")
		total, e := billMoney(get("订单金额"))
		if e != nil {
			return nil, e
		}
		if no == "" || tx == "" || seen[no] {
			return nil, errors.New("账单订单号为空或重复")
		}
		seen[no] = true
		out = append(out, billPayment{no, tx, total})
	}
	if !summarySeen {
		return nil, errors.New("账单缺少汇总信息")
	}
	return out, nil
}

func (w *Wechat) paymentBill(date string) ([]billPayment, error) {
	raw, err := w.downloadBill("/v3/bill/tradebill?bill_date=" + url.QueryEscape(date) + "&bill_type=SUCCESS")
	if err != nil {
		return nil, err
	}
	if raw == nil {
		return []billPayment{}, nil
	}
	return parsePaymentBill(raw)
}

func (w *Wechat) downloadBill(path string) ([]byte, error) {
	result, err := w.request("GET", path, nil)
	if err != nil {
		var apiErr *wechatAPIError
		if errors.As(err, &apiErr) && apiErr.Code == "NO_STATEMENT_EXIST" {
			return nil, nil
		}
		return nil, err
	}
	link, _ := result["download_url"].(string)
	expected, _ := result["hash_value"].(string)
	u, err := url.Parse(link)
	if err != nil || u.Scheme != "https" || u.Host != "api.mch.weixin.qq.com" || u.User != nil || u.Fragment != "" || !strings.HasPrefix(u.Path, "/v3/bill") || result["hash_type"] != "SHA1" || len(expected) != 40 {
		return nil, errors.New("账单下载信息无效")
	}
	path = u.RequestURI()
	ts := strconv.FormatInt(time.Now().Unix(), 10)
	nonce := randomNonce()
	signature, err := w.sign("GET\n" + path + "\n" + ts + "\n" + nonce + "\n\n")
	if err != nil {
		return nil, err
	}
	req, err := http.NewRequest("GET", u.String(), nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", fmt.Sprintf(`WECHATPAY2-SHA256-RSA2048 mchid="%s",nonce_str="%s",timestamp="%s",serial_no="%s",signature="%s"`, os.Getenv("WECHAT_MCH_ID"), nonce, ts, os.Getenv("WECHAT_MCH_SERIAL_NO"), signature))
	client := *w.client
	client.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		return nil, errors.New("账单下载失败")
	}
	raw, err := io.ReadAll(io.LimitReader(resp.Body, (32<<20)+1))
	if err != nil {
		return nil, err
	}
	if len(raw) > 32<<20 {
		return nil, errors.New("账单超过32MB，请人工对账")
	}
	hash := sha1.Sum(raw)
	if !strings.EqualFold(hex.EncodeToString(hash[:]), expected) {
		return nil, errors.New("账单完整性校验失败")
	}
	return raw, nil
}

func paymentDifference(o *Order, p billPayment) string {
	if o == nil {
		return "账单有支付记录，本地订单不存在"
	}
	if o.Quote.Total != p.Total {
		return "账单金额与本地订单金额不一致"
	}
	if o.PaymentStatus != "PAID" && o.PaymentStatus != "REFUNDING" && o.PaymentStatus != "REFUNDED" {
		return "微信已支付，本地订单未入账"
	}
	if o.TransactionID != p.Transaction {
		return "微信交易号与本地交易号不一致"
	}
	return ""
}

func (s *Server) reconcilePayments(ctx context.Context, date string) (any, error) {
	var err error
	date, err = paymentBillDate(date, time.Now())
	if err != nil {
		return nil, err
	}
	// MySQL connection-scoped lock prevents simultaneous runs across replicas.
	conn, err := s.store.db.Conn(ctx)
	if err != nil {
		return nil, err
	}
	defer conn.Close()
	var locked int
	if err = conn.QueryRowContext(ctx, "SELECT GET_LOCK(?,0)", "payment-reconciliation").Scan(&locked); err != nil {
		return nil, err
	}
	if locked != 1 {
		return nil, errors.New("对账任务正在运行，请稍后刷新")
	}
	defer func() {
		releaseCtx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		defer cancel()
		_, _ = conn.ExecContext(releaseCtx, "SELECT RELEASE_LOCK(?)", "payment-reconciliation")
	}()
	_, err = s.store.db.ExecContext(ctx, `INSERT INTO payment_reconciliations(bill_date,status,payload) VALUES(?,'RUNNING',?) ON DUPLICATE KEY UPDATE status='RUNNING',payload=VALUES(payload),updated_at=NOW(3)`, date, jsonValue(M{"billDate": date, "status": "RUNNING"}))
	if err != nil {
		return nil, err
	}
	result := M{"billDate": date, "status": "FAILED", "checked": 0, "differences": 0, "finishedAt": nowString()}
	payments, err := s.wechat.paymentBill(date)
	if err == nil {
		seen := map[string]bool{}
		for _, p := range payments {
			seen[p.No] = true
			var local Order
			e := s.store.db.QueryRowContext(ctx, "SELECT payment_status,total,transaction_id FROM orders WHERE order_no=?", p.No).Scan(&local.PaymentStatus, &local.Quote.Total, &local.TransactionID)
			var o *Order
			if e == nil {
				o = &local
			}
			if e != nil && !errors.Is(e, sql.ErrNoRows) {
				err = e
				break
			}
			result["checked"] = result["checked"].(int) + 1
			if message := paymentDifference(o, p); message != "" {
				result["differences"] = result["differences"].(int) + 1
				if e = s.paymentAlert(ctx, "BILL_DIFFERENCE", date+":"+p.No, message); e != nil {
					err = e
					break
				}
			}
		}
		// paidAt is recorded for new payments. Historical orders lacking it can still
		// be compared when present in the provider bill, but have no reliable local date.
		if err == nil {
			start, _ := time.ParseInLocation("2006-01-02", date, paymentZone)
			end := start.AddDate(0, 0, 1)
			rows, e := s.store.db.QueryContext(ctx, `SELECT order_no FROM orders WHERE payment_status IN ('PAID','REFUNDING','REFUNDED') AND JSON_UNQUOTE(JSON_EXTRACT(payload,'$.paidAt'))>=? AND JSON_UNQUOTE(JSON_EXTRACT(payload,'$.paidAt'))<?`, start.UTC().Format(time.RFC3339), end.UTC().Format(time.RFC3339))
			if e != nil {
				err = e
			} else {
				missing := []string{}
				for rows.Next() {
					var no string
					if e = rows.Scan(&no); e != nil {
						break
					}
					if !seen[no] {
						missing = append(missing, no)
					}
				}
				if e == nil {
					e = rows.Err()
				}
				rows.Close()
				err = e
				for _, no := range missing {
					if err != nil {
						break
					}
					result["differences"] = result["differences"].(int) + 1
					err = s.paymentAlert(ctx, "BILL_DIFFERENCE", date+":"+no, "本地已支付，微信成功交易账单缺少此订单")
				}
			}
		}
	}
	if err != nil {
		result["message"] = "对账失败，请检查微信支付配置、账单生成状态及服务日志"
		_ = s.paymentAlert(ctx, "RECONCILIATION_FAILED", date, "交易账单对账失败，请检查配置或稍后重试")
		log.Printf("payment reconciliation date=%s failed: %v", date, err)
	} else if result["differences"].(int) > 0 {
		result["status"] = "DIFFERENCES"
	} else {
		result["status"] = "MATCHED"
	}
	result["finishedAt"] = nowString()
	_, saveErr := s.store.db.ExecContext(ctx, "UPDATE payment_reconciliations SET status=?,payload=?,updated_at=NOW(3) WHERE bill_date=?", result["status"], jsonValue(result), date)
	if saveErr != nil {
		return nil, saveErr
	}
	return result, nil
}

func (s *Server) paymentOperations(ctx context.Context, actor Claims, req Request) (any, error) {
	switch req.Action {
	case "admin.payment.financeReconcile":
		var d struct{ BillDate, Kind string }
		if err := decode(req.Data, &d); err != nil {
			return nil, err
		}
		result, err := s.reconcileFinance(ctx, d.BillDate, d.Kind)
		if err == nil {
			s.store.audit(ctx, actor, "PAYMENT_FINANCE_RECONCILE", d.Kind, result)
		}
		return result, err
	case "admin.payment.alertCount":
		var count int
		err := s.store.db.QueryRowContext(ctx, "SELECT COUNT(*) FROM payment_alerts WHERE status='OPEN'").Scan(&count)
		return M{"count": count}, err
	case "admin.payment.reconcile":
		result, err := s.reconcilePayments(ctx, mustID(req.Data, "billDate"))
		if err == nil {
			s.store.audit(ctx, actor, "PAYMENT_RECONCILE", "", result)
		}
		return result, err
	case "admin.payment.resolve":
		var d struct{ ID, Note string }
		if err := decode(req.Data, &d); err != nil {
			return nil, err
		}
		d.Note = clean(d.Note, 240)
		if d.ID == "" || d.Note == "" {
			return nil, errors.New("请填写告警处理备注")
		}
		res, err := s.store.db.ExecContext(ctx, "UPDATE payment_alerts SET status='RESOLVED',resolution_note=?,resolved_by=? WHERE id=? AND status='OPEN'", d.Note, actor.Subject, d.ID)
		if err != nil {
			return nil, err
		}
		n, err := res.RowsAffected()
		if err != nil {
			return nil, err
		}
		if n != 1 {
			return nil, errors.New("告警不存在或已处理")
		}
		s.store.audit(ctx, actor, "PAYMENT_ALERT_RESOLVE", d.ID, M{"note": d.Note})
		return M{"resolved": true}, nil
	case "admin.payment.overview":
		rows, err := s.store.db.QueryContext(ctx, "SELECT payload FROM payment_reconciliations ORDER BY bill_date DESC LIMIT 31")
		if err != nil {
			return nil, err
		}
		runs := []M{}
		for rows.Next() {
			var raw []byte
			if err = rows.Scan(&raw); err != nil {
				rows.Close()
				return nil, err
			}
			var run M
			if err = json.Unmarshal(raw, &run); err != nil {
				rows.Close()
				return nil, err
			}
			runs = append(runs, run)
		}
		err = rows.Err()
		rows.Close()
		if err != nil {
			return nil, err
		}
		rows, err = s.store.db.QueryContext(ctx, "SELECT id,kind,target,message,status,occurrences,CAST(last_seen AS CHAR),resolution_note FROM payment_alerts ORDER BY status ASC,last_seen DESC LIMIT 100")
		if err != nil {
			return nil, err
		}
		defer rows.Close()
		alerts := []PaymentAlert{}
		for rows.Next() {
			var a PaymentAlert
			if err = rows.Scan(&a.ID, &a.Kind, &a.Target, &a.Message, &a.Status, &a.Occurrences, &a.LastSeen, &a.Note); err != nil {
				return nil, err
			}
			alerts = append(alerts, a)
		}
		if err = rows.Err(); err != nil {
			return nil, err
		}
		var open int
		if err = s.store.db.QueryRowContext(ctx, "SELECT COUNT(*) FROM payment_alerts WHERE status='OPEN'").Scan(&open); err != nil {
			return nil, err
		}
		finance, err := s.financeRuns(ctx)
		if err != nil {
			return nil, err
		}
		return M{"runs": runs, "financeRuns": finance, "alerts": alerts, "openAlerts": open}, nil
	}
	return nil, errors.New("未知支付操作")
}

func (s *Server) runPaymentReconciler(ctx context.Context) {
	tick := time.NewTicker(30 * time.Minute)
	defer tick.Stop()
	for {
		now := time.Now().In(paymentZone)
		if now.Hour() >= 10 {
			date := now.AddDate(0, 0, -1).Format("2006-01-02")
			var status string
			jobCtx, cancel := context.WithTimeout(ctx, 10*time.Minute)
			err := s.store.db.QueryRowContext(jobCtx, "SELECT status FROM payment_reconciliations WHERE bill_date=?", date).Scan(&status)
			if err != nil || status == "FAILED" || status == "RUNNING" {
				if _, e := s.reconcilePayments(jobCtx, date); e != nil {
					log.Printf("automatic payment reconciliation failed: %v", e)
				}
			}
			for _, kind := range []string{"REFUND", "BASIC", "FEES", "OPERATION"} {
				var status string
				e := s.store.db.QueryRowContext(jobCtx, "SELECT status FROM payment_finance_runs WHERE bill_date=? AND kind=?", date, kind).Scan(&status)
				if e != nil || status == "FAILED" || status == "RUNNING" {
					if _, e = s.reconcileFinance(jobCtx, date, kind); e != nil {
						log.Printf("automatic finance reconciliation kind=%s failed: %v", kind, e)
					}
				}
			}
			cancel()
		}
		select {
		case <-ctx.Done():
			return
		case <-tick.C:
		}
	}
}
