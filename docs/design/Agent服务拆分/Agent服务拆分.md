# Agent 服务拆分

> - **范围**：把画布 Agent 从 Go 后端拆成独立的 TS 服务。最终形态基于 Pi Durable，目标是始终跟上 Pi 生态。Go 只保留平台能力 API。
> - **日期和状态**：2026-10-25，方案已确认，未实现。阶段顺序、旧会话、语言、部署、技能归属都已确认（见第 14 节）。
> - **证据约定**：**事实**附文件路径或来源；**推断**是基于事实的判断；**提案**是尚未实现的设计。
> - **方案演进**：
>   1. 最初只评估"要不要在现有运行时里接入 Pi Durable"，结论是不建议，原因是存储和运行模型冲突。
>   2. 用户提出把 Agent 拆成独立服务（方案 C），冲突随之消失。
>   3. 用户提出把 Pi Durable 放到最后，于是调整为"先让运行时独立，再接 Pi Durable"，保证编排逻辑只搬一次。
> - **配套演示**：[Agent服务拆分-演示.html](Agent服务拆分-演示.html)，包括各阶段架构、功能对比、代码对比和待验证项。

## 1. 设计摘要

- **问题**：Agent 的编排逻辑分散在两处。Go 有约 1.27 万行（会话、运行、审批、续跑、观测、技能），Node 运行时自己实现了插话、压缩、裁剪和断点续跑。这两处都和 Go 后端绑在同一个进程、同一个镜像里，所以无法单独发版，也无法直接使用 Pi 生态。
- **目标**：
  - Agent 服务成为一个**可单独组装**的 TS 服务。以后扩展 Agent（新工具、子 Agent、团队、第三方扩展）只改这个服务。
  - 能**持续跟上** Pi 的版本。
  - Go 只在新增平台能力时才需要改。
- **推荐结论**（提案）：
  1. **分 4 个阶段**：
     - 阶段 1：平台 API。
     - 阶段 2：运行时独立成长驻、无状态的 TS 服务。
     - 阶段 3：接入 Pi Durable，编排逻辑整体搬过去，同时删掉自写逻辑。
     - 阶段 4：观测迁移，下线旧实现。

     每个阶段都能单独上线、单独回退。
  2. **钱和画布数据永远留在 Go**：计费、渠道密钥、画布事务、生成任务、素材、技能管理、鉴权。
  3. **Agent 服务只做组装**：只用 Pi 的内置能力和公开 API，不写自定义 Storage，所有 `@earendil-works/*` 包同步锁定版本。
  4. **部署**：在 Docker Compose 里新增一个 `agent` 服务，单副本，只在内网可见。阶段 3 起增加 SQLite 数据卷，升级脚本要同时备份这个卷。

## 2. 项目现状

### 2.1 事实

**运行时**
- `agent/` 是一个独立的 Node 包，依赖 `@earendil-works/pi-agent-core`、`pi-ai`、`pi-codemode`，版本都是 1.3.0，要求 Node >= 22.19。证据：`agent/package.json`。
- Go 每跑一个运行片段，就起一个 `node main.mjs` 子进程。启动参数和 `steer`/`abort` 走 stdin，片段结束进程就退出。证据：`agent/src/main.mjs`，`backend/internal/service/agent/runtime.go`。
- Go 侧已经有运行时接口 `AgentRuntime`（`Start`/`Interject`/`Cancel`/`Resume`），当前的实现是 `ProcessRuntime`。证据：`backend/internal/service/agent/agent.go`，`runtime.go`。**这是阶段 2 的替换点。**
- Node 通过桥调用 Go，接口有 `/internal/agent/bridge/{v1/chat/completions, tool, tool-event, state, finish}`，鉴权用一次性令牌（存在内存 map 里，有效期 2 小时）。证据：`backend/internal/router/bridge.go`，`backend/internal/service/agent/bridge.go:100-125`。
- 桥**必须监听回环地址**，原因是经过反向代理时 `X-Forwarded-For` 可以伪造。证据：`router/bridge.go` 的注释，`internal/config` 中 `BridgeAddr` 默认 `127.0.0.1:0`。
- `@earendil-works/pi-agent-core` 1.3.0 **没有压缩，也没有持久化和续跑**：dist 里没有压缩实现，README 里没有存储和续跑的章节。所以以下逻辑都是自己写的：
  - `agent/src/compaction.mjs`（摘要压缩）；
  - `context.mjs`（裁剪、去图）；
  - `steer.mjs`（插话中止）；
  - `run.mjs` 里的 `settle`/`start`/`stripFailedTail`（续跑）。

