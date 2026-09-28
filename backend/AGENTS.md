# AGENTS.md — video-canvas 后端

本文件写给在本仓库里写代码的 AI 编码助手（以及新加入的开发者）。动手前先读完，**「硬性规则」一节必须遵守**。

## 技术栈

- Go 1.27，模块名 `video-canvas`
- Web：Gin；参数校验：go-playground/validator（错误提示已翻译成中文）
- ORM：GORM + PostgreSQL（用到 `jsonb`、`ILIKE`，**只支持 PostgreSQL**）
- 缓存：go-redis（`redis.enabled=false` 时缓存层自动降级为空操作）
- 配置：Viper（YAML + `APP_` 前缀环境变量覆盖）；日志：Zap + lumberjack
- 鉴权：JWT（golang-jwt/v5），密码使用 bcrypt

## 目录结构与职责

```
backend/
├── cmd/server/            # 程序入口：解析 -c 参数、加载配置、初始化日志、启动 App、监听退出信号
├── configs/config.yaml    # 默认配置；本地私有配置用 configs/*.local.yaml（已 gitignore）
├── internal/              # 项目内部代码，外部模块不可引用
│   ├── config/            # 配置结构体定义与 Load()，新增配置项先在这里加字段
│   ├── initialize/        # 基础设施初始化（DB / Redis）+ 依赖手动组装（app.go）+ 优雅退出
│   ├── router/            # 路由注册、全局中间件挂载、鉴权分组、NoRoute 兜底
│   ├── middleware/        # RequestID / Logger / Recovery / CORS / JWTAuth
│   ├── handler/           # HTTP 层：绑定参数 → 调 service → 统一响应，不写业务逻辑
│   │   └── bind.go        # bindJSON / bindQuery / bindURI / pathID，校验失败自动返回中文提示
│   ├── service/           # 业务逻辑层：校验、编排、错误转换；依赖以接口形式声明在本层
│   ├── repository/        # 数据访问层：只做 GORM 查询，gorm 错误统一翻译成 ErrNotFound 等
│   ├── cache/             # Redis 缓存封装，rdb 为 nil 时所有方法都是空操作
│   ├── model/             # GORM 表模型 + 请求/响应结构体（XxxReq / XxxItem）
│   │   └── base.go        # BaseModel（主键、时间戳、软删除）+ All()（自动迁移注册表）
│   ├── ai/                # AI 助手模块（暂未实现，占位）
│   └── pkg/               # 内部通用包
│       ├── errcode/       # 业务错误码定义（按模块分段）
│       ├── response/      # 统一响应 OK / OKPage / Fail
│       ├── logger/        # Zap 日志封装
│       └── utils/         # JWT 签发解析、RequestID
├── pkg/                   # 可被外部引用的公共包
│   ├── pagination/        # 分页参数 Query（Normalize / Offset / Limit）
│   └── version/           # 版本信息（构建时通过 ldflags 注入）
├── docs/                  # 文档
├── scripts/               # 脚本
└── Makefile               # run / build / test / lint / tidy
```

### 请求链路与分层约束

```
router → middleware → handler → service → repository / cache → PostgreSQL / Redis
```

- **handler**：只负责 `bindJSON/bindQuery/pathID` 取参、`currentUserID(c)` 取当前用户、调用 service、`response.OK/OKPage/Fail` 返回。不直接访问 repository，不写业务判断。
- **service**：所有业务规则写在这里。依赖的 repository/外部客户端 **以接口形式声明在 service 包内**（参考 `UserRepo`、`CanvasProjectRepo`），便于单元测试替换成 fake。返回给上层的业务错误必须是 `errcode.*`。
- **repository**：只写数据库操作，不含业务规则。gorm 的 `ErrRecordNotFound` 用 `translate()` 转成 `repository.ErrNotFound`，service 层不能直接依赖 gorm 错误。
- **依赖组装**：没有 DI 框架，全部在 [internal/initialize/app.go](internal/initialize/app.go) 里手动 `repo → service → handler` 组装，再传给 `router.Handlers`。

### 统一约定

- 响应格式：`{ "code": 0, "msg": "success", "data": {}, "request_id": "..." }`，`code=0` 表示成功。
- 错误码按模块分段：通用 `1xxxx`、用户 `2xxxx`、画布 `3xxxx`，新模块顺延新号段，定义在 [internal/pkg/errcode/errcode.go](internal/pkg/errcode/errcode.go)。非 `*errcode.Error` 的错误会被 `response.Fail` 记日志并统一返回「服务内部错误」，不要把内部错误信息透传给前端。
- 请求结构体：放在 `model` 包，命名 `CreateXxxReq / UpdateXxxReq / ListXxxReq`，用 `binding` 写校验规则，用 `label` 写中文字段名（校验提示会用它）。部分更新字段用指针（如 `Title *string`）区分「没传」和「传了空值」。
- 数据归属：涉及用户数据的查询/更新/删除必须带 `user_id` 条件；查不到和不属于当前用户统一返回「不存在」，避免暴露资源是否存在。
- 列表接口：用 `pagination.Query` 修正分页参数；不返回大字段（参考 `CanvasProjectItem` 不含 `payload_json`）；模糊搜索用 `escapeLike` 转义通配符。
- 新增表：在 `model.All()` 中注册，`database.auto_migrate=true` 时启动自动迁移。

