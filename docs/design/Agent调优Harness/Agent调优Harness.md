# 画布 Agent：排版修复、调优 Harness 与隐患清单

> - **范围**：三件事。A. Agent 搭画布时节点重叠、组重叠、节点没进组的根因与修法；B. 一套给 AI 编码助手用的 Agent 调优 Harness（轨迹记录 + 命令行跑用例 + 画布结构断言 + 报告）；C. Agent 链路的其他隐患清单。
> - **日期与状态**：2026-10-20，设计已确认，待实现。本轮只出文档，未改代码。
> - **证据约定**：**事实**附文件路径（行号以 2026-10-20 的 `perf/agent` 分支为准）；**推断**是基于事实的判断；**提案**是尚未实现的设计。
> - **方案演进**：问题 1 原本可以直接修，用户选择「先写进设计，稍后再修」。初稿让开发者用命令行 `harness pull` 从生产环境拉取轨迹，用户指出**开发环境连不到生产环境**，于是改成：生产后台生成并下载「轨迹包」（内含报告），再拿到开发环境分析。报告也随之改由后端生成，见 6.B.2；`pull` 已删除。
> - **配套演示**：[Agent调优Harness-演示.html](Agent调优Harness-演示.html)，建议先读。

---

## 1. 设计摘要

| 部分 | 问题 | 结论 |
| --- | --- | --- |
| A 排版 | 节点互相压住，组压住组，组框没框住成员 | 共 4 个根因。主因是后端 `canvasgraph` 把每个节点都按 576×324 排版，而前端节点尺寸随画幅变化。修法：后端照搬前端的尺寸规则，用共享测试向量保证两边一致；组框立刻贴合；本批新建的对象做避让；`canvas_arrange` 支持排列组；前端在成员实际尺寸超出组框时撑开组框并推开相邻对象 |
| B Harness | 看不到 Agent 每一步做了什么，调提示词全靠猜 | 线上每轮运行都记录轨迹：索引存 PostgreSQL，请求和响应原文压缩后存进素材存储，保留 14 天。后台「日志管理 → Agent 运行」只有管理员能用，可以筛选运行记录并下载轨迹包（zip，内含后端生成的 `report.md` 和 `trace.jsonl`），每次下载都留审计；下载下来的包放到开发环境分析。新增 Node 命令行工具 `agent/harness`：用 JSON 写用例，走真实服务跑，Agent 的提问和审批按用例里预写的应答处理；跑完做画布结构断言，在同一种轨迹包的基础上补上断言结果，给 AI 编码助手读 |
| C 隐患 | 用户还没发现的问题 | 共 15 条，高 3、中 6、低 6。最该先修的是一类问题：`parentId` 和 `position` 在合并和撤销时被拆开处理，与 A 同源 |

---

## 2. 项目现状

### 2.1 事实

**Agent 链路**
- Node 运行时在 `agent/src/`，基于 `@earendil-works/pi-agent-core@1.3.0`（`agent/package.json`）。Go 每个运行片段启动一个 Node 进程（`backend/internal/service/agent/runtime.go`）。Node 通过本机回环地址上的桥访问模型和工具，有 5 个端点：`chat/completions`、`tool`、`tool-event`、`state`、`finish`（`backend/internal/router/bridge.go`、`backend/internal/handler/agent/bridge.go:24-37`）。
- 画布编辑由 `backend/internal/agent/canvasgraph` 完成，`Apply` 的规则是全部操作成功才生效（`apply.go:41-55`）。
- 事件写入 `agent_events` 后通过 WebSocket 推送（`service/agent/agent.go:262-279`）。断线后可以用 `GET /api/v1/agent/sessions/:sid/events?after=` 补拉（`router/router.go:123`）。

**排版相关**
- `layout.go:46-52`：`nodeSize` 对所有普通节点都返回 576×324。
- `web/src/utils/canvas/node-frame.ts:12,36-41,80-87`：前端节点框的短边固定为 324，宽高比按以下优先级取：音频固定 1:1；成片实测画幅；`data.mediaAspect`；参数里的 `aspect_ratio` / `ratio`（没填时取模型能力的默认值）；都没有时用 16:9。`video-node.tsx:341-409` 用这个尺寸画图片和视频节点。
- 文本节点由 `NodeCard` 渲染，未传宽度时类名是 `w-xl`，即 576px（`components/canvas/node-card.tsx:263`）。高度按默认 324 推断，未在浏览器里实测。
- `apply.go:49-54`：一批操作全部执行完，才对 `refit` 里的组调用 `fitGroup`。
- `group_ops.go` 的 `createGroup`：没有给成员时，组框取最小尺寸 200×140，位置取 `placeFree()`。
- `layout.go` 的 `placeFree`：新对象放在所有顶层对象最右侧再往右 120，与最上沿对齐。计算时用组当前的 `width`，而这个值可能还没贴合。
- `layout.go` 的 `arrangeTargets`：`nodeIds` 里出现组就报错，所以 `canvas_arrange` 排不了组。
- `prompts/system.md`（版本 9）在「影视链路的搭法」最后写着「搭好后用 canvas_arrange 整理布局」。

**观测和测试**（子代理调研结果，已抽查）
- 已落库的数据：
  - `agent_events`：时间线，按 seq 排序；
  - `agent_mutations`：画布改动，带 `tool_call_id`、`changes`、`revision_before/after`；
  - `agent_model_calls`：每次模型调用的 token、积分和错误（`model/agent.go`）。
- 没落库的数据：
  - 模型请求和响应的原文；
  - 工具调用的参数、返回原文和耗时（`tool.end` 只有一行摘要）；
  - 提示词版本 `prompts.Version`、`model_key`、工具声明 hash、技能版本；
  - 运行输入 `AgentRunInput`。
- `session_jsonl` 每个回合整份覆盖，存的是压缩过、去掉了图片的版本。
- Node 进程的 stderr 只保留最后 4KB（`service/agent/process.go`），stdout 直接丢弃。
- `agent_events` 的注释写着「按保留期清理」，但没有找到清理的实现。
- 测试方面：`agent/test/helpers.mjs` 的 `fakeBridge` 是手写脚本驱动的假模型；`backend/internal/tests/repository/agent_e2e_test.go` 跑的是真 Node、真桥、真库加假 LLM。仓库里没有用例集、评测和录制回放。
- 后台没有 Agent 运行记录页，也没有管理员用的 runs 接口。`web/src/pages/admin/ai/trace/trace-timeline.tsx` 是生成任务试跑用的步骤时间线组件，可以复用。
- 登录方式是 `POST /api/v1/auth/login`。仓库里没有 API Token 机制。管理员审计接口是 `GET /api/v1/admin/logs/audits`（`router.go:301`）。

### 2.2 约束

- 画布保存靠 `revision` 乐观锁；Agent 写画布与用户保存会互相冲突，冲突后由前端做字段级三方合并（`web/src/utils/canvas/graph-changes.ts`）。
- Node 进程里不能有任何密钥（`agent/package.json` 的 description，以及 `initialize/agent.go` 的环境变量白名单）。
- 按项目约定，测试放在 `backend/internal/tests/` 和 `web/src/tests/`。前后端共用的规则用 `backend/internal/tests/testdata/canvasgraph/*.json` 作测试向量（已有 `arrange-cases.json`、`group-cases.json`、`connect-rules.json`）。
- 项目约定不用 Playwright 做端到端测试。Harness 走 HTTP 接口，不驱动浏览器。

### 2.3 缺口

- 后端不知道节点的真实尺寸。
- Agent 不能整理组的位置。
- 没有可回看的执行轨迹，没有回归用例，改了提示词也无法对比改前改后。

### 2.4 需要用户确认的事项

全部已在访谈中确认，见第 11 节。

---

## 3. 用户与场景

- **A 的受益者**：画布用户。他们让 Agent 搭分镜，短剧大多是 9:16，结果却是节点挤成一团，只能自己手动拖开。
- **B 的使用者**：主要是 AI 编码助手（Claude Code / Codex），开发者偶尔看报告。典型流程：
  1. 开发者说「分镜搭建的组老是重叠，调一下」；
  2. 编码助手跑 `storyboard` 用例集，读报告和轨迹，定位是提示词问题还是工具问题；
  3. 修改 `system.md` 或工具，同步提升 `Version`；
  4. 重跑用例，对比通过率。
- **B 的第二个场景**：线上用户反馈「刚才那轮 Agent 乱搞」。管理员在生产后台「日志管理 → Agent 运行」中按用户和时间找到这一轮，点「下载轨迹包」，把 zip 解压到开发环境的 `.harness/imports/<runId>/`，交给编码助手分析。这样做是因为开发环境连不到生产环境。
- **成功指标（提案）**：
  - A：9:16 的分镜用例中，`no_overlap` 和 `members_inside_group` 两项断言全部通过；
  - B：编码助手只靠 `report.md` 加 `trace.jsonl` 就能说出失败发生在第几步、原因是什么，不需要再去查库；
  - C：3 条高优先级问题都有对应的失败测试，修复后测试通过。

---

## 4. 调研

### 4.1 主流方案（访问日期 2026-10-20）