**状态与恢复**
- 每个回合结束（`turn_end`），Node 把整个消息数组交给 Go，覆盖写入 `agent_sessions.session_jsonl`（最大 8MB）。证据：`agent/src/run.mjs`，`bridge.go` 的 `SaveState`。
- 进程没报告结束就退出时，运行被标为 `interrupted`，**用户要手动点"继续"**。证据：`runtime.go:246-290`。
- 生成任务以"审批 id + 序号"作为幂等键，续跑不会重复扣费。证据：`service/agent/generate.go:208`。
- `agent_tool_calls` 表有唯一索引 `(run_id, tool_call_id)`，存了参数和结果（结果可能被截断，也可能被清理）。证据：`migrations/0008_agent_observability.sql:20-38`。
- `agent_mutations` 记录了 `tool_call_id`，但唯一索引是 `(run_id, seq)`，**画布写入没有按工具调用去重**。证据：`migrations/0001_baseline.sql:1006`，`model/agent.go:154`。

**耦合面**
- Go 侧 Agent 代码量：`service/agent` 8094 行，`handler/agent` 1233 行，`internal/agent/*`（canvasgraph、skillpkg、skills、prompts）3399 行。
- 反向引用只有 4 处：`initialize/agent.go`、`initialize/app.go`、`router/router.go`、`router/bridge.go`。
- 画布写入 `CommitCanvasMutation` 在**一个事务**里同时更新 `canvas_projects`（revision 乐观锁）和写入 `agent_mutations`。证据：`repository/agent.go:259`。
- WebSocket Hub 在进程内。多实例要换成 Redis pub/sub，业务代码不用改。证据：`internal/pkg/ws/types.go:35`。
- 前端通过 `/api/v1/agent/*` 和 `/api/v1/canvas/:id/agent/sessions` 访问 Agent，客户端代码在 `web/src/api/agent/`（约 490 行）。证据：`router/router.go:110-138`。

**部署**
- backend 镜像里打包了 Node 和 `agent/`。证据：`backend/Dockerfile:24-44`。
- 一键升级脚本只用 `pg_dump` 备份 Postgres，失败时退回旧镜像。证据：`scripts/update.sh:261-268`。

### 2.2 设计约束

- 渠道密钥不进 Agent 进程，现在如此，拆分后仍然如此。
- 画布保存依赖 `revision` 乐观锁，Agent 写画布的规则必须和用户手动保存一致。
- 生成任务走 `GenerationTaskService.Create`，校验、计价、冻结积分、并发上限和用户在画布上点生成完全一致。
- 前端接口保持不变：拆分过程中前端不需要改。

### 2.3 当前缺口

- Agent 无法单独发版，升级 Pi 就要重新发 Go 镜像。
- 中断后要用户手动继续，而每次发版重启都会中断正在跑的运行。
- 插话、压缩、续跑都是自己写的，与 Pi 的演进方向分叉，每次升级都可能冲突。
- 推断：历史只在 `turn_end` 保存（`agent/src/run.mjs:34`）。如果一个回合里 Go 已经执行了画布写入，Node 还没存历史就崩溃，续跑时这一回合会从头再来：模型重新生成回复，**工具调用 id 也是新的**，于是可能把同样的改动再做一遍。按 `tool_call_id` 去重挡不住这种情况，要靠"每个工具结果出来就保存历史"来缩小这个窗口（见 8.2）。
- 提问、生成、删除三类审批都按调用各建一条（`approval.go:76`），`agent_approvals` 上没有按 `(run_id, tool_call_id)` 的唯一约束。

## 3. 目标与非目标

