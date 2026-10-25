// Package pi runs Pi, the coding agent that reads pages and writes articles.
//
// Tern does not depend on a pi installed on the system, and not on an npm
// package: Settings downloads an official release binary into the app's own
// directory, checks its SHA256, and unpacks it per version. current.json says
// which version runs, so a new version never overwrites an old one and rolling
// back is one click.
package pi

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/solidSpoon/tern-go/internal/config"
)

const releaseRepo = "earendil-works/pi"

var (
	binaryName   = "pi"
	platformKey  = runtime.GOOS + "-" + runtime.GOARCH
	versionRE    = regexp.MustCompile(`^\d+\.\d+\.\d+$`)
	httpClient   = &http.Client{Timeout: 5 * time.Minute}
	downloadOnce sync.Mutex
)

// InstalledVersion is one version in the runtime directory.
type InstalledVersion struct {
	Version     string `json:"version"`
	Active      bool   `json:"active"`
	InstalledAt string `json:"installedAt,omitempty"`
	SizeBytes   int64  `json:"sizeBytes"`
	SHA256      string `json:"sha256,omitempty"`
}

// Status is what the runtime looks like now.
type Status struct {
	Installed bool               `json:"installed"`
	Version   string             `json:"version,omitempty"`
	Path      string             `json:"path"`
	Binary    string             `json:"binary,omitempty"`
	Platform  string             `json:"platform"`
	Versions  []InstalledVersion `json:"versions"`
}

// Progress is how an install is going, for the settings page to poll.
type Progress struct {
	State    string  `json:"state"`
	Progress float64 `json:"progress"`
	Message  string  `json:"message"`
	Version  string  `json:"version,omitempty"`
}

// Runtime installs and runs Pi.
type Runtime struct {
	root string
	mu   sync.Mutex
	last Progress
}

// NewRuntime makes a runtime under data/runtime/pi.
func NewRuntime(paths config.Paths) *Runtime {
	_ = os.MkdirAll(paths.Runtime, 0o755)
	if runtime.GOOS == "windows" {
		binaryName = "pi.exe"
	}
	return &Runtime{root: paths.Runtime, last: Progress{State: "idle"}}
}

// SetRoot points the runtime at the data directory's runtime folder; the Pi
// versions found there are the ones Settings lists afterwards.
func (r *Runtime) SetRoot(dir string) error {
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	r.mu.Lock()
	r.root = dir
	r.mu.Unlock()
	return nil
}

func (r *Runtime) versionDir(version string) string { return filepath.Join(r.root, version) }
func (r *Runtime) binaryIn(version string) string {
	return filepath.Join(r.versionDir(version), binaryName)
}
func (r *Runtime) metaIn(version string) string {
	return filepath.Join(r.versionDir(version), "version.json")
}
func (r *Runtime) currentFile() string { return filepath.Join(r.root, "current.json") }

// Progress returns the state of the last install.
func (r *Runtime) Progress() Progress {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.last
}

func (r *Runtime) setProgress(p Progress) {
	r.mu.Lock()
	r.last = p
	r.mu.Unlock()
}

func (r *Runtime) current() string {
	raw, err := os.ReadFile(r.currentFile())
	if err != nil {
		return ""
	}
	var data struct {
		Version string `json:"version"`
	}
	if json.Unmarshal(raw, &data) != nil || !versionRE.MatchString(data.Version) {
		return ""
	}
	return data.Version
}

