package pi

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"github.com/solidSpoon/tern-go/internal/config"
	"github.com/solidSpoon/tern-go/internal/model"
)

// Agent is Tern's model side: the Pi runtime, the configured model, and the
// few calls Tern makes itself (a word's entry, a connection test).
type Agent struct {
	Settings   *config.Settings
	Runtime    *Runtime
	SessionDir string
	Extension  string
}

// UnavailableError says the agent cannot run yet, with a reason a reader can act
// on.
type UnavailableError struct{ Reason string }

func (e UnavailableError) Error() string { return e.Reason }

// OutputError says the model answered with something Tern could not use.
type OutputError struct{ Reason string }

func (e OutputError) Error() string { return e.Reason }

// Tools are the tools a conversation may use. publish_article and upload_image
// come from Tern's extension.
var Tools = []string{"bash", "read", "write", "edit", "ls", "grep", "find", "publish_article", "upload_image"}

// AgentStatus is what the settings page shows about the agent.
type AgentStatus struct {
	Provider string `json:"provider"`
	Model    string `json:"model"`
	Ready    bool   `json:"ready"`
	Detail   string `json:"detail"`
	HasKey   bool   `json:"hasKey"`
	KeyHint  string `json:"keyHint,omitempty"`
	Runtime  Status `json:"runtime"`
}

// NewAgent makes the agent, writing the extension where Pi loads it.
func NewAgent(settings *config.Settings, runtime *Runtime) *Agent {
	paths := settings.Paths()
	extension, _ := runtime.EnsureExtension(paths.Pi, paths.Extension)
	return &Agent{
		Settings:   settings,
		Runtime:    runtime,
		SessionDir: filepath.Join(paths.Pi, "sessions"),
		Extension:  extension,
	}
}

// Status describes the agent as it is configured now.
func (a *Agent) Status() AgentStatus {
	cfg := a.Settings.Get()
	modelID := cfg.Agent.Model
	if modelID == "" {
		modelID = config.DefaultMdl
	}
	runtimeStatus := a.Runtime.Status()
	status := AgentStatus{
		Provider: config.Provider,
		Model:    modelID,
		Runtime:  runtimeStatus,
		HasKey:   cfg.Agent.APIKey != "",
		KeyHint:  config.KeyHint(cfg.Agent.APIKey),
	}
	if cfg.Agent.Provider == "demo" {
		status.Provider = "demo"
		status.Detail = "demo mode: no model is called"
		return status
	}
	if !runtimeStatus.Installed && os.Getenv("TERN_PI_BIN") == "" {
		status.Detail = "the Pi runtime is not installed: open Settings and download it"
		return status
	}
	if !status.HasKey {
		status.Detail = "no API key yet: add one in Settings"
		return status
	}
	status.Ready = true
	status.Detail = fmt.Sprintf("DeepSeek · %s · Pi %s", modelID, a.Runtime.Version())
	return status
}

// Models lists the models Settings offers.
func (a *Agent) Models() []string { return a.Settings.ModelChoices() }

func (a *Agent) ready() (apiKey, modelID string, err error) {
	cfg := a.Settings.Get()
	if cfg.Agent.Provider == "demo" {
		return "", "", UnavailableError{"Tern is in demo mode and does not call a model"}
	}
	if _, binErr := a.Runtime.Binary(); binErr != nil {
		return "", "", UnavailableError{binErr.Error()}
	}
	key := strings.TrimSpace(cfg.Agent.APIKey)
	if key == "" {
		return "", "", UnavailableError{"no API key yet: open Settings and add one"}
	}
	modelID = cfg.Agent.Model
	if modelID == "" {
		modelID = config.DefaultMdl
	}
	if err := os.MkdirAll(a.SessionDir, 0o755); err != nil {
		return "", "", err
	}
	return key, modelID, nil
}

// ExplainModelError turns what the model's HTTP layer said into something a
// reader can act on.
func ExplainModelError(raw string) string {
	lower := strings.ToLower(raw)
	switch {
	case regexp.MustCompile(`insufficient[_ ]?balance|402`).MatchString(lower):
		return "the DeepSeek account is out of credit (402); retry after topping it up. " + clip(raw, 160)
	case regexp.MustCompile(`401|invalid[_ ]?api[_ ]?key|unauthor`).MatchString(lower):
		return "the API key is invalid or expired (" + clip(raw, 120) + "); add a new one in Settings"
	case regexp.MustCompile(`429|rate[_ ]?limit`).MatchString(lower):
		return "the provider is rate limiting (" + clip(raw, 120) + "); try again shortly"
	}
	return "the model call failed: " + clip(raw, 200)
}

