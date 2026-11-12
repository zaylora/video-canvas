/** 作品页的范围：全部（画布、短剧和营销片混排）、个人画布、个人短剧、个人营销片 */
export type WorksScope = "all" | "mine" | "drama" | "marketing";

const SCOPES: readonly WorksScope[] = ["all", "mine", "drama", "marketing"];

/**
 * 读地址栏 ?scope=：认得的原样返回；没写、写错或已下线的「协作画布」（shared）都回到个人画布。
 * @param raw 地址栏里的值
 */
export function parseWorksScope(raw: string | null): WorksScope {
  return SCOPES.includes(raw as WorksScope) ? (raw as WorksScope) : "mine";
}

/** 混排后的一项：画布、短剧或营销片 */
export type Work<C, D, M = never> =
  | { kind: "canvas"; item: C }
  | { kind: "drama"; item: D }
  | { kind: "marketing"; item: M };

/**
 * 「全部」里把画布、短剧和营销片按更新时间从新到旧混排；时间相同时按画布、短剧、营销片的顺序，顺序稳定。
 * @param canvases 画布列表
 * @param dramas 短剧列表
 * @param projects 营销片列表
 */
export function mixWorks<
  C extends { updatedAt: string },
  D extends { updatedAt: string },
  M extends { updatedAt: string } = never,
>(canvases: readonly C[], dramas: readonly D[], projects: readonly M[] = []): Work<C, D, M>[] {
  const works: Work<C, D, M>[] = [
    ...canvases.map((item) => ({ kind: "canvas" as const, item })),
    ...dramas.map((item) => ({ kind: "drama" as const, item })),
    ...projects.map((item) => ({ kind: "marketing" as const, item })),
  ];
  const time = (work: Work<C, D, M>) => Date.parse(work.item.updatedAt) || 0;
  // Array.prototype.sort 是稳定排序：时间相同时保持画布、短剧、营销片的先后
  return works.sort((a, b) => time(b) - time(a));
}
