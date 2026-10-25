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

**阶段 0（验证，放在阶段 3 之前做；2026-10-25 已完成，结论见第 17 节）**：用 Pi Durable 1.4 跑通一个最小服务：SQLite 存储，一个调用 Go `/tool` 的工具，覆盖插话、中止、杀进程后 `resume()`。重点验证**"等审批"怎么建模**，README 没有现成例子。候选方案有两个：
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

### 8.2 工具调用幂等（2026-10-25 修正；**最终设计见 19.6：用显式的幂等键，不用模型给的 tool_call_id**）

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

### 8.4 阶段 2 的运行时接口（Agent 服务提供，Go 调用；2026-10-25 按实现修正）

| 接口 | 用途 |
|---|---|
| `POST /segments` | 启动一个片段。请求体是启动参数（和进程模式 stdin 第一行一样），请求头 `X-Segment-Id` 是 Go 生成的片段 id。**响应是一条 NDJSON 流，一直开到片段结束**：先 `{"type":"accepted"}`，运行期间定时 `{"type":"heartbeat"}`，最后 `{"type":"exit","code":0\|1,"stderr_tail":"…"}`。`code` 和进程模式的退出码同义：0 报告了结束，1 没能报告 |
| `POST /segments/:id/steer` | 插话，`{"text":"…"}` |
| `POST /segments/:id/abort` | 中止（用户停止） |
| `GET /segments` | 列出正在跑的片段，排障用 |
| `GET /health` | 健康检查，不要令牌 |

除健康检查外都要带 `X-Agent-Service-Token`。错误响应都是 `{"code","msg"}`：401 令牌无效、400 请求不合法、404 没有这个片段、409 片段 id 重复、503 并发满了或正在停机。

- **存活判断：连接就是租约，不再轮询对账。** 最初设计是 Go 每 30 秒调一次 `GET /segments` 对账。实现时发现更简单也更及时的做法：`POST /segments` 的连接一直开着，
  - 服务崩了、被杀、网络断了：流在没有 `exit` 行时结束，Go 立刻拿到异常退出（`Code=-1`），沿用现有的 `watch()` 把运行标为 `interrupted`；
  - 服务假死：超过 `HeartbeatTimeout`（默认 60 秒，服务每 15 秒发一次心跳）没收到任何一行，Go 关掉连接并当作异常退出；
  - Go 退出或强杀（`Kill`）：关掉连接，服务看到连接断开就中止这个片段，不会留下孤儿运行。Go 重启后，遗留的 `running` 运行本来就由启动时的 `InterruptActiveRuns` 标为中断。
- **连接断开和服务停机都不向桥报告结束。** 这是实现时发现的必须做对的一点：桥的 `Finish` 收到 `done` 会把运行标成「成功」。进程模式被 SIGKILL 时什么都不报，运行才会是「已中断」，用户可以继续。所以服务在片段的主人没了（连接断开）或自己要停机时，中止片段的同时切断它和桥的联系（后续的桥请求直接失败），以 `exit` `code=1` 结束。只有用户主动停止（`/abort`）才照常报告，因为 Go 在发 abort 之前已经把运行状态设好。
- **优雅停机**：收到 SIGTERM 后放弃所有片段（中止、不报告），等它们收尾，最多 25 秒，再退出；Compose 的 `stop_grace_period` 设为 30 秒。被切断的运行由 Go 标为中断，阶段 3 起由 Pi Durable 自动续跑。
- **进程内并发**：多个片段在同一个 Node 进程里并发（`runAgent` 没有进程级全局状态）。一个片段里抛出的未处理异常只让它自己以 `code=1` 结束；漏掉的 Promise 异常只记日志；同步抛出且没人接住的异常说明进程状态不可信，进程退出由容器重启，Go 那边连接断开会把这些运行标为中断。并发上限 `AGENT_MAX_SEGMENTS`（默认 64）。

## 9. 部署（已确认）

- **阶段 2**：
  - 新增 `video-canvas-agent` 镜像（`node:22` 加 TS 构建产物），和 backend 用同一套 tag 发版。
  - Compose 新增 `agent` 服务：`restart: unless-stopped`，有健康检查，只用 `expose`。
  - 服务令牌放在 `.env`。
  - backend 镜像删掉 Node 和 `agent/`（`backend/Dockerfile:24-44`）。
- **阶段 3**：
  - 新增命名卷 `agent_data` 存放 SQLite。要用本地卷，不要放在 NFS 这类网络盘上。
  - **`scripts/update.sh` 必须同时备份 `agent_data`，回退时一起恢复。** Pi Durable 的 schema 迁移不可逆：只退回镜像、不恢复数据，旧版本会打不开新版本迁移过的库。
- **实现时的修正（2b-3）**：`agent` 服务放在 **compose profile `agent`** 里，默认不启动，用 `.env` 的 `COMPOSE_PROFILES=agent` 启用，而不是上面写的"直接加进默认服务"。原因：（1）现有部署升级后不能因为多了一个必填的令牌就起不来；（2）用户要明确选择 `APP_AGENT_RUNTIME=http` 才切换，随时能退回 process；（3）backend 镜像先保留 Node，等 http 模式在线上稳定后再去掉，两步合并才是"默认拆分"。必须用 `COMPOSE_PROFILES` 而不是 `--profile`：`update.sh` 和一键升级用的 `docker compose pull/up` 不带 `--profile`，只有前者才会让 agent 跟着升级，否则升级后 backend 和 agent 版本错位。
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
- Go 侧给 `HTTPLauncher` 写测试，用 `httptest` 模拟 Agent 服务：启动、被拒绝、插话、中止、断开、心跳超时、Kill。Go 的端到端用例每个都在 process 和 http 两种模式下各跑一遍（http 模式起真实的 Node 服务）。
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
- 阶段 3 的范围：**方案二**（2026-10-25，用户选择）：会话、运行、审批、事件全部迁进 Agent 服务。实施计划见第 19 节。
- 插话：**用官方效果**（2026-10-25）。只在工具执行的间隙放入；模型正在写回答时不打断，写完后再处理。阶段 3 里 `steer.ts` 和 `settle` 里的相关逻辑直接删掉，不补任何代码。代价见 17.2。

### 14.2 推荐方案，待用户点头
- 无。

### 14.3 默认值（不需要用户决定）
- 服务间鉴权用共享服务令牌，放在 `.env`。
- 阶段 2 的存活判断靠 `POST /segments` 的长连接和心跳，不轮询（见 8.4）。
- 第三方扩展默认不安装。

### 14.4 仍待确认
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

### 16.5 阶段 2a：Pi 升级到 1.4.0（2026-10-25，单独提交）

按「先升级、验证无回归，再改写 TS」分开做，出问题时好定位。

- `agent/package.json`：`pi-agent-core`、`pi-ai`、`pi-codemode` 从 1.3.0 升到 1.4.0（精确版本，`typebox` 仍是 1.3.27）。`package-lock.json` 里 21 个包跟着变。
- 读了 1.4.0 的发布说明：对这三个包只有修复和新增，没有破坏性变更。和我们相关的有：摘要不再有固定输出上限（我们用自己的压缩，不受影响）；空白字符的工具输出不再让 OpenAI 兼容接口返回 400；输出被截断后恢复时不再留下孤立的工具结果。
- 没有改任何源码。验证：`agent/` 的 `npm test` 87 个全过；在临时目录用锁文件 `npm ci` 干净通过（Dockerfile 用的就是 `npm ci --omit=dev`）；Go 的端到端用例加 `-count=1` 强制重跑（`go test` 的缓存不会感知 `node_modules` 变化），建画布并计费、审批后续跑、流式中途取消、杀进程后续跑全部通过。
- 注意：`pi-durable` 1.4.0 要求 `pi-ai` 也是 1.4.0，这一步已经满足，阶段 3 接入时不再需要同步升级。

### 16.6 阶段 2b-1：Agent 运行时改写成 TypeScript（2026-10-25，单独提交）

行为不变，只改写语言、补类型检查和构建。选型：Node 22 运行，`tsc` 构建，依赖仍用 npm（见对话中对 Bun 的评估：Pi Durable 的官方 SQLite 存储用 `node:sqlite`，本机 Bun 1.2.8 没有它，Bun 当运行时要自己写适配）。

- **文件**：`agent/src/*.mjs` 和 `agent/test/*.mjs` 全部改成 `.ts`（`git mv`，保留历史）。新增 `src/types.ts`，放和 Go 通信的数据结构（启动参数、工具声明、工具结果、工具注册表）。
- **工具链**：`typescript` 7.0.2、`@types/node` 22.20.5（精确版本，devDependencies）。`tsconfig.json` 查源码和测试（`strict`、`erasableSyntaxOnly`、`verbatimModuleSyntax`），`tsconfig.build.json` 只编译 `src` 到 `dist/`。导入写 `.ts` 后缀，构建时自动改成 `.js`（`rewriteRelativeImportExtensions`）。
- **脚本**：`npm run typecheck`、`npm test`（Node 类型剥离直接跑 `.ts`，不用先编译）、`npm run build`。
- **入口选择**（`initialize.agentEntryScript`）：目录里有 `src/main.ts` 就用源码，否则用 `dist/main.js`。开发时不用构建，改了 `src` 也不会被旧的 `dist` 盖住；部署镜像里只有 `dist/`。入口是 `.ts` 时给 node 加 `--disable-warning=ExperimentalWarning`，免得每次运行的 stderr 里都有一行「类型剥离是实验特性」，而 stderr 尾部会存进诊断记录。
- **Docker**：`backend/Dockerfile` 新增 `agent-build` 阶段（`npm ci` 含开发依赖，`npm run build`）；最终镜像只带 `dist/` 和生产依赖，不带 `src`、`tsc`。`agent/dist` 加进 `.gitignore` 和 `Dockerfile.dockerignore`。
- **CI**：新增 `agent` 任务（`npm ci`、`typecheck`、`test`、`build`）。之前 CI 没有跑 Node 侧的任何测试。
- **类型上的取舍**：`Msg`/`Part` 是只列出用到字段的宽松形状，pi 的消息能直接赋给它，函数用泛型保持原类型；少数地方用显式转换并写了原因（Go 下发的是普通 JSON Schema 不是 typebox、`renderDeclarations` 只渲染声明不执行）。测试里收到的请求体用 `any`。
- **验证**：`npm run typecheck` 零错误；`npm test` 87 个全过；`npm run build` 产物可用：用假桥分别以 `dist/main.js` 和 `src/main.ts` 拉起真实子进程跑完整的一次运行（工具往返、保存历史、带服务令牌、报告完成）都通过；`docker build` 完整镜像成功，镜像里 `dist/` 在、`src` 和 `tsc` 不在，`node /app/agent/dist/run.js` 能导入全部依赖；Go 的 `go vet`、`go test -count=1 ./...`（含 4 个跑真实 Node 子进程的端到端用例，现在跑的是 `src/main.ts`）、`golangci-lint` 全部通过。
- **没做**：TS 7 目前的 `tsc` 是原生实现，没有 `tsserver` 之外的编程接口；如果以后需要程序化调用编译器，要另评估。

### 16.7 阶段 2b-2：Agent 作为常驻 HTTP 服务（2026-10-25，单独提交）

选型：服务内部**在同一个进程里并发跑多个片段**（`runAgent` 没有进程级全局状态，已确认）；Go 侧不新写运行时，而是新增 `HTTPLauncher` 实现现有的 `Launcher` 接口，`ProcessRuntime` 的令牌、状态机、`watch()`、取消宽限期全部原样复用，`agent.runtime` 在 `process`（默认）和 `http` 之间切换。

**Agent 服务（`agent/`）**
- `src/server.ts`：`createAgentServer`，协议见 8.4。用 Node 自带的 `http`，没有新增依赖。服务令牌常量时间比较，少于 32 个字符拒绝创建；并发上限；片段 id 只允许 `[A-Za-z0-9_-]{1,64}`。
- `src/serve.ts`：入口，用环境变量配置（`AGENT_SERVICE_TOKEN` 必填、`AGENT_LISTEN` 默认 `0.0.0.0:8090`、`AGENT_MAX_SEGMENTS`、`AGENT_HEARTBEAT_MS`），启动前自检并说清楚原因，日志里不出现令牌；SIGTERM / SIGINT 优雅停机；进程级异常处理见 8.4。
- 进程模式的入口 `main.ts` 原样保留。

**Go（`backend/`）**
- `service/agent/http_launcher.go`：`HTTPLauncher`。第一次 `Send` 是启动请求，等到响应头回来才返回，所以被拒绝（401、409、503）或连不上都是启动失败；之后的 `Send` 是插话和中止；`Kill` 关掉连接；`Done` 在 `exit` 行、断开或心跳超时时触发。不走代理环境变量，连接超时 5 秒；片段不随发起启动的请求取消。
- 配置：`agent.runtime`、`agent.url`、`agent.bridge_url`（环境变量 `APP_AGENT_RUNTIME`、`APP_AGENT_URL`、`APP_AGENT_BRIDGE_URL`）。`http` 模式必须同时配 `url`、`bridge_url` 和 `service_token`，格式都在启动时校验；该模式下不再要求本机有 Node 和入口脚本。
- `initialize.AgentLauncher` 选启动器，`initialize.BridgeURLFor` 决定 Agent 访问桥的地址（http 模式用配置的对外地址，process 模式用本机监听地址）。

