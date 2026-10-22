package main

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"encoding/json"
	"errors"
	"io"
	"sort"
	"strings"
	"testing"
	"time"
)

func articleFixture() Article {
	return Article{ID: "ART1", Title: "葡萄", Summary: "种植记录", Cover: "https://example.com/cover.jpg", Stage: "成熟期", Variety: "阳光玫瑰", Type: "article", Content: []string{"原始正文"}, Status: "DRAFT", PublishedAt: "2026-10-01"}
}

func TestArticleScheduleValidation(t *testing.T) {
	now := time.Date(2026, 10, 4, 0, 0, 0, 0, time.UTC)
	a := articleFixture()
	a.Status, a.ScheduledAt = "SCHEDULED", "2026-10-04T09:00:00+08:00"
	a.ReadCount, a.ReaderCount = 99, 88
	got, err := validateArticleRevision(a, now)
	if err != nil || got.ScheduledAt != "2026-10-04T01:00:00Z" || got.PublishedAt != "2026-09-30T16:00:00Z" || got.ReadCount != 0 || got.ReaderCount != 0 {
		t.Fatalf("normalize schedule, Beijing date and server counters: %+v %v", got, err)
	}
	for _, schedule := range []string{"2026-10-04T08:00:00+08:00", "2026-10-03T09:00:00+08:00", "2026-10-04T09:00:00", "garbage", ""} {
		a.ScheduledAt = schedule
		if _, err := validateArticleRevision(a, now); err == nil {
			t.Fatalf("invalid or non-future schedule accepted: %q", schedule)
		}
	}
	a.Status = "DRAFT"
	got, err = validateArticleRevision(a, now)
	if err != nil || got.ScheduledAt != "" {
		t.Fatal("draft retained stale schedule")
	}
	for _, mutate := range []func(*Article){
		func(a *Article) { a.Status = "UNKNOWN" },
		func(a *Article) { a.Type = "unknown" },
		func(a *Article) { a.Type = "video"; a.VideoURL = "" },
		func(a *Article) { a.Content = []string{"  "} },
		func(a *Article) { a.PublishedAt = "2026-02-30" },
		func(a *Article) { a.Content = []string{strings.Repeat("a", 50001)} },
		func(a *Article) {
			a.Content = make([]string, 101)
			for i := range a.Content {
				a.Content[i] = "正文段落"
			}
		},
	} {
		a = articleFixture()
		mutate(&a)
		if _, err := validateArticleRevision(a, now); err == nil {
			t.Fatalf("invalid content accepted: %+v", a)
		}
	}
	a = articleFixture()
	a.Content, a.ContentHTML = nil, `<p><img src="https://example.com/photo.jpg" onerror="evil()"></p><script>bad()</script>`
	got, err = validateArticleRevision(a, now)
	if err != nil || len(got.Content) != 0 || strings.Contains(got.ContentHTML, "onerror") || strings.Contains(got.ContentHTML, "script") || !strings.Contains(got.ContentHTML, "photo.jpg") {
		t.Fatalf("image-only rich content must pass shared sanitizer: %+v %v", got, err)
	}
}

type articleReadFixture struct{ reader, event, day string }
type articleDatabaseState struct {
	article  *Article
	versions map[int]ArticleVersion
	schedule *time.Time
	reads    []articleReadFixture
}
type articleTestDB struct {
	state         articleDatabaseState
	working       *articleDatabaseState
	failSnapshot  bool
	writeUnlocked bool
}
type articleConnector struct{ state *articleTestDB }
type articleDriver struct{ state *articleTestDB }

