import { afterEach, beforeEach, describe, expect, spyOn, test, type Mock } from "bun:test";

import { exportCanvas, importCanvas } from "@/api/canvas";
import service from "@/utils/requests/service";

describe("api/canvas：导出与导入", () => {
  let get: Mock<typeof service.get>;
  let post: Mock<typeof service.post>;
  beforeEach(() => {
    get = spyOn(service, "get");
    post = spyOn(service, "post");
  });
  // service 是全仓共用的单例，必须还原
  afterEach(() => {
    get.mockRestore();
    post.mockRestore();
  });

  test("exportCanvas 请求导出路径，skipped_assets 转成 skippedAssets", async () => {
    const document = { format: "video-canvas/canvas", version: 1 };
    get.mockResolvedValue({ document, report: { skipped_assets: 2 } });

    const result = await exportCanvas("42");

    expect(get.mock.calls[0]?.[0]).toBe("/canvas/42/export");
    expect(result.document).toBe(document as never);
    expect(result.skippedAssets).toBe(2);
  });

  test("exportCanvas 缺少 report 时 skippedAssets 为 0", async () => {
    get.mockResolvedValue({ document: {} });
    expect((await exportCanvas("42")).skippedAssets).toBe(0);
  });

  test("importCanvas 把文档包进 { document } 提交到导入路径", async () => {
    const document = { format: "video-canvas/canvas" };
    post.mockResolvedValue({
      canvas: { id: "7", title: "t", revision: 1 },
      report: { assets: { total: 0, copied: 0 }, characters: { created: 0 }, edges_dropped: 0 },
    });

    await importCanvas(document);

    expect(post.mock.calls[0]?.[0]).toBe("/canvas/import");
    expect(post.mock.calls[0]?.[1]).toEqual({ document });
  });

  test("importCanvas 把报告 snake_case 映射成 camelCase", async () => {
    post.mockResolvedValue({
      canvas: { id: "7", title: "雨夜", revision: 3 },
      report: {
        assets: {
          total: 5,
          copied: 4,
          missing: [{ node_id: "n1", file_name: "a.png" }],
        },
        characters: {
          created: 2,
          renamed: [{ from: "林夏", to: "林夏 2" }],
          invalid: [{ node_id: "n2", name: "路人" }],
        },
        edges_dropped: 1,
      },
    });

    const result = await importCanvas({});

    expect(result).toEqual({
      canvas: { id: "7", title: "雨夜", revision: 3 },
      report: {
        assets: { total: 5, copied: 4, missing: [{ nodeId: "n1", fileName: "a.png" }] },
        characters: {
          created: 2,
          renamed: [{ from: "林夏", to: "林夏 2" }],
          invalid: [{ nodeId: "n2", name: "路人" }],
        },
        edgesDropped: 1,
      },
    });
  });

  test("importCanvas 在后端省略数组或为 null 时兜底为空数组", async () => {
    post.mockResolvedValue({
      canvas: { id: "7", title: "t", revision: 1 },
      report: {
        assets: { total: 1, copied: 1, missing: null },
        characters: { created: 0 },
        edges_dropped: 0,
      },
    });

    const { report } = await importCanvas({});

    expect(report.assets.missing).toEqual([]);
    expect(report.characters.renamed).toEqual([]);
    expect(report.characters.invalid).toEqual([]);
  });
});
