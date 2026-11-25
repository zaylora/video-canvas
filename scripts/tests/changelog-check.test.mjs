// 版本日志规则与 Git 工作流测试。CI 用 `node --test scripts/tests/`，本地也可 `bun test scripts/tests/`。
import assert from "node:assert/strict";
import { describe, test } from "node:test";
import { spawnSync } from "node:child_process";
import { existsSync, mkdirSync, mkdtempSync, readFileSync, rmSync, writeFileSync } from "node:fs";
import { tmpdir } from "node:os";
import { join } from "node:path";
import { fileURLToPath } from "node:url";

const script = fileURLToPath(new URL("../changelog-check.mjs", import.meta.url));

import { lintUnreleased, needsChangelog } from "../changelog-check.mjs";

const changelog = (unreleased, released = "### 修复\n\n- 旧的修复。\n") =>
  `# Changelog\n\n所有重要变更都会记录在此文件中。\n\n## [Unreleased]\n${unreleased}\n## [0.6.16] - 2026-11-17\n\n${released}`;

describe("needsChangelog", () => {
  test("feat / fix / perf 需要写日志", () => {
    assert.equal(needsChangelog("feat(canvas): 支持节点分组"), true);
    assert.equal(needsChangelog("fix: 修复保存失败"), true);
    assert.equal(needsChangelog("perf(web): 画布首屏更快"), true);
  });

  test("破坏性变更不论类型都需要写日志", () => {
    assert.equal(needsChangelog("refactor(api)!: 去掉旧接口"), true);
    assert.equal(needsChangelog("chore: 升级依赖\n\nBREAKING CHANGE: 需要 Go 1.24"), true);
  });

  test("内部维护类提交不需要", () => {
    for (const msg of ["refactor: 拆函数", "test: 补用例", "chore: 升级依赖", "ci: 调整缓存", "docs: 改说明"]) {
      assert.equal(needsChangelog(msg), false, msg);
    }
  });

  test("Changelog: skip 尾注可豁免", () => {
    assert.equal(needsChangelog("feat(agent): 内部工具改名\n\nChangelog: skip"), false);
    assert.equal(needsChangelog("fix: 测试夹具\n\nchangelog:SKIP\n"), false);
  });

  test("合并、回滚、fixup 提交不检查", () => {
    assert.equal(needsChangelog("Merge branch 'develop'"), false);
    assert.equal(needsChangelog('Revert "feat: 支持节点分组"'), false);
    assert.equal(needsChangelog("fixup! feat: 支持节点分组"), false);
    assert.equal(needsChangelog("squash! fix: 修复保存失败"), false);
  });

  test("忽略 git 注释行", () => {
    assert.equal(needsChangelog("refactor: 拆函数\n# feat: 这是注释"), false);
  });
});

describe("lintUnreleased", () => {
  test("合法的小节和条目没有错误", () => {
    const text = changelog("\n### 新增\n\n- 一条。\n  续行说明。\n\n### 破坏性变更\n\n- 另一条。\n");
    assert.deepEqual(lintUnreleased(text), []);
  });

  test("缺少 Unreleased 标题", () => {
    assert.match(lintUnreleased("# Changelog\n\n## [0.1.0]\n")[0], /Unreleased/);
  });

  test("不认识的小节名", () => {
    const errors = lintUnreleased(changelog("\n### Added\n\n- 一条。\n"));
    assert.equal(errors.length, 1);
    assert.match(errors[0], /Added/);
  });

  test("条目必须写在小节下面、以 - 开头", () => {
    assert.equal(lintUnreleased(changelog("\n- 没有小节。\n")).length, 1);
    assert.equal(lintUnreleased(changelog("\n### 修复\n\n随手写的一行\n")).length, 1);
  });
});

