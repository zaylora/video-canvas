import { afterEach, beforeEach, describe, expect, spyOn, test } from "bun:test";

import { downloadAgentRunTrace, listAgentRuns } from "@/api/admin/agent-runs";
import service from "@/utils/requests/service";
import { ApiError } from "@/utils/requests/request";

describe("listAgentRuns", () => {
  let get: ReturnType<typeof spyOn>;
  beforeEach(() => {
    get = spyOn(service, "get");
  });
  // service 是全仓共用的单例，必须还原
  afterEach(() => {
    get.mockRestore();
  });

  test("请求 /admin/agent/runs，空参数不发", async () => {
    get.mockResolvedValue({ list: [], total: 0, page: 1, page_size: 20 });
    await listAgentRuns({ user: "", canvas_id: undefined, status: "failed", page: 2 });
    expect(get).toHaveBeenCalledTimes(1);
    expect(get.mock.calls[0]?.[0]).toBe("/admin/agent/runs");
    expect(get.mock.calls[0]?.[1]).toEqual({ status: "failed", page: 2 });
  });

  test("后端返回 null 或缺 list 时补成空页", async () => {
    get.mockResolvedValue(null);
    expect(await listAgentRuns({ page: 3, page_size: 50 })).toEqual({
      list: [],
      total: 0,
      page: 3,
      page_size: 50,
    });
    get.mockResolvedValue({ total: 0 });
    expect((await listAgentRuns()).list).toEqual([]);
  });
});

describe("downloadAgentRunTrace", () => {
  let request: ReturnType<typeof spyOn>;
  beforeEach(() => {
    request = spyOn(service, "request");
  });
  afterEach(() => {
    request.mockRestore();
  });

  test("GET /admin/agent/runs/:rid/trace，按 blob 取，失败自己提示、不弹全局 toast", async () => {
    const zip = new Blob([new Uint8Array([0x50, 0x4b, 3, 4])]);
    request.mockResolvedValue(zip);
    expect(await downloadAgentRunTrace("9137")).toBe(zip);
    const config = request.mock.calls[0]?.[0] as Record<string, unknown>;
    expect(config.url).toBe("/admin/agent/runs/9137/trace");
    expect(config.responseType).toBe("blob");
    expect(config.silent).toBe(true);
  });

  test("rid 会被编码进路径", async () => {
    request.mockResolvedValue(new Blob([new Uint8Array([0x50, 0x4b])]));
    await downloadAgentRunTrace("a/b");
    const config = request.mock.calls[0]?.[0] as { url: string };
    expect(config.url).toBe("/admin/agent/runs/a%2Fb/trace");
  });

  test("业务错误（4xx 也当响应收下）：信封被识别成 ApiError，带后端文案", async () => {
    request.mockResolvedValue(
      new Blob([JSON.stringify({ code: 60013, msg: "轨迹记录已关闭" })], {
        type: "application/json",
      }),
    );
    const error = await downloadAgentRunTrace("1").catch((e: unknown) => e);
    expect(error).toBeInstanceOf(ApiError);
    expect((error as ApiError).message).toBe("轨迹记录已关闭");
  });

  test("请求选项允许 4xx 响应进入成功分支（好读到信封），401 仍走拦截器清登录态", async () => {
    request.mockResolvedValue(new Blob([new Uint8Array([0x50, 0x4b])]));
    await downloadAgentRunTrace("1");
    const config = request.mock.calls[0]?.[0] as { validateStatus: (s: number) => boolean };
    expect(config.validateStatus(200)).toBe(true);
    expect(config.validateStatus(404)).toBe(true);
    expect(config.validateStatus(410)).toBe(true);
    expect(config.validateStatus(401)).toBe(false);
    expect(config.validateStatus(500)).toBe(false);
  });
});
