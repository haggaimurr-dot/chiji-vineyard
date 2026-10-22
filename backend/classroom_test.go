package main

import (
	"strings"
	"testing"
)

func TestClassroomQuestionsPrivacyAndPagination(t *testing.T) {
	for _, admin := range []bool{false, true} {
		q, args, err := classroomQuestionQuery("user-one", classroomQuestionOptions{ArticleID: "lesson-one", Offset: 20, Limit: 20, Status: "pending"}, admin)
		if err != nil {
			t.Fatal(err)
		}
		if strings.Contains(q, "user-one") || strings.Contains(q, "lesson-one") {
			t.Fatal("unbound SQL parameters")
		}
		if !strings.Contains(q, "q.article_id=?") || !strings.Contains(q, "q.answer=''") || !strings.Contains(q, "ORDER BY q.created_at DESC,q.id DESC") {
			t.Fatal(q)
		}
		if !admin && (!strings.Contains(q, "q.openid=?") || args[0] != "user-one") {
			t.Fatal("missing user isolation")
		}
		if admin && strings.Contains(q, "q.openid=?") {
			t.Fatal("admin cannot see all questions")
		}
		if args[len(args)-2] != 21 || args[len(args)-1] != 20 {
			t.Fatal("pagination lookahead missing")
		}
	}
	for _, d := range []classroomQuestionOptions{{Limit: 0}, {Limit: 51}, {Limit: 20, Offset: -1}, {Limit: 20, Status: "all"}} {
		if _, _, err := classroomQuestionQuery("user", d, false); err == nil {
			t.Fatal("accepted invalid query", d)
		}
	}
	if actionPermissions["admin.classroom.questions"] != "article.read" || actionPermissions["admin.classroom.answer"] != "article.write" {
		t.Fatal("question permissions missing")
	}
}

func TestClassroomUnicodeValidationAndIdempotencyKey(t *testing.T) {
	if text, err := classroomText("  葡萄\n修剪  ", 500); err != nil || text != "葡萄\n修剪" {
		t.Fatal(text, err)
	}
	for _, text := range []string{" ", strings.Repeat("葡", 501)} {
		if _, err := classroomText(text, 500); err == nil {
			t.Fatal("accepted invalid question")
		}
	}
	if _, err := classroomText(strings.Repeat("葡", 500), 500); err != nil {
		t.Fatal(err)
	}
	for _, key := range []string{"", "bad key", "key'", strings.Repeat("a", 129)} {
		if validClassroomEvent(key) {
			t.Fatal("accepted invalid event", key)
		}
	}
	if !validClassroomEvent("question-1_a") {
		t.Fatal("rejected valid event")
	}
}
