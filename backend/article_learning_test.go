package main

import (
	"strings"
	"testing"
	"time"
)

func TestArticleSearchIsolationAndFilters(t *testing.T) {
	for _, view := range []string{"all", "favorite", "history"} {
		query, args, err := articleSearchQuery("reader-one", articleSearchOptions{Keyword: "  葡萄%'_  ", Stage: "采后管理", Variety: "通用", View: view, Offset: 40, Limit: 20})
		if err != nil {
			t.Fatal(err)
		}
		for _, required := range []string{"f.openid=?", "reader_hash=?", "a.status='PUBLISHED'", "a.stage=?", "a.variety=?", "LOCATE(LOWER(?)", "LIMIT ? OFFSET ?"} {
			if !strings.Contains(query, required) {
				t.Fatalf("missing query constraint %q: %s", required, query)
			}
		}
		if len(args) != 7 || args[0] != "reader-one" || args[1] != articleReaderHash("reader-one") || args[4] != "葡萄%'_" || args[5] != 21 || args[6] != 40 {
			t.Fatalf("incorrect bound parameters: %#v", args)
		}
		if strings.Contains(query, "葡萄") || strings.Contains(query, "reader-one") {
			t.Fatal("input interpolated into SQL")
		}
		if view == "favorite" && (!strings.Contains(query, "f.article_id IS NOT NULL") || !strings.Contains(query, "ORDER BY f.created_at DESC,a.id")) {
			t.Fatal("favorites not filtered and ordered")
		}
		if view == "history" && (!strings.Contains(query, "h.article_id IS NOT NULL") || !strings.Contains(query, "MAX(created_at)") || !strings.Contains(query, "ORDER BY h.last_read_at DESC,a.id")) {
			t.Fatal("history not merged and ordered")
		}
		_, otherArgs, err := articleSearchQuery("reader-two", articleSearchOptions{Limit: 20})
		if err != nil || args[1] == otherArgs[1] {
			t.Fatal("reader identities not isolated")
		}
	}
}

func TestArticleSearchRejectsInvalidParameters(t *testing.T) {
	for _, d := range []articleSearchOptions{
		{Limit: 0}, {Limit: 51}, {Limit: 20, Offset: -1}, {Limit: 20, Offset: 100001},
		{Limit: 20, View: "unknown"}, {Limit: 20, Keyword: strings.Repeat("葡", 101)},
	} {
		if _, _, err := articleSearchQuery("reader", d); err == nil {
			t.Fatalf("accepted invalid options: %+v", d)
		}
	}
	if _, _, err := articleSearchQuery("reader", articleSearchOptions{Limit: 20, Keyword: strings.Repeat("葡", 100)}); err != nil {
		t.Fatal(err)
	}
}

func TestArticleRevisionCannotSavePersonalLearningState(t *testing.T) {
	a := articleFixture()
	a.Favorite, a.LastReadAt = true, "2026-10-04T01:00:00Z"
	got, err := validateArticleRevision(a, time.Now())
	if err != nil || got.Favorite || got.LastReadAt != "" {
		t.Fatalf("personal state leaked into published content: %+v %v", got, err)
	}
}
