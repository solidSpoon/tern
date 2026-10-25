// Package server exposes Tern over HTTP: the JSON API, the media folder, and
// the reading pages that the desktop opens in a web window and the phone opens
// in Safari through Surge Ponte.
package server

import (
	"embed"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"log"
	"net/http"
	"os"
	"path"
	"path/filepath"
	"strings"
	"time"

	"github.com/solidSpoon/tern-go/internal/config"
	"github.com/solidSpoon/tern-go/internal/model"
	"github.com/solidSpoon/tern-go/internal/pi"
	"github.com/solidSpoon/tern-go/internal/publish"
	"github.com/solidSpoon/tern-go/internal/store"
	"github.com/solidSpoon/tern-go/internal/threads"
)

//go:embed static/index.html
var webFS embed.FS

// Server is the HTTP face of Tern.
type Server struct {
	Store    *store.Store
	Settings *config.Settings
	Agent    *pi.Agent
	Runtime  *pi.Runtime
	Threads  *threads.Manager
	Importer *publish.Importer
	Port     int
	Handler  http.Handler
}

// New wires the routes.
func New(st *store.Store, settings *config.Settings, agent *pi.Agent, runtime *pi.Runtime, mgr *threads.Manager, importer *publish.Importer, port int) *Server {
	s := &Server{
		Store: st, Settings: settings, Agent: agent, Runtime: runtime,
		Threads: mgr, Importer: importer, Port: port,
	}
	mux := http.NewServeMux()

	mux.HandleFunc("GET /api/health", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, http.StatusOK, map[string]any{"ok": true})
	})

	mux.HandleFunc("GET /api/articles", s.listArticles)
	mux.HandleFunc("GET /api/articles/{id}", s.getArticle)
	mux.HandleFunc("POST /api/lookup", s.lookup)
	mux.HandleFunc("GET /api/vocab", s.listVocab)
	mux.HandleFunc("POST /api/vocab", s.addVocab)
	mux.HandleFunc("DELETE /api/vocab", s.removeVocab)
	mux.HandleFunc("GET /api/config", s.getConfig)
	mux.HandleFunc("POST /api/config", s.saveConfig)
	mux.HandleFunc("POST /api/config/test", s.testConfig)
	mux.HandleFunc("GET /api/runtime", s.getRuntime)
	mux.HandleFunc("POST /api/runtime", s.postRuntime)
	mux.HandleFunc("DELETE /api/runtime", s.deleteRuntime)

	mux.HandleFunc("GET /api/threads", s.listThreads)
	mux.HandleFunc("POST /api/threads", s.createThread)
	mux.HandleFunc("GET /api/threads/{id}", s.getThread)
	mux.HandleFunc("POST /api/threads/{id}/messages", s.postMessage)
	mux.HandleFunc("POST /api/threads/{id}/publish", s.publishArticle)
	mux.HandleFunc("POST /api/threads/{id}/images", s.uploadImage)

	mux.HandleFunc("GET /media/{name...}", s.serveMedia)
	mux.HandleFunc("/", s.serveApp)

	s.Handler = logRequests(mux)
	return s
}

// ListenAndServe starts the server on host:port.
func (s *Server) ListenAndServe(host string, port int) error {
	server := &http.Server{
		Addr:              fmt.Sprintf("%s:%d", host, port),
		Handler:           s.Handler,
		ReadHeaderTimeout: 15 * time.Second,
		// No write timeout: a word lookup can take a minute while the model
		// writes it.
	}
	return server.ListenAndServe()
}

func logRequests(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/health" {
			next.ServeHTTP(w, r)
			return
		}
		next.ServeHTTP(w, r)
	})
}

// ---------------------------------------------------------------- helpers

func writeJSON(w http.ResponseWriter, status int, data any) {
	raw, err := json.Marshal(data)
	if err != nil {
		w.Header().Set("Content-Type", "application/json; charset=utf-8")
		w.WriteHeader(http.StatusInternalServerError)
		_, _ = w.Write([]byte(`{"error":"cannot encode the answer"}`))
		return
	}
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(status)
	_, _ = w.Write(raw)
}

