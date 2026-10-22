package main

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
)

func validContentArticle() Article {
	return Article{Title: "葡萄管理", Summary: "种植经验", Cover: "https://example.com/cover.jpg", Variety: "通用", Stage: "采后管理", Type: "article", Status: "DRAFT", PublishedAt: "2026-10-04", Content: []string{"旧版正文第一段", "第二段 <安全文本>"}}
}

func TestArticleRichTextSanitization(t *testing.T) {
	source := `<h2 onclick="alert(1)">管理<strong>要点</strong></h2><p style="color:red;position:fixed;background-image:url(javascript:alert(1))">正文<a href="jav&#x61;script:alert(1)" onfocus="evil()">危险链接</a><a href="https://example.com/guide?a=1&amp;b=2" target="_blank">参考</a></p><script>secretScript</script><iframe>hiddenIframe</iframe><svg><script>svgScript</script></svg><img src="https://example.com/photo.jpg" onerror="alert(1)"><img src="data:image/svg+xml;base64,ZXZpbA=="><img src="cloud://bucket/photo.jpg">`
	got, paragraphs, err := sanitizeArticleHTML(source)
	if err != nil {
		t.Fatal(err)
	}
	for _, forbidden := range []string{"onclick", "onfocus", "onerror", "javascript", "position", "background-image", "color:red", "secretScript", "hiddenIframe", "svgScript", "<svg", "data:", "cloud://", "target="} {
		if strings.Contains(got, forbidden) {
			t.Fatalf("unsafe value survived %q: %s", forbidden, got)
		}
	}
	for _, expected := range []string{"<h2", "font-size:22px", `href="https://example.com/guide?a=1&amp;b=2"`, `src="https://example.com/photo.jpg"`, "max-width:100%;height:auto"} {
		if !strings.Contains(got, expected) {
			t.Fatalf("safe formatting missing %q: %s", expected, got)
		}
	}
	if strings.Join(paragraphs, "|") != "管理要点|正文危险链接参考" {
		t.Fatalf("plain text lost visible content: %#v", paragraphs)
	}
	second, _, err := sanitizeArticleHTML(got)
	if err != nil || second != got {
		t.Fatalf("sanitizer should remain stable across edits: %v\n%s\n%s", err, got, second)
	}
}

func TestArticleRejectsEmptyOversizedAndExcessImages(t *testing.T) {
	for _, source := range []string{"", "<p><br>&nbsp;\u200b\ufeff\u200d</p>", "<script>alert(1)</script>", `<img src="javascript:alert(1)">`, `<img src="cloud://bucket/photo.jpg">`, strings.Repeat("文", maxArticleHTMLBytes), strings.Repeat(`<img src="https://example.com/photo.jpg">`, maxArticleImages+1), strings.Repeat("<span>", 70) + "正文" + strings.Repeat("</span>", 70)} {
		if _, _, err := sanitizeArticleHTML(source); err == nil {
			t.Fatalf("invalid body accepted: %s", truncate(source, 60))
		}
	}
	if _, text, err := sanitizeArticleHTML(`<p><img src="https://example.com/photo.jpg"></p>`); err != nil || len(text) != 0 {
		t.Fatalf("image-only body should be valid: %v %#v", err, text)
	}
}

func TestArticleURLValidation(t *testing.T) {
	for _, value := range []string{"https://example.com/video.mp4?token=a%2Fb", "http://localhost:8080/media/test", "http://127.0.0.1:8080/media/test", "http://[::1]:8080/media/test"} {
		if _, err := validateContentURL(value, false); err != nil {
			t.Fatalf("valid URL rejected %s: %v", value, err)
		}
	}
	for _, value := range []string{"", "cloud://bucket/file.mp4", "http://example.com/a.mp4", "http://localhost.evil.test/a.mp4", "javascript:alert(1)", "data:video/mp4;base64,YQ==", "blob:https://example.com/id", "//example.com/a.mp4", "https://user:password@example.com/a.mp4", "https://example.com:99999/a.mp4", "https://example.com/a\nb.mp4", `https://example.com\@evil.test/a.mp4`} {
		if _, err := validateContentURL(value, false); err == nil {
			t.Fatalf("unsafe URL accepted %q", value)
		}
	}
}

