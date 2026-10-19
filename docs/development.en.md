# Development guide

This guide is for contributors to 连镜 (`video-canvas`). It covers the development environment, the overall architecture, and the checks your changes must pass. For deployment and usage, see the root [README](../README.en.md). Most linked documents are in Chinese.

## Requirements

| Setup              | Requirements                                                                                                  |
| ------------------ | ------------------------------------------------------------------------------------------------------------- |
| Docker development | Docker (with Compose v2)                                                                                      |
| Local development  | Go 1.27, [Bun](https://bun.sh) 1.3, PostgreSQL 16, Redis 7; Node.js 22.19+ if you work on the Canvas Agent runtime |

## Docker development environment

```bash
git clone https://github.com/zaylora/video-canvas.git
cd video-canvas
./scripts/start-docker.sh  # On Windows run scripts/start-docker.bat; add -d to run in the background
```

The first build takes a few minutes. Once it is up:

| Service              | Address                                                      |
| -------------------- | ------------------------------------------------------------ |
| Frontend             | <http://localhost:5173>                                      |
| Backend health check | <http://localhost:8080/health>                               |
| PostgreSQL           | `localhost:15432` (postgres / root, database `video_canvas`) |
| Redis                | `localhost:16379`                                            |

Common commands (logs, running tests inside containers, resetting data) are in [Docker development](docker-development.md).

## Local development

```bash
# Create a local config (gitignored) and adjust the database connection etc.
cp backend/configs/config.yaml backend/configs/config.local.yaml

./scripts/start.sh  # On Windows run scripts/start.bat; Ctrl+C stops both frontend and backend
```

- The script builds the backend and starts Vite. The frontend proxies `/api` and `/files` to `:8080`.
- The backend reads `config.local.yaml` first. Any setting can be overridden with an environment variable named `APP_` plus the upper-cased config path, for example `APP_DATABASE_DSN`.
- Set `redis.enabled: false` to run without Redis; the cache layer then queries the database directly.
- All configuration options are listed in [backend/README.md](../backend/README.md).

## Architecture

```mermaid
flowchart LR
  B[Browser] -->|HTTP / WebSocket| W[web<br/>React + Vite / Nginx]
  W -->|/api /files| S[backend<br/>Gin]
  S --> P[(PostgreSQL)]
  S --> R[(Redis)]
  S --> O[(Local disk / object storage)]
  S -->|generation tasks| K[worker]
  K --> PR[plugin-runner<br/>JS protocol plugins]
  PR --> M[Model providers]
  S <-->|bridge| A[agent runtime<br/>Node]
  A -. web search .-> SX[SearXNG]
  K -. task status .-> S
  S -. WebSocket push .-> B
```

- **Request path**: The backend is layered as `router → middleware → handler → service → repository / cache`.
- **Asynchronous generation**: Submitting a task returns immediately. The worker calls model providers through the plugin-runner, and status updates are pushed to the frontend over WebSocket. After a reconnect, the frontend reconciles tasks to pick up updates it missed.
- **Protocol plugins**: Each provider protocol is a JS plugin running in an isolated plugin-runner process. See [plugin-contract.md](../backend/docs/plugin-contract.md).
- **Canvas Agent**: The Agent loop runs in a separate Node process (`agent/`) that holds no secrets. Model calls, tool execution and state access all go back to the Go backend through a bridge.
- **Canvas saving**: Canvases are saved with `revision`-based optimistic locking; a version conflict returns 409.
- **Production**: Besides the services above, the production Compose file includes an `updater` service for in-app upgrades and rollbacks.

### Tech stack

| Layer       | Technology                                                                                |
| ----------- | ----------------------------------------------------------------------------------------- |
| Frontend    | React 19 · TypeScript · Vite · Tailwind CSS 4 · shadcn/ui · React Flow · zustand · Motion |
| Backend     | Go 1.27 · Gin · GORM · go-redis · Viper · Zap · goja                                      |
| Agent       | Node.js 22 · pi agent                                                                     |
| Storage     | PostgreSQL 16 (required) · Redis 7 · local disk / object storage (OSS · COS · S3 · R2)    |
| Engineering | golangci-lint · oxlint / oxfmt · pre-commit · GitHub Actions · git-cliff                  |

## Project layout

```text
video-canvas/
├── backend/        # Go backend: API, generation worker, plugin-runner, built-in plugins, updater
├── web/            # React frontend: home and conversations, canvas, director's desk, asset library, admin console
├── agent/          # Node runtime for the Canvas Agent
├── docs/           # Deployment docs, development guide, design docs, plans
├── scripts/        # Deploy, update and startup scripts
├── docker-compose.dev.yml   # Development
└── docker-compose.yml       # Production (GHCR images)
```

## Coding standards

| Scope       | Standard                                                                                                     |
| ----------- | ------------------------------------------------------------------------------------------------------------ |
| Repository  | [AGENTS.md](../AGENTS.md): directory responsibilities, testing conventions                                   |
| Backend     | [backend/AGENTS.md](../backend/AGENTS.md) (hard rules) · [backend standards](../backend/docs/standards/README.md) |
| Frontend    | [web/docs/coding-standards.md](../web/docs/coding-standards.md)                                              |
| UI design   | [.agents/skills/ui-design/SKILL.md](../.agents/skills/ui-design/SKILL.md)                                    |

## Tests and checks

Write a failing test that reproduces the requirement or bug before implementing a feature, fix or refactor. Backend tests live in `backend/internal/tests/`, frontend tests in `web/src/tests/`.

Before submitting, make sure these pass:

```bash
# Backend (in backend/)
make fmt && make lint && make test

# Frontend (in web/)
bun run typecheck && bun run lint && bun run format:check && bun test

# Agent runtime (in agent/, only when you change it)
npm test
```

Format frontend code only with the bundled oxfmt (`bun run format`). Do not use Prettier or your editor's formatter, as they restyle unrelated code.

Installing the Git hooks is recommended so these checks run on commit:

```bash
pre-commit install
```

## Commit messages

Use [Conventional Commits](https://www.conventionalcommits.org/en/v1.0.0/) with the module as the scope, for example:

```text
feat(canvas): support node grouping
docs: update deployment notes
```

The changelog is generated from commit messages with git-cliff, so they appear in release notes as written.

## CI and releases

- Pushes to `master` run lint, build and tests for both backend and frontend.
- Migrations only move forward: migration files included in the latest `v*` tag must not be modified, deleted or renamed. Add a new migration instead, or CI will fail.
- Pushing a `v*` tag makes CI check that the tag, `VERSION` and [CHANGELOG.md](../CHANGELOG.md) agree, build the `video-canvas-backend` and `video-canvas-web` images, publish them to GHCR, and then create a GitHub Release.
- So when releasing, update `VERSION` to the target version and commit it together with the changelog before creating and pushing the tag.

## Related documents

| Topic                                 | Document                                                                                                         |
| ------------------------------------- | ---------------------------------------------------------------------------------------------------------------- |
| Backend structure, configuration, API | [backend/README.md](../backend/README.md)                                                                        |
| AI admin API, plugin contract         | [admin-ai-api.md](../backend/docs/admin-ai-api.md) · [plugin-contract.md](../backend/docs/plugin-contract.md)    |
| Docker development / production       | [docker-development.md](docker-development.md) · [docker-production.md](docker-production.md)                    |
| Product and interaction design        | [design/](design/)                                                                                               |
