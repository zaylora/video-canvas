#!/usr/bin/env bash
# 生产环境更新脚本（Linux / macOS），无需克隆仓库，可直接通过管道运行：
#   curl -fsSL https://raw.githubusercontent.com/zaylora/video-canvas/master/scripts/update.sh | bash
#
# 流程：改 IMAGE_TAG → 取目标版本的 compose 文件 → 拉镜像 → 备份数据库（durable 模式再在线备份 Agent 的数据）→ 重建 → 等新版本健康
# 任何一步失败都会退回更新前的 IMAGE_TAG 和 compose 文件。后台「一键升级」（updater 容器）调用的也是这个脚本。
set -euo pipefail

REPO="zaylora/video-canvas"
RAW="https://raw.githubusercontent.com/$REPO"
ROOT=""
TAG=""
BACKUP=true
KEEP_BACKUPS=3
PRUNE=false
TIMEOUT=600
STATUS_FILE=""
ORPHANS="--remove-orphans"

usage() {
  cat <<'EOF'
用法：update.sh [选项]

部署目录按以下顺序确定：--dir 指定 > 当前目录（含 .env）> 脚本所在目录（含 .env）> ./video-canvas

拉取镜像并重建 backend / plugin-runner / web，数据卷（数据库、素材）保持不变。
新版本 backend 启动时会自动执行数据库迁移；迁移只向前，所以重建前默认先备份数据库。
任何一步失败都会自动退回更新前的版本。

选项：
  --dir PATH           部署目录（deploy.sh 生成 .env 的目录）
  --tag VERSION        目标版本，如 0.4.0（也可用于回退，如 --tag 0.3.2）；
                       不带时取 GitHub 上最新的正式版本。latest 也可以，但失败时无法回退
  --no-backup          不备份数据库（不推荐）
  --keep-backups N     保留最近 N 份备份，默认 3
  --timeout SECONDS    等新版本健康的最长时间，默认 600（老库首次迁移可能较慢）
  --status-file PATH   每一步把进度写成 JSON（给后台一键升级读取）
  --keep-orphans       重建时不删除 compose 文件里已经没有的容器（后台一键升级用：回退到没有 updater 的旧版本时，
                       正在执行本脚本的 updater 容器不能被当成孤儿删掉）
  --prune              完成后清理悬空镜像（会清掉本机所有项目的悬空镜像，默认不清）
  -h, --help           显示帮助

备份位置：部署目录下的 backups/（pre-<版本>-<时间>.dump 是数据库；.env 里 APP_AGENT_RUNTIME=durable 时还有
pre-<版本>-<时间>.agent.sqlite，是 Agent 服务的数据），恢复方法见 docs/docker-production.md「升级与数据库迁移」和「Agent 独立服务」。
EOF
}

while [ $# -gt 0 ]; do
  case "$1" in
    --dir)          ROOT="${2:?--dir 需要参数}"; shift 2 ;;
    --tag)          TAG="${2:?--tag 需要参数}"; shift 2 ;;
    --no-backup)    BACKUP=false; shift ;;
    --keep-backups) KEEP_BACKUPS="${2:?--keep-backups 需要参数}"; shift 2 ;;
    --timeout)      TIMEOUT="${2:?--timeout 需要参数}"; shift 2 ;;
    --status-file)  STATUS_FILE="${2:?--status-file 需要参数}"; shift 2 ;;
    --prune)        PRUNE=true; shift ;;
    --keep-orphans) ORPHANS=""; shift ;;
    --no-prune)     PRUNE=false; shift ;; # 兼容旧参数，现在默认就不清理
    -h|--help)      usage; exit 0 ;;
    *) echo "[错误] 不支持的参数：$1" >&2; usage >&2; exit 2 ;;
  esac
done
TAG="${TAG#v}"
[ -z "$STATUS_FILE" ] || mkdir -p "$(dirname "$STATUS_FILE")"
case "$KEEP_BACKUPS" in ''|*[!0-9]*) echo "[错误] --keep-backups 应为正整数" >&2; exit 2 ;; esac
case "$TIMEOUT" in ''|*[!0-9]*) echo "[错误] --timeout 应为秒数" >&2; exit 2 ;; esac
[ "$KEEP_BACKUPS" -ge 1 ] || KEEP_BACKUPS=1

# ---------- 进度输出 ----------

OLD_TAG=""
NEW_TAG=""
BACKUP_FILE=""
AGENT_BACKUP_FILE=""
PHASE="prepare"

json_escape() {
  local s="$1"
  s="${s//\\/\\\\}"; s="${s//\"/\\\"}"; s="${s//$'\n'/\\n}"; s="${s//$'\r'/}"; s="${s//$'\t'/ }"
  printf '%s' "$s"
}

