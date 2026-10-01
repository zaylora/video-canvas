# Changelog
所有重要变更都会记录在此文件中。


## v0.1.1 - 2026-10-01



### Documentation


- Update changelog for v0.1.0


### Fixed


- 副生图不能参考图片问题以及修改画布 ID 为十六进制


### Other


- 更新管理后台UI界面


## v0.1.0 - 2026-09-30



### Added


- 后端完成插件协议设计

- 添加协议插件


### Changed


- 后端代码规范


### Documentation


- Update changelog for v0.0.1

- 文档整理


### Fixed


- Avoid tag visibility race in release workflow

- 画布引用不了后端模型

- 优化下载功能，确保下载超时后 Body 可读并避免 context 泄漏；更新插件元数据说明

- 更新钩子超时设置为 1 秒，优化并发控制文档


### Other


- 添加代码规范


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


