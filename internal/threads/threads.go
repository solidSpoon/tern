// Package threads keeps Tern's conversations: one link, one conversation, one
// article. Ingestion is not a one-shot job but a conversation the reader can
// keep talking to ("make the third paragraph more colloquial"), and Pi's own
// session files carry the context across server restarts.
package threads

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"time"

	"github.com/solidSpoon/tern-go/internal/config"
	"github.com/solidSpoon/tern-go/internal/model"
	"github.com/solidSpoon/tern-go/internal/pi"
	"github.com/solidSpoon/tern-go/internal/publish"
	"github.com/solidSpoon/tern-go/internal/store"
)

var safeID = regexp.MustCompile(`^[a-z0-9][a-z0-9-]{0,40}$`)

// Manager owns the live conversations and their Pi processes.
type Manager struct {
	store    *store.Store
	agent    *pi.Agent
	settings *config.Settings
	importer *publish.Importer
	port     int
	// threadDir is data/threads.
	threadDir string

	mu    sync.Mutex
	live  map[string]*model.Thread
	busy  map[string]bool
	slots map[string]*slot
}

type slot struct {
	client  *pi.Client
	handler func(pi.Event)
	idle    *time.Timer
	// stream collects assistant deltas, in case the final message has no text.
	stream    strings.Builder
	turnStart time.Time
}

const idleClose = 10 * time.Minute

// New makes a manager. port is where the agent calls back to publish.
func New(st *store.Store, agent *pi.Agent, settings *config.Settings, importer *publish.Importer, port int) (*Manager, error) {
	dir := settings.Paths().Threads
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil, err
	}
	return &Manager{
		store:     st,
		agent:     agent,
		settings:  settings,
		importer:  importer,
		port:      port,
		threadDir: dir,
		live:      map[string]*model.Thread{},
		busy:      map[string]bool{},
		slots:     map[string]*slot{},
	}, nil
}

func (m *Manager) file(id string) string { return filepath.Join(m.threadDir, id+".json") }

// WorkDir is where a conversation's agent works: article.json lives here.
func (m *Manager) WorkDir(id string) string { return filepath.Join(m.threadDir, id, "work") }

// BusyAny reports whether any conversation is running.
func (m *Manager) BusyAny() bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	return len(m.busy) > 0
}

// SetPaths points the conversations at another root, closing the agents of the
// old one.
func (m *Manager) SetPaths(paths config.Paths) error {
	m.mu.Lock()
	if len(m.busy) > 0 {
		m.mu.Unlock()
		return fmt.Errorf("a conversation is still running; wait for it to finish before switching the data directory")
	}
	slots := m.slots
	m.slots = map[string]*slot{}
	m.live = map[string]*model.Thread{}
	m.threadDir = paths.Threads
	m.mu.Unlock()

	for _, holder := range slots {
		holder.client.Close()
	}
	return os.MkdirAll(paths.Threads, 0o755)
}

// UpdatePort tells the manager where callbacks should go (the HTTP server picks
// its port at start).
func (m *Manager) UpdatePort(port int) { m.port = port }

// ---------------------------------------------------------------- reading

func cloneThread(t *model.Thread) *model.Thread {
	out := *t
	out.Events = append([]model.ThreadEvent(nil), t.Events...)
	if out.Events == nil {
		out.Events = []model.ThreadEvent{}
	}
	return &out
}