# status <phase> <state> <message>：打印进度；指定了 --status-file 时同时写 JSON（先写临时文件再改名，读的一方不会读到半截）
# state：running / succeeded / failed / failed_rolled_back / failed_needs_attention
status() {
  PHASE="$1"
  local state="$2" msg="$3"
  echo "==> $msg"
  [ -n "$STATUS_FILE" ] || return 0
  local tmp="$STATUS_FILE.tmp"
  printf '{"target":"%s","previous":"%s","phase":"%s","state":"%s","message":"%s","backup":"%s","updated_at":"%s"}\n' \
    "$(json_escape "$NEW_TAG")" "$(json_escape "$OLD_TAG")" "$PHASE" "$state" "$(json_escape "$msg")" \
    "$(json_escape "$BACKUP_FILE")" "$(date -u +%Y-%m-%dT%H:%M:%SZ)" > "$tmp"
  mv -f "$tmp" "$STATUS_FILE"
}

fail() {
  echo "[错误] $2" >&2
  status "$PHASE" "$1" "$2"
  exit 1
}

# ---------- 部署目录与前置检查 ----------

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
COMPOSE_FILE="$ROOT/docker-compose.yml"

command -v docker >/dev/null 2>&1 || fail failed "未找到 Docker。"
docker info >/dev/null 2>&1 || fail failed "Docker daemon 未运行。"
docker compose version >/dev/null 2>&1 || fail failed "需要 Docker Compose v2。"
[ -f "$ENV_FILE" ] || fail failed "未找到 $ENV_FILE，请先运行 deploy.sh 或用 --dir 指定部署目录"
cd "$ROOT"

# 同一时间只允许一个更新：mkdir 是原子的；进程被杀留下的锁，按里面记的 pid 判断是否失效
LOCK_DIR="$ROOT/.update.lock"
if ! mkdir "$LOCK_DIR" 2>/dev/null; then
  holder="$(cat "$LOCK_DIR/pid" 2>/dev/null || true)"
  if [ -n "$holder" ] && kill -0 "$holder" 2>/dev/null; then
    # 不写状态文件：那是正在进行的那次更新的进度
    echo "[错误] 已有更新在进行中（pid $holder），请等它结束" >&2
    exit 3
  fi
  rm -rf "$LOCK_DIR" && mkdir "$LOCK_DIR"
fi
echo $$ > "$LOCK_DIR/pid"
trap 'rm -rf "$LOCK_DIR" "$COMPOSE_FILE.next"' EXIT

env_value() {
  grep -E "^$1=" "$ENV_FILE" | tail -n1 | cut -d= -f2- || true
}

set_tag() {
  if grep -qE '^IMAGE_TAG=' "$ENV_FILE"; then
    sed -i.bak "s|^IMAGE_TAG=.*|IMAGE_TAG=$1|" "$ENV_FILE" && rm -f "$ENV_FILE.bak"
  else
    echo "IMAGE_TAG=$1" >> "$ENV_FILE"
  fi
}

# latest_release 读 GitHub 上最新正式版本的版本号（不含 v）
latest_release() {
  command -v curl >/dev/null 2>&1 || return 1
  curl -fsSL --max-time 15 -H "Accept: application/vnd.github+json" "https://api.github.com/repos/$REPO/releases/latest" \
    | grep -m1 '"tag_name"' | sed -E 's/.*"tag_name"[[:space:]]*:[[:space:]]*"v?([^"]+)".*/\1/'
}

OLD_TAG="$(env_value IMAGE_TAG)"
OLD_TAG="${OLD_TAG:-latest}"
if [ -z "$TAG" ]; then
  TAG="$(latest_release || true)"
  [ -n "$TAG" ] || fail failed "无法从 GitHub 获取最新版本号，请用 --tag 指定，如 --tag 0.4.0"
fi
NEW_TAG="$TAG"
PG_USER="$(env_value POSTGRES_USER)"; PG_USER="${PG_USER:-postgres}"
PG_DB="$(env_value POSTGRES_DB)"; PG_DB="${PG_DB:-video_canvas}"

echo "==> 部署目录：$ROOT"
status prepare running "当前版本 $OLD_TAG，目标版本 $NEW_TAG"
if [ "$NEW_TAG" = "latest" ] || [ "$OLD_TAG" = "latest" ]; then
  echo "[提示] 使用 latest 时无法确认版本、也无法在失败时自动回退；建议改用具体版本号（--tag x.y.z）。" >&2
fi

# ---------- 回退 ----------

COMPOSE_REPLACED=false

