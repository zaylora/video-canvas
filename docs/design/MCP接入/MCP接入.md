# 画布 MCP 接入

> - **范围**：给画布加一个对外的 MCP 端点，让 Codex / Claude Code / Cursor / WorkBuddy 这类外部 Agent 能直接读写画布、触发生成、打包素材；后台新增独立的「MCP 项目」配置，按项目签发令牌、绑定归属用户与权限范围。
> - **日期和状态**：2026-11-22 规划，2026-11-24 已实现，**对外标注 Beta**：工具集合与权限档位还会随反馈调整，接入方以页面生成的配置片段为准。一期范围见第 12 节。
> - **证据约定**：**事实**附文件路径或行号；**推断**是基于事实的判断；**提案**是尚未实现的设计。
> - **参考项目**：`/Users/quchenchen/Documents/github/agent-plugin`（ChatCut，Apache-2.0，已浅克隆）、`/Users/quchenchen/Documents/github/OpenCut` 与 `opencut-classic`（MIT，已浅克隆）。
> - **不做什么**：不接 ChatCut 的托管服务（闭源、按 credits 计费、素材出境）；不引入 OpenCut 的浏览器渲染链路（见 3.3）。

## 1. 设计摘要

- **问题**：画布的能力现在只在自家 Web 和内置 Agent（小镜）里可用。37 个工具（`backend/internal/service/agent/tool_decls.json`，实测 len=37）已经具备「读状态 / 改画布 / 生成 / 合成 / 打包」的完整能力，但工具箱只对内网桥开放（`backend/internal/router/bridge.go:22-29`，`RequireServiceToken`），外部 Agent 拿不到。
- **参考判断**：ChatCut 用「一份源 + 多宿主清单 + 远程 MCP」把能力铺到 11 个 Agent 宿主上，是目前唯一验证过的分发打法；OpenCut 把 `MCP server (for AI agents)` 直接写在 README 的「即将支持」里（`OpenCut/README.md:24`），说明这是这类产品的公认缺口。两家的共同点是：**它们都没有为了接 Agent 而改引擎**，只加了一层协议适配。
- **推荐结论**（提案）：
  1. **传输**：Streamable HTTP，单端点 `POST /api/v1/mcp`，实现 `initialize` / `tools/list` / `tools/call` / `ping`。
  2. **鉴权**：Bearer。后台为每个「MCP 项目」签发 `vcm_` 令牌，沿用分析令牌已验证的范式（sha256 落库、明文只返回一次、到期、限流、审计）。
  3. **工具面**：`tools/list` 从 `tool_decls.json` 派生，**白名单默认拒绝**；`tools/call` 复用现成的 `ExecutePlatformTool`，不新写一套执行器。
  4. **身份**：一个 MCP 项目绑定一个归属用户（决定画布与积分），每次调用合成 `RunContext`。
  5. **分发**：按 ChatCut 的目录结构出 `mcp/<host>/` 清单，同源内容用相对软链 + 一条一致性测试兜住。

## 2. 项目现状

### 2.1 工具层已经就绪（事实）

- `ExecutePlatformTool(ctx, rc, toolCallID, name string, args json.RawMessage) (*agentsvc.ToolResult, error)`，证据：`backend/internal/handler/agent/platform.go:45`。这就是 MCP `tools/call` 的形状：**name + JSON args → result**。
- 工具声明 37 个，证据：`backend/internal/service/agent/tool_decls.json`。分组实测：`canvas_*` 8 个、`admin_*` 14 个、`drama_*` 5 个、其余 10 个（`plan_update`、`ask_user`、`model_list`、`generate_media`、`task_get`、`skill_search`、`skill_read`、`web_search`、`web_fetch`、`show_card`）。
- 工具声明有基线门禁：`backend/internal/tests/service/agent/testdata/tool_defs_baseline.json`，改 `tool_decls.json` 必须重生成。
- 运行上下文：`RunContext{RunID, UserID, SessionID, CanvasID, Mode, ModelKey, Selection, BudgetCredits, SpentExtra}`，证据：`backend/internal/service/agent/platform.go:23-34`。ID 全是 `json:",string"`（雪花 ID 超 2^53）。
- 写画布受**读前写门禁**约束：同一 `RunID` 内先 `canvas_get_state` 读过的节点才能改。
- 对内路由：`POST /internal/platform/v1/tools/:name`，证据：`backend/internal/router/bridge.go:23`。**这条必须保持只在内网**，不能拿来当对外端点。

### 2.2 后台与令牌已有成熟范式（事实）

- 后台路由分组：`backend/internal/router/router.go:289-399`，权限用 `middleware.RequireAdmin` / `RequireSuperAdmin` / `AdminOrAnalysisToken`。
- 分析令牌表：`model.AdminAPIToken`，证据：`backend/internal/model/agent_observe.go:141-155`。前缀 `vca_`（`:39`）、库里只存 sha256、`Scope` 目前只有 `agent:read`（`:38`）、到期时间、`LastUsedAt`、`RevokedAt`。
- 认证实现：`ObserveService.AuthenticateToken`（`service/agent/observe.go:361-390`）——校验哈希、未注销、未过期、scope、**签发人仍是管理员**、每分钟限流、按需回写 `LastUsedAt`；失败统一回同一句错误，避免探测。
- 中间件：`backend/internal/middleware/analysis_token.go:29`，按前缀分流 Bearer，把身份塞进 `CtxUserIDKey`。
- 后台页范式：`web/src/pages/admin/agent-tokens/`（`index.tsx` + `agent-tokens-view.tsx` + `use-agent-tokens.ts`）、接口层 `web/src/api/admin/agent-token/{endpoints,index,type.d}.ts`、导航 `web/src/pages/admin/admin-nav.ts`。
- 迁移：仓库最新 `backend/migrations/0046_canvas_compose_job.sql`；新表必须同时进 `backend/internal/model/base.go` 的 `All()`，否则 `TestMigrationsMatchModels` 报「整表只在左侧」。
- 错误码：画布段已用到 `30023`（`pkg/errcode/errcode.go:75`）。

### 2.3 两个参考项目的事实

**ChatCut（agent-plugin）**

