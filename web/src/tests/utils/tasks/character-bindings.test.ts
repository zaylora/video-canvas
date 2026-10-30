import { describe, expect, test } from "bun:test";

import type { Capabilities } from "@/api/model/type";
import {
  acceptsSourceKind,
  buildTaskInput,
  hasImageRefs,
  imageLinksOf,
  opToAcceptSource,
  resolveBindings,
  type CharacterRefs,
  type IncomingLink,
} from "@/utils/tasks/capabilities";
import { buildFreshInput } from "@/utils/tasks/fresh-input";

/** 参考图最多 2 张、参考音频最多 1 条的视频模型 */
const video = (patch: Partial<Capabilities["refs"]> = {}): Capabilities => ({
  ops: ["t2v", "i2v", "omni"],
  refs: {
    image: { on: true, max: 2, max_mb: 10 },
    audio: { on: true, max: 1, max_mb: 15 },
    video: { on: false, max: 0, max_mb: 0 },
    ...patch,
  },
  prompt: { max_length: 5000 },
  params: {},
});

/** 不收参考音频的图生视频模型 */
const i2vNoAudio = (): Capabilities => ({
  ops: ["i2v"],
  refs: {
    image: { on: true, max: 4, max_mb: 10 },
    audio: { on: false, max: 0, max_mb: 0 },
    video: { on: false, max: 0, max_mb: 0 },
  },
  prompt: { max_length: 5000 },
  params: {},
});

describe("人物与音色对应：最终素材编号和必要参考校验", () => {
  test("手动素材先占容量，所有主图优先于补充图，重复音频复用最终编号", () => {
    const model = video({
      image: { on: true, max: 3, max_mb: 10 },
      audio: { on: true, max: 3, max_mb: 10 },
    });
    const links = [
      charLink(),
      charLink({
        edgeId: "e2",
        sourceId: "n2",
        character: refs({
          id: "c2",
          name: "林川",
          avatarAssetId: "5",
          extraAssetIds: [],
          voiceAssetId: "10",
        }),
      }),
    ];
    const built = buildFreshInput(
      model,
      { op: "omni", prompt: "@[林晚](n1)与@[林川](n2)对话", images: [100], audios: [99, 9] },
      links,
    );
    expect(built.errors).toEqual({});
    expect(built.input.images).toEqual([100, 1, 5]);
    expect(built.input.audios).toEqual([99, 9, 10]);
    expect(built.input.prompt).toContain("林晚（图片2中的人物）说话时，音色参考音频2。");
    expect(built.input.prompt).toContain("林川（图片3中的人物）说话时，音色参考音频3。");
    expect(built.input.prompt).not.toContain("补充视角");
  });

  test("声音超出名额必须报错，不能只给提示后继续", () => {
    const built = buildFreshInput(video(), { op: "omni", prompt: "对话" }, [
      charLink(),
      charLink({
        edgeId: "e2",
        sourceId: "n2",
        character: refs({ id: "c2", avatarAssetId: "5", extraAssetIds: [], voiceAssetId: "10" }),
      }),
    ]);
    expect(built.errors.audios).toBeTruthy();
  });

  test("声音正在生成时不沿用旧音色，图片生成不受声音任务影响", () => {
    const links = [charLink({ character: refs({ voicePending: true }) })];
    expect(buildFreshInput(video(), { op: "omni", prompt: "说话" }, links).errors.audios).toContain(
      "尚未就绪",
    );
    const image = { ...video(), ops: ["i2i" as const] };
    expect(buildFreshInput(image, { op: "i2i", prompt: "画像" }, links).errors).toEqual({});
  });

  test("参考图不足不能让前一个人的补充图挤掉后一个人的主图", () => {
    const built = buildFreshInput(video(), { op: "omni", prompt: "对话" }, [
      charLink(),
      charLink({
        edgeId: "e2",
        sourceId: "n2",
        character: refs({ id: "c2", avatarAssetId: "5", extraAssetIds: [], voiceAssetId: "9" }),
      }),
    ]);
    expect(built.input.images).toEqual([1, 5]);
    expect(built.errors).toEqual({});
  });

  test("模型不收音频、关闭声音生成和主图名额不足都会阻止提交", () => {
    expect(
      buildFreshInput(i2vNoAudio(), { op: "i2v", prompt: "说话" }, [charLink()]).errors.audios,
    ).toBeTruthy();
    const model = video();
    model.params = {
      generate_audio: { type: "boolean", label: "声音", open: false, default: false },
    };
    expect(
      buildFreshInput(model, { op: "omni", prompt: "说话", generate_audio: true }, [charLink()])
        .errors.generate_audio,
    ).toBeTruthy();
    expect(
      buildFreshInput(
        video({ image: { on: true, max: 1, max_mb: 10 } }),
        { op: "omni", prompt: "说话" },
        [imageLink("a", "100"), charLink()],
      ).errors.images,
    ).toBeTruthy();
  });

  test("最终提示词包含对应说明后再检查长度，原始提示词不回写", () => {
    const model = video();
    model.prompt.max_length = 20;
    const params = { op: "omni", prompt: "说话" };
    expect(buildFreshInput(model, params, [charLink()]).errors.prompt).toBeTruthy();
    expect(params.prompt).toBe("说话");
  });

  test("上游剧本里的角色引用也展开，绑定说明只追加一次", () => {
    const built = buildFreshInput(video(), { op: "omni", prompt: "电影感" }, [
      charLink(),
      {
        edgeId: "script",
        sourceId: "script",
        sourceKind: "script",
        sourceLabel: "剧本",
        targetHandle: null,
        text: "@[林晚](n1)说：你好。",
      },
    ]);
    expect(built.input.prompt).toContain("电影感\n\n林晚（图片1中的人物）说：你好。");
    expect(String(built.input.prompt).split("【人物音色对应】")).toHaveLength(2);
  });
});

