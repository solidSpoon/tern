package app

import (
	"fmt"
	"net"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/egoist/mygo"
	"github.com/egoist/mygo/ui"

	"github.com/solidSpoon/tern-go/internal/config"
	"github.com/solidSpoon/tern-go/internal/pi"
)

func (app *App) settingsPage(c *ui.Context) {
	if !c.Valid() {
		return
	}
	ui.Column(c).Fill().Children(func() {
		// The header stays; only the sections scroll, so the title never slides
		// under the window's top edge.
		head := ui.Column(c).Padding(24, 28, 10).Gap(8).DragWindow()
		head.Children(func() {
			pageHeader(c, "设置", "运行时与模型", "模型、端口，以及文章放在哪里。")
			// One reserved line for every outcome: saving, testing, downloading.
			app.statusArea(c)
		})
		ui.Scroll(c).Grow(1).Children(func() {
			ui.Column(c).Padding(6, 28, 40).Gap(16).Children(func() {
				app.serverSection(c)
				app.runtimeSection(c)
				app.modelSection(c)
			})
		})
	})
}

// statusArea is the settings page's reserved status line. Installing shows a
// progress bar beside the message; nothing here ever moves the sections below.
func (app *App) statusArea(c *ui.Context) {
	t := c.Theme()
	installing := app.install.State == "downloading" || app.install.State == "verifying" ||
		app.install.State == "extracting"
	message, color := app.settingsMsg, t.TextMuted
	switch app.settingsTone {
	case "ok":
		color = t.Success
	case "warn":
		color = t.Danger
	}
	if message == "" && installing {
		message = app.install.Message
	}
	if message == "" && !installing {
		// A quiet summary keeps the reserved line from reading as a hole.
		agent := app.agent.Status()
		piVersion := "未安装"
		if agent.Runtime.Installed {
			piVersion = "v" + agent.Runtime.Version
		}
		if agent.Provider == "demo" {
			message = fmt.Sprintf("Pi %s · 演示模式 · 端口 %d", piVersion, app.port)
		} else {
			message = fmt.Sprintf("Pi %s · %s · 端口 %d", piVersion, agent.Model, app.port)
		}
	}
	row := ui.Row(c).Height(20).Shrink(0).Gap(10).AlignItems(ui.Center)
	row.Children(func() {
		if installing {
			ui.Progress(c, app.install.Progress).Width(140).Label("下载 Pi 运行时")
		}
		if message != "" {
			ui.Text(c, message).FontSize(11.5).TextColor(color).SingleLine().Grow(1)
		}
	})
}

func settingsSection(c *ui.Context, title, state string, stateColor ui.Color, body func()) {
	ui.Column(c).Gap(9).Children(func() {
		ui.Row(c).AlignItems(ui.Baseline).Children(func() {
			ui.Text(c, title).FontSize(15).Bold()
			ui.Spacer(c)
			if state != "" {
				ui.Text(c, state).FontSize(11.5).TextColor(stateColor)
			}
		})
		body()
		ui.Divider(c).Margin(14, 0, 0, 0)
	})
}

func hint(c *ui.Context, text string) {
	ui.Text(c, text).FontSize(11.5).TextColor(c.Theme().TextMuted).LineHeight(1.55).MaxWidth(620)
}

func (app *App) runtimeSection(c *ui.Context) {
	t := c.Theme()
	status := app.runtimeInfo
	stateText, stateColor := "未安装", t.Warning
	if status.Installed {
		stateText, stateColor = "v"+status.Version, t.Success
	}
	if os.Getenv("TERN_PI_BIN") != "" {
		stateText, stateColor = "TERN_PI_BIN", t.Success
	}

	settingsSection(c, "Pi 运行时", stateText, stateColor, func() {
		ui.Row(c).Gap(8).Children(func() {
			label := "下载运行时"
			if status.Installed {
				label = "下载最新版"
			}
			if app.install.State == "downloading" || app.install.State == "verifying" || app.install.State == "extracting" {
				label = "下载中…"
			}
			if ui.PrimaryButton(c.Key("install"), label).
				Disabled(app.settingsBusy).
				Clicked() {
				app.installRuntime("latest")
			}
			ui.Button(c.Key("refresh-runtime"), "刷新").OnClick(func() { app.loadSettings() })
		})
		hint(c, "官方发布的可执行文件，校验后装进数据目录；旧版本保留，随时可以切回。")

		if len(status.Versions) > 0 {
			ui.Column(c).Gap(0).Children(func() {
				for index, version := range status.Versions {
					row := ui.Row(c.Key("v"+version.Version)).Padding(7, 0).Gap(12).AlignItems(ui.Center)
					row.Children(func() {
						ui.Text(c, "v"+version.Version).FontSize(12.5).Bold()
						size := fmt.Sprintf("%d MB", version.SizeBytes/(1<<20))
						if version.InstalledAt != "" {
							size += " · " + version.InstalledAt[:10]
						}
						ui.Text(c, size).FontSize(11).TextColor(t.TextMuted)
						ui.Spacer(c)
						if version.Active {
							ui.Text(c, "当前").FontSize(10.5).TextColor(t.Accent)
						} else if ui.Button(c.Key("switch-"+version.Version), "切换").
							Disabled(app.settingsBusy).
							Clicked() {
							app.activateRuntime(version.Version)
						}
						if ui.Button(c.Key("remove-"+version.Version), "删除").
							Disabled(app.settingsBusy).
							Clicked() {
							app.removeRuntime(version.Version)
						}
					})
					// Only between versions: the section ends with its own rule.
					if index < len(status.Versions)-1 {
						ui.Divider(c)
					}
				}
			})
		}
	})
}

