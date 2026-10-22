import { useCallback, useEffect, useState } from "react";

import { listCharacters } from "@/api/character";
import type { CharacterDto } from "@/api/character/type";
import { useCharactersStore, type CharacterEntry } from "@/store/characters";

/** 角色卡列表的加载状态与操作 */
export type CharacterListState = {
  /** loading 首次加载；ready 已加载；error 失败（请求层已弹提示） */
  status: "loading" | "ready" | "error";
  /** 全部角色卡，更新时间倒序；每用户 ≤ 100 张，不分页，搜索在客户端过滤 */
  items: CharacterDto[];
  /** 重新读取；已有内容时后台刷新，不退回 loading */
  reload: () => void;
};

/**
 * 我的角色卡列表（资产页网格、画布选择框共用）。读到的卡同时写进缓存 store，
 * 所以资产页看过的卡，画布节点不用再请求一次。
 * @param enabled 为 false 时不请求（弹框关闭时用，打开才加载）
 */
export function useCharacterList(enabled = true): CharacterListState {
  const [state, setState] = useState<{
    status: CharacterListState["status"];
    items: CharacterDto[];
  }>({ status: "loading", items: [] });
  const [version, setVersion] = useState(0);

  useEffect(() => {
    if (!enabled) return;
    let active = true;
    listCharacters()
      .then((items) => {
        if (!active) return;
        for (const card of items) useCharactersStore.getState().upsert(card);
        setState({ status: "ready", items });
      })
      .catch(() => {
        // 请求层已弹出全局提示；有旧内容就保留，没有才进失败态
        if (active)
          setState((prev) => (prev.items.length > 0 ? prev : { status: "error", items: [] }));
      });
    return () => {
      active = false;
    };
  }, [enabled, version]);

  const reload = useCallback(() => setVersion((value) => value + 1), []);
  return { ...state, reload };
}

/**
 * 画布节点读一张角色卡（实时引用）：挂载时保证缓存里有且新鲜，之后资产页的改 / 删会直接写进 store，节点自动刷新。
 * @param id 角色卡 ID；undefined（旧数据缺 characterId）时不请求
 * @returns 缓存项；还没开始读时为 undefined
 */
export function useCharacterEntry(id: string | undefined): CharacterEntry | undefined {
  const entry = useCharactersStore((state) => (id ? state.byId[id] : undefined));
  useEffect(() => {
    if (id) void useCharactersStore.getState().ensure([id]);
  }, [id]);
  return entry;
}
