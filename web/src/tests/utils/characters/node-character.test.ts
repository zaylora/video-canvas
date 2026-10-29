import { describe, expect, test } from "bun:test";

import type { CanvasNodeData, NodeCharacter } from "@/types";
import {
  addExtra,
  applyMainImage,
  applyPickedAssets,
  applyVoiceAudio,
  characterDisplayName,
  characterRefsOfNode,
  DEFAULT_LOOK,
  emptyCharacter,
  MAX_EXTRAS,
  newCharacterData,
  normalizeLegacyCharacters,
  renameCharacter,
  roleNames,
  takeCharacterFile,
} from "@/utils/characters/node-character";

const character = (patch: Partial<NodeCharacter> = {}): NodeCharacter => ({
  name: "林晚",
  look: "夜行装",
  description: "",
  extras: [],
  voice: null,
  ...patch,
});

const data = (patch: Partial<CanvasNodeData> = {}): CanvasNodeData => ({
  kind: "character",
  label: "林晚",
  assetId: "10",
  src: "/a/10.png",
  character: character(),
  ...patch,
});

describe("emptyCharacter：空角色卡节点的数据", () => {
  test("形象名默认「基础形象」，没有图、没有音色", () => {
    expect(emptyCharacter("未命名角色 3")).toEqual({
      name: "未命名角色 3",
      look: DEFAULT_LOOK,
      description: "",
      extras: [],
      voice: null,
    });
    expect(DEFAULT_LOOK).toBe("基础形象");
  });
});

describe("characterRefsOfNode：节点自带的数据 → 绑定要用的素材引用", () => {
  test("主图在前、补充图依次在后、音频音色带入参考音频，id 取节点 id", () => {
    const refs = characterRefsOfNode(
      "n1",
      data({
        character: character({
          extras: [
            { assetId: "11", src: "/a/11.png" },
            { assetId: "12", src: "/a/12.png" },
          ],
          voice: { mode: "audio", assetId: "20", src: "/a/20.mp3", origin: "upload" },
        }),
      }),
    );
    expect(refs).toEqual({
      id: "n1",
      name: "林晚",
      avatarAssetId: "10",
      extraAssetIds: ["11", "12"],
      voiceAssetId: "20",
    });
  });

  test("没有主图（空节点）：不带入任何参考，返回 null，不算失效", () => {
    expect(characterRefsOfNode("n1", data({ assetId: undefined, src: undefined }))).toBeNull();
    // 只有补充图也不带：一个形象必须先有主图
    expect(
      characterRefsOfNode(
        "n1",
        data({
          assetId: undefined,
          character: character({ extras: [{ assetId: "11", src: "/a/11.png" }] }),
        }),
      ),
    ).toBeNull();
  });

  test("正在生成：assetId 还指着上一版，不能当参考图", () => {
    expect(characterRefsOfNode("n1", data({ status: "running" }))).toBeNull();
  });

  test("没有声音：voiceAssetId 为 null", () => {
    expect(characterRefsOfNode("n1", data())?.voiceAssetId).toBeNull();
  });

  test("没有 character 元数据（异常数据）：返回 null", () => {
    expect(characterRefsOfNode("n1", data({ character: undefined }))).toBeNull();
  });
});

describe("characterDisplayName：节点和 @ 候选里的显示名", () => {
  test("「角色名 · 形象名」；形象名为空只显示角色名", () => {
    expect(characterDisplayName(data())).toBe("林晚 · 夜行装");
    expect(characterDisplayName(data({ character: character({ look: "" }) }))).toBe("林晚");
  });

  test("没有 character 时退回节点标题", () => {
    expect(characterDisplayName(data({ character: undefined, label: "未命名" }))).toBe("未命名");
  });
});