| 方案 | 观察到的事实 | 对本项目的启发 | 不适用之处 |
| --- | --- | --- | --- |
| OpenTelemetry GenAI 语义约定 | 一次 Agent 调用是一个 `invoke_agent` span，下面并列 `chat` 和 `execute_tool` 两类 span。常用字段有 `gen_ai.tool.call.id`、`gen_ai.tool.name`、`gen_ai.provider.name`。消息原文通过 `gen_ai.input.messages` 等字段记录，默认关闭、需要主动开启。所有文档目前都是 Development 状态 | 直接借用它的 span 类型和字段名，将来要导出到 OTel 后端时不必改名 | 字段名还可能变动，所以本项目只借用命名，不引入 SDK。按规范 execute_tool 只记录「工具执行了、成没成功」，工具内部做了什么要靠我们自己补 |
| Langfuse（自部署） | MIT 许可，核心功能自部署免费，有 trace、session、成本统计和数据集。自部署需要 Web、Worker、PostgreSQL、ClickHouse、Redis、S3 等多个服务。2026-01 被 ClickHouse 收购，之后仍是 MIT | 参考它的组织方式：trace 下挂 observation，按 session 和用户聚合 | 私有化部署时要多拉 ClickHouse 等服务，用户数据要离开本系统。它主要给人看界面，不适合 AI 助手读 |
| promptfoo | 用例写在 YAML 里，断言可以是 `javascript`、`contains`、`llm-rubric` 等，也能当库调用。官方有评测编码 Agent 的指南 | 借用「用例 + 断言列表 + 每个用例有阈值」的写法 | 我们需要多轮对话、审批、提问应答和画布结构断言，用它也得包一层自定义 provider，收益有限。多轮对话的官方写法未核实 |
| 本仓库的生成任务试跑 trace | 只有 `is_test` 任务有 `generation_tasks.trace_json`，后台有 `TraceTimeline` 组件，单请求体上限 64KB（docs/design/日志管理） | 「试跑才留完整 trace」的思路和截断上限可以直接沿用，二期做后台页面时复用这个组件 | 那套只记录插件的 HTTP 步骤，不涉及 LLM |

来源链接见第 12 节。

### 4.2 依赖框架调研与决定

| 候选 | 能覆盖什么 | 代价 | 结论 |
| --- | --- | --- | --- |
| Langfuse | 轨迹存储、界面、数据集、评分 | 部署很重，数据出边界，以人看为主 | 不引入 |
| promptfoo | 用例跑器、断言 | 多轮、审批、画布断言都要自己包 | 不引入 |
| OTel SDK | 标准 span 导出 | 多一层依赖，目前也没有接收端 | 不引入，只对齐命名 |

**用户决定**：自研，字段名对齐 OTel GenAI。理由是项目要私有化部署，主要使用者是 AI 助手，而现有表已经覆盖了大半数据。

---

## 5. 设计原则

1. **前后端尺寸规则只有一份定义，靠共享测试向量保证一致**。延续项目里 `arrange-cases.json` 的做法，杜绝两边各算各的。
2. **Agent 只移动自己本批新建的东西**。避让和推开都不碰用户原有的节点，与 system.md 中「不要动用户没提到的节点」一致。
3. **轨迹是给机器读的**：用 JSONL，一行一个 span，字段稳定；报告先写结论和失败证据，再列时间线。
4. **按真实情况复现**：用例走真实服务，与线上走同一条代码路径，不在 Harness 里另起一套假实现。
5. **花钱有上限**：每个用例必须写预算，整批运行有总预算闸门。

---

## 6. 推荐方案

### 6.A 排版修复

#### 6.A.1 根因

| # | 根因 | 证据 | 表现 | 复现（提案测试用例） |
| --- | --- | --- | --- | --- |
| R1 | 后端按 576×324 排版，前端尺寸随画幅变化 | `layout.go:46-52` 与 `node-frame.ts:36-41` | 9:16 的节点实际是 324×576。组框按 324 高计算（72+324+48=444），节点底边在 72+576=648，伸出组框 204px，看起来像没进组；竖排和网格排列时上下压住。21:9 的节点宽 756，大于 576+120，横向压住 | 组内 3 个 `params.aspect_ratio="9:16"` 的图片节点，组框高度应不小于 576+72+48 |
| R2 | 组框到一批操作结束时才贴合，这一批中间放置新对象时用的是旧尺寸 | `apply.go:49-54`、`createGroup` 取 200×140、`placeFree` | 同一批先建组 g1 并放进 3 个节点，再建组 g2。g2 按 g1 的旧宽度 200 放置，落在 g1 左上角往右 320 处。批末 g1 贴合后宽度变成 3×576+2×120+96=2064，把 g2 整个盖住，组压组 | ops = `[create_group g1, create_node×3 parentGroup g1, create_group g2, create_node×3 parentGroup g2]`，断言 g1 和 g2 的矩形不相交 |
| R3 | `canvas_arrange` 排不了组 | `layout.go` `arrangeTargets` | 提示词让 Agent「搭好后整理布局」，可它根本没法把组排开，只能重叠下去 | `Arrange(nodeIds=[g1,g2])` 现在会报错 |
| R4 | 前端合并 `canvas.patch` 时，`parentId` 和 `position` 各自按 guard 判断 | `graph-changes.ts` `applyChanges` 中 guard 逐字段比较 | 用户在补丁到达前拖过这个节点，结果 `parentId` 写进去了，`position` 却留着本地的绝对坐标，节点被甩出组外。这条与第 C 部分的 C1 是同一个问题 | 见 C1 |

另有两点不单列根因：
- **推断**：模型给组内成员传 `position` 时，可能把绝对坐标当成相对坐标传进来。贴合后组框会被撑得很大，进而压住别的组。修法是在工具说明里写清楚，并在 R2 的避让里兜底。
- **推断**：`placeFree` 永远往右放，画布会无限变宽。这不算 bug，避让规则改完后自然缓解。

#### 6.A.2 修法（已确认方向：后端照搬尺寸规则 + 前端兜底）

**F1 后端节点尺寸（提案）**
- 在 `canvasgraph` 新增 `frameSize(n Node, hint SizeHint) (w, h float64)`，规则与 `resolveNodeAspect` 和 `nodeFrameSize` 逐条一致：
  - 组：取 `width` / `height`，不小于 200×140。
  - 音频节点：324×324。
  - 文本节点：576×324（推断，见 2.1）。
  - 图片和视频节点的宽高比按以下优先级取：
    1. `data.mediaAspect`（数字，大于 0）；
    2. `params.aspect_ratio` 或 `params.ratio`，按 `^(\d+(\.\d+)?):(\d+(\.\d+)?)$` 解析；
    3. `hint.DefaultAspect(model)`，即模型能力里比例参数的默认值，由 service 层注入，canvasgraph 不依赖 modelcfg；
    4. 16:9。
  - 由宽高比算尺寸：比值不小于 1 时，宽 = round(324×比值)、高 = 324；否则宽 = 324、高 = round(324/比值)。
- 成片实测画幅只有前端知道，所以后端不处理，交给 F5 兜底。
- `Options` 新增 `Size SizeHint`。不传时只用前三档之外的规则，即不查模型默认值。
- 新增共享测试向量 `backend/internal/tests/testdata/canvasgraph/node-size-cases.json`，形如 `[{node, defaultAspect?, want:{w,h}}]`。Go 端由 `tests/canvasgraph` 读取，前端由 `web/src/tests/utils/canvas/node-frame.test.ts` 读取，两边对同一组输入断言相同输出。

**F2 组框立刻贴合（提案）**
- `createNode`（带 `parentGroup`）、`setGroup`（成员有增减）、`move`（移动成员）在这一项操作执行完后立即对相关组调用 `fitGroup`，不再等到批末。`refit` map 删除。
- `placeInGroup` 改为：在组内按行排列，一行最多 4 个，超过就换行，行高取这一行里最高的节点。原来的做法是一直往右排。

**F3 本批新建对象避让（提案）**
- 在 `Apply` 批末新增 `settle(created)`：只处理本批新建的**顶层**对象，即新建的组和不属于任何组的新建节点。
- 按创建顺序逐个检查：它的矩形如果与任何其他顶层矩形相交（含 40 的安全间距），就往下移到与它相交的那些矩形的最下沿加 80，重复直到不再相交。
- 已有对象一律不动。
- 新建对象的默认位置从「最右侧」改成「所有内容的左下方」。分镜通常是一镜一组、竖着往下排，这样更符合阅读顺序。这一条是**提案**，见 11.4 默认值。

**F4 `canvas_arrange` 支持排列组（提案）**
- `ArrangeTarget.NodeIDs` 允许包含组，此时把组当成一个整体矩形参与排列，成员跟着组走（成员坐标本来就是相对坐标，不需要改）。
- 新增约束：同一次排列里，所有对象必须同属顶层，或者同属一个组。混在一起时返回 `ErrInvalid`，提示「顶层对象和组内成员不能一起排列」。
- 工具声明 `tool_decls.json` 里对应说明同步修改。
- `system.md` 改为「搭好后用 canvas_arrange 把所有镜头组按 column 排列」，`Version` 升到 10。

