<p align="center">
  <picture>
    <source media="(prefers-color-scheme: dark)" srcset="docs/assets/brand/logo-dark.svg" />
    <img src="docs/assets/brand/logo-light.svg" alt="连镜 logo" width="88" />
  </picture>
</p>

<h1 align="center">连镜</h1>

<p align="center">A self-hosted infinite canvas for AI video creation: orchestrate text, image, video and audio generation across connected nodes, with pluggable model providers.</p>

<p align="center"><a href="README.md">简体中文</a> · <strong>English</strong></p>

<p align="center">
  <a href="CHANGELOG.md"><img src="https://img.shields.io/github/v/tag/zaylora/video-canvas?label=version" alt="Version" /></a>
  <a href="LICENSE"><img src="https://img.shields.io/badge/license-MIT-blue.svg" alt="License: MIT" /></a>
  <a href="https://github.com/zaylora/video-canvas/actions/workflows/ci.yml"><img src="https://github.com/zaylora/video-canvas/actions/workflows/ci.yml/badge.svg" alt="CI" /></a>
  <img src="https://img.shields.io/badge/Go-1.27-00ADD8" alt="Go" />
  <img src="https://img.shields.io/badge/React-19-61DAFB" alt="React" />
</p>

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

## Quick start

### Deploy

The server needs [Docker](https://docs.docker.com/get-docker/) (with Compose v2) and curl. There is no need to clone the repository:

```bash
curl -fsSL https://raw.githubusercontent.com/zaylora/video-canvas/master/scripts/deploy.sh | bash
```

The script creates `video-canvas/` in the current directory, downloads `docker-compose.yml`, generates a `.env` with random secrets, pulls the images from GHCR and starts the stack. Once it is ready, open <http://localhost>.

To update to the latest version:

```bash
curl -fsSL https://raw.githubusercontent.com/zaylora/video-canvas/master/scripts/update.sh | bash
```

<details>
<summary>Custom port, domain and version; rollback; notes</summary>

```bash
# Custom port / public origin / pinned version / install directory
curl -fsSL https://raw.githubusercontent.com/zaylora/video-canvas/master/scripts/deploy.sh | bash -s -- \
  --port 8080 --origin https://canvas.example.com --tag 0.1.6 --dir /opt/video-canvas

# Update a specific directory, or roll back to a given version
curl -fsSL https://raw.githubusercontent.com/zaylora/video-canvas/master/scripts/update.sh | bash -s -- --dir /opt/video-canvas --tag 0.1.5
```

- If the install directory already has a `.env`, the script reuses it and does not regenerate secrets.
- Back up your `.env`. If `APP_AI_SECRET_KEY` is lost, the encrypted model and storage secrets can no longer be decrypted.
- By default the scripts deploy and update to the latest GitHub release and write that exact version into `.env`.
- Before updating, the database is backed up to `backups/` in the install directory (the last 3 backups are kept). If the new version fails to start, or does not report the target version, the script rolls back automatically.
- If the images are private, run `docker login ghcr.io` first.
- For the full production configuration, see [Production Docker deployment](docs/docker-production.md) (Chinese).

</details>

### First run

A fresh deployment has no models available. Complete these steps first:

1. Open the app and register. The first account in the system becomes the super admin automatically.
2. Go to **Admin console → Channels** and create a channel: choose a built-in plugin, enter the Base URL and API key, and save once the connectivity check passes.
3. Fetch or create models under the channel and enable them after validation. Regular users can only see and use enabled models.
4. Go back to the home page and generate, or create a canvas, add a node and generate there. New users get 50 credits by default; change this under **Admin console → System settings → Registration**.

## Documentation

Most documents are currently in Chinese.

| Topic                                     | Document                                                                                                |
| ----------------------------------------- | ------------------------------------------------------------------------------------------------------- |
| Development setup, architecture, releases | [Development guide](docs/development.en.md)                                                             |
| Production deployment                     | [Production Docker deployment](docs/docker-production.md)                                               |
| Backend structure, configuration, API     | [backend/README.md](backend/README.md)                                                                  |
| AI admin API, plugin contract             | [admin-ai-api.md](backend/docs/admin-ai-api.md) · [plugin-contract.md](backend/docs/plugin-contract.md) |
| Product and interaction design            | [docs/design/](docs/design/)                                                                            |
| Changelog                                 | [CHANGELOG.md](CHANGELOG.md)                                                                            |

## Contributing

Issues and pull requests are welcome. See the [development guide](docs/development.en.md) for environment setup, coding standards, pre-submit checks and commit message format.

## License

[MIT](LICENSE)
