<p align="center">
  <img src="assets/brand/png/icon-256.png" width="128" height="128" alt="Tern">
</p>

<h1 align="center">Tern</h1>

<p align="center">双语阅读 · Go + MyGo</p>

<p align="center"><em>北极燕鸥一年往返两极，过两个夏天，见到的白昼比任何动物都多。<br>
双语读者，也活在两个语言的夏天里。</em></p>

把网页变成一篇中英对照的阅读页。桌面端是一个真正的 macOS App：除了**文章阅读页**和**对话详情**用
Web 写，其余界面（文章列表、对话列表、生词本、设置、阅读工具条）全部由
[MyGo](https://github.com/egoist/mygo) 的原生 UI 绘制；同一批页面也通过本机 HTTP 服务暴露给
iPhone 浏览器。

这是 `tern`（Electron + React 的网页实现）的 Go 重写，产品与内容契约保持一致，
可见 `../tern/PRD.md`。

## 快速开始

`go.mod` 用 `replace` 指向同级的 `../mygo`（正在开发的框架）；如果 MyGo 已经发布，
去掉 `replace`、改成固定版本即可。

```bash
./dev.sh                     # 开发：本仓库代码 + ./data + 开发图标，端口 8788（推荐）
go run .                     # 桌面 App + 阅读服务（默认 http://127.0.0.1:8787）
go run . --headless          # 只跑阅读服务，供手机 / 浏览器访问
```

常用环境变量：

| 变量 | 作用 |
|---|---|
| `PORT` / `--port` | 首次运行时的阅读端口（之后以「设置 → 服务 → 端口」为准，保存会重启）；`dev.sh` 默认 8788 |
| `HOST` / `--host` | 监听地址，默认 `127.0.0.1`；要在局域网里调试用 `0.0.0.0` |
| `TERN_HOME` / `--root` | 临时指定数据根目录（默认用设置里选的目录） |
| `TERN_MEDIA_DIR` | 单独覆盖图片目录 |
| `TERN_PUBLIC_URL` | 手机上看到的地址；设了就整个用它（只替换端口），不再拼 Ponte 域名 |
| `TERN_PONTE` | 这台机器在 Surge Ponte 里的名字，默认 `STUDIO` |
| `TERN_PI_BIN` | 直接指定 `pi` 可执行文件，跳过运行时安装（开发用） |
| `TERN_HEADLESS=1` | 同 `--headless` |
| `TERN_ROUTE` | 桌面端启动时打开指定页面，如 `/a/inkwell`（开发 / 深链用） |

手机：这台机器在 Surge Ponte 里叫 `STUDIO`，用 Ponte 组网后 iPhone 直接打开
`http://STUDIO.sgponte:8787`（开发版是 `http://STUDIO.sgponte:8788`，见下）。Ponte 在远端
把 `<名字>.sgponte` 解析成 `127.0.0.1`，所以服务只监听 `127.0.0.1` 也够用。换台机器用
`TERN_PONTE` 改名字，用隧道或别的地址就整个覆盖 `TERN_PUBLIC_URL`。

手机也能导入和继续对话：打开 `/threads`，贴链接开一个新对话；点进去能看到 Agent 的过程，
并在下面继续提要求。文章读完仍然在同一台手机上。

## 构建与发版

```bash
make app                  # 本机 .app（build/darwin-arm64/Tern.app），不出 DMG
make release V=0.1.1      # 写版本号 → 测试 → 打 DMG → 打 tag → 推 GitHub Release
```

只出本机 macOS 的包（当前 arm64）：`go tool mygo build` 生成 `.app` 和
`Tern <版本> <架构>.dmg`，版本号取自 `mygo.json`，tag 是 `v<版本>`。没有 Developer ID
时是 ad hoc 签名，别人下载后需要右键「打开」；有证书时：

```bash
make release V=0.1.1 SIGN="Developer ID Application: Name (TEAMID)"
```

## 桌面端的分工

```
┌──────────────────────────────────────────────────────────────┐
│ MyGo 原生窗口                                                  │
│ ┌──────────┬───────────────────────────────────────────────┐ │
│ │ 侧栏(原生)│ 页面（原生）                                    │ │
│ │ 文章      │  文章列表 / 对话列表 / 生词本 / 设置             │ │
│ │ 对话      │  阅读页、对话详情：ui.WebView 嵌入的网页         │ │
│ │ 生词本    │   对照/译文/原文 · A−/A+ · 在浏览器打开         │ │
│ │ 设置      │                                               │ │
│ └──────────┴───────────────────────────────────────────────┘ │
└──────────────────────────────────────────────────────────────┘
```

- 原生部分是 `internal/app`：`ui.List`、`ui.Segmented`、`ui.WebView` 等，没有 HTML、没有前端构建。
- **没有单独画的标题栏**：`TitleBarHiddenInset`，让红绿灯浮在侧栏的材质上（`VibrancySidebar`），
  `Tern 双语阅读` 就在红绿灯同一行；内容区顶部只有一条不可见的拖动带（Windows / Linux 上顺带让开窗口按钮）。
  阅读页的工具条属于页面，不是窗口标题栏。
- 网页只有一份：`internal/server/static/index.html`（单文件，样式与脚本内联）。
  - 桌面阅读页：`ui.WebView` 加载 `/a/<id>?embed=1&mode=…&size=…`，页面隐藏自己的页头与工具条，
    由原生工具条通过 `window.tern.setMode() / setSize()` 驱动。
  - 桌面对话详情：同一个 WebView 换成 `/t/<id>?embed=1`，上面一条原生工具条负责返回和「阅读全文」，
    对话内容与输入框都在网页里。
  - 手机：`/threads` 与 `/t/<id>` 直接打开，带自己的页头与输入框；阅读页同理。
- 两者共用同一套 JSON API 和同一个文章库，所以“电脑上预览到的就是手机上的效果”。

## 架构

```
main.go                启动阅读服务 + 桌面窗口
internal/
  app/                 MyGo 原生 UI：侧栏 / 文章 / 对话 / 生词本 / 设置 / 阅读页
  server/              net/http：JSON API、/media、阅读页（go:embed）
  threads/             会话：一个链接=一个对话，常驻 Pi 子进程、事件日志、入库
  pi/                  Pi RPC 客户端、运行时安装（GitHub release + SHA256 + 解包）、提示词、查词
  publish/             入库：结构校验 → 图片本地化 → 落库 → 词典预热
  model/               Article / Block / Vocab / Thread 结构（全站唯一内容契约）
  store/               JSON 文件存储（文章库、生词本、词典缓存）
  config/              路径与设置（data/config.json）
```

### 摄取：一个链接 = 一个对话

```
贴链接 → threads.Create
  └─ 起/复用 Pi 子进程（--mode rpc，cwd=data/threads/<id>/work）
     ├─ 提示词要求：ego-browser 抓正文 → 写 article.json → 调 publish_article
     ├─ 扩展 publish_article 把文件 POST 回 /api/threads/<id>/publish
     ├─ 主进程校验（字段级 issues 回给模型）→ 下载图片 → 落库 → 预热词典
     └─ 入库成功后立即 abort 本轮，避免模型继续“自查”
```

- 会话 id 固定在 `tern-<id>`，进程被回收或服务重启后仍能续聊。
- 每个对话同时只跑一轮；服务重启时卡在 running 的对话会标成“中断”。
- `publish_article` 永远读工作目录里的 `article.json`，每次调用覆盖同一篇，不产生副本。

### 阅读页

- 双语块 1:1 对齐：`p / h2 / image / quote / list`，缺译文时降级单语。
- 宽度 ≥ 1000px（嵌入模式 ≥ 780px）时自动左右双栏：英文在左，中文在右。
- 点英文单词查词：文章内 `glossary` → 词典缓存 → 模型；可加入生词本。
- 阅读模式、字号记在本机（桌面写进 `data/config.json` 的 `reading`，手机用 localStorage）。
- 首页、生词本、详情页都支持手机宽度：无横向滚动，底部留出安全区。

### API

| 方法 | 路径 | 说明 |
|---|---|---|
| GET | `/api/articles` | 列表（不含 blocks） |
| GET | `/api/articles/{id}` | 单篇全文 |
| POST | `/api/lookup` | `{ term, context? }` → 词典条目 |
| GET/POST/DELETE | `/api/vocab` | 生词本 |
| GET | `/api/threads` | 对话列表 |
| POST | `/api/threads` | `{ url }` → 新对话（第一轮即摄取） |
| GET | `/api/threads/{id}` | 对话与事件（`busy` 表示正在跑） |
| POST | `/api/threads/{id}/messages` | 续聊 |
| POST | `/api/threads/{id}/publish` | Agent 入库（扩展调用） |
| POST | `/api/threads/{id}/images` | Agent 图床（扩展调用） |
| GET/POST/DELETE | `/api/runtime` | Pi 运行时：下载 / 切换 / 删除 |
| GET/POST | `/api/config` | 设置（密钥只写不读） |
| POST | `/api/config/test` | 用当前配置跑一次最小请求 |
| GET | `/media/*` | 本地图片（内容 hash 命名，长缓存） |
| GET | `/`、`/a/{id}`、`/vocab` | 阅读页（同一张 HTML） |

## AI 运行时

和原版一样，不依赖系统里的 `pi`：设置页从 GitHub releases 下载官方可执行文件，
校验 SHA256，解包到 `data/runtime/pi/<版本>/`，`current.json` 指向当前版本，多版本并存可回滚。
然后填 DeepSeek API Key（写在 `data/config.json`，权限 600，接口不下发）。

密钥通过环境变量交给子进程（`DEEPSEEK_API_KEY`），不出现在命令行里。
扩展源码编译进二进制，启动时写到 `data/pi/tern-extension.ts`，用 `-e` 加载，`-ne` 关闭其它一切扩展/MCP/技能。

## 数据

端口、数据目录都在「设置 → 服务」里：端口改完点「保存并重启」，数据目录选完立即生效。

默认在 **`~/Library/Application Support/Tern/`**（`data/` 与 `media/` 都在里面），
设置页「数据目录 → 选择…」可以换成任意文件夹，切换立即生效、下次启动沿用；
文章库、图片、生词本、对话和 Pi 运行时都跟着走。开发时用 `./dev.sh`
（等价于 `TERN_HOME=. PORT=8788 go run .`）；从源码跑会自动用开发版图标，
端口也和正式版错开（8788 / 8787），两个可以同时开着。

```
data/
  config.json          # 资源目录、模型、阅读偏好（含密钥，0600）
  library/<id>.json    # 文章
  threads/<id>.json    # 对话事件
  threads/<id>/work/   # Agent 的工作目录（article.json、抓下来的正文）
  pi/tern-extension.ts # 内置扩展（每次启动重写）
  pi/sessions/         # Pi 自己的会话文件
  runtime/pi/<版本>/   # Pi 可执行文件
  dict.json            # 查词缓存
  vocab.json           # 生词本
media/                 # 图片（可用 TERN_MEDIA_DIR 覆盖）
```

## 测试

```bash
go test ./...                 # 结构校验 / 存储 / 入库 / 原生 UI（ui.Tester）
TERN_SHOTS=1 go test ./internal/app -run TestSnapshot   # 把每页原生 UI 画到 /tmp/tern-shots
```

`ui.Tester` 在内存里渲染原生视图，不需要窗口；快照测试用来检查排版。

## 界面约定

状态切换、进度、提示与报错**不允许靠新增/删除元素来实现**：那会让周围 UI 跳动。
所有这类信息都占用一个固定高度的槽位（原生 `statusLine`，网页 `.status-line`），
页面骨架先渲染、数据到位后原地填充；对话输入框、阅读工具条这类框架始终存在，只做
`Disabled` / `Invisible`。这条规则写在 [AGENTS.md](./AGENTS.md) 里，改界面前先读。

## 已知限制

- 阅读页依赖系统 webview（WKWebView / WebView2 / WebKitGTK）；浏览器版（wasm）没有它，
  桌面端会自动降级为「在浏览器打开」。
- 没有 Service Worker，断网打不开；文章存 JSON 文件，没有全文检索。
- 未配置 Agent 时，查词只命中文章内的生词表；摄取接口会返回可读的原因。
- Windows 的 Pi 发布包是 zip，当前只实现了 tar.gz 解包。