const refs = (patch: Partial<CharacterRefs> = {}): CharacterRefs => ({
  id: "c1",
  name: "林晚",
  avatarAssetId: "1",
  extraAssetIds: ["2", "3"],
  voiceAssetId: "9",
  ...patch,
});

const charLink = (
  patch: Partial<IncomingLink> & { character?: CharacterRefs | null } = {},
): IncomingLink => ({
  edgeId: "e1",
  sourceId: "n1",
  sourceKind: "character",
  sourceLabel: "林晚",
  targetHandle: null,
  character: refs(),
  ...patch,
});

const imageLink = (edgeId: string, assetId: string): IncomingLink => ({
  edgeId,
  sourceId: `img-${edgeId}`,
  sourceKind: "image",
  sourceLabel: "图片",
  targetHandle: null,
  assetId,
});

const ids = (links: IncomingLink[]) => links.map((link) => link.assetId);

describe("resolveBindings：角色卡展开成参考图 + 参考音频", () => {
  test("主图在前、补充图随后，声音进 audios；虚拟连线记着来自哪张卡的哪一部分", () => {
    const bound = resolveBindings(video({ image: { on: true, max: 9, max_mb: 10 } }), "omni", [
      charLink(),
    ]);
    expect(ids(bound.images)).toEqual(["1", "2", "3"]);
    expect(bound.images.map((l) => l.fromCharacter)).toEqual([
      { id: "c1", part: "avatar", index: 0 },
      { id: "c1", part: "extra", index: 0 },
      { id: "c1", part: "extra", index: 1 },
    ]);
    expect(ids(bound.audios)).toEqual(["9"]);
    expect(bound.audios[0].fromCharacter).toEqual({ id: "c1", part: "voice", index: 0 });
    expect(bound.notices).toEqual([]);
  });

  test("多个角色卡各自展开后依次追加，和普通图片连线按连线顺序混排", () => {
    const bound = resolveBindings(video({ image: { on: true, max: 9, max_mb: 10 } }), "omni", [
      imageLink("a", "100"),
      charLink({
        character: refs({ avatarAssetId: "1", extraAssetIds: ["2"], voiceAssetId: null }),
      }),
      charLink({
        edgeId: "e2",
        sourceId: "n2",
        character: refs({ id: "c2", avatarAssetId: "5", extraAssetIds: [], voiceAssetId: null }),
      }),
    ]);
    expect(ids(bound.images)).toEqual(["100", "1", "5", "2"]);
  });

  test("超出参考图上限：按顺序截断，提示第 N 张起未带入；虚拟连线进 dropped", () => {
    const bound = resolveBindings(video(), "omni", [charLink()]);
    expect(ids(bound.images)).toEqual(["1", "2"]);
    expect(ids(bound.dropped)).toEqual(["3"]);
    expect(bound.notices).toEqual(["参考图最多 2 张，多余补充图未带入"]);
    // 截断后进任务输入的也只有前两张，不会触发「最多 N 个」校验错误
    const built = buildTaskInput(video(), { prompt: "镜头推近" }, bound, "omni", [charLink()]);
    expect(built.errors).toEqual({});
    expect(built.input.images).toEqual([1, 2]);
    expect(built.input.audios).toEqual([9]);
  });

  test("普通图片先占了名额，角色卡的图从剩下的名额里取", () => {
    const bound = resolveBindings(video(), "omni", [imageLink("a", "100"), charLink()]);
    expect(ids(bound.images)).toEqual(["100", "1"]);
    expect(ids(bound.dropped)).toEqual(["2", "3"]);
  });

  test("目标不收参考音频：声音丢弃并提示，形象照常带入", () => {
    const bound = resolveBindings(i2vNoAudio(), "i2v", [charLink()]);
    expect(ids(bound.images)).toEqual(["1", "2", "3"]);
    expect(bound.audios).toEqual([]);
    expect(ids(bound.ignored)).toEqual(["9"]);
    expect(bound.notices).toEqual(["当前模型不支持参考音频，角色卡声音未生效"]);
  });

  test("图片生成方式：角色卡的声音不参与，不进 ignored，也不提示", () => {
    const i2i: Capabilities = {
      ops: ["t2i", "i2i"],
      refs: {
        image: { on: true, max: 4, max_mb: 10 },
        audio: { on: false, max: 0, max_mb: 0 },
        video: { on: false, max: 0, max_mb: 0 },
      },
      prompt: { max_length: 100 },
      params: {},
    };
    const bound = resolveBindings(i2i, "i2i", [charLink()]);
    expect(ids(bound.images)).toEqual(["1", "2", "3"]);
    expect(bound.audios).toEqual([]);
    expect(bound.ignored).toEqual([]);
    expect(bound.notices).toEqual([]);
  });

  test("没有声音：不带音频，也没有提示", () => {
    const bound = resolveBindings(i2vNoAudio(), "i2v", [
      charLink({ character: refs({ voiceAssetId: null }) }),
    ]);
    expect(bound.audios).toEqual([]);
    expect(bound.ignored).toEqual([]);
    expect(bound.notices).toEqual([]);
  });

  test("多个角色的声音完整保留，容量不足由提交校验报错", () => {
    const bound = resolveBindings(video({ image: { on: true, max: 9, max_mb: 10 } }), "omni", [
      charLink({ character: refs({ extraAssetIds: [] }) }),
      charLink({
        edgeId: "e2",
        sourceId: "n2",
        character: refs({ id: "c2", avatarAssetId: "5", extraAssetIds: [], voiceAssetId: "10" }),
      }),
    ]);
    expect(ids(bound.audios)).toEqual(["9", "10"]);
    expect(ids(bound.dropped)).toEqual([]);
    expect(bound.notices).toEqual([]);
  });

  test("已失效（卡被删）或还在加载的角色卡不参与提交，其余连线照常", () => {
    const bound = resolveBindings(video(), "omni", [
      charLink({ character: null }),
      charLink({ edgeId: "e2", sourceId: "n2", character: undefined }),
      imageLink("a", "100"),
    ]);
    expect(ids(bound.images)).toEqual(["100"]);
    expect(bound.audios).toEqual([]);
    expect(bound.notices).toEqual([]);
  });

  test("当前生成方式不收图片（文生视频）：角色卡不绑定任何素材，也不报提示", () => {
    const bound = resolveBindings(video(), "t2v", [charLink()]);
    expect(bound.images).toEqual([]);
    expect(bound.audios).toEqual([]);
    expect(bound.notices).toEqual([]);
  });

  test("模型清单还没到：不展开也不提示", () => {
    const bound = resolveBindings(undefined, undefined, [charLink()]);
    expect(bound.images).toEqual([]);
    expect(bound.notices).toEqual([]);
  });
});

