# 生产 Docker 部署

GitHub Actions 只在推送 `v*` tag 或发布 GitHub Release 时运行。流水线先执行后端和前端的格式、类型、lint、单元测试检查，全部通过后再构建并推送三个 GHCR 镜像（updater 只在启用一键升级时用到）：

```text
ghcr.io/<GitHub 用户或组织>/video-canvas-backend:<tag>
ghcr.io/<GitHub 用户或组织>/video-canvas-web:<tag>
ghcr.io/<GitHub 用户或组织>/video-canvas-updater:<tag>
```

## 发布

```bash
git tag v1.0.0
git push origin v1.0.0
```

发布完成后，镜像会有 `1.0.0`、`1.0` 和 `latest` 标签。

## 启动

在部署机器上准备环境变量（建议放在 Compose 同目录的 `.env`，不要提交）：

```dotenv
IMAGE_OWNER=your-github-owner
IMAGE_TAG=1.0.0
POSTGRES_PASSWORD=change-this
APP_JWT_SECRET=change-this
APP_AI_SECRET_KEY=change-this
APP_SERVER_ALLOWED_ORIGINS=https://canvas.example.com
WEB_PORT=38080
```

登录 GHCR 后启动：

```bash
docker login ghcr.io
docker compose pull
docker compose up -d
```

前端容器负责静态文件，并把 `/api/`、`/api/v1/ws` 和 `/files/` 转发到后端。素材默认存在容器的本地磁盘卷里。生产环境建议接对象存储：以超级管理员登录后台，在「系统设置 → 存储配置」里添加阿里云 OSS / 腾讯云 COS / AWS S3 / Cloudflare R2，测试连接通过后设为默认即可，不需要改环境变量也不需要重启；密钥用 `APP_AI_SECRET_KEY` 加密后存库，页面上只写不读。换存储只影响新素材，旧素材仍从原存储读取。

> 从旧版升级：如果之前用 `APP_STORAGE_DRIVER=s3` 加 `APP_STORAGE_S3_*` 环境变量配置过对象存储（并且这些变量确实传进了后端容器），升级后首次启动会自动把它导入为一套名为「迁移自环境变量」的存储并设为默认，旧素材继续可用；导入后可以删掉这些环境变量。导入需要已配置 `APP_AI_SECRET_KEY`，否则后端会拒绝启动并说明原因。

## 升级与数据库迁移

数据库结构由后端镜像里内嵌的迁移文件决定。新版本的 backend 启动时会先执行还没执行过的迁移，成功后才开始提供服务；迁移失败时进程直接退出，日志里有具体的 SQL 错误，不会带着没迁移完的库运行。从接入迁移之前的老版本（v0.3.2 及更早）升级时，第一次启动会先把旧库补齐到基线结构，不需要手工处理。

推荐用部署目录里的 `update.sh` 升级（`deploy.sh` 部署时已下载；也可以 `curl -fsSL https://raw.githubusercontent.com/zaylora/video-canvas/master/scripts/update.sh -o update.sh`）：

```bash
./update.sh               # 升级到 GitHub 上最新的正式版本
./update.sh --tag 0.4.0   # 升级或回退到指定版本
```

它依次执行：改 `IMAGE_TAG` → 取目标版本的 compose 文件 → 拉镜像 → **备份数据库**到 `backups/`（保留最近 3 份）→ 重建 → 等 `/health` 报告目标版本。任何一步失败都会自动退回原来的版本；`./update.sh --help` 查看全部选项。`IMAGE_TAG=latest` 的部署无法确认版本、也无法自动回退，建议改成具体版本号。

不用脚本时，先手动备份（迁移只向前，没有自动回滚），再把 `.env` 里的 `IMAGE_TAG` 改成目标版本，执行 `docker compose pull && docker compose up -d`：

```bash
docker compose exec -T postgres pg_dump -U postgres -Fc video_canvas > backup-$(date +%Y%m%d%H%M).dump
```

### 后台一键升级（可选）

启用后，超级管理员可以在后台「系统设置 → 版本与升级」里点按钮升级或回退，流程和 `update.sh` 完全相同（它就是在 updater 容器里执行 `update.sh`）。updater 挂载 docker.sock，**等同服务器 root 权限**，所以默认不启动：

```bash
# 在部署目录执行；deploy.sh 新部署的 .env 里已经有 UPDATER_TOKEN，老部署先补一行
grep -q '^UPDATER_TOKEN=' .env || echo "UPDATER_TOKEN=$(openssl rand -hex 32)" >> .env
docker compose up -d                      # 让 backend 读到 token
docker compose --profile updater up -d    # 启动 updater
```

- updater 必须在宿主机的部署目录里启动：它把当前目录挂载为 `/deploy`。
- 一键升级只能升到后台检查到的最新正式版本；回退只能退到最近一次一键升级之前的版本。部署用 `IMAGE_TAG=latest` 时不能一键升级。
- 升级时 updater 自己不会被重建；要升级 updater 本身，在部署目录再执行一次 `docker compose --profile updater up -d`。
- 停用：`docker compose --profile updater rm -sf updater`。

