<p align="center">
  <picture>
    <source media="(prefers-color-scheme: dark)" srcset="docs/assets/brand/logo-dark.svg" />
    <img src="docs/assets/brand/logo-light.svg" alt="连镜 Logo" width="88" />
  </picture>
</p>

<h1 align="center">连镜</h1>

<p align="center">面向 AI 视频创作的无限画布：在节点之间组织文本、图片、视频和音频的生成，自部署，模型供应商可插拔。</p>

<p align="center"><strong>简体中文</strong> · <a href="README.en.md">English</a></p>

<p align="center">
  <a href="CHANGELOG.md"><img src="https://img.shields.io/github/v/tag/zaylora/video-canvas?label=version" alt="Version" /></a>
  <a href="LICENSE"><img src="https://img.shields.io/badge/license-MIT-blue.svg" alt="License: MIT" /></a>
  <a href="https://github.com/zaylora/video-canvas/actions/workflows/ci.yml"><img src="https://github.com/zaylora/video-canvas/actions/workflows/ci.yml/badge.svg" alt="CI" /></a>
  <img src="https://img.shields.io/badge/Go-1.27-00ADD8" alt="Go" />
  <img src="https://img.shields.io/badge/React-19-61DAFB" alt="React" />
</p>

## 功能

**创作**

- **无限画布**：文本、图片、视频、音频节点可以互相连线，上游节点的结果直接作为下游生成的参考。
- **首页对话**：在首页选择模型和参数直接发起生成，结果按对话记录保存，可重试、再编辑，或作为下一次生成的参考素材。
- **素材处理**：图片扩图；视频修剪、截取帧；生成结果保留历史版本，可随时切换。
- **3D 导演台**：用人台、道具和多机位摆出构图，把机位画面导出为视频和首帧图片，再放回画布。
- **画布 Agent**：在画布中对话，Agent 可以创建、编辑和排列节点，联网搜索资料，改动支持撤销；可导入技能包。
- **个人资产库**：上传素材、按文件夹整理，生成结果可一键入库，也可以从资产库添加到画布。

**生成**

- **异步任务**：提交后立即返回，进度通过 WebSocket 推送，断线重连后自动对账；任务可取消。
- **积分结算**：提交时冻结积分，成功后扣除，失败或取消时退回；支持在线充值。
- **插件化接入模型**：每种供应商协议是一个 JS 插件，运行在隔离的 plugin-runner 进程中。内置 OpenAI、Anthropic、Google、豆包、阿里百炼、可灵、即梦、Vidu、海螺、Sora 等插件，也可以上传自定义插件。

**管理与运维**

- **管理后台**：渠道与模型配置（保存前做连通性检查，校验通过后才能启用）、积分定价、用户、支付、使用记录、操作审计和运行总览。
- **密钥安全**：渠道 Key 和存储密钥加密保存，只能写入，不能读出。
- **素材存储**：内置本地磁盘；生产环境可在后台添加阿里云 OSS、腾讯云 COS、AWS S3、Cloudflare R2，并指定默认存储。切换默认存储只影响新素材，已有素材仍从原存储读取。
- **保存不冲突**：画布按 `revision` 乐观锁保存，多个标签页或设备同时编辑时不会互相覆盖。
- **在线升级**：启用 updater 后，超级管理员可以在后台一键升级到最新正式版本，或回退到升级前的版本。

## 快速开始

### 部署

服务器需要 [Docker](https://docs.docker.com/get-docker/)（含 Compose v2）和 curl，无需克隆仓库：

```bash
curl -fsSL https://raw.githubusercontent.com/zaylora/video-canvas/master/scripts/deploy.sh | bash
```

脚本会在当前目录下创建 `video-canvas/`，下载 `docker-compose.yml`，生成带随机密钥的 `.env`，拉取 GHCR 镜像并启动。启动完成后访问 <http://localhost>。

更新到最新版本：

```bash
curl -fsSL https://raw.githubusercontent.com/zaylora/video-canvas/master/scripts/update.sh | bash
```

<details>
<summary>自定义端口、域名和版本，回退及注意事项</summary>

```bash
# 自定义端口 / 访问地址 / 固定版本 / 部署目录
curl -fsSL https://raw.githubusercontent.com/zaylora/video-canvas/master/scripts/deploy.sh | bash -s -- \
  --port 8080 --origin https://canvas.example.com --tag 0.1.6 --dir /opt/video-canvas

# 更新时指定目录，或回退到指定版本
curl -fsSL https://raw.githubusercontent.com/zaylora/video-canvas/master/scripts/update.sh | bash -s -- --dir /opt/video-canvas --tag 0.1.5
```

- 部署目录已有 `.env` 时，脚本直接沿用，不会重新生成密钥。
- 请备份 `.env`。`APP_AI_SECRET_KEY` 丢失后，已加密保存的模型密钥和存储密钥将无法解密。
- 默认部署和更新到 GitHub 上最新的正式版本，并把具体版本号写入 `.env`。
- 更新前会把数据库备份到部署目录的 `backups/`（保留最近 3 份）。新版本未能正常启动，或启动的不是目标版本时，自动回滚到更新前的版本。
- 镜像为私有时，先执行 `docker login ghcr.io`。
- 完整的生产配置见 [生产 Docker 部署](docs/docker-production.md)。

</details>

### 首次使用

刚部署完没有可用模型，需要先完成以下配置：

1. 打开页面注册账号。系统中的第一个账号自动成为超级管理员。
2. 进入 **管理后台 → 渠道**，新建渠道：选择对应的内置插件，填写 Base URL 和 API Key，通过连通性检查后保存。
3. 在渠道下拉取或新建模型，校验通过后启用。启用后，普通用户才能看到并使用该模型。
4. 回到首页发起生成，或新建画布、添加节点后生成。新用户默认有 50 积分，可在 **管理后台 → 系统设置 → 注册设置** 中修改。

## 文档

| 内容                         | 文档                                                                                                    |
| ---------------------------- | ------------------------------------------------------------------------------------------------------- |
| 开发环境、架构、测试与发布   | [开发指南](docs/development.md)                                                                         |
| 生产部署                     | [生产 Docker 部署](docs/docker-production.md)                                                           |
| 后端结构、配置项、完整接口表 | [backend/README.md](backend/README.md)                                                                  |
| AI 管理接口、插件契约        | [admin-ai-api.md](backend/docs/admin-ai-api.md) · [plugin-contract.md](backend/docs/plugin-contract.md) |
| 产品与交互设计               | [docs/design/](docs/design/)                                                                            |
| 版本变更                     | [CHANGELOG.md](CHANGELOG.md)                                                                            |

## 参与贡献

欢迎提交 Issue 和 Pull Request。开发环境搭建、代码规范、提交前检查和提交信息格式见 [开发指南](docs/development.md)。

## 许可证

[MIT](LICENSE)
