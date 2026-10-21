import { describe, expect, test } from "bun:test";

import type {
  AgentRunDetail,
  AgentRunDigest,
  AgentRunStep,
  AgentRunStepTool,
} from "@/api/admin/agent-run/type.d";
import {
  contextLines,
  copyRunMarkdown,
  downloadFileName,
  initialRunDrawer,
  isRunNotFound,
  mutationRows,
  requestText,
  runHeadInfo,
  runDetailJson,
  runDrawerReducer,
  stepOffsetText,
  stepView,
  toolHeadline,
  toolNotes,
  userSignals,
} from "@/utils/admin/agent-run-detail";

const RID = "0123456789abcdef0123456789abcdef";
const START = Date.parse("2026-10-22T09:00:00Z");

const step = (patch: Partial<AgentRunStep> = {}): AgentRunStep => ({
  n: 1,
  kind: "assistant",
  label: "模型回复",
  text: "好的",
  ...patch,
});
const tool = (patch: Partial<AgentRunStepTool> = {}): AgentRunStepTool => ({
  name: "canvas_patch",
  duration_ms: 820,
  is_error: false,
  recorded: true,
  ...patch,
});
const digest = (patch: Partial<AgentRunDigest> = {}): AgentRunDigest =>
  ({
    run_id: RID,
    user_id: 1,
    status: "succeeded",
    request_head: "请求首句",
    steer_count: 0,
    approvals_rejected: 0,
    tool_errors: 0,
    undone: false,
    feedback: 0,
    created_at: "2026-10-22T09:00:00Z",
    ...patch,
  }) as AgentRunDigest;
const detail = (patch: Partial<AgentRunDetail> = {}): AgentRunDetail => ({
  digest: digest(),
  run: null,
  timeline: [],
  events: [],
  tool_calls: [],
  model_calls: [],
  mutations: [],
  approvals: [],
  feedback: null,
  context: [],
  runtime_tail: "",
  ...patch,
});

describe("请求原文", () => {
  test("取时间线里第一个 user 步骤的正文，插话不算", () => {
    const d = detail({
      timeline: [
        step({ n: 1, kind: "steer", text: "插话" }),
        step({ n: 2, kind: "user", text: "原文一" }),
        step({ n: 3, kind: "user", text: "原文二" }),
      ],
    });
    expect(requestText(d)).toBe("原文一");
  });
  test("时间线里没有 user 步骤，退回摘要的请求首句；都没有是空串", () => {
    expect(requestText(detail({ timeline: [step()] }))).toBe("请求首句");
    expect(requestText(detail({ digest: null }))).toBe("");
  });
});

describe("头部信息", () => {
  test("有摘要：直接取摘要的状态、时间、耗时、步数、积分、Token", () => {
    const d = detail({
      digest: digest({
        status: "failed",
        ended_at: "2026-10-22T09:01:00Z",
        duration_ms: 60_000,
        steps: 7,
        spent_credits: 12,
        input_tokens: 1000,
        output_tokens: 200,
      }),
    });
    expect(runHeadInfo(d)).toEqual({
      status: "failed",
      createdAt: "2026-10-22T09:00:00Z",
      endedAt: "2026-10-22T09:01:00Z",
      durationMs: 60_000,
      steps: 7,
      credits: 12,
      inputTokens: 1000,
      outputTokens: 200,
    });
  });
  test("摘要还没生成：退回运行本身，耗时用结束减创建，Token 把模型调用加起来；没结束耗时为 null", () => {
    const run = {
      id: "r",
      session_id: "s",
      canvas_id: "c",
      status: "running",
      mode: "auto",
      budget_credits: 0,
      spent_credits: 3,
      steps: 2,
      max_steps: 40,
      error_code: "",
      error_message: "",
      created_at: "2026-10-22T09:00:00Z",
      ended_at: null,
    };
    const mc = (i: number, o: number) =>
      ({ input_tokens: i, output_tokens: o }) as AgentRunDetail["model_calls"][number];
    const open = runHeadInfo(detail({ digest: null, run, model_calls: [mc(10, 1), mc(20, 2)] }));
    expect(open).toMatchObject({
      status: "running",
      durationMs: null,
      steps: 2,
      credits: 3,
      inputTokens: 30,
      outputTokens: 3,
    });
    const ended = runHeadInfo(
      detail({ digest: null, run: { ...run, ended_at: "2026-10-22T09:00:05Z" } }),
    );
    expect(ended.durationMs).toBe(5000);
  });
  test("摘要和运行都没有：返回 null", () => {
    expect(runHeadInfo(detail({ digest: null, run: null }))).toBeNull();
  });
});

