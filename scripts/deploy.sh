#!/usr/bin/env bash
# 一键部署（Linux / macOS），无需克隆仓库：
#   curl -fsSL https://raw.githubusercontent.com/zaylora/video-canvas/master/scripts/deploy.sh | bash
# 可选参数：bash -s -- --dir PATH --port 8080 --origin https://canvas.example.com --tag 0.1.6
set -euo pipefail

RAW="https://raw.githubusercontent.com/zaylora/video-canvas/master/scripts"
DIR="$PWD/video-canvas"
ORIGIN="http://localhost"
TAG="latest"
PORT="80"

while [ $# -gt 0 ]; do
  case "$1" in
    --dir)    DIR="${2:?--dir 需要参数}"; shift 2 ;;
    --origin) ORIGIN="${2:?--origin 需要参数}"; shift 2 ;;
    --tag)    TAG="${2:?--tag 需要参数}"; shift 2 ;;
    --port)   PORT="${2:?--port 需要参数}"; shift 2 ;;
    -h|--help)
      echo "用法：deploy.sh [--dir 部署目录] [--port Web端口] [--origin 访问地址] [--tag 镜像版本]"
      echo "默认：目录 ./video-canvas，端口 80，版本 latest。镜像为私有时先 docker login ghcr.io"
      exit 0 ;;
    *) echo "[错误] 不支持的参数：$1" >&2; exit 2 ;;
  esac
done

command -v curl >/dev/null 2>&1 || { echo "[错误] 需要 curl。" >&2; exit 1; }
command -v docker >/dev/null 2>&1 || { echo "[错误] 未找到 Docker，请先安装。" >&2; exit 1; }
docker compose version >/dev/null 2>&1 || { echo "[错误] 需要 Docker Compose v2。" >&2; exit 1; }
docker info >/dev/null 2>&1 || { echo "[错误] Docker daemon 未运行。" >&2; exit 1; }

rand() { openssl rand -hex 32 2>/dev/null || head -c 32 /dev/urandom | od -An -tx1 | tr -d ' \n'; }

mkdir -p "$DIR"
cd "$DIR"

echo "==> 部署目录：$DIR"
curl -fsSL "$RAW/docker-compose.yml" -o docker-compose.yml
curl -fsSL "$RAW/update.sh" -o update.sh && chmod +x update.sh || echo "[提示] 未能下载 update.sh，已跳过。" >&2

if [ -f .env ]; then
  echo "==> 沿用已有 .env（不重新生成密钥）"
else
  echo "==> 生成 .env"
  (umask 077; cat > .env <<EOF
POSTGRES_PASSWORD=$(rand)
APP_JWT_SECRET=$(rand)
APP_AI_SECRET_KEY=$(rand)
APP_SERVER_ALLOWED_ORIGINS=$ORIGIN
IMAGE_TAG=$TAG
WEB_PORT=$PORT
EOF
)
  echo "    请备份 $DIR/.env：APP_AI_SECRET_KEY 丢失后，已加密保存的密钥无法解密。"
fi

echo "==> 拉取镜像并启动"
docker compose pull
docker compose up -d --remove-orphans

echo "==> 等待服务就绪"
for _ in $(seq 1 60); do
  bad="$(docker compose ps --format '{{.Service}} {{.Health}}' | awk '$2 != "" && $2 != "healthy" {print $1}')"
  if [ -z "$bad" ] && docker compose ps --status running --services | grep -qx web; then
    echo
    echo "部署完成：http://localhost:$(grep -E '^WEB_PORT=' .env | cut -d= -f2)"
    echo "更新：cd $DIR && ./update.sh    日志：cd $DIR && docker compose logs -f backend web"
    exit 0
  fi
  sleep 3
done

echo "[错误] 3 分钟内服务未全部就绪：cd $DIR && docker compose logs --tail=100 backend plugin-runner" >&2
docker compose ps >&2
exit 1
