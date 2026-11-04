---
name: ci-check
description: 提交或推送 video-canvas 代码前，在本地跑一遍和 GitHub Actions CI 相同的格式、lint、类型、测试和构建检查，并按 CI 规则修好再交付。改完代码准备 commit / push / 开 PR、pre-commit 钩子报错、CI 挂了要修、或用户提到 /ci-check、"跑一下检查"、"格式问题"时使用。
---

# 提交前对齐 CI

CI（`.github/workflows/ci.yml`）有三个 job：backend（golangci-lint 增量 + 迁移只增 + build + `go test -race`）、
agent（oxfmt --check + oxlint + typecheck + test + build）、web（oxfmt --check + tsc + oxlint + bun test + build）。
本 skill 的目标：**推上去之前本地就是绿的**，不靠 CI 发现格式问题再返工。

## 已有的自动约束（不需要你手动做，但要知道）

1. **编辑即格式化**：项目 `.claude/settings.json` 的 PostToolUse 钩子在每次 Edit / Write 后运行
   `scripts/ai-hooks/format-on-edit.mjs`：web / agent 文件跑 oxfmt + oxlint，Go 文件跑 `golangci-lint fmt`（gofmt + goimports，
   本地包前缀 `video-canvas`）。钩子报 lint 错误时（exit 2）**立即按报错修**，别攒到最后。
2. **不许跳过钩子**：PreToolUse 钩子 `scripts/ai-hooks/guard-git.mjs` 会拦下 `--no-verify`、`git commit -n`、
   把 `core.hooksPath` 置空。被拦就修问题，钩子本身坏了就停下来告诉用户。
3. **pre-commit**：`.pre-commit-config.yaml` 在提交时查暂存文件的 oxfmt（web / agent）、整个 web 的 oxlint / tsc、
   整个 agent 的 oxlint、后端 gofmt 与 golangci-lint 增量。

## 交付前流程

1. 跑和 CI 一致的检查（只跑改动涉及的 web / backend / agent）：

   ```bash
   bash scripts/check.sh --fix      # 先自动格式化再检查；想先看问题就去掉 --fix
   SKIP_TESTS=1 bash scripts/check.sh   # 快速版：不跑测试
   bash scripts/check.sh web        # 只跑某一端
   ```

2. 全部 ✓ 再 commit。失败项按下面的「常见失败」处理，修完重跑失败的那一项，最后整体再跑一遍。
3. 交付说明里写上跑了哪些检查和结果；没法跑的（比如本机没有 Postgres/Redis，仓储集成测试会 Skip）要明说。

## 常见失败与修法

| 报错 | 原因 | 修法 |
|------|------|------|
| `Format issues found in above N files` | 前端没用 oxfmt 格式化 | `cd web && BUN_OPTIONS= bun run format`；只格式化自己改的文件时用 `bun node_modules/oxfmt/bin/oxfmt <文件>` |
| agent 的 `Format issues found` | agent 没用 oxfmt 格式化 | `cd agent && npm run format`，或 `npx oxfmt <文件>` |
| golangci-lint 报 `File is not properly formatted (gofmt/goimports)` | Go 没格式化或 import 分组不对 | `cd backend && golangci-lint fmt ./...` |
| `cognitive complexity N of func X is high (> 20) (gocognit)` | 函数分支嵌套太深 | 拆出小函数、提前 return、用表驱动；**不要**加 `//nolint` 糊过去 |
| `funlen`：函数超过 80 行 / 50 条语句 | 函数太长 | 按步骤拆函数 |
| `depguard` | 违反分层（如 service 引了 gin / gorm） | 按 `backend/AGENTS.md` 的依赖方向改 |
| oxlint `no-unused-vars` 等 | 未用的变量 / import | 删掉；确实要留的变量用 `_` 前缀 |
| tsc 报错 | 类型不对 | 修类型，不用 `any` / `@ts-ignore` 绕过 |
| 测试失败 | 行为变了但测试没跟上，或断言依赖时间 / 顺序 | 先确认是代码错还是测试过期；异步断言用轮询 + 超时 |

**写 Go 时就按限额写**：函数 ≤ 80 行、≤ 50 条语句、认知复杂度 ≤ 20。一个函数里 if/for/switch 嵌套到第三层就该拆了。

## 本机的坑

- `BUN_OPTIONS` 带 `--preload` 时，`bun run <脚本>` 只打印帮助并**返回 0**——看起来通过，其实什么都没跑。
  在 web 下跑脚本前先 `unset BUN_OPTIONS`（PowerShell：`$env:BUN_OPTIONS=''`）。`scripts/check.sh` 已经处理了。
  `bun test` 需要安全钩子时见 `AGENTS.md` 的说明。
- 本机 PowerShell 里默认 node 可能是 v16，`node_modules/.bin/oxfmt` 会报 `ERR_UNKNOWN_FILE_EXTENSION`，用 `bun node_modules/oxfmt/bin/oxfmt`。
- Windows 上用 Python 写文件默认 CRLF，会让 gofmt 整份报错；改文件用 Edit / Write 工具。
- 只用仓库自带的 oxfmt / oxlint，不要用 prettier、eslint 或编辑器默认格式化。
- 不要顺手格式化与任务无关的文件（会把别人的改动混进你的提交）；`check.sh --fix` 会格式化整个 web，
  有别人未提交的改动时改用单文件格式化。