**F5 前端兜底（提案，已确认）**
- 成员的实测尺寸（`measured`）变化后超出组框时，组框只撑大、不缩小，规则与 `fitGroupToMembers` 相同。
- 组框撑大后，如果与右侧或下方的顶层对象相交：
  - 横向撑大了 dx：右侧那些与本组纵向范围重叠的对象整体右移 dx；
  - 纵向撑大了 dy：下方那些与本组横向范围重叠的对象整体下移 dy。
  - 级联最多 3 层，防止抖动。
- 这次调整像普通编辑一样保存（会提高 revision），但**不进撤销栈**，因为它不是用户的操作。见 11.3 默认值。
- 触发点：放在 `group-node.tsx` 订阅成员尺寸的地方，用 `requestAnimationFrame` 合并，一帧最多处理一次。
- 性能约束：只看本组成员和相交的邻居，不扫描全画布。参考记忆「画布对齐线」中性能是硬约束的要求。

**F6 parentId 和 position 原子化（提案）**：与 C1、C2 一起修，见第 C 部分。

#### 6.A.3 测试（TDD 顺序）

| 测试 | 位置 | 先失败的断言 |
| --- | --- | --- |
| 尺寸向量 | `tests/canvasgraph/node_size_test.go`、`web/src/tests/utils/canvas/node-frame.test.ts` | 9:16 图片节点得到 324×576 |
| 组框包住竖图成员 | `tests/canvasgraph/apply_test.go` | 组框高度 ≥ 72+576+48 |
| 同批两个组不重叠（R2） | 同上 | g1 和 g2 的矩形不相交 |
| 排列组（R3） | `tests/canvasgraph/arrange_test.go` 加 `arrange-cases.json` | `Arrange([g1,g2], column)` 之后两组上下排开、间距 80 |
| 不移动已有对象 | 同上 | 已有对象坐标不变 |
| 前端兜底推开邻居 | `web/src/tests/utils/canvas/group.test.ts` | 撑大后邻组右移 dx |

### 6.B 调优 Harness

#### 6.B.1 总体

两条入口共用同一种「轨迹包」，报告由后端生成：

```
【跑用例：开发或测试环境】
用例 JSON ──▶ harness CLI ──HTTP──▶ 该环境的后端（真实 LLM、真实生成）
                 │                     │ 记录轨迹：索引写 PG，原文写素材存储
                 │◀── 事件补拉 ────────┤
                 │── 按剧本应答审批 ──▶│
                 │◀── 轨迹包（JSON）───┘ 管理员接口，后端生成 report.md
                 ▼ 补上断言结果
     .harness/runs/<时间>-<用例>/ report.md、trace.jsonl、assertions.json …

【线上问题：生产环境 → 开发环境】
生产后台「日志管理 → Agent 运行」──下载──▶ agent-trace-<runId>.zip
     ──人工拷贝到开发机，解压──▶ .harness/imports/<runId>/ report.md、trace.jsonl …
     ──▶ 交给编码助手分析
```

开发环境连不到生产环境（用户指出），所以线上轨迹**只能由管理员在生产后台导出文件**，不能用命令行直接拉取。

#### 6.B.2 轨迹记录（后端，线上每轮都记）

**span 模型**：命名对齐 OTel GenAI。

| span 类型 | 来源 | 记录内容 |
| --- | --- | --- |
| `invoke_agent` | 每个 run 一个，作为根 | run 输入（message、mode、selection、budget、agent_model_key）、`prompt_version`、`tools_hash`、技能版本、终态、合计；开跑和结束时各存一份**画布快照**（payload_json，计入单 run 上限），轨迹包里的 `canvas.before/after.json` 就取自这里。仓库里不保存历史画布，只能这样留底 |
| `segment` | 每个 Node 进程片段一个 | 启动方式（start / continue / resume / approval）、退出码、stderr 末尾 4KB |
| `chat` | 每次 `ProxyModel` | `purpose`（chat / summary）、上游模型和渠道、请求体原文（经网关处理之后）、响应（text、thinking、tool_calls、finish_reason）、token、首字延迟、总耗时、错误 |
| `execute_tool` | 每次 `executeTool` | 工具名、`tool_call_id`、`parent_tool_call_id`（codemode 的嵌套调用）、参数原文、返回原文、`is_error`、错误分类、耗时、关联的 `mutation_id` |
| `approval` | 审批创建和决定 | 种类、报价、决定、执行结果 |

**存储**（已确认：索引入库，原文进素材存储，保留 14 天）：
- 索引放 PostgreSQL，表结构见第 8 节。
- 原文以「每个片段一个对象」写入现有素材存储（本地磁盘或 S3），key 为 `agent-traces/{yyyy}/{mm}/{dd}/{run_id}/{segment_seq}.jsonl.gz`。
  - 片段结束时上传，即 `finish` 或 `watch` 发现进程退出时。
  - 片段运行期间，原文先追加到后端本机的临时文件。
  - 进程崩溃时，这个片段已经写入的内容照样上传，并把 `trace_complete` 置为 false。
- 上限：
  - 单个 span 的原文最多 256KB，超出部分截断并标 `truncated:true`；
  - 单个 run 最多 8MB，超出后只记索引不记原文，并在 run 上标记。
  - 这些数字是**提案**，参照日志管理设计中「单请求 64KB、单任务 1MB」的思路，按 Agent 请求体更大的特点放宽。
- 脱敏：
  - 请求头一律不记（渠道 Key 在请求头里）；
  - 请求体里的图片（base64 或 URL）替换成 `{"asset_id":…}` 引用；
  - 网关错误沿用现有的 `redactErr`。
- 清理：River 周期任务每天运行一次，删除超过 14 天的 `agent_trace_spans` 行和对应的原文对象。`agent_events` 一直缺的保留期清理也顺带补上，保留期同样 14 天。这是**提案**，见 11.3。
- 开关：`agent.trace.enabled`（默认 true）、`retention_days`（默认 14）、`max_span_bytes`、`max_run_bytes`，写在 `configs/config.yaml` 的 `agent` 段下。

**埋点位置**（提案，都在 Go 侧，Node 不需要改）：
- `chat`：`service/agent/bridge.go` `ProxyModel`。把 `StreamRaw` 的请求体和 `Result` 交给 tracer，首字延迟在第一个 delta 到达时记。
- `execute_tool`：`service/agent/bridge_tools.go` `executeTool` 的开始和结束处。codemode 的嵌套调用走 `ExecuteNestedTool`，带上 parent id。
- `segment`：`service/agent/runtime.go` 的 `launch` 和 `watch`。
- `invoke_agent`：`StartRun` 时写入 run 的版本快照，终态时写合计。

**轨迹包**（提案，后端生成，命令行跑用例和后台下载用的是同一份）：

| 文件 | 内容 |
| --- | --- |
| `report.md` | 报告，结构见 6.B.3 末尾；后端生成的版本没有「断言」一节 |
| `trace.jsonl` | 规整后的 span，一行一个，带原文 |
| `events.jsonl` | 这一轮的 `agent_events` |
| `mutations.jsonl` | 这一轮的 `agent_mutations`（画布改动和 revision） |
| `canvas.before.json` / `canvas.after.json` | 开跑和结束时的画布快照 |
| `meta.json` | run id、用户 id、画布 id、`prompt_version`、模型、`tools_hash`、起止时间、`trace_complete`、导出时间、导出人 |

- 报告生成逻辑全仓只有一份，放在后端新包 `service/agent/tracereport`，是纯函数：输入轨迹数据，输出 Markdown。CLI 不再自己生成时间线，只在报告顶部插入「断言结论」和「失败断言」两节。
- 下载格式：浏览器下载走 zip（Go 标准库 `archive/zip`）；CLI 请求时带 `?format=json`，拿到 `{files: {文件名: 内容}}`，这样 Node 不用为解 zip 加依赖。

**后台页面「日志管理 → Agent 运行」**（已确认：一期只做列表和下载）：
- 位置：`web/src/pages/admin/logs/agent-runs/`，在 `admin-nav.ts` 的「日志管理」下新增一项，排在「操作审计」后面。
- 筛选：用户（邮箱或 id）、画布 id、run id、状态、来源（真实用户 / harness）、时间范围；默认显示最近 7 天。
- 列：时间、用户、画布、模式、状态、步数、工具报错数、积分、耗时、`prompt_version`、来源；最后一列是「下载轨迹包」按钮。
- 不可下载的行：按钮置灰并写明原因，不隐藏。
  - 「已过期（保留 14 天）」：轨迹已被清理；
  - 「轨迹关闭」：当时 `agent.trace.enabled=false`；
  - 「运行中」：运行还没进入终态，下载到的轨迹不完整。仍可下载，按钮旁标「未完成」。
- 下载成功时提示「已下载，此次导出已记入操作审计」。`trace_complete=false` 的行打一个「不完整」标签。
- 一期不做在线查看时间线，已搁置，见 11.2。
- 只有管理员能访问：路由挂在 `RequireAdmin` 下，普通用户看不到原文轨迹，连自己的也看不到。现有的 Agent 面板保持不变。
- 每次下载写一条审计，动作名 `agent.trace.export`，目标是 run id。CLI 跑用例时拿轨迹也算下载，同样写审计，因为走的是同一个接口。

