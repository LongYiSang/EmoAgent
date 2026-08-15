# 配方：改前端

## 何时用

改 `web/` 下的任何东西：聊天页、管理端、日志页、插件页。

## 锚点文件

| 锚点 | 作用 |
|---|---|
| `web/package.json` | `build` 脚本 = 两轮 `tsc --noEmit` + `vite build` |
| `internal/web/embed.go` | `//go:embed static` —— Go 二进制在**编译时**把前端产物打进去 |

## 这个配方存在的唯一理由

**改完前端不 rebuild 就重编 Go 二进制，`go:embed` 会继续打包旧的 dist。**

现象是"代码明明改了，页面没变"，而且：编译成功、启动成功、没有任何报错、没有任何警告。很容易怀疑是缓存、是浏览器、是自己改错了地方 —— 实际上只是产物没重新生成。

## 步骤

### 1. 装依赖（首次或依赖变动时）

**必须用 pnpm，plain `npm` 在这里会失败。** 没有全局安装就用 `npx`：

```bash
cd web && npx --yes pnpm install
```

### 2. 改代码

源码在 `web/src/`，四个页面分别对应 `chat` / `admin` / `logs` / `plugins`，共用部分在 `shared`。

### 3. 构建（别漏）

```bash
cd web && npx --yes pnpm run build
```

产物落到 `internal/web/static/dist`，也就是 `go:embed` 抓取的位置。

### 4. 重编后端

```bash
go build -o ./bin/emoagent ./cmd/emoagent
```

顺序不能反：**先 build 前端，再 build 后端。**

## 只调前端时不用走完整流程

开发期用 dev server，它代理 `/api` 和 `/ws` 到 `:8080`，改动即时热更新，不需要 build，也不需要重编 Go：

```bash
cd web && npx --yes pnpm run dev
```

然后访问 `http://127.0.0.1:5173`（后端要另开一个跑着）。

只有要验证**最终打包结果**、或要交付二进制时，才需要走上面的 build 流程。

## 验证

```bash
cd web && npx --yes pnpm run build
```

`build` 脚本会先跑两轮 `tsc --noEmit`（`tsconfig.json` 与 `tsconfig.node.json`），类型错误在这一步就会暴露，不会带进产物。

确认产物确实更新了 —— 比对 dist 的修改时间，应该是刚刚：

```bash
ls -la internal/web/static/dist
```

然后重编并启动，在浏览器里确认改动生效：

```bash
go build -o ./bin/emoagent ./cmd/emoagent
```
