# Agent 等待生成结果（后台监听 + 自动续跑）

> - **范围**：画布 Agent 申请的生成（图片 / 视频 / 音频）被批准后，本轮照常结束、释放画布；系统在后台监听这批任务，全部结束后把结果投回同一个会话，自动开一轮新运行让 Agent 验收并推进下一步。涉及 Go（任务归属字段、River 门铃 job、两个平台接口）、Agent 服务（Pi 文档 + Pi 后台监听任务、门铃接口、唤醒运行）和前端（后台监听状态、提醒）。
> - **日期和状态**：2026-10-28，设计已确认；Go、Agent、前端均已实现（未提交，未联调、未在浏览器里看过），与设计的出入见 §13。
> - **证据约定**：**事实**附文件路径；**推断**是基于事实的判断；**提案**是尚未实现的设计。
> - **方案演进**：
>   1. 访谈前半段定为「等待期间运行不结束（新状态 `waiting_tasks`）、占画布、可插话」。之后用户提出「用子代理在后台监听，不影响主 Agent 对话」，确认改为**后台监听、主运行结束**（§11 Q9）。被搁置的方案见 §9.3。
>   2. 用户选项里写的是「监听放到 pi-durable 后台任务」。设计时发现 pi-durable 没有外部唤醒 API（§2.1），一度改为「AgentStore 表 + 引擎定时器」。用户追问「pi-durable 也支持长任务」后重新比较（§9.2），确认改为 **pi-durable 原生**：监听状态放会话级 Pi 文档 `app.watches`，每批一个 Pi 后台任务 `app.task-watch` 负责兜底轮询和交付重试（`sleepUntil`），River 门铃仍是快速路径（Q11）。
>   4. 参考 Claude Code 的后台任务约定，加入「同会话正忙时插进当前轮」和「通知不是用户输入」两条（§4.3，Q10）。
>   3. 「监听」不使用 LLM 子代理：等状态不需要模型，用 LLM 会白花 token。用便宜模型子代理做**验收**列为后续（§10）。
> - **配套演示**：[Agent等待生成结果-演示.html](Agent等待生成结果-演示.html)（方案说明）；[Agent等待生成结果-原型.html](Agent等待生成结果-原型.html)（高保真原型，按现有画布页与 Agent 面板的令牌、组件和文案仿真，可走完整条链）

## 1. 设计摘要

**问题**：Agent 用 `generate_media` 申请生成、用户批准后，Go 只负责提交任务；系统提示词明确让模型「批准后不用等」，本轮随即结束。生成几分钟到十几分钟后才完成，Agent 完全不知道，必须用户手动发消息「图出来了，继续」。所以 Agent 跑不了「出角色图 → 看图验收 → 重做不合格的 → 合格的图生视频 → 再验收」这种多步长任务。

**目标**：生成结束后 Agent 自动醒来，拿到结果继续推进；用户只需要在每次花积分时点批准；等待期间画布和对话完全空闲。

**推荐结论**：

1. **本轮照常结束**。批准执行后，在同一个 Pi 提交里为本批任务登记**后台监听**：写会话级 Pi 文档 `app.watches`，并创建一个 Pi 后台任务 `app.task-watch`（`background: true`，不让会话变忙）。运行正常走到 `succeeded`，画布释放，用户可以继续聊、开别的会话。
2. **Go 在任务进入终态的同一个数据库事务里插入 River job**（`agent.task_settled`），job 调 Agent 内部接口当门铃；Agent 不在线就按 River 退避重试。门铃只说「某任务结束了」，结果醒来时再向 Go 取。
3. **一批全部结束才唤醒**：Agent 向 Go 取结果摘要，在**同一个会话**里新开一轮运行（运行的第一条事件是 `tasks.settled`），把摘要作为带 `requestId` 的输入提交给 Pi（重启重放不会重复唤醒），Agent 在完整上下文里验收、推进。
4. **忙时插进当前轮、被占时排队**：唤醒时如果用户正在**同一会话**里和 Agent 聊（运行 `running`），结果以插话（`whenBusy: "steer"`）插进当前这一轮，Agent 当场验收；如果画布被**另一个会话**占着、或本会话在等审批 / 等回答，先记着，等那次运行结束立刻唤醒。（参考 Claude Code 后台任务通知，§4.3）
5. **Pi 后台任务兜底**：监听任务用 `sleepUntil` 持久睡眠，到点向 Go 查还没收到门铃的任务；整批结束后负责交付，交付不成（画布被占、Go 不可用）就睡 60 秒再试。重启后由 pi-durable 自动续上；门铃丢了最多慢两三分钟。

## 2. 项目现状

### 2.1 事实