describe("日志文件工作流", () => {
  function fixture(t) {
    const cwd = mkdtempSync(join(tmpdir(), "vc-changelog-"));
    t.after(() => rmSync(cwd, { recursive: true, force: true }));
    const git = (...args) => {
      const result = spawnSync("git", args, { cwd, encoding: "utf8" });
      assert.equal(result.status, 0, result.stderr);
      return result.stdout.trim();
    };
    const write = (file, content) => writeFileSync(join(cwd, file), content);
    const read = (file) => readFileSync(join(cwd, file), "utf8");
    const run = (...args) => spawnSync(process.execPath, [script, ...args], { cwd, encoding: "utf8" });
    git("init", "-q");
    git("config", "user.name", "Changelog Tests");
    git("config", "user.email", "test@example.com");
    mkdirSync(join(cwd, "changelog.d"));
    write("CHANGELOG.md", "# Changelog\n\n## [Unreleased]\n\n## [1.0.0] - 2026-11-24\n\n- 已发布。\n");
    write("VERSION", "1.0.0\n");
    write("changelog.d/previous.fix.md", "修复已有问题。\n");
    git("add", ".");
    git("commit", "-qm", "chore: initial commit");
    return { cwd, git, read, run, write };
  }

  test("PR 必须新增自己的日志文件，现有日志和版本历史不能抵数", (t) => {
    const { git, run, write } = fixture(t);
    const base = git("rev-parse", "HEAD");
    write("README.md", "change\n");
    git("add", ".");
    git("commit", "-qm", "fix: 修复保存");
    assert.match(run("pr", base).stderr, /changelog\.d/);
    write("changelog.d/save.fix.md", "修复保存失败。\n");
    git("add", ".");
    git("commit", "-qm", "docs: 补充版本日志");
    assert.equal(run("pr", base).status, 0);
  });

  test("两个从同一基准分支出发的修复 PR 可以依次合并", (t) => {
    const { git, read, run, write } = fixture(t);
    const base = git("rev-parse", "HEAD");
    git("switch", "-qc", "integration");
    write("changelog.d/doubao.fix.md", "修复豆包视频请求失败。\n");
    git("add", ".");
    git("commit", "-qm", "fix: 修复豆包视频请求");
    git("switch", "-qc", "storyboard", base);
    write("changelog.d/storyboard.fix.md", "恢复旧画布的分镜资产引用。\n");
    git("add", ".");
    git("commit", "-qm", "fix: 恢复旧画布的资产引用");

    git("switch", "-q", "integration");
    const updatedBase = git("rev-parse", "HEAD");
    git("merge", "--no-ff", "--no-edit", "storyboard");
    assert.equal(run("pr", updatedBase).status, 0);
    assert.equal(read("changelog.d/doubao.fix.md"), "修复豆包视频请求失败。\n");
    assert.equal(read("changelog.d/storyboard.fix.md"), "恢复旧画布的分镜资产引用。\n");
    assert.equal(git("diff", base, "--", "CHANGELOG.md"), "");
  });

  test("本地钩子只读取暂存内容，并允许稍后在同一 PR 补日志", (t) => {
    const { git, run, write } = fixture(t);
    write("message", "fix: 修复保存\n");
    assert.equal(run("commit-msg", "message").status, 0);
    write("changelog.d/save.fix.md", "修复保存失败。\n");
    git("add", "changelog.d/save.fix.md");
    write("changelog.d/save.fix.md", "第一行\n第二行\n");
    assert.equal(run("commit-msg", "message").status, 0);
    git("add", "changelog.d/save.fix.md");
    assert.match(run("commit-msg", "message").stderr, /save\.fix\.md/);
  });

  test("预览保留文件，写入按类别汇总并消费日志文件", (t) => {
    const { cwd, git, read, run, write } = fixture(t);
    write("changelog.d/group.feat.md", "支持节点分组。\n");
    git("add", ".");
    git("commit", "-qm", "feat: 支持节点分组");
    const before = read("CHANGELOG.md");
    const preview = run("release", "1.1.0-beta.1", "2026-11-25");
    assert.equal(preview.status, 0, preview.stderr);
    assert.match(preview.stdout, /## \[1\.1\.0-beta\.1\] - 2026-11-25\n\n### 新增\n\n- 支持节点分组。\n\n### 修复\n\n- 修复已有问题。/);
    assert.equal(read("CHANGELOG.md"), before);
    assert.equal(read("VERSION"), "1.0.0\n");
    const applied = run("release", "1.1.0-beta.1", "2026-11-25", "--write");
    assert.equal(applied.status, 0, applied.stderr);
    assert.ok(read("CHANGELOG.md").includes(preview.stdout.trimEnd()));
    assert.ok(read("CHANGELOG.md").endsWith("## [1.0.0] - 2026-11-24\n\n- 已发布。\n"));
    assert.equal(read("VERSION"), "1.1.0-beta.1\n");
    assert.equal(existsSync(join(cwd, "changelog.d/group.feat.md")), false);
    assert.equal(existsSync(join(cwd, "changelog.d/previous.fix.md")), false);
    assert.notEqual(run("release", "1.1.0-beta.1", "2026-11-25", "--write").status, 0);
  });

  test("发版遇到无效日志时保留版本和原始文件", (t) => {
    const { git, read, run, write } = fixture(t);
    write("changelog.d/invalid.fix.md", "\n");
    git("add", ".");
    git("commit", "-qm", "chore: 无效日志");
    const before = read("CHANGELOG.md");
    const result = run("release", "1.1.0", "2026-11-25", "--write");
    assert.match(result.stderr, /invalid\.fix\.md/);
    assert.equal(read("CHANGELOG.md"), before);
    assert.equal(read("VERSION"), "1.0.0\n");
    assert.equal(read("changelog.d/previous.fix.md"), "修复已有问题。\n");
  });

  test("目录里的点文件（.DS_Store、.gitkeep）不影响检查和发版", (t) => {
    const { git, run, write } = fixture(t);
    write("changelog.d/.gitkeep", "");
    git("add", ".");
    git("commit", "-qm", "chore: 保留目录");
    write("changelog.d/.DS_Store", "\0finder");
    assert.equal(run("lint").status, 0, run("lint").stderr);
    const preview = run("release", "1.1.0", "2026-11-25");
    assert.equal(preview.status, 0, preview.stderr);
    assert.doesNotMatch(preview.stdout, /DS_Store|gitkeep/);
  });

  test("发版遇到未提交的日志文件时拒绝执行，不发布也不删除", (t) => {
    const { cwd, read, run, write } = fixture(t);
    write("changelog.d/wip.feat.md", "还没合并的功能。\n");
    const result = run("release", "1.1.0", "2026-11-25", "--write");
    assert.notEqual(result.status, 0);
    assert.match(result.stderr, /wip\.feat\.md/);
    assert.equal(read("changelog.d/wip.feat.md"), "还没合并的功能。\n");
    assert.equal(read("VERSION"), "1.0.0\n");
    rmSync(join(cwd, "changelog.d/wip.feat.md"));
    write("changelog.d/previous.fix.md", "改了但没提交。\n");
    assert.match(run("release", "1.1.0", "2026-11-25").stderr, /previous\.fix\.md/);
  });

  test("PR 改了 [Unreleased] 时要求它保持为空且格式正确", (t) => {
    const { git, read, run, write } = fixture(t);
    const base = git("rev-parse", "HEAD");
    write("CHANGELOG.md", read("CHANGELOG.md").replace("## [Unreleased]\n", "## [Unreleased]\n\n### 修复\n\n修复保存问题\n"));
    git("add", ".");
    git("commit", "-qm", "docs: 写日志");
    assert.match(run("pr", base).stderr, /changelog\.d|Unreleased/);
  });

  test("PR 不能删除待发布的日志，发版、改类别和 skip-changelog 除外", (t) => {
    const { cwd, git, run, write } = fixture(t);
    const base = git("rev-parse", "HEAD");
    write("changelog.d/mine.fix.md", "修复我的问题。\n");
    git("rm", "-q", "changelog.d/previous.fix.md");
    git("add", ".");
    git("commit", "-qm", "fix: 修复我的问题");
    const result = run("pr", base);
    assert.notEqual(result.status, 0);
    assert.match(result.stderr, /previous\.fix\.md/);
    const skipped = spawnSync(process.execPath, [script, "pr", base], {
      cwd,
      encoding: "utf8",
      env: { ...process.env, CHANGELOG_SKIP: "1" },
    });
    assert.equal(skipped.status, 0, skipped.stderr);

    git("reset", "-q", "--hard", base);
    git("mv", "changelog.d/previous.fix.md", "changelog.d/previous.feat.md");
    git("commit", "-qm", "docs: 调整日志类别");
    assert.equal(run("pr", base).status, 0, run("pr", base).stderr);

    git("reset", "-q", "--hard", base);
    assert.equal(run("release", "1.1.0", "2026-11-25", "--write").status, 0);
    git("add", "-A");
    git("commit", "-qm", "docs: update changelog for v1.1.0");
    assert.equal(run("pr", base).status, 0, run("pr", base).stderr);
  });

  test("基准提交没有 CHANGELOG.md 时视为空", (t) => {
    const { git, read, run, write } = fixture(t);
    const content = read("CHANGELOG.md");
    git("rm", "-q", "CHANGELOG.md");
    git("commit", "-qm", "chore: 暂时去掉");
    const base = git("rev-parse", "HEAD");
    write("CHANGELOG.md", content);
    git("add", ".");
    git("commit", "-qm", "chore: 加回");
    assert.equal(run("pr", base).status, 0, run("pr", base).stderr);
  });

  test("本地钩子拦截暂存到 [Unreleased] 的新条目", (t) => {
    const { git, read, run, write } = fixture(t);
    write("message", "fix: 修复保存\n");
    write("CHANGELOG.md", read("CHANGELOG.md").replace("## [Unreleased]\n", "## [Unreleased]\n\n### 修复\n\n- 修复保存。\n"));
    git("add", "CHANGELOG.md");
    assert.match(run("commit-msg", "message").stderr, /changelog\.d/);
  });
});
