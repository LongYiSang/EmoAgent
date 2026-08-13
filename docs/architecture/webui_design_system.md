# EmoAgent WebUI 设计系统

约束 `web/src/shared/styles/` 的取值范围。**token 本来就存在，问题从来不是缺 token，而是没有约定，于是被逐处绕过** —— 这份文档就是那份约定。

改 `app.css` 前先读这一页。

---

## 1. 排版

### 字号：7 档，不得新增

| token | 值 | 用途 |
|---|---|---|
| `--text-2xs` | 11px | 徽章、级别标签、全大写拉丁标签。**不用于中文正文** |
| `--text-xs` | 12px | meta、hint、辅助说明 |
| `--text-sm` | 13px | 密集表格、日志正文、按钮、导航项 |
| `--text-base` | 14px | 正文默认（`body` 已设置，多数元素无需声明） |
| `--text-lg` | 16px | 卡片标题 |
| `--text-xl` | 20px | 页面标题 |
| `--text-display` | 28px | 总览大数字 |

不允许出现半像素值。此前 100 处声明散在 15 个值上，含 10.5 / 11.5 / 12.5 / 13.5 / 14.5 —— 那是逐处目测微调的痕迹，不是标尺。

### 字重：4 档，上限 700

| token | 值 | 用途 |
|---|---|---|
| `--weight-normal` | 400 | 正文、消息内容、日志行、表单值 |
| `--weight-medium` | 500 | 标签、meta、按钮、导航项 |
| `--weight-semibold` | 600 | 卡片标题、强调数值、徽章、`<strong>`/`<b>`、激活态 |
| `--weight-bold` | 700 | **仅**页面标题 |

> **核心规则：层级靠颜色，不靠字重。**
> 次要文本请换 `--color-muted`，不要把字重从 600 调到 650。
>
> 这不是风格偏好。修复前 `app.css` 有 73 处 font-weight 声明，取值
> 600/650/700/750/800/850/900，**0 处使用 400 或 500**；admin 单页 118 个文本
> 节点里 106 个（90%）字重 ≥600。全都加粗等于没有重点，这正是「界面不好看」
> 的主要来源。650/700/750 在 Inter 里本就几乎不可区分，多出来的档位只制造
> 混乱，不产生信息。

### 层级检查

页面标题与卡片标题**必须**可区分。二者此前共用一条 CSS 规则，同为 20px/750，
页面与其内部卡片之间无法形成任何层级。现在：

- 页面 `<h1>` → `--text-xl` / `--weight-bold`
- 卡片 `.section h2` / `.pane-head h2` → `--text-lg` / `--weight-semibold`

**不要在 tab 内重复渲染页面标题。** `AdminApp` 已经渲染了分组 eyebrow + `<h1>{tab.label}</h1>`。
主从布局里显示「当前选中项名字」的 `<h2>` 是例外，那不是重复。

---

### 字体栈

`Inter Variable` 通过 `@fontsource-variable/inter` 打包，**只引 latin 子集**
（见 `shared/styles/fonts.css`），汉字由栈里后面的 Noto Sans SC / PingFang SC /
Microsoft YaHei 承担。

> **不要在字体栈首位写一个没有打包的字体。** 之前 `--font-ui` 首选 `Inter`
> 却从未提供 `@font-face`，于是在未安装该字体的机器上，第二顺位的
> **Noto Sans SC（中文字体）接管了全部拉丁文字**。日志页 90% 是拉丁内容，
> 所以最先在那里暴露。同理 `--font-mono` 首选的 JetBrains Mono 也没打包，
> 现已补上 Cascadia Mono / Consolas / Menlo 等系统等宽字体兜底。
>
> 只引 latin 子集是有意的：`wght.css` 会带进 7 个子集共 ~224KB，浏览器只下载
> 其中一个，但 `go:embed` 会把 7 个全部塞进二进制。只引 latin = 48KB。

**任何成列的数字都要 `font-variant-numeric: tabular-nums`**（时间戳、计数、
用量）。比例数字会让整列在滚动时左右抖动。