| 事实 | 证据 |
| --- | --- |
| 批准生成后，Go 逐条创建任务、绑定节点，把「任务已提交：{tasks…}」作为工具结果交给模型 | `backend/internal/service/agent/generate.go:214`（`Execute`）、`approval.go:258` |
| 系统提示词让模型不等：「批准后任务在后台跑，不用等，也不用反复查询」 | `backend/internal/agent/prompts/system.md:24` |
| 审批结果经 `#feedBack` 交给模型，运行继续；模型回答后运行 `succeeded` | `agent/src/engine/engine.ts:877`、`engine.ts:741-752` |
| 活跃运行状态：`queued / running / waiting_approval / waiting_input` | `agent/src/engine/status.ts:15` |
| 一个画布同时只能有一个活跃运行（部分唯一索引），冲突报 60001 | `agent/src/store/migrations.ts:67`、`engine.ts:527-530` |
| 同一画布可以有多个会话 | `agent/src/store/store.ts:199-204` |
| 等审批时用户不能发新消息（新运行撞 60001，插话要求 running 否则 60003） | `engine.ts:527-530,961` |
| 审批 24 小时「过期」只是 `decide` 拒绝点击，不改运行、不释放画布 | `engine.ts:32,796` |
| 首轮成功后异步命名标题——「运行结束后再调模型」已有先例 | `engine.ts:659`（`#autoTitle`） |
| 任务四种终态都经过 `GenerationTaskService.finish()` 的同一个 GORM 事务 | `backend/internal/service/generation_task_store.go:241,302,329,381,453` |
| 该事务底层是 `*sql.Tx`，River 与 GORM 共用 `*sql.DB`，可在同一事务 `InsertTx` | `repository/generation_task.go:75,86-90`、`initialize/queue.go:31,42` |
| River 只有队列 `generation`、job `gen.task`；`EnqueueTx` 写死 `TaskArgs` 并跳过终态任务 | `provider/queue/queue.go:27-28`、`runtime.go:198-204` |
| `GenerationTask` 没有运行或会话字段；幂等键 `agent-{approvalID}-{idx}` 反推不可靠（用户请求也可带幂等键） | `model/generation_task.go:45-64`、`generate.go:261` |
| Go 已有调用 Agent 的地址与令牌（目前只用于观测读取） | `config/config.go:93`、`service/agent/observe_remote.go:427,455` |
| Agent 引擎没有自定义 Pi 任务，只在内置任务上挂了 hooks | `engine.ts:234-239,1059-1091` |
| pi-durable 没有外部事件唤醒等待中任务的 API；只有 `sleepUntil`、等其它 Pi 任务、宿主 `submit` | `agent/node_modules/@earendil-works/pi-durable/README.md:140-162,695-704`、`dist/types.d.ts:238-292` |
| `submit` 的 `requestId` 在会话内去重，重启后仍有效 | `pi-durable/README.md:129-138` |
| 任务状态已通过用户级 WebSocket `task.updated` 推给前端 | `service/generation_task.go:187` |

### 2.2 约束

- 生成任务库（PostgreSQL）是任务状态唯一事实来源；推送可丢、可重复、可乱序（`docs/design/长任务生成设计/长任务生成设计.md:86-91`）。门铃同样遵守。
- 运行、会话、审批在 Agent 的 SQLite 里，Go 的 AgentRun 只是壳（`service/agent/platform.go:48-53`），监听记录只能放 Agent。
- Agent 单容器；Go 可能多实例，River worker 只在 `ai.worker.enabled` 的实例跑（`runtime.go:150-153`）。

### 2.3 缺口

1. Go 不知道任务属于哪个 Agent 运行。
2. 没有 Go → Agent 的「任务结束」通道。
3. Agent 不记录「本会话在等哪些任务」，也不会自己开一轮运行。
4. 系统提示词教模型不要等。
5. 等审批时运行无限占用画布（现有缺口；用户决定不收尾，改为「发消息即拒绝并说明」，§6.7）。

## 3. 用户与场景

- **用户**：做 AI 短剧、漫剧、广告的创作者（`system.md:6`）。
- **触发场景**：「把这 6 个镜头的分镜图出了，确认后都做成视频」。视频单条 5–20 分钟（示意）。
- **主任务路径**：Agent 建节点、写提示词 → 申请生成（审批卡）→ 用户批准 → 本轮结束，会话显示「后台等待 0/6」→ 用户这期间可以继续聊或做别的 → 全部结束 → **自动开新一轮**：Agent 看图验收 → 申请重做不合格的 + 合格的图生视频（审批卡）→ 批准 → 后台等待 → 自动开新一轮验收视频并汇报。
- **成功指标（提案）**：
  - 一批任务全部结束到唤醒运行开始：门铃正常时 P95 < 10 秒；门铃丢失时 < 3 分钟。
  - 链式任务中用户手动「催」Agent 的次数 = 0。
  - Go 或 Agent 重启期间结束的任务不丢唤醒、不重复唤醒。

## 4. 调研

### 4.1 外部方案（据已有知识整理，本次未联网复核）

| 方案 | 做法 | 启发 | 不适用处 |
| --- | --- | --- | --- |
| Temporal（Signal / 异步 Activity 完成） | 工作流挂起持久化，外部 Signal 唤醒 | 「挂起持久化 + 外部门铃 + 去重」是标准形态 | 引入整套平台，与 River + Pi 重复 |
| Inngest / Trigger.dev `waitForEvent` | 等带匹配条件的事件，超时兜底 | 等待要有兜底；事件只当门铃 | 托管服务 |
| OpenAI Assistants `requires_action` | 运行停在显式状态，宿主提交结果继续 | 本项目审批已是此模式 | 不处理分钟级异步 |
| LangGraph `interrupt` + checkpointer | 中断点持久化，外部 `Command(resume)` 继续 | 结果作为一条输入续接会话 | Python 生态 |
| pi-durable 后台 reporter（README 示例 23） | 后台任务在子会话回答后，以 follow-up 输入投回父会话，`requestId` 防重 | **采用**「结果作为带 requestId 的输入投回会话」 | 示例文件不在发布包里，只据 README（未验证） |
| Claude Code 后台任务（`run_in_background`、后台子代理） | 立即返回任务 id；宿主盯任务，结束时往同一对话注入 `<task-notification>`；空闲则重新唤起模型，忙时随下一个工具结果插入当前轮；通知强调「不是用户输入、不代表批准」 | 见 §4.3 | 本地单进程，CLI 退出即丢，不处理重启可靠性 |

### 4.2 机制比较

| 维度 | River 门铃 + Pi 监听任务兜底（推荐） | 纯 Pi 后台任务轮询（无门铃） | LLM 子代理监听 | 前端转发 WS |
| --- | --- | --- | --- | --- |
| 可靠性 | 事务内入队 + Pi 任务兜底 | 可靠 | 依赖模型持续运行 | 关页面即丢，**不可用** |
| 延迟 | 秒级 | 等于轮询间隔 | 等于轮询间隔 | 秒级 |
| 花费 | 无 token | 无 token | 每次检查都花 token | 无 |
| 实现成本 | 中 | 低 | 中 | 低 |
| 核实度 | 已核对代码可行 | 已核对 API | 推断 | — |