**与设计的出入**
- 存活判断从「Go 每 30 秒对账」改成「长连接 + 心跳」，见 8.4。
- 连接断开和停机不向桥报告结束，见 8.4。这是写服务测试时发现的：最初的测试断言停机时「每个运行都向桥报告了结束」，仔细想才发现这会让被切断的运行变成「成功」。测试已按正确语义改写。
- 不需要新增 `HTTPRuntime` 类型。

**验证**
- `agent/`：`npm run typecheck` 零错误；`npm test` 104 个全过（原 87，新增服务 13 个、入口 4 个）；构建产物 `dist/serve.js` 在 Alpine 容器里启动正常，健康检查通过、无令牌 401、收到停止信号 0 秒内以退出码 0 结束。
- `backend/`：`go vet ./...` 无输出；`go test -count=1 -race ./...` 全过；4 个端到端用例（建画布并计费、审批后续跑、流式中途取消仍按已产生的用量计费、杀掉后续跑）**在 process 和 http 两种模式下各跑一遍**，http 模式起真实的 Node 服务；`golangci-lint` 0 问题。
- 没做：Compose 里还没有 `agent` 服务、backend 镜像还带着 Node，这是 2b-3。

### 16.8 阶段 2b-3：Agent 镜像与 Compose 服务（2026-10-25，单独提交）

- **镜像**：`agent/Dockerfile`，构建上下文只是 `agent/` 目录，可以脱离整个仓库单独构建。结构和 backend 镜像一致（Alpine 加官方镜像里的 node 二进制，运行镜像里没有 npm，非 root 用户，`HEALTHCHECK`），209MB。
- **Compose**：生产和开发的 compose 都新增 `agent` 服务（profile `agent`）。生产版：单副本、只 `expose` 不发布端口、`stop_grace_period: 30s`、内存上限 2g、健康检查。backend 增加 `APP_AGENT_RUNTIME`（默认 `process`）、`APP_AGENT_URL`、`APP_AGENT_BRIDGE_URL`、`APP_AGENT_SERVICE_TOKEN`。令牌用 `AGENT_SERVICE_TOKEN` 一个变量同时给两边。
- **默认配置**：`runtime=http` 时 `bridge_addr` 默认 `0.0.0.0:47001`（`process` 仍是本机随机端口）。默认的 `config.yaml` 里不能再写死 `bridge_addr`，否则会盖住这个默认值。
- **发布**：`release-docker.yml` 多发一个 `video-canvas-agent` 镜像，和 backend、web 同一个版本号；发布前的质量检查也跑 agent 的类型检查和测试。
- **日志**：backend 启动日志「画布 Agent 桥已启动」带上 `runtime` 字段，运维一眼能看出现在是哪种模式。
- **backend 镜像先保留 Node**（已确认）：http 模式下它不用，但回到 `process` 模式要用。等 http 模式在线上稳定后再单独去掉。

**整栈验证中发现并修好的 bug**：第一次起整栈时，backend 日志里桥监听的是 `127.0.0.1:36225`，不是 `0.0.0.0:47001`，agent 容器连不上。原因是默认 `config.yaml` 里显式写了 `bridge_addr: 127.0.0.1:0`，盖住了「http 模式默认监听 47001」。单元测试看不出来，因为它们直接构造 `Agent{}`，没有经过默认配置文件。已补一个从默认配置文件加载的测试，并把 `config.yaml` 里的默认值改成注释。

**验证**
- 用生产 compose、本地构建的两个镜像，起 postgres、redis、plugin-runner、backend、agent（`COMPOSE_PROFILES=agent`、`APP_AGENT_RUNTIME=http`）：全部 healthy；桥监听 `[::]:47001`，日志里 `runtime=http`；agent 容器访问桥：不带服务令牌 401，带了服务令牌后按运行令牌校验（假令牌 401）；backend 容器访问 agent：带令牌 `GET /segments` 返回空列表，不带 401，`/health` 不要令牌；宿主机没有发布任何端口；`docker compose stop agent` 1 秒、退出码 0，backend 不受影响，`start` 后恢复 healthy。
- 不带 `COMPOSE_PROFILES` 时 `docker compose config --services` 里没有 `agent`；带上后 `update.sh` 用的 `docker compose pull/up` 会处理它。
- Go 端到端用例现在在 `process`、`process-token`（配了服务令牌但仍是 process，`.env` 里只设了令牌没切到 http 时就是这样）、`http` 三种方式下各跑一遍。
- **没做**：没有跑通「真实用户发起一次完整的 Agent 对话」。那需要后台配好渠道和模型，整栈验证只确认了网络、令牌、监听和停机。端到端逻辑由上面的 Go 端到端用例（http 模式起真实 Node 服务）覆盖。

## 17. 阶段 0 验证报告（2026-10-25）

用 Pi Durable 1.4.0 的假模型（`faux`）在临时目录里做了 6 个实验，脚本和锁文件留在 `阶段0验证/`（说明见该目录的 README），从干净安装重新跑通过。**三个待验证的问题都有了答案，阶段 3 可以进行。**

### 17.1 审批怎么建模：可行

**做法**：需要用户决定的工具（`canvas_delete`、`generate_media`、`ask_user`）在 Go 里创建审批后，返回 `control: { terminate: true }` 和一句占位文字。这一轮正常结束。用户决定到来时：

1. 提交一条写入，条目带 `edits: [{ target: <占位工具结果的条目 id>, action: "replace", messages: [真实结果] }]`；
2. 再提交一条输入（例如「用户已处理上面的确认，请继续」）让模型接着跑。

**实测（exp1）**：等审批期间模型没有被多请求（1 次）；决定后模型第 2 次请求看到的是 `[用户请求, 工具调用, 真实的工具结果, 继续]`，占位结果已被替换。第一次提交的状态是 `done`（没有真正的回答，`terminate` 就是这样结束的），所以「等审批」状态必须由我们自己的审批记录表示，不能靠提交状态判断。

**和现在的差别**：现在审批后用 `continue` 模式，历史停在工具结果上直接续跑，不加用户消息。新做法多一条「请继续」的用户消息，模型多读一句话，不影响结果。

**没选的候选**：自定义 task 加 durable sleep 轮询决定。能做，但要自己管轮询间隔，且审批本来就是人的异步决定，用对话本身的状态表示更自然。

### 17.2 插话：用官方效果（已决定），代价如下

**官方行为**（`pi-agent-core` 文档、Pi Durable 规范、实测 exp3-A 三处一致）：
- 工具执行时到达的插话：等这一批工具全部跑完，注入，模型在下一轮回应。这是设计的主场景。
- 模型**正在写回答**时到达的插话：不打断，回答写完后作为下一轮的输入再触发一次回答。

**代价**：模型正在输出时插话，用户会先看到一个按旧要求写完的完整回答，然后才是结合插话的第二个回答，**多一次模型调用的费用**。现在的 `steer.ts` 用 500ms 窗口加中止重答避免这一点，选官方效果就放弃它。

**留作备选的做法**（exp3-B 已验证，不实现）：`abort()` 当前回答再把补充作为新输入提交。半截回答不会进历史，模型看到 `[原请求, 补充]`，和现在一致，`abort` 耗时 5ms。如果以后用户反馈两段回答体验不好，可以只对「模型正在输出」这一种情况加上它；注意工具执行时不能 `abort`（会中断工具，不可重放的工具会留下半截画布改动），必须先用 `LiveDoc` 确认没有工具在跑，读状态和中止之间有几毫秒的窗口，到时需要专门测。

### 17.3 压缩：可行，摘要内容完全由我们决定

**实测（exp4、exp5）**：
- `beforeCompact` 钩子在**自动压缩**（`reason=threshold`）和**手动压缩**（`reason=manual`，带 `instructions`）两条路径上都会被调用。
- 钩子拿到要被替换的全部消息，返回 `{ summary }` 就用我们的摘要；钩子里可以用 `api.models` 调模型（exp5），所以现在 `compaction.ts` 里画布专用的提示词和格式（目标/要求/已完成/进行中/不能丢的数据）可以原样搬进钩子。
- **增量更新可行**：第二次压缩时，`entries[0]` 是上一份摘要（`pi.compaction`），`messages[0]` 是它的内容，钩子可以把它并入新摘要，和现在的「旧摘要作为输入」一致。
- 钩子里的模型调用走的仍是桥，所以仍由 Go 按调用计费，不用另做。

**两个差别**：
- 摘要进入上下文时 Pi 会套一层英文说明：`The conversation history before this point was compacted into the following summary:\n\n<summary>…</summary>`，不能改。现在的前缀是中文「【对话摘要】」。模型读英文说明加中文摘要没有问题，但**摘要的识别方式要变**（现在靠前缀认出旧摘要，之后靠 `entries[0]` 的类型）。
- 触发线由 Pi 的设置决定（`reserveTokens`、`keepRecentTokens`，可按模型覆盖），现在的 `compactionLimits` 可以直接换算成这些设置。

### 17.4 崩溃恢复：核心价值得到验证

**实测（exp2）**：工具执行中 `SIGKILL`，用 SQLite 重开后 `resume()`，不需要任何人点「继续」：

| 工具声明 | 重开后 | 模型看到 |
|---|---|---|
| `replay: "never"` | 不重跑（总执行 1 次） | `Tool … was interrupted and may …`（一条错误结果，说明可能已经执行） |
| `replay: "safe"` | 用同一个调用 id 重跑（总执行 2 次） | 重跑后的真实结果 |

这正好是第 8.2 节规划的：读工具声明 `safe`；写工具在 Go 按 `tool_call_id` 幂等之前声明 `never`，让模型收到「可能已经执行」后自己确认现状，幂等做完后再改成 `safe`。阶段 1 加的「结果丢失」提示和这条语义一致，阶段 3 起由 Pi 自己产生。

### 17.5 一个 Harness 托管很多会话：可行

**实测（exp6）**：一个 Harness、一个 SQLite 文件，200 个会话 × 3 轮同时提问，全部完成，**没有串内容**（每个会话看到的都是自己的消息），总耗时 855ms，进程内存 156MB，SQLite 文件 1.4MB。模型是假的，所以这只说明存储和调度本身不是瓶颈，不代表真实负载；上线前仍要用运行观测的并发数核实（见 13、15 节）。

### 17.6 没有验证的

- **真实模型**：全部用 `faux`，没有验证流式输出、真实的上下文溢出恢复、真实的 token 估算。
- **和 Go 平台 API 的集成**：工具里调 Go、幂等键、事件推送都留到阶段 3 的实现里。
- **多进程**：Pi Durable 一个存储只能被一个进程持有（文档明确写了），没有试，也不打算支持。
- **升级演练**：1.4.0 的 SQLite schema 升级不可逆，升级前备份的流程要在阶段 3 做 `update.sh` 时验证。
- **事件流**：`watchEvents` 能不能直接映射成现有的 `agent_events`（`message.*`、`tool.start/end`、`approval.*`…）没有试，这是阶段 3 前期要做的第一件事。

## 18. 阶段 3 的事件映射结论（2026-10-25）

阶段 3 的第一步是确认「前端完全不用改」能不能成立：现在前端靠 `agent_events` 的 12 类事件渲染，Pi 的 `watchEvents` 能不能映射成它们。结论是**能映射，但没必要映射：这些事件现在就不是 Agent 进程发的，而是 Go 发的，阶段 3 之后仍然该由 Go 发。**

### 18.1 现状：12 类事件全部由 Go 发出

逐个核对了发出位置（`backend/internal/service/agent/`）：

| 事件 | 落库 | 谁发、在什么时候 | 里面有什么只有 Go 知道的 |
|---|---|---|---|
| `message.user` | 是 | `run.go`：用户发起运行、插话时 | 用户选中的节点 `selection`、是否插话 `steer` |
| `message.delta` / `thinking.delta` | 否（临时） | `bridge.go`：模型代理转发上游流式输出，50ms 合并一次 | — |
| `message.done` | 是 | `bridge.go`：一次模型调用结束 | 本次调用扣的积分 `credits`、是否估算 `estimated`、是否被截断 `partial` |
| `tool.start` / `tool.end` | 是 | `bridge_tools.go`：Go 执行一次工具；Agent 自己的工具（codemode）经 `/tool-event` 报备后由 `bridge_agent_tool.go` 发 | 一行摘要 `summary`、被改动的节点 `node_ids`、撤销依据的改动记录 |
| `plan.updated` | 是 | `bridge_tools.go`：`plan_update` 工具 | — |
| `approval.created` / `approval.decided` | 是 | `approval.go` | 预估积分、批准后的执行结果 |
| `run.status` | 是 | 11 处：`run.go`、`bridge.go`（开始、结束、预算用尽、步数用尽）、`approval.go`、`runtime.go`（中断） | 预算、步数、状态机的 CAS |
| `run.usage` | 是 | `agent.go`：每次计费后 | 已花积分、上下文大小 |
| `feedback.*`、`run.undone` | 是 | `observe.go`、`run.go` | 用户评价、撤销 |

这些事件有一个共同点：**都是 Go 在处理一次平台调用时顺手发的**（模型代理、工具执行、用户的 HTTP 请求、审批决定、进程结束）。阶段 3 之后模型和工具仍然走 Go 的平台 API，所以 Go 仍然会看到每一次这样的调用。

### 18.2 Pi 的事件能给什么（实测 exp7）

一次「提问 → 模型说话并调工具 → 工具返回 → 回答」，`watchEvents` 给出：`message_start/end`（用户条目、`pi.system`、助手、工具结果）、`submission`、`run_start/end`、`turn_start/end`、`tool_execution_start/update/end`（带调用 id、工具名、参数、输出增量）、`usage_changed`。流式输出时还有 `message_update`（`text_delta`、`thinking_delta`）。

