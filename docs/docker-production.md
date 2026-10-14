# 生产 Docker 部署

GitHub Actions 只在推送 `v*` tag 或发布 GitHub Release 时运行。流水线先执行后端和前端的格式、类型、lint、单元测试检查，全部通过后再构建并推送两个 GHCR 镜像：

```text
ghcr.io/<GitHub 用户或组织>/video-canvas-backend:<tag>
ghcr.io/<GitHub 用户或组织>/video-canvas-web:<tag>
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

升级前先备份数据库（迁移只向前，没有自动回滚）：

```bash
docker compose exec -T postgres pg_dump -U postgres -Fc video_canvas > backup-$(date +%Y%m%d%H%M).dump
```

然后把 `.env` 里的 `IMAGE_TAG` 改成目标版本，执行 `docker compose pull && docker compose up -d`。

查看迁移状态（只读）：

```bash
docker compose run --rm --no-deps backend migrate status
```

退回旧版本镜像时，如果新版本已经执行过迁移，旧程序会拒绝启动并提示「数据库已执行过本程序没有的迁移」。确认那些迁移只是新增表或列（向后兼容）后，可以设置 `APP_DATABASE_ALLOW_NEWER_SCHEMA=true` 让旧版本照常运行；否则用升级前的备份恢复：

```bash
docker compose stop backend plugin-runner
docker compose exec -T postgres pg_restore -U postgres -d video_canvas --clean --if-exists < backup-xxx.dump
```
