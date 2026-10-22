package main

import "testing"

func TestExternalTraceValidation(t *testing.T) {
	if err := validateExternalTrace(nil); err != nil {
		t.Fatal(err)
	}
	for _, url := range []string{"https://trace.example.com/batch/1?code=abc", "https://平台.example/批次"} {
		if err := validateExternalTrace(&ExternalTrace{Platform: "平台", URL: url}); err != nil {
			t.Fatal(url, err)
		}
	}
	for _, trace := range []ExternalTrace{
		{Platform: "平台", URL: "javascript:alert(1)"},
		{Platform: "平台", URL: "http://example.com"},
		{Platform: "平台", URL: "https://user:password@example.com"},
		{Platform: "平台", URL: "https://"},
		{Platform: "平台", URL: "https://example.com/a b"},
		{URL: "https://example.com"},
	} {
		if validateExternalTrace(&trace) == nil {
			t.Fatalf("accepted invalid trace: %+v", trace)
		}
	}
}
