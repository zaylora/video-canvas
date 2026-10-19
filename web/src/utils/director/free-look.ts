import { MAX_PITCH, wrapYaw } from "./camera-view";

/**
 * 导演视角、机位视角共用的「自由相机」操作的纯计算（不碰 three.js，便于单测）：
 * 精确抓手拖动、滚轮上下摇、滚轮变焦、键盘水平平移、按住平移。
 * 朝向用偏航 / 俯仰（度）：偏航 0 面朝 -z、90 面朝 +x，俯仰为正抬头，没有滚转，和舞台里机位的约定一致。
 */

/** 变焦的视场角范围（度），和机位属性面板里 FOV 的范围一致 */
export const MIN_FOV = 10;
export const MAX_FOV = 120;

/** 滚轮每滚 1 像素，视角摇多少度（滚一格约 100 像素 = 2°） */
const TILT_PER_PIXEL = 0.02;
/** 滚轮每滚 1 像素，视场角变化的比例（滚一格约 10%） */
const ZOOM_PER_PIXEL = 0.001;
/** 地面抓取深度的范围（米），以及视线碰不到地面时的默认深度 */
const MIN_GRAB_DEPTH = 0.5;
const MAX_GRAB_DEPTH = 60;
const DEFAULT_GRAB_DEPTH = 8;

const DEG = Math.PI / 180;

type Vec = [number, number, number];

const clamp = (value: number, min: number, max: number) => Math.min(max, Math.max(min, value));
const dot = (a: readonly number[], b: readonly number[]) => a[0] * b[0] + a[1] * b[1] + a[2] * b[2];

/**
 * 屏幕上某个像素对应的视线方向（相机坐标系里的单位向量，前方是 -z）。
 * @param ndcX 横向，-1 在画面左边缘，1 在右边缘
 * @param ndcY 纵向，-1 在画面下边缘，1 在上边缘
 * @param vfov 竖直视场角（度）
 * @param aspect 画面宽高比
 */
export function pixelRay(ndcX: number, ndcY: number, vfov: number, aspect: number): Vec {
  const tan = Math.tan((vfov / 2) * DEG);
  const x = ndcX * tan * aspect;
  const y = ndcY * tan;
  const length = Math.hypot(x, y, 1);
  return [x / length, y / length, -1 / length];
}

/** 把相机坐标系里的向量转到世界坐标系：先按俯仰绕 x 轴，再按偏航绕 y 轴 */
export function rotateByLook(yaw: number, pitch: number, v: readonly number[]): Vec {
  const cp = Math.cos(pitch * DEG);
  const sp = Math.sin(pitch * DEG);
  const x1 = v[0];
  const y1 = v[1] * cp - v[2] * sp;
  const z1 = v[1] * sp + v[2] * cp;
  const theta = -yaw * DEG;
  const c = Math.cos(theta);
  const s = Math.sin(theta);
  return [x1 * c + z1 * s, y1, -x1 * s + z1 * c];
}

/** 角度（弧度）收回 (-π, π] */
const wrapRad = (angle: number) => {
  const wrapped = ((((angle + Math.PI) % (2 * Math.PI)) + 2 * Math.PI) % (2 * Math.PI)) - Math.PI;
  return wrapped === -Math.PI ? Math.PI : wrapped;
};

/**
 * 求偏航、俯仰，让相机坐标系里的方向 v 转到世界方向 d 上（没有滚转，所以只有两个自由度）。
 * 先由高度（y 分量转前后不变）解出俯仰，再由水平方位差解出偏航。
 * 抬头过高、够不到 d 时取最接近的；俯仰有两个解时取离 currentPitch 近且在限制内的那个。
 */
function solveLook(v: readonly number[], d: readonly number[], currentPitch: number) {
  const a = Math.hypot(v[1], v[2]);
  const phi = Math.atan2(v[2], v[1]);
  const base = Math.acos(clamp(d[1] / a, -1, 1));
  const limit = MAX_PITCH * DEG;
  const current = currentPitch * DEG;
  const candidates = [wrapRad(-phi + base), wrapRad(-phi - base)];
  const inRange = candidates.filter((p) => Math.abs(p) <= limit);
  const pool = inRange.length > 0 ? inRange : candidates;
  const best = pool.reduce((nearest, p) =>
    Math.abs(p - current) < Math.abs(nearest - current) ? p : nearest,
  );
  const pitch = clamp(best, -limit, limit);

  const cp = Math.cos(pitch);
  const sp = Math.sin(pitch);
  const wx = v[0];
  const wz = v[1] * sp + v[2] * cp;
  // 正对着上方或下方时水平方位没有意义，偏航保持不变
  if (Math.hypot(d[0], d[2]) < 1e-9 || Math.hypot(wx, wz) < 1e-9) {
    return { yaw: null, pitch: pitch / DEG };
  }
  const theta = Math.atan2(d[0], d[2]) - Math.atan2(wx, wz);
  return { yaw: wrapYaw(-theta / DEG), pitch: pitch / DEG };
}

