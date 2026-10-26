package app

import (
	"testing"
	"time"

	"github.com/egoist/mygo/ui"

	"github.com/solidSpoon/tern-go/internal/model"
	"github.com/solidSpoon/tern-go/internal/pi"
)

// yOf returns the top of the first element showing text.
func yOf(t *testing.T, tt *ui.Tester, text string) float32 {
	t.Helper()
	box, ok := tt.Find(text)
	if !ok {
		t.Fatalf("nothing shows %q", text)
	}
	return box.Y
}

// Transient states — starting, working, failing, downloading — are shown in
// reserved slots. They must not move the page around them.
func TestTransientStatesKeepLayout(t *testing.T) {
	now := time.Now().UTC().Format(time.RFC3339)

	t.Run("threads", func(t *testing.T) {
		app := newTestApp(t)
		app.router.Push("/threads")
		app.threads = []model.ThreadSummary{{
			ID: "a1", URL: "https://example.com/story", Title: "旧文",
			State: model.ThreadIdle, UpdatedAt: now,
		}}
		tt := ui.NewTester(app.view, 1000, 720)
		quiet := yOf(t, tt, "旧文")

		app.starting = true
		tt.Frame()
		if y := yOf(t, tt, "旧文"); y != quiet {
			t.Errorf("starting moved the list by %.1f DIPs", y-quiet)
		}

		app.starting = false
		app.startErr = "that does not look like a web address"
		tt.Frame()
		if y := yOf(t, tt, "旧文"); y != quiet {
			t.Errorf("an error moved the list by %.1f DIPs", y-quiet)
		}
	})

	t.Run("settings", func(t *testing.T) {
		app := newTestApp(t)
		app.router.Push("/settings")
		tt := ui.NewTester(app.view, 1000, 720)
		quiet := yOf(t, tt, "Pi 运行时")

		app.settingsMsg = "正在请求模型…"
		tt.Frame()
		if y := yOf(t, tt, "Pi 运行时"); y != quiet {
			t.Errorf("a message moved the sections by %.1f DIPs", y-quiet)
		}

		app.settingsMsg = ""
		app.install = pi.Progress{State: "downloading", Progress: 0.4, Message: "downloading 12.0 / 31.0 MB"}
		tt.Frame()
		if y := yOf(t, tt, "Pi 运行时"); y != quiet {
			t.Errorf("the download moved the sections by %.1f DIPs", y-quiet)
		}
	})

	t.Run("feed", func(t *testing.T) {
		app := newTestApp(t)
		app.router.Push("/")
		app.articles = []model.ArticleSummary{{
			ID: "inkwell", Title: model.Bilingual{ZH: "墨水瓶", EN: "Inkwell"},
			Summary: model.Bilingual{ZH: "摘要"}, CreatedAt: now,
		}}
		tt := ui.NewTester(app.view, 1000, 720)
		quiet := yOf(t, tt, "墨水瓶")

		app.articlesErr = "the library is unreadable"
		tt.Frame()
		if y := yOf(t, tt, "墨水瓶"); y != quiet {
			t.Errorf("a refresh error moved the list by %.1f DIPs", y-quiet)
		}
	})
}
