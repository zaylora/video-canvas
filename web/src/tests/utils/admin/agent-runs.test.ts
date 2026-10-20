import { describe, expect, test } from "bun:test";

import type { AgentRunItem } from "@/api/admin/agent-runs/type";
import {
  AGENT_RUN_EMPTY_TEXT,
  AGENT_RUN_FILTER_KEYS,
  agentModeLabel,
  agentRunFiltersOf,
  agentRunRequest,
  agentRunStatusView,
  creditsText,
  durationText,
  ensureTraceZip,
  hasAgentRunFilters,
  isRunUnfinished,
  traceDownloadFailure,
  traceDownloadSuccess,
  traceDownloadView,
  traceFileName,
} from "@/utils/admin/agent-runs";
import { ApiError } from "@/utils/requests/request";

const NOW = new Date("2026-10-20T08:00:00.000Z");

const run = (patch: Partial<AgentRunItem> = {}): AgentRunItem => ({
  id: "9137",
  created_at: "2026-10-20T06:31:00Z",
  ended_at: "2026-10-20T06:31:41Z",
  user_id: "u1",
  user_email: "alice@studio.cn",
  canvas_id: "c1",
  canvas_title: "雨夜追凶 第3集",
  mode: "storyboard",
  status: "succeeded",
  steps: 18,
  tool_errors: 3,
  spent_credits: 0,
  duration_ms: 41000,
  prompt_version: 9,
  source: "user",
  harness_case: "",
  trace: "available",
  trace_complete: true,
  ...patch,
});

describe("筛选 → 请求参数", () => {
  test("什么都没选：默认最近 7 天，页码 1、每页 20，空参数一个都不带", () => {
    const params = agentRunRequest({ page: 1, page_size: 20 }, NOW);
    expect(params).toEqual({
      from: "2026-10-13T08:00:00.000Z",
      to: "2026-10-20T08:00:00.000Z",
      page: 1,
      page_size: 20,
    });
  });

  test("搜索词是用户（去首尾空格），其余筛选逐项换成契约里的参数名", () => {
    const params = agentRunRequest(
      {
        q: "  alice@studio.cn ",
        canvas_id: " c1 ",
        run_id: "9137",
        status: "failed",
        source: "harness",
        range: "24h",
        page: 3,
        page_size: 50,
      },
      NOW,
    );
    expect(params).toEqual({
      user: "alice@studio.cn",
      canvas_id: "c1",
      run_id: "9137",
      status: "failed",
      source: "harness",
      from: "2026-10-19T08:00:00.000Z",
      to: "2026-10-20T08:00:00.000Z",
      page: 3,
      page_size: 50,
    });
  });

  test("URL 里的非法状态、来源、范围回退成不限和 7 天，不会原样发给后端", () => {
    const params = agentRunRequest(
      { status: "weird", source: "bot", range: "forever", page: 1, page_size: 20 },
      NOW,
    );
    expect(params.status).toBeUndefined();
    expect(params.source).toBeUndefined();
    expect(params.from).toBe("2026-10-13T08:00:00.000Z");
  });

  test("页码和每页条数缺省时给 1 / 20", () => {
    const params = agentRunRequest({}, NOW);
    expect(params.page).toBe(1);
    expect(params.page_size).toBe(20);
  });

  test("筛选参数名白名单包含 canvas_id、run_id、status、source、range", () => {
    expect([...AGENT_RUN_FILTER_KEYS].sort()).toEqual(
      ["canvas_id", "run_id", "source", "range", "status"].sort(),
    );
  });
});

describe("筛选状态", () => {
  test("agentRunFiltersOf：缺省值", () => {
    expect(agentRunFiltersOf({ q: "", filters: {} })).toEqual({
      user: "",
      canvasId: "",
      runId: "",
      status: "",
      source: "",
      range: "7d",
    });
  });

  test("hasAgentRunFilters：时间范围不算筛选，其余任一项有值都算", () => {
    const base = agentRunFiltersOf({ q: "", filters: { range: "30d" } });
    expect(hasAgentRunFilters(base)).toBe(false);
    expect(hasAgentRunFilters({ ...base, user: " a " })).toBe(true);
    expect(hasAgentRunFilters({ ...base, user: "   " })).toBe(false);
    expect(hasAgentRunFilters({ ...base, canvasId: "c1" })).toBe(true);
    expect(hasAgentRunFilters({ ...base, runId: "1" })).toBe(true);
    expect(hasAgentRunFilters({ ...base, status: "failed" })).toBe(true);
    expect(hasAgentRunFilters({ ...base, source: "user" })).toBe(true);
  });

  test("空结果文案与设计文档第 7 节一致", () => {
    expect(AGENT_RUN_EMPTY_TEXT).toBe(
      "没有符合条件的运行，换个筛选条件，或者看看是不是已超过 14 天",
    );
  });
});

describe("状态与展示", () => {
  test("运行状态文案：以后端 Run 状态常量为准，未知状态原样显示", () => {
    expect(agentRunStatusView("succeeded")).toEqual({ label: "完成", tone: "success" });
    expect(agentRunStatusView("running").label).toBe("运行中");
    expect(agentRunStatusView("failed").tone).toBe("danger");
    expect(agentRunStatusView("interrupted").label).toBe("中断");
    expect(agentRunStatusView("something_new")).toEqual({
      label: "something_new",
      tone: "neutral",
    });
  });

  test("没结束的状态：排队、运行、等批准、等回答", () => {
    for (const s of ["queued", "running", "waiting_approval", "waiting_input"]) {
      expect(isRunUnfinished(s)).toBe(true);
    }
    for (const s of ["succeeded", "failed", "canceled", "interrupted", "timeout", "expired"]) {
      expect(isRunUnfinished(s)).toBe(false);
    }
  });

  test("模式显示中文名，不认识的原样显示", () => {
    expect(agentModeLabel("storyboard")).toBe("分镜搭建");
    expect(agentModeLabel("all")).toBe("全能创作");
    expect(agentModeLabel("future")).toBe("future");
  });

  test("耗时：null 是 —，其余按秒/分秒", () => {
    expect(durationText(null)).toBe("—");
    expect(durationText(41000)).toBe("41s");
    expect(durationText(123000)).toBe("2m3s");
  });

  test("积分：0 也显示 0，整数不带小数", () => {
    expect(creditsText(0)).toBe("0");
    expect(creditsText(24)).toBe("24");
  });
});

