package server

import (
	"bytes"
	"encoding/json"
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

func TestManifestStartsAtTheFeed(t *testing.T) {
	s := &Server{}
	var icon webIcon
	for _, candidate := range webIcons {
		if strings.HasSuffix(candidate.file, ".webmanifest") {
			icon = candidate
		}
	}
	rec := httptest.NewRecorder()
	s.serveIcon(icon)(rec, httptest.NewRequest(http.MethodGet, icon.path, nil))

	var manifest struct {
		StartURL string `json:"start_url"`
		Scope    string `json:"scope"`
		Display  string `json:"display"`
		Icons    []struct {
			Src string `json:"src"`
		} `json:"icons"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &manifest); err != nil {
		t.Fatalf("the manifest is not JSON: %v", err)
	}
	// The feed is the home page: a shortcut must open on the articles, not on
	// the conversations.
	if manifest.StartURL != "/" {
		t.Errorf("start_url %q, want \"/\"", manifest.StartURL)
	}
	if manifest.Scope != "/" || manifest.Display != "standalone" {
		t.Errorf("scope %q, display %q", manifest.Scope, manifest.Display)
	}
	if len(manifest.Icons) == 0 {
		t.Fatal("the manifest lists no icons")
	}
	for _, icon := range manifest.Icons {
		if !strings.Contains(icon.Src, "?v="+iconTag()) {
			t.Errorf("manifest icon %q does not carry the tag", icon.Src)
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

// A backdrop-filter anywhere on the page makes WebKit rasterise what is around
// it into a coarser layer: in a home screen web app the whole conversation went
// soft, while the same page in Safari was crisp. Nothing needs one, so the page
// keeps none.
func TestReadingPageHasNoBackdropFilter(t *testing.T) {
	if bytes.Contains(bytes.ToLower(readPage(t)), []byte("backdrop-filter:")) {
		t.Error("the reading page uses backdrop-filter again; iOS renders the surrounding text soft")
	}
}