## 硬性规则

### 规则 1：每新增或修改一个接口，都必须同时编写单元测试

没有测试的接口改动视为未完成。

**必须覆盖的层：**

| 层              | 测试文件                          | 写法                                                                        | 要求                                                                                             |
| --------------- | --------------------------------- | --------------------------------------------------------------------------- | ------------------------------------------------------------------------------------------------ |
| service         | `internal/service/xxx_test.go`    | 手写 fake 实现 service 包内声明的 Repo 接口，表驱动测试                     | **必写**。覆盖成功路径 + 每一个业务错误分支（参数非法、不存在、冲突、repo 返回未知错误透传等）   |
| handler         | `internal/handler/xxx_test.go`    | `gin.SetMode(gin.TestMode)` + `httptest`，用真实 service + fake repo 组装   | **必写**。至少覆盖：成功响应、参数校验失败（400 + `10001`）、一个业务错误的 HTTP 状态码和 `code` |
| repository      | `internal/repository/xxx_test.go` | 连接真实 PostgreSQL，从环境变量 `TEST_DATABASE_DSN` 读取，未设置时 `t.Skip` | 有复杂 SQL（乐观锁、模糊搜索、多条件）时写                                                       |
| 纯函数 / 工具包 | 同包 `_test.go`                   | 表驱动                                                                      | 新增工具函数（如 `isJSONObject`、`escapeLike`、`pagination`）时写                                |

**测试约定：**

- 只用标准库 `testing` + `net/http/httptest`，**不要擅自引入 testify、gomock 等新依赖**（需要时先和维护者确认）。
- 测试名 `TestXxxService_Method` / `TestXxxHandler_Method`，子用例名用中文描述场景，例如 `"版本冲突返回 409"`。
- 断言业务错误时比较错误码，而不是指针：`WithMsg` 会返回新副本，`errors.Is` 会失效。用 `errors.As(err, &e)` 后比较 `e.Code`。
- handler 测试中用一个测试中间件模拟登录：`c.Set(middleware.CtxUserIDKey, uint(1))`，不依赖真实 JWT。
- 测试之间不共享可变状态，每个子用例新建 fake。
- 提交前必须通过：

```bash
go test ./...        # 或 make test
gofmt -l . && go vet ./...   # 或 make lint，输出为空才算通过
```

**service 测试示例：**

```go
package service

// fakeCanvasProjectRepo 实现 CanvasProjectRepo，按需返回预设的结果。
type fakeCanvasProjectRepo struct {
	project   *model.CanvasProject
	updateErr error
}

func (f *fakeCanvasProjectRepo) Create(ctx context.Context, p *model.CanvasProject) error { return nil }
func (f *fakeCanvasProjectRepo) GetByID(ctx context.Context, userID, id uint64) (*model.CanvasProject, error) {
	if f.project == nil {
		return nil, repository.ErrNotFound
	}
	return f.project, nil
}
func (f *fakeCanvasProjectRepo) List(ctx context.Context, userID uint64, keyword string, offset, limit int) ([]model.CanvasProjectItem, int64, error) {
	return nil, 0, nil
}
func (f *fakeCanvasProjectRepo) Update(ctx context.Context, userID, id, revision uint64, fields map[string]any) error {
	return f.updateErr
}
func (f *fakeCanvasProjectRepo) Delete(ctx context.Context, userID, id uint64) error { return nil }

func TestCanvasProjectService_Update(t *testing.T) {
	title := "新标题"
	tests := []struct {
		name     string
		req      *model.UpdateCanvasProjectReq
		repoErr  error
		wantCode int // 0 表示期望成功
	}{
		{"只改标题成功", &model.UpdateCanvasProjectReq{Title: &title, Revision: 1}, nil, 0},
		{"未传任何字段", &model.UpdateCanvasProjectReq{Revision: 1}, nil, errcode.ErrCanvasNoChange.Code},
		{"payload 不是 JSON 对象", &model.UpdateCanvasProjectReq{PayloadJSON: json.RawMessage(`[]`), Revision: 1}, nil, errcode.ErrCanvasPayload.Code},
		{"画布不存在", &model.UpdateCanvasProjectReq{Title: &title, Revision: 1}, repository.ErrNotFound, errcode.ErrCanvasNotFound.Code},
		{"版本冲突", &model.UpdateCanvasProjectReq{Title: &title, Revision: 1}, repository.ErrRevisionConflict, errcode.ErrCanvasConflict.Code},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			repo := &fakeCanvasProjectRepo{project: &model.CanvasProject{Title: title, Revision: 2}, updateErr: tt.repoErr}
			_, err := NewCanvasProjectService(repo).Update(context.Background(), 1, 1, tt.req)

			if tt.wantCode == 0 {
				if err != nil {
					t.Fatalf("期望成功，实际返回错误：%v", err)
				}
				return
			}
			var e *errcode.Error
			if !errors.As(err, &e) || e.Code != tt.wantCode {
				t.Fatalf("期望错误码 %d，实际：%v", tt.wantCode, err)
			}
		})
	}
}
```

