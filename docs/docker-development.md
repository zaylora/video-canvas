# Docker 开发环境

项目提供 `docker-compose.dev.yml`，启动 PostgreSQL、Redis、Go 后端和 Vite 前端。

## 启动

```bash
docker compose -f docker-compose.dev.yml up --build
```

也可以直接运行根目录脚本：

```bash
# macOS / Linux
./start-docker.sh

# Windows CMD 或 PowerShell
start-docker.bat
```

后台启动：

```bash
./start-docker.sh --detach
start-docker.bat --detach
```

启动后访问：

- 前端：<http://localhost:5173>
- 后端健康检查：<http://localhost:8080/health>
- PostgreSQL：`localhost:5432`，用户 `postgres`，密码 `root`，数据库 `video_canvas`
- Redis：`localhost:6379`

前端容器里的 Vite 代理通过 `http://backend:8080` 访问后端；浏览器仍然使用同源的 `/api/v1` 和 WebSocket 地址。后端通过 `APP_DATABASE_DSN`、`APP_REDIS_ADDR` 等环境变量连接 Compose 服务名，不要在容器配置里使用 `127.0.0.1`。

## 常用命令

```bash
# 后台启动
docker compose -f docker-compose.dev.yml up -d --build

# 查看日志
docker compose -f docker-compose.dev.yml logs -f backend web

# 执行后端测试
docker compose -f docker-compose.dev.yml exec backend go test ./...

# 执行前端检查
docker compose -f docker-compose.dev.yml exec web bun run typecheck

# 停止容器，保留数据库、Redis 和上传素材
docker compose -f docker-compose.dev.yml down

# 连数据卷一起删除（会清空本地开发数据）
docker compose -f docker-compose.dev.yml down -v
```

## 开发行为

- `backend/` 和 `web/` 以 bind mount 挂载，代码修改会立即进入容器。
- 前端使用 Vite HMR。
- 后端使用 Air 监听 Go 和 YAML 文件，修改后自动重新编译并重启。
- 上传素材保存在命名卷 `backend_assets` 中，容器重启不会丢失。
- Compose 中的 JWT 和 AI 主密钥只用于本地开发；生产环境应通过密钥管理系统或部署平台注入。
