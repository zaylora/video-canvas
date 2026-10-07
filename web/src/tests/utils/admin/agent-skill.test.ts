import { describe, expect, test } from "bun:test";

import type {
  SkillFile,
  SkillImportView,
  SkillIssue,
  SkillItem,
} from "@/api/admin/agent-skill/type.d";
import {
  bannerTone,
  buildFileTree,
  confirmBlockReason,
  countIssues,
  deleteBlockReason,
  fileTagLabel,
  filterSkillItems,
  formatDateTime,
  formatSize,
  importedMessage,
  issueLevelByPath,
  locateLine,
  planSummary,
  sortIssues,
  stripFrontmatter,
  toggleBlockReason,
  versionBadge,
  versionState,
  visibleTreeRows,
} from "@/utils/admin/agent-skill";

const issue = (level: SkillIssue["level"], code: string, path?: string): SkillIssue => ({
  level,
  code,
  path,
  message: `${code} 说明`,
});

const file = (path: string, extra: Partial<SkillFile> = {}): SkillFile => ({
  path,
  size: 10,
  kind: "doc",
  text: true,
  ...extra,
});

const item = (extra: Partial<SkillItem>): SkillItem => ({
  name: "demo",
  title: "演示",
  description: "说明",
  source: "imported",
  readonly: false,
  enabled: false,
  active_version: 1,
  latest_version: 1,
  pending_version: false,
  version_count: 1,
  file_count: 1,
  total_bytes: 1,
  has_scripts: false,
  unsupported_fields: [],
  updated_at: "2026-01-01T00:00:00Z",
  ...extra,
});

const view = (extra: Partial<SkillImportView>): SkillImportView => ({
  id: "i1",
  name: "demo",
  title: "演示",
  description: "说明",
  frontmatter: {},
  unsupported_fields: [],
  files: [],
  has_scripts: false,
  total_bytes: 0,
  issues: [],
  can_confirm: true,
  plan: { action: "create", version: 1, enabled: false },
  ...extra,
});

describe("formatSize", () => {
  test("按量级换算单位", () => {
    expect(formatSize(0)).toBe("0 B");
    expect(formatSize(520)).toBe("520 B");
    expect(formatSize(1843)).toBe("1.8 KB");
    expect(formatSize(5 * 1048576)).toBe("5.0 MB");
  });
});

describe("fileTagLabel", () => {
  test("说明、脚本带语言、二进制、文档、资源", () => {
    expect(fileTagLabel(file("SKILL.md", { kind: "skill" }))).toBe("说明");
    expect(fileTagLabel(file("a.py", { kind: "script", lang: "python" }))).toBe("脚本·python");
    expect(fileTagLabel(file("a.png", { kind: "asset", text: false }))).toBe("二进制");
    expect(fileTagLabel(file("a.md", { kind: "doc" }))).toBe("文档");
    expect(fileTagLabel(file("assets/a.json", { kind: "asset" }))).toBe("资源");
  });
});

describe("问题整理", () => {
  const issues = [
    issue("info", "IGNORED_FILES"),
    issue("warn", "MISSING_REF", "SKILL.md"),
    issue("error", "BAD_NAME"),
    issue("warn", "LONG_SKILL_MD", "SKILL.md"),
    issue("error", "FILE_TOO_LARGE", "assets/big.bin"),
  ];

  test("错误在前、提示其次、信息最后，同级保持原顺序", () => {
    expect(sortIssues(issues).map((i) => i.code)).toEqual([
      "BAD_NAME",
      "FILE_TOO_LARGE",
      "MISSING_REF",
      "LONG_SKILL_MD",
      "IGNORED_FILES",
    ]);
  });

  test("不改动传入的数组", () => {
    const copy = [...issues];
    sortIssues(issues);
    expect(issues).toEqual(copy);
  });

  test("统计各级数量；null 当作空", () => {
    expect(countIssues(issues)).toEqual({ error: 2, warn: 2, info: 1, problems: 4 });
    expect(countIssues(null)).toEqual({ error: 0, warn: 0, info: 0, problems: 0 });
  });

  test("按路径取最高级别，信息级不标记", () => {
    const map = issueLevelByPath([
      issue("warn", "A", "x.md"),
      issue("error", "B", "x.md"),
      issue("info", "C", "y.md"),
      issue("warn", "D"),
    ]);
    expect(map.get("x.md")).toBe("error");
    expect(map.has("y.md")).toBe(false);
    expect(map.size).toBe(1);
  });
});

