package main

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"regexp"
	"strings"
	"time"
)

var articleBeijing = time.FixedZone("Asia/Shanghai", 8*60*60)
var articleReadEventRE = regexp.MustCompile(`^[A-Za-z0-9_-]{1,128}$`)

type ArticleVersion struct {
	Version   int     `json:"version"`
	CreatedAt string  `json:"createdAt"`
	CreatedBy string  `json:"createdBy"`
	Reason    string  `json:"reason"`
	Article   Article `json:"article"`
}

type ArticleReadDay struct {
	Date    string `json:"date"`
	Views   int    `json:"views"`
	Readers int    `json:"readers"`
}

type ArticleStats struct {
	TotalViews   int              `json:"totalViews"`
	TotalReaders int              `json:"totalReaders"`
	TodayViews   int              `json:"todayViews"`
	TodayReaders int              `json:"todayReaders"`
	Days         []ArticleReadDay `json:"days"`
}

func effectiveArticleVersion(a Article) int {
	if a.Version < 1 {
		return 1
	}
	return a.Version
}

func validateArticleRevision(a Article, now time.Time) (Article, error) {
	if a.Type != "article" && a.Type != "video" {
		return a, errors.New("内容类型无效")
	}
	if a.Status != "DRAFT" && a.Status != "PUBLISHED" && a.Status != "SCHEDULED" {
		return a, errors.New("文章状态无效")
	}
	if len(a.Tags) > 20 {
		return a, errors.New("每篇内容最多20个标签")
	}
	tags := []string{}
	for _, tag := range a.Tags {
		tag = strings.TrimSpace(tag)
		if tag == "" || len([]rune(tag)) > 80 {
			return a, errors.New("标签不能为空且最多80字")
		}
		found := false
		for _, existing := range tags {
			if existing == tag {
				found = true
			}
		}
		if !found {
			tags = append(tags, tag)
		}
	}
	a.Tags = tags
	var err error
	a, err = validateArticle(a)
	if err != nil {
		return a, err
	}
	if len(a.Content) > 100 {
		return a, errors.New("正文最多 100 段")
	}
	count := 0
	for _, paragraph := range a.Content {
		count += len([]rune(paragraph))
	}
	if count > 50000 {
		return a, errors.New("正文最多 50000 字")
	}
	if a.Status == "SCHEDULED" {
		due, err := time.Parse(time.RFC3339Nano, a.ScheduledAt)
		if err != nil || !due.After(now) {
			return a, errors.New("定时发布时间必须是带时区的未来时间")
		}
		a.ScheduledAt = due.UTC().Format(time.RFC3339Nano)
	} else {
		a.ScheduledAt = ""
	}
	if a.PublishedAt == "" {
		a.PublishedAt = now.UTC().Format(time.RFC3339Nano)
	} else {
		published, err := time.Parse(time.RFC3339Nano, a.PublishedAt)
		if err != nil {
			published, err = time.ParseInLocation("2006-01-02", a.PublishedAt, articleBeijing)
		}
		if err != nil {
			return a, errors.New("发布日期无效")
		}
		a.PublishedAt = published.UTC().Format(time.RFC3339Nano)
	}
	// Counters come exclusively from article_reads, never from editor input.
	a.ReadCount, a.ReaderCount = 0, 0
	a.Favorite, a.LastReadAt = false, ""
	return a, nil
}

func scanStoredArticle(row *sql.Row) (*Article, time.Time, error) {
	var raw []byte
	var updated time.Time
	var status string
	err := row.Scan(&raw, &updated, &status)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, updated, nil
	}
	if err != nil {
		return nil, updated, err
	}
	var a Article
	if err = json.Unmarshal(raw, &a); err != nil {
		return nil, updated, err
	}
	a.Status = status
	if a.UpdatedAt == "" {
		a.UpdatedAt = updated.UTC().Format(time.RFC3339Nano)
	}
	a.ReadCount, a.ReaderCount = 0, 0
	a.Favorite, a.LastReadAt = false, ""
	return &a, updated, nil
}

func lockedArticle(ctx context.Context, tx *sql.Tx, id string) (*Article, error) {
	a, _, err := scanStoredArticle(tx.QueryRowContext(ctx, "SELECT payload,updated_at,status FROM articles WHERE id=? FOR UPDATE", id))
	return a, err
}

func insertArticleVersion(ctx context.Context, tx *sql.Tx, a Article, actor, reason string, at time.Time) error {
	a.ReadCount, a.ReaderCount = 0, 0
	a.Favorite, a.LastReadAt = false, ""
	_, err := tx.ExecContext(ctx, "INSERT INTO article_versions(article_id,version,payload,created_by,reason,created_at) VALUES(?,?,?,?,?,?)", a.ID, a.Version, jsonValue(a), actor, reason, at.UTC())
	return err
}