func clip(s string, n int) string {
	runes := []rune(s)
	if len(runes) <= n {
		return s
	}
	return string(runes[:n])
}

// ---------------------------------------------------------------- one-shots

func (a *Agent) oneShot(prompt string, timeout time.Duration) (string, error) {
	apiKey, modelID, err := a.ready()
	if err != nil {
		return "", err
	}
	client, err := a.Runtime.Start(ClientOptions{
		Cwd:        filepath.Dir(a.SessionDir),
		SessionDir: a.SessionDir,
		Provider:   config.Provider,
		Model:      modelID,
		APIKey:     apiKey,
		NoSession:  true,
		Tools:      []string{},
	})
	if err != nil {
		return "", UnavailableError{err.Error()}
	}
	defer client.Close()

	if err := client.Prompt(prompt); err != nil {
		return "", UnavailableError{err.Error()}
	}
	if err := client.WaitIdle(timeout); err != nil {
		return "", UnavailableError{err.Error()}
	}
	if failure := client.LastAssistantError(); failure != "" {
		return "", UnavailableError{ExplainModelError(failure)}
	}
	text := client.LastAssistantText()
	if text == "" {
		stderr := strings.TrimSpace(client.StderrTail())
		if stderr != "" {
			return "", OutputError{"the model returned nothing (" + clip(stderr, 300) + ")"}
		}
		return "", OutputError{"the model returned nothing"}
	}
	return text, nil
}

func extractJSON(text string) (json.RawMessage, error) {
	start := strings.Index(text, "{")
	end := strings.LastIndex(text, "}")
	if start < 0 || end <= start {
		return nil, OutputError{"the answer holds no JSON"}
	}
	raw := text[start : end+1]
	if !json.Valid([]byte(raw)) {
		return nil, OutputError{"the answer's JSON is broken"}
	}
	return json.RawMessage(raw), nil
}

func (a *Agent) lookupPrompt(term, context string) string {
	if len(context) > 600 {
		context = context[:600]
	}
	return `Give the bilingual reader one dictionary entry for an English word. Output only a JSON object, no explanation, no code fence:
{ "term": string, "phonetic": string, "pos": string, "zh": string, "en": string, "example": { "en": string, "zh": string } }

zh is a short Chinese definition (≤ 20 characters); pos uses n./v./adj./adv./phr.; en is one English gloss; example is a real sentence from the context, with its Chinese translation. When the word means something specific here, the context wins.

Word: ` + term + `
Context: ` + context
}

// LookupWord asks the model for a word's entry.
func (a *Agent) LookupWord(term, context string) (model.Vocab, error) {
	raw, err := a.oneShot(a.lookupPrompt(term, context), 180*time.Second)
	if err != nil {
		return model.Vocab{}, err
	}
	payload, err := extractJSON(raw)
	if err != nil {
		return model.Vocab{}, err
	}
	var entry model.Vocab
	if err := json.Unmarshal(payload, &entry); err != nil {
		return model.Vocab{}, OutputError{"that dictionary entry is not shaped as asked: " + err.Error()}
	}
	if entry.Term == "" || entry.ZH == "" {
		return model.Vocab{}, OutputError{"that dictionary entry has no word or no Chinese definition"}
	}
	return entry, nil
}

// TestConnection runs the smallest request that proves the model answers.
func (a *Agent) TestConnection() (string, error) {
	raw, err := a.oneShot(`Output only one JSON object, no explanation: {"ok":true}`, 120*time.Second)
	if err != nil {
		return "", err
	}
	payload, err := extractJSON(raw)
	if err != nil {
		return "", err
	}
	var check struct {
		OK bool `json:"ok"`
	}
	if err := json.Unmarshal(payload, &check); err != nil || !check.OK {
		return "", OutputError{"the model answered with something unexpected"}
	}
	return a.Status().Detail, nil
}

// ---------------------------------------------------------------- turns

// StartClient starts (or reuses) a conversation process.
func (a *Agent) StartClient(cwd, sessionID, publishURL, imageURL string, onEvent func(Event)) (*Client, error) {
	apiKey, modelID, err := a.ready()
	if err != nil {
		return nil, err
	}
	return a.Runtime.Start(ClientOptions{
		Cwd:        cwd,
		SessionDir: a.SessionDir,
		Provider:   config.Provider,
		Model:      modelID,
		APIKey:     apiKey,
		SessionID:  sessionID,
		Extension:  a.Extension,
		ExtraEnv: map[string]string{
			"TERN_PUBLISH_URL": publishURL,
			"TERN_IMAGE_URL":   imageURL,
		},
		Tools:   Tools,
		OnEvent: onEvent,
	})
}

