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
│   ├── storage/         # 素材存储：本地磁盘（开发）/ S3 兼容（含 OSS）
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
| POST | /api/v1/assets | 上传素材（multipart，字段 `file`） |
| GET | /api/v1/assets/:id | 素材信息（URL 每次现生成） |
| GET | /files/* | 本地存储的静态文件（仅 `storage.driver=local`，开发用） |

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
| GET | /api/v1/admin/ai/channels/loads | admin | 各渠道当前的任务负载：`running`（同时生成数，对应 `max_running`）、`waiting`（排队数）；没有未完成任务的渠道不返回 |
| GET | /api/v1/admin/ai/channels[/:key] | admin | 渠道列表 / 详情（`secret_set` 只告诉有没有设置 Key） |
| POST | /api/v1/admin/ai/channels | super_admin | 新建渠道 |
| PUT | /api/v1/admin/ai/channels/:key | super_admin | 更新渠道（字段可选；改 `plugin_version` 即切换插件版本） |
| PUT | /api/v1/admin/ai/channels/:key/secret | super_admin | 设置渠道 Key（只写） |
| POST | /api/v1/admin/ai/channels/:key/check | super_admin | 连通性检查 |
| POST | /api/v1/admin/ai/channels/:key/import | admin | 从渠道导入模型草稿（只预填，不落库） |
| GET/POST | /api/v1/admin/ai/models | admin | 列表（含 `label`、`channel`）/ 新建草稿（body `{body, note}`，有校验问题也会保存，发布时才拦） |
| GET/PUT | /api/v1/admin/ai/models/:key | admin | 详情（草稿 + 已发布） / 更新草稿 |
| POST | /api/v1/admin/ai/models/:key/validate | admin | 校验（错误精确到 JSON 路径） |
| POST | /api/v1/admin/ai/models/:key/publish、/rollback | admin | 发布 / 回滚（`{revision_id}`） |
| GET | /api/v1/admin/ai/models/:key/revisions[/:rid] | admin | 版本历史 |
| POST | /api/v1/admin/ai/models/:key/dry-run | admin | 渲染请求描述但不发送（不含注入后的鉴权头，凭证脱敏） |
| POST | /api/v1/admin/ai/models/:key/test-run | admin | 用草稿真实试跑，不扣积分；`GET /admin/ai/test-runs/:id` 轮询，`GET /admin/ai/test-runs/:id/trace` 看追踪 |
| PUT | /api/v1/admin/ai/models/:key/enabled、/sort | admin | 上下架 / 排序 |
| GET | /api/v1/admin/ai/schema/model | admin | 模型配置的 JSON Schema |

统一响应格式：

```json
{ "code": 0, "msg": "success", "data": {}, "request_id": "..." }
```

`code` 为 0 表示成功，错误码定义在 `internal/pkg/errcode`。
