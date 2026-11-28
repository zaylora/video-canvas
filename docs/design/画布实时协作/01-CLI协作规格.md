---
title: 全功能 CLI 画布实时协作
feature_id: V1-FR-001
domain: 画布实时协作
spec_status: Draft
risk: 高风险
---
# 全功能 CLI 画布实时协作

## 功能卡

| 项目 | 内容 |
| --- | --- |
| 功能编号 | V1-FR-001 |
| 使用对象 | 画布所有者、受邀编辑者、查看者、有独立授权的团队自动化 |
| 上游依赖 | [画布实时协作](产品方案.md)、[共享交付](../共享画布/一期方案.md)、生成与积分服务、素材与资产库、小镜 |
| 技术设计 | [CLI 协作技术设计](01-CLI协作规格-技术设计.md) |

## 目标

4–8 位成员可以独立登录 `vc`，与同时在线的网页用户操作同一张普通协作画布。成员有权执行的工作由终端完成，内容和任务状态按原有网页规则保存、共享并追溯。人在终端使用简洁命令或交互确认。脚本使用结构化输入、机器可读输出、确定的版本前置条件和可恢复的提交记录。

全功能以网页上可执行的业务动作和数据结果为验收口径。拖动画布对应坐标和布局命令，播放、放大和取色等视觉检查使用素材导出或显式 `--open`，导演台和涂鸦使用结构化参数表达。画布内的分镜表和普通画布是本功能的对象。短剧专用画布继续按短剧工作流管理。

## 功能对照与命令

以下为拟定命令。既有 `vc` 命令在识别到协作模式后按新契约分流。示例中的 `<画布>`、`<节点>` 和 `<操作>` 都是稳定 ID。读取操作可通过搜索查找 ID，写入解析为确定的 ID 后再预览并提交。

| 能力 | 拟定入口 | 完成标准 |
| --- | --- | --- |
| 登录、身份与发现 | `vc login`、`vc whoami`、`vc canvas list --scope personal/shared/all` | 个人、仅查看和可编辑画布可区分。授权和记录使用实际账号，界面显示用户名。 |
| 打开、检查和搜索 | `vc canvas status/get/graph <画布>`、`vc canvas node list/get <画布>` | 显示角色、可用能力、协作代数、确认版本。节点可按类型、分组和名称筛选。仅查看入口保留角色对应的图谱和素材裁剪。 |
| 实时监听和跟随 | `vc canvas watch <画布> --since <版本>`、`vc canvas presence <画布>`、`vc canvas watch <画布> --follow <成员>` | 内容、任务摘要、权限、评论和在线成员可订阅。跟随输出有权查看的实时节点、选区和视角坐标。浏览具体内容时可主动打开网页。 |
| 项目管理和迁移 | `vc canvas create/import/export/rename <参数>`、`vc canvas archive <画布>` | 按角色管理标题、导入、导出和归档。协作导出固定确认版本，归档复用网页退役和在途任务规则。 |
| 节点和图谱 | `vc canvas node add/set/rm/move/duplicate <画布>`、`vc canvas edge add/rm <画布>`、`vc canvas group add/set/rm <画布>` | 覆盖文本、图片、视频、音频及下列结构化节点。支持连线、分组、层级、对齐、分布和坐标。删除预览展示级联影响。 |
| 参数和引用 | `vc canvas node refs <画布> <节点>`、`vc model show <模型>` | 文本正文、提示词、模型、参数、节点连线引用与手选媒体参考按网页语义保存。覆盖图生视频、参考图和模型支持的首尾帧、音视频参考。 |
| 分镜表 | `vc canvas storyboard shot list/add/set/move/rm <画布>`、`vc canvas storyboard split <画布>` | 镜头用稳定 ID。支持表格元数据、镜头字段、顺序、素材引用、自动拆分镜、追加和覆盖。可查询、等待及按服务能力取消拆分任务。 |
| 导演台 | `vc canvas director scene get/apply <画布> <节点>` | 可读写角色、道具、摄像机、镜头轨迹和关键帧等现有场景模型支持的数据。部分更新基于已知对象和版本。视觉校对可打开网页。 |
| 角色卡 | `vc canvas character get/set <画布> <节点>`、`vc canvas character generate <画布> <节点> --kind image/voice/image-and-voice` | 支持角色字段、图片与音色、音色引用或生成，角色形象和音色连跑可恢复。 |
| 素材与资产库 | `vc asset upload/download <参数>`、`vc canvas asset attach/replace/collect <画布>`、`vc asset library list <参数>` | 同一流程处理本地上传、选个人资产并添加、节点上替换、引用授权媒体、导出或下载、经授权收藏到个人资产库。保留上传、画布素材、个人收藏各自的归属。 |
| 图片加工 | `vc canvas media annotate/grid/outpaint <画布> <节点>` | 图片标注可由笔画文件表达。宫格支持预设和自定义切线。扩图先生成透明底片，再沿用模型和积分规则提交图生图任务。加工产物带来源节点和确认版本。 |
| 视频和音频加工 | `vc canvas media frame/trim/separate-voice <画布> <节点>` | 视频可提取首帧、尾帧和指定时间，视频和音频可按区间修剪。人声分离使用站点当前模型与计价能力。来源和产物随画布同步。 |
| 加工产物挂载 | `vc canvas media stage/attach <画布>` | 暂存绑定加工时的来源节点、素材 ID 和版本，以同一个操作编号确认派生节点和来源连线。 |
| 生成和候选 | `vc generate <类型> --canvas <画布> --node <节点>`、`vc task get/wait/cancel <任务>`、`vc canvas output list/select <画布> <节点>` | 覆盖文本、图片、视频和音频的站点允许能力。支持付费预检、多候选、查询、等待、取消本人有权取消的任务及选择主结果。 |
| 小镜 | `vc agent run/events/approve/cancel <参数>`，补齐已有小镜会话和运行控制 | 会话、历史、实时事件、选择上下文、附件、审批、追加指令、停止、续跑和撤销遵循已有 Agent 权限。后台动作从原协作会话发起。 |
| 评论和提及 | `vc canvas comment list/add <画布>`、`vc canvas member list <画布>` | 读取或发表角色有权访问的评论，评论可定位至节点或画布。提及选择成员 ID，展示用户名。 |
| 活动、版本和撤销 | `vc canvas history list/show/copy <画布>`、`vc canvas undo/redo <画布>` | 活动显示可见的用户名、来源和变更摘要。历史快照和版本副本遵守现有权限，个人撤销和重做处理队友更新与级联依赖。 |
| 成员和交付 | `vc canvas share invite/revoke/leave/copy/transfer <画布>`、`vc canvas member role/grant <画布>`、`vc canvas collaboration enable/disable <画布>` | 更改角色、授予生成或收藏、邀请查看或编辑、退出、发副本、转移和切换协作模式均复用网页契约。退出与转移展示后果并要求确认。 |
| 批量与恢复 | `vc canvas plan/apply/resume <画布>`、`vc canvas op status <画布> <操作>` | 支持校验、一批原子提交、多批独立任务、逐项回报和断线恢复。脚本可以用明确的确认版本和预算上限运行。 |
| 画布成片 | `vc canvas compose <画布>`、`vc task get/wait <任务> --canvas <画布> --epoch E` | 在终端选定素材和顺序、提交任务并导出结果。客户端从授权快照发现 `compose` 能力。首版采用服务端统一编码配置。 |

