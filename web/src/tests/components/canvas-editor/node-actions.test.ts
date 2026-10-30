import { describe, expect, test } from "bun:test";

import {
  hasNodeActions,
  registerNodeActions,
  submitNodeById,
  submitWithVoiceById,
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

describe("按节点 ID 生成形象并在出图后生成音色 submitWithVoiceById", () => {
  test("没有登记的节点：形象和音色都没准备好", async () => {
    expect(await submitWithVoiceById("never-registered")).toEqual({
      image: "节点还没准备好",
      voice: "节点还没准备好",
    });
  });

  test("节点没有音色动作（不是角色卡）：照常生成形象，音色给原因", async () => {
    let submitted = 0;
    const off = registerNodeActions("v-none", () => actions(null, () => (submitted += 1)));
    expect(await submitWithVoiceById("v-none")).toEqual({
      image: null,
      voice: "这个节点不能生成音色",
    });
    expect(submitted).toBe(1);
    off();
  });

  test("音色此刻不能生成（没写音色描述）：照常生成形象，音色给原因", async () => {
    let submitted = 0;
    let voiced = 0;
    const off = registerNodeActions("v-blocked", () => ({
      ...actions(null, () => (submitted += 1)),
      voice: {
        blockedReason: () => "还没有音色描述",
        submitAfterImage: async () => {
          voiced += 1;
          return null;
        },
      },
    }));
    expect(await submitWithVoiceById("v-blocked")).toEqual({
      image: null,
      voice: "还没有音色描述",
    });
    expect([submitted, voiced]).toEqual([1, 0]);
    off();
  });

  test("都能提交：走「生成形象 + 出图后生成音色」一次提交，不另外再提交形象", async () => {
    let submitted = 0;
    let voiced = 0;
    const off = registerNodeActions("v-ok", () => ({
      ...actions(null, () => (submitted += 1)),
      voice: {
        blockedReason: () => null,
        submitAfterImage: async () => {
          voiced += 1;
          return null;
        },
      },
    }));
    expect(await submitWithVoiceById("v-ok")).toEqual({ image: null, voice: null });
    expect([submitted, voiced]).toEqual([0, 1]);
    off();
  });

  test("形象不能提交：两样都不提交，原因相同", async () => {
    let voiced = 0;
    const off = registerNodeActions("v-busy", () => ({
      ...actions("生成中"),
      voice: {
        blockedReason: () => null,
        submitAfterImage: async () => {
          voiced += 1;
          return null;
        },
      },
    }));
    expect(await submitWithVoiceById("v-busy")).toEqual({ image: "生成中", voice: "生成中" });
    expect(voiced).toBe(0);
    off();
  });

  test("提交失败（积分不足）：两样都算失败", async () => {
    const off = registerNodeActions("v-fail", () => ({
      ...actions(null),
      voice: { blockedReason: () => null, submitAfterImage: async () => "积分不足" },
    }));
    expect(await submitWithVoiceById("v-fail")).toEqual({ image: "积分不足", voice: "积分不足" });
    off();
  });
});
