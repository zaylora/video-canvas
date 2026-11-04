#!/usr/bin/env node
// Claude Code PostToolUse 钩子：AI 每次 Edit / Write 完一个文件，立刻用 CI 同一套工具把它格式化，
// web/ 与 agent/ 的 TS 文件再跑一遍 oxlint。格式问题当场消掉，lint 错误以 exit 2 回给 AI 让它马上改，
// 不用等到 CI 报错再返工。
//
// 输入：stdin 上的钩子 JSON（tool_input.file_path）。
// 退出码：0 = 没事或已自动格式化；2 = 有需要 AI 自己修的问题（stderr 内容会回给 AI）。
// 只用 Node 内置模块，保持对 node v16 兼容。
import { spawnSync } from "node:child_process";
import { existsSync, readFileSync } from "node:fs";
import path from "node:path";
import { fileURLToPath } from "node:url";

const ROOT = path.resolve(path.dirname(fileURLToPath(import.meta.url)), "../..");
const FORMAT_EXT = new Set([".ts", ".tsx", ".js", ".jsx", ".mjs", ".cjs", ".json", ".css", ".md"]);
const LINT_EXT = new Set([".ts", ".tsx", ".js", ".jsx", ".mjs", ".cjs"]);
// 用 oxfmt / oxlint 的 TS 子项目。ignore 与各自 .oxfmtrc.json 的 ignorePatterns 保持一致：
// 显式传文件名时 oxfmt 不一定会再套用忽略规则。lintDirs 与 CI 里 `bun run lint` / `npm run lint` 的范围一致
const TS_PROJECTS = {
  web: {
    ignore: [/^src\/components\/ui\//, /^dist\//, /^node_modules\//, /^bun\.lock$/],
    lintDirs: ["src/"],
    lintCmd: "bun run lint",
  },
  agent: {
    ignore: [/^dist\//, /^node_modules\//, /^package-lock\.json$/],
    lintDirs: ["src/", "test/"],
    lintCmd: "npm run lint",
  },
};

function readInput() {
  try {
    return JSON.parse(readFileSync(0, "utf8"));
  } catch {
    return null;
  }
}

// BUN_OPTIONS 里的 --preload 会让 `bun run <脚本>` 退化成打印帮助并返回 0，这里直接调工具入口并去掉它
function toolEnv() {
  const env = { ...process.env };
  delete env.BUN_OPTIONS;
  return env;
}

function hasCommand(cmd) {
  const probe = process.platform === "win32" ? "where" : "which";
  return spawnSync(probe, [cmd], { stdio: "ignore", shell: false }).status === 0;
}

function run(cmd, args, cwd) {
  const res = spawnSync(cmd, args, { cwd, env: toolEnv(), encoding: "utf8", shell: false });
  if (process.env.FORMAT_HOOK_DEBUG) {
    process.stderr.write(`[format-on-edit] ${cmd} ${args.join(" ")} (cwd=${cwd}) -> ${res.status} ${res.error ?? ""}\n`);
  }
  return { code: res.status ?? 1, out: `${res.stdout ?? ""}${res.stderr ?? ""}`.trim(), error: res.error };
}

// oxfmt / oxlint 要求较新的 Node；优先用 bun 跑它们的 JS 入口
function runOxTool(bin, args, cwd) {
  const entry = path.join(cwd, "node_modules", bin, "bin", bin);
  if (!existsSync(entry)) return { code: 0, out: "", skipped: true };
  const runner = hasCommand("bun") ? "bun" : "node";
  return run(runner, [entry, ...args], cwd);
}

const problems = [];

function handleTsProject(name, relInProject) {
  const project = TS_PROJECTS[name];
  const dir = path.join(ROOT, name);
  const ext = path.extname(relInProject);
  if (project.ignore.some((re) => re.test(relInProject))) return;
  if (FORMAT_EXT.has(ext)) {
    const fmt = runOxTool("oxfmt", [relInProject], dir);
    if (fmt.code !== 0) problems.push(`oxfmt 无法格式化 ${name}/${relInProject}（多半是语法错误）：\n${fmt.out}`);
  }
  if (LINT_EXT.has(ext) && project.lintDirs.some((d) => relInProject.startsWith(d))) {
    const lint = runOxTool("oxlint", [relInProject], dir);
    if (lint.code !== 0) {
      problems.push(`oxlint 报错（CI 的 ${name} ${project.lintCmd} 同样会挂），请修掉：\n${lint.out}`);
    }
  }
}

function handleGo(relToBackend) {
  const backendDir = path.join(ROOT, "backend");
  // golangci-lint fmt 读 backend/.golangci.yml：gofmt + goimports(local-prefixes=video-canvas)，与 CI 完全一致
  if (hasCommand("golangci-lint")) {
    const res = run("golangci-lint", ["fmt", relToBackend], backendDir);
    if (res.code === 0) return;
    problems.push(`golangci-lint fmt 失败（多半是语法错误）：\n${res.out}`);
    return;
  }
  if (hasCommand("gofmt")) {
    const res = run("gofmt", ["-w", relToBackend], backendDir);
    if (res.code !== 0) problems.push(`gofmt 失败（多半是语法错误）：\n${res.out}`);
  }
}

const input = readInput();
const filePath = input?.tool_input?.file_path;
if (!filePath || !existsSync(filePath)) process.exit(0);

const rel = path.relative(ROOT, path.resolve(filePath)).split(path.sep).join("/");
if (rel.startsWith("..")) process.exit(0);

const project = Object.keys(TS_PROJECTS).find((name) => rel.startsWith(`${name}/`));
if (project) handleTsProject(project, rel.slice(project.length + 1));
else if (rel.startsWith("backend/") && rel.endsWith(".go")) handleGo(rel.slice("backend/".length));

if (problems.length > 0) {
  process.stderr.write(`${problems.join("\n\n")}\n`);
  process.exit(2);
}
process.exit(0);
