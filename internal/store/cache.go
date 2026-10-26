package store

import (
	"context"
	"encoding/json"
	"log"
	"os"
	"strings"

	"github.com/egoist/mygo/plugins/sqlite"

	"github.com/solidSpoon/tern-go/internal/model"
)

// The lookup cache and, later, the vocabulary book live in one writable SQLite
// file next to the library. Its word columns are the offline dictionary's
// (word, phonetic, pos, zh, en), so a cached entry and a dictionary entry read
// the same way; example and the two timestamps are the parts only user data
// has.
const cacheSchema = `
CREATE TABLE IF NOT EXISTS word (
	lower      TEXT PRIMARY KEY,
	word       TEXT NOT NULL,
	phonetic   TEXT NOT NULL DEFAULT '',
	pos        TEXT NOT NULL DEFAULT '',
	zh         TEXT NOT NULL DEFAULT '',
	en         TEXT NOT NULL DEFAULT '',
	example_en TEXT NOT NULL DEFAULT '',
	example_zh TEXT NOT NULL DEFAULT '',
	cached_at  TEXT NOT NULL DEFAULT '',
	saved_at   TEXT NOT NULL DEFAULT ''
)`

const cacheSavedIndex = `CREATE INDEX IF NOT EXISTS word_saved ON word(saved_at)`

// dictEntry is one value of the old dict.json cache.
type dictEntry struct {
	Entry model.Vocab `json:"entry"`
	At    string      `json:"at"`
}

// cacheDB opens the database once and imports dict.json into it the first
// time. A nil result means SQLite is unavailable: lookups still work, they
// just do not cache.
func (s *Store) cacheDB() *sqlite.DB {
	s.cacheMu.Lock()
	defer s.cacheMu.Unlock()
	if s.cacheReady {
		return s.cache
	}
	s.cacheReady = true

	ctx := context.Background()
	db, err := sqlite.Open(ctx, s.cachePath, sqlite.OpenOptions{})
	if err != nil {
		log.Printf("lookup cache: %v", err)
		return nil
	}
	for _, statement := range []string{cacheSchema, cacheSavedIndex} {
		if _, err := db.Execute(ctx, statement); err != nil {
			log.Printf("lookup cache: %v", err)
			_ = db.Close()
			return nil
		}
	}
	importDictJSON(ctx, db, s.cacheLegacy)
	s.cache = db
	return db
}

// importDictJSON moves the old file's entries into the database and keeps the
// file as dict.json.imported; a missing or empty file is fine.
func importDictJSON(ctx context.Context, db *sqlite.DB, path string) {
	if path == "" {
		return
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		return
	}
	var old map[string]dictEntry
	if err := json.Unmarshal(raw, &old); err != nil || len(old) == 0 {
		return
	}
	if rows, err := db.Query(ctx, "SELECT count(*) FROM word"); err != nil ||
		(len(rows.Values) == 1 && asInt(rows.Values[0][0]) > 0) {
		return
	}
	statements := make([]sqlite.Statement, 0, len(old))
	for _, item := range old {
		statements = append(statements, writeWord(item.Entry, item.At))
	}
	if _, err := db.Transaction(ctx, statements); err != nil {
		log.Printf("lookup cache: importing dict.json: %v", err)
		return
	}
	_ = os.Rename(path, path+".imported")
	log.Printf("lookup cache: imported %d words from %s", len(old), path)
}

// CachedEntry returns the AI's entry for term, if it was cached before.
func (s *Store) CachedEntry(term string) *model.Vocab {
	db := s.cacheDB()
	if db == nil {
		return nil
	}
	rows, err := db.Query(context.Background(), `
		SELECT word, phonetic, pos, zh, en, example_en, example_zh
		FROM word WHERE lower = ? AND cached_at != '' LIMIT 1`,
		strings.ToLower(term))
	if err != nil {
		log.Printf("lookup cache: %v", err)
		return nil
	}
	if len(rows.Values) == 0 {
		return nil
	}
	value := rows.Values[0]
	entry := model.Vocab{
		Term:     asString(value[0]),
		Phonetic: asString(value[1]),
		Pos:      asString(value[2]),
		ZH:       asString(value[3]),
		EN:       asString(value[4]),
	}
	if exampleEN, exampleZH := asString(value[5]), asString(value[6]); exampleEN != "" || exampleZH != "" {
		entry.Example = &model.Example{EN: exampleEN, ZH: exampleZH}
	}
	return &entry
}

// CacheEntry stores one AI lookup.
func (s *Store) CacheEntry(term string, entry model.Vocab) error {
	db := s.cacheDB()
	if db == nil {
		return nil
	}
	statement := writeWord(entry, Now())
	_, err := db.Execute(context.Background(), statement.SQL, statement.Args...)
	return err
}

// writeWord is the upsert shared by the cache and the import. saved_at is left
// alone, so caching a word never drops it from the vocabulary book.
func writeWord(entry model.Vocab, at string) sqlite.Statement {
	exampleEN, exampleZH := "", ""
	if entry.Example != nil {
		exampleEN, exampleZH = entry.Example.EN, entry.Example.ZH
	}
	return sqlite.Statement{
		SQL: `INSERT INTO word (lower, word, phonetic, pos, zh, en, example_en, example_zh, cached_at)
		      VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)
		      ON CONFLICT(lower) DO UPDATE SET
		        word = excluded.word,
		        phonetic = excluded.phonetic,
		        pos = excluded.pos,
		        zh = excluded.zh,
		        en = excluded.en,
		        example_en = excluded.example_en,
		        example_zh = excluded.example_zh,
		        cached_at = excluded.cached_at`,
		Args: []any{
			strings.ToLower(entry.Term), entry.Term, entry.Phonetic, entry.Pos,
			entry.ZH, entry.EN, exampleEN, exampleZH, at,
		},
	}
}

func asString(value any) string {
	if text, ok := value.(string); ok {
		return text
	}
	if value == nil {
		return ""
	}
	return ""
}

func asInt(value any) int64 {
	switch n := value.(type) {
	case int64:
		return n
	case float64:
		return int64(n)
	}
	return 0
}
