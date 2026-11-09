/** 桌宠素材的加载状态：off 是隐藏档不加载，failed 也包含「等太久」 */
export type PetAssetsStatus = "off" | "loading" | "ready" | "failed";

/** 素材等多久还没到，就先让胶囊顶上（之后到了仍然会换成桌宠） */
export const PET_LOAD_TIMEOUT_MS = 4000;

/** 由加载进展算出状态；素材到位优先于失败和超时 */
export function petAssetsStatus(input: {
  enabled: boolean;
  ready: boolean;
  failed: boolean;
  timedOut: boolean;
}): PetAssetsStatus {
  if (!input.enabled) return "off";
  if (input.ready) return "ready";
  if (input.failed || input.timedOut) return "failed";
  return "loading";
}

/**
 * 右下角怎么摆：加载中和就绪都不画胶囊、给桌宠留位（统计条让出 96px），
 * 这样桌宠出现时页面不跳；失败和隐藏档退回胶囊。
 */
export function cornerLayout(status: PetAssetsStatus): {
  showLauncher: boolean;
  reserveForPet: boolean;
} {
  const petSlot = status === "loading" || status === "ready";
  return { showLauncher: !petSlot, reserveForPet: petSlot };
}
