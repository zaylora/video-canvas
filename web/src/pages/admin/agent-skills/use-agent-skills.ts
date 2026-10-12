import { useCallback, useEffect, useRef, useState } from "react";
import { toast } from "sonner";

import { listSkills, setSkillActiveVersion, setSkillEnabled } from "@/api/admin/agent-skill";
import type { SkillItem, SkillPage } from "@/api/admin/agent-skill/type.d";
import { replaceSkillRow } from "@/utils/admin/agent-skill";

import { useAliveRef } from "../use-admin";
import { useAdminList } from "../use-admin-list";

/** 状态筛选取值 */
const SKILL_FILTERS = ["status"] as const;

/** “撤销”toast 停留多久（毫秒）：设计稿规定 5 秒内可撤销 */
const UNDO_MS = 5000;

/** 新导入的行高亮多久后清掉（毫秒），免得筛选切换重挂载行时又闪一次 */
const FRESH_MS = 1500;

/**
 * 技能列表（服务端分页 + 搜索 + 状态筛选）与启停：按 URL 里的 q / status / page 请求当前页，
 * 刷新时保留旧数据，失败时保留上一次的数据。启停是对当页那一行的乐观更新，失败回滚；
 * 其它改动（导入、改名、删除）完成后重新拉取当前页。
 * 请求错误的全局提示由拦截器弹，这里只记状态、回滚乐观更新。
 * @returns 列表状态与操作、各种修改方法
 */
export function useAgentSkills() {
  const aliveRef = useAliveRef();
  const list = useAdminList<SkillItem, SkillPage>(SKILL_FILTERS, (query) => listSkills(query));
  const { items: pageItems, state, reload } = list;
  /** 当页的行：本地可以先于服务端改（乐观更新），列表重新拉回来后以服务端为准 */
  const [items, setItems] = useState<SkillItem[]>([]);
  useEffect(() => setItems(pageItems), [pageItems]);
  const status = state;
  /** 正在启停的技能名：开关禁用，防止连点 */
  const [busyNames, setBusyNames] = useState<ReadonlySet<string>>(new Set());
  /** 刚导入的技能名，对应行一次性高亮 */
  const [freshName, setFreshName] = useState<string | null>(null);
  const freshTimer = useRef<ReturnType<typeof setTimeout> | null>(null);

  useEffect(
    () => () => {
      if (freshTimer.current) clearTimeout(freshTimer.current);
    },
    [],
  );

  /** 用最新的视图替换当页里的同一条（乐观更新、设为生效之后）；当页没有这一条就不动 */
  const replaceOne = useCallback((next: SkillItem) => {
    setItems((prev) => replaceSkillRow(prev, next));
  }, []);

  const markFresh = useCallback((name: string) => {
    setFreshName(name);
    if (freshTimer.current) clearTimeout(freshTimer.current);
    freshTimer.current = setTimeout(() => setFreshName(null), FRESH_MS);
  }, []);

  const setBusy = useCallback((name: string, busy: boolean) => {
    setBusyNames((prev) => {
      const next = new Set(prev);
      if (busy) next.add(name);
      else next.delete(name);
      return next;
    });
  }, []);

  /** 启停的请求本体：乐观更新，失败回滚；返回更新后的技能，失败为 null */
  const applyEnabled = useCallback(
    async (item: SkillItem, enabled: boolean): Promise<SkillItem | null> => {
      setBusy(item.name, true);
      replaceOne({ ...item, enabled });
      try {
        const next = await setSkillEnabled(item.name, enabled);
        if (aliveRef.current) replaceOne(next);
        return next;
      } catch {
        // 失败的全局 toast 已弹，开关回到原位
        if (aliveRef.current) replaceOne(item);
        return null;
      } finally {
        if (aliveRef.current) setBusy(item.name, false);
      }
    },
    [aliveRef, replaceOne, setBusy],
  );

  /**
   * 启用 / 停用：成功后 toast 带“撤销”（5 秒内），撤销即切回原状态（不再弹新的撤销）。
   * @returns 成功与否，调用方（弹窗）据此刷新自己的详情
   */
  const toggleEnabled = useCallback(
    async (item: SkillItem, enabled: boolean): Promise<boolean> => {
      const next = await applyEnabled(item, enabled);
      if (!next) return false;
      toast.success(`${enabled ? "已启用" : "已停用"}「${next.title}」`, {
        duration: UNDO_MS,
        action: { label: "撤销", onClick: () => void applyEnabled(next, !enabled) },
      });
      return true;
    },
    [applyEnabled],
  );

  /** 设为生效的请求本体；失败为 null */
  const applyVersion = useCallback(
    async (item: SkillItem, version: number): Promise<SkillItem | null> => {
      try {
        const next = await setSkillActiveVersion(item.name, version);
        if (aliveRef.current) replaceOne(next);
        return next;
      } catch {
        return null;
      }
    },
    [aliveRef, replaceOne],
  );

  /**
   * 设为生效版本（回滚就是把旧版本设为生效）：成功后 toast 带“撤销”，撤销即切回原来的版本。
   * @returns 更新后的技能；失败为 null
   */
  const activateVersion = useCallback(
    async (item: SkillItem, version: number): Promise<SkillItem | null> => {
      const previous = item.active_version;
      const next = await applyVersion(item, version);
      if (!next) return null;
      toast.success(`「${next.title}」已切换到 v${version}`, {
        duration: UNDO_MS,
        action:
          previous === null
            ? undefined
            : { label: "撤销", onClick: () => void applyVersion(next, previous) },
      });
      return next;
    },
    [applyVersion],
  );

  return {
    ...list,
    items,
    status,
    busyNames,
    freshName,
    reload,
    replaceOne,
    markFresh,
    toggleEnabled,
    activateVersion,
  };
}

/** useAgentSkills 的返回值，传给表格与弹窗 */
export type AgentSkillsApi = ReturnType<typeof useAgentSkills>;
