# video-canvas backend

Gin + GORM + go-redis + Viper + Zap 的 Go 后端骨架。

## 目录结构

```
backend/
├── cmd/server/          # 程序入口
├── configs/             # 配置文件
├── internal/            # 项目内部代码（核心）
│   ├── config/          # 配置定义
│   ├── model/           # 数据模型
│   ├── repository/      # 数据访问层
│   ├── service/         # 业务逻辑层
│   ├── handler/         # HTTP 处理层
│   ├── router/          # 路由注册
│   ├── middleware/      # 中间件
│   ├── provider/        # 外部供应商调用与任务调度：dsl（声明式配置）/ engine（通用执行引擎）/ worker（调度）
│   ├── pkg/ws/          # 用户级 WebSocket：Hub / ticket
│   ├── storage/         # 素材存储：内置本地磁盘 + S3 兼容对象存储（OSS / COS / S3 / R2），Registry 按 id 解析
│   ├── cache/           # Redis 缓存
│   ├── initialize/      # 初始化
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

数据库仅支持 PostgreSQL，启动前需准备好数据库并在 `database.dsn` 中填写连接信息。

- 启用 Redis：`redis.enabled` 改为 `true`
- 环境变量覆盖：`APP_` + 配置路径，如 `APP_SERVER_PORT=9000`

## 接口

| 方法 | 路径 | 说明 |
|------|------|------|
| GET | /health | 健康检查（数据库、Redis、版本信息） |
| POST | /api/v1/users | 创建用户 |
| GET | /api/v1/users?page=1&page_size=10 | 用户列表 |
| GET | /api/v1/users/:id | 用户详情（带 Redis 缓存） |
| PUT | /api/v1/users/:id | 更新用户 |
| DELETE | /api/v1/users/:id | 删除用户（软删除） |
| POST | /api/v1/auth/register、/api/v1/auth/login | 注册 / 登录 |
| GET/POST/PUT/DELETE | /api/v1/canvas[/:id] | 画布项目（乐观锁 revision） |
| GET | /api/v1/models?kind=video | 模型清单（`kind` 取 video / image / audio / text；已发布且启用，含 vendor / tags / capabilities，不含渠道与插件细节） |
| POST | /api/v1/generation-tasks | 提交生成任务，请求头 `Idempotency-Key`；生成数量 N 时传 `node_ids`（N 个节点）拆成 N 个任务，返回 **202** + `{items: [{node_id, task} \| {node_id, error}]}`，节点级错误 40001 积分不足(402)、40002 并发已满(429)；请求级错误 40003 模型不可用、40006 参数不合法 |
| GET | /api/v1/generation-tasks/:id | 单个任务 |
| GET | /api/v1/generation-tasks?ids=1,2,3 / ?status=active | 批量对账（≤100）/ 当前用户进行中的任务 |
| POST | /api/v1/generation-tasks/:id/cancel | 软取消，退回冻结积分 |
| GET | /api/v1/credits | 积分余额 `{balance, frozen, available}`（新用户初始 50） |
| POST | /api/v1/ws/ticket | 换取一次性 WebSocket ticket（30s 有效） |
| GET | /api/v1/ws?ticket=… | WebSocket 升级，推送 `task.updated`（无需 JWT，身份由 ticket 决定） |
| POST | /api/v1/assets | 上传素材（multipart，字段 `file`），由后端中转 |
| POST | /api/v1/assets/upload-intents | 申请上传：存储开了浏览器直传时返回直传凭证（`mode=direct`，`method` 为 `post` 或 `put`），否则返回 `mode=proxy`，客户端改走上一个接口 |
| POST | /api/v1/assets/upload-intents/:id/complete | 直传完成后登记素材：后端复核大小、按内容嗅探类型，不合法的对象会被删除；重复提交幂等 |
| GET | /api/v1/assets/:id | 素材信息（`url` 是稳定地址 `/files/<key>`，不会过期） |
| GET | /files/* | 素材稳定地址，不鉴权（靠 key 不可猜测）：素材在本地存储则直接返回文件（支持 Range），在对象存储则现签名并 302 跳转 |
| GET | /files/*?v=thumb\|poster | 素材缩略图 / 视频封面：素材所在存储有已发布的图片处理服务时 302 到厂商的处理地址；没有时缩略图回退原图、封面 404；种类不匹配（图片请求 poster、视频请求 thumb、音频）404 |

### AI 管理接口（admin / super_admin）

角色：`user` / `admin`（运营，管模型）/ `super_admin`（运维，装插件、管渠道与 Key）。管理接口读与模型相关写要求 `admin` 或 `super_admin`；
插件与渠道的写接口只有 `super_admin`，`admin` 调用返回 403（10004）。首个 `super_admin` 只能用 SQL 提升：
`UPDATE users SET role = 'super_admin' WHERE username = 'xxx';`（角色最多缓存 30 秒）。

协议由**插件**（JS，跑在独立的 plugin-runner 进程里）实现，渠道固定一个插件版本 + 地址 + 加密的 Key，模型绑定渠道。
内置插件（`plugins/newapi.js`）启动时按 key + version 自动登记，不能删除。渠道 Key 只写不读，主密钥来自环境变量 `APP_AI_SECRET_KEY`。
完整的请求 / 响应见 [admin-ai-api.md](docs/admin-ai-api.md)，插件契约见 [plugin-contract.md](docs/plugin-contract.md)。

| 方法 | 路径 | 权限 | 说明 |
|------|------|------|------|
| GET | /api/v1/admin/ai/me | admin | 当前用户 `{user_id, role}`，前端据此隐藏写操作 |
| GET | /api/v1/admin/ai/plugins | admin | 插件与版本列表（含 meta、渠道数） |
| POST | /api/v1/admin/ai/plugins | super_admin | 上传插件（multipart，字段 `file`）；预检不通过也返回 200，`accepted=false` + `issues` |
| PUT | /api/v1/admin/ai/plugins/:key/enabled | super_admin | 启停插件 |
| DELETE | /api/v1/admin/ai/plugins/:key/versions/:version | super_admin | 删除未被引用的版本（内置插件 / 仍被引用返回 409） |
| GET | /api/v1/admin/ai/plugins/:key/delete-check | super_admin | 删除预检：`{blockers:[{kind,message,refs}]}`，kind 为 `builtin_plugin` / `plugin_channels` / `active_tasks` |
| DELETE | /api/v1/admin/ai/plugins/:key | super_admin | 删除整个插件（全部版本）；内置插件 409（50008），任一版本仍被渠道或进行中的任务引用 409（50005） |
| GET | /api/v1/admin/ai/channels/loads | admin | 各渠道当前的任务负载：`running`（同时生成数，对应 `max_running`）、`waiting`（排队数）；没有未完成任务的渠道不返回 |
| GET | /api/v1/admin/ai/channels[/:key] | admin | 渠道列表 / 详情（`secret_set` 只告诉有没有设置 Key） |
| POST | /api/v1/admin/ai/channels | super_admin | 新建渠道 |
| PUT | /api/v1/admin/ai/channels/:key | super_admin | 更新渠道（字段可选；改 `plugin_version` 即切换插件版本） |
| PUT | /api/v1/admin/ai/channels/:key/secret | super_admin | 设置渠道 Key（只写） |
| POST | /api/v1/admin/ai/channels/:key/check | super_admin | 连通性检查 |
| POST | /api/v1/admin/ai/channels/:key/import | admin | 从渠道导入模型草稿（只预填，不落库） |
| GET | /api/v1/admin/ai/channels/:key/delete-check | super_admin | 删除预检，kind 为 `channel_models`（refs 是模型）/ `active_tasks` |
| DELETE | /api/v1/admin/ai/channels/:key | super_admin | 删除渠道并删掉它的 Key；仍被模型（最新草稿或已发布版本）或进行中的任务引用 409（50016） |
| GET | /api/v1/admin/storages[/:id] | admin | 存储列表 / 详情（`secret_set` 只告诉有没有设置密钥，`access_key_id` 已脱敏；带素材数、是否锁定、最近一次测试结果） |
| GET | /api/v1/admin/storages/presets | admin | 服务商预设：地域列表、直传方式、固定的寻址方式 |
| POST | /api/v1/admin/storages/test | super_admin | 测试一份未保存的配置（不落库），返回分步结果 |
| POST | /api/v1/admin/storages | super_admin | 新建存储：保存前自动测试，测试不通过仍保存但不能设为默认；密钥加密存 `ai_secrets` |
| PUT | /api/v1/admin/storages/:id | super_admin | 修改配置（整份表单，带 `version` 乐观锁，冲突 409 / 51009）；已有素材时定位字段（地域、桶、前缀等）锁定，409 / 51005 |
| PUT | /api/v1/admin/storages/:id/secret | super_admin | 同时替换 AccessKey ID 与 Secret：先用新凭证测试，不通过什么都不改 |
| POST | /api/v1/admin/storages/:id/check | super_admin | 用已存密钥重新测试 |
| PUT | /api/v1/admin/storages/default | super_admin | 设为默认存储（最近测试未通过 409 / 51010）；只影响新素材 |
| GET | /api/v1/admin/storages/:id/delete-check | super_admin | 删除预检：素材数、进行中的上传数、能否删除及原因 |
| DELETE | /api/v1/admin/storages/:id | super_admin | 删除存储与它的密钥；内置 / 默认 / 仍被素材引用 409 |
| GET | /api/v1/admin/image-processors[/:id] | admin | 图片处理服务列表 / 详情（绑定的存储、工作配置与线上配置、`has_draft`、可回滚版本、最近一次校验与试跑） |
| GET | /api/v1/admin/image-processors/presets | admin | 厂商预设：Cloudflare R2 / 腾讯云数据万象 / 阿里云 OSS，允许绑定的存储、可选格式、是否支持视频封面、默认配置 |
| POST | /api/v1/admin/image-processors | super_admin | 新建草稿；厂商与存储不匹配 400 / 52005（腾讯云只能绑 COS、阿里云只能绑 OSS、Cloudflare 只能绑有公开域名的 R2） |
| PUT | /api/v1/admin/image-processors/:id | super_admin | 保存草稿（整份，带 `version` 乐观锁，冲突 409 / 52004）；已发布的线上配置不受影响 |
| POST | /api/v1/admin/image-processors/:id/check | super_admin | 校验与试跑（绑定关系、域名、签名、用该存储里真实素材各取一次缩略图 / 封面），结果写入 `check`；未通过也返回 200 |
| POST | /api/v1/admin/image-processors/:id/publish | super_admin | 发布草稿（body `{version}`）：要求针对这一版草稿的校验没有 fail，否则 409 / 52007；同存储旧的已发布服务自动停用 |
| POST | /api/v1/admin/image-processors/:id/rollback | super_admin | 回滚到上一个已发布版本，没有 409 / 52009 |
| POST | /api/v1/admin/image-processors/:id/disable | super_admin | 停用：该存储回退原图 / 占位 |
| DELETE | /api/v1/admin/image-processors/:id | super_admin | 删除草稿或已停用的；已发布的 409 / 52008 |
| GET/POST | /api/v1/admin/ai/models | admin | 列表（含 `label`、`channel`）/ 新建草稿（body `{body, note}`，有校验问题也会保存，发布时才拦） |
| GET/PUT | /api/v1/admin/ai/models/:key | admin | 详情（草稿 + 已发布） / 更新草稿 |
| POST | /api/v1/admin/ai/models/:key/validate | admin | 校验（错误精确到 JSON 路径） |
| POST | /api/v1/admin/ai/models/:key/publish、/rollback | admin | 发布 / 回滚（`{revision_id}`） |
| GET | /api/v1/admin/ai/models/:key/revisions[/:rid] | admin | 版本历史 |
| POST | /api/v1/admin/ai/models/:key/dry-run | admin | 渲染请求描述但不发送（不含注入后的鉴权头，凭证脱敏） |
| POST | /api/v1/admin/ai/models/:key/test-run | admin | 用草稿真实试跑，不扣积分；`GET /admin/ai/test-runs/:id` 轮询，`GET /admin/ai/test-runs/:id/trace` 看追踪 |
| PUT | /api/v1/admin/ai/models/:key/enabled、/sort | admin | 上下架 / 排序 |
| GET | /api/v1/admin/ai/models/:key/delete-check | admin | 删除预检，kind 为 `model_enabled` |
| DELETE | /api/v1/admin/ai/models/:key | admin | 硬删除模型及其全部版本（必须先下架，否则 409 / 50031），不可恢复；之后同名 key 可以重新新建 / 导入 |
| GET | /api/v1/admin/ai/schema/model | admin | 模型配置的 JSON Schema |

统一响应格式：

```json
{ "code": 0, "msg": "success", "data": {}, "request_id": "..." }
```

`code` 为 0 表示成功，错误码定义在 `internal/pkg/errcode`。
