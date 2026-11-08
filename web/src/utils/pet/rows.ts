/** 图集列数：每行最多 8 帧 */
export const PET_COLS = 8;

/** 图集里一格的像素尺寸（hatch-pet 规范） */
export const PET_CELL = { width: 192, height: 208 } as const;

/** 动作行名称：前 9 行与 Codex / hatch-pet 一致，sleeping 是本项目的扩展行 */
export type PetRowName =
  | "idle"
  | "running-right"
  | "running-left"
  | "waving"
  | "jumping"
  | "failed"
  | "waiting"
  | "running"
  | "review"
  | "sleeping";

/** 一行动作：行号、帧数、每帧停留的毫秒数 */
export type PetRow = {
  name: PetRowName;
  /** 在图集里的行号，从 0 起 */
  row: number;
  frames: number;
  /** 每帧的停留时间，长度等于 frames */
  durations: readonly number[];
};

/** 前面每帧同样长、最后一帧停留更久 */
const evenThenLast = (frames: number, each: number, last: number): number[] => [
  ...Array.from({ length: frames - 1 }, () => each),
  last,
];

const ROW_DEFS: ReadonlyArray<readonly [PetRowName, readonly number[]]> = [
  ["idle", [280, 110, 110, 140, 140, 320]],
  ["running-right", evenThenLast(8, 120, 220)],
  ["running-left", evenThenLast(8, 120, 220)],
  ["waving", evenThenLast(4, 140, 280)],
  ["jumping", evenThenLast(5, 140, 280)],
  ["failed", evenThenLast(8, 140, 240)],
  ["waiting", evenThenLast(6, 150, 260)],
  ["running", evenThenLast(6, 120, 220)],
  ["review", evenThenLast(6, 150, 280)],
  ["sleeping", evenThenLast(6, 220, 400)],
];

/** 默认的 10 行动作表 */
export const PET_ROWS: readonly PetRow[] = ROW_DEFS.map(([name, durations], row) => ({
  name,
  row,
  frames: durations.length,
  durations,
}));

/** 按名字查行号 */
export const PET_ROW = Object.fromEntries(PET_ROWS.map((r) => [r.name, r.row])) as Record<
  PetRowName,
  number
>;

/** 桌宠清单 pet.json：Codex 字段加两个可选扩展 */
export type PetManifest = {
  id: string;
  displayName: string;
  description: string;
  /** 图集文件，相对 pet.json 所在目录 */
  spritesheetPath: string;
  /** 声明有哪些扩展行，目前只认 "sleeping" */
  extensions: string[];
  /** 覆盖默认帧时长：行名 → 每帧毫秒数（帧数取数组长度） */
  rows: Partial<Record<PetRowName, number[]>>;
};

const isRecord = (v: unknown): v is Record<string, unknown> =>
  typeof v === "object" && v !== null && !Array.isArray(v);

const isDurations = (v: unknown): v is number[] =>
  Array.isArray(v) &&
  v.length >= 1 &&
  v.length <= PET_COLS &&
  v.every((n) => typeof n === "number" && Number.isFinite(n) && n > 0);

/**
 * 解析并规整 pet.json。缺 id 或图集路径时抛错（调用方据此退回胶囊）；
 * 可选字段缺省或非法时取默认，不抛错。
 */
export function parsePetManifest(raw: unknown): PetManifest {
  if (!isRecord(raw)) throw new Error("pet.json 不是对象");
  const { id, spritesheetPath } = raw;
  if (typeof id !== "string" || !id) throw new Error("pet.json 缺少 id");
  if (typeof spritesheetPath !== "string" || !spritesheetPath) {
    throw new Error("pet.json 缺少 spritesheetPath");
  }
  const extensions = Array.isArray(raw.extensions)
    ? raw.extensions.filter((e): e is string => typeof e === "string")
    : [];
  const rows: PetManifest["rows"] = {};
  if (isRecord(raw.rows)) {
    for (const { name } of PET_ROWS) {
      const d = raw.rows[name];
      if (isDurations(d)) rows[name] = d;
    }
  }
  return {
    id,
    displayName: typeof raw.displayName === "string" && raw.displayName ? raw.displayName : id,
    description: typeof raw.description === "string" ? raw.description : "",
    spritesheetPath,
    extensions,
    rows,
  };
}

/**
 * 按清单得出实际可用的动作行。没声明 sleeping 扩展时去掉第 10 行，
 * 由 hasSleeping 告诉渲染层「睡觉」要走回退（idle 第一帧 + 降透明度 + z）。
 */
export function resolvePetRows(manifest: PetManifest): { rows: PetRow[]; hasSleeping: boolean } {
  const hasSleeping = manifest.extensions.includes("sleeping");
  const rows = PET_ROWS.filter((r) => r.name !== "sleeping" || hasSleeping).map((r) => {
    const override = manifest.rows[r.name];
    return override ? { ...r, frames: override.length, durations: override } : r;
  });
  return { rows, hasSleeping };
}
