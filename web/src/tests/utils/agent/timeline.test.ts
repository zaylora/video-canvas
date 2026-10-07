import { describe, expect, test } from "bun:test";

import type { AgentEventDto } from "@/api/agent/type";
import { applyAgentEvent, emptySession } from "@/utils/agent/session-state";
import { buildRunStrip, buildTimeline } from "@/utils/agent/timeline";

let seq = 0;
const ev = (
  type: string,
  data: Record<string, unknown> = {},
  run: string | null = "r1",
  s = ++seq,
): AgentEventDto => ({
  session_id: "s1",
  canvas_id: "c1",
  run_id: run,
  seq: s,
  type,
  data,
  created_at: "",
});
const fold = (events: AgentEventDto[]) => events.reduce(applyAgentEvent, emptySession());
const reset = () => (seq = 0);

describe("消息流", () => {
  test("用户消息、助手回复、工具行按顺序排好；工具行由 start 开头 end 补全", () => {
    reset();
    const items = buildTimeline(
      fold([
        ev("message.user", { text: "拆分镜" }),
        ev("run.status", { status: "running" }),
        ev("message.done", { text: "我先读剧本", thinking: "想想" }),
        ev("tool.start", { id: "c1", name: "canvas_get_state" }),
        ev("tool.end", {
          id: "c1",
          name: "canvas_get_state",
          is_error: false,
          summary: "读取画布目录（3 个节点）",
          node_ids: ["a"],
        }),
      ]),
    );
    expect(items.map((i) => i.type)).toEqual(["user", "assistant", "tool"]);
    expect(items[1]).toMatchObject({ text: "我先读剧本", thinking: "想想", streaming: false });
    expect(items[2]).toMatchObject({
      name: "canvas_get_state",
      status: "done",
      summary: "读取画布目录（3 个节点）",
      nodeIds: ["a"],
    });
  });

  test("工具失败标为 error；没有 tool.start 只有 tool.end（回放缺了前半）也能显示", () => {
    reset();
    const items = buildTimeline(
      fold([
        ev("tool.end", {
          id: "x",
          name: "canvas_apply_ops",
          is_error: true,
          summary: "第 1 项不合法",
        }),
      ]),
    );
    expect(items[0]).toMatchObject({ type: "tool", status: "error", summary: "第 1 项不合法" });
  });

  test("运行结束了还没有结果的工具：已中止；运行还在跑的仍是进行中", () => {
    reset();
    const running = buildTimeline(
      fold([
        ev("run.status", { status: "running" }),
        ev("tool.start", { id: "c1", name: "generate_media" }),
      ]),
    );
    expect(running[0]).toMatchObject({ status: "running" });
    const ended = buildTimeline(
      fold([
        ev("run.status", { status: "running" }),
        ev("tool.start", { id: "c1", name: "generate_media" }),
        ev("run.status", { status: "canceled" }),
      ]),
    );
    expect(ended.find((i) => i.type === "tool")).toMatchObject({ status: "aborted" });
  });

  test("插话标出来；没有正文也没有思考的 message.done 不占位", () => {
    reset();
    const items = buildTimeline(
      fold([
        ev("message.user", { text: "再加一个", steer: true }),
        ev("message.done", { text: "", tool_calls: [] }),
      ]),
    );
    expect(items).toHaveLength(1);
    expect(items[0]).toMatchObject({ type: "user", steer: true });
  });

  test("审批卡片只放一次；运行状态卡片只给值得说的状态，同一轮同一状态不重复", () => {
    reset();
    const items = buildTimeline(
      fold([
        ev("run.status", { status: "running" }),
        ev("approval.created", { id: "a1", status: "pending", kind: "generate" }),
        ev("approval.decided", { id: "a1", status: "executed", kind: "generate" }),
        ev("run.status", { status: "failed", error: "Agent 模型不可用" }),
        ev("run.status", { status: "failed", error: "Agent 模型不可用" }),
        ev("run.status", { status: "succeeded" }),
      ]),
    );
    expect(items.map((i) => i.type)).toEqual(["approval", "status"]);
    expect(items[1]).toMatchObject({ status: "failed", error: "Agent 模型不可用" });
  });

  test("计划全部完成时留一张卡片，每轮只一张", () => {
    reset();
    const done = [
      { title: "a", status: "done" },
      { title: "b", status: "done" },
    ];
    const items = buildTimeline(
      fold([
        ev("plan.updated", { steps: [{ title: "a", status: "doing" }] }),
        ev("plan.updated", { steps: done }),
        ev("plan.updated", { steps: done }),
      ]),
    );
    expect(items).toEqual([{ type: "plan-done", key: expect.any(String), total: 2 }]);
  });

  test("流式文字放在最后", () => {
    reset();
    const items = buildTimeline(
      fold([ev("message.user", { text: "hi" }), ev("message.delta", { text: "你好" }, "r1", 0)]),
    );
    expect(items.at(-1)).toMatchObject({ type: "assistant", text: "你好", streaming: true });
  });
});

