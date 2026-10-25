package pi

import (
	"strings"
	"testing"
)

func TestUserPathFindsUserTools(t *testing.T) {
	path := userPath()
	for _, want := range []string{".local/bin", "/opt/homebrew/bin", "/usr/local/bin"} {
		if !strings.Contains(path, want) {
			t.Errorf("userPath() = %q, want it to include %q", path, want)
		}
	}
}

func TestWithoutPath(t *testing.T) {
	env := withoutPath([]string{"PATH=/usr/bin:/bin", "HOME=/Users/x", "PATH2=/tmp"})
	for _, kv := range env {
		if strings.HasPrefix(kv, "PATH=") {
			t.Fatalf("withoutPath left %q", kv)
		}
	}
	if len(env) != 2 {
		t.Fatalf("withoutPath kept %d entries, want 2: %v", len(env), env)
	}
}