/**
 * 抓手拖动：按下时鼠标下面那个方向，鼠标拖到哪，那个方向就跟到哪（精确到每一像素，没有惯性）。
 * 不是「每像素转固定角度」的近似：屏幕边缘的像素对应的角度比中间小，近似会越拖越偏、像被甩出去。
 * @param start 按下时的朝向（度）
 * @param from 按下时鼠标的位置（ndc，-1 ~ 1）
 * @param to 现在鼠标的位置（ndc）
 * @param vfov 此刻实际用的竖直视场角（度）
 * @param aspect 视口宽高比
 * @returns 新的朝向；抬头过高够不到时取最接近的，俯仰不超过 ±MAX_PITCH
 */
export function grabLook(
  start: { yaw: number; pitch: number },
  from: { x: number; y: number },
  to: { x: number; y: number },
  vfov: number,
  aspect: number,
): { yaw: number; pitch: number } {
  const grabbed = rotateByLook(start.yaw, start.pitch, pixelRay(from.x, from.y, vfov, aspect));
  const solved = solveLook(pixelRay(to.x, to.y, vfov, aspect), grabbed, start.pitch);
  return { yaw: solved.yaw ?? start.yaw, pitch: solved.pitch };
}

/** 滚轮的滚动量统一成像素：按行、按页滚动的鼠标换算一下 */
export function normalizeWheel(delta: number, deltaMode: number): number {
  if (deltaMode === 1) return delta * 16;
  if (deltaMode === 2) return delta * 400;
  return delta;
}

/** 滚轮上下摇：向上滚（deltaY 为负）视角向上抬，向下滚向下压；俯仰夹在 ±MAX_PITCH 内 */
export function tiltByWheel(pitch: number, deltaY: number): number {
  return clamp(pitch - deltaY * TILT_PER_PIXEL, -MAX_PITCH, MAX_PITCH);
}

/** 滚轮变焦：向上滚拉近（视场角变小），向下滚拉远；按比例变化，夹在 MIN_FOV ~ MAX_FOV */
export function zoomByWheel(fov: number, deltaY: number): number {
  return clamp(fov * Math.exp(deltaY * ZOOM_PER_PIXEL), MIN_FOV, MAX_FOV);
}

/** 按住的键换算成平移意图：WASD 和方向键等价，相反的键抵消；forward 前为正，right 右为正 */
export function moveInput(keys: ReadonlySet<string>): { forward: number; right: number } {
  const has = (...names: string[]) => names.some((name) => keys.has(name));
  return {
    forward: (has("w", "arrowup") ? 1 : 0) - (has("s", "arrowdown") ? 1 : 0),
    right: (has("d", "arrowright") ? 1 : 0) - (has("a", "arrowleft") ? 1 : 0),
  };
}

/**
 * 水平平移：前后沿朝向的水平方向、左右沿它的右手方向，高度不变，抬头低头不影响；
 * 斜着走速度不变。
 * @param distance 这一步走多远（米）
 */
export function walk(
  position: { x: number; y: number; z: number },
  yaw: number,
  input: { forward: number; right: number },
  distance: number,
): { x: number; y: number; z: number } {
  const length = Math.hypot(input.forward, input.right);
  if (length === 0) return { ...position };
  const f = input.forward / length;
  const r = input.right / length;
  const sin = Math.sin(yaw * DEG);
  const cos = Math.cos(yaw * DEG);
  return {
    x: position.x + (sin * f + cos * r) * distance,
    y: position.y,
    z: position.z + (-cos * f + sin * r) * distance,
  };
}

/**
 * 按住平移（导演视角的右键）要抓住的深度：鼠标下的视线落到地面上就取到地面的距离，
 * 碰不到地面（往天上看、平视）取默认深度；夹在 0.5 ~ 60 米。
 */
export function grabDepth(position: { y: number }, direction: readonly number[]): number {
  if (direction[1] < -1e-3) {
    const t = -position.y / direction[1];
    if (t > 0) return clamp(t, MIN_GRAB_DEPTH, MAX_GRAB_DEPTH);
  }
  return DEFAULT_GRAB_DEPTH;
}

/**
 * 按住平移：朝向不变，只挪相机位置，让被抓住的世界点跟着鼠标走（保持它到相机的前向深度不变）。
 * @param pose 按下时的相机位置和朝向
 * @param pinned 被抓住的世界点
 * @param to 现在鼠标的位置（ndc）
 * @returns 新的相机位置
 */
export function panPosition(
  pose: { x: number; y: number; z: number; yaw: number; pitch: number },
  pinned: readonly number[],
  to: { x: number; y: number },
  vfov: number,
  aspect: number,
): { x: number; y: number; z: number } {
  const forward = rotateByLook(pose.yaw, pose.pitch, [0, 0, -1]);
  const depth = dot([pinned[0] - pose.x, pinned[1] - pose.y, pinned[2] - pose.z], forward);
  const ray = rotateByLook(pose.yaw, pose.pitch, pixelRay(to.x, to.y, vfov, aspect));
  const along = dot(ray, forward);
  if (!(along > 1e-6)) return { x: pose.x, y: pose.y, z: pose.z };
  const s = depth / along;
  return { x: pinned[0] - ray[0] * s, y: pinned[1] - ray[1] * s, z: pinned[2] - ray[2] * s };
}