逐项对照：

| 平台事件 | Pi 能给 | 缺什么 |
|---|---|---|
| `message.user` | 自定义用户条目 `app.user` 的 `message_end`（实测 `data` 能带 `selection`、`steer`） | 够 |
| `message.delta` / `thinking.delta` | `message_update` 的 `text_delta` / `thinking_delta` | 够 |
| `message.done` | 助手的 `message_end` | **`credits`、`estimated`、`partial`** |
| `tool.start` / `tool.end` | `tool_execution_start/end`（id、名字、参数） | **`summary`、`node_ids`、改动记录**（得靠工具结果的 `details` 带回） |
| `plan.updated` | 无（是 Go 的工具） | 全部 |
| `approval.*` | 无 | 全部 |
| `run.status` | `run_start/end`、`submission` 大致对应 running、succeeded | 预算用尽、步数用尽、等审批、中断这些状态 |
| `run.usage` | `usage_changed` 是 token，不是积分 | 积分 |

Pi 的事件还带很多前端不需要的噪音（`pi.system`、`turn_start/end`、`inbox_update`…），映射时要过滤。

### 18.3 决定：事件继续由 Go 发（推荐），不做映射层

如果让 Agent 服务映射 Pi 的事件再推给 Go：
- 积分、节点改动、审批这些字段 Agent 服务拿不到，要么让 Go 再补发一遍（同一件事两处各发一半），要么让工具结果把 Go 的数据绕一圈带回来；
- Go 现在发的事件还得去掉，否则前端会收到重复的 `tool.start`、`message.done`；
- 要新做一套事件的幂等键和重发机制。

而如果事件继续由 Go 发：**前端、后台运行记录页、观测导出、`agent-trace` 全都不用动，`agent_events` 的序号（`seq`）也仍由 Go 分配**。第 8.3 节规划的「事件推送入口」（`POST /internal/platform/events`）在阶段 3 里不再需要。

### 18.4 这个结论暴露的真正问题：重放会让事件重复

Pi 在崩溃后会用**同一个 `toolCallId`** 重跑 `replay: "safe"` 的工具（实测 exp2）。Go 的 `executeTool` 每次调用都无条件发 `tool.start`，`recordToolStart` 虽然对重复的 `(run_id, tool_call_id)` 不覆盖，但**事件照发**。前端按 `tool.start` 的 id 建一行，重复的 `tool.start` 会多出一行并造成重复的 key。

这和第 8.2 节要做的「写工具按 `(run_id, tool_call_id)` 幂等」是同一件事，应当一起解决：Go 收到一个已经完成过的 `(run_id, tool_call_id)` 时，**不再执行、不再发事件，直接返回第一次的结果**。依据不能是 `agent_tool_calls`（它是观测数据，写失败只记日志、结果会被截断和清理），要在真正产生副作用的表上加唯一约束（8.2 已写）。

### 18.5 阶段 3 要做的事因此变小，但要定一个范围

| | 方案一：只迁「对话状态与恢复」（推荐） | 方案二：会话、运行、审批、事件全迁（原计划） |
|---|---|---|
| Go 里保留 | 会话、运行、审批、事件、`agent_events`、观测、技能，约 5500 行 | 只剩平台能力 |
| 搬进 Agent 服务 | 对话历史（取代 `session_jsonl` 和 `/state`）、续跑、压缩、插话 | 以上全部，外加运行状态机、审批、事件日志和回放接口 |
| 要新做的 | 重放幂等（8.2）、`interrupted` 与自动续跑的衔接 | 以上，外加事件日志（序号、回放、保留策略）、审批执行经平台 API、观测导出迁移、后台运行记录页改读新服务 |
| 前端和后台 | **不改** | 改读新服务，或由 Go 反向代理 |
| 风险 | 小，每步可单独上线和回退 | 大，阶段 3 和 4 合并成一次 |
| 以后做子 Agent、团队 | 子运行要在 Go 里建模（会话、运行、事件都要支持父子关系），Go 要动 | Agent 服务内部完成，Go 不动 |

方案一是方案二的子集：以后需要时再把运行和事件搬过去，已经做的不浪费。

### 18.6 方案一里仍要解决的一个衔接问题

现在「Agent 服务断开 = Go 把运行标为 `interrupted`，等用户点继续」（长连接当租约）。接了 Pi Durable 之后，Agent 服务重启时 Pi 会**自动续跑**没跑完的运行，这和「Go 已经把它标成中断」冲突：桥令牌已经被收回，续跑出来的模型调用和工具调用会被 Go 拒绝。需要把「中断」改成两段：先是「恢复中」（Agent 服务重启后重新向 Go 登记这个运行，换新令牌），超过一个期限仍没有恢复才标为 `interrupted`。这是阶段 3 实现时最先要设计和测试的一块。

## 19. 阶段 3 实施计划：方案二（2026-10-25）

用户选择方案二：会话、运行、审批、事件日志都归 Agent 服务，Go 只保留平台能力。第 18 节已说明这比方案一大得多，这里按「每步可单独上线、旧路径一直保留、用开关切换」来拆，直到最后一步才删旧代码。

### 19.1 目标形态

| 归 Agent 服务（TypeScript，Pi Durable） | 留在 Go |
|---|---|
| 会话、运行（状态机）、审批、事件日志（序号、回放、历史）、对话历史、续跑、压缩、插话 | 用户鉴权，对 `/agent/*` 反向代理并带上用户身份 |
| `/agent/*` 的 REST 接口（和现在一模一样的 DTO） | 平台 API：模型代理与按调用计费、工具执行（画布读写、生成、技能、联网）、素材 |
| 审批的决定与续跑（批准后的生成、删除通过平台 API 执行） | 画布写入与撤销依据（`agent_mutations`）、生成任务、积分账本 |
| 事件向 Go 推送，由 Go 经 WebSocket 发给前端 | WebSocket Hub、观测读模型、技能管理、旧会话的只读接口 |

### 19.2 要先定的四件事（含推荐）

**D1：ID 怎么不撞号。** 对外 ID 是 Postgres 自增主键经 AES 编码成十六进制串（`idcodec`）。Agent 服务自己生成的运行 ID 会和旧运行的 ID 重叠，而 `agent_mutations`、`agent_model_calls`、`agent_run_digests`、`agent_feedback` 都按 `run_id` 关联。推荐：**Agent 服务的所有 ID 从一个保留区间开始**（例如 2^40 以上），永远不和 Postgres 自增 ID 重叠；`idcodec` 对任意 uint64 都能编码，前端无感。

**D2：运行的预算、步数、积分记在哪。** 积分是平台的钱，必须在 Go 里原子扣减，不能让 Agent 服务说了算。推荐：Go 新建一张很小的**运行账本**表（`agent_run_ledger`：`run_id`、`user_id`、`canvas_id`、`max_steps`、`max_credits`、`steps`、`spent`），Agent 服务开始运行时登记，之后每次模型调用和工具调用由 Go 按 `run_id` 校验和累加。这是**新增表，不改任何现有表**，所以没有存量数据冲突。

**D3：观测怎么办。** 后台运行记录页、`agent-trace`、`/agent-review` 都读 Go 的 `agent_events`、`agent_tool_calls`、摘要等。推荐：Agent 服务每产生一个事件就推给 Go（本来就要推，用于 WebSocket），**Go 同时按 `(session_id, seq)` 幂等地存一份副本**作为观测读模型。这样阶段 3 里观测、后台、`agent-trace` 全部不用动；事件日志的权威仍是 Agent 服务（回放接口由它提供）。代价是事件存了两份。把观测彻底迁走留到阶段 4。

**D4：旧会话。** 之前已定「旧会话不管，只读」。推荐：Go 保留旧会话的**只读**接口（会话列表、事件、历史）；`/agent/*` 的反向代理按会话 ID 所属的区间（见 D1）决定转给 Go 还是 Agent 服务；画布上的会话列表把两边合并。旧会话不能再继续对话。

### 19.3 分步计划

每一步都有退出条件，没通过不进下一步。`agent.runtime` 的取值扩展为 `process`、`http`、`durable`；`durable` 之外的两种在阶段 3 里一直保留，随时可以退回。

| 步骤 | 内容 | 退出条件 |
|---|---|---|
| **3.0 Go 平台 API 的上下文模式** | 平台 API 除现有的「运行令牌」外，支持「服务令牌 + 上下文头」（用户、会话、运行、画布、模型、模式、选中节点）。新增运行账本表（D2）；新增**工具调用幂等表**（`(run_id, tool_call_id)` 唯一，存结果，重放时直接返回且不再发事件）。都是新增表。保留现有路径 | 现有行为不变；幂等和账本有完整的 Go 测试，含并发 |
| **3.1 Agent 服务的数据层** | Agent 服务用 SQLite 存会话、运行、审批、事件、ID 分配器（D1），和 Pi Durable 共用一个文件（Pi 的表加前缀，应用表同库，同一个事务）；自己的迁移机制 | 重启后数据完好；事件序号单调且无空洞；备份恢复演练通过 |
| **3.2 对话引擎** | 在 Agent 服务里用 Pi Durable 实现运行：画布、生成、技能、联网各一个扩展（工具经平台 API 调用 Go）；模型请求经 Go 的模型代理；写工具声明 `replay: "never"`，读工具 `"safe"`；压缩用 `beforeCompact` 钩子（第 17.3 节）；插话用官方效果 | 用假 Go 跑通契约测试：工具往返、审批暂停与续跑、崩溃后自动续跑、压缩、并发会话 |
| **3.3 运行生命周期与 REST 接口** | 实现现有的 12 个运行相关接口（开始、插话、取消、继续、撤销、审批决定、读事件、读历史、评价…）和 12 类事件，字段和现在一致；预算用尽、步数用尽、等审批、已中断等状态；「恢复中」状态（第 18.6 节） | **黄金文件对比**：用现在的 Go 实现录下每个接口和每类事件的 JSON，Agent 服务对同样的输入输出必须一致 |
| **3.4 Go 反向代理与路由** | `/api/v1/agent/*` 在 Go 里鉴权后转给 Agent 服务（带用户身份头）；按 ID 区间决定新旧；会话列表合并；事件推送入口 `POST /internal/platform/events` 写 WebSocket 并存观测副本（D3）。`agent.runtime=durable` 开关 | 前端不改一行，现有的前端测试和 Go 测试全过；用开关能整体退回 `http` 模式 |
| **3.5 观测对接** | 后台运行记录页、`agent-trace`、导出接口在新旧运行上都能用；版本对比指标连续 | 同一份导出脚本能拉到新运行；`/agent-review` 能分析 |
| **3.6 升级与灰度** | `update.sh` 与一键升级备份和恢复 Agent 的数据卷；文档；按用户灰度 | 升级、回退演练通过；灰度用户的运行行为一致 |

阶段 4（清理）：下线旧的 `process`、`http` 两种运行时和 Go 里的旧会话/运行/审批写入代码，保留旧会话只读接口；观测彻底迁走。

### 19.4 规模与风险

- 工作量大约是阶段 1 到 2 总和的好几倍：Go 侧 `service/agent`、`handler/agent` 里的会话、运行、审批逻辑约 3500 行要用 TypeScript 重新实现一遍，加上黄金文件测试。
- 最大的风险是**行为不一致**：运行状态机、审批、预算这些细节很多（例如审批过期、重复点击、取消时已产生的用量仍要计费）。所以 3.3 用黄金文件对比，不靠人工核对。
- 第二个风险是**两处记账**（Go 的运行账本、Agent 的运行状态）可能不一致。约定：钱和步数以 Go 的账本为准，状态以 Agent 服务为准，两者靠 `run_id` 关联，对账任务放在 3.6。
- 每一步都保留退路，但**3.1 以后的数据（SQLite）不可逆地只存在 Agent 服务里**：退回旧运行时后，这些新会话在旧路径下看不到（会变成只读）。

### 19.5 四个决定与计划的调整（2026-10-25，用户回答）

| 项 | 决定 | 对第 19.3 节计划的影响 |
|---|---|---|
| D1 ID 不撞号 | **用雪花算法生成 ID**（用户决定，见 19.8）。取代「保留区间 2^40 以上」的想法 | 3.1 的 ID 分配器改为雪花算法 |
| D2 预算和积分 | **按 token 扣，不新建运行账本表。** 每次模型调用按实际 token 扣积分，仍在 Go（现状，不变）。「本轮额度」上限：Agent 服务开始运行时把上限放进上下文头，Go 每次调用时用该运行在 `agent_model_calls` 里已扣积分的累加值去比。步数由 Agent 服务自己计数 | 3.0 **去掉运行账本表** |
| D3 观测 | **观测数据留在 Agent 服务，Go 需要时向它请求，Go 做中转，不存事件副本。**（用户确认：包括下面的例外） 例外：模型调用的扣费记录和请求体是 Go 代理模型时产生的，只能留在 Go，后台运行详情由 Go 把自己的这部分和 Agent 服务的数据拼起来 | 3.5 改为：Agent 服务提供导出和统计接口；Go 的后台、`agent-trace` 接口改成向它请求再合并；3.4 的事件推送入口只负责推 WebSocket，不再存库 |
| D4 旧会话 | **直接删除，不留只读接口。删除的时机定在阶段 4**（用户确认）（新方式稳定、成为默认之后），在那之前仍可退回旧路径。删除前 `update.sh` 会 `pg_dump` 备份 | 3.4 **不再需要**按 ID 区间分流和合并会话列表；阶段 4 增加一次删除旧 `agent_*` 数据的迁移 |

