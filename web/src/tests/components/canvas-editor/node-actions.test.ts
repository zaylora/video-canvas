import { describe, expect, test } from "bun:test";

import {
  hasNodeActions,
  registerNodeActions,
  submitNodeById,
  submitVoiceById,
  type NodeActions,
} from "@/components/canvas-editor/node-actions";

const actions = (
  blocked: string | null,
  onSubmit: () => void = () => {},
  failure: string | null = null,
): NodeActions => ({
  blockedReason: () => blocked,
  submit: async () => {
    onSubmit();
    return failure;
  },
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

  test("提交本身失败（积分不足等）：直接返回失败原因，调用方不用事后读节点状态去猜", async () => {
    const off = registerNodeActions("n-fail", () => actions(null, () => {}, "积分不足"));
    expect(await submitNodeById("n-fail")).toBe("积分不足");
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

describe("按节点 ID 生成音色 submitVoiceById", () => {
  test("节点没有音色动作（不是角色卡）：返回原因，不报错", async () => {
    const off = registerNodeActions("v-none", () => actions(null));
    expect(await submitVoiceById("v-none")).toBe("这个节点不能生成音色");
    off();
  });

  test("有音色动作：先看能否提交，能就提交", async () => {
    let voiced = 0;
    let blocked: string | null = "先写音色描述";
    const off = registerNodeActions("v-ok", () => ({
      ...actions(null),
      voice: {
        blockedReason: () => blocked,
        submit: async () => {
          voiced += 1;
          return null;
        },
      },
    }));
    expect(await submitVoiceById("v-ok")).toBe("先写音色描述");
    expect(voiced).toBe(0);
    blocked = null;
    expect(await submitVoiceById("v-ok")).toBeNull();
    expect(voiced).toBe(1);
    off();
  });
});