describe("buildFileTree / visibleTreeRows", () => {
  const files = [
    file("references/tone.md"),
    file("SKILL.md", { kind: "skill" }),
    file("references/examples/a.md"),
    file("scripts/run.py", { kind: "script", lang: "python" }),
    file("notes.md"),
  ];

  test("目录在前、文件在后，根下 SKILL.md 排第一", () => {
    const rows = visibleTreeRows(buildFileTree(files), new Set());
    expect(rows.map((r) => `${r.depth}:${r.node.path}`)).toEqual([
      "0:references",
      "1:references/examples",
      "2:references/examples/a.md",
      "1:references/tone.md",
      "0:scripts",
      "1:scripts/run.py",
      "0:SKILL.md",
      "0:notes.md",
    ]);
  });

  test("折叠的目录不展示子孙", () => {
    const rows = visibleTreeRows(buildFileTree(files), new Set(["references"]));
    expect(rows.map((r) => r.node.path)).toEqual([
      "references",
      "scripts",
      "scripts/run.py",
      "SKILL.md",
      "notes.md",
    ]);
  });

  test("没有文件时为空", () => {
    expect(buildFileTree([])).toEqual([]);
  });
});

describe("locateLine", () => {
  const text = "a\nsee scripts/run.py here\nb";
  test("返回第一次出现的行号（从 1 起）", () => {
    expect(locateLine(text, "scripts/run.py")).toBe(2);
  });
  test("找不到或空串返回 null", () => {
    expect(locateLine(text, "nope")).toBeNull();
    expect(locateLine(text, "")).toBeNull();
  });
});

describe("导入预检的文案", () => {
  test("横幅：有错误红、仅提示黄、干净绿；不能确认也算红", () => {
    expect(bannerTone(view({ issues: [issue("error", "X")], can_confirm: false }))).toBe("bad");
    expect(bannerTone(view({ issues: [], can_confirm: false }))).toBe("bad");
    expect(bannerTone(view({ issues: [issue("warn", "X")] }))).toBe("warn");
    expect(bannerTone(view({ issues: [issue("info", "X")] }))).toBe("ok");
  });

  test("确认被禁用的原因写错误个数", () => {
    expect(confirmBlockReason(view({}))).toBeNull();
    expect(
      confirmBlockReason(
        view({ can_confirm: false, issues: [issue("error", "A"), issue("error", "B")] }),
      ),
    ).toBe("有 2 个错误需要先处理");
    expect(confirmBlockReason(view({ can_confirm: false, issues: [] }))).toBe(
      "预检未通过，不能导入",
    );
  });

  test("将要发生什么：新技能、同名新版本、被拦住", () => {
    expect(planSummary(view({ plan: { action: "create", version: 1, enabled: false } }))).toBe(
      "将创建技能 demo，v1，默认停用",
    );
    expect(
      planSummary(
        view({ plan: { action: "new_version", version: 3, active_version: 2, enabled: true } }),
      ),
    ).toBe("将为 demo 新增 v3；当前生效版本仍是 v2，需要你在详情里手动切换");
    expect(
      planSummary(
        view({
          can_confirm: false,
          plan: { action: "blocked", enabled: false },
          issues: [issue("error", "BUILTIN_NAME")],
        }),
      ),
    ).toBe("BUILTIN_NAME 说明");
  });
});

