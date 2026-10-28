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

  test("导出和导入放宽请求超时到 5 分钟（默认 2 分钟不够复制大量素材）", async () => {
    get.mockResolvedValue({ document: {} });
    post.mockResolvedValue({
      canvas: { id: "7", title: "t", revision: 1 },
      report: { assets: { total: 0, copied: 0 }, edges_dropped: 0 },
    });

    await exportCanvas("42");
    await importCanvas({});

    expect(get.mock.calls[0]?.[2]?.timeout).toBe(5 * 60 * 1000);
    expect(post.mock.calls[0]?.[2]?.timeout).toBe(5 * 60 * 1000);
  });

  test("exportCanvas 缺少 report 时 skippedAssets 为 0", async () => {
    get.mockResolvedValue({ document: {} });
    expect((await exportCanvas("42")).skippedAssets).toBe(0);
  });

  test("importCanvas 把文档包进 { document } 提交到导入路径", async () => {
    const document = { format: "video-canvas/canvas" };
    post.mockResolvedValue({
      canvas: { id: "7", title: "t", revision: 1 },
      report: { assets: { total: 0, copied: 0 }, edges_dropped: 0 },
    });

    await importCanvas(document);

    expect(post.mock.calls[0]?.[0]).toBe("/canvas/import");
    expect(post.mock.calls[0]?.[1]).toEqual({ document });
  });

  test("importCanvas 把报告 snake_case 映射成 camelCase（报告里没有角色卡：角色数据随节点导入）", async () => {
    post.mockResolvedValue({
      canvas: { id: "7", title: "雨夜", revision: 3 },
      report: {
        assets: {
          total: 5,
          copied: 4,
          missing: [{ node_id: "n1", file_name: "a.png" }],
        },
        edges_dropped: 1,
      },
    });

    const result = await importCanvas({});

    expect(result).toEqual({
      canvas: { id: "7", title: "雨夜", revision: 3 },
      report: {
        assets: { total: 5, copied: 4, missing: [{ nodeId: "n1", fileName: "a.png" }] },
        edgesDropped: 1,
      },
    });
  });

  test("importCanvas 在后端省略数组或为 null 时兜底为空数组", async () => {
    post.mockResolvedValue({
      canvas: { id: "7", title: "t", revision: 1 },
      report: {
        assets: { total: 1, copied: 1, missing: null },
        edges_dropped: 0,
      },
    });

    const { report } = await importCanvas({});

    expect(report.assets.missing).toEqual([]);
    // 角色卡内嵌在画布里，导入报告里不再有角色卡一项
    expect(report).not.toHaveProperty("characters");
  });
  test("exportCanvas / importCanvas 把取消信号透传给请求层，不传时不带 signal", async () => {
    get.mockResolvedValue({ document: {} });
    post.mockResolvedValue({
      canvas: { id: "7", title: "t", revision: 1 },
      report: { assets: { total: 0, copied: 0 }, edges_dropped: 0 },
    });
    const controller = new AbortController();

    await exportCanvas("42", controller.signal);
    await importCanvas({}, controller.signal);
    expect(get.mock.calls[0]?.[2]?.signal).toBe(controller.signal);
    expect(post.mock.calls[0]?.[2]?.signal).toBe(controller.signal);

    await exportCanvas("42");
    await importCanvas({});
    expect(get.mock.calls[1]?.[2]?.signal).toBeUndefined();
    expect(post.mock.calls[1]?.[2]?.signal).toBeUndefined();
  });
});
