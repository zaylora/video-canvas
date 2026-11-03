# 后台 Agent 助手设计

> - **范围**：在管理后台加一个对话式助手，代管理员完成「模型参数配置」「调价」（一期），以及「对接新网站 API：搜文档 → 匹配或编写协议插件 → 验证 → 建渠道与模型」（二期）。所有写操作先出变更单，管理员确认后才落库。
> - **日期与状态**：2026-11-02，设计已确认，待实现。
> - **证据约定**：**事实**附文件路径（仓库现状，2026-11-02 读取）或来源链接；**推断**是基于事实的判断；**提案**是尚未实现的设计。
> - **方案演进**：
>   1. 最初提的是「新接一个搜索 API（Tavily/Serper 等）」，用户已选。随后核对代码发现画布 Agent 已有 `web_search` / `web_fetch`（Exa → Bing → DuckDuckGo，可选 SearXNG，Go 发请求、有 SSRF 防护），改为**直接复用**，不新接搜索服务（见 §2.4、§11）。
>   2. 用户要求宿主复用项目现有的 pi agent（`agent/` 服务），不新写 Agent 循环；本文按「在 `agent/` 里加 `admin` 场景」设计（见 §6.1）。
> - **配套演示**：[后台Agent助手-演示.html](后台Agent助手-演示.html)（先读它，讲流程和数据）；聊天框视觉**复用画布助手**（§6.2.1，2026-11-02 定）；[后台助手聊天框原型.html](后台助手聊天框原型.html) 是被取代的第二版，只作参考。

## 1. 设计摘要

**问题**：后台配置门槛高。对接新平台要自己找或写 JS 协议插件；模型的能力参数（生成方式、参数选项、参考素材上限）和价格（按次/按秒/Token、规格分档、成本）都要手填，调价需要逐个模型改、自己算。

**目标**：管理员用一句话完成这些操作，比如「把视频类模型都按成本加价 30%」「可灵 2.1 的分辨率参数按官方文档补齐」「帮我对接 xx 平台的视频接口」。Agent 负责查资料、算数、写配置，管理员只做确认。

**推荐结论**：

- 复用 `agent/`（pi-durable）的会话、运行、审批状态机，新增 `mode=admin` 场景：独立的系统提示词和后台工具集，会话不绑画布（`canvas_id=0`）。
- 只读操作自动执行；**每个写操作都汇总成一张「变更单」**（逐条显示改前/改后、算式和校验结果），管理员可以整单或逐条批准，执行时按原接口权限重新校验，并做并发冲突检测。
- 调价公式：上游官方价 → 实时汇率换人民币 → 乘以「1 元 = N 积分」得到积分成本 `cost` → 乘以加价率得到售价，向上取整。也支持纯指令，比如「全体涨 10%」。
- 入口是全后台通用的右侧抽屉，自动带上当前页面的上下文。
- 一期做模型参数与调价，二期做插件全链路。

## 2. 项目现状

### 2.1 协议插件（事实）

- 插件是 CommonJS 单文件纯函数，导出 `meta` 和钩子（`buildSubmitRequest` / `parseSubmitResponse` / `buildQueryRequest` / `parseQueryResponse` 等）。**插件没有网络、没有文件 I/O、不能用 async**；HTTP 请求由 Go 宿主发出。见 `backend/docs/plugin-contract.md`。
- 内置插件 21 个，位于 `backend/plugins/*.js`，经 `embed.FS` 嵌入，启动时注册（`backend/internal/initialize/builtin_plugins.go`）；上传的插件存在 `ai_plugins` / `ai_plugin_versions` 表，**版本不可变**（`backend/internal/model/ai_plugin.go`）。截图里总览显示「33 个插件」，推断是内置加上传之和。
- 上传接口 `POST /api/v1/admin/ai/plugins` 仅限 `super_admin`，上传时 runner 会预检并自动分配版本号（`backend/internal/service/ai_plugin.go` `Upload`）。
- runner 基于 goja，单个钩子 200ms 超时，`ctx` 和返回值各 ≤1MB（`backend/internal/provider/pluginrunner/`）。
- **验证手段都挂在模型上**：`POST /admin/ai/models/:key/dry-run` 只渲染请求、不发出；`POST /admin/ai/models/:key/test-run` 真实调用上游、不扣用户积分；`GET /admin/ai/test-runs/:id/trace` 能看各钩子的输入输出和 HTTP 报文（`backend/internal/router/router.go:258-266`）。目前没有「对一段未上传的插件代码做试跑」的接口。

### 2.2 渠道与模型（事实）

- 渠道 `ai_channels` 只管 `base_url`、请求头、限流；API Key 和 Secret Key 用 AES-256-GCM 加密存在 `ai_secrets`，写入接口是 `PUT /admin/ai/channels/:key/secret`（仅 `super_admin`）。
- 模型配置 `ModelConfig` 存在 `ai_config_revisions.body_json`，每个模型一行，**没有版本号或乐观锁**（`backend/internal/provider/modelcfg/types.go:88`）。字段包括 `channels[0].{channel, upstream_model, protocol{plugin,version,settings}}`、`capabilities{ops, refs, prompt, params, context, vision, video}`、`pricing`。
- 生成参数 `ParamField`：`type` 取 enum/number/boolean，另有 `options`、`default`、`min/max/step`、`open`、`spec`（可作规格价条件）、`fanout`（`backend/internal/provider/modelcfg/capabilities.go`）。
- `SaveModel`：未启用的模型有校验问题也能保存；**已启用的模型保存即生效，必须通过启用检查**，否则拒绝保存（`backend/internal/service/ai_config_save.go:32`）。启用检查涵盖：正文校验、渠道存在且启用、插件存在且启用、版本支持该 kind、settings 合法、需要鉴权时渠道已配 Key（`backend/internal/service/ai_config.go` `CheckEnable`）。
- 拉取模型：`POST /admin/ai/channels/:key/pull-models`，由 Go 直接请求 `{base_url}/v1/models`，不落库。

### 2.3 价格与积分（事实）

