// Package config holds Tern's paths and the settings it keeps on disk.
//
// Everything lives under one root: data/ for JSON, media/ for images. The root
// is $TERN_HOME, or the working directory. The root may be overridden at any
// time with TERN_MEDIA_DIR for the media folder alone.
package config

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
)

// The provider and its model list. Changing provider means changing the
// endpoint and the fallback choices here.
const (
	Provider   = "deepseek"
	DefaultMdl = "deepseek-flash"
	// ModelsURL is the provider's OpenAI-compatible model list.
	ModelsURL = "https://api.deepseek.com/models"
	// ModelChoice is the second fallback, for when the list cannot be fetched.
	ModelChoice = "deepseek-v4-pro"
)

// Agent is the model configuration. The key never leaves the machine: it is
// handed to the Pi child process through its environment, and no HTTP API
// returns it.
type Agent struct {
	// Provider is "pi" (call the model) or "demo" (never call it).
	Provider  string `json:"provider"`
	APIKey    string `json:"apiKey,omitempty"`
	Model     string `json:"model,omitempty"`
	TimeoutMS int    `json:"timeoutMs,omitempty"`
}

// Reading is how the article page was last read. The reader remembers it
// across launches, as the phone page remembers it in localStorage.
type Reading struct {
	Mode string `json:"mode,omitempty"`
	Size string `json:"size,omitempty"`
}

// Config is data/config.json.
type Config struct {
	AssetsDir string `json:"assetsDir"`
	// Port is the reading server's port; 0 until the reader chooses one (then
	// $PORT, else 8787).
	Port    int     `json:"port,omitempty"`
	Agent   Agent   `json:"agent"`
	Reading Reading `json:"reading"`
}

// Paths are the directories of one root.
type Paths struct {
	Root      string
	Data      string
	Library   string
	Threads   string
	Media     string
	Pi        string
	Runtime   string
	Config    string
	Dict      string
	DB        string
	Extension string
}

// PathsFor lays the directories out under root.
func PathsFor(root string) Paths {
	data := filepath.Join(root, "data")
	pi := filepath.Join(data, "pi")
	return Paths{
		Root:      root,
		Data:      data,
		Library:   filepath.Join(data, "library"),
		Threads:   filepath.Join(data, "threads"),
		Media:     filepath.Join(root, "media"),
		Pi:        pi,
		Runtime:   filepath.Join(data, "runtime", "pi"),
		Config:    filepath.Join(data, "config.json"),
		Dict:      filepath.Join(data, "dict.json"),
		DB:        filepath.Join(data, "tern.db"),
		Extension: filepath.Join(pi, "tern-extension.ts"),
	}
}

// Settings is the resolved configuration, with its paths.
type Settings struct {
	paths Paths
	cfg   Config
	mu    sync.RWMutex
}

// AppFolder is Tern's own folder, outside any data root: it remembers which
// root the reader chose, so the library never has to live in the app's source
// folder.
func AppFolder() string {
	dir, err := os.UserConfigDir()
	if err != nil {
		return filepath.Join(os.TempDir(), "Tern")
	}
	return filepath.Join(dir, "Tern")
}

func rootPointer() string { return filepath.Join(AppFolder(), "root.txt") }

// Root resolves the data root: $TERN_HOME, else the directory chosen in
// Settings, else ~/Library/Application Support/Tern.
func Root() string {
	if dir := os.Getenv("TERN_HOME"); dir != "" {
		if abs, err := filepath.Abs(dir); err == nil {
			return abs
		}
		return dir
	}
	if raw, err := os.ReadFile(rootPointer()); err == nil {
		if dir := strings.TrimSpace(string(raw)); dir != "" {
			return dir
		}
	}
	return AppFolder()
}

// RecordRoot remembers the chosen root for the next launch.
func RecordRoot(root string) error {
	if err := os.MkdirAll(AppFolder(), 0o755); err != nil {
		return err
	}
	return os.WriteFile(rootPointer(), []byte(root+"\n"), 0o644)
}

// Default is the configuration of a fresh install.
func Default(paths Paths) Config {
	return Config{
		AssetsDir: paths.Media,
		Agent: Agent{
			Provider:  "pi",
			Model:     DefaultMdl,
			TimeoutMS: 900_000,
		},
		Reading: Reading{Mode: "dual", Size: "md"},
	}
}

// Load reads data/config.json, writing it first when it does not exist.
func Load(root string) (*Settings, error) {
	paths := PathsFor(root)
	for _, dir := range []string{paths.Data, paths.Library, paths.Threads, paths.Pi, paths.Runtime} {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return nil, err
		}
	}

	s := &Settings{paths: paths, cfg: Default(paths)}
	if raw, err := os.ReadFile(paths.Config); err == nil {
		var parsed Config
		if json.Unmarshal(raw, &parsed) == nil {
			s.cfg = merge(Default(paths), parsed)
		}
	}
	if dir := os.Getenv("TERN_MEDIA_DIR"); dir != "" {
		if abs, err := filepath.Abs(dir); err == nil {
			s.cfg.AssetsDir = abs
		}
	}
	if key := os.Getenv("DEEPSEEK_API_KEY"); key != "" && s.cfg.Agent.APIKey == "" {
		s.cfg.Agent.APIKey = key
	}
	if err := os.MkdirAll(s.cfg.AssetsDir, 0o755); err != nil {
		return nil, err
	}
	if _, err := os.Stat(paths.Config); os.IsNotExist(err) {
		if err := s.Save(s.cfg); err != nil {
			return nil, err
		}
	}
	return s, nil
}

