---
name: video-canvas-release
description: 发布 video-canvas 新版本；从项目 VERSION 读取当前版本，按默认小版本递增、需求版本递增中间版本或用户指定的完整版本创建并推送 Git 标签。
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

## 发版流程

1. 进入项目目录，读取 `git status --short --branch`、`VERSION`、远端标签和当前提交。不要覆盖用户已有的未提交改动；工作区不干净时先说明并停止。
2. 根据上面的规则计算目标版本。确认本地和远端不存在同名 `v<version>` 标签。
3. 发版基于当前 `master`（或用户明确指定的分支）。运行项目已有的定向检查；至少确认版本格式和仓库状态。
4. **不需要手动更新 `VERSION`**。本仓库的 `.github/workflows/release-on-tag.yml` 工作流会在推送标签后自动回写 `VERSION/CHANGELOG.md` 到 `master` 分支，因此直接创建和推送标签即可，无需本地提交版本文件。
5. 创建带说明的标签：`git tag -a v<version> -m "Release v<version>"`，然后推送：`git push origin v<version>`。
6. 通过 `gh run list` 等待并核对 `Release on tag` 和 `Release Docker images`：发布说明、`VERSION/CHANGELOG.md` 回写、质量检查和镜像发布都成功后才算完成。若任务只要求创建标签，可在标签推送成功后返回标签地址，并说明工作流状态。
7. 最终报告目标版本、标签、Release 地址、工作流地址和每项验证结果。不要把密钥、令牌或运行时数据写入仓库。

## 本仓库发布约定

推送 `v*` 标签会触发：

- `.github/workflows/release-on-tag.yml`：生成 GitHub Release，并在 `master` 更新 `CHANGELOG.md` 与 `VERSION`。
- `.github/workflows/release-docker.yml`：执行质量检查并构建、推送 backend/web 镜像。

标签格式必须是 `vMAJOR.MINOR.PATCH`，例如 `v0.1.7`。
