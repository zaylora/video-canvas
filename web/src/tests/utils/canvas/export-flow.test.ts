import { describe, expect, mock, test } from "bun:test";

import type { exportCanvas } from "@/api/canvas";
import { runCanvasExport } from "@/utils/canvas/export-flow";
import { exportFileName } from "@/utils/canvas/transfer-file";

type Deps = Parameters<typeof runCanvasExport>[0];
type ExportResult = Awaited<ReturnType<typeof exportCanvas>>;

const doc = { format: "video-canvas/canvas", version: 1 } as unknown as ExportResult["document"];

/** 组装一套可观察的依赖，默认保存成功、导出成功且没有跳过素材 */
function setup(overrides: Partial<Deps> = {}) {
  const deps = {
    canvasId: "42",
    title: "雨夜短片",
    saveNow: mock(async () => true),
    exportCanvas: mock(async () => ({ document: doc, skippedAssets: 0 })),
    download: mock((_name: string, _data: unknown) => {}),
    notify: { success: mock(), warning: mock(), error: mock() },
    ...overrides,
  };
  return deps as typeof deps & Deps;
}

describe("runCanvasExport：画布内导出流程", () => {
  test("保存没成功：不请求导出，提示还没保存上", async () => {
    const deps = setup({ saveNow: mock(async () => false) });
    await runCanvasExport(deps);

    expect(deps.exportCanvas).not.toHaveBeenCalled();
    expect(deps.download).not.toHaveBeenCalled();
    expect(deps.notify.error).toHaveBeenCalledTimes(1);
    expect(String(deps.notify.error.mock.calls[0]?.[0])).toContain("没保存上");
    expect(deps.notify.success).not.toHaveBeenCalled();
  });

  test("导出成功：按标题生成文件名下载一次，提示成功，不弹警告", async () => {
    const deps = setup();
    await runCanvasExport(deps);

    expect(deps.exportCanvas).toHaveBeenCalledWith("42");
    expect(deps.download).toHaveBeenCalledTimes(1);
    expect(deps.download).toHaveBeenCalledWith(exportFileName("雨夜短片"), doc);
    expect(deps.notify.success).toHaveBeenCalledTimes(1);
    expect(deps.notify.warning).not.toHaveBeenCalled();
    expect(deps.notify.error).not.toHaveBeenCalled();
  });

  test("有素材被跳过：仍然下载，并额外警告跳过的个数", async () => {
    const deps = setup({ exportCanvas: mock(async () => ({ document: doc, skippedAssets: 2 })) });
    await runCanvasExport(deps);

    expect(deps.download).toHaveBeenCalledTimes(1);
    expect(deps.notify.success).toHaveBeenCalledTimes(1);
    expect(deps.notify.warning).toHaveBeenCalledTimes(1);
    expect(String(deps.notify.warning.mock.calls[0]?.[0])).toContain("2");
  });

  test("导出请求失败：提示失败、不下载，也不向外抛错", async () => {
    const deps = setup({
      exportCanvas: mock(async () => {
        throw new Error("boom");
      }),
    });
    await expect(runCanvasExport(deps)).resolves.toBeUndefined();

    expect(deps.download).not.toHaveBeenCalled();
    expect(deps.notify.success).not.toHaveBeenCalled();
    expect(deps.notify.error).toHaveBeenCalledTimes(1);
    expect(deps.notify.error).toHaveBeenCalledWith("导出失败，请稍后重试");
  });
});
