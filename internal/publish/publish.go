// Package publish turns what the agent wrote into an article: it checks the
// structure, localizes the pictures, writes the article and warms the
// dictionary.
package publish

import (
	"encoding/json"
	"fmt"
	"os"
	"strings"

	"github.com/solidSpoon/tern-go/internal/model"
	"github.com/solidSpoon/tern-go/internal/store"
)

// Importer imports articles into the store.
type Importer struct {
	Store  *store.Store
	Assets func() string
}

// Result says what an import produced.
type Result struct {
	Article *model.Article `json:"article"`
}

// Import validates raw data (usually the agent's article.json), stores the
// pictures locally, writes the article and warms the dictionary with its
// words. When existing is not empty, that article is replaced instead of a new
// one being made.
func (im *Importer) Import(draft *model.Draft, sourceURL, existing string) (*Result, model.Issues) {
	model.NormalizeDraft(draft)
	if issues := draft.ValidateDraft(); len(issues) > 0 {
		return nil, issues
	}

	localized := im.localize(draft)
	now := store.Now()

	var replacing *model.Article
	if existing != "" {
		if found, err := im.Store.Article(existing); err == nil {
			replacing = found
		}
	}

	id := ""
	createdAt := now
	if replacing != nil {
		id = replacing.ID
		createdAt = replacing.CreatedAt
	} else {
		base := localized.Title.EN
		if strings.TrimSpace(base) == "" {
			base = localized.Title.ZH
		}
		id = im.Store.FreeID(base)
	}

	article := &model.Article{
		Version:   model.TemplateVersion,
		ID:        id,
		Title:     localized.Title,
		Summary:   localized.Summary,
		Cover:     localized.Cover,
		Source:    localized.Source,
		Tags:      localized.Tags,
		Blocks:    localized.Blocks,
		Vocab:     localized.Vocab,
		Glossary:  localized.Glossary,
		CreatedAt: createdAt,
		UpdatedAt: now,
	}
	if sourceURL != "" {
		article.Source.URL = sourceURL
	}
	if article.Tags == nil {
		article.Tags = []string{}
	}
	if article.Vocab == nil {
		article.Vocab = []model.Vocab{}
	}
	if article.Glossary == nil {
		article.Glossary = []model.Vocab{}
	}

	if issues := article.Validate(); len(issues) > 0 {
		return nil, issues
	}
	if err := im.Store.SaveArticle(article); err != nil {
		if issues, ok := err.(model.Issues); ok {
			return nil, issues
		}
		return nil, model.Issues{{Path: "(store)", Message: err.Error()}}
	}

	return &Result{Article: article}, nil
}

// localize rewrites every picture's src into the local media folder. A picture
// that cannot be downloaded keeps its original URL: a slow remote image beats a
// broken one.
func (im *Importer) localize(draft *model.Draft) *model.Draft {
	if im.Assets == nil {
		return draft
	}
	assets := im.Assets()
	slug := draft.Title.EN
	if strings.TrimSpace(slug) == "" {
		slug = draft.Title.ZH
	}
	cache := map[string]string{}
	resolve := func(src string) string {
		if local, ok := cache[src]; ok {
			return local
		}
		local := src
		if stored, ok := downloadForArticle(assets, src, slug); ok {
			local = stored
		}
		cache[src] = local
		return local
	}

	out := *draft
	if draft.Cover != nil {
		cover := *draft.Cover
		cover.Src = resolve(cover.Src)
		out.Cover = &cover
	}
	out.Blocks = make([]model.Block, len(draft.Blocks))
	for i, block := range draft.Blocks {
		if block.Type == model.BlockImage && block.Src != "" {
			block.Src = resolve(block.Src)
		}
		out.Blocks[i] = block
	}
	return &out
}

// ReadArticleFile reads the agent's article.json, tolerating a Markdown fence
// and text around the JSON object.
func ReadArticleFile(file string) (*model.Draft, error) {
	raw, err := os.ReadFile(file)
	if err != nil {
		return nil, fmt.Errorf("cannot read %s: %w", file, err)
	}
	text := string(raw)
	start := strings.Index(text, "{")
	end := strings.LastIndex(text, "}")
	if start < 0 || end <= start {
		return nil, fmt.Errorf("%s holds no JSON object", file)
	}
	var draft model.Draft
	if err := json.Unmarshal([]byte(text[start:end+1]), &draft); err != nil {
		return nil, fmt.Errorf("%s is not valid JSON: %w", file, err)
	}
	return &draft, nil
}
