import type { CameraKey, CameraPose, DirectorCamera, DirectorScene } from "@/types";

import { roundKeyTime } from "./keys";
import { forwardOf, lookAngles, rightOf, sampleCamera, sampleCharacter } from "./sample";
import { TARGET_HEIGHT } from "./scene";

/** 运镜预设 id */
export type CameraPresetId =
  | "fixed"
  | "dollyIn"
  | "dollyOut"
  | "panLeft"
  | "panRight"
  | "tiltUp"
  | "tiltDown"
  | "truckLeft"
  | "truckRight"
  | "craneUp"
  | "craneDown"
  | "orbit"
  | "follow";

export type CameraPreset = {
  id: CameraPresetId;
  label: string;
  description: string;
  /** 强度的单位；空串表示这个预设没有强度参数 */
  unit: "" | "%" | "°" | "m";
  defaultStrength: number;
  min: number;
  max: number;
  step: number;
};

/** 运镜预设清单，顺序就是下拉里的顺序 */
export const CAMERA_PRESETS: readonly CameraPreset[] = [
  {
    id: "fixed",
    label: "固定",
    description: "清空关键帧，机位全程不动",
    unit: "",
    defaultStrength: 0,
    min: 0,
    max: 0,
    step: 1,
  },
  {
    id: "dollyIn",
    label: "推近",
    description: "沿视线向目标前进，距离 = 到目标距离 × 强度",
    unit: "%",
    defaultStrength: 40,
    min: 10,
    max: 80,
    step: 5,
  },
  {
    id: "dollyOut",
    label: "拉远",
    description: "沿视线后退，距离 = 到目标距离 × 强度",
    unit: "%",
    defaultStrength: 40,
    min: 10,
    max: 150,
    step: 5,
  },
  {
    id: "panLeft",
    label: "左摇",
    description: "机位不动，原地向左转",
    unit: "°",
    defaultStrength: 30,
    min: 5,
    max: 90,
    step: 5,
  },
  {
    id: "panRight",
    label: "右摇",
    description: "机位不动，原地向右转",
    unit: "°",
    defaultStrength: 30,
    min: 5,
    max: 90,
    step: 5,
  },
  {
    id: "tiltUp",
    label: "上摇",
    description: "机位不动，原地抬头",
    unit: "°",
    defaultStrength: 15,
    min: 5,
    max: 60,
    step: 5,
  },
  {
    id: "tiltDown",
    label: "下摇",
    description: "机位不动，原地低头",
    unit: "°",
    defaultStrength: 15,
    min: 5,
    max: 60,
    step: 5,
  },
  {
    id: "truckLeft",
    label: "左移",
    description: "机位向左平移，朝向不变",
    unit: "m",
    defaultStrength: 2,
    min: 0.5,
    max: 6,
    step: 0.5,
  },
  {
    id: "truckRight",
    label: "右移",
    description: "机位向右平移，朝向不变",
    unit: "m",
    defaultStrength: 2,
    min: 0.5,
    max: 6,
    step: 0.5,
  },
  {
    id: "craneUp",
    label: "升",
    description: "竖直升高，同时保持看向目标",
    unit: "m",
    defaultStrength: 1.5,
    min: 0.5,
    max: 5,
    step: 0.5,
  },
  {
    id: "craneDown",
    label: "降",
    description: "竖直降低（不低于 0.2 米），保持看向目标",
    unit: "m",
    defaultStrength: 1.5,
    min: 0.5,
    max: 5,
    step: 0.5,
  },
  {
    id: "orbit",
    label: "环绕",
    description: "绕目标水平旋转，每 0.5 秒烘焙一帧",
    unit: "°",
    defaultStrength: 90,
    min: 15,
    max: 360,
    step: 15,
  },
  {
    id: "follow",
    label: "跟拍",
    description: "保持起点时与目标人物的相对位置，按人物走位每 0.5 秒烘焙一帧",
    unit: "",
    defaultStrength: 0,
    min: 0,
    max: 0,
    step: 1,
  },
];

/** 烘焙类预设的采样间隔（秒） */
const BAKE_STEP = 0.5;
/** 机位最低高度、最大俯仰 */
const MIN_HEIGHT = 0.2;
const MAX_PITCH = 85;
/** 视线前方没有人物时，目标取前方多远的点 */
const FALLBACK_DISTANCE = 4;

type Vec3 = [number, number, number];

const dot = (a: Vec3, b: Vec3) => a[0] * b[0] + a[1] * b[1] + a[2] * b[2];

/**
 * 运镜目标：机位设了看向就用那个人物；否则取视线前方（偏离视线不超过约 40°）最近的人物；
 * 都没有就取视线前方 4 米的点。人物取 0 秒时的站位、胸口高度。
 */
function presetTarget(scene: DirectorScene, camera: DirectorCamera, start: CameraPose) {
  const chest = (id: string) => {
    const character = scene.characters.find((c) => c.id === id);
    if (!character) return undefined;
    const pose = sampleCharacter(character, 0);
    return { character, point: [pose.x, TARGET_HEIGHT * character.scale, pose.z] as Vec3 };
  };
  if (camera.lookAt) {
    const target = chest(camera.lookAt);
    if (target) return target;
  }
  const forward = forwardOf(start);
  const right = rightOf(start);
  const up: Vec3 = [
    right[1] * forward[2] - right[2] * forward[1],
    right[2] * forward[0] - right[0] * forward[2],
    right[0] * forward[1] - right[1] * forward[0],
  ];
  let best: { depth: number; target: NonNullable<ReturnType<typeof chest>> } | undefined;
  for (const character of scene.characters) {
    const target = chest(character.id)!;
    const d: Vec3 = [
      target.point[0] - start.x,
      target.point[1] - start.y,
      target.point[2] - start.z,
    ];
    const depth = dot(d, forward);
    if (depth <= 0) continue;
    const off = Math.hypot(dot(d, right), dot(d, up)) / depth;
    if (off < 0.84 && (!best || depth < best.depth)) best = { depth, target };
  }
  if (best) return best.target;
  return {
    character: undefined,
    point: [
      start.x + forward[0] * FALLBACK_DISTANCE,
      start.y + forward[1] * FALLBACK_DISTANCE,
      start.z + forward[2] * FALLBACK_DISTANCE,
    ] as Vec3,
  };
}

