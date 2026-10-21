import { describe, expect, test } from "bun:test";
import { renderToStaticMarkup } from "react-dom/server";

import type { AgentRunDetail } from "@/api/admin/agent-run/type.d";
import { RunDrawerView } from "@/pages/admin/agent-runs/run-drawer-view";
import { initialRunDrawer, type RunDrawerState } from "@/utils/admin/agent-run-detail";

import { detail, digest, step, tool } from "./fixtures";

const noop = () => undefined;
const drawer = (state: RunDrawerState, props: { copying?: boolean; expandAll?: boolean } = {}) =>
  renderToStaticMarkup(
    <RunDrawerView
      state={state}
      copying={props.copying ?? false}
      expandAll={props.expandAll}
      onCopyMarkdown={noop}
      onDownload={noop}
      onRetry={noop}
    />,
  );
const ready = (d: AgentRunDetail, props: { copying?: boolean; expandAll?: boolean } = {}) =>
  drawer({ rid: d.digest?.run_id ?? "rid", phase: "ready", detail: d }, props);

describe("抽屉：加载与失败状态", () => {
  test("加载中：骨架，没有操作栏", () => {
    const out = drawer({ rid: "a", phase: "loading", detail: null });
    expect(out).toContain('data-slot="run-drawer-skeleton"');
    expect(out).toContain('aria-busy="true"');
    expect(out).not.toContain("复制为 Markdown");
  });
  test("刚挂载还没开始请求（idle）也是骨架", () => {
    expect(drawer(initialRunDrawer)).toContain('data-slot="run-drawer-skeleton"');
  });
  test("404：显示「运行不存在」，没有重试", () => {
    const out = drawer({ rid: "a", phase: "notfound", detail: null });
    expect(out).toContain("运行不存在");
    expect(out).not.toContain("重试");
  });
  test("其他错误：Notice 加重试", () => {
    const out = drawer({ rid: "a", phase: "error", detail: null });
    expect(out).toContain('data-slot="notice"');
    expect(out).toContain("运行详情加载失败");
    expect(out).toContain("重试");
  });
});

describe("抽屉：头部、Harness、操作栏", () => {
  test("请求原文取时间线里第一个 user 步骤；基础信息齐全", () => {
    const out = ready(
      detail({
        digest: digest({
          status: "failed",
          steps: 7,
          spent_credits: 12,
          input_tokens: 1200,
          output_tokens: 300,
        }),
        timeline: [step({ n: 1, kind: "user", label: "用户", text: "请把整条时间线重排" })],
      }),
    );
    expect(out).toContain("请把整条时间线重排");
    expect(out).toContain("失败");
    expect(out).toContain("1m5s");
    expect(out).toContain(">7<");
    expect(out).toContain(">12<");
    expect(out).toContain("1,200 / 300");
    for (const label of [
      "状态",
      "创建时间",
      "结束时间",
      "耗时",
      "步数",
      "积分",
      "Token（输入 / 输出）",
    ])
      expect(out).toContain(label);
  });
  test("时间线里没有 user 步骤：退回请求首句", () => {
    expect(ready(detail({ digest: digest({ request_head: "首句兜底" }), timeline: [] }))).toContain(
      "首句兜底",
    );
  });
  test("Harness：提示词版本、harness_key、build_sha、模型、技能版本", () => {
    const out = ready(
      detail({ digest: digest({ skills: { storyboard: "v3", "canvas-basics": "builtin" } }) }),
    );
    expect(out).toContain("v10");
    expect(out).toContain("a1b2c3d4e5f6");
    expect(out).toContain("abc1234");
    expect(out).toContain("gpt-test");
    expect(out).toContain("storyboard v3");
    expect(out).toContain("canvas-basics builtin");
  });
  test("没有读取技能 / 没有摘要：给出说明", () => {
    expect(ready(detail())).toContain("没有读取技能");
    expect(ready(detail({ digest: null }))).toContain("摘要还没生成");
  });
  test("操作栏：复制为 Markdown、下载 JSON；复制中按钮置灰", () => {
    const out = ready(detail());
    expect(out).toContain("复制为 Markdown");
    expect(out).toContain("下载 JSON");
    const busy = ready(detail(), { copying: true });
    expect(busy).toContain("复制中…");
    expect(/<button[^>]*>复制中…/.exec(busy)?.[0]).toContain(' disabled=""');
  });
});

