# Agent 聊天框改进：图片附件、活动行摘要、结束时的刷新感

> - **范围**：画布 Agent 面板的三处改进。① 聊天框能直接加图片作为消息附件，不再先落到画布；② 每段工具调用收起后有一句自然语言说明，每条操作带动作图标；③ 回答结束时消息流「刷一下」的问题。
> - **日期和状态**：2026-10-28，设计已确认，**已实现（未提交、未真机验证）**，实现与设计的出入见第 13 节。
> - **证据约定**：**事实**附文件路径（行号以 2026-10-28 的 master `5a06833` 为准）；**推断**是基于事实的判断；**提案**是尚未实现的设计。外部产品观察标「调研未复核」。
> - **方案演进**：需求 2 原推荐「动作短语 + 图标」，用户选了「让 Agent 写一句自然语言」，动作短语降级为总结到达前、单个工具时和总结失败时的兜底（6.2.3）。
> - **配套演示**：[Agent聊天框改进-演示.html](Agent聊天框改进-演示.html)（先读它）。
> - 上游设计：[画布Agent助手设计.md](../画布Agent助手设计/画布Agent助手设计.md) 6.8、6.12 和 13 节；本文修改其中「图片先落成画布节点」的约定（6.12、13 节 6b）。

## 1. 设计摘要

| # | 问题 | 推荐结论 |
| --- | --- | --- |
| 1 | 在聊天框加图会先在画布中心建一个图片节点，污染画布 | 图片直接上传为素材，以**附件托盘**显示在输入框上方，随消息发出；Agent 服务把图片作为这条用户消息的图片内容交给模型。Agent 需要拿它当参考时，自己用「修改画布」把它落成图片节点 |
| 2 | 工具调用收起后只有「✓ 69s 📖2 ✎1」，读不懂；展开后每行只有状态图标 | 每段活动行（保持现有的「每段各自折叠」）收起后显示**一句模型写的说明**，如「✨ 确认了角色设定并搭好 6 个镜头 · 12s」；说明到达前用动作短语兜底。展开后每行用**动作图标打头**，状态靠颜色和右侧文字 |
| 3 | 回答结束时消息像刷新了一下 | 根因已在代码里确认：流式消息结束时 key 从 `stream` 变成 `e{seq}`，整条消息被卸载重建，并重播淡入上浮动画；同时「已思考」插到正文上方、复制行和底栏出现，几处布局一起跳动。修法：稳定 key + 预留位置 + 底栏只做透明度过渡 |

## 2. 项目现状

### 2.1 事实

**图片附件**

- 输入框有三种加图入口：粘贴、拖入（`web/src/pages/canvas/agent/agent-editor.tsx:326-335, 470-471`）和 ＋ 菜单「上传图片」（`attach-popover.tsx:107-123`）。三者都经 `pickImageFiles` 过滤，一次最多 `MAX_ATTACH_IMAGES = 9` 张（`web/src/utils/agent/image-files.ts`），然后调用同一个 `onAttachImages`。
- `onAttachImages` 最终是 `flow.tsx:681-691` 的 `attachImagesForAgent`：先在视口中心落图片节点（`addUploadedNodes`，`use-canvas-menu.ts:338-398`），上传完成后把新节点作为 `@[名](node:id)` chip 插进输入框。
- 后端拒绝素材类引用：`backend/internal/service/agent/refs.go:90`，文案是「暂不支持引用素材附件…请先把图片放进画布」。
- 模型只能通过工具 `canvas_inspect_image {nodeIds}` 看图（`backend/internal/service/agent/inspect.go`）。对象存储给 URL，本地磁盘读出后转 base64，单张 ≤ 5 MB；格式只收 png/jpeg/webp/gif；素材必须属于当前用户。
- 用户消息事件 `message.user` 的 payload 只有 `{text, mode, selection}`（`agent/src/engine/engine.ts:407`），不存图片。
- 发起运行的链路：Go 路由 `POST /agent/sessions/:sid/runs` 转发（`backend/internal/router/router.go:136`）→ Agent 服务 `startRun`（`agent/src/api/service.ts:145`）→ `platform.checkMessage`（Go `platform_session.go:33` → `refs.go:48 checkRefs`）→ `engine.startRun`。插话走 `service.ts:158 interject`，校验链路相同。
- `uploadAsset(file, {signal, onProgress})`（`web/src/api/asset/index.ts:79-106`）与画布无关：先直传，失败时退回中转。资产库、参考图条、视频参数面板都在用它。
- 模型的 `vision` 字段前端可见（`web/src/api/agent/type.d.ts:44`）；模型选择里已经会标出「不能看图」（`agent-composer.tsx:137`）。Agent 服务按 `vision` 给 Pi 声明 `input: ["text","image"]`（`agent/src/model-provider.ts:155`）。
- `canvas_apply_ops` 的声明（`backend/internal/service/agent/tool_decls.json`）里没有「用已有素材建图片节点」的字段（检索 `asset`/`src` 无匹配）。

**活动行**