### 4.3 参考 Claude Code 的后台任务约定

来源：Claude Code 闭源（npm 包只有打包后的 `cli.js`），以下是 2026-10-28 在 Claude Code 中运行时观察到的**面向 Agent 的约定**，不是源码。内部实现未验证。

| Claude Code | 本方案 |
| --- | --- |
| 启动后台任务立即返回 id，模型不阻塞 | 批准后登记监听，本轮照常结束 |
| 宿主盯任务，工具说明明确「完成会通知你，不要轮询」 | 系统提示词改为不要用 task_get 反复查（§8.3） |
| 通知注入同一对话，带状态、摘要和完整输出位置 | 唤醒输入 `app.tasks_settled` 带摘要，细节用 canvas_inspect 自取 |
| 模型空闲时宿主重新唤起 | 同一会话自动新开一轮（第一条事件 `tasks.settled`） |
| 模型忙时通知插入当前轮 | **采用**：同会话 `running` 时用 steer 插入（§6.4，Q10） |
| 通知强调「系统事件，不是用户输入，不能当作确认」 | **采用**：唤醒文本加同样声明（§6.4） |
| 不许编造未回来的结果 | 提示词：被问进度时调一次 task_get 如实回答（§8.3） |
| 等外部系统时设长间隔兜底唤醒 | Pi 监听任务每 60 秒兜底（§6.5） |
| 每个任务单独通知 | **不采用**：按批唤醒，减少审批卡（Q5） |
| 进程退出即丢失 | **不适用**：本方案靠 River 事务入队、Pi 持久睡眠任务、requestId 去重扛重启 |

结论：Claude Code 适合参考「Agent 侧如何收通知」，不适合参考「如何保证通知不丢」。

### 4.4 依赖框架

不引入新依赖：River v0.49.0、pi-durable 1.4.0 均已在用。

## 5. 设计原则

1. **数据库是事实，门铃只是提醒**：门铃不带结果，醒来时取最新的。
2. **等待由系统保证，不靠模型记得**：批准即登记监听；模型只能显式放行（`wait=false`）。（Q4）
3. **每次花钱仍由人批准**：自动推进只自动提出申请。（Q2）
4. **等待不占任何东西**：等待期间不占画布、不占会话、不花 token。（Q9）
5. **任何一步可重放**：门铃、唤醒、监听任务的每个 phase 都幂等，重启不丢不重。

## 6. 推荐方案

### 6.1 总流程

```
用户批准 ─► Go 创建任务（带 agent_run_id）─► Agent 一次 Pi 提交：写 app.watches + 建后台任务 app.task-watch ─► 本轮回答后 succeeded，释放画布
                                                     │
任务终态 ─► finish() 事务内 InsertTx agent.task_settled ─► River job ─► POST Agent /internal/v1/watches/task-settled
                                                     │
Agent 记账：该批全部结束？ ── 否 ─► 等
                         └─ 是 ─► 画布空闲？ ── 否 ─► 排队（画布上的运行结束时再试）
                                            └─ 是 ─► 取摘要 ─► 新开唤醒运行 ─► submit(摘要, requestId) ─► Agent 验收推进
兜底：app.task-watch 首次睡到登记后 2 分钟，之后每 60 秒醒一次：查 Go 补记 → 整批结束就尝试交付 → 已交付或已取消就结束自己
```

### 6.2 监听的登记

- `decide` 批准生成、执行成功后，拿 `exec.result.tasks[]`，若审批 payload 的 `wait=true`，在一个 `conv.commit()` 里：① 在会话文档 `app.watches` 里加一批（`batch_id = approval_id`，每个任务 `pending`）；② `tx.createTask(TaskWatch, { batchId }, { background: true })`，把任务 id 记进这批。两步同生共死。
- 提交就失败的条目没有任务，不登记（失败已在审批结果里告诉模型）。
- `generate_media` 新增可选参数 `wait`（布尔，默认 `true`）。提示词约定：只有用户明确说「提交就行，不用管结果」时才传 `false`。
- 登记后给模型的工具结果追加一句：「这批任务结束后系统会自动把结果发给你，现在不用等，可以先结束本轮或继续做别的。」
- 一轮里批准了多批：各自独立监听、各自唤醒；但同一会话的多批若在同一时刻都已结束且尚未交付，**合并为一次唤醒**（§6.4 第 1 步）。

### 6.3 门铃（Go → Agent，经 River）

1. `AgentGenerator.submit` 创建任务时带内部字段 `AgentRunID`（不从用户请求读取），写入 `generation_tasks.agent_run_id`。
2. `finish()` 事务内：`agent_run_id <> 0` 且非试跑、且 Agent 已启用时，`InsertTx` 一个 `agent.task_settled` job（args：`task_id`、`run_id`；按 args 唯一）。插入失败则整笔回滚，任务由 worker 再次处理。
3. Job 在新队列 `agent_notify`（并发 4，不占生成名额）执行：`POST {agent.url}/internal/v1/watches/task-settled`，请求头 `X-Agent-Service-Token`，body `{ task_id, run_id, status }`。
   - 2xx → 完成。404（没有对应监听，如已取消监听或 `wait=false`）→ 完成，不重试。
   - 连不上 / 5xx → 返回错误，按**显式退避**重试：2 秒起翻倍、封顶 10 分钟，`MaxAttempts=25`（约覆盖 3 小时）。

### 6.4 唤醒

收到门铃：

