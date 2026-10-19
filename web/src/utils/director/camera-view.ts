import type { DirectorScene } from "@/types";

/** 主视口看哪里：导演视角（自由环绕）或机位视角（站在当前机位里看） */
export type ViewMode = "director" | "camera";

/** 机位俯仰的最大角度（度）：再往上就接近正上 / 正下，偏航失去意义 */
export const MAX_PITCH = 85;

const RAD = Math.PI / 180;

/**
 * 机位视角下，主视口里实际用的竖直视场角。
 * 画面按机位的画幅在视口里居中放到最大：视口比画幅宽，竖直视场角照用（左右留白）；
 * 视口比画幅窄，画幅按宽度顶满，竖直视场角要放大，才能让画幅的左右边刚好落在视口两边。
 * @param fov 机位的竖直视场角（度）
 * @param ratio 机位画幅的宽高比
 * @param viewAspect 视口的宽高比；还没量出来（无效）时原样返回
 */
export function viewFov(fov: number, ratio: number, viewAspect: number): number {
  if (!(viewAspect > 0) || viewAspect >= ratio) return fov;
  return (2 * Math.atan((Math.tan((fov / 2) * RAD) * ratio) / viewAspect)) / RAD;
}

/** 偏航角收回 [-180, 180)，方向不变 */
export const wrapYaw = (yaw: number) => ((((yaw + 180) % 360) + 360) % 360) - 180;

/**
 * 鼠标在机位视角里拖动 (dx, dy) 像素后的机位朝向。
 * 和导演视角里的拖动手感一致：画面跟着手走，向右拖机位向左转、向下拖机位向上抬。
 * 每像素转 视场角 / 视口高度 度，所以画面里的东西和鼠标 1:1 跟手。
 * @param pose 拖动前的偏航、俯仰（度）
 * @param fov 此刻视口实际用的竖直视场角（度），见 viewFov
 * @param viewHeight 视口高度（像素）；无效时不动
 */
export function lookBy(
  pose: { yaw: number; pitch: number },
  dx: number,
  dy: number,
  fov: number,
  viewHeight: number,
): { yaw: number; pitch: number } {
  if (!(viewHeight > 0)) return { yaw: pose.yaw, pitch: pose.pitch };
  const perPixel = fov / viewHeight;
  return {
    yaw: wrapYaw(pose.yaw - dx * perPixel),
    pitch: Math.min(MAX_PITCH, Math.max(-MAX_PITCH, pose.pitch + dy * perPixel)),
  };
}

/**
 * 机位视角下正在看的机位 id；不是机位视角、没有场景、没有机位或当前机位不存在时是 null（按导演视角处理）。
 */
export function activeCameraOf(scene: DirectorScene | null, mode: ViewMode): string | null {
  if (mode !== "camera" || !scene) return null;
  return scene.cameras.find((c) => c.id === scene.activeCameraId)?.id ?? null;
}
