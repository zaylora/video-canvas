# Agent 运行观测与调优

> - **范围**：记录线上每次画布 Agent 运行的完整轨迹（对话、工具调用、画布改动、审批、用户反馈），提供给 Claude Code / Codex 读取分析，找出 Agent 没完成好的需求，持续改进 Agent Harness（系统提示词、工具声明与实现、技能、运行时参数）。
> - **日期和状态**：2026-10-21，一期已实现（后端、用户端反馈、令牌页、本地 CLI 和 skill），二期（后台运行列表与详情）未做。与设计的出入见第 13 节。
> - **证据约定**：**事实**附文件路径或来源链接；**推断**是基于事实的判断；**提案**是尚未实现的设计。
> - **方案演进**：没有转向。依赖选型考虑过 Langfuse 自托管、OTel + Phoenix，用户选择自研（见 4.3、9）。
> - **配套演示**：[Agent运行观测-演示.html](Agent运行观测-演示.html)

## 1. 设计摘要

- **问题**：Agent 已经上线，用户用起来会遇到不顺手的地方。仓库已经把运行数据记下了大部分，但后台看不到，AI 也读不到，所以现在没法系统地知道“哪些需求 Agent 没做好、为什么没做好”。
- **目标**：把线上运行变成 AI 能批量读取的结构化轨迹。在仓库里一条命令就能拉取数据、逐条判定、归类失败，最后定位到 Harness 的具体改动点；改完上线后，按 Harness 版本对比指标，确认是否真的变好。
- **推荐结论**（提案）：
  1. **采集补齐**：在现有 PostgreSQL 上补齐以下内容，字段命名参照 OTel `gen_ai.*`。
     - 工具调用逐条落库
     - 模型调用补充耗时、渠道和 stop_reason
     - 每次运行记录 Harness 版本
     - 每次运行生成一条长期保留的摘要
     - 点赞 / 点踩反馈
  2. **只读导出接口**和**分析令牌**：令牌由后台签发，只能读、会过期，每次拉取都写审计。
  3. **本地 CLI `agent-trace`**：把运行拉到 `.traces/`，每次运行一份 Markdown，另有 JSON 和索引文件。
  4. **分析 skill `/agent-review`**：Claude Code 和 Codex 通用，流程是逐条判定 → 失败归类 → 定位到代码 → 输出报告。
  5. **版本对比**：`agent-trace compare` 按 Harness 版本对比点踩率、撤销率、失败结束率和 AI 判定失败率。
  6. **后台页面后做**：先给最简的令牌管理页，再做运行列表、详情时间线和版本对比卡片。

## 2. 项目现状

### 2.1 事实

**运行时**
- Agent 每个运行片段都由 Go 起一个 Node 子进程 `node main.mjs`。启动参数和 `steer` / `abort` 控制消息走 stdin。证据：`backend/internal/service/agent/runtime.go:161-214`，`agent/src/main.mjs:7-37`。
- Node 通过 bridge 调后端，接口有 `/internal/agent/bridge/{v1/chat/completions, tool, tool-event, state, finish}`，鉴权用一次性 Bearer 令牌，有效期 2 小时。证据：`backend/internal/router/bridge.go:20-25`，`backend/internal/handler/agent/bridge.go:98-113`。
- 模型调用经过 Go 侧的 `ProxyModel`，依次是：取快照、预检预算、`llmgateway.StreamRaw`、结算、`recordMessage`。证据：`backend/internal/service/agent/bridge.go:176-236`。
- 工具有 14 个：canvas_get_state、canvas_apply_ops、canvas_arrange、canvas_delete、plan_update、ask_user、model_list、generate_media、task_get、skill_search、skill_read、canvas_inspect_image、web_search、web_fetch。另外 Node 自带一个 `codemode`。证据：`backend/internal/service/agent/tool_decls.json`，`agent/src/agent-tools/codemode.mjs:22`。
- 工具执行前写 `tool.start`，执行后写 `tool.end`。`tool.end` 只带 `id`、`name`、`is_error`、`summary`、`parent_id` 和 `extra`，**不带参数和完整结果**。证据：`backend/internal/service/agent/bridge_tools.go:120-134`。

**落库**
- 表结构证据：`backend/internal/model/agent.go:88-207`。
  - `agent_sessions.session_jsonl`：pi 的完整消息数组。每轮由 Node 的 `/state` 整体覆盖，压缩后早期内容会丢；不含 system 消息和图片。
  - `agent_runs`：状态、步数、积分、错误码、错误文案（512 字）。
  - `agent_events`：按 seq 落库。会落库的类型有 message.user（steer）、message.done、tool.start / tool.end、plan.updated、approval.*、run.status、run.usage。增量消息只推送、不落库。
  - `agent_mutations`：每次画布写入一条，撤销时写 `kind=undo`，并给原记录打 `undone_at`。
  - `agent_approvals`：审批和提问的决定。
  - `agent_model_calls`：每次模型调用的 token、积分和已脱敏的错误（255 字）。
- 系统提示词版本 `prompts.Version = 9` 写在代码常量里，没有落库。证据：`backend/internal/agent/prompts/prompts.go:14`。
- 运行中固定的技能版本只放在内存里，以 `name@vN` 的形式写进 tool.end 的 summary。证据：`backend/internal/service/agent/bridge.go:78`，`backend/internal/agent/skills/skills.go:96-99`。

**鉴权、审计和日志**
- 后台接口统一用 `JWTAuth` + `middleware.RequireAdmin`，没有 API Token 机制。证据：`backend/internal/router/router.go:167-323`，`backend/internal/middleware/admin.go:22`。
- 审计表 `admin_audit_logs` 只增不改不删。证据：`docs/design/日志管理/日志管理.md`。
- 有 RequestID 中间件。证据：`backend/internal/middleware/request_id.go`。
- 迁移用编号 SQL，当前最大编号是 `0007_task_queue.sql`。证据：`backend/migrations/`。

