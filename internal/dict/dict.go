// Package dict reads Tern's offline dictionary: a small SQLite file built from
// ECDICT by tools/dict. It answers a tapped word instantly and without the
// model, so the reading page's lookup works with no runtime and no network.
//
// The file is optional: without it Open returns no dictionary and the server
// falls back to the agent.
package dict

import (
	"context"
	"errors"
	"fmt"
	"os"
	"strings"

	"github.com/egoist/mygo/plugins/sqlite"
)

// Entry is one headword as the reading page shows it.
type Entry struct {
	Term     string
	Phonetic string
	Pos      string
	ZH       string
	EN       string
}

// Dictionary is an open dictionary file. It is safe for concurrent use.
type Dictionary struct {
	db *sqlite.DB
}

// Open opens the dictionary at path read-only. A missing file is not an error:
// it returns (nil, nil) so callers just skip the offline path.
func Open(path string) (*Dictionary, error) {
	if strings.TrimSpace(path) == "" {
		return nil, nil
	}
	if _, err := os.Stat(path); errors.Is(err, os.ErrNotExist) {
		return nil, nil
	} else if err != nil {
		return nil, err
	}
	db, err := sqlite.Open(context.Background(), path, sqlite.OpenOptions{ReadOnly: true})
	if err != nil {
		return nil, fmt.Errorf("dictionary %s: %w", path, err)
	}
	return &Dictionary{db: db}, nil
}

// Close releases the file.
func (d *Dictionary) Close() error {
	if d == nil || d.db == nil {
		return nil
	}
	return d.db.Close()
}

// Lookup finds a headword, or the base word of an inflected form.
func (d *Dictionary) Lookup(term string) (Entry, bool, error) {
	if d == nil || d.db == nil {
		return Entry{}, false, nil
	}
	lower := strings.ToLower(strings.TrimSpace(term))
	if lower == "" {
		return Entry{}, false, nil
	}
	ctx := context.Background()

	rows, err := d.db.Query(ctx, `
		SELECT word, phonetic, pos, zh, en
		FROM word WHERE lower = ? LIMIT 1`, lower)
	if err != nil {
		return Entry{}, false, err
	}
	if entry, ok := firstEntry(rows); ok {
		return entry, true, nil
	}

	rows, err = d.db.Query(ctx, `
		SELECT w.word, w.phonetic, w.pos, w.zh, w.en
		FROM form f JOIN word w ON w.id = f.word_id
		WHERE f.form = ? LIMIT 1`, lower)
	if err != nil {
		return Entry{}, false, err
	}
	entry, ok := firstEntry(rows)
	return entry, ok, nil
}

func firstEntry(rows sqlite.Rows) (Entry, bool) {
	if len(rows.Values) == 0 {
		return Entry{}, false
	}
	value := rows.Values[0]
	if len(value) < 5 {
		return Entry{}, false
	}
	return Entry{
		Term:     asString(value[0]),
		Phonetic: asString(value[1]),
		Pos:      asString(value[2]),
		ZH:       asString(value[3]),
		EN:       asString(value[4]),
	}, true
}

func asString(value any) string {
	if s, ok := value.(string); ok {
		return s
	}
	if value == nil {
		return ""
	}
	return fmt.Sprint(value)
}