**目标**
- Agent 服务独立部署、独立发版、独立升级 Pi。
- 以后扩展 Agent 的能力时 Go 不用改。新增平台能力时才需要改 Go。
- 最终获得 Pi Durable 的持久化执行：每次提交原子化、自动续跑、崩溃熔断。
- 为子 Agent、团队、记忆等扩展留好位置（见第 11 节）。

**非目标**
- 不迁移旧会话（用户已确认）。阶段 3 之前的会话在 Go 里只读保留。
- 不做多副本和水平扩容（见第 9 节）。
- 不改前端接口，不改画布、计费、生成任务的业务规则。

## 4. 调研

### 4.1 Pi Durable（`@earendil-works/pi-durable`）

事实来自 npm 元数据和 1.4.0 包内的 README、CHANGELOG。

- **基本信息**：MIT 协议，最新版 1.4.0（2026-10-21），要求 Node >= 22.19.0。它锁定依赖同版本的 `pi-ai`、`chord`、`codemode`、`pi-telemetry`。
- **核心概念**：
  - Harness：一个打开的存储加上调度机制。
  - Conversation：由不可变 entry 组成的记录。
  - Commit：原子写入。
  - Document：和记录存在一起的类型化 JSON 状态。
  - Task：每一步都存检查点的状态机。
  - Submission：一次提交，可以按 `requestId` 去重。
  - Extension：一组 tools、sections、hooks、wraps、tasks 的打包。
- **恢复语义**：
  - 每次工具调用都是一个独立的 task，执行前先提交"意图"。
  - 崩溃后，只有声明了 `replay: "safe"` 的工具会重跑；其余工具给模型返回 `interrupted` 错误结果。
  - `resume()` 继续未完成的工作。
  - `restartLimit` 默认 5 次：同一个 task 连续崩溃 5 次后被挂起，等待 `retryTask`。
- **内置能力**：
  - 插话和后续消息（`whenBusy: "steer"`，`steeringMode: "all"`）；
  - 压缩（`pi.compaction`，可按模型设置阈值）；
  - `afterFailedRun`；
  - 子任务、分叉、子 Agent；
  - durable sleep（1.4.0 新增）；
  - OTel 遥测（只记 id，不记内容）；
  - Hooks：`beforeRequest`、`afterResponse`、`onYield`、`afterTools`、`beforeTool`、`afterTool`。
- **存储**：Memory、IndexedDB、SQLite（WAL）、JSONL、Cloudflare Durable Object，**没有 Postgres**。"一个存储同一时间只能被一个进程持有，没有跨进程锁。"
- **发版节奏**：1.0.0（10-01）到 1.4.0（10-21），三周发了 11 个版本。**小版本里也有破坏性变更**。1.4.0 就有：
  - 删除了 `sleep()`；
  - 增加了 `sleeping` 状态；
  - **SQLite 自动升级到 schema v2，升级后 1.3.0 打不开，部署不能回滚**；
  - 崩溃熔断改成默认开启；
  - `defineDoc` 必须显式传 `checkpointWhen`。
- **扩展格式**：`defineExtension` 是 Pi Durable 自己的格式。README 没有提到和 `pi-coding-agent` 扩展的兼容性，所以 pi.dev 上的包要逐个确认能不能用。

### 4.2 Toonflow（HBAI-Ltd/Toonflow-app，MIT 协议，提交 `30139c9`，2026-10-21）

- **形态**：桌面应用（Electrobun + Bun + Vue Flow），单用户，数据在本地工作区文件里。Agent 基于 `@earendil-works/pi-coding-agent` 0.85.1，会话、压缩、重试都直接用 SDK。证据：`apps/server/package.json`，`apps/server/src/agent/runtime/index.ts`。
- **值得借鉴的地方**（详见第 11 节）：
  - **每个工具自带提示词**：包级 `prompt`，加上运行时的 `promptSnippet`/`promptGuidelines`。只合并本轮实际加载的工具的规则。证据：`packages/toolScaffold/readme.md`。
  - **子 Agent 和声明式团队**：`subAgent` 并行委派，结果有明确状态，子会话用户可以进入，子 Agent 用 `report` 上报。`team.json` 声明成员、可委派对象、工具、技能和知识。证据：`apps/server/src/agent/tools/subAgent.ts`，`runtime/delegation.ts`，`packages/teams/storyboardTeam/team.json`。
  - **节点自己注册 Agent 函数**（`nodeTools.register`），Agent 按需查询 schema。证据：`packages/nodeScaffold/readme.md:196-237`。
  - **画布读取有上限**：64 KiB，支持游标分页、字段投影、按路径分段读。
  - **用户记忆带 revision 乐观锁**。证据：`apps/server/src/agent/tools/memory.ts`。