### 19.6 3.0 的修订：从「画布写入幂等」开始（2026-10-25，已实现，见 20.1）

最先要做、也是后面所有步骤的前提的是**画布写入幂等**。设计过程中改了两次，最终是这样：

1. **幂等键必须由调用方显式给，不能用模型给的 `tool_call_id`。** 一开始想按 `(run_id, tool_call_id)` 去重，写完测试后发现既有的测试辅助函数对每次调用都用同一个 id；顺着查下去，Pi 只在发请求时为个别供应商改写 id 格式，不保证唯一，我们的网关也把上游的 id 原样透传，有些兼容网关每个回答都从 `call_0` 开始编号。同一个运行的不同回合就可能出现相同的 id，按它去重会**悄悄丢掉真实的写入**，比重复写更糟。所以幂等键由调用方显式带上（阶段 3 里是 Pi 的工具任务 id：每次工具调用唯一、同一次调用重放时不变）；不带键的旧路径行为完全不变。
2. **要把当初返回给模型的结果存下来。** 画布写入的结果里有 `tempId → 真实 id` 的映射（模型靠它引用刚建的节点）和写入后的提醒（文字里带着节点 id），以前都没有存。重放只返回「已执行」，模型拿到的 id 就是错的。
3. **去重放在读画布之后。** 如果「查有没有写过」放在「读画布」之前，另一个相同的调用恰好在两者之间提交，就会在新 revision 上再写一遍。放在读之后：要么这里看得到对方的记录，要么提交时因 revision 变了而冲突、重试时再查到。
4. **迁移只新增两列，没有新索引、没有唯一约束。** `agent_mutations` 新增 `idem_key varchar(64) NOT NULL DEFAULT ''` 和 `result_json jsonb NOT NULL DEFAULT '{}'`，旧代码不写它们也能正常插入，所以回退到旧镜像仍能用当前的库。按 `(run_id, idem_key)` 查走现有 `(run_id, seq)` 唯一索引的前缀；不建唯一约束是因为键为空的行很多、存量数据也无法保证，并发靠画布本来就有的乐观锁串行化。
5. **撤销（`kind=undo`）和不带键的写入不去重；去重键带上种类**：同一个键做另一种写入（例如生成后绑定任务）是另一件事。

3.0 剩下的内容（平台 API 的上下文模式、审批拆成「预估」和「执行」两个平台接口）在这之后做。审批要拆开是因为方案二里审批记录属于 Agent 服务，而现在的 `generate_media`、`canvas_delete` 工具在 Go 里同时创建审批和执行：Agent 服务里的工具要先调 Go 的「预估」接口（校验、计价），用户批准后再调「执行」接口（以审批 id 做幂等键，和现在一致）。

### 19.7 模型请求前的钩子（2026-10-25，用户问：调用模型前有钩子吗？能从 Go 拿数据吗？）

有。Pi Durable 的 `GenerationTask` 有 `beforeRequest` 钩子（实测 exp8）：

| | 结果 |
|---|---|
| 什么时候调用 | **每一次**模型请求前，包括工具跑完后的下一次、崩溃恢复后重发的那一次 |
| 能做什么 | 异步，里面可以 `fetch` 向 Go 要数据，把要给模型看的内容加进这一次请求的消息里 |
| 注入的内容会不会进对话记录 | **不会**，只对那一次请求生效，所以每次都能拿最新的 |
| 向 Go 要数据失败 | 异常被上报（`extension_error`）并**跳过**，请求照常发出，只是没有注入 |
| 能不能用它拦住这次调用（比如额度用完） | **不能**。钩子抛异常只会被上报并跳过，请求仍会发出 |

由此得到三点：

1. **额度检查仍然只能放在 Go 的模型代理里**（现状就是这样：`ProxyModel` 在转发上游之前预检余额和本轮额度，不够就返回错误）。钩子拦不住，也不需要，因为所有模型请求本来就经 Go。
2. **可以顺手改进「画布最新状态」的给法。** 现在 Go 只在运行开始时把画布目录拼进第一条用户消息，之后模型要自己调 `canvas_get_state` 才能看到用户中途手动做的改动。用 `beforeRequest` 可以每次请求前向 Go 取一个很短的「自上次以来的变化」注入。这是可选的优化，不影响拆分，先不做。
3. **「看到发给模型的实际内容」已经有了**：Go 的模型代理会把每次请求实际发给上游的完整请求体脱敏后保留 7 天（`agent_model_call_bodies`），拆分后模型调用仍经 Go，这个能力不变。

### 19.8 ID 用雪花算法（2026-10-25，用户决定）

Agent 服务生成的会话、运行、审批、事件 ID 都用雪花算法，而不是「从 2^40 开始的保留区间」：

- **结构**：1 位符号（恒为 0）、41 位毫秒时间戳（自 2026-01-01 UTC 起）、10 位工作节点号、12 位毫秒内序号，即每个节点每毫秒最多 4096 个。总共 63 位，Postgres 的 `bigint` 放得下，`idcodec` 对任意 `uint64` 都能编码。
- **为什么不会和旧 ID 撞号**：旧的 ID 是 Postgres 序列生成的小整数；雪花 ID = 毫秒数 × 2^22，纪元（2026-01-01）之后一天就超过 3×10^14，到 2026 年 10 月已经约 10^17（**已经超过 2^53，JavaScript 的数字装不下，必须用字符串或 bigint**）。两者不可能重叠。
- **唯一性的前提**：同一时刻每个节点的工作节点号不同。现在只有一个 Agent 容器，工作节点号固定（环境变量 `AGENT_WORKER_ID`，默认 1）；以后如果分片，每个分片要用不同的值。
- **时钟回拨**：本进程内保持时间戳不倒退，系统时钟被往回拨时继续沿用上一个时间戳并累加序号，序号用完则等到下一毫秒。
- **顺带的好处**：ID 按时间递增，事件按 ID 排序等于按时间排序。
- **要注意的一件事**：对外的 ID 是经 `idcodec`（AES，密钥是 `APP_SERVER_ID_KEY`）编码的十六进制串。Agent 服务要返回和接收前端认识的 ID，就**必须和 Go 用同一个密钥、同一套编码**（用 `node:crypto` 在 TypeScript 里重写 `idcodec`，约 50 行）。密钥通过环境变量 `AGENT_ID_KEY` 提供，和 `APP_SERVER_ID_KEY` 相同。3.3 的黄金文件对比会直接发现编码不一致。

## 20. 阶段 3 实现记录

### 20.1 3.0a：画布写入幂等（2026-10-25）

实现了第 19.6 节的设计：

- `agent_mutations` 新增 `idem_key`、`result_json` 两列（迁移 `0012_agent_mutation_result.sql`）。
- `AgentCanvasService` 新增 `WithIdempotencyKey(ctx, key)` / `IdempotencyKeyFrom(ctx)`；`mutate` 在读画布之后、应用修改之前查同一个 `(运行, 幂等键, 种类)` 的已有记录，有就用存下来的 `result_json`（`id_map`、提醒）和改动记录还原结果，不再写画布、不再推 `canvas.patch`。`ApplyOps`、`Arrange`、`DeleteApproved`、`BindTasks`、`AddFrames` 都走 `mutate`，一起获得幂等。
- 没有改仓储接口：复用现有的 `ListMutations`。

**测试**（先写测试、确认因预期原因失败，再实现）：
- 重放返回原结果（含 `id_map`、`warnings`、`Created`、`Revision`）且不再推送；重放前用户保存过画布，仍返回当初那次写入；
- **模型的 `tool_call_id` 重复、幂等键不同时两次都写**（不能用模型的 id 去重的原因）；不带键永不去重（旧路径行为不变）；同一个键在另一个运行里互不影响；去重按种类；
- 重放一个已完成的删除不会因为「节点不存在」报错；
- 8 个并发的相同调用只写一次，重复跑 100 遍（`-race`）稳定；
- **真实 Postgres 上**：新列真的存进去并读回来、重放、10 个并发只写一条记录（重复跑 30 遍稳定）。内存替身绕过了序列化，这一项不能只靠它。

**过程中的一次教训**：第一版按模型的 `tool_call_id` 去重，既有的 `TestAgentGenerator_Execute` 因此失败（测试辅助函数对每次调用都用同一个 id，后面的写入被当成重放跳过）。我先用 `git stash` 在干净的 HEAD 上确认它原本是通过的，确认是自己的改动引起的，再顺着查到真实环境里 id 也可能重复，改成了显式幂等键。没有去改既有测试迁就实现。

**验证**：`go vet ./...`、`go test -count=1 -race ./...`（全量）、迁移结构一致性测试、`golangci-lint`、`agent/` 的 typecheck 与 104 个测试全部通过。

### 20.2 3.0b-A：平台 API 的上下文模式与工具执行端点（2026-10-25）

3.0b 切成三个可单独提交的切片：**A** 上下文模式的鉴权和工具执行（本节）；**B** 模型代理的上下文模式和用量查询；**C** 审批拆成「预估」和「执行」。

**盘点结果**：现有工具处理函数对运行的依赖很小。它们只用 `run` 的身份字段（用户、画布、会话、运行号）和 `sess` 里的内存状态（选中节点、模式、模型、联网次数、技能固定版本）；真正依赖运行**记录**或会发事件的只有 `executeTool` 外壳（`tool.start/end`、步数、步数上限）和创建审批的三个工具（`generate_media`、`canvas_delete`、`ask_user`），以及发 `plan.updated` 的 `plan_update`。所以**不需要重写工具，只需要一个不依赖运行记录的执行入口**。

**做了什么**
- `RunContext`：调用方随请求声明的运行信息（运行、用户、会话、画布、模式、模型、选中节点、本轮预算、除模型调用外已花的积分）。**ID 在 JSON 里是十进制字符串**：雪花 ID 超过 2^53，用 JSON 数字会在 JavaScript 里丢精度，所以 Go 侧用 `json:",string"`，传数字会被拒绝。
- `AgentBridge.ExecutePlatformTool`：没有运行记录要查、不发 `tool.start/end`、不记步数和步数上限、不留存工具调用记录（这些归 Agent 服务）。**任务模式的限制仍在这里强制**（不依赖调用方自觉）；校验不过的是给模型看的工具错误（`is_error`），上下文不合法或调用了不能经这里执行的工具才是 HTTP 错误。
- 不能经通用入口执行的工具：`plan_update`、`ask_user`（只在 Agent 服务里）和 `canvas_delete`、`generate_media`（会创建审批，审批归 Agent 服务，由 C 切片提供「预估」和「执行」两个专门接口）。
- 没有令牌了，运行在内存里的状态（联网次数上限、技能固定版本）改按运行 id 存，闲置 12 小时自动清掉，`EndPlatformRun` 主动释放。联网上限因此从「每个运行片段 20 次」变成「每个运行 20 次」，比现在略严；一个运行跨多次审批时合计计算。
- HTTP：`POST /internal/platform/v1/tools/:name`、`DELETE /internal/platform/v1/runs/:run_id`。运行上下文放在请求头 `X-Agent-Context`（JSON 的 base64url，上限 16KB），幂等键放在 `X-Idempotency-Key`（1 到 64 个字母、数字或 `._:-`）。
- **路由只在配了服务令牌时才挂载**：没有这道门，平台接口根本不存在（404），而不是存在但拒绝。平台接口不要求桥的运行令牌，只认服务令牌。`router.NewBridge` 多了一个 `platform` 参数。

**测试**（先写测试、确认因预期原因失败，再实现）
- 服务层 10 个：**运行记录根本不存在**也能执行且不发事件；写画布带幂等键时重放结果一致、同一个模型 id 配不同键是两次写入；模式规则仍生效；未知工具、Agent 侧工具、非法上下文；别人的画布读写都得到「画布不存在」且不泄露内容；联网上限按运行累计、运行之间互不影响、`EndPlatformRun` 清零、闲置过期自动清掉。
- Handler 层：上下文和幂等键的解析；ID 必须是字符串、不是数字、上下文过大、幂等键太长或含非法字符都是 400 且不进入业务；工具层失败仍是 HTTP 200。
- 路由层：没有或错误的服务令牌 401 且不进业务；**没配服务令牌时平台路由 404**；没有平台 handler 时 404。
- **真实 HTTP + 真实 Postgres**：运行 id 用 2^60 + 1（转 float64 会和相邻的数撞成同一个），库里没有这个运行，画布写入成功、重放只留一条记录、`run_id` 无损存进 `bigint`、相邻两个大 ID 不混淆、**不写任何事件**、错误的服务令牌 401。

### 20.3 3.0b-B：模型代理的上下文模式与用量查询（2026-10-25）

**做了什么**
- `ProxyModelPlatform`：和令牌模式的 `ProxyModel` 做同样的事（模型快照、渠道 Key、预检、透传流式响应、按用量结算、留存请求体），区别是运行记录在 Agent 服务里：
  - **本轮已花的积分 = 调用方声明的 `spent_extra`（已批准的生成）+ Go 按 `agent_model_calls` 累加的模型调用实扣**，不新建账本表（D2）；
  - 不发事件（`message.delta`、`message.done`、`run.usage` 都归 Agent 服务），预算用尽也不改运行状态，只返回错误，由 Agent 服务决定怎么办；
  - 额度检查只能放在这里：第 19.7 节验证过 Agent 服务的「请求前钩子」抛异常拦不住请求，而所有模型请求本来就经 Go；
  - 结算仍用不会被取消的上下文，用户停止（调用方断开）时已经产生的部分照样收费；
  - 开始流之前把这次调用的 id 放进响应头 `X-Agent-Call-Id`（十进制字符串），调用方事后据此查这次调用的积分。
