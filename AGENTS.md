# Tern (Go + MyGo) — agent guide

This repository is Tern rewritten in Go on top of [MyGo](../mygo). Read this
before changing the app or the reading page.

## Shape of the app

- One desktop window. Everything is **native UI** (`internal/app`, package
  `ui`) except the article body: the feed, conversations, vocabulary book,
  settings, the sidebar and the reading toolbar are drawn by MyGo.
- The reading page is one self-contained HTML file
  (`internal/server/static/index.html`, embedded with `go:embed`) used twice:
  - desktop: `ui.WebView` inside the window, loaded with `?embed=1`, its own
    masthead and toolbar hidden; the native toolbar drives it through
    `window.tern.setMode()/setSize()`;
  - phone: the same URL without `embed`, with the page's own header and
    controls, served by the Go HTTP server.
- Data lives in `data/` and `media/` under a **user-chosen root**: by default
  `~/Library/Application Support/Tern`, changed in Settings → 数据目录 (the
  pointer is `~/Library/Application Support/Tern/root.txt`, `TERN_HOME`
  overrides it for development). Articles are the JSON contract in
  `internal/model`; agents only fill that contract — no HTML, no components, no
  styles.

## Hard rule: keep the UI still

**Never show a transient state by adding or removing elements.** Loading,
progress, hints, success and error messages must not change the position of
anything else. Layout jumps feel broken, especially on a phone.

Do this instead:

- Give every transient state a **reserved slot**: a row of fixed height that is
  always built, whose text changes. Native code uses `statusLine` in
  `internal/app/app.go`; the web page uses `.status-line` (a `min-height`
  paragraph).
- Keep page shells: render the header, the status line and the content area
  before the data arrives, then fill the content **in place**. Loading uses a
  skeleton in the same box, never a different screen.
- Keep chrome that must survive state changes: a conversation's composer, a
  reader's toolbar and a page header are always built; they are only
  `Disabled` or `Invisible` (which keeps its room), never absent.
- Growing or shrinking content (mode switches, list changes) keeps the reading
  position: the page restores `window.scrollY` after repainting, native lists
  keep their `ListState` and use `FollowEnd` only for live logs.
- Toggle appearance, not existence: `Disabled`, `ReadOnly`, `Invisible`,
  `Opacity`, colors, labels. Buttons that appear conditionally should be shown
  disabled instead.
- Settings holds only what a reader can set: the port (saved, then the app
  restarts), the data directory (applied at once), the phone address (copy), the
  Pi runtime versions, and the model and key. Do not put paths of subfolders,
  article counts or other statistics there.
- The window's title bar is not drawn: `TitleBarHiddenInset` with the traffic
  lights over the vibrancy sidebar, the wordmark on their line, an invisible
  30 DIP `DragWindow` band above the pages, and `DragWindow` on every page
  header. Dragging any of them moves the window and a double click zooms or
  minimizes as the system says; controls inside still take their presses (the
  innermost element is pressed). Do not add a second title bar.
- Prefer `ui.Invisible`/`Opacity(0)` to conditional children for anything that
  occupies layout; reserve `if` for data that is genuinely absent (an empty
  list, a missing article).

Measured behaviours that must keep working (tests cover some of them):

- threads page: starting a conversation or showing an error never moves the
  thread list;
- chat: the composer exists while loading, while the agent works and on error;
- settings: saving, testing, downloading show in one reserved line, so the
  sections never move;
- feed: a refresh error keeps the list and comments in the reserved line;
- web: the home header and status line exist before the feed; switching
  `对照/译文/原文` keeps the scroll offset.

## Development

```bash
./dev.sh                          # development: this checkout's code, ./data, dev icon
./dev.sh -port 8788               # a second copy next to the installed app
./dev.sh --headless               # server only (phone / browser)
TERN_ROUTE=/a/inkwell ./dev.sh    # open the app on a page
go test ./...                     # model, store, publish, pi, native views
TERN_SHOTS=1 go test ./internal/app -run TestSnapshot   # native frames to /tmp/tern-shots
```

`dev.sh` is `TERN_HOME=. go run .` with the working directory pinned: the
library lives in `./data`, and the installed app's copy in
`~/Library/Application Support/Tern` is left alone. Arguments after `./dev.sh`
go straight to the app.

A run from source (`go run .`, `./dev.sh`) shows the development icon in the
Dock: `useDockIcon` reads `assets/brand/png/icon-dev-512.png` when
`mygo.App.IsPackaged()` is false. A packaged `.app` is left alone — its icon
comes from the bundle, the same way `mygo build` uses `icon.png` and
`mygo build -debug` uses `icon-dev.png`.

Check the reading page at 390 px (single column, no sideways scroll) and
1000+ px (two columns), with and without `embed=1`.

## Ingestion

- One link = one conversation (`internal/threads`); Pi runs in RPC mode with a
  pinned `--session-id`, `-ne` and `--tools`, and the extension in
  `internal/pi/extension.ts` gives it `publish_article` and `upload_image`.
- `publish_article` reads `article.json` from the thread's work directory and
  POSTs it back to the server, which validates (field paths returned to the
  model), localizes pictures, stores the article and warms the dictionary.
- Key goes to the child through the environment only; no API reads it back.
