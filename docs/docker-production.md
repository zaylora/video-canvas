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
WEB_PORT=80
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

查看迁移状态（只读）：

```bash
docker compose run --rm --no-deps backend migrate status
```

退回旧版本镜像时，如果新版本已经执行过迁移，旧程序会拒绝启动并提示「数据库已执行过本程序没有的迁移」。确认那些迁移只是新增表或列（向后兼容）后，可以设置 `APP_DATABASE_ALLOW_NEWER_SCHEMA=true` 让旧版本照常运行；否则用升级前的备份恢复：

```bash
docker compose stop backend plugin-runner
docker compose exec -T postgres pg_restore -U postgres -d video_canvas --clean --if-exists < backup-xxx.dump
```