- Apache-2.0，但**只有接入层开源**：219 个 `.md` + 26 个宿主清单 + 4 个小脚本；桌面端是闭源 dmg（`ChatCut-Inc/homebrew-tap` 的 `Casks/chatcut.rb` 里是 `ChatCut-0.9.1-arm64-*.dmg` + sha256 + `auto_updates true`）。
- MCP 在托管端 `https://api.chatcut.io/api/external-mcp/mcp`。实测：`POST initialize` → `401`，`www-authenticate: Bearer resource_metadata="…/oauth-protected-resource"`；错误体明确写 `Authorization: Bearer <chatcut API key or JWT> is required`——**除 OAuth 外还支持 API Key**。
- OAuth 元数据：授权码 + `refresh_token` + PKCE `S256` + 动态注册（`registration_endpoint`）。
- 多宿主同源靠**相对软链 + 一致性测试**：`tests/shared-package-files.test.mjs:41` 断言「内容相同的文件必须用相对软链，不能是副本」；另有版本头同步测试、Antigravity 的 24000 字节上限测试。
- 工具面（从技能里统计）：`read_project`、`edit_item`、`manage_timelines`、`apply_script`、`edit_captions`、`submit_*`、`track_progress`、`track_export`。两个值得抄的设计：**`edit_item` 用事务包（`{"json":"{\"updates\":[…]}"}`）而不是散字段**；**`track_progress` / `track_export` 把异步任务做成显式轮询工具**，正好对上你们的 `task_get`。
- 技能层里有两条经验直接对应你们踩过的坑：`verification`（「工具返回成功 ≠ 结果正确」）、`known-errors`（把失败形状写死，避免模型瞎试）。

**OpenCut**

- `OpenCut-app/OpenCut`（新仓库）是重写骨架：15 个 `.rs`，`apps/desktop/src/main.rs` 41 行 GPUI 空窗口，`crates/media` 里只有 `setup.ts` + `ffmpeg.json`。`Editor API`、`MCP server`、`headless`、`脚本标签` 全在 `README.md:22-26` 的「即将支持」列表里。
- `opencut-classic`（生产在跑）：MIT，Next.js 16 + React 19 + Zustand；渲染在浏览器——`services/renderer/scene-builder.ts` 的 `buildScene` 产出节点，`scene-exporter.ts` 用 `mediabunny` 的 `Output` + `CanvasSource(renderer.getOutputCanvas())` 逐帧编码；Rust `crates/{gpu,compositor,effects,masks,time}` 共约 3720 行，是内核零件但未成「解码→合成→编码」管线。
- 时间线契约干净可借：`apps/web/src/timeline/types.ts`（287 行）——`TScene` → `SceneTracks` → `TimelineTrack`（`video|text|audio|graphic|effect`）→ element 带 `trimStart/trimEnd` 与 `MediaTime`。

## 3. 借什么、不借什么

| 参考点 | 取 | 理由 |
| --- | --- | --- |
| 远程 MCP + Bearer（API Key） | ✓ 一期 | 最简单；ChatCut 自己就留了这条口子，不是只有 OAuth |
| Streamable HTTP 单端点 | ✓ 一期 | 与 MCP 规范一致，宿主侧零改造 |
| 多宿主清单 + 相对软链 + 一致性测试 | ✓ 二期 | 纯结构，零风险，直接决定能被几个宿主发现 |
| 工具面事务化（`edit_item` 的 updates 包） | ✓ 参考 | 你们 `canvas_apply_ops` 已经是 ops 数组，语义一致 |
| 异步任务显式轮询工具 | ✓ | 对应 `task_get`，成片合成/生成都需要 |
| 技能层（verification / known-errors） | ✓ 三期 | 提升外部 Agent 成功率，成本低 |
| OAuth 2.1 + DCR + PKCE | 三期 | 一期是「后台独立配置项目」，没有第三方自助注册的诉求 |
| ChatCut 托管服务本身 | ✗ | 闭源、按 credits 计费、素材出境，且是同类竞品 |
| OpenCut 的浏览器渲染链路 | ✗ | 依赖 canvas/WebGPU，服务端跑不了；把成片稳定性押在浏览器进程上不可接受 |

### 3.3 为什么明确不碰 OpenCut 引擎

（推断）OpenCut 的合成是「每帧画在 canvas 上再编码」，你们的成片是 `pkg/videoframe/compose.go:22` 的统一转码 + `concat -c copy`。两套渲染语义不同：它的 effect/mask/transition 走 shader，ffmpeg 复刻不了。**MCP 这一层与引擎完全解耦**，先做 MCP 不会欠下渲染债；timeline 契约留到需要精剪时再评估。

## 4. 目标架构

```
外部 Agent（Codex / Claude Code / Cursor / WorkBuddy）
        │  Authorization: Bearer vcm_…
        ▼
POST /api/v1/mcp     ← Streamable HTTP，JSON-RPC 2.0
        │
        ├─ RequireMCPToken：前缀分流 → 查 sha256 → 校验项目启用 / 未注销 / 未过期 / 用户可用
        │                   → 得到 {ProjectID, UserID, Scopes, DefaultCanvasID}
        ├─ 方法分发：initialize / tools/list / tools/call / ping
        ├─ tools/list：从 tool_decls.json 派生，按白名单与 scope 过滤
        └─ tools/call：从 arguments.canvas 定画布（缺省用 DefaultCanvasID）
                        → 按归属用户校验该画布 → 剥离 canvas → 合成 RunContext
                        → ExecuteExternalTool（复用现成执行器）
                          │
                          ▼
                  工具层（37 个声明，白名单子集）
                          │
                          ▼
                  画布 / 生成队列 / 素材存储（既有链路，零改动）
```

**关键取舍**：不另写工具执行逻辑，`tools/call` 只是「协议翻译 + 身份注入」。这样工具行为只有一处定义，MCP 与内置 Agent 永远不会漂移。

## 5. 数据模型（已实现）

迁移 `backend/migrations/0047_mcp_project.sql`（仓库编号必须连续，见 `tests/migration/schema_test.go:49`）。