func (c articleConnector) Connect(context.Context) (driver.Conn, error) { return c.state, nil }
func (c articleConnector) Driver() driver.Driver                        { return articleDriver{c.state} }
func (d articleDriver) Open(string) (driver.Conn, error)                { return d.state, nil }
func (d *articleTestDB) Prepare(string) (driver.Stmt, error) {
	return nil, errors.New("unexpected prepare")
}
func (d *articleTestDB) Close() error { return nil }
func cloneArticleState(in articleDatabaseState) articleDatabaseState {
	var out articleDatabaseState
	if in.article != nil {
		a := *in.article
		a.Content = append([]string(nil), a.Content...)
		out.article = &a
	}
	if in.schedule != nil {
		due := *in.schedule
		out.schedule = &due
	}
	out.versions = map[int]ArticleVersion{}
	for k, v := range in.versions {
		out.versions[k] = v
	}
	out.reads = append([]articleReadFixture(nil), in.reads...)
	return out
}
func (d *articleTestDB) Begin() (driver.Tx, error) {
	state := cloneArticleState(d.state)
	d.working = &state
	return d, nil
}
func (d *articleTestDB) Commit() error   { d.state = *d.working; d.working = nil; return nil }
func (d *articleTestDB) Rollback() error { d.working = nil; return nil }
func (d *articleTestDB) current() *articleDatabaseState {
	if d.working != nil {
		return d.working
	}
	return &d.state
}

type articleRows struct {
	columns []string
	values  [][]driver.Value
	index   int
}