- 时间线把同一轮里连续的工具调用合成一个 `activity` 项（`web/src/utils/agent/timeline.ts:457-488 groupTools`），中间夹了助手发言就会断开。
- `ActivityGroup`（`web/src/pages/canvas/agent/activity-group.tsx`）运行中自动展开，结束后自动收起；用户手动开合过之后不再自动开合。收起时显示「状态图标 + 秒数 + 动词图标与计数」，文字版只放在 `title`/`aria-label` 里（`utils/agent/activity.ts activitySummary`）。
- 展开后的 `ToolRow` 左侧只有状态图标（Loader2 / Check / TriangleAlert / CircleStop），后面是 `label` 和 `summary`；涉及节点时整行可点击定位。
- 动词 → 图标映射已经有 17 个（`activity.ts VERB_ICONS` + `activity-group.tsx VERB_ICON`）；其中「看视频」和「截帧」都用 `Film`，分不出来。
- 事件 `tool.start`/`tool.end` 的载荷带 `id, name, label, verb, summary, is_error, node_ids`（`timeline.ts:270-311`）。
- 自动命名 `#autoTitle`（`engine.ts:542-570`）复用本会话的 Agent 模型（`a.rc.model_key`），调用 `completeSimple`，带 `X-Agent-Purpose: title` 头，有超时和 maxTokens，结果以 `session.titled` 事件推送。

**结束时的刷新感**

- 流式中的文本在 store 的 `sessions[id].stream` 里（`web/src/utils/agent/session-state.ts:174-192`），时间线末尾追加 `{type:"assistant", key:"stream", streaming:true}`（`timeline.ts:395-403`）。
- `message.done` 到达时清空 stream（`session-state.ts:162-164`），时间线改为出现 `{key: "e"+seq, streaming:false}`（`timeline.ts:259-268`）。
- 消息列表用 `motion.div key={entry.key}`，凡是不在 `historyKeys` 里的 key 都算「新」，从 `opacity 0, y 6` 淡入（`message-list.tsx:186-200`）；`trackHistoryKeys` 只认首屏和往上补的历史（`utils/agent/message-scroll.ts`）。
- `Assistant`（`message-list.tsx:494-557`）：「已思考」按钮只在 `!item.streaming` 时渲染，位置在正文**上方**；`!streaming && !copied` 时正文下方多一行 `h-6` 的复制按钮（平时透明，但占位置）。
- 运行结束时状态行由 `AnimatePresence` 退场（`status-line.tsx`），同时插入 `footer` 项（`run-footer.ts`），后者同样从 `y 6` 淡入。
- 回答结束时**不会**重新拉取会话、画布或视口（omp 报告逐项排查，我抽查了 `use-agent-canvas-sync` 和 `agent-sync.ts`，只有 `canvas.patch` 断号或 409 时才会 `fetchLatest`）。

### 2.2 设计约束

- 画布和会话都是用户私有的，素材按 `user_id` 隔离；附件也必须校验归属。
- 会话历史（`agent_events`，SQLite）不存图片内容，只能存引用。
- 不能看图的模型不能收图片（Pi 的 `input` 里没有 image）。
- 前端格式化和 lint 只用 oxfmt/oxlint；测试放 `web/src/tests/`、`backend/internal/tests/`、`agent/test/`。

### 2.3 当前缺口

1. 没有「消息附件」的概念：请求体、事件载荷、时间线都没有附件字段。
2. Agent 拿不到附件图片，也无法把附件落到画布。
3. 活动行收起后没有人能读懂的说明；展开后看不出每条是什么操作。
4. 流式消息和落库消息不是同一个 React 元素。

## 3. 用户与场景

- **用户**：在画布上做 AI 视频的创作者，常用 Agent 读画布、搭分镜、批量生成，单轮常有 5–30 次工具调用。
- **场景 A（附件）**：从别处截了一张风格参考图，粘贴进聊天框，问「照这个色调重写这 6 个镜头的提示词」。不希望画布中央多出一张图。
- **场景 B（附件当参考）**：贴一张角色照片，说「用这张图当参考生成三视图」。这时 Agent 应把图落到画布，连到新的图片节点上。
- **场景 C（活动行）**：长任务跑完后回看，想一眼知道每段做了什么，必要时展开看具体哪条失败了。
- **场景 D（刷新感）**：每条回答结束、每段中间发言结束时，正在读的文字不应闪烁或跳动。
- **成功指标**：附件发送后画布节点数不变；回看时不展开就能说出每段做了什么；回答结束前后同一段文字的 DOM 节点不变、位置不跳（可写测试验证）。

## 4. 调研

### 4.1 主流方案（访问日期 2026-10-28，基于对产品的已有了解，调研未复核，没有逐页截图核对）

| 产品 | 附件 | 过程折叠 | 对本项目的启发 |
| --- | --- | --- | --- |
| ChatGPT | 输入框上方缩略图托盘，上传中有进度，悬停出现 ×；发出后显示在用户气泡上方 | 推理过程收成「已思考 Xs」，可展开 | 托盘形态和发出后的位置直接采用 |
| Claude.ai | 附件卡片在输入框上方；不支持看图的情况会提示 | 工具调用收成一行说明，可展开 | 不支持看图时提示，而不是悄悄丢掉 |
| Codex（用户截图） | — | 每段工具调用之间穿插发言，各自收成「✎ 编辑了文件 运行了命令 已搜索网页」 | 用户指定的形态：每段各自折叠，动作带图标 |
| Cursor Agent | 输入框上方图片 chip | 每次工具调用一行，图标表示动作，失败标红 | 动作图标打头，状态用颜色表示 |
| Claude Code | — | 工具调用带模型写的 `description`，界面显示它 | 「模型写一句说明」可行；本项目选择事后异步总结（6.2.3 说明理由） |

