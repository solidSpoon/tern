package pi

import (
	"strings"
	"testing"
)

func TestIngestPrompt(t *testing.T) {
	prompt := IngestPrompt("https://example.com/story", "/tmp/work")
	for _, want := range []string{
		"https://example.com/story",
		"/tmp/work",
		"`ego-browser skill`",
		"`ego-browser nodejs < script.mjs`",
		"article.json",
		"publish_article",
		"1:1",
		"vocab",
		"command -v ego-browser",
		"never scan the disk",
	} {
		if !strings.Contains(prompt, want) {
			t.Errorf("the prompt does not mention %q", want)
		}
	}
	if strings.Contains(prompt, "glossary") {
		t.Error("the prompt still asks for a glossary")
	}
	if strings.Contains(prompt, "\\`") {
		t.Error("the prompt escaped a backtick")
	}
}

func TestExplainModelError(t *testing.T) {
	cases := map[string]string{
		"Insufficient balance": "out of credit",
		"401 Unauthorized":     "invalid or expired",
		"429 rate limit":       "rate limiting",
		"something unexpected": "the model call failed",
	}
	for raw, want := range cases {
		if got := ExplainModelError(raw); !strings.Contains(got, want) {
			t.Errorf("ExplainModelError(%q) = %q, want %q", raw, got, want)
		}
	}
}
