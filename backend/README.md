# video-canvas backend

Gin + GORM + go-redis + Viper + Zap 的 Go 后端骨架。

## 目录结构

```
backend/
├── cmd/server/          # 程序入口
├── configs/             # 配置文件
├── migrations/          # 版本化数据库迁移 SQL（内嵌进二进制）
├── internal/            # 项目内部代码（核心）
│   ├── config/          # 配置定义
│   ├── model/           # 数据模型
│   ├── repository/      # 数据访问层
│   ├── service/         # 业务逻辑层
│   ├── handler/         # HTTP 处理层
│   ├── router/          # 路由注册
│   ├── middleware/      # 中间件
│   ├── provider/        # 外部供应商调用与任务调度：worker（任务“这一步”的执行逻辑）/ queue（基于 River 的任务队列：一个任务一个 job，调度、并发、救回）/ plugin（协议插件宿主）
│   ├── pkg/ws/          # 用户级 WebSocket：Hub / ticket
│   ├── storage/         # 素材存储：内置本地磁盘 + S3 兼容对象存储（OSS / COS / S3 / R2），Registry 按 id 解析
│   ├── cache/           # Redis 缓存
│   ├── initialize/      # 初始化
│   ├── migration/       # 迁移执行器 + 老库补齐（启动时自动执行）
│   └── pkg/             # 内部通用工具包
│       └── # response / errcode / logger / utils
├── pkg/                 # 可被外部引用的公共包（version / pagination）
└── go.mod / go.sum
```

请求链路：`router → middleware → handler → service → repository / cache`。
依赖在 `internal/initialize/app.go` 里手动组装，新增模块时照着 user 模块加一套即可。

## 快速开始

```bash
go run ./cmd/server -c configs/config.yaml
# 或者
make run
```

数据库仅支持 PostgreSQL，启动前需准备好数据库并在 `database.dsn` 中填写连接信息。启动时会自动执行 `migrations/` 里还没执行过的迁移（`database.auto_migrate`，默认开），迁移失败则不启动；`go run ./cmd/server migrate status` 查看迁移状态。写迁移的规则见 [AGENTS.md](AGENTS.md)「数据库迁移」。

- 启用 Redis：`redis.enabled` 改为 `true`
- 环境变量覆盖：`APP_` + 配置路径，如 `APP_SERVER_PORT=9000`
- 登录记录保留天数固定为 180 天（`service.DefaultLoginLogRetentionDays`，不再可配置）：后台任务在启动后 1~5 分钟内先清理一次，之后每 24 小时一次，每批最多删 1000 行；失败只记日志，不影响服务
- 并发上限与新用户初始积分不在配置文件里：由后台「注册设置」的库值决定，库里没有值时回落到代码常量 `service.DefaultMaxActiveTasks`（4）与 `service.DefaultInitialCredits`（50）

## 接口

所有登录后的接口都经过 `JWTAuth` → `RequireActive`：账号被停用返回 403 / 53004，token 的 `token_version` 与用户当前值不符返回 401。用户状态走 Redis 缓存（`user:<id>`，无 Redis 直接查库），`RequireAdmin` / `RequireSuperAdmin` 的角色查询与它共用同一套。原先的 `GET /users`、`GET /users/:id` 已删除。

