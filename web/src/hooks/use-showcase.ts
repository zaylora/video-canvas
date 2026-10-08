import { useEffect, useState } from "react";

import { getShowcase } from "@/api/showcase";
import type { ShowcaseDto } from "@/api/showcase/type";

/**
 * 登录页展示：进页面取一次公开配置。
 * 取不到（网络、后端旧版本）就是 null，登录页回落到默认渐变背景，不影响登录。
 * @returns 设置和启用的作品；加载中或失败为 null
 */
export function useShowcase(): ShowcaseDto | null {
  const [data, setData] = useState<ShowcaseDto | null>(null);

  useEffect(() => {
    let alive = true;
    getShowcase()
      .then((value) => alive && setData(value))
      .catch(() => undefined);
    return () => {
      alive = false;
    };
  }, []);

  return data;
}