describe("角色卡与连线规则里的素材判断", () => {
  test("acceptsSourceKind：只要图片或音频有一种收就放行", () => {
    expect(acceptsSourceKind(video(), "omni", "character")).toBe(true);
    expect(acceptsSourceKind(video(), "t2v", "character")).toBe(false);
    expect(acceptsSourceKind(i2vNoAudio(), "i2v", "character")).toBe(true);
    expect(acceptsSourceKind(undefined, "t2v", "character")).toBe(true);
  });

  test("opToAcceptSource：文生视频接角色卡，优先切到图片和音频都收的方式", () => {
    expect(opToAcceptSource(video(), "t2v", "character")).toBe("omni");
    const caps: Capabilities = {
      ...video(),
      ops: ["t2v", "i2v", "omni"],
      refs: {
        image: { on: true, max: 2, max_mb: 10 },
        audio: { on: false, max: 0, max_mb: 0 },
        video: { on: false, max: 0, max_mb: 0 },
      },
    };
    // 音频关闭：omni 只收图片，和 i2v 一样；仍取全能参考
    expect(opToAcceptSource(caps, "t2v", "character")).toBe("omni");
    expect(opToAcceptSource(i2vNoAudio(), "t2v" as never, "character")).toBe("i2v");
  });

  test("imageLinksOf / hasImageRefs：有主图的角色卡算图片引用，失效的不算", () => {
    expect(hasImageRefs({}, { images: imageLinksOf([charLink()]) })).toBe(true);
    expect(hasImageRefs({}, { images: imageLinksOf([charLink({ character: null })]) })).toBe(false);
    expect(ids(imageLinksOf([imageLink("a", "100"), charLink()]))).toEqual(["100", "1"]);
  });
});

