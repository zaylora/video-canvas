#!/usr/bin/env bash
# 一键启动前后端（Linux / macOS / Windows Git Bash）
#   后端: backend/ (Go, 默认 :8080)
#   前端: web/     (Vite, 默认 :5173，/api 与 /files 代理到后端)
# 用法: ./start.sh        Ctrl+C 同时停止前后端
set -euo pipefail

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
BACKEND_DIR="$ROOT/backend"
WEB_DIR="$ROOT/web"

# 配置文件：优先用本机的 config.local.yaml（已被 gitignore），没有就用默认配置
if [ -f "$BACKEND_DIR/configs/config.local.yaml" ]; then
  CONFIG="configs/config.local.yaml"
else
  CONFIG="configs/config.yaml"
fi

command -v go >/dev/null 2>&1 || { echo "[错误] 未找到 go，请先安装 Go" >&2; exit 1; }

# 前端包管理器：项目用 bun.lock，优先 bun，没有则退回 npm
if command -v bun >/dev/null 2>&1; then
  PM="bun"
elif command -v npm >/dev/null 2>&1; then
  PM="npm"
else
  echo "[错误] 未找到 bun 或 npm，请先安装其中之一" >&2
  exit 1
fi

# 先编译再运行：直接 go run 会多出一层子进程，停止时容易残留
EXT=""
case "$(uname -s)" in MINGW* | MSYS* | CYGWIN*) EXT=".exe" ;; esac
BIN="$BACKEND_DIR/bin/server$EXT"

echo "==> 编译后端"
(cd "$BACKEND_DIR" && go build -o "bin/server$EXT" ./cmd/server)

if [ ! -d "$WEB_DIR/node_modules" ]; then
  echo "==> 安装前端依赖 ($PM install)"
  (cd "$WEB_DIR" && "$PM" install)
fi

BACKEND_PID=""
WEB_PID=""

# 递归结束进程及其子进程（npm run dev 会多套几层）
kill_tree() {
  local pid=$1 child
  if command -v pgrep >/dev/null 2>&1; then
    for child in $(pgrep -P "$pid" 2>/dev/null || true); do
      kill_tree "$child"
    done
  fi
  kill "$pid" 2>/dev/null || true
}

cleanup() {
  trap - EXIT INT TERM
  echo
  echo "==> 停止服务"
  [ -n "$BACKEND_PID" ] && kill_tree "$BACKEND_PID"
  [ -n "$WEB_PID" ] && kill_tree "$WEB_PID"
  wait 2>/dev/null || true
}
trap cleanup EXIT INT TERM

echo "==> 启动后端 (配置: $CONFIG)"
(cd "$BACKEND_DIR" && exec "$BIN" -c "$CONFIG") &
BACKEND_PID=$!

echo "==> 启动前端 ($PM run dev)"
(cd "$WEB_DIR" && exec "$PM" run dev) &
WEB_PID=$!

echo "==> 前端 http://localhost:5173    后端 http://localhost:8080    Ctrl+C 停止"

# 任意一个退出就一起收掉（不用 wait -n，兼容 macOS 自带的 bash 3.2）
while kill -0 "$BACKEND_PID" 2>/dev/null && kill -0 "$WEB_PID" 2>/dev/null; do
  sleep 1
done
echo "[提示] 有服务已退出，正在关闭另一个"
