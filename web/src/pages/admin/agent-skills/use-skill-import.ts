import { useCallback, useEffect, useRef, useState } from "react";
import { toast } from "sonner";

import { confirmImport, discardImport, importSkill } from "@/api/admin/agent-skill";
import type { SkillImportView, SkillItem } from "@/api/admin/agent-skill/type.d";
import { errorMessage } from "@/utils/admin/errors";
import {
  confirmFailureNotice,
  importReducer,
  initialImportState,
  preparePick,
  type ImportAction,
  type ImportState,
} from "@/utils/admin/agent-skill-import";

/** 选中的文件及其相对路径 */
export type PickedItem = { file: File; path: string };

/** 导入成功后交给页面的结果 */
export type ImportedResult = {
  /** 入库后的技能 */
  item: SkillItem;
  /** 确认前的预检结果（用来写“新技能 / 新版本”的 toast） */
  view: SkillImportView;
};

/** 上传界面上显示的名字：单个文件用文件名，文件夹用外层目录名 */
function pickLabel(items: readonly PickedItem[]): string {
  const first = items[0];
  if (!first) return "";
  const path = first.path.replace(/^(\.?\/)+/, "");
  return items.length === 1 && !path.includes("/") ? first.file.name : path.split("/")[0];
}

/**
 * 导入状态机的副作用层：选择 → 上传预检 → 确认。状态转移在 utils 的 importReducer 里（纯函数、有单测），
 * 这里只负责发请求、取消上传、丢弃暂存。
 * 请求错误的全局 toast 由拦截器弹；这里再把原因留在第 1 步的拖放区里（设计稿要求就地保留）。
 * @param onImported 确认成功后回调（页面据此刷新列表、关闭对话框）
 * @returns 状态与各动作
 */
export function useSkillImport(onImported: (result: ImportedResult) => void) {
  const [state, setState] = useState<ImportState>(initialImportState);
  /** 同步镜像：异步回调里要读到最新状态，不能等下一次渲染 */
  const stateRef = useRef<ImportState>(initialImportState);
  /** 请求序号：取消或重选之后，旧请求返回的结果直接丢掉 */
  const ticket = useRef(0);
  const controller = useRef<AbortController | null>(null);
  const onImportedRef = useRef(onImported);
  useEffect(() => {
    onImportedRef.current = onImported;
  }, [onImported]);

  const dispatch = useCallback((action: ImportAction) => {
    stateRef.current = importReducer(stateRef.current, action);
    setState(stateRef.current);
  }, []);

  /** 丢弃当前结果页对应的暂存包（不等结果，失败也不打扰） */
  const discardStaged = useCallback(() => {
    const current = stateRef.current;
    if (current.step === "review" && !current.confirming) {
      void discardImport(current.view.id).catch(() => {});
    }
  }, []);

  useEffect(
    () => () => {
      controller.current?.abort();
    },
    [],
  );

  /**
   * 开始一次导入：先在前端按限额拦，通过了才上传。
   * 在结果页再选新文件会丢弃旧暂存；校验不通过时保留当前结果页，只弹提示。
   */
  const begin = useCallback(
    async (items: readonly PickedItem[]) => {
      const current = stateRef.current;
      if (current.step === "uploading" || (current.step === "review" && current.confirming)) return;
      const prepared = preparePick(items);
      if (!prepared.ok) {
        const message = prepared.hint ? `${prepared.error}。${prepared.hint}` : prepared.error;
        if (current.step === "review") toast.error(message);
        else dispatch({ type: "rejected", message });
        return;
      }
      discardStaged();
      dispatch({ type: "start", name: pickLabel(items) });
      const mine = ++ticket.current;
      const abort = new AbortController();
      controller.current = abort;
      try {
        const view = await importSkill(prepared.input, {
          signal: abort.signal,
          onProgress: (ratio) => {
            if (mine === ticket.current) dispatch({ type: "progress", ratio });
          },
        });
        if (mine !== ticket.current) {
          // 用户已取消或重选：这份结果没人要了，别让暂存留着
          if (view.id) void discardImport(view.id).catch(() => {});
          return;
        }
        dispatch({ type: "uploaded", view });
      } catch (error) {
        if (mine !== ticket.current || abort.signal.aborted) return;
        dispatch({ type: "failed", message: errorMessage(error, "上传失败，请重试") });
      }
    },
    [discardStaged, dispatch],
  );

  /** 取消上传 */
  const cancel = useCallback(() => {
    ticket.current += 1;
    controller.current?.abort();
    dispatch({ type: "cancelled" });
  }, [dispatch]);

  /** 重新选择：丢弃暂存，回到第 1 步 */
  const reselect = useCallback(() => {
    if (stateRef.current.step !== "review" || stateRef.current.confirming) return;
    discardStaged();
    dispatch({ type: "reselect" });
  }, [discardStaged, dispatch]);

  /** 确认导入 */
  const confirm = useCallback(async () => {
    const current = stateRef.current;
    if (current.step !== "review" || current.confirming || !current.view.can_confirm) return;
    const view = current.view;
    dispatch({ type: "confirm" });
    try {
      const item = await confirmImport(view.id);
      // 成功后不改状态：对话框正在退出，内容保持原样，退出动画播完由页面调 reset
      onImportedRef.current({ item, view });
    } catch (error) {
      dispatch({ type: "confirmFailed", message: confirmFailureNotice(error) });
    }
  }, [dispatch]);

  /**
   * 关闭对话框：取消上传、丢弃暂存。确认中不允许关，返回 false。
   * 状态不在这里重置：退出动画还在播，内容不该先闪回第 1 步；动画播完再调 reset
   */
  const close = useCallback((): boolean => {
    const current = stateRef.current;
    if (current.step === "review" && current.confirming) return false;
    ticket.current += 1;
    controller.current?.abort();
    discardStaged();
    return true;
  }, [discardStaged]);

  /** 回到初始状态：对话框退出动画播完后，或再次打开之前调用 */
  const reset = useCallback(() => dispatch({ type: "reset" }), [dispatch]);

  return { state, begin, cancel, reselect, confirm, close, reset };
}
