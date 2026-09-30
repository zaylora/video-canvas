import { CheckCircle2, CircleAlert, Info, Loader2, Trash2 } from "lucide-react";

import type { ConfigIssue, TraceStep } from "@/api/admin-ai/type";
import type { TaskView } from "@/api/generation-task/type";
import { Button } from "@/components/ui/button";
import { Tabs, TabsList, TabsTrigger } from "@/components/ui/tabs";
import { cn } from "@/lib/utils";
import { isTerminalStatus } from "@/utils/tasks/status";

import { FoldableCode, Notice, Tag } from "./shared";
import { TraceTimeline } from "./trace/trace-timeline";

/** “问题”标签里的一条记录：保存、校验、发布、回滚、切换表单失败等都往这里写 */
export type ResultEntry = {
  /** 列表 key */
  id: string;
  /** 标题 */
  title: string;
  /** 语气 */
  tone: "success" | "error" | "info";
  /** 产生时间戳（毫秒） */
  time: number;
  /** 校验问题，path 是 JSON 路径，可点击定位 */
  issues?: ConfigIssue[];
  /** 一句补充说明 */
  text?: string;
};

/** dry-run 的结果 */
export type DryRunState = {
  /** 产生时间戳（毫秒） */
  time: number;
  /** 插件返回的请求描述（已校验、Key 脱敏） */
  json?: unknown;
  /** 失败时的就地说明（例如 runner 不可用） */
  error?: string;
};

/** 试跑的进展 */
export type RunState = {
  /** 试跑任务 ID */
  taskId: number | string;
  /** 最近一次拿到的任务视图 */
  view: TaskView;
  /** 补充说明（查询失败等） */
  note?: string;
  /** 轮询超过上限仍未结束：不标失败，提示稍后再看 */
  timedOut: boolean;
};

/** 追踪的加载状态 */
export type TraceState = {
  /** 追踪所属的试跑任务 */
  taskId: number | string;
  /** 加载中 / 已就绪 / 失败 */
  status: "loading" | "ready" | "error";
  /** 追踪步骤 */
  steps: TraceStep[];
};

/** 结果面板的四个标签 */
export type ResultTabId = "issues" | "dry-run" | "run" | "trace";

const TABS: Array<{ id: ResultTabId; label: string }> = [
  { id: "issues", label: "问题" },
  { id: "dry-run", label: "请求描述" },
  { id: "run", label: "试跑" },
  { id: "trace", label: "追踪" },
];

const TONE_ICON = { success: CheckCircle2, error: CircleAlert, info: Info } as const;
const TONE_CLASS = {
  success: "text-emerald-600 dark:text-emerald-400",
  error: "text-destructive",
  info: "text-muted-foreground",
} as const;

function Entry({ entry, onLocate }: { entry: ResultEntry; onLocate: (path: string) => void }) {
  const Icon = TONE_ICON[entry.tone];
  return (
    <li className="flex flex-col gap-2 rounded-xl border p-3 text-xs">
      <div className="flex items-start gap-2">
        <Icon className={cn("mt-0.5 size-4 shrink-0", TONE_CLASS[entry.tone])} />
        <div className="min-w-0 flex-1">
          <p className="text-sm font-medium break-words">{entry.title}</p>
          <p className="text-muted-foreground">{new Date(entry.time).toLocaleTimeString()}</p>
        </div>
      </div>
      {entry.text && <p className="break-words">{entry.text}</p>}
      {entry.issues && entry.issues.length > 0 && (
        <ul className="flex flex-col gap-1">
          {entry.issues.map((issue, index) => (
            <li key={`${issue.path}-${index}`}>
              <button
                type="button"
                className="bg-destructive/5 hover:bg-destructive/10 w-full rounded-md px-2 py-1.5 text-left"
                title="定位到对应字段"
                onClick={() => onLocate(issue.path)}
              >
                <code className="text-destructive font-mono break-all">{issue.path || "(根)"}</code>
                <span className="ml-1.5 break-words">{issue.message}</span>
              </button>
            </li>
          ))}
        </ul>
      )}
    </li>
  );
}

function RunView({ run }: { run: RunState }) {
  const { view } = run;
  const terminal = isTerminalStatus(view.status);
  const ok = view.status === "succeeded";
  const progress = typeof view.progress === "number" ? Math.round(view.progress * 100) : null;
  return (
    <div className="flex flex-col gap-3">
      <div className="flex flex-wrap items-center gap-2">
        <h3 className="text-sm font-medium">试跑任务 #{run.taskId}</h3>
        <Tag tone={!terminal ? "info" : ok ? "success" : "danger"}>
          {!terminal && <Loader2 className="size-3 animate-spin" />}
          {view.status}
        </Tag>
      </div>
      {!terminal && (
        <>
          <p className="text-muted-foreground text-xs">
            真实调用平台，不扣用户积分。每 3 秒刷新一次状态。
          </p>
          {progress !== null && (
            <div
              className="bg-muted h-1.5 overflow-hidden rounded-full"
              role="progressbar"
              aria-valuenow={progress}
            >
              <div
                className="bg-primary h-full transition-[width]"
                style={{ width: `${progress}%` }}
              />
            </div>
          )}
        </>
      )}
      {terminal && !ok && (
        <Notice tone="danger" title="试跑失败">
          {view.error_message || "上游没有返回原因，看“追踪”标签定位失败的那一步。"}
        </Notice>
      )}
      {terminal && ok && (
        <Notice tone="success" title="试跑成功">
          {view.outputs?.length ? `产出 ${view.outputs.length} 个结果。` : "没有产出文件。"}
        </Notice>
      )}
      {ok && view.outputs && view.outputs.length > 0 && (
        <ul className="flex flex-col gap-1 text-xs">
          {view.outputs.map((output, index) => (
            <li key={index} className="bg-muted rounded-md px-2 py-1.5 font-mono break-all">
              {output.media_type} · {output.url ?? output.text ?? ""}
            </li>
          ))}
        </ul>
      )}
      {run.timedOut && (
        <Notice tone="info">试跑仍在进行，可稍后在任务里查看。这不代表失败。</Notice>
      )}
      {run.note && <Notice tone="warning">{run.note}</Notice>}
      <details>
        <summary className="text-primary cursor-pointer text-xs hover:underline">
          任务原始数据
        </summary>
        <FoldableCode className="mt-2" text={JSON.stringify(view, null, 2)} />
      </details>
    </div>
  );
}

