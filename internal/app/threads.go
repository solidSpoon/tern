package app

import (
	"encoding/json"
	"fmt"
	"path/filepath"
	"strings"

	"github.com/egoist/mygo/ui"
	"github.com/egoist/mygo/ui/markdown"

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
			app.applyThread(thread)
			app.loadThreads()
			app.router.Push("/t/" + thread.ID)
		})
	}()
}

// ------------------------------------------------------------------ chat

// chatItem is one thing the conversation list shows: an event, or a run of tool
// calls merged into chips.
type chatItem struct {
	kind  string // "event" or "tools"
	event model.ThreadEvent
	tools []toolChip
}

type toolChip struct {
	key    string
	tool   string
	hint   string
	detail string
	status string
	count  int
}

// groupEvents merges consecutive tool calls into one line and, inside it,
// repeated calls of the same tool on the same thing into "read x3".
func groupEvents(events []model.ThreadEvent) []chatItem {
	items := make([]chatItem, 0, len(events))
	var run []toolChip

	flush := func() {
		if len(run) > 0 {
			items = append(items, chatItem{kind: "tools", tools: run})
			run = nil
		}
	}

	for _, event := range events {
		if event.Type != model.EventTool {
			flush()
			items = append(items, chatItem{kind: "event", event: event})
			continue
		}
		hint := shortHint(event.Tool, event.Args)
		if event.Detail != "" {
			hint = clipText(strings.Split(event.Detail, "\n")[0], 40)
		}
		status := "ok"
		if event.Status == "error" {
			status = "error"
		}
		if n := len(run); n > 0 && run[n-1].tool == event.Tool && run[n-1].hint == hint {
			run[n-1].count++
			if status == "error" {
				run[n-1].status = "error"
			}
			continue
		}
		run = append(run, toolChip{
			key:    event.ID,
			tool:   event.Tool,
			hint:   hint,
			detail: argHint(event.Tool, event.Args),
			status: status,
			count:  1,
		})
	}
	flush()
	return items
}

func argHint(tool, args string) string {
	if args == "" {
		return ""
	}
	var parsed map[string]any
	raw := args
	if json.Unmarshal([]byte(args), &parsed) == nil {
		for _, key := range []string{"command", "path", "pattern"} {
			if text, ok := parsed[key].(string); ok && text != "" {
				raw = text
				break
			}
		}
	}
	return strings.TrimSpace(strings.Split(raw, "\n")[0])
}

// shortHint keeps the telling bit of a call: the first word of a command, or a
// file's name.
func shortHint(tool, args string) string {
	raw := argHint(tool, args)
	if raw == "" {
		return ""
	}
	if tool == "bash" {
		command := raw
		if index := strings.Index(command, "&&"); index >= 0 {
			command = strings.TrimSpace(command[index+2:])
		}
		fields := strings.Fields(command)
		if len(fields) == 0 {
			return ""
		}
		first := strings.Trim(fields[0], `"'`)
		return clipText(filepath.Base(first), 16)
	}
	cleaned := strings.ReplaceAll(raw, "\\", "/")
	parts := strings.Split(cleaned, "/")
	for i := len(parts) - 1; i >= 0; i-- {
		if trimmed := strings.TrimSpace(parts[i]); trimmed != "" {
			return clipText(trimmed, 18)
		}
	}
	return ""
}

func clipText(s string, n int) string {
	runes := []rune(s)
	if len(runes) <= n {
		return s
	}
	return string(runes[:n])
}