- `AgentBilling.SettleExternal`：同 `Settle`，但不更新运行的已花积分（没有运行记录可更新）。
- `PlatformRunUsage` 和 `GET /internal/platform/v1/runs/:run_id/usage?call_id=`：返回本轮模型调用已实扣的积分、上下文大小（最近一次**对话**调用的输入 token，摘要调用不算：它的输入是被压缩的那段历史，不代表对话现在多大），带 `call_id` 时一并给出这次调用的结算情况（`pending` 或 `settled`、应收、实扣、是否估算）。路径里的运行必须和上下文里声明的一致。
- `POST /internal/platform/v1/model/chat/completions`：OpenAI 兼容端点，pi 把它当模型提供方。流开始之前出错（含上下文不合法）一律返回 OpenAI 格式的错误（`{error:{message,type}}`），因为调用方的 OpenAI 客户端只认这个格式。令牌模式和上下文模式共用同一个外壳 `serveModelStream`，没有复制代码。
- 仓储新增 `RunModelUsage`、`ModelCall`；`BridgeDeps` 新增可选依赖 `RunUsage`（独立接口，没有扩大 `AgentBillingRepo`，免得牵动一大批测试替身）。

**测试**（先写测试、确认因预期原因失败，再实现）
- 服务层：运行记录不存在也能代理，不调 `AddRunUsage`、不发事件；预算按「已结算调用之和 + `spent_extra`」算，被拒绝的不创建调用记录、不发往上游；余额不足、没给模型、模型下线、上下文不合法；上游中途失败、调用方取消仍结算；摘要用途记成 `summary` 且不算上下文大小；用量汇总、按调用 id 查询、别的运行和别的用户查不到、没有调用的运行用量为零。
- 仓储（**真实 Postgres**）：按实扣而不是应收累加（余额不足时二者不同）；按用户和运行隔离；只数已结算的；运行 id 用 2^60 级的相邻大数不混淆。
- Handler：流和调用 id 响应头、摘要用途、OpenAI 格式的各种错误且不泄露内部细节、非法上下文、请求体过大 413、用量接口的各种参数校验。
- 路由：模型代理和用量接口同样只认服务令牌。
- **真实 HTTP + 真实 Postgres + 假上游模型**：运行记录不在 Go 里，流逐字原样转出（含用量分片和结束标记），调用已结算、积分真的扣了 5、没有运行记录也没有事件；用量查询一致；预算用尽返回 402 且**不发往上游、不创建调用记录**；摘要用途；别的用户查不到。

**真实数据库抓到的一个错误**：`RunModelUsage` 第一版让两条查询共用一个 `*gorm.DB`，第一条的 `Select(SUM)` 留在了第二条里，Postgres 报 GROUP BY 错误。内存替身完全看不出来。已改成每条查询用新的对象，并在代码里写了原因。

### 20.4 3.0b-C：审批拆成预估、解析、执行三个平台接口（2026-10-25）

**做了什么**

方案二里审批记录和状态流转（待决定 → 已批准 → 已执行……）归 Agent 服务，Go 只保留和钱、画布、生成任务有关的规则，提供三个**无状态**操作，规则和措辞只有一份，不在 Agent 服务里再写一遍：

| 接口 | 输入 | 输出 | 说明 |
| --- | --- | --- | --- |
| `POST /internal/platform/v1/approvals/propose` | `kind`（generate / delete / ask）、`args`（模型给工具的参数） | `payload`（审批内容）、`quote_credits`、`summary`、`content`（给模型看的）、`terminate` | 不落库、不发事件。任务模式限制、参数校验、预检、算价都复用对应工具里已有的逻辑；校验不过是 `is_error: true` 的 200 响应，不是 HTTP 错误 |
| `POST /internal/platform/v1/approvals/resolve` | `kind`、`payload`（预估给的内容，原样传回）、`decision`（和令牌模式的决定请求体相同） | `status`（approved / partial / rejected）、`decision`、`quote_credits`、批准的条目 / 节点 / 连线 | 纯逻辑：逐项勾选、张数只能少不能多、按批准的内容重新算价、检查本轮预算。直接调用令牌模式 `Decide` 用的 `resolveDecision`，不是复制；业务错误（超预算、一项都没批准……）原样返回错误码 |
| `POST /internal/platform/v1/approvals/execute` | `kind`、`approval_id`（十进制字符串）、`tool_call_id`、`outcome`（解析的结果原样传回） | `status`（none / executed / failed）、`result`、`outcome_text`（给模型看的工具结果） | 删除改画布、生成创建任务并绑定到节点；拒绝和提问不执行，只给文字 |

三个接口都要服务令牌 + `X-Agent-Context`，和其他平台接口一样；没有服务令牌时根本不挂载。

**执行是幂等的**（键是审批 id，即 `approval-<id>`）：Agent 服务在记录结果之前崩溃，重启后重试同一个审批，得到的是第一次的结果。
- 删除：幂等键存进 `agent_mutations.idem_key`，重放走 3.0a 的去重，不会因为「节点已经没了」误报失败，也不会写第二条改动。
- 生成：任务幂等键仍是 `agent-<审批 id>-<序号>`（任务服务据此不重复扣费）；**另外把提交结果随那条 bind 改动存进 `result_json`**，重放时直接返回。没有这一步，重放会得到「节点正在生成」，把已经成功的批准误报成失败。崩溃发生在「任务已创建、尚未绑定」之间时没有 bind 记录，重放会再走一遍提交，由任务服务的幂等键兜底。
- 请求被取消、超时这类「不知道有没有执行」的情况返回 error，让调用方重试，**不**记成审批失败（否则会告诉模型「没有扣积分」，实际可能已经扣了）。业务上的失败（节点不存在、积分不足、模型下线）是 `status: failed`，文字里写明原因。

**重构**：`AgentService.execute` 拆出纯逻辑 `runApproved`（不碰审批记录）和 `approvalExecResult`，令牌模式的 `execute` 和平台的 `ExecuteApproval` 共用，令牌模式行为不变（既有测试全部不改）。

**测试**（先写测试、确认因预期原因失败，再实现）
- 预估：生成算价但什么都不创建、说明为什么算不了、模式限制、删除列出标题和是否含产物、提问的校验和模型清单、非法输入。
- 解析：和令牌模式 `Decide` **逐条对照**的一致性测试（21 个场景）；变异检查证明它不是空转（改动预算判断，恰好两个预算用例失败）；状态和执行计划；预算含按调用记录累加的模型花费；非法输入。
- 执行：删除并重放得到同样结果且只有一条改动；部分删除的文字；删除失败是 `failed` 而不是异常；生成用真实的 `AgentGenerator` 提交，任务键、绑定、重放不再提交；生成失败说明没扣积分；拒绝和提问不执行；别人的画布不会被动；非法输入无副作用；请求被取消是错误而不是失败。
- 变异检查：去掉生成的重放短路、去掉执行接口的幂等键，对应的重放测试都会失败。
- Handler：三个接口的参数传递、雪花审批 id 按字符串解析、各种非法请求（缺上下文、缺字段、`null`、审批 id 是数字、决定不认识）不进入业务层、业务错误码原样返回。
- 路由：三个接口都只认服务令牌。
- **真实 HTTP + 真实 Postgres**：运行和审批 id 都是 2^60 级；预估不落库；解析 → 执行 → 重放，只留一条删除改动，`idem_key` 是 `approval-<id>`，Go 不为平台调用创建审批记录；错误的服务令牌 401。

**没有覆盖、留给后面**
- 生成的执行没有走真实数据库端到端（e2e 环境没有配生成任务服务）；服务层用真实的 `AgentGenerator` 加假任务服务覆盖，bind 的 `result_json` 存取在 jsonb 上由删除流程间接验证。接入 Agent 服务的审批引擎（3.2）时补一条带真实任务服务的端到端。
- `approval_id` 目前只用来做幂等键和任务键；Agent 服务生成的雪花 id 必须全局唯一，否则不同审批会互相当成重放（3.1 的 ID 分配器负责保证）。

### 20.5 3.1：Agent 服务的数据层（2026-10-25）

**做了什么**（都在 `agent/src/`，`agent/test/` 有测试；这一步只建数据层，还没有接进 `serve.ts`，3.2、3.3 才会用它）

- `ids.ts`
  - **`createIdCodec(key)`**：Go 的 `idcodec` 在 TypeScript 里的逐字节移植（SHA-256 派生 AES-128 密钥，ID 加固定校验串，单块 ECB）。测试里的向量是 **Go 实际算出来的**（临时跑了一个 Go 程序，6 个值含 `2^53`、`2^60+1`、`2^63-1`、`2^64-1`），不是自己和自己对。也和 Go 一样大小写都认、拒绝 0。
  - **`createSnowflake({workerId, now, wait, notBefore})`**：41 位毫秒（自 2026-01-01 UTC）+ 10 位节点号 + 12 位序号，同步生成，返回 `bigint` 或十进制字符串。时钟回拨时沿用上一个时间戳、序号用完就向前借一毫秒（不空等：回拨可能是几秒）；同一毫秒 4096 个用完才等下一毫秒。
  - **`notBefore`**：打开数据库时取库里所有表**最大 ID 的时间戳**作为下限。设计 19.8 没想到的一个洞：进程重启期间系统时钟被拨回去（NTP 校时），新进程会发出比重启前更小、甚至相同的 ID。现在新 ID 一定比库里已有的大，有测试。
- `store/migrations.ts`：自己的迁移机制（`agent_schema_migrations`）。只增不改（记校验和，已发布的迁移被改过就拒绝启动）；**库的版本比代码新（回退了镜像）拒绝启动并说清原因**；每条迁移一个事务，失败整条回滚、版本不前进；版本号必须从 1 连续。
- `store/store.ts`：`AgentStore`（`node:sqlite`，同步）。语义和 Go 的 `AgentRepository` 一一对应：按用户隔离（别人的和不存在的一样是 `NotFoundError`）、会话软删除、**状态迁移是 CAS**（`UPDATE … WHERE status IN (…) RETURNING *`，没命中是 `StateConflictError`）、**同一画布只有一个活跃运行由部分唯一索引保证**（`DuplicateError`）、事件序号在同一个事务里先加一再写事件（写失败整体回滚，序号不会留下空洞）、`interruptActiveRuns` / `expirePendingApprovals` / `addRunUsage` 等。可更新的字段是白名单，字段名不会拼进 SQL。
  - 表：`agent_sessions`、`agent_runs`、`agent_events`、`agent_approvals`，和 Pi 的 `durable_*` 表不重名。会话历史（Go 里的 `session_jsonl`）不在这里，归 Pi Durable。
  - ID 在库里是 `INTEGER`（64 位），读出来用 `bigint`，对外是十进制字符串，**不会经过 JavaScript 的 `number`**。
  - 打开时设 WAL、`synchronous=NORMAL`、`busy_timeout=5s`、外键。**进程被杀不丢已提交的事务**；整机断电可能丢最后几个事务但不会损坏文件。
  - `backupTo(path)`：`VACUUM INTO` 做在线一致快照，单个自包含文件（不依赖 `-wal`），同一个文件里 Pi 的表一起备份；不覆盖已有文件。3.6 的 `update.sh` 用它。

**对第 19.3 节的一处修正**：那里写「应用表和 Pi 的表同库、同一个事务」。实际上 Pi 的 `SqliteStorage` 自己持有连接（只能通过它的公开 API 使用，设计决定是不写自定义存储），应用表和 Pi 的表在**同一个文件、两个连接**里，**不能在同一个事务里提交**。后果：「Pi 提交了工具结果、但应用事件还没写」或反过来，这两步之间崩溃会不一致。3.2 要按这个约束设计：事件的写入放在 Pi 的任务**之后**并可重放（事件有序号，重复写入要能识别），不依赖跨库事务。

**测试**（先写测试、确认因预期原因失败，再实现；Node 23 和 Node 22 镜像里各跑一遍，34 条全过）
- ID：Go 向量、拒绝格式不对/被改过/密钥不同/0/超出 64 位、结构（41+10+12）、严格递增且不重复、4096 个用完才等、时钟回拨、`notBefore`、节点号范围、2026 年的 ID 超过 2^53。
- 迁移：新库全部应用且不重复、库比代码新拒绝、迁移被改过拒绝、失败整步回滚后可接着跑、版本号连续。
- 存储：会话（创建、隔离、列表顺序、软删除不计数、更新）、事件（序号连续、按会话独立、分页、失败回滚无空洞）、运行（字段、活跃唯一、CAS、白名单、累加、启动时中断）、审批（CAS、重复点击、批量失效）。
- **重启和崩溃**：重启后数据完好、时钟拨回后新 ID 仍更大；**子进程不停追加事件，在 300 条之后 `SIGKILL`，重开后已确认的一条不丢、序号连续、`lastSeq` 一致**（连跑 20 次无失败）。
- **备份恢复演练**：备份是那一刻的快照（不含之后的写入），恢复后数据、序号、ID 分配都接得上；不覆盖已有备份。
- 并发写：另一个进程占着写锁 300 毫秒，这边等待而不是立刻 `SQLITE_BUSY`（Pi 的连接共用这个文件）。
- 变异检查：把追加事件移出事务，回滚测试**和崩溃测试都失败**（`SIGKILL` 落在「序号加一」和「写事件」之间会留下空洞，说明崩溃测试不是摆设）；去掉 `notBefore`，重启测试失败。