- `Pricing{billing: per_call|per_second|token, unit, per_second, token{in,out}, tiers[], cost}`。价格都是**整数积分**；token 按「积分 / 百万 Token」计价；`tiers` 是规格价格，条件命中最多的一条胜出；`cost` 是积分成本，只在管理端展示，不参与扣费（`backend/internal/provider/modelcfg/pricing.go:25-70`）。
- 计价纯函数：`backend/internal/provider/modelcfg/quote.go`，前端有等价实现 `web/src/utils/pricing/quote.ts`。
- **没有全局的「积分 ↔ 人民币」汇率**。充值套餐各自定义 `price_fen` 和 `base_credits`（`backend/internal/model/payment.go:102`）。
- 系统设置用键值表 `system_settings(key, value, updated_by, updated_at)`（`backend/internal/model/system_setting.go`）。

### 2.4 现有 pi agent（事实，复用对象）

- `agent/` 是 Node/TS 服务，基于 `@earendil-works/pi-durable`，用 SQLite 存 `agent_sessions` / `agent_runs` / `agent_approvals` / `agent_events`（`agent/src/store/migrations.ts`）。
- 工具声明由 Go 统一定义（`backend/internal/service/agent/tool_registry.go`），按 `mode` 过滤；系统提示词在 `run_setup.go` 里按 `mode` 切换。
- 审批流程：TS 侧用 `APPROVAL_KIND` 识别需要审批的工具（`agent/src/durable-tools.ts:55`）→ Go 的 `ProposeApproval` 生成审批内容（不落库）→ 运行进入 `waiting_approval` → 用户决定（支持 `approved` / `partial` / `rejected`）→ Go 的 `ResolveApproval` 和 `ExecuteApproval` 落库，执行结果文字喂回模型继续（`backend/internal/service/agent/platform_approval.go`）。
- 联网能力：`web_search`、`web_fetch` 由 Go 发出，搜索源依次是 Exa（免 key 的 MCP）、Bing、DuckDuckGo，可配 SearXNG；目标是内网时拒绝（`backend/internal/initialize/agent.go:89`、`backend/internal/provider/webaccess/`）。
- 模型：用 `kind=agent` 的模型，经 Go 的 `llmgateway` 调用，支持 openai/gemini 格式；默认取列表第一个，也可以指定（`agent/src/api/service.ts` `#pickModel`）。
- 观测：Go 定时把 `agent_runs` 同步进 `agent_run_digests`，在后台「运行记录」里展示（`backend/internal/service/agent/observe_remote.go`）。

**和画布耦合、需要改的地方**（事实，已抽查）：

1. `uk_agent_runs_active ON agent_runs(canvas_id) WHERE status IN (活跃)`（`agent/src/store/migrations.ts:67`）。后台会话用 `canvas_id=0` 时，全站同一时间只能有一个后台运行。
2. `APPROVAL_KIND`、`SAFE` 是写死在 TS 里的集合（`agent/src/durable-tools.ts:55-72`）。
3. `PrepareRun` 会读画布目录并拼进第一条消息；`ValidAgentMode` 只认识画布的 5 种模式；`checkCanvas` / `checkMessage` 假定会话属于某个画布。
4. 代理只传了 `X-Agent-User`，没传角色（`backend/internal/handler/agent/proxy.go:83`）。
5. `/admin/agent/*` 这个路径前缀已经被技能、运行记录、分析令牌占用了（`router.go:269-297`）。

### 2.5 审计（事实）

- `ai_audit_logs` 记录插件、渠道、模型、存储的变更，动作包括 `plugin.upload`、`channel.create`、`model.import`、`model.protocol` 等；`admin_audit_logs` 记录用户、设置、支付等操作。两张表在「操作审计」页合并展示（`backend/internal/model/ai_audit.go`、`admin_audit.go`）。

### 2.6 当前缺口

- 没有后台助手入口，也没有 `admin` 场景。
- 没有积分汇率和美元汇率的配置或来源。
- 没有「对未上传的插件代码做静态验证」的接口（二期需要）。
- 模型配置没有版本号，后台多人同时编辑会互相覆盖。变更单需要自己做冲突检测。

## 3. 用户与场景

- **用户**：平台运营 `admin` 和运维 `super_admin`，熟悉业务，但不一定会写插件；使用频率是每周几次，集中在上新模型、上游调价的时候。
- **触发场景**：
  - S1：上游调价，或要统一调整利润，比如「视频类按成本加价 30%」「Seedance 1080p 改成 12 积分/秒」。
  - S2：新模型的参数不全，比如「按官方文档补齐可灵 2.1 的分辨率、时长、比例参数，并做规格价」。
  - S3（二期）：对接新平台，比如「接一下 xx 的视频生成接口」。
- **成功指标**（提案）：S1 从下指令到变更单生效 ≤ 2 分钟（不含人工思考）；变更单一次通过率 ≥ 80%（不需要再改就能确认）；助手引起的线上配置事故为 0（启用检查加冲突检测兜底）。

## 4. 调研

### 4.1 主流方案（访问日期 2026-11-02）

