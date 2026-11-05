#!/usr/bin/env node
// Claude Code PreToolUse 钩子：拦下跳过 Git 钩子的提交 / 推送（--no-verify、commit -n、把 hooksPath 指空）。
// pre-commit 就是为了在本地挡住 CI 会挂的格式和 lint 问题，跳过它等于把返工推给 CI。
// 退出码 2 表示拒绝执行，stderr 会回给 AI。只用 Node 内置模块。
import { readFileSync } from "node:fs";

let command = "";
try {
  command = JSON.parse(readFileSync(0, "utf8"))?.tool_input?.command ?? "";
} catch {
  process.exit(0);
}

// 按 shell 分隔符切成单条命令再判断，避免 `echo --no-verify && git status` 之类误伤
const segments = command.split(/&&|\|\||[;|\n]/);
const reasons = [];
// 只看「以 git 开头」的命令（允许前面带 VAR=x 环境变量），正文 / heredoc 里提到这些字样不算
const GIT_CMD = /^\s*(?:[A-Za-z_][A-Za-z0-9_]*=\S*\s+)*(?:\S*[\\/])?git(?:\.exe)?\s/;
for (const seg of segments) {
  if (!GIT_CMD.test(seg)) continue;
  if (/--no-verify\b/.test(seg)) reasons.push("--no-verify");
  if (/\bcommit\b/.test(seg) && /\s-n(\s|$)/.test(seg)) reasons.push("git commit -n");
  if (/core\.hooksPath=(\/dev\/null|NUL|""|''|\s|$)/i.test(seg)) reasons.push("core.hooksPath 置空");
}

if (reasons.length > 0) {
  process.stderr.write(
    [
      `已拦截：命令里用了 ${[...new Set(reasons)].join("、")}，会跳过 pre-commit 检查。`,
      "钩子报什么就修什么：格式问题在 web/ 下跑 `BUN_OPTIONS= bun run format`、后端跑 `cd backend && golangci-lint fmt ./...`，",
      "lint / 类型错误按报错改。不确定时跑 `bash scripts/check.sh` 看与 CI 一致的结果。",
      "如果确认是钩子本身坏了，停下来告诉用户，不要绕过。",
    ].join("\n") + "\n",
  );
  process.exit(2);
}
process.exit(0);
