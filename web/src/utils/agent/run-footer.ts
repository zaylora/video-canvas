import type { TimelineItem } from "./timeline";

type RunSummaryItem = Extract<TimelineItem, { type: "run-summary" }>;

/** 消息流要画的一项：普通消息，或一轮回答下面的底栏 */
export type FlowEntry =
  | {
      kind: "item";
      key: string;
      item: TimelineItem;
      /** 这段回答的复制按钮挪到了底栏上，它自己不再显示悬停复制 */
      copied: boolean;
    }
  | {
      kind: "footer";
      key: string;
      runId: string;
      /** 这一轮改过画布才有 */
      summary?: RunSummaryItem;
      /** 底栏「复制」复制的内容：这一轮最后一段回答，没有就是空串 */
      copyText: string;
    };

/**
 * 把每轮运行收尾的「改动摘要」和「反馈位」合成回答下面的一行底栏（设计稿 6.12）：
 * 左边改动胶囊，右边复制、👍、👎。复制的是这一轮最后一段回答，往回找到这一轮的用户消息为止，不跨轮。
 */
export function withRunFooters(items: readonly TimelineItem[]): FlowEntry[] {
  const out: FlowEntry[] = [];
  for (const item of items) {
    if (item.type !== "run-summary" && item.type !== "run-feedback") {
      out.push({ kind: "item", key: item.key, item, copied: false });
      continue;
    }
    const last = out.at(-1);
    if (last?.kind === "footer" && last.runId === item.runId) {
      if (item.type === "run-summary") last.summary = item;
      continue;
    }
    let copyText = "";
    for (let i = out.length - 1; i >= 0; i--) {
      const e = out[i];
      if (e.kind !== "item" || e.item.type === "user") break;
      if (e.item.type === "assistant" && e.item.text) {
        copyText = e.item.text;
        e.copied = true;
        break;
      }
    }
    out.push({
      kind: "footer",
      key: `foot-${item.runId}`,
      runId: item.runId,
      summary: item.type === "run-summary" ? item : undefined,
      copyText,
    });
  }
  return out;
}