func (app *App) modelSection(c *ui.Context) {
	t := c.Theme()
	agent := app.agent.Status()
	stateText, stateColor := "非就绪", t.Warning
	if agent.Ready {
		stateText, stateColor = "就绪", t.Success
	}

	settingsSection(c, "DeepSeek 模型", stateText, stateColor, func() {
		keyState := "未配置"
		placeholder := "sk-…"
		if agent.HasKey {
			keyState = agent.KeyHint
			placeholder = "已保存 · 留空则不修改"
		}
		ui.Text(c, "API Key · "+keyState).FontSize(11.5).TextColor(t.TextMuted)

		ui.Row(c).Gap(8).Children(func() {
			ui.TextInput(c, &app.keyDraft).
				Password().
				Placeholder(placeholder).
				Label("DeepSeek API Key").
				Grow(1).
				Submitted() // Enter saves, like the button below
			if ui.Button(c.Key("save-key"), "保存密钥").Disabled(app.settingsBusy).Clicked() {
				app.saveSettings()
			}
			if ui.Button(c.Key("clear-key"), "清除").
				Disabled(app.settingsBusy || !agent.HasKey).
				Clicked() {
				app.clearKey()
			}
		})

		models := app.models
		if len(models) == 0 {
			models = app.agent.Models()
		}
		choice := app.modelChoice
		if choice == "" {
			choice = agent.Model
		}
		ui.Row(c).Gap(10).AlignItems(ui.Center).Children(func() {
			ui.Text(c, "模型").FontSize(12).TextColor(t.TextMuted)
			ui.Select(c, &choice, models).Label("模型").Width(240).Disabled(app.settingsBusy)
		})
		if choice != app.modelChoice {
			app.modelChoice = choice
			// Choosing a model switches to it: no second save button to find.
			// (The model it already runs needs no switch.)
			if choice != "" && choice != agent.Model {
				app.applyModel(choice)
			}
		}

		ui.Row(c).Gap(8).Children(func() {
			if ui.Button(c.Key("test"), "测试连接").
				Disabled(app.settingsBusy || !agent.HasKey || (!agent.Runtime.Installed && os.Getenv("TERN_PI_BIN") == "")).
				Clicked() {
				app.testConnection()
			}
		})

		hint(c, "模型选好即生效；密钥只保存在本机，接口不会下发。")
	})
}

// serverSection is what the reader actually needs to set beyond the model:
// the port the phone reads from, and where the library lives. Paths of
// subfolders and counts of articles are not settings.
func (app *App) serverSection(c *ui.Context) {
	t := c.Theme()
	mono := "ui-monospace, SFMono-Regular, Menlo, monospace"

	settingsSection(c, "服务", "", t.TextMuted, func() {
		ui.Row(c).Gap(10).AlignItems(ui.Center).Children(func() {
			ui.Text(c, "端口").FontSize(11.5).TextColor(t.TextMuted).Width(72)
			ui.TextInput(c, &app.portText).Label("端口").Width(96).Submitted()
			port, _ := strconv.Atoi(strings.TrimSpace(app.portText))
			if ui.Button(c.Key("save-port"), "保存并重启").
				Disabled(app.settingsBusy || port == app.port).
				Clicked() {
				app.savePort()
			}
		})
		hint(c, "手机通过这个端口阅读；改完保存会重启。")

		ui.Row(c).Gap(14).AlignItems(ui.Center).Children(func() {
			ui.Text(c, "数据目录").FontSize(11.5).TextColor(t.TextMuted).Width(72)
			path := ui.Text(c, app.dataRoot).Font(mono).FontSize(11).
				TextColor(t.TextMuted).SingleLine().Grow(1)
			path.Tooltip(app.dataRoot)
			if ui.Button(c.Key("choose-data"), "选择…").Disabled(app.settingsBusy).Clicked() {
				app.chooseDataDir()
			}
		})
		hint(c, "文章、图片、生词本和对话都在这里；选好后立即切换。")

		ui.Row(c).Gap(14).AlignItems(ui.Center).Children(func() {
			ui.Text(c, "手机访问").FontSize(11.5).TextColor(t.TextMuted).Width(72)
			link := ui.Text(c, app.baseURL).Font(mono).FontSize(11).
				TextColor(t.TextMuted).SingleLine().Grow(1)
			link.Tooltip(app.baseURL)
			if ui.Button(c.Key("copy-url"), "复制").Clicked() {
				c.WriteClipboard(app.baseURL)
				c.Toast("地址已复制")
			}
		})
	})
}

