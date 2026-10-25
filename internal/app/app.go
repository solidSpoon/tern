// Package app is Tern's desktop interface, drawn by MyGo itself: the feed, the
// conversations, the vocabulary book and the settings are native UI, while an
// article opens in a web window — the same reading page the phone loads.
package app

import (
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/egoist/mygo"
	"github.com/egoist/mygo/ui"
	"github.com/egoist/mygo/ui/markdown"

	"github.com/solidSpoon/tern-go/internal/config"
	"github.com/solidSpoon/tern-go/internal/model"
	"github.com/solidSpoon/tern-go/internal/pi"
	"github.com/solidSpoon/tern-go/internal/store"
	"github.com/solidSpoon/tern-go/internal/threads"
)

// App is the window's state: what every page reads.
type App struct {
	settings *config.Settings
	store    *store.Store
	agent    *pi.Agent
	runtime  *pi.Runtime
	mgr      *threads.Manager
	baseURL  string

	win    *mygo.Window
	router *ui.Router

	// feed
	articles    []model.ArticleSummary
	articlesErr string
	feedList    ui.ListState
	covers      map[string]*ui.Bitmap
	coversBusy  map[string]bool

	// conversations
	threads    []model.ThreadSummary
	threadsErr string
	threadList ui.ListState
	newURL     string
	starting   bool
	startErr   string

	// one conversation
	thread    *model.Thread
	threadID  string
	chatItems []chatItem
	chat      ui.ListState
	chatErr   string
	draft     string
	sending   bool
	md        markdown.Options

	// the reading pane: one web view under a native toolbar
	reader          *mygo.WebView
	readerID        string
	readerTitle     string
	readerSource    string
	readerSourceURL string
	readerErr       string
	readerMode      int
	readerSize      int
	localURL        string

	// vocabulary
	vocab     []model.Vocab
	vocabErr  string
	vocabList ui.ListState

	// settings
	dataRoot     string
	port         int
	portText     string
	restart      func() error
	cfg          config.Config
	models       []string
	modelChoice  string
	keyDraft     string
	settingsMsg  string
	settingsTone string
	settingsBusy bool
	runtimeInfo  pi.Status
	install      pi.Progress
	articleCount int

	pollMu    sync.Mutex
	pollID    string
	pollState model.ThreadState
}

// indexOf finds a preference's index, 0 when it is unknown.
func indexOf(list []string, value string) int {
	for i, item := range list {
		if item == value {
			return i
		}
	}
	return 0
}

// New makes the app's state.
func New(settings *config.Settings, st *store.Store, agent *pi.Agent, runtime *pi.Runtime, mgr *threads.Manager, baseURL, localURL string, port int, restart func() error) *App {
	model := settings.Get().Agent.Model
	if model == "" {
		model = config.DefaultMdl
	}
	app := &App{
		modelChoice: model,
		port:        port,
		portText:    fmt.Sprint(port),
		restart:     restart,
		settings:    settings,
		store:       st,
		agent:       agent,
		runtime:     runtime,
		mgr:         mgr,
		baseURL:     baseURL,
		localURL:    localURL,
		router:      ui.NewRouter("/"),
		covers:      map[string]*ui.Bitmap{},
		coversBusy:  map[string]bool{},
	}
	// Pages replace each other at once: a reader flipping through articles does
	// not want a slide, and every page keeps its own scroll and state anyway.
	app.router.Transition = ui.TransitionNone
	// A development aid, and a deep link: TERN_ROUTE=/a/inkwell opens the app
	// on that page.
	if route := os.Getenv("TERN_ROUTE"); route != "" {
		app.router.Push(route)
	}
	return app
}

