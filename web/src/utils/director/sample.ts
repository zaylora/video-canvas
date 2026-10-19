import type {
  CameraPose,
  CharacterPose,
  DirectorCamera,
  DirectorCharacter,
  KeyEase,
} from "@/types";

/** 角度转弧度 */
export const DEG = Math.PI / 180;

const lerp = (from: number, to: number, u: number) => from + (to - from) * u;

/** 角度按最短方向插值：170° 到 -170° 经过 180°，不绕一大圈 */
const lerpAngle = (from: number, to: number, u: number) => {
  const delta = ((((to - from) % 360) + 540) % 360) - 180;
  return from + delta * u;
};

/** 关键帧到下一帧的进度曲线：缓入缓出用 smoothstep */
const eased = (ease: KeyEase, u: number) => (ease === "linear" ? u : u * u * (3 - 2 * u));

/**
 * 在一组按时间升序的关键帧里取 t 时刻的值。
 * 没有关键帧用静态状态；首帧之前取首帧、末帧之后取末帧；插值方式由前一帧决定。
 */
function sampleKeys<P extends object, K extends P & { t: number; ease: KeyEase }>(
  keys: K[],
  t: number,
  fields: readonly (keyof P)[],
  fallback: P,
  angles: readonly (keyof P)[],
): P {
  const pick = (source: P) =>
    Object.fromEntries(fields.map((field) => [field, source[field]])) as P;
  if (keys.length === 0) return pick(fallback);
  if (t <= keys[0].t) return pick(keys[0]);
  const lastKey = keys[keys.length - 1];
  if (t >= lastKey.t) return pick(lastKey);
  let index = 0;
  while (keys[index + 1].t < t) index++;
  const from = keys[index];
  const to = keys[index + 1];
  const u = eased(from.ease, (t - from.t) / (to.t - from.t));
  const result = {} as Record<keyof P, number>;
  for (const field of fields) {
    const a = from[field] as number;
    const b = to[field] as number;
    result[field] = angles.includes(field) ? lerpAngle(a, b, u) : lerp(a, b, u);
  }
  return result as P;
}

const CHARACTER_FIELDS = ["x", "z", "yaw"] as const;
const CAMERA_FIELDS = ["x", "y", "z", "yaw", "pitch", "fov"] as const;

/** 人物在 t 秒时的站位 */
export const sampleCharacter = (character: DirectorCharacter, t: number): CharacterPose =>
  sampleKeys<CharacterPose, DirectorCharacter["keys"][number]>(
    character.keys,
    t,
    CHARACTER_FIELDS,
    character,
    ["yaw"],
  );

/** 机位在 t 秒时的状态 */
export const sampleCamera = (camera: DirectorCamera, t: number): CameraPose =>
  sampleKeys<CameraPose, DirectorCamera["keys"][number]>(camera.keys, t, CAMERA_FIELDS, camera, [
    "yaw",
    "pitch",
  ]);

/** 机位视线方向（单位向量） */
export function forwardOf(pose: Pick<CameraPose, "yaw" | "pitch">): [number, number, number] {
  const yaw = pose.yaw * DEG;
  const pitch = pose.pitch * DEG;
  return [Math.sin(yaw) * Math.cos(pitch), Math.sin(pitch), -Math.cos(yaw) * Math.cos(pitch)];
}

/** 机位右手方向（水平单位向量） */
export function rightOf(pose: Pick<CameraPose, "yaw">): [number, number, number] {
  const yaw = pose.yaw * DEG;
  return [Math.cos(yaw), 0, Math.sin(yaw)];
}

/** 从 from 看向 to 需要的偏航和俯仰（度） */
export function lookAngles(
  from: [number, number, number],
  to: [number, number, number],
): { yaw: number; pitch: number } {
  const dx = to[0] - from[0];
  const dy = to[1] - from[1];
  const dz = to[2] - from[2];
  return {
    yaw: Math.atan2(dx, -dz) / DEG,
    pitch: Math.atan2(dy, Math.hypot(dx, dz)) / DEG,
  };
}
