/** 作品页的范围：全部（画布和短剧混排）、个人画布、个人短剧 */
export type WorksScope = "all" | "mine" | "drama";

const SCOPES: readonly WorksScope[] = ["all", "mine", "drama"];

/**
 * 读地址栏 ?scope=：认得的原样返回；没写、写错或已下线的「协作画布」（shared）都回到个人画布。
 * @param raw 地址栏里的值
 */
export function parseWorksScope(raw: string | null): WorksScope {
  return SCOPES.includes(raw as WorksScope) ? (raw as WorksScope) : "mine";
}

/** 混排后的一项：画布或短剧 */
export type Work<C, D> = { kind: "canvas"; item: C } | { kind: "drama"; item: D };

/**
 * 「全部」里把画布和短剧按更新时间从新到旧混排；时间相同时画布在前，顺序稳定。
 * @param canvases 画布列表
 * @param dramas 短剧列表
 */
export function mixWorks<C extends { updatedAt: string }, D extends { updatedAt: string }>(
  canvases: readonly C[],
  dramas: readonly D[],
): Work<C, D>[] {
  const works: Work<C, D>[] = [
    ...canvases.map((item) => ({ kind: "canvas" as const, item })),
    ...dramas.map((item) => ({ kind: "drama" as const, item })),
  ];
  const time = (work: Work<C, D>) => Date.parse(work.item.updatedAt) || 0;
  // Array.prototype.sort 是稳定排序：时间相同时保持画布在前
  return works.sort((a, b) => time(b) - time(a));
}
