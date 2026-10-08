import { describe, expect, test } from "bun:test";

import type { AgentEventDto } from "@/api/agent/type";
import { applyAgentEvent, emptySession } from "@/utils/agent/session-state";
import {
  buildRunStrip,
  buildStatusLine,
  buildTimeline,
  type TimelineItem,
} from "@/utils/agent/timeline";

let seq = 0;
/** 事件时间：第 n 条事件在基准时间之后 n 秒，方便断言耗时 */
const at = (n: number) => new Date(Date.UTC(2026, 9, 8, 12, 0, n)).toISOString();
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
  created_at: s > 0 ? at(s) : "",
});
const fold = (events: AgentEventDto[]) => events.reduce(applyAgentEvent, emptySession());
const reset = () => (seq = 0);

describe("消息流", () => {
  test("用户消息、助手回复、活动块按顺序排好；工具调用由 start 开头 end 补全", () => {
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
    expect(items.map((i) => i.type)).toEqual(["user", "assistant", "activity"]);
    expect(items[1]).toMatchObject({ text: "我先读剧本", thinking: "想想", streaming: false });
    // 运行还在跑，工具之间的空档仍算进行中
    expect(items[2]).toMatchObject({ status: "running", errors: 0 });
    expect(activity(items[2]).tools).toEqual([
      {
        key: "tc1",
        name: "canvas_get_state",
        status: "done",
        summary: "读取画布目录（3 个节点）",
        nodeIds: ["a"],
      },
    ]);
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
    expect(items[0]).toMatchObject({ type: "activity", status: "done", errors: 1 });
    expect(activity(items[0]).tools[0]).toMatchObject({
      status: "error",
      summary: "第 1 项不合法",
    });
  });

  test("运行结束了还没有结果的工具：已中止；运行还在跑的仍是进行中", () => {
    reset();
    const running = buildTimeline(
      fold([
        ev("run.status", { status: "running" }),
        ev("tool.start", { id: "c1", name: "generate_media" }),
      ]),
    );
    expect(running[0]).toMatchObject({ type: "activity", status: "running" });
    const ended = buildTimeline(
      fold([
        ev("run.status", { status: "running" }),
        ev("tool.start", { id: "c1", name: "generate_media" }),
        ev("run.status", { status: "canceled" }),
      ]),
    );
    const block = ended.find((i) => i.type === "activity");
    expect(block).toMatchObject({ status: "aborted" });
    expect(activity(block).tools[0].status).toBe("aborted");
  });

  test("运行还在跑时，最后一个活动块在工具之间的空档也保持进行中；运行结束后恢复 done", () => {
    reset();
    const events = [
      ev("run.status", { status: "running" }),
      ev("tool.start", { id: "c1", name: "read_canvas" }),
      ev("tool.end", { id: "c1", name: "read_canvas" }),
    ];
    const live = buildTimeline(fold(events));
    expect(live[0]).toMatchObject({ type: "activity", status: "running" });
    const finished = buildTimeline(fold([...events, ev("run.status", { status: "completed" })]));
    expect(finished.find((i) => i.type === "activity")).toMatchObject({ status: "done" });
  });

  test("连续的工具调用合成一个活动块，中间隔了消息就分开；耗时从第一次开始到最后一次结束", () => {
    reset();
    const items = buildTimeline(
      fold([
        ev("run.status", { status: "running" }),
        ev("tool.start", { id: "a", name: "canvas_get_state" }),
        ev("tool.end", { id: "a", name: "canvas_get_state", summary: "读取" }),
        ev("tool.start", { id: "b", name: "canvas_apply_ops" }),
        ev("tool.end", { id: "b", name: "canvas_apply_ops", summary: "应用" }),
        ev("message.done", { text: "搭好了" }),
        ev("tool.start", { id: "c", name: "canvas_arrange" }),
        ev("tool.end", { id: "c", name: "canvas_arrange", summary: "整理" }),
      ]),
    );
    expect(items.map((i) => i.type)).toEqual(["activity", "assistant", "activity"]);
    const first = activity(items[0]);
    expect(first.tools.map((t) => t.name)).toEqual(["canvas_get_state", "canvas_apply_ops"]);
    // 第 2 条事件开始、第 5 条事件结束：3 秒
    expect(first.endedAt! - first.startedAt!).toBe(3000);
    expect(activity(items[2]).tools).toHaveLength(1);
  });

  test("不同轮次的工具调用不并在一块", () => {
    reset();
    const items = buildTimeline(
      fold([
        ev("tool.end", { id: "a", name: "canvas_get_state" }, "r1"),
        ev("tool.end", { id: "b", name: "canvas_get_state" }, "r2"),
      ]),
    );
    expect(items.map((i) => i.type)).toEqual(["activity", "activity"]);
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

describe("本轮改动摘要", () => {
  const write = (run: string, created: string[] = ["n1"], updated: string[] = []) => [
    ev("run.status", { status: "running" }, run),
    ev("tool.start", { id: `${run}t`, name: "canvas_apply_ops" }, run),
    ev(
      "tool.end",
      {
        id: `${run}t`,
        name: "canvas_apply_ops",
        is_error: false,
        summary: "应用 3 项",
        created,
        updated,
      },
      run,
    ),
  ];

  test("改过画布且已收尾的运行：最后一项后面放摘要", () => {
    reset();
    const items = buildTimeline(
      fold([
        ...write("r1"),
        ev("message.done", { text: "搭好了" }, "r1"),
        ev("run.status", { status: "succeeded" }, "r1"),
      ]),
    );
    expect(items.map((i) => i.type)).toEqual(["activity", "assistant", "run-summary"]);
    expect(items[2]).toMatchObject({ runId: "r1", created: ["n1"], updated: [], deleted: [] });
  });

  test("新建、修改按节点去重；同一轮里新建后又改的只算新建；批准执行的删除算删除", () => {
    reset();
    const items = buildTimeline(
      fold([
        ...write("r1", ["n1", "n2"], []),
        ev("tool.start", { id: "u", name: "canvas_apply_ops" }),
        ev("tool.end", { id: "u", name: "canvas_apply_ops", created: [], updated: ["n1", "s1"] }),
        ev("tool.end", { id: "u2", name: "canvas_apply_ops", updated: ["s1"] }),
        ev("approval.created", { id: "d1", run_id: "r1", kind: "delete", status: "pending" }),
        ev("approval.decided", {
          id: "d1",
          run_id: "r1",
          kind: "delete",
          status: "executed",
          payload: { node_ids: ["x", "n2", "y"], labels: ["废稿 1", "镜头 2", "废稿 3"] },
          decision: { decision: "approve", node_ids: ["x", "n2"] },
        }),
        ev("run.status", { status: "succeeded" }),
      ]),
    );
    expect(items.find((i) => i.type === "run-summary")).toMatchObject({
      created: ["n1"],
      updated: ["s1"],
      deleted: [
        { id: "x", label: "废稿 1" },
        { id: "n2", label: "镜头 2" },
      ],
    });
  });

  test("还在运行、只读、或工具失败：没有摘要", () => {
    reset();
    expect(buildTimeline(fold(write("r1"))).some((i) => i.type === "run-summary")).toBe(false);
    reset();
    const readOnly = fold([
      ev("run.status", { status: "running" }),
      ev("tool.end", { id: "x", name: "canvas_get_state", is_error: false }),
      ev("run.status", { status: "succeeded" }),
    ]);
    expect(buildTimeline(readOnly).some((i) => i.type === "run-summary")).toBe(false);
    reset();
    const failed = fold([
      ev("run.status", { status: "running" }),
      ev("tool.end", { id: "x", name: "canvas_apply_ops", is_error: true }),
      ev("run.status", { status: "succeeded" }),
    ]);
    expect(buildTimeline(failed).some((i) => i.type === "run-summary")).toBe(false);
  });

  test("多轮运行各自一个摘要，位置不错位", () => {
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
    expect(items.map((i) => i.type)).toEqual([
      "activity",
      "run-summary",
      "user",
      "activity",
      "run-summary",
    ]);
    expect(
      items.filter((i) => i.type === "run-summary").map((i) => (i as { runId: string }).runId),
    ).toEqual(["r1", "r2"]);
  });
});

describe("置顶计划条", () => {
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
      unfinished: false,
    });
  });

  test("运行中没有计划：不显示（运行状态由消息流末尾的状态行负责）", () => {
    reset();
    expect(buildRunStrip(fold([ev("run.status", { status: "running" })]), hidden)).toBeNull();
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
      ev("plan.updated", plan(["doing"]), "r2"),
    ]);
    expect(buildRunStrip(s, hidden)).toMatchObject({ runId: "r2", active: true });
  });
});

