#!/usr/bin/env bash
# Docker 开发环境启动脚本（macOS / Linux）
set -euo pipefail

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
COMPOSE_FILE="$ROOT/docker-compose.dev.yml"
BUILD=true
DETACH=false

for arg in "$@"; do
  case "$arg" in
    --no-build)
      BUILD=false
      ;;
    -d|--detach)
      DETACH=true
      ;;
    -h|--help)
      cat <<'EOF'
用法：./scripts/start-docker.sh [选项]

选项：
  -d, --detach   后台启动
  --no-build     不重新构建镜像
  -h, --help     显示帮助
EOF
      exit 0
      ;;
    *)
      echo "[错误] 不支持的参数：$arg" >&2
      exit 2
      ;;
  esac
done

command -v docker >/dev/null 2>&1 || {
  echo "[错误] 未找到 Docker，请先安装 Docker Desktop。" >&2
  exit 1
}

if [ ! -f "$COMPOSE_FILE" ]; then
  echo "[错误] 未找到 $COMPOSE_FILE" >&2
  exit 1
fi

if ! docker info >/dev/null 2>&1; then
  if [ -d "/Applications/Docker.app" ]; then
    echo "==> Docker Desktop 未运行，正在启动"
    open -a Docker
    for _ in $(seq 1 30); do
      if docker info >/dev/null 2>&1; then
        break
      fi
      sleep 2
    done
  fi
fi

if ! docker info >/dev/null 2>&1; then
  echo "[错误] Docker daemon 未运行，请启动 Docker Desktop 后重试。" >&2
  exit 1
fi

cd "$ROOT"

if [ "$BUILD" = true ]; then
  echo "==> 构建 video-canvas Docker 镜像"
  docker compose -f "$COMPOSE_FILE" build
fi

# 构建会产生旧的悬空镜像；只清理未被标签引用的镜像，不影响运行中的服务。
echo "==> 清理悬空 Docker 镜像"
docker image prune --force

CMD=(docker compose -f "$COMPOSE_FILE" up --remove-orphans)
[ "$DETACH" = true ] && CMD+=(-d)

echo "==> 启动 video-canvas Docker 开发环境"
"${CMD[@]}"

if [ "$DETACH" = true ]; then
  echo
  echo "前端：http://localhost:5173"
  echo "后端：http://localhost:8080/health"
  echo "日志：docker compose -f docker-compose.dev.yml logs -f backend web"
fi