1. 门铃带 `run_id` → AgentStore 查到会话 → 打开 Pi 会话，`conv.commit()` 里把文档中该任务 `pending → settled`、记 `task_status`（Pi 提交是串行原子的，重复门铃无副作用；文档里没有这个任务或该批已取消 → 404）。若该批已无 `pending`，该批状态改为 `deliverable`，并尝试唤醒该会话。

尝试唤醒（按会话加锁）：

1. 取该会话所有「可交付」且未交付的批，合并。
2. 画布上已有活跃运行：
   - 是**本会话**的运行且状态为 `running`（用户正在聊）→ 取摘要后 `conv.submit({ type: "input", whenBusy: "steer", content: 唤醒文本, requestId })`，在当前工具步之后交给 Agent；复用引擎现有插话机制（`a.steers`、`steer.read` 事件），时间线显示为系统条目而不是用户插话；`delivered_run_id` 记当前运行。（Q10）
   - 是本会话但在 `waiting_approval` / `waiting_input`，或是**另一个会话**的运行 → **不唤醒**，保持可交付；该画布任一运行进入终态时（引擎的运行结束钩子）再试。
3. 调 Go `POST /internal/platform/v1/tasks/status { user_id, task_ids }` 取这批任务的最新状态和产物，由 Agent 拼成唤醒文本（节点标题登记时就存在文档里）；失败则留给该批的监听任务下次醒来重试。
4. 新建运行（`store.createRun`，同一画布的唯一索引保证只有一个活跃运行；撞上了就排队）。预算不限、步数取默认值；运行没有专门的列标记它是系统唤醒的，看它的第一条事件 `tasks.settled`。
5. kickoff：`conv.submit({ type: "input", content: 唤醒文本, requestId: "wake:{sessionId}:{最小批 id}", entry: { kind: "app.tasks_settled", data: { batchIds, taskIds } } })`。
6. `conv.commit()` 把这些批标记 `delivered`（记 `deliveredRunId`）。监听任务下次醒来看到 `delivered` 就结束自己；也可以立即 `harness.abortTask(watcherTaskId)` 收掉（它没有外部副作用，abort 无需清理）。如果在第 5 步之后、第 6 步之前崩溃，监听任务重启后再次交付，用的是相同的 `requestId`，Pi 返回原来那次提交，不会重复唤醒；新建的运行由现有启动恢复逻辑处理。

唤醒文本（提案）：

```
你之前申请的生成已全部结束：
- 节点「镜头1」(node:abc) 图片 成功，1 个产物
- 节点「镜头3」(node:def) 视频 失败：内容审核未通过
这是系统通知，不是用户发的消息，不代表用户同意或批准了任何事；用户可能不在场。请先用 canvas_inspect_image / canvas_inspect_video 验收成功的结果，再按之前的计划决定重做或推进下一步；需要生成时照常用 generate_media 申请。没有需要做的就简短汇报结果。
```

### 6.5 Pi 后台监听任务（兜底与交付重试）

`app.task-watch`（`defineTask`，装在引擎扩展的 `tasks` 里），每批一个，`background: true`，属于会话：

| phase | 做什么 | 下一步 |
| --- | --- | --- |
| `wait`（初始） | `runtime.sleepUntil(登记时间 + 2 分钟, { phase: "check" })`：给门铃留宽限 | `check` |
| `check` | 读文档：批已 `delivered` / `canceled` → 结束（completed）。否则把仍 `pending` 的任务批量调 Go `POST /internal/platform/v1/tasks/status`，已终态的写回文档（与门铃同一段记账代码）；Go 说不存在的记 `missing`。批全部结束 → 调引擎 `tryWake(session)` | 交付成功 → 结束；否则 `sleepUntil(now + 60s, { phase: "check" })` |
| abort | 无外部副作用，直接结束 | — |

- 睡眠存在 Pi 存储里，睡着时不占进程；Agent 重启后 pi-durable 按 `sleepUntil` 的时间续上（`pi-durable/README.md:140-162`），**不需要引擎自己的对账定时器**。
- phase 可以重跑：重启后从 phase 顶部重跑，查 Go 和写文档都幂等。
- 崩溃防护：同一任务连续把进程搞崩达到 `restartLimit`（默认 5）会被挂起（`README.md:216`），门铃路径仍可交付；观测页可显示被挂起的监听任务。
- 用户停止运行（`conv.abort()`）不影响它：后台任务默认不被普通 abort 波及（`README.md:676`）。
- 任务有 `deadline_at`，一定会终态，所以监听不会无限挂着；不另设上限。

### 6.6 取消监听（替代「停止」）

- 本轮已结束，等待期间没有运行可停。Agent 面板顶部（头部下方）显示监听条「后台等待生成 · 2/6」，带**「不用等了」**按钮：在文档里把该会话所有未交付的批标记 `canceled`，并用 `harness.abortTask()` 收掉它们的监听任务；之后的门铃返回 404。生成任务**不取消**，产物照常回填节点、照常扣费。（Q8 原则：只停 Agent，任务继续）
- 唤醒运行开始后，用户点停止，行为同普通运行。
- 删除会话：其监听一并取消（会话级 `abort(context, { background: true })` 或逐个 `abortTask`）。

### 6.7 等审批时发消息

等审批（`waiting_approval`）时，在输入框发消息 = **拒绝本次审批 + 补充说明**：走 `decide(reject, answer=消息)`，运行继续，模型看到「用户拒绝了这次生成。用户的说明：…」（`approval.go:220` 已支持）。`waiting_input`（ask_user）时发消息 = 回答。（Q7）

界面上**不用新做**：等审批时输入框已经被决定框替换（`web/src/pages/canvas/agent/decision-dock.tsx`），它的第 3 项「拒绝，并告诉 Agent 怎么改…」就是这个行为（`decision-dock.tsx:92`）；提问卡的「其他…」同理。上面说的后端转换只用来兜底其它客户端。2026-10-28 画原型时扫码确认。