```sql
-- MCP 项目：一个外部 Agent 应用一个项目，绑一个画布 + 一个归属用户。
CREATE TABLE mcp_projects (
    id bigserial PRIMARY KEY,
    name varchar(64) NOT NULL,
    owner_user_id bigint NOT NULL,              -- 归属用户：画布与积分都算在他头上
    canvas_id bigint NOT NULL,                  -- 绑定的画布（库内自增 id）
    scopes jsonb NOT NULL DEFAULT '[]'::jsonb,  -- ["canvas:read","canvas:write","canvas:compose"]
    enabled boolean NOT NULL DEFAULT true,
    created_by bigint NOT NULL,                 -- 后台操作人
    created_at timestamptz,
    updated_at timestamptz
);
CREATE INDEX idx_mcp_projects_owner ON mcp_projects (owner_user_id);
CREATE INDEX idx_mcp_projects_canvas ON mcp_projects (canvas_id);

-- 项目令牌：一个项目可有多个（换机、轮换）。库里只存 sha256。
CREATE TABLE mcp_project_tokens (
    id bigserial PRIMARY KEY,
    project_id bigint NOT NULL,
    name varchar(64) NOT NULL,
    prefix varchar(12) NOT NULL,                -- 明文前 8 位，列表里辨认
    token_hash varchar(64) NOT NULL,
    expires_at timestamptz NOT NULL,
    last_used_at timestamptz,
    revoked_at timestamptz,
    created_at timestamptz
);
CREATE UNIQUE INDEX uk_mcp_project_tokens_hash ON mcp_project_tokens (token_hash);
CREATE INDEX idx_mcp_project_tokens_project ON mcp_project_tokens (project_id);
```

模型 `backend/internal/model/mcp_project.go`，两个都注册进了 `model/base.go` 的 `All()`。

**权限单位是用户，不是画布（两次修正的记录）**

初稿是「项目挂一组画布白名单（空 = 全部）」，落地时改成「项目绑单个画布」，**这个改动是错的**，已推翻。
错在把工具层的现状当成了权限模型的依据：

- 画布工具的参数里没有 canvas 参数（全部作用在 `RunContext.CanvasID`，见 `handler/agent/platform.go:23-34`），
  这只说明**协议层需要自己补这个参数**，不说明权限应该按画布切。
- 按画布切会带来一个荒谬的后果：用户有 20 块画布，就得建 20 个项目、20 条令牌、配 20 次。

现在的模型分三层：

| 层 | 是什么 |
| --- | --- |
| 令牌 | → 归属用户 + 权限档位，**不绑任何资源** |
| 单次调用 | → `arguments.canvas` 指定画布，服务端用 `Canvases.GetByID(userID, canvasID)` 校验归属 |
| 项目 | → 只是配置容器：名称、权限档、**默认画布（可空）** |

`mcp_projects.canvas_id` 因此改成可空，语义从「绑定的画布」变成「没指定时用哪块」，
它**不构成权限边界**——边界的唯一判据是「这块画布属不属于这个用户」。

协议层的做法（`handler/mcp/server.go`）：

1. `catalog()` 给画布类工具的 `inputSchema` 注入 `canvas` 字段（工具声明本身不动，模型才知道要传）；
2. `parseArgs()` 把 `canvas` 摘出来，剩余参数原样交给工具（工具完全不知道这个字段存在）；
3. `resolveCanvas()` 定画布：显式 `canvas` > 项目默认画布 > 报错引导「先调 canvas_list」；
   无论哪条都按归属用户查一次 `CanvasProjectRepository.GetByID`；
4. 画布问题按 MCP 约定回 `isError: true` 的结果（模型能读到提示自己纠正），不走 JSON-RPC 错误。

另加一个网关自实现工具 `canvas_list`（不在工具登记表里，只查目录、无副作用），
否则外部 Agent 根本不知道有哪些画布 code 可填。

**画布对外一律用地址里的 code，不暴露自增 id**：请求体收 `canvas_code`（32 位十六进制，`pkg/idcodec`），服务端 `idcodec.Decode` 后落库为 `canvas_id`；回显同样给 code。后台不需要（也不应该）让运维去记数据库自增 id。

**为什么不复用 `admin_api_tokens`**（事实支撑）：那张表的认证前提是「签发人必须是 admin / super_admin」（`service/agent/observe.go:377-383` 每次实时查角色），scope 也写死只有 `agent:read`。MCP 项目要绑**普通用户**、要多档 scope、要有「项目级启停」这一层，塞进去会直接破坏它现有的鉴权前提。分表、复用范式。

## 6. 鉴权（提案）

> 实现时改了两处，本节保留当时的判断供对照：权限单位从「画布」改为「用户」（§15）；令牌明文改为可回看（库里存 AES-GCM 密文，§16）。下面的「只返回一次」以 §16 为准。

- 明文格式 `vcm_` + 32 字节随机十六进制；库里只存 sha256；**明文只在创建响应里返回一次**。
- 中间件 `RequireMCPToken`：按 `vcm_` 前缀分流（与 `analysis_token.go:31` 同一手法，互不干扰）→ 查 hash → 校验未注销 / 未过期 / 项目 `enabled` / 归属用户 `active` → 注入 `{ProjectID, UserID, Scopes}`。
- 失败统一回同一句「MCP 令牌无效、已过期或已停用」，不区分具体原因（对齐 `errBadToken` 的做法）。
- 限流：固定窗口，按 token 每分钟 N 次；多实例各自计数，对单机 Agent 用量足够（对齐 `allow()` 的取舍）。
- 审计：签发 / 注销 / 改项目写 `admin_audit`，新增 `AdminAuditMCPTokenIssue` / `AdminAuditMCPTokenRevoke` / `AdminAuditMCPProjectUpdate`。
- 到期上限 90 天，不允许永久令牌（对齐分析令牌的取舍）。

## 7. 传输与协议（提案）

- 端点 `POST /api/v1/mcp`，**注册在无 JWT 的公共组**（与 `router.go:99` 的打包下载同组思路），由自己的中间件鉴权。`GET` 暂不实现（不提供 SSE 流）。
- 方法：
  - `initialize` → 回 `protocolVersion`、`capabilities: {tools: {}}`、`serverInfo`；生成 `Mcp-Session-Id` 响应头。
  - `tools/list` → 只回白名单且被 scope 允许的工具，`inputSchema` 直接用 `tool_decls.json` 里的 `parameters`。
  - `tools/call` → 执行；**工具业务失败回 `isError: true` 的 result，不抛 JSON-RPC error**（MCP 约定，模型要看得到错误文本自己修正）。
  - `ping` → 空结果。
- `Mcp-Session-Id`：一期只用于日志与审计关联，**不用来存服务端状态**（多实例没有粘性时会炸）。`RunID` 由 `(project_id, session_id)` 派生一个稳定的合成值，只为满足读前写门禁的内存键。
- 超时：单次请求 60s 上限；`canvas_compose` / `generate_media` 都是提交即返回 job id，不在 HTTP 里等。

## 8. 工具面（已实现，白名单默认拒绝）