### Agent 独立服务（可选）

默认情况下画布 Agent 跑在 backend 容器里：每个运行片段起一个 Node 子进程，跑完退出。可以改成把 Agent 拆成独立的常驻服务（`agent` 容器，镜像 `video-canvas-agent`），好处是：

- Agent 可以单独发版、单独升级 pi，不用重新发 backend；
- backend 重启（比如一键升级）时，正在跑的运行不会被打断：它们由 agent 容器继续跑，backend 起来后桥地址不变，调用照常；
- backend 镜像以后可以不再带 Node。

只在 compose 内网提供服务，不对外开放任何端口。backend 和 agent 之间用同一个服务令牌互相认，令牌是唯一的凭据，请当作密码保管。

```bash
# 在部署目录执行，三行都要
echo "AGENT_SERVICE_TOKEN=$(openssl rand -hex 32)" >> .env   # 至少 32 个字符
echo "APP_AGENT_RUNTIME=http" >> .env
echo "COMPOSE_PROFILES=agent" >> .env
docker compose pull && docker compose up -d
```

- **一定要用 `COMPOSE_PROFILES=agent` 启用，不要每次手敲 `--profile agent`。** `update.sh` 和后台一键升级用的是不带 `--profile` 的 `docker compose pull/up`，只有这样 agent 才会跟着一起升级；否则升级后 backend 是新版、agent 还是旧版，两边的协议只在同一个版本里保证兼容。
- 确认生效：`docker compose ps` 里有 `agent` 且状态是 healthy；backend 启动日志里「画布 Agent 桥已启动」那一行带 `"runtime": "http"`。
- 回到旧方式：从 `.env` 里删掉 `APP_AGENT_RUNTIME=http` 和 `COMPOSE_PROFILES=agent`，再执行 `docker compose up -d --remove-orphans`。令牌可以留着。
- **重启 agent 容器会中断正在跑的运行**（它们会显示「已中断」，用户可以点「继续」）。停止时服务有最多 30 秒收尾；backend 重启则不影响。
- 并发和内存：一个 agent 进程里同时跑多个运行，默认最多 64 个（`AGENT_MAX_SEGMENTS`），容器内存上限 2g（`AGENT_MEM_LIMIT`）。这两个值是按经验定的，上线后看后台运行记录里的并发数再调。
- 现在只支持**一个** agent 容器：不要把它扩成多副本。
- agent 容器不访问数据库、不持有任何渠道密钥；模型调用、画布读写、计费、素材都经 backend。

### 联网工具与搜索（可选）

画布 Agent 可以联网查资料、读网页（`web_search`、`web_fetch`）。请求都由 backend 发出，Agent 进程自己不联网；目标是本机或内网地址时一律拒绝，每个运行片段最多联网 20 次。

- **开关**：默认开启；`.env` 里设 `APP_AGENT_WEB_ENABLED=false` 可以关闭，关闭后模型看不到这两个工具。
- **搜索源**：默认依次试 Exa（公开的免 Key 接口）、Bing 网页、DuckDuckGo 网页，都不需要 API Key。后两个是抓搜索引擎网页：实测 DuckDuckGo 对数据中心的出口会直接返回人机验证页，Bing 对自动化请求返回的结果相关性很差，所以实际靠 Exa；它是 Exa 托管的免费额度，会限流。**生产环境建议部署 SearXNG**（聚合多个引擎，自己掌控）：

```bash
echo 'APP_AGENT_SEARXNG_URL=http://searxng:8080' >> .env
docker compose --profile search up -d      # 启动 searxng；backend 需要读到新的环境变量，再执行一次 docker compose up -d
```

- 配了 `APP_AGENT_SEARXNG_URL` 后，它排在搜索源的第一位，失败或没结果再依次试 Exa、Bing、DuckDuckGo。
- 搜索词会发给第三方（Exa、Bing 等搜索服务）。系统提示词已要求 Agent 不要把用户未公开的剧本、台词放进搜索词，但这不是技术上的保证；有保密要求的部署请关闭联网。

查看迁移状态（只读）：

```bash
docker compose run --rm --no-deps backend migrate status
```

退回旧版本镜像时，如果新版本已经执行过迁移，旧程序会拒绝启动并提示「数据库已执行过本程序没有的迁移」。确认那些迁移只是新增表或列（向后兼容）后，可以设置 `APP_DATABASE_ALLOW_NEWER_SCHEMA=true` 让旧版本照常运行；否则用升级前的备份恢复：

```bash
docker compose stop backend plugin-runner
docker compose exec -T postgres pg_restore -U postgres -d video_canvas --clean --if-exists < backup-xxx.dump
```
