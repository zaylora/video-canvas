import { describe, expect, test } from "bun:test";

import { withRunFooters } from "@/utils/agent/run-footer";
import type { TimelineItem } from "@/utils/agent/timeline";

const user = (key: string): TimelineItem => ({
  type: "user",
  key,
  text: key,
  steer: false,
  selection: [],
});
const say = (key: string, text: string): TimelineItem => ({
  type: "assistant",
  key,
  text,
  thinking: "",
  streaming: false,
});
const summary = (runId: string): TimelineItem => ({
  type: "run-summary",
  key: `rs${runId}`,
  runId,
  created: ["n1"],
  updated: [],
  deleted: [],
});
const feedback = (runId: string): TimelineItem => ({
  type: "run-feedback",
  key: `fb${runId}`,
  runId,
});

describe("withRunFooters：把一轮的改动摘要和反馈合成回答下面的一行底栏", () => {
  test("改动摘要 + 反馈合成一个 footer，复制取这一轮最后一段回答", () => {
    const out = withRunFooters([
      user("u1"),
      say("a1", "先读一下画布"),
      say("a2", "已建好清单"),
      summary("r1"),
      feedback("r1"),
    ]);
    expect(out.map((e) => e.key)).toEqual(["u1", "a1", "a2", "foot-r1"]);
    const foot = out[3];
    expect(foot.kind).toBe("footer");
    if (foot.kind !== "footer") return;
    expect(foot.runId).toBe("r1");
    expect(foot.summary?.created).toEqual(["n1"]);
    expect(foot.copyText).toBe("已建好清单");
  });

  test("被复制的那段回答标上 copied，它自己不再显示悬停复制", () => {
    const out = withRunFooters([user("u1"), say("a1", "x"), say("a2", "y"), feedback("r1")]);
    const flags = out.map((e) => (e.kind === "item" ? e.copied : null));
    expect(flags).toEqual([false, false, true, null]);
  });

  test("没改画布：只有反馈也成 footer，summary 为空", () => {
    const out = withRunFooters([user("u1"), say("a1", "好的"), feedback("r1")]);
    const foot = out.at(-1);
    expect(foot?.kind === "footer" && foot.summary).toBeUndefined();
  });

  test("这一轮没有回答文字（比如停止了）：没有复制内容", () => {
    const out = withRunFooters([user("u1"), feedback("r1"), user("u2"), say("a9", "别轮的")]);
    const foot = out[1];
    expect(foot.kind === "footer" && foot.copyText).toBe("");
  });

  test("回答只往回找到这一轮的用户消息为止，不跨轮", () => {
    const out = withRunFooters([say("a0", "上一轮"), user("u1"), feedback("r1")]);
    const foot = out.at(-1);
    expect(foot?.kind === "footer" && foot.copyText).toBe("");
  });

  test("只有改动摘要、反馈还没出现（理论上不会）：仍然合成 footer", () => {
    const out = withRunFooters([user("u1"), summary("r1")]);
    expect(out.at(-1)?.kind).toBe("footer");
  });
});