func (r *articleRows) Columns() []string { return r.columns }
func (r *articleRows) Close() error      { return nil }
func (r *articleRows) Next(out []driver.Value) error {
	if r.index >= len(r.values) {
		return io.EOF
	}
	copy(out, r.values[r.index])
	r.index++
	return nil
}
func articleTestRows(columns string, values ...[]driver.Value) driver.Rows {
	return &articleRows{columns: strings.Split(columns, ","), values: values}
}
func articleTestCounts(reads []articleReadFixture, day string) (int64, int64) {
	var views int64
	readers := map[string]bool{}
	for _, r := range reads {
		if day == "" || r.day == day {
			views++
			readers[r.reader] = true
		}
	}
	return views, int64(len(readers))
}
func (d *articleTestDB) QueryContext(_ context.Context, q string, args []driver.NamedValue) (driver.Rows, error) {
	state := d.current()
	switch {
	case strings.HasPrefix(q, "SELECT article_id FROM article_schedules"):
		if state.schedule != nil && !state.schedule.After(args[0].Value.(time.Time)) {
			return articleTestRows("article_id", []driver.Value{state.article.ID}), nil
		}
		return articleTestRows("article_id"), nil
	case strings.HasPrefix(q, "SELECT payload,updated_at,status FROM articles"):
		if d.working != nil && !strings.Contains(q, "FOR UPDATE") {
			return nil, errors.New("article write must lock row")
		}
		if state.article == nil || state.article.ID != args[0].Value.(string) || (strings.Contains(q, "status='PUBLISHED'") && state.article.Status != "PUBLISHED") {
			return articleTestRows("payload,updated_at,status"), nil
		}
		return articleTestRows("payload,updated_at,status", []driver.Value{[]byte(jsonValue(state.article)), normalizeDate("2026-10-01T00:00:00Z"), state.article.Status}), nil
	case strings.HasPrefix(q, "SELECT scheduled_at FROM article_schedules"):
		if !strings.Contains(q, "FOR UPDATE") {
			return nil, errors.New("schedule must lock row")
		}
		if state.schedule == nil {
			return articleTestRows("scheduled_at"), nil
		}
		return articleTestRows("scheduled_at", []driver.Value{*state.schedule}), nil
	case strings.HasPrefix(q, "SELECT COUNT(*) FROM articles"):
		n := int64(0)
		if state.article != nil && state.article.ID == args[0].Value.(string) {
			n = 1
		}
		return articleTestRows("count", []driver.Value{n}), nil
	case strings.HasPrefix(q, "SELECT COUNT(*),COUNT(DISTINCT reader_hash)"):
		views, readers := articleTestCounts(state.reads, "")
		if strings.Contains(q, "COALESCE(SUM") {
			todayViews, todayReaders := articleTestCounts(state.reads, args[0].Value.(string))
			return articleTestRows("views,readers,todayViews,todayReaders", []driver.Value{views, readers, todayViews, todayReaders}), nil
		}
		return articleTestRows("views,readers", []driver.Value{views, readers}), nil
	case strings.HasPrefix(q, "SELECT payload FROM article_versions"):
		v, ok := state.versions[int(args[1].Value.(int64))]
		if !ok {
			return articleTestRows("payload"), nil
		}
		return articleTestRows("payload", []driver.Value{[]byte(jsonValue(v.Article))}), nil
	case strings.HasPrefix(q, "SELECT version,payload,created_by,reason,created_at"):
		keys := []int{}
		for k := range state.versions {
			keys = append(keys, k)
		}
		sort.Sort(sort.Reverse(sort.IntSlice(keys)))
		values := [][]driver.Value{}
		for _, k := range keys {
			v := state.versions[k]
			values = append(values, []driver.Value{int64(k), []byte(jsonValue(v.Article)), v.CreatedBy, v.Reason, normalizeDate(v.CreatedAt)})
		}
		return articleTestRows("version,payload,created_by,reason,created_at", values...), nil
	case strings.HasPrefix(q, "SELECT DATE_FORMAT(read_date"):
		days := map[string]bool{}
		for _, r := range state.reads {
			if r.day >= args[1].Value.(string) && r.day <= args[2].Value.(string) {
				days[r.day] = true
			}
		}
		values := [][]driver.Value{}
		for day := range days {
			views, readers := articleTestCounts(state.reads, day)
			values = append(values, []driver.Value{day, views, readers})
		}
		return articleTestRows("date,views,readers", values...), nil
	case strings.HasPrefix(q, "SELECT a.payload,a.updated_at,a.status"):
		if state.article == nil || strings.Contains(q, "a.status='PUBLISHED'") && state.article.Status != "PUBLISHED" {
			return articleTestRows("payload,updated_at,status,views,readers"), nil
		}
		views, readers := articleTestCounts(state.reads, "")
		return articleTestRows("payload,updated_at,status,views,readers", []driver.Value{[]byte(jsonValue(state.article)), normalizeDate("2026-10-01T00:00:00Z"), state.article.Status, views, readers}), nil
	}
	return nil, errors.New("unexpected query: " + q)
}
func (d *articleTestDB) ExecContext(_ context.Context, q string, args []driver.NamedValue) (driver.Result, error) {
	if strings.HasPrefix(q, "INSERT INTO audit_logs") {
		return driver.RowsAffected(1), nil
	}
	if d.working == nil {
		d.writeUnlocked = true
		return nil, errors.New("article write outside transaction")
	}
	state := d.working
	switch {
	case strings.Contains(q, "INTO article_versions"):
		if d.failSnapshot {
			return nil, errors.New("snapshot unavailable")
		}
		version := int(args[1].Value.(int64))
		if _, exists := state.versions[version]; exists {
			if strings.Contains(q, "IGNORE") {
				return driver.RowsAffected(0), nil
			}
			return nil, errors.New("immutable version duplicate")
		}
		var a Article
		if err := json.Unmarshal([]byte(args[2].Value.(string)), &a); err != nil {
			return nil, err
		}
		state.versions[version] = ArticleVersion{Version: version, Article: a, CreatedBy: args[3].Value.(string), Reason: args[4].Value.(string), CreatedAt: args[5].Value.(time.Time).UTC().Format(time.RFC3339Nano)}
	case strings.HasPrefix(q, "INSERT INTO articles"):
		if state.article != nil {
			return nil, errors.New("duplicate article")
		}
		var a Article
		if err := json.Unmarshal([]byte(args[5].Value.(string)), &a); err != nil {
			return nil, err
		}
		state.article = &a
	case strings.HasPrefix(q, "UPDATE articles"):
		var a Article
		if err := json.Unmarshal([]byte(args[4].Value.(string)), &a); err != nil {
			return nil, err
		}
		state.article = &a
	case strings.HasPrefix(q, "INSERT INTO article_schedules"):
		due := args[1].Value.(time.Time)
		state.schedule = &due
	case strings.HasPrefix(q, "DELETE FROM article_schedules"):
		state.schedule = nil
	case strings.HasPrefix(q, "INSERT IGNORE INTO article_reads"):
		reader, event := args[1].Value.(string), args[2].Value.(string)
		for _, r := range state.reads {
			if reader == r.reader && event == r.event {
				return driver.RowsAffected(0), nil
			}
		}
		state.reads = append(state.reads, articleReadFixture{reader: reader, event: event, day: args[3].Value.(string)})
	default:
		return nil, errors.New("unexpected write: " + q)
	}
	return driver.RowsAffected(1), nil
}
func newArticleTestStore(t *testing.T, a *Article) (*Store, *articleTestDB) {
	t.Helper()
	state := &articleTestDB{state: cloneArticleState(articleDatabaseState{article: a, versions: map[int]ArticleVersion{}})}
	db := sql.OpenDB(articleConnector{state})
	db.SetMaxOpenConns(1)
	t.Cleanup(func() { db.Close() })
	return &Store{db: db}, state
}