// RunTurn sends one prompt and waits for Pi to settle. stopWhen is checked
// while it works, and aborts the turn as soon as it returns true (the article
// landed, so the model need not keep going).
func RunTurn(client *Client, prompt string, timeout time.Duration, stopWhen func() bool) error {
	if err := client.Prompt(prompt); err != nil {
		return UnavailableError{err.Error()}
	}
	stopWatch := make(chan struct{})
	if stopWhen != nil {
		go func() {
			ticker := time.NewTicker(800 * time.Millisecond)
			defer ticker.Stop()
			for {
				select {
				case <-stopWatch:
					return
				case <-ticker.C:
					if stopWhen() {
						client.Abort()
						return
					}
				}
			}
		}()
	}
	err := client.WaitIdle(timeout)
	close(stopWatch)
	if err != nil {
		return UnavailableError{err.Error()}
	}
	if failure := client.LastAssistantError(); failure != "" {
		return UnavailableError{ExplainModelError(failure)}
	}
	return nil
}

// ---------------------------------------------------------------- the prompt

const templateSpec = `Article structure (TypeScript):
{
  title:   { zh: string, en: string }
  summary: { zh: string, en: string }        // each ≤ 80 characters, like a news standfirst
  cover:   { src: string, alt?: string } | null
  source:  { url: string, site?: string, author?: string, publishedAt?: string }
  tags:    string[]                          // 2–4, in Chinese
  blocks:  Block[]
  vocab:     { term, phonetic?, pos?, zh, en?, example?: { en, zh } }[]   // 4–8 keywords
};

Block union (number ids b1, b2… in order):
  { type: "p",     id, zh: string, en: string, note?: string }
  { type: "h2",    id, zh: string, en?: string }
  { type: "image", id, src: string, alt?: string, caption?: { zh, en? } }
  { type: "quote", id, zh: string, en?: string, cite?: string }
  { type: "list",  id, items: { zh: string, en?: string }[] }`

// IngestPrompt is the first message of a conversation: one link in, one
// bilingual article out.
func IngestPrompt(url, workDir string) string {
	return `You work in a temporary directory on a personal Mac. Turn this web page into one bilingual (English/Chinese) article and publish it.

URL: ` + url + `
Working directory: ` + workDir + `

Do this in order:

1. Get the text: use ego-browser to open the page and extract the article body. How is up to you —
   run ` + "`ego-browser skill`" + ` first to see how it works (taskSpace / page.goto / page.evaluate),
   write a .mjs script and run it with ` + "`ego-browser nodejs < script.mjs`" + `, and save the result in the working directory.
   Check it with ` + "`command -v ego-browser`" + ` first. If that is empty, do not go looking for the
   program — fetch the page with curl instead. If a selector fails, try others and wait for rendering.
   Stay inside the working directory: never scan the disk, the home folder or other apps.
   If the text really cannot be had, say so — never invent content.

2. Structure it: write the body into article.json in the working directory. Rules:
   - Keep the blocks in the order of the original, with paragraphs **1:1**: never merge two paragraphs into one.
   - en is the English original (verbatim, line breaks may be fixed); zh is the Chinese translation: fluent, written Chinese, no explanations.
   - When the original is not English, en holds the original and zh the Chinese translation.
   - Drop navigation, ads, subscription prompts, comments, related links, and reference markers such as [a] [1].
   - Use h2 only for real subheadings.
   - note only when a term or a piece of cultural background would not be understood; at most 3.
   - Never invent facts that are not in the original; the summary and tags may generalize.
   - vocab lists the 4–8 most important words of the article, with pos and a Chinese definition;
     examples quote the article when possible.
   - Pictures: keep the page's original src — Tern downloads them on import. If you need to change one
     (replace, crop, convert), save it into the working directory, call upload_image, and use the returned
     /media/... in the cover or an image block.

` + templateSpec + `

3. Publish: call the publish_article tool (no arguments — it always reads article.json from the working directory).
   It checks the structure, downloads the pictures and writes the article into the library.
   "已入库：<id>（N 段）" means success. If it returns field errors, fix article.json and call it **again** — every call updates the same article.

**Language**: talk to the user in Simplified Chinese (keep file names and commands in English). Keep reports short: a few sentences on what you did, how many paragraphs, how many words — no lists of details.

**Finishing**: after writing article.json you must call publish_article. Only "已入库" counts as done; do not stop at a progress report.

The user may keep talking in this conversation (change a translation, retitle, add words). Treat the files in the working directory as the truth, edit them, then call publish_article again.`
}
