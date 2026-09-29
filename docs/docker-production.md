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
docker compose -f docker-compose.prod.yml pull
docker compose -f docker-compose.prod.yml up -d
```

前端容器负责静态文件，并把 `/api/`、`/api/v1/ws` 和 `/files/` 转发到后端。生产环境使用 S3 时，将 `APP_STORAGE_DRIVER=s3` 以及对应的 `APP_STORAGE_S3_*` 变量加入 `.env`，不要把密钥写入 Compose 文件。