### 4.2 比较与启发

- 附件放**输入框上方托盘**是事实标准；作为行内 chip 插进正文的少见，因为图片和文字的语义不同。
- 过程折叠有两派：只露结论的（ChatGPT 的推理）和每段都露一行的（Codex/Cursor）。用户选了后者，与本项目「文字和工具交错」的时间线结构一致，改动最小。
- 摘要文字有三种来源：规则拼句、调用时写意图、事后总结。只有事后总结写的是「做成了什么」，代价是多一次模型调用和几秒延迟，所以需要兜底文案。

### 4.3 依赖框架调研

- 图标用现有的 `lucide-react`，动效用现有的 `motion`，上传用现有的 `uploadAsset`，缩略图预览可复用项目里已有的预览组件（实现时确认具体组件）。
- **结论：未发现需要新增的依赖。**

## 5. 设计原则

1. **聊天归聊天，画布归画布**：用户在聊天框做的动作不改画布；画布只在 Agent 明确要用时才变，而且变动可撤销。
2. **不展开就读得懂**：收起状态必须是人话，图标是辅助，不是唯一信息。
3. **同一段文字，从生成到落库是同一个元素**：流式结束不应造成重建、淡入或位移。
4. **会导致布局跳动的元素提前占位，或者不占位**：结束后才出现的东西，要么一开始就在，要么浮在内容之上。
5. **兜底永远可用**：总结失败、模型不能看图、上传失败时，都有明确的退路。

## 6. 推荐方案

### 6.1 需求 1：图片附件

#### 6.1.1 入口与托盘

- 入口不变（＋ 菜单「上传图片」、粘贴、拖入输入框），但它们改为调用面板内的 `useAgentAttachments().add(files)`，**不再经过 `flow.tsx`**。
- **托盘**放在输入框容器内、编辑区上方，与选中节点的上下文行同层。每张缩略图 56×56，圆角 8，图片 `object-cover`，间距 6，超出一行横向滚动。
- 单张状态：

| 状态 | 外观 | 操作 |
| --- | --- | --- |
| 上传中 | 本地预览（`URL.createObjectURL`）加 40% 遮罩和环形进度 | × 取消上传（abort） |
| 已上传 | 正常显示 | 悬停出 ×；点击看大图 |
| 失败 | 1px 警告色描边，中央 ↻ 图标，`title` 里写原因 | 点 ↻ 重试，× 移除 |
| 模型不能看图 | 50% 透明度，托盘底部一行警告：「当前模型看不到图片，换一个能看图的模型或移除图片」 | × 移除 |

- 上限：每条消息最多 9 张（沿用 `MAX_ATTACH_IMAGES`）。超出时只收前面的，toast 提示「一条消息最多 9 张，多出的 N 张没加」（沿用 `pickImageFiles` 的 `clipped`）。
- 非图片文件：粘贴或拖入时忽略并提示（现状如此）；＋ 菜单的「导入剧本/文本」不变。
- **发送按钮**：有图片在上传中时禁用，提示「图片上传中…」；有失败的图片时禁用，提示「有图片上传失败，重试或移除后再发送」；当前模型不能看图且托盘里有图时禁用，提示同托盘警告。
- **当前模型不能看图**：上传入口仍可点，但上传完成就显示「不能看图」状态（不偷偷拦掉，让用户自己切换模型）。
- 发送成功后清空托盘；发送失败（接口报错）保留托盘和文字。
- **插话**（运行中发消息）同样支持附件，规则一致。
- 选中节点引用（浅色占位 → 深色 chip）、`@` 节点、模型和技能 chip 都不变。

#### 6.1.2 发出后的显示

- 用户消息气泡上方一行缩略图（48×48），右对齐，与气泡同侧；点击看大图（复用现有的图片预览组件，实现时确认）。
- 缩略图地址用素材的稳定路径 `/files/*` 加 `?v=thumb`（`backend/internal/handler/files.go`：本地存储由后端直出，对象存储在访问时现签名并 302 跳转，**地址不会过期**）。
- 图片加载失败（素材被删）时显示灰色占位和「图片已失效」。
- 历史回放显示相同内容：附件来自 `message.user` 的 payload（6.1.4）。
- 插话队列（输入框顶部的托盘）里的条目一行放不下缩略图，改为行尾写「N 张图」；读到之后进消息流，和普通消息一样显示缩略图。

#### 6.1.3 给模型