// useDockIcon gives a run from source the development icon: `go run .` and
// dev.sh have no bundle to carry an icon, while an installed app does. A
// packaged build (`mygo build`, `mygo build -debug`) takes its icon from the
// bundle's icon.png / icon-dev.png, so this leaves it alone.
func (app *App) useDockIcon() {
	if mygo.App.IsPackaged() {
		return
	}
	png, err := os.ReadFile(filepath.Join("assets", "brand", "png", "icon-dev-512.png"))
	if err != nil {
		return // the icon has not been generated yet: keep the generic one
	}
	if err := mygo.App.Dock.SetIcon(png); err != nil {
		logError("dock icon", err)
	}
}

// Run opens the window and runs the app.
func (app *App) Run() error {
	mygo.App.SetName("Tern")
	mygo.App.WhenReady(func() {
		app.useDockIcon()
		app.openWindow()
		app.loadFeed()
		app.loadThreads()
		app.loadVocab()
		app.loadSettings()
		app.startPoller()
	})
	// Tern lives in the Dock: closing the window does not quit the server, so
	// the phone can still read.
	mygo.App.OnWindowAllClosed(func() {})
	mygo.App.OnActivate(func(hasVisibleWindows bool) {
		if !hasVisibleWindows {
			app.openWindow()
		}
	})
	return mygo.App.Run()
}

// openWindow makes the app's window: native pages, and one web view for the
// reading pane, which every article uses.
func (app *App) openWindow() {
	reading := app.settings.Get().Reading
	app.readerMode = indexOf(readerModes, reading.Mode)
	app.readerSize = indexOf(readerSizes, reading.Size)

	win := mygo.NewWindow(mygo.WindowOptions{
		Title:     "Tern · 双语阅读",
		Width:     1120,
		Height:    800,
		MinWidth:  820,
		MinHeight: 560,
		// No title bar is drawn: the traffic lights sit over the sidebar's
		// material, as in Finder or Mail. On Windows and Linux the window
		// buttons take the first 52 DIPs of the top right.
		TitleBarStyle:  mygo.TitleBarHiddenInset,
		TitleBarHeight: sidebarHeaderHeight,
		Vibrancy:       mygo.VibrancySidebar,
		Content:        ui.View(app.view),
	})
	app.win = win

	view, err := win.NewWebView(mygo.WebViewOptions{
		BackgroundColor: "light-dark(#fbfbfb, #111112)",
	})
	if err != nil {
		logError("reading pane", err)
	}
	app.reader = view
	if view != nil {
		app.bindReader(view)
	}
	win.Invalidate()
}

// ------------------------------------------------------------------ shell

// applyTheme gives the native UI Tern's own ink and accent. Backgrounds stay
// the system's: the app should sit on the platform, not on a color of its own.
// The reading page repeats these values in its CSS tokens.
func (app *App) applyTheme(c *ui.Context) {
	theme := *c.Theme()
	if theme.Dark {
		theme.Text = ui.Hex("#f1e7d4")
		theme.TextMuted = ui.Hex("#8fa3a8")
		theme.Accent = ui.Hex("#e8873c")
		theme.AccentText = ui.Hex("#17110c")
	} else {
		theme.Text = ui.Hex("#122a31")
		theme.TextMuted = ui.Hex("#6f8084")
		theme.Accent = ui.Hex("#b4552a")
		theme.AccentText = ui.Hex("#fff8ec")
	}
	theme.AccentHover = theme.Accent.Mix(ui.RGB(255, 255, 255), 0.14)
	theme.AccentPressed = theme.Accent.Mix(ui.RGB(0, 0, 0), 0.16)
	theme.Selection = theme.Accent.Alpha(0.18)
	c.SetTheme(&theme)
}