describe("下载状态判定", () => {
  test("available 且已结束：可下载，无说明", () => {
    expect(traceDownloadView(run())).toEqual({
      canDownload: true,
      reason: null,
      unfinished: false,
      incomplete: false,
    });
  });

  test("trace=expired：置灰并写明「已过期（保留 14 天）」", () => {
    const view = traceDownloadView(run({ trace: "expired" }));
    expect(view.canDownload).toBe(false);
    expect(view.reason).toBe("已过期（保留 14 天）");
  });

  test("trace=disabled：置灰并写明「当时轨迹记录已关闭」", () => {
    const view = traceDownloadView(run({ trace: "disabled" }));
    expect(view.canDownload).toBe(false);
    expect(view.reason).toBe("当时轨迹记录已关闭");
  });

  test("运行中：仍可下载，标「未完成」", () => {
    const view = traceDownloadView(run({ status: "running", ended_at: null, duration_ms: null }));
    expect(view.canDownload).toBe(true);
    expect(view.unfinished).toBe(true);
    expect(view.reason).toBeNull();
  });

  test("已过期的运行中行：不能下载，也不再标未完成", () => {
    const view = traceDownloadView(run({ status: "running", trace: "expired" }));
    expect(view.canDownload).toBe(false);
    expect(view.unfinished).toBe(false);
  });

  test("trace_complete=false：可下载，打「不完整」标签", () => {
    const view = traceDownloadView(run({ trace_complete: false }));
    expect(view.canDownload).toBe(true);
    expect(view.incomplete).toBe(true);
  });

  test("过期或关闭的行不打「不完整」：它们连轨迹都没有，标签没有意义", () => {
    expect(traceDownloadView(run({ trace: "expired", trace_complete: false })).incomplete).toBe(
      false,
    );
  });
});

describe("下载成功与失败的提示", () => {
  test("文件名 agent-trace-<rid>.zip", () => {
    expect(traceFileName("9137")).toBe("agent-trace-9137.zip");
  });

  test("成功：已下载，此次导出已记入操作审计", () => {
    expect(traceDownloadSuccess(run())).toEqual({
      tone: "success",
      message: "已下载，此次导出已记入操作审计",
    });
  });

  test("运行未结束时的成功提示多一句轨迹不完整", () => {
    const hint = traceDownloadSuccess(run({ status: "running" }));
    expect(hint.tone).toBe("warning");
    expect(hint.message).toBe("已下载（运行未结束，轨迹不完整），此次导出已记入操作审计");
  });

  test("网络类失败：提示可以重试，服务端已记审计，不必刷新列表", () => {
    for (const code of ["NETWORK_ERROR", "TIMEOUT", "HTTP_502"] as const) {
      const failure = traceDownloadFailure(new ApiError("x", code, 0));
      expect(failure.message).toBe("下载失败，可以重试");
      expect(failure.refresh).toBe(false);
    }
    expect(traceDownloadFailure(new Error("boom")).message).toBe("下载失败，可以重试");
  });

  test("后端业务错误：直接说明原因（如轨迹已过期），并要求刷新列表", () => {
    const failure = traceDownloadFailure(new ApiError("轨迹已过期", 60012, 410));
    expect(failure.message).toBe("下载失败：轨迹已过期");
    expect(failure.refresh).toBe(true);
  });
});

describe("ensureTraceZip：区分 zip 与业务错误信封", () => {
  const zipBlob = () => new Blob([new Uint8Array([0x50, 0x4b, 0x03, 0x04, 1, 2, 3])]);

  test("以 PK 开头的是 zip，原样返回", async () => {
    const blob = zipBlob();
    expect(await ensureTraceZip(blob)).toBe(blob);
  });

  test("内容类型标成 json 也看内容：真 zip 不会被误判", async () => {
    const blob = new Blob([new Uint8Array([0x50, 0x4b, 0x03, 0x04])], { type: "application/json" });
    expect(await ensureTraceZip(blob)).toBe(blob);
  });

  test("{code,msg} 信封：抛出带后端文案和业务码的 ApiError，而不是把 JSON 当 zip", async () => {
    const blob = new Blob([JSON.stringify({ code: 60012, msg: "轨迹已过期" })], {
      type: "application/json",
    });
    const error = await ensureTraceZip(blob).catch((e: unknown) => e);
    expect(error).toBeInstanceOf(ApiError);
    expect((error as ApiError).message).toBe("轨迹已过期");
    expect((error as ApiError).code).toBe(60012);
  });

  test("信封 code 为 0 也不是 zip：按异常响应处理", async () => {
    const blob = new Blob([JSON.stringify({ code: 0, msg: "ok", data: {} })]);
    await expect(ensureTraceZip(blob)).rejects.toThrow();
  });

  test("既不是 zip 也不是信封（空内容、乱码）：抛出，不当成文件保存", async () => {
    await expect(ensureTraceZip(new Blob([]))).rejects.toThrow();
    await expect(ensureTraceZip(new Blob(["<html>502</html>"]))).rejects.toThrow();
  });
});
