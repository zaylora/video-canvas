# AGENTS.md — video-canvas 后端

本文件写给在本仓库写代码的 AI 编码助手，以及新加入的开发者。动手前先读完，**「硬性规则」一节必须遵守**。
完整规范在 [docs/standards/](docs/standards/README.md)，规则编号（A1、E3……）指向那里的条目。

## 技术栈

- Go 1.27，模块名 `video-canvas`
- Web：Gin；参数校验：go-playground/validator（错误提示已翻译成中文）
- ORM：GORM + PostgreSQL（用到了 `jsonb`、`ILIKE`，**只支持 PostgreSQL**）
- 缓存：go-redis（`redis.enabled=false` 时，缓存层自动降级为空操作）
- 配置：Viper（YAML，可用 `APP_` 前缀的环境变量覆盖）；日志：Zap + lumberjack
- 鉴权：JWT（golang-jwt/v5），密码用 bcrypt
- 检查：golangci-lint v2（配置见 [.golangci.yml](.golangci.yml)）

## 目录结构

```
backend/
├── cmd/server/            # 入口：解析 -c、加载配置、初始化日志、启动 App、监听退出信号
├── configs/config.yaml    # 默认配置；本地私有配置用 configs/*.local.yaml（已 gitignore）
├── internal/
│   ├── config/            # 配置结构体与 Load()
│   ├── initialize/        # DB / Redis 初始化 + 依赖手动组装（app.go）+ 优雅退出
│   ├── router/            # 路由注册、中间件挂载、鉴权分组
│   ├── middleware/        # RequestID / Logger / Recovery / CORS / JWTAuth / Admin
│   ├── handler/           # HTTP 层：取参 → 调 service → 统一响应；bind.go 放取参工具
│   ├── service/           # 业务逻辑层；依赖以接口形式声明在本层
│   ├── repository/        # 数据访问层：只写 GORM 查询，gorm 错误翻译成 ErrNotFound 等
│   ├── cache/             # Redis 缓存，rdb 为 nil 时为空操作
│   ├── model/             # 表模型 + 请求/响应结构体（XxxReq / XxxItem / XxxView）
│   ├── provider/          # 外部平台调用与任务调度：types.go（契约）、engine/、worker/、dsl/
│   │                      #   ⚠ dsl/ 将按 docs/design/协议插件设计.md 被 JS 插件运行时取代，新代码不要再依赖它
│   ├── storage/           # 素材对象存储：local（仅开发）/ s3（S3 兼容，含 OSS）
│   ├── pkg/               # 内部通用包：errcode / response / logger / utils / ws
│   └── tests/             # 全部测试，目录结构与被测包一致
├── pkg/                   # 可被外部引用的公共包（pagination / version），不能依赖 internal
└── docs/standards/        # 代码规范
```

请求链路：`router → middleware → handler → service → repository / cache → PostgreSQL / Redis`

## 硬性规则

违反以下任意一条，都视为改动未完成。

### 架构

1. **分层单向依赖**（A1）：
   - service 不 import `gin`、`gorm.io/gorm`；
   - handler 不访问 repository、cache、数据库；
   - `model` 不依赖任何内部包。

   由 `depguard` 检查。

2. **依赖接口声明在 service 包内**（A2）；service 只依赖 `provider` 包里的契约，不依赖 `engine`、`worker`、`dsl`（A3）。
3. **依赖只在 [initialize/app.go](internal/initialize/app.go) 里手动组装**（A4）。不引入 DI 框架，不做包级可变单例。
4. **事务只通过 repository 的 `WithTx(ctx, fn)` 开启**（A5）。事务内只用 `tx`，不做网络 I/O。
5. **响应只走 `response.OK / OKPage / Accepted / Fail`**（A6）。不要在 handler 里写 `c.JSON`。
6. **用户数据必须带 `user_id` 条件**；资源不存在和不属于当前用户，统一返回“不存在”（A7）。
7. **ctx 作为第一个参数一路往下传**（A8）。`context.Background()` 只出现在 initialize 和后台 goroutine 的根上。

### 错误与日志

8. **业务错误只在 service 产生**，并且必须是 `errcode.*`（E1、E2）。不要把内部错误信息透传给前端。
9. **包装下层错误用 `%w`，判断错误用 `errors.Is / As`**（E3、E4）。断言业务错误时比较 `Code`，不比较指针。
10. **日志只在边界记一次**（E7）：HTTP 请求由 `response.Fail` 记，后台任务由 worker 顶层记。不要用 `fmt.Print*`。
11. **已发布的错误码数值不改**。新模块从 `5xxxx` 号段开始（A9）。

### 测试（完整要求见 [05-测试](docs/standards/05-测试.md)）

