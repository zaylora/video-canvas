import type { ProductFacts } from "@/api/marketing/type";

/** 每行一条：拆成数组并去掉空行 */
export const toLines = (text: string): string[] =>
  text
    .split("\n")
    .map((line) => line.trim())
    .filter(Boolean);

/**
 * 把事实卡编辑弹窗里的文本拼回完整结构：卖点按文字对上原来的 proof / source，新增的卖点标为来自描述；
 * 商品名留空保持原名；品类、人群原样保留（弹窗里不改）。
 * @param base 原事实卡
 * @param edit 弹窗里的各字段（多行的每行一条）
 */
export function buildFacts(
  base: ProductFacts,
  edit: { name: string; anchors: string; selling: string; prohibited: string; cta: string },
): ProductFacts {
  return {
    ...base,
    name: edit.name.trim() || base.name,
    anchors: toLines(edit.anchors),
    sellingPoints: toLines(edit.selling).map(
      (text) =>
        base.sellingPoints.find((item) => item.text === text) ?? {
          text,
          proof: "",
          source: "description" as const,
        },
    ),
    prohibited: toLines(edit.prohibited),
    cta: edit.cta.trim(),
  };
}