// Get returns a conversation by id.
func (m *Manager) Get(id string) (*model.Thread, error) {
	if !safeID.MatchString(id) {
		return nil, fmt.Errorf("no such conversation")
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if existing, ok := m.live[id]; ok {
		m.reviveLocked(existing)
		return cloneThread(existing), nil
	}
	raw, err := os.ReadFile(m.file(id))
	if err != nil {
		return nil, fmt.Errorf("no such conversation")
	}
	var thread model.Thread
	if err := json.Unmarshal(raw, &thread); err != nil {
		return nil, fmt.Errorf("that conversation file is broken")
	}
	m.live[id] = &thread
	m.reviveLocked(&thread)
	return cloneThread(&thread), nil
}

// List returns every conversation, most recently touched first.
func (m *Manager) List() []model.ThreadSummary {
	entries, err := os.ReadDir(m.threadDir)
	if err != nil {
		return []model.ThreadSummary{}
	}
	out := []model.ThreadSummary{}
	m.mu.Lock()
	defer m.mu.Unlock()
	for _, entry := range entries {
		name := entry.Name()
		if entry.IsDir() || !strings.HasSuffix(name, ".json") {
			continue
		}
		id := strings.TrimSuffix(name, ".json")
		var thread *model.Thread
		if existing, ok := m.live[id]; ok {
			thread = existing
		} else {
			raw, err := os.ReadFile(filepath.Join(m.threadDir, name))
			if err != nil {
				continue
			}
			var parsed model.Thread
			if json.Unmarshal(raw, &parsed) != nil {
				continue
			}
			thread = &parsed
			m.live[id] = thread
		}
		m.reviveLocked(thread)
		out = append(out, thread.SummaryView())
	}
	for i := 1; i < len(out); i++ {
		for j := i; j > 0 && out[j].UpdatedAt > out[j-1].UpdatedAt; j-- {
			out[j], out[j-1] = out[j-1], out[j]
		}
	}
	return out
}

// Busy reports whether a turn is running.
func (m *Manager) Busy(id string) bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.busy[id]
}

// reviveLocked marks a conversation that was running when the process died as
// interrupted, so the interface never spins forever.
func (m *Manager) reviveLocked(t *model.Thread) {
	if t.State != model.ThreadRunning || m.busy[t.ID] {
		return
	}
	t.State = model.ThreadError
	t.Error = "the server restarted and this round was interrupted; send another message to continue"
	t.Events = append(t.Events, model.ThreadEvent{
		ID:   store.NewID()[:8],
		Type: model.EventStatus,
		Text: "the server restarted and this round was interrupted",
		At:   store.Now(),
	})
	t.UpdatedAt = store.Now()
	m.saveLocked(t)
}

func (m *Manager) saveLocked(t *model.Thread) {
	_ = store.WriteJSONAtomic(m.file(t.ID), t)
}

func (m *Manager) save(t *model.Thread) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.saveLocked(t)
}

func (m *Manager) pushLocked(t *model.Thread, event model.ThreadEvent) {
	t.Events = append(t.Events, event)
	t.UpdatedAt = event.At
	m.saveLocked(t)
}

func eventID() string { return store.NewID()[:8] }

// ---------------------------------------------------------------- processes

func (m *Manager) slotFor(t *model.Thread) (*slot, error) {
	m.mu.Lock()
	existing, ok := m.slots[t.ID]
	m.mu.Unlock()
	if ok && existing.client.Alive() {
		return existing, nil
	}

	workDir := m.WorkDir(t.ID)
	if err := os.MkdirAll(workDir, 0o755); err != nil {
		return nil, err
	}
	holder := &slot{}
	client, err := m.agent.StartClient(
		workDir,
		"tern-"+t.ID,
		fmt.Sprintf("http://127.0.0.1:%d/api/threads/%s/publish", m.port, t.ID),
		fmt.Sprintf("http://127.0.0.1:%d/api/threads/%s/images", m.port, t.ID),
		func(event pi.Event) { holder.handler(event) },
	)
	if err != nil {
		if unavailable, ok := err.(pi.UnavailableError); ok {
			return nil, unavailable
		}
		return nil, pi.UnavailableError{Reason: err.Error()}
	}
	holder.client = client
	m.mu.Lock()
	m.slots[t.ID] = holder
	m.mu.Unlock()
	return holder, nil
}

func (m *Manager) touch(id string) {
	m.mu.Lock()
	holder, ok := m.slots[id]
	if !ok {
		m.mu.Unlock()
		return
	}
	if holder.idle != nil {
		holder.idle.Stop()
	}
	holder.idle = time.AfterFunc(idleClose, func() {
		m.mu.Lock()
		if current, still := m.slots[id]; still && current == holder {
			delete(m.slots, id)
		}
		m.mu.Unlock()
		holder.client.Close()
	})
	m.mu.Unlock()
}

