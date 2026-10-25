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