- **不借鉴的地方**：
  - 画布操作在浏览器里执行。待回传的调用存在进程内的 Map 里，注释自己写了"多进程部署需共享请求通道"。
  - 插件是可信代码，没有沙箱。
  - 子任务的并发、轮次、时长不设上限。
  - 超长的规则型技能（`packages/skills/workflow/SKILL.md`）。
  - 停在 Pi 0.85.1，没有跟上 1.x。这说明"跟上 Pi 生态"需要专门投入，不会自动发生。

## 5. 设计原则

1. **能用 Pi 内置的就用内置的，自写逻辑要删掉。** 产品上的差异用 hooks 和 settings 表达，不 fork、不打补丁。
2. **只依赖公开 API。** 只用包根导出和文档列出的子路径，不深层引用 dist 内部。**不写自定义 Storage**，存储内部结构每个版本都可能变，用官方的 SQLite 存储。
3. **所有包同版本升级。** 所有 `@earendil-works/*` 精确锁定同一个版本，作为一组一起升级。
4. **钱和画布永远在 Go。** 每次升级 Pi，最坏情况也只影响对话，碰不到积分和用户作品。
5. **升级流程固定**：备份 SQLite → 跑契约测试（假模型 + 假时钟，Pi Durable 支持注入 `now`/`setTimer`）→ 灰度 → 全量。
6. **第三方扩展走白名单**：扩展能看到提示词、工具调用和凭证，只装审过的。
7. **编排逻辑只搬一次**：从 Go 直接搬到 Pi Durable，不经过"用 TS 在 pi-agent-core 上重写"的中间态。

## 6. 目标架构（提案）

```
前端 ──HTTP /api/v1/agent/*──▶ Go（JWT 鉴权后反向代理）──▶ Agent 服务（TS, Pi Durable Harness + SQLite）
前端 ◀──WebSocket── Go Hub ◀── 平台 API：事件推送 ─────────────┘
Agent 服务 ──模型 / 工具 / 技能 / 计费──▶ Go 平台能力 API（由现在的桥升级而来）
```

| 归 Agent 服务（阶段 3 起） | 留在 Go（平台能力） |
|---|---|
| 会话、运行、时间线事件、审批和提问、计划 | 用户鉴权、RBAC，WebSocket 推送 |
| 系统提示词（按扩展拆分）、工具声明 | 画布读写和撤销（`canvasgraph`、`agent_mutations`，留在同一个事务里） |
| 插话、压缩、续跑、子 Agent | 模型网关、渠道密钥、按调用结算（`agent_model_calls`） |
| 扩展组装：`video-canvas-*` 扩展和白名单内的第三方扩展 | 生成任务（幂等键、冻结积分）、素材存储 |
| 运行观测数据（transcript、OTel） | 技能管理：导入、版本、启停、存储、审计 |

## 7. 分阶段方案（提案）

| 阶段 | 内容 | Go 的改动 | 退出条件 | 回退方式 |
|---|---|---|---|---|
| 1. 平台 API | 桥升级成平台 API（第 8 节），现有 Node 运行时照常工作；Node 改为每个工具结果后保存历史 | **唯一一次大改** | 现有行为不变，Go 测试和 `agent` 测试全部通过 | 配置回到回环监听 |
| 2. 运行时独立 | `agent/` 改写成长驻、**无状态**的 TS 服务，同步升级到 Pi 1.4。Go 新增 `HTTPRuntime` 实现 `AgentRuntime`，用配置在 `process` 和 `http` 之间切换。会话、运行、审批**仍然由 Go 管** | 只新增一个运行时实现 | 灰度用户的运行行为完全一致（插话、中止、审批、续跑） | 配置切回 `process` |
| 3. 接入 Pi Durable | 会话、运行、审批、插话、压缩、续跑搬进服务，同时删掉两边的自写逻辑。`/agent/*` 由 Go 反向代理，前端接口不变。阶段 3 之前的会话只读 | 删代码，加反向代理和平台 API 的"上下文头模式" | 第 12 节的验收全部通过；升级脚本能备份和恢复 `agent_data` | 反向代理开关切回 Go 实现（阶段 3 期间旧实现保留） |
| 4. 收尾 | 后台运行记录页和 `agent-trace` 改读新服务的导出接口，然后删除 Go 侧的会话、运行、审批代码和旧的 `agent/` 运行时 | 删代码 | 版本对比指标连续；全量用户在新服务上 | 不可回退（之前的阶段都已验证过） |