## 2. 间距

4px 基准，两个半档供胶囊内部使用：

`--space-05` 2 · `--space-1` 4 · `--space-15` 6 · `--space-2` 8 · `--space-3` 12 · `--space-4` 16 · `--space-5` 24

不允许 1 / 3 / 5 / 7 / 10 / 14px 这类落在标尺外的值。

---

## 3. 圆角

`--radius-sm` 8 · `--radius-md` 12 · `--radius-lg` 14 · `--radius-xl` 16 · 胶囊 `999px`

**嵌套时圆角必须向内递减：**

```
外层卡片 .section / .log-stream   → --radius-lg (14)
内层分区 .slot / .config-section  → --radius-md (12)
控件 input / button / 图标按钮     → --radius-sm (8)
```

修复前 `.slot` 是 16px 却位于 14px 的 `.section` 内 —— 内圆角大于外圆角，
方向反了，嵌套盒子会一直看起来微微「鼓出来」。这类问题说不出名字，但一眼就觉得不对。

---

## 4. 颜色

### ink-on-soft 配对表

ink 系颜色是**按白底设计的**。一旦背景换成 `*-soft` 淡色底就会掉到 AA 以下，
必须换用 on-soft 变体：

| 背景 | 不要用 | 要用 |
|---|---|---|
| `--color-primary-soft` | `--color-primary` (3.57:1) | `--color-primary-on-soft` (5.17:1) |
| `--color-surface-2` | `--color-muted` (4.34:1) | `--color-muted-on-soft` (6.86:1) |

`--color-muted-2` **禁止用于任何文本** —— 白底上仅 2.56:1。它只能做分隔线之类的非文本用途。

### 暗色

- 只有一套暗色调色板，定义在 `tokens.css` 的 `:root[data-theme="dark"]`。
  四个 HTML 入口都有首屏前脚本负责解析并写入 `data-theme`，所以**不需要**
  再复制一份 `prefers-color-scheme` 版本（复制就意味着以后要同步两处）。
- 面板底色一律走 `--surface-veil` / `--surface-veil-strong`。
  **不要硬编码 `rgba(255,255,255,…)`** —— 那会在暗色下变成白块。
- 暗色的强调色是亮青，其上必须用**深**字（`--color-primary-foreground`
  在暗色下是 `#0b1220`）。近白色文字在亮青上只有 2.65:1。

### 语义色 vs 装饰色

状态色（success / warning / danger）**只在真的表达该状态时使用**。
不要为了好看给卡片上色 —— 那会稀释真正的告警信号。

---

## 5. 验证

前端没有测试框架。改完排版/颜色后跑：

```bash
cd web && npx --yes pnpm run build
```

再在浏览器里跑这两段自查（明暗各一次）：

- **标尺自查**：统计页面上 distinct 的 `font-size` / `font-weight`。
  字号应全部落在上表 7 档内，字重应只有 400/500/600/700。
- **对比度自查**：遍历所有文本节点计算对比度，正文需 ≥4.5:1
  （≥24px，或 ≥18.66px 且 ≥700 时为 3:1）。目标是 0 处失败。
  `aria-hidden` 的 emoji 是误报 —— 彩色字形不受 `color` 影响。

> 注意：不要靠改 `data-theme` 属性就地切换主题来做审计，计算样式可能不会
> 完整重解析，会得到假的失败项。**改 localStorage 后 reload。**

---

## 附：本轮修复留下的三条经验

1. **虚拟列表里的间距要放在被测量的盒子内。** `.timeline-virtual-row` 用
   `measureElement` 测高，margin 不会被计入，会导致行重叠；padding 才会。
2. **`minmax(…, max-content)` 不要用在虚拟列表的行上。** 行是绝对定位的，
   容器只量得到表头，于是行比容器宽却又滚不到 —— 日志页约 475px 正文
   曾经因此永久不可见。
3. **flex 容器里的图标按钮要写 `flex-shrink: 0`。** 日志工具栏的动作列
   曾被压到 83px，五个 34px 按钮各自缩成 ~14px。