媒体动作按模型、节点类型、站点配置和角色显示当前可用性。终端说明能力不可用的具体原因以及可以采用的现有入口。全功能发布前逐项核对表中的业务动作，并使用真实测试站点确认依赖服务已可用。

## 权限与副作用

- 查看者可以读取允许的快照、媒体和活动，主动开启 `watch` 或退出共享。内容编辑、生成、画布素材导出、评论和收藏分别沿用网页授权和可见性规则。取得源文件的下载行为也复核当前授权。
- 编辑者可以修改授权画布。生成和收藏独立授权，任务使用发起人额度。任务私有参数、余额、成本明细、任务取消权、运行审批和个人资产信息沿用既有可见性规则。
- 所有者管理协作模式、邀请、角色、生成和收藏授权、版本导出、归档与交付。发送副本和转移后的接收对象进入接收人的个人画布。普通退出成功后，相同账号需要重新获邀才能进入。已经确认交付的副本按原规则独立。
- `plan` 展示动作、目标、前置版本、影响范围及预算。删除节点、修改授权、退出、归档和交付等沿用显式确认，确认后才实际提交。付费脚本使用明确的预算上限和授权范围。服务端复核权限、受影响版本和最终预算。
- 实时版本、任务账单与私有生成输入按现有规则裁剪。日志和机器输出使用可见的用户名和公开标识。设备凭据、长期素材地址和临时签名链接按现有保密边界处理。

## 并发交互

- 客户端保留用户看到的起始版本和预览。独立字段、不同节点以及可合并的分镜等结构子项并行提交。重叠字段的后提交者收到当前值和版本，可以保留自己的草稿、比较并基于新版本应用。
- 删节点、剪切、重连线、移动分组等依赖结构操作按批校验。失败批次的图谱与素材引用均不发生变化。脚本显式声明的其他独立批次可以继续。拆分前展示批间依赖和部分成功口径。
- 断网时可准备本地计划。恢复时核对账号、画布、协作代数和权限，先查未确定结果，再处理本地计划。确认超时与任务失败分别报告。任务取消和停止本地等待是两个独立操作。
- 网页和 CLI 都操作确认图谱。一个人通过 CLI 提交，其他人保留自己的网页视角并接收变化。CLI 普通监听保持无光标的在线状态。需要跟随时主动开启视角订阅。

