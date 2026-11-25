#!/usr/bin/env node
// 只用 Node 内置模块，兼容 Node 16。
import { execFileSync } from "node:child_process";
import { lstatSync, readFileSync, readdirSync, unlinkSync, writeFileSync } from "node:fs";
import { fileURLToPath } from "node:url";

const FILE = "CHANGELOG.md";
const DIRECTORY = "changelog.d";
const SECTIONS = new Map([
  ["feat", "新增"],
  ["perf", "改进"],
  ["fix", "修复"],
  ["deprecated", "废弃"],
  ["breaking", "破坏性变更"],
  ["docs", "文档"],
]);
const NEEDS_TYPES = new Set(["feat", "fix", "perf"]);

/** 提交信息是否要求写日志；会去掉 git 的 # 注释行。 */
export function needsChangelog(message) {
  const lines = message.split("\n").filter((line) => !line.startsWith("#"));
  const subject = (lines[0] || "").trim();
  if (/^(Merge |Revert |fixup! |squash! |amend! )/.test(subject)) return false;
  if (lines.some((line) => /^changelog:\s*skip\s*$/i.test(line.trim()))) return false;
  const match = /^(\w+)(\([^)]*\))?(!)?:/.exec(subject);
  if (match && match[3]) return true;
  if (lines.some((line) => /^BREAKING[ -]CHANGE:/.test(line))) return true;
  return Boolean(match) && NEEDS_TYPES.has(match[1].toLowerCase());
}