describe("提示词里 @ 角色卡：提交时展开成角色名（二期）", () => {
  const caps = () => video({ image: { on: true, max: 9, max_mb: 10 } });
  const prompt = "让 @[林晚](n1) 走向镜头";

  test("已连线：@ 换成角色名，形象和声音由连线带进 input", () => {
    const links = [charLink()];
    const bound = resolveBindings(caps(), "omni", links);
    const built = buildTaskInput(caps(), { prompt }, bound, "omni", links);
    expect(built.errors).toEqual({});
    expect(built.input.prompt).toStartWith("让 林晚（图片1中的人物） 走向镜头");
    expect(built.input.images).toEqual([1, 2, 3]);
    expect(built.input.audios).toEqual([9]);
  });

  test("角色卡改名后：展开成卡此刻的名字，不是 token 里写入时的旧名", () => {
    const links = [charLink({ character: refs({ name: "林晚晚" }) })];
    const built = buildTaskInput(
      caps(),
      { prompt },
      resolveBindings(caps(), "omni", links),
      "omni",
      links,
    );
    expect(built.input.prompt).toStartWith("让 林晚晚（图片1中的人物） 走向镜头");
  });

  test("节点已删（连线里没有它）：退回 token 里的名字，其余连线照常，不报错", () => {
    const links = [imageLink("a", "100")];
    const built = buildTaskInput(
      caps(),
      { prompt },
      resolveBindings(caps(), "omni", links),
      "omni",
      links,
    );
    expect(built.errors).toEqual({});
    expect(built.input.prompt).toBe("让 林晚 走向镜头");
    expect(built.input.images).toEqual([100]);
    expect(built.input.audios).toBeUndefined();
  });

  test("卡已失效（连线还在）：退回名字，不带形象和声音，不报错", () => {
    const links = [charLink({ character: null }), imageLink("a", "100")];
    const built = buildTaskInput(
      caps(),
      { prompt },
      resolveBindings(caps(), "omni", links),
      "omni",
      links,
    );
    expect(built.errors).toEqual({});
    expect(built.input.prompt).toBe("让 林晚 走向镜头");
    expect(built.input.images).toEqual([100]);
    expect(built.input.audios).toBeUndefined();
  });

  test("普通图片的 @ 行为不变，仍展开成编号", () => {
    const links = [imageLink("a", "100")];
    const built = buildTaskInput(
      caps(),
      { prompt: "把 @[图](img-a) 里的人放进去" },
      resolveBindings(caps(), "omni", links),
      "omni",
      links,
    );
    expect(built.input.prompt).toBe("把 图片1 里的人放进去");
  });
});
