import { useCallback, useEffect, useReducer, useState } from "react";
import { toast } from "sonner";

import { exportAgentRunMarkdown, getAgentRun } from "@/api/admin/agent-run";
import type { AgentRunDetail } from "@/api/admin/agent-run/type.d";
import { copyToClipboard } from "@/utils/admin/clipboard";
import {
  copyRunMarkdown,
  downloadFileName,
  initialRunDrawer,
  isRunNotFound,
  runDetailJson,
  runDrawerReducer,
  saveTextFile,
} from "@/utils/admin/agent-run-detail";

/**
 * 详情抽屉的数据：rid 变化就重新加载，null 表示抽屉已关闭、清理掉旧详情。
 * 换行或重试之后，旧请求晚到的结果会被丢弃。请求错误的全局提示由拦截器弹，404 / 60012 显示「运行不存在」。
 * @param rid 要看的运行（编码串）；null 表示没有
 * @returns 抽屉状态与重试
 */
export function useAgentRunDetail(rid: string | null) {
  const [state, dispatch] = useReducer(runDrawerReducer, initialRunDrawer);
  const [attempt, setAttempt] = useState(0);

  useEffect(() => {
    if (!rid) {
      dispatch({ type: "close" });
      return;
    }
    let stale = false;
    dispatch({ type: "open", rid });
    getAgentRun(rid)
      .then((detail) => !stale && dispatch({ type: "loaded", rid, detail }))
      .catch((error: unknown) => {
        if (!stale) dispatch({ type: "failed", rid, notFound: isRunNotFound(error) });
      });
    return () => {
      stale = true;
    };
  }, [rid, attempt]);

  const retry = useCallback(() => setAttempt((n) => n + 1), []);
  return { state, retry };
}

/**
 * 抽屉操作栏：复制为 Markdown（调导出接口，成功后写剪贴板）、下载 JSON（只序列化已加载的详情，不再请求）
 * @param rid 运行 ID（编码串）；null 表示抽屉没打开
 * @param detail 已加载的详情；还没加载好时两个操作都不做事
 * @returns 复制中的状态与两个操作
 */
export function useRunActions(rid: string | null, detail: AgentRunDetail | null) {
  const [copying, setCopying] = useState(false);

  const copyMarkdown = useCallback(async () => {
    if (!rid) return;
    setCopying(true);
    try {
      const result = await copyRunMarkdown(rid, {
        exportMarkdown: exportAgentRunMarkdown,
        copy: copyToClipboard,
      });
      if (result === "copied") toast.success("已复制 Markdown，与导出内容一致");
      else toast.error("浏览器不允许写入剪贴板，请改用「下载 JSON」");
    } catch {
      // 导出失败：全局提示已经弹了，这里只恢复按钮
    } finally {
      setCopying(false);
    }
  }, [rid]);

  const download = useCallback(() => {
    if (!rid || !detail) return;
    saveTextFile(downloadFileName(rid), runDetailJson(detail));
    toast.success("已下载，工具参数和结果为完整内容");
  }, [rid, detail]);

  return { copying, copyMarkdown, download };
}