## 典型流程

下面的命令以已由终端授权的制作人员为例，镜头清单来自团队自己的制作流程。

```bash
vc canvas list --scope shared
vc canvas status C123 --json
vc canvas watch C123 --epoch 1 --since 120 --jsonl
vc canvas plan C123 --input shots.json -o plan.json
vc canvas apply C123 --plan plan.json --json
vc canvas resume C123 --run <运行编号> --json
```

窗口一持续接收协作进度。窗口二批量准备镜头和生成任务。网页端导演同时改另外的镜头并挑选候选。批处理中断后用原运行编号接续，输出逐项列出确认版本、任务编号、冲突和可补跑项目。

## 验收标准

#### AC01 角色和发现

Given 个人画布、共享查看画布和活跃协作画布均存在，三个角色用各自账号登录。

When 列表查询、读图谱、查看媒体和尝试编辑。

Then 入口、内容和权限与网页一致，只有允许的账号可提交操作。个人旧命令和输出继续符合既有契约。

#### AC02 监听与恢复

Given 网页和 CLI 已连接，网络出现重复、乱序、尾部通知丢失或历史缺口。

When 接收增量、重连或者重新开始监听。

Then 各端以连续确认版本收敛，必要时回读快照。私有事件裁剪、权限变化通知和临时在线状态均按角色处理。

#### AC03 字段、结构与批量

Given 多人从共同版本编辑同一节点的独立字段和相同字段，并同时执行删除、连线、分组与结构化子项操作。

When 协作批次跨实例同时提交。

Then 独立改动全部保留，过期或无效的批次整体拒绝，冲突明确指向具体记录或字段。分镜、导演台和角色卡数据可在网页及 CLI 中往返验证。

#### AC04 操作收据和退出恢复

Given 提交发生在请求超时、进程退出以及重启之前。

When 重新读取授权并按保存的操作 ID 查收据或重试相同请求。

Then 只产生一次副作用，收据与最终图谱一致。异载荷、过期、权限不足以及跨会话都返回可识别的结果。

#### AC05 媒体与资产

Given 一段有访问授权的素材可在节点中读取，同时存在并发替换、上传失败和在途撤权。

When 加工、上传、挂载、下载和收藏。

Then 数据来源与输出可追溯，确认后的素材才进入图谱。本地失败可恢复，越权操作被阻止，独立收藏和暂存回收遵循归属规则。

#### AC06 付费任务、候选与成片

Given 生成和相关创作任务包含多候选、定额、价格变化与部分受理。

When 模拟丢响应、重复提交、逆序回填、取消或在途撤权。

Then 服务端按计划版本、身份和预算接受任务，每个付费意图最多一次有效结算。终态和主结果遵守节点绑定，脚本能接着等待已受理的任务。

#### AC07 创作任务与小镜

Given 分镜、角色图与音色、小镜运行都有已确认的画布输入。

When 两个账号并行提交拆镜、组合生成或小镜审批和写回，并在中间关闭 CLI。

Then 进程结束后任务按各自工作流恢复，队友的有效改动保留，换代和权限撤回后的旧写入被拦截。

#### AC08 审阅、历史与交付

Given 存在评论、可见活动、已有任务以及可能过期的操作确认。

When 发评论、查历史、撤销重做、退出、归档、发副本、转移或切换模式。

Then 各动作与网页契约一致。高影响操作的确认绑定目标，提交成功后队友按正确的角色和作品入口读取。

#### AC09 无人值守权限与协议

Given 自动化有针对账号、站点和画布的授权，脚本会读取 `--json` 或 `--jsonl`。

When 请求超出授权范围、能力不可用，或对照旧版 CLI 的输出格式运行命令。

Then 服务端拒绝越界副作用，各种结果以稳定类型与退出码可识别。凭据泄漏和权限混用测试覆盖 stdout、stderr、日志与续接文件。

#### AC10 容量与发布

Given 两个后端实例、约千节点的画布和面向终端的真实 CLI 进程。

When 4 个 CLI 和 8 个 CLI 分别执行纯 CLI 并发测试，另以 4 个真实 CLI 进程和 4 个运行实际画布页面的浏览器会话进行连续 30 分钟的混合负载与故障恢复测试。混合组的 8 个客户端以独立测试账号同时在线，执行普通编辑、批量提交和热点争用，并在故障恢复后继续操作。

Then 校验持续可用性、延迟、限流、公平性、正确性及发布门槛。参数和测量方法见技术设计。