// Legacy payloads have no version. Preserve their complete baseline before the
// first edit or restore; repeat attempts cannot change an existing snapshot.
func ensureLegacyArticleVersion(ctx context.Context, tx *sql.Tx, a Article) error {
	if a.Version > 0 {
		return nil
	}
	a.Version = 1
	a.ReadCount, a.ReaderCount = 0, 0
	a.Favorite, a.LastReadAt = false, ""
	_, err := tx.ExecContext(ctx, "INSERT IGNORE INTO article_versions(article_id,version,payload,created_by,reason,created_at) VALUES(?,?,?,?,?,?)", a.ID, a.Version, jsonValue(a), "legacy", "legacy", normalizeDate(a.UpdatedAt).UTC())
	return err
}

func persistArticle(ctx context.Context, tx *sql.Tx, a Article, existing bool) error {
	var err error
	if existing {
		_, err = tx.ExecContext(ctx, "UPDATE articles SET status=?,stage=?,variety=?,published_at=?,payload=? WHERE id=?", a.Status, a.Stage, a.Variety, normalizeDate(a.PublishedAt).UTC(), jsonValue(a), a.ID)
	} else {
		_, err = tx.ExecContext(ctx, "INSERT INTO articles(id,status,stage,variety,published_at,payload) VALUES(?,?,?,?,?,?)", a.ID, a.Status, a.Stage, a.Variety, normalizeDate(a.PublishedAt).UTC(), jsonValue(a))
	}
	if err != nil {
		return err
	}
	if a.Status == "SCHEDULED" {
		_, err = tx.ExecContext(ctx, "INSERT INTO article_schedules(article_id,scheduled_at) VALUES(?,?) ON DUPLICATE KEY UPDATE scheduled_at=VALUES(scheduled_at)", a.ID, normalizeDate(a.ScheduledAt).UTC())
	} else {
		_, err = tx.ExecContext(ctx, "DELETE FROM article_schedules WHERE article_id=?", a.ID)
	}
	return err
}

func articleCounters(ctx context.Context, q interface {
	QueryRowContext(context.Context, string, ...any) *sql.Row
}, a *Article) error {
	return q.QueryRowContext(ctx, "SELECT COUNT(*),COUNT(DISTINCT reader_hash) FROM article_reads WHERE article_id=?", a.ID).Scan(&a.ReadCount, &a.ReaderCount)
}

func (s *Server) saveArticle(ctx context.Context, actor Claims, raw json.RawMessage) (any, error) {
	var d struct {
		Article Article `json:"article"`
	}
	if err := decode(raw, &d); err != nil {
		return nil, err
	}
	now := time.Now().UTC()
	a, err := validateArticleRevision(d.Article, now)
	if err != nil {
		return nil, err
	}
	if a.ID == "" {
		a.ID = randomID("ART")
	}
	if len(a.ID) > 80 || strings.TrimSpace(a.ID) != a.ID {
		return nil, errors.New("文章编号无效")
	}
	if err = s.store.publishDueArticles(ctx, now); err != nil {
		return nil, err
	}
	tx, err := s.store.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	current, err := lockedArticle(ctx, tx, a.ID)
	if err != nil {
		return nil, err
	}
	if err = applyArticleReview(&a, current); err != nil {
		return nil, err
	}
	reason := "create"
	a.Version = 1
	if current != nil {
		if d.Article.Version != effectiveArticleVersion(*current) {
			return nil, errors.New("内容版本已变化，请刷新后重试")
		}
		if err = ensureLegacyArticleVersion(ctx, tx, *current); err != nil {
			return nil, err
		}
		a.Version, reason = effectiveArticleVersion(*current)+1, "save"
	}
	a.UpdatedAt = now.Format(time.RFC3339Nano)
	if err = persistArticle(ctx, tx, a, current != nil); err != nil {
		return nil, err
	}
	if err = insertArticleVersion(ctx, tx, a, actor.Subject, reason, now); err != nil {
		return nil, err
	}
	if err = articleCounters(ctx, tx, &a); err != nil {
		return nil, err
	}
	if err = tx.Commit(); err != nil {
		return nil, err
	}
	s.store.audit(ctx, actor, "ARTICLE_SAVE", a.ID, M{"title": a.Title, "version": a.Version, "status": a.Status, "scheduledAt": a.ScheduledAt})
	return a, nil
}

