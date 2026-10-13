import { useMemo } from "react";

import { useDialogTarget } from "../../use-dialog-target";

/**
 * 模型编辑器“选中了什么”的来源与切换方式。
 * 模型页和渠道页都放在页面自己的状态里；渠道页从弹窗里新建 / 编辑模型时不用离开渠道页，
 * 关掉弹窗还能回到渠道的模型页签。
 */
export type ModelRoute = {
  /** 正在编辑的已有模型 key */
  key: string | null;
  /** 是否在新建 */
  isNew: boolean;
  /** 新建时带入的导入草稿 id */
  draftId: string | null;
  fromImport: boolean;
  /** 新建时预选的渠道 */
  presetChannel: string | null;
  /** 打开某个模型；test 为 true 时同时打开测试弹窗；replace 不留历史记录 */
  openModel: (key: string, options?: { test?: boolean; replace?: boolean }) => void;
  /** 进入新建 */
  openNew: () => void;
  /** 关闭编辑器 */
  close: (options?: { replace?: boolean }) => void;
};

/** 模型页要打开的弹窗：已有模型（可同时开测试），或新建（可带导入草稿、预选渠道） */
export type ModelDialogOpen = {
  /** 已有模型 key */
  key?: string;
  /** 是否新建 */
  isNew?: boolean;
  /** 新建时带入的导入草稿 id */
  draftId?: string;
  /** 新建是否来自导入 */
  fromImport?: boolean;
  /** 新建时预选的渠道 */
  presetChannel?: string;
  /** 打开已有模型时只弹测试弹窗 */
  test?: boolean;
};

/**
 * 模型页用：选中状态放在页面自己的 state 里，不进 URL
 * （进了 URL 会把类型、搜索、页码筛选冲掉）；别的页面要直达，用 dialogEntry 经路由 state 带过来。
 * @returns route 给编辑器用；test 为 true 表示只弹测试弹窗
 */
export function useModelPageRoute(): { route: ModelRoute; test: boolean } {
  const [open, setOpen] = useDialogTarget<ModelDialogOpen>();
  const isNew = !!open?.isNew;
  const key = isNew ? null : (open?.key ?? null);
  const draftId = isNew ? (open?.draftId ?? null) : null;
  const fromImport = isNew && !!open?.fromImport;
  const presetChannel = isNew ? (open?.presetChannel ?? null) : null;
  const test = !!open?.test && !isNew;
  const route = useMemo<ModelRoute>(
    () => ({
      key,
      isNew,
      draftId,
      fromImport,
      presetChannel,
      openModel: (next, options) => setOpen({ key: next, test: options?.test }),
      openNew: () => setOpen({ isNew: true }),
      close: () => setOpen(null),
    }),
    [key, isNew, draftId, fromImport, presetChannel, setOpen],
  );
  return { route, test };
}