### 6.8 提醒（前端）

- 运行进入 `waiting_approval` / `waiting_input`：标签标题前加「⏸ 待批准 · 」；页面在后台时发浏览器通知「{画布名}：Agent 需要你批准一批生成（预估 N 积分）」，点击通知聚焦标签页并打开 Agent 面板。
- 唤醒运行开始、页面在后台时：通知「{画布名}：生成已完成，Agent 正在验收」。
- 有后台监听时：标题加「⏳ 2/6 · 」。
- 通知权限在用户第一次批准生成时请求，不在页面加载时弹；被拒就只改标题。关了页面收不到（Q6）。

### 6.9 界面

- 监听条放在 **Agent 面板顶部、头部下方**（用户 2026-10-28 看原型后指定；不放输入框顶沿，因为等审批时输入框被决定框替换，放顶部两种状态下都看得到）：`后台等待生成 · 2/6 已结束`，点开向下展开每个任务（节点名、状态），点击定位节点；右侧「不用等了」。
- 画布被其它运行占着、唤醒在排队时：监听条显示「已全部完成，等当前运行结束后 Agent 继续」。
- 唤醒运行在消息流里以系统条目开头「生成全部结束，Agent 继续」（`app.tasks_settled`），不显示成用户消息。
- 等审批时输入框占位文字：「发送会拒绝这次申请并告诉 Agent 你的想法」。

## 7. 状态与异常

| 情形 | 行为 |
| --- | --- |
| 任务成功 / 失败 / 超时 / 取消 | 都算 settled，摘要写清状态和原因 |
| Agent 宕机期间任务结束 | River 退避重试门铃；Agent 起来后 Pi 监听任务按睡眠时间醒来也会补 |
| Go 宕机 | 门铃 job 在库里，恢复后发；取摘要失败由监听任务 60 秒后重试 |
| 门铃重复 / 乱序 | CAS 记账，只看「是否全部 settled」 |
| 唤醒提交后崩溃 | `requestId` 去重，不重复唤醒 |
| 唤醒时用户正在同会话聊天（运行 running） | 以 steer 插进当前这一轮，Agent 当场验收 |
| 唤醒时本会话在等审批 / 等回答 | 排队，运行结束后唤醒 |
| 插话提交后崩溃 | 同一 `requestId` 去重；若当前运行已结束，监听任务按普通唤醒新开一轮 |
| 监听任务反复崩溃被挂起 | 门铃路径仍可交付；观测页提示，可 `retryTask` |
| 唤醒时同画布另一个会话在跑 | 排队，那次运行结束后唤醒 |
| 等待期间用户手动重新生成同一节点 | 原任务照常 settled；Agent 验收以 `canvas_get_state` 读到的画布为准 |
| 模型传 `wait=false` | 不登记监听 |
| 唤醒运行里又申请生成 | 新审批，批准后新监听，循环 |
| 链条无限循环 | 每次生成都要人批准，花钱有人把关；token 开销由每轮预算与步数限制 |
| 试跑任务 / Agent 未启用 | 不入队门铃 |
| 用户点「不用等了」后门铃到达 | 404，job 完成 |
| 唤醒运行失败（模型错误等） | 同普通运行失败，用户可「继续」；批已标记交付，不重复唤醒 |
| 会话压缩 / fork | `app.watches` 是文档不是对话记录，压缩不影响；fork 出的新会话从空监听开始（`fork: "initial"`） |

## 8. 数据与工程影响（提案）

### 8.1 Go

`generation_tasks` 新增：

| 字段 | 类型 | 含义 |
| --- | --- | --- |
| `agent_run_id` | `bigint not null default 0` | 发起它的 Agent 运行 id（Go 里运行 id 是 uint64），0 表示不是 Agent 发起的；部分索引 `WHERE agent_run_id <> 0` |

- `model.CreateGenerationTaskReq` 加 `AgentRunID string \`json:"-"\``，只由 `AgentGenerator.submit` 填写。
- 迁移走 `backend/internal/migration` 现有机制。

River：

| 项 | 值 |
| --- | --- |
| 队列 | `agent_notify`，`MaxWorkers=4` |
| kind | `agent.task_settled` |
| args | `{ task_id: uint64, run_id: string }`，`UniqueOpts{ ByArgs: true }` |
| 重试 | `MaxAttempts=25`；自定义 `RetryPolicy`：通知 job 用 2 秒起翻倍、封顶 10 分钟的退避，其它 job 沿用 River 默认 |
| 注册 | `ai.worker.enabled` 且 `agent.enabled` 的实例 |

改动点：`provider/queue/queue.go`（新 Args）、`runtime.go`（注册 worker、`rc.Queues` 加队列、新增 `EnqueueAgentNotifyTx`）、`repository/generation_task.go`（`GenerationTaskTx`、`TaskEnqueuer` 加方法）、`service/generation_task_store.go`（`finish()` 事务内入队）、`repository/queue.go`（后台队列统计包含新队列；sweep 不需要管它，丢失由 Pi 监听任务兜底）、新增 `service/agent/notify.go`（调用 Agent，参照 `observe_remote.go`）。

平台 API 新增（Agent → Go，`/internal/platform/v1`，按 `X-Agent-Context` 的用户隔离）：

| 方法 路径 | 请求 | 返回 | 业务错误 |
| --- | --- | --- | --- |
| `POST /tasks/status` | `{ user_id: string, task_ids: string[1..50] }`（id 都是十进制字符串，不要运行上下文头） | `{ items: [{ task_id, node_id, kind, status, progress?, error?, credits?, outputs?: [{ media_type, asset_id?, text? }] }] }`，顺序与请求一致；不存在或不属于该用户的 `status="missing"` | 400 参数 |

只有这一个接口：对账查询和唤醒取结果是同一份数据，**没有单独的 digest 接口**；节点标题不由 Go 给，登记监听时从审批内容里取、存在 `app.watches` 里。