describe("列表", () => {
  const list = [
    item({ name: "a", title: "对白润色", enabled: true, description: "改对白" }),
    item({ name: "b", title: "九宫格", enabled: false, description: "拆镜头" }),
    item({ name: "c", title: "剧本拆镜", source: "builtin", readonly: true, enabled: true }),
  ];

  test("按名称、技能名、说明搜索，忽略大小写与首尾空格", () => {
    expect(filterSkillItems(list, " 对白 ", "all").map((i) => i.name)).toEqual(["a"]);
    expect(filterSkillItems(list, "B", "all").map((i) => i.name)).toEqual(["b"]);
    expect(filterSkillItems(list, "拆", "all").map((i) => i.name)).toEqual(["b", "c"]);
  });

  test("状态筛选：已启用、已停用、内置", () => {
    expect(filterSkillItems(list, "", "enabled").map((i) => i.name)).toEqual(["a", "c"]);
    expect(filterSkillItems(list, "", "disabled").map((i) => i.name)).toEqual(["b"]);
    expect(filterSkillItems(list, "", "builtin").map((i) => i.name)).toEqual(["c"]);
    expect(filterSkillItems(list, "", "all")).toHaveLength(3);
  });

  test("版本标记：内置显示 —，待生效带最新版本号", () => {
    expect(versionBadge(item({ source: "builtin", active_version: null }))).toEqual({
      active: "—",
      pending: null,
    });
    expect(
      versionBadge(item({ active_version: 2, latest_version: 3, pending_version: true })),
    ).toEqual({
      active: "v2",
      pending: "v3 待生效",
    });
    expect(versionBadge(item({ active_version: 2, latest_version: 2 }))).toEqual({
      active: "v2",
      pending: null,
    });
  });

  test("开关被禁用的原因：内置不能改；没有生效版本不能启用", () => {
    expect(toggleBlockReason(item({ readonly: true, source: "builtin" }))).toBe(
      "内置技能随版本发布，不能停用",
    );
    expect(toggleBlockReason(item({ active_version: null }))).toBe("没有生效版本，无法启用");
    expect(toggleBlockReason(item({}))).toBeNull();
    expect(toggleBlockReason(item({ active_version: null, enabled: true }))).toBeNull();
  });

  test("删除被禁用的原因：内置、启用中", () => {
    expect(deleteBlockReason(item({ readonly: true }))).toBe("内置技能不能删除");
    expect(deleteBlockReason(item({ enabled: true }))).toBe("请先停用再删除");
    expect(deleteBlockReason(item({}))).toBeNull();
  });
});

describe("stripFrontmatter", () => {
  test("去掉开头的 frontmatter，返回正文和被去掉的行数", () => {
    const text = "---\nname: a\ndescription: b\n---\n\n# 标题\n正文";
    expect(stripFrontmatter(text)).toEqual({ body: "# 标题\n正文", skippedLines: 4 });
  });
  test("没有 frontmatter 或没有结束线时原样返回", () => {
    expect(stripFrontmatter("# 标题")).toEqual({ body: "# 标题", skippedLines: 0 });
    expect(stripFrontmatter("---\nname: a\n正文")).toEqual({
      body: "---\nname: a\n正文",
      skippedLines: 0,
    });
  });
});

describe("importedMessage", () => {
  test("新技能默认停用；新版本说明生效版本不变", () => {
    expect(importedMessage(view({}))).toBe("已导入「demo」v1，默认停用");
    expect(
      importedMessage(
        view({ plan: { action: "new_version", version: 3, active_version: 2, enabled: true } }),
      ),
    ).toBe("已为「demo」新增 v3，生效版本不变，需要在详情里手动切换");
  });
});

describe("formatDateTime / versionState", () => {
  test("补零到分钟；解析不了原样返回", () => {
    expect(formatDateTime("2026-01-02T03:04:05")).toBe("2026-01-02 03:04");
    expect(formatDateTime("不是时间")).toBe("不是时间");
  });

  test("版本状态：生效中、待生效（比生效版本新）、历史", () => {
    expect(versionState(2, 2)).toBe("active");
    expect(versionState(3, 2)).toBe("pending");
    expect(versionState(1, 2)).toBe("history");
    expect(versionState(1, null)).toBe("history");
  });
});