| 分组 | 工具 | scope | 说明 |
| --- | --- | --- | --- |
| 读 | `canvas_list` | `canvas:read`（任一画布档可见） | **网关自实现**：列归属用户名下的画布 code，画布类工具的前置 |
| 读 | `canvas_get_state`、`canvas_inspect_image`、`canvas_inspect_video`、`canvas_capture_frame`、`task_get`、`model_list`、`skill_search`、`skill_read` | `canvas:read` | `canvas_get_state` 同时是写操作的前置 |
| 写 | `canvas_apply_ops`、`canvas_arrange` | `canvas:write` | 受读前写门禁约束 |
| 合成 | `canvas_compose` | `canvas:compose` | 消耗算力不消耗积分 |

排除表（`handler/mcp/tools.go` 的 `deniedTools`，每条都写了原因）：
`canvas_delete`、`generate_media`（需人工审批）、`ask_user`、`show_card`（无对应 UI）、
`web_search`、`web_fetch`（宿主自带）、`plan_update`（只在 Agent 服务内）、
`admin_*`（14 个运营后台）、`drama_*`（5 个短剧片场链路）。

**门禁**：覆盖测试要求工具登记表里每个工具**要么在开放表、要么在排除表**（`tests/handler/mcp_test.go`），
漏掉就红；另有两条硬约束测试：开放表里不能出现需要审批的工具、不能出现 `admin_*`。

## 9. 页面（已实现两个入口）

### 9.1 我的 MCP 接入（用户自助，主入口）

任何登录用户都能给自己开，不必找管理员。

- 路由 `/mcp`（`pages/mcp/index.tsx`），入口在侧栏账号菜单「个人中心」下面：**我的 MCP 接入**。
- 接口 `/api/v1/me/mcp/projects`（`handler/mcp_self.go`）：列表 / 新建 / 修改 / 删除 + `/:id/tokens` 签发注销。
  **归属用户固定为登录用户**，请求体里的 `owner_user_id` 一律忽略；项目不属于自己时按「不存在」返回，不泄露存在性。
- 每人最多 10 个项目（`mcpMaxProjectsPerUser`），防脚本刷项目与令牌。
- 新建表单只有三样：名称、默认画布（可空）、权限档位。没有「归属用户」这一栏。

### 9.2 后台 MCP 接入（运维视角）

- 路由 `/admin/agent/mcp`，导航在「AI 配置」组第 7 项（图标 `Cable`）。
- 接口 `/api/v1/admin/mcp/projects`，全部 `RequireAdmin`，**仅删除项目 `superOnly`**。
- 比 9.1 多两样能力：能指定**任意用户**的归属、能看到全部项目（含归属用户列）。
  用途是代客开通与违规处置（停用别人的接入）。

两套页面共用 `components/mcp-projects/{view.tsx,use-mcp-projects.ts}`，
靠 `mode: "admin" | "self"` 切换「归属用户」列与选择器；接口集合由 `useMCPProjects(audience)` 选择。

## 10. 部署（已实施）

- **nginx 零改动**：既有 `/api/` 反代已经覆盖 `POST /api/v1/mcp`，2.88 实测直通（web 日志有 `POST /api/v1/mcp 200`）。当前响应是一问一答的普通 JSON，不需要关 buffer。
- 无需新进程、无需新容器；生成与合成仍走既有队列。
- 若将来上 SSE（`text/event-stream`），再给 `location = /api/v1/mcp` 加 `proxy_buffering off` 与更长的 `proxy_read_timeout`。

## 11. 门禁与测试（提案）

| 门禁 | 判据 |
| --- | --- |
| 工具面一致性 | `tools/list` 输出 == 白名单∩scope，且 `tool_decls.json` 每个工具都被分类（覆盖测试） |
| schema 一致 | `tools/list` 的 `inputSchema` 与 `tool_decls.json.parameters` 逐字段相等 |
| 越权 | 没有 `canvas:compose` 的令牌调 `canvas_compose` → JSON-RPC `-32602`「这个 MCP 项目没有该能力的权限」 |
| 画布越界 | 绑定画布不属于归属用户 → `ErrInvalidParams`「这个用户没有该画布」（`fillProject` 里用 `Canvases.GetByID(ownerID, canvasID)` 校验） |
| 令牌生命周期 | 过期 / 注销 / 项目停用 / 用户禁用 四种都拒绝 |
| 读前写门禁 | 未先 `canvas_get_state` 直接 `canvas_apply_ops` → 拒绝 |
| 真库迁移 | `TEST_DATABASE_DSN` 下跑 `internal/tests/migration`，**不给 DSN 是 skip 不是 pass** |
| 前端 | `tsc` / `oxlint` / `oxfmt` / `bun test` |

## 12. 分期

| 期 | 内容 | 完成判据 |
| --- | --- | --- |
| 一期 | 迁移 0047 + 模型 + 令牌中间件 + `initialize`/`tools/list`/`tools/call` + 读工具 + 后台项目页与令牌签发 | 用 `curl` 走完 initialize → tools/list → canvas_get_state 全链路；后台能签发令牌并看到最近使用 |
| 二期 | 写工具 + `canvas:generate` 配额 + 多宿主清单（`mcp/<host>/`，软链同源 + 一致性测试） | Codex 与 Claude Code 各自配一次，能改画布 |
| 三期 | OAuth（对齐 ChatCut 的 DCR+PKCE）、技能层（verification / known-errors 体例）、OpenCut timeline 契约评估 | 第三方可自助接入 |

## 13. 风险与未决

| 风险 | 处置 |
| --- | --- |
| 外部 Agent 写画布与 Web 多标签页冲突 | 沿用现有 409 + revision 语义，**冲突必须显式失败**，让 Agent 重新读状态；不要做「最后写入者赢」 |
| 生成类工具花积分 | `canvas:generate` 默认关闭；项目里可配每轮 `BudgetCredits` |
| 工具面泄露运营能力 | `admin_*` 硬排除；覆盖测试防回归 |
| 多实例 session 无粘性 | 一期 session 只用于日志，不存状态 |
| 令牌明文外泄 | 只返回一次、只存 sha256、90 天上限、可即时注销、每次用都写 `LastUsedAt` |

**未决**：
1. 归属用户的选择方式——后台管理员直接指定任意用户，还是只能绑自己？（建议：super_admin 可绑任意用户，admin 只能绑自己）
2. `canvas:generate` 是否需要「每次调用二次确认」，还是靠预算封顶（建议：预算封顶，避免外部 Agent 卡在确认上）。
3. 是否要在 `tools/list` 里同时返回中文描述（`tool_decls.json` 目前是中文描述，外部宿主的模型可能英文更强，但中文描述更贴业务）。

