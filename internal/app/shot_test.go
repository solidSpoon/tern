package app

import (
	"image/png"
	"os"
	"testing"
	"time"

	"github.com/egoist/mygo/ui"

	"github.com/solidSpoon/tern-go/internal/model"
	"github.com/solidSpoon/tern-go/internal/pi"
)

// TestSnapshot writes a frame of every native page to /tmp/tern-shots for a
// visual check. Set TERN_SHOTS=1 to run it.
func TestSnapshot(t *testing.T) {
	if os.Getenv("TERN_SHOTS") != "1" {
		t.Skip("set TERN_SHOTS=1 to write frames")
	}
	if err := os.MkdirAll("/tmp/tern-shots", 0o755); err != nil {
		t.Fatal(err)
	}

	now := time.Now().UTC().Format(time.RFC3339)
	pages := []struct {
		name  string
		path  string
		setup func(app *App)
	}{
		{"feed", "/", func(app *App) {
			app.articles = []model.ArticleSummary{
				{
					ID: "inkwell", Title: model.Bilingual{ZH: "墨水瓶", EN: "Inkwell"},
					Summary: model.Bilingual{ZH: "墨水瓶是盛放墨水的小容器，多由玻璃、瓷器、银等制成。它源自古埃及，20 世纪中叶逐渐被钢笔取代。", EN: "An inkwell is a small jar for ink."},
					Source:  model.Source{Site: "Wikipedia", PublishedAt: now},
					Tags:    []string{"墨水瓶"}, CreatedAt: now,
					ParagraphCount: 12, WordCount: 900, Vocab: make([]model.Vocab, 8),
				},
				{
					ID: "palimpsest", Title: model.Bilingual{ZH: "重写本", EN: "Palimpsest"},
					Summary: model.Bilingual{ZH: "重写本指刮去或洗掉旧字迹后重新书写的羊皮纸手稿。"},
					Source:  model.Source{Site: "Wikipedia", PublishedAt: now},
					Tags:    []string{"重写本"}, CreatedAt: now,
					ParagraphCount: 7, WordCount: 520, Vocab: make([]model.Vocab, 6),
				},
			}
		}},
		{"reader", "/a/inkwell", func(app *App) {
			app.readerTitle = "墨水瓶"
			app.readerSource = "Wikipedia"
		}},
		{"threads", "/threads", func(app *App) {
			app.threads = []model.ThreadSummary{
				{ID: "a1", URL: "https://en.wikipedia.org/wiki/Inkwell", Title: "墨水瓶", State: model.ThreadIdle, Article: "inkwell", UpdatedAt: now},
				{ID: "b2", URL: "https://en.wikipedia.org/wiki/Palimpsest", State: model.ThreadRunning, UpdatedAt: now},
				{ID: "c3", URL: "https://example.com/old-story", State: model.ThreadError, UpdatedAt: now},
			}
		}},
		{"vocab", "/vocab", func(app *App) {
			app.vocab = []model.Vocab{
				{Term: "palimpsest", Phonetic: "ˈpælɪmpsest", Pos: "n", ZH: "重写本", EN: "a manuscript written over an erased one",
					Example: &model.Example{EN: "The parchment was reused as a palimpsest.", ZH: "这张羊皮纸被当作重写本再次使用。"}},
				{Term: "vellum", Pos: "n", ZH: "犊皮纸", EN: "fine parchment from calfskin"},
			}
		}},
		{"settings", "/settings", func(app *App) {
			app.install = pi.Progress{State: "done", Progress: 1, Message: "installed and switched to Pi 1.4.0"}
		}},
	}

	// The window's narrowest width, where the reader toolbar is tightest.
	t.Run("reader-narrow", func(t *testing.T) {
		app := newTestApp(t)
		app.router.Push("/a/inkwell")
		app.readerID = "inkwell"
		app.readerTitle = "墨水瓶：从炭黑到苯胺的一个很长的标题"
		app.readerSource = "Wikipedia"
		app.readerSourceURL = "https://en.wikipedia.org/wiki/Inkwell"
		tt := ui.NewTester(app.view, 820, 620)
		tt.Frame()
		file, err := os.Create("/tmp/tern-shots/native-reader-narrow.png")
		if err != nil {
			t.Fatal(err)
		}
		if err := png.Encode(file, tt.Image()); err != nil {
			t.Fatal(err)
		}
		file.Close()
	})

	for _, page := range pages {
		page := page
		t.Run(page.name, func(t *testing.T) {
			app := newTestApp(t)
			app.router.Push(page.path)
			page.setup(app)
			tt := ui.NewTester(app.view, 1000, 720)
			tt.Frame()
			file, err := os.Create("/tmp/tern-shots/native-" + page.name + ".png")
			if err != nil {
				t.Fatal(err)
			}
			if err := png.Encode(file, tt.Image()); err != nil {
				t.Fatal(err)
			}
			if err := file.Close(); err != nil {
				t.Fatal(err)
			}
		})
	}
}