/** [Unreleased] 标题之后、下一个二级标题之前的行；没有该标题时返回 null。 */
function unreleasedLines(text) {
  const lines = text.split("\n");
  const start = lines.findIndex((line) => /^## \[Unreleased\]\s*$/i.test(line));
  if (start === -1) return null;
  const rest = lines.slice(start + 1);
  const end = rest.findIndex((line) => line.startsWith("## "));
  return end === -1 ? rest : rest.slice(0, end);
}

/** [Unreleased] 段的归一化内容，用来判断有没有被改动；文件不存在时为空串。 */
function unreleasedSnapshot(text) {
  if (!text) return "";
  const lines = unreleasedLines(text);
  return lines === null ? null : lines.join("\n").trim();
}

/** [Unreleased] 的格式问题：只允许约定的小节名，条目写在小节下、以 "- " 开头，续行缩进。 */
export function lintUnreleased(text) {
  const lines = unreleasedLines(text);
  if (lines === null) return [`${FILE} 缺少 "## [Unreleased]" 标题，发版后也要在顶部保留它`];
  const errors = [];
  let section = null;
  for (const line of lines) {
    if (line.trim() === "") continue;
    const heading = /^### (.+?)\s*$/.exec(line);
    if (heading) {
      section = heading[1];
      if (![...SECTIONS.values()].includes(section)) errors.push(`[Unreleased] 里的小节「${section}」不在约定范围内：${[...SECTIONS.values()].join(" / ")}`);
    } else if (line.startsWith("- ")) {
      if (section === null) errors.push(`条目要写在「### 小节」下面：${line}`);
    } else if (!/^\s+\S/.test(line)) {
      errors.push(`[Unreleased] 里只能有「### 小节」和以 "- " 开头的条目：${line}`);
    }
  }
  return errors;
}


function git(...args) {
  return execFileSync("git", args, { encoding: "utf8" });
}

/** 读取某个版本里的文件；文件不存在时返回空串。 */
function gitShowOrEmpty(spec) {
  try {
    return execFileSync("git", ["show", spec], { encoding: "utf8", stdio: ["ignore", "pipe", "ignore"] });
  } catch {
    return "";
  }
}

/** 以点开头的文件（.DS_Store、.gitkeep 等）不是日志，一律忽略。 */
function isFragmentName(path) {
  return !path.split("/").pop().startsWith(".");
}

function changedFragments(filter, ...revisions) {
  // 删除检查要识别改名（例如改类别），其余检查按文件逐个校验。
  const renames = filter === "D" ? "--find-renames" : "--no-renames";
  return git("diff", renames, "-z", "--name-only", "--diff-filter=" + filter, ...revisions, "--", DIRECTORY)
    .split("\0")
    .filter(Boolean)
    .filter(isFragmentName);
}

/** 改了 [Unreleased] 时要求它格式正确且为空：未发布的日志应写到 changelog.d/。 */
function checkUnreleasedUntouched(before, after) {
  if (unreleasedSnapshot(before) === unreleasedSnapshot(after)) return;
  const errors = lintUnreleased(after);
  if (errors.length) throw new Error(errors.join("\n"));
  if (unreleasedSnapshot(after)) {
    throw new Error("请把未发布的日志写到 " + DIRECTORY + "/，" + FILE + " 的 [Unreleased] 保持为空。");
  }
}

function parseFragment(path, text) {
  const match = /^changelog\.d\/[a-z0-9]+(?:-[a-z0-9]+)*\.([a-z]+)\.md$/.exec(path);
  if (!match || !SECTIONS.has(match[1])) {
    throw new Error(path + " 应使用 <改动名>.<类别>.md，类别可选 " + [...SECTIONS.keys()].join(" / ") + "。");
  }
  const entry = text.trim();
  if (!entry || /[\r\n\0]/.test(entry) || /^(?:#|[-*+] |\d+[.)] )/.test(entry)) {
    throw new Error(path + " 应包含一行用户可见的说明。");
  }
  return { path, section: match[1], entry };
}

function checkDiffFragments(filter, revision, ...revisions) {
  const paths = changedFragments(filter, ...revisions);
  return paths.map((path) => {
    const listing = revision ? git("ls-tree", revision, "--", path) : git("ls-files", "--stage", "--", path);
    if (!/^100(?:644|755) /.test(listing)) throw new Error(path + " 必须是普通文件。");
    return parseFragment(path, git("show", revision + ":" + path));
  });
}

function checkPullRequest(baseRef) {
  const base = git("merge-base", baseRef, "HEAD").trim();
  checkDiffFragments("AMT", "HEAD", base, "HEAD");
  checkUnreleasedUntouched(gitShowOrEmpty(base + ":" + FILE), gitShowOrEmpty("HEAD:" + FILE));

  if (process.env.CHANGELOG_SKIP === "1") {
    console.log("PR 带 skip-changelog 标签，跳过新增日志要求和删除检查。");
    return;
  }

  // 只有发版（会同时改 VERSION）才能消费日志文件，防止合并冲突时误删别人的日志。
  const deleted = changedFragments("D", base, "HEAD");
  const isRelease = git("diff", "--name-only", base, "HEAD", "--", "VERSION").trim() !== "";
  if (deleted.length > 0 && !isRelease) {
    throw new Error([
      "PR 删除了待发布的日志，只有发版才能删除：",
      ...deleted.map((path) => "  - " + path),
      "如果确实要撤掉这些日志（例如功能已回滚），请维护者给 PR 打 skip-changelog 标签。",
    ].join("\n"));
  }

  const needing = git("log", "--no-merges", "--format=%B%x00", base + "..HEAD")
    .split("\0")
    .map((message) => message.trim())
    .filter(needsChangelog)
    .map((message) => message.split("\n")[0]);
  const added = changedFragments("A", base, "HEAD");
  if (needing.length > 0 && added.length === 0) {
    throw new Error([
      "PR 里的以下改动需要版本日志：",
      ...needing.map((subject) => "  - " + subject),
      "请新增 " + DIRECTORY + "/<改动名>.<类别>.md，正文写一行面向用户的中文说明。",
    ].join("\n"));
  }
  console.log(added.length ? "新增 " + added.length + " 条版本日志。" : "此 PR 无需新增版本日志。");
}

function readFragments() {
  let files;
  try {
    files = readdirSync(DIRECTORY);
  } catch (error) {
    if (error.code === "ENOENT") return [];
    throw error;
  }
  return files.filter(isFragmentName).sort().map((name) => {
    const path = DIRECTORY + "/" + name;
    if (!lstatSync(path).isFile()) throw new Error(path + " 必须是普通文件。");
    return parseFragment(path, readFileSync(path, "utf8"));
  });
}

function lint(changelog) {
  const errors = lintUnreleased(changelog);
  if (errors.length) throw new Error(errors.join("\n"));
  if (unreleasedLines(changelog).some((line) => line.trim())) {
    throw new Error(FILE + " 的 [Unreleased] 应为空，请先将条目移入 " + DIRECTORY + "/。");
  }
}

/** 发版只汇总已提交的日志：未提交或改动过的文件会被误发布并删除，且无法从 Git 找回。 */
function assertFragmentsCommitted() {
  const dirty = git("status", "--porcelain", "-z", "--untracked-files=all", "--", DIRECTORY)
    .split("\0")
    .filter(Boolean)
    .map((line) => line.slice(3))
    .filter(isFragmentName);
  if (dirty.length > 0) {
    throw new Error([
      DIRECTORY + "/ 里有未提交的改动，请先提交或移走再发版：",
      ...dirty.map((path) => "  - " + path),
    ].join("\n"));
  }
}

function release(version, date, write) {
  if (!/^\d+\.\d+\.\d+(?:[.-][0-9A-Za-z.-]+)?$/.test(version)) {
    throw new Error("版本号格式应为 1.2.3 或 1.2.3-beta.1。");
  }
  if (!/^\d{4}-\d{2}-\d{2}$/.test(date) ||
      !Number.isFinite(Date.parse(date)) || new Date(date).toISOString().slice(0, 10) !== date) {
    throw new Error("发版日期必须是有效的 YYYY-MM-DD 日期。");
  }
  const changelog = readFileSync(FILE, "utf8");
  lint(changelog);
  if (changelog.split(/\r?\n/).some((line) => {
    const title = /^## (?:\[([^\]]+)\]|v(\S+))/.exec(line);
    return title && (title[1] ?? title[2]) === version;
  })) throw new Error(FILE + " 已存在版本 " + version + "。");

  assertFragmentsCommitted();
  const fragments = readFragments();
  if (fragments.length === 0) throw new Error(DIRECTORY + "/ 中没有待发布的日志。");

  const sections = [];
  for (const [type, heading] of SECTIONS) {
    const entries = fragments.filter((fragment) => fragment.section === type);
    if (entries.length) sections.push("### " + heading + "\n\n" + entries.map(({ entry }) => "- " + entry).join("\n"));
  }
  const notes = "## [" + version + "] - " + date + "\n\n" + sections.join("\n\n") + "\n";
  if (!write) return process.stdout.write(notes);

  // 先写入汇总，再消费原文件。发版提交会把两者和 VERSION 一起记录。
  const updated = changelog.replace(/^## \[Unreleased\][^\S\r\n]*(?:\r?\n|$)(?:[^\S\r\n]*\r?\n)*/im, (heading) =>
    heading.trimEnd() + "\n\n" + notes + "\n");
  writeFileSync(FILE, updated);
  writeFileSync("VERSION", version + "\n");
  for (const { path } of fragments) unlinkSync(path);
  console.log("已汇总 " + fragments.length + " 条版本日志，目标版本 " + version + "。");
}

function main([mode, arg, date, flag, ...extra]) {
  if (mode === "commit-msg" && arg) {
    checkDiffFragments("AMT", "", "--cached");
    return checkUnreleasedUntouched(gitShowOrEmpty("HEAD:" + FILE), gitShowOrEmpty(":" + FILE));
  }
  if (mode === "pr" && arg) return checkPullRequest(arg);
  if (mode === "lint") {
    lint(readFileSync(FILE, "utf8"));
    readFragments();
    return console.log("版本日志格式正确。");
  }
  if (mode === "release" && arg && date && (!flag || flag === "--write") && !extra.length) {
    return release(arg, date, flag === "--write");
  }
  throw new Error("用法：changelog-check.mjs commit-msg <消息文件> | pr <基准分支> | lint | release <版本> <日期> [--write]");
}

if (process.argv[1] === fileURLToPath(import.meta.url)) {
  try {
    main(process.argv.slice(2));
  } catch (error) {
    console.error(error.message);
    process.exitCode = 1;
  }
}
