package main

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"net/url"
	"slices"
	"strings"
	"time"
)

type ContentResource struct {
	ID      string `json:"id"`
	Name    string `json:"name"`
	Kind    string `json:"kind"`
	URL     string `json:"url,omitempty"`
	Note    string `json:"note,omitempty"`
	Version int    `json:"version"`
}

func validateContentResource(r ContentResource) (ContentResource, error) {
	r.Name, r.Note, r.URL = strings.TrimSpace(r.Name), strings.TrimSpace(r.Note), strings.TrimSpace(r.URL)
	if r.Name == "" || len([]rune(r.Name)) > 80 || len([]rune(r.Note)) > 500 {
		return r, errors.New("名称必填且最多80字，备注最多500字")
	}
	if r.Kind != "tag" && r.Kind != "image" && r.Kind != "video" {
		return r, errors.New("素材类型无效")
	}
	if r.Kind == "tag" {
		r.URL = ""
	} else {
		u, err := url.Parse(r.URL)
		if err != nil || u.Scheme != "https" || u.Host == "" || u.User != nil {
			return r, errors.New("素材地址必须是有效 HTTPS 地址")
		}
	}
	return r, nil
}

// A review applies to the content itself, not the eventual publication time.
func sameReviewContent(a, b Article) bool {
	return a.Title == b.Title && a.Summary == b.Summary && a.Cover == b.Cover && a.Variety == b.Variety && a.Stage == b.Stage && a.Type == b.Type && a.VideoURL == b.VideoURL && a.ContentHTML == b.ContentHTML && slices.Equal(a.Content, b.Content) && slices.Equal(a.Tags, b.Tags)
}

func applyArticleReview(a *Article, current *Article) error {
	a.ReviewStatus, a.ReviewNote, a.ReviewedBy, a.ReviewedAt = "", "", "", ""
	if current == nil || current.ReviewStatus == "" {
		return nil
	}
	if sameReviewContent(*a, *current) {
		a.ReviewStatus, a.ReviewNote, a.ReviewedBy, a.ReviewedAt = current.ReviewStatus, current.ReviewNote, current.ReviewedBy, current.ReviewedAt
	} else {
		a.ReviewStatus = "UNSUBMITTED"
	}
	if a.Status != "DRAFT" && a.ReviewStatus != "APPROVED" {
		return errors.New("此内容需要重新提交审核，通过后才能发布")
	}
	return nil
}

func (s *Server) reviewArticle(ctx context.Context, actor Claims, action string, raw json.RawMessage) (any, error) {
	var d struct {
		ArticleID       string `json:"articleId"`
		ExpectedVersion int    `json:"expectedVersion"`
		Decision        string `json:"decision"`
		Note            string `json:"note"`
	}
	if err := decode(raw, &d); err != nil {
		return nil, err
	}
	tx, err := s.store.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	a, err := lockedArticle(ctx, tx, d.ArticleID)
	if err != nil {
		return nil, err
	}
	if a == nil {
		return nil, errors.New("内容不存在")
	}
	if effectiveArticleVersion(*a) != d.ExpectedVersion {
		return nil, errors.New("内容版本已变化，请刷新后重试")
	}
	now := time.Now().UTC()
	if err = ensureLegacyArticleVersion(ctx, tx, *a); err != nil {
		return nil, err
	}
	if action == "admin.article.submit" {
		if a.Status != "DRAFT" || a.ReviewStatus == "PENDING" {
			return nil, errors.New("仅未待审的草稿可以提交审核")
		}
		if _, err = validateArticleRevision(*a, now); err != nil {
			return nil, err
		}
		a.ReviewStatus, a.ReviewNote, a.ReviewedBy, a.ReviewedAt = "PENDING", "", "", ""
	} else {
		if a.Status != "DRAFT" || a.ReviewStatus != "PENDING" {
			return nil, errors.New("仅待审核草稿可以审核")
		}
		d.Note = strings.TrimSpace(d.Note)
		if len([]rune(d.Note)) > 500 || (d.Decision != "APPROVED" && d.Decision != "REJECTED") || (d.Decision == "REJECTED" && d.Note == "") {
			return nil, errors.New("请选择通过或退回；退回原因必填，最多500字")
		}
		a.ReviewStatus, a.ReviewNote, a.ReviewedBy, a.ReviewedAt = d.Decision, d.Note, actor.Username, now.Format(time.RFC3339Nano)
	}
	a.Version, a.UpdatedAt = effectiveArticleVersion(*a)+1, now.Format(time.RFC3339Nano)
	if err = persistArticle(ctx, tx, *a, true); err != nil {
		return nil, err
	}
	if err = insertArticleVersion(ctx, tx, *a, actor.Subject, a.ReviewStatus, now); err != nil {
		return nil, err
	}
	if err = articleCounters(ctx, tx, a); err != nil {
		return nil, err
	}
	if err = tx.Commit(); err != nil {
		return nil, err
	}
	s.store.audit(ctx, actor, "ARTICLE_REVIEW", a.ID, M{"status": a.ReviewStatus, "note": a.ReviewNote, "version": a.Version})
	return a, nil
}