restore_files() {
  [ "$NEW_TAG" != "$OLD_TAG" ] && set_tag "$OLD_TAG"
  if [ "$COMPOSE_REPLACED" = true ] && [ -f "$COMPOSE_FILE.bak" ]; then
    mv -f "$COMPOSE_FILE.bak" "$COMPOSE_FILE"
  fi
}

# health_version 输出 backend /health 里的版本号；backend 还没起来时输出空
health_version() {
  docker compose exec -T backend wget -qO- http://127.0.0.1:8080/health 2>/dev/null \
    | grep -o '"version":"[^"]*"' | head -n1 | cut -d'"' -f4 || true
}

# wait_ready <版本>：等全部带健康检查的容器 healthy、web 在运行，且 backend 报告的版本就是目标版本
wait_ready() {
  local want="$1" deadline=$(( $(date +%s) + TIMEOUT ))
  while [ "$(date +%s)" -lt "$deadline" ]; do
    local unhealthy running_web ver
    unhealthy="$(docker compose ps --format '{{.Service}} {{.Health}}' | awk '$2 != "" && $2 != "healthy" {print $1}')"
    running_web="$(docker compose ps --status running --services | grep -x web || true)"
    if [ -z "$unhealthy" ] && [ -n "$running_web" ]; then
      ver="$(health_version)"
      if [ "$want" = "latest" ] || [ "${ver#v}" = "$want" ]; then
        return 0
      fi
    fi
    sleep 3
  done
  return 1
}

# rollback <原因>：退回旧 tag 和旧 compose 文件并重建。旧版本因为「数据库已被新版本迁移」起不来时，需要人工处理
rollback() {
  local reason="$1" logs
  logs="$(docker compose logs --tail=100 backend 2>&1 || true)"
  if [ "$(env_value APP_AGENT_RUNTIME)" = "durable" ]; then
    logs="$logs"$'\n'"----- agent 最后 50 行日志 -----"$'\n'"$(docker compose logs --tail=50 agent 2>&1 || true)"
  fi
  echo "[错误] $reason" >&2
  echo "----- 新版本 backend 最后 100 行日志 -----" >&2
  echo "$logs" >&2
  echo "-------------------------------------------" >&2
  if [ "$NEW_TAG" = "$OLD_TAG" ]; then
    fail failed "$reason；目标版本与当前版本相同（$OLD_TAG），无法自动回退，可用 --tag <旧版本> 回退"
  fi
  status rollback running "$reason，正在退回 $OLD_TAG"
  restore_files
  docker compose up -d $ORPHANS || true
  if wait_ready "$OLD_TAG"; then
    fail failed_rolled_back "$reason；已退回 $OLD_TAG，服务正常"
  fi
  if docker compose logs --tail=50 backend 2>&1 | grep -q "数据库已执行过本程序没有的迁移"; then
    fail failed_needs_attention "$reason；退回 $OLD_TAG 后它拒绝启动：数据库已被 $NEW_TAG 迁移过。确认那些迁移只新增表或列后，在 .env 加 APP_DATABASE_ALLOW_NEWER_SCHEMA=true 再 docker compose up -d；否则用备份 ${BACKUP_FILE:-（无）} 恢复"
  fi
  if docker compose logs --tail=50 agent 2>&1 | grep -q "比当前代码"; then
    fail failed_needs_attention "$reason；退回 $OLD_TAG 后 agent 拒绝启动：它的数据已被 $NEW_TAG 升级过。换回 $NEW_TAG，或停掉 agent、用备份 ${AGENT_BACKUP_FILE:-（无）} 恢复数据（见 docs/docker-production.md「Agent 独立服务」）"
  fi
  fail failed_needs_attention "$reason；退回 $OLD_TAG 后服务仍未就绪，请查看 docker compose logs backend agent 手动排查"
}

# ---------- 1. 改 tag、取目标版本的 compose 文件 ----------

[ "$NEW_TAG" != "$OLD_TAG" ] && set_tag "$NEW_TAG"

status compose running "获取 $NEW_TAG 的 docker-compose.yml"
ref="v$NEW_TAG"
[ "$NEW_TAG" = "latest" ] && ref="master"
if command -v curl >/dev/null 2>&1 && curl -fsSL --max-time 30 "$RAW/$ref/docker-compose.yml" -o "$COMPOSE_FILE.next"; then
  if ! cmp -s "$COMPOSE_FILE.next" "$COMPOSE_FILE"; then
    cp "$COMPOSE_FILE" "$COMPOSE_FILE.bak"
    mv "$COMPOSE_FILE.next" "$COMPOSE_FILE"
    COMPOSE_REPLACED=true
    echo "    已更新 docker-compose.yml（旧文件备份为 docker-compose.yml.bak）"
  fi
else
  echo "[提示] 无法获取 $ref 的 compose 文件，沿用本地版本。" >&2
fi

