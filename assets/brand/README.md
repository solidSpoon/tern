# Tern 品牌素材

一只卡通燕鸥的头像：大脑袋、黑色头顶、一只圆眼睛、长橙喙、脸颊一点腮红。

整张图是 `tools/icon` 用代码画的，没有描图、没有位图素材：头是一个圆，头顶沿着头的
圆弧切出来再收一条波浪线，喙是两段贝塞尔，眼睛和腮红是椭圆。改形状就是改 `main.go`
里的几个数，重新跑一次即可。

同一份描述同时产出 SVG、PNG、`.icns` 和 `.ico`，Dock 图标和网页 favicon 不会走样。

## 重新生成

```bash
go run ./tools/icon                 # 重写 assets/brand 下的全部文件
go run ./tools/icon -try /tmp/x.png # 只画一张 1024 草稿，先看效果
```

`tools/icon/main.go` 只用标准库加 `golang.org/x/image`，不依赖任何外部工具。
头的位置在 `tern()` 开头（`cx, cy, r`），五官和配色都在附近，直接改数值。

## 生产 / 开发两个图标

| 文件 | 用途 |
|---|---|
| `icon.svg` / `png/icon-*.png` / `macos/AppIcon.icns` | 生产：极地夜色的底，米白的脸 |
| `icon-dev.svg` / `png/icon-dev-*.png` / `macos/AppIcon-Debug.icns` | 开发：暖赭（日落）的底，同一只鸟 |
| `web/favicon.ico`、`web/apple-touch-icon.png`、`web/icon-192.png`、`web/icon-512.png` | 阅读页 / PWA |
| `preview.png` | 两个变体 + 尺寸梯度 + 深色 Dock 条，人工检查用 |

两个变体**角色和形状完全相同，只换底色与喙的深浅**：Dock、⌘-Tab 里一眼能分出开发版，
又不用角标，也不用第二套视觉语言。极夜 / 日落这一对也正好对上“一年过两个夏天”。

## 配色

| 名称 | 生产 | 开发 |
|---|---|---|
| 底色（上 / 下） | `#17505C` → `#226B79` | `#C2653C` → `#AE4E2C` |
| 头顶 | `#08222A` | `#2B1610` |
| 脸 | `#F8F0DD` | `#FBF3E2` |
| 喙 | `#E8873C` | `#E07A38` |
| 眼 | `#0D2A33` | `#2B1610` |
| 腮红 | `#EFC3A2` | `#E79A76` |

底是 824 的大圆角方（圆角半径 185），四周留 100 的透明边，和系统图标的网格对齐。
底板在 SVG 里是一圈真正的贝塞尔圆角，不是几百段折线。脸被底板下沿裁掉一截，是刻意的：
头像比整只鸟更耐缩小，16 px 下还剩“黑头顶 + 白脸 + 橙喙”三个信号。

## 在应用里用

开发时不用打 bundle：从源码启动（`go run .` / `./dev.sh`）时，app 发现自己在
bundle 外（`mygo.App.IsPackaged()` 为假），就把 `png/icon-dev-512.png` 设成 Dock
图标；打包成 `.app` 后不再干预，图标由 bundle 里的 `.icns` 提供。

macOS 端把 `.icns` 交给打包步骤即可；网页端在 `internal/server/static/index.html`
的 `<head>` 里加：

```html
<link rel="icon" href="/favicon.ico" sizes="any">
<link rel="apple-touch-icon" href="/apple-touch-icon.png">
```

`design/ref.jpg` 是最早的参考稿，现在只作资料留存，生成器不再读它。
