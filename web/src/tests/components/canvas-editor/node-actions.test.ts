import { describe, expect, test } from "bun:test";

import {
  hasNodeActions,
  registerNodeActions,
  submitNodeById,
  type NodeActions,
} from "@/components/canvas-editor/node-actions";

const actions = (blocked: string | null, onSubmit: () => void = () => {}): NodeActions => ({
  blockedReason: () => blocked,
  submit: async () => onSubmit(),
});

describe("按节点 ID 触发生成 submitNodeById", () => {
  test("没有登记的节点：提示节点还没准备好", async () => {
    expect(await submitNodeById("never-registered")).toBe("节点还没准备好");
  });

  test("已登记且可以提交：发起提交并返回 null，不能被当成「没准备好」", async () => {
    let submitted = 0;
    const off = registerNodeActions("n-ready", () => actions(null, () => (submitted += 1)));
    expect(await submitNodeById("n-ready")).toBeNull();
    expect(submitted).toBe(1);
    off();
  });

  test("已登记但此刻不能提交：返回节点自己给的原因，且不提交", async () => {
    let submitted = 0;
    const off = registerNodeActions("n-blocked", () =>
      actions("请先写提示词", () => (submitted += 1)),
    );
    expect(await submitNodeById("n-blocked")).toBe("请先写提示词");
    expect(submitted).toBe(0);
    off();
  });

  test("读到的动作为空（如角色卡还没读到）：按没准备好处理，不报错", async () => {
    const off = registerNodeActions("n-empty", () => null);
    expect(await submitNodeById("n-empty")).toBe("节点还没准备好");
    off();
  });

  test("登记读的是最新的动作：节点重新渲染后原因跟着变", async () => {
    let current = actions("请先写提示词");
    const off = registerNodeActions("n-latest", () => current);
    expect(await submitNodeById("n-latest")).toBe("请先写提示词");
    current = actions(null);
    expect(await submitNodeById("n-latest")).toBeNull();
    off();
  });

  test("撤销登记后节点不可再触发", async () => {
    const off = registerNodeActions("n-off", () => actions(null));
    expect(hasNodeActions("n-off")).toBe(true);
    off();
    expect(hasNodeActions("n-off")).toBe(false);
    expect(await submitNodeById("n-off")).toBe("节点还没准备好");
  });
});
