import { useCallback, useEffect, useRef, useState } from "react";

import { sha256Hex } from "@/utils/drama/adaptation";

/**
 * 给一组文字算 SHA-256（异步，结果缓存）；返回同步查询函数，还没算完的返回 undefined。
 * 片场用它判断转写稿是否过期：原稿指纹 ≠ 转写时记下的 source_hash。
 */
export function useTextHashes(texts: readonly string[]): (text: string) => string | undefined {
  const cache = useRef(new Map<string, string>());
  const [version, setVersion] = useState(0);
  const joined = texts.join("\u0000");

  useEffect(() => {
    let active = true;
    const missing = [...new Set(texts)].filter((text) => !cache.current.has(text));
    if (missing.length === 0) return;
    void Promise.all(
      missing.map(async (text) => cache.current.set(text, await sha256Hex(text))),
    ).then(() => active && setVersion((v) => v + 1));
    return () => {
      active = false;
    };
    // texts 的内容由 joined 代表，避免每次渲染新数组都重算
    // oxlint-disable-next-line react-hooks/exhaustive-deps
  }, [joined]);

  // 每算完一批换一个函数，依赖它的 useMemo 会跟着重算
  // oxlint-disable-next-line react-hooks/exhaustive-deps
  return useCallback((text: string) => cache.current.get(text), [version]);
}