describe("抽屉：用户信号", () => {
  test("没有任何信号：写「没有用户反馈或撤销」", () => {
    const out = ready(detail());
    expect(out).toContain("没有用户反馈或撤销");
    expect(out).not.toContain('data-slot="run-signals"');
  });
  test("点踩：原因中文标签 + 说明原文；撤销、插话、被拒审批、工具报错", () => {
    const out = ready(
      detail({
        digest: digest({
          feedback: -1,
          undone: true,
          steer_count: 2,
          approvals_rejected: 1,
          tool_errors: 3,
        }),
        feedback: {
          rating: -1,
          reasons: ["misread", "wrong_edit", "slow", "bad_output", "other"],
          comment: "改错了镜头 3",
        },
      }),
    );
    expect(out).toContain('data-slot="run-signals"');
    expect(out).toContain("没帮上忙");
    for (const label of ["没理解我的需求", "画布改错了", "步骤太多或太慢", "生成结果不好", "其他"])
      expect(out).toContain(label);
    expect(out).toContain("改错了镜头 3");
    expect(out).toContain("用户撤销了这一轮的画布改动");
    expect(out).toContain("2 次");
    expect(out).toContain("1 次");
    expect(out).toContain("3 次");
    expect(out).not.toContain("没有用户反馈或撤销");
  });
  test("点赞：显示有帮助", () => {
    const out = ready(
      detail({
        digest: digest({ feedback: 1 }),
        feedback: { rating: 1, reasons: [], comment: "" },
      }),
    );
    expect(out).toContain("有帮助");
  });
});

describe("抽屉：时间线各 kind", () => {
  const timeline = [
    step({ n: 1, kind: "user", label: "用户", text: "用户的话", at: "2026-10-22T09:00:00Z" }),
    step({ n: 2, kind: "steer", label: "用户插话", text: "换成夜景", at: "2026-10-22T09:00:05Z" }),
    step({
      n: 3,
      kind: "assistant",
      label: "模型回复",
      text: "我先看一下画布",
      thinking: "先读取节点再决定",
      at: "2026-10-22T09:00:07.500Z",
    }),
    step({ n: 4, kind: "assistant", label: "模型回复", text: "没说完", partial: true }),
    step({
      n: 5,
      kind: "tool",
      label: "工具调用",
      text: "",
      tool: tool(),
      at: "2026-10-22T09:01:05Z",
    }),
    step({ n: 6, kind: "approval", label: "审批请求", text: "kind=generate" }),
    step({ n: 7, kind: "plan", label: "计划", text: "3 项" }),
    step({ n: 8, kind: "undo", label: "用户撤销本轮", text: "" }),
    step({ n: 9, kind: "feedback", label: "用户反馈", text: "👎 没理解我的需求" }),
    step({ n: 10, kind: "notice", label: "运行时提示", text: "超时重试" }),
    step({ n: 11, kind: "status", label: "状态", text: "failed" }),
    step({ n: 12, kind: "other", label: "tool.weird", text: "x=1" }),
  ];
  const out = ready(detail({ timeline }));

  test("每步带 #编号（等宽小字）、类型名和相对运行开始的时间", () => {
    for (let n = 1; n <= 12; n++) expect(out).toContain(`>#${n}</code>`);
    expect(out).toContain('class="text-muted-foreground shrink-0 font-mono text-[11px]">#1<');
    for (const label of [
      "用户插话",
      "审批请求",
      "计划",
      "用户撤销本轮",
      "用户反馈",
      "运行时提示",
      "状态",
    ])
      expect(out).toContain(label);
    expect(out).toContain("+5.0s");
    expect(out).toContain("+7.5s");
    expect(out).toContain("+1m05s");
  });
  test("user / steer / assistant 显示正文；半截回答有标记", () => {
    for (const text of ["用户的话", "换成夜景", "我先看一下画布", "没说完"])
      expect(out).toContain(text);
    expect(out).toContain("半截回答，被中断");
  });
  test("assistant 的思考默认折叠：只有「思考」触发行，没有正文", () => {
    expect(out).toContain("思考");
    expect(out).not.toContain("先读取节点再决定");
    expect(ready(detail({ timeline }), { expandAll: true })).toContain("先读取节点再决定");
  });
  test("tool 步骤默认折叠：一行「工具名 · 耗时 · 成败」，参数和结果不渲染", () => {
    expect(out).toContain("canvas_patch");
    expect(out).toContain("820ms");
    expect(out).toContain("成功");
    expect(out).not.toContain("ok-result");
    expect(out).not.toContain("改了 1 个节点");
  });
  test("approval / plan / undo / feedback / notice / status / other：label + text 一行", () => {
    expect(out).toContain("kind=generate");
    expect(out).toContain("3 项");
    expect(out).toContain("超时重试");
    expect(out).toContain("tool.weird");
    expect(out).toContain("x=1");
  });
  test("undo 警告色、feedback 👎 红色、status 失败红色（状态值译成中文）", () => {
    const li = (kind: string) =>
      new RegExp(`<li[^>]*data-kind="${kind}".*?</li>`).exec(out)?.[0] ?? "";
    expect(li("undo")).toContain("text-status-warning");
    expect(li("feedback")).toContain("text-destructive");
    expect(li("status")).toContain("text-destructive");
    expect(li("status")).toContain("失败");
    expect(li("plan")).not.toContain("text-destructive");
    expect(li("plan")).not.toContain("text-status-warning");
  });
  test("时间线为空：给说明", () => {
    expect(ready(detail({ timeline: [] }))).toContain("这次运行没有记录到步骤");
  });
});