**阶段 0（验证，放在阶段 3 之前做）**：用 Pi Durable 1.4 跑通一个最小服务：SQLite 存储，一个调用 Go `/tool` 的工具，覆盖插话、中止、杀进程后 `resume()`。重点验证**"等审批"怎么建模**，README 没有现成例子。候选方案有两个：
- 工具返回 `terminate`，用户的决定再作为一次提交进来；
- 用自定义 task 加 durable sleep。

没跑通就停下来重新评估。

## 8. 阶段 1：平台能力 API（提案）

### 8.1 监听与鉴权

- **监听**：配置了服务令牌 `APP_AGENT_SERVICE_TOKEN` 时，允许桥监听非回环地址，比如 Compose 内网的 `0.0.0.0:<端口>`，只用 `expose`，不映射到宿主机。没配置时保持现在的回环限制，向后兼容。
- **两层鉴权**：
  - **服务令牌**（请求头 `X-Agent-Service-Token`）：证明调用方是 Agent 服务，用常量时间比较。
  - **运行令牌**（现有的 `Authorization: Bearer`）：证明是哪个运行。阶段 1、2 不变。
- **阶段 3 增加"上下文头模式"**：运行归 Agent 服务管理以后，Go 不再签发运行令牌，改成"服务令牌 + 上下文头"（用户 id、会话引用、运行引用、计费范围）。Go 依据这些上下文头计费、校验画布归属。阶段 1 只定义格式，不启用。

### 8.2 工具调用幂等（2026-10-25 修正）

最初的设计是"阶段 1 按 `tool_call_id` 去重，堵住崩溃后重复写画布"。读代码后发现，这在现有运行时里起不了作用：续跑时模型会重新生成调用，id 是新的。所以拆成两件事，分别放在不同阶段：

1. **阶段 1：缩小丢进度的窗口。** Node 在每个工具结果出来后（`message_end`）就保存历史，不再等 `turn_end`。这样续跑时模型能看到已经执行过的调用和结果，不会重做。改动很小，现在就有收益。
2. **阶段 3 的前提：写工具按 `(run_id, tool_call_id)` 幂等。** 这是为了让写工具在 Pi Durable 里可以声明为 `replay: "safe"`：崩溃后 Pi 用**同一个 id** 重跑，Go 直接返回第一次的结果。如果不做幂等，写工具只能声明为不可重放，模型收到 `interrupted` 后自己决定要不要重试，又会回到"新 id、可能重做"的老问题。
   - **去重依据要和副作用在同一个事务里**：
     - 画布写入用 `agent_mutations`，加上 `(run_id, tool_call_id)` 的唯一约束；
     - 提问、生成、删除用 `agent_approvals`，加上同样的约束；
     - `plan_update` 是整体覆盖，天然幂等。
   - **不能用 `agent_tool_calls` 做依据**：它是观测数据，写失败只记日志，没有绑定时不记录，结果还会被截断和清理（`observe_toolcall.go` 的接口注释）。
   - 要做数据迁移（新增唯一约束），实现前要先确认存量数据里有没有重复的记录。
   - 实现时要核实一点：一次工具调用会不会产生多条 `agent_mutations`，这决定了约束加在哪一层。
- **读工具**天然可以重放，阶段 3 里声明为 `replay: "safe"`。

### 8.3 事件推送入口（阶段 3 才用，阶段 1 先定契约）

