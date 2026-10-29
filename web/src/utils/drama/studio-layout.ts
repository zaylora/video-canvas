/** 片场的横向三栏 */
export type StudioColumn = "library" | "preview" | "editor";

/**
 * 片场此刻显示哪些栏（从左到右）：窗口变窄时先收起资产库（<1280），再收起预览（<1024），编辑区始终在。
 * 拖动分栏只在显示出来的栏之间进行；返回值也用作记住布局时的栏 id 列表。
 * @param width.lg 窗口宽度 ≥ 1024
 * @param width.xl 窗口宽度 ≥ 1280
 */
export function studioColumns(width: { lg: boolean; xl: boolean }): StudioColumn[] {
  const columns: StudioColumn[] = [];
  if (width.xl) columns.push("library");
  if (width.lg) columns.push("preview");
  columns.push("editor");
  return columns;
}
