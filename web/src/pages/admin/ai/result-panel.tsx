import { useState, useSyncExternalStore } from "react";
import { Loader2, Trash2 } from "lucide-react";

import type { TraceStep } from "@/api/admin-ai/type.d";
import type { TaskOutput, TaskView } from "@/api/generation-task/type.d";
import { FoldableCode } from "@/components/admin-ui/foldable-code";
import { Notice } from "@/components/admin-ui/notice";
import { RequestLog, RequestLogItem } from "@/components/admin-ui/request-log";
import { Tag } from "@/components/admin-ui/tag";
import { Button } from "@/components/ui/button";
import { Switch } from "@/components/ui/switch";
import { Tabs, TabsList, TabsTrigger } from "@/components/ui/tabs";
import { clearRequestLog, getRequestLog, subscribeRequestLog } from "@/utils/requests/request-log";
import { isTerminalStatus } from "@/utils/tasks/status";

import { TraceTimeline } from "./trace/trace-timeline";

/** 操作结果提示（保存、校验、发布等），以 toast 弹出 */
export type ResultNotice = {
  title: string;
  tone: "success" | "error" | "info";
  /** 一句补充说明 */
  text?: string;
  /** 校验问题；问题本身由编辑器就地显示，这里只为兼容调用方 */
  issues?: unknown;
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

/** 测试结果的四个标签 */
export type ResultTabId = "run" | "dry-run" | "trace" | "log";

const TABS: Array<{ id: ResultTabId; label: string }> = [
  { id: "run", label: "结果" },
  { id: "dry-run", label: "请求描述" },
  { id: "trace", label: "追踪" },
  { id: "log", label: "日志" },
];

/** 试跑产物：图片 / 视频 / 音频直接播放，文本显示正文 */
export function OutputPreview({ output }: { output: TaskOutput }) {
  if (output.media_type === "text")
    return (
      <p className="bg-muted/40 max-h-64 overflow-y-auto rounded-lg border p-3 text-sm whitespace-pre-wrap">
        {output.text}
      </p>
    );
  if (!output.url) return null;
  if (output.media_type === "image")
    return (
      <img src={output.url} alt="试跑产物" className="w-full rounded-lg border object-contain" />
    );
  if (output.media_type === "video")
    return <video src={output.url} controls className="w-full rounded-lg border" />;
  if (output.media_type === "audio") return <audio src={output.url} controls className="w-full" />;
  return (
    <a
      href={output.url}
      target="_blank"
      rel="noreferrer"
      className="text-primary text-xs underline"
    >
      {output.url}
    </a>
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
          {!terminal && <Loader2 className="animate-spin" />}
          {view.status}
        </Tag>
      </div>
      {!terminal && (
        <>
          <p className="text-muted-foreground text-xs">
            真实调用上游，不扣用户积分。每 3 秒刷新一次状态。
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
        <Notice tone="danger" title="测试失败">
          {view.error_message || "上游没有返回原因，看“追踪”定位失败的那一步。"}
        </Notice>
      )}
      {ok && view.outputs && view.outputs.length > 0
        ? view.outputs.map((output, index) => <OutputPreview key={index} output={output} />)
        : ok && (
            <Notice tone="success" title="测试成功">
              没有产出文件。
            </Notice>
          )}
      {run.timedOut && (
        <Notice tone="info">试跑仍在进行，可稍后在任务里查看。这不代表失败。</Notice>
      )}
      {run.note && <Notice tone="warning">{run.note}</Notice>}
    </div>
  );
}

/** “日志”标签：后台接口的请求与响应；默认只看和当前模型 / 试跑任务有关的 */
function LogView({ modelKey, taskId }: { modelKey: string; taskId?: number | string }) {
  const entries = useSyncExternalStore(subscribeRequestLog, getRequestLog);
  const [onlyCurrent, setOnlyCurrent] = useState(true);
  const related = (url: string) =>
    (!!modelKey && url.includes(`/models/${encodeURIComponent(modelKey)}`)) ||
    (taskId !== undefined && url.includes(`/test-runs/${taskId}`));
  const shown = onlyCurrent ? entries.filter((entry) => related(entry.url)) : entries;
  return (
    <div className="flex flex-col gap-2">
      <div className="flex items-center gap-2 text-xs">
        <label className="text-muted-foreground flex items-center gap-2">
          <Switch size="sm" checked={onlyCurrent} onCheckedChange={setOnlyCurrent} />
          只看当前模型
        </label>
        <Button
          size="xs"
          variant="ghost"
          className="ml-auto"
          disabled={entries.length === 0}
          onClick={clearRequestLog}
        >
          <Trash2 />
          清空
        </Button>
      </div>
      {shown.length === 0 ? (
        <p className="text-muted-foreground text-xs">
          还没有请求。保存、校验、dry-run、试跑的每次请求与返回都会记在这里（Key 已隐藏）。
        </p>
      ) : (
        <RequestLog>
          {shown.map((entry) => (
            <RequestLogItem key={entry.id} entry={entry} />
          ))}
        </RequestLog>
      )}
    </div>
  );
}

/**
 * 测试结果：结果 / 请求描述 / 追踪 / 日志。
 * dry-run 的标题写“请求描述”而不是“最终请求”：后端返回的是插件给出的请求描述，
 * 并不含宿主注入后的鉴权头。试跑结束后由工作区把标签切到“追踪”。
 */
export function ResultPanel({
  tab,
  onTabChange,
  modelKey,
  dryRun,
  run,
  trace,
  onRefreshTrace,
}: {
  tab: ResultTabId;
  onTabChange: (tab: ResultTabId) => void;
  modelKey: string;
  dryRun: DryRunState | null;
  run: RunState | null;
  trace: TraceState | null;
  onRefreshTrace: () => void;
}) {
  const badge: Record<ResultTabId, number | null> = {
    run: run ? 1 : null,
    "dry-run": dryRun ? 1 : null,
    trace: trace && trace.status === "ready" ? trace.steps.length : null,
    log: null,
  };
  return (
    <Tabs
      value={tab}
      onValueChange={(value) => onTabChange(value as ResultTabId)}
      className="flex min-h-0 flex-1 flex-col gap-0"
    >
      <TabsList className="mx-4 mt-3 w-auto self-stretch">
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
        className="min-h-0 flex-1 overflow-y-auto p-4"
        role="tabpanel"
        aria-label={TABS.find((t) => t.id === tab)?.label}
      >
        {tab === "run" &&
          (!run ? (
            <p className="text-muted-foreground text-xs">
              在左边的节点里选好参数，点“开始测试”真实调用一次上游，不扣用户积分，结果不进素材库。
            </p>
          ) : (
            <RunView run={run} />
          ))}

        {tab === "dry-run" &&
          (!dryRun ? (
            <p className="text-muted-foreground text-xs">
              点“只看请求”查看插件组装出的请求：已校验、鉴权头脱敏，不会真正发送。
            </p>
          ) : dryRun.error ? (
            <Notice tone="danger" title="没有拿到请求描述">
              {dryRun.error}
            </Notice>
          ) : (
            <div className="flex flex-col gap-2">
              <p className="text-muted-foreground text-xs">
                插件返回的请求描述（已校验，Key 与鉴权头脱敏），未发送。
                {new Date(dryRun.time).toLocaleTimeString()}
              </p>
              <FoldableCode defaultOpen text={JSON.stringify(dryRun.json, null, 2)} />
            </div>
          ))}

        {tab === "trace" &&
          (!trace ? (
            <p className="text-muted-foreground text-xs">
              测试完成后，这里会显示每次钩子与每次 HTTP 的时间线。
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

        {tab === "log" && <LogView modelKey={modelKey} taskId={run?.taskId} />}
      </div>
    </Tabs>
  );
}
