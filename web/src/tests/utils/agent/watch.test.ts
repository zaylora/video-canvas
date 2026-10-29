import { describe, expect, test } from "bun:test";

import type { AgentEventDto, AgentWatchBatchDto } from "@/api/agent/type";
import { applyAgentEvent, applyHistoryPage, emptySession } from "@/utils/agent/session-state";
import { buildTimeline } from "@/utils/agent/timeline";
import {
  activeWatches,
  notificationFor,
  summarizeWatches,
  watchTitlePrefix,
} from "@/utils/agent/watch";
import { canvasPageTitle } from "@/utils/page-title";

const batch = (over: Partial<AgentWatchBatchDto> = {}): AgentWatchBatchDto => ({
  batch_id: "b1",
  state: "waiting",
  total: 2,
  settled: 0,
  tasks: [
    { task_id: "101", node_id: "n1", label: "镜头 1", status: "pending" },
    { task_id: "102", node_id: "n2", label: "镜头 2", status: "pending" },
  ],
  ...over,
});

const ev = (
  seq: number,
  type: string,
  data: Record<string, unknown> = {},
  run: string | null = "r1",
): AgentEventDto => ({
  session_id: "s1",
  canvas_id: "c1",
  run_id: run,
  seq,
  type,
  data,
  created_at: "2026-10-28T12:00:00Z",
});

describe("watch.updated：按批覆盖，已结束的批不再算进行中", () => {
  test("同一个 batch_id 后来的事件覆盖先前的", () => {
    let s = applyAgentEvent(emptySession(), ev(1, "watch.updated", batch()));
    s = applyAgentEvent(s, ev(2, "watch.updated", batch({ settled: 1 })));
    expect(Object.keys(s.watches)).toEqual(["b1"]);
    expect(s.watches.b1.settled).toBe(1);
  });

  test("回放补来的旧事件不会把新状态倒回去", () => {
    let s = applyAgentEvent(emptySession(), ev(2, "watch.updated", batch({ settled: 2 })));
    s = applyAgentEvent(s, ev(1, "watch.updated", batch({ settled: 0 })));
    expect(s.watches.b1.settled).toBe(2);
  });

  test("缺少 batch_id 的事件被忽略", () => {
    const s = applyAgentEvent(emptySession(), ev(1, "watch.updated", { state: "waiting" }));
    expect(s.watches).toEqual({});
  });

  test("activeWatches 只留 waiting / deliverable；delivered、canceled 不算", () => {
    let s = emptySession();
    s = applyAgentEvent(s, ev(1, "watch.updated", batch({ batch_id: "a", state: "waiting" })));
    s = applyAgentEvent(s, ev(2, "watch.updated", batch({ batch_id: "b", state: "deliverable" })));
    s = applyAgentEvent(s, ev(3, "watch.updated", batch({ batch_id: "c", state: "delivered" })));
    s = applyAgentEvent(s, ev(4, "watch.updated", batch({ batch_id: "d", state: "canceled" })));
    expect(activeWatches(s).map((b) => b.batch_id)).toEqual(["a", "b"]);
  });

  test("聊天记录页里的 watch.updated 也折进状态：以序号最大的为准", () => {
    const page = [
      ev(1, "watch.updated", batch()),
      ev(2, "watch.updated", batch({ state: "delivered", settled: 2 })),
    ];
    const s = applyHistoryPage(emptySession(), page, false);
    expect(s.watches.b1.state).toBe("delivered");
    expect(activeWatches(s)).toEqual([]);
  });
});

describe("监听条的聚合：多批合并，失败数、排队文案", () => {
  test("没有进行中的批：不显示", () => {
    expect(summarizeWatches([])).toBeNull();
  });

  test("等待中：总数、已结束数、失败数跨批相加", () => {
    const a = batch({
      batch_id: "a",
      settled: 1,
      tasks: [
        { task_id: "1", node_id: "n1", label: "镜头 1", status: "settled", task_status: "failed" },
        { task_id: "2", node_id: "n2", label: "镜头 2", status: "pending" },
      ],
    });
    const b = batch({
      batch_id: "b",
      total: 1,
      settled: 1,
      tasks: [
        {
          task_id: "3",
          node_id: "n3",
          label: "镜头 3",
          status: "settled",
          task_status: "succeeded",
        },
      ],
    });
    const s = summarizeWatches([a, b])!;
    expect([s.total, s.settled, s.failed]).toEqual([3, 2, 1]);
    expect(s.queued).toBe(false);
    expect(s.text).toBe("后台等待生成 · 2/3 已结束");
    expect(s.tasks.map((t) => t.label)).toEqual(["镜头 1", "镜头 2", "镜头 3"]);
  });

  test("有批已全部结束在排队（deliverable）：文案换成排队说明", () => {
    const s = summarizeWatches([batch({ state: "deliverable", settled: 2 })])!;
    expect(s.queued).toBe(true);
    expect(s.text).toBe("已全部结束 · 等当前运行结束后 Agent 继续");
  });

  test("任务状态文案：生成中 / 成功 / 失败（超时、取消、不存在都算失败）", () => {
    const tasks = (["succeeded", "failed", "expired", "canceled", "missing"] as const).map(
      (task_status, i) => ({
        task_id: String(i),
        node_id: `n${i}`,
        label: `镜头 ${i}`,
        status: "settled" as const,
        task_status,
      }),
    );
    const s = summarizeWatches([
      batch({
        total: 6,
        settled: 5,
        tasks: [...tasks, { ...tasks[0], task_id: "p", status: "pending", task_status: undefined }],
      }),
    ])!;
    expect(s.tasks.map((t) => t.statusText)).toEqual([
      "成功",
      "失败",
      "失败",
      "失败",
      "失败",
      "生成中",
    ]);
    expect(s.failed).toBe(4);
  });
});

