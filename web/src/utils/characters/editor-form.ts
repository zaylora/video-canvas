import type { AssetDto } from "@/api/asset/type";
import { CHARACTER_ERR } from "@/api/character/errors";
import type { CharacterDto, CharacterInput } from "@/api/character/type";
import { ApiError } from "@/utils/requests/request";

/** 名称最多多少字（契约 §3） */
export const NAME_MAX = 64;
/** 角色设定最多多少字 */
export const DESC_MAX = 500;
/** 补充图最多几张 */
export const EXTRA_MAX = 4;

/** 编辑弹框里的表单内容：素材存整个 AssetDto，槽位直接拿它画缩略图 */
export type EditorForm = {
  /** 名称，输入原样保存，提交时才去首尾空格 */
  name: string;
  /** 角色设定 */
  description: string;
  /** 主图；null 是还没选（或原主图素材已不可用） */
  avatar: AssetDto | null;
  /** 补充图，按展示顺序 */
  extras: AssetDto[];
  /** 声音（音频素材）；null 是没有 */
  voice: AssetDto | null;
};

/** 表单预校验的结果：有哪个字段的错就在哪个字段下显示 */
export type EditorErrors = {
  /** 名称框下的提示 */
  name?: string;
  /** 主图槽的提示（槽描红） */
  avatar?: string;
  /** 设定框下的提示 */
  description?: string;
};

/** 打开「选素材」时要选什么：决定按哪种素材过滤、已用素材怎么标 */
export type PickMode = "avatar" | "extra" | "voice";

/** 新建时的空表单 */
export const emptyForm = (): EditorForm => ({
  name: "",
  description: "",
  avatar: null,
  extras: [],
  voice: null,
});

/** 编辑已有角色卡时的初始表单 */
export const formFromCharacter = (card: CharacterDto): EditorForm => ({
  name: card.name,
  description: card.description,
  avatar: card.avatar,
  extras: [...card.extras],
  voice: card.voice,
});

/** 按字符数（不是 UTF-16 单元）算长度，emoji 不被算成两个字 */
const lengthOf = (text: string) => [...text].length;

/**
 * 保存前的预校验（后端同样校验，这里让错误就地出现、少一次请求）：
 * 名称去首尾空格后 1–64 字，设定 ≤ 500 字，主图必填。名称不要求唯一（短剧里同一角色有多个造型），前端不做重名校验。
 * @param form 表单内容
 * @returns 各字段的错误；空对象表示可以保存
 */
export function validateForm(form: EditorForm): EditorErrors {
  const errors: EditorErrors = {};
  const name = form.name.trim();
  if (!name) errors.name = "请填写角色名称";
  else if (lengthOf(name) > NAME_MAX) errors.name = `名称不能超过 ${NAME_MAX} 个字`;
  if (lengthOf(form.description) > DESC_MAX) {
    errors.description = `角色设定不能超过 ${DESC_MAX} 个字`;
  }
  if (!form.avatar) errors.avatar = "请选择一张形象主图";
  return errors;
}

/**
 * 把第 index 张补充图设为主图：和原主图互换位置；原本没有主图时直接提升（补充图少一张）。
 * @param form 表单内容
 * @param index 补充图下标
 */
export function setMain(form: EditorForm, index: number): EditorForm {
  const next = form.extras[index];
  if (!next) return form;
  const extras = [...form.extras];
  if (form.avatar) extras[index] = form.avatar;
  else extras.splice(index, 1);
  return { ...form, avatar: next, extras };
}

/**
 * 移除一张补充图；下标越界原样返回。
 * @param form 表单内容
 * @param index 补充图下标
 */
export function removeExtra(form: EditorForm, index: number): EditorForm {
  if (index < 0 || index >= form.extras.length) return form;
  return { ...form, extras: form.extras.filter((_, i) => i !== index) };
}

/**
 * 更换主图：选的是现有补充图时，它从补充图里拿走，原主图退回补充图第一位；选的是新素材则直接替换。
 * @param form 表单内容
 * @param asset 选中的图片素材
 */
export function pickAvatar(form: EditorForm, asset: AssetDto): EditorForm {
  const fromExtras = form.extras.some((item) => item.id === asset.id);
  if (!fromExtras) return { ...form, avatar: asset };
  const rest = form.extras.filter((item) => item.id !== asset.id);
  return { ...form, avatar: asset, extras: form.avatar ? [form.avatar, ...rest] : rest };
}

/**
 * 追加补充图：去重、跳过主图、总数最多 4 张，超出的丢弃。
 * @param form 表单内容
 * @param assets 本次选中的图片素材，按勾选顺序
 */
export function addExtras(form: EditorForm, assets: AssetDto[]): EditorForm {
  const extras = [...form.extras];
  for (const asset of assets) {
    if (extras.length >= EXTRA_MAX) break;
    if (asset.id === form.avatar?.id || extras.some((item) => item.id === asset.id)) continue;
    extras.push(asset);
  }
  return { ...form, extras };
}

/**
 * 选素材时，一个素材现在能不能选、不能选的原因：已用的灰掉并标注，不隐藏。
 * @param form 表单内容
 * @param mode 在选主图、补充图还是声音
 * @param asset 列表里的素材
 * @returns 标注文字；null 表示可以选
 */
export function assetReason(form: EditorForm, mode: PickMode, asset: AssetDto): string | null {
  if (mode === "extra") {
    if (asset.id === form.avatar?.id) return "已是主图";
    if (form.extras.some((item) => item.id === asset.id)) return "已添加";
  }
  if (mode === "avatar" && asset.id === form.avatar?.id) return "当前主图";
  return null;
}

/**
 * 表单转接口入参：名称去首尾空格，素材换成 id。调用前要先过 validateForm（主图必有）。
 * @param form 表单内容
 */
export function toInput(form: EditorForm): CharacterInput {
  return {
    name: form.name.trim(),
    description: form.description,
    avatarAssetId: form.avatar?.id ?? "",
    extraAssetIds: form.extras.map((item) => item.id),
    voiceAssetId: form.voice?.id ?? null,
  };
}

/** 保存失败该怎么摆：banner 是弹框底部横幅，gone 是角色卡已不存在（名称不再唯一，没有挂在字段下的失败） */
export type SaveFailure =
  | {
      /** 弹框底部横幅，已填内容保留 */
      kind: "banner";
      /** 提示文案 */
      message: string;
    }
  | {
      /** 编辑时角色卡已被别处删除：关弹框并刷新列表 */
      kind: "gone";
      /** 提示文案 */
      message: string;
    };

/**
 * 把保存接口的失败翻译成界面处理方式（契约 §4「前端处理」列）。
 * 保存请求是静默的（不弹全局 toast），所以任何失败都要在这里找到落点，不能吞掉。
 * @param error 请求抛出的错误
 */
export function mapSaveError(error: unknown): SaveFailure {
  if (error instanceof ApiError) {
    switch (error.code) {
      case CHARACTER_ERR.notFound:
        return { kind: "gone", message: error.message };
      default:
        return { kind: "banner", message: error.message };
    }
  }
  return { kind: "banner", message: error instanceof Error ? error.message : "保存失败，请重试" };
}
