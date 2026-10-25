package app

import (
	"fmt"

	"github.com/egoist/mygo/ui"

	"github.com/solidSpoon/tern-go/internal/model"
)

func (app *App) vocabPage(c *ui.Context) {
	ui.Column(c).Fill().Children(func() {
		head := ui.Column(c).Padding(24, 28, 16).Gap(6).DragWindow()
		head.Children(func() {
			pageHeader(c, "生词本", fmt.Sprintf("%d 个词", len(app.vocab)),
				"阅读时点英文单词，再点「加入生词本」，就会出现在这里；手机和桌面共享同一份。")
			ui.Row(c).Gap(8).Children(func() {
				ui.Button(c.Key("refresh-vocab"), "刷新").OnClick(func() { app.loadVocab() })
			})
		})
		ui.Divider(c)

		switch {
		case app.vocabErr != "":
			emptyState(c, "读不到生词本", app.vocabErr, nil)
		case len(app.vocab) == 0:
			emptyState(c, "还是空的", "在文章里点英文单词，浮层里就能加入生词本。", nil)
		default:
			app.vocabList.Key = func(row int) any { return app.vocab[row].Term }
			app.vocabList.Label = func(row int) string { return app.vocab[row].Term }
			ui.List(c, &app.vocabList, len(app.vocab), func(row int) {
				app.vocabRow(c, app.vocab[row])
			}).Grow(1)
		}
	})
}

func (app *App) vocabRow(c *ui.Context, entry model.Vocab) {
	t := c.Theme()
	ui.Column(c).Key(entry.Term).Padding(14, 28).Gap(4).Children(func() {
		ui.Row(c).AlignItems(ui.Baseline).Gap(10).Children(func() {
			ui.Text(c, entry.Term).Font("Iowan Old Style, Songti SC, Palatino").FontSize(18).Bold()
			if entry.Phonetic != "" {
				ui.Text(c, "/"+entry.Phonetic+"/").FontSize(11.5).TextColor(t.TextMuted)
			}
			if entry.Pos != "" {
				ui.Text(c, entry.Pos).FontSize(10.5).TextColor(t.Accent)
			}
			ui.Spacer(c)
			if ui.Button(c.Key("remove-"+entry.Term), "移除").Clicked() {
				app.removeVocab(entry.Term)
			}
		})
		ui.Text(c, entry.ZH).FontSize(13.5)
		if entry.EN != "" {
			ui.Text(c, entry.EN).FontSize(12).Italic().TextColor(t.TextMuted)
		}
		if entry.Example != nil {
			ui.Column(c).Margin(4, 0, 0, 0).Padding(8, 10).Radius(6).MaxWidth(620).
				Background(t.Surface).Gap(2).Children(func() {
				ui.Text(c, entry.Example.EN).FontSize(12).TextColor(t.Text.Mix(t.TextMuted, 0.45)).LineHeight(1.5)
				ui.Text(c, entry.Example.ZH).FontSize(11.5).TextColor(t.TextMuted)
			})
		}
	})
	ui.Divider(c).MarginX(28)
}

func (app *App) removeVocab(term string) {
	go func() {
		list, err := app.store.RemoveVocab(term)
		app.refresh(func() {
			if err != nil {
				app.vocabErr = err.Error()
				return
			}
			app.vocab = list
		})
	}()
}