describe("normalizeLegacyCharacters：旧画布里引用全局角色卡的节点", () => {
  test("有 characterId、没有 character：变成空角色节点，保留 labelCache 的名字，去掉旧字段", () => {
    const old: CanvasNodeData = {
      kind: "character",
      label: "小明",
      characterId: "c1",
      labelCache: { name: "小明", coverAssetId: "9" },
    };
    const [out] = normalizeLegacyCharacters([
      { id: "n1", type: "canvas", position: { x: 0, y: 0 }, data: old },
    ]);
    expect(out.data.character).toEqual(emptyCharacter("小明"));
    expect(out.data).not.toHaveProperty("characterId");
    expect(out.data).not.toHaveProperty("labelCache");
    // 不迁移封面：labelCache.coverAssetId 不会变成主图
    expect(out.data.assetId).toBeUndefined();
  });

  test("短剧旧节点：名字和造型取 data.drama，已经生成的主图（assetId / src）保留", () => {
    const old: CanvasNodeData = {
      kind: "character",
      label: "林晚",
      characterId: "c2",
      labelCache: { name: "林晚（缓存）" },
      assetId: "55",
      src: "/a/55.png",
      status: "done",
      drama: { type: "character", name: "林晚", look: "夜行装", episodes: [3] },
    };
    const [out] = normalizeLegacyCharacters([
      { id: "n1", type: "canvas", position: { x: 0, y: 0 }, data: old },
    ]);
    expect(out.data.character?.name).toBe("林晚");
    expect(out.data.character?.look).toBe("夜行装");
    expect(out.data.assetId).toBe("55");
    expect(out.data.drama?.episodes).toEqual([3]);
  });

  test("没有任何名字可用：叫「未命名角色」", () => {
    const old: CanvasNodeData = { kind: "character", label: "", characterId: "c3" };
    const [out] = normalizeLegacyCharacters([
      { id: "n1", type: "canvas", position: { x: 0, y: 0 }, data: old },
    ]);
    expect(out.data.character?.name).toBe("未命名角色");
  });

  test("已经是新数据、其他种类的节点、分组：原样返回同一个对象（不触发多余的重渲染）", () => {
    const fresh = { id: "n1", type: "canvas" as const, position: { x: 0, y: 0 }, data: data() };
    const image = {
      id: "n2",
      type: "canvas" as const,
      position: { x: 0, y: 0 },
      data: { kind: "image", label: "图" } as CanvasNodeData,
    };
    const input = [fresh, image];
    const out = normalizeLegacyCharacters(input);
    expect(out[0]).toBe(fresh);
    expect(out[1]).toBe(image);
  });

  test("新数据里残留 characterId：去掉，保留 character", () => {
    const mixed = {
      id: "n1",
      type: "canvas" as const,
      position: { x: 0, y: 0 },
      data: data({ characterId: "c9" }),
    };
    const [out] = normalizeLegacyCharacters([mixed]);
    expect(out.data).not.toHaveProperty("characterId");
    expect(out.data.character?.name).toBe("林晚");
  });
});

describe("addExtra：补充图最多 4 张、去重、不含主图、不接受非图片", () => {
  const ref = (n: number) => ({ assetId: String(n), src: `/a/${n}.png` });

  test("正常追加", () => {
    const out = addExtra(character(), "10", ref(11));
    expect(out).toEqual({ ok: true, character: character({ extras: [ref(11)] }) });
  });

  test("已达 4 张：拒绝并说明原因", () => {
    const full = character({ extras: [ref(11), ref(12), ref(13), ref(14)] });
    expect(MAX_EXTRAS).toBe(4);
    expect(addExtra(full, "10", ref(15))).toEqual({ ok: false, reason: "补充图最多 4 张" });
  });

  test("已是主图 / 已添加过：拒绝", () => {
    expect(addExtra(character(), "10", ref(10))).toEqual({ ok: false, reason: "已是主图" });
    expect(addExtra(character({ extras: [ref(11)] }), "10", ref(11))).toEqual({
      ok: false,
      reason: "已添加过这张图",
    });
  });
});

describe("旧版文本音色：声音只剩音频，描述变成生成声音的提示词", () => {
  const withText = (text: string, extra: Partial<CanvasNodeData> = {}) => ({
    id: "n1",
    type: "canvas" as const,
    position: { x: 0, y: 0 },
    data: data({
      character: {
        ...character(),
        voice: { mode: "text", text },
      } as unknown as NodeCharacter,
      ...extra,
    }),
  });

  test("没有声音：voice 变成 null，描述搬进 voicePrompt，不丢内容", () => {
    const [out] = normalizeLegacyCharacters([withText("年轻女声，偏低")]);
    expect(out.data.character?.voice).toBeNull();
    expect(out.data.character?.voicePrompt).toBe("年轻女声，偏低");
  });

  test("已经有 voicePrompt：不覆盖", () => {
    const node = withText("旧描述");
    node.data.character = { ...node.data.character!, voicePrompt: "新写的" };
    const [out] = normalizeLegacyCharacters([node]);
    expect(out.data.character?.voicePrompt).toBe("新写的");
    expect(out.data.character?.voice).toBeNull();
  });

  test("空白描述：不写 voicePrompt", () => {
    const [out] = normalizeLegacyCharacters([withText("   ")]);
    expect(out.data.character?.voice).toBeNull();
    expect(out.data.character).not.toHaveProperty("voicePrompt");
  });

  test("音频音色原样保留，节点是同一个对象", () => {
    const audio = {
      mode: "audio" as const,
      assetId: "20",
      src: "/a/20.mp3",
      origin: "upload" as const,
    };
    const node = {
      id: "n1",
      type: "canvas" as const,
      position: { x: 0, y: 0 },
      data: data({ character: character({ voice: audio }) }),
    };
    expect(normalizeLegacyCharacters([node])[0]).toBe(node);
  });
});

