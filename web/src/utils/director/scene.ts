import type {
  CameraPose,
  DirectorCamera,
  DirectorCharacter,
  DirectorProp,
  DirectorScene,
  FigureModel,
  PoseId,
  PropShape,
} from "@/types";

import { propLabel, propSize } from "./prop-presets";
import { lookAngles } from "./sample";

/** 播放和导出的帧率 */
export const DIRECTOR_FPS = 24;

/** 对象数量上限：保护渲染和导出性能 */
export const DIRECTOR_LIMITS = { characters: 8, props: 30, cameras: 6 } as const;

/** 镜头时长范围（秒） */
export const DURATION_RANGE = { min: 1, max: 15, step: 0.5, default: 5 } as const;

/** 场景 JSON 上限：存在节点里随画布保存，不能把 payload 撑大 */
export const MAX_SCENE_BYTES = 256 * 1024;

/** 运镜预设、默认机位看向人物的高度（胸口） */
export const TARGET_HEIGHT = 1.3;

/** 姿势预设的中文名，顺序就是下拉里的顺序 */
export const POSE_LABELS: Record<PoseId, string> = {
  stand: "站立",
  walk: "行走",
  run: "奔跑",
  sit: "坐",
  crouch: "蹲",
  point: "单手指向",
  armsCrossed: "双手抱胸",
  wave: "挥手",
  talk: "对话",
  raiseHand: "举手",
  lookBack: "回头",
  lying: "倒地",
};

/** 人物灰模的着色，按添加顺序取第一个没用过的 */
export const CHARACTER_COLORS = [
  "#d4d4d8",
  "#fca5a5",
  "#93c5fd",
  "#86efac",
  "#fcd34d",
  "#c4b5fd",
  "#f9a8d4",
  "#67e8f9",
] as const;

type NewId = () => string;
type AddKind = "character" | "prop" | "camera";

const LIMIT_OF: Record<AddKind, number> = {
  character: DIRECTOR_LIMITS.characters,
  prop: DIRECTOR_LIMITS.props,
  camera: DIRECTOR_LIMITS.cameras,
};

const countOf = (scene: DirectorScene, kind: AddKind) =>
  kind === "character"
    ? scene.characters.length
    : kind === "prop"
      ? scene.props.length
      : scene.cameras.length;

/** 还能不能再加一个这种对象 */
export const canAdd = (scene: DirectorScene, kind: AddKind) =>
  countOf(scene, kind) < LIMIT_OF[kind];

/** 到上限时的提示 */
export const limitMessage = (kind: AddKind) =>
  `最多 ${LIMIT_OF[kind]} 个${kind === "character" ? "人物" : kind === "prop" ? "道具" : "机位"}`;

/** 取第一个没被占用的序号名：机位1、机位2… */
const nextNumbered = (prefix: string, names: string[]) => {
  for (let n = 1; ; n++) if (!names.includes(`${prefix}${n}`)) return `${prefix}${n}`;
};

/** 从 from 看向原点处人物胸口的机位 */
const cameraLookingAtOrigin = (x: number, y: number, z: number): CameraPose => ({
  x,
  y,
  z,
  ...lookAngles([x, y, z], [0, TARGET_HEIGHT, 0]),
  fov: 50,
});

/** 新建导演台节点时的场景：一个人物站在原点面向机位，一台机位在正前方 4 米 */
export function createDefaultScene(newId: NewId = () => crypto.randomUUID()): DirectorScene {
  const characterId = newId();
  const cameraId = newId();
  return {
    version: 1,
    duration: DURATION_RANGE.default,
    activeCameraId: cameraId,
    characters: [
      {
        id: characterId,
        name: "角色A",
        color: CHARACTER_COLORS[0],
        model: "male",
        pose: "stand",
        scale: 1,
        x: 0,
        z: 0,
        yaw: 180,
        keys: [],
      },
    ],
    props: [],
    cameras: [
      {
        id: cameraId,
        name: "机位1",
        aspect: "16:9",
        ...cameraLookingAtOrigin(0, 1.6, 4),
        lookAt: characterId,
        keys: [],
      },
    ],
  };
}

