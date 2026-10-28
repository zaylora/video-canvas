import { describe, expect, test } from "bun:test";

import type { AssetDto } from "@/api/asset/type";
import type { CharacterDto } from "@/api/character/type";
import {
  addExtras,
  assetReason,
  emptyForm,
  formFromCharacter,
  pickAvatar,
  mapSaveError,
  removeExtra,
  setMain,
  toInput,
  validateForm,
  type EditorForm,
} from "@/utils/characters/editor-form";
import { ApiError } from "@/utils/requests/request";

const asset = (id: string, kind: AssetDto["kind"] = "image"): AssetDto => ({
  id,
  kind,
  url: `/f/${id}`,
  mimeType: "",
  byteSize: 1,
  width: null,
  height: null,
  durationMs: null,
  fileName: `f${id}`,
});

const form = (patch: Partial<EditorForm> = {}): EditorForm => ({
  ...emptyForm(),
  name: "林晚",
  avatar: asset("1"),
  ...patch,
});

const ids = (assets: AssetDto[]) => assets.map((a) => a.id);

describe("validateForm：保存前的预校验", () => {
  test("名称去首尾空格后为空：名称框下提示；没选主图：主图槽提示", () => {
    expect(validateForm(form({ name: "   " })).name).toBe("请填写角色名称");
    expect(validateForm(form({ avatar: null })).avatar).toBe("请选择一张形象主图");
    expect(validateForm(form())).toEqual({});
  });

  test("名称超过 64 字、设定超过 500 字都拒绝（按字符数，不切坏 emoji）", () => {
    expect(validateForm(form({ name: "字".repeat(65) })).name).toBe("名称不能超过 64 个字");
    expect(validateForm(form({ name: "字".repeat(64) })).name).toBeUndefined();
    expect(validateForm(form({ name: "😀".repeat(64) })).name).toBeUndefined();
    expect(validateForm(form({ description: "x".repeat(501) })).description).toBe(
      "角色设定不能超过 500 个字",
    );
  });
});

describe("主图与补充图的编辑操作", () => {
  test("设为主图：补充图与主图互换位置；没有主图时直接提升", () => {
    const f = form({ extras: [asset("2"), asset("3")] });
    const swapped = setMain(f, 1);
    expect(swapped.avatar?.id).toBe("3");
    expect(ids(swapped.extras)).toEqual(["2", "1"]);
    const noAvatar = setMain(form({ avatar: null, extras: [asset("2"), asset("3")] }), 0);
    expect(noAvatar.avatar?.id).toBe("2");
    expect(ids(noAvatar.extras)).toEqual(["3"]);
  });

  test("移除补充图；越界下标不动", () => {
    const f = form({ extras: [asset("2"), asset("3")] });
    expect(ids(removeExtra(f, 0).extras)).toEqual(["3"]);
    expect(removeExtra(f, 9)).toBe(f);
  });

  test("更换主图：从补充图里选，原主图退回补充图第一位；选新图则直接替换", () => {
    const f = form({ extras: [asset("2"), asset("3")] });
    const fromExtra = pickAvatar(f, asset("3"));
    expect(fromExtra.avatar?.id).toBe("3");
    expect(ids(fromExtra.extras)).toEqual(["1", "2"]);
    const fresh = pickAvatar(f, asset("7"));
    expect(fresh.avatar?.id).toBe("7");
    expect(ids(fresh.extras)).toEqual(["2", "3"]);
  });

  test("添加补充图：去重、不含主图、最多 4 张，超出的丢弃", () => {
    const f = form({ extras: [asset("2")] });
    const next = addExtras(f, [
      asset("1"),
      asset("2"),
      asset("3"),
      asset("4"),
      asset("5"),
      asset("6"),
    ]);
    expect(ids(next.extras)).toEqual(["2", "3", "4", "5"]);
  });

  test("assetReason：选素材时已用的灰掉并标原因，不隐藏", () => {
    const f = form({ extras: [asset("2")], voice: asset("9", "audio") });
    expect(assetReason(f, "extra", asset("1"))).toBe("已是主图");
    expect(assetReason(f, "extra", asset("2"))).toBe("已添加");
    expect(assetReason(f, "extra", asset("3"))).toBeNull();
    // 主图可以从补充图里换：不禁用，只是标注
    expect(assetReason(f, "avatar", asset("2"))).toBeNull();
    expect(assetReason(f, "avatar", asset("1"))).toBe("当前主图");
    expect(assetReason(f, "voice", asset("9", "audio"))).toBeNull();
  });
});

describe("表单与请求体", () => {
  const card: CharacterDto = {
    id: "c1",
    name: "林晚",
    description: "短发",
    avatar: asset("1"),
    extras: [asset("2")],
    voice: asset("9", "audio"),
    createdAt: "",
    updatedAt: "",
  };

  test("formFromCharacter / toInput：名称去空格，素材转 id，没有声音是 null", () => {
    const f = formFromCharacter({ ...card, voice: null });
    expect(toInput({ ...f, name: "  林晚 " })).toEqual({
      name: "林晚",
      description: "短发",
      avatarAssetId: "1",
      extraAssetIds: ["2"],
      voiceAssetId: null,
    });
    expect(toInput(formFromCharacter(card)).voiceAssetId).toBe("9");
  });

  test("主图素材已不可用（avatar 为 null）的卡编辑时主图槽是空的，必须重选才能保存", () => {
    const f = formFromCharacter({ ...card, avatar: null });
    expect(validateForm(f).avatar).toBeDefined();
  });
});

describe("mapSaveError：业务错误按契约 §4 摆到该摆的地方", () => {
  const err = (code: number, message: string) => new ApiError(message, code, 400);

  test("名称不再唯一：66001 不再有名称框下的特殊提示，按普通业务错误走横幅", () => {
    expect(mapSaveError(err(66001, "已有同名角色卡")).kind).toBe("banner");
  });

  test("66002 / 66003 / 66005 在弹框底部横幅，文案用后端给的", () => {
    expect(mapSaveError(err(66002, "角色卡数量已达上限"))).toEqual({
      kind: "banner",
      message: "角色卡数量已达上限",
    });
    expect(mapSaveError(err(66003, "引用的素材不可用：主图不存在")).kind).toBe("banner");
    expect(mapSaveError(err(66005, "名称过长")).kind).toBe("banner");
  });

  test("66004 角色卡不存在：编辑时被别处删了，要关弹框并刷新列表", () => {
    expect(mapSaveError(err(66004, "角色卡不存在")).kind).toBe("gone");
  });

  test("网络错误等未知错误同样给横幅（请求已静默，不能吞掉）", () => {
    expect(mapSaveError(new ApiError("网络异常", "NETWORK_ERROR", 0))).toEqual({
      kind: "banner",
      message: "网络异常",
    });
    expect(mapSaveError(new Error("boom")).kind).toBe("banner");
  });
});
