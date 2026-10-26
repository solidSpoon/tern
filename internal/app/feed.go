package app

import (
	"fmt"
	"strings"

	"github.com/egoist/mygo/ui"

	"github.com/solidSpoon/tern-go/internal/model"
)

// feedPage is the article feed: a card per read, each opening the reading page
// in a web window.
func (app *App) feedPage(c *ui.Context) {
	t := c.Theme()
	ui.Column(c).Fill().Children(func() {
		head := ui.Column(c).Padding(24, 28, 16).Gap(12).DragWindow()
		head.Children(func() {
			ui.Row(c).AlignItems(ui.End).Gap(12).Children(func() {
				ui.Column(c).Grow(1).Gap(5).Children(func() {
					ui.Text(c, "文章").Font("Iowan Old Style, Songti SC, Palatino").FontSize(25).Bold()
				})
				ui.Button(c.Key("refresh"), "刷新").OnClick(func() {
					app.loadFeed()
					app.loadThreads()
				})
				ui.PrimaryButton(c.Key("add"), "添加链接").OnClick(func() {
					app.router.Push("/threads")
				})
			})
			ui.Text(c, "点开一篇进入中英对照；手机打开同一个地址就能读。").
				FontSize(12.5).TextColor(t.TextMuted)
			// A refresh that fails keeps the list in place and says so here.
			statusLine(c, 20, app.articlesErr, t.Danger, false)
		})

		ui.Divider(c)

		switch {
		case len(app.articles) == 0 && app.articlesErr != "":
			emptyState(c, "读不到列表", app.articlesErr, func() {
				if ui.Button(c, "重试").Clicked() {
					app.loadFeed()
				}
			})
		case len(app.articles) == 0:
			emptyState(c, "还没有文章", "在「对话」里贴一个网页链接，Agent 会抓正文、逐段翻译并入库。", func() {
				if ui.PrimaryButton(c, "添加第一篇").Clicked() {
					app.router.Push("/threads")
				}
			})
		default:
			app.feedList.Key = func(row int) any { return app.articles[row].ID }
			app.feedList.Label = func(row int) string { return app.articles[row].Title.ZH }
			ui.List(c, &app.feedList, len(app.articles), func(row int) {
				app.feedRow(c, app.articles[row])
			}).Grow(1)
		}
	})
}

func (app *App) feedRow(c *ui.Context, article model.ArticleSummary) {
	t := c.Theme()
	line := ui.Row(c.Key(article.ID)).Padding(16, 28).Gap(16).AlignItems(ui.Start)
	if line.Hovered() {
		line.Background(t.Surface)
	}
	line.OnClick(func() { app.router.Push("/a/" + article.ID) })
	line.Children(func() {
		cover := ""
		if article.Cover != nil {
			cover = article.Cover.Src
		}
		if bitmap := app.covers[cover]; bitmap != nil {
			ui.Image(c, bitmap).Size(88, 88).Fit(ui.Cover).Radius(6)
		} else if article.Cover != nil {
			ui.Box(c).Size(88, 88).Radius(6).Background(t.Surface).Border(1, t.Border)
		}

		ui.Column(c).Grow(1).Gap(3).Children(func() {
			if len(article.Tags) > 0 {
				ui.Text(c, strings.ToUpper(article.Tags[0])).FontSize(10).TextColor(t.Accent)
			}
			ui.Text(c, article.Title.ZH).Font("Iowan Old Style, Songti SC, Palatino").FontSize(18).Bold().MaxLines(2).LineHeight(1.35)
			if article.Title.EN != "" {
				ui.Text(c, article.Title.EN).FontSize(12.5).Italic().TextColor(t.TextMuted).MaxLines(1)
			}
			summary := article.Summary.ZH
			if summary == "" {
				summary = article.Summary.EN
			}
			if summary != "" {
				ui.Text(c, summary).FontSize(12.5).TextColor(t.TextMuted).MaxLines(2).LineHeight(1.55)
			}
			meta := []string{}
			if article.Source.Site != "" {
				meta = append(meta, article.Source.Site)
			}
			if date := formatDate(article.Source.PublishedAt); date != "" {
				meta = append(meta, date)
			} else if date := formatDate(article.CreatedAt); date != "" {
				meta = append(meta, date)
			}
			meta = append(meta, fmt.Sprintf("%d 段", article.ParagraphCount))
			meta = append(meta, fmt.Sprintf("约 %d 分钟", max(1, article.WordCount/220)))
			if len(article.Vocab) > 0 {
				meta = append(meta, fmt.Sprintf("%d 生词", len(article.Vocab)))
			}
			ui.Text(c, strings.Join(meta, " · ")).FontSize(11).TextColor(t.TextMuted)
		})
	})
	ui.Divider(c).MarginX(28)
}

func max(a, b int) int {
	if a > b {
		return a
	}
	return b
}
