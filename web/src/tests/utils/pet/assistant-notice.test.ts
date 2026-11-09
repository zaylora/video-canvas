import { describe, expect, test } from "bun:test";

import { approvalBubble, runEndBubble, runEndReaction } from "@/utils/pet/assistant-notice";

describe("approvalBubble：等你决定的气泡", () => {
  test("审批：不自动消失、最高优先级、waiting 动作，id 按审批编号", () => {
    const b = approvalBubble("a1", "generate");
    expect(b).toMatchObject({
      id: "assistant:approval:a1",
      source: "assistant",
      priority: "approval",
      mood: "waiting",
      ttl: null,
      text: "这一步要你点个头",
    });
  });

  test("提问换一句话", () => {
    expect(approvalBubble("a2", "ask").text).toBe("有个问题想问问你");
  });
});

describe("runEndBubble：一轮结束的气泡", () => {
  test("成功：「好啦，回复写完了」，jumping，4 秒", () => {
    expect(runEndBubble("r1", "succeeded", "")).toMatchObject({
      id: "assistant:done:r1",
      source: "assistant",
      priority: "result",
      mood: "jumping",
      ttl: 4000,
      text: "好啦，回复写完了",
    });
  });

  test("失败：带原因，failed，6 秒，优先级是错误", () => {
    expect(runEndBubble("r2", "failed", "模型超时")).toMatchObject({
      priority: "error",
      mood: "failed",
      ttl: 6000,
      text: "卡住了：模型超时",
    });
  });

  test("失败但没有原因：按状态给默认说法", () => {
    expect(runEndBubble("r3", "failed", "")!.text).toBe("卡住了，这一轮没跑完");
    expect(runEndBubble("r4", "timeout", "")!.text).toBe("卡住了：运行超时");
    expect(runEndBubble("r5", "step_limit", "")!.text).toBe("卡住了：达到步数上限");
  });

  test("用户自己停掉的不冒；还在进行中的不冒", () => {
    expect(runEndBubble("r6", "canceled", "")).toBeNull();
    expect(runEndBubble("r7", "running", "")).toBeNull();
    expect(runEndBubble("r8", undefined, "")).toBeNull();
  });
});

describe("runEndReaction：浮窗打开时一轮结束，输入框上沿的小镜播一次反应", () => {
  test("成功庆祝，失败沮丧", () => {
    expect(runEndReaction("succeeded")).toBe("celebrate");
    expect(runEndReaction("failed")).toBe("failed");
    expect(runEndReaction("timeout")).toBe("failed");
  });

  test("用户自己停掉的、还没结束的不播", () => {
    expect(runEndReaction("canceled")).toBeNull();
    expect(runEndReaction(undefined)).toBeNull();
    expect(runEndReaction("running")).toBeNull();
  });
});
