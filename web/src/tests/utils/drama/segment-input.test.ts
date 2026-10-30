import { describe, expect, test } from "bun:test";

import type { Capabilities } from "@/api/model/type";
import type { NodeCharacter } from "@/types";
import type { RefAsset } from "@/utils/drama/segment-assets";
import { planSegmentInput } from "@/utils/drama/segment-input";

/** 角色卡节点自带的数据：补充图 2、音频音色 9（素材 id 在 payload 里是字符串） */
const roleData = (patch: Partial<NodeCharacter> = {}): NodeCharacter => ({
  name: "林川",
  look: "",
  description: "",
  extras: [{ assetId: "2", src: "https://cdn/2" }],
  voice: { mode: "audio", assetId: "9", src: "https://cdn/9", origin: "upload" },
  ...patch,
});

/** 角色卡节点资产：主图 1（节点自己的 assetId / src） */
const character = (id: string, patch: Partial<RefAsset> = {}): RefAsset => ({
  id,
  type: "character",
  name: "林川",
  look: "",
  label: "林川",
  episodes: [],
  kind: "character",
  src: "https://cdn/1",
  mediaType: "image",
  assetId: "1",
  character: roleData(),
  generating: false,
  ...patch,
});

const scene = (id: string, patch: Partial<RefAsset> = {}): RefAsset => ({
  id,
  type: "scene",
  name: "村口",
  look: "",
  label: "村口",
  episodes: [],
  kind: "image",
  src: "https://cdn/s",
  mediaType: "image",
  assetId: "5",
  generating: false,
  ...patch,
});

/** 全能参考视频模型：参考图 ≤ max、可选参考音频，时长 4–15 */
const video = (
  over: { ops?: Capabilities["ops"]; max?: number; audio?: boolean } = {},
): Capabilities => ({
  ops: over.ops ?? ["t2v", "i2v", "omni"],
  refs: {
    image: { on: true, max: over.max ?? 9, max_mb: 10 },
    audio: { on: over.audio ?? true, max: 3, max_mb: 10 },
    video: { on: false, max: 0, max_mb: 0 },
  },
  prompt: { max_length: 2000 },
  params: {
    duration: { type: "number", label: "时长", open: true, min: 4, max: 15, default: 5 },
  },
});

const base = {
  seconds: 12,
  ratio: "9:16",
  overrides: {},
};

