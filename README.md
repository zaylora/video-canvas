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

### 01 / 无限画布：让创作过程连起来

- 在画布中添加文本、图片、视频和音频节点，并通过连线组织创作流程。
- 将上游节点的生成结果直接作为下游节点的参考，减少反复搬运素材。
- 保留生成历史版本，支持重试、编辑和继续创作。
- 支持图片扩图、视频修剪与视频帧截取，并管理个人素材资产。

### 02 / 3D 导演台：先构图，再生成

- 使用人台、道具与多机位设计场景和镜头构图。
- 将机位画面导出为视频或首帧图片，再送回画布继续创作。

### 03 / 画布 Agent：用对话辅助编排

- 在画布中与 Agent 对话，创建、编辑和排列节点。
- 支持联网搜索、撤销 Agent 对画布的改动，以及导入技能包。

### 04 / 多模型与自托管：保留选择权

- 模型供应商通过 JS 插件接入，并在隔离的 plugin-runner 进程中运行。
- 内置覆盖 OpenAI、Anthropic、Google、豆包、阿里百炼、可灵、即梦、Vidu、海螺、Sora 等供应商的插件，支持上传自定义插件。
- 支持自托管、用户与模型管理、积分结算、素材存储配置及运行维护。

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

### 方式一：在线体验

访问 **[canvas.vbing.site](https://canvas.vbing.site)** 了解连镜。

> 在线站点的开放权限、注册方式和模型额度以实际页面为准。

### 方式二：Docker 部署(推荐)

服务器需要安装 [Docker](https://docs.docker.com/get-docker/)（含 Compose v2）和 `curl`。无需先克隆仓库：

```bash
curl -fsSL https://raw.githubusercontent.com/zaylora/video-canvas/master/scripts/deploy.sh | bash
```

脚本会创建 `video-canvas/` 目录，下载 Compose 配置、生成带随机密钥的 `.env`，并拉取镜像启动服务。默认监听 38080 端口（用 `--port` 修改），完成后访问 `http://{你的ip}:38080`。通过 IP 或域名访问时，需用 `--origin` 指定对应的访问地址，如 `bash -s -- --origin http://{你的ip}:38080`，否则跨域校验会拒绝请求。

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
| 开发环境、架构、测试与发布   | [开发指南](docs/development.md)                                                                         |
| 生产部署                     | [生产 Docker 部署](docs/docker-production.md)                                                           |
| 后端结构、配置项、完整接口表 | [backend/README.md](backend/README.md)                                                                  |
| AI 管理接口、插件契约        | [admin-ai-api.md](backend/docs/admin-ai-api.md) · [plugin-contract.md](backend/docs/plugin-contract.md) |
| 产品与交互设计               | [docs/design/](docs/design/)                                                                            |
| 版本变更                     | [CHANGELOG.md](CHANGELOG.md)                                                                            |

## 参与贡献

欢迎提交 Issue、功能建议和 Pull Request。如果你正在使用连镜，也欢迎分享你的工作流、使用体验或遇到的问题。

开始贡献前，请先阅读 [开发指南](docs/development.md)。

如果连镜对你有帮助，欢迎给项目一个 ⭐ Star，让更多创作者发现它。
## 许可证

[MIT](LICENSE)