describe("时间线步骤显示", () => {
  test("编号、类型名原样来自后端，时间显示为相对运行开始的偏移", () => {
    const v = stepView(
      step({ n: 12, label: "用户", kind: "user", at: "2026-10-22T09:00:03.500Z" }),
      START,
    );
    expect(v.id).toBe("#12");
    expect(v.label).toBe("用户");
    expect(v.offset).toBe("+3.5s");
    expect(v.indent).toBe(false);
  });
  test("偏移：不足 1 分钟一位小数的秒，之后是 分+秒", () => {
    expect(stepOffsetText("2026-10-22T09:00:00Z", START)).toBe("+0.0s");
    expect(stepOffsetText("2026-10-22T09:01:05Z", START)).toBe("+1m05s");
    expect(stepOffsetText("2026-10-22T09:12:00Z", START)).toBe("+12m00s");
  });
  test("没有 at 显示空串；起点未知或 at 早于起点，退回时分秒", () => {
    expect(stepOffsetText(undefined, START)).toBe("");
    expect(stepOffsetText("2026-10-22T09:00:00Z", null)).toMatch(/^\d{2}:\d{2}:\d{2}$/);
    expect(stepOffsetText("2026-10-22T08:00:00Z", START)).toMatch(/^\d{2}:\d{2}:\d{2}$/);
  });
  test("nested 缩进", () => {
    expect(stepView(step({ kind: "tool", nested: true, tool: tool() }), START).indent).toBe(true);
  });
  test("色调：撤销是警告；反馈 👎 红、👍 绿；状态按运行状态着色；其余中性", () => {
    const tone = (s: Partial<AgentRunStep>) => stepView(step(s), START).tone;
    expect(tone({ kind: "undo", text: "" })).toBe("warning");
    expect(tone({ kind: "feedback", text: "👎 没理解我的需求" })).toBe("danger");
    expect(tone({ kind: "feedback", text: "👍" })).toBe("success");
    expect(tone({ kind: "feedback", text: "", label: "用户取消了反馈" })).toBe("neutral");
    expect(tone({ kind: "status", text: "failed" })).toBe("danger");
    expect(tone({ kind: "status", text: "step_limit" })).toBe("warning");
    expect(tone({ kind: "status", text: "succeeded" })).toBe("success");
    expect(tone({ kind: "status", text: "running" })).toBe("info");
    expect(tone({ kind: "status", text: "canceled" })).toBe("neutral");
    expect(tone({ kind: "plan" })).toBe("neutral");
    expect(tone({ kind: "tool", tool: tool({ is_error: true }) })).toBe("danger");
    expect(tone({ kind: "tool", tool: tool() })).toBe("neutral");
  });
  test("状态步骤的文字译成中文状态名，其他步骤保持原文", () => {
    expect(stepView(step({ kind: "status", text: "failed" }), START).text).toBe("失败");
    expect(stepView(step({ kind: "plan", text: "计划 3 项" }), START).text).toBe("计划 3 项");
  });
});

describe("工具步骤", () => {
  test("标题：工具名 · 耗时 · 成败", () => {
    expect(toolHeadline(tool())).toEqual({
      name: "canvas_patch",
      duration: "820ms",
      failed: false,
      outcome: "成功",
    });
    expect(toolHeadline(tool({ duration_ms: 2500, is_error: true }))).toEqual({
      name: "canvas_patch",
      duration: "2.50s",
      failed: true,
      outcome: "报错",
    });
  });
  test("没有耗时记录：duration 为 null", () => {
    expect(toolHeadline(tool({ duration_ms: undefined })).duration).toBeNull();
    expect(toolHeadline(tool({ duration_ms: 0 })).duration).toBeNull();
  });
  test("提示：已截断", () => {
    expect(toolNotes(tool({ truncated: true })).map((n) => n.text)).toEqual([
      "已截断，完整内容请下载 JSON",
    ]);
  });
  test("提示：已过保留期，带清理日期", () => {
    const [note] = toolNotes(tool({ purged_at: "2026-10-20T03:00:00Z" }));
    expect(note!.key).toBe("purged");
    expect(note!.text).toContain("已过保留期");
    expect(note!.text).toContain("参数和结果已清理，摘要和耗时仍在");
    expect(note!.text).toMatch(/\d{4}-\d{2}-\d{2}/);
  });
  test("提示：未记录参数", () => {
    expect(toolNotes(tool({ recorded: false })).map((n) => n.text)).toEqual(["未记录参数"]);
  });
  test("过保留期的调用不再提示截断；什么都没有就没有提示", () => {
    expect(
      toolNotes(tool({ truncated: true, purged_at: "2026-10-20T03:00:00Z" })).map((n) => n.key),
    ).toEqual(["purged"]);
    expect(toolNotes(tool())).toEqual([]);
  });
});

