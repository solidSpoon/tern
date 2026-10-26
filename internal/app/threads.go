package app

import (
	"strings"

	"github.com/egoist/mygo/ui"

	"github.com/solidSpoon/tern-go/internal/model"
)

// ------------------------------------------------------------------ list page

func (app *App) threadsPage(c *ui.Context) {
	t := c.Theme()
	ui.Column(c).Fill().Children(func() {
		head := ui.Column(c).Padding(24, 28, 16).Gap(14).DragWindow()
		head.Children(func() {
			pageHeader(c, "对话", "贴一个链接", "一个链接开一个对话。Agent 会打开页面、抽出正文、逐段翻译并入库，之后你可以在这个对话里继续提要求。")

			ui.Row(c).Gap(8).Children(func() {
				if ui.TextInput(c, &app.newURL).
					Placeholder("https://example.com/article").
					Label("网页链接").
					Submitted() {
					app.startThread()
				}
				ui.PrimaryButton(c.Key("start"), "开始").
					Disabled(app.starting || strings.TrimSpace(app.newURL) == "").
					OnClick(func() { app.startThread() })
			})

			// The status line is always there: starting, failing, or empty.
			status := app.startErr
			color := t.Danger
			if status == "" && app.starting {
				status, color = "正在开启对话…", t.TextMuted
			}
			statusLine(c, 20, status, color, app.starting && app.startErr == "")
		})

		ui.Divider(c)

		switch {
		case app.threadsErr != "":
			emptyState(c, "读不到对话", app.threadsErr, nil)
		case len(app.threads) == 0:
			emptyState(c, "还没有对话", "贴一个链接试试。", nil)
		default:
			app.threadList.Key = func(row int) any { return app.threads[row].ID }
			app.threadList.Label = func(row int) string {
				thread := app.threads[row]
				if thread.Title != "" {
					return thread.Title
				}
				return hostOf(thread.URL)
			}
			ui.List(c, &app.threadList, len(app.threads), func(row int) {
				app.threadRow(c, app.threads[row])
			}).Grow(1)
		}
	})
}

func (app *App) threadRow(c *ui.Context, thread model.ThreadSummary) {
	t := c.Theme()
	line := ui.Row(c.Key(thread.ID)).Padding(14, 28).Gap(12).AlignItems(ui.Center)
	if line.Hovered() {
		line.Background(t.Surface)
	}
	line.OnClick(func() { app.router.Push("/t/" + thread.ID) })
	line.Children(func() {
		color := t.TextMuted
		switch thread.State {
		case model.ThreadRunning:
			color = t.Accent
		case model.ThreadError:
			color = t.Danger
		}
		ui.Box(c).Size(7, 7).Radius(999).Background(color)

		ui.Column(c).Grow(1).Gap(2).Children(func() {
			title := thread.Title
			if title == "" {
				title = hostOf(thread.URL)
			}
			ui.Text(c, title).FontSize(14.5).Bold().MaxLines(1)
			ui.Text(c, thread.URL).FontSize(11).TextColor(t.TextMuted).MaxLines(1)
		})

		ui.Text(c, stateLabel(thread.State)+" · "+formatWhen(thread.UpdatedAt)).
			FontSize(11).TextColor(t.TextMuted)
	})
	ui.Divider(c).MarginX(28)
}

func (app *App) startThread() {
	url := strings.TrimSpace(app.newURL)
	if url == "" || app.starting {
		return
	}
	app.starting = true
	app.startErr = ""
	go func() {
		thread, err := app.mgr.Create(url)
		app.refresh(func() {
			app.starting = false
			if err != nil {
				app.startErr = err.Error()
				return
			}
			app.newURL = ""
			app.loadThreads()
			app.router.Push("/t/" + thread.ID)
		})
	}()
}

// ------------------------------------------------------------------ chat

// chatWebPage shows one conversation: the page the phone uses, in the window's
// web view, under a native bar that owns the way back and the link to the
// finished article. The page itself has the log and the composer.
func (app *App) chatWebPage(c *ui.Context, id string) {
	t := c.Theme()

	if id != app.chatID {
		app.pollMu.Lock()
		app.chatID = id
		app.pollMu.Unlock()
		app.readerID = ""
		app.chatTitle = ""
		app.chatHost = ""
		app.chatArticle = ""
		app.chatReady = false
		app.chatErr = ""
		app.loadChat(id)
	}

	ui.Column(c).Fill().Children(func() {
		bar := ui.Row(c).Padding(8, 24, 8).Gap(10).AlignItems(ui.Center).DragWindow()
		bar.Children(func() {
			if ui.Button(c.Key("chat-back"), "← 对话").Clicked() {
				app.router.Push("/threads")
			}
			ui.Column(c).Grow(1).MinWidth(120).Gap(0).Children(func() {
				title := app.chatTitle
				if title == "" {
					title = app.chatHost
				}
				if title == "" {
					title = "对话"
				}
				ui.Text(c, title).Font("Iowan Old Style, Songti SC, Palatino").
					FontSize(12.5).Bold().SingleLine()
				if app.chatHost != "" {
					ui.Text(c, app.chatHost).FontSize(9.5).TextColor(t.TextMuted).SingleLine()
				}
			})
			if app.chatArticle != "" {
				if ui.PrimaryButton(c.Key("chat-read"), "阅读全文 →").Clicked() {
					app.router.Push("/a/" + app.chatArticle)
				}
			}
		})
		ui.Divider(c)

		if app.chatErr != "" || app.reader == nil {
			detail := app.chatErr
			if detail == "" {
				detail = "这个窗口不能嵌入网页（系统 webview 不可用）。可以在浏览器里打开这个对话。"
			}
			emptyState(c, "打不开这个对话", detail, func() {
				ui.Row(c).Gap(8).Children(func() {
					if ui.PrimaryButton(c, "在浏览器打开").Clicked() {
						c.OpenURL(app.chatURL(id, false))
					}
					if ui.Button(c, "重试").Clicked() {
						app.chatID = ""
					}
				})
			})
			return
		}
		if !app.chatReady {
			ui.Column(c).Fill().Center().Gap(8).Children(func() {
				ui.Spinner(c)
				ui.Text(c, "正在打开对话…").FontSize(12).TextColor(t.TextMuted)
			})
			return
		}
		ui.WebView(c, app.reader).Grow(1)
	})
}

func (app *App) loadChat(id string) {
	go func() {
		thread, err := app.mgr.Get(id)
		app.refresh(func() {
			if err != nil {
				app.chatErr = err.Error()
				return
			}
			app.chatTitle = thread.Title
			app.chatHost = hostOf(thread.URL)
			app.chatArticle = thread.Article
			app.chatReady = true
			if err := app.showWeb(app.chatURL(id, true)); err != nil {
				app.chatErr = err.Error()
			}
		})
	}()
}

// chatURL is the phone's conversation page; embedded drops its own header, as
// the native bar above has the way back.
func (app *App) chatURL(id string, embedded bool) string {
	url := strings.TrimSuffix(app.localURL, "/") + "/t/" + id
	if embedded {
		url += "?embed=1"
	}
	return url
}
