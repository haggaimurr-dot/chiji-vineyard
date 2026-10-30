package main

import (
	"context"
	"encoding/csv"
	"encoding/json"
	"errors"
	"strconv"
	"strings"
)

// 经营概览报表导出（新增）：与页面同一数据源，导出 CSV（Excel 可直接打开）。
// 含汇总、环比、每日趋势三节。
func (s *Server) dashboardExport(ctx context.Context, actor Claims, raw json.RawMessage) (any, error) {
	data, err := s.dashboard(ctx, raw)
	if err != nil {
		return nil, err
	}
	m, ok := data.(M)
	if !ok {
		return nil, errors.New("报表数据异常")
	}
	num := func(v any) int64 {
		switch n := v.(type) {
		case int64:
			return n
		case int:
			return int64(n)
		case float64:
			return int64(n)
		}
		return 0
	}
	str := func(v any) string {
		if s, ok := v.(string); ok {
			return s
		}
		return ""
	}
	itoa := func(v any) string { return strconv.FormatInt(num(v), 10) }
	var content strings.Builder
	content.WriteRune('\ufeff')
	w := csv.NewWriter(&content)
	w.UseCRLF = true
	write := func(row ...string) error { return w.Write(row) }
	// 汇总
	if err := write("经营概览报表", str(m["startDate"])+" 至 "+str(m["endDate"])); err != nil {
		return nil, err
	}
	if err := write("指标", "数值"); err != nil {
		return nil, err
	}
	for _, row := range [][]string{
		{"净收入（元）", moneyCSV(int(num(m["revenue"])))},
		{"毛收入（元）", moneyCSV(int(num(m["grossRevenue"])))},
		{"退款金额（元）", moneyCSV(int(num(m["refundedAmount"])))},
		{"支付订单数", itoa(m["paidOrders"])},
		{"购买人数", itoa(m["buyers"])},
		{"复购人数", itoa(m["repeatBuyers"])},
		{"复购率", str(m["repurchaseRate"])},
		{"待发货订单", itoa(m["pendingShipment"])},
		{"待处理退款", itoa(m["pendingRefund"])},
		{"新增线索", itoa(m["newLeads"])},
	} {
		if err := write(row...); err != nil {
			return nil, err
		}
	}
	// 环比
	if prev, ok := m["previous"].(M); ok {
		if err := write(""); err != nil {
			return nil, err
		}
		if err := write("环比（上一周期）", ""); err != nil {
			return nil, err
		}
		for _, row := range [][]string{
			{"净收入（元）", moneyCSV(int(num(prev["netRevenue"])))},
			{"毛收入（元）", moneyCSV(int(num(prev["grossRevenue"])))},
			{"退款金额（元）", moneyCSV(int(num(prev["refundedAmount"])))},
			{"支付订单数", itoa(prev["paidOrders"])},
		} {
			if err := write(row...); err != nil {
				return nil, err
			}
		}
	}
	// 每日趋势
	if err := write(""); err != nil {
		return nil, err
	}
	if err := write("每日趋势", ""); err != nil {
		return nil, err
	}
	if err := write("日期", "支付订单数", "毛收入（元）", "退款金额（元）", "净收入（元）"); err != nil {
		return nil, err
	}
	if trend, ok := m["trend"].([]dashboardDay); ok {
		for _, d := range trend {
			if err := write(d.Date, itoa(d.PaidOrders), moneyCSV(int(d.GrossRevenue)), moneyCSV(int(d.RefundedAmount)), moneyCSV(int(d.NetRevenue))); err != nil {
				return nil, err
			}
		}
	}
	w.Flush()
	if err := w.Error(); err != nil {
		return nil, err
	}
	if _, err = s.store.db.ExecContext(ctx, "INSERT INTO audit_logs(admin_id,username,action,target_id,detail) VALUES(?,?,?,?,?)", actor.Subject, actor.Username, "DASHBOARD_EXPORT", "", jsonValue(M{"startDate": m["startDate"], "endDate": m["endDate"]})); err != nil {
		return nil, errors.New("导出日志保存失败，请稍后重试")
	}
	filename := "经营概览_" + str(m["startDate"]) + "_" + str(m["endDate"]) + ".csv"
	return M{"filename": filename, "content": content.String()}, nil
}