**配置**（3.2 起接进 `serve.ts`）：`AGENT_DB_PATH`（SQLite 文件，Docker 里放 `agent_data` 卷）、`AGENT_ID_KEY`（和 Go 的 `APP_SERVER_ID_KEY` 相同）、`AGENT_WORKER_ID`（默认 1）。

**没做、留给后面**
- 事件保留期清理（Go 里按 `created_at` 清理）：`idx_agent_events_created` 已建，清理逻辑在 3.3 随事件接口一起做。
- 没有往 `serve.ts` 里接线，所以现在服务的行为没有任何变化。

### 20.6 重启后没跑完的运行怎么办（2026-10-25，用户决定，exp9 实测）

取代第 18.6 节。那里的冲突（Go 先把运行标成中断、收回桥令牌，Pi 续跑时被拒绝）在方案二里不存在了：运行状态在 Agent 的 SQLite 里，Go 没有令牌也没有租约，没人会抢先标中断。剩下的是行为选择。

**用户决定**：选 C（限时自动续跑）；自动续跑时要显示「恢复中」；不额外让 Go 在续跑前检查余额和预算（模型代理每次调用本来就检查，不够时运行停在预算用尽）。

| | 行为 |
|---|---|
| 窗口内（默认 5 分钟，`AGENT_RESUME_WINDOW_SECONDS`） | 自动续跑，用户不用点任何东西。先发一条「恢复中」，续上后回到 `running` |
| 超过窗口，或任务被重启上限挡住 | 标为 `interrupted`，用户点「继续」 |

**实测（`阶段0验证/exp9-restart.ts`，Pi Durable 1.4.0）**

| 情形 | 结果 |
|---|---|
| 正常停机（SIGTERM 后只 `close()`，不 `abort()`）时在执行写工具，重开后 `resume()` | 和 SIGKILL 一样：写工具（`replay: "never"`）不重跑，模型收到「被中断，可能已经执行」，接着往下答。**发版时 Pi 的自动续跑可用** |
| 正常停机时正在等模型回答 | 重开后**重新发起一次模型请求**（被切断的那次丢了）。多一次模型调用，被切断那次由 Go 按已经流出的部分结算，可以接受 |
| 重开后对会话 `abort()`，再提交一条「继续」 | 可行：悬着的工具调用补上结果 `Tool … was aborted`，新输入照常处理，历史连贯 |
| 重开后既不 `resume` 也不 `abort`，直接提交新输入 | **提交本身就会启动调度**，悬着的任务先续跑，新输入排在后面 |
| 连续被杀 | 第 5 次之后任务被挡住：`inspect()` 里状态是 `blocked / restart_limit`，`resume()` 也不动它；`retryTask()` 清零后才再跑 |

**由此得到的设计约束**

1. **调度是全局的，不能按会话选择性续跑。** `Harness.open()` 不派发任何任务；`resume()`、任何一次 `submit()`、`abort()` 都会启动调度，并且对**所有**会话生效。所以启动时必须在接受任何请求之前，把「过期」的会话先处理掉，否则第一个用户发消息就会把所有过期的运行一起续跑。启动顺序固定为：
   1. 打开存储和 Harness（此时调度是暂停的）；
   2. `inspect()` 读出所有未完成任务，对照库里状态为 `queued` / `running` 的运行；
   3. 对每个运行判断：任务被 `restart_limit` 挡住，或停机时长超过窗口，或库里没有对应的任务 → 对该会话 `abort()`，运行标为 `interrupted` 并发 `run.status`；其余 → 保留 `running`，发「恢复中」；
   4. 然后才开始接受请求。
2. **停机时长怎么量**：运行期间每 10 秒把 `lease_until` 续到「现在 + 30 秒」（该字段 3.1 已建好）；启动时 `停机时长 = 现在 − (lease_until − 30 秒)`。不用事件时间：模型长时间思考时可能一直没有事件。
3. **正常停机（SIGTERM）**：只 `close()`，**不 `abort()`**，也不把运行标成中断；最后续一次租约。运行留在库里，下次启动按窗口判断。这和现在「停机就中止所有运行」相反，**服务的 `shutdown` 要改**。
4. **「继续」按钮**：对会话 `abort()`（如果还有活任务）后提交一条固定的内部输入。补上的工具结果只说「被中止」，没说**可能已经执行**，而这对写画布的工具很重要，所以这条输入要自带提醒：「上一次运行被中断，最后一步的工具结果丢失，可能已经执行，请先查看画布现状再继续」。这取代阶段 1 加的 `closeDanglingToolCalls`。
5. **等审批、等回答的运行不受影响**：它们没有活任务（第 17.1 节的建模），状态在库里，不看窗口。
6. **「恢复中」不能作为落库的运行状态**：库里「同一画布只有一个活跃运行」靠部分唯一索引，只认 `queued` / `running` / `waiting_*` 四个值；如果把运行改成 `recovering`，画布会被释放，用户能在恢复期间再开一个运行。所以「恢复中」只作为**事件**发出（`run.status`，`status: "recovering"`），库里仍是 `running`。

**要改前端（小改，唯一一处）**：前端把 `run.status` 的值直接当运行状态，只有 `web/src/utils/agent/session-state.ts` 的 `ACTIVE_RUN_STATUSES` 里的才算「进行中」，认不得的值会被当成运行已结束（流式文字被清掉）。所以要把 `recovering` 加进进行中的集合，并让消息流末尾的状态行显示「正在恢复…」。几行代码加测试，放在 3.3 产生这条事件的时候一起做。第 19.3 节 3.4 的退出条件「前端不改一行」相应改为「除 `recovering` 外不改」。

**留给 3.2 验证**
- exp9 的工具不理会中止信号，所以正常停机时被记了一次重启（`restarts=1`）。设计文档引用的 Pi 说法是「正常关闭不计数」；真实工具（调 Go 的 HTTP 请求）会响应中止信号，3.2 要用这样的工具再测一次，否则每次发版都在消耗重启上限的额度。
- 窗口内自动续跑时，已经在 Go 那边开始的模型调用（上一个进程被杀时切断的那次）要确认会被结算掉，不会一直停在 `pending`。

### 20.7 3.2 对话引擎：拆分、两个新发现的缺口、模型请求怎么带运行上下文（2026-10-25）

3.2 是整个阶段 3 里最大的一块（把 `run.ts`、`steer.ts`、`compaction.ts`、`agent-tools/` 和 Go 里 `bridge*.go`、`runtime.go`、`run.go`、`approval.go` 的编排逻辑用 Pi Durable 重写一遍），按能单独验证的小步走。

**拆分**

| 步 | 内容 | 验证 |
|---|---|---|
| 3.2-0 | **Go 补两个口**（见下）：工具结果带上 `tool.end` 的完整事件内容；新增「运行准备」平台接口 | Go 测试，含真实 HTTP + Postgres |
| 3.2-1 | `agent/` 加依赖 `pi-durable`；平台客户端（工具、审批三接口、用量、释放运行）；模型提供方（按下面的做法带运行上下文） | 假 Go 服务的契约测试 |
| 3.2-2 | 工具扩展：Go 下发的服务端工具（`replay` 读 `safe`、写 `never`，幂等键 = Pi 工具任务 id）、`plan_update`、`ask_user` 与审批工具（预估 → 终止本轮）、codemode 移植 | 假 Go + 假模型 |
| 3.2-3 | 运行引擎：开始、插话、取消、审批后续跑、「继续」、步数与预算用尽、事件落库（含 `message.done` 积分、`run.usage`）、`beforeCompact` 压缩、20.6 的启动恢复和正常停机 | 假 Go + 假模型，含崩溃恢复、并发会话 |

**缺口一：平台工具接口的结果不够生成 `tool.end` 事件。** 现在 Go 自己发 `tool.end`，内容有一行摘要 `summary`、被改动的节点 `node_ids` / `created` / `updated`、标签 `label` / `verb`；而平台接口只回 `content / is_error / images / terminate`，摘要在私有字段里没下发。方案二里事件由 Agent 发，3.0b-A 漏了这一块。补法：平台工具结果多一个 `tool_end` 字段，**就是 Go 原来发的那个事件内容**（原样），Agent 加上自己的序号落库即可，不用在 TypeScript 里再写一遍摘要和字段，也保证黄金文件对得上。`tool.start` 的内容只有 `id / name / label / verb`，由工具声明静态得到。

**缺口二：运行从哪儿拿系统提示词、工具声明、模型信息。** 现在 Go 启动运行时把系统提示词（`prompts.SystemWithSkills`，含技能目录）、工具声明（`declaredTools`，随任务模式和是否支持看图变化）、模型信息（上下文窗口、最大输出、是否看图）、带画布目录的第一条用户消息（`prompts.User`）、提示词版本、工具哈希和构建号（运行记录里用来比较版本效果）一并下发。方案二里运行由 Agent 发起，这些都是平台的知识（提示词、画布、技能、模型配置都在 Go），所以要一个**运行准备**接口：`POST /internal/platform/v1/runs/prepare`，入参运行上下文加用户消息和选中节点，出参上面这些。续跑也调它（取最新的系统提示词和工具声明，第一条消息不要）。

**exp10：模型请求怎么带「这是哪个运行」**（`阶段0验证/exp10-run-context.ts`）。Go 的模型代理要求每个请求带 `X-Agent-Context`（运行 id、预算、已花……），但 Pi 的流选项（`settings.stream.headers`）是整个 Harness 共用的，一个进程托管很多会话，不能按会话区分。做法：`beforeRequest` 钩子里读出 Pi 给这个会话生成的提供方会话 id（`ProviderDoc`），登记「提供方会话 id → 运行上下文」；包一层模型提供方，发请求时按 `options.sessionId` 查表加请求头。实测：
- 两个会话并发各问一次，各自的请求带的是各自的运行上下文，没有串；
- 同一个会话的下一轮运行换了上下文（比如批准后 `spent_extra` 变了），请求带的是新的；
- Harness 打开之后可以再注册新模型（Go 下发的模型清单会变），新模型的请求同样带对。

压缩用的摘要请求走 Pi 内置压缩时也经过同一个提供方；我们自己在 `beforeCompact` 里调模型时，请求头要自己带上运行上下文和 `X-Agent-Purpose: summary`。

### 20.8 3.2-0：Go 补的两个口（2026-10-25，已实现）

**做了什么**
- **`tool_end`**：平台工具结果（`POST /internal/platform/v1/tools/:name`）多一个 `tool_end` 字段，内容就是令牌模式下 Go 自己发的那个 `tool.end` 事件（`id / name / is_error / summary / label / verb` 加各工具自己的 `node_ids / created / updated`……）。令牌模式和平台共用 `toolEndEvent` 一个函数组出来，不是复制；被拒绝的调用（未知工具、模式不允许）也有，摘要是拒绝原因。令牌模式的响应不变。`tool.start` 的内容只有 `id / name / label / verb`，label 和 verb 由工具声明带来，Agent 不用再问。
- **运行准备** `POST /internal/platform/v1/runs/prepare`：入参运行上下文（头）加可选请求体 `{message, max_steps}`；出参 `{model{name,context_window,max_tokens,vision}, system_prompt, tools[], disabled_tools[], first_message, harness{prompt_version,tools_hash,build_sha,key}}`。
  - 不带 `message` 是续跑：只给最新的模型、提示词、工具声明，没有第一条消息；
  - 带 `message` 时读画布目录（选中的节点优先）拼出第一条用户消息，和令牌模式 `Start` 共用 `firstUserMessage`；
  - 令牌模式的 `ProcessRuntime.baseInput` 也改成调同一个 `runSetup`，**组装逻辑只有一份**；既有测试一条没改全部通过；
  - 工具声明随任务模式和模型是否支持看图变化，配置里停用的工具（`SetDisabledTools`）去掉并在 `disabled_tools` 里告诉 Agent；
  - 不读写任何记录、不发事件。

**测试**（先写测试、确认因预期原因失败，再实现）
- `tool_end` 与令牌模式的 `tool.end` 事件**逐字段一致**（读画布、写画布、整理布局、查模型、未知工具、校验失败共 6 种；新建节点的随机 id 比较前抹掉）；拒绝也有；响应 JSON 里确实有这个字段；真实 HTTP 响应里也有。
- 运行准备：模型规格取自快照、没有技能时提示词就是 `System(mode)`、有技能时带目录、工具随模式和看图变化、停用的工具去掉、第一条消息与 `prompts.User` 的拼装一致、默认 40 步、版本信息取自服务配置、各种错误（上下文不合法、非 Agent 模型、没有上下文规格、负的步数、别人的画布）、不发事件不建记录。
- Handler 与路由：参数传递、各种非法请求不进入业务层、业务错误码保留、只认服务令牌。
- **真实 HTTP + 真实 Postgres**：画布目录读自真实的库（选中的 `n_old` 和 `n_script` 都在第一条消息里），雪花大小的运行 id，不落任何记录。

**接下来**（3.2-1 起都在 TypeScript 里）：加依赖、平台客户端、模型提供方（exp10 的做法）。

### 20.9 3.2-1：依赖、平台客户端、模型提供方（2026-10-25）

**依赖**：`agent/` 新增 `@earendil-works/pi-durable`、`chord`、`pi-telemetry`，版本钉死 `1.4.0`（和 `pi-agent-core`、`pi-ai` 同版本，和阶段 0 验证目录一致）。锁文件已更新，Node 22 下 `npm ci`、类型检查、测试通过。

