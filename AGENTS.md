# video-canvas 开发目录说明

这是仓库给 Claude、Codex 和其他编码助手使用的入口。全项目的开发逻辑、目录职责、TDD 约定和命令见 [`AGENTS.md`](AGENTS.md)；请先读它，再按目标目录读取更具体的规则。

## 必须遵守的优先级

1. 用户当前请求和现有产品契约。
2. 目标目录最近的 `AGENTS.md` 或 `CLAUDE.md`。
3. 根目录 [`AGENTS.md`](AGENTS.md) 的全项目约定。
4. 相关设计、API、插件契约和代码规范文档。

当前已有的具体规则包括：

- `backend/AGENTS.md`：后端分层、错误处理、依赖组装、敏感信息和测试要求。
- `backend/docs/standards/`：后端架构、日志、复用、测试和工程化规范。
- `web/docs/coding-standards.md`：前端注释、API、类型、错误处理和组件约定。
- `.agents/skills/ui-design/SKILL.md`：前端界面设计和交互改动流程。

## 工作方式

- 先读项目现状和相关测试，再动手改文件。不要假设 Vite 模板 README 代表本项目的真实架构，真实入口以根 README、后端 README、代码和 CI 为准。
- 对功能、修复、行为变化和重构，先写能复现需求或 bug 的失败测试，再实现，再重构。测试覆盖用户可观察行为、边界和业务错误。
- 后端测试放 `backend/internal/tests/`，前端测试放 `web/src/tests/`。先跑定向测试，再跑受影响目录的完整检查；异步测试采用轮询与超时。
- 不覆盖或回退用户已有的未提交改动。修改前查看 `git status`，只编辑当前任务需要的文件。
- 不把密钥、令牌、真实数据库连接串或生成的运行时数据写入仓库。
- 不需要使用 playright 进行端到端测试

## 关键架构记忆

- 仓库根目录 `agent/` 是画布 Agent 的 Node 运行时（pi agent 循环，零密钥，经桥回调 Go）；Go 侧的 Agent 逻辑在 `backend/internal/agent/`、`backend/internal/service/agent/` 和 `backend/internal/handler/agent/`。运行时改动在 `agent/` 下跑 `npm test`。

- 产品是 React 无限画布 + Go API 的 AI 视频创作应用。
- 后端链路为 `router → middleware → handler → service → repository/cache`，异步生成经 worker 和 JS plugin-runner 调用模型供应商。
- 前端通过 HTTP 请求读写资源，通过 WebSocket 接收任务状态，并用任务对账恢复断线期间的更新。
- 画布保存依赖 `revision` 乐观锁；模型配置没有版本管理，保存即写入唯一一份配置，启用开关决定用户是否可见（启用前要通过校验与渠道检查）；素材存储可为本地磁盘或 S3 兼容存储。

交付时给出修改摘要、验证命令及其结果。若某项检查因环境缺失无法运行，明确说明原因和替代验证。
