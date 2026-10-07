import { describe, expect, test } from "bun:test";

import type { SkillImportView } from "@/api/admin/agent-skill/type.d";
import {
  IMPORT_LIMITS,
  confirmFailureNotice,
  importReducer,
  initialImportState,
  normalizeEntryPath,
  preparePick,
  readDroppedEntries,
  stepOf,
  type FsEntryLike,
  type ImportState,
} from "@/utils/admin/agent-skill-import";

const MB = 1048576;
const f = (name: string, size = 10) => ({ name, size });
const pick = (name: string, size = 10, path = name) => ({ file: f(name, size), path });

const view = (extra: Partial<SkillImportView> = {}): SkillImportView => ({
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

describe("normalizeEntryPath", () => {
  test("统一成 / 分隔，去掉开头的 ./ 和 /，合并重复斜杠", () => {
    expect(normalizeEntryPath("/skill/SKILL.md")).toBe("skill/SKILL.md");
    expect(normalizeEntryPath("./skill//a.md")).toBe("skill/a.md");
    expect(normalizeEntryPath("skill\\sub\\a.md")).toBe("skill/sub/a.md");
  });
});

describe("preparePick：上传前先拦", () => {
  test("单个 zip 直接作为压缩包", () => {
    const r = preparePick([pick("a.ZIP", 3 * MB)]);
    expect(r.ok && r.input.kind).toBe("zip");
  });

  test("zip 超过 50 MB 被拦并给出下一步", () => {
    const r = preparePick([pick("a.zip", IMPORT_LIMITS.zipBytes + 1)]);
    expect(r.ok).toBe(false);
    if (!r.ok) {
      expect(r.error).toContain("50 MB");
      expect(r.hint).toContain("重新打包");
    }
  });

  test("zip 与其他文件混选被拒绝：一次只导一个技能", () => {
    const r = preparePick([pick("a.zip"), pick("SKILL.md")]);
    expect(r.ok).toBe(false);
  });

  test("单个 SKILL.md 路径就是文件名", () => {
    const r = preparePick([pick("SKILL.md")]);
    expect(r.ok && r.input).toEqual({
      kind: "files",
      entries: [{ file: f("SKILL.md"), path: "SKILL.md" }],
      ignored: 0,
    });
  });

  test("文件夹：路径整理后保留外层目录，并丢掉系统垃圾文件", () => {
    const r = preparePick([
      pick("SKILL.md", 10, "/demo/SKILL.md"),
      pick("tone.md", 10, "./demo/references/tone.md"),
      pick(".DS_Store", 10, "demo/.DS_Store"),
      pick("x", 10, "__MACOSX/demo/._SKILL.md"),
      pick("HEAD", 10, "demo/.git/HEAD"),
      pick("Thumbs.db", 10, "demo/Thumbs.db"),
    ]);
    expect(r.ok).toBe(true);
    if (r.ok && r.input.kind === "files") {
      expect(r.input.entries.map((e) => e.path)).toEqual([
        "demo/SKILL.md",
        "demo/references/tone.md",
      ]);
      expect(r.input.ignored).toBe(4);
    }
  });

  test("文件超过 500 个、总量超过 100 MB、单文件超过 25 MB 都被拦", () => {
    const many = Array.from({ length: 501 }, (_, i) => pick(`f${i}.md`, 1, `d/f${i}.md`));
    const tooMany = preparePick(many);
    expect(!tooMany.ok && tooMany.error).toContain("500");

    const big = [
      pick("a.bin", 24 * MB, "d/a.bin"),
      pick("b.bin", 24 * MB, "d/b.bin"),
      pick("c.bin", 24 * MB, "d/c.bin"),
      pick("d.bin", 24 * MB, "d/d.bin"),
      pick("e.bin", 10 * MB, "d/e.bin"),
    ];
    const tooBig = preparePick(big);
    expect(!tooBig.ok && tooBig.error).toContain("100 MB");

    const huge = preparePick([pick("a.bin", 26 * MB, "d/a.bin")]);
    expect(!huge.ok && huge.error).toContain("25 MB");
  });

  test("恰好在限额上可以通过", () => {
    const r = preparePick(Array.from({ length: 500 }, (_, i) => pick(`f${i}`, 1, `d/f${i}`)));
    expect(r.ok).toBe(true);
    expect(preparePick([pick("a.zip", IMPORT_LIMITS.zipBytes)]).ok).toBe(true);
  });

  test("没有读到文件、或过滤后什么都不剩", () => {
    expect(preparePick([]).ok).toBe(false);
    expect(preparePick([pick(".DS_Store", 1, "d/.DS_Store")]).ok).toBe(false);
  });
});

describe("importReducer 状态转移", () => {
  const uploading = importReducer(initialImportState, { type: "start", name: "demo.zip" });

  test("初始在第 1 步", () => {
    expect(initialImportState).toEqual({ step: "pick", notice: null });
    expect(stepOf(initialImportState)).toBe(1);
  });

  test("开始上传：进度 0，阶段是上传", () => {
    expect(uploading).toEqual({
      step: "uploading",
      name: "demo.zip",
      phase: "upload",
      progress: 0,
    });
    expect(stepOf(uploading)).toBe(1);
  });

  test("进度只增不减；传完进入“正在检查”阶段", () => {
    const a = importReducer(uploading, { type: "progress", ratio: 0.5 });
    const b = importReducer(a, { type: "progress", ratio: 0.3 });
    expect(b).toMatchObject({ phase: "upload", progress: 0.5 });
    const c = importReducer(b, { type: "progress", ratio: 1 });
    expect(c).toMatchObject({ phase: "check", progress: 1 });
  });

  test("预检结果有暂存 id：进入第 2 步；即使有错误也进入", () => {
    const v = view({
      can_confirm: false,
      issues: [{ level: "error", code: "BAD_NAME", message: "x" }],
    });
    const s = importReducer(uploading, { type: "uploaded", view: v });
    expect(s).toEqual({ step: "review", view: v, confirming: false });
    expect(stepOf(s)).toBe(2);
  });

  test("包完全不可读（id 为空）：回到第 1 步并写原因，不进入第 2 步", () => {
    const v = view({
      id: "",
      can_confirm: false,
      issues: [{ level: "error", code: "NO_SKILL_MD", message: "没有找到 SKILL.md" }],
    });
    expect(importReducer(uploading, { type: "uploaded", view: v })).toEqual({
      step: "pick",
      notice: "没有找到 SKILL.md",
    });
    expect(
      importReducer(uploading, { type: "uploaded", view: view({ id: "", issues: [] }) }),
    ).toEqual({
      step: "pick",
      notice: "无法读取这个包，请确认是有效的技能压缩包、文件夹或 SKILL.md",
    });
  });

  test("上传失败 / 取消 / 前端校验不通过", () => {
    expect(importReducer(uploading, { type: "failed", message: "网络异常" })).toEqual({
      step: "pick",
      notice: "网络异常",
    });
    expect(importReducer(uploading, { type: "cancelled" })).toEqual({ step: "pick", notice: null });
    expect(importReducer(initialImportState, { type: "rejected", message: "超限" })).toEqual({
      step: "pick",
      notice: "超限",
    });
  });

  test("已在检查结果页时，前端校验不通过不丢掉当前结果", () => {
    const review: ImportState = { step: "review", view: view(), confirming: false };
    expect(importReducer(review, { type: "rejected", message: "超限" })).toBe(review);
  });

  test("上传中不响应新的 start", () => {
    expect(importReducer(uploading, { type: "start", name: "other.zip" })).toBe(uploading);
  });

  test("在第 2 步重新选择文件可以再次 start；确认中不可以", () => {
    const review: ImportState = { step: "review", view: view(), confirming: false };
    expect(importReducer(review, { type: "start", name: "b.zip" })).toMatchObject({
      step: "uploading",
    });
    const confirming = importReducer(review, { type: "confirm" });
    expect(confirming).toEqual({ step: "review", view: review.view, confirming: true });
    expect(importReducer(confirming, { type: "start", name: "b.zip" })).toBe(confirming);
    expect(importReducer(confirming, { type: "reselect" })).toBe(confirming);
  });

  test("有错误的预检不能进入确认", () => {
    const bad: ImportState = {
      step: "review",
      view: view({ can_confirm: false }),
      confirming: false,
    };
    expect(importReducer(bad, { type: "confirm" })).toBe(bad);
  });

  test("确认失败：回到第 1 步并保留原因", () => {
    const confirming: ImportState = { step: "review", view: view(), confirming: true };
    expect(importReducer(confirming, { type: "confirmFailed", message: "预检结果已过期" })).toEqual(
      {
        step: "pick",
        notice: "预检结果已过期",
      },
    );
  });

  test("重新选择：回到第 1 步，没有提示", () => {
    const review: ImportState = { step: "review", view: view(), confirming: false };
    expect(importReducer(review, { type: "reselect" })).toEqual({ step: "pick", notice: null });
  });

  test("reset 回到初始", () => {
    expect(importReducer(uploading, { type: "reset" })).toEqual(initialImportState);
  });
});

describe("confirmFailureNotice", () => {
  test("暂存过期补一句下一步，其余直接用后端说明", () => {
    expect(confirmFailureNotice({ code: 61001, message: "导入已过期" })).toBe(
      "导入已过期，请重新选择文件",
    );
    expect(confirmFailureNotice({ code: 61006, message: "与 v2 内容相同" })).toBe("与 v2 内容相同");
    expect(confirmFailureNotice(new Error(""))).toBe("导入失败，请重新选择文件");
  });
});

describe("readDroppedEntries：递归读目录", () => {
  const fileEntry = (fullPath: string, size = 1): FsEntryLike => ({
    isFile: true,
    isDirectory: false,
    name: fullPath.split("/").pop() ?? "",
    fullPath,
    file: (ok) => ok({ name: fullPath.split("/").pop() ?? "", size } as File),
  });

  /** 目录的 readEntries 按批返回，读到空数组才算读完（和浏览器一致，一批最多 100 条） */
  const dirEntry = (fullPath: string, children: FsEntryLike[], batch = 2): FsEntryLike => ({
    isFile: false,
    isDirectory: true,
    name: fullPath.split("/").pop() ?? "",
    fullPath,
    createReader: () => {
      let offset = 0;
      return {
        readEntries: (ok) => {
          const slice = children.slice(offset, offset + batch);
          offset += batch;
          ok(slice);
        },
      };
    },
  });

  test("分批读完所有子项，路径取 fullPath 并去掉开头斜杠", async () => {
    const tree = dirEntry("/demo", [
      fileEntry("/demo/SKILL.md"),
      dirEntry("/demo/references", [
        fileEntry("/demo/references/a.md"),
        fileEntry("/demo/references/b.md"),
        fileEntry("/demo/references/c.md"),
      ]),
      fileEntry("/demo/notes.md"),
    ]);
    const out = await readDroppedEntries([tree]);
    expect(out.map((e) => e.path).sort()).toEqual([
      "demo/SKILL.md",
      "demo/notes.md",
      "demo/references/a.md",
      "demo/references/b.md",
      "demo/references/c.md",
    ]);
  });

  test("直接拖入的单个文件，路径就是文件名", async () => {
    const out = await readDroppedEntries([fileEntry("/SKILL.md", 7)]);
    expect(out).toHaveLength(1);
    expect(out[0].path).toBe("SKILL.md");
    expect(out[0].file.size).toBe(7);
  });

  test("读某个文件失败时整体失败，不悄悄丢文件", async () => {
    const bad: FsEntryLike = {
      isFile: true,
      isDirectory: false,
      name: "x",
      fullPath: "/x",
      file: (_ok, fail) => fail(new Error("读不到")),
    };
    await expect(readDroppedEntries([bad])).rejects.toThrow("读不到");
  });
});
