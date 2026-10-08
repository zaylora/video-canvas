import { useCallback, useEffect, useMemo, useState } from "react";
import { toast } from "sonner";

import {
  createShowcaseItem,
  deleteShowcaseItem,
  getShowcaseAdmin,
  reorderShowcase,
  updateShowcaseItem,
  updateShowcaseSettings,
} from "@/api/admin/showcase";
import type {
  ShowcaseAdminItem,
  ShowcaseLibraryItem,
  ShowcaseAdminView,
  ShowcaseSettings,
} from "@/api/admin/showcase/type";
import type { ShowcaseItemDto } from "@/api/showcase/type";

import { useAliveRef, type LoadStatus } from "../../use-admin";

/**
 * 后台作品转成登录页播放器吃的结构：预览和登录页用同一个播放器，所以字段要对齐
 * @param item 后台作品
 */
export const toPlayerItem = (item: ShowcaseAdminItem): ShowcaseItemDto => ({
  id: String(item.id),
  videoUrl: item.video_url,
  posterUrl: item.poster_url || null,
  prompt: item.prompt,
  modelLabel: item.model_label,
  startSec: item.start_sec,
  width: item.width > 0 ? item.width : null,
  height: item.height > 0 ? item.height : null,
  byteSize: item.byte_size > 0 ? item.byte_size : null,
});

/**
 * 登录页展示后台页的数据与操作：进页面取一次；改动先落到界面，请求失败再回滚到服务端的样子。
 * 请求错误的全局提示由拦截器弹，这里只管状态。
 */
export function useShowcaseAdmin() {
  const aliveRef = useAliveRef();
  const [view, setView] = useState<ShowcaseAdminView | null>(null);
  const [status, setStatus] = useState<LoadStatus>("loading");
  /** 正在保存的作品 ID（开关、删除），对应行半透明、不可操作 */
  const [busyIds, setBusyIds] = useState<ReadonlySet<number>>(new Set());

  const reload = useCallback(async () => {
    try {
      const value = await getShowcaseAdmin();
      if (!aliveRef.current) return;
      setView(value);
      setStatus("ready");
    } catch {
      if (aliveRef.current) setStatus((prev) => (prev === "ready" ? prev : "error"));
    }
  }, [aliveRef]);

  useEffect(() => {
    void reload();
  }, [reload]);

  const setBusy = (id: number, busy: boolean) =>
    setBusyIds((prev) => {
      const next = new Set(prev);
      if (busy) next.add(id);
      else next.delete(id);
      return next;
    });

  /** 新增或修改一条作品后，把服务端返回的结果放回列表（新增的追加到末尾） */
  const upsert = useCallback((item: ShowcaseAdminItem) => {
    setView((prev) => {
      if (!prev) return prev;
      const exists = prev.items.some((entry) => entry.id === item.id);
      return {
        ...prev,
        items: exists
          ? prev.items.map((entry) => (entry.id === item.id ? item : entry))
          : [...prev.items, item],
      };
    });
  }, []);

  /** 启用 / 停用：停用的保留在列表里，只是不参与轮播 */
  const toggleEnabled = useCallback(
    async (item: ShowcaseAdminItem) => {
      setBusy(item.id, true);
      try {
        upsert(await updateShowcaseItem(item.id, { enabled: !item.enabled }));
      } catch {
        // 全局 toast 已弹，开关保持原样
      } finally {
        if (aliveRef.current) setBusy(item.id, false);
      }
    },
    [aliveRef, upsert],
  );

  /** 重新创建一条作品并放回原来的位置：「撤销移除」用，素材和文案都还在，只是条目重新建 */
  const restore = useCallback(
    async (item: ShowcaseAdminItem, index: number) => {
      const created = await createShowcaseItem({
        asset_id: item.asset_id,
        poster_asset_id: item.poster_asset_id,
        prompt: item.prompt,
        model_label: item.model_label,
        start_sec: item.start_sec,
        enabled: item.enabled,
      });
      if (!aliveRef.current) return;
      /** 新条目默认排在末尾，插回原位后再保存一次顺序 */
      setView((prev) => {
        if (!prev) return prev;
        const items = [...prev.items, created];
        const [last] = items.splice(items.length - 1, 1);
        items.splice(Math.min(index, items.length), 0, last);
        void reorderShowcase(items.map((entry) => entry.id)).catch(() => void reload());
        return { ...prev, items };
      });
      toast.success("已撤销移除");
    },
    [aliveRef, reload],
  );

  /** 从列表里移除这条作品（不删素材）。不弹确认：toast 里可以撤销 */
  const remove = useCallback(
    async (item: ShowcaseAdminItem) => {
      const index = view?.items.findIndex((entry) => entry.id === item.id) ?? -1;
      setBusy(item.id, true);
      try {
        await deleteShowcaseItem(item.id);
        if (!aliveRef.current) return;
        setView((prev) =>
          prev ? { ...prev, items: prev.items.filter((entry) => entry.id !== item.id) } : prev,
        );
        toast("已从片单移除", {
          action: {
            label: "撤销",
            onClick: () => void restore(item, Math.max(index, 0)).catch(() => undefined),
          },
        });
      } catch {
        // 全局 toast 已弹，条目留在原处
      } finally {
        if (aliveRef.current) setBusy(item.id, false);
      }
    },
    [aliveRef, restore, view],
  );

  /** 从素材库批量添加：逐条创建（顺序决定排序），任何一条失败就停，已成功的保留 */
  const addFromLibrary = useCallback(
    async (entries: ShowcaseLibraryItem[]) => {
      let added = 0;
      try {
        for (const entry of entries) {
          upsert(
            await createShowcaseItem({
              asset_id: entry.asset_id,
              prompt: entry.prompt,
              model_label: entry.model_label,
            }),
          );
          added += 1;
        }
      } finally {
        if (added > 0) toast.success(`已添加 ${added} 个作品，提示词已从生成记录带入`);
      }
    },
    [upsert],
  );

  /** 拖动排序：先改界面，保存失败就按服务端重新拉一遍 */
  const reorder = useCallback(
    async (items: ShowcaseAdminItem[]) => {
      setView((prev) => (prev ? { ...prev, items } : prev));
      try {
        await reorderShowcase(items.map((item) => item.id));
        if (aliveRef.current) toast("顺序已保存");
      } catch {
        if (aliveRef.current) void reload();
      }
    },
    [aliveRef, reload],
  );

  /** 保存轮播设置：先改界面，失败回滚到修改前 */
  const settings = view?.settings;
  const saveSettings = useCallback(
    async (next: ShowcaseSettings) => {
      setView((prev) => (prev ? { ...prev, settings: next } : prev));
      try {
        const saved = await updateShowcaseSettings(next);
        if (aliveRef.current) setView((prev) => (prev ? { ...prev, settings: saved } : prev));
      } catch {
        if (aliveRef.current && settings) {
          setView((prev) => (prev ? { ...prev, settings } : prev));
        }
      }
    },
    [aliveRef, settings],
  );

  /** 启用的作品，给预览播放器 */
  const playerItems = useMemo(
    () => (view?.items ?? []).filter((item) => item.enabled).map(toPlayerItem),
    [view],
  );

  return {
    view,
    status,
    busyIds,
    reload,
    upsert,
    toggleEnabled,
    remove,
    addFromLibrary,
    reorder,
    saveSettings,
    playerItems,
  };
}