- Agent 服务在 `startRun`/`interject` 时，把附件交给 Go 解析（6.1.4 `checkMessage` 扩展）。Go 复用 `inspect.go` 的 `loadImage`：校验归属和格式，对象存储返回 URL，本地磁盘返回 base64（单张 ≤ 5 MB）。
- 交给 Pi 的这条用户消息，content 为 `text` + 每张 `image` 块。文本末尾追加一行（模型可见，不显示给人）：`附件图片：图1(asset:123)、图2(asset:456)`，让模型知道 asset id，以便后面重新看图或落画布。
- **上下文裁剪**（提案，沿用 `canvas_inspect_image` 的思路）：只保留最近 2 条带图片的用户消息里的图片，更早的换成文字「[图1 已省略，需要时调用 查看图片 并传 assetIds]」。写进会话历史（`session_jsonl`）前去掉图片。**更正**：此前记成「现有的裁剪代码在前端、附件复用」不对——实现时检索确认仓库里**没有**这套逻辑（`compaction.ts` 只对摘要把图片换成「[图片]」），所以新写了：`agent/src/context.ts` 的 `pruneOldImages`，在发给模型前的 `onPayload` 钩子里只保留最近 2 条带图片的消息（用户附件和工具结果一起算），更早的每张换成一段说明。Pi 自己存的历史（`pi_*` 表）里仍带图片，本地存储下每张最多 5 MB，对象存储只是地址；是否要在入库前去图留作后续。

#### 6.1.4 Agent 用附件做参考（用户已确认：Agent 需要时自己落到画布）

- `canvas_inspect_image` 增加可选参数 `assetIds: string[]`（与 `nodeIds` 二选一或并用，合计 1–4 张），用来重看已省略的附件。
- `canvas_apply_ops` 的「新建节点」操作增加可选字段 `assetId`：只对图片节点有效，节点建出来就是已完成状态，`src` 指向素材地址。素材必须属于当前用户、是 png/jpeg/webp/gif，不满足时返回业务错误。**与最初设计的出入**：原设计还要求「必须是本会话某条消息的附件」，但消息事件存在 Agent 服务的 SQLite 里，Go 没有这份记录，所以实际只靠素材归属把关（别人的素材和不存在一样报「不存在」）。`canvas_inspect_image` 的 `assetIds` 同理。
- 系统提示词补一句：用户附件默认只用来理解；要拿它做生成参考时，先用 `canvas_apply_ops` 带 `assetId` 落成图片节点再连线。这一改动可以和其他画布改动一样撤销。

#### 6.1.5 删除的旧逻辑

- `flow.tsx` 的 `attachImagesForAgent`，以及 `AgentPanel`/`AgentComposer` 上的 `onAttachImages` 属性链。
- `agent-composer.tsx` 里「插入 chip」那段附件逻辑；`AgentEditor` 的 `onImages` 改为回调到托盘。
- 上游设计文档 6.12（第 850、859 行附近）和 13 节「上传图片：先落成图片节点」的描述需要更新（实现时回写）。

### 6.2 需求 2：活动行摘要与操作图标

保持「每段各自折叠」：分组规则（`groupTools`）不变，运行中自动展开、结束后自动收起、手动开合以后不再自动开合，这些也都不变。

#### 6.2.1 收起时的一行

```
[主图标] 一句说明                         · 1 项失败   12s  ›
```

- **主图标**：取本段中优先级最高的动作的图标。优先级：生成 > 删除 > 修改 > 截帧 > 整理 > 看视频 > 看图 > 搜索 > 读网页 > 脚本 > 提问 > 计划 > 技能 > 查模型 > 查任务 > 读取 > 其他。主图标后面不再放动词图标串。
- **一句说明**：本段结束后模型写的总结（6.2.3）；还没到、只有 1 个工具、或者总结失败时，用**动作短语**兜底（6.2.2）。字号 12.5、颜色 `muted-foreground`，单行省略，`title` 里放全文。
- **失败数**：`· N 项失败`，警告色（现状保留）。
- **耗时**：右侧 `tabular-nums`，60 秒以上写成 `1m 12s`。
- 中止的段：主图标换成 `CircleStop`，说明写「已中止」加兜底短语，不调用总结。
- 运行中：头部仍是 `Loader2` 加「处理中」（现状），实时动态在状态行里。
- 摘要从兜底短语换成模型说明时，用 160ms 交叉淡入；宽度可以变化，不做宽度动画（单行左对齐，不会推动其他内容）。

#### 6.2.2 兜底动作短语

按动词首次出现的顺序拼接，最多 3 种，再多写「等 N 项」；同一动词出现多次时加「N 次」。

| 动词 | 短语 | 图标（lucide） |
| --- | --- | --- |
| 读取 | 读取了画布 | BookOpen |
| 修改 | 修改了画布 | PencilLine |
| 整理 | 整理了布局 | LayoutGrid |
| 删除 | 申请了删除 | Trash2 |
| 看图 | 查看了图片 | ScanEye |
| 计划 | 更新了计划 | ListChecks |
| 提问 | 向你提了问题 | MessageCircleQuestion |
| 查模型 | 查看了可用模型 | Clapperboard |
| 生成 | 申请了生成 | WandSparkles |
| 查任务 | 查询了任务 | ListTodo |
| 技能 | 查阅了技能 | Lightbulb |
| 脚本 | 运行了脚本 | Terminal |
| 看视频 | 查看了视频 | Film |
| 截帧 | 截取了视频帧 | **Camera**（改：原来和看视频共用 Film） |
| 搜索 | 搜索了网页 | Search |
| 读网页 | 读取了网页 | Globe |
| 其他 | 调用了「{label}」 | Wrench |

