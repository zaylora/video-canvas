import type { Segment, SegmentTake } from "@/api/drama/type";

/** 按 seq 排序（不改原数组） */
export const sortSegments = (list: readonly Segment[]): Segment[] =>
  [...list].sort((a, b) => a.seq - b.seq);

/**
 * 并入一条服务端快照（WebSocket 推送 / 接口返回）：
 * 同 id 更新；revision 更小的迟到快照忽略；相同 revision 也采用（take 状态变化不改 revision）；
 * 没见过的 id 插入。结果按 seq 排序。
 */
export function mergeSegment(list: readonly Segment[], incoming: Segment): Segment[] {
  const existing = list.find((item) => item.id === incoming.id);
  if (existing && incoming.revision < existing.revision) return [...list];
  return sortSegments([...list.filter((item) => item.id !== incoming.id), incoming]);
}

/** 从列表里拿掉一个片段；其余片段的 seq 不动（以服务端为准） */
export const removeSegmentFrom = (list: readonly Segment[], id: string): Segment[] =>
  list.filter((item) => item.id !== id);

/**
 * 按 ids 的顺序排好并重写 seq（从 0 起连续）；ids 里没有的片段排在最后，ids 里多出来的忽略。
 * 拖动片段条时先在本地换序，再把 ids 交给 PUT /segments/order。
 */
export function applyOrder(list: readonly Segment[], ids: readonly string[]): Segment[] {
  const byId = new Map(list.map((item) => [item.id, item]));
  const picked = ids.flatMap((id) => {
    const item = byId.get(id);
    byId.delete(id);
    return item ? [item] : [];
  });
  const rest = list.filter((item) => byId.has(item.id));
  return [...picked, ...rest].map((item, index) => ({ ...item, seq: index }));
}

export const orderIds = (list: readonly Segment[]): string[] => list.map((item) => item.id);

export const sameOrder = (a: readonly string[], b: readonly string[]): boolean =>
  a.length === b.length && a.every((id, index) => id === b[index]);

/** 片段条上一个片段的最小宽度（像素），再短也要放得下序号和时长 */
const CLIP_MIN = 64;
/** 片段条每秒多宽（像素） */
const CLIP_PER_SECOND = 12;

/** 片段条上的宽度：按时长，有下限 */
export const clipWidth = (seconds: number): number =>
  Math.max(CLIP_MIN, Math.round(seconds * CLIP_PER_SECOND));

/** 各片段时长之和（秒） */
export const totalSeconds = (list: readonly Pick<Segment, "seconds">[]): number =>
  list.reduce((sum, item) => sum + item.seconds, 0);

/** 采用的那条 take；没有采用或找不到为 null */
export const chosenTake = (segment: Segment): SegmentTake | null =>
  (segment.chosenTaskId && segment.takes.find((take) => take.taskId === segment.chosenTaskId)) ||
  null;

/** 能播放的采用视频：采用的那条成功并且有素材地址 */
export function playableTake(segment: Segment): SegmentTake | null {
  const take = chosenTake(segment);
  return take && take.status === "succeeded" && take.asset ? take : null;
}

/** 片段有没有采用的视频 */
export const hasVideo = (segment: Segment): boolean => playableTake(segment) !== null;

/** 片段正在生成视频 */
export const isRunning = (segment: Segment): boolean => segment.status === "running";

/**
 * 点选一条 take 想采用它：ok 可以请求；already 已经是采用的，不重复请求；
 * invalid 只能采用本片段已成功（且有素材）的结果（后端对应 67011）。
 */
export function chooseCheck(segment: Segment, taskId: string): "ok" | "already" | "invalid" {
  const take = segment.takes.find((item) => item.taskId === taskId);
  if (!take || take.status !== "succeeded" || !take.asset) return "invalid";
  return segment.chosenTaskId === taskId ? "already" : "ok";
}

/** 批量生成的对象：没有采用视频、不在生成中、提示词不为空 */
export const batchTargets = (list: readonly Segment[]): Segment[] =>
  list.filter((item) => !hasVideo(item) && !isRunning(item) && item.prompt.trim() !== "");

/** 失败时的说明：最近一条失败 take 的原因；不是 error 状态为 null */
export function latestError(segment: Segment): string | null {
  if (segment.status !== "error") return null;
  const failed = [...segment.takes].reverse().find((take) => take.status === "failed");
  return failed?.errorMessage || "生成失败";
}