### 8.2 Agent（pi-durable + SQLite）

**Pi 文档 `app.watches`**（`defineDoc`，和 Pi 的表在同一个 SQLite 文件，表前缀 `pi_`）：

```ts
const Watches = defineDoc<WatchesDoc>({
  kind: "app.watches", version: 1, scope: "conversation",
  history: "latest", fork: "initial",
  initial: () => ({ batches: {} }),
  checkpointWhen: () => false,
});
interface WatchesDoc {
  batches: Record<string /* 审批 id */, {
    runId: string;               // 发起它的运行
    watcherTaskId: string;       // app.task-watch 的 Pi 任务 id
    createdAt: number;
    state: "waiting" | "deliverable" | "delivered" | "canceled";
    deliveredRunId?: string;     // 交付给哪次运行（唤醒的新运行，或插话时的当前运行）
    tasks: Record<string /* 生成任务 id */, {
      nodeId: string;
      status: "pending" | "settled";
      taskStatus?: "succeeded" | "failed" | "expired" | "canceled" | "missing";
      settledAt?: number;
    }>;
  }>;
}
```

- 已结束（`delivered` / `canceled`）超过 7 天的批，在下次写文档时顺手清掉，防止文档无限变大。
- 进度对外推送：每次提交后由引擎发 `watch.updated` 事件，走现有事件推送（比接 `harness.watchDoc` 省事）。

**Pi 任务 `app.task-watch`**（`defineTask`，`version: 1`）：输入 `{ batchId, sessionId }`，checkpoint `{ phase: "wait" | "check" }`，行为见 §6.5。装在引擎新建的扩展 `engine-watch` 的 `tasks` 字段里；重启时必须先装好扩展再 `harness.resume()`（`README.md:472`）。

**AgentStore（SQLite）**：**不加列、不建表**。运行状态不新增，活跃运行唯一索引不变；唤醒新开的运行靠它的第一条事件 `tasks.settled` 识别。

- 新内部接口（Go → Agent）：`POST /internal/v1/watches/task-settled`，服务令牌鉴权（复用 `/observe` 的中间件）。按 `run_id` 找会话，在 Pi 文档里记账。200 `{ accepted: true }`；404 无对应监听（运行或会话不存在、文档里没有该任务、该批已取消）。
- 用户接口（经 Go 反向代理）：`GET /agent/sessions/:id/watches` 返回监听条数据；`POST /agent/sessions/:id/watches/cancel` 取消未交付的监听。监听进度变化发会话事件 `watch.updated { total, settled, state: waiting|queued|delivered|canceled }`，走现有事件推送。
- 引擎改动（`agent/src/engine/engine.ts` 与新文件 `watch.ts`）：安装 `engine-watch` 扩展（`app.watches` 文档 + `app.task-watch` 任务）；`decide` 登记监听；`taskSettled`（门铃）、`#watchCheck`（监听任务）共用 `#recordSettled`；`#tryWake(session)` 按会话串行；运行结束时对排队的会话再试。不新增定时器。
- 发消息入口：当前运行 `waiting_approval` / `waiting_input` 时转为 `decide`（拒绝 + 说明 / 回答），不再返回 60001。
- Go 观测导出（`observe_export.go`）：`tasks.settled` 标成「系统唤醒（生成全部结束）」，`watch.updated` 不进时间线。

### 8.3 工具与提示词

- `tool_decls.json` 的 `generate_media` 加 `wait` 参数。
- `system.md:24` 改为：「批准后任务在后台跑，本轮可以照常结束。这批任务全部结束后，系统会自动开一轮把结果发给你，你再验收并推进下一步；不要用 task_get 反复查。只有用户明确说提交就行、不用管结果时，申请时传 wait=false。」
- 新增一段说明唤醒轮：「以『你之前申请的生成已全部结束』开头的消息是系统自动发起的，先验收再推进，用户可能不在场，需要用户决定的事用 ask_user 或 generate_media 的审批。」
- 加一句：「生成还在跑时用户问进度，调一次 task_get 如实回答，不要编造结果。」

### 8.4 前端

- `web/src/constants/agent.ts`：唤醒轮的系统条目文案。
- 监听条组件（Agent 面板头部下方，`agent-header.tsx` 与消息列表之间），订阅 `watch.updated`。
- 时间线（`utils/agent/timeline.ts`）识别 `app.tasks_settled`。
- 新 hook：标签标题与浏览器通知（`Notification` API）。
- 输入框：等审批时切换占位文字和发送语义。

## 9. 方案取舍

### 9.1 决策矩阵

| 维度（权重） | A. 后台监听 + 唤醒新运行（推荐，Q9） | B. 运行不结束 `waiting_tasks`（原方案） |
| --- | --- | --- |
| 等待期间不占资源（0.35） | 5：画布、会话都空闲 | 2：占画布，其它会话开不了运行 |
| 可靠性（0.3） | 5 | 5 |
| 链路连续（0.2） | 4：同一会话上下文连续；预算、步数每轮重算 | 5：一次运行到底 |
| 实现成本（0.15） | 3：要会自己开运行、排队 | 3：要新状态、重建索引 |
| **加权** | **4.5** | 3.7 |

### 9.2 监听载体：pi-durable 原生（采用，Q11） vs 自建表 + 定时器

