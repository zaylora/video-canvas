import { CheckCircle2, CircleAlert, Info, Trash2 } from "lucide-react";

import type { ConfigIssue } from "@/api/admin-ai/type";
import { Button } from "@/components/ui/button";
import { cn } from "@/lib/utils";

/** 右侧结果面板里的一条记录：保存、校验、dry-run、试跑、发布、导入都往这里写 */
export type ResultEntry = {
  id: string;
  title: string;
  tone: "success" | "error" | "info";
  time: number;
  /** 校验问题，path 是 JSON 路径 */
  issues?: ConfigIssue[];
  /** dry-run / 试跑结果等结构化内容 */
  json?: unknown;
  /** 一句补充说明 */
  text?: string;
};

const TONE_ICON = {
  success: CheckCircle2,
  error: CircleAlert,
  info: Info,
} as const;

const TONE_CLASS = {
  success: "text-emerald-600 dark:text-emerald-400",
  error: "text-destructive",
  info: "text-muted-foreground",
} as const;

function Entry({ entry }: { entry: ResultEntry }) {
  const Icon = TONE_ICON[entry.tone];
  return (
    <li className="flex flex-col gap-2 rounded-xl border p-3 text-xs">
      <div className="flex items-start gap-2">
        <Icon className={cn("mt-0.5 size-4 shrink-0", TONE_CLASS[entry.tone])} />
        <div className="min-w-0 flex-1">
          <p className="text-sm font-medium break-words">{entry.title}</p>
          <p className="text-muted-foreground">
            {new Date(entry.time).toLocaleTimeString()}
          </p>
        </div>
      </div>
      {entry.text && <p className="break-words">{entry.text}</p>}
      {entry.issues && entry.issues.length > 0 && (
        <ul className="flex flex-col gap-1">
          {entry.issues.map((issue, index) => (
            <li key={`${issue.path}-${index}`} className="bg-destructive/5 rounded-md px-2 py-1.5">
              <code className="text-destructive font-mono break-all">
                {issue.path || "(根)"}
              </code>
              <span className="ml-1.5 break-words">{issue.message}</span>
            </li>
          ))}
        </ul>
      )}
      {entry.json !== undefined && (
        <pre className="bg-muted max-h-96 overflow-auto rounded-md p-2 font-mono text-[11px] leading-5 break-all whitespace-pre-wrap">
          {JSON.stringify(entry.json, null, 2)}
        </pre>
      )}
    </li>
  );
}

/** 右侧结果面板：最新的在上面 */
export function ResultPanel({
  entries,
  onClear,
}: {
  entries: ResultEntry[];
  onClear: () => void;
}) {
  return (
    <aside className="flex min-h-0 w-full flex-col border-l lg:w-96">
      <div className="flex h-11 shrink-0 items-center justify-between border-b px-3">
        <h2 className="text-sm font-medium">结果</h2>
        <Button variant="ghost" size="xs" disabled={entries.length === 0} onClick={onClear}>
          <Trash2 />
          清空
        </Button>
      </div>
      <div className="min-h-0 flex-1 overflow-y-auto p-3">
        {entries.length === 0 ? (
          <p className="text-muted-foreground text-xs">
            保存、校验、dry-run、试跑的结果会显示在这里。
          </p>
        ) : (
          <ul className="flex flex-col gap-2">
            {entries.map((entry) => (
              <Entry key={entry.id} entry={entry} />
            ))}
          </ul>
        )}
      </div>
    </aside>
  );
}