describe("roleNames：「已有角色」下拉", () => {
  test("画布里角色卡节点的角色名，按出现顺序去重，忽略空名和别的节点", () => {
    const nodes = [
      { data: data({ character: character({ name: "林晚" }) }) },
      { data: data({ character: character({ name: "沈既白" }) }) },
      { data: data({ character: character({ name: "林晚", look: "礼服" }) }) },
      { data: data({ character: character({ name: " " }) }) },
      { data: { kind: "image", label: "图" } as CanvasNodeData },
    ];
    expect(roleNames(nodes)).toEqual(["林晚", "沈既白"]);
  });
});

describe("newCharacterData：加号菜单新建角色卡节点时的默认数据", () => {
  test("带空角色数据，名字按画布里已有节点名编号（未命名角色、未命名角色 2…），标题同名", () => {
    expect(newCharacterData([])).toEqual({
      label: "未命名角色",
      character: emptyCharacter("未命名角色"),
    });
    const second = newCharacterData(["图片", "未命名角色"]);
    expect(second.label).toBe("未命名角色 2");
    expect(second.character.name).toBe("未命名角色 2");
    expect(newCharacterData(["未命名角色", "未命名角色 2"]).label).toBe("未命名角色 3");
  });
});

describe("applyMainImage：选了 / 上传了一张图作为主图", () => {
  const asset = { id: "10", url: "/a/10.png", kind: "image" } as const;

  test("写进节点的 assetId / src / mediaType，状态复位；之前同一张图做补充图的从补充图里去掉", () => {
    const out = applyMainImage(
      data({
        assetId: undefined,
        src: undefined,
        status: "error",
        error: "x",
        character: character({
          extras: [
            { assetId: "10", src: "/a/10.png" },
            { assetId: "11", src: "/a/11.png" },
          ],
        }),
      }),
      asset,
    );
    expect(out).toMatchObject({
      assetId: "10",
      src: "/a/10.png",
      mediaType: "image",
      status: "idle",
    });
    expect(out.error).toBeUndefined();
    expect(out.character?.extras).toEqual([{ assetId: "11", src: "/a/11.png" }]);
  });

  test("新主图进历史并成为当前版本，原来的主图留在历史里可以切回去", () => {
    const out = applyMainImage(data({ assetId: "5", src: "/a/5.png" }), asset);
    expect(out.outputs?.map((item) => item.assetId)).toEqual(["5", "10"]);
    expect(out.activeOutputId).toBe("10");
    expect(out.src).toBe("/a/10.png");
  });

  test("已有多版历史时追加一版；同一张图再选一次只切过去，不重复入列", () => {
    const outputs = [
      { id: "5", src: "/a/5.png", mediaType: "image" as const, assetId: "5", createdAt: 1 },
      { id: "10", src: "/a/10.png", mediaType: "image" as const, assetId: "10", createdAt: 2 },
    ];
    const again = applyMainImage(
      data({ assetId: "5", src: "/a/5.png", outputs, activeOutputId: "5" }),
      asset,
    );
    expect(again.outputs?.map((item) => item.id)).toEqual(["5", "10"]);
    expect(again.activeOutputId).toBe("10");
  });
});

describe("applyVoiceAudio：绑定音频音色", () => {
  test("写成音频音色并记录来源", () => {
    const out = applyVoiceAudio(
      character(),
      { id: "20", url: "/a/20.mp3", kind: "audio" },
      "generated",
    );
    expect(out.voice).toEqual({
      mode: "audio",
      assetId: "20",
      src: "/a/20.mp3",
      origin: "generated",
    });
  });
});

