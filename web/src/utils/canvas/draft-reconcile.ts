import type { CanvasGraphDto } from "@/api/canvas/type";

import type { Draft } from "./draft-store";

/** 打开画布后，界面要怎么交代这次恢复 */
export type Recovery = { kind: "restored" } | { kind: "conflict"; graph: CanvasGraphDto };

export type Reconciliation =
  | { kind: "none" }
  | { kind: "discard" }
  | { kind: "restore"; graph: CanvasGraphDto }
  | { kind: "conflict"; graph: CanvasGraphDto };

/** 键按字典序排好再序列化：服务端 JSONB 会重排键，不能直接比较字符串 */
export function canonicalJson(value: unknown): string {
  if (Array.isArray(value)) return `[${value.map(canonicalJson).join(",")}]`;
  if (value && typeof value === "object") {
    const record = value as Record<string, unknown>;
    const body = Object.keys(record)
      .sort()
      .filter((key) => record[key] !== undefined)
      .map((key) => `${JSON.stringify(key)}:${canonicalJson(record[key])}`);
    return `{${body.join(",")}}`;
  }
  return JSON.stringify(value) ?? "null";
}

/** 两份图谱内容是否相同；视口是视图状态，不算内容 */
export function sameContent(a: CanvasGraphDto, b: CanvasGraphDto) {
  const strip = ({ viewport: _viewport, ...rest }: CanvasGraphDto) => rest;
  return canonicalJson(strip(a)) === canonicalJson(strip(b));
}

/**
 * 打开画布时对账本地草稿和云端：
 * - 没有草稿、已同步、内容和云端一样：丢弃（内容一样多见于「上传成功了，草稿还没来得及删就关页」）
 * - 基准版本等于云端当前版本：说明云端没人动过，自动恢复并补传
 * - 版本对不上：云端在别处被改过，不能静默覆盖，交给冲突弹窗
 */
export function reconcileDraft({
  draft,
  cloudVersion,
  cloudGraph,
}: {
  draft: Draft | null;
  cloudVersion: number;
  cloudGraph: CanvasGraphDto;
}): Reconciliation {
  if (!draft) return { kind: "none" };
  if (!draft.cloudDirty) return { kind: "discard" };
  if (sameContent(draft.graph, cloudGraph)) return { kind: "discard" };
  if (draft.baseVersion === cloudVersion) return { kind: "restore", graph: draft.graph };
  return { kind: "conflict", graph: draft.graph };
}