describe("状态行", () => {
  test("没有进行中的运行：不显示", () => {
    reset();
    expect(buildStatusLine(emptySession())).toBeNull();
    expect(
      buildStatusLine(
        fold([ev("run.status", { status: "running" }), ev("run.status", { status: "succeeded" })]),
      ),
    ).toBeNull();
  });

  test("运行中：有进行中的工具就说在做什么，否则是思考中；计时从这一段运行开始", () => {
    reset();
    const thinking = fold([
      ev("run.status", { status: "queued" }),
      ev("run.status", { status: "running" }),
    ]);
    expect(buildStatusLine(thinking)).toEqual({
      runId: "r1",
      text: "思考中…",
      waiting: false,
      since: Date.parse(at(1)),
    });
    const working = fold([
      ...thinking.events,
      ev("tool.start", { id: "a", name: "canvas_arrange" }),
    ]);
    expect(buildStatusLine(working)?.text).toBe("正在整理布局…");
    const doneTool = fold([...working.events, ev("tool.end", { id: "a", name: "canvas_arrange" })]);
    expect(buildStatusLine(doneTool)?.text).toBe("思考中…");
  });

  test("正文流式输出时不显示，避免和闪烁光标重复", () => {
    reset();
    const s = fold([
      ev("run.status", { status: "running" }),
      ev("message.delta", { text: "好的" }, "r1", 0),
    ]);
    expect(buildStatusLine(s)).toBeNull();
  });

  test("等你确认 / 等你回答：静态提示", () => {
    reset();
    expect(buildStatusLine(fold([ev("run.status", { status: "waiting_approval" })]))).toMatchObject(
      { text: "等你确认", waiting: true },
    );
    reset();
    expect(buildStatusLine(fold([ev("run.status", { status: "waiting_input" })]))).toMatchObject({
      text: "等你回答",
      waiting: true,
    });
  });
});

/** 取活动块（断言用） */
function activity(item: TimelineItem | undefined) {
  if (item?.type !== "activity") throw new Error("不是活动块");
  return item;
}
