package main

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"time"
)

// 内容数据分析（新增）：阅读 / 收藏 / 转化（末次点击口径：从内容页商品入口下单且已付款）。
// 归因口径：订单 payload.sourceArticleId = 文章 ID，且 payment_status != 'UNPAID'。

type contentAnalyticRow struct {
	ArticleID         string `json:"articleId"`
	Title             string `json:"title"`
	Views             int    `json:"views"`
	Readers           int    `json:"readers"`
	Favorites         int    `json:"favorites"`
	Conversions       int    `json:"conversions"`
	ConversionAmount  int    `json:"conversionAmount"`
}

func (s *Server) contentAnalytics(ctx context.Context, raw json.RawMessage) (any, error) {
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
	rows, err := s.store.db.QueryContext(ctx, `SELECT a.id,JSON_UNQUOTE(JSON_EXTRACT(a.payload,'$.title')),
		(SELECT COUNT(*) FROM article_reads r WHERE r.article_id=a.id AND r.read_date>=? AND r.read_date<=?),
		(SELECT COUNT(DISTINCT reader_hash) FROM article_reads r WHERE r.article_id=a.id AND r.read_date>=? AND r.read_date<=?),
		(SELECT COUNT(*) FROM article_favorites f WHERE f.article_id=a.id AND DATE(f.created_at)>=? AND DATE(f.created_at)<=?),
		(SELECT COUNT(*) FROM orders o WHERE JSON_UNQUOTE(JSON_EXTRACT(o.payload,'$.sourceArticleId'))=a.id AND o.payment_status<>'UNPAID' AND DATE(o.created_at)>=? AND DATE(o.created_at)<=?),
		(SELECT COALESCE(SUM(o.total),0) FROM orders o WHERE JSON_UNQUOTE(JSON_EXTRACT(o.payload,'$.sourceArticleId'))=a.id AND o.payment_status<>'UNPAID' AND DATE(o.created_at)>=? AND DATE(o.created_at)<=?)
		FROM articles a WHERE a.status='PUBLISHED' ORDER BY a.id`,
		d.Start, d.End, d.Start, d.End, d.Start, d.End, d.Start, d.End, d.Start, d.End)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := []contentAnalyticRow{}
	totals := contentAnalyticRow{ArticleID: "TOTAL", Title: "合计"}
	for rows.Next() {
		var r contentAnalyticRow
		var title sql.NullString
		if err = rows.Scan(&r.ArticleID, &title, &r.Views, &r.Readers, &r.Favorites, &r.Conversions, &r.ConversionAmount); err != nil {
			return nil, err
		}
		r.Title = title.String
		items = append(items, r)
		totals.Views += r.Views
		totals.Readers += r.Readers
		totals.Favorites += r.Favorites
		totals.Conversions += r.Conversions
		totals.ConversionAmount += r.ConversionAmount
	}
	if err = rows.Err(); err != nil {
		return nil, err
	}
	return M{"items": items, "total": totals, "startDate": d.Start, "endDate": d.End}, nil
}