describe("用户信号", () => {
  test("没有任何信号：hasAny 为假", () => {
    expect(userSignals(detail()).hasAny).toBe(false);
  });
  test("点踩：原因译成中文，带说明原文", () => {
    const s = userSignals(
      detail({ feedback: { rating: -1, reasons: ["misread", "slow"], comment: "太慢了" } }),
    );
    expect(s.hasAny).toBe(true);
    expect(s.feedback).toEqual({
      rating: -1,
      reasons: ["没理解我的需求", "步骤太多或太慢"],
      comment: "太慢了",
    });
  });
  test("详情里没有 feedback 但摘要有评价：只显示评价，没有原因", () => {
    const s = userSignals(detail({ digest: digest({ feedback: 1 }) }));
    expect(s.feedback).toEqual({ rating: 1, reasons: [], comment: "" });
  });
  test("撤销：摘要标了，或任一改动有撤销时间", () => {
    expect(userSignals(detail({ digest: digest({ undone: true }) })).undone).toBe(true);
    expect(
      userSignals(
        detail({
          digest: null,
          mutations: [
            {
              seq: 1,
              tool_call_id: "t",
              kind: "k",
              changes: [],
              revision_before: 1,
              revision_after: 2,
              undone_at: "2026-10-22T09:05:00Z",
            },
          ],
        }),
      ).undone,
    ).toBe(true);
  });
  test("插话、被拒审批、工具报错取自摘要；任一大于 0 都算有信号", () => {
    const s = userSignals(
      detail({ digest: digest({ steer_count: 2, approvals_rejected: 1, tool_errors: 3 }) }),
    );
    expect([s.steerCount, s.approvalsRejected, s.toolErrors, s.hasAny]).toEqual([2, 1, 3, true]);
  });
  test("没有摘要时，插话和工具报错从时间线数出来", () => {
    const s = userSignals(
      detail({
        digest: null,
        timeline: [
          step({ kind: "steer" }),
          step({ kind: "tool", tool: tool({ is_error: true }) }),
          step({ kind: "tool", tool: tool() }),
        ],
      }),
    );
    expect([s.steerCount, s.toolErrors]).toEqual([1, 1]);
  });
});

describe("画布改动", () => {
  test("改动条数是 changes 数组长度，工具调用对上工具名，撤销看 undone_at", () => {
    const rows = mutationRows(
      [
        {
          seq: 1,
          tool_call_id: "call_1",
          kind: "update_nodes",
          changes: [1, 2, 3],
          revision_before: 1,
          revision_after: 2,
          undone_at: null,
        },
        {
          seq: 2,
          tool_call_id: "call_x",
          kind: "add_nodes",
          changes: null,
          revision_before: 2,
          revision_after: 3,
          undone_at: "2026-10-22T09:05:00Z",
        },
      ],
      [
        {
          id: "call_1",
          parent_id: "",
          name: "canvas_patch",
          args: null,
          result: null,
          result_truncated: false,
          is_error: false,
          summary: "",
          started_at: "2026-10-22T09:00:00Z",
          ended_at: null,
          duration_ms: 1,
          purged: false,
        },
      ],
    );
    expect(rows).toEqual([
      {
        seq: 1,
        kind: "update_nodes",
        count: 3,
        toolName: "canvas_patch",
        toolCallId: "call_1",
        undone: false,
      },
      { seq: 2, kind: "add_nodes", count: 0, toolName: "", toolCallId: "call_x", undone: true },
    ]);
  });
});