例：「读取了画布、搜索了网页 2 次、修改了画布」「读取了画布、查看了图片、修改了画布等 5 项」。

#### 6.2.3 模型写的一句说明（用户已确认：结束后异步总结）

- **触发**：Agent 引擎里一段工具调用**闭合**时（后面出现了助手发言、审批或提问卡片，或者运行结束），且这段至少有 **2 个**工具调用、状态不是中止。只有 1 个工具的段直接用兜底短语，不调用模型（省钱，而且一条操作的短语已经足够清楚）。
- **模型**（用户已确认）：复用本会话的 Agent 模型，与自动命名 `#autoTitle` 的做法一致（`engine.ts:557`），`completeSimple`，请求头 `X-Agent-Purpose: activity-summary`，超时 8s，`maxTokens` 64。单独配便宜模型留到后续（10.2）。
- **输入**：用户本轮消息的前 200 字（去掉 @ 引用的内部语法），加上本段每个工具的 `label`、`summary`、是否失败（每条 ≤ 120 字）。
- **提示词**（提案）：「用一句话概括这些操作做成了什么，过去时，不超过 20 个字，和用户同一种语言，不加句号和引号；只描述结果，不要编造没有出现的内容；有失败的如实说明。」
- **清洗**：取第一行，去掉引号和结尾标点，最多 24 字；清洗后为空时视为失败。
- **事件**：持久化事件 `activity.summarized`，`{group_id, text}`。`group_id` = 本段第一个工具调用的 id（即 `tool.start.data.id`）。可能在 `run.status` 之后才到，前端照常处理。
- **并发**：不阻塞运行，与主循环并行；同一轮最多同时 2 个总结请求，排队的按顺序执行；运行被删除或服务关闭时放弃。
- **失败**：超时、报错或清洗后为空都不重试，不发事件，前端一直显示兜底短语。
- **计费**（用户 2026-10-28 确认：**算**）：和会话命名一样，照常预检预算、计入用户用量，不算对话的上下文大小。Go 新增用途 `activity`（`bridge.go ModelPurposeActivity`，`ModelPurposeOf` 认它；否则会被当成普通对话，算进上下文大小）。注：先前一版文档写成「不计入用量」，是把「会话命名不算」当成了事实，实际代码里命名也是计费的（`bridge.go` 的 `ModelPurposeTitle` 注释「照常预检预算、计费」），已更正。
- **历史**：事件落库，回放时直接显示说明；旧会话没有这个事件，显示兜底短语。

#### 6.2.4 展开后的每一行

```
│ [动作图标] 名称    摘要                         [右侧状态/定位]
```

- 左侧图标换成**动作图标**（同 6.2.2 表格），默认 `muted-foreground`。
- 进行中：动作图标换成旋转的 `Loader2`。
- 成功：只显示动作图标，不再画对勾。
- 失败：动作图标改为警告色，右侧写「失败」（警告色）。
- 中止：动作图标 50% 透明度，右侧写「已中止」。
- 可定位（有 `nodeIds`）：悬停时右侧出现 `LocateFixed` 图标，整行可点击（现状行为保留）。
- 名称、摘要的字号和截断规则不变。

### 6.3 需求 3：回答结束时的刷新感

#### 6.3.1 根因（事实，按影响排序）

1. **重新挂载并淡入**：流式项 key 为 `stream`，落库后变成 `e{seq}`，`motion.div` 被卸载重建，Markdown 重新解析；新 key 不在 `historyKeys` 里，于是重播 `opacity 0 → 1, y 6 → 0`。正在读的文字会瞬间变透明再浮回来。这在每段中间发言结束时也会发生。
2. **「已思考」插到正文上方**：只有 `!streaming` 时才渲染，结束瞬间正文被往下推一行（约 24px）。
3. **复制行一闪**：`message.done` 先到、`run.status` 后到，中间这一刻 `copied=false`，正文下方多出一行 `h-6`；底栏出现后又消失。
4. **状态行退场与底栏入场叠加**：状态行收起，底栏从 `y 6` 淡入，贴底滚动又跟着调了两次。

#### 6.3.2 修法（提案）