// CloseAll stops every Pi process (server shutdown).
func (m *Manager) CloseAll() {
	m.mu.Lock()
	slots := m.slots
	m.slots = map[string]*slot{}
	m.mu.Unlock()
	for _, holder := range slots {
		holder.client.Close()
	}
}

// ---------------------------------------------------------------- events

func (m *Manager) bindEvents(id string) func(pi.Event) {
	return func(event pi.Event) {
		kind, _ := event["type"].(string)
		now := store.Now()

		m.mu.Lock()
		t, ok := m.live[id]
		if !ok {
			m.mu.Unlock()
			return
		}
		holder := m.slots[id]

		switch kind {
		case "turn_start":
			if holder != nil {
				holder.stream.Reset()
				holder.turnStart = time.Now()
			}

		case "tool_execution_start":
			callID, _ := event["toolCallId"].(string)
			if callID == "" {
				callID = eventID()
			}
			args, _ := json.Marshal(event["args"])
			name, _ := event["toolName"].(string)
			if name == "" {
				name = "tool"
			}
			m.pushLocked(t, model.ThreadEvent{
				ID: callID, Type: model.EventTool, Tool: name,
				Args: string(args), Status: "running", At: now,
			})

		case "tool_execution_end":
			callID, _ := event["toolCallId"].(string)
			for i := range t.Events {
				if t.Events[i].Type != model.EventTool || t.Events[i].ID != callID {
					continue
				}
				if failed, _ := event["isError"].(bool); failed {
					t.Events[i].Status = "error"
				} else {
					t.Events[i].Status = "ok"
				}
				// The publish result is worth keeping in the log: "已入库：ink（41 段…）".
				if t.Events[i].Tool == "publish_article" {
					if result, ok := event["result"].(map[string]any); ok {
						if content, ok := result["content"].([]any); ok {
							var parts []string
							for _, item := range content {
								entry, ok := item.(map[string]any)
								if !ok {
									continue
								}
								if text, _ := entry["text"].(string); text != "" {
									parts = append(parts, text)
								}
							}
							text := strings.TrimSpace(strings.Join(parts, " "))
							if text != "" {
								t.Events[i].Detail = clip(strings.Split(text, "\n")[0], 160)
							}
						}
					}
				}
				t.UpdatedAt = now
				m.saveLocked(t)
				break
			}

		case "message_update":
			if holder != nil {
				if delta, ok := event["assistantMessageEvent"].(map[string]any); ok {
					if kind, _ := delta["type"].(string); kind == "text_delta" {
						if text, _ := delta["delta"].(string); text != "" {
							holder.stream.WriteString(text)
						}
					}
				}
			}

		case "turn_end":
			final := ""
			if message, ok := event["message"].(map[string]any); ok {
				final = assistantText(message)
			}
			if final == "" && holder != nil {
				final = strings.TrimSpace(holder.stream.String())
			}
			if final != "" {
				m.pushLocked(t, model.ThreadEvent{
					ID: eventID(), Type: model.EventAssistant, Text: final, At: now,
				})
			}
			if holder != nil {
				holder.stream.Reset()
			}
		}
		m.mu.Unlock()
	}
}

func assistantText(message map[string]any) string {
	parts, ok := message["content"].([]any)
	if !ok {
		return ""
	}
	var texts []string
	for _, part := range parts {
		item, ok := part.(map[string]any)
		if !ok {
			continue
		}
		if kind, _ := item["type"].(string); kind == "text" {
			if text, _ := item["text"].(string); text != "" {
				texts = append(texts, text)
			}
		}
	}
	return strings.TrimSpace(strings.Join(texts, ""))
}

func clip(s string, n int) string {
	runes := []rune(s)
	if len(runes) <= n {
		return s
	}
	return string(runes[:n])
}

// ---------------------------------------------------------------- turns