func (app *App) view(c *ui.Context) {
	app.applyTheme(c)
	t := c.Theme()
	// The window's material shows wherever the view paints no background: the
	// sidebar is see-through, the pages are not.
	vibrant := c.Vibrancy()
	if vibrant {
		c.Root().Background(ui.Transparent)
	}
	ui.Row(c).Fill().AlignItems(ui.Stretch).Children(func() {
		app.sidebar(c)
		ui.Column(c).Grow(1).FillHeight().Background(t.Background).Children(func() {
			app.dragStrip(c)
			app.router.View(c, func(r *ui.Route) {
				switch {
				case r.Match("/"):
					r.Title("文章")
					app.feedPage(c)
				case r.Match("/threads"):
					r.Title("对话")
					app.threadsPage(c)
				case r.Match("/t/{id}"):
					r.Title("对话")
					app.chatPage(c, r.Param("id"))
				case r.Match("/a/{id}"):
					r.Title(app.readerTitle)
					app.readerPage(c, r.Param("id"))
				case r.Match("/vocab"):
					r.Title("生词本")
					app.vocabPage(c)
				case r.Match("/settings"):
					r.Title("设置")
					app.settingsPage(c)
				default:
					ui.Column(c).Fill().Center().Children(func() {
						ui.Text(c, "页面不存在").FontSize(20).Bold()
					})
				}
			})
		})
	})
}

// sidebarHeaderHeight is the room the window controls take at the top of the
// sidebar on macOS, where the wordmark sits beside them.
const sidebarHeaderHeight = 52

// dragStrip is the window's invisible title bar: a transparent band in the
// content area that drags the window, and on Windows and Linux keeps the page
// clear of the window buttons drawn over the window's top right.
func (app *App) dragStrip(c *ui.Context) {
	bar := c.TitleBar()
	// A title bar's own height: dragging and double-clicking anywhere in this
	// band moves or zooms the window, as a real title bar does. On Windows and
	// Linux the band is also tall enough to keep the page clear of the window
	// buttons.
	height := float32(30)
	if bar.Right > 0 && bar.Height > height {
		height = bar.Height
	}
	if height <= 0 {
		return
	}
	ui.Row(c).Height(height).FillWidth().DragWindow()
}

// sectionPath is the sidebar item that matches the page shown: a single
// conversation still belongs to "对话".
func (app *App) sectionPath() string {
	path := app.router.Path()
	switch {
	case strings.HasPrefix(path, "/t/"):
		return "/threads"
	case strings.HasPrefix(path, "/a/"):
		return "/"
	case path == "" || path == "/":
		return "/"
	}
	return path
}

func (app *App) sidebar(c *ui.Context) {
	t := c.Theme()
	vibrant := c.Vibrancy()
	bar := c.TitleBar()
	side := ui.Column(c).Width(220).FillHeight().
		Padding(0, 10, 14).
		Gap(2).
		BorderWidth(0, 1, 0, 0).BorderColor(t.Border)
	if !vibrant {
		side.Background(t.Surface)
	}
	side.Children(func() {
		// Beside the traffic lights, not below a strip of chrome.
		header := ui.Row(c).Height(sidebarHeaderHeight).
			Padding(0, 4, 0, bar.Left+8).
			AlignItems(ui.Center).Gap(8)
		header.DragWindow()
		header.Children(func() {
			ui.Text(c, "Tern").Font("Iowan Old Style, Songti SC, Palatino").FontSize(17).Bold().Italic()
			ui.Text(c, "双语阅读").FontSize(9.5).TextColor(t.TextMuted)
		})

		page := app.sectionPath()
		nav := ui.Sidebar(c, &page, func() {
			ui.SidebarItem(c, "/", nil, "文章")
			ui.SidebarItem(c, "/threads", nil, "对话").Children(func() {
				if app.runningThreads() > 0 {
					ui.Badge(c, fmt.Sprint(app.runningThreads()))
				}
			})
			ui.SidebarItem(c, "/vocab", nil, "生词本").Children(func() {
				if len(app.vocab) > 0 {
					ui.Badge(c, fmt.Sprint(len(app.vocab)))
				}
			})
			ui.SidebarItem(c, "/settings", nil, "设置")
		}).Grow(1)
		// One surface from the wordmark to the footer: the list does not paint
		// its own shorter rectangle behind the items.
		nav.Background(ui.Transparent)
		if nav.Changed() {
			app.router.Push(page)
		}

		ui.Divider(c).Margin(6, 0)
		ui.Column(c).Padding(4, 8).Gap(3).Children(func() {
			ui.Text(c, "手机阅读").FontSize(10).TextColor(t.TextMuted)
			ui.Text(c, strings.TrimPrefix(app.baseURL, "http://")).FontSize(10.5).TextColor(t.TextMuted).SingleLine()
		})
	})
}