#### 6.B.3 命令行工具 `agent/harness`（提案）

- 位置和运行环境：`agent/harness/`。与 Agent 运行时共用 Node ≥ 22.19，不新增依赖，用例格式选 JSON 也是为了这一点。
- 命令：

| 命令 | 期次 | 作用 |
| --- | --- | --- |
| `node agent/harness/cli.mjs run <用例文件或目录> [--repeat N] [--max-credits N] [--keep] [--out .harness/runs]` | 一期 | 跑用例 |
| `node agent/harness/cli.mjs report <运行目录>` | 一期 | 改了断言之后，根据目录里的轨迹重新执行断言并刷新报告的断言部分 |
| `diff <运行目录A> <运行目录B>` | 二期 | 对比两次运行：断言、步数、token、积分、工具报错 |
| `to-case <轨迹包目录>` | 二期 | 把线上导出的轨迹包转成用例：初始画布用 `canvas.before.json`，应答剧本用实际的审批决定 |

- 线上导出的轨迹包不需要专门的命令：解压到 `.harness/imports/<runId>/` 就能让编码助手读，`agent/harness/README.md` 里写明这个约定。

- 认证：
  - 环境变量 `HARNESS_BASE_URL`、`HARNESS_EMAIL`、`HARNESS_PASSWORD`，用于跑用例的专用测试账号；
  - `HARNESS_ADMIN_EMAIL`、`HARNESS_ADMIN_PASSWORD`，用于从**同一个环境**的后端取轨迹包；
  - 登录接口 `POST /auth/login` 的请求体字段是 `username`（`model.LoginUserReq`），不支持邮箱登录，所以这里的「EMAIL」实际填用户名；也可以改用 `HARNESS_USERNAME` / `HARNESS_ADMIN_USERNAME`（`HARNESS_USERNAME` 可以代替 `HARNESS_EMAIL`）；
  - 都通过 `POST /api/v1/auth/login` 换 JWT。不新增 API Token 机制。
  - 密钥不写进仓库，也不写进输出目录。

- 单个用例的执行流程：
  1. 登录，然后 `POST /api/v1/canvas` 用 `fixture` 新建画布，标题 `[harness] <用例id> <时间>`。
  2. `POST /api/v1/canvas/:id/agent/sessions`，请求体 `{mode, model_key}`。
  3. `POST /api/v1/agent/sessions/:sid/runs`，请求头带 `X-Agent-Harness: <用例id>`，请求体 `{message, mode, selection, budget_credits}`。
  4. 每秒调一次 `GET /api/v1/agent/sessions/:sid/events?after=<seq>` 补拉事件。
     - 遇到 `approval.created`，按 `answers` 剧本匹配后调用 `POST /api/v1/agent/approvals/:aid/decision`。
     - 没有匹配上的提问或审批：立即 `POST /runs/:rid/cancel`，用例判为失败，原因写「未预写的提问：<原文>」。
  5. `run.status` 进入终态（不在 queued / running / waiting_approval / waiting_input 里即算终态，包括 budget_exhausted、step_limit、interrupted，harness 不自动续跑）后：如果 `wait_tasks=true`，读画布找带 `taskId` 的节点，再每 5 秒轮询 `GET /generation-tasks/:id`，直到任务全部完成或失败，或者超时。不看画布上的 `outputs`：`BindTasks` 只写 taskId 和状态，产物由浏览器里的前端回填，harness 没有浏览器；任务产物数记进 `tasks.json`，`nodes_have_output` 据此判断。
  6. 读最终画布 `GET /api/v1/canvas/:id`；用管理员身份请求 `GET /api/v1/admin/agent/runs/:rid/trace?format=json`，把轨迹包里的文件写进输出目录；执行断言；写 `assertions.json`，并在 `report.md` 顶部插入断言结论。
  7. 默认删除测试画布，`--keep` 时保留。
- 超时：每个用例用 `timeout_sec`，默认 900；超时后取消运行并判为失败。
- 花钱闸门（**提案**）：
  - 每个用例必须写 `budget`；
  - 开跑前把所有用例的预算乘以 `repeat` 求和，超过 `--max-credits`（默认 200）就拒绝开跑；
  - 后端按 run 的预算强制执行。
- 不确定性：`--repeat N` 把同一用例跑 N 次，报告里给出通过率。默认 1，因为用例走真实服务，每次都花钱。
- 输出目录 `.harness/runs/<YYYYMMDD-HHmmss>-<用例id>[-<第几次>]/`，要加进 `.gitignore`：

  输出目录就是轨迹包（见 6.B.2）再加两样：`assertions.json`（每条断言的结果和证据），以及 `meta.json` 里补充的字段（用例 id、后端地址）。

- 整批运行另有 `summary.md` 和 `summary.json`：每个用例一行，含是否通过、通过率、步数、积分、耗时。

**report.md 的固定结构**（为 AI 阅读设计；第 1、2 节由 CLI 插入，后台导出的版本只有第 0、3、4、5 节）：
0. 运行概要（后端生成）：终态、`prompt_version`、模型、步数、token 入/出、积分、耗时、用户原话。
1. 结论行：`通过 / 失败（3/5 条断言）`。
2. 失败断言：每条写期望和实际，并附证据，例如节点 id、两个相交的矩形坐标、相关 span 的序号。
3. 可疑信号（自动标出）：
   - 同一条工具报错重复出现 3 次以上；
   - 摘要压缩被触发；
   - 达到步数上限；
   - 有未预写的提问；
   - `trace_complete=false`。
4. 时间线表：序号、span 类型、名称、耗时、状态、一行摘要。
5. 最终画布概要：组到成员的树，每个节点写种类、尺寸和是否有提示词。

#### 6.B.4 用例格式（提案）

```json
{
  "id": "storyboard-9x16-3shots",
  "title": "三镜头 9:16 分镜搭建",
  "tags": ["storyboard", "layout"],
  "fixture": "fixtures/script-only.canvas.json",
  "mode": "storyboard",
  "agent_model_key": "",
  "message": "把剧本拆成 3 个镜头，竖屏 9:16，每个镜头一组：镜头文本→关键帧→视频",
  "selection": ["n_script"],
  "budget": 0,
  "timeout_sec": 600,
  "wait_tasks": false,
  "answers": [
    { "match": { "kind": "ask", "contains": "画幅" }, "answer": { "answer": "9:16" } },
    { "match": { "kind": "ask", "ask_kind": "model" }, "answer": { "answer": "<图片模型 key>" } },
    { "match": { "kind": "generate" }, "answer": { "decision": "reject" } },
    { "match": { "kind": "delete" }, "answer": { "decision": "reject" } }
  ],
  "expect": [
    { "type": "run_status", "in": ["succeeded"] },
    { "type": "no_overlap" },
    { "type": "members_inside_group" },
    { "type": "count", "of": "group", "min": 3, "max": 4 },
    { "type": "every_group_has", "kinds": ["script", "image", "video"] },
    { "type": "edge_exists", "from": "image", "to": "video", "scope": "group" },
    { "type": "nodes_have_prompt", "kinds": ["image", "video"] },
    { "type": "max_tool_errors", "max": 2 },
    { "type": "max_steps", "max": 40 }
  ]
}
```

- 实现时核对到的事实：`ask_user` 的 `kind` 只有 `choice` 和 `model`，存在 `approval.created` 的 `data.payload.kind`；选模型题另有 `payload.model_kind`（`image`/`video`）和 `payload.models[]`，提问原文在 `payload.question`。所以 `ask_kind` 对应 `payload.kind`，另支持 `model_kind` 匹配；轨迹包里 `approval` span 的 `ask_kind` 也取 `payload.kind`。
- `answers` 按顺序匹配，第一条命中的生效。`kind` 对应审批种类，已核对 `model/agent.go:61-63`，只有 `generate` / `delete` / `ask` 三种；`ask_kind` 区分 ask 下的子类，例如选模型（`ask_user` 的 `kind=model`），`contains` 匹配提问原文。`answer` 的字段与 `DecideAgentApprovalReq`（`model/agent_req.go:61-65`）一致：`decision`、`items`、`answer`、`add_budget`。
- 断言清单（一期）：

| 类型 | 判定 | 证据 |
| --- | --- | --- |
| `run_status` | 终态在列表里 | 终态和错误码 |
| `no_overlap` | 顶层对象两两之间、同组成员两两之间矩形不相交。尺寸用 F1 的规则，由测试向量保证与前端一致 | 相交的两项及其矩形 |
| `members_inside_group` | 每个成员矩形完全落在组框内（含留白） | 越界的成员和越界的量 |
| `count` | 某种节点或组的数量在 [min, max] 内 | 实际数量 |
| `every_group_has` | 每个组都包含所列种类 | 缺了哪种的组 |
| `edge_exists` | 存在 from→to 的连线；`scope=group` 表示每个组内都要有 | 缺线的组 |
| `nodes_have_prompt` / `nodes_have_output` | 指定种类的节点提示词非空，或已有产物 | 不满足的节点 |
| `asked` / `not_asked` | 是否出现过某类提问 | 提问原文 |
| `max_tool_errors` / `max_steps` / `max_credits` | 过程指标的上限 | 实际值 |

