import { describe, expect, mock, test } from "bun:test";

import type { CanvasArchiveTicketDto } from "@/api/canvas/type";
import {
  archiveGroupsText,
  archiveSizeText,
  runCanvasArchive,
  type CanvasArchiveDeps,
} from "@/utils/canvas/archive-flow";

const ticket = (overrides: Partial<CanvasArchiveTicketDto> = {}): CanvasArchiveTicketDto => ({
  url: "/canvas/abc/assets/archive?token=t",
  file_name: "旧收音机_素材_20261122.zip",
  count: 23,
  groups: { 剧本: 1, 分镜表: 2, 角色: 1, 图片: 12, 视频: 6, 音频: 0, 成片: 1 },
  total_bytes: 3 * (1 << 20),
  skipped: 0,
  expires_at: 1_800_000_000,
  ...overrides,
});

function setup(overrides: Partial<CanvasArchiveDeps> = {}) {
  const deps = {
    canvasId: "abc",
    request: mock(async () => ticket()),
    open: mock((_url: string) => {}),
    notify: { success: mock(), warning: mock() },
    ...overrides,
  };
  return deps as typeof deps & CanvasArchiveDeps;
}

describe("runCanvasArchive：按节点归类打包下载", () => {
  test("拿到票据就把地址交给浏览器下载，并提示文件数与体积", async () => {
    const deps = setup();
    expect(await runCanvasArchive(deps)).toBe("started");

    expect(deps.open).toHaveBeenCalledWith("/canvas/abc/assets/archive?token=t");
    expect(deps.notify.success).toHaveBeenCalledTimes(1);
    const [message, description] = deps.notify.success.mock.calls[0]!;
    expect(String(message)).toContain("23 个文件");
    expect(String(description)).toContain("剧本 1");
    expect(String(description)).toContain("成片 1");
    expect(String(description)).toContain("3.0 MB");
    expect(String(description)).toContain("旧收音机_素材_20261122.zip");
    expect(deps.notify.warning).not.toHaveBeenCalled();
  });

  test("请求失败：不触发下载、不夸口成功（错误提示由请求层负责）", async () => {
    const deps = setup({
      request: mock(async () => {
        throw new Error("boom");
      }),
    });
    expect(await runCanvasArchive(deps)).toBe("failed");
    expect(deps.open).not.toHaveBeenCalled();
    expect(deps.notify.success).not.toHaveBeenCalled();
  });

  test("有素材文件取不到：额外提示一次别让用户以为包是全的", async () => {
    const deps = setup({ request: mock(async () => ticket({ skipped: 2 })) });
    await runCanvasArchive(deps);
    expect(deps.notify.warning).toHaveBeenCalledTimes(1);
    expect(String(deps.notify.warning.mock.calls[0]?.[0])).toContain("2 项");
  });
});

describe("archiveSizeText / archiveGroupsText", () => {
  test("体积按 B / KB / MB / GB 换档", () => {
    expect(archiveSizeText(512)).toBe("512 B");
    expect(archiveSizeText(2048)).toBe("2 KB");
    expect(archiveSizeText(5 * (1 << 20))).toBe("5.0 MB");
    expect(archiveSizeText(2.5 * (1 << 30))).toBe("2.50 GB");
  });

  test("分组文案只列有内容的组，且按生产链路排序", () => {
    expect(archiveGroupsText({ 成片: 1, 视频: 3, 剧本: 1, 音频: 0, 图片: 2 })).toBe(
      "剧本 1 · 图片 2 · 视频 3 · 成片 1",
    );
    expect(archiveGroupsText({})).toBe("");
  });
});
