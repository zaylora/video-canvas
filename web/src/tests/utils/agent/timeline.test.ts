import { describe, expect, test } from "bun:test";

import type { AgentEventDto } from "@/api/agent/type";
import {
  ACTIVE_RUN_STATUSES,
  activeRunId,
  applyAgentEvent,
  emptySession,
} from "@/utils/agent/session-state";
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
    // 运行还在跑：不放评价条
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

  test("工具调用带上事件里的界面名称和动词；end 里有就以 end 为准，缺了前半也能显示", () => {
    reset();
    const items = buildTimeline(
      fold([
        ev("run.status", { status: "running" }),
        ev("tool.start", { id: "a", name: "web_search", label: "搜索网页", verb: "搜索" }),
        ev("tool.end", {
          id: "a",
          name: "web_search",
          label: "搜索网页",
          verb: "搜索",
          summary: "5 条结果",
        }),
        ev("tool.end", {
          id: "b",
          name: "web_fetch",
          label: "读取网页",
          verb: "读网页",
          summary: "ok",
        }),
      ]),
    );
    const tools = activity(items.find((i) => i.type === "activity")).tools;
    expect(tools.map((t) => [t.label, t.verb])).toEqual([
      ["搜索网页", "搜索"],
      ["读取网页", "读网页"],
    ]);
  });

  test("用户消息带上发送时选中的节点；没选、旧事件、插话都是空数组", () => {
    reset();
    const items = buildTimeline(
      fold([
        ev("message.user", { text: "这个是什么内容？", selection: ["n1", "n2"] }),
        ev("message.user", { text: "没选节点", selection: null }),
        ev("message.user", { text: "旧事件没有这个字段" }),
        ev("message.user", { text: "插话", steer: true }),
        ev("message.user", { text: "脏数据", selection: ["n3", 7, null, ""] }),
      ]),
    );
    const users = items.filter((i) => i.type === "user") as Extract<
      TimelineItem,
      { type: "user" }
    >[];
    expect(users.map((u) => u.selection)).toEqual([["n1", "n2"], [], [], [], ["n3"]]);
  });

  test("运行状态卡片带上后端下发的标题和提示；旧事件没有就是空串", () => {
    reset();
    const items = buildTimeline(
      fold([
        ev("run.status", { status: "running" }),
        ev("run.status", {
          status: "step_limit",
          title: "达到本轮步数上限",
          hint: "继续的话会再给一轮步数",
        }),
      ]),
    );
    expect(items.find((i) => i.type === "status")).toMatchObject({
      status: "step_limit",
      title: "达到本轮步数上限",
      hint: "继续的话会再给一轮步数",
    });
    reset();
    const old = buildTimeline(fold([ev("run.status", { status: "canceled" })]));
    expect(old.find((i) => i.type === "status")).toMatchObject({ title: "", hint: "" });
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
    expect(items.map((i) => i.type)).toEqual(["approval", "status", "run-feedback"]);
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
    expect(items.map((i) => i.type)).toEqual([
      "activity",
      "assistant",
      "run-summary",
      "run-feedback",
    ]);
    expect(items[2]).toMatchObject({ runId: "r1", created: ["n1"], updated: [], deleted: [] });
  });

  test("只截了视频帧的一轮：新建的帧图节点算新建，也有摘要和「撤销本轮」", () => {
    reset();
    const items = buildTimeline(
      fold([
        ev("run.status", { status: "running" }, "r1"),
        ev("tool.start", { id: "f", name: "canvas_capture_frame" }, "r1"),
        ev(
          "tool.end",
          {
            id: "f",
            name: "canvas_capture_frame",
            is_error: false,
            summary: "截取 2 帧",
            node_ids: ["f1", "f2"],
            created: ["f1", "f2"],
          },
          "r1",
        ),
        ev("run.status", { status: "succeeded" }, "r1"),
      ]),
    );
    expect(items.find((i) => i.type === "run-summary")).toMatchObject({
      runId: "r1",
      created: ["f1", "f2"],
      updated: [],
      deleted: [],
    });
  });

  test("看视频是只读的：不产生摘要", () => {
    reset();
    const items = buildTimeline(
      fold([
        ev("run.status", { status: "running" }, "r1"),
        ev("tool.end", { id: "v", name: "canvas_inspect_video", is_error: false }, "r1"),
        ev("run.status", { status: "succeeded" }, "r1"),
      ]),
    );
    expect(items.some((i) => i.type === "run-summary")).toBe(false);
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
      "run-feedback",
      "user",
      "activity",
      "run-summary",
      "run-feedback",
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

  test("等审批、等回答之后回到运行：计时从恢复运行那一刻算，不把等待的几小时算进去", () => {
    reset();
    const HOURS = 11 * 3600; // 事件时间是基准之后 seq 秒
    for (const wait of ["waiting_approval", "waiting_input"]) {
      const s = fold([
        ev("run.status", { status: "queued" }, "r1", 1),
        ev("run.status", { status: "running" }, "r1", 2),
        ev("run.status", { status: wait }, "r1", 3),
        ev("run.status", { status: "running" }, "r1", HOURS),
      ]);
      expect(buildStatusLine(s)?.since).toBe(Date.parse(at(HOURS)));
    }
  });

  test("服务重启后自动续跑（recovering）：算进行中，状态行说「正在恢复…」；接上后回到正常，计时从恢复那一刻起", () => {
    reset();
    const recovering = fold([
      ev("run.status", { status: "queued" }, "r1", 1),
      ev("run.status", { status: "running" }, "r1", 2),
      ev("tool.start", { id: "a", name: "canvas_arrange" }, "r1", 3),
      ev("run.status", { status: "recovering" }, "r1", 600),
    ]);
    // 还占着画布：流式残留不会被当成运行已结束清掉
    expect(ACTIVE_RUN_STATUSES).toContain("recovering");
    expect(activeRunId(recovering)).toBe("r1");
    // 重启前那个工具已经不在了，不能继续显示「正在整理布局…」
    expect(buildStatusLine(recovering)).toEqual({
      runId: "r1",
      text: "正在恢复…",
      waiting: false,
      since: Date.parse(at(600)),
    });
    // 接上了：服务补一条 running，之后和平常一样
    const back = fold([...recovering.events, ev("run.status", { status: "running" }, "r1", 601)]);
    const line = buildStatusLine(back);
    expect(line?.text).toBe("正在整理布局…");
    expect(line?.since).toBe(Date.parse(at(600)));
  });

  test("恢复中会清掉重启前流出的半截回答（它不会再来了），状态行才能显示", () => {
    reset();
    const s = fold([
      ev("run.status", { status: "running" }, "r1", 1),
      ev("message.delta", { text: "半截回答" }, "r1", 0),
    ]);
    expect(s.stream.text).toBe("半截回答");
    const after = applyAgentEvent(s, ev("run.status", { status: "recovering" }, "r1", 2));
    expect(after.stream.text).toBe("");
    expect(buildStatusLine(after)?.text).toBe("正在恢复…");
  });

  test("恢复中不会在消息流里留一张状态卡片（不是需要用户知道的结果）", () => {
    reset();
    const items = buildTimeline(
      fold([
        ev("message.user", { text: "拆分镜" }),
        ev("run.status", { status: "running" }),
        ev("run.status", { status: "recovering" }),
        ev("run.status", { status: "running" }),
      ]),
    );
    expect(items.filter((i) => i.type === "status")).toEqual([]);
  });

  test("计时：直接 queued → running 仍从 queued 算；工具切换不重置；再次 queued 重新起算", () => {
    reset();
    const base = fold([
      ev("run.status", { status: "queued" }, "r1", 1),
      ev("run.status", { status: "running" }, "r1", 2),
      ev("tool.start", { id: "a", name: "canvas_arrange" }, "r1", 3),
      ev("tool.end", { id: "a", name: "canvas_arrange" }, "r1", 4),
    ]);
    expect(buildStatusLine(base)?.since).toBe(Date.parse(at(1)));
    const again = fold([
      ...base.events,
      ev("run.status", { status: "interrupted" }, "r1", 5),
      ev("run.status", { status: "queued" }, "r1", 50),
      ev("run.status", { status: "running" }, "r1", 51),
    ]);
    expect(buildStatusLine(again)?.since).toBe(Date.parse(at(50)));
  });

  test("工具名称由后端随事件下发：新工具不用改前端；状态行也用它", () => {
    reset();
    const working = fold([
      ev("run.status", { status: "running" }),
      ev("tool.start", { id: "w", name: "web_search", label: "搜索网页", verb: "搜索" }),
    ]);
    expect(buildStatusLine(working)?.text).toBe("正在搜索网页…");
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

describe("插话排队", () => {
  const user = (items: TimelineItem[], i: number) => {
    const it = items[i];
    if (it?.type !== "user") throw new Error(`第 ${i} 项不是用户消息：${it?.type}`);
    return it;
  };

  test("还没读到的插话停在消息流末尾排队，在流式回答之后；key 取收下它的事件序号", () => {
    reset();
    const queued = ev("message.user", { id: "u1", text: "改成竖屏", steer: true });
    const items = buildTimeline(
      fold([
        ev("message.user", { text: "拆分镜" }),
        ev("run.status", { status: "running" }),
        ev("tool.start", { id: "c1", name: "canvas_get_state" }),
        queued,
        ev("message.delta", { text: "我先读" }, "r1", 0),
      ]),
    );
    expect(items.map((i) => i.type)).toEqual(["user", "activity", "assistant", "user"]);
    expect(user(items, 3)).toMatchObject({
      text: "改成竖屏",
      steer: true,
      queued: "waiting",
      steerId: "u1",
      runId: "r1",
      key: `e${queued.seq}`,
    });
  });

  test("steer.read 之后插话才进入时间线：位置在读到它的那一刻，key 不变", () => {
    reset();
    const queued = ev("message.user", { id: "u1", text: "改成竖屏", steer: true });
    const items = buildTimeline(
      fold([
        ev("message.user", { text: "拆分镜" }),
        ev("run.status", { status: "running" }),
        ev("tool.start", { id: "c1", name: "canvas_get_state" }),
        queued,
        ev("tool.end", { id: "c1", name: "canvas_get_state", summary: "读取" }),
        ev("steer.read", { ids: ["u1"] }),
        ev("message.done", { text: "好的，竖屏" }),
      ]),
    );
    // 插话排在活动块之后、看了它的那条回答之前，不再是发送时的位置
    expect(items.map((i) => i.type)).toEqual(["user", "activity", "user", "assistant"]);
    expect(user(items, 2)).toMatchObject({ steer: true, key: `e${queued.seq}` });
    expect(user(items, 2).queued).toBeUndefined();
  });

  test("几条插话一起读到：按收下的顺序，在同一个位置", () => {
    reset();
    const items = buildTimeline(
      fold([
        ev("message.user", { text: "拆分镜" }),
        ev("run.status", { status: "running" }),
        ev("message.user", { id: "u1", text: "甲", steer: true }),
        ev("message.user", { id: "u2", text: "乙", steer: true }),
        ev("steer.read", { ids: ["u1", "u2"] }),
        ev("message.done", { text: "收到" }),
      ]),
    );
    expect(items.map((i) => (i.type === "user" ? i.text : i.type))).toEqual([
      "拆分镜",
      "甲",
      "乙",
      "assistant",
    ]);
    expect(items.slice(1, 3).every((i) => i.type === "user" && !i.queued)).toBe(true);
  });

  test("等你回答期间排着的插话还在队列里，回答之后读到才进入时间线", () => {
    reset();
    const events = [
      ev("message.user", { text: "拆分镜" }),
      ev("run.status", { status: "running" }),
      ev("message.user", { id: "u1", text: "用双剑", steer: true }),
      ev("approval.created", { id: "a1", run_id: "r1", kind: "ask", status: "pending" }),
      ev("run.status", { status: "waiting_input" }),
    ];
    const waiting = buildTimeline(fold(events));
    expect(waiting.map((i) => i.type)).toEqual(["user", "approval", "user"]);
    expect(user(waiting, 2)).toMatchObject({ queued: "waiting", runId: "r1" });

    const answered = buildTimeline(
      fold([
        ...events,
        ev("run.status", { status: "running" }),
        ev("steer.read", { ids: ["u1"] }),
        ev("message.done", { text: "好" }),
      ]),
    );
    expect(answered.map((i) => i.type)).toEqual(["user", "approval", "user", "assistant"]);
    expect(user(answered, 2).queued).toBeUndefined();
  });

  test("steer.dropped：没读到也不会再读到，留在它被丢掉的位置（在「已停止」卡片之前），标为 missed", () => {
    reset();
    const items = buildTimeline(
      fold([
        ev("message.user", { text: "拆分镜" }),
        ev("run.status", { status: "running" }),
        ev("message.user", { id: "u1", text: "改成竖屏", steer: true }),
        ev("steer.dropped", { ids: ["u1"], reason: "canceled" }),
        ev("run.status", { status: "canceled" }),
      ]),
    );
    expect(items.map((i) => i.type)).toEqual(["user", "user", "status", "run-feedback"]);
    expect(user(items, 1)).toMatchObject({ queued: "missed", resendable: true, steerId: "u1" });
  });

  test("没读到的插话只有在用户还没发新消息时才能「作为新消息发送」", () => {
    reset();
    const items = buildTimeline(
      fold([
        ev("message.user", { text: "拆分镜" }),
        ev("run.status", { status: "running" }),
        ev("message.user", { id: "u1", text: "改成竖屏", steer: true }),
        ev("steer.dropped", { ids: ["u1"], reason: "failed" }),
        ev("run.status", { status: "failed", error: "上游挂了" }),
        ev("message.user", { text: "改成竖屏" }, "r2"),
      ]),
    );
    const missed = items.find((i) => i.type === "user" && i.queued === "missed");
    expect(missed).toMatchObject({ resendable: false });
  });

  test("旧事件里没有 id 的插话照旧按事件顺序放，不排队", () => {
    reset();
    const items = buildTimeline(
      fold([
        ev("message.user", { text: "拆分镜" }),
        ev("run.status", { status: "running" }),
        ev("message.user", { text: "再暖一点", steer: true }),
        ev("message.done", { text: "好" }),
      ]),
    );
    expect(items.map((i) => i.type)).toEqual(["user", "user", "assistant"]);
    expect(user(items, 1)).toMatchObject({ steer: true });
    expect(user(items, 1).queued).toBeUndefined();
  });

  test("运行已经结束、却没收到 steer.read / steer.dropped（事件丢了）：不能一直排队，按没读到处理", () => {
    reset();
    const items = buildTimeline(
      fold([
        ev("message.user", { text: "拆分镜" }),
        ev("run.status", { status: "running" }),
        ev("message.user", { id: "u1", text: "改成竖屏", steer: true }),
        ev("run.status", { status: "failed", error: "上游挂了" }),
      ]),
    );
    expect(items.find((i) => i.type === "user" && i.steer)).toMatchObject({ queued: "missed" });
  });

  test("步数用尽暂停时排着的插话仍在排队：点「继续」才会读", () => {
    reset();
    const items = buildTimeline(
      fold([
        ev("message.user", { text: "拆分镜" }),
        ev("run.status", { status: "running" }),
        ev("message.user", { id: "u1", text: "用双剑", steer: true }),
        ev("run.status", { status: "step_limit" }),
      ]),
    );
    expect(items.find((i) => i.type === "user" && i.steer)).toMatchObject({
      queued: "waiting",
      runId: "r1",
    });
  });
});

describe("压缩提示", () => {
  const compaction = (items: TimelineItem[]) =>
    items.filter(
      (i): i is Extract<TimelineItem, { type: "compaction" }> => i.type === "compaction",
    );

  test("压缩完成：在发生的位置留一条分隔行，压缩后的占用等之后第一条用量到了才补上", () => {
    reset();
    const base = [
      ev("message.user", { text: "重画分镜" }),
      ev("run.status", { status: "running" }),
      ev("tool.start", { id: "c1", name: "canvas_get_state" }),
      ev("tool.end", { id: "c1", name: "canvas_get_state", summary: "读取" }),
      ev("context.compacted", { before: 82_000 }),
    ];
    const waiting = buildTimeline(fold(base));
    expect(waiting.map((i) => i.type)).toEqual(["user", "activity", "compaction"]);
    expect(compaction(waiting)[0]).toMatchObject({ failed: false, before: 82_000, after: null });

    const done = buildTimeline(
      fold([...base, ev("run.usage", { spent_credits: 3, context_tokens: 31_000 })]),
    );
    expect(compaction(done)[0]).toMatchObject({ before: 82_000, after: 31_000 });
  });

  test("压缩失败：标 failed，没有压缩后的占用", () => {
    reset();
    const items = buildTimeline(
      fold([
        ev("message.user", { text: "重画分镜" }),
        ev("run.status", { status: "running" }),
        ev("context.compacted", { failed: true }),
        ev("run.usage", { spent_credits: 3, context_tokens: 82_000 }),
      ]),
    );
    expect(compaction(items)[0]).toMatchObject({ failed: true, after: null });
  });

  test("一轮里压缩多次：每条各取自己之后的第一条用量", () => {
    reset();
    const items = buildTimeline(
      fold([
        ev("message.user", { text: "重画分镜" }),
        ev("run.status", { status: "running" }),
        ev("context.compacted", { before: 80_000 }),
        ev("run.usage", { context_tokens: 30_000 }),
        ev("run.usage", { context_tokens: 60_000 }),
        ev("context.compacted", { before: 85_000 }),
        ev("run.usage", { context_tokens: 35_000 }),
      ]),
    );
    expect(compaction(items).map((c) => [c.before, c.after])).toEqual([
      [80_000, 30_000],
      [85_000, 35_000],
    ]);
  });

  test("状态行：压缩中说在整理较早的对话，压缩结束换回思考中；等你回答时仍是等你回答", () => {
    reset();
    const running = [
      ev("run.status", { status: "queued" }),
      ev("run.status", { status: "running" }),
    ];
    const compacting = fold([...running, ev("context.compacting", {}, "r1", 0)]);
    expect(buildStatusLine(compacting)).toMatchObject({
      text: "正在整理较早的对话…",
      waiting: false,
    });
    // 计时不因为换文案而重来
    expect(buildStatusLine(compacting)?.since).toBe(Date.parse(at(1)));
    const after = applyAgentEvent(compacting, ev("context.compacted", { before: 80_000 }));
    expect(buildStatusLine(after)?.text).toBe("思考中…");
  });
});

/** 取活动块（断言用） */
function activity(item: TimelineItem | undefined) {
  if (item?.type !== "activity") throw new Error("不是活动块");
  return item;
}