// ------------------------------------------------------------------ actions

func (app *App) saveSettings() {
	if app.settingsBusy {
		return
	}
	app.settingsBusy = true
	app.settingsMsg = "保存中…"
	app.settingsTone = ""
	key := strings.TrimSpace(app.keyDraft)
	go func() {
		var err error
		if key != "" {
			err = app.settings.PatchAgent(config.Agent{APIKey: key})
		}
		status := app.agent.Status()
		app.refresh(func() {
			app.settingsBusy = false
			if err != nil {
				app.settingsTone, app.settingsMsg = "warn", err.Error()
				return
			}
			app.settingsTone, app.settingsMsg = "ok", "密钥已保存"
			app.keyDraft = ""
			app.modelChoice = status.Model
			app.loadSettings()
		})
	}()
}

// applyModel switches the agent to another model right away.
func (app *App) applyModel(model string) {
	if app.settingsBusy {
		return
	}
	app.settingsBusy = true
	app.settingsTone, app.settingsMsg = "", "正在切换到 "+model+"…"
	go func() {
		err := app.settings.PatchAgent(config.Agent{Model: model})
		status := app.agent.Status()
		app.refresh(func() {
			app.settingsBusy = false
			if err != nil {
				app.settingsTone, app.settingsMsg = "warn", err.Error()
				// Put the select back on what actually runs.
				app.modelChoice = app.settings.Get().Agent.Model
				return
			}
			app.settingsTone, app.settingsMsg = "ok", "模型已切换："+status.Model
			app.modelChoice = status.Model
			app.loadSettings()
		})
	}()
}

// savePort stores the new port and restarts the app: the server, the agent's
// callbacks and the open reading page all belong to the old port.
func (app *App) savePort() {
	if app.settingsBusy || app.restart == nil {
		return
	}
	port, err := strconv.Atoi(strings.TrimSpace(app.portText))
	if err != nil || port < 1024 || port > 65535 {
		app.settingsTone, app.settingsMsg = "warn", "端口要是 1024–65535 之间的数字"
		return
	}
	if app.mgr.BusyAny() {
		app.settingsTone, app.settingsMsg = "warn", "有对话正在跑，等它结束再改端口"
		return
	}
	// If the port is taken, the restarted app could not start: say so now.
	if listener, err := net.Listen("tcp", fmt.Sprintf("127.0.0.1:%d", port)); err != nil {
		app.settingsTone, app.settingsMsg = "warn", fmt.Sprintf("端口 %d 已被占用，换一个", port)
		return
	} else {
		_ = listener.Close()
	}
	app.settingsBusy = true
	app.settingsTone, app.settingsMsg = "", fmt.Sprintf("端口改为 %d，正在重启…", port)
	go func() {
		if err := app.settings.SetPort(port); err != nil {
			app.refresh(func() {
				app.settingsBusy = false
				app.settingsTone, app.settingsMsg = "warn", err.Error()
			})
			return
		}
		if err := app.restart(); err != nil {
			app.refresh(func() {
				app.settingsBusy = false
				app.settingsTone, app.settingsMsg = "warn", err.Error()
			})
		}
	}()
}

// chooseDataDir asks for the folder that holds data/ and media/.
func (app *App) chooseDataDir() {
	if app.settingsBusy || app.win == nil {
		return
	}
	go func() {
		paths, err := mygo.Dialog.Open(mygo.OpenDialogOptions{
			Parent:            app.win,
			Title:             "选择数据目录",
			Message:           "文章库、图片、生词本和对话都会放在这个目录的 data/ 与 media/ 里。",
			ButtonLabel:       "使用这个目录",
			Directory:         true,
			CreateDirectories: true,
			DefaultPath:       app.settings.Paths().Root,
		})
		if err != nil || len(paths) == 0 || paths[0] == "" {
			return
		}
		app.refresh(func() { app.switchDataDir(paths[0]) })
	}()
}