func articleSaveRaw(a Article) json.RawMessage { return json.RawMessage(jsonValue(M{"article": a})) }

func TestArticleSaveRestoreAndConflict(t *testing.T) {
	a := articleFixture()
	a.ContentHTML = "<p><strong>原始正文</strong></p>"
	store, db := newArticleTestStore(t, &a)
	s := &Server{store: store}
	actor := Claims{Subject: "ADM1", Username: "editor"}
	db.state.reads = []articleReadFixture{{reader: "r1", event: "e1", day: "2026-10-01"}, {reader: "r1", event: "e2", day: "2026-10-01"}}
	versions, err := store.articleVersions(context.Background(), a.ID)
	if err != nil || len(versions) != 1 || versions[0].Version != 1 || versions[0].Article.ContentHTML != a.ContentHTML {
		t.Fatalf("legacy history: %+v %v", versions, err)
	}
	a.Version, a.Title, a.Status = 1, "修订正文", "SCHEDULED"
	a.ContentHTML = "<p>修订正文</p>"
	a.ScheduledAt = time.Now().Add(time.Hour).Format(time.RFC3339Nano)
	a.ReadCount, a.ReaderCount = 900, 900
	result, err := s.saveArticle(context.Background(), actor, articleSaveRaw(a))
	if err != nil {
		t.Fatal(err)
	}
	saved := result.(Article)
	if saved.Version != 2 || saved.ReadCount != 2 || saved.ReaderCount != 1 || db.state.schedule == nil || len(db.state.versions) != 2 {
		t.Fatalf("transactional save: %+v %+v", saved, db.state)
	}
	if baseline := db.state.versions[1].Article; baseline.Title != "葡萄" || baseline.ContentHTML != "<p><strong>原始正文</strong></p>" {
		t.Fatalf("baseline changed: %+v", baseline)
	}
	if _, err = s.saveArticle(context.Background(), actor, articleSaveRaw(a)); err == nil || db.state.article.Version != 2 {
		t.Fatal("stale editor overwrote newer version")
	}
	result, err = s.restoreArticle(context.Background(), actor, json.RawMessage(`{"articleId":"ART1","version":1,"expectedVersion":2}`))
	if err != nil {
		t.Fatal(err)
	}
	restored := result.(Article)
	normalizedBaselineHTML, _, sanitizeErr := sanitizeArticleHTML("<p><strong>原始正文</strong></p>")
	if sanitizeErr != nil {
		t.Fatal(sanitizeErr)
	}
	if restored.Version != 3 || restored.Status != "DRAFT" || restored.ScheduledAt != "" || db.state.schedule != nil || restored.ReadCount != 2 || restored.ReaderCount != 1 || restored.ContentHTML != normalizedBaselineHTML {
		t.Fatalf("restore must create draft and preserve reads: %+v", restored)
	}
	versions, err = store.articleVersions(context.Background(), a.ID)
	if err != nil || len(versions) != 3 || versions[0].Version != 3 || versions[0].Reason != "restore:1" || versions[1].Version != 2 {
		t.Fatalf("ordered immutable history: %+v %v", versions, err)
	}
	if _, err = s.restoreArticle(context.Background(), actor, json.RawMessage(`{"articleId":"ART1","version":1,"expectedVersion":2}`)); err == nil {
		t.Fatal("stale restore accepted")
	}
	if db.writeUnlocked {
		t.Fatal("unlocked article mutation")
	}
}