func fail(w http.ResponseWriter, status int, message string) {
	writeJSON(w, status, map[string]string{"error": message})
}

func readBody(w http.ResponseWriter, r *http.Request, limit int64) ([]byte, bool) {
	raw, err := io.ReadAll(io.LimitReader(r.Body, limit+1))
	if err != nil {
		fail(w, http.StatusBadRequest, "cannot read the request body")
		return nil, false
	}
	if int64(len(raw)) > limit {
		fail(w, http.StatusRequestEntityTooLarge, "the request body is too large")
		return nil, false
	}
	return raw, true
}

func decode[T any](w http.ResponseWriter, r *http.Request, limit int64) (T, bool) {
	var value T
	raw, ok := readBody(w, r, limit)
	if !ok {
		return value, false
	}
	if len(raw) > 0 {
		if err := json.Unmarshal(raw, &value); err != nil {
			fail(w, http.StatusBadRequest, "the request body is not valid JSON")
			return value, false
		}
	}
	return value, true
}

// ---------------------------------------------------------------- articles

func (s *Server) listArticles(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]any{"articles": s.Store.ListArticles()})
}

func (s *Server) getArticle(w http.ResponseWriter, r *http.Request) {
	article, err := s.Store.Article(r.PathValue("id"))
	if err != nil {
		fail(w, http.StatusNotFound, "no such article")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"article": article})
}

// ---------------------------------------------------------------- lookup

func (s *Server) lookup(w http.ResponseWriter, r *http.Request) {
	body, ok := decode[struct {
		Term    string `json:"term"`
		Context string `json:"context"`
	}](w, r, 1<<20)
	if !ok {
		return
	}
	term := store.NormalizeTerm(body.Term)
	if term == "" {
		fail(w, http.StatusBadRequest, "no word to look up")
		return
	}
	if cached := s.Store.CachedEntry(term); cached != nil {
		writeJSON(w, http.StatusOK, map[string]any{"entry": cached, "cached": true})
		return
	}
	entry, err := s.Agent.LookupWord(term, body.Context)
	if err != nil {
		var unavailable pi.UnavailableError
		var output pi.OutputError
		switch {
		case errors.As(err, &unavailable):
			fail(w, http.StatusServiceUnavailable, "the AI is not ready: "+unavailable.Error())
		case errors.As(err, &output):
			fail(w, http.StatusBadGateway, "the dictionary call failed: "+output.Error())
		default:
			fail(w, http.StatusInternalServerError, err.Error())
		}
		return
	}
	if err := s.Store.CacheEntry(term, entry); err != nil {
		log.Printf("dictionary: %v", err)
	}
	writeJSON(w, http.StatusOK, map[string]any{"entry": entry, "cached": false})
}

// ---------------------------------------------------------------- vocabulary

func (s *Server) listVocab(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]any{"vocab": s.Store.Vocab()})
}

func (s *Server) addVocab(w http.ResponseWriter, r *http.Request) {
	entry, ok := decode[model.Vocab](w, r, 1<<20)
	if !ok {
		return
	}
	if entry.Term == "" || entry.ZH == "" {
		fail(w, http.StatusBadRequest, "a saved word needs term and zh")
		return
	}
	list, err := s.Store.AddVocab(entry)
	if err != nil {
		fail(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"vocab": list})
}

func (s *Server) removeVocab(w http.ResponseWriter, r *http.Request) {
	term := store.NormalizeTerm(r.URL.Query().Get("term"))
	if term == "" {
		fail(w, http.StatusBadRequest, "no word to remove")
		return
	}
	list, err := s.Store.RemoveVocab(term)
	if err != nil {
		fail(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"vocab": list})
}

// ---------------------------------------------------------------- config

type serverConfig struct {
	Root         string         `json:"root"`
	AssetsDir    string         `json:"assetsDir"`
	Agent        pi.AgentStatus `json:"agent"`
	Models       []string       `json:"models"`
	ArticleCount int            `json:"articleCount"`
}

