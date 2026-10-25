package pi

import (
	_ "embed"
	"os"
)

// extensionSource is Tern's Pi extension: the two tools the agent gets.
// It is embedded in the binary (the Go counterpart of go:embed of an asset) and
// written where Pi can load it at every start, so upgrading Tern upgrades the
// tools.
//
//go:embed extension.ts
var extensionSource string

// EnsureExtension writes the extension to data/pi/tern-extension.ts when it
// changed, and returns its path.
func (r *Runtime) EnsureExtension(dir, file string) (string, error) {
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", err
	}
	current, err := os.ReadFile(file)
	if err != nil || string(current) != extensionSource {
		if err := os.WriteFile(file, []byte(extensionSource), 0o644); err != nil {
			return "", err
		}
	}
	return file, nil
}