| # | 改动 | 位置 |
| --- | --- | --- |
| F1 | **稳定 key**：助手消息的 key 改为 `a:{runId}:{n}`，n 是这条消息在本轮里的序号（从 0 开始，按 `message.done` 的顺序计）；流式项的 key 用「本轮已有 `message.done` 条数」作为 n。落库后 key 不变，React 复用同一个元素，不重建也不重播动画 | `timeline.ts` 的 `message.done` 分支和流式项 |
| F2 | **「已思考」提前出现**：流式中只要已经有 thinking，就显示同一位置的按钮，文字为「思考中」（不可展开，流光样式沿用 `ShimmerText`），结束后变成「已思考」。思考总在正文之前，正文开始时按钮已经在位，结束时不再插入 | `message-list.tsx` 的 `Assistant` |
| F3 | **复制按钮不占位**：中间消息的复制按钮改为相对消息右下角绝对定位，悬停出现，不占布局高度；运行进行中（本轮未结束）不渲染 | 同上 |
| F4 | **底栏只做透明度过渡**：`footer` 项入场只做 `opacity 0 → 1`（不位移），时长 `DURATION.base`；状态行退场改为 `AnimatePresence mode="popLayout"` 并只做透明度动画，退场时不占位，底栏直接接在它原来的位置 | `message-list.tsx`、`status-line.tsx` |
| F5 | **去掉流式光标**（用户 2026-10-28 确认不要）：删掉 `Assistant` 里 `item.streaming` 时的 `animate-pulse` 光标（`message-list.tsx:531-533`）。文字本身在增长，不需要光标提示；结束时也少了一次行尾变化 | `Assistant` |

不需要改的：画布同步、会话列表、滚动锚定逻辑（排查结论见 2.1）。

## 7. 状态与异常

| 场景 | 行为 |
| --- | --- |
| 附件上传失败 | 缩略图显示失败态，可重试或移除；发送按钮禁用，并写明原因 |
| 发送时附件校验失败（素材已删、不属于用户、格式不支持、本地存储超过 5 MB） | 接口返回 10001，文案点名哪张图（「图 2：图片超过 5 MB」）；不创建运行，托盘和文字保留 |
| 当前模型不能看图 | 托盘图片变暗并显示警告，发送禁用；切换到能看图的模型后恢复 |
| 发送后换成不能看图的模型继续对话 | 历史里的附件照常显示，但不交给模型（Agent 服务按 `vision` 过滤，替换成「[图1：当前模型不能看图]」） |
| 插话带附件 | 与发起运行相同的校验；插话进入队列时显示缩略图 |
| 附件链接失效 | 缩略图显示「图片已失效」占位；不影响对话 |
| Agent 用附件落画布但素材不是本会话附件 | `canvas_apply_ops` 返回业务错误，模型自行处理 |
| 摘要总结超时或失败 | 一直显示兜底短语，不重试 |
| 摘要在运行结束之后才到 | 正常替换（交叉淡入） |
| 撤销本轮 | 附件落到画布的节点随本轮改动一起撤销；附件本身（素材）不删 |
| 断线重连 | `activity.summarized` 是持久化事件，补齐事件时一并回放 |

## 8. 数据与工程影响（提案）

### 8.1 接口

| 接口 | 变化 |
| --- | --- |
| `POST /agent/sessions/:sid/runs` | 请求体加 `attachments?: { asset_id: string }[]`，0–9 项，去重 |
| 插话接口（`interject`，现有路径不变） | 同上 |
| Go 平台 `checkMessage`（`platform_session.go`） | 参数加 `attachments`；校验归属、图片类型、数量；返回每张的 `{asset_id, name, mime_type, width, height, url}` 供落库，以及给模型用的 `{mime_type, url \| data}`（本地存储 >5 MB 时报错） |
| `canvas_inspect_image` | 参数加 `assetIds?: string[]`；与 `nodeIds` 合计 1–4 张 |
| `canvas_apply_ops` 新建节点 | 加 `assetId?: string`，只对图片节点有效；必须是本会话的附件 |

### 8.2 事件

| 事件 | 变化 |
| --- | --- |
| `message.user` | payload 加 `attachments?: AgentAttachment[]`（`{asset_id, name, mime_type, width, height, url}`），`url` 是稳定路径 `/files/*`（不过期，不存签名直链）；只存引用和展示信息，不存图片内容 |
| `activity.summarized`（新） | 持久化，`{group_id: string, text: string}` |

### 8.3 前端

- 新增 `web/src/hooks/use-agent-attachments.ts`：托盘状态（`{localId, file, previewUrl, status: uploading|done|error, progress, asset?, error?}[]`），提供 add/retry/remove/clear/toPayload，内部用 `uploadAsset` 和 AbortController，卸载时回收预览 URL。
- 新增 `web/src/pages/canvas/agent/attachment-tray.tsx`；用户消息组件显示附件行。
- `timeline.ts`：`UserItem` 加 `attachments`；`ActivityItem` 加 `groupId`、`summary?: string`；处理 `activity.summarized`；按 F1 改 key。
- `activity.ts`：`activitySummary` 返回 `{icon, phrase, seconds, errors}`，新增 `VERB_PHRASE` 和优先级表；`frame` 改用 Camera。
- `activity-group.tsx`：按 6.2.1、6.2.4 改渲染。

### 8.4 Agent 服务与 Go

- `agent/src/api/service.ts`：`startRun`/`interject` 透传 `attachments`，从 `checkMessage` 拿到解析结果；`engine.startRun`/`interject` 把图片块拼进用户消息，`message.user` 记录附件。
- `agent/src/engine/engine.ts`：分段闭合检测和异步总结（仿 `#autoTitle`），新增 `activity-summary.ts` 纯函数（提示词构建、清洗）。
- Go：`refs.go`/`platform_session.go` 扩展附件校验；`inspect.go` 抽出按 assetId 取图的函数供两处复用；`canvas_apply_ops` 支持 `assetId`；`tool_decls.json` 更新两个工具的声明；系统提示词补一句。