### 规则 2：service 层必须写清楚逻辑注释

service 是业务规则的唯一落点，读代码的人应该只看注释就能知道这个方法做了什么、为什么这么做。现有的 [internal/service/canvas_project.go](internal/service/canvas_project.go) 和 [internal/service/user.go](internal/service/user.go) 就是标准写法，新代码照此执行：

1. **每个导出方法都要有中文 doc 注释**，以方法名开头，一句话说明做什么，再补充关键的业务约束或返回值含义：
   ```go
   // Update 带乐观锁更新，成功后返回最新的画布（revision 已 +1）。
   ```
2. **方法体按步骤编号注释**：`// 1. ...`、`// 2. ...`，每一步说明「做什么」，关键处说明「为什么」。续行用 `//    ` 缩进对齐：
   ```go
   // 3. 乐观锁更新：只有 revision 与数据库一致才更新并把 revision +1；
   //    画布不存在返回 404，版本不一致说明已被其他人/其他端修改，返回 409
   ```
3. **以下情况必须写出原因**，不能只写做了什么：
   - 安全相关的设计（如「用户不存在和密码错误统一返回同一个错误，避免暴露用户名是否存在」）
   - 故意忽略的错误（如「缓存写失败只影响下次命中率，所以忽略错误」）
   - 默认值、边界修正、降级策略（如「不传时默认为 {}」「未启用 Redis 时继续查库」）
   - 错误转换（repository 错误 → 哪个 errcode，对应什么 HTTP 状态码）
4. **service 包内声明的依赖接口**要有注释说明用途；非导出的辅助函数也要一句话说明。
5. 注释跟随代码更新：改了逻辑必须同步改注释，禁止留下与代码不符的注释。

handler / repository 层保持现有密度即可：handler 方法一行中文说明接口用途，repository 方法说明查询条件和返回的哨兵错误。

## 新增一个接口 / 模块的步骤清单

以新增 `xxx` 模块为例，按顺序完成：

1. `internal/model/xxx.go`：表模型（嵌入 `BaseModel`，写 `TableName()`）+ 请求结构体（`binding` + `label`）；新表加入 `model.All()`。
2. `internal/pkg/errcode/errcode.go`：在该模块号段新增业务错误码。
3. `internal/repository/xxx.go`：数据访问方法，gorm 错误用 `translate()` 翻译。
4. `internal/service/xxx.go`：声明 `XxxRepo` 接口 + 实现业务方法，**按规则 2 写逻辑注释**。
5. `internal/handler/xxx.go`：绑定参数、调 service、统一响应。
6. `internal/router/router.go`：`Handlers` 加字段，注册路由（需要登录的挂在 `auth` 分组下）。
7. `internal/initialize/app.go`：组装 `repo → service → handler`。
8. **按规则 1 编写 service 和 handler 单元测试**，`go test ./...` 与 `make lint` 全部通过。
9. 更新 [README.md](README.md) 的接口表。

## 常用命令

```bash
make run     # go run ./cmd/server -c configs/config.yaml
make build   # 编译到 bin/server，注入版本信息
make test    # go test ./...
make lint    # gofmt -l . && go vet ./...
make tidy    # go mod tidy
```

配置可用环境变量覆盖：`APP_` + 大写路径，点换成下划线，例如 `APP_SERVER_PORT=9000`、`APP_DATABASE_DSN=...`、`APP_JWT_SECRET=...`。**不要把真实密钥写进 `configs/config.yaml`**。

## 其他注意事项

- 注释、错误提示、日志文案统一使用中文。
- 不要修改已发布的错误码数值，前端依赖它做判断。
- 不要在 handler 里写 `c.JSON`，统一走 `response` 包，保证带上 `request_id`。
- 新增外部依赖前先确认是否必要，完成后执行 `go mod tidy`。
