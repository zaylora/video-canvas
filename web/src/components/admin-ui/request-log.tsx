import type { ComponentProps } from "react";
import { ChevronRight } from "lucide-react";

import { FoldableCode } from "@/components/admin-ui/foldable-code";
import { Tag } from "@/components/admin-ui/tag";
import { cn } from "@/lib/utils";
import type { RequestLogEntry } from "@/utils/requests/request-log";

const toText = (value: unknown) =>
  typeof value === "string" ? value : JSON.stringify(value, null, 2);

const formatDuration = (ms: number) => (ms >= 1000 ? `${(ms / 1000).toFixed(1)}s` : `${ms}ms`);

/**
 * 接口请求日志：
 * <RequestLog>{entries.map((entry) => <RequestLogItem key={entry.id} entry={entry} />)}</RequestLog>
 * 每条一行（方法、路径、状态、耗时），点开看请求体与响应体。
 */
function RequestLog({ className, ...props }: ComponentProps<"ol">) {
  return (
    <ol
      data-slot="request-log"
      className={cn("flex flex-col divide-y rounded-lg border", className)}
      {...props}
    />
  );
}

function RequestLogItem({
  entry,
  className,
  ...props
}: ComponentProps<"li"> & { entry: RequestLogEntry }) {
  return (
    <li
      data-slot="request-log-item"
      data-ok={entry.ok}
      className={cn("text-xs", className)}
      {...props}
    >
      <details className="group/log">
        <summary className="hover:bg-muted/50 flex cursor-pointer list-none items-center gap-2 px-2.5 py-2">
          <ChevronRight className="text-muted-foreground size-3.5 shrink-0 transition group-open/log:rotate-90" />
          <Tag mono tone={entry.ok ? "neutral" : "danger"} className="w-12 justify-center">
            {entry.method}
          </Tag>
          <span className="min-w-0 flex-1 truncate font-mono" title={entry.url}>
            {entry.url}
          </span>
          <Tag mono tone={entry.ok ? "success" : "danger"}>
            {entry.status || "ERR"}
            {entry.code !== undefined && entry.code !== 0 && ` · ${entry.code}`}
          </Tag>
          <span className="text-muted-foreground w-12 shrink-0 text-right tabular-nums">
            {formatDuration(entry.duration)}
          </span>
        </summary>
        <div className="flex flex-col gap-2 border-t px-2.5 py-2">
          <p className="text-muted-foreground tabular-nums">
            {new Date(entry.time).toLocaleTimeString()}
          </p>
          {entry.request !== undefined && (
            <div className="flex flex-col gap-1">
              <span className="text-muted-foreground">请求体</span>
              <FoldableCode text={toText(entry.request)} maxChars={800} />
            </div>
          )}
          <div className="flex flex-col gap-1">
            <span className="text-muted-foreground">响应</span>
            <FoldableCode text={toText(entry.response ?? "（空）")} maxChars={800} />
          </div>
        </div>
      </details>
    </li>
  );
}

export { RequestLog, RequestLogItem };