func merge(base, parsed Config) Config {
	if parsed.AssetsDir != "" {
		base.AssetsDir = parsed.AssetsDir
	}
	if parsed.Agent.Provider == "demo" {
		base.Agent.Provider = "demo"
	} else if parsed.Agent.Provider != "" {
		base.Agent.Provider = "pi"
	}
	if parsed.Agent.APIKey != "" {
		base.Agent.APIKey = parsed.Agent.APIKey
	}
	if parsed.Agent.Model != "" {
		base.Agent.Model = parsed.Agent.Model
	}
	if parsed.Agent.TimeoutMS > 0 {
		base.Agent.TimeoutMS = parsed.Agent.TimeoutMS
	}
	if parsed.Port > 0 {
		base.Port = parsed.Port
	}
	if parsed.Reading.Mode != "" {
		base.Reading.Mode = parsed.Reading.Mode
	}
	if parsed.Reading.Size != "" {
		base.Reading.Size = parsed.Reading.Size
	}
	return base
}

// Paths returns the directories.
func (s *Settings) Paths() Paths { return s.paths }

// Get returns a copy of the configuration.
func (s *Settings) Get() Config {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.cfg
}

// AssetsDir is the media folder, created if needed.
func (s *Settings) AssetsDir() string {
	s.mu.RLock()
	dir := s.cfg.AssetsDir
	s.mu.RUnlock()
	_ = os.MkdirAll(dir, 0o755)
	return dir
}

// Save writes the configuration with permissions 0600: it holds the key.
func (s *Settings) Save(next Config) error {
	if next.AssetsDir == "" {
		next.AssetsDir = s.paths.Media
	}
	if next.Agent.Provider == "" {
		next.Agent.Provider = "pi"
	}
	if next.Agent.Model == "" {
		next.Agent.Model = DefaultMdl
	}
	if next.Agent.TimeoutMS <= 0 {
		next.Agent.TimeoutMS = 900_000
	}
	if next.Reading.Mode == "" {
		next.Reading.Mode = "dual"
	}
	if next.Reading.Size == "" {
		next.Reading.Size = "md"
	}
	normalized := Config{AssetsDir: next.AssetsDir, Port: next.Port, Agent: next.Agent, Reading: next.Reading}

	raw, err := json.MarshalIndent(normalized, "", "  ")
	if err != nil {
		return err
	}
	if err := os.MkdirAll(s.paths.Data, 0o755); err != nil {
		return err
	}
	if err := os.WriteFile(s.paths.Config, append(raw, '\n'), 0o600); err != nil {
		return err
	}
	_ = os.Chmod(s.paths.Config, 0o600)

	s.mu.Lock()
	s.cfg = normalized
	s.mu.Unlock()
	_ = os.MkdirAll(normalized.AssetsDir, 0o755)
	return nil
}

// PatchAgent changes only the agent part of the configuration.
func (s *Settings) PatchAgent(patch Agent) error {
	next := s.Get()
	if patch.Provider != "" {
		next.Agent.Provider = patch.Provider
	}
	if patch.APIKey != "" {
		next.Agent.APIKey = patch.APIKey
	}
	if patch.Model != "" {
		next.Agent.Model = patch.Model
	}
	if patch.TimeoutMS > 0 {
		next.Agent.TimeoutMS = patch.TimeoutMS
	}
	return s.Save(next)
}

// SetPort saves the port the reading server listens on.
func (s *Settings) SetPort(port int) error {
	if port < 1024 || port > 65535 {
		return fmt.Errorf("the port must be between 1024 and 65535")
	}
	next := s.Get()
	next.Port = port
	return s.Save(next)
}

// SwitchRoot points the settings at another data root, keeping the agent and
// reading preferences. The article library, pictures, conversations and Pi
// runtime of the new root are used from then on.
func (s *Settings) SwitchRoot(root string) (Paths, error) {
	abs, err := filepath.Abs(root)
	if err != nil {
		return s.Paths(), err
	}
	next := PathsFor(abs)
	for _, dir := range []string{next.Data, next.Library, next.Threads, next.Pi, next.Runtime, next.Media} {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return s.Paths(), err
		}
	}

	current := s.Get()
	current.AssetsDir = next.Media

	s.mu.Lock()
	s.paths = next
	s.mu.Unlock()

	if err := s.Save(current); err != nil {
		return next, err
	}
	return next, nil
}

// SaveReading remembers how the reader reads.
func (s *Settings) SaveReading(mode, size string) error {
	next := s.Get()
	if next.Reading.Mode == mode && next.Reading.Size == size {
		return nil
	}
	next.Reading = Reading{Mode: mode, Size: size}
	return s.Save(next)
}

// ClearAPIKey removes the stored key.
func (s *Settings) ClearAPIKey() error {
	next := s.Get()
	next.Agent.APIKey = ""
	return s.Save(next)
}

// ModelChoices is the fallback list Settings offers when the provider's model
// list cannot be fetched, with the configured one first when it is not one of
// them.
func (s *Settings) ModelChoices() []string {
	current := s.Get().Agent.Model
	out := []string{DefaultMdl, ModelChoice}
	if current == "" || current == DefaultMdl || current == ModelChoice {
		return out
	}
	return append([]string{current}, out...)
}

// KeyHint is the masked form of a key, for the settings page.
func KeyHint(key string) string {
	if len(key) <= 8 {
		return ""
	}
	return fmt.Sprintf("%s…%s", key[:4], key[len(key)-4:])
}
