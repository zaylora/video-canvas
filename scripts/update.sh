#!/usr/bin/env bash
# 生产环境更新脚本（Linux / macOS）：拉取新镜像并滚动重建，失败时可回滚
set -euo pipefail

# 生产环境更新脚本（Linux / macOS），无需克隆仓库，可直接通过管道运行：
#   curl -fsSL https://raw.githubusercontent.com/zaylora/video-canvas/master/scripts/update.sh | bash
ROOT=""
TAG=""
PRUNE=true

usage() {
  cat <<'EOF'
用法：update.sh [选项]

部署目录按以下顺序确定：--dir 指定 > 当前目录（含 .env）> 脚本所在目录（含 .env）> ./video-canvas

拉取镜像并重建 backend / plugin-runner / web，数据卷（数据库、素材）保持不变。
更新失败会自动回滚到更新前的 IMAGE_TAG。

选项：
  --dir PATH     部署目录（deploy.sh 生成 .env 的目录）
  --tag TAG      更新到指定版本并写入 .env 的 IMAGE_TAG（也可用于回退，如 --tag 0.1.5）
  --no-prune     不清理悬空镜像
  -h, --help     显示帮助

不带 --tag 时默认更新到 latest（并把 .env 的 IMAGE_TAG 改为 latest）。
EOF
}

while [ $# -gt 0 ]; do
  case "$1" in
    --dir)      ROOT="${2:?--dir 需要参数}"; shift 2 ;;
    --tag)      TAG="${2:?--tag 需要参数}"; shift 2 ;;
    --no-prune) PRUNE=false; shift ;;
    -h|--help)  usage; exit 0 ;;
    *) echo "[错误] 不支持的参数：$1" >&2; usage >&2; exit 2 ;;
  esac
done

if [ -z "$ROOT" ]; then
  script_dir=""
  [ -n "${BASH_SOURCE[0]:-}" ] && [ -f "${BASH_SOURCE[0]}" ] && script_dir="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
  if [ -f "$PWD/.env" ] && [ -f "$PWD/docker-compose.yml" ]; then
    ROOT="$PWD"
  elif [ -n "$script_dir" ] && [ -f "$script_dir/.env" ] && [ -f "$script_dir/docker-compose.yml" ]; then
    ROOT="$script_dir"
  else
    ROOT="$PWD/video-canvas"
  fi
fi
[ -d "$ROOT" ] || { echo "[错误] 部署目录不存在：$ROOT，请用 --dir 指定。" >&2; exit 1; }
ROOT="$(cd "$ROOT" && pwd)"
ENV_FILE="$ROOT/.env"

command -v docker >/dev/null 2>&1 || { echo "[错误] 未找到 Docker。" >&2; exit 1; }
docker info >/dev/null 2>&1 || { echo "[错误] Docker daemon 未运行。" >&2; exit 1; }
[ -f "$ENV_FILE" ] || { echo "[错误] 未找到 .env，请先运行 deploy.sh 或用 --dir 指定部署目录" >&2; exit 1; }

cd "$ROOT"

current_tag() {
  grep -E '^IMAGE_TAG=' "$ENV_FILE" | tail -n1 | cut -d= -f2 || true
}

set_tag() {
  if grep -qE '^IMAGE_TAG=' "$ENV_FILE"; then
    sed -i.bak "s|^IMAGE_TAG=.*|IMAGE_TAG=$1|" "$ENV_FILE" && rm -f "$ENV_FILE.bak"
  else
    echo "IMAGE_TAG=$1" >> "$ENV_FILE"
  fi
}

wait_healthy() {
  for _ in $(seq 1 60); do
    unhealthy="$(docker compose ps --format '{{.Service}} {{.Health}}' | awk '$2 != "" && $2 != "healthy" {print $1}')"
    running_web="$(docker compose ps --status running --services | grep -x web || true)"
    if [ -z "$unhealthy" ] && [ -n "$running_web" ]; then
      return 0
    fi
    sleep 3
  done
  return 1
}

OLD_TAG="$(current_tag)"
OLD_TAG="${OLD_TAG:-latest}"
NEW_TAG="${TAG:-latest}"

echo "==> 部署目录：$ROOT"
echo "==> 当前版本：$OLD_TAG，目标版本：$NEW_TAG"
[ "$NEW_TAG" != "$OLD_TAG" ] && set_tag "$NEW_TAG"

refresh_compose() {
  local ref="master" tmp
  command -v curl >/dev/null 2>&1 || return 0
  tmp="$(mktemp)"
  if curl -fsSL "https://raw.githubusercontent.com/zaylora/video-canvas/$ref/docker-compose.yml" -o "$tmp"; then
    if ! cmp -s "$tmp" docker-compose.yml; then
      cp docker-compose.yml docker-compose.yml.bak
      mv "$tmp" docker-compose.yml
      echo "==> 已同步 docker-compose.yml（来自 $ref，旧文件备份为 docker-compose.yml.bak）"
    else
      rm -f "$tmp"
    fi
  else
    rm -f "$tmp"
    echo "[提示] 无法从 GitHub 获取 $ref 的 compose 文件，沿用本地版本。" >&2
  fi
}

refresh_compose

echo "==> 拉取镜像"
if ! docker compose pull; then
  echo "[错误] 拉取失败，未改动运行中的服务。" >&2
  [ "$NEW_TAG" != "$OLD_TAG" ] && set_tag "$OLD_TAG"
  exit 1
fi

echo "==> 重建服务"
docker compose up -d --remove-orphans

echo "==> 等待服务健康"
if ! wait_healthy; then
  echo "[错误] 更新后服务未就绪，查看日志：docker compose logs --tail=100 backend plugin-runner" >&2
  if [ "$NEW_TAG" != "$OLD_TAG" ]; then
    echo "==> 回滚到 $OLD_TAG"
    set_tag "$OLD_TAG"
    [ -f docker-compose.yml.bak ] && mv docker-compose.yml.bak docker-compose.yml
    docker compose up -d --remove-orphans
    wait_healthy && echo "已回滚到 $OLD_TAG" || echo "[警告] 回滚后仍未就绪，请手动排查。" >&2
  else
    echo "[提示] 使用的是同名 tag（$OLD_TAG），无法自动回滚；可用 ./update.sh --tag <旧版本> 回退。" >&2
  fi
  exit 1
fi

if [ "$PRUNE" = true ]; then
  echo "==> 清理悬空镜像"
  docker image prune --force >/dev/null
fi

echo
docker compose ps
echo
echo "更新完成：$NEW_TAG"