func (app *App) chatPage(c *ui.Context, id string) {
	t := c.Theme()

	if app.threadID != id {
		app.threadID = id
		app.thread = nil
		app.chatItems = nil
		app.chatErr = ""
		app.pollMu.Lock()
		app.pollID = ""
		app.pollState = model.ThreadIdle
		app.pollMu.Unlock()
		app.loadThread(id)
	}

	thread := app.thread
	ui.Column(c).Fill().Children(func() {
		// Header
		bar := ui.Row(c).Padding(16, 28, 12).Gap(12).AlignItems(ui.Center).DragWindow()
		bar.Children(func() {
			if ui.Button(c.Key("back"), "← 对话").Clicked() {
				app.router.Push("/threads")
			}
			ui.Column(c).Grow(1).Gap(1).Children(func() {
				if thread != nil {
					ui.Text(c, hostOf(thread.URL)).FontSize(13).Bold().SingleLine()
					ui.Text(c, thread.URL).FontSize(10.5).TextColor(t.TextMuted).SingleLine()
				} else {
					ui.Text(c, "对话").FontSize(13).Bold()
				}
			})
			if thread != nil && thread.Article != "" {
				if ui.PrimaryButton(c.Key("read"), "阅读全文 →").Clicked() {
					app.router.Push("/a/" + thread.Article)
				}
			}
		})
		ui.Divider(c)

		// Conversation. Loading and failing keep the same frame: only what
		// fills the list area changes, never whether the composer exists.
		switch {
		case app.chatErr != "":
			ui.Column(c).Grow(1).Center().Padding(40).Children(func() {
				ui.Text(c, "打不开这个对话").FontSize(17).Bold()
				ui.Text(c, app.chatErr).FontSize(12).TextColor(t.Danger).TextAlign(ui.Center).MaxWidth(420)
			})
		case thread == nil:
			ui.Column(c).Grow(1).Center().Gap(8).Children(func() {
				ui.Spinner(c)
				ui.Text(c, "读取对话…").FontSize(12).TextColor(t.TextMuted)
			})
		default:
			app.chat.Key = func(row int) any { return app.chatItems[row].key() }
			app.chat.FollowEnd = true
			app.chat.FollowAnimation = &ui.Smooth
			ui.List(c, &app.chat, len(app.chatItems), func(row int) {
				app.chatRow(c, app.chatItems[row])
			}).Grow(1).Padding(18, 28)
		}

		// The status strip is always here: working, failed, or quiet.
		status, color, busy := "", t.TextMuted, false
		if thread != nil {
			switch {
			case thread.State == model.ThreadRunning:
				status, color, busy = "Agent 正在工作…", t.TextMuted, true
			case thread.State == model.ThreadError && thread.Error != "":
				status, color = thread.Error, t.Danger
			}
		}
		ui.Row(c).Padding(0, 28, 0).Children(func() {
			statusLine(c, 22, status, color, busy)
		})

		// Composer: always built, disabled while there is nothing to send to.
		ui.Divider(c)
		ui.Row(c).Padding(12, 28, 16).Gap(10).AlignItems(ui.End).Children(func() {
			placeholder := "继续提要求，例如：把第三段翻得更口语一点"
			switch {
			case thread == nil:
				placeholder = "读取对话…"
			case thread.State == model.ThreadRunning:
				placeholder = "Agent 干活中……"
			case thread.Article == "":
				placeholder = "补充说明，或让它继续"
			}
			disabled := thread == nil || thread.State == model.ThreadRunning || app.sending
			ui.TextArea(c, &app.draft).
				Lines(1, 5).
				Grow(1).
				Placeholder(placeholder).
				Disabled(disabled)
			ui.PrimaryButton(c.Key("send"), "发送").
				Disabled(disabled || strings.TrimSpace(app.draft) == "").
				OnClick(func() { app.sendDraft() })
		})
		if c.Shortcut(ui.Cmd, ui.KeyEnter) {
			app.sendDraft()
		}
	})
}

func (app *App) chatRow(c *ui.Context, item chatItem) {
	t := c.Theme()
	if item.kind == "tools" {
		row := ui.Row(c).Wrap().FillWidth().GapX(14).GapY(2).Padding(2, 0).Margin(0, 0, 12, 0)
		row.Children(func() {
			for _, chip := range item.tools {
				color := t.TextMuted
				if chip.status == "error" {
					color = t.Danger
				}
				label := chip.tool
				if chip.hint != "" {
					label += " " + chip.hint
				}
				if chip.count > 1 {
					label += fmt.Sprintf(" ×%d", chip.count)
				}
				element := ui.Text(c, "· "+label).
					Font("ui-monospace, SFMono-Regular, Menlo, monospace").
					FontSize(10.5).TextColor(color).SingleLine()
				if chip.detail != "" {
					element.Tooltip(chip.detail)
				}
			}
		})
		return
	}

	event := item.event
	switch event.Type {
	case model.EventUser:
		ui.Column(c).Gap(4).Padding(0, 0, 10, 0).Children(func() {
			ui.Text(c, "你").FontSize(10).TextColor(t.TextMuted)
			ui.Column(c).MaxWidth(560).AlignSelf(ui.Start).
				Padding(10, 14).Radius(10).Background(t.Surface).
				Children(func() {
					ui.Text(c, event.Text).FontSize(13).TextColor(t.Text).LineHeight(1.5)
				})
		})

	case model.EventAssistant:
		ui.Column(c).Gap(5).Padding(0, 0, 14, 0).Children(func() {
			ui.Text(c, "Tern").FontSize(10).TextColor(t.TextMuted)
			ui.Column(c).MaxWidth(640).AlignSelf(ui.Start).Children(func() {
				markdown.View(c, event.Text, &app.md)
			})
		})

	case model.EventStatus:
		color := t.TextMuted
		if strings.HasPrefix(event.Text, "失败") {
			color = t.Danger
		}
		ui.Text(c, event.Text).FontSize(11.5).TextColor(color).
			Padding(0, 0, 10, 0).LineHeight(1.5)
	}
}

// key names a chat item in the list, so rows keep their state as events come.
func (item chatItem) key() string {
	if item.kind == "tools" {
		if len(item.tools) > 0 {
			return "tools-" + item.tools[0].key
		}
		return "tools"
	}
	return item.event.ID + item.event.At
}

func (app *App) sendDraft() {
	text := strings.TrimSpace(app.draft)
	if text == "" || app.sending || app.thread == nil {
		return
	}
	if app.thread.State == model.ThreadRunning {
		return
	}
	id := app.thread.ID
	app.sending = true
	app.draft = ""
	app.chatErr = ""
	go func() {
		thread, err := app.mgr.PostMessage(id, text)
		app.refresh(func() {
			app.sending = false
			if err != nil {
				app.chatErr = err.Error()
				return
			}
			if app.threadID == id {
				app.applyThread(thread)
			}
		})
	}()
}