| 方案 | 写操作把关 | 依据/来源 | 对本项目的启发 | 不适用处 |
| --- | --- | --- | --- | --- |
| Shopify Sidekick | 所有写操作先出草稿，商家确认后才保存；遵守员工权限 | [shopify.dev/docs/apps/build/ai](https://shopify.dev/docs/apps/build/ai)（官方）+ 多篇第三方评测 | 「先提案、后确认」；**确认时按操作者原有权限校验** | 面向电商，没有成本定价 |
| Supabase AI Assistant | 只读 SELECT 自动执行，改数据要确认；只发 schema 给模型，不发数据 | [supabase.com/blog/supabase-ai-assistant-v2](https://supabase.com/blog/supabase-ai-assistant-v2)、PR #51608 | **读自动、写确认**；尽量少把敏感数据发给模型 | 确认流程官方没写细（调研未复核） |
| New API「同步上游」 | 先预览新增、变更、删除，确认后再应用 | [docs.newapi.pro 模型管理](https://docs.newapi.pro/zh/docs/api/management/model-management/models-sync_upstream-preview-get)、issue #6697 | **变更预览 diff**；成本和售价分开存（issue 里正在提） | 只同步倍率，不会看文档、写协议 |
| LiteLLM 价格表 | —（数据源） | [docs.litellm.ai/docs/proxy/model_cost_map](https://docs.litellm.ai/docs/proxy/model_cost_map) | 可以作为文本模型价格的**备用数据源** | 不覆盖国内视频/图片平台，价格可能缺失 |
| Pipedream String / AI codegen | 生成单步代码，用户在编辑器里看了再部署 | [pipedream.com/docs/workflows/building-workflows/build-with-ai](https://pipedream.com/docs/workflows/building-workflows/build-with-ai) | 从 API 文档生成集成代码，并**先测再部署** | String 将于 2027-03-31 停服；不读任意文档站 |
| n8n HTTP Request「Import cURL」 | 人工粘贴 | [docs.n8n.io HTTP Request](https://docs.n8n.io/integrations/builtin/core-nodes/n8n-nodes-base.httprequest) | 文档里的 cURL 示例是最可靠的请求样本；Agent 写插件时优先找它 | 没有 AI 读文档的能力 |

### 4.2 比较（对本项目的取舍维度）

| 维度 | 逐次审批（Sidekick） | 变更单批量预览（New API） | 授权后全自动 |
| --- | --- | --- | --- |
| 安全 | 高 | 高（逐条可拒） | 低 |
| 效率（批量调价） | 低：20 个模型要点 20 次 | 高：一张单 | 最高 |
| 可解释 | 中 | 高（算式、校验结果都在单上） | 低（事后看审计） |
| 与现有审批状态机的匹配度 | 高 | 高（`partial` 已支持） | 要新加授权状态 |
| 核实度 | 已核对官方页面 | 已核对官方文档 | — |

**结论**：选变更单批量预览（用户已确认）。

### 4.3 依赖框架调研

- **Agent 编排**：沿用现有的 `@earendil-works/pi-durable`，用户指定复用，不新引入。
- **联网搜索**：沿用 `backend/internal/provider/webaccess`，不新引入。
- **汇率源**：[Frankfurter](https://frankfurter.dev)（欧洲央行汇率，免 key，开源可自建）。2026-11-02 实测 `GET https://api.frankfurter.dev/v1/latest?from=USD&to=CNY` 返回 `{"base":"USD","date":"2026-10-30","rates":{"CNY":6.6529}}`，按工作日更新。它是 HTTP 服务，不是代码依赖，不改 `go.mod`。
- **diff 展示**：变更单只对比结构化字段，前端自己渲染「字段 / 改前 / 改后」表格就够了；插件代码 diff（二期）先看项目里是否已有代码 diff 组件（`协议插件前端设计.md` 提到过「代码差量对比」，待实现时核对）。**未发现需要新引入的依赖。**

## 5. 设计原则

1. **读自动、写必确认**：任何改动线上配置的操作都经过变更单（用户决定，参考 Sidekick 和 Supabase）。
2. **改动可解释、可复算**：每个价格都附算式（上游价、汇率、积分率、加价率、取整），每条变更都附校验结果（参考 New API 预览）。
3. **复用现有规则，不另起一套**：落库走现有 service（`SaveModel` 的启用检查、插件上传预检、密钥加密），权限沿用现有接口，审计写进现有表。
4. **密钥不进对话**：API Key 只经安全卡片直达 Go，模型和会话记录里都看不到（用户决定）。
5. **花钱的动作单独确认**：真实试跑要调用上游、会产生费用，单独出卡片（用户决定）。
6. **不污染画布**：后台会话不进画布的运行记录统计，也不占画布的并发锁。

## 6. 推荐方案

### 6.1 架构（提案）

```
浏览器（后台右侧抽屉）
  │  HTTP + SSE（复用 /api/v1/agent/* 代理，新增 /api/v1/admin/assistant/sessions）
Go 后端 ──代理──▶ agent/ 服务（pi-durable，mode=admin，canvas_id=0）
  ▲                  │ 工具调用 / 审批
  └── /internal/platform/v1/*（新增 admin_* 工具、admin_change 审批种类）
         │
         ├─ 读：AIConfigService / AIChannelService / AIPluginService / 统计
         ├─ 写（仅在审批执行时）：SaveModel、SetEnabled、system_settings、Upload(二期)、CreateChannel(二期)
         └─ web_search / web_fetch / 汇率服务
```

- 在 `agent/` 里加 `admin` 场景。会话写入 `agent_sessions`，固定 `canvas_id=0`、`mode='admin'`。
- Go 侧 `PrepareRun` 遇到 `mode=admin` 时：不读画布目录；系统提示词改用 `backend/internal/agent/prompts/admin.md`（新增）；工具集只下发 `admin` 允许的工具；第一条消息拼入「页面上下文」和「定价设置快照」。

### 6.2 入口与界面（提案，用户已选「全后台右侧抽屉」）

- 后台顶栏右侧（主题切换按钮左边）放一个「助手」按钮，样式用 `ChromePill`，附快捷键 `Ctrl/⌘ J`，遵守清透胶囊规范，不加操作说明小字。点击后在右侧打开 460px 宽的面板。面板浮在正文旁边（`--chrome` 半透明底、`--chrome-border` 描边、圆角 12px），推挤正文，不遮盖。
- 面板结构：头部（会话标题下拉切换、新会话、关闭）→ 对话记录 → 输入区。助手模型选择放在输入区底栏。
- **页面上下文**：每次发送时带上 `page_context = {route, model_key?, channel_key?, plugin_key?}`，由当前路由和选中行决定，在输入框底栏显示为可移除的引用标签；发出后，这些引用跟在该条消息下面。

#### 6.2.0 身份：同一个「小镜」（2026-11-02 用户决定）

后台助手和画布助手是**同一个 Agent「小镜」**，身份认同一致。小镜根据用户所在的位置拿到对应的工具：在画布里注册画布工具，在后台注册后台工具（`mode=admin`）。后台提示词 `backend/internal/agent/prompts/admin.md` 开头写明「你是小镜……现在你在管理后台」，问「你是谁」时回答小镜，不再自称「后台配置助手」。用户在后台问画布上的事时，小镜如实说明要到画布里打开小镜去做。测试：`backend/internal/tests/agent/prompts/admin_identity_test.go`。

#### 6.2.1 聊天框视觉：复用画布助手（2026-11-02 用户决定）

**现行决定**：一期实现后，用户看到后台面板和画布助手长得不一样，决定后台**直接复用画布助手的组件和样式**：面板外壳、会话下拉、消息列表、「思考中 / 已思考」、工具活动组、底部决定区 `DecisionDock`、输入框 `AgentComposer`、空状态 `AgentEmpty`。做法同短剧助手（`pages/drama/assistant/assistant-pane.tsx`：`AgentPanel` + `useAgentController`）。后台特有的只有：

- 变更单 `admin_change` 作为决定区里的一种审批，内容是逐项的「当前 → 变更后」、状态、计算依据；时间线里的审批行显示结果摘要和「回滚」。
- 页面上下文以画布的引用 chip 样式显示在输入框和消息下方。
- 画布专有能力（图片附件、@ 节点、选中占位、技能、视口）在后台关闭。

理由：同一个产品里只有一种助手外观，以后改一处两边一起变，不用维护两套聊天界面。

**被取代的第二版规范**（保留作参考，不再实现）：原型 [后台助手聊天框原型.html](后台助手聊天框原型.html)。

- **去掉**：闪光、魔杖类图标，紫色或渐变强调色，对话气泡，快捷指令胶囊，打字和闪烁的文字特效，欢迎语。
- **对话记录**：按时间线排列，每条是 头像（用户用圆形首字，助手用方形「助」）+ 名字 + 时间，正文左对齐，不分左右气泡；日期分隔线。
- **执行步骤**：默认折叠成一行。运行中显示「执行中 · 第 3/6 步」加一条 2px 的细进度条；完成后显示「执行了 6 步 · 3.2s」。展开后是等宽字体的日志行：状态符号、工具名、参数摘要、耗时（右对齐、等宽数字）。
- **变更单**：评审式卡片。头部是编号 `CO-0012`、状态点（待审核 / 已应用 / 已拒绝）、标题，以及一行元信息（几项、是否应用即生效）。每一项是 勾选框、名称、key、状态点；下面是字段行「字段 / 当前（删除线）/ → / 变更后（绿色）」，用等宽数字列对齐，只给数值上色，不涂整行。校验问题和警告各用一条浅色底的提示行；「计算依据」默认收起，展开后是键值表，含来源链接。底栏是「已选 n / m」+「拒绝」（文字按钮，点后展开原因输入框）+「应用 n 项变更」（主按钮）。应用后，底栏显示操作人、时间、审计编号，以及「回滚」。
- **待审核提示**：有待审核的变更单时，输入框上方固定一行「● 变更单 CO-0012 待审核 · 4 项」，点编号可跳回那张单；输入框不锁定，补充要求会让旧单作废、重新出单。
- **输入区**：多行输入框（自动增高，最多 160px），底栏左侧是引用标签，右侧是模型选择和「发送 Ctrl↵」；运行中，发送按钮换成「停止」。
- **空状态**：新会话只显示图标、「新会话」标题和最近 3 个会话；没有可用模型时显示图标、标题和「前往模型渠道」按钮。
- **配色**：只用 `.admin-theme` 的 zinc 中性色，加上 ok / bad / warn 三个语义色，明暗主题都适配。
- 变更单确认成功后，前端广播 `admin-config-changed` 事件，左侧模型/渠道/插件页的数据随之重新拉取。

### 6.3 一期工具集（提案）

| 工具 | 类型 | 说明 |
| --- | --- | --- |
| `admin_list_models` | 读 | 按 kind、启用状态、渠道、关键字过滤；返回 key、label、kind、enabled、billing、默认价、cost 是否已填、近 7 天调用数 |
| `admin_get_model` | 读 | 完整 `ModelConfig`（含 cost），另附 `body_sha256`，用于冲突检测 |
| `admin_list_channels` | 读 | 渠道 key、name、base_url、是否已配 Key（布尔值，不返回密钥） |
| `admin_get_pricing_settings` | 读 | `credits_per_yuan`、`default_markup_pct`、当前美元汇率及日期、汇率来源（实时/手动覆盖） |
| `admin_quote_preview` | 读 | 用 `quote.go` 按给定配置和输入算一次价，供 Agent 自检 |
| `web_search` / `web_fetch` | 读 | 复用现有工具：查官方价格页和参数文档 |
| `plan_update` | 读 | 复用现有工具：多步任务的计划 |
| `admin_propose_changes` | **审批** | 提交变更单，种类 `admin_change`，见 §6.4 |

### 6.4 变更单（核心，提案）

**Agent 提交的参数**：

```json
{
  "title": "视频模型按成本加价 30%",
  "reason": "用户要求：视频类全部按成本 ×1.3",
  "items": [
    {
      "target": "model",                    // model | pricing_settings（二期加 plugin | channel | model_create）
      "key": "kling-v2-1",
      "base_sha256": "…",                   // 取自 admin_get_model，执行时比对
      "patch": {                            // 只允许这些顶层字段：pricing / capabilities / label / hint / tags / enabled
        "pricing": { "billing": "per_second", "per_second": 10, "cost": {"on": true, "per_second": 7}, "tiers": [ … ] }
      },
      "pricing_basis": [                    // 每个改动的价位一条算式；只改参数时可省略
        { "path": "pricing.per_second", "upstream": {"amount": 0.14, "currency": "USD", "unit": "秒", "source_url": "https://…"},
          "fx": 6.6529, "credits_per_yuan": 7.5, "cost": 7, "markup_pct": 30, "price": 10 }
      ]
    }
  ]
}
```

**Go 的 `ProposeApproval`（`admin_change`）**，纯计算，不落库：

1. 逐条读取当前配置，比对 `base_sha256`。不一致就把该条标为 `stale`，并告诉模型「配置已变化，请重新读取」。
2. 应用 patch，得到 `after`，用 `modelcfg` 校验。目标模型已启用时还要过 `CheckEnable`，不通过的条目标为 `invalid` 并附 issues。
3. **复算价格**：用 `pricing_basis` 里的输入按统一公式重算，和 patch 里的价格不一致就标为 `mismatch`，防止模型算错。
4. 生成字段级 diff：`[{path, before, after}]`。
5. 按条目判断确认者需要的角色（`model`、`pricing_settings`：admin；二期的 `plugin`、`channel`：super_admin，与现有接口一致），记为 `required_role`。
6. payload 中包含 `items[{…, before, after, diff, status: ok|stale|invalid|mismatch, issues, required_role}]`，再加上 `signature`（HMAC 覆盖 items 的 after 和 base_sha256，沿用短剧审批的签名做法，防止执行前被篡改）。

**有问题的条目仍然展示，但不能勾选**；如果一张单全部是问题条目，提案就直接以 `IsError` 返回模型，不打扰用户。

**用户决定**：整单批准、逐条勾选（`partial`），或拒绝（可附一句理由喂回模型）。

**Go 的 `ExecuteApproval`**：

1. 校验签名。
2. **按 `user_id` 从数据库实时查角色**（不信任请求头），角色不满足 `required_role` 的条目不执行，原因写进结果。
3. 逐条在**单独的事务**里再比对一次 `base_sha256`（防止确认期间有人改过），然后调用 `SaveModel` 或写 `system_settings`。各条之间不互相回滚，每条的结果单独记录。
4. 每条写一条审计：`ai_audit_logs.action = model.update` 或 `pricing.settings`，`detail_json` 包含 `{via:"assistant", session_id, run_id, approval_id, diff}`。
5. 返回给模型的结果文字形如：「已生效 18 条；2 条失败：kling-v2-1 配置已被他人修改；seedance-pro 启用检查未通过：…」。

**撤销**：执行结果里保存每条的 `before`。消息流中已执行的变更单上有「撤销」按钮，点击后生成一张反向变更单（同样要确认，也同样做冲突检测），不直接回滚。

### 6.5 调价算法（提案，用户已选「上游成本 + 加价率」）

```
cost_yuan   = upstream_amount × (currency == USD ? fx_usd_cny : 1)
cost_credit = ceil(cost_yuan × credits_per_yuan)          // 积分成本，写入 pricing.cost / tier.cost
price       = ceil(cost_credit × (1 + markup_pct/100))    // 售价；markup_pct 默认取设置项，用户可在指令里覆盖
```

- 单位换算由 Agent 负责：上游按次就对应 `unit`，按秒对应 `per_second`，按百万 Token 对应 `token.in/out`。上游按「每 5 秒」「每 1K tokens」报价的，Agent 要先换算成平台单位，并把换算写进 `pricing_basis.upstream.note`。Go 只复算最后三步。
- 有规格分档（分辨率、时长档）时，每个 tier 各自一条 `pricing_basis`。
- **纯指令调价**（如「全体涨 10%」）：`pricing_basis` 用 `{"mode":"relative","from":旧价,"pct":10}`，同样向上取整。小价位取整后的实际涨幅会大于指令（如 2 涨 10% → 3，实际 +50%），单上写明实际涨幅；**降价时取整可能让价格不变**（如 2 降 10% → ⌈1.8⌉ = 2），这类条目标「取整后未变化」、默认不勾选。
- 售价**低于成本**时，该条目标为警告，不禁止勾选，用红色提示。
- 找不到上游官方价格时，Agent 不能编造：要么问用户，要么跳过并在回复里列出。

### 6.6 定价设置（提案，用户已选「新增定价设置，美元汇率实时」）

- 系统设置里新增「定价设置」页（`/admin/settings/pricing`），使用 `system_settings` 的这些键：
  - `pricing_credits_per_yuan`：1 元 = N 积分，正数，最多两位小数，必填（未设置时助手拒绝做成本换算，并提示先去设置）。
  - `pricing_default_markup_pct`：默认加价率，整数，0–1000，缺省 30。
  - `pricing_usd_cny_override`：手动覆盖的美元汇率，可空；为空时用实时汇率。
  - `pricing_usd_cny_last` / `pricing_usd_cny_date` / `pricing_usd_cny_fetched_at`：最近一次拉到的实时汇率，由系统写入。
- **汇率服务**（`backend/internal/provider/fx`，新增）：每 6 小时请求一次 `https://api.frankfurter.dev/v1/latest?from=USD&to=CNY`，走现有出站 IP 策略，超时 10 秒。成功就写入 last/date/fetched_at；失败沿用上次的值。上次的值超过 7 天时，变更单的算式里标「汇率可能过期」。从未成功拉取过、也没有手动覆盖时，带美元的条目标为 `invalid`。
- 页面上显示当前汇率、日期和来源（实时/手动），只提供一个「手动覆盖」输入框，不加说明小字。

### 6.7 二期：插件全链路（提案）

流程：用户说「接一下 xx 的视频接口」→ Agent 用 `web_search` / `web_fetch` 找官方 API 文档（优先 OpenAPI 和 cURL 示例）→ `admin_list_plugins` 比对已有插件的 `vendor`、`endpoints`、`auth`，能复用就直接走「建渠道 + 建模型」→ 不能复用就写插件草稿 → **自动静态验证** → 出「插件 + 渠道 + 模型」组合变更单 → 确认后落库 →（可选）真实试跑卡片 → 失败时看 trace 自动修，最多 3 轮。

新增工具：

| 工具 | 类型 | 说明 |
| --- | --- | --- |
| `admin_list_plugins` / `admin_get_plugin` | 读 | 插件 meta 与代码（内置的也能读，作为写法样例） |
| `admin_plugin_check` | 读 | **新接口**：对一段未上传的代码，在 runner 里做预检（解析 meta、钩子是否齐全），用给定的样例 ctx 跑 `buildSubmitRequest`，用文档里的响应样例跑 `parse*`，返回渲染出的请求（脱敏）、解析结果和 `utils.log`。不落库，不发网络请求 |
| `admin_propose_changes` | 审批 | items 新增 `target: plugin`（code、meta 摘要、和已有版本的代码 diff）、`channel`（不含密钥）、`model_create` |
| `admin_request_secret` | 审批（种类 `admin_secret`） | 弹安全输入卡片，见下 |
| `admin_test_run` | 审批（种类 `admin_test_run`） | 真实试跑卡片：显示模型、输入、**预估上游费用**（按 cost 换算，示意）；同意后调用现有 `test-run`，结果和 trace 摘要喂回模型 |

- **插件代码的落库**：草稿只存在于变更单的 payload 里，确认后才调用 `AIPluginService.Upload`（仍是 `super_admin`，仍走 runner 预检，版本号自动分配）。上传失败则该条失败，同单里依赖它的模型条目也跳过。
- **密钥卡片**（用户已选）：卡片里只有一个密码输入框和「保存」按钮。前端直接调用现有的 `PUT /admin/ai/channels/:key/secret`，成功后再调审批决定接口，决定内容只有 `{configured:true}`。**密钥不经过 agent 服务、不写入 SQLite、不发给模型。** 渠道还没建（变更单未确认）时，卡片置灰并提示「先确认建渠道的变更单」。
- **联网不限次数**（2026-11-02 用户决定）：画布每个运行片段最多联网 20 次（`maxWebCalls`），后台（admin 模式）不受这个限制，因为对接平台要反复查文档。代码见 `backend/internal/service/agent/bridge_web.go` 的 `webCheck`，测试见 `TestWebTools_AdminModeHasNoCallLimit`。
- **自动修复上限**：同一个插件在一次运行里最多 3 次静态验证失败后重写、最多 3 次真实试跑；超过就停下，把 trace 摘要交给用户。

### 6.8 系统提示词要点（提案，`backend/internal/agent/prompts/admin.md`）

- 身份：video-canvas 后台配置助手，只操作 AI 配置，不碰用户、支付和存储。
- 规则：先读后改；写操作只能通过 `admin_propose_changes`；一张变更单只做一件事；价格必须附 `pricing_basis` 和 `source_url`；找不到价格不能编造；不在回复里复述密钥；改已启用的模型前要提醒「保存即生效」。
- 附上 `ModelConfig` / `Pricing` / `ParamField` 的字段说明（从 `GET /admin/ai/schema/model` 生成，避免手抄出错），二期再附 `plugin-contract.md` 的要点。

## 7. 状态与异常

| 状态 | 表现 | 处理 |
| --- | --- | --- |
| 没有可用的 agent 模型 | 抽屉显示空状态：图标、「暂无可用的助手模型」、按钮「去配置模型」 | 复用 `60005 modelNA` |
| 当前用户已有运行在进行 | 输入框可以插话（复用插话排队），不能新开第二个运行 | 新索引按用户互斥，见 §8.1 |
| 读工具失败 | 活动行显示失败原因，模型自行决定重试 | 和画布 Agent 一致 |
| 提案全部是问题条目 | 不出卡片，模型收到原因后自己修 | `IsError` |
| 变更单待确认 | 运行停在 `waiting_approval`，抽屉底部固定显示决定区；关掉抽屉再打开仍在 | 复用审批持久化 |
| 确认期间配置被别人改了 | 该条执行失败，显示「已被修改，请让助手重新读取」 | `base_sha256` 二次比对 |
| 角色不够（二期：插件、渠道条目） | 条目上显示「需 super_admin」，勾选框禁用 | `required_role` |
| 已启用模型的启用检查不通过 | 条目标为 invalid，列出 issues | 复用 `SaveModel` 规则 |
| 汇率过期或缺失 | 算式上显示黄色或红色标记 | §6.6 |
| 售价低于成本 | 红色警告，可以勾选 | §6.5 |
| 执行中途部分失败 | 每条单独显示结果，已成功的不回滚 | §6.4 |
| 撤销 | 生成反向变更单 | §6.4 |
| 断线重连 | SSE 按 seq 补齐 | 复用 |
| （二期）真实试跑失败 | 卡片显示上游错误和 trace 链接；自动修，最多 3 轮 | §6.7 |

## 8. 数据与工程影响

### 8.1 agent/ 服务（提案）

- `migrations.ts` 新增迁移：删除 `uk_agent_runs_active`，改建两条部分唯一索引：
  - `uk_agent_runs_active_canvas ON agent_runs(canvas_id) WHERE canvas_id > 0 AND status IN (活跃)`
  - `uk_agent_runs_active_admin ON agent_runs(user_id) WHERE canvas_id = 0 AND status IN (活跃)`
- `api/http.ts`：`MODES` 加入 `admin`；新增路由 `GET/POST /admin/sessions`（会话列表和新建，只返回 `mode=admin` 且属于本人的会话）。处理时读取 `X-Agent-Role`，没有 admin 角色就返回 403。其余 runs、approvals、events 路由复用，但要校验会话归属。
- `api/service.ts`：`mode=admin` 时跳过 `checkCanvas` / `checkMessage`；`page_context` 原样透传给 `prepare`。
- `durable-tools.ts`：`APPROVAL_KIND` 增加 `admin_propose_changes: "admin_change"`（二期再加 `admin_request_secret`、`admin_test_run`）；`SAFE` 加入所有 `admin_*` 读工具和 `admin_propose_changes`。
- **规避「TS 写死审批种类」的发版风险**（推断加提案）：长期可以让 Go 在工具声明里带 `approval_kind` 字段，TS 改为读取声明，不再维护常量表。一期先按现状改常量，这件事作为技术债记录。

### 8.2 Go 后端（提案）

- `router.go`：新增 `adminAssistant := auth.Group("/admin/assistant", RequireAdmin)`，下面 `GET/POST /sessions` 转发到 agent 服务的 `/admin/sessions`。之所以不用 `/admin/agent`，是因为它已被占用。
- `handler/agent/proxy.go`：所有转发都加上 `X-Agent-Role`（从 `RoleLookup` 取）。
- `model/agent.go`：`ValidAgentMode` 加入 `admin`；`RunContext.validate`：`mode=admin` 时 `canvas_id` 必须为 0，否则必须大于 0。
- `service/agent/run_setup.go`：增加 `admin` 分支（提示词、工具、第一条消息）。
- `service/agent/tool_registry.go`：注册 `admin_*` 工具，`modes: [admin]`；画布模式下看不到这些工具，`admin` 模式下也看不到画布工具（`web_search`、`web_fetch`、`plan_update` 两边都有）。
- `service/agent/admin_change.go`（新增）：实现 §6.4 的提案、解析、执行，接入 `approvalTools` 和 `ExecuteApproval` 的分支。
- `provider/fx`（新增）加一个定时任务（挂在现有的定时机制上，实现时确认用 River 周期任务还是简单 ticker）。
- `service/settings`：定价设置的读写接口 `GET/PUT /admin/settings/pricing`，读写都是 admin 或 super_admin（用户确认，见 §11），写入记 `admin_audit_logs.action = settings.pricing`。
- 计费：`mode=admin` 的运行**不扣管理员积分**，但照常记录 Token 用量（用户确认，见 §11）。实现方式是在 `SettleModelCall` 前按 mode 跳过扣费，只写用量。
- 观测：`agent_run_digests` 已有 `mode`。「运行记录」默认过滤掉 `admin`，另加一个场景筛选项，可单独查看后台助手的运行。

### 8.3 前端（提案）

- 把画布 Agent 里可复用的部分抽到 `web/src/components/agent-chat/`：`message-list`、`markdown`、`activity-group`、`run-summary`、`decision-dock` 框架，以及去掉画布依赖后的 `use-agent-controller`。画布侧改为引用抽出后的组件，行为不变（要有回归测试）。
- 新增 `web/src/pages/admin/assistant/`：`assistant-drawer.tsx`、`change-order-card.tsx`（按目标分组的 diff 表、逐条勾选、算式展开、状态标签、撤销按钮）、`page-context-chip.tsx`；二期再加 `secret-card.tsx`、`test-run-card.tsx`、`plugin-diff.tsx`。
- `layout.tsx` 顶栏加入口按钮和抽屉；`admin-store` 记录抽屉开关（每个浏览器各自记住）。
- `settings/pricing` 页。
- 测试放在 `web/src/tests/`：变更单的勾选、禁用、partial 提交；页面上下文的生成；收到 `admin-config-changed` 后刷新。

### 8.4 接口汇总（提案）

| 方法 路径 | 说明 | 业务错误 |
| --- | --- | --- |
| `GET /api/v1/admin/assistant/sessions` | 本人的后台助手会话 | 403 非管理员 |
| `POST /api/v1/admin/assistant/sessions` | 新建会话 `{agent_model_key?}` | 60005 无可用模型 |
| `POST /api/v1/agent/sessions/:sid/runs` | 复用；body 增加 `page_context` | 60001 已有活跃运行 |
| `POST /api/v1/agent/approvals/:aid/decision` | 复用；`admin_change` 的决定是 `{action: approve|partial|reject, item_ids?: [], note?}` | 签名不符、角色不够（按条目返回） |
| `GET /api/v1/admin/settings/pricing` | 定价设置和汇率状态 | — |
| `PUT /api/v1/admin/settings/pricing` | `{credits_per_yuan, default_markup_pct, usd_cny_override?}` | 400 参数越界；403 非管理员 |
| `POST /internal/platform/v1/tools/admin_*` | 内部读工具 | — |
| （二期）`POST /internal/platform/v1/tools/admin_plugin_check` | 未落库代码的静态验证 | 预检失败按 issues 返回 |

## 9. 方案取舍

| 维度（权重） | A 复用 pi agent + 变更单（推荐，已选） | B Go 内新写轻量循环 | C 授权后全自动 |
| --- | --- | --- | --- |
| 安全（0.35） | 高：逐条确认、角色复核、冲突检测 | 高 | 低 |
| 实现成本（0.25） | 中：要改索引、mode、代理 | 高：要重做会话、SSE、审批 | 中 |
| 体验一致性（0.2） | 高：和画布 Agent 同一套交互 | 低 | 中 |
| 可维护性（0.2） | 中：TS 有写死的审批种类 | 中：两套 Agent | 中 |

**明确放弃的**：会话内授权自动执行；新接付费搜索 API（先用现有的 Exa 链路）；从充值套餐推算积分汇率（套餐一改，所有成本都跟着变）；助手管理用户、支付、存储；助手自动定时调价（没有人在场，就违反「写必确认」）。

## 10. MVP 与后续

**一期：模型参数与调价**

- 范围：§6.1–6.6、§8.1–8.3（不含二期工具）、定价设置页、汇率服务、运行记录筛选。
- 验收：
  1. 在任意后台页打开抽屉，说「把所有视频模型按成本加价 30%」，助手读取配置、查价、出一张变更单，单上逐条显示 diff 和算式；勾选其中 3 条确认后，只有这 3 条生效，审计里能看到 3 条记录，且 `via=assistant`。
  2. 在变更单待确认期间，到模型页手动改其中一个模型，再确认：该条显示「已被修改」，其他条照常生效。
  3. 用 admin 账号让助手把默认加价率改成 35%：能直接确认生效，审计里的确认人是这个 admin。
  4. 让助手把某个已启用模型的 `channels` 删掉：条目标为 invalid，确认按钮不可用。
  5. 两个管理员同时使用助手，互不阻塞；画布 Agent 的运行记录里看不到后台会话。
  6. 断开汇率源：变更单仍能生成，算式标「汇率可能过期」；从未拉到过汇率时，带美元的条目标为 invalid。
  7. 「全体涨 10%」时，2 积分的价位变成 3，单上写明实际涨幅 +50%；「全体降 10%」时，2 积分的条目标「取整后未变化」且默认不勾选。

**二期：插件全链路**

- 范围：§6.7。
- 验收：
  1. 对一个已有内置插件能覆盖的平台，说「接一下」：助手复用插件，只建渠道和模型。
  2. 对一个没有插件的平台：助手给出插件草稿、静态验证结果（渲染出的请求、样例解析结果），以及「插件 + 渠道 + 模型」组合变更单。
  3. 密钥卡片保存后，agent 的 SQLite、`agent_events` 和模型请求留存里都搜不到这个密钥。
  4. 真实试跑必须点同意才发起；失败后助手最多自动修 3 轮。

**后续**（不排期）：Go 下发审批种类，去掉 TS 常量表；LiteLLM 价格表作为文本模型价格的备用来源；变更单导出。

## 11. 待确认问题与已确认事项

**已确认（用户在访谈中的回答）**

1. 写操作一律出变更单，确认后落库。
2. 调价依据：查上游成本，再按加价率算；也支持纯指令调价。
3. 联网：用户原选「接搜索 API + 网页抓取」；核对代码后改为复用现有的 `web_search` / `web_fetch`（同样是搜索加抓取，只是不新接服务），用户已确认。
4. 宿主复用项目现有的 pi agent（`agent/`）。
5. 插件验证：自动做静态验证；真实试跑另出卡片确认；失败后自动修，最多 3 次。
6. 入口：全后台右侧抽屉。
7. 权限：沿用现有接口权限，确认时重新校验。
8. 密钥：对话里弹安全输入卡片，密钥不进对话。
9. 积分汇率：新增定价设置，「1 元 = N 积分」人工填；美元兑人民币取实时汇率，不用人工填。
10. MVP：一期模型与调价，二期插件。
11. 后台助手不扣管理员积分，只记录 Token 用量。
12. 定价设置（积分汇率、默认加价率、美元汇率覆盖）admin 和 super_admin 都能改，直接改和通过助手改都一样。

**已搁置的决定**：会话内授权全自动执行（以后要做「可信操作白名单」时可复用 §6.4 的执行逻辑）；付费搜索源。

**默认值（不需要用户决定，实现时照此）**：汇率源用 Frankfurter，每 6 小时拉一次，超过 7 天标过期；默认加价率 30%；价格向上取整，取整后未变化的条目默认不勾选；售价低于成本只警告不禁止；面板宽 460px，推挤正文；新路由前缀用 `/admin/assistant`。

**仍待确认**

- 汇率定时任务挂在 River 周期任务上还是简单 ticker：实现时看现有的定时机制再定，不影响验收。

## 12. 来源与假设

**外部来源（访问日期 2026-11-02）**

- Shopify Sidekick / App AI：https://shopify.dev/docs/apps/build/ai
- Supabase AI Assistant v2：https://supabase.com/blog/supabase-ai-assistant-v2；PR：https://github.com/supabase/supabase/pull/51608
- New API 同步上游：https://docs.newapi.pro/zh/docs/api/management/model-management/models-sync_upstream-preview-get；issue：https://github.com/QuantumNous/new-api/issues/6697
- LiteLLM 价格表：https://docs.litellm.ai/docs/proxy/model_cost_map
- Pipedream AI：https://pipedream.com/docs/workflows/building-workflows/build-with-ai
- n8n HTTP Request：https://docs.n8n.io/integrations/builtin/core-nodes/n8n-nodes-base.httprequest
- Frankfurter：https://api.frankfurter.dev/v1/latest?from=USD&to=CNY（实测）

**项目文件**：见 §2 各条引用。

**假设与未验证**

- 截图里的「33 个插件」是内置 21 个加上传 12 个：推断，未查库。
- Exa、Bing、DuckDuckGo 搜索对国内厂商文档站（火山、可灵、阿里）的召回质量：未验证，需要在一期实测；不够好时再考虑付费搜索源。
- 各上游价格页能否被 `web_fetch` 抓到正文（有些是 JS 渲染的）：未验证。抓不到时助手应请用户粘贴价格。
- Supabase 写操作确认的具体流程：调研未复核。

## 13. 实现记录

**2026-11-02（分支 `feat/后台agent`，未提交，未接真实上游联调）**

- 一期（模型参数与调价）、二期（插件全链路）都已实现。分工：后端由 codex gpt-6.1-sol 写，前端由 Sonnet 子代理（一期）和 codex（二期）写，Claude 写契约并验收。契约：[一期接口契约.md](一期接口契约.md)、[二期接口契约.md](二期接口契约.md)。
- 与设计的出入：
  - 聊天界面改为复用画布助手（§6.2.1）。
  - 后台助手和画布助手是同一个「小镜」（§6.2.0）。
  - 变更单号为 `CO-YYMMDD-` 加 6 位十六进制，只保证同一实例内唯一。
  - 插件静态验证和真实试跑的次数上限持久化在 `agent_admin_attempts` 表（迁移 0024）。
  - 插件、渠道、新建模型不支持自动回滚。
- 联调中修掉的问题：
  - Go 的 idcodec 把零画布编码成 `null`，前端 WebSocket 解析器曾经把后台事件全部丢弃。
  - 复用的画布组件需要外包 `ReactFlowProvider`（`AdminCanvasScope`）。
  - 算价工具缺 `prompt` 时报错。
  - 价格依据的路径用 JSON Pointer 写法时被判非法。
  - 运行记录在空闲时反复重写最后一条运行。
- 验证：Go `go test ./...` 全量通过，加 race 定向；agent 320 个测试；web 4038 个测试。agent 有个别原有的时序用例在机器高负载时偶尔失败，单独重跑能通过。
- 遗留事项：真实上游试跑与浏览器验收；开发环境改了 backend/agent 后要手动重启容器（air 在 9p 挂载下收不到文件事件）。
