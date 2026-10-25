package threads

import (
	"testing"

	"github.com/solidSpoon/tern-go/internal/config"
	"github.com/solidSpoon/tern-go/internal/model"
)

func TestValidURL(t *testing.T) {
	for _, ok := range []string{
		"https://en.wikipedia.org/wiki/Inkwell",
		"http://example.com/a?b=1#c",
		" https://example.com ",
	} {
		if _, err := ValidURL(ok); err != nil {
			t.Errorf("ValidURL(%q) = %v, want a link", ok, err)
		}
	}
	for _, bad := range []string{"", "example.com", "file:///etc/passwd", "http://localhost:8787"} {
		if _, err := ValidURL(bad); err == nil {
			t.Errorf("ValidURL(%q) was accepted", bad)
		}
	}
}

func TestSetPathsRefusesWhileBusy(t *testing.T) {
	m := &Manager{busy: map[string]bool{"t1": true}, live: map[string]*model.Thread{}}
	if err := m.SetPaths(config.Paths{Threads: t.TempDir()}); err == nil {
		t.Fatal("switching while a conversation runs was allowed")
	}
}
