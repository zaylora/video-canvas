import type { ReactNode } from "react";

import { cn } from "@/lib/utils";
import { documentLabel, formatFileSize } from "@/utils/agent/document-files";

/** 卡片第二行：「TXT · 11.41KB」；不知道大小时只有类型 */
export function fileMeta(name: string, size?: number): string {
  const label = documentLabel(name);
  return size === undefined ? label : `${label} · ${formatFileSize(size)}`;
}

/**
 * 文档附件的文件卡片：左边带 TXT / MD 字样的文件图标，右边两行（文件名、类型和大小）。
 * 输入框托盘里（可移除、有上传状态）和聊天记录里（可点开）共用：状态和操作由调用方放进 children / leading / trailing。
 */
export function FileCard({
  name,
  size,
  className,
  leading,
  children,
}: {
  name: string;
  size?: number;
  className?: string;
  /** 盖在文件图标上的东西（上传进度环） */
  leading?: ReactNode;
  /** 绝对定位在卡片上的操作（× 移除、↻ 重试） */
  children?: ReactNode;
}) {
  return (
    <div
      title={name}
      className={cn(
        "bg-foreground/6 border-chrome-border relative flex h-14 max-w-[240px] min-w-[150px] shrink-0 items-center gap-2.5 rounded-xl border pr-7 pl-2.5",
        className,
      )}
    >
      <span
        aria-hidden
        className="bg-status-running/15 text-status-running relative grid h-9 w-8 shrink-0 place-items-center rounded-md text-[9.5px] font-bold tracking-wide"
      >
        {documentLabel(name)}
        {leading}
      </span>
      <span className="min-w-0">
        <span className="block truncate text-[13px] leading-tight font-medium">{name}</span>
        <span className="text-muted-foreground block truncate text-xs leading-snug">
          {fileMeta(name, size)}
        </span>
      </span>
      {children}
    </div>
  );
}