func (s *Store) articleVersions(ctx context.Context, id string) ([]ArticleVersion, error) {
	a, err := s.article(ctx, id, true)
	if err != nil {
		return nil, err
	}
	if a == nil {
		return nil, errors.New("文章不存在")
	}
	rows, err := s.db.QueryContext(ctx, "SELECT version,payload,created_by,reason,created_at FROM article_versions WHERE article_id=? ORDER BY version DESC", id)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	versions := []ArticleVersion{}
	for rows.Next() {
		var v ArticleVersion
		var raw []byte
		var created time.Time
		if err = rows.Scan(&v.Version, &raw, &v.CreatedBy, &v.Reason, &created); err != nil {
			return nil, err
		}
		if err = json.Unmarshal(raw, &v.Article); err != nil {
			return nil, err
		}
		v.CreatedAt = created.UTC().Format(time.RFC3339Nano)
		versions = append(versions, v)
	}
	if err = rows.Err(); err != nil {
		return nil, err
	}
	if len(versions) == 0 {
		a.ReadCount, a.ReaderCount = 0, 0
		a.Favorite, a.LastReadAt = false, ""
		versions = append(versions, ArticleVersion{Version: 1, Article: *a, CreatedBy: "legacy", Reason: "legacy", CreatedAt: a.UpdatedAt})
	}
	return versions, nil
}

func (s *Server) restoreArticle(ctx context.Context, actor Claims, raw json.RawMessage) (any, error) {
	var d struct {
		ArticleID       string `json:"articleId"`
		Version         int    `json:"version"`
		ExpectedVersion int    `json:"expectedVersion"`
	}
	if err := decode(raw, &d); err != nil {
		return nil, err
	}
	if d.ArticleID == "" || d.Version < 1 || d.ExpectedVersion < 1 {
		return nil, errors.New("文章或版本无效")
	}
	now := time.Now().UTC()
	if err := s.store.publishDueArticles(ctx, now); err != nil {
		return nil, err
	}
	tx, err := s.store.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	current, err := lockedArticle(ctx, tx, d.ArticleID)
	if err != nil {
		return nil, err
	}
	if current == nil {
		return nil, errors.New("文章不存在")
	}
	if d.ExpectedVersion != effectiveArticleVersion(*current) {
		return nil, errors.New("内容版本已变化，请刷新后重试")
	}
	if err = ensureLegacyArticleVersion(ctx, tx, *current); err != nil {
		return nil, err
	}
	var snapshot []byte
	if err = tx.QueryRowContext(ctx, "SELECT payload FROM article_versions WHERE article_id=? AND version=?", d.ArticleID, d.Version).Scan(&snapshot); errors.Is(err, sql.ErrNoRows) {
		return nil, errors.New("内容版本不存在")
	} else if err != nil {
		return nil, err
	}
	var restored Article
	if err = json.Unmarshal(snapshot, &restored); err != nil {
		return nil, err
	}
	restored.ID, restored.Version = current.ID, effectiveArticleVersion(*current)+1
	restored.Status, restored.ScheduledAt = "DRAFT", ""
	if current.ReviewStatus != "" || restored.ReviewStatus != "" {
		restored.ReviewStatus = "UNSUBMITTED"
		restored.ReviewNote = ""
		restored.ReviewedBy = ""
		restored.ReviewedAt = ""
	}
	// Historical snapshots retain the original source, but a newly restored
	// draft must pass the current URL and HTML safety boundary before saving.
	restored, err = validateArticleRevision(restored, now)
	if err != nil {
		return nil, err
	}
	restored.UpdatedAt = now.Format(time.RFC3339Nano)
	restored.ReadCount, restored.ReaderCount = 0, 0
	if err = persistArticle(ctx, tx, restored, true); err != nil {
		return nil, err
	}
	if err = insertArticleVersion(ctx, tx, restored, actor.Subject, fmt.Sprintf("restore:%d", d.Version), now); err != nil {
		return nil, err
	}
	if err = articleCounters(ctx, tx, &restored); err != nil {
		return nil, err
	}
	if err = tx.Commit(); err != nil {
		return nil, err
	}
	s.store.audit(ctx, actor, "ARTICLE_RESTORE", restored.ID, M{"fromVersion": d.Version, "version": restored.Version})
	return restored, nil
}

// The worker and request paths use the same locked transition. A second
// instance cannot publish an already-published article or overwrite an edit.
func (s *Store) publishDueArticle(ctx context.Context, id string, now time.Time) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	a, err := lockedArticle(ctx, tx, id)
	if err != nil {
		return err
	}
	var due time.Time
	err = tx.QueryRowContext(ctx, "SELECT scheduled_at FROM article_schedules WHERE article_id=? FOR UPDATE", id).Scan(&due)
	if errors.Is(err, sql.ErrNoRows) {
		return nil
	}
	if err != nil {
		return err
	}
	if due.After(now) {
		return nil
	}
	if a != nil && a.Status == "SCHEDULED" {
		if err = ensureLegacyArticleVersion(ctx, tx, *a); err != nil {
			return err
		}
		a.Version = effectiveArticleVersion(*a) + 1
		a.Status, a.ScheduledAt = "PUBLISHED", ""
		a.PublishedAt, a.UpdatedAt = due.UTC().Format(time.RFC3339Nano), now.UTC().Format(time.RFC3339Nano)
		if err = persistArticle(ctx, tx, *a, true); err != nil {
			return err
		}
		if err = insertArticleVersion(ctx, tx, *a, "system", "scheduled_publish", now); err != nil {
			return err
		}
	} else if _, err = tx.ExecContext(ctx, "DELETE FROM article_schedules WHERE article_id=?", id); err != nil {
		return err
	}
	return tx.Commit()
}

