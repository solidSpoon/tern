package app

import (
	"os"
	"testing"

	"github.com/egoist/mygo/ui"

	"github.com/solidSpoon/tern-go/internal/config"
	"github.com/solidSpoon/tern-go/internal/pi"
	"github.com/solidSpoon/tern-go/internal/publish"
	"github.com/solidSpoon/tern-go/internal/store"
	"github.com/solidSpoon/tern-go/internal/threads"
)

// newTestApp is an app on a temporary read: enough to build every page.
func newTestApp(t *testing.T) *App {
	t.Helper()
	root := t.TempDir()
	settings, err := config.Load(root)
	if err != nil {
		t.Fatal(err)
	}
	library, err := store.New(settings.Paths())
	if err != nil {
		t.Fatal(err)
	}
	runtime := pi.NewRuntime(settings.Paths())
	agent := pi.NewAgent(settings, runtime)
	importer := &publish.Importer{Store: library, Assets: settings.AssetsDir}
	mgr, err := threads.New(library, agent, settings, importer, 0)
	if err != nil {
		t.Fatal(err)
	}
	return New(settings, library, agent, runtime, mgr, "http://127.0.0.1:0", "http://127.0.0.1:0", 8787, nil)
}

// The reader is a native toolbar over the embedded page; the toolbar starts in
// the comparison mode and follows a click.
func TestReaderToolbar(t *testing.T) {
	app := newTestApp(t)
	app.router.Push("/a/missing")

	tt := ui.NewTester(app.view, 1000, 700)
	if !tt.HasText("对照") || !tt.HasText("译文") || !tt.HasText("原文") {
		t.Fatalf("the reading modes are missing: %v", tt.Texts())
	}
	if app.readerMode != 0 {
		t.Fatalf("the reader starts in mode %d, want 0 (comparison)", app.readerMode)
	}

	// The first segment is the chosen one: its pill is filled, the others are
	// not.
	img := tt.Image()
	dark := func(word string) bool {
		box, ok := tt.Find(word)
		if !ok {
			t.Fatalf("no box for %q", word)
		}
		// Sample the pill's background, just left of the label.
		x := int(box.X) - 5
		y := int(box.Y + box.H/2)
		r, g, b, _ := img.At(x, y).RGBA()
		return r < 0x6000 && g < 0x6000 && b < 0x6000
	}
	if !dark("对照") {
		t.Error("the comparison segment is not the chosen one")
	}
	if dark("原文") {
		t.Error("the original-text segment looks chosen")
	}

	if err := tt.Click("译文"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	if app.readerMode != 1 {
		t.Fatalf("clicking 译文 set mode %d", app.readerMode)
	}
}

func TestReaderURL(t *testing.T) {
	app := newTestApp(t)
	app.readerMode = 2
	app.readerSize = 0
	url := app.readerURL("ink", true)
	if want := "http://127.0.0.1:0/a/ink?embed=1&mode=en&size=sm"; url != want {
		t.Errorf("embedded URL = %q, want %q", url, want)
	}
	plain := app.readerURL("ink", false)
	if want := "http://127.0.0.1:0/a/ink"; plain != want {
		t.Errorf("browser URL = %q, want %q", plain, want)
	}
	if _, err := os.Stat(app.store.Paths().Library); err != nil {
		t.Fatal(err)
	}
}