// ------------------------------------------------------------------ helpers

func (app *App) runningThreads() int {
	count := 0
	for _, thread := range app.threads {
		if thread.State == model.ThreadRunning {
			count++
		}
	}
	return count
}

// refresh runs fn on the main thread and draws a new frame.
func (app *App) refresh(fn func()) {
	if app.win == nil {
		return
	}
	app.win.Update(fn)
}

func (app *App) loadFeed() {
	go func() {
		list := app.store.ListArticles()
		app.refresh(func() {
			app.articles = list
			app.articlesErr = ""
			for _, article := range list {
				if article.Cover != nil {
					app.prefetchCover(article.Cover.Src)
				}
			}
		})
	}()
}

// prefetchCover decodes a cover off the main thread; the view shows a quiet
// placeholder until it arrives.
func (app *App) prefetchCover(src string) {
	if src == "" || app.covers[src] != nil || app.coversBusy[src] {
		return
	}
	app.coversBusy[src] = true
	go func() {
		bitmap := app.decodeCover(src)
		app.refresh(func() {
			delete(app.coversBusy, src)
			if bitmap != nil {
				app.covers[src] = bitmap
			}
		})
	}()
}

func (app *App) decodeCover(src string) *ui.Bitmap {
	if !strings.HasPrefix(src, "/media/") {
		return nil
	}
	name := strings.TrimPrefix(src, "/media/")
	if strings.Contains(name, "..") {
		return nil
	}
	data, err := os.ReadFile(filepath.Join(app.settings.AssetsDir(), filepath.FromSlash(name)))
	if err != nil {
		return nil
	}
	bitmap, err := ui.DecodeBitmap(data)
	if err != nil {
		return nil
	}
	return bitmap
}

func (app *App) loadThreads() {
	go func() {
		list := app.mgr.List()
		app.refresh(func() {
			app.threads = list
			app.threadsErr = ""
		})
	}()
}

func (app *App) loadVocab() {
	go func() {
		list := app.store.Vocab()
		app.refresh(func() {
			app.vocab = list
			app.vocabErr = ""
		})
	}()
}

func (app *App) loadSettings() {
	go func() {
		cfg := app.settings.Get()
		models := app.agent.Models()
		status := app.runtime.Status()
		progress := app.runtime.Progress()
		agentStatus := app.agent.Status()
		count := len(app.store.ListArticles())
		app.refresh(func() {
			app.cfg = cfg
			app.models = models
			app.runtimeInfo = status
			app.install = progress
			app.articleCount = count
			app.dataRoot = app.settings.Paths().Root
			if app.modelChoice == "" {
				app.modelChoice = agentStatus.Model
			}
		})
	}()
}

// startPoller follows the conversation on screen while the agent works.
func (app *App) startPoller() {
	go func() {
		ticker := time.NewTicker(time.Second)
		defer ticker.Stop()
		for range ticker.C {
			app.pollMu.Lock()
			id, state := app.pollID, app.pollState
			app.pollMu.Unlock()
			if id == "" || state != model.ThreadRunning || app.win == nil {
				continue
			}
			thread, err := app.mgr.Get(id)
			if err != nil {
				continue
			}
			app.refresh(func() {
				if app.threadID == id {
					app.applyThread(thread)
				}
			})
		}
	}()
}