describe("takeCharacterFile：角色卡只收图片（主图 / 补充图）或音频（音色），不合规给一句话", () => {
  const file = (type: string, size = 1024) => ({ type, size }) as File;

  test("对的种类放行", () => {
    expect(takeCharacterFile(file("image/png"), "image")).toEqual({ ok: true });
    expect(takeCharacterFile(file("audio/mpeg"), "audio")).toEqual({ ok: true });
  });

  test("种类不对：说清只能传什么，不让视频混进来", () => {
    expect(takeCharacterFile(file("audio/mpeg"), "image")).toEqual({
      ok: false,
      reason: "角色形象只能上传图片",
    });
    expect(takeCharacterFile(file("image/png"), "audio")).toEqual({
      ok: false,
      reason: "音色只能上传音频",
    });
    expect(takeCharacterFile(file("video/mp4"), "image").ok).toBe(false);
  });

  test("超过大小上限：沿用画布上传的上限和说法", () => {
    const big = takeCharacterFile(file("image/png", 21 * 1024 * 1024), "image");
    expect(big.ok).toBe(false);
    expect(big.ok === false && big.reason).toContain("20MB");
  });

  test("三类之外的文件", () => {
    expect(takeCharacterFile(file("application/pdf"), "image").ok).toBe(false);
  });
});

describe("applyPickedAssets：素材库 / 上传选好素材后写回节点", () => {
  const a = (id: string, kind: "image" | "audio" = "image") =>
    ({ id, url: `/a/${id}`, kind }) as never;

  test("主图：取第一张，落在节点自己的 assetId / src", () => {
    const out = applyPickedAssets(
      data({ assetId: undefined, src: undefined }),
      [a("7"), a("8")],
      "main",
    );
    expect(out.warnings).toEqual([]);
    expect(out.patch).toMatchObject({ assetId: "7", src: "/a/7", mediaType: "image" });
  });

  test("补充图：一次选多张按顺序加；已是主图、重复、超出 4 张的跳过并说明原因", () => {
    const base = data({
      assetId: "10",
      character: character({
        extras: [
          { assetId: "11", src: "/a/11" },
          { assetId: "12", src: "/a/12" },
        ],
      }),
    });
    const out = applyPickedAssets(base, [a("10"), a("11"), a("13"), a("14"), a("15")], "extra");
    expect(out.patch.character?.extras.map((x) => x.assetId)).toEqual(["11", "12", "13", "14"]);
    expect(out.warnings).toEqual(["已是主图", "已添加过这张图", "补充图最多 4 张"]);
  });

  test("补充图：全部被拒时 patch 为空（节点不变）", () => {
    const full = data({
      character: character({
        extras: ["1", "2", "3", "4"].map((id) => ({ assetId: id, src: `/a/${id}` })),
      }),
    });
    const out = applyPickedAssets(full, [a("5")], "extra");
    expect(out.patch).toEqual({});
    expect(out.warnings).toEqual(["补充图最多 4 张"]);
  });

  test("音色：绑定成音频音色，带来源", () => {
    const out = applyPickedAssets(data(), [a("20", "audio")], "voice", "upload");
    expect(out.patch.character?.voice).toEqual({
      mode: "audio",
      assetId: "20",
      src: "/a/20",
      origin: "upload",
    });
  });

  test("没选到东西：什么也不改", () => {
    expect(applyPickedAssets(data(), [], "main")).toEqual({ patch: {}, warnings: [] });
  });
});

describe("renameCharacter：改角色名", () => {
  test("节点标题还是原来的角色名（没手动改过）时一起跟着改", () => {
    const out = renameCharacter(data({ label: "林晚" }), "沈既白");
    expect(out.character?.name).toBe("沈既白");
    expect(out.label).toBe("沈既白");
  });

  test("标题是用户自己起的名字：只改角色名，不动标题", () => {
    const out = renameCharacter(data({ label: "女主 · 夜景" }), "沈既白");
    expect(out.character?.name).toBe("沈既白");
    expect(out.label).toBe("女主 · 夜景");
  });

  test("名字超过上限按字符截断，保留首尾以外的内容不动", () => {
    const out = renameCharacter(data(), "字".repeat(80));
    expect([...(out.character?.name ?? "")]).toHaveLength(64);
  });
});