## 14. 实施与验收记录（2026-11-23，2.88 生产）

### 已交付

| 层 | 文件 | 说明 |
| --- | --- | --- |
| 迁移 | `backend/migrations/0047_mcp_project.sql` | 两张表；启动自动执行（`initialize/db.go:31` 的 `AutoMigrate`） |
| 模型 | `model/mcp_project.go`、`model/base.go`、`model/admin_audit.go` | 视图 / 请求体 / scope 常量；审计动作 `mcp.project_create` 等 |
| 数据层 | `repository/mcp_project.go` | 项目 CRUD（删项目连令牌一起删）、令牌签发 / 注销 / `TokenStats` |
| 服务层 | `service/mcp_project.go` | 令牌生命周期、`Authenticate`、限流（120/min/令牌）、`LastUsedAt` 回写 |
| 执行层 | `service/agent/platform.go` 的 `ExecuteExternalTool` | 复用 `ExecutePlatformTool` 的校验，但**不写 `agent_tool_calls`**（外部接入没有 `agent_runs` 行，写了就是孤儿数据） |
| 协议层 | `handler/mcp/server.go`、`handler/mcp/tools.go` | `initialize` / `tools/list` / `tools/call` / `ping` / `notifications/*`；工具面白名单 + 排除表 |
| 鉴权 | `middleware/mcp_token.go` | 只认 `Bearer vcm_`，把 project/user/canvas/scopes 注入上下文 |
| 后台 | `handler/admin_mcp.go` + `web/src/pages/admin/mcp-projects/**` | 项目 CRUD、令牌签发 / 注销、配置片段复制 |

### 2.88 实测证据（`https://lj.igeewa.com`）

| 步骤 | 结果 |
| --- | --- |
| 无令牌 `POST /api/v1/mcp` | `401` + `{"code":62001,"msg":"MCP 令牌无效、已过期或已停用"}` |
| `initialize`（`2025-06-18`） | `200`，返回 `serverInfo` 与 `Mcp-Session-Id` 响应头 |
| `notifications/initialized` | `202`，空 body（符合规范：通知不回包） |
| `ping` | `{"result":{}}` |
| `tools/list`（read+write） | **10 个工具**：`canvas_get_state`、`canvas_apply_ops`、`canvas_arrange`、`canvas_capture_frame`、`canvas_inspect_image`、`canvas_inspect_video`、`model_list`、`skill_read`、`skill_search`、`task_get` |
| `canvas_get_state` | 返回绑定画布的目录（`nodes` / `total` / `edgeCount`） |
| `canvas_apply_ops` 建节点 | `{"changes":1,"id_map":{"t1":"n_3e12afb37c"},"ok":true,"revision":2}`，读回可见 |
| 越权 `canvas_compose`（无 compose 档） | JSON-RPC `-32602`「这个 MCP 项目没有该能力的权限」 |
| 排除工具 `canvas_delete` | JSON-RPC `-32602`「这个能力不对外开放」（`tools/list` 里也不出现） |
| 业务失败（script→script 连线） | `isError: true` + 「script 节点的下游只能是 image、video、audio、storyboard」，**整批不生效**（原子性） |
| 令牌回写 | `mcp_project_tokens.last_used_at` 有值；`admin_audit_logs` 记到 `mcp.project_create`、`mcp.token_issue` |

### 部署事实（回滚用）

- 镜像：`video-canvas-backend:0.7.9-mcp3`、`video-canvas-web:0.7.9-mcp3`（`/opt/video-canvas/docker-compose.filing.yml` 固定）
- 库备份：`/opt/video-canvas/backups/video_canvas.pre-mcp-20261123_015249.sql.gz`（20 MB）
- 回滚：`filing.yml` 改回 `0.7.9-mcp1`…`archive5` 任一一版 + `docker compose -f docker-compose.yml -f docker-compose.filing.yml up -d`；**漏掉 `-f` 会回滚到 `.env` 里的 0.6.17**
- 自测画布：`MCP 端到端自测`（`8d40668b6f71f73dc78767d3a59c7026`），里面有 2 个文本节点，勿删

### 已知未做

1. **`canvas:generate` 未开放**：`generate_media` 走审批链路，一期不接。
2. **OAuth 未做**：令牌是手填 Bearer，第三方自助接入要等到三期。
3. **多宿主清单未做**：现在只有「复制配置片段」，没有 `mcp/<host>/` 目录与一致性测试。
4. **`canvas_delete` 等仍需人**：外部只能新建与修改，删除留在 Web 里做。

## 15. 第二轮：改为用户级权限 + 用户自助（0.7.9-mcp4，同日）

### 为什么要改

第一版把项目绑死一块画布，还把「只有管理员能开」当成前提。两条都不对：

1. **权限是用户级的**。令牌代表「某个用户授权了一个外部应用」，能访问该用户名下的全部资源；
   画布只是资源之一，不该成为权限单位。绑死画布的直接后果是「20 块画布要建 20 个项目」。
2. **开通权应该给用户**。这是用户给自己的工具开权限，不是需要行政审批的事。

### 改了什么

| 层 | 改动 |
| --- | --- |
| 迁移 | `0048_mcp_user_scope.sql`：`mcp_projects.canvas_id` 改为可空，语义变成「默认画布」 |
| 模型 | `CanvasID *uint64`；`canvas_code` 回显可空；`owner_user_id` 在请求体里变可选（自助入口填自己） |
| 服务层 | `MCPIdentity.DefaultCanvasID`；新增 `Owned*` 六个方法（`service/mcp_project_self.go`）；每人 10 个项目上限 |
| 令牌认证 | 拆到 `service/mcp_token_auth.go`（原文件超 500 行的 lint 限制） |
| 协议层 | `catalog()` 注入 `canvas` 参数；`parseArgs()` 摘出 canvas；`resolveCanvas()` 定画布并校验归属 |
| 网关工具 | 新增 `canvas_list`（自实现，不走工具登记表） |
| 接口 | 新增 `/api/v1/me/mcp/projects` 全套（`handler/mcp_self.go`），归属固定为登录用户 |
| 前端 | 共用组件挪到 `components/mcp-projects/`；新增 `/mcp`（我的 MCP 接入）与账号菜单入口 |

### 2.88 实测（`0.7.9-mcp4`）

