package main

import (
	"context"
	"encoding/json"
	"errors"
	"time"
)

// 线索来源渠道统计（新增）：按 channel 分组统计线索数、成单数（WON）、成单率、商机金额。

type leadChannelStat struct {
	Channel          string `json:"channel"`
	Count            int    `json:"count"`
	Won              int    `json:"won"`
	OpportunityAmount int64 `json:"opportunityAmount"`
}

func (s *Server) leadChannelStats(ctx context.Context, raw json.RawMessage) (any, error) {
	var d struct {
		Start string `json:"start"`
		End   string `json:"end"`
	}
	if err := decode(raw, &d); err != nil {
		return nil, err
	}
	if d.Start == "" || d.End == "" {
		now := time.Now()
		d.End = now.Format("2006-01-02")
		d.Start = now.AddDate(0, 0, -29).Format("2006-01-02")
	}
	if _, err := time.Parse("2006-01-02", d.Start); err != nil {
		return nil, errors.New("开始日期格式无效")
	}
	if _, err := time.Parse("2006-01-02", d.End); err != nil {
		return nil, errors.New("结束日期格式无效")
	}
	if d.Start > d.End {
		return nil, errors.New("开始日期不能晚于结束日期")
	}
	rows, err := s.store.db.QueryContext(ctx, `SELECT COALESCE(NULLIF(TRIM(JSON_UNQUOTE(JSON_EXTRACT(payload,'$.channel'))),''),'未填写'),COUNT(*),
		SUM(CASE WHEN status='WON' THEN 1 ELSE 0 END),
		COALESCE(SUM(CAST(JSON_UNQUOTE(JSON_EXTRACT(payload,'$.opportunityAmount')) AS SIGNED)),0)
		FROM partner_leads WHERE created_at>=? AND created_at<? GROUP BY COALESCE(NULLIF(TRIM(JSON_UNQUOTE(JSON_EXTRACT(payload,'$.channel'))),''),'未填写') ORDER BY COUNT(*) DESC`,
		d.Start+" 00:00:00", d.End+" 23:59:59.999")
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := []leadChannelStat{}
	total := leadChannelStat{Channel: "合计"}
	for rows.Next() {
		var st leadChannelStat
		if err = rows.Scan(&st.Channel, &st.Count, &st.Won, &st.OpportunityAmount); err != nil {
			return nil, err
		}
		items = append(items, st)
		total.Count += st.Count
		total.Won += st.Won
		total.OpportunityAmount += st.OpportunityAmount
	}
	if err = rows.Err(); err != nil {
		return nil, err
	}
	return M{"items": items, "total": total, "startDate": d.Start, "endDate": d.End}, nil
}