func (m *Manager) runTurn(t *model.Thread, prompt string) error {
	if err := os.MkdirAll(m.WorkDir(t.ID), 0o755); err != nil {
		return err
	}
	holder, err := m.slotFor(t)
	if err != nil {
		return err
	}
	holder.handler = m.bindEvents(t.ID)

	m.mu.Lock()
	before := t.PublishSeq
	timeout := time.Duration(m.settings.Get().Agent.TimeoutMS) * time.Millisecond
	if timeout <= 0 {
		timeout = 15 * time.Minute
	}
	m.mu.Unlock()

	err = pi.RunTurn(holder.client, prompt, timeout, func() bool {
		m.mu.Lock()
		defer m.mu.Unlock()
		return t.PublishSeq > before
	})
	m.touch(t.ID)
	return err
}

// begin marks a conversation busy before its round starts, so the interface
// sees it running as soon as the request returns.
func (m *Manager) begin(t *model.Thread) {
	m.mu.Lock()
	m.busy[t.ID] = true
	t.State = model.ThreadRunning
	t.Error = ""
	m.saveLocked(t)
	m.mu.Unlock()
}

// runGuarded runs one round and records how it ended.
func (m *Manager) runGuarded(t *model.Thread, run func() error) {
	err := run()

	m.mu.Lock()
	if err != nil {
		t.State = model.ThreadError
		t.Error = clip(err.Error(), 600)
		m.pushLocked(t, model.ThreadEvent{
			ID: eventID(), Type: model.EventStatus, Text: "失败：" + t.Error, At: store.Now(),
		})
	} else {
		t.State = model.ThreadIdle
	}
	t.UpdatedAt = store.Now()
	m.saveLocked(t)
	delete(m.busy, t.ID)
	m.mu.Unlock()
}

// ValidURL checks a link before a conversation spends a model round on it.
func ValidURL(raw string) (string, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return "", fmt.Errorf("paste a full web address (with https://)")
	}
	parsed, err := url.Parse(raw)
	if err != nil || parsed.Host == "" || !strings.Contains(parsed.Host, ".") {
		return "", fmt.Errorf("that does not look like a web address")
	}
	if parsed.Scheme != "http" && parsed.Scheme != "https" {
		return "", fmt.Errorf("only http and https links are supported")
	}
	return parsed.String(), nil
}

// Create starts a conversation: the first round is the ingestion.
func (m *Manager) Create(rawURL string) (*model.Thread, error) {
	url, err := ValidURL(rawURL)
	if err != nil {
		return nil, err
	}
	if status := m.agent.Status(); !status.Ready {
		return nil, pi.UnavailableError{Reason: "the agent is not ready: " + status.Detail}
	}

	id := store.NewID()
	t := &model.Thread{
		ID:        id,
		URL:       url,
		State:     model.ThreadIdle,
		Events:    []model.ThreadEvent{},
		CreatedAt: store.Now(),
		UpdatedAt: store.Now(),
	}
	m.mu.Lock()
	m.live[id] = t
	m.saveLocked(t)
	m.mu.Unlock()

	if err := os.MkdirAll(m.WorkDir(id), 0o755); err != nil {
		return nil, err
	}

	m.mu.Lock()
	m.pushLocked(t, model.ThreadEvent{ID: eventID(), Type: model.EventUser, Text: url, At: store.Now()})
	snapshot := cloneThread(t)
	m.mu.Unlock()

	m.begin(t)

	go m.runGuarded(t, func() error {
		if err := m.runTurn(t, pi.IngestPrompt(url, m.WorkDir(id))); err != nil {
			return err
		}
		// Nothing published yet: give the agent a push, at most twice.
		for nudge := 0; nudge < 2 && m.articleOf(id) == ""; nudge++ {
			_, statErr := os.Stat(filepath.Join(m.WorkDir(id), "article.json"))
			wrote := statErr == nil
			m.mu.Lock()
			text := "article.json is written but not published yet; pushing the agent"
			if !wrote {
				text = "no article.json yet; pushing the agent"
			}
			m.pushLocked(t, model.ThreadEvent{ID: eventID(), Type: model.EventStatus, Text: text, At: store.Now()})
			m.mu.Unlock()
			prompt := "article.json is ready but not published. Call publish_article now (no arguments); if it returns field errors, fix the file and call it again immediately."
			if !wrote {
				prompt = "There is no article.json yet. Turn the text you extracted into the article JSON (write it to " +
					filepath.Join(m.WorkDir(id), "article.json") + "), then call publish_article."
			}
			if err := m.runTurn(t, prompt); err != nil {
				return err
			}
		}
		if m.articleOf(id) == "" {
			return fmt.Errorf("the agent finished without producing an article Tern could take")
		}
		return nil
	})
	return snapshot, nil
}