// Each library entry has its own version and uses the existing configs table.
func (s *Server) contentLibrary(ctx context.Context, actor Claims, action string, raw json.RawMessage) (any, error) {
	if action == "admin.content.library" {
		rows, err := s.store.db.QueryContext(ctx, "SELECT payload FROM configs WHERE config_key LIKE 'content.resource.%' ORDER BY config_key")
		if err != nil {
			return nil, err
		}
		defer rows.Close()
		out := []ContentResource{}
		for rows.Next() {
			var b []byte
			var r ContentResource
			if err = rows.Scan(&b); err != nil {
				return nil, err
			}
			if err = json.Unmarshal(b, &r); err != nil {
				return nil, err
			}
			out = append(out, r)
		}
		return out, rows.Err()
	}
	var d struct {
		Resource ContentResource `json:"resource"`
	}
	if err := decode(raw, &d); err != nil {
		return nil, err
	}
	r, err := validateContentResource(d.Resource)
	if err != nil {
		return nil, err
	}
	if r.ID == "" {
		r.ID = randomID("RES")
	}
	if len(r.ID) > 30 || strings.ContainsAny(r.ID, "/%_ .") {
		return nil, errors.New("资源编号无效")
	}
	permission := "article.write"
	if r.Kind != "tag" {
		permission = "media.upload"
	}
	permissions, err := s.store.adminPermissions(ctx, actor.Subject, actor.Role)
	if err != nil {
		return nil, err
	}
	allowed := actor.Role == "SUPER_ADMIN"
	for _, p := range permissions {
		if p == permission {
			allowed = true
		}
	}
	if !allowed {
		return nil, errPermission
	}
	tx, err := s.store.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	key := "content.resource." + r.ID
	var stored []byte
	err = tx.QueryRowContext(ctx, "SELECT payload FROM configs WHERE config_key=? FOR UPDATE", key).Scan(&stored)
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return nil, err
	}
	current := ContentResource{}
	if len(stored) > 0 {
		if err = json.Unmarshal(stored, &current); err != nil {
			return nil, err
		}
		if current.Version != r.Version || current.Kind != r.Kind {
			return nil, errors.New("资源已变化，请刷新后重试")
		}
	} else if r.Version != 0 {
		return nil, errors.New("资源不存在，请刷新后重试")
	}
	if action == "admin.content.resource.delete" {
		if current.ID == "" {
			return nil, errors.New("资源不存在")
		}
		_, err = tx.ExecContext(ctx, "DELETE FROM configs WHERE config_key=?", key)
	} else {
		r.Version = current.Version + 1
		_, err = tx.ExecContext(ctx, "INSERT INTO configs(config_key,payload) VALUES(?,?) ON DUPLICATE KEY UPDATE payload=VALUES(payload)", key, jsonValue(r))
	}
	if err != nil {
		return nil, err
	}
	if err = tx.Commit(); err != nil {
		return nil, err
	}
	s.store.audit(ctx, actor, "CONTENT_RESOURCE", r.ID, M{"action": action, "name": r.Name, "kind": r.Kind})
	return r, nil
}

func clearArticleReview(a *Article) {
	a.ReviewStatus, a.ReviewNote, a.ReviewedBy, a.ReviewedAt = "", "", "", ""
}
