# Changelog
所有重要变更都会记录在此文件中。


## v0.1.9 - 2026-10-05



### Added


- 加入用户管理和注册功能

- 添加节点预设以及剪刀样式


### Changed


- 移除注册设置中的邮箱验证选项，更新相关逻辑和测试

- 目录和文档调整

- 更新发版流程，移除手动更新 VERSION 的步骤

- 运镜一次点了

- 视频节点操作栏样式问题


### Documentation


- Update changelog for v0.1.7

- Update changelog for v0.1.8


### Fixed


- 前端浏览器保存用户会话记录问题；注册首个用户判断问题


### Other


- 添加发版skills

- 节点预设

- 调整格式


## v0.1.7 - 2026-10-04



### Added


- 添加部署脚本

- 视频加载UI


### Documentation


- Update changelog for v0.1.6


### Other


- 节点加载占位UI


## v0.1.6 - 2026-10-04



### Added


- 加入媒体转换服务

- 优化视频节点的宽度以更好地显示画面细节

- 双击预览图片和视频

- 新增分组节点功能及相关测试


### Changed


- 优化生成插件提示词

- 移除不必要的入场动画，简化卡片组件逻辑


### Documentation


- Update changelog for v0.1.5


### Fixed


- 无法拖动视频节点问题


## v0.1.5 - 2026-10-03



### Added


- 添加 docker-compose 配置文件以支持服务管理

- 画布自动保存改为停手后再存

- 添加@引用和节点富文本

- 添加对象存储配置


### Changed


- Docker开发配置引用新依赖无法更新问题


### Documentation


- Update changelog for v0.1.4

- Docker引入依赖无法更新问题


### Other


- 可以拉去取多个节点连线

- 更新设计skills

- 更新设计skills

- 更新skills


## v0.1.4 - 2026-10-02



### Added


- 渠道最大同时生成数，超出的任务在平台排队


### Documentation


- Update changelog for v0.1.3


### Other


- 并发调度文档

- 画布UI重新设计

- 美化画布UI

- 美化首页UI

- 首页进入后台过渡设计


## v0.1.3 - 2026-10-02



### Added


- Feat：新增渠道 Key 建议及升级请求功能

* 实现 ，根据渠道名称及现有 Key 自动生成建议的渠道 Key。

* 新增 ，支持将渠道升级至新版本插件，并管理相关配置。

* 新增渠道和模型的健康检查工具，包括  和 。

* 为 、 和  模块新增相关功能测试。

* 完善批量导入处理逻辑，包括 Key 校验及草稿状态处理。


### Documentation


- Update changelog for v0.1.2


## v0.1.2 - 2026-10-01



### Added


- 编辑模型补齐（一二期）：厂商 Logo 与标签、模型能力结构化

- 编辑模型补齐（三期）：积分定价、生成数量与 Token 结算

- 插件可以预填不同模型的能力参数

- 添加任务提交时间字段，更新相关逻辑和测试


### Changed


- 清理无用注释和空行


### Documentation


- Update changelog for v0.1.1

- 添加readme文档

- 添加模型补齐设计


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


