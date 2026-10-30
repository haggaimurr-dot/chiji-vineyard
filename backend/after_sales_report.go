package main

import (
	"context"
	"database/sql"
	"encoding/csv"
	"encoding/json"
	"errors"
	"strconv"
	"strings"
	"time"
)

// 售后原因分析报表（新增）：退款原因 × 工单类别的分布统计，支持 CSV 导出。
type refundReasonStat struct {
	Reason string `json:"reason"`
	Count  int    `json:"count"`
	Amount int    `json:"amount"`
}

type ticketCategoryStat struct {
	Category string `json:"category"`
	Count    int    `json:"count"`
	Open     int    `json:"open"`
}

type reasonReport struct {
	StartDate    string             `json:"startDate"`
	EndDate      string             `json:"endDate"`
	RefundTotal  int                `json:"refundTotal"`
	RefundAmount int                `json:"refundAmount"`
	TicketTotal  int                `json:"ticketTotal"`
	RefundReasons []refundReasonStat `json:"refundReasons"`
	TicketCategories []ticketCategoryStat `json:"ticketCategories"`
}

func reasonReportRange(raw json.RawMessage) (start, end string, err error) {
	var d struct {
		Start string `json:"start"`
		End   string `json:"end"`
	}
	if err = decode(raw, &d); err != nil {
		return "", "", err
	}
	if d.Start == "" || d.End == "" {
		now := time.Now()
		d.End = now.Format("2006-01-02")
		d.Start = now.AddDate(0, 0, -29).Format("2006-01-02")
	}
	if _, err = time.Parse("2006-01-02", d.Start); err != nil {
		return "", "", errors.New("开始日期格式无效")
	}
	if _, err = time.Parse("2006-01-02", d.End); err != nil {
		return "", "", errors.New("结束日期格式无效")
	}
	if d.Start > d.End {
		return "", "", errors.New("开始日期不能晚于结束日期")
	}
	return d.Start, d.End, nil
}

func (s *Server) aftersalesReasonReport(ctx context.Context, raw json.RawMessage) (any, error) {
	start, end, err := reasonReportRange(raw)
	if err != nil {
		return nil, err
	}
	rep := reasonReport{StartDate: start, EndDate: end, RefundReasons: []refundReasonStat{}, TicketCategories: []ticketCategoryStat{}}
	// 退款原因分布（含已拒绝：用于分析申请原因构成）。
	rows, err := s.store.db.QueryContext(ctx, `SELECT reason,COUNT(*),COALESCE(SUM(amount),0) FROM refunds
		WHERE created_at>=? AND created_at<? ORDER BY COUNT(*) DESC`, start+" 00:00:00", end+" 23:59:59.999")
	if err != nil {
		return nil, err
	}
	func() {
		defer rows.Close()
		for rows.Next() {
			var st refundReasonStat
			if err := rows.Scan(&st.Reason, &st.Count, &st.Amount); err != nil {
				return
			}
			if st.Reason == "" {
				st.Reason = "未填写"
			}
			rep.RefundReasons = append(rep.RefundReasons, st)
			rep.RefundTotal += st.Count
			rep.RefundAmount += st.Amount
		}
	}()
	if err = rows.Err(); err != nil {
		return nil, err
	}
	// 工单类别分布。
	rows, err = s.store.db.QueryContext(ctx, `SELECT JSON_UNQUOTE(JSON_EXTRACT(payload,'$.category')),COUNT(*),
		SUM(CASE WHEN status IN ('OPEN','PROCESSING') THEN 1 ELSE 0 END)
		FROM after_sales_tickets WHERE created_at>=? AND created_at<? GROUP BY JSON_EXTRACT(payload,'$.category') ORDER BY COUNT(*) DESC`,
		start+" 00:00:00", end+" 23:59:59.999")
	if err != nil {
		return nil, err
	}
	func() {
		defer rows.Close()
		for rows.Next() {
			var st ticketCategoryStat
			var cat sql.NullString
			if err := rows.Scan(&cat, &st.Count, &st.Open); err != nil {
				return
			}
			st.Category = cat.String
			if st.Category == "" {
				st.Category = "OTHER"
			}
			rep.TicketCategories = append(rep.TicketCategories, st)
			rep.TicketTotal += st.Count
		}
	}()
	if err = rows.Err(); err != nil {
		return nil, err
	}
	return rep, nil
}

func (s *Server) aftersalesReasonReportExport(ctx context.Context, actor Claims, raw json.RawMessage) (any, error) {
	data, err := s.aftersalesReasonReport(ctx, raw)
	if err != nil {
		return nil, err
	}
	rep, ok := data.(reasonReport)
	if !ok {
		return nil, errors.New("报表数据异常")
	}
	var content strings.Builder
	content.WriteRune('\ufeff')
	w := csv.NewWriter(&content)
	w.UseCRLF = true
	write := func(row ...string) error { return w.Write(row) }
	if err := write("售后原因分析报表", rep.StartDate+" 至 "+rep.EndDate); err != nil {
		return nil, err
	}
	if err := write("退款总数", strconv.Itoa(rep.RefundTotal), "退款总金额（元）", moneyCSV(rep.RefundAmount), "工单总数", strconv.Itoa(rep.TicketTotal)); err != nil {
		return nil, err
	}
	if err := write(""); err != nil {
		return nil, err
	}
	if err := write("退款原因", "笔数", "金额（元）"); err != nil {
		return nil, err
	}
	for _, r := range rep.RefundReasons {
		if err := write(r.Reason, strconv.Itoa(r.Count), moneyCSV(r.Amount)); err != nil {
			return nil, err
		}
	}
	if err := write(""); err != nil {
		return nil, err
	}
	if err := write("工单类别", "数量", "未关闭"); err != nil {
		return nil, err
	}
	for _, t := range rep.TicketCategories {
		if err := write(t.Category, strconv.Itoa(t.Count), strconv.Itoa(t.Open)); err != nil {
			return nil, err
		}
	}
	w.Flush()
	if err := w.Error(); err != nil {
		return nil, err
	}
	s.store.audit(ctx, actor, "AFTERSALES_REASON_REPORT_EXPORT", "", M{"start": rep.StartDate, "end": rep.EndDate})
	return M{"filename": "售后原因分析_" + rep.StartDate + "_" + rep.EndDate + ".csv", "content": content.String()}, nil
}
