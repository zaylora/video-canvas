#!/usr/bin/env bash
# 本地跑一遍和 .github/workflows/ci.yml 相同的检查，只跑改动涉及的部分（相对 master 的提交 + 工作区未提交改动）。
#
# 用法：
#   bash scripts/check.sh            # 按改动自动选 web / backend / agent
#   bash scripts/check.sh --fix      # 先自动格式化（web/agent 用 oxfmt，backend 用 golangci-lint fmt）再检查
#   bash scripts/check.sh --all      # 不看改动，三端全跑
#   bash scripts/check.sh web agent  # 只跑指定部分
#   SKIP_TESTS=1 bash scripts/check.sh  # 只跑格式 / 类型 / lint / 构建，跳过测试
#
# 注意：BUN_OPTIONS 里带 --preload 时 `bun run <脚本>` 会打印帮助并返回 0（假通过），这里统一清掉它。
set -uo pipefail

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "$ROOT" || exit 1
unset BUN_OPTIONS

FIX=0
ALL=0
TARGETS=()
for arg in "$@"; do
  case "$arg" in
    --fix) FIX=1 ;;
    --all) ALL=1 ;;
    web | backend | agent) TARGETS+=("$arg") ;;
    *) echo "未知参数：$arg" >&2; exit 2 ;;
  esac
done

if [ ${#TARGETS[@]} -eq 0 ]; then
  if [ "$ALL" -eq 1 ]; then
    TARGETS=(web backend agent)
  else
    base="$(git merge-base HEAD master 2>/dev/null || git merge-base HEAD origin/master 2>/dev/null || echo HEAD)"
    changed="$( { git diff --name-only "$base"; git diff --name-only; git ls-files --others --exclude-standard; } | sort -u)"
    for t in web backend agent; do
      grep -q "^$t/" <<<"$changed" && TARGETS+=("$t")
    done
    if [ ${#TARGETS[@]} -eq 0 ]; then echo "没有 web/ backend/ agent/ 的改动，无需检查。"; exit 0; fi
  fi
fi

FAILED=()
step() { # step <名称> <目录> <命令...>
  local name="$1" dir="$2"; shift 2
  printf '\n\033[1m▶ %s\033[0m  (%s)\n' "$name" "$dir"
  if (cd "$ROOT/$dir" && "$@"); then
    printf '\033[32m✓ %s\033[0m\n' "$name"
  else
    printf '\033[31m✗ %s\033[0m\n' "$name"
    FAILED+=("$name")
  fi
}
want_tests() { [ "${SKIP_TESTS:-0}" != "1" ]; }

for t in "${TARGETS[@]}"; do
  case "$t" in
    web)
      [ "$FIX" -eq 1 ] && step "web 自动格式化" web bun run format
      step "web 格式检查 (oxfmt --check)" web bun run format:check
      step "web 类型检查 (tsc)" web bun run typecheck
      step "web lint (oxlint)" web bun run lint
      want_tests && step "web 测试 (bun test)" web bun run test
      step "web 构建" web bun run build
      ;;
    backend)
      [ "$FIX" -eq 1 ] && step "backend 自动格式化" backend golangci-lint fmt ./...
      step "backend golangci-lint（相对 master 的增量，含 gofmt/goimports）" backend \
        golangci-lint run --new-from-merge-base=master ./...
      step "backend 构建" backend go build ./...
      want_tests && step "backend 测试 (go test)" backend go test ./...
      ;;
    agent)
      [ "$FIX" -eq 1 ] && step "agent 自动格式化" agent npm run format
      step "agent 格式检查 (oxfmt --check)" agent npm run format:check
      step "agent lint (oxlint)" agent npm run lint
      step "agent 类型检查" agent npm run typecheck
      want_tests && step "agent 测试" agent npm test
      step "agent 构建" agent npm run build
      ;;
  esac
done

echo
if [ ${#FAILED[@]} -eq 0 ]; then
  printf '\033[32m全部通过（%s）\033[0m\n' "${TARGETS[*]}"
  exit 0
fi
printf '\033[31m未通过：\033[0m\n'
printf '  - %s\n' "${FAILED[@]}"
exit 1
