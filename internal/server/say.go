package server

import (
	"crypto/sha1"
	"encoding/hex"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
)

// spokenWord is what may be handed to the Mac's speech synthesizer: one word,
// or a short phrase, and nothing that could be read as an option.
var spokenWord = regexp.MustCompile(`^[A-Za-z][A-Za-z' -]{0,39}$`)

// speechLock serializes generation, so two taps on the same word make one file.
var speechLock sync.Mutex

// say returns an m4a of the word, spoken by the Mac and cached under the data
// directory. The phone plays it with an audio element, which works where iOS's
// speech synthesis is unreliable.
func (s *Server) say(w http.ResponseWriter, r *http.Request) {
	term := strings.TrimSpace(r.URL.Query().Get("term"))
	if !spokenWord.MatchString(term) {
		fail(w, http.StatusBadRequest, "no word to say")
		return
	}
	sum := sha1.Sum([]byte(strings.ToLower(term)))
	dir := filepath.Join(s.Settings.Paths().Data, "say")
	file := filepath.Join(dir, hex.EncodeToString(sum[:])+".m4a")

	speechLock.Lock()
	if _, err := os.Stat(file); err != nil {
		if err := speakToFile(term, file); err != nil {
			speechLock.Unlock()
			log.Printf("speech: %v", err)
			fail(w, http.StatusServiceUnavailable, "the Mac could not speak that word")
			return
		}
	}
	speechLock.Unlock()

	w.Header().Set("Content-Type", "audio/mp4")
	w.Header().Set("Cache-Control", "public, max-age=86400")
	http.ServeFile(w, r, file)
}

// speakToFile writes the word to a temporary aiff, converts it to AAC in an
// m4a, and renames it into place only when both steps worked.
func speakToFile(term, out string) error {
	dir := filepath.Dir(out)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	tmp, err := os.CreateTemp(dir, "say-*.aiff")
	if err != nil {
		return err
	}
	aiff := tmp.Name()
	tmp.Close()
	defer os.Remove(aiff)

	if err := exec.Command("/usr/bin/say", "-v", "Samantha", "-o", aiff, term).Run(); err != nil {
		// A missing Samantha voice is not a reason to stay silent.
		if err := exec.Command("/usr/bin/say", "-o", aiff, term).Run(); err != nil {
			return fmt.Errorf("say: %w", err)
		}
	}

	part := out + ".tmp"
	if err := exec.Command("/usr/bin/afconvert", "-f", "m4af", "-d", "aac", "-b", "64000", aiff, part).Run(); err != nil {
		os.Remove(part)
		return fmt.Errorf("afconvert: %w", err)
	}
	return os.Rename(part, out)
}
