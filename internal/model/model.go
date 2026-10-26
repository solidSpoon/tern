// Package model is Tern's content contract: what an article is, and what a
// thread's conversation looks like. The agent may only fill these structures;
// it never produces HTML, components or styles.
package model

import (
	"fmt"
	"strings"
)

// TemplateVersion is the version of the article structure. Bump it when the
// structure changes, and migrate old articles in the store.
const TemplateVersion = 1

// Bilingual is a Chinese/English pair. The English side may be empty when the
// source has no English, in which case readers fall back to Chinese only.
type Bilingual struct {
	ZH string `json:"zh"`
	EN string `json:"en"`
}

// Example is a sentence showing a word in use.
type Example struct {
	EN string `json:"en"`
	ZH string `json:"zh"`
}

// Vocab is a dictionary entry or a word of the article's vocabulary. SavedAt is
// only left over from the removed vocabulary book; old articles may still carry
// it.
type Vocab struct {
	Term     string   `json:"term"`
	Phonetic string   `json:"phonetic,omitempty"`
	Pos      string   `json:"pos,omitempty"`
	ZH       string   `json:"zh"`
	EN       string   `json:"en,omitempty"`
	Example  *Example `json:"example,omitempty"`
	SavedAt  string   `json:"savedAt,omitempty"`
}

// Cover is the image of an article's card.
type Cover struct {
	Src string `json:"src"`
	Alt string `json:"alt,omitempty"`
	// Ratio is the width/height ratio, so the feed can reserve the space before
	// the picture loads.
	Ratio float64 `json:"ratio,omitempty"`
}

// Source says where the article came from.
type Source struct {
	URL         string `json:"url"`
	Site        string `json:"site,omitempty"`
	Author      string `json:"author,omitempty"`
	PublishedAt string `json:"publishedAt,omitempty"`
}

// Block kinds.
const (
	BlockParagraph = "p"
	BlockHeading   = "h2"
	BlockImage     = "image"
	BlockQuote     = "quote"
	BlockList      = "list"
)

// ListItem is one item of a list block.
type ListItem struct {
	ZH string `json:"zh"`
	EN string `json:"en,omitempty"`
}

// Block is one slot of the article. Paragraphs are the only required kind.
type Block struct {
	Type string `json:"type"`
	ID   string `json:"id"`

	ZH   string `json:"zh,omitempty"`
	EN   string `json:"en,omitempty"`
	Note string `json:"note,omitempty"`

	Src     string     `json:"src,omitempty"`
	Alt     string     `json:"alt,omitempty"`
	Caption *Bilingual `json:"caption,omitempty"`

	Cite  string     `json:"cite,omitempty"`
	Items []ListItem `json:"items,omitempty"`
}

// Article is one bilingual read.
type Article struct {
	Version int       `json:"version"`
	ID      string    `json:"id"`
	Title   Bilingual `json:"title"`
	Summary Bilingual `json:"summary"`
	Cover   *Cover    `json:"cover"`
	Source  Source    `json:"source"`
	Tags    []string  `json:"tags"`
	Blocks  []Block   `json:"blocks"`
	// Vocab is the handful of keywords shown on the card.
	Vocab []Vocab `json:"vocab"`
	// Glossary is the whole article's word list. New articles no longer write
	// one — the offline dictionary does that job — but old articles still read
	// it.
	Glossary  []Vocab `json:"glossary"`
	CreatedAt string  `json:"createdAt"`
	UpdatedAt string  `json:"updatedAt"`
}

// ArticleSummary is an article without its body, for the feed and the cards.
type ArticleSummary struct {
	Version   int       `json:"version"`
	ID        string    `json:"id"`
	Title     Bilingual `json:"title"`
	Summary   Bilingual `json:"summary"`
	Cover     *Cover    `json:"cover"`
	Source    Source    `json:"source"`
	Tags      []string  `json:"tags"`
	Vocab     []Vocab   `json:"vocab"`
	CreatedAt string    `json:"createdAt"`
	UpdatedAt string    `json:"updatedAt"`

	ParagraphCount int `json:"paragraphCount"`
	WordCount      int `json:"wordCount"`
}

