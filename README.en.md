<p align="center">
  <picture>
    <source media="(prefers-color-scheme: dark)" srcset="docs/assets/brand/logo-dark.svg" />
    <img src="docs/assets/brand/logo-light.svg" alt="连镜 Logo" width="88" />
  </picture>
</p>

<div align="center">

# 连镜

**Open-source AI visual creation workbench**

Connect inspiration, shots and generation flows on an infinite canvas.
From text and images to video and audio, keep your creative process organized, reusable and easy to iterate on.

[Live demo](https://canvas.vbing.site) · [Quick start](#quick-start) · [Features](#features) · [简体中文](README.md)

[![License: MIT](https://img.shields.io/badge/License-MIT-black.svg)](LICENSE)
[![GitHub Release](https://img.shields.io/github/v/tag/zaylora/video-canvas?label=version)](https://github.com/zaylora/video-canvas/tags)
[![CI](https://github.com/zaylora/video-canvas/actions/workflows/ci.yml/badge.svg)](https://github.com/zaylora/video-canvas/actions/workflows/ci.yml)

</div>

## Features

**Creation**

- **Infinite canvas**: Connect text, image, video and audio nodes. The output of an upstream node is used directly as a reference for downstream generation.
- **Home conversations**: Pick a model and parameters on the home page and generate right away. Results are kept as a conversation and can be retried, edited, or used as references for the next generation.
- **Media tools**: Outpaint images; trim videos and capture frames. Every generation keeps its version history, and you can switch between versions at any time.
- **3D director's desk**: Compose shots with mannequins, props and multiple cameras, then export camera views as videos and first-frame images back to the canvas.
- **Canvas Agent**: Chat inside the canvas. The Agent can create, edit and arrange nodes and search the web, and its changes can be undone. Skill packs can be imported.
- **Personal asset library**: Upload media and organize it in folders. Generated results can be added in one click and placed back onto any canvas.

**Generation**

- **Asynchronous tasks**: Submitting returns immediately. Progress is pushed over WebSocket and reconciled automatically after a reconnect. Tasks can be cancelled.
- **Credit settlement**: Credits are frozen on submission, charged on success and refunded on failure or cancellation. Online top-up is supported.
- **Pluggable model providers**: Each provider protocol is a JS plugin running in an isolated plugin-runner process. Built-in plugins cover OpenAI, Anthropic, Google, Doubao, Alibaba Bailian, Kling, Jimeng, Vidu, Hailuo, Sora and more, and you can upload your own.

**Administration and operations**

- **Admin console**: Channel and model configuration (connectivity is checked before saving, and a model can only be enabled after validation), credit pricing, users, payments, usage records, audit logs and a runtime overview.
- **Secret handling**: Channel keys and storage secrets are stored encrypted and are write-only.
- **Asset storage**: Local disk is built in. In production you can add Alibaba Cloud OSS, Tencent Cloud COS, AWS S3 or Cloudflare R2 in the admin console and choose a default. Changing the default only affects new assets; existing assets are always read from the storage they were written to.
- **Conflict-free saving**: Canvases are saved with `revision`-based optimistic locking, so edits from several tabs or devices never overwrite each other.
- **In-app upgrades**: With the updater enabled, a super admin can upgrade to the latest release, or roll back to the version before the last upgrade, from the admin console.

## Screenshots

<table>
  <tr>
    <th width="50%">Home</th>
    <th width="50%">Infinite canvas</th>
  </tr>
  <tr>
    <td><a href="docs/assets/screenshots/%E9%A6%96%E9%A1%B5.png"><img src="docs/assets/screenshots/%E9%A6%96%E9%A1%B5.png" alt="Home" /></a></td>
    <td><a href="docs/assets/screenshots/%E7%94%BB%E5%B8%83.png"><img src="docs/assets/screenshots/%E7%94%BB%E5%B8%83.png" alt="Infinite canvas" /></a></td>
  </tr>
  <tr>
    <th>3D director's desk</th>
    <th>Admin console</th>
  </tr>
  <tr>
    <td><a href="docs/assets/screenshots/%E5%AF%BC%E6%BC%94%E5%8F%B0.png"><img src="docs/assets/screenshots/%E5%AF%BC%E6%BC%94%E5%8F%B0.png" alt="3D director's desk" /></a></td>
    <td><a href="docs/assets/screenshots/%E5%90%8E%E5%8F%B0%E7%AE%A1%E7%90%86.png"><img src="docs/assets/screenshots/%E5%90%8E%E5%8F%B0%E7%AE%A1%E7%90%86.png" alt="Admin console" /></a></td>
  </tr>
</table>

## Quick start

### Deploy with Docker

The server needs [Docker](https://docs.docker.com/get-docker/) (with Compose v2) and `curl`. There is no need to clone the repository:

```bash
curl -fsSL https://raw.githubusercontent.com/zaylora/video-canvas/master/scripts/deploy.sh | bash
```

When it finishes, open `http://{your-ip}:38080`. When accessing through an IP or a domain, pass the matching address with `--origin`, e.g. `bash -s -- --origin http://{your-ip}:38080`; otherwise the cross-origin check rejects requests.

> Before running a remote script, read [`scripts/deploy.sh`](scripts/deploy.sh) and make sure you are comfortable with what it does.

### First run

1. Open the app and register. The first account in the system becomes the super admin.
2. Go to **Admin console → Channels**, choose a provider plugin, enter the Base URL and API key, and save once the connectivity check passes.
3. Fetch or create models under the channel, validate them and enable them.
4. Go back to the home page to generate, or create a canvas and organize the generation flow with nodes.

### Updates and production configuration

To update to the latest release:

```bash
curl -fsSL https://raw.githubusercontent.com/zaylora/video-canvas/master/scripts/update.sh | bash
```

For custom ports, domains, pinned versions, rollback and the full production configuration, see [Production Docker deployment](docs/docker-production.md) (Chinese).

## Documentation

Most documents are currently in Chinese.

| Topic                                     | Document                                                                                                |
| ----------------------------------------- | ------------------------------------------------------------------------------------------------------- |
| Development setup, architecture, releases | [Development guide](docs/development.en.md) · [Contributing](CONTRIBUTING.en.md)                         |
| Production deployment                     | [Production Docker deployment](docs/docker-production.md)                                               |
| Backend structure, configuration, API     | [backend/README.md](backend/README.md)                                                                  |
| AI admin API, plugin contract             | [admin-ai-api.md](backend/docs/admin-ai-api.md) · [plugin-contract.md](backend/docs/plugin-contract.md) |
| Product and interaction design            | [docs/design/](docs/design/)                                                                            |
| Changelog                                 | [CHANGELOG.md](CHANGELOG.md)                                                                            |

## Contributing

Issues, feature requests and pull requests are welcome. If you use 连镜, we would also love to hear about your workflow, your experience and any problems you run into.

Before contributing, read the [contributing guide](CONTRIBUTING.en.md) (how to file issues and open pull requests) and the [development guide](docs/development.en.md) (environment setup, coding standards, pre-submit checks).

If 连镜 is useful to you, a ⭐ Star helps more creators find it.

## License

[MIT](LICENSE)