describe("planSegmentInput：@ 解析与参考素材组装（契约 §13）", () => {
  const assets = [character("n1"), scene("n2")];

  test("角色卡展开成主图 + 补充图（参考图）和声音（参考音频），@ 在提示词里换成名字 / 图片编号", () => {
    const plan = planSegmentInput({
      ...base,
      caps: video(),
      prompt: "@[林川](n1)走进@[村口](n2)",
      assets,
    });
    expect(plan.op).toBe("omni");
    expect(plan.input.images).toEqual([5, 1, 2]);
    expect(plan.input.audios).toEqual([9]);
    expect(plan.input.prompt).toStartWith("林川（图片2中的人物）走进图片1");
    expect(plan.input.prompt).toContain("林川（图片2中的人物）说话时，音色参考音频1。");
    expect(plan.input.duration).toBe(12);
    expect(plan.errors).toEqual({});
    expect(plan.imageCount).toBe(3);
    expect(plan.imageMax).toBe(9);
    const linkedChar = plan.refs.find((ref) => ref.id === "n1");
    expect(linkedChar).toMatchObject({ state: "image", imageCount: 2, voice: "used" });
  });

  test("超出参考图上限按顺序截断，提示第几张起未带入，角色卡被截的部分标出来", () => {
    const plan = planSegmentInput({
      ...base,
      caps: video({ max: 2 }),
      prompt: "@[村口](n2)@[林川](n1)",
      assets,
    });
    expect(plan.input.images).toEqual([5, 1]);
    expect(plan.notices.join("")).toContain("参考图最多 2 张");
    expect(plan.refs.find((ref) => ref.id === "n1")).toMatchObject({
      imageCount: 1,
      truncated: true,
    });
  });

  test("模型不收参考音频：阻止提交，角色标记为声音被忽略", () => {
    const plan = planSegmentInput({
      ...base,
      caps: video({ audio: false }),
      prompt: "@[林川](n1)",
      assets,
    });
    expect(plan.input.audios).toBeUndefined();
    expect(plan.errors.audios).toBeTruthy();
    expect(plan.refs[0].voice).toBe("ignored");
  });

  test("角色没有声音：不带音频，也不提示", () => {
    const plan = planSegmentInput({
      ...base,
      caps: video(),
      prompt: "@[林川](n1)",
      assets: [character("n1", { character: roleData({ voice: null }) }), scene("n2")],
    });
    expect(plan.input.audios).toBeUndefined();
    expect(plan.refs[0].voice).toBe("none");
  });

  test("声音仍在生成时阻止片段提交，失败后可复用已经绑定的声音", () => {
    for (const voiceTaskStatus of ["running", "failed"]) {
      const plan = planSegmentInput({
        ...base,
        caps: video(),
        prompt: "@[林川](n1)说话",
        assets: [character("n1", { voiceTaskId: "101", voiceTaskStatus })],
      });
      expect(Boolean(plan.errors.audios)).toBe(voiceTaskStatus === "running");
      expect(plan.input.audios).toEqual([9]);
    }
  });

  test("没有图的资产只保留文字：空角色节点（没有形象图）、场景没有素材都不进参考图，也不算失效", () => {
    const plan = planSegmentInput({
      ...base,
      caps: video(),
      prompt: "@[林川](n1)在@[村口](n2)",
      assets: [
        character("n1", { src: null, assetId: undefined }),
        scene("n2", { src: null, assetId: undefined }),
      ],
    });
    expect(plan.refs.map((ref) => ref.state)).toEqual(["text", "text"]);
    expect(plan.input.images).toBeUndefined();
    expect(plan.input.prompt).toBe("林川在村口");
    // 一张图都没有且模型有文生视频：退回 t2v，不报缺参考素材
    expect(plan.op).toBe("t2v");
    expect(plan.errors.images).toBeTruthy();
  });

  test("引用的节点已被删：标成失效、记下名字，提示词退回成名字，不阻断", () => {
    const plan = planSegmentInput({
      ...base,
      caps: video(),
      prompt: "@[旧场景](gone)里@[村口](n2)",
      assets: [scene("n2")],
    });
    expect(plan.deadLabels).toEqual(["旧场景"]);
    expect(plan.refs[0]).toMatchObject({ id: "gone", label: "旧场景", state: "dead" });
    expect(plan.input.prompt).toBe("旧场景里图片1");
    expect(plan.errors).toEqual({});
  });

  test("角色节点正在生成形象：assetId 还指着上一版，不当参考图，只用文字", () => {
    const plan = planSegmentInput({
      ...base,
      caps: video(),
      prompt: "@[林川](n1)",
      assets: [character("n1", { generating: true })],
    });
    expect(plan.refs[0].state).toBe("text");
    expect(plan.input.images).toBeUndefined();
  });

  test("角色节点缺角色数据（异常数据）：按失效处理，记下名字，不阻断", () => {
    const plan = planSegmentInput({
      ...base,
      caps: video(),
      prompt: "@[林川](n1)",
      assets: [character("n1", { character: undefined })],
    });
    expect(plan.refs[0].state).toBe("dead");
    expect(plan.deadLabels).toEqual(["林川"]);
  });

  test("同一个资产引用多次只算一次，按第一次出现的顺序", () => {
    const plan = planSegmentInput({
      ...base,
      caps: video(),
      prompt: "@[村口](n2)@[林川](n1)@[村口](n2)",
      assets,
    });
    expect(plan.refs.map((ref) => ref.id)).toEqual(["n2", "n1"]);
    expect(plan.input.images).toEqual([5, 1, 2]);
  });
});

describe("planSegmentInput：时长与校验", () => {
  test("片段时长超出模型范围：按上限提交，并提示", () => {
    const plan = planSegmentInput({
      ...base,
      seconds: 20,
      caps: video(),
      prompt: "一个镜头",
      assets: [],
    });
    expect(plan.input.duration).toBe(15);
    expect(plan.notices.join("")).toContain("15");
  });

  test("提示词为空：报错并指向提示词", () => {
    const plan = planSegmentInput({
      ...base,
      caps: video(),
      prompt: "  ",
      assets: [],
    });
    expect(plan.errors.prompt).toBeTruthy();
  });

  test("只有全能参考的模型又没有任何参考图：报错要求至少一个参考素材", () => {
    const plan = planSegmentInput({
      ...base,
      caps: video({ ops: ["omni"] }),
      prompt: "一个镜头",
      assets: [],
    });
    expect(plan.op).toBe("omni");
    expect(plan.errors.images).toBeTruthy();
  });

  test("模型能力还没加载：不组装", () => {
    const plan = planSegmentInput({
      ...base,
      caps: undefined,
      prompt: "x",
      assets: [],
    });
    expect(plan.input).toEqual({});
  });
});
