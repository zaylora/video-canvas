# Changelog

所有重要变更都会记录在此文件中。


## v0.0.1 - 2026-09-29



### Added


- Feat：新增画布项目管理功能

- 移除配置中未使用的数据库驱动字段。
- 实现 URI 参数绑定，用于处理路径参数。
- 新增 CanvasProjectHandler，用于处理画布项目相关操作：创建、列表查询、详情查询、更新和删除。
- 在 UserHandler 中新增用户列表和用户详情查询功能。
- 初始化画布项目服务（Service）和仓储（Repository）。
- 定义 CanvasProject 模型以及相关的请求/响应结构。
- 为用户列表和画布项目列表实现分页功能。
- 增加画布相关操作的错误处理。
- 更新分页配置，将单页最大数据量限制为 50 条。

- Feat：加入前端和设计skills

- 添加长任务生成

- Feat：添加一键启动脚本

- 添加 Docker 开发环境支持，包括 Dockerfile 和 docker-compose 配置

- Feat: 添加 GitHub Actions 工作流以支持 Docker 镜像发布和版本管理
feat: 添加 CHANGELOG.md 和 VERSION 文件以记录版本信息


### Changed


- Chore：将后端代码迁移到 backend/ 子目录，仓库改为 monorepo 结构

Co-Authored-By: Claude Opus 5.5 <noreply@anthropic.com>


### Fixed


- Fix：移除 dist/


### Other


- Init


