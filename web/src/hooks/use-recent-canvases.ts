import { useEffect, useState } from "react";

import { getCanvasList } from "@/api/canvas";
import type { CanvasListItemDto } from "@/api/canvas/type";

/**
 * 侧栏「画布」分组用的最近画布：进入外壳时拉一次，失败就当没有。
 * 请求失败的提示由拦截器统一弹，这里不重复提示。
 * @param limit 最多取几张
 * @returns 按最近更新排序的画布，加载中或失败时为空数组
 */
export function useRecentCanvases(limit: number): CanvasListItemDto[] {
  const [items, setItems] = useState<CanvasListItemDto[]>([]);

  useEffect(() => {
    let active = true;
    getCanvasList({ page: 1, page_size: limit })
      .then((result) => {
        if (active) setItems(Array.isArray(result?.items) ? result.items : []);
      })
      .catch(() => undefined);
    return () => {
      active = false;
    };
  }, [limit]);

  return items;
}