#### AC 逐项测试设计

| AC | 验收重点 | 测试层级 | 目标资产 | 目标命令 | 证据状态 |
| --- | --- | --- | --- | --- | --- |
| AC01 | 列表、授权快照、兼容性 | CLI 进程契约 | `cli/test/collaboration-contract.test.ts` | `cd cli && npm run build && node --test test/collaboration-contract.test.ts` | 本地通过 |
| AC02 | 尾部丢包、越权退订、快照与回放 | CLI 进程契约 | `cli/test/collaboration-contract.test.ts` | `cd cli && npm run build && node --test test/collaboration-contract.test.ts` | 本地通过 |
| AC03 | 并发收敛、结构依赖和批次原子性 | PostgreSQL 集成 | `backend/internal/tests/repository/canvas_collaboration_ops_test.go`、`backend/internal/tests/repository/canvas_collaboration_structure_test.go` | `cd backend && go test -race ./internal/tests/repository -run '^TestCanvasCollaboration(ConcurrentOps|Structure|ConcurrentDuplicateEdge)$'` | 本地通过 |
| AC04 | 超时、恢复与幂等边界 | CLI 进程契约 | `cli/test/collaboration-resume.test.ts` | `cd cli && npm run build && node --test test/collaboration-resume.test.ts` | 本地通过 |
| AC05 | 媒体与归属，本地和对象存储 | CLI 进程契约及 PostgreSQL HTTP 集成 | `cli/test/collaboration-media.test.ts`、`backend/internal/tests/repository/canvas_media_draft_test.go`、`backend/internal/tests/repository/canvas_media_draft_http_test.go` | `cd cli && npm run build && node --test test/collaboration-media.test.ts`，`cd backend && go test -race ./internal/tests/repository -run '^TestCanvasMediaDraft'` | 本地通过 |
| AC06 | 计费、候选、回填、成片 | PostgreSQL 与媒体服务集成 | `backend/internal/tests/service/canvas_collaboration_generation_test.go`、`backend/internal/tests/repository/canvas_compose_test.go` | `cd backend && go test -race ./internal/tests/{repository,service} -run '^(TestCanvasCollaboration(GenerationSettlement|CandidateBatch|CumulativeBudget|BudgetSharedWithSplit)|TestCanvasCompose)'` | 本地通过 |
| AC07 | 拆镜、音色、小镜生命周期 | CLI 进程契约及 PostgreSQL 集成 | `cli/test/collaboration-workflows.test.ts`、`cli/test/collaboration-generation.test.ts`、`backend/internal/tests/repository/canvas_collaboration_special_tasks_test.go` | `cd cli && npm run build && node --test test/collaboration-workflows.test.ts test/collaboration-generation.test.ts`，`cd backend && go test ./internal/tests/repository -run '^TestCanvasCollaborationSplit'` | 本地通过 |
| AC08 | 历史、交付、撤销与管理 | CLI 进程、PostgreSQL HTTP 集成及前端回归 | `cli/test/collaboration-review.test.ts`、`cli/test/collaboration-sharing.test.ts`、`backend/internal/tests/repository/canvas_management_test.go` | `cd cli && npm run build && node --test test/collaboration-review.test.ts test/collaboration-sharing.test.ts`，`cd backend && go test -race ./internal/tests/repository -run '^TestCanvasCollaborationManagement'` | 本地通过 |
| AC09 | 令牌范围、流式输出、错误契约 | CLI 进程及 PostgreSQL HTTP 集成 | `cli/test/collaboration-automation.test.ts`、`cli/test/collaboration-contract.test.ts`、`backend/internal/tests/repository/canvas_automation_key_test.go` | `cd cli && npm run build && node --test test/collaboration-automation.test.ts test/collaboration-contract.test.ts`，`cd backend && go test -race ./internal/tests/repository -run '^TestCanvasAutomationScopeAndRevocation$'` | 本地通过 |
| AC10 | 4、8 个纯 CLI 并发，4 CLI + 4 真实网页持续 30 分钟及故障恢复 | 隔离环境负载与实际浏览器 | `backend/internal/tests/repository/cli_collaboration_load_test.go`、`backend/internal/tests/repository/canvas_mixed_load_test.go` | `cd backend && TEST_CLI_COLLABORATION_LOAD=1 go test ./internal/tests/repository -run '^TestRealCLICollaborationConcurrent$' -v`，混合组按技术设计执行 | 本地通过 |

## 交付顺序

1. 统一能力发现、账户权限、共享列表、快照与 `watch`，完成小规模的网页与 CLI 双向读写。
2. 贯通结构化编辑、计划、原子提交、脚本恢复、撤销与重做，验证 4 和 8 个 CLI 账户。
3. 贯通媒体、资产、付费生成、成片、分镜、角色音色和小镜，完成一次全程不依赖网页编辑的创作交付。
4. 贯通评论、活动、权限管理、共享退出、交付与归档，完成混合容量、三平台和回滚验证。内测可分阶段开放，全功能发布以功能对照和 AC 全部通过为门槛。