func (s *Store) publishDueArticles(ctx context.Context, now time.Time) error {
	for {
		rows, err := s.db.QueryContext(ctx, "SELECT article_id FROM article_schedules WHERE scheduled_at<=? ORDER BY scheduled_at,article_id LIMIT 100", now.UTC())
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
			if err = s.publishDueArticle(ctx, id, now); err != nil {
				return err
			}
		}
		if len(ids) < 100 {
			return nil
		}
	}
}

func (s *Store) runArticlePublisher(ctx context.Context) {
	ticker := time.NewTicker(30 * time.Second)
	defer ticker.Stop()
	for {
		job, cancel := context.WithTimeout(ctx, 25*time.Second)
		if err := s.publishDueArticles(job, time.Now().UTC()); err != nil && ctx.Err() == nil {
			log.Printf("article scheduled publication: %v", err)
		}
		cancel()
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
	}
}

func articleReaderHash(openid string) string {
	sum := sha256.Sum256([]byte("article-reader:v1\x00" + openid))
	return hex.EncodeToString(sum[:])
}

func (s *Store) recordArticleRead(ctx context.Context, openid string, raw json.RawMessage) (any, error) {
	var d struct {
		ArticleID string `json:"articleId"`
		EventID   string `json:"eventId"`
	}
	if err := decode(raw, &d); err != nil {
		return nil, err
	}
	if openid == "" || d.ArticleID == "" || len(d.ArticleID) > 80 || !articleReadEventRE.MatchString(d.EventID) {
		return nil, errors.New("阅读事件无效")
	}
	now := time.Now().UTC()
	if err := s.publishDueArticles(ctx, now); err != nil {
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
		return nil, errors.New("文章尚未发布或已下架")
	}
	result, err := tx.ExecContext(ctx, "INSERT IGNORE INTO article_reads(article_id,reader_hash,event_id,read_date,created_at) VALUES(?,?,?,?,?)", a.ID, articleReaderHash(openid), d.EventID, now.In(articleBeijing).Format("2006-01-02"), now)
	if err != nil {
		return nil, err
	}
	count, err := result.RowsAffected()
	if err != nil {
		return nil, err
	}
	if err = tx.Commit(); err != nil {
		return nil, err
	}
	return M{"recorded": count == 1}, nil
}

func emptyArticleStats(now time.Time) ArticleStats {
	today := now.In(articleBeijing)
	stats := ArticleStats{Days: make([]ArticleReadDay, 14)}
	for i := range stats.Days {
		stats.Days[i].Date = today.AddDate(0, 0, i-13).Format("2006-01-02")
	}
	return stats
}

func (s *Store) articleStats(ctx context.Context, id string) (ArticleStats, error) {
	stats := emptyArticleStats(time.Now())
	var exists int
	if err := s.db.QueryRowContext(ctx, "SELECT COUNT(*) FROM articles WHERE id=?", id).Scan(&exists); err != nil {
		return stats, err
	}
	if exists == 0 {
		return stats, errors.New("文章不存在")
	}
	today := stats.Days[len(stats.Days)-1].Date
	err := s.db.QueryRowContext(ctx, "SELECT COUNT(*),COUNT(DISTINCT reader_hash),COALESCE(SUM(read_date=?),0),COUNT(DISTINCT CASE WHEN read_date=? THEN reader_hash END) FROM article_reads WHERE article_id=?", today, today, id).Scan(&stats.TotalViews, &stats.TotalReaders, &stats.TodayViews, &stats.TodayReaders)
	if err != nil {
		return stats, err
	}
	rows, err := s.db.QueryContext(ctx, "SELECT DATE_FORMAT(read_date,'%Y-%m-%d'),COUNT(*),COUNT(DISTINCT reader_hash) FROM article_reads WHERE article_id=? AND read_date>=? AND read_date<=? GROUP BY read_date ORDER BY read_date", id, stats.Days[0].Date, today)
	if err != nil {
		return stats, err
	}
	defer rows.Close()
	byDate := map[string]int{}
	for i, day := range stats.Days {
		byDate[day.Date] = i
	}
	for rows.Next() {
		var day ArticleReadDay
		if err = rows.Scan(&day.Date, &day.Views, &day.Readers); err != nil {
			return stats, err
		}
		if i, ok := byDate[day.Date]; ok {
			stats.Days[i] = day
		}
	}
	return stats, rows.Err()
}