- 已确认**不做** LLM 裁判，主观质量由人看报告判断。

#### 6.B.5 给编码助手的使用说明（提案）

新增 `agent/harness/README.md`，写明调优循环：
1. 跑基线：`run cases/ --out .harness/base`；
2. 修改 `system.md` 或工具，同时提升 `Version`；
3. 重跑；
4. 在二期的 `diff` 上线之前，先人工对比两份 `summary.md`。

另外在根 `AGENTS.md` 的 Agent 一节加一句索引，指向这份 README。

### 6.C 隐患清单

审查范围是整条 Agent 链路，下表中除标「推测」的条目外都读代码确认过，其中 C1、C2、C4、C5、C9、C13 已二次抽查。

| # | 级别 | 问题 | 位置 | 触发场景 | 建议修法 |
| --- | --- | --- | --- | --- | --- |
| C1 | 高 | 前端合并补丁时 `parentId` 和 `position` 被拆开判断（即 A 的 R4） | `web/src/utils/canvas/graph-changes.ts` `applyChanges` | Agent 把 n1 放进组，用户在补丁到达前拖过 n1 → `parentId` 被写入，`position` 保留本地的绝对坐标 → n1 被甩出组 | 把两者当成一组原子字段：任一个被本地改过，两个都保留本地值；或者按本地 `parentId` 换算坐标 |
| C2 | 高 | 撤销本轮时同样把这两个字段拆开 | `canvasgraph/revert.go:100-114` | Agent 把节点放进组，之后用户只移动了它 → 撤销时 `parentId` 被恢复，`position` 被跳过 → 相对坐标被当成绝对坐标 | `revertField` 对这两个字段成对判断，或者恢复时用 `reparent` 换算 |
| C3 | 高 | 撤销会删掉用户本地改过、但还没保存的 Agent 新建节点，连同用户新连的线 | `revert.go:157-181`、`graph-changes.ts`（delete 不受 guard 保护）、`use-agent-controller.ts:221-230` | 用户改了 Agent 新建节点的提示词、连了线，还在防抖窗口内就点了「撤销本轮」 | 撤销前先 `flush()` 保存并等它完成；或者前端对 delete 也做 guard，保留节点并提示 |
| C4 | 中 | 生成任务已创建但绑定节点失败时，告诉模型「没有扣积分」，可能导致重复生成、重复扣费 | `service/agent/generate.go:247-248`、`approval.go:368` | `BindTasks` 连续冲突 3 次 → 审批记为失败 → 节点上没有 taskId → 模型再次申请生成 | 绑定失败时仍返回已创建的任务并照实告诉模型；绑定改为可重试 |
| C5 | 中 | 批准时就把报价计入已花积分，执行失败不回滚；等待批准期间用户改了参数，也不重新报价 | `approval.go:151-156`、`generate.go` | 部分节点执行失败 → 预算被虚占 → 后续调用提前提示预算用尽 | 按实际成功的任务记账；提交时重新报价并与原报价比较 |
| C6 | 中 | 审批后续跑失败时，模型以后一直以为还在等批准 | `approval.go:161-164`、`runtime.go`、`agent/src/run.mjs` | `Resume` 失败 → 运行被标为 interrupted → 用户点「继续」→ 历史里仍是占位文字「等待决定」 | resume 时用审批表的 `result_json` 替换占位的工具结果 |
| C7 | 中 | 保存请求在途时收到补丁，会误弹冲突窗 | `web/src/utils/canvas/agent-sync.ts:99-101`、`save-coordinator.ts:304-312` | base=v 的保存在途 → 补丁把本地推到 v+1 → 保存返回 409 → `resync` 返回 null → 弹冲突窗 | 服务端不比本地新时返回 `getVersion()`，直接重试保存 |
| C8 | 中 | 每收到一次 Agent 补丁就清空用户的撤销栈 | `use-agent-canvas-sync.ts:67` | Agent 运行期间用户的 Ctrl+Z 基本不可用 | 把远端合并记成一道屏障，或只对被改到的节点做 rebase |
| C9 | 中 | 画布目录每轮都追加进用户消息并存进历史，越积越多 | `runtime.go:80-89`、`agent/src/context.mjs` | 同一会话发 10 条消息，历史里就有 10 份目录 → token 成本高、更早触发压缩、预检额度虚高 | 历史里只保留最新一份目录，旧的换成占位文字 |
| C10 | 低 | 片段收尾窗口里的插话被静默丢弃 | `agent/src/run.mjs:48-58` | 在 settle 之后、finish 之前插话 | finish 前检查队列，有插话就由 Go 自动续跑 |
| C11 | 低 | codemode 超时时，正在进行的写入可能已经提交，却不在「已生效」列表里 | `agent/src/agent-tools/codemode.mjs` | 嵌套的 apply_ops 已提交，脚本恰好超时 → 模型重复执行 → 重复建节点 | 调用前先记「进行中」，失败文案里提示模型先读画布确认 |
| C12 | 低 | 本页收到过一次补丁之后，所有 409 都自动合并，`touched` 永不复位 | `agent-sync.ts:111,126` | 另一个标签页的保存冲突也被静默合并 | 运行结束、对账完成后复位 |
| C13 | 低 | 审批超过 24 小时过期后，运行一直停在 waiting 状态，占着这张画布 | `approval.go:122` | 过期后无法发起新运行，只能手动点停止 | 判定过期时顺手把运行置为 canceled，或加定时清理 |
| C14 | 低 | 「提示词优化」模式能改任意 params | `service/agent/bridge_tools.go:213-215` | 越出模式承诺，改了时长、比例 | 这个模式下只允许 prompt |
| C15 | 低（推测） | 前端规整过的节点，撤销时会被判为「你已修改」，删不掉 | `revert.go:171-180` | 前端补全默认参数后保存回去 | 只比较 Agent 写过的字段 |

另有两点：
- 安全方面没有发现实质问题：桥只监听回环地址，令牌通过 stdin 传入，子进程环境变量是白名单，`web_fetch` 防 SSRF。有一个小隐患：`ExecuteNestedTool` 不校验 `parent_tool_call_id` 是否真的报备过，任意非空值都能绕过步数计数（Node 是可信进程，风险低）。
- 可观测性缺口（B 部分解决）：Node 的 stderr 基本被丢弃；`agent_events` 没有保留期清理。

**建议修复顺序**：C1、C2 与 A 一起修，它们是同一类问题 → C3 → C4、C5，涉及钱 → C6、C7、C8 → 其余。

---

## 7. 状态与异常

| 场景 | 行为 |
| --- | --- |
| 尺寸提示拿不到模型默认比例 | 退回 16:9，前端兜底 F5 负责纠正 |
| 避让找不到空位 | 一直往下移，不设上限，画布本来就是无限的 |
| 轨迹上传失败 | 记 warn 日志，run 标 `trace_complete=false`，不影响用户的运行 |
| 单个 span 或单个 run 超过上限 | 截断并打标记；超过整轮上限后只记索引 |
| 轨迹已过期 | 后台下载按钮置灰并写「已过期（保留 14 天）」；直接调用接口则返回业务错误「轨迹已过期」 |
| 运行还没结束 | 可以下载，`meta.json` 和报告顶部标「运行未结束，轨迹不完整」 |
| Harness 遇到未预写的提问 | 取消运行，判为失败，在报告里写出提问原文和建议补的 `answers` 条目 |
| Harness 超时 | 取消运行，判为失败，已有的轨迹照常拉取 |
| Harness 预算闸门触发 | 不开跑，打印每个用例的预算明细 |
| Harness 登录失败或管理员凭据缺失 | `run` 仍执行，只是缺少轨迹包，CLI 用补拉到的事件自己写一份简版 `trace.jsonl`（没有原文），报告标「无轨迹（缺管理员凭据）」 |
| 测试画布删除失败 | 报告里提示手动清理，并给出画布 id |
| 线上用户的隐私 | 只有管理员能导出，每次导出都留审计，14 天后自动删除。zip 下载后就离开了系统，不再受审计管控；开发机上的解压目录 `.harness/` 已加进 `.gitignore`，不会进仓库 |
| 后台下载失败（网络中断） | 提示「下载失败，可以重试」。审计在服务端开始传输时就已写入，所以失败的下载也会留一条审计 |
| 后台筛选结果为空 | 表格里提示「没有符合条件的运行，换个筛选条件，或者看看是不是已超过 14 天」 |

---

## 8. 数据与工程影响（提案）

### 8.1 表结构

**`agent_runs` 新增字段**