const assertCanAdd = (scene: DirectorScene, kind: AddKind) => {
  if (!canAdd(scene, kind)) throw new Error(limitMessage(kind));
};

/** 人物灰模的中文名 */
export const FIGURE_LABELS: Record<FigureModel, string> = { male: "男性", female: "女性" };

/** 加一个人物：名字取第一个没用过的字母，左右交替排开 */
export function addCharacter(
  scene: DirectorScene,
  newId: NewId,
  model: FigureModel = "male",
): { scene: DirectorScene; id: string } {
  assertCanAdd(scene, "character");
  const names = scene.characters.map((c) => c.name);
  const letter = "ABCDEFGHIJKLMNOPQRSTUVWXYZ".split("").find((l) => !names.includes(`角色${l}`));
  const used = scene.characters.map((c) => c.color);
  const n = scene.characters.length;
  const id = newId();
  const character: DirectorCharacter = {
    id,
    name: `角色${letter ?? n + 1}`,
    color: CHARACTER_COLORS.find((c) => !used.includes(c)) ?? CHARACTER_COLORS[0],
    model,
    pose: "stand",
    scale: 1,
    x: (n % 2 ? 1 : -1) * Math.ceil(n / 2) * 1.2,
    z: 0,
    yaw: 180,
    keys: [],
  };
  return { scene: { ...scene, characters: [...scene.characters, character] }, id };
}

/** 新道具的落点：一行放 4 个，一行一行往后排，免得叠在同一个位置 */
const propSpot = (count: number) => ({
  x: 2 + (count % 4) * 1.5,
  z: -1.5 - Math.floor(count / 4) * 1.5,
});

/** 加一个道具（几何体或物体） */
export function addProp(
  scene: DirectorScene,
  newId: NewId,
  shape: PropShape = "box",
): { scene: DirectorScene; id: string } {
  assertCanAdd(scene, "prop");
  const [w, h, d] = propSize(shape);
  const id = newId();
  const prop: DirectorProp = {
    id,
    name: nextNumbered(
      propLabel(shape),
      scene.props.map((p) => p.name),
    ),
    shape,
    ...propSpot(scene.props.length),
    yaw: 0,
    w,
    h,
    d,
  };
  return { scene: { ...scene, props: [...scene.props, prop] }, id };
}

/** 加一台机位并设为当前机位；不传位置时摆在右前方看向原点 */
export function addCamera(
  scene: DirectorScene,
  newId: NewId,
  pose?: CameraPose,
): { scene: DirectorScene; id: string } {
  assertCanAdd(scene, "camera");
  const id = newId();
  const camera: DirectorCamera = {
    id,
    name: nextNumbered(
      "机位",
      scene.cameras.map((c) => c.name),
    ),
    aspect: "16:9",
    ...(pose ?? cameraLookingAtOrigin(3, 1.6, 3)),
    keys: [],
  };
  return {
    scene: { ...scene, cameras: [...scene.cameras, camera], activeCameraId: id },
    id,
  };
}

/** 删掉一个对象：当前机位被删就换成剩下的第一台，看向它的机位清掉引用 */
export function removeObject(scene: DirectorScene, id: string): DirectorScene {
  const cameras = scene.cameras
    .filter((c) => c.id !== id)
    .map((c) => (c.lookAt === id ? { ...c, lookAt: undefined } : c));
  return {
    ...scene,
    characters: scene.characters.filter((c) => c.id !== id),
    props: scene.props.filter((p) => p.id !== id),
    cameras,
    activeCameraId:
      scene.activeCameraId === id || !cameras.some((c) => c.id === scene.activeCameraId)
        ? (cameras[0]?.id ?? "")
        : scene.activeCameraId,
  };
}