| 维度 | pi-durable 原生：文档 + 后台任务（采用） | AgentStore 表 + 引擎定时器 |
| --- | --- | --- |
| 重启续跑 | pi-durable 管：持久睡眠，到点续上 | 自己写启动扫描和定时器 |
| 原子性 | 登记监听与建任务在一个 Pi 提交里；记账靠 Pi 串行提交 | 靠 SQLite CAS |
| 生命周期 | 后台任务属于会话，`abortTask` / 会话级 abort 收尾；崩溃循环自动挂起 | 自己处理 |
| 可观测 | Pi 任务图里能看到每个监听任务的状态 | 查表 |
| 门铃快速路径 | 有（引擎记账后直接交付）；Pi 任务不能被外部提前叫醒，只做兜底 | 有 |
| 代价 | 本项目第一次用 `defineTask` / `defineDoc`；文档和任务的版本要跟着迁移；按运行反查要先打开会话 | 和现有运行、审批的写法一致，查问题直接看 SQL |

用户追问「pi-durable 也支持长任务」后选了原生方案。也考虑过去掉 River 门铃、纯靠 Pi 轮询：放弃，因为延迟只能等于轮询间隔（30–60 秒），门铃能做到秒级。

### 9.3 已搁置的原方案：运行不结束

批准后运行进入新状态 `waiting_tasks`，仍占画布，等待期间可插话，停止即取消等待。用户先确认后又改为 A（Q3 → Q9）。保留的理由：如果以后要求「一次目标一个预算」，可以回到 B；对应的状态机设计见本文件 git 历史的第一版。

### 9.4 明确放弃

- LLM 子代理做监听：等状态不需要模型。
- 前端收到 `task.updated` 后转告 Agent：关页面就失效。
- 审批超时自动结束运行（Q7）。
- 取消监听时连生成一起取消（Q8）。
- 站内通知中心、飞书提醒（Q6）。
- 预算内自动批准（Q2）。

## 10. MVP 与后续

**一期（本次，一个 PR）**：§6 全部 + §8 全部。

验收：

1. 让 Agent 出 2 张图：批准后本轮结束，会话显示「后台等待生成 0/2」，画布可以开新运行；2 张都结束后 10 秒内自动开一轮，Agent 验收（模型能看图时调用 `canvas_inspect_image`）。
2. 链式：出图 → 自动唤醒 → 提出图生视频审批 → 批准 → 后台等待 → 自动唤醒并汇报。全程用户只点两次批准。
3. 等待期间在同一会话聊别的：正常对话；生成在这轮进行中结束时，结果作为系统条目插进这一轮，Agent 在同一轮里接着验收。
4. 等待期间另一个会话占着同画布：唤醒排队，那个运行结束后唤醒。
5. 等审批时在输入框发「先别做视频」：审批被拒绝，Agent 收到说明并回应，不报「已有运行」。
6. 点「不用等了」：监听取消，生成仍完成并回填节点，Agent 不唤醒。
7. 等待中重启 Agent 容器：任务结束后仍唤醒，且只唤醒一次。
8. 停掉门铃 worker（模拟门铃丢失）：约 3 分钟内由 Pi 监听任务唤醒。
9. 页面在后台时进入等审批：收到浏览器通知，标题有「⏸ 待批准」。
10. 测试：Go `internal/tests/` 覆盖 finish 入队（含试跑不入队、事务回滚）、门铃 HTTP 分支（2xx / 404 / 5xx）、两个平台接口；Agent `test/` 覆盖登记（文档与任务在同一提交）、重复门铃、合并唤醒、同会话插话交付、画布被占排队、requestId 去重、监听任务在假时钟下的睡眠与重启续跑（pi-durable 支持传入 `now` / `setTimer`，`README.md:164`）、取消监听；前端 `src/tests/` 覆盖监听条、标题、通知触发、输入框语义。

**后续**：

- **子代理验收**：唤醒后先派便宜模型的子代理看图 / 看视频，只把「哪些合格、哪些要重做、原因」交给主 Agent，省主上下文和费用（用户提出子代理想法时讨论过，本期不做）。
- 站内「待你处理」/ 飞书提醒。
- 一批内「失败立即唤醒」策略。
- 观测页展示等待时长与唤醒次数。

## 11. 待确认与已确认

### 已确认（用户回答，2026-10-28）

1. **目标是链式推进多步**：生成完自动验收、自动提出下一批申请。
2. **每次生成都审批**。
3. ~~等待生成时运行不结束、占画布、可插话~~ → 已被第 9 条取代。
4. **系统自动等，模型可放行**（`wait=false` 仅限用户明确说不用管）。
5. **一批全部结束才唤醒一次**。
6. **提醒 = 标签标题 + 浏览器通知**，纯前端。
7. **审批不收尾**：等审批时发新消息 = 拒绝并补充说明。
8. **只停 Agent，生成任务继续**（现在对应「不用等了」和唤醒运行的停止）。
9. **后台监听、主运行结束**：等待期间不占画布和会话，结束后在同一会话自动开新一轮。监听由系统做，不用 LLM。
10. **同会话正在回答时，结果插进当前这一轮**（参考 Claude Code）；另一会话占着画布或本会话在等审批时仍排队。
11. **监听用 pi-durable 原生**：状态放会话级 Pi 文档，兜底轮询和交付重试由 Pi 后台任务做；River 门铃保留为快速路径。

### 已搁置

- 运行不结束的 `waiting_tasks` 方案（§9.3）。
- 预算内自动批准、首批审批整个计划。
- 时间窗合并唤醒、失败立即唤醒。
- 站内通知。
- 审批超时自动结束运行。
- 子代理验收（列入后续）。

### 默认值（不需要用户决定）

- 门铃队列并发 4、`MaxAttempts=25`；监听任务首次睡 2 分钟（门铃宽限）、之后每 60 秒；已结束的批保留 7 天。
- 唤醒运行使用默认预算与步数。
- 同一会话同时可交付的多批合并为一次唤醒。
- `waiting_input` 时发消息当作回答。

### 仍待确认

- 无阻塞实现的问题。

## 12. 来源与假设