/**
 * 右侧结果面板：标签页化（问题 / 请求描述 / 试跑 / 追踪）。
 * dry-run 的标题写“请求描述”而不是“最终请求”：后端返回的是插件给出的请求描述，
 * 并不含宿主注入后的鉴权头。试跑结束后由页面把标签切到“追踪”。
 * @param onLocate 点击问题条目，定位到 JSON 行或表单字段
 * @param onRefreshTrace 刷新追踪
 */
export function ResultPanel({
  tab,
  onTabChange,
  entries,
  dryRun,
  run,
  trace,
  onClear,
  onLocate,
  onRefreshTrace,
}: {
  tab: ResultTabId;
  onTabChange: (tab: ResultTabId) => void;
  entries: ResultEntry[];
  dryRun: DryRunState | null;
  run: RunState | null;
  trace: TraceState | null;
  onClear: () => void;
  onLocate: (path: string) => void;
  onRefreshTrace: () => void;
}) {
  const hasAnything = entries.length > 0 || !!dryRun || !!run || !!trace;
  const badge: Record<ResultTabId, number | null> = {
    issues: entries.length || null,
    "dry-run": dryRun ? 1 : null,
    run: run ? 1 : null,
    trace: trace && trace.status === "ready" ? trace.steps.length : null,
  };
  return (
    <aside className="flex min-h-96 min-w-0 flex-col border-t lg:min-h-0 lg:w-96 lg:shrink-0 lg:border-t-0 lg:border-l">
      <div className="flex h-11 shrink-0 items-center justify-between gap-2 border-b px-3">
        <h2 className="text-sm font-medium">结果</h2>
        <Button variant="ghost" size="xs" disabled={!hasAnything} onClick={onClear}>
          <Trash2 />
          清空
        </Button>
      </div>
      <Tabs
        value={tab}
        onValueChange={(value) => onTabChange(value as ResultTabId)}
        className="min-h-0 flex-1 gap-0"
      >
        <TabsList className="mx-3 mt-2 w-auto self-stretch">
          {TABS.map((item) => (
            <TabsTrigger key={item.id} value={item.id}>
              {item.label}
              {badge[item.id] !== null && (
                <span className="text-muted-foreground text-[11px]">{badge[item.id]}</span>
              )}
            </TabsTrigger>
          ))}
        </TabsList>
        <div
          className="min-h-0 flex-1 overflow-y-auto p-3"
          role="tabpanel"
          aria-label={TABS.find((t) => t.id === tab)?.label}
        >
          {tab === "issues" &&
            (entries.length === 0 ? (
              <p className="text-muted-foreground text-xs">
                保存、校验、发布的结果与问题会显示在这里。
              </p>
            ) : (
              <ul className="flex flex-col gap-2">
                {entries.map((entry) => (
                  <Entry key={entry.id} entry={entry} onLocate={onLocate} />
                ))}
              </ul>
            ))}

          {tab === "dry-run" &&
            (!dryRun ? (
              <p className="text-muted-foreground text-xs">
                点“dry-run”查看插件返回的请求描述：已校验、鉴权头脱敏，不会真正发送。
              </p>
            ) : dryRun.error ? (
              <Notice tone="danger" title="dry-run 没有完成">
                {dryRun.error}
              </Notice>
            ) : (
              <div className="flex flex-col gap-2">
                <h3 className="text-sm font-medium">请求描述</h3>
                <p className="text-muted-foreground text-xs">
                  插件返回的请求描述（已校验，Key 与鉴权头脱敏），未发送。
                  {new Date(dryRun.time).toLocaleTimeString()}
                </p>
                <FoldableCode defaultOpen text={JSON.stringify(dryRun.json, null, 2)} />
              </div>
            ))}

          {tab === "run" &&
            (!run ? (
              <p className="text-muted-foreground text-xs">
                点“试跑”真实调用一次上游，不扣用户积分；结束后自动切到“追踪”。
              </p>
            ) : (
              <RunView run={run} />
            ))}

          {tab === "trace" &&
            (!trace ? (
              <p className="text-muted-foreground text-xs">
                试跑完成后，这里会显示每次钩子与每次 HTTP 的时间线。
              </p>
            ) : trace.status === "error" ? (
              <Notice tone="warning" title="没能取到追踪">
                稍后点“刷新”重试。
                <Button className="mt-2" size="xs" variant="outline" onClick={onRefreshTrace}>
                  刷新
                </Button>
              </Notice>
            ) : (
              <TraceTimeline
                taskId={trace.taskId}
                steps={trace.steps}
                loading={trace.status === "loading"}
                onRefresh={onRefreshTrace}
              />
            ))}
        </div>
      </Tabs>
    </aside>
  );
}