- `POST /internal/platform/events`：入参是 `{user_id, session_ref, run_ref, canvas_id, type, data}`，Go 转发给 WebSocket Hub。
- 事件类型和现在的 `agent_events` 一致：`message.*`、`tool.*`、`plan.updated`、`approval.*`、`run.status`、`run.usage`、`canvas.patch`。这样前端不用改。
- 推送可以丢，前端靠对账恢复，这和现在的约定一致。

### 8.4 阶段 2 的运行时接口（Agent 服务提供，Go 调用）

| 接口 | 用途 |
|---|---|
| `POST /segments` | 启动一个片段，入参和现在 stdin 第一行的 JSON 相同（`start`/`continue`/`resume`） |
| `POST /segments/:run_id/steer` | 插话 |
| `POST /segments/:run_id/abort` | 中止 |
| `GET /segments` | 列出正在跑的片段，Go 据此判断哪些运行已经中断 |
| `GET /health` | 健康检查 |

- **存活判断**：现在靠 `watch()` 等进程退出，阶段 2 改成**租约**。
  - Go 每 30 秒调用一次 `GET /segments` 对账：运行还是 running，但服务里已经没有它的片段，就标为 `interrupted`。
  - Agent 服务重启后列表为空，所以重启时的中断也能被发现。
- **优雅停机**：Agent 服务收到 SIGTERM 后先中止所有片段并报告 `finish`，再退出。Compose 的 `stop_grace_period` 设为 30 秒。

## 9. 部署（已确认）

- **阶段 2**：
  - 新增 `video-canvas-agent` 镜像（`node:22` 加 TS 构建产物），和 backend 用同一套 tag 发版。
  - Compose 新增 `agent` 服务：`restart: unless-stopped`，有健康检查，只用 `expose`。
  - 服务令牌放在 `.env`。
  - backend 镜像删掉 Node 和 `agent/`（`backend/Dockerfile:24-44`）。
- **阶段 3**：
  - 新增命名卷 `agent_data` 存放 SQLite。要用本地卷，不要放在 NFS 这类网络盘上。
  - **`scripts/update.sh` 必须同时备份 `agent_data`，回退时一起恢复。** Pi Durable 的 schema 迁移不可逆：只退回镜像、不恢复数据，旧版本会打不开新版本迁移过的库。
- **单副本**：Pi Durable 没有跨进程锁。以后要扩容，就按会话做粘性路由和分片（每个分片一个 SQLite），现在不做。

## 10. 技能（已确认）

- **管理留在 Go**：导入、版本、启停、存储、审计和 `/admin/agent/skills` 页面都不动。这些依赖 Postgres、存储注册表和后台审计，属于平台能力。
- **Agent 服务里做一个 `video-canvas-skills` 扩展**：
  - 用一个 `section` 把已启用技能的目录放进系统提示词；
  - 提供 `skill_search`/`skill_read` 两个工具，通过平台 API 读取；
  - 运行开始时锁定技能版本，阶段 3 起存进 Pi 的 document。现在锁定的版本只放在内存里。
- Pi Durable 没有技能这个概念。推断：如果 Pi 生态的技能包也是 SKILL.md 格式，可以用 Go 现有的导入流程（`internal/agent/skillpkg`）直接导进来。

## 11. 扩展路线：借鉴 Toonflow（提案）

| 借鉴点 | 做法 | 阶段 |
|---|---|---|
| 画布分页读取 | `canvas_get_state` 增加游标分页、字段投影、长字段分段读，单次响应设上限。现在只能按 `nodeIds`/`groupId` 过滤 | **不依赖拆分**，现在就能做（Go） |
| 提问支持多字段表单 | `ask_user` 一次问多个字段，"跳过"后不停止运行。现在每次只能问一个问题 | 不依赖拆分（Go + 前端） |
| 节点操作目录 | `canvasgraph` 按节点类型提供可用操作和 schema，Agent 按需查询，代替一个巨大的 `canvas_apply_ops` schema。执行仍在 Go，revision 乐观锁不变 | 在阶段 1 设计平台 API 时预留 |
| 工具自带提示词 | `prompts/system.md`（9.8KB，一整块）拆开，每个工具的规则跟着工具走。停用的工具，它的规则也不进提示词 | 阶段 2 改写 TS 时做，阶段 3 落成扩展的 `sections` |
| 子 Agent 和团队 | 基于 Pi Durable 的 child tasks 实现子 Agent。团队用声明式清单（入口成员、可委派对象、工具白名单、技能、知识），例如"导演统筹 + 编剧 + 审阅"。**必须有上限**：步数、预算、并发数、时长 | 阶段 3 之后 |
| 用户记忆 | 每个用户一份记忆，用 Pi 的 document 存储，保存时带 revision。提示词写清楚哪些该记、哪些不该记 | 阶段 3 之后 |