| 项 | 结果 |
| --- | --- |
| `0048` 迁移 | `information_schema` 查 `mcp_projects.canvas_id` → `is_nullable = YES` |
| 自助建项目（不带 owner、不带画布） | `200`，`owner_user_id=1`、无 `canvas_code` |
| 自助签发令牌 | `200`，明文一次性返回 |
| `tools/list` | **11 个**（原 10 + `canvas_list`），`canvas_get_state` 的 schema 里有 `canvas` 字段 |
| `canvas_list` | `total 9`，条目 `id` 是 32 位 code；只含 `drama_id IS NULL` 的普通画布 |
| 同一令牌读画布 A | `isError:false`，返回 `n_3e12afb37c` 所在画布 |
| 同一令牌读画布 B | `isError:false`，返回另一块画布（**证明不再绑死单块**） |
| 显式 canvas 覆盖默认画布 | 默认设为 A，传 B 的 code → 读到 B |
| 不带 canvas 且无默认 | `isError:true`「先调 canvas_list 拿到画布 code」 |
| 乱码 canvas | `isError:true`「不是有效的画布 code」 |
| `canvas_apply_ops` 带 canvas 写入 | `{"changes":1,"revision":4}`，读回可见 |

### 单测覆盖

- `tests/handler/mcp_canvas_test.go`：默认画布兜底、他人画布拒绝、缺画布引导、显式覆盖默认、
  `canvas_list` 只列本人、只有写权限也能列画布、`canvas` 字段不进工具参数。
- `tests/service/mcp_project_test.go`：自助建项目强制归属本人、默认画布可为空、别人的项目改删签全被拒、
  每人 10 个上限。

### 部署事实

- 镜像 `video-canvas-backend:0.7.9-mcp4`、`video-canvas-web:0.7.9-mcp4`。
- 回滚到 `0.7.9-mcp3` 不恢复库也能跑：mcp3 把 `canvas_id` 当必填，而已有项目都有值；
  NULL 只可能出现在新版本建的记录上（本项目 `MCP 用户级自测` 就是）。

## 16. 第三轮：七档权限 + 生成直连 + 一项目一令牌（0.7.9-mcp8，同日）

### 为什么要改（四条都是用户当场指出的）

1. **创建流程冗余**：新建时要填名称、归属用户、默认画布——后两项不是新建时该做的决定。
2. **创建时不能勾权限**：能力勾选被挪到「编辑」，等于新建完必须再点一次，这是设计错误。
3. **生成不开放等于没价值**：把「需要人工审批」当成「一期都不开」的理由，结果 MCP 只能看不能产出。
4. **有效期与超时被混为一谈**：令牌多久失效（有效期）和一次调用最多等多久（超时）是两件事。

### 改了什么

| 层 | 改动 |
| --- | --- |
| 迁移 | `0050_mcp_token_secret.sql`：`mcp_project_tokens.secret_cipher text NOT NULL DEFAULT ''` |
| 模型 | 新增 `canvas:generate`（第七档，排在影响最大的一端）；`MCPScopeRequires` 加 `generate → read`；`MCPTokenView.HasSecret`；`MCPTokenIssueReq.Days` 改 `omitempty`（留空走默认值） |
| 设置 | 新增两项：`mcp_token_days`（1–90，默认 30）、`mcp_call_timeout`（5–300 秒，默认 60），`SettingsService.MCPTokenDays` / `MCPCallTimeout` |
| 令牌明文 | 鉴权仍用 sha256；另存 AES-256-GCM 密文（`base64(nonce||ct)`），AAD = `mcp-token:` + 该令牌自己的 hash，主密钥复用 `APP_AI_SECRET_KEY`。老令牌无密文 → `has_secret=false`，界面只让重新签发 |
| 一项目一令牌 | `IssueToken` 成功后 `DeleteOtherTokens(projectID, id)`：先建后删，中途失败也不会出现「一枚都没有」 |
| 生成直连 | `platform.go` 新增 `externalNoApproval = {"generate_media": true}`；`ExecuteExternalTool` 对登记项放行并给 `toolCall.external=true`；`toolGenerate` 分支到 `toolGenerateExternal` → `AgentGenerator.ExecuteDirect` |
| 幂等 | `ExecuteDirect(ctx, run, callKey, items)`：任务键 `mcp-<callKey>-<i>`、绑定键 `mcp-<callKey>`、`AgentRunID=0`（外部没有真实运行，写进去只会留下孤儿通知） |
| 调用超时 | `Server.SetCallTimeout`；`callTool` 内 `context.WithTimeout` 包住画布解析与工具执行；超时回 `isError` 的「拆小重试 / 让管理员调大」而不是 JSON-RPC 错误 |
| 不落画布的工具 | `RunContext.NoCanvas`：`task_get` / `model_list` / `skill_search` / `skill_read` 在项目没配默认画布时不再被「缺少 canvas_id」挡住 |
| 对外语义 | `externalDescriptions["generate_media"]` 重写说明（提交即返回、用 `task_get` 查进度）；schema 里摘掉对这条通道无意义的 `wait` |
| 接口 | `GET /admin/mcp/projects/:id/tokens/:tokenId/secret`、`GET /me/mcp/projects/:id/tokens/:tokenId/secret` |
| 前端 | 新建/编辑共用 `project-dialog.tsx`（`PageHeaderActions` + `Dialog`，不再是页内表单）；`scope-fields.tsx` 勾选走依赖闭包；`copy-secret-button.tsx` 点时才取明文；`AccessInfo` 常驻端点 + 配置片段（含 `timeout: 60000`，单位毫秒）；令牌按钮在已有令牌时显示「重新签发」 |

### 2.88 实测（`0.7.9-mcp8`，`https://lj.igeewa.com`）

| 项 | 结果 |
| --- | --- |
| `0050` / `0051` 迁移 | `数据库迁移完成 0050_mcp_token_secret.sql 0.034s`；孤儿迁移按 `allow_newer_schema` 放行（0043/0045–0048） |
| `GET /admin/settings/register` | 含 `mcp_token_days:30`、`mcp_call_timeout:60` |
| 改设置 `days=14 / timeout=90` | `200`，回读一致；`timeout=1000` 被拒（「MCP 调用超时必须小于或等于300」） |
| 签发留空有效期 | `expires = 2026-12-23`（默认 30 天）；把后台改成 14 后再签 → `2026-12-07`（**证明默认值真的被消费**） |
| 令牌列表 | 只剩 1 枚（id 7）；旧令牌 id 6 已被删 |
| 取明文 | `code 0`，长度 68，与签发时完全一致；无密文的老令牌拒绝复制 |
| 新旧令牌 | 旧 `401`、新 `200` |
| 项目 2（原有 2 枚令牌）再签发 | 列表只剩 1 枚（id 8）——多枚也一次清干净 |
| `tools/list` | `["canvas_apply_ops","canvas_get_state","canvas_list","generate_media","model_list","skill_read","skill_search","task_get"]`，与 `browse+read+write+generate` 严格一致 |
| `generate_media` schema | 只有 `canvas` / `items` / `reason`（`wait` 已摘），说明写「提交即返回，用 task_get 查进度」 |
| `canvas_list` / `task_get` / `model_list` / `skill_search` | 全部 `isError:false`（`task_get` 的 `taskId` 要传**十进制字符串**） |
| 生成产物 | 任务 605 `status:succeeded`、`charged_credits:1`、`asset_id:911` |