func TestArticleSaveSnapshotFailureRollsBack(t *testing.T) {
	store, db := newArticleTestStore(t, nil)
	s := &Server{store: store}
	a := articleFixture()
	a.Status, a.ScheduledAt = "SCHEDULED", time.Now().Add(time.Hour).Format(time.RFC3339Nano)
	db.failSnapshot = true
	if _, err := s.saveArticle(context.Background(), Claims{Subject: "ADM1"}, articleSaveRaw(a)); err == nil {
		t.Fatal("snapshot failure ignored")
	}
	if db.state.article != nil || db.state.schedule != nil || len(db.state.versions) != 0 {
		t.Fatal("partial create committed without immutable snapshot")
	}
	db.failSnapshot = false
	result, err := s.saveArticle(context.Background(), Claims{Subject: "ADM1"}, articleSaveRaw(a))
	if err != nil || result.(Article).Version != 1 || len(db.state.versions) != 1 {
		t.Fatalf("new article: %+v %v", result, err)
	}
}

func TestArticleDuePublicationAtomicAndIdempotent(t *testing.T) {
	now := time.Date(2026, 10, 4, 1, 0, 0, 0, time.UTC)
	a := articleFixture()
	a.Version, a.Status, a.ScheduledAt = 1, "SCHEDULED", now.Format(time.RFC3339)
	store, db := newArticleTestStore(t, &a)
	db.state.schedule = &now
	db.state.versions[1] = ArticleVersion{Version: 1, Article: a, Reason: "create", CreatedAt: now.Add(-time.Hour).Format(time.RFC3339)}
	if err := store.publishDueArticles(context.Background(), now.Add(-time.Nanosecond)); err != nil || db.state.article.Status != "SCHEDULED" {
		t.Fatal("scheduled article published early")
	}
	db.failSnapshot = true
	if err := store.publishDueArticles(context.Background(), now); err == nil {
		t.Fatal("snapshot error ignored")
	}
	if db.state.article.Status != "SCHEDULED" || db.state.schedule == nil || db.state.article.Version != 1 {
		t.Fatal("failed publication partially committed")
	}
	db.failSnapshot = false
	if err := store.publishDueArticles(context.Background(), now); err != nil {
		t.Fatal(err)
	}
	if db.state.article.Status != "PUBLISHED" || db.state.article.Version != 2 || db.state.article.PublishedAt != now.Format(time.RFC3339) || db.state.article.ScheduledAt != "" || db.state.schedule != nil || db.state.versions[2].Reason != "scheduled_publish" {
		t.Fatalf("due transition: %+v", db.state)
	}
	if err := store.publishDueArticles(context.Background(), now.Add(time.Minute)); err != nil || len(db.state.versions) != 2 {
		t.Fatal("repeat scheduler created another revision")
	}
}

