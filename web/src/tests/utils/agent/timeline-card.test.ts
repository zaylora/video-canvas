import { describe, expect, test } from "bun:test";

import type { AgentEventDto } from "@/api/agent/type";
import { applyAgentEvent, emptySession } from "@/utils/agent/session-state";
import { buildTimeline } from "@/utils/agent/timeline";

let seq = 0;
const ev = (type: string, data: Record<string, unknown> = {}, run = "r1"): AgentEventDto => {
  seq++;
  return {
    session_id: "s1",
    canvas_id: "c1",
    run_id: run,
    seq,
    type,
    data,
    created_at: new Date(Date.UTC(2026, 10, 3, 12, 0, seq)).toISOString(),
  };
};
const fold = (events: AgentEventDto[]) => events.reduce(applyAgentEvent, emptySession());

const CARD = {
  kind: "summary",
  title: "有可复用的插件",
  tone: "ok",
  rows: [{ label: "插件", value: "New API 视频", ref: { type: "plugin", key: "new-api-video" } }],
};
const toolPair = (id: string, name: string, isError = false) => [
  ev("tool.start", { id, name }),
  ev("tool.end", { id, name, is_error: isError, summary: "x" }),
];

describe("card.shown：消息流里的独立卡片", () => {
  test("卡片是独立条目，位置在它出现的地方：前后的活动组和正文各在一边", () => {
    seq = 0;
    const items = buildTimeline(
      fold([
        ev("message.user", { text: "有没有可复用的插件" }),
        ev("run.status", { status: "running" }),
        ...toolPair("t1", "admin_list_plugins"),
        ...toolPair("c1", "show_card"),
        ev("card.shown", { id: "c1", card: CARD }),
        ev("message.done", { text: "可以直接用。" }),
        ev("run.status", { status: "succeeded" }),
      ]),
    );
    expect(items.map((i) => i.type)).toEqual([
      "user",
      "activity",
      "card",
      "assistant",
      "run-feedback",
    ]);
    const card = items[2];
    expect(card.type === "card" && card.card).toMatchObject({
      kind: "summary",
      title: "有可复用的插件",
    });
    expect(card.key).toBe("cc1");
  });

  test("show_card 成功的那次调用不在活动组里留一行；只有它时不出现空的活动组", () => {
    seq = 0;
    const items = buildTimeline(
      fold([
        ev("message.user", { text: "看看" }),
        ev("run.status", { status: "running" }),
        ...toolPair("c1", "show_card"),
        ev("card.shown", { id: "c1", card: CARD }),
        ev("run.status", { status: "succeeded" }),
      ]),
    );
    expect(items.map((i) => i.type)).toEqual(["user", "card", "run-feedback"]);
  });

  test("活动组夹着卡片时被卡片切成两组，卡片前后的调用各归各的组", () => {
    seq = 0;
    const items = buildTimeline(
      fold([
        ev("message.user", { text: "看看" }),
        ev("run.status", { status: "running" }),
        ...toolPair("t1", "admin_list_plugins"),
        ...toolPair("c1", "show_card"),
        ev("card.shown", { id: "c1", card: CARD }),
        ...toolPair("t2", "admin_list_models"),
        ev("run.status", { status: "succeeded" }),
      ]),
    );
    expect(items.map((i) => i.type)).toEqual([
      "user",
      "activity",
      "card",
      "activity",
      "run-feedback",
    ]);
    for (const i of [1, 3]) {
      const a = items[i];
      expect(a.type === "activity" && a.tools.map((t) => t.name)).not.toContain("show_card");
    }
  });

  test("show_card 失败的调用留在活动组里，没有卡片", () => {
    seq = 0;
    const items = buildTimeline(
      fold([
        ev("message.user", { text: "看看" }),
        ev("run.status", { status: "running" }),
        ...toolPair("c1", "show_card", true),
        ev("run.status", { status: "succeeded" }),
      ]),
    );
    expect(items.map((i) => i.type)).toEqual(["user", "activity", "run-feedback"]);
    const a = items[1];
    expect(a.type === "activity" && a.errors).toBe(1);
  });

  test("历史回放：重新折叠同一批事件得到同样的卡片；结构不对的卡不渲染", () => {
    seq = 0;
    const events = [
      ev("message.user", { text: "看看" }),
      ev("run.status", { status: "running" }),
      ev("card.shown", { id: "c1", card: CARD }),
      ev("card.shown", { id: "c2", card: { kind: "chart", title: "坏卡", rows: [] } }),
      ev("card.shown", { id: "c3", card: { ...CARD, title: "第三张" } }),
      ev("run.status", { status: "succeeded" }),
    ];
    const a = buildTimeline(fold(events));
    const b = buildTimeline(fold([...events]));
    expect(a).toEqual(b);
    expect(a.filter((i) => i.type === "card").map((i) => i.key)).toEqual(["cc1", "cc3"]);
  });
});
