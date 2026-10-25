package publish

import (
	"os"
	"testing"

	"github.com/solidSpoon/tern-go/internal/config"
	"github.com/solidSpoon/tern-go/internal/model"
	"github.com/solidSpoon/tern-go/internal/store"
)

func TestImportReplacesAndWarms(t *testing.T) {
	root := t.TempDir()
	settings, err := config.Load(root)
	if err != nil {
		t.Fatal(err)
	}
	library, err := store.New(settings.Paths())
	if err != nil {
		t.Fatal(err)
	}
	importer := &Importer{Store: library, Assets: settings.AssetsDir}

	draft := &model.Draft{
		Title:   model.Bilingual{ZH: "墨水瓶", EN: "Inkwell"},
		Summary: model.Bilingual{ZH: "摘要", EN: "A summary"},
		Source:  model.Source{URL: "https://example.com/ink"},
		Tags:    []string{"器物"},
		Blocks: []model.Block{
			{Type: model.BlockParagraph, ZH: "第一段", EN: "The first paragraph."},
			{Type: model.BlockParagraph, ZH: "第二段", EN: "The second paragraph."},
			{Type: model.BlockImage, Src: "/media/cover.png"},
		},
		Vocab:    []model.Vocab{{Term: "inkwell", ZH: "墨水瓶"}},
		Glossary: []model.Vocab{{Term: "porcelain", ZH: "瓷器"}},
	}

	result, issues := importer.Import(draft, "https://example.com/ink", "")
	if len(issues) > 0 {
		t.Fatalf("import failed: %v", issues)
	}
	article := result.Article
	if article.ID != "inkwell" {
		t.Errorf("id = %q, want inkwell", article.ID)
	}
	if article.Version != model.TemplateVersion {
		t.Errorf("version = %d", article.Version)
	}
	if article.Blocks[0].ID != "b1" || article.Blocks[2].ID != "b3" {
		t.Errorf("block ids were not filled: %+v", article.Blocks)
	}
	if result.Warmed != 2 {
		t.Errorf("warmed = %d, want 2", result.Warmed)
	}
	if cached := library.CachedEntry("porcelain"); cached == nil || cached.ZH != "瓷器" {
		t.Errorf("the dictionary was not warmed: %+v", cached)
	}
	if _, err := os.Stat(settings.Paths().Library + "/inkwell.json"); err != nil {
		t.Fatalf("the article file is missing: %v", err)
	}

	// A second import updates the same article instead of making inkwell-2.
	draft.Title.ZH = "墨水瓶（修订）"
	again, issues := importer.Import(draft, "https://example.com/ink", article.ID)
	if len(issues) > 0 {
		t.Fatalf("second import failed: %v", issues)
	}
	if again.Article.ID != article.ID {
		t.Errorf("the second import made %q, want %q", again.Article.ID, article.ID)
	}
	if again.Article.CreatedAt != article.CreatedAt {
		t.Error("createdAt changed on an update")
	}
	if again.Article.Title.ZH != "墨水瓶（修订）" {
		t.Errorf("title = %q", again.Article.Title.ZH)
	}
}

func TestImportReportsFields(t *testing.T) {
	root := t.TempDir()
	settings, err := config.Load(root)
	if err != nil {
		t.Fatal(err)
	}
	library, err := store.New(settings.Paths())
	if err != nil {
		t.Fatal(err)
	}
	importer := &Importer{Store: library, Assets: settings.AssetsDir}

	_, issues := importer.Import(&model.Draft{
		Title:  model.Bilingual{ZH: "x", EN: "x"},
		Blocks: []model.Block{{Type: "p"}},
	}, "", "")
	if len(issues) < 2 {
		t.Fatalf("want field issues, got %v", issues)
	}
	want := map[string]bool{"blocks.0.zh": false, "blocks.0.en": false}
	for _, issue := range issues {
		if _, ok := want[issue.Path]; ok {
			want[issue.Path] = true
		}
	}
	for path, found := range want {
		if !found {
			t.Errorf("no issue for %s: %v", path, issues.Strings())
		}
	}
}