- 项目文件：§2.1 各行路径（2026-10-28 读取，分支 `release/v0.5.4`，含未提交改动）。
- pi-durable 1.4.0 README（`agent/node_modules/@earendil-works/pi-durable/README.md`）；示例 23 不在发布包内，**未验证**。
- 外部方案据已有知识，本次**未联网复核**。
- ~~River v0.49.0 默认退避曲线未核实~~：已改成显式退避（见 §6.3、§13），不再依赖默认曲线。
- 假设：Agent 单容器（`durable.ts` 注释）；多 Agent 分片时门铃要按会话路由，本期不处理。
- pi-durable 的 `defineDoc` / `defineTask` / `background` / `abortTask` 已核对 README 与 `dist/harness/types.d.ts:75,199,755`，本项目还没用过，**未实测**。
- 未验证：唤醒运行用默认预算是否会让用户觉得「每轮预算重算」不直观——上线后观察。


## 13. 实现记录（2026-10-28）

**已实现，未提交，未联调（Go ↔ Agent 没有一起跑过）**；各端自己的测试通过（见下），前端页面没有在浏览器里看过。

### 与设计的出入

| 设计 | 实际 | 原因 |
| --- | --- | --- |
| `agent_run_id varchar(32)` | `bigint not null default 0`，部分索引 `<> 0` | Go 里运行 id 是 uint64，不用转字符串 |
| 通知 job 用 River 默认退避 | 自定义 `RetryPolicy`：2 秒起翻倍、封顶 10 分钟 | 默认曲线没核实；显式的更可控，测试也能缩短起点 |
| 两个平台接口 `tasks/status`、`tasks/digest` | 只有 `tasks/status`，返回节点、种类、状态、错误、产物 | 对账和取结果是同一份数据；节点标题登记时存进文档 |
| `agent_runs` 加 `trigger`、`wake_of` 列 | 不加列 | 少一次迁移；唤醒运行靠第一条事件 `tasks.settled` 识别 |
| 交付后监听任务收尾 | 立即 `abortTask`；但监听任务自己那次醒来触发的交付不 abort 自己（会等自己结束而死锁），让它看到已交付后自己收尾 | 实现中发现 |
| 唤醒文本 | 多一句「这条消息没有附画布目录，要改节点前先用 canvas_get_state 读取」 | 唤醒的那一轮没有画布目录 |
| — | 删除会话时一并取消它的后台等待 | 免得留下没人认领的监听任务 |
| — | 新增 Agent 错误码 60014「没有对应的等待」，门铃回 404 | Go 的通知器把 404 当作已送达，不再重试 |
| 提示词 | 版本 18 → 19；`generate_media` 增加 `wait` 参数；工具声明基准文件同步 | — |

### 改动清单

- **Go**：`migrations/0016_generation_task_agent_run.sql`；`model.GenerationTask.AgentRunID`、`CreateGenerationTaskReq.AgentRunID`（`json:"-"`）；`finish()` 事务内 `EnqueueAgentNotify`；`provider/queue`（`NotifyArgs`、`notifyWorker`、`EnqueueAgentNotifyTx`、`retryPolicy`、队列 `agent_notify`）；`provider.AgentNotifier`；`service/agent/notify.go`（`TaskNotifier`）；`service/agent/platform_tasks.go` 与 `handler/agent` 的 `TaskStatus`；`router/bridge.go` 与 `router/router.go`（两个转发路由）；`GeneratePayload.Wait`；`tool_decls.json`、`system.md`、`initialize/queue.go`。
- **Agent**：`src/engine/watch.ts`（文档、任务、`wakeText`）；`engine.ts`（登记、门铃、交付、`watches`/`cancelWatches`）；`api/service.ts`、`api/http.ts`（`GET /agent/sessions/:sid/watches`、`POST …/watches/cancel`、`POST /internal/v1/watches/task-settled`）；`platform-client.ts`（`taskStates`）；`store.ts`（`sessionByConversation`）。
- **前端**：`utils/agent/watch.ts`、`pages/canvas/agent/watch-strip.tsx`、`hooks/use-agent-notifications.ts`，以及会话状态、时间线、标签标题、控制器的接入。

### 验证

- Go：`go test ./internal/...` 全绿，唯一失败的是 `internal/tests/scripts` 里一条升级脚本测试（Windows 路径形式不匹配，与本次无关）；`golangci-lint run --new-from-rev=HEAD ./internal/...` 0 个新问题。新增测试覆盖：创建任务记下运行 id；成功/失败/超时/取消四种终态都入队门铃，试跑和用户任务不入队，入队失败整笔回滚；River 门铃的送达、退避重试、回滚、未启用、去重（真实 PostgreSQL）；通知器的 2xx/404/5xx/连不上；`tasks/status` 的 handler、业务层和路由令牌；`wait` 默认值；提示词。
- Agent：新增 24 个测试（`watch.test.ts`、`engine-watch.test.ts`、`api-http.test.ts` 与 `platform-client.test.ts` 的补充），覆盖登记、门铃、重复门铃、失败/超时结果、画布被占排队、同会话插进当前轮、等审批时排队、不用等了、门铃丢了由 Pi 监听任务兜底、Go 暂时不可用。全量 `npm test` 283/286 通过，3 个失败是 Windows 路径问题（`E:\E:\…`），与本次无关；`npm run typecheck`、`npm run build` 通过。
- 前端：`bun test` 全量 3425 通过；`oxfmt`、`oxlint` 对改动文件无新问题；`tsc` 有一个与本次无关的既有报错（`text-editor-dialog.tsx`）。

### 遗留

- 没有端到端联调（真起 Go + River + Agent）。
- `defineDoc` / `defineTask` 已在假时钟下验证睡眠、重启前后的行为，但**没有测真实重启**（关掉 Agent 进程再起）。
- 前端页面没有在浏览器里看过；通知权限请求的时机、标签标题是否被别处覆盖需要实测。
- 子代理验收仍未做（§10 后续）。
