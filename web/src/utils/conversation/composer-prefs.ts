import type { GenerateKind } from "@/api/conversation/type";

/** 输入卡片记在浏览器里的偏好：只是个人习惯，丢了不影响功能 */
export type ComposerPrefs = {
  /** 上次用的创作模式 */
  mode: GenerateKind;
  /** 每个模式上次选的模型 key */
  modelByMode: Partial<Record<GenerateKind, string>>;
  /** 每个模型上次用的参数（含生成方式、生成数量），按插入顺序保存 */
  paramsByModel: Record<string, Record<string, unknown>>;
};

/** localStorage 的键 */
const KEY = "vc.composer.prefs";

/** 最多记几个模型的参数，超出时丢最早的，避免长期使用后无限增长 */
const MAX_MODELS = 30;

const KINDS: GenerateKind[] = ["image", "video", "audio"];

/** 没存过偏好时的默认值：图片模式，不预设模型（取清单第一个）和参数（取模型默认值） */
export const DEFAULT_PREFS: ComposerPrefs = { mode: "image", modelByMode: {}, paramsByModel: {} };

const isRecord = (value: unknown): value is Record<string, unknown> =>
  typeof value === "object" && value !== null && !Array.isArray(value);

/** 读不到、不是合法 JSON、被改坏的内容都当没存过，不让一份坏数据挡住整个页面 */
const readStorage = (): unknown => {
  try {
    const raw = globalThis.localStorage?.getItem(KEY);
    return raw ? JSON.parse(raw) : null;
  } catch {
    return null;
  }
};

/**
 * 读取偏好。内容经过逐项校验，不认识的字段丢掉。
 * @returns 偏好；存储不可用（隐私模式）或内容损坏时是默认值
 */
export function loadPrefs(): ComposerPrefs {
  const raw = readStorage();
  if (!isRecord(raw)) return DEFAULT_PREFS;
  const mode = KINDS.find((kind) => kind === raw.mode) ?? DEFAULT_PREFS.mode;

  const modelByMode: ComposerPrefs["modelByMode"] = {};
  if (isRecord(raw.modelByMode)) {
    for (const kind of KINDS) {
      const value = raw.modelByMode[kind];
      if (typeof value === "string" && value) modelByMode[kind] = value;
    }
  }

  const paramsByModel: ComposerPrefs["paramsByModel"] = {};
  if (isRecord(raw.paramsByModel)) {
    for (const [key, value] of Object.entries(raw.paramsByModel)) {
      if (isRecord(value)) paramsByModel[key] = value;
    }
  }
  return { mode, modelByMode, paramsByModel };
}

/**
 * 保存偏好；参数只留最近记下的 MAX_MODELS 个模型。存储不可用时静默跳过：
 * 偏好丢了只是下次回到默认，不值得为它打断用户。
 * @param prefs 要保存的偏好
 */
export function savePrefs(prefs: ComposerPrefs) {
  const entries = Object.entries(prefs.paramsByModel);
  const paramsByModel = Object.fromEntries(entries.slice(-MAX_MODELS));
  try {
    globalThis.localStorage?.setItem(KEY, JSON.stringify({ ...prefs, paramsByModel }));
  } catch {
    // 隐私模式或空间已满：偏好存不下来不影响使用
  }
}