12. **每新增或修改一个接口，都必须同时写单元测试**（T1）：
    - service 测试**必写**：用 fake repo 做表驱动，覆盖成功路径和每一个业务错误分支；
    - handler 测试**必写**：至少覆盖成功、参数校验失败（400 + `10001`）、一个业务错误。
13. 测试统一放在 `internal/tests/<包路径>/`，用外部测试包；需要未导出符号时，用 `export_testing.go` 暴露（T2、T3）。
14. **只用标准库** `testing` + `httptest`，不要擅自引入 testify、gomock（T4）。

### service 层逻辑注释（R6）

service 是业务规则唯一的落点。读代码的人应该只看注释就能知道一个方法做了什么、为什么这么做。标准写法见 [service/canvas_project.go](internal/service/canvas_project.go)、[service/generation_task.go](internal/service/generation_task.go)。

1. **每个导出方法都要有中文 doc 注释，并以方法名开头**。一句话说明它做什么，再补充关键的业务约束或返回值的含义：
   ```go
   // Update 带乐观锁更新，成功后返回最新的画布（revision 已 +1）。
   ```
2. **方法体按步骤编号写注释**：`// 1. ...`、`// 2. ...`。每一步说明“做什么”，关键处说明“为什么”。续行用 `//    ` 缩进对齐：
   ```go
   // 3. 乐观锁更新：只有 revision 与数据库一致才更新并把 revision +1；
   //    画布不存在返回 404，版本不一致说明已被其他人/其他端修改，返回 409
   ```
3. **以下情况必须写出原因**，不能只写做了什么：
   - 安全相关的设计；
   - 故意忽略的错误；
   - 默认值、边界修正、降级策略；
   - 错误转换（repository 错误 → errcode → HTTP 状态码）。
4. service 包内声明的依赖接口，每个方法都要写注释；非导出的辅助函数也要有一句话说明。
5. 改了逻辑，必须同步改注释。

handler 和 repository 层：导出方法写一行以方法名开头的中文注释。repository 的注释还要写明查询条件，以及会返回哪些哨兵错误。

### 提交前

15. 以下三条命令**全部通过**才算完成：

```bash
make fmt    # golangci-lint fmt ./...
make lint   # golangci-lint run --new-from-merge-base=master ./...  输出 0 issues 才算通过
make test   # go test ./...；涉及 goroutine / worker / ws 的改动再跑 make test-race
```

存量问题（`make lint-all` 能看到）不会挡住提交，但你改过的代码行必须是干净的，见 [存量问题清单](docs/standards/存量问题清单.md)。

## 新增一个接口 / 模块的步骤清单

以新增 `xxx` 模块为例，按顺序完成：

1. `internal/model/xxx.go`：表模型（嵌入 `BaseModel`，写 `TableName()`）+ 请求结构体（`binding` + `label`）。部分更新的字段用指针。新表要加进 `model.All()`。
2. `internal/pkg/errcode/errcode.go`：在该模块的号段里新增业务错误码。
3. `internal/repository/xxx.go`：数据访问方法，gorm 错误用 `translate()` 翻译。
4. `internal/service/xxx.go`：声明 `XxxRepo` 接口，实现业务方法，**并按上面的要求写逻辑注释**。
5. `internal/handler/xxx.go`：绑定参数、调 service、统一响应。
6. `internal/router/router.go`：在 `Handlers` 里加字段并注册路由。需要登录的路由挂在 `auth` 分组下。
7. `internal/initialize/app.go`：组装 `repo → service → handler`。
8. 编写 service 和 handler 的单元测试。
9. 更新 [README.md](README.md) 的接口表。

动手前先查 [公共设施清单](docs/standards/04-复用与抽象.md#u6-先用已有的公共设施必须)，已有的能力不要重新实现。

## 常用命令

```bash
make run        # go run ./cmd/server -c configs/config.yaml
make build      # 编译到 bin/server，注入版本信息
make test       # go test ./...
make test-race  # go test -race ./...
make fmt        # 格式化
make lint       # 增量检查（提交前必须通过）
make lint-all   # 全量检查
make tools      # 安装固定版本的 golangci-lint
make tidy       # go mod tidy
```

没装 make 时，直接运行注释里的命令，对照表见 [06-工程化与协作 G1](docs/standards/06-工程化与协作.md)。

配置可以用环境变量覆盖：`APP_` + 大写路径，点换成下划线，例如 `APP_SERVER_PORT=9000`、`APP_DATABASE_DSN=...`。**不要把真实密钥写进 `configs/config.yaml`**。

## 其他注意事项

- 注释、错误提示、日志文案统一用中文。
- 新增外部依赖之前，先确认是否必要；完成后执行 `go mod tidy`。
- 提交信息用 Conventional Commits，**type 后面用半角冒号**：`feat(task): 支持批量取消`（G4）。
