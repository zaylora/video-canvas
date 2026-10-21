<p align="center">
  <picture>
    <source media="(prefers-color-scheme: dark)" srcset="docs/assets/brand/logo-dark.svg" />
    <img src="docs/assets/brand/logo-light.svg" alt="连镜 Logo" width="88" />
  </picture>
</p>

<div align="center">

# 连镜

**开源 AI 视觉创作工作台**

在无限画布中连接灵感、镜头与生成流程。
从文本、图片到视频与音频，让创作过程可以组织、复用和持续迭代。

[在线体验](https://canvas.vbing.site) · [快速部署](#快速开始) · [功能介绍](#核心功能) · [English](README.en.md)

[![License: MIT](https://img.shields.io/badge/License-MIT-black.svg)](LICENSE)
[![GitHub Release](https://img.shields.io/github/v/tag/zaylora/video-canvas?label=version)](https://github.com/zaylora/video-canvas/tags)
[![CI](https://github.com/zaylora/video-canvas/actions/workflows/ci.yml/badge.svg)](https://github.com/zaylora/video-canvas/actions/workflows/ci.yml)

</div>


## 核心功能


**创作**

- **无限画布**：文本、图片、视频、音频节点可以互相连线，上游节点的结果直接作为下游生成的参考。
- **首页对话**：在首页选择模型和参数直接发起生成，结果按对话记录保存，可重试、再编辑，或作为下一次生成的参考素材。
- **素材处理**：图片扩图；图片编辑；视频修剪、截取帧；生成结果保留历史版本，可随时切换。
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

## 项目截图

<table>
  <tr>
    <th width="50%">首页</th>
    <th width="50%">无限画布</th>
  </tr>
  <tr>
    <td><a href="docs/assets/screenshots/%E9%A6%96%E9%A1%B5.png"><img src="docs/assets/screenshots/%E9%A6%96%E9%A1%B5.png" alt="首页" /></a></td>
    <td><a href="docs/assets/screenshots/%E7%94%BB%E5%B8%83.png"><img src="docs/assets/screenshots/%E7%94%BB%E5%B8%83.png" alt="无限画布" /></a></td>
  </tr>
  <tr>
    <th>3D 导演台</th>
    <th>后台管理</th>
  </tr>
  <tr>
    <td><a href="docs/assets/screenshots/%E5%AF%BC%E6%BC%94%E5%8F%B0.png"><img src="docs/assets/screenshots/%E5%AF%BC%E6%BC%94%E5%8F%B0.png" alt="3D 导演台" /></a></td>
    <td><a href="docs/assets/screenshots/%E5%90%8E%E5%8F%B0%E7%AE%A1%E7%90%86.png"><img src="docs/assets/screenshots/%E5%90%8E%E5%8F%B0%E7%AE%A1%E7%90%86.png" alt="后台管理" /></a></td>
  </tr>
</table>


## 快速开始

### Docker 部署

服务器需要安装 [Docker](https://docs.docker.com/get-docker/)（含 Compose v2）和 `curl`。无需先克隆仓库：

```bash
curl -fsSL https://raw.githubusercontent.com/zaylora/video-canvas/master/scripts/deploy.sh | bash
```

完成后访问 `http://{你的ip}:38080`。通过 IP 或域名访问时，需用 `--origin` 指定对应的访问地址，如 `bash -s -- --origin http://{你的ip}:38080`，否则跨域校验会拒绝请求。

> 运行远程脚本前，建议先阅读 [`scripts/deploy.sh`](scripts/deploy.sh) 并确认脚本内容。

### 首次使用

1. 打开页面注册账号。首个注册账号会成为超级管理员。
2. 进入 **管理后台 → 渠道**，选择供应商插件，填写 Base URL 和 API Key，通过连通性检查后保存。
3. 在渠道下拉取或新建模型，完成校验并启用。
4. 返回首页生成素材，或者新建画布，通过节点组织生成流程。

### 更新与生产配置

更新至最新正式版本：

```bash
curl -fsSL https://raw.githubusercontent.com/zaylora/video-canvas/master/scripts/update.sh | bash
```


## 文档

| 内容                         | 文档                                                                                                    |
| ---------------------------- | ------------------------------------------------------------------------------------------------------- |
| 开发环境、架构、测试与发布   | [开发指南](docs/development.md) · [贡献指南](CONTRIBUTING.md)                                            |
| 生产部署                     | [生产 Docker 部署](docs/docker-production.md)                                                           |
| 后端结构、配置项、完整接口表 | [backend/README.md](backend/README.md)                                                                  |
| AI 管理接口、插件契约        | [admin-ai-api.md](backend/docs/admin-ai-api.md) · [plugin-contract.md](backend/docs/plugin-contract.md) |
| 产品与交互设计               | [docs/design/](docs/design/)                                                                            |
| 版本变更                     | [CHANGELOG.md](CHANGELOG.md)                                                                            |

## 参与贡献

欢迎提交 Issue、功能建议和 Pull Request。如果你正在使用连镜，也欢迎分享你的工作流、使用体验或遇到的问题。

开始贡献前，请先阅读 [贡献指南](CONTRIBUTING.md)（如何提 Issue 和 PR）和 [开发指南](docs/development.md)（环境、规范、提交前检查）。

如果连镜对你有帮助，欢迎给项目一个 ⭐ Star，让更多创作者发现它。
## 许可证

[MIT](LICENSE)
