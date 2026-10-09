import { useEffect, useState } from "react";

import { getAsset } from "@/api/asset";

/** 素材 ID -> 地址的请求缓存：同一张参考图出现在很多条记录里时只请求一次；取不到记成 null，不反复重试 */
const cache = new Map<number, Promise<string | null>>();

const urlOf = (id: number) => {
  let pending = cache.get(id);
  if (!pending) {
    pending = getAsset(id)
      .then((asset) => asset.url)
      .catch(() => null);
    cache.set(id, pending);
  }
  return pending;
};

/**
 * 按素材 ID 取地址，用来给记录画参考图缩略图。
 * 取到之前、素材已被清理取不到的位置是 null，调用方自己决定怎么占位。
 * @param ids 素材 ID 列表
 * @returns 与 ids 一一对应的地址
 */
export function useAssetUrls(ids: number[]): Array<string | null> {
  const key = ids.join(",");
  const [state, setState] = useState<{ key: string; urls: Array<string | null> }>({
    key: "",
    urls: [],
  });

  useEffect(() => {
    let active = true;
    void Promise.all(ids.map(urlOf)).then((urls) => {
      if (active) setState({ key, urls });
    });
    return () => {
      active = false;
    };
    // ids 的内容由 key 代表，避免调用方每次渲染传新数组导致重复请求
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [key]);

  return state.key === key ? state.urls : ids.map(() => null);
}
