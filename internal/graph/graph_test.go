package graph

import (
	"strings"
	"testing"
)

func TestStatus(t *testing.T) {
	if Status(map[string]any{"status": "Accepted"}) != "Accepted" {
		t.Fatal("expected Accepted")
	}
	if Status(map[string]any{"status": 202.0}) != "" || Status(nil) != "" {
		t.Fatal("expected empty")
	}
}

func TestErrorMessage(t *testing.T) {
	got := ErrorMessage(map[string]any{"error": map[string]any{"message": "  Access\n denied  "}})
	if got != "Access denied" {
		t.Fatalf("got %q", got)
	}
}

func TestErrorMessageTruncatesFallback(t *testing.T) {
	got := ErrorMessage(map[string]any{"unexpected": strings.Repeat("x", 200)})
	if len([]rune(got)) != 120 {
		t.Fatalf("len %d", len([]rune(got)))
	}
}

func TestErrorMessagePrimitive(t *testing.T) {
	if got := ErrorMessage("service unavailable"); got != `"service unavailable"` {
		t.Fatalf("got %q", got)
	}
}

func TestResolveURL(t *testing.T) {
	if u, err := resolveURL("/x"); err != nil || u != BaseURL+"/x" {
		t.Fatal(u, err)
	}
	if _, err := resolveURL("https://graph.microsoft.com/v1.0/next?$skiptoken=1"); err != nil {
		t.Fatal(err)
	}
	for _, bad := range []string{
		"https://evil.example/x",
		"http://graph.microsoft.com/x",
		"https://graph.microsoft.com.evil.example/x",
		"https://graph.microsoft.com@evil.example/x",
	} {
		if _, err := resolveURL(bad); err == nil {
			t.Fatal("expected error for", bad)
		}
	}
}