func (m *Manager) articleOf(id string) string {
	m.mu.Lock()
	defer m.mu.Unlock()
	if t, ok := m.live[id]; ok {
		return t.Article
	}
	return ""
}

// PostMessage continues a conversation.
func (m *Manager) PostMessage(id, text string) (*model.Thread, error) {
	t, err := m.Get(id)
	if err != nil {
		return nil, err
	}
	text = strings.TrimSpace(text)
	if text == "" {
		return nil, fmt.Errorf("the message is empty")
	}
	if m.Busy(id) || t.State == model.ThreadRunning {
		return nil, fmt.Errorf("this conversation is still running; wait for it to finish")
	}

	m.mu.Lock()
	live := m.live[id]
	m.pushLocked(live, model.ThreadEvent{ID: eventID(), Type: model.EventUser, Text: text, At: store.Now()})
	snapshot := cloneThread(live)
	m.mu.Unlock()

	m.begin(live)
	go m.runGuarded(live, func() error { return m.runTurn(live, text) })
	return snapshot, nil
}

// Publish takes what the agent produced: validate, localize pictures, store the
// article, warm the dictionary.
func (m *Manager) Publish(id string, draft *model.Draft) (*publish.Result, model.Issues, error) {
	// Load the thread from disk when it is not live: a publish can follow a
	// restart, and the record is the truth.
	thread, err := m.Get(id)
	if err != nil {
		return nil, nil, err
	}

	// Importing downloads pictures, so it runs outside the lock.
	result, issues := m.importer.Import(draft, thread.URL, thread.Article)
	if len(issues) > 0 {
		return nil, issues, nil
	}

	m.mu.Lock()
	live, ok := m.live[id]
	if !ok {
		live = thread
		m.live[id] = live
	}
	live.Article = result.Article.ID
	live.Title = result.Article.Title.ZH
	if live.Title == "" {
		live.Title = result.Article.Title.EN
	}
	live.PublishSeq++
	live.UpdatedAt = store.Now()
	m.saveLocked(live)
	m.mu.Unlock()
	return result, nil, nil
}

// PublishRaw is Publish for the bytes the extension sends.
func (m *Manager) PublishRaw(id string, raw []byte) (*publish.Result, model.Issues, error) {
	var draft model.Draft
	if err := json.Unmarshal(raw, &draft); err != nil {
		return nil, model.Issues{{Path: "(body)", Message: "not valid JSON: " + err.Error()}}, nil
	}
	return m.Publish(id, &draft)
}

// UploadImage stores a picture for the agent, returning its /media address.
type Uploaded struct {
	Src   string `json:"src"`
	Bytes int    `json:"bytes"`
	Ext   string `json:"ext"`
}

// UploadImage takes base64 bytes or a URL from the agent's upload_image tool.
func (m *Manager) UploadImage(dataBase64, url, label string) (*Uploaded, error) {
	assets := m.settings.AssetsDir()
	var data []byte
	if dataBase64 != "" {
		decoded, err := base64.StdEncoding.DecodeString(dataBase64)
		if err != nil {
			return nil, publish.ImageError{Reason: "the picture is not valid base64"}
		}
		data = decoded
	} else if url != "" {
		fetched, err := publish.FetchImage(url, "")
		if err != nil {
			return nil, err
		}
		data = fetched
	} else {
		return nil, fmt.Errorf("give either dataBase64 or url")
	}
	src, ext, err := publish.StoreImage(assets, data, label)
	if err != nil {
		return nil, err
	}
	return &Uploaded{Src: src, Bytes: len(data), Ext: ext}, nil
}