describe("tasks.settled：时间线里的唤醒分隔条", () => {
  test("新开一轮和插进当前轮用不同文案", () => {
    let s = emptySession();
    s = applyAgentEvent(s, ev(1, "tasks.settled", { batch_ids: ["b1"], steer: false, tasks: [] }));
    s = applyAgentEvent(s, ev(2, "tasks.settled", { batch_ids: ["b2"], steer: true, tasks: [] }));
    const items = buildTimeline(s).filter((i) => i.type === "settled");
    expect(items.map((i) => i.type === "settled" && i.steer)).toEqual([false, true]);
    expect(items.map((i) => i.type === "settled" && i.text)).toEqual([
      "生成全部结束 · Agent 自动继续",
      "生成全部结束 · 插进这一轮",
    ]);
  });
});

describe("标签页标题前缀", () => {
  const waiting = summarizeWatches([batch({ settled: 1 })]);

  test("有待处理的审批：⏸ 待批准，优先于等待进度", () => {
    expect(watchTitlePrefix(true, waiting)).toBe("⏸ 待批准 · ");
  });

  test("只有进行中的监听：⏳ k/n", () => {
    expect(watchTitlePrefix(false, waiting)).toBe("⏳ 1/2 · ");
  });

  test("都没有：没有前缀；已全部结束在排队也不显示进度", () => {
    expect(watchTitlePrefix(false, null)).toBe("");
    const queued = summarizeWatches([batch({ state: "deliverable", settled: 2 })]);
    expect(watchTitlePrefix(false, queued)).toBe("");
  });

  test("拼到画布标题前面", () => {
    expect(canvasPageTitle("雨夜短片", "⏸ 待批准 · ")).toBe("⏸ 待批准 · 雨夜短片 - 连镜");
    expect(canvasPageTitle("雨夜短片")).toBe("雨夜短片 - 连镜");
  });
});

describe("浏览器通知：只对实时到达、页面在后台的两类事件", () => {
  const approval = ev(5, "approval.created", { id: "a1", kind: "generate", status: "pending" });
  const settled = (steer: boolean) =>
    ev(6, "tasks.settled", { batch_ids: ["b1"], steer, tasks: [] });

  test("待批准的生成：提醒去批准", () => {
    expect(notificationFor(approval, { canvasTitle: "雨夜短片", hidden: true })).toEqual({
      title: "雨夜短片：Agent 需要你批准一批生成",
    });
  });

  test("生成全部结束、新开一轮：提醒 Agent 正在验收；插进当前轮不通知", () => {
    expect(notificationFor(settled(false), { canvasTitle: "雨夜短片", hidden: true })).toEqual({
      title: "雨夜短片：生成已完成，Agent 正在验收",
    });
    expect(notificationFor(settled(true), { canvasTitle: "雨夜短片", hidden: true })).toBeNull();
  });

  test("页面在前台不通知", () => {
    expect(notificationFor(approval, { canvasTitle: "雨夜短片", hidden: false })).toBeNull();
    expect(notificationFor(settled(false), { canvasTitle: "雨夜短片", hidden: false })).toBeNull();
  });

  test("提问、删除审批和已决定的审批不通知；没起名用「未命名画布」", () => {
    const hidden = { canvasTitle: "", hidden: true };
    expect(
      notificationFor(
        ev(7, "approval.created", { id: "a2", kind: "ask", status: "pending" }),
        hidden,
      ),
    ).toBeNull();
    expect(
      notificationFor(
        ev(8, "approval.decided", { id: "a1", kind: "generate", status: "approved" }),
        hidden,
      ),
    ).toBeNull();
    expect(notificationFor(approval, hidden)?.title).toBe("未命名画布：Agent 需要你批准一批生成");
  });
});
