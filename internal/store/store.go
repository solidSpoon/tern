// Package store keeps Tern's data in plain JSON files, written through one
// serial queue. No native modules, no database: a single reader's library is
// small, and readable files are easier to repair.
package store

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/solidSpoon/tern-go/internal/config"
	"github.com/solidSpoon/tern-go/internal/model"
)

// ErrNotFound is returned when an article does not exist.
var ErrNotFound = errors.New("article not found")

var safeID = regexp.MustCompile(`^[a-z0-9][a-z0-9-]{0,80}$`)

// Store is the JSON library, dictionary and vocabulary book.
type Store struct {
	paths config.Paths
	mu    sync.Mutex
}

// New makes a store under the given paths, creating the directories.
func New(paths config.Paths) (*Store, error) {
	for _, dir := range []string{paths.Library, paths.Data} {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return nil, err
		}
	}
	return &Store{paths: paths}, nil
}

// Paths exposes the directories the store works in.
func (s *Store) Paths() config.Paths { return s.paths }

// SetPaths points the store at another root (the reader chose a data
// directory). Writes in flight finish first.
func (s *Store) SetPaths(paths config.Paths) error {
	for _, dir := range []string{paths.Library, paths.Data} {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return err
		}
	}
	s.mu.Lock()
	s.paths = paths
	s.mu.Unlock()
	return nil
}

// ---------------------------------------------------------------- fs helpers

// Lock runs fn while holding the store's write lock, so that concurrent
// requests never interleave half-written files.
func (s *Store) Lock(fn func() error) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	return fn()
}

// ReadJSON reads a JSON file, returning fallback when it is missing or broken.
func ReadJSON(file string, fallback any) any {
	raw, err := os.ReadFile(file)
	if err != nil {
		return fallback
	}
	var out any
	if err := json.Unmarshal(raw, &out); err != nil {
		return fallback
	}
	return out
}

// WriteJSONAtomic writes JSON through a temporary file and a rename, so a crash
// never leaves half a document behind.
func WriteJSONAtomic(file string, data any) error {
	if err := os.MkdirAll(filepath.Dir(file), 0o755); err != nil {
		return err
	}
	raw, err := json.MarshalIndent(data, "", "  ")
	if err != nil {
		return err
	}
	tmp := file + ".tmp"
	if err := os.WriteFile(tmp, append(raw, '\n'), 0o644); err != nil {
		return err
	}
	return os.Rename(tmp, file)
}

// Slug turns a title into an id: lowercase ASCII words joined by dashes.
func Slug(input string) string {
	var b strings.Builder
	dash := false
	for _, r := range strings.ToLower(input) {
		switch {
		case r >= 'a' && r <= 'z', r >= '0' && r <= '9':
			b.WriteRune(r)
			dash = false
		default:
			if !dash && b.Len() > 0 {
				b.WriteByte('-')
				dash = true
			}
		}
		if b.Len() >= 60 {
			break
		}
	}
	out := strings.Trim(b.String(), "-")
	if out == "" {
		return randomHex(4)
	}
	return out
}

func randomHex(n int) string {
	buf := make([]byte, n)
	if _, err := rand.Read(buf); err != nil {
		return strings.Repeat("0", n*2)
	}
	return hex.EncodeToString(buf)
}

// NewID makes a short random id, for threads.
func NewID() string {
	buf := make([]byte, 6)
	if _, err := rand.Read(buf); err != nil {
		return time.Now().Format("150405.000000")
	}
	return hex.EncodeToString(buf)
}

// Now is the timestamp format used across the files.
func Now() string { return time.Now().UTC().Format(time.RFC3339) }

// ---------------------------------------------------------------- articles

// ListArticles returns the feed, newest first, without article bodies.
func (s *Store) ListArticles() []model.ArticleSummary {
	out := []model.ArticleSummary{}
	entries, err := os.ReadDir(s.paths.Library)
	if err != nil {
		return out
	}
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".json") {
			continue
		}
		var article model.Article
		raw, err := os.ReadFile(filepath.Join(s.paths.Library, entry.Name()))
		if err != nil || json.Unmarshal(raw, &article) != nil {
			continue
		}
		if article.ID == "" || len(article.Blocks) == 0 {
			continue
		}
		out = append(out, article.SummaryView())
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].CreatedAt == out[j].CreatedAt {
			return out[i].ID < out[j].ID
		}
		return out[i].CreatedAt > out[j].CreatedAt
	})
	return out
}

// Article returns one article, or ErrNotFound.
func (s *Store) Article(id string) (*model.Article, error) {
	if !safeID.MatchString(id) {
		return nil, ErrNotFound
	}
	raw, err := os.ReadFile(filepath.Join(s.paths.Library, id+".json"))
	if err != nil {
		return nil, ErrNotFound
	}
	var article model.Article
	if err := json.Unmarshal(raw, &article); err != nil {
		return nil, ErrNotFound
	}
	return &article, nil
}

