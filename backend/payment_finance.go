package main

import (
	"context"
	"database/sql"
	"encoding/csv"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net/url"
	"strconv"
	"strings"
	"time"
)

type financeRow map[string]string

func financeValue(v string) string { return strings.TrimPrefix(strings.TrimSpace(v), "`") }

// Validate the whole file before publishing any totals or differences.
func parseFinanceBill(raw []byte, kind string) ([]financeRow, error) {
	if raw == nil {
		return []financeRow{}, nil
	}
	r := csv.NewReader(strings.NewReader(strings.TrimPrefix(string(raw), "\ufeff")))
	r.FieldsPerRecord = -1
	header, err := r.Read()
	if err != nil {
		return nil, errors.New("账单为空")
	}
	cols := map[string]bool{}
	for i, v := range header {
		header[i] = strings.ReplaceAll(strings.ReplaceAll(financeValue(v), "（", "("), "）", ")")
		if cols[header[i]] {
			return nil, errors.New("账单字段重复")
		}
		cols[header[i]] = true
	}
	required := []string{"资金流水单号", "记账时间", "业务类型", "收支类型", "收支金额(元)", "账户结余(元)"}
	summaryKey := "资金流水总笔数"
	if kind == "REFUND" {
		required = []string{"商户订单号", "商户退款单号", "微信退款单号", "退款状态", "申请退款金额", "退款金额", "手续费"}
		summaryKey = "总交易单数"
	}
	for _, key := range required {
		if !cols[key] {
			return nil, fmt.Errorf("账单缺少字段：%s", key)
		}
	}
	out := []financeRow{}
	seen := map[string]bool{}
	for {
		values, e := r.Read()
		if e != nil {
			if e == io.EOF {
				return nil, errors.New("账单缺少汇总")
			}
			return nil, e
		}
		if len(values) > 0 && financeValue(values[0]) == summaryKey {
			totals, e := r.Read()
			if e != nil || len(totals) != len(values) {
				return nil, errors.New("账单汇总无效")
			}
			summaryCols := map[string]bool{}
			for _, key := range values {
				summaryCols[financeValue(key)] = true
			}
			requiredSummary := []string{"资金流水总笔数", "收入笔数", "收入金额", "支出笔数", "支出金额"}
			if kind == "REFUND" {
				requiredSummary = []string{"总交易单数", "退款总金额", "手续费总金额", "申请退款总金额"}
			}
			for _, key := range requiredSummary {
				if !summaryCols[key] {
					return nil, fmt.Errorf("账单汇总缺少字段：%s", key)
				}
			}
			count, e := strconv.Atoi(financeValue(totals[0]))
			if e != nil || count != len(out) {
				return nil, errors.New("账单笔数不一致")
			}
			for i, key := range values {
				key = financeValue(key)
				var expected int
				check := true
				switch key {
				case "退款总金额":
					for _, row := range out {
						n, _ := billMoney(row["退款金额"])
						expected += n
					}
				case "申请退款总金额":
					for _, row := range out {
						n, _ := billMoney(row["申请退款金额"])
						expected += n
					}
				case "手续费总金额":
					for _, row := range out {
						n, _ := signedBillMoney(row["手续费"])
						expected += n
					}
				case "收入金额", "支出金额":
					direction := "收入"
					if key == "支出金额" {
						direction = "支出"
					}
					for _, row := range out {
						if row["收支类型"] == direction {
							n, _ := billMoney(row["收支金额(元)"])
							expected += n
						}
					}
				case "收入笔数", "支出笔数":
					direction := "收入"
					if key == "支出笔数" {
						direction = "支出"
					}
					for _, row := range out {
						if row["收支类型"] == direction {
							expected++
						}
					}
					actual, e := strconv.Atoi(financeValue(totals[i]))
					if e != nil || actual != expected {
						return nil, errors.New("账单汇总笔数不一致")
					}
					check = false
				default:
					check = false
				}
				if check {
					actual, e := signedBillMoney(totals[i])
					if e != nil || actual != expected {
						return nil, errors.New("账单汇总金额不一致")
					}
				}
			}
			if _, e = r.Read(); e != io.EOF {
				return nil, errors.New("账单汇总后存在额外数据")
			}
			return out, nil
		}
		if len(values) != len(header) {
			return nil, errors.New("账单行字段数量无效")
		}
		row := financeRow{}
		for i, v := range values {
			row[header[i]] = financeValue(v)
		}
		id := row["资金流水单号"]
		if kind == "REFUND" {
			id = row["商户退款单号"]
			if row["商户订单号"] == "" || row["微信退款单号"] == "" {
				return nil, errors.New("退款关联单号缺失")
			}
			switch row["退款状态"] {
			case "SUCCESS", "PROCESSING", "CHANGE", "REFUNDCLOSE", "ABNORMAL", "CLOSED":
			default:
				return nil, errors.New("未知退款状态")
			}
			for _, key := range []string{"申请退款金额", "退款金额"} {
				if _, e = billMoney(row[key]); e != nil {
					return nil, e
				}
			}
			if _, e = signedBillMoney(row["手续费"]); e != nil {
				return nil, e
			}
		} else {
			if row["收支类型"] != "收入" && row["收支类型"] != "支出" {
				return nil, errors.New("未知收支类型")
			}
			if _, e = time.ParseInLocation("2006-01-02 15:04:05", row["记账时间"], paymentZone); e != nil {
				return nil, errors.New("记账时间无效")
			}
			for _, key := range []string{"收支金额(元)", "账户结余(元)"} {
				if _, e = billMoney(row[key]); e != nil {
					return nil, e
				}
			}
		}
		if id == "" || seen[id] {
			return nil, errors.New("账单单号为空或重复")
		}
		seen[id] = true
		out = append(out, row)
	}
}
func signedBillMoney(value string) (int, error) {
	value = financeValue(value)
	sign := 1
	if strings.HasPrefix(value, "-") {
		sign = -1
		value = strings.TrimPrefix(value, "-")
	}
	n, e := billMoney(value)
	return sign * n, e
}
func refundBillDifference(local *Refund, row financeRow) string {
	if local == nil {
		return "微信账单有退款，本地退款单不存在"
	}
	amount, _ := billMoney(row["申请退款金额"])
	if local.OrderNo != row["商户订单号"] || local.Amount != amount {
		return "退款关联订单或申请金额不一致"
	}
	if local.ProviderRefundID != row["微信退款单号"] {
		return "微信退款单号不一致"
	}
	if row["退款状态"] == "SUCCESS" && local.Status != "SUCCEEDED" {
		return "微信已退款成功，本地退款未完成"
	}
	if local.Status == "APPLIED" || local.Status == "REJECTED" {
		return "微信已受理退款，本地未审批"
	}
	if row["退款状态"] != "SUCCESS" && row["退款状态"] != "PROCESSING" && local.Status != "FAILED" {
		return "微信账单退款异常或关闭，本地退款状态不一致"
	}
	if local.Status != "APPROVED" && local.Status != "SUCCEEDED" && local.Status != "FAILED" {
		return "本地退款状态无效"
	}
	// PROCESSING is an immutable day-end snapshot; the local state may have advanced.
	return ""
}
func financeTotals(rows []financeRow, kind string) M {
	totals := M{"income": 0, "expense": 0, "net": 0, "feesCharged": 0, "feesReturned": 0, "netFees": 0, "settlementGross": 0, "settlementRefund": 0, "settlementNet": 0, "refundRequested": 0, "refundSettled": 0, "refundSuccessful": 0}
	add := func(key string, n int) { totals[key] = totals[key].(int) + n }
	for _, row := range rows {
		if kind == "REFUND" {
			n, _ := billMoney(row["申请退款金额"])
			add("refundRequested", n)
			n, _ = billMoney(row["退款金额"])
			add("refundSettled", n)
			if row["退款状态"] == "SUCCESS" {
				add("refundSuccessful", 1)
			}
			fee, _ := signedBillMoney(row["手续费"])
			if fee < 0 {
				add("feesReturned", -fee)
			} else {
				add("feesCharged", fee)
			}
		} else {
			n, _ := billMoney(row["收支金额(元)"])
			key := "income"
			if row["收支类型"] == "支出" {
				key = "expense"
			}
			add(key, n)
			if row["业务类型"] == "交易" && key == "income" {
				add("settlementGross", n)
			}
			if row["业务类型"] == "退款" && key == "expense" {
				add("settlementRefund", n)
			}
			if row["业务类型"] == "扣除交易手续费" && key == "expense" {
				add("feesCharged", n)
			}
			if row["业务类型"] == "退还交易手续费" && key == "income" {
				add("feesReturned", n)
			}
		}
	}
	totals["net"] = totals["income"].(int) - totals["expense"].(int)
	totals["netFees"] = totals["feesCharged"].(int) - totals["feesReturned"].(int)
	totals["settlementNet"] = totals["settlementGross"].(int) - totals["settlementRefund"].(int) - totals["netFees"].(int)
	return totals
}
func validFinanceKind(kind string) bool {
	return kind == "REFUND" || kind == "BASIC" || kind == "FEES" || kind == "OPERATION"
}
func (s *Server) reconcileFinance(ctx context.Context, date, kind string) (any, error) {
	if !validFinanceKind(kind) {
		return nil, errors.New("请选择退款账单或资金账户")
	}
	date, err := paymentBillDate(date, time.Now())
	if err != nil {
		return nil, err
	}
	conn, err := s.store.db.Conn(ctx)
	if err != nil {
		return nil, err
	}
	defer conn.Close()
	lock := "payment-finance:" + kind
	var locked int
	if err = conn.QueryRowContext(ctx, "SELECT GET_LOCK(?,0)", lock).Scan(&locked); err != nil {
		return nil, err
	}
	if locked != 1 {
		return nil, errors.New("对账任务正在运行，请稍后刷新")
	}
	defer func() {
		c, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		defer cancel()
		_, _ = conn.ExecContext(c, "SELECT RELEASE_LOCK(?)", lock)
	}()
	result := M{"billDate": date, "kind": kind, "status": "RUNNING", "checked": 0, "differences": 0}
	_, err = s.store.db.ExecContext(ctx, `INSERT INTO payment_finance_runs(bill_date,kind,status,payload) VALUES(?,?,'RUNNING',?) ON DUPLICATE KEY UPDATE status='RUNNING',payload=VALUES(payload),updated_at=NOW(3)`, date, kind, jsonValue(result))
	if err != nil {
		return nil, err
	}
	path := "/v3/bill/fundflowbill?bill_date=" + url.QueryEscape(date) + "&account_type=" + kind
	if kind == "REFUND" {
		path = "/v3/bill/tradebill?bill_date=" + url.QueryEscape(date) + "&bill_type=REFUND"
	}
	raw, err := s.wechat.downloadBill(path)
	var rows []financeRow
	if err == nil {
		rows, err = parseFinanceBill(raw, kind)
	}
	details := []M{}
	difference := func(id, message string) error {
		result["differences"] = result["differences"].(int) + 1
		if len(details) < 100 {
			details = append(details, M{"refundNo": id, "message": message})
		}
		return s.paymentAlert(ctx, "REFUND_BILL_DIFFERENCE", date+":"+id, message)
	}
	if err == nil && kind == "REFUND" {
		seen := map[string]bool{}
		for _, row := range rows {
			id := row["商户退款单号"]
			seen[id] = true
			local := Refund{}
			e := s.store.db.QueryRowContext(ctx, "SELECT order_no,amount,status,provider_refund_id FROM refunds WHERE id=?", id).Scan(&local.OrderNo, &local.Amount, &local.Status, &local.ProviderRefundID)
			var p *Refund
			if e == nil {
				p = &local
			} else if !errors.Is(e, sql.ErrNoRows) {
				err = e
				break
			}
			if message := refundBillDifference(p, row); message != "" {
				if err = difference(id, message); err != nil {
					break
				}
			}
		}
		if err == nil {
			start, _ := time.ParseInLocation("2006-01-02", date, paymentZone)
			local, e := s.store.db.QueryContext(ctx, "SELECT id FROM refunds WHERE accepted_at>=? AND accepted_at<?", start.UTC(), start.AddDate(0, 0, 1).UTC())
			if e != nil {
				err = e
			} else {
				missing := []string{}
				for local.Next() {
					var id string
					if e = local.Scan(&id); e != nil {
						break
					}
					if !seen[id] {
						missing = append(missing, id)
					}
				}
				if e == nil {
					e = local.Err()
				}
				local.Close()
				err = e
				for _, id := range missing {
					if err != nil {
						break
					}
					err = difference(id, "本地微信已受理退款，微信退款账单缺单")
				}
			}
		}
	}
	if err != nil {
		result["status"] = "FAILED"
		result["message"] = "账单处理失败，请检查商户配置、账单状态并重试"
		_ = s.paymentAlert(ctx, "FINANCE_RECONCILIATION_FAILED", date+":"+kind, "退款或资金账单处理失败，请检查配置或稍后重试")
		log.Printf("finance reconciliation kind=%s date=%s failed: %v", kind, date, err)
	} else {
		result["status"] = "MATCHED"
		if kind != "REFUND" {
			result["status"] = "COMPLETED"
		}
		if result["differences"].(int) > 0 {
			result["status"] = "DIFFERENCES"
		}
		result["checked"] = len(rows)
		result["totals"] = financeTotals(rows, kind)
		result["details"] = details
		result["emptyBill"] = raw == nil
	}
	result["finishedAt"] = nowString()
	_, err = s.store.db.ExecContext(ctx, "UPDATE payment_finance_runs SET status=?,payload=?,updated_at=NOW(3) WHERE bill_date=? AND kind=?", result["status"], jsonValue(result), date, kind)
	return result, err
}
func (s *Server) financeRuns(ctx context.Context) ([]M, error) {
	rows, err := s.store.db.QueryContext(ctx, "SELECT payload FROM payment_finance_runs ORDER BY bill_date DESC,kind LIMIT 124")
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []M{}
	for rows.Next() {
		var raw []byte
		if err = rows.Scan(&raw); err != nil {
			return nil, err
		}
		var m M
		if err = json.Unmarshal(raw, &m); err != nil {
			return nil, err
		}
		out = append(out, m)
	}
	return out, rows.Err()
}