### 8.5 实现难点

- 分段闭合要在引擎侧判断，规则要和前端 `groupTools` 一致（同一轮里连续的 `tool.start`，被 `message.done`、审批、提问打断）。建议把规则写成双方共用的测试用例。
- 上下文图片裁剪如果与工具结果图片共用一套计数，需要统一「最近 2 条」的口径。
- 附件 `url`：签名直链会过期，后端重定向路径 `/files/*` 不会（用户确认，见 `handler/files.go`）。落库和显示一律用 `/files/*` 稳定路径；交给模型时由 Go 现签名（对象存储）或读出转 base64（本地存储），不落库。

## 9. 方案取舍

### 9.1 决策矩阵（权重：用户确认的「读得懂 / 不打扰画布」优先，其次实现成本）

| 维度（权重） | 附件进消息（选） | 附件作引用、按需看 |
| --- | --- | --- |
| 模型一定看过图（0.4） | 5 | 2 |
| token 成本（0.2） | 3 | 5 |
| 实现成本（0.2） | 3 | 4 |
| 与主流一致（0.2） | 5 | 2 |
| 合计 | **4.2** | 3.0 |

| 维度（权重） | 异步总结（选） | 调用时写 intent | 规则拼句 |
| --- | --- | --- | --- |
| 读起来像人话（0.4） | 5 | 4 | 2 |
| 无延迟（0.2） | 2 | 5 | 5 |
| 成本（0.2） | 3 | 4 | 5 |
| 改动面（0.2） | 4 | 2 | 5 |
| 合计 | **3.8** | 3.8 | 3.8 |

三者分数接近；用户明确选择了异步总结，理由是写的是「做成了什么」。兜底短语保证延迟和失败时仍然可读。

### 9.2 已搁置的方案

- **整轮过程折成一行**（只露最终回答）：用户选了每段各自折叠。将来如果长任务的消息流太长，可以在此基础上加「本轮过程」外层折叠，不影响本方案的数据结构。
- **附件作引用、模型按需看**：省 token，但模型可能不看图就回答。`canvas_inspect_image` 的 `assetIds` 参数保留了这条路。
- **工具调用时写 intent**：零延迟，但要改全部工具声明，写的也是意图而不是结果。

### 9.3 明确放弃

- 附件拖到画布的快捷操作（本期不做，见 10.2）。
- 文本、视频、音频附件（本期只做图片）。

## 10. MVP 与后续

### 10.1 本期（三个需求可以独立上线，建议顺序 3 → 2 → 1）

1. **刷新感（F1–F5）**。验收：时间线测试中，流式项的 key 等于随后 `message.done` 项的 key；组件测试中，`message.done` 前后正文是同一个 DOM 元素；「已思考」在流式阶段已经出现。
2. **活动行**。验收：`activitySummary` 的兜底短语、主图标优先级、「等 N 项」都有单元测试；`activity.summarized` 到达后显示说明，历史回放也显示；单工具段和中止段不调用总结；展开行用动作图标，失败行右侧有「失败」。引擎测试覆盖分段闭合和总结失败时不发事件。
3. **图片附件**。验收：加图后画布节点数不变；托盘覆盖上传中、失败重试、移除、9 张上限、模型不能看图时禁用发送；发出的 `message.user` 带附件，历史回放显示缩略图；Agent 服务给 Pi 的消息含图片块；Go 校验归属、格式、大小并点名报错；`canvas_apply_ops` 带 `assetId` 能建出已完成的图片节点，非本会话附件会被拒。

### 10.2 后续

- 已发送附件悬停时提供「放到画布」按钮。
- 摘要可以单独配一个便宜模型（后台配置）。
- 文本和视频附件。

## 11. 待确认问题与已确认事项

### 11.1 已确认（用户回答，2026-10-28）

1. 加图时直接上传素材，图片作为附件显示在聊天框上方，随消息带上 URL。
2. 附件图要当生成参考时，由 Agent 自己落到画布。
3. 折叠粒度：每段各自折叠（像 Codex 截图那样）。
4. 收起时的说明：让 Agent 写一句自然语言。
5. 说明由谁写：每段结束后用模型异步总结。
6. 展开行：动作图标打头，状态靠颜色和右侧文字。
7. 附件地址：签名直链会过期，后端重定向的 `/files/*` 不会，用后者。
8. 上下文图片裁剪：最近 2 条带图片的消息保留图片（此条原记作「在前端已有、附件复用」，实现时确认不存在，改为新写，见 6.1.3）。
9. 活动段总结**计入**用户用量（和会话命名一样照常计费；先前记成「不计入」，已更正）。
10. 总结复用当前会话模型（与自动命名一致）。
11. 去掉流式输出时的闪烁光标（F5）。

### 11.2 已搁置的决定

- 整轮过程折成一行（9.2）。

### 11.3 默认值（不需要用户决定，可调）

- 每条消息最多 9 张附件；本地存储单张 ≤ 5 MB（沿用看图工具的限制）。
- 上下文只保留最近 2 条带图片的用户消息里的图片。
- 只有 1 个工具的段不做总结；总结超时 8s、`maxTokens` 64、≤ 20 字。
- 插话也支持附件。
- 模型不能看图时，托盘图片变暗并禁用发送（不自动移除）。

