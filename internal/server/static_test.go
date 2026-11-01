package server

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// The home screen icon and the tab icon are files, not part of the page: a
// phone asks for them by path. A page whose only icon is an inline SVG data:
// link (what Tern shipped first) gets no icon on an iOS home screen, so these
// paths must answer with an image and the page must name them.
func TestWebIconsServe(t *testing.T) {
	s := &Server{}
	for _, icon := range webIcons {
		rec := httptest.NewRecorder()
		s.serveIcon(icon)(rec, httptest.NewRequest(http.MethodGet, icon.path, nil))
		if rec.Code != http.StatusOK {
			t.Errorf("%s: status %d, want 200", icon.path, rec.Code)
			continue
		}
		if got := rec.Header().Get("Content-Type"); got != icon.ctype {
			t.Errorf("%s: content type %q, want %q", icon.path, got, icon.ctype)
		}
		body := rec.Body.Bytes()
		if len(body) == 0 {
			t.Errorf("%s: empty body", icon.path)
		}
		if strings.HasSuffix(icon.file, ".png") && !bytes.HasPrefix(body, []byte("\x89PNG\r\n\x1a\n")) {
			t.Errorf("%s: not a PNG", icon.file)
		}
	}
}

// The icon URLs carry a hash of the icons, so a redrawn icon is a new URL: an
// iOS home screen shortcut holds on to the old picture otherwise.
func TestReadingPageNamesItsIcons(t *testing.T) {
	tag := iconTag()
	if len(tag) != 8 {
		t.Fatalf("icon tag %q is not eight hex digits", tag)
	}
	page := readPage(t)
	for _, want := range []string{
		`rel="apple-touch-icon" href="/apple-touch-icon.png?v=` + tag,
		`rel="icon" href="/favicon.ico?v=` + tag,
		`rel="manifest" href="/site.webmanifest?v=` + tag,
	} {
		if !bytes.Contains(page, []byte(want)) {
			t.Errorf("the reading page does not name %s", want)
		}
	}
	if bytes.Contains(page, iconTagPlaceholder) {
		t.Error("the page still carries the {{icons}} placeholder")
	}
}

func TestManifestNamesTaggedIcons(t *testing.T) {
	s := &Server{}
	var manifest webIcon
	for _, icon := range webIcons {
		if strings.HasSuffix(icon.file, ".webmanifest") {
			manifest = icon
		}
	}
	rec := httptest.NewRecorder()
	s.serveIcon(manifest)(rec, httptest.NewRequest(http.MethodGet, manifest.path, nil))
	for _, want := range []string{"icon-192.png?v=" + iconTag(), "icon-512.png?v=" + iconTag()} {
		if !bytes.Contains(rec.Body.Bytes(), []byte(want)) {
			t.Errorf("the manifest does not name %s", want)
		}
	}
}

// readPage is the page as the server sends it, with the placeholder filled.
func readPage(t *testing.T) []byte {
	t.Helper()
	raw, err := webFS.ReadFile("static/index.html")
	if err != nil {
		t.Fatal(err)
	}
	return withIconTag(raw)
}