| 方法                | 路径                                                  | 说明                                                                                                                                                                                                                                                                                                                                                                               |
| ------------------- | ----------------------------------------------------- | ---------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| GET                 | /health                                               | 健康检查（数据库、Redis、版本信息）                                                                                                                                                                                                                                                                                                                                                |
| GET                 | /api/v1/auth/config                                   | 注册配置 `{register_enabled, email_verify_required}`：开放 = 后台「开放注册」开关打开；要验证码 = SMTP 已启用且 users 表非空（首个账号免验证）；没启用邮件服务时邮箱仍必填但不验证                                                                                                                                                                                                                       |
| POST                | /api/v1/auth/register/code                            | 发注册验证码 `{email}`：6 位数字、10 分钟有效、错 5 次作废；同邮箱 60 秒冷却、同 IP 每小时 20 次（429）；注册关闭 / 无 SMTP 403 / 53001，邮箱已注册 409 / 53002。验证码存 Redis（未启用时进程内存），仅 debug 模式打到日志                                                                                                                                                         |
| POST                | /api/v1/auth/register                                 | 注册即登录 `{username, email, password, code}` → `{token, expire_at, role}`；密码按统一规则（8..72 字节、不等于用户名、不在弱密码表，否则 10001 / 55003，登录仍接受 6 位）；users 表为空时首个账号成为 `super_admin`（注册事务内加咨询锁，并发安全），事务内建积分账户并写 `initial` 流水；错误 53001 / 20002 / 53002 / 53003                                                                                                                                                     |
| POST                | /api/v1/auth/login                                    | 登录 → `{token, expire_at, role}`；账号停用 403 / 53004；成功更新 `last_login_at`，成功 / 密码错 / 停用都写登录记录；token 带 `token_version`                                                                                                                                                                                                                                      |
| GET                 | /api/v1/showcase                                      | 登录页背景轮播（**无需登录**）→ `{settings:{clip_seconds, show_on_login, poster_only_on_save_data}, items:[{id, video_url, poster_url, prompt, model_label, start_sec, width, height, byte_size}]}`：只返回已启用条目，按 `sort`、`id` 升序，视频素材已被删的条目跳过，`show_on_login=false` 时 `items` 为 `[]`；URL 复用素材稳定地址 `/files/<key>`，`poster_url` 无封面为空串；不暴露素材 ID、创建人等内部字段；带 `Cache-Control: public, max-age=60` |
| GET                 | /api/v1/admin/users?q=&status=&role=&page=&page_size= | admin 用户列表（`q` 模糊匹配用户名 / 邮箱，`page_size` ≤ 100，含积分、进行中任务数、实际生效并发上限）                                                                                                                                                                                                                                                                             |
| GET                 | /api/v1/admin/users/:id                               | admin 用户详情：列表字段 + `email_verified_at`、任务统计、最近 3 条审计                                                                                                                                                                                                                                                                                                            |
| GET                 | /api/v1/admin/users/:id/tasks?status=&cursor=&limit=  | admin 用户生成记录（排除试跑）；`status` 为 all / success / failed（含 expired、canceled）/ running；游标分页 `{items, next_cursor}`，`next_cursor` 为空串表示没有更多，`limit` 默认 20、最大 100                                                                                                                                                                                  |
| GET                 | /api/v1/admin/users/:id/ledger?type=&cursor=&limit=   | admin 用户积分流水（带 `operator_name`）；`type` 为 all / admin（仅 admin_adjust）/ task（freeze、settle、refund，不含 initial）；游标分页同上；`amount` 是带符号的「对可用积分的影响」（仅响应层映射，库里的值不变，对账仍用原值）：freeze / settle 为负，refund / initial 为正，admin_adjust 为原值                                                                              |
| GET                 | /api/v1/admin/users/:id/logins?result=&cursor=&limit= | admin 用户登录记录；`result=fail` 只看 badpw / blocked；游标分页同上                                                                                                                                                                                                                                                                                                               |
| POST                | /api/v1/admin/users/:id/credits                       | admin 调整积分 `{mode: add\|sub\|set, amount, note(1..100)}`，返回 `{balance, frozen, available}`；set 把**可用积分**设为 amount（余额 = amount + 冻结）；sub 超出可用返回 400 / 53005（文案含「最多可扣 N」）；与任务冻结 / 结算共用 `LockCredit` 行锁，写 `admin_adjust` 流水与审计                                                                                              |
| PUT                 | /api/v1/admin/users/:id/limits                        | admin 设置并发上限 `{max_active_tasks: 1..64 \| null}`，清用户缓存，写审计                                                                                                                                                                                                                                                                                                         |
| PUT                 | /api/v1/admin/users/:id/status                        | admin 封禁 / 启用 `{status, cancel_active?}`，返回 `{status, canceled, cancel_failed, failed_task_ids, cancel_error?}`；封禁时清缓存、断开该用户全部 WebSocket，`cancel_active=true` 时取消进行中任务并退还冻结（逐个处理，部分失败不回滚）                                                                                                                                        |
| PUT                 | /api/v1/admin/users/:id/role                          | **super_admin** 调整角色 `{role: user\|admin\|super_admin}`；不能改自己（403 / 10004「不能修改自己的角色」）、不能降级最后一个 super_admin（403 / 10004，事务内咨询锁 + 统计，并发安全）；角色没变化直接成功不写审计；写 `user.role` 审计 `{from, to}`，清用户缓存让新角色立即生效                                                                                                 |
| POST                | /api/v1/admin/users/:id/reset-password                | **super_admin** 重置密码 `{new_password?}`（手填时按统一密码规则：8..72 字节、不等于用户名、不在弱密码表，否则 10001 / 55003）（可省略整个 body；可重置自己的）；缺省生成 16 位强随机临时密码（crypto/rand，字母数字、去掉易混淆字符）；返回 `{temp_password}`，**仅此一次**（指定了 `new_password` 时返回同一值）；bcrypt 存储，`token_version` +1 并清缓存，该用户已签发的 token 立即 401；审计 `user.reset_password` 只记 `{generated}`，日志与审计不含明文 |
| POST                | /api/v1/admin/users/batch/credits                     | admin 批量发积分 `{ids(≤200，去重), amount>0, note}`，返回 `{results:[{id, ok, error?}]}`，每个用户独立权限判断与事务                                                                                                                                                                                                                                                              |
| PUT                 | /api/v1/admin/users/batch/status                      | admin 批量封禁 / 启用 `{ids(≤200，去重), status}`（不取消任务），返回同上                                                                                                                                                                                                                                                                                                          |
| GET                 | /api/v1/admin/system/version                          | admin 当前版本 `{version, git_commit, build_time, go_version, latest_migration}`                                                                                                                                                                                                                                                                           |
| GET                 | /api/v1/admin/system/updates                          | admin 检查新版本（GitHub Releases 最新正式版，缓存 1 小时；`?force=1` 强制，30 秒内复用）；失败也返回 200，原因在 `warning`；`update.check_enabled=false` 时 `enabled=false`                                                                                                                                                                                                                |
| GET                 | /api/v1/admin/system/migrations                       | admin 每个数据库迁移文件的执行状态 `[{filename, applied, applied_at, app_version, modified, unknown}]`                                                                                                                                                                                                                                                               |
| GET                 | /api/v1/admin/system/upgrade                          | admin 一键升级面板 `{available, unavailable_reason, image_tag, uses_latest, running, last, backups, log_tail, active_tasks, rollback_target, current_migrations}`；updater 未启用或连不上不算错误                                                                                                                                                                        |
| POST                | /api/v1/admin/system/upgrade                          | super_admin 一键升级 `{target}`：只能是检查到的最新版本；updater 执行 update.sh，立即返回 202；正在升级 409 / 63002，用 latest 409 / 63004，审计 `system.upgrade`                                                                                                                                                                                                                        |
| POST                | /api/v1/admin/system/rollback                         | super_admin 回退 `{target}`：只能是最近一次一键升级之前的版本（`rollback_target`），数据库迁移不撤销；审计 `system.rollback`                                                                                                                                                                                                                                                            |
| GET                 | /api/v1/admin/settings/register                       | admin 注册设置 `{register_enabled, initial_credits, default_max_active_tasks, queue_multiplier, queue_max_wait_minutes}`（库值优先，缺省为代码常量：初始积分 50、并发上限 4、排队倍数 5、排队超时 60 分钟）；排队上限 = 用户并发上限 × `queue_multiplier`，超过才拒绝新任务（429 / 40002） |
| PUT                 | /api/v1/admin/settings/register                       | super_admin 保存注册设置，写审计                                                                                                                                                                                                                                                                                                                                                   |
| GET                 | /api/v1/admin/settings/smtp                           | admin 邮件服务配置，**密码永不返回**，只给 `has_password`                                                                                                                                                                                                                                                                                                                          |
| PUT                 | /api/v1/admin/settings/smtp                           | super_admin 保存邮件配置（不含密码）；主机不能指向内网 / 回环 / 链路本地（含 DNS 解析后的 IP），否则 400 / 53006                                                                                                                                                                                                                                                                   |
| PUT                 | /api/v1/admin/settings/smtp/password                  | super_admin 设置 SMTP 密码（只写）；用 `APP_AI_SECRET_KEY` 加密，缺密钥 400 / 53006                                                                                                                                                                                                                                                                                                |
| POST                | /api/v1/admin/settings/smtp/test                      | super_admin 发测试邮件 `{to}`，结果写入 `last_check_*`；失败 502 / 53007（文案已脱敏）                                                                                                                                                                                                                                                                                             |
| GET                 | /api/v1/admin/settings/showcase                       | admin 登录页展示：`{settings, items}`，items 含禁用条目，每项 `{id, asset_id, poster_asset_id(无则 null), video_url, poster_url, prompt, model_label, start_sec, enabled, width, height, byte_size, duration_ms, file_name, created_at}`；视频素材已被删的条目仍列出（视频地址为空串），方便删除 |
| GET                 | /api/v1/admin/settings/showcase/library               | admin 素材库（后台“从素材库添加”）：query `page`（默认 1）、`page_size`（默认 48，最大 100，小于 1 回落默认、超过 100 收敛为 100）→ `{items:[{asset_id, video_url, prompt, model_label, owner, created_at, width, height, byte_size, duration_ms, added}], total, page, page_size}`：只列全平台 `kind=video` 且 `source=generated` 的素材，按 `created_at`、`id` 倒序；`prompt`（≤80 字）与 `model_label`（≤40 字，取任务快照里的模型展示名，退回模型 key）取自素材的 `task_id` 对应任务，取不到为空串；`owner` 是作者用户名（取不到为 `用户#<id>`）；`added` 表示已被未删除条目引用；`page` 不是数字 400 / 10001 |
| POST                | /api/v1/admin/settings/showcase/items                 | super_admin 新增条目 `{asset_id, poster_asset_id?, prompt(去空白后 1..80 字), model_label(≤40), start_sec(0..3600), enabled(缺省 true)}`；视频必须是 video 素材，且是**自己的**（任何来源）或**平台生成的**（`source=generated`，任何用户的，供“从素材库添加”）；封面必须是自己的 image 素材（不可见的按不存在，404 / 40007，类型不对 400 / 54002）；`sort` = 当前最大值 + 1；写审计 |
| PUT                 | /api/v1/admin/settings/showcase/items/:id             | super_admin 部分更新 `{asset_id?, prompt?, model_label?, start_sec?, enabled?, poster_asset_id?}`：`asset_id` 没传 = 不改，传了 = 替换视频（规则同新增，`sort` / `enabled` 不变，不自动改封面，需要时同时传 `poster_asset_id`）；`poster_asset_id` 没传 = 不改，显式 `null` = 清空封面，数字 = 换封面；一个字段都没传 400 / 54002，条目不存在 404 / 54001；写审计 |
| DELETE              | /api/v1/admin/settings/showcase/items/:id             | super_admin 删除条目（只删条目，不删素材）；不存在 404 / 54001；写审计 |
| PUT                 | /api/v1/admin/settings/showcase/order                 | super_admin 重排 `{ids:[…]}`：必须恰好包含全部现有条目各一次（多、少、重复都 400 / 54003），在一个事务里按数组顺序把 `sort` 重写为 0..n-1；写审计 |
| PUT                 | /api/v1/admin/settings/showcase/settings              | super_admin 保存 `{clip_seconds(4..15), show_on_login, poster_only_on_save_data}`（三项必填，秒数越界 400 / 54002），`system_settings` 里 `showcase_*` 三个键一次写入，返回最新设置；库值缺省或非法时回落默认（7 秒 / 展示 / 省流量只显示封面）；写审计 |
| GET/POST/PUT/DELETE | /api/v1/canvas[/:id]                                  | 画布项目（乐观锁 revision）；列表项带 `cover_url`：画布里第一张生成成功的图的缩略图地址，没有则为空串                                                                                                                                                                                                                                                                                                                                                         |
| GET                 | /api/v1/canvas/:id/export                             | 导出画布为文档：`{document, report:{skipped_assets}}`；素材 ID 换成内容哈希、角色卡内联，读不到的素材跳过并计入 `skipped_assets`；不存在或不属于自己 404 / 30001，超出导入上限（节点 2000、素材 200 个、素材总大小 1 GB、文件 10 MB）413 / 30008 |
| POST                | /api/v1/canvas/import                                 | 从导出文档导入为新画布 `{document}` → `{canvas:{id,title,revision},report:{assets:{total,copied,missing},characters:{created,renamed,invalid},edges_dropped}}`，标题加「（导入）」；文件不合法 400 / 30005，版本过高 400 / 30006，请求体或文件超限 413 / 30007（文件上限 10 MB，请求体上限 11 MB） |
| GET/POST            | /api/v1/canvas/:id/agent/sessions                     | 画布 Agent 会话列表 / 新建 `{title?, mode?: all\|script\|storyboard\|prompt, model_key?}`；每个画布最多 50 个会话（60010）；id 与画布一样是十六进制串，其余 Agent 接口同。**这一组 Agent 接口由独立的 Agent 服务实现**，Go 校验登录态后反向代理（路径、方法、响应信封不变）；没启用 Agent 时统一返回 503 / 60005 |
| PATCH/DELETE        | /api/v1/agent/sessions/:sid                           | 重命名 `{title ≤40}` / 删除（会话占着画布时 409 / 60001）；会话不存在或不属于自己 404 / 60011 |
| GET                 | /api/v1/agent/sessions/:sid/events?after=&limit=      | 回放事件（`seq > after`，limit ≤200），断线重连后对账用；实时推送是 WebSocket 的 `agent.event`，走用户频道，消息里带 `canvas_id` 供前端过滤 |
| POST                | /api/v1/agent/sessions/:sid/runs                      | 发起一轮运行 `{message, mode?, selection?, viewport?, budget_credits?, agent_model_key?}` → **202**；预算默认 50（0 合法）；同一画布同时只能有一个活跃运行（409 / 60001）；没有已发布的 Agent 模型 400 / 60002；运行时不可用 503 / 60005 |
| POST                | /api/v1/agent/runs/:rid/interject                     | 运行中插话 `{message}`，仅 running（409 / 60003） |
| POST                | /api/v1/agent/runs/:rid/cancel                        | 停止运行：已完成的画布改动保留，待处理的审批失效；已结束 409 / 60003 |
| POST                | /api/v1/agent/runs/:rid/resume                        | 继续 interrupted / budget_exhausted / step_limit 的运行 `{add_budget?}`；预算用尽必须追加（400 / 10001） |
| POST                | /api/v1/agent/runs/:rid/undo                          | 撤销本轮对画布的改动 → `{reverted, skipped:[{kind,node_id,field,reason}], revision}`；字段只有仍是 Agent 写入的值才恢复，用户改过的保留并列出；运行中 409 / 60003，已撤销 409 / 60007 |
| POST                | /api/v1/agent/approvals/:aid/decision                 | 对审批的决定 `{decision: approve\|reject, items?: [{index, approve, count?}], answer?, add_budget?}`：批准生成要不超本轮预算（402 / 60008），张数只能少不能多；已处理或已过期 409 / 60004；审批不存在 404 / 60013 |
| GET                 | /api/v1/agent/models                                  | 当前可用的 Agent 模型 `[{key, name, vision}]`；接入模型配置前为空 |
| GET                 | /api/v1/agent/skills                                  | 已启用技能目录 `[{name,title,description,source}]`（内置 + 后台导入且已启用），`@` 弹层读它 |
| GET                 | /api/v1/admin/agent/skills?q=&status=                 | 【admin】技能列表（内置只读 + 导入）；`status=enabled\|disabled` |
| POST                | /api/v1/admin/agent/skills/imports                    | 【admin】上传并预检（multipart：zip 用 `file`；文件夹 / 单个 SKILL.md 用成对的 `files` + `paths`）。预检不通过也返回 200，问题在 `issues`；zip ≤ 50MB、总量 ≤ 100MB，超限 413 / 61011。内容只在内存里，暂存包 1 小时过期 |
| GET/DELETE          | /api/v1/admin/agent/skills/imports/:id[/files?path=]  | 【admin】预览暂存包里的文件 / 放弃暂存；不是本人的或已过期 404 / 61001 |
| POST                | /api/v1/admin/agent/skills/imports/:id/confirm        | 【admin】确认入库：新技能默认停用、v1 生效；同名得到新版本、生效版本不变。内置同名 61003，内容与已有版本相同 61006，预检未通过 61002 |
| GET/PUT/DELETE      | /api/v1/admin/agent/skills/:name                      | 【admin】详情（含版本列表）/ 改显示名 `{title}` / 删除（须先停用 61010；内置 61007；不存在 61004）。删除后对象存储里的整包由清理任务删除 |
| PUT                 | /api/v1/admin/agent/skills/:name/enabled              | 【admin】启停 `{enabled}`；无生效版本 61009 |
| PUT                 | /api/v1/admin/agent/skills/:name/active-version       | 【admin】设为生效 `{version}`（回滚也是它）；版本不存在 61005。已在运行的 Agent 按版本固定，不受影响 |
| GET/DELETE          | /api/v1/admin/agent/skills/:name/versions/:v[...]     | 【admin】版本详情 / `/files?path=` 读包内文本文件（二进制只返回大小）/ `/download` 下载整包 / DELETE 删除版本（生效版本 61008）；全部写操作写 `admin_audit_logs`（`agent_skill.*`） |
| PUT/DELETE          | /api/v1/agent/runs/:rid/feedback                      | 对一轮运行打 👍/👎 `{rating: 1\|-1, reasons?: misread\|wrong_edit\|slow\|bad_output\|other（≤5，点赞时忽略）, comment?: ≤500 字}` → `{rating, reasons, comment}`，再调用就是改选；DELETE 取消（幂等）。运行还在跑或在等用户 409 / 60014；别人的运行 404 / 60012。会话里追加 `feedback.set` / `feedback.cleared` 事件 |
| GET                 | /api/v1/admin/agent/runs                              | 【admin 或分析令牌】Agent 运行摘要列表（创建时间倒序，游标分页）：`since`/`until`（RFC3339 或 YYYY-MM-DD）、`status`（逗号分隔）、`feedback=down\|up\|none\|any`、`undone=true`、`harness_key`、`prompt_version`、`model_key`、`user_id`、`limit`（≤500）、分页二选一：`page`（页码，从 1 起，后台页面用，返回 `{items, total, page, page_size}`）或 `cursor`（本地 CLI 用，返回 `{items, next_cursor}`，不额外计总数），两者同时给 400；run/session/canvas id 是编码串 |
| GET                 | /api/v1/admin/agent/runs/:rid[/export?format=]        | 【admin 或分析令牌】一次运行的结构化详情（含工具参数和结果）/ 导出给 AI 读：`format=md`（默认，`{format, content}`）、`json`（直接返回结构化对象）、`both`（`{format, content, data}`，本地 CLI 一次拿全）。Markdown 每段 ≤2KB，JSON 完整。结构化数据里的 `timeline`（步骤 `{n, kind, label, text, thinking?, partial?, nested?, at?, tool?}`）与 Markdown 时间线由同一份步骤列表生成，后台详情抽屉直接用它；运行不存在 404 / 60012 |
| GET                 | /api/v1/admin/agent/stats?group_by=                   | 【admin 或分析令牌】按 `harness_key` / `prompt_version` / `model_key` 分组的版本对比指标 `{groups:[{key,n,feedback_rate,down_rate,undo_rate,fail_end_rate,median_duration_ms,median_steps,avg_credits}]}`，比率是 0–1 小数，分母为 0 记 0；另带 `prompt_version`、`build_sha`（按 harness_key 分组时有意义）和 `last_at`（该组最近一次运行时间），**组按 `last_at` 倒序**，前几个就是最近的版本 |
| GET/POST/DELETE     | /api/v1/admin/agent/tokens[/:id]                      | 【admin，仅 JWT】分析令牌：列表 `{items}`（含已过期的，不含已注销的）/ 签发 `{name ≤64, days 1–90}` → `{item, token}`（明文 `vca_…` 只返回这一次，库里只存 sha256）/ 注销（幂等，注销后立即失效并从列表消失）。令牌只读、只能访问上面三个运行记录接口；每分钟 120 次（429）；无效、过期、注销或签发人不再是管理员一律 401；令牌访问写 `admin_audit_logs`（`agent.runs_read`），签发和注销也写（`agent.token_issue` / `agent.token_revoke`） |
| GET                 | /api/v1/models?kind=video                             | 模型清单（`kind` 取 video / image / audio / text；已发布且启用，含 vendor / tags / capabilities，不含渠道与插件细节）                                                                                                                                                                                                                                                              |
| GET/POST            | /api/v1/conversations                                 | 首页生成的对话列表（按最近记录时间倒序，所有对话平等，`active` 标出有进行中任务的对话）/ 新建 `{title?}`；每人最多 200 段（62002）；id 是十六进制串 |
| PATCH/DELETE        | /api/v1/conversations/:id                             | 重命名 `{title}` / 软删除，不存在或不属于自己 62001 |
| GET                 | /api/v1/conversations/:id/records                     | 记录分页 `?before=&limit=`（默认 20，最大 50），新到旧，每条带任务快照；`next` 为下一页的 before |
| POST                | /api/v1/conversations/:id/records                     | 提交一条生成记录，`:id` 可为 `new`（新建一段对话）/ 对话 id；请求头 `Idempotency-Key`；`{kind: image|video|audio, model_id, prompt, input, count 1–4, title?}`，返回 **202**；某一格失败只让那一格 `tasks[i]=null` 并写入 `submit_errors`，请求级错误同生成任务（40003 / 40006） |
| DELETE              | /api/v1/conversations/:id/records/:rid                | 软删除记录，`?cancel_active=true` 时先取消进行中的任务并退回积分；62005 记录不存在 |
| POST                | /api/v1/generation-tasks                              | 提交生成任务，请求头 `Idempotency-Key`；生成数量 N 时传 `node_ids`（N 个节点）拆成 N 个任务，返回 **202** + `{items: [{node_id, task} \| {node_id, error}]}`，节点级错误 40001 积分不足(402)、40002 并发已满(429)；请求级错误 40003 模型不可用、40006 参数不合法                                                                                                                   |
| GET                 | /api/v1/generation-tasks/:id                          | 单个任务                                                                                                                                                                                                                                                                                                                                                                           |
| GET                 | /api/v1/generation-tasks?ids=1,2,3 / ?status=active   | 批量对账（≤100）/ 当前用户进行中的任务                                                                                                                                                                                                                                                                                                                                             |
| POST                | /api/v1/generation-tasks/:id/cancel                   | 软取消，退回冻结积分                                                                                                                                                                                                                                                                                                                                                               |
| GET                 | /api/v1/credits                                       | 积分余额 `{balance, frozen, available}`（新用户初始积分取后台注册设置，缺省 50）                                                                                                                                                                                                                                                                                                   |
| GET                 | /api/v1/me                                            | 个人资料 `{id, username, nickname, email, role, avatar_url, created_at, email_verified_at}`；`avatar_url = /files/<avatar_key>`，没有头像为空串 |
| PATCH               | /api/v1/me                                            | 改昵称 `{nickname}`：去首尾空白后 0..32 字符、不含控制字符 / 换行，否则 10001；空串表示清空 |
| POST                | /api/v1/me/avatar                                     | 上传头像（multipart `file`，≤2MB）：内容嗅探只允许 png/jpeg/webp/gif（否则 55005），超 2MB 或尺寸 >2048 返回 55006；key 为 `avatars/<uid>/<16 位随机>.<ext>`，不入 assets 表，尽力删除旧头像 |
| DELETE              | /api/v1/me/avatar                                     | 移除头像，尽力删除旧文件 |
| PUT                 | /api/v1/me/password                                   | 改密码 `{old_password, new_password}` → 与登录同构的 `{token, expire_at, role}`；15 分钟内输错 5 次锁 15 分钟（55004 / 429）；55001 当前密码错误（Msg 带剩余次数）/ 55002 与当前相同 / 55003 过于常见或等于用户名；成功后 token_version+1、断开全部 WS |
| GET                 | /api/v1/me/stats                                      | `{total, success, failed, last7d, spent_credits, canvas_count}`，口径同后台 TaskStats（排除试跑） |
| GET                 | /api/v1/me/activity?tz=&year=                         | 热力图 `{tz, start, end, years, total, days:[{date,count,image,video,audio,text}]}`；按用户时区分天、排除试跑，只返回 count>0 的日子；tz 非法回落 Asia/Shanghai；year 省略为最近一年，超出注册年份..今年返回 10001 |
| GET                 | /api/v1/me/credits/ledger?type=&page=&page_size=      | 积分流水页码分页 `{items:[{id,type,amount,task_id,agent_call_id,note,created_at}], total, page, page_size}`；page_size 只允许 10/20/50，page<1 返回 10001；按 created_at DESC, id DESC；不返回操作人 |
| POST                | /api/v1/ws/ticket                                     | 换取一次性 WebSocket ticket（30s 有效）                                                                                                                                                                                                                                                                                                                                            |
| GET                 | /api/v1/ws?ticket=…                                   | WebSocket 升级，推送 `task.updated`（无需 JWT，身份由 ticket 决定）                                                                                                                                                                                                                                                                                                                |
| POST                | /api/v1/assets                                        | 上传素材（multipart，字段 `file`），由后端中转                                                                                                                                                                                                                                                                                                                                     |
| POST                | /api/v1/assets/upload-intents                         | 申请上传：存储开了浏览器直传时返回直传凭证（`mode=direct`，`method` 为 `post` 或 `put`），否则返回 `mode=proxy`，客户端改走上一个接口                                                                                                                                                                                                                                              |
| POST                | /api/v1/assets/upload-intents/:id/complete            | 直传完成后登记素材：后端复核大小、按内容嗅探类型，不合法的对象会被删除；重复提交幂等                                                                                                                                                                                                                                                                                               |
| GET                 | /api/v1/assets/:id                                    | 素材信息（`url` 是稳定地址 `/files/<key>`，不会过期）                                                                                                                                                                                                                                                                                                                              |
| GET                 | /api/v1/assets/generated                              | 生成历史：`?kind=image|video|audio`（必填）`&source=all|conversation|canvas&canvas_id=&created_from=RFC3339&order=desc|asc&q=&cursor=&limit=`（默认 40，最大 100）；只含 `source=generated` 且非试跑任务的素材，每项带 `prompt`、`model_label`、`canvas`（`null`=首页对话；画布已删为 `deleted:true`）、`library_item_id`；返回 `{items,next_cursor}` |
| GET/POST            | /api/v1/asset-folders                                 | 资产库文件夹列表 `{items:[{id,name,item_count,created_at}]}`（不分页）/ 新建 `{name}`，返回 **201**；名称去首尾空白后 1–30 字（64001），同名忽略大小写（64002），每人最多 100 个（64003）；id 是十六进制串 |
| PATCH/DELETE        | /api/v1/asset-folders/:id                             | 重命名 `{name}` / 删除文件夹并同一事务移出其中的素材，返回 `{removed}`，不动素材与存储文件；不存在或不属于自己 64004 |
| GET                 | /api/v1/asset-library/items                           | 资产库条目：`?folder_id=root|<id>|all&kind=&q=&cursor=&limit=`，加入时间倒序，返回 `{items:[{id,asset,folder_id,created_at}],next_cursor}`；文件夹不存在 64004 |
| POST                | /api/v1/asset-library/items                           | 加入资产库 `{asset_id, folder_id?}`：新增 **201**，素材已在库里 **200** 并返回现有归属；素材不属于自己 40007，文件夹不存在 64004 |
| PATCH/DELETE        | /api/v1/asset-library/items/:id                       | 移动 `{folder_id: id|null}`（返回条目）/ 移出资产库（不动素材）；不存在或不属于自己 64005 |
| GET                 | /api/v1/characters                                    | 角色卡列表 `{items:[CharacterView]}`，`updated_at` 倒序不分页；`?q=` 按名称包含搜索；`?ids=a,b,c` 按十六进制 ID 批量读取（≤ 100，有 `ids` 时忽略 `q`，查不到的**缺席**） |
| POST                | /api/v1/characters                                    | 新建 `{name, description?, avatar_asset_id, extra_asset_ids?, voice_asset_id?}`，返回 **201**；名称 1–64 字且忽略大小写唯一（66001），最多 100 张（66002），素材须属于自己且主图/补充图为 image、声音为 audio（66003），补充图 ≤ 4 张、去重、不含主图（66005）；id 是十六进制串 |
| GET/PATCH/DELETE    | /api/v1/characters/:id                                | 详情 / 部分更新（`voice_asset_id: null` 清空声音、`extra_asset_ids: []` 清空补充图，未出现的字段不改）/ 删除（只删角色卡，不动素材），删除返回 **204** 空正文；不存在或不属于自己 66004 |
| GET                 | /files/*                                              | 素材稳定地址，不鉴权（靠 key 不可猜测）：素材在本地存储则直接返回文件（支持 Range），在对象存储则现签名并 302 跳转                                                                                                                                                                                                                                                                 |
| GET                 | /files/*?v=thumb\|poster                              | 素材缩略图 / 视频封面：素材所在存储有已发布的图片处理服务时 302 到厂商的处理地址；没有时缩略图回退原图、封面 404；种类不匹配（图片请求 poster、视频请求 thumb、音频）404                                                                                                                                                                                                           |

### AI 管理接口（admin / super_admin）

角色：`user` / `admin`（运营，管模型）/ `super_admin`（运维，装插件、管渠道与 Key）。管理接口读与模型相关写要求 `admin` 或 `super_admin`；
插件与渠道的写接口只有 `super_admin`，`admin` 调用返回 403（10004）。首个 `super_admin` 只能用 SQL 提升：
`UPDATE users SET role = 'super_admin' WHERE username = 'xxx';`（角色最多缓存 30 秒）。

协议由**插件**（JS，跑在独立的 plugin-runner 进程里）实现，渠道固定一个插件版本 + 地址 + 加密的 Key，模型绑定渠道。
内置插件（`plugins/*.js`，一个插件只管一种类型：文本 / 图片 / 视频 / 音频）启动时按 key + version 自动登记，不能删除。渠道 Key 只写不读，主密钥来自环境变量 `APP_AI_SECRET_KEY`。
完整的请求 / 响应见 [admin-ai-api.md](docs/admin-ai-api.md)，插件契约见 [plugin-contract.md](docs/plugin-contract.md)。

| 方法     | 路径                                            | 权限        | 说明                                                                                                                 |
| -------- | ----------------------------------------------- | ----------- | -------------------------------------------------------------------------------------------------------------------- |
| GET      | /api/v1/admin/ai/me                             | admin       | 当前用户 `{user_id, role}`，前端据此隐藏写操作                                                                       |
| GET      | /api/v1/admin/ai/stats                          | admin       | 总览页任务统计：`?days=7\|30`（默认 7），返回 `daily`（连续 `days` 天的成功 / 失败 / 其他）、`by_model`、`by_kind`；试跑不计 |
| GET      | /api/v1/admin/ai/plugins                        | admin       | 插件与版本列表（含 meta、每个版本被多少模型固定） |
| POST     | /api/v1/admin/ai/plugins                        | super_admin | 上传插件（multipart，字段 `file`）；预检不通过也返回 200，`accepted=false` + `issues`                                |
| PUT      | /api/v1/admin/ai/plugins/:key/enabled           | super_admin | 启停插件                                                                                                             |
| DELETE   | /api/v1/admin/ai/plugins/:key/versions/:version | super_admin | 删除未被引用的版本（内置插件 / 仍被引用返回 409）                                                                    |
| GET      | /api/v1/admin/ai/plugins/:key/delete-check      | super_admin | 删除预检：`{blockers:[{kind,message,refs}]}`，kind 为 `builtin_plugin` / `plugin_models` / `active_tasks` |
| POST     | /api/v1/admin/ai/plugins/:key/upgrade-models    | super_admin | 把协议固定在旧版本上的模型批量升到指定 / 最新版本，逐个处理、失败不中断 → `{to_version, upgraded, failed}` |
| DELETE   | /api/v1/admin/ai/plugins/:key                   | super_admin | 删除整个插件（全部版本）；内置插件 409（50008），任一版本仍被模型或进行中的任务引用 409（50005） |
| GET      | /api/v1/admin/ai/channels/loads                 | admin       | 各渠道当前的任务负载：`running`（同时生成数，对应 `max_running`）、`waiting`（排队数）；没有未完成任务的渠道不返回   |
| GET      | /api/v1/admin/queue/overview                    | admin       | 后台「任务队列」页总览：`queue`（一个生成任务从提交、查询到转存全程是一个 job：`paused`、每实例并发 `concurrency`、硬上限 `hard_cap`、job 计数 `counts` = `waiting` 排队 / `running` 处理中 / `retryable` / `discarded_1h`）、`channels`（渠道名额占用、`max_running`、等待数、最早等待秒数）、`instances`（实例心跳、是否在线 / leader、`stats` = 在处理的任务数 `inflight` 与生效并发 `limit`） |
| GET      | /api/v1/admin/queue/throughput                  | admin       | 近 N 分钟（`?minutes=`，15–1440，默认 60）每分钟的完成数、失败数、平均排队时长；没有任务的分钟补 0；试跑不计 |
| PUT      | /api/v1/admin/queue/concurrency                 | super_admin | 调整每实例同时处理的任务数 `{concurrency}`，范围 1 到硬上限（配置 `ai.queue.max_concurrency`），保存后各实例 5 秒内生效；返回 `{concurrency, applied_instances, total_instances}`，审计 `queue.concurrency` |
| POST     | /api/v1/admin/queue/pause                       | super_admin | 暂停队列（对所有实例生效，幂等；不再开始新任务，正在处理的继续，排队时间照常累计），审计 `queue.pause` |
| POST     | /api/v1/admin/queue/resume                      | super_admin | 恢复队列（幂等），审计 `queue.resume` |
| POST     | /api/v1/admin/queue/river-ticket                | super_admin | 换取打开 River UI（任务级监控：查看、重试、取消、删除 job）的一次性地址 `{url}`，ticket 30 秒内有效、只能用一次 |
| ANY      | /api/v1/admin/river/*                           | ticket / cookie | 嵌入的 River UI，不走 Bearer：用 ticket 换 HttpOnly + SameSite=Strict 的会话 cookie（30 分钟，滑动续期），之后每个请求都实时校验账号仍是启用的 super_admin；没有会话 401，角色不够 403 |
| GET      | /api/v1/admin/ai/channels[/:key]                | admin       | 渠道列表（渠道顺序）/ 详情；`secret_set`、`secret_key_set` 只告诉有没有设置 Key |
| POST     | /api/v1/admin/ai/channels                       | super_admin | 新建渠道：只有名称、地址、请求头、限流，**不绑协议**，保存只做字段校验 |
| PUT      | /api/v1/admin/ai/channels/:key                  | super_admin | 更新渠道（字段可选；`headers` 传了就整体替换） |
| PUT      | /api/v1/admin/ai/channels/:key/secret           | super_admin | 设置渠道 API Key（只写） |
| PUT      | /api/v1/admin/ai/channels/:key/secret-key       | super_admin | 设置 / 清除渠道 Secret Key（可选的第二凭证，空串清除，只写） |
| POST     | /api/v1/admin/ai/channels/:key/pull-models      | admin       | 向上游拉取模型清单（`GET {base_url}/v1/models`，不落库）→ `{models:[{id,exists}],total}` |
| POST     | /api/v1/admin/ai/channels/:key/models/import    | admin       | 把所选上游模型导入成“待配置”模型（未启用、没选协议），已有的跳过 |
| PUT      | /api/v1/admin/ai/channels/:key/models/sort      | admin       | 调整渠道内模型顺序：`{keys}` 必须是该渠道全部模型 |
| PUT      | /api/v1/admin/ai/channels/sort                  | super_admin | 调整渠道顺序：`{keys}` 必须是全部渠道；用户看到的模型顺序 = 渠道顺序 + 渠道内顺序 |
| GET      | /api/v1/admin/ai/channels/:key/delete-check     | super_admin | 删除预检，kind 为 `channel_models`（refs 是模型）/ `active_tasks`                                                    |
| DELETE   | /api/v1/admin/ai/channels/:key                  | super_admin | 删除渠道并删掉它的 Key 与 Secret Key；仍被模型（最新草稿或已发布版本）或进行中的任务引用 409（50016）                              |
| GET      | /api/v1/admin/storages[/:id]                    | admin       | 存储列表 / 详情（`secret_set` 只告诉有没有设置密钥，`access_key_id` 已脱敏；带素材数、是否锁定、最近一次测试结果）   |
| GET      | /api/v1/admin/storages/presets                  | admin       | 服务商预设：地域列表、直传方式、固定的寻址方式                                                                       |
| POST     | /api/v1/admin/storages/test                     | super_admin | 测试一份未保存的配置（不落库），返回分步结果                                                                         |
| POST     | /api/v1/admin/storages                          | super_admin | 新建存储：保存前自动测试，测试不通过仍保存但不能设为默认；密钥加密存 `ai_secrets`                                    |
| PUT      | /api/v1/admin/storages/:id                      | super_admin | 修改配置（整份表单，带 `version` 乐观锁，冲突 409 / 51009）；已有素材时定位字段（地域、桶、前缀等）锁定，409 / 51005 |
| PUT      | /api/v1/admin/storages/:id/secret               | super_admin | 同时替换 AccessKey ID 与 Secret：先用新凭证测试，不通过什么都不改                                                    |
| POST     | /api/v1/admin/storages/:id/check                | super_admin | 用已存密钥重新测试                                                                                                   |
| PUT      | /api/v1/admin/storages/default                  | super_admin | 设为默认存储（最近测试未通过 409 / 51010）；只影响新素材                                                             |
| GET      | /api/v1/admin/storages/:id/delete-check         | super_admin | 删除预检：素材数、进行中的上传数、能否删除及原因                                                                     |
| DELETE   | /api/v1/admin/storages/:id                      | super_admin | 删除存储与它的密钥；内置 / 默认 / 仍被素材引用 409                                                                   |
| GET      | /api/v1/admin/image-processors[/:id]            | admin       | 图片处理服务列表 / 详情（绑定的存储、工作配置与线上配置、`has_draft`、可回滚版本、最近一次校验与试跑）               |
| GET      | /api/v1/admin/image-processors/presets          | admin       | 厂商预设：Cloudflare R2 / 腾讯云数据万象 / 阿里云 OSS，允许绑定的存储、可选格式、是否支持视频封面、默认配置          |
| POST     | /api/v1/admin/image-processors                  | super_admin | 新建草稿；厂商与存储不匹配 400 / 52005（腾讯云只能绑 COS、阿里云只能绑 OSS、Cloudflare 只能绑有公开域名的 R2）       |
| PUT      | /api/v1/admin/image-processors/:id              | super_admin | 保存草稿（整份，带 `version` 乐观锁，冲突 409 / 52004）；已发布的线上配置不受影响                                    |
| POST     | /api/v1/admin/image-processors/:id/check        | super_admin | 校验与试跑（绑定关系、域名、签名、用该存储里真实素材各取一次缩略图 / 封面），结果写入 `check`；未通过也返回 200      |
| POST     | /api/v1/admin/image-processors/:id/publish      | super_admin | 发布草稿（body `{version}`）：要求针对这一版草稿的校验没有 fail，否则 409 / 52007；同存储旧的已发布服务自动停用      |
| POST     | /api/v1/admin/image-processors/:id/rollback     | super_admin | 回滚到上一个已发布版本，没有 409 / 52009                                                                             |
| POST     | /api/v1/admin/image-processors/:id/disable      | super_admin | 停用：该存储回退原图 / 占位                                                                                          |
| DELETE   | /api/v1/admin/image-processors/:id              | super_admin | 删除草稿或已停用的；已发布的 409 / 52008                                                                             |
| GET/POST | /api/v1/admin/ai/models                         | admin       | 列表（含 `label`、`channel`、`enabled`）/ 新建（body `{body, note}`，默认未启用；有校验问题也会保存，启用时才拦）                       |
| GET/PUT  | /api/v1/admin/ai/models/:key                    | admin       | 详情（配置正文 `body`） / 更新（已启用的保存即生效，校验不过拒绝）                                                                                     |
| POST     | /api/v1/admin/ai/models/:key/validate           | admin       | 校验（错误精确到 JSON 路径）                                                                                         |
| POST     | /api/v1/admin/ai/models/:key/dry-run            | admin       | 渲染请求描述但不发送（不含注入后的鉴权头，凭证脱敏）                                                                 |
| POST     | /api/v1/admin/ai/models/:key/test-run           | admin       | 用已保存的配置真实试跑，不扣积分；`GET /admin/ai/test-runs/:id` 轮询，`GET /admin/ai/test-runs/:id/trace` 看追踪             |
| PUT      | /api/v1/admin/ai/models/:key/enabled、/sort     | admin       | 启用 / 停用（启用前检查配置、渠道与 Key）/ 排序                                                                                                        |
| GET      | /api/v1/admin/ai/models/:key/delete-check       | admin       | 删除预检，kind 为 `model_enabled`                                                                                    |
| DELETE   | /api/v1/admin/ai/models/:key                    | admin       | 硬删除模型（必须先下架，否则 409 / 50031），不可恢复；之后同名 key 可以重新新建 / 导入                   |
| GET      | /api/v1/admin/ai/schema/model                   | admin       | 模型配置的 JSON Schema                                                                                               |

统一响应格式：

```json
{ "code": 0, "msg": "success", "data": {}, "request_id": "..." }
```

`code` 为 0 表示成功，错误码定义在 `internal/pkg/errcode`。
