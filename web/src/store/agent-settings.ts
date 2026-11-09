import { create } from "zustand";
import { persist } from "zustand/middleware";

import type { PetMode } from "@/utils/pet/mood";
import type { PetAnchor, PetCorner } from "@/utils/pet/placement";

/** 停靠宽度：默认、最小、最大（最大还不超过窗口的一半） */
export const DOCK_WIDTH = { default: 400, min: 320, max: 560 } as const;

/** 浮窗在画布里的位置：相对画布右下角的偏移（像素），拖动后记住 */
export type AgentPanelOffset = { dx: number; dy: number };

/** 桌宠的个人设置：打扰档位、拖动后的位置、皮肤 */
export type PetSettings = {
  mode: PetMode;
  /** 相对最近一个角的偏移，null 是默认位置（右下角） */
  anchor: PetAnchor | null;
  /** 皮肤目录名（web/public/pets/{skin}）；一期只有 xiaojing */
  skin: string;
};

export const DEFAULT_PET_SETTINGS: PetSettings = { mode: "lively", anchor: null, skin: "xiaojing" };

const PET_MODES: readonly PetMode[] = ["lively", "quiet", "hidden"];
const PET_CORNERS: readonly PetCorner[] = ["br", "bl", "tr", "tl"];

const isRecord = (v: unknown): v is Record<string, unknown> =>
  typeof v === "object" && v !== null && !Array.isArray(v);

/** 把存下来的 pet 逐项校验，缺的或非法的回退默认值 */
function normalizePet(raw: unknown): PetSettings {
  const src = isRecord(raw) ? raw : {};
  const a = src.anchor;
  const anchorOk =
    isRecord(a) &&
    PET_CORNERS.includes(a.corner as PetCorner) &&
    Number.isFinite(a.dx) &&
    Number.isFinite(a.dy);
  return {
    mode: PET_MODES.includes(src.mode as PetMode)
      ? (src.mode as PetMode)
      : DEFAULT_PET_SETTINGS.mode,
    anchor: anchorOk
      ? { corner: a.corner as PetCorner, dx: a.dx as number, dy: a.dy as number }
      : null,
    skin: typeof src.skin === "string" && src.skin ? src.skin : DEFAULT_PET_SETTINGS.skin,
  };
}

/** persist 迁移：旧数据（版本 0）没有 pet 字段，补上默认值，其余字段原样保留 */
export function migrateAgentSettings(persisted: unknown, _version: number): unknown {
  const base = isRecord(persisted) ? persisted : {};
  return { ...base, pet: normalizePet(base.pet) };
}

type AgentSettings = {
  /** 选用的 Agent 模型 key，空表示取第一个可用的 */
  modelKey: string;
  /** 是否显示思考过程 */
  showThinking: boolean;
  /** 浮窗位置，null 是默认位置（右下角） */
  offset: AgentPanelOffset | null;
  /** 停靠到画布右侧（全高侧栏）；窗口太窄放不下时临时退回浮窗，偏好不变 */
  docked: boolean;
  /** 停靠时的宽度（像素） */
  dockWidth: number;
  /** 桌宠设置；隐藏桌宠后浮窗恢复使用上面的 offset */
  pet: PetSettings;
  /** 改设置 */
  patch: (next: Partial<Omit<AgentSettings, "patch" | "patchPet">>) => void;
  /** 只改桌宠设置里给出的字段 */
  patchPet: (next: Partial<PetSettings>) => void;
};

/** 浮窗的个人设置：存在本机，换设备不跟随 */
export const useAgentSettings = create<AgentSettings>()(
  persist(
    (set) => ({
      modelKey: "",
      showThinking: false,
      offset: null,
      docked: false,
      dockWidth: DOCK_WIDTH.default,
      pet: { ...DEFAULT_PET_SETTINGS },
      patch: (next) => set(next),
      patchPet: (next) => set((s) => ({ pet: { ...s.pet, ...next } })),
    }),
    {
      name: "video-canvas-agent-settings",
      version: 1,
      migrate: (persisted, version) => migrateAgentSettings(persisted, version) as AgentSettings,
    },
  ),
);
