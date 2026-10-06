---
name: video-canvas-release
description: 发布 video-canvas 新版本；先根据 Git 变更生成并确认 AI 版本日志，再更新 CHANGELOG、创建并推送 Git 标签。
metadata:
  short-description: 发布 video-canvas 版本
---

# video-canvas 发版

用于用户要求发布 video-canvas 新版本时。项目目录固定为 `/Users/Zhuanz/Desktop/video-canvas`，版本文件为 `/Users/Zhuanz/Desktop/video-canvas/VERSION`。

## 版本判定

先读取 `VERSION`，要求内容是 `MAJOR.MINOR.PATCH` 三段数字：

- 用户没有提供版本号：`PATCH + 1`，例如 `0.1.6 → 0.1.7`。
- 用户说“需求版本”或明确要求中间版本，但没有给出完整三段：`MINOR + 1` 且 `PATCH = 0`，例如 `0.1.6 → 0.2.0`。
- 用户提供两段版本（例如 `0.2`、`0.2.x`）：视为需求版本，生成 `0.2.0`。
- 用户提供完整三段版本（例如 `0.1.7`）：使用该版本，不自动改写。
- 大版本只在用户明确提供时改变；版本号必须是非负整数，目标版本必须大于当前版本。

如果用户同时给出互相冲突的版本表达，以明确的完整三段版本为准；无法判断时停下并询问。

## 版本日志规则

版本日志面向使用 video-canvas 的用户，不是 Git 提交的复制品。发布前由 AI 根据上一个版本标签到当前提交的变更生成草稿，用户确认后才写入 `CHANGELOG.md`。

只保留用户可感知或需要采取行动的变化：

- `新增`：用户可以完成的新功能。
- `改进`：性能、稳定性、体验或兼容性改善。
- `修复`：用户遇到的问题及修复结果。
- `废弃`：将要移除的功能或接口，以及替代方案。
- `破坏性变更`：升级时必须修改配置、调用方式或数据的变化。
- `文档`：影响使用、部署或迁移的重要文档变化。

过滤以下内容，不写入用户版本日志：更新 CHANGELOG、版本号或发布 skill；合并分支；格式化和内部代码整理；没有用户影响的测试、依赖和 CI 维护；只描述实现细节、函数名、文件名或内部重命名的提交。

每条日志使用简体中文，描述用户结果，尽量一条只写一件事。无法从提交、代码差异、测试或 PR 描述确认的内容标记为“待确认”，不得猜测。

## 发版流程

1. 进入项目目录，读取 `git status --short --branch`、`VERSION`、远端标签和当前提交。不要覆盖用户已有的未提交改动；工作区不干净时先说明并停止。
2. 根据上面的规则计算目标版本。确认本地和远端不存在同名 `v<version>` 标签。
3. 找到最近的发布标签 `v<previous-version>`，读取 `v<previous-version>..HEAD` 的非合并提交、变更文件和必要的代码差异。不要只根据提交标题猜测用户行为。
4. 根据“版本日志规则”生成目标版本的日志草稿，按 `新增`、`改进`、`修复`、`废弃`、`破坏性变更`、`文档` 分组。把草稿展示给用户，等待用户确认或修改；确认前不得改写 `CHANGELOG.md`、提交、创建标签或推送。
5. 用户确认后，将草稿写入 `CHANGELOG.md`：顶部保留空的 `## [Unreleased]`；将本次内容写为 `## [<version>] - YYYY-MM-DD`；不复制 Git 提交标题，不写 `Update changelog` 等维护记录；保留已有历史版本，不改写历史事实；更新底部版本比较链接（如果文件使用比较链接）。
6. 运行项目已有的定向检查，并检查 `CHANGELOG.md` 中目标版本存在且至少有一条用户可见变更。提交日志文件：`docs: update changelog for v<version>`。本仓库的 CI 会自动回写 `VERSION`，因此不要本地修改 `VERSION`。
7. 创建带说明的标签：`git tag -a v<version> -m "Release v<version>"`，然后推送：`git push origin v<version>`。
8. 通过 `gh run list` 等待并核对 `Release on tag` 和 `Release Docker images`：发布说明应来自已确认的 `CHANGELOG.md`，`VERSION` 回写、质量检查和镜像发布都成功后才算完成。若任务只要求创建标签，可在标签推送成功后返回标签地址，并说明工作流状态。
9. 最终报告目标版本、标签、Release 地址、工作流地址和每项验证结果。不要把密钥、令牌或运行时数据写入仓库。

## 本仓库发布约定

推送 `v*` 标签会触发：

- `.github/workflows/release-on-tag.yml`：生成 GitHub Release，并在 `master` 更新 `CHANGELOG.md` 与 `VERSION`。
- `.github/workflows/release-docker.yml`：执行质量检查并构建、推送 backend/web 镜像。

标签格式必须是 `vMAJOR.MINOR.PATCH`，例如 `v0.1.7`。
