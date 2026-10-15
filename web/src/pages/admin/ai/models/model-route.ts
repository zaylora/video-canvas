import { useCallback, useEffect, useMemo, useState } from "react";

/**
 * 模型编辑器“选中了什么”的来源与切换方式。
 * 选中状态放在模型管理面板自己的 state 里（不进 URL：进了 URL 会把搜索、筛选、页码冲掉）；
 * 别的页面要直达某个模型，通过面板的 openModelKey / openTest 属性带进来。
 */
export type ModelRoute = {
  /** 正在编辑的已有模型 key */
  key: string | null;
  /** 是否在新建 */
  isNew: boolean;
  /** 新建时预选的渠道 */
  presetChannel: string | null;
  /** 打开某个模型；test 为 true 时同时打开测试弹窗；replace 不留历史记录（面板里没有历史，保留是为了和调用方的写法一致） */
  openModel: (key: string, options?: { test?: boolean; replace?: boolean }) => void;
  /** 进入新建；presetChannel 是预选的所属渠道（当前渠道） */
  openNew: (options?: { presetChannel?: string }) => void;
  /** 关闭编辑器 */
  close: (options?: { replace?: boolean }) => void;
};

/** 要打开的模型弹窗：已有模型（可同时开测试），或新建（可预选渠道） */
export type ModelDialogOpen = {
  /** 已有模型 key */
  key?: string;
  /** 是否新建 */
  isNew?: boolean;
  /** 新建时预选的渠道 */
  presetChannel?: string;
  /** 打开已有模型时只弹测试弹窗 */
  test?: boolean;
};

/**
 * 模型管理面板用：选中状态放在面板自己的 state 里。
 * @param openModelKey 外部深链要打开的模型 key；变化为非空时打开，用户之后关掉不会再自动打开
 * @param openTest 深链是否直接打开测试弹窗
 * @param onOpenHandled 深链被消费后回调，宿主据此清掉自己保存的深链（避免切走再切回又弹出来）
 * @param presetChannel “新增模型”预选的渠道（当前渠道）
 * @returns route 给编辑器用；test 为 true 表示只弹测试弹窗
 */
export function useModelPanelRoute(
  openModelKey?: string | null,
  openTest?: boolean,
  onOpenHandled?: () => void,
  presetChannel?: string,
): { route: ModelRoute; test: boolean } {
  const [open, setOpen] = useState<ModelDialogOpen | null>(null);

  useEffect(() => {
    if (!openModelKey) return;
    setOpen({ key: openModelKey, test: !!openTest });
    onOpenHandled?.();
    // 只在深链变化时打开；onOpenHandled 的引用不参与
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [openModelKey, openTest]);

  const isNew = !!open?.isNew;
  const key = isNew ? null : (open?.key ?? null);
  const preset = isNew ? (open?.presetChannel ?? null) : null;
  const test = !!open?.test && !isNew;
  const openModel = useCallback<ModelRoute["openModel"]>(
    (next, options) => setOpen({ key: next, test: options?.test }),
    [],
  );
  const openNew = useCallback<ModelRoute["openNew"]>(
    (options) => setOpen({ isNew: true, presetChannel: options?.presetChannel ?? presetChannel }),
    [presetChannel],
  );
  const close = useCallback(() => setOpen(null), []);
  const route = useMemo<ModelRoute>(
    () => ({ key, isNew, presetChannel: preset, openModel, openNew, close }),
    [key, isNew, preset, openModel, openNew, close],
  );
  return { route, test };
}
