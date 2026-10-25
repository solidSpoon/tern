package app

import (
	"fmt"
	"strings"

	"github.com/egoist/mygo"
	"github.com/egoist/mygo/ui"
)

// readerState is the reading pane's part of the app: one web view, which shows
// the same page the phone loads, under a native toolbar.
//
// The article is the only part of Tern written as a web page. It is embedded in
// the window (ui.WebView) rather than opened in a window of its own, so the
// sidebar and the toolbar stay native and the reader is one page among the
// app's pages.

var readerModes = []string{"dual", "zh", "en"}
var readerSizes = []string{"sm", "md", "lg"}

func (app *App) readerModeName() string {
	return readerModes[clampIndex(app.readerMode, len(readerModes))]
}
func (app *App) readerSizeName() string {
	return readerSizes[clampIndex(app.readerSize, len(readerSizes))]
}

func clampIndex(index, length int) int {
	if index < 0 {
		return 0
	}
	if index >= length {
		return length - 1
	}
	return index
}

// readerPage is the article page: a native toolbar over the embedded reader.
func (app *App) readerPage(c *ui.Context, id string) {
	t := c.Theme()

	if id != app.readerID {
		app.readerID = id
		app.readerTitle = ""
		app.readerSource = ""
		app.readerErr = ""
		app.loadReader(id)
	}

	ui.Column(c).Fill().Children(func() {
		bar := ui.Row(c).Padding(8, 24, 8).Gap(10).AlignItems(ui.Center).DragWindow()
		bar.Children(func() {
			app.readingControls(c)
		})
		ui.Divider(c)

		if app.readerErr != "" || app.reader == nil {
			detail := app.readerErr
			if detail == "" {
				detail = "这个窗口不能嵌入网页（系统 webview 不可用）。可以在浏览器里读这一篇。"
			}
			emptyState(c, "打不开这篇", detail, func() {
				ui.Row(c).Gap(8).Children(func() {
					if ui.PrimaryButton(c, "在浏览器打开").Clicked() {
						c.OpenURL(app.readerURL(app.readerID, false))
					}
					if ui.Button(c, "重试").Clicked() {
						app.readerID = ""
					}
				})
			})
			return
		}
		if app.readerTitle == "" {
			ui.Column(c).Fill().Center().Gap(8).Children(func() {
				ui.Spinner(c)
				ui.Text(c, "正在打开阅读页…").FontSize(12).TextColor(t.TextMuted)
			})
			return
		}

		ui.WebView(c, app.reader).Grow(1)
	})
}

// segmentedChoice is the reading modes' switch with Tern's look: the chosen
// segment is filled with the ink color, as the reading page draws it, rather
// than the theme's barely visible pressed surface.
func segmentedChoice(c *ui.Context, key any, value *int, labels ...string) bool {
	t := c.Theme()
	parts := ui.SegmentedBase(c.Key(key), value, len(labels))
	parts.Track.Padding(2).Gap(2).Radius(999).
		Background(t.Background).Border(1, t.Border).
		AlignItems(ui.Center).
		Children(func() {
			for i, label := range labels {
				chosen := i == *value
				segment := parts.Segment(i).Padding(3, 11).Radius(999)
				if chosen {
					segment.Background(t.Text)
				}
				segment.Children(func() {
					text := ui.Text(c, label).FontSize(11.5)
					if chosen {
						text.TextColor(t.Background)
					} else {
						text.TextColor(t.TextMuted)
					}
				})
			}
		})
	return parts.Track.Changed()
}