# ---------- 2. 拉镜像（失败时服务一直没动） ----------

status pull running "拉取镜像"
if ! docker compose pull; then
  restore_files
  fail failed "拉取镜像失败，未改动运行中的服务"
fi

# ---------- 3. 备份数据库（拉取成功后才备份，镜像拉不下来就不白做） ----------

if [ "$BACKUP" = true ]; then
  mkdir -p "$ROOT/backups"
  BACKUP_FILE="$ROOT/backups/pre-$NEW_TAG-$(date +%Y%m%d%H%M%S).dump"
  status backup running "备份数据库到 $BACKUP_FILE"
  if ! docker compose exec -T postgres pg_dump -U "$PG_USER" -Fc "$PG_DB" > "$BACKUP_FILE" || [ ! -s "$BACKUP_FILE" ]; then
    rm -f "$BACKUP_FILE"
    BACKUP_FILE=""
    restore_files
    fail failed "备份数据库失败，已中止更新（服务未改动）；确认 postgres 容器在运行，或用 --no-backup 跳过备份"
  fi
  # 只保留最近 KEEP_BACKUPS 份（文件名带时间，按名字排序即按时间）
  ls -1 "$ROOT"/backups/pre-*.dump 2>/dev/null | sort -r | tail -n +"$((KEEP_BACKUPS + 1))" | while IFS= read -r old; do
    rm -f "$old"
  done
else
  echo "[提示] 已跳过数据库备份（--no-backup）。" >&2
fi

# ---------- 3b. 在线备份 Agent 的数据（durable 模式） ----------

# 会话、运行、审批、事件和对话历史都在 agent_data 卷的 SQLite 里，Postgres 的备份里没有。用目标版本的镜像起一个一次性容器读卷：
# agent 正在运行也行（SQLite 在线备份，不用停机），没在运行也行；备份先写到容器里的临时文件，再从标准输出流到宿主机，
# 日志和结果 JSON 走标准错误。卷里还没有数据（第一次启用）退出码 3，不算失败。
backup_agent() {
  local tmp="$ROOT/backups/.agent-backup.tmp" rc=0
  mkdir -p "$ROOT/backups"
  AGENT_BACKUP_FILE="$ROOT/backups/pre-$NEW_TAG-$(date +%Y%m%d%H%M%S).agent.sqlite"
  status backup running "在线备份 Agent 数据到 $AGENT_BACKUP_FILE"
  docker compose run --rm --no-deps -T --entrypoint sh agent -c \
    '[ -f /data/agent.sqlite ] || exit 3; AGENT_DB_PATH=/data/agent.sqlite node dist/backup-cli.js /tmp/agent-backup.sqlite 1>&2 && cat /tmp/agent-backup.sqlite' \
    > "$tmp" || rc=$?
  if [ "$rc" -eq 3 ]; then
    rm -f "$tmp"
    AGENT_BACKUP_FILE=""
    echo "[提示] Agent 还没有数据（第一次启用），无需备份。"
    return 0
  fi
  if [ "$rc" -ne 0 ] || [ ! -s "$tmp" ] || ! head -c 15 "$tmp" | grep -q "SQLite format 3"; then
    rm -f "$tmp"
    AGENT_BACKUP_FILE=""
    restore_files
    fail failed "备份 Agent 数据失败，已中止更新（服务未改动）；目标版本的镜像里需要有备份命令（dist/backup-cli.js），或用 --no-backup 跳过备份"
  fi
  mv -f "$tmp" "$AGENT_BACKUP_FILE"
  ls -1 "$ROOT"/backups/pre-*.agent.sqlite 2>/dev/null | sort -r | tail -n +"$((KEEP_BACKUPS + 1))" | while IFS= read -r old; do
    rm -f "$old"
  done
}

if [ "$BACKUP" = true ] && [ "$(env_value APP_AGENT_RUNTIME)" = "durable" ]; then
  backup_agent
fi

# ---------- 4. 重建并等新版本就绪 ----------

status recreate running "重建服务（新版本启动时会自动执行数据库迁移）"
# 新版本起不来时 up -d 本身就会失败，不能让 set -e 直接退出，要走回退
if ! docker compose up -d $ORPHANS; then
  rollback "新版本启动失败"
fi

status health running "等待 $NEW_TAG 就绪（最多 $TIMEOUT 秒）"
if ! wait_ready "$NEW_TAG"; then
  rollback "新版本在 $TIMEOUT 秒内没有就绪"
fi

rm -f "$COMPOSE_FILE.bak"
if [ "$PRUNE" = true ]; then
  echo "==> 清理悬空镜像"
  docker image prune --force >/dev/null
fi

echo
docker compose ps
echo
status finish succeeded "更新完成：$NEW_TAG"