// Summary projects an article for the feed.
func (a *Article) SummaryView() ArticleSummary {
	paragraphs := 0
	words := 0
	for _, b := range a.Blocks {
		if b.Type != BlockParagraph {
			continue
		}
		paragraphs++
		words += len(strings.Fields(b.EN))
	}
	return ArticleSummary{
		Version:        a.Version,
		ID:             a.ID,
		Title:          a.Title,
		Summary:        a.Summary,
		Cover:          a.Cover,
		Source:         a.Source,
		Tags:           a.Tags,
		Vocab:          a.Vocab,
		CreatedAt:      a.CreatedAt,
		UpdatedAt:      a.UpdatedAt,
		ParagraphCount: paragraphs,
		WordCount:      words,
	}
}

// ---------------------------------------------------------------- validation

// Issue is one field that failed validation. The agent reads these and fixes
// its JSON, so they name the path inside the document.
type Issue struct {
	Path    string `json:"path"`
	Message string `json:"message"`
}

func (i Issue) String() string { return i.Path + ": " + i.Message }

// Issues is a list of problems, which is also an error.
type Issues []Issue

func (is Issues) Error() string {
	parts := make([]string, 0, len(is))
	for i, issue := range is {
		if i == 3 {
			parts = append(parts, fmt.Sprintf("and %d more", len(is)-3))
			break
		}
		parts = append(parts, issue.String())
	}
	return "article structure: " + strings.Join(parts, "; ")
}

func (is Issues) Strings() []string {
	out := make([]string, len(is))
	for i, issue := range is {
		out[i] = issue.String()
	}
	return out
}

// Draft is what the agent writes: an article without the fields the server
// fills in (version, id, timestamps). Cover, tags, vocab and glossary are
// optional; blocks are required.
type Draft struct {
	Title    Bilingual `json:"title"`
	Summary  Bilingual `json:"summary"`
	Cover    *Cover    `json:"cover"`
	Source   Source    `json:"source"`
	Tags     []string  `json:"tags"`
	Blocks   []Block   `json:"blocks"`
	Vocab    []Vocab   `json:"vocab"`
	Glossary []Vocab   `json:"glossary"`
}

// ValidateDraft checks what the agent produced and returns every problem it
// found, so the model can fix them all at once.
func (d *Draft) ValidateDraft() Issues {
	var issues Issues
	add := func(path, message string) {
		issues = append(issues, Issue{Path: path, Message: message})
	}

	if len(d.Blocks) == 0 {
		add("blocks", "at least one block is required")
	}
	for i, b := range d.Blocks {
		path := fmt.Sprintf("blocks.%d", i)
		if b.ID == "" {
			add(path+".id", "block id is required")
		}
		switch b.Type {
		case BlockParagraph:
			if b.ZH == "" {
				add(path+".zh", "a paragraph needs Chinese text")
			}
			if b.EN == "" {
				add(path+".en", "a paragraph needs the English original (use \"zh\" text when the source is not English)")
			}
		case BlockHeading:
			if b.ZH == "" {
				add(path+".zh", "a heading needs Chinese text")
			}
		case BlockImage:
			if b.Src == "" {
				add(path+".src", "an image needs src")
			}
			if b.Caption != nil && b.Caption.ZH == "" {
				add(path+".caption.zh", "an image caption needs Chinese text")
			}
		case BlockQuote:
			if b.ZH == "" {
				add(path+".zh", "a quote needs Chinese text")
			}
		case BlockList:
			if len(b.Items) == 0 {
				add(path+".items", "a list needs at least one item")
			}
			for j, item := range b.Items {
				if item.ZH == "" {
					add(fmt.Sprintf("%s.items.%d.zh", path, j), "a list item needs Chinese text")
				}
			}
		case "":
			add(path+".type", "block type is required: p, h2, image, quote or list")
		default:
			add(path+".type", fmt.Sprintf("unknown block type %q: use p, h2, image, quote or list", b.Type))
		}
	}
	if d.Cover != nil {
		if d.Cover.Src == "" {
			add("cover.src", "a cover needs src")
		}
		if d.Cover.Ratio < 0 {
			add("cover.ratio", "ratio must be positive")
		}
	}
	issues = append(issues, validateVocab("vocab", d.Vocab)...)
	issues = append(issues, validateVocab("glossary", d.Glossary)...)
	return issues
}

