// Command dict builds Tern's offline dictionary from ECDICT's stardict.db.
//
//	go run ./tools/dict -source /path/to/stardict.db -out resources/dictionary.db
//
// The output is a small SQLite file with only what a reader needs: the common
// headwords (ECDICT's frequency- or exam-tagged ones), a normalized phonetic,
// the part of speech, the Chinese translation and the English definition, plus
// the inflected forms that map back to a headword ("sundials" → "sundial").
//
// The database is read at run time by internal/dict through MyGo's SQLite
// plugin, so no database driver is linked into the app.
package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"github.com/egoist/mygo/plugins/sqlite"
)

func main() {
	source := flag.String("source", "", "ECDICT stardict.db (required)")
	out := flag.String("out", "resources/dictionary.db", "output database")
	flag.Parse()
	if *source == "" {
		fmt.Fprintln(os.Stderr, "usage: dict -source stardict.db [-out resources/dictionary.db]")
		os.Exit(2)
	}
	if err := build(*source, *out); err != nil {
		fmt.Fprintln(os.Stderr, "dict:", err)
		os.Exit(1)
	}
}

func build(source, out string) error {
	ctx := context.Background()
	if _, err := os.Stat(source); err != nil {
		return fmt.Errorf("source: %w", err)
	}
	if err := os.MkdirAll(filepath.Dir(out), 0o755); err != nil {
		return err
	}

	src, err := sqlite.Open(ctx, source, sqlite.OpenOptions{ReadOnly: true})
	if err != nil {
		return err
	}
	defer src.Close()

	tmp := out + ".tmp"
	_ = os.Remove(tmp)
	db, err := sqlite.Open(ctx, tmp, sqlite.OpenOptions{})
	if err != nil {
		return err
	}
	defer db.Close()

	run := func(sql string, args ...any) error {
		if _, err := db.Execute(ctx, sql, args...); err != nil {
			return fmt.Errorf("%w\n%s", err, firstLine(sql))
		}
		return nil
	}

	for _, pragma := range []string{
		"PRAGMA journal_mode=OFF",
		"PRAGMA synchronous=OFF",
		"PRAGMA cache_size=-64000",
	} {
		if err := run(pragma); err != nil {
			return err
		}
	}

	schema := []string{
		`CREATE TABLE word (
			id       INTEGER PRIMARY KEY,
			word     TEXT NOT NULL,
			lower    TEXT NOT NULL UNIQUE,
			phonetic TEXT NOT NULL DEFAULT '',
			pos      TEXT NOT NULL DEFAULT '',
			zh       TEXT NOT NULL DEFAULT '',
			en       TEXT NOT NULL DEFAULT '',
			freq     INTEGER NOT NULL DEFAULT 0,
			bnc      INTEGER NOT NULL DEFAULT 0
		)`,
		`CREATE TABLE form (form TEXT NOT NULL, word_id INTEGER NOT NULL)`,
		`CREATE TABLE form_stage (form TEXT NOT NULL, word TEXT NOT NULL)`,
		`CREATE TABLE meta (key TEXT PRIMARY KEY, value TEXT NOT NULL)`,
	}
	for _, stmt := range schema {
		if err := run(stmt); err != nil {
			return err
		}
	}

	started := time.Now()
	const pageSize = 5000
	last := int64(0)
	wordCount, formCount := 0, 0
	for {
		rows, err := src.Query(ctx, `
			SELECT rowid, word, IFNULL(phonetic,''), IFNULL(pos,''),
			       IFNULL(translation,''), IFNULL(definition,''),
			       IFNULL(frq,0), IFNULL(bnc,0), IFNULL(exchange,'')
			FROM stardict
			WHERE rowid > ?
			  AND translation != ''
			  AND (frq > 0 OR bnc > 0 OR collins > 0 OR oxford > 0
			       OR (tag IS NOT NULL AND tag != ''))
			ORDER BY rowid
			LIMIT ?`, last, pageSize)
		if err != nil {
			return err
		}
		if len(rows.Values) == 0 {
			break
		}

		stmts := make([]sqlite.Statement, 0, len(rows.Values)*2)
		for _, value := range rows.Values {
			last = asInt(value[0])
			word := strings.TrimSpace(asString(value[1]))
			if word == "" {
				continue
			}
			zh := strings.TrimSpace(posPrefix.ReplaceAllString(asString(value[4]), ""))
			if zh == "" {
				continue
			}
			stmts = append(stmts, sqlite.Statement{
				SQL: `INSERT OR IGNORE INTO word (word, lower, phonetic, pos, zh, en, freq, bnc)
				      VALUES (?, ?, ?, ?, ?, ?, ?, ?)`,
				Args: []any{
					word, strings.ToLower(word), cleanPhonetic(asString(value[2])),
					cleanPOS(asString(value[3])), zh,
					strings.TrimSpace(posPrefix.ReplaceAllString(asString(value[5]), "")),
					asInt(value[6]), asInt(value[7]),
				},
			})
			wordCount++
			for _, form := range cleanForms(asString(value[8])) {
				stmts = append(stmts, sqlite.Statement{
					SQL:  "INSERT INTO form_stage (form, word) VALUES (?, ?)",
					Args: []any{form, word},
				})
				formCount++
			}
		}
		if _, err := db.Transaction(ctx, stmts); err != nil {
			return err
		}
	}

	// The forms are collected with their headword still as text; the join turns
	// them into ids in one pass instead of a lookup per row.
	if err := run(`
		INSERT INTO form (form, word_id)
		SELECT f.form, w.id
		FROM form_stage f JOIN word w ON w.lower = lower(f.word)`); err != nil {
		return err
	}
	if err := run(`CREATE INDEX form_form ON form(form)`); err != nil {
		return err
	}
	if err := run(`DROP TABLE form_stage`); err != nil {
		return err
	}

	var words, forms int64
	if rows, err := db.Query(ctx, "SELECT (SELECT count(*) FROM word), (SELECT count(*) FROM form)"); err != nil {
		return err
	} else if len(rows.Values) == 1 {
		words = asInt(rows.Values[0][0])
		forms = asInt(rows.Values[0][1])
	}
	for _, meta := range [][2]string{
		{"source", filepath.Base(source)},
		{"built_at", time.Now().UTC().Format(time.RFC3339)},
		{"words", fmt.Sprint(words)},
		{"forms", fmt.Sprint(forms)},
	} {
		if err := run("INSERT INTO meta (key, value) VALUES (?, ?)", meta[0], meta[1]); err != nil {
			return err
		}
	}
	if err := run("VACUUM"); err != nil {
		return err
	}
	if err := db.Close(); err != nil {
		return err
	}
	if err := os.Rename(tmp, out); err != nil {
		return err
	}
	size := int64(0)
	if info, err := os.Stat(out); err == nil {
		size = info.Size()
	}
	fmt.Printf("%s: %s words, %s forms, %.1f MB, %s\n",
		out, commas(words), commas(forms), float64(size)/(1<<20), time.Since(started).Round(time.Second))
	return nil
}

