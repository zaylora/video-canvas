import { useCallback, useEffect, useState } from "react";
import { useNavigate } from "react-router";

import { createCanvas, deleteCanvas, getCanvasList } from "@/api/canvas";
import { draftStore } from "@/utils/canvas/draft-idb";
import { DRAFT_RETENTION_MS } from "@/utils/canvas/draft-store";
import { removeViewport } from "@/utils/canvas/viewport-store";
import { getCurrentUserId } from "@/utils/storage/user-id";
import type { CanvasListItemDto } from "@/api/canvas/type";
import { rememberCanvasTitle } from "@/utils/canvas/title-cache";
import { openCanvasWindow } from "@/utils/home/canvas-window";
import {
  readCanvasCache,
  removeFromCanvasCache,
  writeCanvasCache,
} from "@/utils/home/canvas-cache";

/** 搜索输入停下多久才发请求（毫秒） */
const SEARCH_DEBOUNCE = 300;

/**
 * 新建一张空画布并在新窗口打开；创建中重复点击会被忽略。侧栏、首页和列表页的新建卡共用。
 * 新窗口要在点击时同步开出来（见 openCanvasWindow），被浏览器拦截就退回当前页跳转。
 */
export function useCreateCanvas() {
  const navigate = useNavigate();
  const [creating, setCreating] = useState(false);

  const create = useCallback(async () => {
    if (creating) return;
    setCreating(true);
    const win = openCanvasWindow();
    try {
      const canvas = await createCanvas();
      const url = `/canvas/${canvas.id}`;
      if (!win.go(url)) navigate(url);
    } catch (error) {
      win.cancel();
      throw error;
    } finally {
      setCreating(false);
    }
  }, [creating, navigate]);

  return { creating, create };
}

/** 一次请求的标识：页大小、关键字、重试次数任一变化就是新的一次 */
const requestKeyOf = (pageSize: number, keyword: string, reloadKey: number) =>
  `${pageSize}|${keyword}|${reloadKey}`;

/** 可用来先渲染的缓存列表；空列表不算，免得有画布的人先闪一下「还没有画布」 */
function seedItems(): CanvasListItemDto[] | null {
  const cached = readCanvasCache(getCurrentUserId());
  return cached && cached.length > 0 ? cached : null;
}

/**
 * 首页与「所有画布」页共用的画布列表：拉列表、按标题搜索（防抖）、新建后跳进画布、删除。
 * 请求失败的提示由拦截器统一弹，这里只记下 error 好让页面显示「重试」。
 * 进页面先用上次拉到的列表渲染（utils/home/canvas-cache），再后台刷新：
 * 首页点「更多」的转场要在新页面第一次渲染就有卡片当终点，不能等接口。
 * @param pageSize 一次拉多少张
 */
export function useCanvasList(pageSize: number) {
  const [items, setItems] = useState<CanvasListItemDto[]>(() => seedItems() ?? []);
  /** 输入框里的原文，立即更新 */
  const [query, setQuery] = useState("");
  /** 防抖后真正拿去请求的关键字 */
  const [keyword, setKeyword] = useState("");
  /** 递增一次就重拉一次（「重试」按钮） */
  const [reloadKey, setReloadKey] = useState(0);
  /** 最近一次请求结束时对应的请求标识和是否失败；标识和当前不一致就是在加载 */
  const [settled, setSettled] = useState<{ key: string; error: boolean } | null>(() =>
    seedItems() ? { key: requestKeyOf(pageSize, "", 0), error: false } : null,
  );
  const { creating, create } = useCreateCanvas();
  /** 正在删除的画布：卡片变半透明、不可点 */
  const [deleting, setDeleting] = useState<ReadonlySet<string>>(new Set());

  const requestKey = requestKeyOf(pageSize, keyword, reloadKey);
  const loading = settled?.key !== requestKey;
  const error = !loading && settled.error;

  useEffect(() => {
    // 打开列表时顺手清理已同步且超过保留期的草稿；没同步的草稿永远不清
    const userId = getCurrentUserId();
    if (userId) void draftStore.purgeExpired(userId, DRAFT_RETENTION_MS);
  }, []);

  useEffect(() => {
    const timer = setTimeout(() => setKeyword(query.trim()), SEARCH_DEBOUNCE);
    return () => clearTimeout(timer);
  }, [query]);

  useEffect(() => {
    let active = true;
    getCanvasList({ page: 1, page_size: pageSize, keyword: keyword || undefined })
      .then((result) => {
        if (!active) return;
        const list = Array.isArray(result?.items) ? result.items : [];
        /** 记下画布名，任务在别处完成时的 toast 要用 */
        for (const item of list) rememberCanvasTitle(item.id, item.title);
        setItems(list);
        /** 带搜索词的结果只是一部分，不当成「最近的列表」缓存 */
        if (!keyword) writeCanvasCache(getCurrentUserId(), list);
        setSettled({ key: requestKey, error: false });
      })
      .catch(() => {
        if (active) setSettled({ key: requestKey, error: true });
      });
    return () => {
      active = false;
    };
  }, [keyword, pageSize, requestKey]);

  const reload = useCallback(() => setReloadKey((key) => key + 1), []);

  const remove = useCallback(async (id: string) => {
    setDeleting((prev) => new Set(prev).add(id));
    try {
      await deleteCanvas(id);
      setItems((prev) => prev.filter((item) => item.id !== id));
      // 画布没了，本机的草稿和视口记录也一起清掉
      const userId = getCurrentUserId();
      removeFromCanvasCache(userId, id);
      if (userId) {
        void draftStore.remove(userId, id);
        removeViewport(userId, id);
      }
    } catch {
      /** 失败提示拦截器已经弹过，这里只把卡片留在原处 */
    } finally {
      setDeleting((prev) => {
        const next = new Set(prev);
        next.delete(id);
        return next;
      });
    }
  }, []);

  return {
    items,
    loading,
    error,
    query,
    setQuery,
    /** 已生效的关键字，空状态文案用 */
    keyword,
    reload,
    creating,
    create,
    deleting,
    remove,
  };
}

export type CanvasListState = ReturnType<typeof useCanvasList>;