func validateVocab(path string, list []Vocab) Issues {
	var issues Issues
	for i, v := range list {
		p := fmt.Sprintf("%s.%d", path, i)
		if v.Term == "" {
			issues = append(issues, Issue{Path: p + ".term", Message: "a word is required"})
		}
		if v.ZH == "" {
			issues = append(issues, Issue{Path: p + ".zh", Message: "a Chinese definition is required"})
		}
		if v.Example != nil && (v.Example.EN == "" || v.Example.ZH == "") {
			issues = append(issues, Issue{Path: p + ".example", Message: "an example needs both en and zh"})
		}
	}
	return issues
}

// Validate checks a full stored article before it is written.
func (a *Article) Validate() Issues {
	draft := Draft{
		Title: a.Title, Summary: a.Summary, Cover: a.Cover, Source: a.Source,
		Tags: a.Tags, Blocks: a.Blocks, Vocab: a.Vocab, Glossary: a.Glossary,
	}
	issues := draft.ValidateDraft()
	if a.Version != TemplateVersion {
		issues = append(issues, Issue{Path: "version", Message: fmt.Sprintf("must be %d", TemplateVersion)})
	}
	if a.ID == "" {
		issues = append(issues, Issue{Path: "id", Message: "id is required"})
	}
	if a.CreatedAt == "" {
		issues = append(issues, Issue{Path: "createdAt", Message: "createdAt is required"})
	}
	return issues
}

// NormalizeDraft fills the block ids the model often leaves out, before the
// draft is validated.
func NormalizeDraft(d *Draft) {
	for i := range d.Blocks {
		if d.Blocks[i].ID == "" {
			d.Blocks[i].ID = fmt.Sprintf("b%d", i+1)
		}
	}
}

// ---------------------------------------------------------------- threads

// ThreadState is where a conversation stands.
type ThreadState string

const (
	ThreadRunning ThreadState = "running"
	ThreadIdle    ThreadState = "idle"
	ThreadError   ThreadState = "error"
)

// EventKind says what happened in a conversation.
type EventKind string

const (
	EventUser      EventKind = "user"
	EventAssistant EventKind = "assistant"
	EventTool      EventKind = "tool"
	EventStatus    EventKind = "status"
)

// ThreadEvent is one line of the conversation log.
type ThreadEvent struct {
	ID     string    `json:"id"`
	Type   EventKind `json:"type"`
	At     string    `json:"at"`
	Text   string    `json:"text,omitempty"`
	Tool   string    `json:"tool,omitempty"`
	Args   string    `json:"args,omitempty"`
	Status string    `json:"status,omitempty"`
	Detail string    `json:"detail,omitempty"`
}

// Thread is one link, and the conversation that keeps working on it.
type Thread struct {
	ID      string      `json:"id"`
	URL     string      `json:"url"`
	Title   string      `json:"title,omitempty"`
	State   ThreadState `json:"state"`
	Error   string      `json:"error,omitempty"`
	Article string      `json:"articleId,omitempty"`
	// SessionFile is the Pi session this conversation continues.
	SessionFile string `json:"sessionFile,omitempty"`
	// PublishSeq counts the successful imports, so a turn can stop as soon as
	// the article landed.
	PublishSeq int           `json:"publishSeq,omitempty"`
	Events     []ThreadEvent `json:"events"`
	CreatedAt  string        `json:"createdAt"`
	UpdatedAt  string        `json:"updatedAt"`
}

// ThreadSummary is a thread without its events.
type ThreadSummary struct {
	ID           string      `json:"id"`
	URL          string      `json:"url"`
	Title        string      `json:"title,omitempty"`
	State        ThreadState `json:"state"`
	Article      string      `json:"articleId,omitempty"`
	MessageCount int         `json:"messageCount"`
	CreatedAt    string      `json:"createdAt"`
	UpdatedAt    string      `json:"updatedAt"`
}

// SummaryView projects a thread for the conversation list.
func (t *Thread) SummaryView() ThreadSummary {
	return ThreadSummary{
		ID:           t.ID,
		URL:          t.URL,
		Title:        t.Title,
		State:        t.State,
		Article:      t.Article,
		MessageCount: len(t.Events),
		CreatedAt:    t.CreatedAt,
		UpdatedAt:    t.UpdatedAt,
	}
}
