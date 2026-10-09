import { useEffect, useRef, useState } from "react";

import { cn } from "@/lib/utils";

/**
 * 失败提示里的任务 ID：用户点一下复制，发给运维就能在后端日志里按 task_id 查到失败原因。
 * 没有 ID（比如请求没提交成功、任务根本没建出来）时什么都不画。剪贴板不可用时静默不复制，文字本身仍可选中。
 * @param id 任务 ID
 * @param className 追加的样式，画布节点里要带上 nodrag 才不会点一下就拖动节点
 */
export function TaskIdTag({ id, className }: { id?: string | null; className?: string }) {
  const [copied, setCopied] = useState(false);
  const timer = useRef<ReturnType<typeof setTimeout> | null>(null);
  useEffect(
    () => () => {
      if (timer.current) clearTimeout(timer.current);
    },
    [],
  );
  if (!id) return null;

  const copy = async () => {
    try {
      await navigator.clipboard.writeText(id);
    } catch {
      return;
    }
    setCopied(true);
    if (timer.current) clearTimeout(timer.current);
    timer.current = setTimeout(() => setCopied(false), 1500);
  };

  return (
    <button
      type="button"
      title="点击复制任务 ID"
      aria-label={`任务 ID ${id}，点击复制`}
      onClick={() => void copy()}
      className={cn(
        "text-muted-foreground/80 hover:text-foreground grid max-w-full cursor-pointer text-[10px] leading-tight break-all outline-none focus-visible:underline",
        className,
      )}
    >
      {/* 两段文字叠在同一格里，按钮尺寸由较高的那段定；复制后只切换可见性，不改行数，周围的内容不会跟着跳 */}
      <span className={cn("col-start-1 row-start-1", copied && "invisible")}>任务 ID：{id}</span>
      <span aria-hidden className={cn("col-start-1 row-start-1", !copied && "invisible")}>
        已复制任务 ID
      </span>
    </button>
  );
}
