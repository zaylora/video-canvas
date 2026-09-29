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
| GET | /api/v1/models?kind=video | 模型清单（已发布且启用，含 input_schema，不含平台细节） |
| POST | /api/v1/generation-tasks | 提交生成任务，请求头 `Idempotency-Key`，返回 **202** + 任务快照；错误码 40001 积分不足(402)、40002 并发已满(429)、40003 模型不可用、40006 参数不合法 |
| GET | /api/v1/generation-tasks/:id | 单个任务 |
| GET | /api/v1/generation-tasks?ids=1,2,3 / ?status=active | 批量对账（≤100）/ 当前用户进行中的任务 |
| POST | /api/v1/generation-tasks/:id/cancel | 软取消，退回冻结积分 |
| GET | /api/v1/credits | 积分余额 `{balance, frozen, available}`（新用户初始 50） |
| POST | /api/v1/ws/ticket | 换取一次性 WebSocket ticket（30s 有效） |
| GET | /api/v1/ws?ticket=… | WebSocket 升级，推送 `task.updated`（无需 JWT，身份由 ticket 决定） |
| POST | /api/v1/webhooks/:provider/:secret | 平台回调，只触发立即轮询，不信任内容；`ai.webhook_secret` 未配置时 404 |
| POST | /api/v1/assets | 上传素材（multipart，字段 `file`） |
| GET | /api/v1/assets/:id | 素材信息（URL 每次现生成） |
| GET | /files/* | 本地存储的静态文件（仅 `storage.driver=local`，开发用） |

### AI 配置管理（需要 admin 角色）

提升管理员：`UPDATE users SET role = 'admin' WHERE username = 'xxx';`（角色最多缓存 30 秒）。
配置存在数据库里，发布后热生效，详见 [平台协议配置化设计](../docs/design/平台协议配置化设计.md)。
平台凭证只写不读，主密钥来自环境变量 `APP_AI_SECRET_KEY`；首次启动会自动种下并发布 `runninghub` 平台，
需要管理员调用 `PUT /api/v1/admin/ai/secrets/runninghub_api_key` 设置 Key。

| 方法 | 路径 | 说明 |
|------|------|------|
| GET/POST | /api/v1/admin/ai/{providers,models} | 列表 / 新建草稿（body `{body, note}`，有校验问题也会保存，发布时才拦） |
| GET/PUT | /api/v1/admin/ai/{providers,models}/:key | 详情（草稿 + 已发布） / 更新草稿 |
| POST | /api/v1/admin/ai/{providers,models}/:key/validate | 校验（错误精确到 JSON 路径） |
| POST | /api/v1/admin/ai/{providers,models}/:key/publish、/rollback | 发布 / 回滚（`{revision_id}`） |
| GET | /api/v1/admin/ai/{providers,models}/:key/revisions[/:rid] | 版本历史 |
| POST | /api/v1/admin/ai/models/:key/dry-run | 渲染请求但不发送（凭证脱敏） |
| POST | /api/v1/admin/ai/models/:key/test-run | 用草稿真实试跑，不扣积分；`GET /admin/ai/test-runs/:id` 轮询 |
| PUT | /api/v1/admin/ai/models/:key/enabled、/sort | 上下架 / 排序 |
| POST | /api/v1/admin/ai/import/runninghub | `{webapp_id}` 自动导入节点，返回模型草稿建议（不落库） |
| GET/PUT | /api/v1/admin/ai/secrets[/:name] | 凭证：只返回是否已设置；PUT 只写 |
| GET | /api/v1/admin/ai/schema/:target | DSL 的 JSON Schema |

统一响应格式：

```json
{ "code": 0, "msg": "success", "data": {}, "request_id": "..." }
```

`code` 为 0 表示成功，错误码定义在 `internal/pkg/errcode`。