func (app *App) applyThread(thread *model.Thread) {
	app.thread = thread
	app.threadID = thread.ID
	app.chatItems = groupEvents(thread.Events)
	app.chatErr = ""

	app.pollMu.Lock()
	app.pollID = thread.ID
	app.pollState = thread.State
	app.pollMu.Unlock()

	// Keep the list's badges and the feed honest when a round finishes.
	if thread.State != model.ThreadRunning {
		app.loadThreads()
		if thread.Article != "" {
			app.loadFeed()
		}
	}
}

func (app *App) loadThread(id string) {
	go func() {
		thread, err := app.mgr.Get(id)
		app.refresh(func() {
			if err != nil {
				app.chatErr = err.Error()
				return
			}
			app.applyThread(thread)
		})
	}()
}

// ------------------------------------------------------------------ formatting

func formatWhen(iso string) string {
	moment, err := time.Parse(time.RFC3339, iso)
	if err != nil {
		return ""
	}
	diff := time.Since(moment)
	switch {
	case diff < time.Minute:
		return "刚刚"
	case diff < time.Hour:
		return fmt.Sprintf("%d 分钟前", int(diff.Minutes()))
	case diff < 24*time.Hour:
		return fmt.Sprintf("%d 小时前", int(diff.Hours()))
	}
	return fmt.Sprintf("%d 月 %d 日", moment.Month(), moment.Day())
}

func formatDate(iso string) string {
	if iso == "" {
		return ""
	}
	moment, err := time.Parse(time.RFC3339, iso)
	if err != nil {
		return ""
	}
	return fmt.Sprintf("%d.%02d.%02d", moment.Year(), moment.Month(), moment.Day())
}

func hostOf(raw string) string {
	trimmed := strings.TrimPrefix(strings.TrimPrefix(raw, "https://"), "http://")
	if slash := strings.IndexAny(trimmed, "/?#"); slash >= 0 {
		trimmed = trimmed[:slash]
	}
	return trimmed
}

func stateLabel(state model.ThreadState) string {
	switch state {
	case model.ThreadRunning:
		return "进行中"
	case model.ThreadError:
		return "失败"
	}
	return "已完成"
}

// statusLine is a slot for a transient state: a spinner, a progress hint or an
// error. It is always built at the same height, so showing one never moves the
// page around it. Transient UI must never appear by adding an element: that
// shifts everything below it.
func statusLine(c *ui.Context, height float32, text string, color ui.Color, busy bool) {
	row := ui.Row(c).Height(height).Shrink(0).Gap(8).AlignItems(ui.Center)
	row.Children(func() {
		if text == "" {
			return
		}
		if busy {
			ui.Spinner(c)
		}
		ui.Text(c, text).FontSize(11.5).TextColor(color).SingleLine().Grow(1)
	})
}

// emptyState is the centered message a page shows when it has nothing.
func emptyState(c *ui.Context, title, detail string, action func()) {
	t := c.Theme()
	ui.Column(c).Fill().Center().Gap(10).Padding(40).Children(func() {
		ui.Text(c, title).FontSize(20).Bold()
		ui.Text(c, detail).FontSize(13).TextColor(t.TextMuted).TextAlign(ui.Center).MaxWidth(420)
		if action != nil {
			action()
		}
	})
}

// pageHeader is the title block every page starts with.
func pageHeader(c *ui.Context, kicker, title, lede string) {
	t := c.Theme()
	ui.Column(c).Gap(5).Children(func() {
		if kicker != "" {
			ui.Text(c, strings.ToUpper(kicker)).FontSize(10.5).TextColor(t.Accent)
		}
		ui.Text(c, title).FontSize(24).Bold()
		if lede != "" {
			ui.Text(c, lede).FontSize(12.5).TextColor(t.TextMuted).LineHeight(1.5).MaxWidth(560)
		}
	})
}

func logError(context string, err error) {
	if err != nil {
		log.Printf("%s: %v", context, err)
	}
}
