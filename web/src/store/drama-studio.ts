import { create } from "zustand";

/** 一部短剧在片段工作区里的个人偏好：选了哪个视频 / 首帧模型、分辨率等生成参数 */
export type StudioPrefs = {
  /** 视频模型 key；空串 = 取第一个可用的 */
  videoModel: string;
  /** 首帧图片模型 key；空串 = 取第一个 */
  imageModel: string;
  /** 用户在顶栏选的生成参数（分辨率等），键是模型参数名 */
  overrides: Record<string, unknown>;
};

const EMPTY: StudioPrefs = { videoModel: "", imageModel: "", overrides: {} };
const storageKey = (dramaId: string) => `drama-studio:${dramaId}`;

/** 读本机存的偏好；读不到、解析失败都当没存过 */
export function readPrefs(dramaId: string): StudioPrefs {
  try {
    const raw = localStorage.getItem(storageKey(dramaId));
    if (!raw) return EMPTY;
    const parsed = JSON.parse(raw) as Partial<StudioPrefs> | null;
    return {
      videoModel: typeof parsed?.videoModel === "string" ? parsed.videoModel : "",
      imageModel: typeof parsed?.imageModel === "string" ? parsed.imageModel : "",
      overrides:
        parsed?.overrides &&
        typeof parsed.overrides === "object" &&
        !Array.isArray(parsed.overrides)
          ? parsed.overrides
          : {},
    };
  } catch {
    return EMPTY;
  }
}

function writePrefs(dramaId: string, prefs: StudioPrefs) {
  try {
    localStorage.setItem(storageKey(dramaId), JSON.stringify(prefs));
  } catch {
    // 存不下（隐私模式、配额）就只在本次会话里生效
  }
}

type StudioPrefsState = {
  byDrama: Record<string, StudioPrefs>;
  /** 读（首次从本机存储载入） */
  get: (dramaId: string) => StudioPrefs;
  patch: (dramaId: string, change: Partial<StudioPrefs>) => void;
};

export const useStudioPrefsStore = create<StudioPrefsState>((set, get) => ({
  byDrama: {},
  get: (dramaId) => get().byDrama[dramaId] ?? readPrefs(dramaId),
  patch: (dramaId, change) => {
    const next = { ...get().get(dramaId), ...change };
    writePrefs(dramaId, next);
    set((state) => ({ byDrama: { ...state.byDrama, [dramaId]: next } }));
  },
}));

/** 订阅一部短剧的偏好；没存过时是稳定的空对象 */
export function useStudioPrefs(dramaId: string): StudioPrefs {
  return useStudioPrefsStore((state) => state.byDrama[dramaId]) ?? readCached(dramaId);
}

const cache = new Map<string, StudioPrefs>();
function readCached(dramaId: string): StudioPrefs {
  let value = cache.get(dramaId);
  if (!value) {
    value = readPrefs(dramaId);
    cache.set(dramaId, value);
  }
  return value;
}
