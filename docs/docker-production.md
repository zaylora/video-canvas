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

### Agent 服务

画布 Agent 是独立的常驻服务（`agent` 容器，镜像 `video-canvas-agent`，Pi Durable + SQLite），**默认随 Compose 一起启动**：

- 会话、运行、审批、事件和对话历史都在 agent 容器自己的数据里（`agent_data` 卷）；backend 负责登录校验、反向代理前端的接口，并提供平台能力（画布读写、模型代理和计费、生成任务、素材）；
- 服务重启后没跑完的运行，停机时间不超过 `AGENT_RESUME_WINDOW_SECONDS`（默认 300 秒）会自动续跑，界面显示「恢复中」，超过就标为「已中断」由用户点「继续」。backend 重启不影响运行；
- 可以单独发版、跟上 Pi 的更新；backend 镜像里没有 Node。

只在 compose 内网提供服务，不对外开放任何端口。backend 和 agent 之间用同一个服务令牌（`.env` 里的 `AGENT_SERVICE_TOKEN`）互相认，令牌是唯一的凭据，请当作密码保管。**`deploy.sh` 新部署时会生成它；`update.sh`（包括后台一键升级）从没有独立 Agent 服务的旧版本升上来时，发现 `.env` 里没有就自动生成一个**（已有的不会动）。

- **确认生效**：`docker compose ps` 里有 `agent` 且状态是 healthy；backend 启动日志里有「画布 Agent 平台 API 已启动」。agent 的健康检查还包含一项自检：它启动后会向 backend 核对「对外 ID 的编码密钥」是否一致，**不一致时报 unhealthy**（一键升级会因此自动回退），日志里会说明怎么改。backend 默认使用镜像里配置文件的 `id_key`，agent 的 `AGENT_ID_KEY` 默认也是 `id_key`；只有你改过 backend 的密钥才需要在 `.env` 里同步设 `AGENT_ID_KEY`。
- **不用 Agent**：`.env` 里设 `APP_AGENT_ENABLED=false`，画布里的 Agent 会提示「暂不可用」（agent 容器仍会启动，空闲时几乎不占资源）。还要在后台「AI 配置」里发布 agent 类型的模型才有可用模型。
- **旧会话不会迁移**：从旧版本（Agent 跑在 backend 容器里）升上来之后，画布上看到的是 agent 服务里的新会话；之前保存在 backend 数据库里的旧会话不再显示（数据还在，后台的运行记录、导出和统计仍可查旧运行）。
- **后台的「运行记录」、导出、统计**：新的运行和旧的运行在同一个列表里；新的运行比实际结束晚几秒（最多约 10 秒）出现在列表里，评价和撤销之后摘要也是这样更新。
- **重启 agent 容器**：没跑完的运行按上面的窗口续跑。停止时服务有最多 30 秒收尾，不会中止运行。
- 现在只支持**一个** agent 容器：不要把它扩成多副本（SQLite 单写者）。`agent_data` 是本地卷，不要换成网络盘。
- agent 容器不访问 Postgres、不持有任何渠道密钥；模型调用（含计费）、画布读写、生成任务、素材都经 backend。

**升级与备份**

`update.sh`（包括后台一键升级）除了 Postgres 的 `pre-<版本>-<时间>.dump`，还会**在线**备份 agent 的数据到 `backups/pre-<版本>-<时间>.agent.sqlite`：不用停机、不打断运行，用目标版本的镜像读 `agent_data` 卷，备份完做完整性检查，两种备份各保留最近 `--keep-backups` 份。**备份失败会中止升级**（服务保持原样）；卷里还没有数据（第一次升级到有 Agent 服务的版本）时不算失败。Agent 的数据只向前升级（迁移只新增），所以升级前备份。

升级失败会自动退回旧版本。如果新版本已经升级过 agent 的数据、旧镜像因此拒绝启动（日志里是「数据库的版本 N 比当前代码新」），`update.sh` 会报「需要人工处理」并指出备份文件：换回新版镜像，或按下面的步骤恢复。

**恢复 agent 的数据**（在部署目录执行；先停 agent，恢复后再起）：

```bash
docker compose stop agent
docker compose run --rm --no-deps -T --entrypoint sh agent -c \
  'rm -f /data/agent.sqlite /data/agent.sqlite-wal /data/agent.sqlite-shm && cat > /data/agent.sqlite' \
  < backups/pre-0.4.0-20261026120000.agent.sqlite      # 换成要恢复的备份文件
docker compose up -d agent
```

手动备份（随时可以做，服务不停）：

```bash
docker compose run --rm --no-deps -T --entrypoint sh agent -c \
  'AGENT_DB_PATH=/data/agent.sqlite node dist/backup-cli.js /tmp/b.sqlite 1>&2 && cat /tmp/b.sqlite' > backups/manual-$(date +%Y%m%d%H%M%S).agent.sqlite
```

**从旧版本升级后回退到旧版本**：用 `update.sh --tag <旧版本>` 回退镜像即可，旧版本的 Agent 跑在 backend 容器里，看到的是 backend 数据库里的旧会话；agent 服务里的新会话留在 `agent_data` 卷里（再升级回来还在）。**没跑完的新运行在退回时不会自动续跑**，退回前先等它们结束。想彻底清掉 agent 的数据：`docker compose down` 后 `docker volume rm <项目名>_agent_data`（先确认已备份）。

**已知限制**：agent 的事件和会话目前没有保留期清理，数据只增不减，按用量规划 `agent_data` 卷的大小；备份文件也会随之变大。

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