// cleanPOS keeps the major part of ECDICT's breakdown ("v:2/n:98" → "v.").
// posPrefix is the "n. " a translation starts with; the part of speech has
// its own column, so it is not repeated in the meaning.
var posPrefix = regexp.MustCompile(`^[a-zA-Z]+\.\s*`)

func cleanPOS(raw string) string {
	code := raw
	if i := strings.IndexByte(code, ':'); i > 0 {
		code = code[:i]
	}
	switch code {
	case "n":
		return "n."
	case "v":
		return "v."
	case "j":
		return "adj."
	case "r":
		return "adv."
	}
	return ""
}

// cleanPhonetic turns ECDICT's ASCII stress marks into IPA and its Cyrillic
// schwa into the Latin one.
func cleanPhonetic(raw string) string {
	return strings.TrimSpace(strings.NewReplacer(
		"'", "ˈ",
		",", "ˌ",
		"ә", "ə",
	).Replace(raw))
}

// cleanForms reads ECDICT's exchange column ("p:sat/d:sat/i:sitting/3:sits"):
// past, done, -ing, third person, comparative, superlative, plural.
func cleanForms(exchange string) []string {
	if exchange == "" {
		return nil
	}
	seen := map[string]bool{}
	var forms []string
	for _, pair := range strings.Split(exchange, "/") {
		if len(pair) < 3 || pair[1] != ':' {
			continue
		}
		switch pair[0] {
		case 'p', 'd', 'i', '3', 'r', 't', 's':
		default:
			continue
		}
		form := strings.ToLower(strings.TrimSpace(pair[2:]))
		if form == "" || seen[form] {
			continue
		}
		seen[form] = true
		forms = append(forms, form)
	}
	return forms
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

func asInt(value any) int64 {
	switch v := value.(type) {
	case int64:
		return v
	case int:
		return int64(v)
	case float64:
		return int64(v)
	case string:
		var n int64
		fmt.Sscan(v, &n)
		return n
	}
	return 0
}

func commas(n int64) string {
	s := fmt.Sprint(n)
	for i := len(s) - 3; i > 0; i -= 3 {
		s = s[:i] + "," + s[i:]
	}
	return s
}

func firstLine(sql string) string {
	sql = strings.TrimSpace(sql)
	if i := strings.IndexByte(sql, '\n'); i >= 0 {
		return strings.TrimSpace(sql[:i])
	}
	return sql
}