func TestArticleReadIdempotencyAndVisibility(t *testing.T) {
	a := articleFixture()
	a.Status = "PUBLISHED"
	store, db := newArticleTestStore(t, &a)
	for i, visit := range []struct {
		reader, event string
		recorded      bool
	}{{"openid-one", "visit1", true}, {"openid-one", "visit1", false}, {"openid-one", "visit2", true}, {"openid-two", "visit1", true}} {
		result, err := store.recordArticleRead(context.Background(), visit.reader, json.RawMessage(jsonValue(M{"articleId": a.ID, "eventId": visit.event})))
		if err != nil || result.(M)["recorded"] != visit.recorded {
			t.Fatalf("visit%d: %+v %v", i, result, err)
		}
	}
	got, err := store.article(context.Background(), a.ID, false)
	if err != nil || got.ReadCount != 3 || got.ReaderCount != 2 || got.Version != 1 || got.UpdatedAt == "" {
		t.Fatalf("public counters and legacy metadata: %+v %v", got, err)
	}
	for _, r := range db.state.reads {
		if strings.Contains(r.reader, "openid") || len(r.reader) != 64 {
			t.Fatal("raw reader identity persisted")
		}
	}
	db.state.article.Status = "DRAFT"
	if _, err := store.recordArticleRead(context.Background(), "openid-one", json.RawMessage(`{"articleId":"ART1","eventId":"draft"}`)); err == nil {
		t.Fatal("draft counted")
	}
	got, err = store.article(context.Background(), a.ID, false)
	if err != nil || got != nil {
		t.Fatal("draft exposed publicly")
	}
	list, err := store.articles(context.Background(), "", "", false)
	if err != nil || len(list) != 0 {
		t.Fatal("public list exposes draft")
	}
	list, err = store.articles(context.Background(), "", "", true)
	if err != nil || len(list) != 1 || list[0].ReadCount != 3 || list[0].ReaderCount != 2 {
		t.Fatalf("admin list counters: %+v %v", list, err)
	}
	db.state.article.Status = "SCHEDULED"
	future := time.Now().Add(time.Hour)
	db.state.schedule = &future
	got, err = store.article(context.Background(), a.ID, false)
	if err != nil || got != nil {
		t.Fatal("future scheduled article exposed publicly")
	}
	list, err = store.articles(context.Background(), "", "", false)
	if err != nil || len(list) != 0 {
		t.Fatal("future scheduled article exposed in public list")
	}
	if _, err := store.recordArticleRead(context.Background(), "openid-one", json.RawMessage(`{"articleId":"ART1","eventId":"scheduled"}`)); err == nil {
		t.Fatal("future scheduled article counted")
	}
	for _, raw := range []json.RawMessage{json.RawMessage(`{"articleId":"ART1"}`), json.RawMessage(`{"articleId":"ART1","eventId":"has space"}`)} {
		if _, err := store.recordArticleRead(context.Background(), "openid-one", raw); err == nil {
			t.Fatal("invalid event accepted")
		}
	}
}

func TestArticlePublisherRechecksCancelledOrPostponedSchedule(t *testing.T) {
	now := time.Now().UTC()
	a := articleFixture()
	a.Version, a.Status = 2, "SCHEDULED"
	store, db := newArticleTestStore(t, &a)
	future := now.Add(time.Hour)
	db.state.schedule = &future
	// A candidate collected before an editor postponed its publication must
	// re-read the schedule after acquiring the article lock.
	if err := store.publishDueArticle(context.Background(), a.ID, now); err != nil || db.state.article.Version != 2 || db.state.article.Status != "SCHEDULED" {
		t.Fatal("worker overwrote postponed schedule")
	}
	db.state.article.Status, db.state.article.Version, db.state.schedule = "DRAFT", 3, nil
	if err := store.publishDueArticle(context.Background(), a.ID, now); err != nil || db.state.article.Version != 3 || db.state.article.Status != "DRAFT" {
		t.Fatal("worker overwrote cancelled publication")
	}
}

func TestArticleStatisticsBeijingDaysAndDistinctReaders(t *testing.T) {
	boundary := time.Date(2026, 10, 3, 16, 0, 0, 0, time.UTC)
	zero := emptyArticleStats(boundary)
	if len(zero.Days) != 14 || zero.Days[0].Date != "2026-09-21" || zero.Days[13].Date != "2026-10-04" {
		t.Fatalf("Beijing date boundary: %+v", zero)
	}
	a := articleFixture()
	store, db := newArticleTestStore(t, &a)
	today := time.Now().In(articleBeijing).Format("2006-01-02")
	yesterday := time.Now().In(articleBeijing).AddDate(0, 0, -1).Format("2006-01-02")
	db.state.reads = []articleReadFixture{{"r1", "e1", "2020-01-01"}, {"r1", "e2", yesterday}, {"r1", "e3", today}, {"r1", "e4", today}, {"r2", "e5", today}}
	stats, err := store.articleStats(context.Background(), a.ID)
	if err != nil || stats.TotalViews != 5 || stats.TotalReaders != 2 || stats.TodayViews != 3 || stats.TodayReaders != 2 || stats.Days[12].Views != 1 || stats.Days[13].Views != 3 || stats.Days[13].Readers != 2 {
		t.Fatalf("PV/UV aggregation: %+v %v", stats, err)
	}
	for _, day := range stats.Days[:12] {
		if day.Views != 0 || day.Readers != 0 {
			t.Fatal("missing days not zero-filled")
		}
	}
	if _, err = store.articleStats(context.Background(), "missing"); err == nil {
		t.Fatal("unknown article returned fabricated statistics")
	}
}