## 实现与验证

### 验证记录

- 2026-11-28，macOS / Node 22，`cd cli && npm run format:check && npm run lint && npm run typecheck && npm test && npm run build`，134 项 CLI 测试通过。覆盖共享发现、快照、监听缺口、节点冲突、收据恢复、生成续接、素材及候选接口、审阅与交付确认。CLI 使用本地 HTTP 模拟站点运行进程契约测试。
- 2026-11-28，本地独立 PostgreSQL 测试库和 Docker Redis，`cd backend && go test ./internal/tests/repository ./internal/tests/service -run '^(TestRealCLICollaborationConcurrent|TestCanvasCollaboration)' -count=1`，启用 `TEST_CLI_COLLABORATION_LOAD=1`。收据访问隔离、撤销目标并发检查、生成积分上限与账务、4 人及 8 人 CLI 同时写入和监听全部通过。对 1,000 节点的长提示词样本，双实例并行提交与单监听进程收敛分别用时 340 ms 和 556 ms，包含进程启动，样本用于收敛冒烟验收。S3 专用环境和其他容量夹具按环境开关跳过。
- 2026-11-28，macOS / Node 22 本地进程测试，`cd cli && npm run build && node --test test/collaboration-resume.test.ts test/collaboration-workflows.test.ts test/collaboration-generation.test.ts test/collaboration-content.test.ts`。覆盖多批部分成功、依赖恢复、镜头稳定 ID、拆分镜确认和任务恢复、角色音色绑定、授权下载及拒绝覆盖文件。
- 2026-11-28，独立 PostgreSQL 测试库和 Docker Redis，`cd backend && go test ./internal/tests/repository ./internal/tests/service ./internal/tests/migration -run '^(TestCanvasShareHTTPScopedMedia|TestCanvasCollaborationSplit|TestStoryboardSplit_|TestMigrationsMatchModels|TestCanvasCollaborationMigration|TestRealCLICollaborationConcurrent)' -count=1`。三组通过，覆盖撤权后拒绝下载、拆分镜积分和幂等、覆盖与并行修改、迁移结构一致性、4 人与 8 人 CLI 回放收敛。媒体 HTTP 测试模拟存储服务。
- 2026-11-28，独立 PostgreSQL 测试库和 Docker Redis。构建 CLI 后运行 `go test ./internal/tests/repository -run '^TestRealCLICollaborationConcurrent$' -count=1`，4 与 8 个真实写进程及独立监听进程均收敛。此次 1,000 节点样本的总耗时为 287 和 497 ms，包含进程启动。跟随用例从另一实例收到终端心跳，并收到协议客户端发送的用户名、视角及选区。
- 2026-11-28，CLI 进程测试覆盖协议能力与批次上限、节点移动、连线、分组、角色组合生成响应丢失后的续接。Go WS 竞态检查及前端定向测试覆盖角色授权、在线名单与网页视角独立过期。
- 2026-11-28，真实 FFmpeg 媒体夹具验证从授权视频提取尾帧和区间修剪，并验证源版本变化时清理临时产物。结构化节点进程测试确认角色卡、导演台和分镜表整字段覆盖受提交版本保护。
- 2026-11-28，双实例 PostgreSQL / Redis 协议压测 `TEST_CANVAS_COLLABORATION_LOAD=1 go test ./internal/tests/repository -run '^TestCanvasCollaborationLoad$' -count=1`。千节点长提示词样本、10 个协议客户端和模拟 80 ms RTT 下，拖动、连线、分组、提示词和 50 条批次各有 108 个接收样本，P95 约 241、209、248、221、241 ms。普通传播 P95 约 204 ms，包含采样等待约 305 ms，100 批追赶约 269 ms。
- 2026-11-28，CLI 进程契约和真实 PostgreSQL HTTP 集成验证评论独立回放、幂等去重、跨页游标、具名作者与降级拒绝。`go test ./internal/tests/repository ./internal/tests/migration -run '^(TestCanvasCollaborationHTTP|TestCanvasCollaborationSnapshotReviewCapabilities|TestMigrationsMatchModels)$' -count=1` 通过。
- 2026-11-28，`TEST_CLI_COLLABORATION_LOAD=1 go test ./internal/tests/repository -run '^TestRealCLICollaborationConcurrent$' -v -count=1` 在 PostgreSQL / Redis 双实例通过 4 个子用例，覆盖 4 和 8 个 CLI 写入进程与独立观察者、在线跟随、跨实例评论写入及授权回放。本次写入收敛总耗时分别为 286 ms 与 1.546 s，含进程启动及与本地综合检查并行的资源竞争。
- 2026-11-28，跨实例 PostgreSQL / Redis 竞态测试验证编辑成员收到评论唤醒。PostgreSQL HTTP 测试验证查看者无法回放评论正文。真实 `vc` 监听从 Redis 协议夹具收到已确认运行的授权进度。CLI 进度单测覆盖旧任务、乱序样本、到期和清理，以及终态优先于临时进度。
- 2026-11-28，真实 FFmpeg 进程测试覆盖自定义切线、奇数尺寸图片的准确切分、拒绝覆盖已有输出及源版本竞争时清理本地临时目录。
- 2026-11-28，PostgreSQL 回归 `go test ./internal/tests/repository -run '^TestCanvasCollaboration(ShotConflicts|Structured|Split)' -count=1` 覆盖网页整表编辑后，CLI 从原版本修改不同镜头或同一镜头的不同字段。相同字段、过期删除与顺序冲突被拒绝。CLI 进程测试验证原观察版本、镜头操作载荷和小镜会话历史、附件、追加指令、续跑及撤销接口。
- 2026-11-28，`cd cli && npm run build && node --test test/collaboration-media.test.ts` 使用真实 FFmpeg 核对笔画输出像素、拒绝无效笔画文件和覆盖请求，验证加工期间版本变化时清理临时输出。
- 2026-11-28，PostgreSQL `TestCanvasCollaborationDirectorObjectConflicts` 验证网页改道具名称后，CLI 可从观察版本改同一对象的坐标及其他对象。过期同属性编辑与删除返回冲突。`cli/test/collaboration-workflows.test.ts` 验证 `director scene apply` 保留观察版本和对象操作载荷。
- 2026-11-28，PostgreSQL 和 Redis 双实例 `TestRealCLICollaborationConcurrent/same-storyboard` 验证 8 个真实 CLI 从同一版本提交同一分镜表的不同对白，独立监听进程收到最终版本，快照保留 8 条编辑。
- 2026-11-28，`cli/test/collaboration-content.test.ts` 验证协作 HTTP 读取保留超过 JavaScript 安全整数范围的账号和素材 ID，收藏请求向现有后端发送原始整数。
- 2026-11-28，PostgreSQL 的 `TestCanvasCollaborationStructuredPersonalUndo` 与 `TestCanvasCollaborationDirectorPersonalUndo` 验证按属性执行个人撤销，保留队友修改，重做只恢复实际撤销的分镜字段。
- 2026-11-28，CLI 契约测试和 PostgreSQL 加本地对象存储的 `TestCanvasMediaDraft*` 验证版本绑定、暂存与挂载、同编号重试、异内容拒绝、撤权、源替换和回收失败后重试。SQL 迁移与 GORM 模型一致。
- 2026-11-28，`cli/test/collaboration-tasks.test.ts` 和 PostgreSQL `TestCanvasCollaborationTaskSummaries` 验证编辑成员可读拆分镜与角色音色状态，查看者和已撤权成员访问被拒绝。列表和监听事件使用同一个裁剪接口。
- 2026-11-28，`cli/test/collaboration-compose.test.ts` 验证有序提交、等待和取消契约。`TestCanvasComposeRender` 经授权 HTTP 接口、PostgreSQL、River 和真实 FFmpeg 执行双片段成片，按时间点验证顺序，检查派生节点、素材和来源连线。定向测试亦验证重复提交、写回前取消、来源删除和撤权。
- 2026-11-28，`TestCanvasComposeSubmission` 验证成员在途成片阻止交付，取消后解除限制。`TestCanvasMediaDraftS3` 和 `TestCanvasCollaborationGenerationS3` 通过 SeaweedFS 的实际 S3 接口验证素材挂载、逐字节读取、权限回收和过期清理。复测使用隔离桶并设置 `TEST_CANVAS_COLLABORATION_S3_ENDPOINT` 和 `TEST_DATABASE_DSN`。
- 2026-11-28，`TestCanvasAutomationScopeAndRevocation` 验证凭据绑定画布、可编辑范围、操作归属、禁用高影响操作、主动吊销、移除后重邀、凭据过期及协作换代。CLI 进程测试验证跨站隔离、禁止重定向及普通登录态隔离。
- 2026-11-28，`TestCanvasCollaborationCumulativeBudget` 验证同组并发受理、原请求重试、上限不可变、取消释放与实际结算。`TestCanvasCollaborationBudgetSharedWithSplit` 验证拆分镜和普通生成共用余额。CLI 契约验证旧站点明确拒绝累计预算请求且不提交写入。
- 2026-11-28，CLI 进程契约跨越 JavaScript 安全整数上限，验证精确的快照、计划文件、提交请求、收据以及连续回放。新的 `watch --jsonl` 外层继续输出十进制字符串，旧接口的安全整数输出保留原有类型。
- 2026-11-28，`TestRealCLICollaborationConcurrent/progress-large-version` 通过两个 Go 实例、Redis 及真实 CLI 进程验证大代数的在线心跳和生成进度，包含共享快照中的任务绑定。
- 2026-11-28，归档与重命名通过 CLI 进程契约、PostgreSQL 仓储与 HTTP 集成验证。归档预览显示受影响的成员人数并绑定画布版本与成员身份和角色。取消归档无写入。预览过期或有在途任务时服务端拒绝归档。成功后所有者和成员的原画布访问均失效。
- 2026-11-28，`cd web && bun test src/tests/pages/canvas/collaboration.test.tsx src/tests/pages/canvas/shared-reader.test.tsx` 覆盖重命名后网页编辑者恢复本地草稿、仅查看者开启实时订阅后同步标题，以及关闭订阅后的访问权限检查。
- 2026-11-28，`TEST_CLI_COLLABORATION_LOAD=1 go test -race ./internal/tests/repository -run '^TestRealCLICollaborationConcurrent$/^rename$' -count=1` 验证真实 `vc canvas rename` 经过第一个 Go 实例提交，第二个实例收到通知后可回放新标题和连续版本。
- 2026-11-28，`TEST_CANVAS_COLLABORATION_LOAD=1 go test ./internal/tests/repository -run '^TestCanvasCollaborationLoad$' -count=1 -v` 通过双 Go 实例、PostgreSQL 和 Redis 的短时协议夹具。10 个协议客户端在千节点画布模拟 80 ms RTT，拖动、连线、分组、提示词编辑及 50 节点批次各有 108 个接收样本，P95 为 211–253 ms。另有 180 个常规传播样本，提交到接收 P95 为 220 ms，计入编辑采样等待后为 323 ms。5 轮断线追赶 100 个批次的 P95 为 264 ms。
- 2026-11-28，`TestVocalSeparationInput`、`TestVocalSeparationRequiresTwoDistinctAudioStems` 和 `TestCanvasVocalSeparationBindsSourceAndPreservesHistory` 验证单一输入、源素材绑定、双音轨校验和草稿历史保留。`TestCanvasAutomationEnforcesIssuedBudget` 经 PostgreSQL 和实际 HTTP 路由验证固定预算、并发受理、幂等和任务读取隔离。`TestMigrationsMatchModels` 验证预算列和数据库约束与迁移一致。
- 2026-11-28，网页及小镜覆盖相邻版本 `9007199254740992` 与 `9007199254740993` 的精确读取、提交和持久化。网页请求保留用户自定义字段内的同名字符串。`TestCanvasMixedLargeVersions` 使用真实网页和 CLI 在超过安全整数上限的协作会话中继续写入，最终确认版本为 `9007199254741064`。四端格式、静态检查、构建和测试通过。
- 2026-11-28，4 与 8 个纯 CLI 写入进程均通过双实例并发和收敛测试。在线状态跟随在 Redis 订阅就绪后连续通过三次。Redis 降级、重放恢复、撤权先于排队写入在独立的 PostgreSQL 和 Redis 集成测试中通过。`TestRealCLICollaborationConcurrent/lost-response-failover` 模拟提交成功后的响应丢失和第一实例停服，验证真实 CLI 经第二实例读到原收据，保持单次提交。
- 2026-11-28，4 个真实 `vc` 客户端和 4 个真实 Chrome 会话在约千节点长提示词画布上持续编辑 30 分钟。每个网页提交 600 次改名，断网后恢复编辑并收敛，最终确认版本为 4802。扩展夹具的 25 秒短测还覆盖 16 节点批次、热点冲突、已提交后的响应丢失和八端收敛。
- 2026-11-28，混合组加入 16 节点批次与并发热点后再次连续编辑 30 分钟。每个网页提交 598 次改名，断网恢复后收敛至版本 5010，记录 250 次热点冲突。网页提交到远端渲染的 7164 个样本 P95 为 1684 ms，超过 300 ms 的发布门槛。本轮与另一个压测共享宿主机。短测的 74 条版本通知到达每个网页，写请求 P95 为 637 ms，回放请求 P95 为 210 ms。
- 2026-11-28，单独运行 `TestCanvasMixedRealClients`，双实例上的 4 个 CLI 客户端与 4 个真实 Chrome 会话连续编辑 30 分钟 1 秒。每个客户端完成 600 轮，CLI 每 5 轮提交一次 16 节点批次，共观测到 359 次热点冲突。网页断网和已提交后的响应丢失均在持续负载中恢复。四个 CLI 读到同一确认版本 4923，四个网页显示服务端确认的最终节点名称。网页提交到远端渲染的 7176 个样本 P95 为 291 ms，达到本场景 300 ms 的目标。网页增量请求和写请求的 P95 分别为 91 ms 和 173 ms。
- 2026-11-28，混合夹具增加图谱类型选择和请求、通知载荷、PostgreSQL WAL 指标。`TEST_CANVAS_MIXED_LAYOUT=long-prompt TEST_CANVAS_MIXED_SMOKE=1` 的 4 CLI + 4 Chrome 烟测通过，84 个网页远端渲染样本 P95 为 237 ms，GET/POST 协作请求 P95 为 90/115 ms，通知载荷 59764 B，PostgreSQL WAL 增量 510384 B，断网与提交后响应丢失均恢复。
- 2026-11-28，独立资源环境完成 `TEST_CANVAS_MIXED_LAYOUT=grouped` 的 4 CLI + 4 Chrome 连续 30 分钟混合负载。每个网页提交 600 次，359 次热点冲突，网页断网和提交响应丢失恢复，最终版本 4923。7,176 个远端网页渲染样本 P95 为 263 ms，GET/POST 请求 P95 为 89/142 ms，WAL 增量 22,871,112 B，达到混载发布门槛。
- 2026-11-28，`TEST_CANVAS_MIXED_LAYOUT=grouped TEST_CANVAS_MIXED_SMOKE=1` 在 1,010 个节点和 2,870 条连线的图谱上通过 4 CLI + 4 Chrome 烟测。84 个网页远端渲染样本 P95 为 326 ms，GET/POST 协作请求 P95 为 89/166 ms，通知载荷 59764 B，PostgreSQL WAL 增量 320592 B，结构化图谱、断网和提交后响应丢失均收敛。该结果超过 300 ms 目标，需在独立资源环境继续复测并定位连线图谱下的额外渲染耗时。
- 2026-11-28，模式切换新增服务端成员影响预览。网页和 CLI 先读取同一版本、成员数与不可伪造摘要，提交时复核摘要，成员授权或版本变化会拒绝旧确认并要求重新预览。PostgreSQL、HTTP、网页组件和 CLI 契约回归通过，保留无预览字段的旧 HTTP 调用兼容。
- 2026-11-28，快照和增量回放将成员角色、生成授权与资产库授权合并为一次授权读取。新增 SQL 查询计数回归，编辑成员的快照和回放均只读取一次 `canvas_viewers`。
- 2026-11-28，网页复用未变的乐观图谱与连线数组，无可见变更的对账只推进确认版本。`VITE_API_BASE_URL=/api/v1 bash scripts/check.sh web` 的 5,465 项测试及格式、类型、lint、构建检查通过。以相同构建配置复测 `TEST_CANVAS_MIXED_LAYOUT=grouped TEST_CANVAS_MIXED_SMOKE=1`，4 CLI + 4 Chrome 在 1,010 节点和 2,870 条连线下连续编辑 27 秒，网页断网和已提交响应丢失后收敛至版本 76。84 个远端渲染样本的 P95 为 361 ms，GET/POST 请求 P95 为 91/198 ms。此前同机器同场景短测的远端渲染 P95 为 366 ms，两次短测不足以确认稳定提速。分组图谱仍超过 300 ms 发布目标，需分别测量服务端提交、增量到达和网页应用的耗时。
- 2026-11-28，分组短测的 326 ms 结果已由独立 30 分钟混载中的 263 ms 复测覆盖。仍保留十会话协议矩阵重复复测中的长提示词尾延迟，作为发布前稳定性工作项。
- 2026-11-28，协作历史检查点改为数据库内 JSONB 聚合并增加活跃记录顺序索引，协议压测夹具改为通知到达即并行回放。`TestCanvasCollaborationConcurrentLoad` 连续两轮覆盖四种布局和分散 / 热点两种竞争，最高 P95 为 266 ms，十会话本地性能阻断关闭。服务重启、队列恢复和正式候选发布证据仍待补齐。

### 尚待验收的范围

- 人声分离已接通模型契约、网页音频工具条和 CLI。真实供应商上线验收需要在测试站点配置一个符合契约的模型。
- 分镜和导演台普通属性支持细粒度撤销。增删、重排或有引用依赖的编辑仍按整个结构化字段撤销。角色组合任务链路及小镜权限与运行端到端场景待验收。
- 归档、共享授权和模式切换已绑定服务端影响预览，交付仍需按服务端契约逐项核对。
- 发布仍需核对其他图谱及操作类型的 P95、混合场景下的服务重启、队列与 WAL 指标，以及三平台安装和真实媒体工作流。

[协作发布指标](产品方案.md#发布指标) · [当前 CLI 契约](../../../cli/README.md)