// SaveArticle validates and writes an article.
func (s *Store) SaveArticle(article *model.Article) error {
	if issues := article.Validate(); len(issues) > 0 {
		return issues
	}
	return s.Lock(func() error {
		return WriteJSONAtomic(filepath.Join(s.paths.Library, article.ID+".json"), article)
	})
}

// DeleteArticle removes an article, returning whether it existed.
func (s *Store) DeleteArticle(id string) bool {
	if !safeID.MatchString(id) {
		return false
	}
	removed := false
	_ = s.Lock(func() error {
		err := os.Remove(filepath.Join(s.paths.Library, id+".json"))
		removed = err == nil
		return nil
	})
	return removed
}

// FreeID returns an unused article id based on base.
func (s *Store) FreeID(base string) string {
	id := Slug(base)
	candidate := id
	for n := 2; ; n++ {
		if _, err := os.Stat(filepath.Join(s.paths.Library, candidate+".json")); os.IsNotExist(err) {
			return candidate
		}
		candidate = id + "-" + itoa(n)
	}
}

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	var digits []byte
	for n > 0 {
		digits = append([]byte{byte('0' + n%10)}, digits...)
		n /= 10
	}
	return string(digits)
}

// ---------------------------------------------------------------- vocabulary

// Vocab returns the reader's vocabulary book, newest first.
func (s *Store) Vocab() []model.Vocab {
	var list []model.Vocab
	raw, err := os.ReadFile(s.paths.Vocab)
	if err == nil {
		_ = json.Unmarshal(raw, &list)
	}
	if list == nil {
		return []model.Vocab{}
	}
	return list
}

// AddVocab inserts or replaces a word, newest first.
func (s *Store) AddVocab(entry model.Vocab) ([]model.Vocab, error) {
	var list []model.Vocab
	err := s.Lock(func() error {
		current := s.Vocab()
		list = make([]model.Vocab, 0, len(current)+1)
		for _, item := range current {
			if !strings.EqualFold(item.Term, entry.Term) {
				list = append(list, item)
			}
		}
		entry.SavedAt = Now()
		list = append([]model.Vocab{entry}, list...)
		return WriteJSONAtomic(s.paths.Vocab, list)
	})
	return list, err
}

// RemoveVocab deletes a word.
func (s *Store) RemoveVocab(term string) ([]model.Vocab, error) {
	var list []model.Vocab
	err := s.Lock(func() error {
		current := s.Vocab()
		list = make([]model.Vocab, 0, len(current))
		for _, item := range current {
			if !strings.EqualFold(item.Term, term) {
				list = append(list, item)
			}
		}
		return WriteJSONAtomic(s.paths.Vocab, list)
	})
	return list, err
}

// ---------------------------------------------------------------- dictionary

type dictEntry struct {
	Entry model.Vocab `json:"entry"`
	At    string      `json:"at"`
}

func (s *Store) readDict() map[string]dictEntry {
	cache := map[string]dictEntry{}
	raw, err := os.ReadFile(s.paths.Dict)
	if err == nil {
		_ = json.Unmarshal(raw, &cache)
	}
	return cache
}

// CachedEntry returns a cached dictionary entry for term.
func (s *Store) CachedEntry(term string) *model.Vocab {
	entry, ok := s.readDict()[strings.ToLower(term)]
	if !ok {
		return nil
	}
	return &entry.Entry
}

// CacheEntry stores one entry.
func (s *Store) CacheEntry(term string, entry model.Vocab) error {
	return s.Lock(func() error {
		cache := s.readDict()
		cache[strings.ToLower(term)] = dictEntry{Entry: entry, At: Now()}
		return WriteJSONAtomic(s.paths.Dict, cache)
	})
}

// CacheEntries warms the dictionary with a whole article's words, and returns
// how many were new.
func (s *Store) CacheEntries(entries []model.Vocab) (int, error) {
	added := 0
	err := s.Lock(func() error {
		cache := s.readDict()
		now := Now()
		for _, entry := range entries {
			key := strings.ToLower(entry.Term)
			if _, ok := cache[key]; ok {
				continue
			}
			cache[key] = dictEntry{Entry: entry, At: now}
			added++
		}
		if added == 0 {
			return nil
		}
		return WriteJSONAtomic(s.paths.Dict, cache)
	})
	return added, err
}

// NormalizeTerm lowercases a word and trims punctuation, as a lookup treats it.
func NormalizeTerm(raw string) string {
	term := strings.ToLower(strings.TrimSpace(raw))
	term = strings.Trim(term, " \t\n\r.,;:!?\"'“”‘’()[]{}<>«»…—-–")
	if strings.ContainsAny(term, "0123456789/_@#") {
		return ""
	}
	return term
}