**`src/platform-client.ts`（平台客户端）**：Agent 调 Go 平台的唯一入口，和 3.0、3.2-0 做的七个接口一一对应：`tool`、`prepare`、`propose`、`resolve`、`execute`、`usage`、`endRun`。
- 服务令牌随每个请求；`X-Agent-Context` 是运行上下文 JSON 的 base64url（含中文的节点 id 也能往返）；工具调用可带 `X-Idempotency-Key`。
- 运行上下文的四个身份字段必须是规范的十进制字符串，传数字（雪花 ID 会丢精度）、前导零、零、非数字一律在**本地**报错；上下文头超过 Go 的 16KB 上限（选中的节点太多）也在本地报错并说明原因，不等 Go 返回 400。幂等键格式、审批 id 同样先在本地校验。
- 错误分三类，调用方要分得清：业务错误（信封 `code` 不为 0，带 HTTP 状态和业务码）、HTTP 非 2xx 或不是 JSON（反向代理的 502 页面）、连不上（状态码 0）；请求被 `signal` 中止时抛出的是原来的中止错误，**不包装成 PlatformError**。
- 响应类型和 Go 的 JSON 逐字段对应（`tool_end`、`RunSetup`、审批三个结构、用量）。

**`src/model-provider.ts`（模型提供方）**：把 exp10 的做法落成代码。
- `RunContexts`：应用每次提交前登记「会话 id → 运行上下文」；`runContextExtension` 的 `beforeRequest` 钩子登记「提供方会话 id → 会话 id」；流函数据此给每个请求加 `X-Agent-Context`。
- 响应头 `X-Agent-Call-Id`（十进制字符串，超过 2^53）通过 `onCall` 交出，带上提供方会话 id，3.2-3 用它查这次调用的积分，写进 `message.done`。
- `createModelRegistry().ensure(spec)`：模型规格（上下文窗口、输出上限、是否看图）随管理员配置变化，Harness 打开之后可以反复调用，**规格有变化才重新注册**。兼容老的 OpenAI 网关：请求里没有 `store`、`max_completion_tokens`，输出上限用 `max_tokens`。
- 压缩时我们自己在 `beforeCompact` 里调模型，请求头要自己带 `X-Agent-Purpose: summary` 和运行上下文（3.2-3 做）。

**测试**（先写测试、确认因预期原因失败，再实现）
- 平台客户端 12 条：路径、请求头、请求体、返回值；上下文的编码往返和校验；业务错误 / 非 JSON / 连不上 / 中止四种失败的区别；审批三接口和用量、释放运行。用一个假的 Go 服务（`test/platform-fake.ts`）。
- 模型提供方 7 条，**用真的 Pi Harness 加假的模型代理**（`test/model-proxy-fake.ts`）：请求发到正确路径并带服务令牌；两个会话并发不串（请求内容也对得上各自的上下文）；同一会话下一轮换了上下文生效；调用 id 交给 `onCall` 并能找回会话；模型规格更新和新增；没有较新的请求字段。
- 变异检查：去掉会话登记（4 条失败）、去掉规格有变化才注册的判断（1 条失败）、不取响应头里的调用 id（1 条失败）。

**没有覆盖、留给后面**
- 没登记运行上下文的会话发模型请求时，请求会不带 `X-Agent-Context` 发出，由 Go 返回 400（OpenAI 格式的错误，Pi 记为模型错误）。这是编码错误，3.2-3 的运行引擎保证提交前一定登记，并有测试。
- 还没有和真实的 Go 联调，全部用假服务；联调放在 3.2-3 的端到端里。

### 20.10 3.2-2：工具扩展（2026-10-25）

**做了什么**（`agent/src/durable-tools.ts`、`codemode.ts`；Go 侧只给工具声明加了 `verb` 字段）

- **服务端工具**：Go 下发的每个工具声明变成一个 Pi Durable 工具。名字、说明、参数 schema 都来自 Go，这里不内置任何业务工具。执行就是 `POST /tools/:name`，幂等键是 **Pi 的工具任务 id**（每次调用唯一、同一次调用重放时不变）。
- **重放策略**：
  - 读工具、改画布的写工具（`canvas_apply_ops`、`canvas_arrange`）、审批类和计划：`safe`。写工具能 `safe` 是因为 Go 按幂等键去重（3.0a），重放返回第一次的结果，不会写两遍；审批类由宿主按调用 id 去重。
  - 截帧（会写素材）和没登记的新工具：`unsafe`，崩溃后模型收到「被中断，可能已经执行」自己确认现状。
  - **更正**：之前文档里写的 `replay: "never"` 不对，Pi 的类型是 `"safe" | "unsafe"`，默认 `unsafe`（实验里传 `"never"` 恰好被当成非 `safe`）。
- **同一轮的调用一个个执行**（`executionMode: "sequential"`），否则同一条消息里的多个画布写入会并发；去掉它会有两条测试失败。
- **模型把单个值当数组给**（`nodeIds` 写成字符串）：用 `prepareArguments` 在校验前补成数组，复用原来的 `wrapScalarArrays`，行为不变。
- **图片**：对象存储里的图片只有地址，工具把地址放在 `data` 里，模型提供方发请求前还原成真正的地址（新增，有测试）。
- **审批类三个工具**（`generate_media` / `canvas_delete` / `ask_user`）：先 `propose` 预估；失败是给模型看的错误，不创建审批；通过后交给宿主创建审批，本轮停下。事件顺序和现在一致：`tool.start` → 创建审批 → `tool.end`。
- **本轮已暂停**（等审批、等回答、步数用尽）：同一轮里后面的调用不执行，也不发 `tool.start`，告诉模型稍后重调。
- **`plan_update`** 在 TypeScript 里本地校验（最多 12 步、标题 1–80 字、状态 todo/doing/done，措辞与 Go 一致），通过后交给宿主发 `plan.updated`。它只产生一个事件，没有平台副作用，所以没有走平台。
- **平台调用本身失败**（HTTP 500、连不上）：模型看到错误结果，时间线收尾（`tool.end` 为错误），运行不崩；被中止则原样抛出，让 Pi 按中止处理。
- **codemode 换成 Pi 自带的**（`createCodemodeExtension`），不再自己维护：脚本在崩溃后能接着跑（重启后从记录的启动数据重新开始，已完成的工具调用按各自的重放策略取回结果）。脚本里能调哪些工具由 `callers` 决定：Go 标为「可被脚本调用」的才有。脚本里每次调用仍走平台、各自一个幂等键，`tool.start/end` 带外层调用的 id（`parent_id`）。Pi 自带的 `codemode` 工具不经过我们的扩展，所以用同一个 `trackedCall` 包了一层，时间线上留一步「运行脚本」、计 1 步（摘要沿用原来的「脚本调用了 N 次工具」）。
  - **模型看到的变化**：工具说明和脚本里的写法由 Pi 生成（`tools.工具名({参数})`），不再是原来那份中文说明。系统提示词里第 12 条关于 codemode 的使用建议仍然适用。这是行为变化，上线前要用真实模型看一次。

**宿主接口 `ToolHost`**：工具层不管运行状态，步数、事件、审批、暂停都通过它交给运行引擎（3.2-3 实现）。要求：`approval` 必须按调用 id 幂等；`begin` 可以拒绝（步数用尽、脚本里嵌套调用次数超限），拒绝时工具层把宿主给的文字当结果交给模型；步数只在没有 `parentId` 的调用上计。

**测试**（真的 Pi Harness + 假模型 + 假 Go 服务 + 假宿主）：工具往返与幂等键格式、`is_error` 映射、图片、重放策略表、步数上限、本轮已暂停时跳过、`plan_update` 成功与四种错误、审批三种与预估失败、平台失败、数组补全、顺序执行（且两次调用的幂等键不同）、codemode 的嵌套事件与步数、可被脚本调用的工具范围、脚本里工具失败。**崩溃重放**：第一个 Harness 发出平台调用后关闭，重开的 Harness 续跑，同一个调用带着同一个幂等键再来一次。变异检查：写工具改成 `unsafe`（重放测试失败）、去掉暂停检查、不带幂等键、去掉 `sequential`、去掉数组补全、跳过创建审批、`callers` 全放开、嵌套 `tool.end` 不带 `parent_id`，每一项都有测试失败。

**留给 3.2-3**
- 脚本里嵌套调用次数的上限（原来 Go 对一个运行片段限 100 次）：由宿主在 `begin` 里按运行计数并拒绝。
- 工具扩展和 codemode 还没接进真正的运行引擎，也没有和真实的 Go 联调。

### 20.11 3.2-3a：运行引擎主线（2026-10-25）

**范围调整**：用户决定嵌套调用次数上限先不设（20.10 里留给 3.2-3 的那一项取消）。3.2-3 拆成三步：**3a 引擎主线**（本节）、3b 审批决定与「继续」、3c 压缩和启动恢复 / 正常停机。

**做了什么**（`agent/src/engine/`）
- **`AgentEngine`**：`startRun`、`interject`、`cancel`、`idle`、`close`。一个会话对应一个 Pi 会话（对话历史归 Pi），一次运行 = 向它提交一条用户输入直到 Pi 空闲。会话与 Pi 会话的对应关系记在 `agent_sessions.conversation_id`（迁移 2）。
- **发起**：校验会话归属、消息非空、已选好模型 → 创建运行（`queued`；同一画布同时只有一个活跃运行由唯一索引保证，冲突是 60001）→ 记会话的模式、模型和标题（首条消息当标题，沿用 `cleanTitle` 规则）→ `message.user`、`run.status` → 调平台**运行准备**（版本字段写进运行，之后沿用）→ 配置 Pi 会话（模型、系统提示词、这一轮可用的工具）→ `running` → 后台驱动。准备失败：运行标为失败、画布立刻空出来、对用户说「Agent 暂不可用」（60005），和现在一致。
- **事件**（内容与 Go 现在发的一致）：`message.user`、`run.status`（需要提示的状态带 `title` / `hint`）、`run.usage`、`message.done`、`tool.start` / `tool.end`、`plan.updated`、`approval.created`；文字增量只推送不落库（`seq` 为 0）。顺序也一致：**先 `run.usage` 再 `message.done`**；审批是 `tool.start` → `approval.created` → `tool.end`。
- **`message.done` 用 Pi 的 `afterResponse` 钩子发**，不用 `watchEvents`：钩子在任务内按序执行，天然先于工具执行，不需要额外的同步；`watchEvents` 标着实验性，且溢出时会把未送达的批次合并成快照，所以只用它推文字增量（丢了不要紧，`message.done` 带完整内容，前端按它对账）。积分来自平台的用量接口（按响应头里的调用 id 查），结算比流结束晚几毫秒，最多等约 2 秒。
- **结束状态**：模型出错 → `failed`（错误信息截到 200 字，事件里带 `error`）；**本轮额度用完 → `budget_exhausted`**，不是失败；步数用尽 → `step_limit`；等审批 / 等回答 → `waiting_approval` / `waiting_input`；取消 → `canceled`；其余 `succeeded`。结束时用 CAS 迁移，运行已被别的流程改了状态（取消、等审批）就不动。每段运行结束都通知 Go 释放这个运行的状态。
- **插话**：官方效果，但**必须在提交时指定 `whenBusy: "steer"`**，默认是排队成下一轮（会多一次模型请求，实测发现）。正在执行的工具不被打断，工具跑完后的下一次请求带上插话，只多一次请求。
- **取消**：先 CAS 到 `canceled`、让等待的审批失效，再中止 Pi 里进行中的调用，最后发 `run.status`；已经结束的运行取消是 60003，别人的运行是 60012。
- **审批**按工具调用 id 幂等（崩溃后重放不会出现两个审批），审批 24 小时有效；对外 id 用 idcodec 编码。
- **工具声明**：Go 下发的声明变化时才重新安装工具扩展；每个会话只启用本轮模式和模型允许的工具（`configure({tools})`）。

**实测发现的几处，已处理**
1. **错误响应不走 `onResponse`**：pi 用 OpenAI SDK，非 2xx 直接抛错，`onResponse` 只在成功时调用。所以模型提供方改成注入自己的 `fetch`，在这一层看状态码和业务错误码。同时 `maxRetries: 0` 关掉 SDK 自带的重试（重试在 Go，不然同一次失败算两遍）。
2. **Go 要补一个响应头 `X-Agent-Error-Code`**（已做）：模型代理的业务错误（额度用完、余额不足、模型不可用）带上错误码。OpenAI 格式的错误体只有文字，不能靠比对文案区分「额度用完」和别的错误。令牌模式共用同一个外壳，一并有。
3. **流式增量按 Pi 的提交节奏出现**（`partialIntervalMs`，设为 100ms；每次提交是一次存储写入，不宜更密）。第一段文字在 `message_start` 的初始内容里，需要一并当作增量。
4. **`replay` 是 `safe | unsafe`**（20.10 已更正）。

**测试**（真的 Pi Harness + 内存存储，假的 Go：平台接口和模型代理在同一个地址，模型的每一轮回答按脚本给出，含工具调用和 SSE 流）15 条：整轮的事件顺序和内容、增量不落库、工具回合、模型出错、额度用完、余额不足、步数用尽、两种审批、审批幂等、取消（含已结束 / 别人的运行）、取消时审批失效、插话、同画布互斥和不同画布并发时各带各的运行上下文、各种校验失败。连跑 8 次无失败；变异检查：插话改回排队、审批不去重、取消不让审批失效、额度用完当失败、不计步数，每项都有测试失败。

