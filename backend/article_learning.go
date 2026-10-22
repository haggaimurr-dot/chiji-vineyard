package main

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"time"
	"unicode/utf8"
)

type articleSearchOptions struct {
	Keyword string `json:"keyword"`
	Stage   string `json:"stage"`
	Variety string `json:"variety"`
	View    string `json:"view"`
	Offset  int    `json:"offset"`
	Limit   int    `json:"limit"`
}

func articleSearchQuery(openid string, d articleSearchOptions) (string, []any, error) {
	d.Keyword = strings.TrimSpace(d.Keyword)
	if utf8.RuneCountInString(d.Keyword) > 100 || d.Offset < 0 || d.Offset > 100000 || d.Limit < 1 || d.Limit > 50 {
		return "", nil, errors.New("课堂查询参数无效")
	}
	if d.View != "" && d.View != "all" && d.View != "favorite" && d.View != "history" {
		return "", nil, errors.New("课堂列表类型无效")
	}
	query := "SELECT a.payload,a.status,a.updated_at,COALESCE(f.article_id,''),h.last_read_at,COALESCE(r.views,0),COALESCE(r.readers,0) FROM articles a LEFT JOIN article_favorites f ON f.article_id=a.id AND f.openid=? LEFT JOIN (SELECT article_id,MAX(created_at) AS last_read_at FROM article_reads WHERE reader_hash=? GROUP BY article_id) h ON h.article_id=a.id LEFT JOIN (SELECT article_id,COUNT(*) AS views,COUNT(DISTINCT reader_hash) AS readers FROM article_reads GROUP BY article_id) r ON r.article_id=a.id WHERE a.status='PUBLISHED'"
	args := []any{openid, articleReaderHash(openid)}
	if d.View == "favorite" {
		query += " AND f.article_id IS NOT NULL"
	}
	if d.View == "history" {
		query += " AND h.article_id IS NOT NULL"
	}
	if d.Stage != "" && d.Stage != "全部" {
		query += " AND a.stage=?"
		args = append(args, d.Stage)
	}
	if d.Variety != "" && d.Variety != "全部" {
		query += " AND a.variety=?"
		args = append(args, d.Variety)
	}
	if d.Keyword != "" {
		// LOCATE treats user input literally, including SQL wildcard characters.
		query += " AND LOCATE(LOWER(?),LOWER(CONCAT_WS(' ',JSON_UNQUOTE(JSON_EXTRACT(a.payload,'$.title')),JSON_UNQUOTE(JSON_EXTRACT(a.payload,'$.summary')),a.stage,a.variety)))>0"
		args = append(args, d.Keyword)
	}
	switch d.View {
	case "history":
		query += " ORDER BY h.last_read_at DESC,a.id"
	case "favorite":
		query += " ORDER BY f.created_at DESC,a.id"
	default:
		query += " ORDER BY a.published_at DESC,a.id"
	}
	query += " LIMIT ? OFFSET ?"
	return query, append(args, d.Limit+1, d.Offset), nil
}

func (s *Store) searchArticles(ctx context.Context, openid string, raw json.RawMessage) (any, error) {
	d := articleSearchOptions{Limit: 20}
	if err := decode(raw, &d); err != nil {
		return nil, err
	}
	query, args, err := articleSearchQuery(openid, d)
	if err != nil {
		return nil, err
	}
	if err = s.publishDueArticles(ctx, time.Now().UTC()); err != nil {
		return nil, err
	}
	rows, err := s.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	articles := []Article{}
	for rows.Next() {
		var a Article
		var payload []byte
		var updated time.Time
		var favorite string
		var lastRead *time.Time
		var views, readers int
		if err = rows.Scan(&payload, &a.Status, &updated, &favorite, &lastRead, &views, &readers); err != nil {
			return nil, err
		}
		status := a.Status
		if err = json.Unmarshal(payload, &a); err != nil {
			return nil, err
		}
		a.Status, a.Version, a.Favorite = status, effectiveArticleVersion(a), favorite != ""
		a.LastReadAt = ""
		if lastRead != nil {
			a.LastReadAt = lastRead.UTC().Format(time.RFC3339Nano)
		}
		if a.UpdatedAt == "" {
			a.UpdatedAt = updated.UTC().Format(time.RFC3339Nano)
		}
		// Cards omit the article body and video source; detail loads these on demand.
		a.Content, a.ContentHTML, a.VideoURL = nil, "", ""
		a.ReadCount, a.ReaderCount = views, readers
		articles = append(articles, a)
	}
	if err = rows.Err(); err != nil {
		return nil, err
	}
	hasMore := len(articles) > d.Limit
	if hasMore {
		articles = articles[:d.Limit]
	}
	return M{"articles": articles, "hasMore": hasMore}, nil
}

func (s *Store) setArticleFavorite(ctx context.Context, openid string, raw json.RawMessage) (any, error) {
	var d struct {
		ArticleID string `json:"articleId"`
		Enabled   *bool  `json:"enabled"`
	}
	if err := decode(raw, &d); err != nil {
		return nil, err
	}
	if d.ArticleID == "" || len(d.ArticleID) > 80 || d.Enabled == nil {
		return nil, errors.New("收藏参数无效")
	}
	if !*d.Enabled {
		_, err := s.db.ExecContext(ctx, "DELETE FROM article_favorites WHERE openid=? AND article_id=?", openid, d.ArticleID)
		return M{"enabled": false}, err
	}
	if err := s.publishDueArticles(ctx, time.Now().UTC()); err != nil {
		return nil, err
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	a, err := lockedArticle(ctx, tx, d.ArticleID)
	if err != nil {
		return nil, err
	}
	if a == nil || a.Status != "PUBLISHED" {
		return nil, errors.New("内容不存在或已下架")
	}
	_, err = tx.ExecContext(ctx, "INSERT INTO article_favorites(openid,article_id) VALUES(?,?) ON DUPLICATE KEY UPDATE article_id=VALUES(article_id)", openid, d.ArticleID)
	if err != nil {
		return nil, err
	}
	return M{"enabled": true}, tx.Commit()
}

func (s *Store) learningArticle(ctx context.Context, openid, id string) (*Article, error) {
	a, err := s.article(ctx, id, false)
	if err != nil || a == nil {
		return a, err
	}
	var count int
	err = s.db.QueryRowContext(ctx, "SELECT COUNT(*) FROM article_favorites WHERE openid=? AND article_id=?", openid, id).Scan(&count)
	a.Favorite = count > 0
	if err != nil {
		return nil, err
	}
	err = s.db.QueryRowContext(ctx, "SELECT COALESCE(MAX(completed),0) FROM classroom_progress WHERE openid=? AND article_id=?", openid, id).Scan(&a.Completed)
	return a, err
}