func (r *Runtime) writeCurrent(version string) error {
	raw, err := json.MarshalIndent(map[string]string{
		"version":     version,
		"activatedAt": time.Now().UTC().Format(time.RFC3339),
	}, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(r.currentFile(), append(raw, '\n'), 0o644)
}

type versionMeta struct {
	Version     string `json:"version"`
	InstalledAt string `json:"installedAt"`
	Platform    string `json:"platform"`
	Asset       string `json:"asset"`
	SHA256      string `json:"sha256"`
	Files       int    `json:"files"`
}

// Status reads the versions installed and the one active.
func (r *Runtime) Status() Status {
	current := r.current()
	status := Status{Platform: platformKey, Path: r.root, Versions: []InstalledVersion{}}
	entries, err := os.ReadDir(r.root)
	if err == nil {
		for _, entry := range entries {
			if !entry.IsDir() || !versionRE.MatchString(entry.Name()) {
				continue
			}
			version := entry.Name()
			meta := versionMeta{}
			if raw, readErr := os.ReadFile(r.metaIn(version)); readErr == nil {
				_ = json.Unmarshal(raw, &meta)
			}
			status.Versions = append(status.Versions, InstalledVersion{
				Version:     version,
				Active:      version == current,
				InstalledAt: meta.InstalledAt,
				SizeBytes:   dirSize(r.versionDir(version)),
				SHA256:      meta.SHA256,
			})
		}
	}
	sort.Slice(status.Versions, func(i, j int) bool {
		return compareVersions(status.Versions[i].Version, status.Versions[j].Version) > 0
	})
	for _, version := range status.Versions {
		if version.Active {
			status.Installed = true
			status.Version = version.Version
			status.Path = r.versionDir(version.Version)
			status.Binary = r.binaryIn(version.Version)
		}
	}
	return status
}

func compareVersions(a, b string) int {
	pa, pb := strings.Split(a, "."), strings.Split(b, ".")
	for i := 0; i < 3; i++ {
		var na, nb int
		if i < len(pa) {
			fmt.Sscanf(pa[i], "%d", &na)
		}
		if i < len(pb) {
			fmt.Sscanf(pb[i], "%d", &nb)
		}
		if na != nb {
			if na > nb {
				return 1
			}
			return -1
		}
	}
	return 0
}

func dirSize(dir string) int64 {
	var total int64
	_ = filepath.Walk(dir, func(_ string, info os.FileInfo, err error) error {
		if err == nil && info != nil && !info.IsDir() {
			total += info.Size()
		}
		return nil
	})
	return total
}

// Binary is the pi executable to run: $TERN_PI_BIN when set, else the active
// version.
func (r *Runtime) Binary() (string, error) {
	if override := os.Getenv("TERN_PI_BIN"); override != "" {
		if _, err := os.Stat(override); err != nil {
			return "", fmt.Errorf("TERN_PI_BIN points at %s, which does not exist", override)
		}
		return override, nil
	}
	version := r.current()
	if version == "" {
		return "", errors.New("the Pi runtime is not installed yet: open Settings and download it")
	}
	binary := r.binaryIn(version)
	if _, err := os.Stat(binary); err != nil {
		return "", fmt.Errorf("the Pi runtime (%s) is missing its binary", version)
	}
	return binary, nil
}

// Version reports the active version, from the runtime or from TERN_PI_BIN.
func (r *Runtime) Version() string {
	if os.Getenv("TERN_PI_BIN") != "" {
		return "custom"
	}
	return r.current()
}

// ---------------------------------------------------------------- installing

type asset struct {
	Name string `json:"name"`
	URL  string `json:"browser_download_url"`
	Size int64  `json:"size"`
}

type release struct {
	Version string  `json:"tag_name"`
	Assets  []asset `json:"assets"`
}

func (r *Runtime) resolveRelease(requested string) (*release, error) {
	url := fmt.Sprintf("https://api.github.com/repos/%s/releases/latest", releaseRepo)
	if requested != "" && requested != "latest" {
		url = fmt.Sprintf("https://api.github.com/repos/%s/releases/tags/v%s",
			releaseRepo, strings.TrimPrefix(requested, "v"))
	}
	req, err := http.NewRequest(http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("User-Agent", "tern-reader")
	resp, err := httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("cannot reach GitHub: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("cannot list releases: HTTP %d (GitHub may be rate limiting, try later)", resp.StatusCode)
	}
	var data release
	if err := json.NewDecoder(resp.Body).Decode(&data); err != nil {
		return nil, err
	}
	data.Version = strings.TrimPrefix(data.Version, "v")
	if data.Version == "" || len(data.Assets) == 0 {
		return nil, errors.New("this release has no files")
	}
	return &data, nil
}

// Install downloads (or switches to) a Pi version and activates it.
func (r *Runtime) Install(requested string) (Status, error) {
	downloadOnce.Lock()
	defer downloadOnce.Unlock()

	if runtime.GOOS == "windows" {
		return r.Status(), errors.New("Windows releases are zip files, which Tern does not unpack yet: put pi.exe in data/runtime/pi/<version>/ yourself")
	}
	if requested != "" && requested != "latest" {
		wanted := strings.TrimPrefix(requested, "v")
		if versionRE.MatchString(wanted) {
			if _, err := os.Stat(r.binaryIn(wanted)); err == nil {
				if err := r.writeCurrent(wanted); err != nil {
					return r.Status(), err
				}
				r.setProgress(Progress{State: "done", Progress: 1, Message: "switched to Pi " + wanted, Version: wanted})
				return r.Status(), nil
			}
		}
	}

	r.setProgress(Progress{State: "downloading", Progress: 0, Message: "looking up the release"})
	rel, err := r.resolveRelease(requested)
	if err != nil {
		r.setProgress(Progress{State: "error", Message: err.Error()})
		return r.Status(), err
	}
	if _, statErr := os.Stat(r.binaryIn(rel.Version)); statErr == nil {
		if err := r.writeCurrent(rel.Version); err != nil {
			return r.Status(), err
		}
		r.setProgress(Progress{State: "done", Progress: 1, Message: "Pi " + rel.Version + " was already here; switched to it", Version: rel.Version})
		return r.Status(), nil
	}

	assetName := fmt.Sprintf("pi-%s.tar.gz", platformKey)
	var chosen *asset
	for i := range rel.Assets {
		if rel.Assets[i].Name == assetName {
			chosen = &rel.Assets[i]
		}
	}
	if chosen == nil {
		err := fmt.Errorf("this release has no %s", assetName)
		r.setProgress(Progress{State: "error", Message: err.Error()})
		return r.Status(), err
	}

	r.setProgress(Progress{
		State:    "downloading",
		Progress: 0.02,
		Message:  fmt.Sprintf("downloading Pi %s (%.0f MB)", rel.Version, float64(chosen.Size)/1e6),
		Version:  rel.Version,
	})
	archive, err := r.download(chosen.URL)
	if err != nil {
		r.setProgress(Progress{State: "error", Message: err.Error()})
		return r.Status(), err
	}

	r.setProgress(Progress{State: "verifying", Progress: 0.92, Message: "checking SHA256", Version: rel.Version})
	sha, err := r.verify(archive, rel, assetName)
	if err != nil {
		r.setProgress(Progress{State: "error", Message: err.Error()})
		return r.Status(), err
	}

	r.setProgress(Progress{State: "extracting", Progress: 0.95, Message: "unpacking into " + rel.Version + "/", Version: rel.Version})
	files, err := extractTarGz(archive, r.versionDir(rel.Version))
	if err != nil {
		r.setProgress(Progress{State: "error", Message: err.Error()})
		return r.Status(), err
	}

	meta, _ := json.MarshalIndent(versionMeta{
		Version:     rel.Version,
		InstalledAt: time.Now().UTC().Format(time.RFC3339),
		Platform:    platformKey,
		Asset:       assetName,
		SHA256:      sha,
		Files:       files,
	}, "", "  ")
	_ = os.WriteFile(r.metaIn(rel.Version), append(meta, '\n'), 0o644)

	if probed, probeErr := runVersion(r.binaryIn(rel.Version)); probeErr != nil || probed != rel.Version {
		_ = os.RemoveAll(r.versionDir(rel.Version))
		reason := fmt.Sprintf("self-check failed: expected %s, got %q", rel.Version, probed)
		if probeErr != nil {
			reason = "self-check failed: " + probeErr.Error()
		}
		err := errors.New(reason)
		r.setProgress(Progress{State: "error", Message: err.Error()})
		return r.Status(), err
	}

	if err := r.writeCurrent(rel.Version); err != nil {
		return r.Status(), err
	}
	r.setProgress(Progress{State: "done", Progress: 1, Message: "installed and switched to Pi " + rel.Version, Version: rel.Version})
	return r.Status(), nil
}

func (r *Runtime) download(url string) ([]byte, error) {
	req, err := http.NewRequest(http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", "tern-reader")
	resp, err := httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("download failed: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("download failed: HTTP %d", resp.StatusCode)
	}
	total := resp.ContentLength
	var buf bytes.Buffer
	chunk := make([]byte, 128<<10)
	var received int64
	for {
		n, readErr := resp.Body.Read(chunk)
		if n > 0 {
			buf.Write(chunk[:n])
			received += int64(n)
			if total > 0 {
				progress := float64(received) / float64(total) * 0.9
				if progress > 0.9 {
					progress = 0.9
				}
				r.setProgress(Progress{
					State:    "downloading",
					Progress: progress,
					Message:  fmt.Sprintf("downloading %.1f / %.1f MB", float64(received)/1e6, float64(total)/1e6),
					Version:  r.Progress().Version,
				})
			}
		}
		if readErr == io.EOF {
			break
		}
		if readErr != nil {
			return nil, fmt.Errorf("download failed: %w", readErr)
		}
	}
	return buf.Bytes(), nil
}

func (r *Runtime) verify(archive []byte, rel *release, assetName string) (string, error) {
	var sums *asset
	for i := range rel.Assets {
		if rel.Assets[i].Name == "SHA256SUMS" {
			sums = &rel.Assets[i]
		}
	}
	if sums == nil {
		return "", nil
	}
	req, err := http.NewRequest(http.MethodGet, sums.URL, nil)
	if err != nil {
		return "", nil
	}
	req.Header.Set("User-Agent", "tern-reader")
	resp, err := httpClient.Do(req)
	if err != nil {
		return "", nil
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return "", nil
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return "", nil
	}
	expected := ""
	for _, line := range strings.Split(string(body), "\n") {
		fields := strings.Fields(line)
		if len(fields) == 2 && fields[1] == assetName {
			expected = strings.ToLower(fields[0])
		}
	}
	if expected == "" {
		return "", nil
	}
	sum := sha256.Sum256(archive)
	actual := hex.EncodeToString(sum[:])
	if actual != expected {
		return "", fmt.Errorf("checksum mismatch (expected %s…)", expected[:12])
	}
	return actual, nil
}

// extractTarGz unpacks the archive into target, replacing what was there.
func extractTarGz(archive []byte, target string) (int, error) {
	gz, err := gzip.NewReader(bytes.NewReader(archive))
	if err != nil {
		return 0, fmt.Errorf("the archive is not gzip: %w", err)
	}
	defer gz.Close()

	staging := target + ".new"
	_ = os.RemoveAll(staging)
	if err := os.MkdirAll(staging, 0o755); err != nil {
		return 0, err
	}

	reader := tar.NewReader(gz)
	count := 0
	for {
		header, err := reader.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			_ = os.RemoveAll(staging)
			return 0, fmt.Errorf("the archive is broken: %w", err)
		}
		name := strings.TrimPrefix(header.Name, "pi/")
		name = strings.TrimPrefix(name, "./")
		if name == "" || strings.HasPrefix(name, "..") || strings.Contains(name, "..") {
			continue
		}
		dest := filepath.Join(staging, name)
		switch header.Typeflag {
		case tar.TypeDir:
			if err := os.MkdirAll(dest, 0o755); err != nil {
				return 0, err
			}
		case tar.TypeReg:
			if err := os.MkdirAll(filepath.Dir(dest), 0o755); err != nil {
				return 0, err
			}
			file, err := os.OpenFile(dest, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, os.FileMode(header.Mode)&0o777)
			if err != nil {
				return 0, err
			}
			if _, err := io.Copy(file, io.LimitReader(reader, header.Size)); err != nil {
				file.Close()
				return 0, err
			}
			file.Close()
			count++
		}
	}

	binary := filepath.Join(staging, binaryName)
	if _, err := os.Stat(binary); err != nil {
		_ = os.RemoveAll(staging)
		return 0, fmt.Errorf("the archive has no %s: its layout may have changed for %s", binaryName, platformKey)
	}
	if err := os.Chmod(binary, 0o755); err != nil {
		return 0, err
	}
	_ = os.RemoveAll(target)
	if err := os.Rename(staging, target); err != nil {
		return 0, err
	}
	return count, nil
}

// Activate switches to an installed version.
func (r *Runtime) Activate(version string) (Status, error) {
	if !versionRE.MatchString(version) {
		return r.Status(), fmt.Errorf("no version %q installed", version)
	}
	if _, err := os.Stat(r.binaryIn(version)); err != nil {
		return r.Status(), fmt.Errorf("no version %q installed", version)
	}
	if err := r.writeCurrent(version); err != nil {
		return r.Status(), err
	}
	r.setProgress(Progress{State: "done", Progress: 1, Message: "switched to Pi " + version, Version: version})
	return r.Status(), nil
}

// Remove deletes one version, or every version when version is empty. Removing
// the active one switches to the newest that is left.
func (r *Runtime) Remove(version string) Status {
	targets := []string{}
	if version != "" {
		targets = append(targets, version)
	} else {
		entries, _ := os.ReadDir(r.root)
		for _, entry := range entries {
			if entry.IsDir() && versionRE.MatchString(entry.Name()) {
				targets = append(targets, entry.Name())
			}
		}
	}
	current := r.current()
	for _, target := range targets {
		if !versionRE.MatchString(target) {
			continue
		}
		_ = os.RemoveAll(r.versionDir(target))
	}
	stillActive := false
	for _, target := range targets {
		if target == current {
			stillActive = true
		}
	}
	if stillActive {
		remaining := r.Status().Versions
		if len(remaining) > 0 {
			_ = r.writeCurrent(remaining[0].Version)
		} else {
			_ = os.Remove(r.currentFile())
		}
	}
	message := "removed every runtime"
	if version != "" {
		message = "removed " + version
	}
	r.setProgress(Progress{State: "idle", Message: message})
	return r.Status()
}

func runVersion(binary string) (string, error) {
	cmd := exec.Command(binary, "--version")
	cmd.Env = append(os.Environ(), "PI_NO_UPDATE_CHECK=1", "NO_COLOR=1")
	out, err := cmd.Output()
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(string(out)), nil
}