func TestArticleLegacyAndRichContentCompatibility(t *testing.T) {
	a, err := validateArticle(validContentArticle())
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(a.ContentHTML, "&lt;安全文本&gt;") || strings.Join(a.Content, "|") != "旧版正文第一段|第二段 <安全文本>" {
		t.Fatalf("legacy plain text changed: %#v", a)
	}
	a.ContentHTML = `<h3>更新</h3><p>富文本<strong>正文</strong></p>`
	a.Content = []string{"陈旧内容"}
	a.VideoURL, a.Duration = "https://example.com/old.mp4", "03:12"
	a, err = validateArticle(a)
	if err != nil || strings.Join(a.Content, "|") != "更新|富文本正文" || a.VideoURL != "" || a.Duration != "" {
		t.Fatalf("rich text should be authoritative and stale video cleared: %#v %v", a, err)
	}
	var decoded Article
	if err = json.Unmarshal([]byte(`{"title":"旧记录","content":["原正文"]}`), &decoded); err != nil || decoded.ContentHTML != "" || decoded.Content[0] != "原正文" {
		t.Fatalf("old stored payload incompatible: %#v %v", decoded, err)
	}
}

func TestArticleServerValidation(t *testing.T) {
	cases := []struct {
		name   string
		change func(*Article)
	}{
		{"empty title", func(a *Article) { a.Title = " " }},
		{"empty variety", func(a *Article) { a.Variety = "" }},
		{"empty stage", func(a *Article) { a.Stage = "" }},
		{"long summary", func(a *Article) { a.Summary = strings.Repeat("文", 261) }},
		{"bad status", func(a *Article) { a.Status = "DELETED" }},
		{"bad type", func(a *Article) { a.Type = "audio" }},
		{"invalid date", func(a *Article) { a.PublishedAt = "2026-02-30" }},
		{"invalid cover", func(a *Article) { a.Cover = "javascript:alert(1)" }},
		{"empty video", func(a *Article) { a.Type = "video"; a.VideoURL = "" }},
		{"cloud video", func(a *Article) { a.Type = "video"; a.VideoURL = "cloud://bucket/video.mp4" }},
		{"bad duration", func(a *Article) { a.Type = "video"; a.VideoURL = "https://example.com/video.mp4"; a.Duration = "04:99" }},
		{"empty body", func(a *Article) { a.Content = nil; a.ContentHTML = "<p><br></p>" }},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			a := validContentArticle()
			tc.change(&a)
			if _, err := validateArticle(a); err == nil {
				t.Fatal("invalid article accepted")
			}
		})
	}
	a := validContentArticle()
	a.Type, a.VideoURL, a.Duration = "video", "https://example.com/video.mp4?signature=secret", "04:36"
	if _, err := validateArticle(a); err != nil {
		t.Fatalf("video article rejected: %v", err)
	}
	a.Type, a.VideoURL, a.Duration, a.Cover = "article", "", "", "cloud://bucket/cover.jpg"
	if _, err := validateArticle(a); err != nil {
		t.Fatalf("legacy cloud cover rejected: %v", err)
	}
}

func TestArticleRestoreSanitizesHistoricalHTML(t *testing.T) {
	current := validContentArticle()
	current.ID, current.Version = "ART_restore", 2
	store, db := newArticleTestStore(t, &current)
	legacy := current
	legacy.Version = 1
	legacy.ContentHTML = `<p>历史正文<strong onclick="alert(1)">重点</strong></p><script>oldDangerousScript</script><img src="javascript:alert(1)">`
	db.state.versions[1] = ArticleVersion{Version: 1, Article: legacy}
	s := &Server{store: store}
	result, err := s.restoreArticle(context.Background(), Claims{Subject: "ADM_restore"}, json.RawMessage(`{"articleId":"ART_restore","version":1,"expectedVersion":2}`))
	if err != nil {
		t.Fatal(err)
	}
	restored := result.(Article)
	if restored.Version != 3 || restored.Status != "DRAFT" || strings.Contains(restored.ContentHTML, "onclick") || strings.Contains(restored.ContentHTML, "oldDangerousScript") || strings.Contains(restored.ContentHTML, "javascript:") || strings.Join(restored.Content, "|") != "历史正文重点" {
		t.Fatalf("restored HTML bypassed sanitizer: %#v", restored)
	}
	if db.state.versions[1].Article.ContentHTML != legacy.ContentHTML {
		t.Fatal("historical snapshot source was modified")
	}
}