describe("抽屉：工具步骤展开", () => {
  const open = (t: ReturnType<typeof tool>, extra: Partial<ReturnType<typeof step>> = {}) =>
    ready(
      detail({
        timeline: [step({ n: 3, kind: "tool", label: "工具调用", text: "", tool: t, ...extra })],
      }),
      {
        expandAll: true,
      },
    );

  test("展开后：参数、结果、摘要；参数和结果在可滚动、限制高度的等宽块里", () => {
    const out = open(tool());
    expect(out).toContain("{&quot;node&quot;:&quot;n1&quot;}");
    expect(out).toContain("ok-result");
    expect(out).toContain("改了 1 个节点");
    expect(out).toMatch(/<pre class="[^"]*max-h-48[^"]*overflow-auto[^"]*font-mono[^"]*">/);
  });
  test("报错：标题和结果用 danger 高亮，标题写「报错」", () => {
    const out = open(tool({ is_error: true }));
    expect(out).toContain("报错");
    expect(out).toContain("text-destructive font-medium");
    expect(out).toContain("bg-destructive/10");
  });
  test("truncated：提示「已截断，完整内容请下载 JSON」", () => {
    expect(open(tool({ truncated: true }))).toContain("已截断，完整内容请下载 JSON");
    expect(open(tool())).not.toContain("已截断");
  });
  test("purged_at：提示已过保留期（日期），参数和结果已清理，摘要和耗时仍在", () => {
    const out = open(
      tool({ purged_at: "2026-10-20T03:00:00Z", args: undefined, result: undefined }),
    );
    expect(out).toContain("已过保留期（");
    expect(out).toContain("参数和结果已清理，摘要和耗时仍在");
    expect(out).toContain("改了 1 个节点");
    expect(out).not.toContain(">参数<");
  });
  test("recorded=false：提示「未记录参数」", () => {
    const out = open(
      tool({
        recorded: false,
        args: undefined,
        result: undefined,
        summary: undefined,
        duration_ms: undefined,
      }),
    );
    expect(out).toContain("未记录参数");
  });
  test("nested：缩进并带「↳」；顶层调用没有", () => {
    const nested = open(tool(), { nested: true });
    expect(nested).toContain("↳");
    expect(nested).toContain('data-nested="true"');
    expect(nested).toMatch(/<li[^>]*class="[^"]*ml-5[^"]*"/);
    const top = open(tool());
    expect(top).not.toContain("↳");
    expect(top).not.toContain("data-nested");
  });
});