// readingControls are the reading page's controls, drawn in the window's title
// bar: one bar of chrome, not two.
func (app *App) readingControls(c *ui.Context) {
	t := c.Theme()

	if ui.Button(c.Key("reader-back"), "← 文章").Clicked() {
		app.router.Push("/")
	}

	ui.Column(c).Grow(1).MinWidth(120).Gap(0).Children(func() {
		title := app.readerTitle
		if title == "" {
			title = "阅读"
		}
		ui.Text(c, title).
			Font("Iowan Old Style, Songti SC, Palatino").
			FontSize(12.5).Bold().SingleLine()
		if app.readerSource != "" {
			ui.Text(c, app.readerSource).FontSize(9.5).TextColor(t.TextMuted).SingleLine()
		}
	})

	if segmentedChoice(c, "reader-mode", &app.readerMode, "对照", "译文", "原文") {
		app.applyReaderMode()
		app.rememberReading()
	}

	ui.Row(c).Gap(4).Children(func() {
		if ui.Button(c.Key("reader-smaller"), "A−").
			Disabled(app.readerSize == 0).
			Clicked() {
			app.readerSize--
			app.applyReaderMode()
			app.rememberReading()
		}
		if ui.Button(c.Key("reader-larger"), "A+").
			Disabled(app.readerSize == len(readerSizes)-1).
			Clicked() {
			app.readerSize++
			app.applyReaderMode()
			app.rememberReading()
		}
	})

	more := ui.Button(c.Key("reader-more"), "⋯")
	more.Label("更多")
	more.Tooltip("在浏览器中阅读，或打开原文出处")
	more.Menu(func(m *ui.Menu) {
		if m.Item("在浏览器中阅读").Chosen() {
			c.OpenURL(app.readerURL(app.readerID, false))
		}
		if app.readerSourceURL != "" && m.Item("打开原文出处").Chosen() {
			c.OpenURL(app.readerSourceURL)
		}
	})
}

// readerURL is the page as the embedded reader or a browser sees it. The
// embedded page gets embed=1, so it drops its masthead and toolbar: the native
// ones above it do that work.
func (app *App) readerURL(id string, embedded bool) string {
	url := strings.TrimSuffix(app.localURL, "/") + "/a/" + id
	if embedded {
		url += fmt.Sprintf("?embed=1&mode=%s&size=%s", app.readerModeName(), app.readerSizeName())
	}
	return url
}

func (app *App) loadReader(id string) {
	go func() {
		article, err := app.store.Article(id)
		app.refresh(func() {
			if err != nil {
				app.readerErr = "no such article"
				return
			}
			app.readerTitle = article.Title.ZH
			app.readerSource = article.Source.Site
			app.readerSourceURL = article.Source.URL
			app.showReader(id)
		})
	}()
}

// showReader loads the page into the web view. Loading a page per article keeps
// the reader identical to the phone's, and the native toolbar above it stays.
func (app *App) showReader(id string) {
	if app.reader == nil {
		app.readerErr = "the web view could not be created"
		return
	}
	if err := app.reader.Page().LoadURL(app.readerURL(id, true)); err != nil {
		app.readerErr = err.Error()
	}
}

// applyReaderMode tells the embedded page what the native toolbar chose. The
// page may still be loading, so it also re-applies the state when it finishes.
func (app *App) applyReaderMode() {
	go func() {
		logError("reading preferences", app.settings.SaveReading(app.readerModeName(), app.readerSizeName()))
	}()
	if app.reader == nil {
		return
	}
	script := fmt.Sprintf(
		"if (window.tern) { window.tern.setMode(%q); window.tern.setSize(%q); }",
		app.readerModeName(), app.readerSizeName(),
	)
	if _, err := app.reader.Page().Eval(script); err != nil {
		logError("reading page", err)
	}
}

// rememberReading keeps the desktop's choice for the next launch, where the
// page's localStorage is not what decides.
func (app *App) rememberReading() {
	mode, size := app.readerModeName(), app.readerSizeName()
	go func() { logError("reading preferences", app.settings.SaveReading(mode, size)) }()
}

// bindReader installs the hooks the web view needs once: the window keeps its
// own title, and the page is told the current mode after every load.
func (app *App) bindReader(view *mygo.WebView) {
	view.Page().OnPageTitleUpdated(func(e *mygo.TitleEvent) { e.PreventDefault() })
	view.Page().OnDidFinishLoad(func() {
		app.refresh(func() { app.applyReaderMode() })
	})
}
