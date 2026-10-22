package main

import (
	"context"
	"encoding/json"
	"testing"
)

func TestReviewLifecycleAndConflicts(t *testing.T) {
	a := articleFixture()
	a.Version = 1
	store, db := newArticleTestStore(t, &a)
	s := &Server{store: store}
	actor := Claims{Subject: "ADM1", Username: "审核员", Role: "SUPER_ADMIN"}
	call := func(action, decision, note string, version int) (any, error) {
		return s.reviewArticle(context.Background(), actor, action, json.RawMessage(jsonValue(M{"articleId": a.ID, "expectedVersion": version, "decision": decision, "note": note})))
	}
	if _, err := call("admin.article.submit", "", "", 1); err != nil {
		t.Fatal(err)
	}
	if db.state.article.ReviewStatus != "PENDING" || db.state.article.Version != 2 {
		t.Fatal("submission missing")
	}
	if _, err := call("admin.article.review", "APPROVED", "", 1); err == nil {
		t.Fatal("stale review accepted")
	}
	if _, err := call("admin.article.review", "REJECTED", "", 2); err == nil {
		t.Fatal("empty rejection accepted")
	}
	if _, err := call("admin.article.review", "REJECTED", "补充说明", 2); err != nil {
		t.Fatal(err)
	}
	if db.state.article.ReviewStatus != "REJECTED" || db.state.article.ReviewNote != "补充说明" {
		t.Fatal("rejection missing")
	}
	if _, err := call("admin.article.submit", "", "", 3); err != nil {
		t.Fatal(err)
	}
	db.failSnapshot = true
	if _, err := call("admin.article.review", "APPROVED", "", 4); err == nil {
		t.Fatal("snapshot failure ignored")
	}
	if db.state.article.ReviewStatus != "PENDING" || db.state.article.Version != 4 {
		t.Fatal("review partially committed")
	}
	db.failSnapshot = false
	if _, err := call("admin.article.review", "APPROVED", "通过", 4); err != nil {
		t.Fatal(err)
	}
	if db.state.article.ReviewStatus != "APPROVED" || db.state.article.Status != "DRAFT" || db.state.article.ReviewedBy != "审核员" {
		t.Fatal("approval should keep draft and record actor")
	}
	if _, err := call("admin.article.review", "APPROVED", "", 5); err == nil {
		t.Fatal("repeated review accepted")
	}
}

func TestReviewCannotBeForgedOrReusedForEdits(t *testing.T) {
	a := articleFixture()
	a.ReviewStatus = "APPROVED"
	unchanged := a
	unchanged.Status = "SCHEDULED"
	unchanged.Tags = []string{}
	if err := applyArticleReview(&unchanged, &a); err != nil || unchanged.ReviewStatus != "APPROVED" {
		t.Fatal("publication-only edit invalidated approval", err)
	}
	changed := a
	changed.Title = "修改标题"
	changed.Status = "PUBLISHED"
	if err := applyArticleReview(&changed, &a); err == nil {
		t.Fatal("changed content reused approval")
	}
	changed.Status = "DRAFT"
	if err := applyArticleReview(&changed, &a); err != nil || changed.ReviewStatus != "UNSUBMITTED" {
		t.Fatal("draft edit must require resubmission")
	}
	forged := articleFixture()
	forged.ReviewStatus = "APPROVED"
	forged.ReviewedBy = "伪造"
	if err := applyArticleReview(&forged, nil); err != nil || forged.ReviewStatus != "" || forged.ReviewedBy != "" {
		t.Fatal("client supplied review metadata trusted")
	}
}

func TestContentResourceValidation(t *testing.T) {
	for _, r := range []ContentResource{{Name: "标签", Kind: "tag"}, {Name: "图片", Kind: "image", URL: "https://example.com/a.jpg"}, {Name: "视频", Kind: "video", URL: "https://example.com/a.mp4"}} {
		if _, err := validateContentResource(r); err != nil {
			t.Fatal(err)
		}
	}
	for _, r := range []ContentResource{{Kind: "tag"}, {Name: "x", Kind: "other"}, {Name: "x", Kind: "image", URL: "javascript:alert(1)"}, {Name: "x", Kind: "video", URL: "http://example.com/a.mp4"}, {Name: "x", Kind: "image", URL: "https://user:password@example.com/x"}} {
		if _, err := validateContentResource(r); err == nil {
			t.Fatal("invalid resource accepted", r)
		}
	}
}

func TestPublicArticlesDoNotExposeReviewNotes(t *testing.T) {
	a := articleFixture()
	a.Status = "PUBLISHED"
	a.ReviewStatus = "APPROVED"
	a.ReviewNote = "内部说明"
	a.ReviewedBy = "审核员"
	a.ReviewedAt = "2026-10-06T00:00:00Z"
	store, _ := newArticleTestStore(t, &a)
	detail, err := store.article(context.Background(), a.ID, false)
	if err != nil || detail.ReviewNote != "" || detail.ReviewedBy != "" || detail.ReviewStatus != "" {
		t.Fatal("public detail exposed review", err)
	}
	list, err := store.articles(context.Background(), "", "", false)
	if err != nil || len(list) != 1 || list[0].ReviewNote != "" {
		t.Fatal("public list exposed review", err)
	}
	admin, err := store.article(context.Background(), a.ID, true)
	if err != nil || admin.ReviewNote != "内部说明" {
		t.Fatal("admin lost review metadata", err)
	}
}