### 11.4 仍待确认

- 无。（实现中更正了两处设计假设，见 6.1.3、6.1.4 和第 13 节。）

## 12. 来源与假设

### 12.1 外部来源

- ChatGPT、Claude.ai、Cursor、Claude Code 的交互基于已有了解，2026-10-28 没有逐页核对（调研未复核）。
- Codex 的折叠形态来自用户提供的截图（`waveterm_paste_1793153829937_qr8dz7.png`）。

### 12.2 项目文件

见 2.1 各条后的路径。代码摸底由 omp（gemini-3.8-flash）完成三份只读报告，关键结论（`timeline.ts` 的 key、`message-list.tsx` 的淡入、`flow.tsx` 的附件落画布、`refs.go` 的拒绝、`engine.ts` 的命名模型、`Assistant` 的「已思考」位置）已逐一抽查原文核对。

### 12.3 假设与未验证

- 附件大图复用画布的 `MediaLightbox`（已核对并实现）。
- 刷新感的根因由代码推断，没有在浏览器里录屏复现；F1–F5 修完后需要用户真机确认手感。

## 13. 实现记录（2026-10-28，分支 release/v0.5.4，未提交）

### 13.1 已实现

| 需求 | 做了什么 | 主要文件 |
| --- | --- | --- |
| ③ 刷新感 | 助手消息 key 改为 `a:{runId}:{n}`（流式和落库同一个）；「思考中→已思考」同位置；复制按钮绝对定位不占位；底栏只淡入、状态行 `popLayout` 只淡出；去掉流式光标 | `web/src/utils/agent/timeline.ts`、`web/src/pages/canvas/agent/message-list.tsx`、`status-line.tsx` |
| ② 活动行 | `groupId`/`summary`、`activity.summarized` 事件、主图标优先级、兜底动作短语、展开行动作图标、截帧改相机图标；引擎按已落库事件分段、≥2 工具才异步总结（并发 2、超时 8s、收尾最多等 10s）；Go 新增 `activity` 用途 | `web/src/utils/agent/activity.ts`、`activity-group.tsx`；`agent/src/activity-summary.ts`、`engine.ts`；`backend/.../bridge.go` |
| ① 附件 | 托盘（上传/重试/取消/9 张上限/模型不能看图禁用发送）、气泡缩略图（灯箱）、请求带 `attachments`；Agent 服务 `POST …/runs`/`interject` 校验并交 Go；Go `POST /internal/platform/v1/attachments/check`；`message.user` 记附件；图片块随消息交模型；`pruneOldImages`；`canvas_inspect_image` 加 `assetIds`；`canvas_apply_ops` 的 `create_node` 加 `assetId`；系统提示词 v18 | `web/src/hooks/use-agent-attachments.ts`、`attachment-tray.tsx`、`message-attachments.tsx`；`agent/src/attachments.ts`、`api/service.ts`；`backend/.../attachments.go`、`inspect.go`、`canvasgraph/apply.go` |

### 13.2 与设计的出入

1. **裁剪代码是新写的**，不是复用前端已有逻辑（6.1.3）。
2. **`assetId` 只校验归属，不要求「本会话附件」**（6.1.4）。
3. **插话托盘显示「N 张图」**，不是 32×32 缩略图（6.1.2）。
4. **`CheckMessage` 没改**：附件校验是独立接口 `attachments/check`，不动原有测试和契约。
5. **活动说明计费**：用户改为「算」，和会话命名一样照常计费；Go 新增 `activity` 用途，不算对话上下文（6.2.3）。
6. 把发送按钮的禁用原因放在按钮的 tooltip / 悬停文案里（上传中、有失败、模型不能看图）。
7. 没带文字只带图片时发送仍被禁用（消息文字必填，沿用现有规则）。
8. 基线文件 `testdata/tool_defs_baseline.json` 随两个工具声明的变化更新了（惯例，历史提交都这么做）。

### 13.3 验证

- Web：`bun test` 全量；`tsc -b` 只剩已有的 Tiptap 类型报错；oxlint 无新增；oxfmt 已格式化。
- Agent 服务：`node --test`，新增 5 个测试文件（活动说明、附件、裁剪、接口层）；剩 3 个 Windows 路径相关的「真起进程」用例失败，在没有本次改动的干净状态下同样失败。
- Go：`go test ./internal/...` 全绿，除 `internal/tests/scripts` 的一个更新回滚用例（路径格式差异，与本次无关）。
- **未做**：浏览器里真机看（尤其刷新感手感、托盘、灯箱）；本地存储 + 多张大图时的 Pi 历史体积；总结模型的真实延迟与费用。

### 13.4 遗留

- 恢复运行（崩溃续跑）后，排队中的插话附件的图片块会丢（事件里仍有引用，下一次重发要重新校验）。
- Pi 历史里仍保留图片内容（见 6.1.3 更正），可后续在入库前去图。
- 分段闭合在崩溃恢复后会丢失（`group_id` 对不上），该段继续显示动作短语。
