package main

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"math"
	"strings"
	"time"
	"unicode/utf8"
)

type CourseEntity struct {
	ID         string   `json:"id"`
	Title      string   `json:"title"`
	Summary    string   `json:"summary"`
	Stage      string   `json:"stage"`
	Variety    string   `json:"variety"`
	Status     string   `json:"status"`
	SortOrder  int      `json:"sortOrder"`
	Version    int      `json:"version"`
	ArticleIDs []string `json:"articleIds"`
}

func validateCourse(c *CourseEntity) error {
	c.Title = strings.TrimSpace(c.Title)
	if c.Title == "" || utf8.RuneCountInString(c.Title) > 120 || utf8.RuneCountInString(c.Summary) > 1000 || len(c.ID) > 80 || len(c.Stage) > 240 || len(c.Variety) > 240 || (c.Status != "DRAFT" && c.Status != "PUBLISHED") || len(c.ArticleIDs) > 200 || c.SortOrder < -100000 || c.SortOrder > 100000 {
		return errors.New("课程参数无效（标题必填，最多200节）")
	}
	seen := map[string]bool{}
	for _, id := range c.ArticleIDs {
		if id == "" || len(id) > 80 || seen[id] {
			return errors.New("课时编号无效或重复")
		}
		seen[id] = true
	}
	if c.Status == "PUBLISHED" && len(c.ArticleIDs) == 0 {
		return errors.New("发布课程前请添加课时")
	}
	return nil
}
func (s *Store) courseList(ctx context.Context) ([]CourseEntity, error) {
	rows, err := s.db.QueryContext(ctx, "SELECT payload FROM classroom_courses ORDER BY sort_order,id")
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []CourseEntity{}
	for rows.Next() {
		var raw []byte
		if err = rows.Scan(&raw); err != nil {
			return nil, err
		}
		var c CourseEntity
		if err = json.Unmarshal(raw, &c); err != nil {
			return nil, err
		}
		out = append(out, c)
	}
	return out, rows.Err()
}
func (s *Store) courseSave(ctx context.Context, actor Claims, raw json.RawMessage) (any, error) {
	var d struct {
		Course          CourseEntity `json:"course"`
		ExpectedVersion int          `json:"expectedVersion"`
	}
	if err := decode(raw, &d); err != nil {
		return nil, err
	}
	c := d.Course
	if err := validateCourse(&c); err != nil {
		return nil, err
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	if c.ID == "" {
		if d.ExpectedVersion != 0 {
			return nil, errors.New("新课程版本无效")
		}
		c.ID = randomID("CR")
		c.Version = 1
	} else {
		var version int
		if err = tx.QueryRowContext(ctx, "SELECT version FROM classroom_courses WHERE id=? FOR UPDATE", c.ID).Scan(&version); err != nil {
			return nil, err
		}
		if version != d.ExpectedVersion {
			return nil, errors.New("课程已更新，请刷新后重试")
		}
		c.Version = version + 1
	}
	for _, id := range c.ArticleIDs {
		a, e := lockedArticle(ctx, tx, id)
		if e != nil {
			return nil, e
		}
		if a == nil {
			return nil, errors.New("课时内容不存在")
		}
		if c.Status == "PUBLISHED" && a.Status != "PUBLISHED" {
			return nil, errors.New("发布课程只能关联已发布内容")
		}
	}
	_, err = tx.ExecContext(ctx, "INSERT INTO classroom_courses(id,status,sort_order,version,payload) VALUES(?,?,?,?,?) ON DUPLICATE KEY UPDATE status=VALUES(status),sort_order=VALUES(sort_order),version=VALUES(version),payload=VALUES(payload)", c.ID, c.Status, c.SortOrder, c.Version, jsonValue(c))
	if err != nil {
		return nil, err
	}
	_, err = tx.ExecContext(ctx, "INSERT INTO audit_logs(admin_id,username,action,target_id,detail) VALUES(?,?,?,?,?)", actor.Subject, actor.Username, "COURSE_SAVE", c.ID, jsonValue(c))
	if err != nil {
		return nil, err
	}
	return c, tx.Commit()
}
func (s *Store) classroomCourses(ctx context.Context, openid string) (any, error) {
	if err := s.publishDueArticles(ctx, time.Now().UTC()); err != nil {
		return nil, err
	}
	entities, err := s.courseList(ctx)
	if err != nil {
		return nil, err
	}
	rows, err := s.db.QueryContext(ctx, "SELECT a.id,JSON_UNQUOTE(JSON_EXTRACT(a.payload,'$.title')),COALESCE(p.completed,0) FROM articles a LEFT JOIN classroom_progress p ON p.article_id=a.id AND p.openid=? WHERE a.status='PUBLISHED'", openid)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	lessons := map[string]classroomLesson{}
	for rows.Next() {
		var l classroomLesson
		if err = rows.Scan(&l.ID, &l.Title, &l.Completed); err != nil {
			return nil, err
		}
		lessons[l.ID] = l
	}
	if err = rows.Err(); err != nil {
		return nil, err
	}
	courses := []classroomCourse{}
	unique := map[string]bool{}
	completed := 0
	for _, e := range entities {
		if e.Status != "PUBLISHED" {
			continue
		}
		c := classroomCourse{ID: e.ID, Title: e.Title, Stage: e.Stage, Variety: e.Variety, Lessons: []classroomLesson{}}
		for _, id := range e.ArticleIDs {
			l, ok := lessons[id]
			if !ok {
				continue
			}
			c.Lessons = append(c.Lessons, l)
			if l.Completed {
				c.Completed++
			} else if c.NextID == "" {
				c.NextID = id
			}
			if !unique[id] {
				unique[id] = true
				if l.Completed {
					completed++
				}
			}
		}
		if len(c.Lessons) > 0 {
			c.Percent = c.Completed * 100 / len(c.Lessons)
		}
		courses = append(courses, c)
	}
	return M{"courses": courses, "total": len(unique), "completed": completed}, nil
}

type VideoProgress struct {
	ArticleID string  `json:"articleId"`
	Source    string  `json:"source"`
	Position  float64 `json:"position"`
	Duration  float64 `json:"duration"`
}

func validateVideoProgress(d VideoProgress) error {
	if d.ArticleID == "" || len(d.ArticleID) > 80 || d.Source == "" || len(d.Source) > 2048 || math.IsNaN(d.Position) || math.IsInf(d.Position, 0) || math.IsNaN(d.Duration) || math.IsInf(d.Duration, 0) || d.Duration <= 0 || d.Duration > 86400 || d.Position < 0 || d.Position > d.Duration {
		return errors.New("视频进度参数无效")
	}
	return nil
}
func (s *Store) videoProgress(ctx context.Context, openid string, raw json.RawMessage, save bool) (any, error) {
	var d VideoProgress
	if err := decode(raw, &d); err != nil {
		return nil, err
	}
	if save {
		if err := validateVideoProgress(d); err != nil {
			return nil, err
		}
	} else if d.ArticleID == "" || len(d.ArticleID) > 80 {
		return nil, errors.New("内容编号无效")
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
	if a == nil || a.Status != "PUBLISHED" || a.Type != "video" {
		return nil, errors.New("视频不存在或已下架")
	}
	if save {
		if d.Source != a.VideoURL {
			return nil, errors.New("视频已更新，请重新打开内容")
		}
		_, err = tx.ExecContext(ctx, "INSERT INTO classroom_video_progress(openid,article_id,source,position,duration) VALUES(?,?,?,?,?) ON DUPLICATE KEY UPDATE source=VALUES(source),position=VALUES(position),duration=VALUES(duration)", openid, d.ArticleID, d.Source, d.Position, d.Duration)
	} else {
		d.Source = a.VideoURL
		err = tx.QueryRowContext(ctx, "SELECT position,duration FROM classroom_video_progress WHERE openid=? AND article_id=? AND BINARY source=BINARY ?", openid, d.ArticleID, a.VideoURL).Scan(&d.Position, &d.Duration)
		if errors.Is(err, sql.ErrNoRows) {
			err = nil
		}
	}
	if err != nil {
		return nil, err
	}
	return d, tx.Commit()
}