// switchDataDir moves the running app to another root. Nothing is copied: the
// new folder is used as it is, and remembered for the next launch.
func (app *App) switchDataDir(dir string) {
	if app.settingsBusy {
		return
	}
	if app.mgr.BusyAny() {
		app.settingsTone, app.settingsMsg = "warn", "有对话正在跑，等它结束再切换目录"
		return
	}
	app.settingsBusy = true
	app.settingsTone, app.settingsMsg = "", "正在切换到 "+dir+"…"
	go func() {
		err := app.moveToDataDir(dir)
		app.refresh(func() {
			app.settingsBusy = false
			if err != nil {
				app.settingsTone, app.settingsMsg = "warn", err.Error()
				return
			}
			// Everything on screen belonged to the old root.
			app.readerID, app.readerTitle, app.readerSource, app.readerSourceURL = "", "", "", ""
			app.threadID, app.thread, app.chatItems, app.chatErr = "", nil, nil, ""
			app.startErr, app.articlesErr, app.threadsErr, app.vocabErr = "", "", "", ""
			app.covers = map[string]*ui.Bitmap{}
			app.dataRoot = app.settings.Paths().Root
			app.settingsTone, app.settingsMsg = "ok", "数据目录已切换"
			app.loadSettings()
			app.loadFeed()
			app.loadThreads()
			app.loadVocab()
		})
	}()
}

func (app *App) moveToDataDir(dir string) error {
	paths, err := app.settings.SwitchRoot(dir)
	if err != nil {
		return err
	}
	if err := app.store.SetPaths(paths); err != nil {
		return err
	}
	if err := app.mgr.SetPaths(paths); err != nil {
		return err
	}
	if err := app.runtime.SetRoot(paths.Runtime); err != nil {
		return err
	}
	return config.RecordRoot(paths.Root)
}

func (app *App) clearKey() {
	if app.settingsBusy {
		return
	}
	app.settingsBusy = true
	go func() {
		err := app.settings.ClearAPIKey()
		app.refresh(func() {
			app.settingsBusy = false
			if err != nil {
				app.settingsTone, app.settingsMsg = "warn", err.Error()
				return
			}
			app.settingsTone, app.settingsMsg = "ok", "密钥已清除"
			app.keyDraft = ""
			app.loadSettings()
		})
	}()
}

func (app *App) testConnection() {
	if app.settingsBusy {
		return
	}
	app.settingsBusy = true
	app.settingsTone, app.settingsMsg = "", "正在请求模型…"
	go func() {
		detail, err := app.agent.TestConnection()
		app.refresh(func() {
			app.settingsBusy = false
			if err != nil {
				app.settingsTone, app.settingsMsg = "warn", err.Error()
				return
			}
			app.settingsTone, app.settingsMsg = "ok", "连接正常 · "+detail
		})
	}()
}

func (app *App) installRuntime(version string) {
	if app.settingsBusy {
		return
	}
	app.settingsBusy = true
	app.settingsMsg = ""
	app.install = pi.Progress{State: "downloading", Progress: 0, Message: "正在查询版本"}
	done := make(chan struct{})
	go func() {
		ticker := time.NewTicker(400 * time.Millisecond)
		go func() {
			for {
				select {
				case <-done:
					ticker.Stop()
					return
				case <-ticker.C:
					progress := app.runtime.Progress()
					app.refresh(func() { app.install = progress })
				}
			}
		}()

		status, err := app.runtime.Install(version)
		close(done)
		progress := app.runtime.Progress()
		app.refresh(func() {
			app.settingsBusy = false
			app.runtimeInfo = status
			app.install = progress
			if err != nil {
				app.settingsTone, app.settingsMsg = "warn", err.Error()
				return
			}
			app.settingsTone, app.settingsMsg = "ok", "Pi 运行时已就绪"
			app.loadSettings()
		})
	}()
}

func (app *App) activateRuntime(version string) {
	if app.settingsBusy {
		return
	}
	app.settingsBusy = true
	go func() {
		status, err := app.runtime.Activate(version)
		app.refresh(func() {
			app.settingsBusy = false
			app.runtimeInfo = status
			if err != nil {
				app.settingsTone, app.settingsMsg = "warn", err.Error()
				return
			}
			app.settingsTone, app.settingsMsg = "ok", "已切换到 Pi "+version
			app.loadSettings()
		})
	}()
}

func (app *App) removeRuntime(version string) {
	if app.settingsBusy {
		return
	}
	app.settingsBusy = true
	go func() {
		status := app.runtime.Remove(version)
		app.refresh(func() {
			app.settingsBusy = false
			app.runtimeInfo = status
			app.settingsTone, app.settingsMsg = "ok", "已删除 Pi "+version
			app.loadSettings()
		})
	}()
}