const key = (t: number, pose: CameraPose, ease: CameraKey["ease"]): CameraKey => ({
  t: roundKeyTime(t),
  x: pose.x,
  y: pose.y,
  z: pose.z,
  yaw: pose.yaw,
  pitch: pose.pitch,
  fov: pose.fov,
  ease,
});

const clampPitch = (pitch: number) => Math.min(MAX_PITCH, Math.max(-MAX_PITCH, pitch));

/** 按预设生成整段的关键帧；warning 是给用户的提示 */
function presetKeys(
  scene: DirectorScene,
  camera: DirectorCamera,
  preset: CameraPresetId,
  strength: number,
): { keys: CameraKey[]; warning?: string } {
  const duration = scene.duration;
  const start = sampleCamera(camera, 0);
  const target = presetTarget(scene, camera, start);
  const end: CameraPose = { ...start };
  const bakeTimes = () => {
    const times: number[] = [];
    for (let t = 0; t < duration - 1e-6; t += BAKE_STEP) times.push(t);
    times.push(duration);
    return times;
  };

  switch (preset) {
    case "fixed":
      return { keys: [key(0, start, "smooth")] };
    case "dollyIn":
    case "dollyOut": {
      const p = target.point;
      const distance = Math.hypot(p[0] - start.x, p[1] - start.y, p[2] - start.z);
      const move = ((preset === "dollyIn" ? 1 : -1) * distance * strength) / 100;
      const forward = forwardOf(start);
      end.x += forward[0] * move;
      end.y = Math.max(MIN_HEIGHT, end.y + forward[1] * move);
      end.z += forward[2] * move;
      break;
    }
    case "panLeft":
      end.yaw -= strength;
      break;
    case "panRight":
      end.yaw += strength;
      break;
    case "tiltUp":
      end.pitch = clampPitch(end.pitch + strength);
      break;
    case "tiltDown":
      end.pitch = clampPitch(end.pitch - strength);
      break;
    case "truckLeft":
    case "truckRight": {
      const sign = preset === "truckRight" ? 1 : -1;
      const right = rightOf(start);
      end.x += right[0] * strength * sign;
      end.z += right[2] * strength * sign;
      break;
    }
    case "craneUp":
    case "craneDown": {
      end.y = Math.max(MIN_HEIGHT, end.y + (preset === "craneUp" ? strength : -strength));
      const look = lookAngles([end.x, end.y, end.z], target.point);
      end.yaw = look.yaw;
      end.pitch = clampPitch(look.pitch);
      break;
    }
    case "orbit": {
      const [tx, , tz] = target.point;
      const rx = start.x - tx;
      const rz = start.z - tz;
      // 起点时机位和"正对目标"的夹角保持不变，转的过程中构图不跳
      const yawOffset = start.yaw - lookAngles([start.x, start.y, start.z], target.point).yaw;
      const keys = bakeTimes().map((t) => {
        const angle = ((strength * t) / duration) * (Math.PI / 180);
        const x = tx + rx * Math.cos(angle) - rz * Math.sin(angle);
        const z = tz + rx * Math.sin(angle) + rz * Math.cos(angle);
        const look = lookAngles([x, start.y, z], target.point);
        return key(t, { ...start, x, z, yaw: look.yaw + yawOffset }, "linear");
      });
      return { keys };
    }
    case "follow": {
      const character = target.character;
      if (!character) {
        return {
          keys: [key(0, start, "smooth")],
          warning: "没有可跟随的人物：请在机位属性里设置「看向」",
        };
      }
      const origin = sampleCharacter(character, 0);
      const keys = bakeTimes().map((t) => {
        const now = sampleCharacter(character, t);
        return key(
          t,
          { ...start, x: start.x + now.x - origin.x, z: start.z + now.z - origin.z },
          "linear",
        );
      });
      const warning =
        character.keys.length < 2
          ? `「${character.name}」还没有走位关键帧，跟拍效果等同固定机位；先给人物打走位关键帧`
          : undefined;
      return { keys, warning };
    }
  }
  return { keys: [key(0, start, "smooth"), key(duration, end, "smooth")] };
}

/**
 * 给机位套用运镜预设：以机位 0 秒时的状态为起点，替换它整段的关键帧。
 * 不改动传入的场景；机位不存在时原样返回。
 * @param strength 强度，单位见预设的 unit；不传用默认值
 */
export function applyPreset(
  scene: DirectorScene,
  cameraId: string,
  preset: CameraPresetId,
  strength?: number,
): { scene: DirectorScene; warning?: string } {
  const camera = scene.cameras.find((c) => c.id === cameraId);
  const meta = CAMERA_PRESETS.find((p) => p.id === preset);
  if (!camera || !meta) return { scene };
  const amount = strength ?? meta.defaultStrength;
  const { keys, warning } = presetKeys(scene, camera, preset, amount);
  return {
    scene: {
      ...scene,
      cameras: scene.cameras.map((c) => (c.id === cameraId ? { ...c, keys } : c)),
    },
    warning,
  };
}
