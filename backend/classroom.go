package main

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"time"
	"unicode/utf8"
)

type classroomLesson struct {
	ID        string `json:"id"`
	Title     string `json:"title"`
	Completed bool   `json:"completed"`
}
type classroomCourse struct {
	ID        string            `json:"id"`
	Title     string            `json:"title"`
	Stage     string            `json:"stage"`
	Variety   string            `json:"variety"`
	Lessons   []classroomLesson `json:"lessons"`
	Completed int               `json:"completed"`
	Percent   int               `json:"percent"`
	NextID    string            `json:"nextId"`
}

func (s *Store) classroomComplete(ctx context.Context, openid string, raw json.RawMessage) (any, error) {
	var d struct {
		ArticleID string `json:"articleId"`
		Completed *bool  `json:"completed"`
	}
	if err := decode(raw, &d); err != nil {
		return nil, err
	}
	if d.ArticleID == "" || len(d.ArticleID) > 80 || d.Completed == nil {
		return nil, errors.New("学习进度参数无效")
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
	_, err = tx.ExecContext(ctx, "INSERT INTO classroom_progress(openid,article_id,completed) VALUES(?,?,?) ON DUPLICATE KEY UPDATE completed=VALUES(completed)", openid, d.ArticleID, *d.Completed)
	if err != nil {
		return nil, err
	}
	return M{"completed": *d.Completed}, tx.Commit()
}

type classroomQuestion struct {
	ID           string `json:"id"`
	ArticleID    string `json:"articleId"`
	ArticleTitle string `json:"articleTitle"`
	Question     string `json:"question"`
	Answer       string `json:"answer"`
	CreatedAt    string `json:"createdAt"`
	AnsweredAt   string `json:"answeredAt"`
}
type classroomQuestionOptions struct {
	ArticleID string `json:"articleId"`
	Offset    int    `json:"offset"`
	Limit     int    `json:"limit"`
	Status    string `json:"status"`
}

func classroomQuestionQuery(openid string, d classroomQuestionOptions, admin bool) (string, []any, error) {
	if d.Offset < 0 || d.Offset > 100000 || d.Limit < 1 || d.Limit > 50 || len(d.ArticleID) > 80 || (d.Status != "" && d.Status != "pending" && d.Status != "answered") {
		return "", nil, errors.New("问答查询参数无效")
	}
	q := "SELECT q.id,q.article_id,COALESCE(JSON_UNQUOTE(JSON_EXTRACT(a.payload,'$.title')),'已移除的内容'),q.question,q.answer,q.created_at,q.answered_at FROM classroom_questions q LEFT JOIN articles a ON a.id=q.article_id WHERE 1=1"
	args := []any{}
	if !admin {
		q += " AND q.openid=?"
		args = append(args, openid)
	}
	if d.ArticleID != "" {
		q += " AND q.article_id=?"
		args = append(args, d.ArticleID)
	}
	if d.Status == "pending" {
		q += " AND q.answer=''"
	} else if d.Status == "answered" {
		q += " AND q.answer<>''"
	}
	q += " ORDER BY q.created_at DESC,q.id DESC LIMIT ? OFFSET ?"
	return q, append(args, d.Limit+1, d.Offset), nil
}
func (s *Store) classroomQuestions(ctx context.Context, openid string, raw json.RawMessage, admin bool) (any, error) {
	d := classroomQuestionOptions{Limit: 20}
	if err := decode(raw, &d); err != nil {
		return nil, err
	}
	q, args, err := classroomQuestionQuery(openid, d, admin)
	if err != nil {
		return nil, err
	}
	rows, err := s.db.QueryContext(ctx, q, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := []classroomQuestion{}
	for rows.Next() {
		var item classroomQuestion
		var created time.Time
		var answered *time.Time
		if err = rows.Scan(&item.ID, &item.ArticleID, &item.ArticleTitle, &item.Question, &item.Answer, &created, &answered); err != nil {
			return nil, err
		}
		item.CreatedAt = created.UTC().Format(time.RFC3339Nano)
		if answered != nil {
			item.AnsweredAt = answered.UTC().Format(time.RFC3339Nano)
		}
		items = append(items, item)
	}
	if err = rows.Err(); err != nil {
		return nil, err
	}
	more := len(items) > d.Limit
	if more {
		items = items[:d.Limit]
	}
	return M{"items": items, "hasMore": more}, nil
}
func classroomText(value string, max int) (string, error) {
	value = strings.TrimSpace(value)
	if value == "" || utf8.RuneCountInString(value) > max {
		return "", errors.New("请填写有效内容，字数超出限制或内容为空")
	}
	return value, nil
}
func (s *Store) classroomAsk(ctx context.Context, openid string, raw json.RawMessage) (any, error) {
	var d struct {
		ArticleID string `json:"articleId"`
		Question  string `json:"question"`
		EventID   string `json:"eventId"`
	}
	if err := decode(raw, &d); err != nil {
		return nil, err
	}
	text, err := classroomText(d.Question, 500)
	if err != nil {
		return nil, err
	}
	if d.ArticleID == "" || len(d.ArticleID) > 80 || !validClassroomEvent(d.EventID) {
		return nil, errors.New("提问参数无效")
	}
	if err = s.publishDueArticles(ctx, time.Now().UTC()); err != nil {
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
	// The caller keeps the event ID after network failures, preventing duplicates.
	_, err = tx.ExecContext(ctx, "INSERT INTO classroom_questions(id,openid,article_id,event_id,question,answer) VALUES(?,?,?,?,?,'') ON DUPLICATE KEY UPDATE id=id", randomID("CQ"), openid, d.ArticleID, d.EventID, text)
	if err != nil {
		return nil, err
	}
	return M{"submitted": true}, tx.Commit()
}
func validClassroomEvent(id string) bool {
	if len(id) < 1 || len(id) > 128 {
		return false
	}
	for _, c := range id {
		if !(c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c >= '0' && c <= '9' || c == '_' || c == '-') {
			return false
		}
	}
	return true
}
func (s *Store) classroomAnswer(ctx context.Context, actor Claims, raw json.RawMessage) (any, error) {
	var d struct {
		ID             string `json:"id"`
		Answer         string `json:"answer"`
		ExpectedAnswer string `json:"expectedAnswer"`
	}
	if err := decode(raw, &d); err != nil {
		return nil, err
	}
	text, err := classroomText(d.Answer, 2000)
	if err != nil {
		return nil, err
	}
	if d.ID == "" || len(d.ID) > 80 {
		return nil, errors.New("问题编号无效")
	}
	result, err := s.db.ExecContext(ctx, "UPDATE classroom_questions SET answer=?,answered_at=NOW(3),answered_by=? WHERE id=? AND BINARY answer=BINARY ?", text, actor.Subject, d.ID, d.ExpectedAnswer)
	if err != nil {
		return nil, err
	}
	n, err := result.RowsAffected()
	if err != nil {
		return nil, err
	}
	if n == 0 {
		return nil, errors.New("问题不存在或回复已更新，请刷新后重试")
	}
	s.audit(ctx, actor, "CLASSROOM_ANSWER", d.ID, M{"answer": text})
	return M{"answered": true}, nil
}