## 12. 测试策略

按 TDD：先写能复现需求的失败测试，再实现。

**阶段 1（Go，`backend/internal/tests/`）**
- `agent/test/run.test.mjs`：每个工具结果出来后，桥上都收到一次 `/state`；在同一回合的两个工具之间模拟崩溃，保存的历史里有第一个工具的结果。
- 写工具幂等（阶段 3 之前完成）：`service/agent/bridge_test.go` 和 `repository/agent_e2e_test.go`：
  - 同一个 `(run_id, tool_call_id)` 的 `canvas_apply_ops` 调两次，画布只改一次，第二次返回相同结果；
  - 两次并发调用，只执行一次；
  - `ask_user`、`generate_media`、`canvas_delete` 重复调用，只建一条审批；
  - 读工具重复调用不受影响。
- `handler/agent/bridge_test.go`：
  - 配置了服务令牌时，缺少或错误的服务令牌返回 401；
  - 没配置时行为不变。
- `config/config_test.go`：
  - 没配置服务令牌时，非回环地址被拒绝；
  - 配置了服务令牌时允许。

**阶段 2**
- `agent/` 改用 TS 后，保留并迁移现有的 `node --test` 用例（run、steer、compaction、context、tools、codemode）。
- Go 侧给 `HTTPRuntime` 写测试，用 `httptest` 模拟 Agent 服务：启动、插话、中止、对账时把运行标为中断、服务重启后能发现中断。
- 异步测试一律用轮询加超时。

**阶段 3**
- 契约测试：假模型加假时钟，覆盖插话、中止、审批暂停与续跑、杀进程后 `resume()`、崩溃熔断。
- 升级演练：用旧版本数据跑新版本 → 回退 → 恢复备份，验证数据完好。

## 13. 风险

| 风险 | 影响 | 应对 |
|---|---|---|
| Pi 发版快，小版本也有破坏性变更 | 升级成本不可控 | 只依赖公开 API；所有包同版本升级；契约测试；不写自定义 Storage |
| SQLite schema 迁移不可逆 | 回退镜像后服务起不来 | 改造升级脚本，备份和恢复 `agent_data` |
| 两套状态（画布在 Go，对话在 Agent 服务） | 崩溃后重复写画布 | 阶段 1 改为每个工具结果后保存；阶段 3 之前写工具按 `tool_call_id` 幂等，这是声明 `replay: "safe"` 的前提（8.2） |
| 审批在 Pi Durable 里的建模方式未经验证 | 阶段 3 可能返工 | 阶段 0 先验证，跑不通就停 |
| 单进程持有存储 | 不能直接横向扩容 | 现阶段单副本；以后按会话分片 |
| pi.dev 上的包未必兼容 Pi Durable | 生态红利低于预期 | 逐个确认，走白名单 |

## 14. 已确认与待确认

### 14.1 已确认（用户回答，2026-10-25）
- 按方案 C 拆分，动机是使用并始终跟上 Pi 生态。
- 阶段顺序：Pi Durable 放到最后，编排逻辑只搬一次（第 7 节）。
- 旧会话不迁移。
- Agent 服务改用 TypeScript。
- 部署：Docker Compose 新增 `agent` 服务，单副本，只在内网（第 9 节）。
- 技能：管理留在 Go，Agent 服务通过扩展使用（第 10 节）。

### 14.2 推荐方案，待用户点头
- 无。

### 14.3 默认值（不需要用户决定）
- 服务间鉴权用共享服务令牌，放在 `.env`。
- 阶段 2 的存活判断用 30 秒一次的对账。
- 第三方扩展默认不安装。

