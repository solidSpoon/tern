package publish

import (
	"bytes"
	"crypto/sha1"
	"encoding/hex"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/solidSpoon/tern-go/internal/store"
)

// MaxImageBytes is the largest image Tern stores.
const MaxImageBytes = 15 << 20

// ImageError explains why an image could not be stored.
type ImageError struct{ Reason string }

func (e ImageError) Error() string { return e.Reason }

var imageClient = &http.Client{Timeout: 30 * time.Second}

// detectExt reads the picture's magic bytes; the file name is not trusted.
func detectExt(b []byte) string {
	if len(b) < 12 {
		return ""
	}
	switch {
	case b[0] == 0xff && b[1] == 0xd8 && b[2] == 0xff:
		return "jpg"
	case bytes.HasPrefix(b, []byte{0x89, 'P', 'N', 'G', 0x0d, 0x0a, 0x1a, 0x0a}):
		return "png"
	case string(b[:4]) == "GIF8":
		return "gif"
	case string(b[:4]) == "RIFF" && string(b[8:12]) == "WEBP":
		return "webp"
	case string(b[4:12]) == "ftypavif":
		return "avif"
	}
	head := strings.ToLower(strings.TrimSpace(string(b[:min(256, len(b))])))
	if strings.HasPrefix(head, "<svg") || (strings.HasPrefix(head, "<?xml") && strings.Contains(head, "<svg")) {
		return "svg"
	}
	return ""
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}

// StoreImage writes bytes into the media folder under a content hash, so the
// same picture is stored once. It returns the /media/... URL.
func StoreImage(assetsDir string, data []byte, label string) (src string, ext string, err error) {
	if len(data) == 0 {
		return "", "", ImageError{"the image is empty"}
	}
	if len(data) > MaxImageBytes {
		return "", "", ImageError{fmt.Sprintf("the image is too large (%.1f MB, limit %d MB)", float64(len(data))/1e6, MaxImageBytes>>20)}
	}
	ext = detectExt(data)
	if ext == "" {
		return "", "", ImageError{"not a picture Tern knows (jpg, png, gif, webp, svg, avif)"}
	}
	sum := sha1.Sum(data)
	hash := hex.EncodeToString(sum[:])[:8]
	prefix := slugLabel(label)
	if prefix != "" {
		prefix += "-"
	}
	name := prefix + hash + "." + ext
	target := filepath.Join(assetsDir, name)
	if _, statErr := os.Stat(target); os.IsNotExist(statErr) {
		if mkErr := os.MkdirAll(assetsDir, 0o755); mkErr != nil {
			return "", "", mkErr
		}
		if writeErr := os.WriteFile(target, data, 0o644); writeErr != nil {
			return "", "", writeErr
		}
	}
	return "/media/" + name, ext, nil
}

func slugLabel(label string) string {
	var b strings.Builder
	dash := false
	for _, r := range strings.ToLower(label) {
		if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') {
			b.WriteRune(r)
			dash = false
			continue
		}
		if !dash && b.Len() > 0 {
			b.WriteByte('-')
			dash = true
		}
	}
	return strings.Trim(b.String(), "-")
}

// FetchImage downloads a remote picture for the agent's upload_image tool.
func FetchImage(url, referer string) ([]byte, error) {
	if !strings.HasPrefix(url, "http://") && !strings.HasPrefix(url, "https://") {
		return nil, ImageError{"only http and https addresses are supported"}
	}
	req, err := http.NewRequest(http.MethodGet, url, nil)
	if err != nil {
		return nil, ImageError{err.Error()}
	}
	req.Header.Set("User-Agent", "Mozilla/5.0 TernReader/0.1")
	if referer != "" {
		req.Header.Set("Referer", referer)
	}
	resp, err := imageClient.Do(req)
	if err != nil {
		return nil, ImageError{"download failed: " + err.Error()}
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, ImageError{fmt.Sprintf("download failed: HTTP %d", resp.StatusCode)}
	}
	ctype := resp.Header.Get("Content-Type")
	if ctype != "" && !strings.HasPrefix(ctype, "image/") {
		return nil, ImageError{"that address is not a picture (" + ctype + ")"}
	}
	if resp.ContentLength > MaxImageBytes {
		return nil, ImageError{"the image is too large (over 15 MB)"}
	}
	data, err := io.ReadAll(io.LimitReader(resp.Body, MaxImageBytes+1))
	if err != nil {
		return nil, ImageError{"download failed: " + err.Error()}
	}
	return data, nil
}

// downloadForArticle fetches an article's picture and stores it locally, so the
// reading page never depends on a third-party host.
func downloadForArticle(assetsDir, url, slug string) (string, bool) {
	if !strings.HasPrefix(url, "http://") && !strings.HasPrefix(url, "https://") {
		return "", false
	}
	req, err := http.NewRequest(http.MethodGet, url, nil)
	if err != nil {
		return "", false
	}
	req.Header.Set("User-Agent", "Mozilla/5.0 TernReader/0.1")
	req.Header.Set("Referer", url)
	resp, err := imageClient.Do(req)
	if err != nil {
		return "", false
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return "", false
	}
	if ctype := resp.Header.Get("Content-Type"); ctype != "" && !strings.HasPrefix(ctype, "image/") {
		return "", false
	}
	data, err := io.ReadAll(io.LimitReader(resp.Body, MaxImageBytes+1))
	if err != nil || len(data) == 0 || len(data) > MaxImageBytes {
		return "", false
	}
	ext := detectExt(data)
	if ext == "" {
		return "", false
	}
	// The name carries a hash of the source URL, so a URL always maps to the
	// same file and can be cached forever.
	sum := sha1.Sum([]byte(url))
	hash := hex.EncodeToString(sum[:])[:8]
	base := store.Slug(slug)
	if base == "" {
		base = "img"
	}
	name := base + "-" + hash + "." + ext
	if err := os.MkdirAll(assetsDir, 0o755); err != nil {
		return "", false
	}
	if err := os.WriteFile(filepath.Join(assetsDir, name), data, 0o644); err != nil {
		return "", false
	}
	return "/media/" + name, true
}