/** 把时长限制在范围内并按步长取整 */
export const clampDuration = (duration: number) =>
  Math.min(
    DURATION_RANGE.max,
    Math.max(DURATION_RANGE.min, Math.round(duration / DURATION_RANGE.step) * DURATION_RANGE.step),
  );

/** 改时长：已有关键帧按比例缩放，运镜节奏不变 */
export function setDuration(scene: DirectorScene, duration: number): DirectorScene {
  const next = clampDuration(duration);
  const ratio = next / scene.duration;
  const scale = <K extends { t: number }>(keys: K[]) =>
    keys.map((k) => ({ ...k, t: Math.round(k.t * ratio * 100) / 100 }));
  return {
    ...scene,
    duration: next,
    characters: scene.characters.map((c) => ({ ...c, keys: scale(c.keys) })),
    cameras: scene.cameras.map((c) => ({ ...c, keys: scale(c.keys) })),
  };
}

/** 导出一段视频的帧数 */
export const frameCount = (duration: number) => Math.round(duration * DIRECTOR_FPS);

/** 复制节点时用：所有对象换新 id，当前机位和看向引用跟着换；缩略图不带走 */
export function cloneScene(scene: DirectorScene, newId: NewId): DirectorScene {
  const map = new Map<string, string>();
  const remap = (id: string) => {
    if (!map.has(id)) map.set(id, newId());
    return map.get(id)!;
  };
  const copy = structuredClone(scene);
  copy.characters.forEach((c) => (c.id = remap(c.id)));
  copy.props.forEach((p) => (p.id = remap(p.id)));
  copy.cameras.forEach((c) => {
    c.id = remap(c.id);
    if (c.lookAt) c.lookAt = map.get(c.lookAt);
  });
  copy.activeCameraId = map.get(scene.activeCameraId) ?? copy.cameras[0]?.id ?? "";
  delete copy.thumb;
  return copy;
}

/** 场景太大时给出原因，写入节点前调用 */
export function sceneSizeError(scene: DirectorScene): string | null {
  const bytes = new TextEncoder().encode(JSON.stringify(scene)).length;
  if (bytes <= MAX_SCENE_BYTES) return null;
  return `场景数据过大（${Math.ceil(bytes / 1024)}KB，上限 ${MAX_SCENE_BYTES / 1024}KB），请删减关键帧或对象`;
}

const isRecord = (value: unknown): value is Record<string, unknown> =>
  typeof value === "object" && value !== null && !Array.isArray(value);

const hasIds = (value: unknown) =>
  Array.isArray(value) && value.every((item) => isRecord(item) && typeof item.id === "string");

/**
 * 读档时校验场景：结构不对返回 undefined（节点按没有场景处理），
 * 时长夹到合法范围、关键帧按时间排好，其余字段原样保留。
 */
export function normalizeScene(raw: unknown): DirectorScene | undefined {
  if (!isRecord(raw) || raw.version !== 1 || typeof raw.duration !== "number") return undefined;
  if (!hasIds(raw.characters) || !hasIds(raw.props) || !hasIds(raw.cameras)) return undefined;
  const scene = structuredClone(raw) as unknown as DirectorScene;
  const sorted = <K extends { t: number }>(keys: unknown) =>
    (Array.isArray(keys) ? (keys as K[]) : []).slice().sort((a, b) => a.t - b.t);
  scene.duration = clampDuration(scene.duration);
  scene.characters.forEach((c) => {
    c.keys = sorted(c.keys);
    // 旧场景没有 model，或读到不认识的值：按男性
    if (c.model !== "male" && c.model !== "female") c.model = "male";
  });
  scene.cameras.forEach((c) => (c.keys = sorted(c.keys)));
  if (typeof scene.activeCameraId !== "string") scene.activeCameraId = scene.cameras[0]?.id ?? "";
  return scene;
}
