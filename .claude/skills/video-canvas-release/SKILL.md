---
name: video-canvas-release
description: 发布 video-canvas 新版本；汇总 changelog.d/ 的日志，用 Git 变更查漏，确认发布说明后更新 VERSION 并打标签。
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

版本日志面向 video-canvas 用户。开发时随改动在 `changelog.d/` 新增日志文件，格式见 `CONTRIBUTING.md`「版本日志」。发版以这些文件为来源，用 Git 提交查漏。

只保留用户可感知或需要采取行动的变化：

- `新增`：用户可以完成的新功能。
- `改进`：性能、稳定性、体验或兼容性改善。
- `修复`：用户遇到的问题及修复结果。
- `废弃`：将要移除的功能或接口，以及替代方案。
- `破坏性变更`：升级时必须修改配置、调用方式或数据的变化。
- `文档`：影响使用、部署或迁移的重要文档变化。

过滤以下内容，不写入用户版本日志：更新 CHANGELOG、版本号或发布 skill；合并分支；格式化和内部代码整理；没有用户影响的测试、依赖和 CI 维护；只描述实现细节、函数名、文件名或内部重命名的提交。

每条日志使用简体中文，一条只写一件事，用用户能感受到的结果描述。遇到需要进一步确认的改动，标记为“待确认”，并查阅代码差异、测试或 PR 描述。

## 发版流程

1. 进入项目目录，读取 `git status --short --branch`、`VERSION`、远端标签和当前提交。不要覆盖用户已有的未提交改动；工作区不干净时先说明并停止。
2. 根据上面的规则计算目标版本。确认本地和远端不存在同名 `v<version>` 标签。
3. 运行 `node scripts/changelog-check.mjs release <version> <YYYY-MM-DD>` 预览待发布条目。脚本只汇总已提交的日志文件，`changelog.d/` 有未提交或改动过的文件时会拒绝执行。目录中无待发布条目时向用户说明。
4. 找到最近的发布标签 `v<previous-version>`，对照 `v<previous-version>..HEAD` 中需要版本日志的非合并提交，核对是否遗漏了用户可感知的变化。
5. 以预览为基础整理草稿，合并重复条目。把草稿和待确认的遗漏展示给用户，待其确认后写入。
6. 运行预览命令并加上 `--write`，让脚本写入新版本段落、更新 `VERSION` 并移除已汇总的日志文件。把新版本段落调整为已确认的内容。若文件使用版本比较链接，同步更新对应链接。
7. 运行 `node scripts/changelog-check.mjs lint`，检查新版本段落、`VERSION` 及已汇总的文件删除记录。把这些改动一起提交，提交消息可用 `docs: update changelog for v<version>`。
8. 创建带说明的标签：`git tag -a v<version> -m "Release v<version>"`，然后推送：`git push origin v<version>`。
9. 通过 `gh run list` 等待并核对 `Release Docker images`（其中 `validate` 作业先校验标签、`VERSION` 与 CHANGELOG，`release` 作业在镜像推送成功后才创建 Release）：发布说明应来自已确认的 `CHANGELOG.md`，`VERSION` 校验、质量检查和镜像发布都成功后才算完成。若任务只要求创建标签，可在标签推送成功后返回标签地址，并说明工作流状态。
10. 最终报告目标版本、标签、Release 地址、工作流地址和每项验证结果。不要把密钥、令牌或运行时数据写入仓库。

## 本仓库发布约定

推送 `v*` 标签会触发：

- `.github/workflows/release-docker.yml` 的 `release` 作业：镜像推送成功后，校验 `VERSION` 与标签一致，并生成 GitHub Release；不会再向 `master` 推送提交，因此本地不会因发版而分叉。
- `.github/workflows/release-docker.yml`：执行质量检查并构建、推送 backend/web 镜像。

标签格式必须是 `vMAJOR.MINOR.PATCH`，例如 `v0.1.7`。