### 这一轮踩到的两个真问题

1. **有效期必填**：前端留空不传 `days`，后端 DTO 还是 `binding:"required"` → 线上直接回「有效期为必填字段」。
   改 `omitempty` + 服务层兜底，并补 `TestMCPTokenDaysFallBackToDefault`（默认 30 / 库值 7 / 显式 45 / 91 拒）。
2. **不落画布的工具被上下文校验挡住**：精确化 `CanvasNeeded` 后 `RunContext.CanvasID=0`，
   而 `validate()` 的规则是「非后台模式必须有画布」→ `task_get` 报「运行上下文缺少 run_id、user_id、session_id 或 canvas_id」。
   修法不是放宽校验，而是显式声明 `NoCanvas`；补 `TestMCPCanvasLessToolsNeedNoCanvas`（四类工具放行 + 画布工具仍必须带画布）。

### 已知未做 / 边界

- **外部生成的节点状态要等网页端回填**：任务跑完后 `generation_tasks` 是 succeeded，但画布文档里节点仍是
  `running`（前端 `utils/tasks/backfill.ts` 在打开画布时按 `node.taskId` 回填产物与状态）。
  纯 MCP 用户从不打开画布时，该节点会一直显示「正在生成中」，`generate_media` 也会以「正在生成中」拒绝。
  根治要在服务端做一次结果回写（要处理产物合并、revision 冲突），留到二期。
- **单次超时只能靠单测证明**：线上没有稳定超过 5 秒的工具调用，`TestMCPCallTimeoutReturnsActionableError`
  用 30ms 上限 + 故意挂住的假工具覆盖；设置项本身已验证可写可读。
- **`plugin-runner` 仍跑 `0.7.9-mcp7` 镜像**：compose 里已改成 mcp8，但没重启（它与 MCP 无关，避免打断队列）。

## 17. 第四轮：短剧成为独立能力域（0.7.10-mcp9，同日）

### 为什么要改

短剧工具一直在工具登记表里，但被 `deniedTools` 全部关着，理由是「短剧片场链路，与画布语义不同」。
这个理由只说对了一半：短剧**确实**是画布（`canvas_projects.drama_id` 指过去），归属边界完全复用；
但它改的是剧本原文、跑的是流水线任务，跟「挪节点、改提示词」不是一回事。

同时「需要审批」也不构成不开放的理由——生成素材已经证明过：外部接入弹不出审批卡片，
真正该挡的是「不可逆且没有天然上限」的操作（删除），而不是一切需要确认的操作。

用户的原话是「增加短剧」，并强调 **MCP 是独立能力**：所以短剧的权限档不挂在 `canvas:` 前缀下，
而是自成一条能力域 `drama:*`，界面上也分块摆。

### 三个必须先解决的前提（只把工具加进白名单是不够的）

1. **`canvas_list` 看不到短剧**。仓储的 `List` 写死 `WHERE canvas_projects.drama_id IS NULL`，
   因为网页端画布首页与短剧列表是两个页面。外部工具没有「另一个页面」，
   不加 `drama_list` 就永远不知道该往 `drama_*` 的 `canvas` 参数里填什么——工具开了也是死的。
2. **短剧工具只登记在 drama 模式**。`catalog` 原来只取 `AgentToolDecls(AgentModeAll)`，
   drama 专属工具根本不在那份声明里；不合并一次，勾了权限也看不到。
3. **`RunContext.Mode` 写死 `all`**。写工具要过 `checkDramaPayload` 的 `rc.Mode == drama`，
   外部接入若一律用 `all`，就算放行也会被判「模式不正确」。修法是按工具名给模式（`ExternalMode`），
   让短剧工具走既有校验，而不是在工具层另开绕过口。

### 改了什么

| 层 | 改动 |
| --- | --- |
| 权限档 | 新增独立能力域三档：`drama:read` / `drama:write` / `drama:run`；`write`/`run` 依赖 `read`；**不依赖 `canvas:*`** |
| 工具开放 | `toolScopes` 登记 `drama_list` / `drama_read` / 3 个 `drama_update_*` / `drama_run_job`；从 `deniedTools` 移除 5 条 |
| 目录工具 | 新增 `drama_list`（网关自带，和 `canvas_list` 同级）：返回 `canvas` code、标题、状态、阶段、目标集数，不返回原文 |
| 声明合并 | `catalog` 合并 `AgentModeAll` 与 `AgentModeDrama` 两份声明并按 name 去重 |
| 模式注入 | `ExternalMode(name)`：`drama_*` → `drama`，其余 → `all`；`server.go` 用它构造 `RunContext` |
| 画布参数 | `CanvasNeeded` 覆盖 `drama_*`（它们都要落在具体短剧画布上） |
| 直连执行 | `externalNoApproval` 登记 4 个短剧写/跑工具；`dramaProposal` 在 `tc.external` 时走 `dramaDirect` → `executeDramaWrite` |
| 幂等键 | `externalDramaKey(toolCallID)`：sha256 取 8 字节并**置最高位**，避免与自增审批 ID 相撞（`CreateAgentJob` 要求非零且把它当幂等键） |
| 对外说明 | `externalDescriptions` 补 5 条，写明「立即生效、不经审批」「跑任务花积分、提交即返回、用 drama_read 的 active_jobs 查进度」 |
| 前端 | `MCP_SCOPE_GROUPS`（画布 / 短剧两块）；`MCP_SCOPES` 十档；`ScopeChecklist` 按域分组渲染 |

### 边界与取舍

- **短剧创建不开放**：建项目要传原文（文件或长文本），还要选模型、比例、目标集数，
  外部工具做这件事既笨重又容易建出一堆废项目。现有项目在网页里建好，外部工具接手改与跑。
- **短剧任务进度只能 `drama_read` 的 overview 看**：没有单独的 `drama_job_get`，因为
  `DramaView.active_jobs` 已经带状态与步骤，够判断「跑完没有」；再加一个工具只会让模型多一次选择。
