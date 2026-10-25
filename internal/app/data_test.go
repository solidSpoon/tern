package app

import (
	"os"
	"path/filepath"
	"testing"
)

// Pointing the app at another data directory moves the store, the
// conversations and the settings there, and writes the config in the new root.
func TestSwitchDataRoot(t *testing.T) {
	app := newTestApp(t)
	dir := t.TempDir()

	paths, err := app.settings.SwitchRoot(dir)
	if err != nil {
		t.Fatal(err)
	}
	if err := app.store.SetPaths(paths); err != nil {
		t.Fatal(err)
	}
	if err := app.mgr.SetPaths(paths); err != nil {
		t.Fatal(err)
	}

	if got := app.store.Paths().Root; got != dir {
		t.Errorf("store root = %q, want %q", got, dir)
	}
	if got := app.settings.Paths().Root; got != dir {
		t.Errorf("settings root = %q, want %q", got, dir)
	}
	if got := app.settings.Paths().Library; got != filepath.Join(dir, "data", "library") {
		t.Errorf("library = %q", got)
	}
	if got := app.settings.AssetsDir(); got != filepath.Join(dir, "media") {
		t.Errorf("media = %q", got)
	}
	if _, err := os.Stat(filepath.Join(dir, "data", "config.json")); err != nil {
		t.Errorf("the config was not written in the new root: %v", err)
	}
	if got := len(app.store.ListArticles()); got != 0 {
		t.Errorf("the new root shows %d articles", got)
	}
}