**前端与后台**
- 后台菜单里 Agent 相关的只有“技能”。证据：`web/src/.../admin-nav.ts:39`。
- 后台没有任何查看 Agent 会话或运行的页面和接口。

### 2.2 设计约束
- 渠道 Key 不进 Node 进程（`bridge.go:84-85`），上游错误里的 Key 会遮成 `***`（`llmgateway/gateway.go:207`）。除 Key 以外的内容按原文保存，这是日志管理约束 C1。
- 图片地址写进 `session_jsonl` 前一律去掉（`agent/src/context.mjs:51`），画布目录也隐藏了 src、outputs 等字段。导出必须保持这条边界。
- 后台读接口统一用 RequireAdmin，审计只增不删。
- 会话不写进画布 `payload_json`（`model/agent.go:86` 的注释），所以本设计**不触碰画布 payload 和 revision**。

### 2.3 当前缺口
1. 后台没有 Agent 运行的查看接口和页面，AI 也拿不到数据。
2. 工具参数和完整结果没有按次留存，工具耗时也没有记录。
3. 模型调用没有记耗时、首字耗时、渠道、上游模型和 stop_reason。
4. 运行没有记 Harness 版本（提示词版本、工具声明、技能版本、构建号），所以无法按版本对比。
5. 没有显式的用户反馈；撤销本轮不产生事件。
6. Node 的诊断信息在进程正常退出时丢弃；Go 和 Node 之间没有关联键。
7. `agent_events` 注释写了按保留期清理，但代码里没有清理任务。本设计只给新增的大字段加清理，原有事件的清理不在范围内，见 11.4。

## 3. 用户与场景

- **用户**：项目维护者，同时是 Agent Harness 的开发者，在本仓库里用 Claude Code 或 Codex 调优。频率是每周或每次发版前跑 1–2 次分析。
- **触发场景**：
  - 发版后想知道新提示词有没有变好。
  - 想找出最近一周用户最常遇到的不顺。
  - 某个用户反馈“Agent 不懂我”，想看那几次运行。
- **主任务**：`/agent-review --since 7d`，依次拉取、判定、归类、出报告，再挑高频问题改 Harness，发版后用 `agent-trace compare` 验证。
- **成功指标**（提案）：
  - 从开始到拿到一份按频次排序、每条附证据的失败报告，≤ 15 分钟（示意，与样本量有关）。
  - 报告中每个失败类别都能对应到 Harness 的具体文件或明确的“产品能力缺失”。
  - 新版本上线后，能用 ≥ 30 次已反馈或已判定的运行，对比出点踩率和 AI 判定失败率的变化。

## 4. 调研范围

访问日期均为 2026-10-21。

### 4.1 主流方案