describe("撤销本轮的操作条", () => {
  const write = (run: string) => [
    ev("run.status", { status: "running" }, run),
    ev("tool.start", { id: `${run}t`, name: "canvas_apply_ops" }, run),
    ev(
      "tool.end",
      { id: `${run}t`, name: "canvas_apply_ops", is_error: false, summary: "应用 3 项" },
      run,
    ),
  ];

  test("改过画布且已收尾的运行：最后一项后面放操作条", () => {
    reset();
    const items = buildTimeline(
      fold([
        ...write("r1"),
        ev("message.done", { text: "搭好了" }, "r1"),
        ev("run.status", { status: "succeeded" }, "r1"),
      ]),
    );
    expect(items.map((i) => i.type)).toEqual(["tool", "assistant", "run-footer"]);
    expect(items[2]).toMatchObject({ runId: "r1" });
  });

  test("还在运行、只读、或工具失败：没有操作条", () => {
    reset();
    expect(buildTimeline(fold(write("r1"))).some((i) => i.type === "run-footer")).toBe(false);
    reset();
    const readOnly = fold([
      ev("run.status", { status: "running" }),
      ev("tool.end", { id: "x", name: "canvas_get_state", is_error: false }),
      ev("run.status", { status: "succeeded" }),
    ]);
    expect(buildTimeline(readOnly).some((i) => i.type === "run-footer")).toBe(false);
    reset();
    const failed = fold([
      ev("run.status", { status: "running" }),
      ev("tool.end", { id: "x", name: "canvas_apply_ops", is_error: true }),
      ev("run.status", { status: "succeeded" }),
    ]);
    expect(buildTimeline(failed).some((i) => i.type === "run-footer")).toBe(false);
  });

  test("多轮运行各自一个操作条，位置不错位", () => {
    reset();
    const items = buildTimeline(
      fold([
        ...write("r1"),
        ev("run.status", { status: "succeeded" }, "r1"),
        ev("message.user", { text: "再来" }, "r2"),
        ...write("r2"),
        ev("run.status", { status: "succeeded" }, "r2"),
      ]),
    );
    expect(items.map((i) => i.type)).toEqual(["tool", "run-footer", "user", "tool", "run-footer"]);
    expect(
      items.filter((i) => i.type === "run-footer").map((i) => (i as { runId: string }).runId),
    ).toEqual(["r1", "r2"]);
  });
});

describe("置顶运行条", () => {
  const plan = (statuses: string[]) => ({
    steps: statuses.map((s, i) => ({ title: `步骤${i + 1}`, status: s })),
  });
  const hidden = new Set<string>();

  test("没有运行：不显示", () => {
    expect(buildRunStrip(emptySession(), hidden)).toBeNull();
  });

  test("运行中有计划：进度、当前步骤", () => {
    reset();
    const s = fold([
      ev("run.status", { status: "running" }),
      ev("plan.updated", plan(["done", "doing", "todo"])),
    ]);
    expect(buildRunStrip(s, hidden)).toMatchObject({
      progress: { done: 1, total: 3 },
      current: "步骤2",
      active: true,
      prefix: "",
      unfinished: false,
    });
  });

  test("运行中没有计划：只有状态", () => {
    reset();
    const strip = buildRunStrip(fold([ev("run.status", { status: "running" })]), hidden);
    expect(strip).toMatchObject({ progress: null, current: "", active: true });
  });

  test("等批准、等回答带前缀", () => {
    reset();
    expect(
      buildRunStrip(fold([ev("run.status", { status: "waiting_approval" })]), hidden)?.prefix,
    ).toBe("等你确认");
    expect(
      buildRunStrip(fold([ev("run.status", { status: "waiting_input" })]), hidden)?.prefix,
    ).toBe("等你回答");
  });

  test("运行结束、计划没做完：显示未完成，用户隐藏后不再显示", () => {
    reset();
    const s = fold([
      ev("run.status", { status: "running" }),
      ev("plan.updated", plan(["done", "todo"])),
      ev("run.status", { status: "canceled" }),
    ]);
    expect(buildRunStrip(s, hidden)).toMatchObject({
      unfinished: true,
      active: false,
      current: "步骤2",
    });
    expect(buildRunStrip(s, new Set(["r1"]))).toBeNull();
  });

  test("运行结束、计划全部完成或没有计划：不显示", () => {
    reset();
    expect(
      buildRunStrip(
        fold([
          ev("run.status", { status: "running" }),
          ev("plan.updated", plan(["done"])),
          ev("run.status", { status: "succeeded" }),
        ]),
        hidden,
      ),
    ).toBeNull();
    expect(
      buildRunStrip(
        fold([ev("run.status", { status: "running" }), ev("run.status", { status: "succeeded" })]),
        hidden,
      ),
    ).toBeNull();
  });

  test("多轮运行：以最新那一轮为准", () => {
    reset();
    const s = fold([
      ev("run.status", { status: "running" }, "r1"),
      ev("plan.updated", plan(["todo"]), "r1"),
      ev("run.status", { status: "canceled" }, "r1"),
      ev("run.status", { status: "running" }, "r2"),
    ]);
    expect(buildRunStrip(s, hidden)).toMatchObject({ runId: "r2", active: true, progress: null });
  });
});