| 字段 | 类型 | 含义 |
| --- | --- | --- |
| `prompt_version` | int | 开跑时的 `prompts.Version` |
| `agent_model_key` | varchar(128) | 本轮使用的 Agent 模型 |
| `tools_hash` | char(16) | 工具声明 JSON 的 sha256 前 16 位 |
| `skill_pins` | jsonb | 技能名到版本号的映射 |
| `input_json` | jsonb | `AgentRunInput`，包括 message、mode、selection、budget |
| `harness_case` | varchar(128) | 来自请求头 `X-Agent-Harness`，为空表示真实用户；只是标签，不影响权限 |
| `trace_complete` | bool，默认 true | 轨迹是否完整 |
| `trace_bytes` | bigint | 原文的总字节数 |

**`agent_model_calls` 新增字段**

| 字段 | 类型 | 含义 |
| --- | --- | --- |
| `span_id` | varchar(32) | 对应轨迹里的 span |
| `purpose` | varchar(16) | `chat` 或 `summary`，取自请求头 `X-Agent-Purpose` |
| `segment_seq` | int | 属于第几个片段 |
| `first_token_ms` / `duration_ms` | int | 首字延迟和总耗时 |
| `finish_reason` | varchar(32) | 结束原因 |
| `upstream_model` / `channel_id` | varchar / bigint | 实际走的上游模型和渠道 |

**新表 `agent_trace_spans`**（工具、片段和审批的 span 索引；chat span 的索引继续放在 `agent_model_calls`）

| 字段 | 类型 | 约束和含义 |
| --- | --- | --- |
| `id` | bigint | 主键 |
| `run_id` | bigint | 索引 `(run_id, seq)` |
| `seq` | int | run 内的顺序 |
| `span_id` | varchar(32) | 唯一 `(run_id, span_id)` |
| `parent_span_id` | varchar(32) | codemode 嵌套调用指向父调用 |
| `kind` | varchar(16) | `segment` / `execute_tool` / `approval` |
| `name` | varchar(64) | 工具名或片段的启动方式 |
| `tool_call_id` | varchar(64) | 可为空 |
| `status` | varchar(16) | `ok` / `error` / `denied` / `aborted` |
| `error_code` | varchar(32) | 错误分类 |
| `summary` | varchar(255) | 一行摘要 |
| `duration_ms` | int | 耗时 |
| `bytes` | int | 原文大小 |
| `mutation_id` | bigint | 可为空，关联 `agent_mutations` |
| `started_at` / `ended_at` | timestamptz | `started_at` 建索引，供清理任务用 |

迁移沿用项目现有的迁移机制。表里不存密钥。

### 8.2 原文对象的行格式（`*.jsonl.gz`，一行一个）

```json
{"span_id":"c7","kind":"chat","seq":12,"request":{"model":"…","messages":[…],"tools":"<tools_hash>"},"response":{"text":"…","thinking":"…","tool_calls":[{"id":"call_1","name":"canvas_apply_ops","arguments":"…"}],"finish_reason":"tool_calls"},"usage":{"input":12034,"output":512,"cached":8000},"truncated":false}
{"span_id":"t8","kind":"execute_tool","seq":13,"tool_call_id":"call_1","args":{…},"result":"…","is_error":false}
```

工具声明全文只在每个片段的第一行（`kind:"system"`）记录一次，后面的 chat span 用 `tools_hash` 引用。

### 8.3 接口

| 方法与路径 | 说明 | 业务错误 |
| --- | --- | --- |
| `GET /api/v1/admin/agent/runs?user_id&canvas_id&status&harness_case&from&to&page&page_size` | 运行列表，每行带 status、steps、token 合计、积分、耗时、工具报错数、`prompt_version`、`harness_case` | 无权限 403 |
| `GET /api/v1/admin/agent/runs/:rid/trace` | 返回轨迹包（文件清单见 6.B.2）。默认返回 zip，带 `Content-Disposition: attachment; filename=agent-trace-<rid>.zip`；`?format=json` 返回 `{files:{文件名:内容}}`，供 CLI 使用。每次调用写一条审计 `agent.trace.export` | 运行不存在；轨迹已过期；轨迹已关闭 |
| 现有 `POST /api/v1/agent/sessions/:sid/runs` | 新增可选请求头 `X-Agent-Harness`，最长 128 字符，写入 `harness_case` | 无变化 |

两个新接口挂在 `RequireAdmin` 下（`router.go` 中 `/admin` 分组）。

### 8.4 接口契约（实现用，后端、后台页面、命令行三方按此对齐）

**通用**：路径前缀 `/api/v1`；响应沿用项目的 `{code, msg, data}`；JSON 里的各种 id 都是 `idcodec` 编码后的字符串；时间是 RFC3339 字符串；字段名全部 snake_case。

**列表** `GET /admin/agent/runs`，页码分页（与 `GET /admin/logs/audits` 一致）：

| 参数 | 含义 |
| --- | --- |
| `user` | 用户邮箱（包含匹配）或用户 id |
| `canvas_id` / `run_id` | 精确匹配 |
| `status` | 取 `model/agent.go` 的 Run 状态常量，共 12 个：`queued / running / waiting_approval / waiting_input / succeeded / failed / canceled / budget_exhausted / step_limit / timeout / interrupted / expired`（没有 `done` 和 `error`） |
| `source` | `user`（真实用户，`harness_case` 为空）或 `harness` |
| `from` / `to` | 创建时间范围；不传默认最近 7 天 |
| `page` / `page_size` | 默认 1 / 20，`page_size` 最大 100 |

`data = {list, total, page, page_size}`，`list` 每项：

```json
{
  "id": "…", "created_at": "…", "ended_at": "…|null",
  "user_id": "…", "user_email": "alice@studio.cn",
  "canvas_id": "…", "canvas_title": "雨夜追凶 第3集",
  "mode": "storyboard", "status": "succeeded",
  "steps": 18, "tool_errors": 3, "spent_credits": 0, "duration_ms": 41000,
  "prompt_version": 9, "source": "user", "harness_case": "",
  "trace": "available",
  "trace_complete": true
}
```

- `trace` 取值：`available`（可下载）/ `expired`（超过保留期，原文已清）/ `disabled`（当时 `agent.trace.enabled=false`，从没记过）。
- 运行还没到终态时 `ended_at` 与 `duration_ms` 为 null，`trace` 仍按上面判断；前端据 `status` 自行标「未完成」。
- `tool_errors`：这一轮 `execute_tool` span 里 `status=error` 的数量，来自 `agent_trace_spans`。轨迹关闭或已过期时为 0。

**轨迹包** `GET /admin/agent/runs/:rid/trace[?format=json]`：

- 默认返回 zip，`Content-Type: application/zip`，`Content-Disposition: attachment; filename="agent-trace-<rid>.zip"`，直接下载。
- `?format=json` 返回常规信封，`data = {"files": {"report.md": "…文本…", "trace.jsonl": "…", …}}`，值都是字符串，供命令行使用。
- 两种格式里的文件一致：`report.md`、`trace.jsonl`、`events.jsonl`、`mutations.jsonl`、`canvas.before.json`、`canvas.after.json`、`meta.json`。画布快照不存在（轨迹关闭）时，对应文件缺省而不是空文件。
- 业务错误：run 不存在（沿用现有 Agent 错误码）；轨迹已过期；轨迹记录已关闭。必须用 `errcode` 里新增的明确错误码，不能复用笼统的错误。
- 每次调用写审计，动作 `agent.trace.export`，目标是 run id，在**开始传输之前**写入（下载中途失败也留记录）。

**`trace.jsonl`**，一行一个 span，按 `seq` 升序。所有行共有：

```json
{"seq": 1, "span_id": "c1", "parent_span_id": null, "kind": "chat",
 "name": "<模型 key 或工具名或启动方式>", "status": "ok",
 "summary": "一行摘要", "started_at": "…", "duration_ms": 3100, "error_code": ""}
```

- `kind` 取 `segment | chat | execute_tool | approval`；`status` 取 `ok | error | denied | aborted | pending`。
- `chat` 另有：`purpose`（`chat`/`summary`）、`upstream_model`、`channel_id`、`first_token_ms`、`finish_reason`、`usage:{input,output,cached}`、`credits`、`request`（发给模型的请求体原文，去掉了请求头，图片换成 `{"asset_id":…}`）、`response:{text,thinking,tool_calls:[{id,name,arguments}]}`、`truncated`。
- `execute_tool` 另有：`tool_call_id`、`args`（解析后的参数对象）、`result`（返回给模型的文本原文）、`is_error`、`mutation_id`（没有改画布则无）、`truncated`。`parent_span_id` 指向 codemode 的外层调用。
- `segment` 另有：`launch`（`start|continue|resume|approval`）、`exit_code`、`stderr_tail`。
- `approval` 不单独存表，由后端在生成轨迹包时从 `agent_events` 的 `approval.created` / `approval.decided` 拼出：`approval_kind`（`generate|delete|ask`）、`ask_kind`（ask 的子类，没有则空）、`question`（ask 的提问原文）、`quote`、`decision`、`result`。
- 第一行固定是 `{"seq":0,"kind":"system", "prompt_version":…, "tools_hash":"…", "system_prompt":"…", "tools":[…]}`，工具声明全文只出现这一次，`chat` 的 `request` 里用 `tools_hash` 引用。