| 方案 | 数据模型 | 反馈与评分 | 从线上挖失败 | 给 AI 读 | 自托管与许可证 | 对本项目 |
| --- | --- | --- | --- | --- | --- | --- |
| [Langfuse](https://langfuse.com/docs/evaluation/scores/data-model) | observation（span、generation、tool…）归入 trace，trace 归入 session | score 挂在 trace、observation 或 session 上；来源分 API、EVAL、ANNOTATION | 无自动聚类，靠 [Error Analysis 指南](https://langfuse.com/academy/monitoring/error-analysis) | 官方 [MCP](https://langfuse.com/docs/api-and-data-platform/features/mcp-server) 和 CLI | 核心 MIT；需要 PG + ClickHouse + Redis + S3 | 功能最全，运维最重 |
| [LangSmith](https://docs.langchain.com/langsmith/insights) | run tree + thread | feedback；online evaluator | Insights 多层聚类 | 远程 MCP、`langsmith-fetch` | 闭源 SaaS；自托管只在企业版 | 不适合 |
| [Arize Phoenix](https://arize.com/docs/phoenix/self-hosting) | OTel / OpenInference span | 标注 | 无自动聚类 | MCP | ELv2；单进程，可用 PG | 运维轻，功能少 |
| [Braintrust](https://www.braintrust.dev/docs/observe/topics) | logs / spans | scorers | Topics 每日聚类，Loop / Patterns | 内置 Loop | 闭源 SaaS | 自动化最强，不适合 |
| [OTel GenAI 约定](https://github.com/open-telemetry/semantic-conventions-genai/blob/main/docs/gen-ai/gen-ai-agent-spans.md) | invoke_agent → chat / execute_tool 等 span | — | — | — | 规范，处于 Development 状态 | 用于字段命名 |

方法论来源：
- [Anthropic Clio](https://www.anthropic.com/research/clio)：抽取 facet → 语义聚类 → 生成去隐私描述 → 组织成层级。
- [Hamel Husain：evals FAQ / error analysis](https://hamel.dev/blog/posts/evals-faq/)：open coding ≥ 30 条 → axial coding → 统计频次 → 对高频失败做检查或 judge 并校准。
- [Anthropic：Demystifying evals for AI agents](https://www.anthropic.com/engineering/demystifying-evals-for-ai-agents)：先读 transcript，评测任务从真实失败里来。

**比较结论**（推断）：
- 业界的共同做法是：完整轨迹 + 显式和隐式反馈 → 人或 AI 读样本、建立失败分类 → 按频次修 → 固化成检查或评测 → 按版本复盘。
- 托管平台的价值主要在 UI、标注队列和自动聚类。本项目的第一读者是本地 AI，所以这些价值可以由“结构化导出 + 分析 skill”替代大半。

### 4.2 比较维度

| 维度 | 自研（PG + 导出 + skill） | Langfuse 自托管 | OTel + Phoenix |
| --- | --- | --- | --- |
| 新增运维 | 无 | ClickHouse、Redis、S3，外加 2 个容器 | 1 个服务 |
| 与现有表的匹配度 | 直接复用 `agent_*` 表 | 需要另外上报一份 | 需要另外上报一份 |
| AI 读取 | 自定导出格式，最贴合 Claude Code 和 Codex | MCP / CLI | MCP |
| 人工浏览 UI | 需要自研（放到二期） | 现成 | 现成 |
| 自动聚类 / judge | 由本地 skill 完成 | judge 需要自己配 | 无 |
| 数据出库 | 只到维护者本地 | 到另一套服务 | 到另一套服务 |
| 将来迁移 | 字段按 `gen_ai.*` 命名，可加 OTel exporter | — | — |
| 核实度 | 已核对项目代码 | 调研未复核 | 调研未复核 |

### 4.3 依赖框架调研
- 候选：Langfuse（MIT）、Arize Phoenix（ELv2）、OpenTelemetry Go SDK（Apache-2.0）。
- **用户决定**：自研，复用现有 PostgreSQL，不引入以上依赖。字段命名参照 OTel `gen_ai.*`，为以后接入 OTel exporter 留出口，不预先引入 SDK。
- CLI 用 Node 写，不加第三方依赖（只用 `fetch`、`fs`）。理由是仓库里已有 Node 环境（`agent/`），Claude Code 和 Codex 都能直接运行。

## 5. 设计原则

1. **AI 是第一读者**：导出格式先保证 AI 能顺序读懂一次运行的来龙去脉（用户要什么 → Agent 怎么想、做了什么 → 结果怎样 → 用户满不满意），后台页面是辅助。依据：用户回答。
2. **复用已落库的数据，只补缺口**：不另建一套 trace 系统，也不改画布 payload。依据：项目事实 2.1、用户选择自研。
3. **每次运行都能对上 Harness 版本**：没有版本，就无法证明改动有效。依据：用户选择版本对比。
4. **大字段短期留，摘要长期留**：完整轨迹保留 30 天，运行摘要和反馈长期保留，支撑版本对比。依据：用户回答。
5. **分析发生在本地，线上只提供只读数据**：线上不调用模型做判定，不消耗额度。依据：用户选择按需手动分析。
6. **判定要有证据**：AI 的每条判定都必须引用到具体步骤号，失败类别要能落到 Harness 的组件上。依据：error analysis 方法论。

## 6. 推荐方案（提案）

### 6.1 整体链路

```
用户在画布上用 Agent ──► 后端采集（补齐后的 agent_* 表 + 反馈 + 运行摘要）
                                │  只读导出接口 /admin/agent/runs*（JWT 或分析令牌）
                                ▼
        本地仓库：agent-trace pull ──► .traces/（index.jsonl + runs/*.md + runs/*.json）
                                ▼
        /agent-review skill：逐条判定 ─► verdicts.jsonl ─► 失败归类 ─► 报告 + Harness 改动建议
                                ▼
        改提示词 / 工具 / 技能 ─► 发版（prompt_version、tools_hash、build_sha 变化）
                                ▼
        agent-trace compare --by harness ─► 新旧版本指标对比 ─► 回到第一步
```

### 6.2 采集补齐（后端）

1. **Harness 版本**：运行创建时写入 `agent_runs`，字段如下。
   - `prompt_version`：取 `prompts.Version`。
   - `tools_hash`：`tool_decls.json` 的 sha256 前 12 位。codemode 的声明在 Node 侧（`agent/src/agent-tools/codemode.mjs`），不在 Go 里，所以不进这个哈希；改了 codemode 说明请同时改 `prompts.Version`。
   - `build_sha`：构建时注入的 git commit（`pkg/version.GitCommit`）。**事实**：`backend/Makefile` 和 `backend/Dockerfile` 都已用 ldflags 注入；本地 `go run` 没有注入（值是 `none`），此时记空串。
   - `harness_key`：前三项拼接后再哈希，取 12 位，用于分组。
   - 运行中固定的技能版本，在运行结束时写进运行摘要。
2. **工具调用逐条落库**：新表 `agent_tool_calls`。在 `bridge_tools.go` 写 tool.start / tool.end 的同一位置插入和更新。
   - 记录参数和结果。结果超过 32KB 时截断，并把 `result_truncated` 置为 true。
   - 记录耗时。
   - 脚本里发起的子调用通过 `parent_id` 关联。
3. **模型调用补字段**：`agent_model_calls` 增加 `purpose`（main / summary，取自请求头 `X-Agent-Purpose`）、`channel_key`、`upstream_model`、`latency_ms`、`ttft_ms`、`stop_reason`。
4. **撤销产生事件**：撤销本轮时额外写一条 `run.undone` 事件，payload 为 `{run_id, mutations}`。
5. **Node 诊断**：Node 的诊断（压缩失败、保存历史失败等）本来就写 stderr。进程每次退出（不论正常还是异常），stderr 末尾 4KB 都写进运行摘要的 `runtime_tail`（30 天后清空）。**本期不做** `runtime.notice` 事件：stderr 末尾已经能覆盖这些诊断，不值得再加一个桥接口；导出器遇到这类事件仍会渲染，将来加了不用改导出。
6. **关联键**：桥的访问日志带 `run_id`，由 Go 从一次性桥令牌解析（令牌就是运行的身份），**不靠 Node 自报请求头**。运行本身就是 trace，`trace_id` 直接用 `run_id`，不另造 ID。
7. **运行摘要 `agent_run_digests`**：运行进入终态时生成（upsert），续跑后再次进入终态会覆盖。反馈、撤销发生时更新对应字段。长期保留。

### 6.3 用户反馈（前端用户端）

- 每轮运行结束后，在 `run-summary` 末尾显示 👍 / 👎。
  - 👍 直接提交。
  - 👎 展开原因标签（多选，可不选）和一个可选的文字框（≤ 500 字）。原因标签有：没理解我的需求、画布改错了、步骤太多或太慢、生成结果不好、其他。
- 可以改选或取消：再次点同一个按钮就是取消，删除记录。
- 只能对自己的、已进入终态的运行反馈。运行中按钮禁用，并提示“运行结束后可评价”。
- 失败提示“评价没保存，稍后再试”，按钮恢复原状。

### 6.4 只读导出接口与分析令牌

- 导出接口挂在 `/admin/agent/runs` 下，同时接受两种凭证：
  - 管理员 JWT（RequireAdmin）
  - 分析令牌：前缀 `vca_`，scope 是 `agent:read`
- 分析令牌：
  - 签发：管理员在后台签发。明文只显示一次，库里只存 sha256。
  - 有效期：默认 30 天，最长 90 天。
  - 失效：可随时注销；签发人被降权后，令牌随之失效。
  - 审计：签发、注销、每次导出调用都写 `admin_audit_logs`。导出调用按“令牌 + 查询条件 + 返回条数”记一条。
  - 限流：每个令牌每分钟 120 次（本地 CLI 一次导出一次请求；原定 60 次，评估后放宽）。
- 导出内容**按原文**（用户决定），但要守住三条边界：
  - 渠道 Key 继续是 `***`。
  - 不包含图片签名 URL，与 `session_jsonl` 的规则一致。
  - 用户只导出 `user_id` 和用户名，不导出邮箱和手机号。

### 6.5 本地 CLI `agent-trace`

- 位置：`scripts/agent-trace/agent-trace.mjs`。只用 Node 18+ 内置能力，无第三方依赖。
- 配置：从环境变量 `VC_API_BASE` 和 `VC_TRACE_TOKEN` 读取，也可以写进 `.traces/.env`（gitignore）。
- 命令：
  - `pull [--since 7d] [--until] [--feedback down|up|any|none] [--undone] [--status failed,canceled,...] [--harness <key>] [--prompt-version 9] [--user <id>] [--limit 200] [--sample 50]`
    - 先拉摘要列表，再逐条拉导出，写到 `.traces/`。
    - 已经拉过、且 `digest.updated_at` 没变的运行跳过。
    - `--sample N` 在命中的运行中随机抽 N 条，并保证点踩、撤销、失败的运行优先入选（分层抽样，见 6.6）。
  - `show <run_id>`：在终端打印一次运行的 Markdown。
  - `compare --by harness|prompt_version|model [--since 30d]`：拉取服务端聚合指标，与本地 `verdicts.jsonl` 合并，输出对比表。样本少于 30 时标“样本不足”。
- 目录结构（整个 `.traces/` 加入 gitignore）：

```
.traces/
  index.jsonl              # 每行一条运行摘要（agent_run_digests 的导出）
  runs/2026-10-20_r1234.md # AI 读的单次运行文档
  runs/2026-10-20_r1234.json # 同一运行的完整结构化数据（工具参数/结果不截断到 2KB）
  verdicts.jsonl           # /agent-review 写入的逐条判定
  reports/2026-10-21.md    # 分析报告
```

- 单次运行 Markdown（`format=md`，后端生成，CLI 原样保存）分段如下：
  1. 头部 YAML：run_id、时间、用户、模式、模型、harness（prompt_version / tools_hash / build_sha / skills）、状态、步数、积分、耗时，以及信号（feedback、reasons、undone、approvals_rejected、steer_count、tool_errors）。
  2. **用户请求**：本轮用户原文。另附同会话前 2 轮的用户原文和 Agent 结论，作为上下文。
  3. **时间线**：按 seq 编号 `#1 #2 …`。
     - 每步一行类型，可能的类型有：模型回复（含 thinking 摘要，只取前 600 字）、工具调用（名称、参数、结果，各截到 2KB，标明是否截断，附耗时）、审批（内容、用户决定）、用户插话、运行时提示。
  4. **画布改动**：每条 mutation 的种类、改动条数、是否被撤销。
  5. **结束**：终态、错误码和文案、Agent 的最后一条回复、用户反馈原文。
- 截断规则：Markdown 里单段最多 2KB，JSON 里最多 32KB（与落库上限一致）。超过 30 天的运行，工具参数和结果显示为“已过保留期”。

### 6.6 分析 skill `/agent-review`

- 位置：`.claude/skills/agent-review/SKILL.md`；`AGENTS.md` 里写一行引用，供 Codex 使用。
- 失败分类：放在 `docs/agent-eval/failure-taxonomy.md`，纳入版本管理。初版是**种子分类**，前 1–2 次分析要先开放编码，再收敛分类。
- 步骤：
  1. 运行 `agent-trace pull`，参数默认是 `--since 7d --sample 60`。分层抽样：点踩、撤销、失败结束、拒绝审批的运行全部入选（最多 40 条），其余随机抽满。
  2. **逐条判定**：每条运行写一行到 `verdicts.jsonl`，字段如下。
     - `run_id`、`harness_key`
     - `goal`：一句话复述用户要什么
     - `outcome`：achieved / partial / failed / unclear
     - `failure_codes[]`：取自 taxonomy；新问题写 `new:<短名>`
     - `evidence`：步骤号加一句话说明
     - `component`：prompt / tool_desc / tool_impl / skill / runtime / model / product_gap / user_side
     - `suggestion`：改进建议
     - `confidence`
  3. **归类**：统计各 failure_code 的频次。出现 ≥ 3 次的 `new:*` 建议并入 taxonomy，由人确认后提交。
  4. **定位**：对出现频次排前 5 的类别，读相关代码（`backend/internal/agent/prompts/system.md`、`tool_decls.json`、`tool_registry.go`、技能正文等），给出具体的改动建议，并引用 2–3 条样本运行作为证据。
  5. **出报告**：`reports/<日期>.md`，包含样本说明、结论摘要、失败类别表（频次、占比、样本、组件、建议）、做得好的地方、以及建议的下一步。
- 规则：
  - 判定必须引用步骤号。
  - 拿不准时判 `unclear`，不硬判。
  - 报告里不复制大段用户原文，只引用步骤号和必要的短句。
  - 报告和 verdicts 不提交进仓库。
- 种子分类（提案，示意，需在首次分析中修正）：

| code | 含义 | 常见组件 |
| --- | --- | --- |
| misread_intent | 没理解或曲解用户需求 | prompt |
| over_ask / under_ask | 该问没问，或问了太多 | prompt |
| wrong_tool | 选错工具或参数错误 | tool_desc |
| tool_failure | 工具报错或结果不符合预期 | tool_impl |
| canvas_mess | 画布改动错误、布局混乱或改了不该改的 | tool_impl / prompt |
| incomplete | 没做完就结束 | runtime / prompt |
| loop_waste | 重复调用、步数或积分浪费 | prompt / runtime |
| skill_gap | 缺技能或技能写法不对 | skill |
| gen_quality | 生成的图片或视频质量不达预期 | model / product_gap |
| product_gap | 产品当前不支持 | product_gap |

### 6.7 版本对比

- 服务端接口 `GET /admin/agent/stats?group_by=harness_key|prompt_version|model_key&since&until`，返回每组的以下指标：
  - 运行数
  - 反馈率
  - 点踩率：点踩数 ÷ 有反馈的运行数
  - 撤销率：被撤销的运行数 ÷ 有画布改动的运行数
  - 失败结束率：终态为 failed，或因步数、预算耗尽而暂停的运行占比。具体状态集合实现时按 `Run*` 常量确定。
  - 中位耗时、中位步数、平均积分
- CLI 本地合并 `verdicts.jsonl` 后，再算 AI 判定失败率：(failed + partial) ÷ 已判定数。
- 任一组的样本少于 30 时，只显示数值，不给“变好 / 变坏”的结论。

### 6.8 后台页面（二期）

在后台菜单的 Agent 分组下新增“运行记录”：
- **列表**：筛选条件有时间、状态、反馈、是否撤销、Harness 版本、模型、用户。列有：时间、用户、请求首句、状态、步数、积分、👍/👎、撤销。
- **详情抽屉**：时间线按步骤展示，与导出的 Markdown 同源，可折叠工具参数和结果。按钮有“复制为 Markdown”“下载 JSON”。
- **版本对比卡片**：最近 3 个 harness_key 的核心指标。
- **分析令牌**（一期就做，最简版）：令牌列表、签发、注销。

## 7. 状态与异常

| 场景 | 行为 |
| --- | --- |
| 工具调用落库失败 | 只打 Warn 日志，不影响运行（与 `AddRunUsage` 失败时的处理一致）；导出里该步显示“未记录参数” |
| 运行被中断（租约过期、进程崩溃） | 恢复流程把运行置为终态时同样生成摘要；`runtime_tail` 保留 stderr |
| 续跑（审批后、继续） | 同一个 run_id 再次进入终态时覆盖摘要；时间线自然连续 |
| 反馈提交失败 | 前端提示并恢复按钮；可重试；接口幂等（每个 run 和用户只有一条） |
| 对运行中或别人的运行反馈 | 前者返回 `ErrAgentRunNotEnded`，后者返回 `ErrAgentRunMissing`（60012，不暴露是否存在） |
| 令牌过期、注销，或签发人降权 | 401，CLI 提示“令牌失效，请在后台重新签发” |
| 限流 | 429，CLI 退避重试 3 次后停止，已写的文件保留 |
| 超过 30 天 | 摘要和反馈仍可导出；工具参数和结果、`runtime_tail` 显示“已过保留期” |
| 运行被删（会话软删除） | 导出仍可读，摘要里标 `session_deleted: true`。删会话是用户行为，本身也是信号 |
| 拉取中断 | 重跑 `pull` 按 `updated_at` 增量补齐 |
| AI 判定没把握 | 记为 `unclear`，计入报告里的“需人工看”清单 |

## 8. 数据与工程影响（提案）

迁移：新增 `backend/migrations/0008_agent_observability.sql`，沿用编号 SQL 机制。

### 8.1 新表 `agent_tool_calls`

| 字段 | 类型 | 说明 |
| --- | --- | --- |
| id | bigserial PK | |
| run_id | bigint not null | 索引 `(run_id, started_at)` |
| session_id | bigint not null | |
| tool_call_id | varchar(64) not null | 与 `(run_id, tool_call_id)` 一起组成唯一键 |
| parent_id | varchar(64) default '' | 脚本里发起的子调用 |
| name | varchar(64) not null | gen_ai.tool.name |
| args_json | jsonb null | 调用参数；过保留期后置为 NULL |
| result_text | text null | 返回给模型的内容，≤ 32KB；过保留期后置为 NULL |
| result_truncated | bool default false | |
| is_error | bool default false | |
| summary | varchar(255) default '' | 与 tool.end 的 summary 一致 |
| started_at / ended_at | timestamptz | |
| duration_ms | int default 0 | |
| purged_at | timestamptz null | 大字段被清理的时间 |

### 8.2 `agent_runs` 增加字段

`prompt_version int default 0`、`tools_hash varchar(12) default ''`、`build_sha varchar(40) default ''`、`harness_key varchar(12) default ''`，并对 `(harness_key, created_at)` 建索引。

### 8.3 `agent_model_calls` 增加字段

`purpose varchar(16) default 'main'`、`channel_id bigint default 0`、`upstream_model varchar(128) default ''`、`latency_ms int default 0`、`ttft_ms int default 0`、`stop_reason varchar(32) default ''`。

### 8.4 新表 `agent_feedback`

| 字段 | 类型 | 说明 |
| --- | --- | --- |
| id | bigserial PK | |
| run_id | bigint not null | 与 `(run_id, user_id)` 一起组成唯一键 |
| user_id | bigint not null | |
| rating | smallint not null | 1 或 -1 |
| reasons | jsonb default '[]' | 字符串数组，取值：misread / wrong_edit / slow / bad_output / other（不用 `text[]`：与项目里其他数组字段一致，GORM 不用额外类型） |
| comment | varchar(500) default '' | |
| created_at / updated_at | timestamptz | |

### 8.5 新表 `agent_run_digests`（长期保留）

| 字段 | 类型 | 说明 |
| --- | --- | --- |
| run_id | bigint PK | |
| session_id / user_id / canvas_id | bigint | 对 `user_id` 建索引 |
| mode / model_key | varchar | |
| prompt_version / tools_hash / build_sha / harness_key | | 冗余存一份，方便直接分组；索引 `(harness_key, ended_at)` |
| skills | jsonb default '{}' | 形如 `{"script-breakdown":"v3"}` |
| status / error_code | varchar | 终态 |
| request_head | varchar(200) | 用户请求的前 200 字，用于列表 |
| steps / tool_calls / tool_errors / model_calls | int | |
| input_tokens / output_tokens / spent_credits | int | |
| duration_ms | int | 从 created_at 到 ended_at |
| approvals_total / approvals_rejected / steer_count / mutation_count | int | |
| undone | bool | |
| feedback | smallint default 0 | 1、-1 或 0（未反馈），与 agent_feedback 同步 |
| session_deleted | bool | 导出时计算，不落库也可以 |
| runtime_tail | text null | stderr 末尾 4KB，30 天后置为 NULL |
| ended_at / updated_at | timestamptz | 对 `ended_at` 建索引 |

### 8.6 新表 `admin_api_tokens`

| 字段 | 类型 | 说明 |
| --- | --- | --- |
| id | bigserial PK | |
| name | varchar(64) | 用途备注 |
| prefix | varchar(12) | 明文的前 8 位，用于在列表里辨认 |
| token_hash | char(64) unique | sha256，**不存明文** |
| scope | varchar(32) | 本期只有 `agent:read` |
| created_by | bigint | 签发人；鉴权时校验签发人仍是管理员 |
| expires_at | timestamptz not null | |
| last_used_at / revoked_at | timestamptz null | |
| created_at | timestamptz | |

### 8.7 接口

| 方法与路径 | 说明 | 业务错误 |
| --- | --- | --- |
| `PUT /agent/runs/:rid/feedback` | 用户端。body 为 `{rating:1\|-1, reasons?, comment?}`；返回 feedback。会写事件 `feedback.set`，并更新摘要 | ErrAgentRunMissing（60012，别人的运行也返回它）、ErrAgentRunNotEnded（60014）、参数错误 |
| `DELETE /agent/runs/:rid/feedback` | 用户端，取消反馈，幂等 | ErrAgentRunMissing（60012） |
| `GET /admin/agent/runs` | 摘要列表。参数：since、until、status、feedback、undone、harness_key、prompt_version、model_key、user_id、cursor、limit（≤ 500）。返回 `{items, next_cursor}` | 参数错误 |
| `GET /admin/agent/runs/:rid` | 详情 JSON：digest、events、tool_calls、model_calls、mutations、approvals、feedback、上下文轮次 | ErrAgentRunMissing（60012） |
| `GET /admin/agent/runs/:rid/export?format=md\|json\|both` | AI 可读导出，见 6.5。`md` 返回 `{format, content}`，`json` 直接返回结构化对象，`both` 返回 `{format, content, data}`（本地 CLI 用它，一次请求拿全） | ErrAgentRunMissing |
| `GET /admin/agent/stats` | 版本对比聚合，见 6.7 | 参数错误 |
| `GET/POST /admin/agent/tokens`，`DELETE /admin/agent/tokens/:id` | 令牌管理。POST 只返回一次明文；签发和注销写审计 | 参数错误（有效期超过 90 天） |

鉴权：`/admin/agent/runs*` 和 `/admin/agent/stats` 用新中间件 `AdminOrAnalysisToken`。它先认 `vca_` 前缀的令牌，查 hash，校验有效期、注销状态、scope 和签发人角色；认不出令牌时退回 JWT + RequireAdmin。令牌管理接口只接受 JWT。

分层落位（遵循 `backend/AGENTS.md` 的 router → handler → service → repository）：
- handler：`handler/agent/admin_runs.go`
- service：`service/agent/observe.go`（摘要生成、导出拼装、统计）
- repository：`repository/agent_observe.go`
- 清理任务：`service/agent/observe_cleanup.go`，参照 `login_log_cleanup.go`，每天执行一次。它把 30 天前的 `args_json`、`result_text`、`runtime_tail` 置为 NULL，并写 `purged_at`。

### 8.8 伪代码（提案）

```go
// bridge_tools.go：工具执行前后
call := Repo.InsertToolCall(run, toolCallID, parentID, name, args, now)   // 失败只 Warn
out, herr := spec.run(...)
Repo.FinishToolCall(call.ID, truncate(res.Content, 32<<10), res.IsError, summary, since(start))

// run 进入终态（finish / fail / cancel / 中断恢复）
digest := observe.BuildDigest(run)   // 汇总 tool_calls、model_calls、approvals、mutations、steer 事件
Repo.UpsertDigest(digest)

// 撤销本轮
Repo.UndoRun(...); emit("run.undone", ...); Repo.MarkDigestUndone(run.ID)
```

### 8.9 实现难点
- 结果截断要和返回给模型的内容一致：记录的是返回给模型的文本，而不是原始工具输出。
- 摘要里的 `steer_count` 和 `approvals_rejected` 从 events 和 approvals 统计。续跑会多次进入终态，需要保证 upsert 幂等。
- `build_sha` 依赖构建注入：已核实 Makefile 和 Dockerfile 都已注入，无需改动。
- `tools_hash` 在启动时算一次并缓存。

## 9. 方案取舍

默认权重来自用户回答：AI 可读性和闭环速度最高，其次是运维成本和数据不外流，人工 UI 最低。

| 维度（权重） | 自研（推荐） | Langfuse 自托管 | OTel + Phoenix |
| --- | --- | --- | --- |
| AI 可读与闭环速度（5） | 5 | 4 | 3 |
| 新增运维（4） | 5 | 2 | 4 |
| 复用现有数据（3） | 5 | 2 | 2 |
| 人工浏览 UI（2） | 2 | 5 | 4 |
| 自动评分能力（2） | 3（本地 skill） | 4 | 2 |
| 加权合计（满分 80） | **70** | 52 | 49 |

明确放弃的东西：
- 后端定时自动判定（用户选择按需手动）。
- 离线回归评测集（用户选择版本对比）。
- 用户逐个授权和导出去标识（用户选择原样导出）。
- 隐式信号的自动打标规则：撤销、拒绝、插话等只作为导出字段，由 AI 判定时参考，不另建打标系统（用户在信号题中选了点赞点踩和 AI 全量判定）。
- MCP Server。

## 10. MVP 与后续

### 一期：采集 + 反馈 + 导出 + CLI + skill
- 内容：迁移 0008；工具调用、模型调用字段、Harness 版本、摘要、`run.undone`、stderr 末尾（`runtime_tail`）的采集；用户端 👍/👎；导出接口和 stats；分析令牌（最简后台页）；`agent-trace`；`/agent-review` 和种子 taxonomy；清理任务。
- 验收：
  1. 跑完一次包含工具调用的运行后，`agent_tool_calls` 每次调用都有一条记录，参数、结果和耗时齐全；摘要的 harness 字段非空。
  2. 用户能点 👍/👎、改选、取消；运行中按钮禁用；别人的运行返回 404。
  3. `agent-trace pull --since 1d` 能用分析令牌拉到 Markdown，文件里没有渠道 Key 和图片签名 URL；注销令牌后返回 401。
  4. `/agent-review` 能产出 verdicts 和报告，每条判定都引用步骤号。
  5. 清理任务把 30 天前的大字段置空，摘要不受影响。
  6. 后端测试放 `backend/internal/tests/`，覆盖反馈的权限、导出的脱敏、令牌的过期和注销、摘要 upsert 的幂等；前端测试覆盖反馈按钮的各种状态。
### 二期：后台运行记录
- 内容：列表、详情时间线、版本对比卡片。
- 验收：能按反馈、撤销、Harness 版本筛选；详情与导出的 Markdown 内容一致。
### 后续（不排期）
- 后端每日 judge
- 回归评测集
- OTel exporter 接入 Langfuse 或 Phoenix
- 自动聚类（Clio 式）

## 11. 待确认问题与已确认事项

### 11.1 已确认（用户回答，2026-10-21）
1. 第一读者是 AI，人为辅。一期先做导出，后台只先做最简的部分。
2. 采集路线自研，复用现有 PG，不引入 Langfuse、Phoenix 或 OTel SDK。
3. AI 读数据的方式：本地 CLI 拉成文件，再配一个分析 skill。
4. 失败信号：点赞点踩 + AI 全量判定。
5. 隐私：原样导出（Key 和图片签名 URL 仍按现有规则不出现）。
6. 分析节奏：按需手动运行 skill，线上不调用模型做判定。
7. 闭环验证：按 Harness 版本对比指标，不建离线评测。
8. 保留期：完整轨迹 30 天，摘要和反馈长期保留。

### 11.2 已搁置的决定
- 后端每日自动 judge：将来启用时，可直接复用 taxonomy 和 verdict 字段。
- 失败样本沉淀为回归用例：需要回放环境。
- 用户授权与导出去标识：如果对外合规有要求再启用，在导出层加开关即可。

### 11.3 默认值（不需要用户决定）
- 结果截断：落库 32KB，Markdown 2KB。
- 令牌有效期：默认 30 天，最长 90 天；限流每分钟 120 次。
- 默认样本：最近 7 天抽 60 条，信号样本最多 40 条。
- 样本少于 30 时不下结论。

### 11.4 仍待确认
- 用户协议里是否需要补一句“对话用于改进服务”：这属于产品或法务决定，不阻塞实现。
- `agent_events` 原有的 90 天清理是否一并补上：不在本设计范围内，建议另开任务。

## 12. 来源与假设

- 外部来源（访问于 2026-10-21）：
  - https://langfuse.com/docs/evaluation/scores/data-model
  - https://langfuse.com/docs/api-and-data-platform/features/mcp-server
  - https://langfuse.com/academy/monitoring/error-analysis
  - https://docs.langchain.com/langsmith/insights
  - https://arize.com/docs/phoenix/self-hosting
  - https://www.braintrust.dev/docs/observe/topics
  - https://github.com/open-telemetry/semantic-conventions-genai/blob/main/docs/gen-ai/gen-ai-agent-spans.md
  - https://www.anthropic.com/research/clio
  - https://hamel.dev/blog/posts/evals-faq/
  - https://www.anthropic.com/engineering/demystifying-evals-for-ai-agents
- 外部调研由子任务检索完成，未逐条复核页面，比较表中标注为“调研未复核”。
- 项目文件：见第 2 节。行号来自 2026-10-21 的 master（7baa1a3）。
- 假设：
  - 样本量足够支撑周度分析：每周有数十次运行，未验证。
  - 维护者本地有 Node 18+。
- 成功指标里的时间数字是示意。

## 13. 实现记录

> 2026-10-21 一期实现。后端测试、迁移一致性测试和 lint 通过；前端、CLI 各自的测试见下。

### 13.1 已实现
- **迁移** `backend/migrations/0008_agent_observability.sql`：`agent_runs`、`agent_model_calls` 加列；新表 `agent_tool_calls`、`agent_feedback`、`agent_run_digests`、`admin_api_tokens`。结构一致性测试通过。
- **采集**：
  - Harness 版本在 `StartRun` 时写入运行（`service/agent/harness.go`）。
  - 工具调用逐条留存（`observe_toolcall.go`）：令牌和运行校验通过后才算一次调用，**被拒绝的调用（选错工具、模式不允许、步数用尽）也记**；参数超 64KB 只留 `_truncated` 摘要，结果超 32KB 截断并标记；codemode 的脚本和返回文本由 Node 随报备一起交给 Go。
  - 模型调用补用途、渠道、上游模型、总耗时、首字耗时、结束原因。
  - 运行摘要由 `run.status` 事件钩子在运行离开排队和运行中时生成（`AgentService.afterRunStatus`）；用户反馈、撤销、进程退出也会刷新。
  - 撤销本轮发 `run.undone` 事件（真的写了撤销才发）。
  - 进程退出时 stderr 末尾写进摘要的 `runtime_tail`。
- **用户端反馈** `PUT/DELETE /agent/runs/:rid/feedback`；会话事件里有 `feedback.set` / `feedback.cleared`（载荷里不放原始 run_id，前端靠事件自带的编码 id 对账）。
- **后台只读接口与令牌**：`/admin/agent/runs`、`/runs/:rid`、`/runs/:rid/export`、`/stats`（管理员 JWT 或分析令牌）；`/admin/agent/tokens`（仅 JWT）。令牌访问写审计，管理员自己看不写。
- **清理任务**：每天一次，把 30 天前的工具参数和结果、`runtime_tail` 置空；摘要和反馈长期保留。
- **本地 CLI** `scripts/agent-trace/`、分析 skill `.claude/skills/agent-review/`、种子分类 `docs/agent-eval/failure-taxonomy.md`。
- **前端**：用户端 👍/👎（每轮运行摘要末尾）、后台「分析令牌」页。

### 13.2 与设计的出入
| 设计 | 实现 | 原因 |
| --- | --- | --- |
| `agent_feedback.reasons` 是 `text[]` | `jsonb` | 与项目其他数组字段一致 |
| `agent_model_calls.channel_id` | `channel_key varchar(64)` | 渠道快照里只有 key，没有 id |
| `tools_hash` 含 codemode 声明 | 只含 `tool_decls.json` | codemode 声明在 Node 侧；改了请同时改 `prompts.Version` |
| Node 调桥带 `X-Agent-Run-ID` | Go 从桥令牌解析 run_id 写进访问日志 | 令牌就是运行的身份，不依赖 Node 自报 |
| `runtime.notice` 事件 | 不做 | `runtime_tail` 已覆盖诊断需求 |
| 令牌限流每分钟 60 次 | 120 次 | CLI 一次导出一次请求，留足余量 |
| 导出 `format=md\|json` | 增加 `both` | 减半请求数 |
| 后台菜单有独立的 Agent 分组 | 「分析令牌」放在 AI 配置分组、「技能」之后 | 现有菜单没有 Agent 分组，且 `admin-nav` 测试断言不存在，本期不改菜单结构 |

### 13.3 验证
- 后端：`go test ./...` 全绿（含仓储集成测试、迁移一致性测试、端到端导出测试）；`golangci-lint run --new-from-merge-base=master ./...` 0 issues。
- Node 运行时：`agent/` 下 `npm test` 81 项通过。
- CLI：`scripts/agent-trace` 下 `node --test` 21 项通过（用假 fetch，**未对真实后端联调**）。
- 前端：`bun test` 全量通过，`tsc -b --noEmit` 通过，oxlint 0 个 error（详见交付说明）。
- **未验证**：没有在浏览器里点过 👍/👎 和令牌页；没有用真实令牌从 CLI 拉过线上数据；没有在真实运行里看过 `/agent-review` 的产出质量。这三件需要你实际用一遍。

### 13.4 后续调整（2026-10-22，按使用反馈）
- 分析令牌页：删掉页面顶部说明和底部的 401 提示；「吊销」统一改称「注销」；**注销后的令牌不再出现在列表里**（库里仍保留，鉴权据此拒绝、审计可追溯）。`GET /admin/agent/tokens` 因此不再返回已注销的令牌。
- 签发后的明文令牌改用弹窗显示，只有「我已保存」能关。
- 评价条：运行中不显示，结束后靠左紧贴在这一轮最后一项内容下方。

### 13.5 遗留
- 二期：后台运行列表、详情时间线、版本对比卡片。
- 用户协议里是否补一句「对话用于改进服务」：产品或法务决定，未处理。
- `agent_events` 原有的 90 天清理仍没有（本设计只清新增的大字段），建议另开任务。
- 前端：`ErrAgentRunNotEnded`（60014）还没有专门文案，保存失败统一提示「评价没保存，稍后再试」。