### 14.4 仍待确认
- 阶段 3 的审批建模方式，由阶段 0 验证后决定。
- 第 11 节扩展路线的优先级和排期。

## 15. 来源与假设

- npm：`@earendil-works/pi-durable` 1.4.0 的元数据、README 和 CHANGELOG（2026-10-25 下载到本地阅读，未安装运行）。
- `agent/node_modules/@earendil-works/pi-agent-core` 1.3.0 的 README 和 dist。
- Toonflow：<https://github.com/HBAI-Ltd/Toonflow-app>，提交 `30139c9`（2026-10-21 版本，只读未运行）。
- 外部文章（只看了搜索摘要，正文没能抓取）：[Earendil：Pi Durable](https://earendil.com/posts/pi-durable/)，[Temporal：The immortal life of Pi](https://temporal.io/blog/the-immortal-life-of-pi-running-the-pi-coding-agent-on-temporal)，[Cloudflare Agents：Pi](https://developers.cloudflare.com/agents/harnesses/pi/)，[What Durable Agent Harnesses Actually Guarantee](https://redreamality.com/blog/pi-durable-agent-harness-durable-execution/)。
- 假设：Agent 的规模在单副本承受范围内。目前没有并发运行数的线上数据，阶段 2 上线前用运行观测数据核实。

## 16. 实现记录

### 16.1 阶段 1 已实现（2026-10-25）

**Node（`agent/`）**
- 每个工具结果出来（`message_end`，角色是 toolResult）就把历史交回 Go，回合结束时内容没变就不重复发。崩溃最多丢一个工具的进度，原来最多丢一整个回合。证据：`agent/src/run.mjs` 的 `subscribe`。
- 新增 `closeDanglingToolCalls`：加载历史时，没有结果的工具调用补一条中文的「结果丢失」，提醒模型它可能已经执行、先确认现状。被出错或中止截断的 assistant 不补。
- 启动参数里有 `service_token` 就带上请求头 `X-Agent-Service-Token`，桥请求和模型请求（含摘要压缩）都带。

**Go（`backend/`）**
- 配置 `agent.service_token`（环境变量 `APP_AGENT_SERVICE_TOKEN`，至少 32 个字符）。配了才允许 `bridge_addr` 是非回环地址；令牌太短、或非回环地址没配令牌，启动时报错，报错里不带令牌内容。
- 新增中间件 `RequireServiceToken`：常量时间比较，只回同一个 401，令牌为空时拒绝所有请求。`router.NewBridge` 增加参数 `serviceToken`，不为空时在业务之前校验。
- `initialize.ListenBridge` 在配置校验之外再拦一道；`BridgeLocalURL` 让监听 `0.0.0.0` 时本机 Node 仍连 `127.0.0.1`。
- `RuntimeConfig.ServiceToken` 把令牌交给 Node。

### 16.2 与设计的出入

- **pi-ai 已经会给没有结果的调用补一句英文 `No result provided`**，所以历史不会被接口拒绝；补中文提示的目的是让模型知道「它可能已经执行」，不是为了通过接口校验。
- **服务令牌必须放在认证结果的 `headers` 里**（`auth.resolve` 返回），放在 provider 的 `headers` 上 pi 不会用。
- 8.3 的事件推送入口和 8.1 的「上下文头模式」**没有做**：它们阶段 3 才用，阶段 1 只定义了格式。
- 写工具按 `tool_call_id` 幂等没有放在阶段 1（见 8.2 的修正）。

### 16.3 验证

- `agent/`：`npm test`，87 个用例全部通过（原 81 个，新增 6 个）。
- `backend/`：`go vet ./...` 无输出；`go test ./...` 全部通过（用了本机测试库，含启动真实 Node 子进程的端到端用例）；`golangci-lint fmt --diff` 和 `golangci-lint run --new-from-merge-base=master` 均为 0 问题。

### 16.4 遗留

- Compose 里还没有 `agent` 服务，没有改 `docker-compose.yml`，那是阶段 2。
- 每个工具结果都保存一次历史，历史大时（最大 8MB）请求变多。上线前看一下运行观测里工具调用密集的运行，必要时加一个很短的合并窗口。