describe("上下文", () => {
  const ev = (type: string, data: Record<string, unknown> | null) => ({
    session_id: "s",
    canvas_id: "c",
    run_id: null,
    seq: 1,
    type,
    data,
    created_at: "2026-10-22T08:00:00Z",
  });
  test("用户消息和 Agent 回复各一行，取 data.text，其他事件和没有文字的忽略", () => {
    expect(
      contextLines([
        ev("message.user", { text: "上一轮的问题" }),
        ev("message.done", { text: "上一轮的回答" }),
        ev("tool.start", { text: "不显示" }),
        ev("message.done", null),
        ev("message.user", { text: 5 }),
      ]),
    ).toEqual([
      { who: "用户", text: "上一轮的问题" },
      { who: "Agent", text: "上一轮的回答" },
    ]);
  });
});

describe("下载 JSON 与复制 Markdown", () => {
  test("文件名取 rid 前 8 位", () => {
    expect(downloadFileName(RID)).toBe("agent-run-01234567.json");
  });
  test("内容是已加载详情的缩进 JSON，能原样解析回来，不再请求接口", () => {
    const d = detail({ timeline: [step({ text: '含中文，"引号"' })] });
    const text = runDetailJson(d);
    expect(text).toContain("\n  ");
    expect(JSON.parse(text)).toEqual(d);
  });
  test("复制：导出成功且写入剪贴板成功，返回 copied，写入的就是导出的 Markdown", async () => {
    const written: string[] = [];
    const result = await copyRunMarkdown(RID, {
      exportMarkdown: async (rid) => `# ${rid}`,
      copy: async (text) => (written.push(text), true),
    });
    expect(result).toBe("copied");
    expect(written).toEqual([`# ${RID}`]);
  });
  test("剪贴板写入失败：返回 copy-failed，由页面提示", async () => {
    expect(
      await copyRunMarkdown(RID, { exportMarkdown: async () => "md", copy: async () => false }),
    ).toBe("copy-failed");
  });
  test("导出接口失败：错误原样抛出，不碰剪贴板", async () => {
    let touched = false;
    const err = new Error("boom");
    await expect(
      copyRunMarkdown(RID, {
        exportMarkdown: async () => {
          throw err;
        },
        copy: async () => ((touched = true), true),
      }),
    ).rejects.toBe(err);
    expect(touched).toBe(false);
  });
});

describe("运行不存在", () => {
  test("HTTP 404 或业务码 60012", () => {
    expect(isRunNotFound({ status: 404 })).toBe(true);
    expect(isRunNotFound({ code: 60012 })).toBe(true);
    expect(isRunNotFound({ status: 500, code: 50000 })).toBe(false);
    expect(isRunNotFound(null)).toBe(false);
    expect(isRunNotFound(new Error("x"))).toBe(false);
  });
});

describe("抽屉状态", () => {
  test("打开新的行：进入加载中并丢掉旧详情", () => {
    const loaded = runDrawerReducer(
      runDrawerReducer(initialRunDrawer, { type: "open", rid: "a" }),
      { type: "loaded", rid: "a", detail: detail() },
    );
    expect(loaded.phase).toBe("ready");
    const next = runDrawerReducer(loaded, { type: "open", rid: "b" });
    expect(next).toEqual({ rid: "b", phase: "loading", detail: null });
  });
  test("旧请求晚到的结果被丢弃", () => {
    const s = runDrawerReducer(initialRunDrawer, { type: "open", rid: "b" });
    expect(runDrawerReducer(s, { type: "loaded", rid: "a", detail: detail() })).toBe(s);
    expect(runDrawerReducer(s, { type: "failed", rid: "a", notFound: true })).toBe(s);
  });
  test("失败：404 是 notfound，其他是 error", () => {
    const s = runDrawerReducer(initialRunDrawer, { type: "open", rid: "a" });
    expect(runDrawerReducer(s, { type: "failed", rid: "a", notFound: true }).phase).toBe(
      "notfound",
    );
    expect(runDrawerReducer(s, { type: "failed", rid: "a", notFound: false }).phase).toBe("error");
  });
  test("关闭后清理回初始", () => {
    const s = runDrawerReducer(initialRunDrawer, { type: "open", rid: "a" });
    expect(runDrawerReducer(s, { type: "close" })).toEqual(initialRunDrawer);
  });
});