- **重发请求会新建任务**：幂等键按调用 ID 派生，挡的是同一次调用的内部重放，
  不是客户端主动重发。与 `generate_media` 的现状一致；要跨请求幂等得让外部传 idempotency key，二期再谈。
- **`drama_list` 放在 `drama:read`**：没勾短剧的用户连这个工具都看不到，
  避免「能看到短剧列表但什么都干不了」的半吊子状态。

### 门禁

- 后端 `go build` / `gofmt -l` 干净；`go test ./internal/...`（真库）全绿。
- 新增用例：`TestMCPDramaToolsAreIndependentScope`（三档看到的工具严格对应、不夹带画布权限）、
  `TestMCPDramaToolsRequireCanvas`（没画布给可执行提示）、`TestMCPDramaListReturnsCanvasCode`、
  `TestDrama_ExternalMode`、`TestDrama_ExternalDirectWrite`（直连落库、参数校验仍生效）、
  `TestDrama_ExternalRunJob`（幂等键高位、重放不重复建任务）、`TestDrama_ExternalRejectsNonDramaCanvas`。
- 前端 `bun test src/tests/utils/admin/mcp-project.test.ts` 28 项通过、`tsc -b` 无错。

## 18. 第五轮：一个项目一个接入，建好即给配置（2026-11-24）

### 为什么改

第一版把「项目」与「令牌」分成两层：建项目只写一行记录，令牌要再点一次「签发」才出现，
明文与配置片段也是那一步之后才有。实际用起来是三步（建项目 → 签发 → 复制），
而列表里的 `token_count` 只可能是 0 或 1——这层「令牌列表」没有第二种形态。

### 改了什么

| 层 | 改动 |
| --- | --- |
| 接口 | `POST /admin/mcp/projects` 与 `POST /me/mcp/projects` 的响应从项目变成 `{item, issued}`：项目 + 首枚令牌（明文、端点）。令牌名默认「接入令牌」，有效期取「注册设置」的默认值 |
| 服务 | 抽出 `issue()`，`Create` 与 `IssueToken` 共用同一套签发规则；`Create` 里签发失败会 `DeleteProject` 回滚，不留连不上的空接入 |
| 请求 | `MCPTokenIssueReq.name` 从 required 改为可空：留空由服务端用默认名，「重新生成」不再需要填表 |
| 页面 | 删掉「令牌列表 + 签发表单」，项目展开就是「接入配置」：配置片段、令牌状态与到期、重新生成；表格也不再显示令牌数量 |
| 路由 | 令牌从「集合」收成「项目的属性」：`POST /projects/:id/token`（生成或轮换）、`GET /projects/:id/token/secret`（取明文）；`GET/POST/DELETE /projects/:id/tokens/:tokenId` 全部去掉，注销随之下线（断开靠停用项目或重新生成） |
| 列表 | 项目视图直接带 `token` 字段，仓储新增 `TokensByProjects` 批量取每个项目最新一枚，列表页一次请求就能渲染令牌状态 |
| 删除 | `copy-secret-button.tsx` 不再有使用者，随面板一起删掉 |

### 取舍

- 明文默认不显示：配置片段要先点「显示配置片段」，此时才向服务端要密文并拼片段，
  避免一进页面就把令牌摊在屏幕上。
- 不做「先建项目、事后补签」：一个项目一枚令牌，补签与重新生成是同一个动作，只留一个按钮。

### 验证

- 后端：`TestMCPOwnedCreateForcesSelfAndAllowsNoDefaultCanvas` 增加「返回里必须带首枚令牌明文」的断言；
  `go test ./internal/tests/service/ ./internal/tests/handler/ -run MCP` 全绿。
- 前端：`tsc -b` 无错；`bun test src/tests/utils/admin/` 960 项通过。

## 19. 第六轮：按后台通用规范收敛界面（2026-11-24）

### 为什么改

第五轮收成「一个项目一枚令牌」后，页面上同一组信息出现了两遍：顶部的「接入信息」面板给端点 + 占位片段，
展开项目后又给一次令牌 + 完整 JSON 片段。实际用起来还多出两类问题：弹窗是打开时才挂载的（关闭没有退出动画），
能力勾选用的是原生复选框加三行小字说明，和后台上其它「能力」多选长得不一样。

### 改了什么

| 层 | 改动 |
| --- | --- |
| 接入配置 | 展开项目就是两块：**API Key**（单独一行、可单独复制）与**配置片段**（粘贴即可用的完整 JSON，地址与令牌都在里面）。页面顶部那块重复的「接入信息」面板删掉 |
| 令牌展示 | 取回明文后用 `SecretReveal`（绿框 + 等宽码 + 复制 + 隐藏），和后台「分析令牌」页同一套组件；未取回时显示 `prefix` + 打码点 |
| 弹窗 | 常驻挂载 + `DialogSession`：关闭时正文留到退出动画播完，每次打开回到初始值；内容区限高自己滚动（规范 6.3） |
| 能力勾选 | 换成按能力域分行的 `ToggleChip`，依赖关系写进悬停提示，不再用「需要：X」这种说明小字（规范 6.2） |
| 错误处理 | 写操作失败不再自己弹 toast，交给全局拦截器；列表失败改为表格位置上的「项目列表加载失败 + 重试」（规范 §3） |
| 顶部说明 | 删掉操作步骤式文案，只留 Beta 与授权 / 计费这类必须告知的事实 |
| 代码 | `mcpConfigSnippet` 与它的测试一并删除（没了片段就没有生成器）；注释统一改 JSDoc |

### 取舍

- **去重的是「出现两遍」，不是片段本身**：片段要留（一次粘贴就能接上，比手填 URL + 头省事），
  但地址与令牌只在它里面出现一次，页面别处不再单独铺一遍端点。
- 片段格式目前只有 Claude 系那一种；将来要覆盖 Codex（TOML）这类，应当是「选客户端 → 给对应格式」，
  而不是在页面上并排放几份。
- 明文仍可随时取回（密文在 `secret_cipher`），所以「隐藏」只是收起显示，不是销毁凭证。

### 验证

- `tsc -b` 无错；`oxlint src/components/mcp-projects` 0 error；`bun test src/tests/utils/admin src/tests/components` 1305 项通过。
- 线上（`0.7.9-mcpkey2`）：产物里不再有 `mcpConfigSnippet` 的痕迹，`SecretReveal` 组件随主包一起加载。
