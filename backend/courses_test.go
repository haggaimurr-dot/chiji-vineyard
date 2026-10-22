package main

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"encoding/json"
	"math"
	"strings"
	"testing"
)

func TestIndependentCourseValidation(t *testing.T) {
	c := CourseEntity{Title: "跨阶段课程", Status: "PUBLISHED", ArticleIDs: []string{"late", "early"}}
	if err := validateCourse(&c); err != nil {
		t.Fatal(err)
	}
	for _, ids := range [][]string{nil, {"same", "same"}, {""}} {
		c.ArticleIDs = ids
		if validateCourse(&c) == nil {
			t.Fatal("accepted invalid lessons", ids)
		}
	}
	if actionPermissions["admin.article.review"] != "article.review" || actionPermissions["admin.course.save"] != "article.write" {
		t.Fatal("permissions not separated")
	}
}
func TestVideoProgressBounds(t *testing.T) {
	d := VideoProgress{ArticleID: "video", Source: "https://example.com/video.mp4", Position: 35, Duration: 120}
	if err := validateVideoProgress(d); err != nil {
		t.Fatal(err)
	}
	for _, position := range []float64{-1, 121, math.NaN(), math.Inf(1)} {
		d.Position = position
		if validateVideoProgress(d) == nil {
			t.Fatal("accepted invalid position")
		}
	}
	d.Position = 0
	d.Duration = 0
	if validateVideoProgress(d) == nil {
		t.Fatal("accepted zero duration")
	}
}

type videoTestDB struct {
	*articleTestDB
	samples map[string]VideoProgress
}
type videoConnector struct{ db *videoTestDB }

func (c videoConnector) Connect(context.Context) (driver.Conn, error) { return c.db, nil }
func (c videoConnector) Driver() driver.Driver                        { return articleDriver{c.db.articleTestDB} }
func (d *videoTestDB) QueryContext(ctx context.Context, q string, args []driver.NamedValue) (driver.Rows, error) {
	if strings.HasPrefix(q, "SELECT position,duration FROM classroom_video_progress") {
		p, ok := d.samples[args[0].Value.(string)+":"+args[1].Value.(string)]
		if !ok || p.Source != args[2].Value.(string) {
			return articleTestRows("position,duration"), nil
		}
		return articleTestRows("position,duration", []driver.Value{p.Position, p.Duration}), nil
	}
	return d.articleTestDB.QueryContext(ctx, q, args)
}
func (d *videoTestDB) ExecContext(ctx context.Context, q string, args []driver.NamedValue) (driver.Result, error) {
	if strings.HasPrefix(q, "INSERT INTO classroom_video_progress") {
		d.samples[args[0].Value.(string)+":"+args[1].Value.(string)] = VideoProgress{ArticleID: args[1].Value.(string), Source: args[2].Value.(string), Position: args[3].Value.(float64), Duration: args[4].Value.(float64)}
		return driver.RowsAffected(1), nil
	}
	return d.articleTestDB.ExecContext(ctx, q, args)
}
func TestVideoProgressIdentityAndReplacement(t *testing.T) {
	a := articleFixture()
	a.Type = "video"
	a.Status = "PUBLISHED"
	a.VideoURL = "https://example.com/a.mp4"
	_, base := newArticleTestStore(t, &a)
	fixture := &videoTestDB{articleTestDB: base, samples: map[string]VideoProgress{}}
	db := sql.OpenDB(videoConnector{fixture})
	defer db.Close()
	store := &Store{db: db}
	ctx := context.Background()
	data := json.RawMessage(jsonValue(VideoProgress{ArticleID: a.ID, Source: a.VideoURL, Position: 35, Duration: 120}))
	if _, err := store.videoProgress(ctx, "one", data, true); err != nil {
		t.Fatal(err)
	}
	get := func(user string) VideoProgress {
		result, err := store.videoProgress(ctx, user, json.RawMessage(jsonValue(M{"articleId": a.ID})), false)
		if err != nil {
			t.Fatal(err)
		}
		return result.(VideoProgress)
	}
	if get("one").Position != 35 || get("two").Position != 0 {
		t.Fatal("progress not isolated")
	}
	base.state.article.VideoURL = "https://example.com/b.mp4"
	if get("one").Position != 0 {
		t.Fatal("replacement reused progress")
	}
	if _, err := store.videoProgress(ctx, "one", data, true); err == nil {
		t.Fatal("stale source accepted")
	}
	base.state.article.Status = "DRAFT"
	if _, err := store.videoProgress(ctx, "one", data, false); err == nil {
		t.Fatal("draft visible")
	}
}
