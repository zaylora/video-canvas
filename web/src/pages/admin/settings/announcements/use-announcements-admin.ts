import { useCallback, useEffect, useState } from "react";
import { toast } from "sonner";

import {
  deleteAnnouncement,
  getAnnouncementsAdmin,
  reorderAnnouncements,
  setAnnouncementEnabled,
} from "@/api/admin/announcement";
import type { AnnouncementAdminItem } from "@/api/admin/announcement/type";

import { useAliveRef, type LoadStatus } from "../../use-admin";

/**
 * 公告后台页的数据与操作：进页面取一次；开关、排序先落到界面，请求失败再按服务端重新拉。
 * 「展示中 / 超出上限」这类状态取决于整张表的排名，所以每次写操作成功后都重新拉一遍列表，保证状态是对的。
 * 请求错误的全局提示由拦截器弹，这里只管状态。
 */
export function useAnnouncementsAdmin() {
  const aliveRef = useAliveRef();
  const [items, setItems] = useState<AnnouncementAdminItem[]>([]);
  const [status, setStatus] = useState<LoadStatus>("loading");
  /** 正在保存的公告 ID（开关），对应行半透明、不可操作 */
  const [busyIds, setBusyIds] = useState<ReadonlySet<number>>(new Set());

  const reload = useCallback(async () => {
    try {
      const view = await getAnnouncementsAdmin();
      if (!aliveRef.current) return;
      setItems(view.items);
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

  /** 启用 / 停用 */
  const toggleEnabled = useCallback(
    async (item: AnnouncementAdminItem) => {
      setBusy(item.id, true);
      try {
        await setAnnouncementEnabled(item.id, !item.enabled);
        await reload();
      } catch {
        // 全局 toast 已弹，开关保持原样
      } finally {
        if (aliveRef.current) setBusy(item.id, false);
      }
    },
    [aliveRef, reload],
  );

  /** 删除一条（调用方先确认过） */
  const remove = useCallback(
    async (item: AnnouncementAdminItem) => {
      await deleteAnnouncement(item.id);
      if (!aliveRef.current) return;
      toast("已删除公告");
      await reload();
    },
    [aliveRef, reload],
  );

  /** 拖动排序：先改界面，保存成功后重新拉（排名变了，状态可能跟着变），失败也按服务端重新拉 */
  const reorder = useCallback(
    async (next: AnnouncementAdminItem[]) => {
      setItems(next);
      try {
        await reorderAnnouncements(next.map((item) => item.id));
        if (aliveRef.current) toast("顺序已保存");
      } finally {
        if (aliveRef.current) void reload();
      }
    },
    [aliveRef, reload],
  );

  return { items, status, busyIds, reload, toggleEnabled, remove, reorder };
}