func (s *Server) configNow() serverConfig {
	paths := s.Settings.Paths()
	return serverConfig{
		Root:         paths.Root,
		AssetsDir:    s.Settings.AssetsDir(),
		Agent:        s.Agent.Status(),
		Models:       s.Agent.Models(),
		ArticleCount: len(s.Store.ListArticles()),
	}
}

func (s *Server) getConfig(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, s.configNow())
}

func (s *Server) saveConfig(w http.ResponseWriter, r *http.Request) {
	body, ok := decode[struct {
		APIKey *string `json:"apiKey"`
		Model  string  `json:"model"`
	}](w, r, 1<<20)
	if !ok {
		return
	}
	if body.APIKey != nil {
		key := strings.TrimSpace(*body.APIKey)
		if err := s.Settings.PatchAgent(config.Agent{APIKey: key}); err != nil {
			fail(w, http.StatusInternalServerError, err.Error())
			return
		}
	}
	if body.Model != "" {
		if err := s.Settings.PatchAgent(config.Agent{Model: strings.TrimSpace(body.Model)}); err != nil {
			fail(w, http.StatusInternalServerError, err.Error())
			return
		}
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"agent":  s.Agent.Status(),
		"models": s.Agent.Models(),
	})
}

func (s *Server) testConfig(w http.ResponseWriter, r *http.Request) {
	detail, err := s.Agent.TestConnection()
	if err != nil {
		writeJSON(w, http.StatusOK, map[string]any{"ok": false, "error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "model": detail})
}

// ---------------------------------------------------------------- runtime

func (s *Server) getRuntime(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]any{
		"runtime": s.Runtime.Status(),
		"install": s.Runtime.Progress(),
	})
}

func (s *Server) postRuntime(w http.ResponseWriter, r *http.Request) {
	body, ok := decode[struct {
		Version  string `json:"version"`
		Activate string `json:"activate"`
	}](w, r, 1<<20)
	if !ok {
		return
	}
	if body.Activate != "" {
		status, err := s.Runtime.Activate(body.Activate)
		if err != nil {
			writeJSON(w, http.StatusOK, map[string]any{
				"error": err.Error(), "runtime": status, "install": s.Runtime.Progress(),
			})
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"runtime": status, "install": s.Runtime.Progress()})
		return
	}
	version := body.Version
	if version == "" {
		version = "latest"
	}
	status, err := s.Runtime.Install(version)
	if err != nil {
		writeJSON(w, http.StatusOK, map[string]any{
			"error": err.Error(), "runtime": status, "install": s.Runtime.Progress(),
		})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"runtime": status, "install": s.Runtime.Progress()})
}

func (s *Server) deleteRuntime(w http.ResponseWriter, r *http.Request) {
	status := s.Runtime.Remove(r.URL.Query().Get("version"))
	writeJSON(w, http.StatusOK, map[string]any{"runtime": status, "install": s.Runtime.Progress()})
}

// ---------------------------------------------------------------- threads

func (s *Server) listThreads(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]any{"threads": s.Threads.List()})
}

func (s *Server) createThread(w http.ResponseWriter, r *http.Request) {
	body, ok := decode[struct {
		URL string `json:"url"`
	}](w, r, 1<<20)
	if !ok {
		return
	}
	thread, err := s.Threads.Create(body.URL)
	if err != nil {
		var unavailable pi.UnavailableError
		if errors.As(err, &unavailable) {
			fail(w, http.StatusBadRequest, unavailable.Error())
			return
		}
		fail(w, http.StatusBadRequest, err.Error())
		return
	}
	writeJSON(w, http.StatusCreated, map[string]any{"thread": thread})
}

func (s *Server) getThread(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	thread, err := s.Threads.Get(id)
	if err != nil {
		fail(w, http.StatusNotFound, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"thread": thread, "busy": s.Threads.Busy(id)})
}

func (s *Server) postMessage(w http.ResponseWriter, r *http.Request) {
	body, ok := decode[struct {
		Text string `json:"text"`
	}](w, r, 1<<20)
	if !ok {
		return
	}
	thread, err := s.Threads.PostMessage(r.PathValue("id"), body.Text)
	if err != nil {
		fail(w, http.StatusConflict, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"thread": thread})
}