**`meta.json`**：`{run_id, user_id, canvas_id, mode, status, prompt_version, agent_model_key, tools_hash, skill_pins, harness_case, started_at, ended_at, trace_complete, truncated, exported_at, exported_by}`；命令行跑用例时会再补 `{case_id, base_url}`。

**`canvas.before.json` / `canvas.after.json`**：画布 `payload_json` 原文（`{nodes, edges, viewport}`），开跑和进入终态时各取一次。

**`report.md`**（后端生成，纯函数 `tracereport.Build`）：
```
# Agent 运行报告 · run <id>
> 终态 succeeded · prompt v9 · 模型 xxx · 18 步 · token 入 31.4k / 出 4.9k · 积分 0 · 耗时 41s
> 用户原话：…（最多 200 字）
> ⚠ 运行未结束，轨迹不完整           ← 仅在 status 不是终态或 trace_complete=false 时出现
## 可疑信号
- 同一条工具报错重复 3 次：canvas_arrange「…」       ← 无则写「无」
## 时间线
| # | span | 名称 | 耗时 | 状态 | 摘要 |
## 最终画布概要
组 → 成员树；每个节点写种类、尺寸（按前后端共用的尺寸规则）、是否有提示词
```
命令行在第一行标题后插入 `## 断言结论` 和 `## 失败断言` 两节。

### 8.5 修改范围（预估）

- 后端：
  - `canvasgraph`：layout、apply、group_ops、revert 及测试向量；
  - `service/agent`：bridge、bridge_tools、runtime、approval、generate，新增 tracer 和清理任务；
  - repository、model、迁移；
  - router 和 admin handler；
  - `prompts/system.md` 升到 v10，`tool_decls.json`。
- 前端：`graph-changes.ts`、`group-node.tsx` 和 `group.ts`（F5）、`agent-sync.ts`、`use-agent-canvas-sync.ts`；新增后台页面 `pages/admin/logs/agent-runs/`、`api/admin/agent-runs/`，`admin-nav.ts` 加一项。
- 后端另加：`service/agent/tracereport`（报告生成）、admin handler（列表和下载）。
- 新增：`agent/harness/`（cli、runner、assertions、report、cases、fixtures）、`.gitignore` 加 `.harness/`。

---

## 9. 方案取舍

**A 的尺寸来源**（权重：正确性 0.5、Agent 读到的坐标与屏幕一致 0.3、实现成本 0.2）

| 方案 | 正确性 | 坐标一致 | 成本 | 结论 |
| --- | --- | --- | --- | --- |
| 后端照搬规则 + 前端兜底 | 高 | 高 | 中 | **采用** |
| 只修后端 | 中（Auto 比例和实测画幅仍会撑破） | 高 | 低 | 放弃 |
| 只让前端重排 | 高 | 低（Agent 读到的坐标与屏幕不一致） | 中 | 放弃 |

**B 的形态**：用户已确认主要使用者是 AI 编码助手，因此选择「CLI + JSONL + Markdown」。放弃的方向如下：
- 后台在线查看时间线：一期只做列表和下载，在线查看放到后续，届时复用 `TraceTimeline`；
- 命令行直接从生产拉取轨迹（`pull`）：开发环境连不到生产环境，改为后台下载文件；
- LLM 裁判：用户明确不要；
- 录制回放：用户选择走真实服务。录制回放可以作为后续给代码回归用的补充，本期不做。

**明确放弃**：引入 Langfuse 或 promptfoo；改造 Node 运行时来做埋点（Go 侧已经能拿到全部数据）。

---

## 10. 分期与验收

| 期次 | 内容 | 验收条件 |
| --- | --- | --- |
| 一期-A | F1 至 F6，加 C1、C2 | 6.A.3 的测试全部先失败、后通过；`go test ./internal/tests/canvasgraph/...` 和前端相关测试通过；9:16 分镜用例的 `no_overlap` 和 `members_inside_group` 通过 |
| 一期-B | 轨迹落库和上传、后端报告生成、管理员两个接口（zip / json）、审计、14 天清理；后台「Agent 运行」页（列表 + 下载）；CLI 的 `run` / `report`，一期断言全集，`summary` | 跑 `agent/harness/cases/` 下至少 3 个用例：分镜 9:16、剧本创编、提示词优化。每个都产出完整的输出目录；故意删掉 `answers` 里的一条，用例判为失败且报告写明原因；在后台下载一轮运行的 zip，解压后 `report.md` 和 `trace.jsonl` 里有 chat 和 tool 原文，并在操作审计里看到这次导出；已过期的行按钮置灰并写明原因 |
| 一期-C | C3 至 C5 | 每条先写失败测试，修复后通过 |
| 二期 | `diff`、`to-case`；C6 至 C15；可选的后台在线查看时间线 | `diff` 能列出两次运行在断言、步数、积分、报错上的变化 |

---

## 11. 已确认、已搁置、默认值、待确认

### 11.1 已确认（用户在访谈中的回答）

1. 问题 1 本轮只写进设计，稍后再修。
2. Harness 主要给 AI 编码助手用，形态是 CLI + JSONL 轨迹 + 报告。
3. 跑用例时 LLM 和图片、视频生成**全部走真实服务**。
4. 评判方式是**画布结构断言**加**人工看报告**，不做 LLM 裁判。
5. Agent 的提问和审批按**用例里预写的剧本**应答，没有预写的提问直接判失败。
6. 线上真实运行**也记录轨迹**，并且可以导出成用例（导出成用例在二期）。
7. 轨迹的**索引存 PostgreSQL，原文存素材存储，保留 14 天**。
8. **只有管理员**能查看和导出线上轨迹，导出**留审计**。
9. 一期范围：轨迹 + CLI 跑用例 + 结构断言；二期：对比和转用例。
10. 尺寸修法：**后端照搬尺寸规则，前端兜底**（撑开组框并推开相邻对象）。
11. 依赖：**自研，字段名对齐 OTel GenAI**，不引入 Langfuse 和 promptfoo。
12. **开发环境连不到生产环境**：线上轨迹由管理员在生产后台下载轨迹包，拿到开发环境解压后分析；报告由后端生成，删除 `harness pull`。
13. **后台页一期只做列表和下载**：位置在「日志管理 → Agent 运行」，不做在线查看时间线。

### 11.2 已搁置（以后启用时可复用）

- 后台在线查看时间线：复用 `web/src/pages/admin/ai/trace/trace-timeline.tsx`。
- LLM 裁判打分。
- 录制回放，作为代码回归用的离线模式。
- 用户在面板里主动「上报这一轮」。

### 11.3 默认值（不需要用户决定，实现时照此执行）

- 用例格式用 JSON（不新增依赖）；输出目录 `.harness/`，加进 `.gitignore`。
- 单个 span 原文上限 256KB，单个 run 上限 8MB；整批运行的预算闸门默认 200 积分；单个用例默认超时 900 秒；事件补拉间隔 1 秒，等待生成任务的轮询间隔 5 秒。
- F5 前端兜底产生的位置调整照常保存，不进撤销栈；级联最多 3 层。
- F3 新建顶层对象的默认位置改为所有内容的左下方，避让时往下移；组内成员一行最多 4 个。
- `agent_events` 的保留期清理顺带实现，同样 14 天。
- `system.md` 升到版本 10。

### 11.4 仍待确认

- 文本节点高度是否固定为 324：没有在浏览器里实测，会影响 F1 和测试向量。实现前先在浏览器里量一次。
- `ask` 审批的 payload 里区分「选项提问」和「选模型」的字段名，实现 `ask_kind` 匹配时核对。
- 后端多实例部署时，一个 run 的多个片段是否可能落在不同实例上。这会影响「片段期间原文先写本机临时文件」是否可行。目前按「每个片段由启动它的实例负责上传」设计，不依赖跨实例共享。

---

## 12. 来源与假设

**外部来源**（访问日期 2026-10-20）
- OpenTelemetry GenAI agent spans：https://github.com/open-telemetry/semantic-conventions-genai/blob/main/docs/gen-ai/gen-ai-agent-spans.md
- OpenTelemetry GenAI spans（execute_tool）：https://github.com/open-telemetry/semantic-conventions-genai/blob/main/docs/gen-ai/gen-ai-spans.md
- Langfuse 仓库：https://github.com/langfuse/langfuse
- Langfuse 自部署定价：https://langfuse.com/pricing-self-host
- promptfoo 配置指南：https://www.promptfoo.dev/docs/configuration/guide/
- promptfoo 评测编码 Agent：https://www.promptfoo.dev/docs/guides/evaluate-coding-agents/

**项目文件**：见正文各处标注的路径。

**假设和未验证项**
- 文本节点高度 324：推断，未实测。
- Langfuse 自部署服务组成、promptfoo 多轮写法：来自检索，未逐条复核官方文档。
- 第 6.C 节除抽查过的条目外，其余由代码审查子代理读代码得出，没有逐条写测试复现。

---

## 13. 实现记录（2026-10-20）

> 状态：一期（A、B、C3/C4/C5）与二期（`diff`、`to-case`、C5 后半、C6、C8–C15、报告摘 stderr、审计 Agent 模块）均已实现。一期已提交，二期见 13.5。**都没有在浏览器和真实服务上验证。**