describe("抽屉：折叠区与画布改动", () => {
  const full = detail({
    tool_calls: [
      {
        id: "call_1",
        parent_id: "",
        name: "canvas_patch",
        args: {},
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
    mutations: [
      {
        seq: 1,
        tool_call_id: "call_1",
        kind: "update_nodes",
        changes: [{}, {}, {}],
        revision_before: 1,
        revision_after: 2,
        undone_at: null,
      },
      {
        seq: 2,
        tool_call_id: "call_zz",
        kind: "add_nodes",
        changes: [{}],
        revision_before: 2,
        revision_after: 3,
        undone_at: "2026-10-22T09:05:00Z",
      },
    ],
    model_calls: [
      {
        purpose: "main",
        model_key: "gpt-test",
        channel_key: "ch-a",
        upstream_model: "gpt-up",
        input_tokens: 1200,
        output_tokens: 300,
        cached_tokens: 0,
        usage_estimated: false,
        credits: 1,
        charged: 1,
        latency_ms: 2500,
        ttft_ms: 400,
        stop_reason: "end_turn",
        error: "上游 429",
        created_at: "2026-10-22T09:00:01Z",
      },
    ],
    runtime_tail: "node: boom at line 3",
    context: [
      {
        session_id: "s",
        canvas_id: "c",
        run_id: null,
        seq: 1,
        type: "message.user",
        data: { text: "上一轮问题" },
        created_at: "2026-10-22T08:00:00Z",
      },
      {
        session_id: "s",
        canvas_id: "c",
        run_id: null,
        seq: 2,
        type: "message.done",
        data: { text: "上一轮回答" },
        created_at: "2026-10-22T08:00:05Z",
      },
    ],
  });

  test("没有对应数据时，这些区块都不出现", () => {
    const out = ready(detail());
    for (const title of ["画布改动", "模型调用", "Node 进程输出", "上下文"])
      expect(out).not.toContain(title);
  });
  test("画布改动：序号、种类、改动条数、来自哪个工具调用、是否已撤销", () => {
    const out = ready(full);
    expect(out).toContain("画布改动");
    expect(out).toContain("update_nodes");
    expect(out).toContain(">3<");
    expect(out).toContain("canvas_patch");
    expect(out).toContain("已撤销");
  });
  test("折叠区默认折叠：只有标题行，内容不渲染", () => {
    const out = ready(full);
    for (const title of ["模型调用", "Node 进程输出", "上下文"]) expect(out).toContain(title);
    expect(out).not.toContain("gpt-up");
    expect(out).not.toContain("node: boom");
    expect(out).not.toContain("上一轮问题");
  });
  test("展开后：模型调用各列、Node 输出预格式化文本、上下文各一行", () => {
    const out = ready(full, { expandAll: true });
    for (const head of [
      "用途",
      "模型",
      "渠道",
      "上游模型",
      "输入 / 输出 Token",
      "耗时",
      "首字",
      "结束原因",
      "错误",
    ])
      expect(out).toContain(`>${head}</th>`);
    for (const cell of [
      "对话",
      "ch-a",
      "gpt-up",
      "1,200 / 300",
      "2.50s",
      "400ms",
      "end_turn",
      "上游 429",
    ])
      expect(out).toContain(cell);
    expect(out).toMatch(
      /<pre data-slot="runtime-tail"[^>]*font-mono[^>]*>node: boom at line 3<\/pre>/,
    );
    expect(out).toContain("上一轮问题");
    expect(out).toContain("上一轮回答");
  });
});