func (s *Server) publishArticle(w http.ResponseWriter, r *http.Request) {
	raw, ok := readBody(w, r, 8<<20)
	if !ok {
		return
	}
	result, issues, err := s.Threads.PublishRaw(r.PathValue("id"), raw)
	if err != nil {
		writeJSON(w, http.StatusOK, map[string]any{"ok": false, "issues": []string{err.Error()}})
		return
	}
	if len(issues) > 0 {
		writeJSON(w, http.StatusOK, map[string]any{"ok": false, "issues": issues.Strings()})
		return
	}
	article := result.Article
	writeJSON(w, http.StatusOK, map[string]any{
		"ok":        true,
		"articleId": article.ID,
		"blocks":    len(article.Blocks),
		"warmed":    result.Warmed,
	})
}

func (s *Server) uploadImage(w http.ResponseWriter, r *http.Request) {
	body, ok := decode[struct {
		DataBase64 string `json:"dataBase64"`
		URL        string `json:"url"`
		Label      string `json:"label"`
	}](w, r, 25<<20)
	if !ok {
		return
	}
	uploaded, err := s.Threads.UploadImage(body.DataBase64, body.URL, body.Label)
	if err != nil {
		writeJSON(w, http.StatusOK, map[string]any{"ok": false, "error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"ok": true, "src": uploaded.Src, "bytes": uploaded.Bytes, "ext": uploaded.Ext,
	})
}

// ---------------------------------------------------------------- static

var mediaTypes = map[string]string{
	".html": "text/html; charset=utf-8", ".js": "text/javascript; charset=utf-8",
	".css": "text/css; charset=utf-8", ".json": "application/json; charset=utf-8",
	".svg": "image/svg+xml", ".png": "image/png", ".jpg": "image/jpeg",
	".jpeg": "image/jpeg", ".webp": "image/webp", ".avif": "image/avif",
	".gif": "image/gif", ".ico": "image/x-icon", ".woff2": "font/woff2",
	".txt": "text/plain; charset=utf-8",
}

func (s *Server) serveMedia(w http.ResponseWriter, r *http.Request) {
	name := r.PathValue("name")
	if name == "" || strings.Contains(name, "..") || strings.ContainsRune(name, 0) {
		http.Error(w, "forbidden", http.StatusForbidden)
		return
	}
	root := s.Settings.AssetsDir()
	file := filepath.Join(root, filepath.FromSlash(name))
	if !strings.HasPrefix(file, root+string(filepath.Separator)) {
		http.Error(w, "forbidden", http.StatusForbidden)
		return
	}
	info, err := os.Stat(file)
	if err != nil || info.IsDir() {
		http.Error(w, "not found", http.StatusNotFound)
		return
	}
	// Media names carry a content hash: they never change.
	w.Header().Set("Cache-Control", "public, max-age=31536000, immutable")
	serveFile(w, r, file, info)
}

func (s *Server) serveApp(w http.ResponseWriter, r *http.Request) {
	if strings.HasPrefix(r.URL.Path, "/api/") {
		fail(w, http.StatusNotFound, "unknown endpoint "+r.Method+" "+r.URL.Path)
		return
	}
	if strings.Contains(r.URL.Path, ".") && r.URL.Path != "/" {
		// A missing static file must not fall back to HTML: the browser would
		// read HTML as JavaScript and show a blank page.
		http.Error(w, "not found", http.StatusNotFound)
		return
	}
	raw, err := fs.ReadFile(webFS, "static/index.html")
	if err != nil {
		http.Error(w, "the reading page is missing from this build", http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("Cache-Control", "no-cache")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(raw)
}

func serveFile(w http.ResponseWriter, r *http.Request, file string, _ fs.FileInfo) {
	if ctype := mediaTypes[strings.ToLower(path.Ext(file))]; ctype != "" {
		w.Header().Set("Content-Type", ctype)
	}
	http.ServeFile(w, r, file)
}
