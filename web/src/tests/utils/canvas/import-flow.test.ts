import { describe, expect, mock, test } from "bun:test";

import type { CanvasImportResultDto } from "@/api/canvas/type";
import { runCanvasImport, summarizeImportReport } from "@/utils/canvas/import-flow";
import { MAX_IMPORT_BYTES } from "@/utils/canvas/transfer-file";

type Report = CanvasImportResultDto["report"];

const validDoc = { format: "video-canvas/canvas", version: 1, canvas: { title: "雨夜短片" } };

/** 一份没有任何需要注意项的报告，用例按需覆盖其中一项 */
function cleanReport(overrides: Partial<Record<string, unknown>> = {}): Report {
  return {
    assets: { total: 2, copied: 2, missing: [] },
    characters: { created: 1, renamed: [], invalid: [] },
    edgesDropped: 0,
    ...overrides,
  } as Report;
}

/** 组装文件和请求；文本默认是合法的导出文件，请求默认返回干净报告 */
function setup(options: { size?: number; text?: string; report?: Report; reject?: boolean } = {}) {
  const text = options.text ?? JSON.stringify(validDoc);
  const file = {
    size: options.size ?? text.length,
    text: mock(async () => text),
  };
  const importCanvas = mock(async (_document: unknown) => {
    if (options.reject) throw new Error("服务端拒绝");
    return {
      canvas: { id: "88", title: "雨夜短片（导入）", revision: 1 },
      report: options.report ?? cleanReport(),
    };
  });
  return { file, importCanvas };
}

describe("runCanvasImport：列表页导入流程", () => {
  test("文件超过上限：提示过大，不读文件也不发请求", async () => {
    const deps = setup({ size: MAX_IMPORT_BYTES + 1 });
    const outcome = await runCanvasImport(deps);

    expect(outcome).toEqual({ kind: "invalid", message: "文件过大，上限 10 MB" });
    expect(deps.file.text).not.toHaveBeenCalled();
    expect(deps.importCanvas).not.toHaveBeenCalled();
  });

  test("读取文件失败：提示读取失败，不发请求也不抛错", async () => {
    const deps = setup();
    deps.file.text.mockImplementation(async () => {
      throw new Error("NotReadableError");
    });
    const outcome = await runCanvasImport(deps);

    expect(outcome).toEqual({ kind: "invalid", message: "读取文件失败" });
    expect(deps.importCanvas).not.toHaveBeenCalled();
  });

  test("不是 JSON：invalid，不发请求", async () => {
    const deps = setup({ text: "这不是 JSON" });
    const outcome = await runCanvasImport(deps);

    expect(outcome).toEqual({ kind: "invalid", message: "不是有效的 JSON 文件" });
    expect(deps.importCanvas).not.toHaveBeenCalled();
  });

  test("format 不对：invalid，不发请求", async () => {
    const deps = setup({ text: JSON.stringify({ ...validDoc, format: "other/thing" }) });
    const outcome = await runCanvasImport(deps);

    expect(outcome).toEqual({ kind: "invalid", message: "不是本系统导出的画布文件" });
    expect(deps.importCanvas).not.toHaveBeenCalled();
  });

  test("版本高于当前支持：invalid，不发请求", async () => {
    const deps = setup({ text: JSON.stringify({ ...validDoc, version: 2 }) });
    const outcome = await runCanvasImport(deps);

    expect(outcome).toEqual({ kind: "invalid", message: "文件来自更新版本，请升级后再导入" });
    expect(deps.importCanvas).not.toHaveBeenCalled();
  });

  test("请求失败：只返回 error，不带文案（全局提示已弹）", async () => {
    const deps = setup({ reject: true });
    const outcome = await runCanvasImport(deps);

    expect(outcome).toEqual({ kind: "error" });
  });

  test("干净报告：opened，请求只发一次且带解析后的文档", async () => {
    const deps = setup();
    const outcome = await runCanvasImport(deps);

    expect(outcome).toEqual({ kind: "opened", canvasId: "88", title: "雨夜短片（导入）" });
    expect(deps.importCanvas).toHaveBeenCalledTimes(1);
    expect(deps.importCanvas.mock.calls[0]?.[0]).toEqual(validDoc);
  });

  test("有素材找不到：report，带回报告", async () => {
    const report = cleanReport({
      assets: { total: 2, copied: 1, missing: [{ nodeId: "n7", fileName: "雨夜.mp4" }] },
    });
    const outcome = await runCanvasImport(setup({ report }));

    expect(outcome).toEqual({
      kind: "report",
      canvasId: "88",
      title: "雨夜短片（导入）",
      report,
    });
  });

  test("只有角色卡重名：report", async () => {
    const report = cleanReport({
      characters: { created: 1, renamed: [{ from: "老王", to: "老王 (2)" }], invalid: [] },
    });
    const outcome = await runCanvasImport(setup({ report }));

    expect(outcome.kind).toBe("report");
  });

  test("只有角色卡主图缺失：report", async () => {
    const report = cleanReport({
      characters: { created: 0, renamed: [], invalid: [{ nodeId: "n9", name: "老王" }] },
    });
    const outcome = await runCanvasImport(setup({ report }));

    expect(outcome.kind).toBe("report");
  });

  test("只有连线被丢弃：report", async () => {
    const outcome = await runCanvasImport(setup({ report: cleanReport({ edgesDropped: 1 }) }));

    expect(outcome.kind).toBe("report");
  });
});

describe("summarizeImportReport：导入结果弹窗的数量", () => {
  test("干净报告：总数为 0", () => {
    expect(summarizeImportReport(cleanReport())).toEqual({
      missing: 0,
      renamed: 0,
      invalid: 0,
      edgesDropped: 0,
      total: 0,
    });
  });

  test("各项分别计数；连线被丢弃整体只算 1 项", () => {
    const report = cleanReport({
      assets: {
        total: 3,
        copied: 1,
        missing: [
          { nodeId: "n1", fileName: "a.png" },
          { nodeId: "n2", fileName: "b.png" },
        ],
      },
      characters: {
        created: 1,
        renamed: [{ from: "甲", to: "甲 (2)" }],
        invalid: [{ nodeId: "n9", name: "乙" }],
      },
      edgesDropped: 5,
    });

    expect(summarizeImportReport(report)).toEqual({
      missing: 2,
      renamed: 1,
      invalid: 1,
      edgesDropped: 5,
      total: 5,
    });
  });
});
