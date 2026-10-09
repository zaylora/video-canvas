import { useEffect, useRef } from "react";
import { useStoreApi, useUpdateNodeInternals } from "@xyflow/react";

import { createInternalsBatcher } from "@/utils/canvas/internals-batcher";

/** 每张画布（每个 xyflow store）共用一个批次，同一轮里各节点的请求合成一次 */
const batchers = new WeakMap<object, (id: string) => void>();

/**
 * 输入口随所选模型的 schema 增减；口变了必须通知 xyflow 重新测量，否则连到新口上的线会因为
 * 「找不到 handle」被藏起来。
 *
 * 挂载时 xyflow 自己会量一遍，这里不重复通知；之后只在签名变化时排队，并和别的节点合并，
 * 否则 200 个节点各排一个 rAF，每个都让整张画布同步重渲染一遍。
 */
export function useHandleRemeasure(id: string, signature: string) {
  const store = useStoreApi();
  const updateNodeInternals = useUpdateNodeInternals();
  const measured = useRef(signature);
  useEffect(() => {
    if (measured.current === signature) return;
    measured.current = signature;
    let request = batchers.get(store);
    if (!request) {
      request = createInternalsBatcher((ids) => updateNodeInternals(ids));
      batchers.set(store, request);
    }
    request(id);
  }, [id, signature, store, updateNodeInternals]);
}