### 13.1 已实现

| 范围 | 内容 | 位置 |
| --- | --- | --- |
| A 排版 | F1 `NodeSize` 与共享向量 `node-size-cases.json`（18 例，前后端对照测试都通过）；F2 立即贴合与组内一行最多 4 个；F3 `settle` 避让，已有对象不动，新对象默认放左下方；F4 `canvas_arrange` 可排组，组与成员混排报错；F5 前端 `growGroupsToFitMembers`（撑大组框并推开邻居，最多连锁 3 层，不进撤销栈）；提示词升到 v10 | `canvasgraph/size.go`、`layout.go`、`apply.go`、`group_ops.go`；`web/src/utils/canvas/group-grow.ts`；`prompts/system.md` |
| 模型默认画幅 | `AgentCanvasService.SetModelRegistry`，节点没写比例时取模型比例参数默认值 | `service/agent/canvas.go`、`initialize/app.go` |
| C1 / C2 | `parentId` 与 `position` 成对判断：前端合并 `FRAME_PATHS`，后端撤销 `frameEdited` | `graph-changes.ts`、`revert.go` |
| C3 | 撤销前先保存，存不上就不撤销 | `utils/agent/undo-after-save.ts`、`use-agent-controller.ts` |
| C4 | 任务已创建、绑定节点失败：结果带 `bind_warning`，审批记为已执行，模型被告知「不要重复申请」 | `generate.go`、`approval.go` |
| C5（主要部分） | 批准时先记的报价，整体失败退全额、部分失败退失败条目的价格×张数 | `approval.go` 的 `unusedQuote` |
| C7 | 保存在途时本地版本被补丁推进，409 后直接以新版本重试 | `save-coordinator.ts` |
| B 后端 | 配置、迁移 `0008_agent_trace.sql`、记录器、脱敏与截断、轨迹包、报告生成 `tracereport`、管理员两个接口、审计 `agent.trace.export`、清理 | `service/agent/trace*.go`、`handler/agent/trace.go`、`repository/agent_trace.go` |
| B 后台页 | 「日志管理 → Agent 运行」列表与下载，审计动作中文文案 | `web/src/pages/admin/logs/agent-runs/`、`web/src/api/admin/agent-runs/` |
| B 命令行 | `agent/harness`：`run` / `report`、13 种断言、应答剧本、预算闸门 | `agent/harness/`，测试 `agent/test/harness/` |

### 13.2 与设计的出入

1. 清理任务做成了每天一次的 goroutine（带随机延迟，ctx 可取消），没有用 River 周期任务：仓库里没有 River 周期任务的先例。
2. `invoke_agent` 没有做成独立的 span 类型，只有第一行 `system` 和 `canvas`（`phase=before/after`）两种行；8.4 契约本来也没有它。
3. `agent_model_calls.channel_id` 用 varchar 存渠道 key（快照里只有 key）；多加了 `seq`、`segment_seq`、`storage_id`、`object_key` 几列。
4. 审计写失败时整个导出失败（不留记录就不交出用户对话原文），与技能管理「只打日志」的惯例不同。
5. 运行状态值以真实常量为准（`succeeded` 等 12 个），文档里早期的 `done / error` 已改。
6. 命令行 `wait_tasks` 改为轮询 `GET /generation-tasks/:id`；登录字段是 `username`；花钱的示例用例放在 `cases-paid/`。
7. 审计页的「模块」下拉没有加 `agent` 项（后端是否接受该前缀未核对），只补了动作文案。

### 13.3 没做

- 后台「在线看时间线」（已搁置，二期也没做）。
- 前端撑开组框、后台页面的视觉效果、Ctrl+Z 在 Agent 连续写画布时的真实表现，都没在浏览器里看过。
- C13 只处理了 `StartRun`：`DeleteSession` 和 service 层的 `Resume` 如果被一个过期的等待运行占着画布，仍会返回 60001。
- C9 只瘦了发给模型的视图，`session_jsonl` 里仍保存每轮完整目录；要一并瘦身，在 `saveState` 加一行 `elideOldCatalogs` 即可。
- C10 的收尾窗口仍有极小的竞态（`bridge.finish` 那次往返之间），彻底消除要 Go 配合在 finish 时返回「还有未处理插话」。

### 13.4 验证情况

- 后端不依赖数据库的测试全部通过；**带 `TEST_DATABASE_DSN` 的仓储与迁移测试没有完整复跑**（子代理的命令被自动模式拦截），需要你自己在本机跑：`TEST_DATABASE_DSN=… go test -count=1 ./internal/tests/repository/... ./internal/tests/migration/...`。
- `TestE2E_*` 4 个用例在改动前就因 Agent 暂不可用（60005，本机 Node 版本过低）而失败，与本次改动无关，但改动后没有再对照确认。
- 没有在真实 Node 进程和真实 LLM 上跑过任何用例，命令行工具只用假后端验证过。

### 13.5 二期（2026-10-20）

| 范围 | 内容 | 位置 |
| --- | --- | --- |
| 命令行 | `diff <目录A> <目录B>`：概要表、断言退化/改进、工具调用序列差异，整批目录按用例配对，有退化退出码 1；`to-case <轨迹包目录>`：把线上轨迹包还原成用例（初始画布、应答剧本、预算、保守断言），产物目录已加入 `.gitignore`，输出提醒线上内容需脱敏 | `agent/harness/diff.mjs`、`tocase.mjs`、`bundle.mjs` |
| C5 后半 | 提交时按最新节点输入重新报价，单价高于批准价就不提交该条目并记入 `failed`，由 `unusedQuote` 退款 | `service/agent/generate.go` |
| C6 | 继续一个被中断的运行时，用审批的真实结果替换历史里的占位工具结果 | `service/agent/resume_state.go`、`runtime.go` |
| C8 | Agent 改动同样并进撤销栈、重做栈里的每张快照（`rebaseSnapshot`），不再清栈 | `web/src/utils/canvas/history.ts`、`use-canvas-history.ts` |
| C9 | 发给模型的上下文里只留最新一份画布目录 | `agent/src/context.mjs`、`run.mjs`、`compaction.mjs` |
| C10 | finish 前检查未处理的插话，有就再跑一轮（上限 3 轮） | `agent/src/run.mjs` |
| C11 | codemode 失败文案里列出「状态未知」的进行中调用 | `agent/src/agent-tools/codemode.mjs` |
| C12 | 运行结束并对账后复位 `touched`，之后别处的 409 照旧弹冲突窗 | `web/src/utils/canvas/agent-sync.ts` |
| C13 | 审批过期时把运行置为 `expired`，`StartRun` 遇到过期的等待运行自动放行 | `service/agent/approval.go`、`run.go` |
| C14 | 「提示词优化」模式的 `update_node` 只允许改 `prompt` | `service/agent/bridge_tools.go` |
| C15 | 撤销只比较 Agent 写过的字段，前端补的默认值不再让节点删不掉 | `canvasgraph/revert.go` |
| 报告 | segment 异常结束时，可疑信号和时间线直接写出 stderr 里的错误（优先 `*Error` 行，其次含 `ERR_` 的行） | `tracereport/build.go` |
| 审计 | 后端 `module` 白名单加入 `agent`；前端加「Agent」模块项，`agent_run` 显示为「Agent 运行」 | `model/admin_audit_list.go`、`web/src/utils/admin/log-rules.ts` |

**与设计的出入**
1. C10 在 Node 里 finish 前再跑一轮，没有让 Go 自动续跑（原设计）。
2. stderr 摘取优先级与设计略有不同：Node 堆栈末尾常是 `code: 'ERR_…'`，按字面会摘不到包名，所以先取最后一条 `*Error` 行。
3. `to-case` 的选项题用提问开头约 12 个字作 `contains`，不用整句，因为模型重新措辞后整句匹配不上；选模型题用 `ask_kind:model` 加 `model_kind`。
4. C6 只对 `toolCallId` 与审批的 `tool_call_id` 完全相等时生效，codemode 嵌套调用的审批 id 形如 `父id.子id` 时保持原样。
5. C5 比较的是单价而不是总价（一个条目一个任务）。
6. C8 的 rebase 对「用户先改了 X、Agent 又改了 X」的情况，撤销后 X 会是 Agent 的值。
7. C11 的「状态未知」列表不区分只读和写入（工具声明里没有只读标记），只读调用也会被列出，无害但略啰嗦。

**验证**：后端不依赖数据库的测试全绿；前端 2743 个、`agent/harness` 191 个、Node 运行时 94 个（在临时副本里装依赖后跑，没动 `agent/node_modules`）全过。带 `TEST_DATABASE_DSN` 的仓储与迁移测试仍需自行运行。

另外：本机 `agent/node_modules` 是 10 月 8 日装的，缺 `@earendil-works/pi-codemode` 和 `quickjs-wasi`，导致开发容器里 Agent 一启动就崩（轨迹包 `stderr_tail` 里就是这个错误）。需要在宿主机执行 `cd agent && npm ci`。

