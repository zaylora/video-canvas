import { useEffect, useState } from "react";

import { getCanvasList } from "@/api/canvas";
import type { CanvasListItemDto } from "@/api/canvas/type";

/**
 * 侧栏「画布」分组用的最近画布：进入外壳时拉一次，失败就当没有。
 * 请求失败的提示由拦截器统一弹，这里不重复提示。
 * @param limit 最多取几张
 * @returns items 按最近更新排序，加载中或失败时为空数组；loading 在首次请求结束前为 true，侧栏据此显示骨架屏
 */
export function useRecentCanvases(limit: number): {
  items: CanvasListItemDto[];
  loading: boolean;
} {
  const [items, setItems] = useState<CanvasListItemDto[]>([]);
  /** 已经请求完成（成功或失败）的 limit；和当前 limit 不一致就是在加载 */
  const [settledLimit, setSettledLimit] = useState<number | null>(null);

  useEffect(() => {
    let active = true;
    getCanvasList({ page: 1, page_size: limit })
      .then((result) => {
        if (active) setItems(Array.isArray(result?.items) ? result.items : []);
      })
      .catch(() => undefined)
      .finally(() => {
        if (active) setSettledLimit(limit);
      });
    return () => {
      active = false;
    };
  }, [limit]);

  return { items, loading: settledLimit !== limit };
}