**没做、留给后面**
- **3b**：审批的决定（解析 → 执行 → 让运行接着往下走）、过期；「继续」（中断、额度用完、步数用尽后）；追加预算。
- **3c**：压缩（`beforeCompact`）、启动恢复和正常停机（20.6）。
- **3.3**：消息里引用（节点、模型、技能）的校验和默认模型的选择在 Go 那边（`checkRefs`、`pickModel`），REST 层需要新的平台接口来问；运行超时（`timeout` 状态）现在没有触发条件。
- 插话恰好落在运行收尾的瞬间时，可能出现「运行已标成功、Pi 又开始了新一轮」的竞态，3b 里用「空闲后再检查队列」处理。

### 20.12 3.2-3b：审批的决定和「继续」（2026-10-25）

**做了什么**（`engine.decide`、`engine.resume`）

- **`decide`（用户对审批做决定）**，顺序和 Go 的 `Decide` 一致：
  1. 校验：审批和运行都得是自己的（`60013` / `60012`）；已处理（重复点击）、已过期（24 小时）、运行已被停止 → `60004`。
  2. 平台**解析**（逐项勾选、重新算价、检查本轮预算）。决定不合法（超预算 `60008`、一项都没批准 `10001`）时什么都不改，业务错误的码和 HTTP 状态原样返回，审批仍待处理、运行仍在等。
  3. 记决定：审批状态迁移是 CAS，**两个标签页同时点只有一次生效**，另一次是 `60004`，不会执行两次（解析是纯逻辑，所以输的一方没有任何副作用）。
  4. 运行回到 `running`（追加的预算一并生效；**拒绝时追加的预算不生效**）；被批准的预估先记进已花积分，防止同一轮反复批准超预算；发 `run.status`、`run.usage`。
  5. 平台**执行**（删除或创建生成任务，幂等，键是审批 id）。执行失败（积分不足、模型下线）不让这次请求失败，审批记为 `failed`，原因转告模型。**执行接口连不上**：重试 3 次（幂等所以安全），仍不行就如实告诉模型「这次执行可能已经生效也可能没有，请先确认现状」，审批记为 `failed`——不假装知道结果。
  6. 发 `approval.decided`，再让运行接着往下走。起不来就把运行标为 `interrupted`，用户可以稍后点「继续」。
- **把结果交给模型**：用真实结果替换那次工具调用的占位结果（exp1 验证过的 `edits` 写入），再提交一条输入让模型继续。这样模型看到的是「用户批准了生成：……任务已提交」，不是「已提交，等待决定」。
- **`resume`（继续）**：中断、额度用完、步数用尽的运行可以继续（其他状态 `60003`）。额度用完必须追加预算（`10001`）；步数用尽把上限放宽一轮（+40）；回到 `queued` 重新占画布（被别的运行占了是 `60001`，状态不变）；事件 `run.status queued` 带 `resumed: true`。继续前先中止 Pi 里可能留着的未完成任务。**被中断**的继续，给模型的提醒是「最后一步的工具结果丢失，那一步可能已经执行，先确认现状」（20.6 的设计）；其余的只说「请继续刚才的任务」。准备失败：运行标失败、画布立刻空出来、`60005`。
- **运行片段的共用启动流程 `#launch`**：发起、审批后续跑、继续共用（运行准备 → 配置 Pi → 标 running → 后台驱动）；版本字段只在第一段记，之后沿用。续跑时步数从运行里已累计的数开始。
- **插话与收尾**：用一把小锁让插话和收尾互斥——决定收尾之后到的插话被拒绝（`60003`），不会出现「运行已标成功、Pi 又开始新一轮」；收尾前再确认 Pi 的队列里没有未处理的输入。**说明**：目前的测试（插话落在模型正在输出的时候）去掉这段队列检查也通过，说明 Pi 的 `waitForIdle` 本身已经把排队的插话算在内；这段检查是防御性的，我没能构造出只有它能挡住的场景。

**和 Go 保持一致的细节**：审批 24 小时有效且只在「决定时」检查——Go 里没有任何代码把等待超过 24 小时的运行标成 `expired`（状态和提示文案存在，但没有触发条件），所以过期的审批只是不能再决定，运行停在等待状态，用户可以取消。这里保持同样的行为，没有凭空加清扫任务。

**测试**（真的 Pi Harness + 假 Go；假 Go 新增审批解析、执行接口）15 条新增：批准全流程（调用内容、已花积分、事件顺序、模型下一次请求里的真实结果）、拒绝（含追加预算不生效）、回答提问、部分批准、执行失败、执行接口连不上、决定不合法、各种不能决定的情况、已过期、重复点击并发；继续的四种状态、限制、画布被占、准备失败；插话落在收尾瞬间。连跑 8 次无失败。变异检查：不用占位替换、不检查过期、追加预算对拒绝也生效、步数不放宽、额度用完不要求追加、执行不重试，每项都有测试失败。

**没做、留给后面**：3c 压缩、启动恢复和正常停机（20.6，会用到这里的 `interrupted` 和 `resume`）；3.3 的 REST 层。

### 20.13 3.2-3c：压缩、启动恢复、正常停机（2026-10-25）

**压缩**（`beforeCompact` 钩子）
- 历史超过触发线时 Pi 调钩子，我们用**画布专用的摘要提示词**（目标 / 要求 / 已完成 / 进行中 / 关键决定 / 不能丢的数据）让模型写摘要，复用原来 `compaction.ts` 的提示词和序列化（工具结果和参数截短）。摘要请求走平台的模型代理，带运行上下文和 `X-Agent-Purpose: summary`，Go 照常计费、检查额度。
- **再次压缩时增量更新**：Pi 把上一份摘要套了一层英文说明放回上下文，我们取出里面的正文，作为 `<之前的摘要>` 并进新摘要。
- **触发线按模型的上下文窗口算**（`compactionLimits`，窗口小的模型收紧），用 Pi 的 `modelOverrides` 按模型配置。后台压缩关闭：写摘要要有运行的上下文，运行结束后没有。
- **摘要不是助手的话**：不发 `message.done`、不推增量；它的调用 id 单独放（`onCall` 带用途），否则会错配到下一条对话消息的积分上。摘要调用结算出来有扣费就推一条 `run.usage`（上下文大小只看对话调用）。
- **摘要失败**（上游出错、返回空）：不压缩，运行照常继续，这个运行片段里不再重试。**实测抓到一个真 bug**：摘要请求的 502 被记成了「模型出错」，把整个运行判成失败。错误回调现在区分用途。
- 实测：Pi 估算上下文大小时会用上一次回答报告的真实用量，所以触发取决于上游报的 `prompt_tokens`；中文接近一字一个 Token，Pi 自己的估算（3.5 字符一个）偏低——原来的 `compaction.ts` 取了两者较大的，Pi 的钩子没法这样。万一漏触发，上游会报上下文溢出，Pi 有 `overflow` 原因的压缩兜底。上线后看真实模型的 `context_tokens` 再调 `reserve`。

**租约、启动恢复、正常停机**（设计文档 20.6 的实现）
- **租约**：运行期间每 10 秒续一次（`lease_until = 现在 + 30 秒`），启动时 `停机时长 = 现在 − (lease_until − 30 秒)`。不用事件时间：模型长时间思考时可能一直没有事件。
- **启动恢复**，在接受任何请求之前，顺序固定：
  1. 读出所有 `queued` / `running` 的运行和 Pi 里没跑完的任务；
  2. **窗口内**（默认 5 分钟）、任务还在、没被重启上限挡住 → 接好（调运行准备、装工具、配置会话），发 `recovering`；
  3. **其余** → 收掉 Pi 里没跑完的任务，标为 `interrupted`；
  4. 不属于任何待恢复运行的活任务也收掉；
  5. 最后才启动调度，窗口内的运行接着往下走。
  等审批、等回答的运行没有活任务，状态在库里，不受影响（有测试：重启后用户仍可决定、运行做完）。
- **「恢复中」只是事件**，不落库状态：库里仍是 `running`，画布没有被放走。第一件真正的事发生之前补一条 `running`。
- **重放的工具不再发第二个 `tool.start`**：恢复前已经落库的 `tool.start` 会被记下，被重放的那次抑制一次（模型可能复用 `tool_call_id`，所以只抑制一次，不是永久去重）。
- **正常停机**：`close()` 只关闭存储，**不中止运行**，也不标中断或失败；停机前最后续一次租约。进行中的 `drive` 因存储关闭而报错时，不当成运行失败。

**实测**（用真的进程 `SIGKILL`，假 Go 在父进程里，应用数据和 Pi 数据在同一个 SQLite 文件）
| 情形 | 结果 |
|---|---|
| 窗口内被杀 | 自动续跑；状态事件序列 `queued → running → recovering → running → succeeded`；工具用**同一个幂等键**重放（两个进程里一样）；只有一个 `tool.start` |
| 超过窗口 | 标为 `interrupted`，Pi 任务收掉，工具不重做；点继续后模型被提醒「可能已经执行」，做完 |
| 一个窗口内、一个已过期 | 窗口内的接着做完，过期的标中断；**不收掉的话，窗口内那个运行装上的工具会把过期任务唤醒成幽灵，悄悄多发一次模型请求**（变异检查证实） |
| 连续被杀 5 次 | 到 Pi 的重启上限，不再自动续跑，标为中断 |
| 正常停机后重启 | 运行留在 `running`、不发任何状态事件；Pi 的任务**没有被记成「被打断」**（`restarts` 为空），所以每次发版不会消耗重启上限的额度（20.10 留下的疑问：用响应中止信号的工具后得到证实） |
| 等审批时被杀 | 不受影响；重启后决定 → 执行 → 结果交给模型 → 运行做完 |
| 刚创建就被杀（没有 Pi 任务） | 直接标为中断 |

**两个更正 / 说明**
- 之前以为「启动时收掉过期任务」单独就有用。实测在只有一个过期运行时它是冗余的：启动时注册表里没有工具，Pi 会自己把「找不到定义」的任务处理掉。它**只在窗口内的运行同时存在时必要**，上面那条测试覆盖。
- 20.10 留下的「被杀时 Go 那边挂起的模型调用会不会结算」：这是 Go 侧的行为，3.0b-B 已测（调用方断开仍按已产生的部分结算，用不会被取消的上下文）。Agent 被杀等价于调用方断开。

**测试**：压缩 4 条、恢复 7 条（真进程）；agent 共 224 条。恢复 + 引擎共 41 条连跑 6 次无失败。变异检查：窗口不起作用、过期不收任务（混合场景）、不抑制重复 `tool.start`、停机时也收尾、不看重启上限、不发「恢复中」，每项都有测试失败。

**3.2 到此完成**，引擎还没接进 `serve.ts`。下一步 3.3：REST 接口 + 12 类事件的黄金文件对比，以及前端 `recovering` 的小改。

### 20.14 3.3 的拆分和 3.3a：会话侧的四个平台接口（2026-10-25）

**现状盘点**：前端用的 agent 接口一共 15 个——会话 5 个（列表、新建、改名、删除，加模型清单）、事件 2 个（回放、历史）、运行 4 个（开始、插话、取消、继续）、撤销 1 个、审批决定 1 个、评价 2 个。方案二里会话、运行、事件、审批归 Agent 服务，这些接口由它实现；Go 只做反向代理（3.4）。但有四件事只有 Go 能做：

| 平台接口 | 为什么在 Go |
|---|---|
| `POST /internal/platform/v1/canvases/check` | 画布归属在 Postgres。别人的和不存在的一样是「画布不存在」 |
| `GET /internal/platform/v1/agent/models` | 模型清单来自模型注册表（管理员后台配置并发布） |
| `POST /internal/platform/v1/messages/check` | 消息里引用的节点、模型、技能是否有效，要读画布、注册表和技能库；同时把选中的节点里已不存在的去掉（界面状态过期不拦发送）。规则就是发起运行时用的 `checkRefs`，不是复制 |
| `POST /internal/platform/v1/runs/:run_id/undo` | 撤销所依据的改动记录（`agent_mutations`）和画布都在 Go。运行是不是自己的、是不是已经结束由持有运行记录的 Agent 服务校验，Go 只按运行上下文做画布上的事 |

**拆分**：3.3a（本节，Go）→ 3.3b（TypeScript：REST 层本体 + 与 Go 输出逐项对照的黄金文件）→ 3.3c（评价、接进 `serve.ts`、前端 `recovering` 的小改）。

**做了什么（3.3a）**
- 四个桥方法 `CheckCanvas` / `AgentModels` / `CheckMessage` / `UndoPlatformRun`，对应上面四个接口；ID 一律是十进制字符串，都要服务令牌。
- **撤销抽出共用核心 `UndoRun`**：令牌模式的 `Undo`（先查运行记录、校验状态）和平台的撤销共用；既有的撤销测试一条没改全部通过。
- 空模型清单是 `[]` 不是 `null`（前端据此显示「没有可用模型」）；空的选中列表同样。

**测试**：service（引用校验的 5 种错误与 `StartRun` 报的是**同一句话**、撤销的全过程和重复撤销、没改过画布时 `Revision` 为 0——调用方据此不发 `run.undone`、别人的画布不被动）；handler（雪花大小的 id 按字符串解析、各种非法请求、业务错误码原样保留）；路由（都只认服务令牌）；**真实 HTTP + Postgres**（画布归属、清单、消息引用读真实画布、先经平台写节点再撤销，画布回到原样，重复撤销是 `60007`）。
