import { describe, expect, mock, test } from "bun:test";
import axios from "axios";

import type { exportCanvas } from "@/api/canvas";
import { runCanvasExport } from "@/utils/canvas/export-flow";
import { createTransferCancel } from "@/utils/canvas/transfer-cancel";
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

    expect(deps.exportCanvas).toHaveBeenCalledWith("42", undefined);
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

  test("导出请求失败：不下载、不再重复提示（请求层已弹全局提示），也不向外抛错", async () => {
    const deps = setup({
      exportCanvas: mock(async () => {
        throw new Error("boom");
      }),
    });
    await expect(runCanvasExport(deps)).resolves.toBe("failed");

    expect(deps.download).not.toHaveBeenCalled();
    expect(deps.notify.success).not.toHaveBeenCalled();
    expect(deps.notify.warning).not.toHaveBeenCalled();
    expect(deps.notify.error).not.toHaveBeenCalled();
  });

  test("成功时返回 done", async () => {
    expect(await runCanvasExport(setup())).toBe("done");
  });

  test("请求失败（非取消）返回 failed", async () => {
    const deps = setup({
      exportCanvas: mock(async () => {
        throw new Error("boom");
      }),
    });
    expect(await runCanvasExport(deps)).toBe("failed");
  });

  test("保存没成功返回 failed，不请求", async () => {
    const deps = setup({ saveNow: mock(async () => false) });
    expect(await runCanvasExport(deps)).toBe("failed");
    expect(deps.exportCanvas).not.toHaveBeenCalled();
  });

  test("请求被取消：返回 cancelled，不下载、不提示成功也不提示错误", async () => {
    const cancel = createTransferCancel();
    const deps = setup({
      signal: cancel.signal,
      exportCanvas: mock(async () => {
        cancel.cancel();
        throw new axios.CanceledError("canceled");
      }),
    });
    expect(await runCanvasExport(deps)).toBe("cancelled");

    expect(deps.download).not.toHaveBeenCalled();
    expect(deps.notify.success).not.toHaveBeenCalled();
    expect(deps.notify.error).not.toHaveBeenCalled();
    expect(deps.notify.warning).not.toHaveBeenCalled();
  });

  test("取消时请求恰好已成功：按成功处理，下载文件，不返回 cancelled", async () => {
    const cancel = createTransferCancel();
    const deps = setup({
      signal: cancel.signal,
      exportCanvas: mock(async () => {
        cancel.cancel(); // 取消发生在响应已经到达之后：请求没有抛错
        return { document: doc, skippedAssets: 0 };
      }),
    });
    expect(await runCanvasExport(deps)).toBe("done");
    expect(deps.download).toHaveBeenCalledTimes(1);
    expect(deps.notify.success).toHaveBeenCalledTimes(1);
  });

  test("保存阶段就已取消：不再请求导出", async () => {
    const cancel = createTransferCancel();
    const deps = setup({
      signal: cancel.signal,
      saveNow: mock(async () => {
        cancel.cancel();
        return true;
      }),
    });
    expect(await runCanvasExport(deps)).toBe("cancelled");
    expect(deps.exportCanvas).not.toHaveBeenCalled();
    expect(deps.download).not.toHaveBeenCalled();
  });
});
