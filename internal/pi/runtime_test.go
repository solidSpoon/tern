package pi

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"os"
	"path/filepath"
	"testing"
)

// makeArchive builds a tar.gz the way a release does: everything under pi/.
func makeArchive(t *testing.T, entries map[string]string) []byte {
	t.Helper()
	var buf bytes.Buffer
	gz := gzip.NewWriter(&buf)
	tw := tar.NewWriter(gz)
	for name, body := range entries {
		header := &tar.Header{Name: name, Mode: 0o644, Size: int64(len(body)), Typeflag: tar.TypeReg}
		if err := tw.WriteHeader(header); err != nil {
			t.Fatal(err)
		}
		if _, err := tw.Write([]byte(body)); err != nil {
			t.Fatal(err)
		}
	}
	if err := tw.Close(); err != nil {
		t.Fatal(err)
	}
	if err := gz.Close(); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

func TestExtractTarGz(t *testing.T) {
	target := filepath.Join(t.TempDir(), "1.2.3")
	archive := makeArchive(t, map[string]string{
		"pi/pi":             "#!/bin/sh\necho 1.2.3\n",
		"pi/docs/readme.md": "docs",
		"pi/../escape":      "nope",
	})
	files, err := extractTarGz(archive, target)
	if err != nil {
		t.Fatal(err)
	}
	if files != 2 {
		t.Errorf("extracted %d files, want 2", files)
	}
	binary := filepath.Join(target, "pi")
	info, err := os.Stat(binary)
	if err != nil {
		t.Fatalf("the binary is missing: %v", err)
	}
	if info.Mode()&0o111 == 0 {
		t.Error("the binary is not executable")
	}
	if _, err := os.Stat(filepath.Join(target, "docs", "readme.md")); err != nil {
		t.Errorf("docs were not unpacked: %v", err)
	}
	if _, err := os.Stat(filepath.Join(filepath.Dir(target), "escape")); err == nil {
		t.Error("a path outside the target was written")
	}
}

func TestExtractRejectsArchiveWithoutBinary(t *testing.T) {
	if _, err := extractTarGz(makeArchive(t, map[string]string{"pi/readme.md": "hi"}), filepath.Join(t.TempDir(), "1")); err == nil {
		t.Fatal("an archive without pi was accepted")
	}
}

func TestCompareVersions(t *testing.T) {
	if compareVersions("1.10.0", "1.9.9") <= 0 {
		t.Error("1.10.0 should be newer than 1.9.9")
	}
	if compareVersions("1.4.0", "1.4.0") != 0 {
		t.Error("a version differs from itself")
	}
	if compareVersions("1.3.9", "1.4.0") >= 0 {
		t.Error("1.3.9 should be older than 1.4.0")
	}
}
