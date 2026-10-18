import {
  Clapperboard,
  File as FileIcon,
  ExternalLink,
  Image as ImageIcon,
  Lightbulb,
  Music,
  Type,
} from "lucide-react";
import { useState, type ReactNode } from "react";

import type { UsageDetail, UsageOutput } from "@/api/admin/usage/type";
import { CopyButton } from "@/components/admin-ui/copy-button";
import {
  DescriptionDetails,
  DescriptionItem,
  DescriptionList,
  DescriptionTerm,
} from "@/components/admin-ui/description-list";
import { FoldableCode } from "@/components/admin-ui/foldable-code";
import { Notice } from "@/components/admin-ui/notice";
import {
  Timeline,
  TimelineContent,
  TimelineHeader,
  TimelineIndicator,
  TimelineItem,
} from "@/components/admin-ui/timeline";
import { cn } from "@/lib/utils";
import {
  creditPlain,
  failureAdvice,
  failureHint,
  formatDateTimeSec,
  humanDuration,
  ledgerRows,
  NO_TIMELINE_TEXT,
  outputCaption,
  outputRender,
  showValue,
  spanMs,
  taskVerdict,
  timelineRows,
  usageStatusView,
  type OutputRender,
  type TaskLike,
} from "@/utils/admin/log-rules";

import { TONE_DOT, TONE_SOFT } from "./tone";

/** 抽屉里的一个区块：小标题 + 内容 */
function Section({
  title,
  children,
  className,
}: {
  title: ReactNode;
  children: ReactNode;
  className?: string;
}) {
  return (
    <section className={cn("flex flex-col gap-2.5", className)}>
      <h3 className="text-sm font-semibold">{title}</h3>
      {children}
    </section>
  );
}

const MONO = "font-mono text-xs font-normal";

/** 详情里的 task → 卡片与结论卡共用的字段 */
export function taskLike(detail: UsageDetail): TaskLike {
  const { task, error } = detail;
  return {
    kind: task.kind,
    status: task.status,
    credits: task.credits,
    charged_credits: task.charged_credits,
    is_test: task.is_test,
    error_message: error.message || task.error_message,
    model_key: task.model_key,
    model_name: task.model_name,
    duration_ms: spanMs(task.created_at, task.finished_at),
    created_at: task.created_at,
  };
}

/** 结论卡：抽屉第一屏，状态点 + 一句结论 + 用时 / 原因 / 积分，按状态着色；进行中带进度条 */
function VerdictCard({ detail, now }: { detail: UsageDetail; now: number }) {
  const verdict = taskVerdict(taskLike(detail), now);
  const progress = detail.task.progress;
  return (
    <div
      data-slot="task-verdict"
      className={cn("flex gap-3 rounded-lg p-3", TONE_SOFT[verdict.tone])}
    >
      <span
        aria-hidden
        className={cn(
          "mt-1.5 size-2.5 shrink-0 rounded-full",
          TONE_DOT[verdict.tone],
          verdict.pulse && "animate-pulse motion-reduce:animate-none",
        )}
      />
      <div className="min-w-0 flex-1">
        <div className="text-sm font-semibold">{verdict.title}</div>
        <div className="text-muted-foreground mt-0.5 text-xs break-words">{verdict.sub}</div>
        {verdict.pulse && progress !== null && (
          <div className="mt-2 flex items-center gap-2">
            <div
              role="progressbar"
              aria-valuemin={0}
              aria-valuemax={100}
              aria-valuenow={progress}
              className="bg-background/60 h-1.5 flex-1 overflow-hidden rounded-full"
            >
              <div
                className="bg-status-running h-full rounded-full transition-[width] duration-500"
                style={{ width: `${progress}%` }}
              />
            </div>
            <span className="font-mono text-xs tabular-nums">{progress}%</span>
          </div>
        )}
      </div>
    </div>
  );
}

/** 失败 / 超时 / 取消时的「怎么办」：成功和进行中不显示 */
function AdviceHint({ detail }: { detail: UsageDetail }) {
  const advice = failureAdvice(detail.error.code, detail.task.status);
  if (!advice) return null;
  return (
    <div data-slot="task-advice" className="text-muted-foreground flex gap-2 text-xs leading-5">
      <Lightbulb className="mt-0.5 size-3.5 shrink-0" aria-hidden />
      <p>
        <b className="text-foreground font-medium">{advice.title}。</b>
        {advice.text}
      </p>
    </div>
  );
}

/** 超过这个长度（或行数）的提示词默认折叠成 4 行 */
const FOLD_CHARS = 160;
const FOLD_LINES = 4;

/** 提示词：长文本折叠最多 4 行，点「展开」看全文 */
function PromptBlock({ input }: { input: Record<string, unknown> }) {
  const [open, setOpen] = useState(false);
  const prompt = typeof input.prompt === "string" ? input.prompt : "";
  if (!prompt) return null;
  const long = prompt.length > FOLD_CHARS || prompt.split("\n").length > FOLD_LINES;
  return (
    <Section title="提示词">
      <p className={cn("text-sm break-words whitespace-pre-wrap", long && !open && "line-clamp-4")}>
        {prompt}
      </p>
      {long && (
        <button
          type="button"
          aria-expanded={open}
          className="text-primary focus-visible:ring-ring/50 self-start rounded-sm text-xs outline-none hover:underline focus-visible:ring-2"
          onClick={() => setOpen((v) => !v)}
        >
          {open ? "收起" : "展开"}
        </button>
      )}
    </Section>
  );
}

/** 积分：一句话，再列 freeze / settle / refund 流水 */
function CreditBlock({ detail, ledger }: { detail: UsageDetail; ledger: boolean }) {
  const rows = ledgerRows(detail.ledger);
  return (
    <Section title="积分">
      <p className="text-sm">{creditPlain(taskLike(detail))}</p>
      {ledger && rows.length > 0 && (
        <div className="rounded-lg border">
          {rows.map((row) => (
            <div
              key={row.id}
              className="flex items-center justify-between border-b px-3 py-2 text-xs last:border-b-0"
            >
              <span>
                {row.label}{" "}
                <span className="text-muted-foreground font-mono tabular-nums">
                  {formatDateTimeSec(row.createdAt)}
                </span>
              </span>
              <span
                className={cn(
                  "font-mono tabular-nums",
                  row.amount > 0 && "text-status-success",
                  row.amount < 0 && "text-destructive",
                )}
              >
                {row.amountText}
              </span>
            </div>
          ))}
        </div>
      )}
    </Section>
  );
}

/** 经过（状态时间线）：事件名人话，每步显示时:分:秒与距上一步的间隔；老任务写明没有记录 */
function TimelineBlock({ detail }: { detail: UsageDetail }) {
  const rows = timelineRows(detail.events);
  const status = usageStatusView(detail.task.status);
  return (
    <Section title="经过">
      {rows.length === 0 ? (
        <Notice tone="warning">{NO_TIMELINE_TEXT}</Notice>
      ) : (
        <Timeline>
          {rows.map((row, index) => (
            <TimelineItem key={row.id} className="pb-4">
              <TimelineIndicator active={index === rows.length - 1 && !status.active} />
              <TimelineContent>
                <TimelineHeader>
                  <span className="text-sm font-medium">{row.label}</span>
                  <span className="text-muted-foreground font-mono text-xs tabular-nums">
                    {row.clock}
                    {row.deltaMs !== null && ` · +${humanDuration(row.deltaMs)}`}
                  </span>
                </TimelineHeader>
                {row.note && <p className="text-muted-foreground text-xs">{row.note}</p>}
              </TimelineContent>
            </TimelineItem>
          ))}
          {status.active && (
            <TimelineItem className="pb-0">
              <TimelineIndicator active />
              <TimelineContent>
                <TimelineHeader>
                  <span className="text-sm font-medium">{status.label}</span>
                  <span className="text-muted-foreground text-xs">现在</span>
                </TimelineHeader>
              </TimelineContent>
            </TimelineItem>
          )}
        </Timeline>
      )}
    </Section>
  );
}

/** 参数：input 里除提示词外的键值，对象 / 数组按 JSON */
function ParamsBlock({ input }: { input: Record<string, unknown> }) {
  const entries = Object.entries(input).filter(([key]) => key !== "prompt");
  if (entries.length === 0) return null;
  return (
    <Section title="参数">
      <DescriptionList className="grid-cols-1 md:grid-cols-1 2xl:grid-cols-1">
        {entries.map(([key, value]) => (
          <DescriptionItem key={key}>
            <DescriptionTerm>{key}</DescriptionTerm>
            <DescriptionDetails className={cn("block break-words whitespace-pre-wrap", MONO)}>
              {showValue(value)}
            </DescriptionDetails>
          </DescriptionItem>
        ))}
      </DescriptionList>
    </Section>
  );
}

/** 失败原因详情：错误码 + 上游错误原文（折叠代码块）；没有原文时按情况给提示 */
function FailureDetail({ detail }: { detail: UsageDetail }) {
  const { task, error } = detail;
  if (!["failed", "expired", "canceled"].includes(task.status)) return null;
  const hint = failureHint({
    status: task.status,
    upstream: error.upstream,
    hasEvents: detail.events.length > 0,
  });
  return (
    <Section title="失败原因">
      <DescriptionList className="grid-cols-1 md:grid-cols-1 2xl:grid-cols-1">
        <DescriptionItem>
          <DescriptionTerm>错误码</DescriptionTerm>
          <DescriptionDetails className={MONO}>{error.code || "—"}</DescriptionDetails>
        </DescriptionItem>
      </DescriptionList>
      {error.upstream ? (
        <div className="flex flex-col gap-1">
          <span className="text-muted-foreground text-xs">
            上游返回的原文（仅渠道 Key 被遮，上限 16KB）
          </span>
          <FoldableCode text={error.upstream} defaultOpen />
        </div>
      ) : (
        hint && <Notice tone="warning">{hint}</Notice>
      )}
    </Section>
  );
}

/** 任务信息：完整任务编号（可复制）、渠道 / 插件、画布 / 节点 */
function TaskInfoBlock({ detail }: { detail: UsageDetail }) {
  const { task } = detail;
  return (
    <Section title="任务信息">
      <DescriptionList className="grid-cols-1 md:grid-cols-1 2xl:grid-cols-1">
        <DescriptionItem>
          <DescriptionTerm>任务编号</DescriptionTerm>
          <DescriptionDetails className={cn(MONO, "break-all")}>
            {task.task_ref}
            <CopyButton text={task.task_ref} label="复制任务编号" iconOnly />
          </DescriptionDetails>
        </DescriptionItem>
        <DescriptionItem>
          <DescriptionTerm>渠道 / 插件</DescriptionTerm>
          <DescriptionDetails className={MONO}>
            {task.channel || "—"} · {task.plugin || "—"}
          </DescriptionDetails>
        </DescriptionItem>
        <DescriptionItem>
          <DescriptionTerm>画布 / 节点</DescriptionTerm>
          <DescriptionDetails className={MONO}>
            {task.canvas_id ? `${task.canvas_id} / ${task.node_id || "—"}` : "—"}
          </DescriptionDetails>
        </DescriptionItem>
      </DescriptionList>
    </Section>
  );
}

function OutputIcon({ kind }: { kind: OutputRender["kind"] }) {
  if (kind === "video") return <Clapperboard />;
  if (kind === "image") return <ImageIcon />;
  if (kind === "audio") return <Music />;
  if (kind === "text") return <Type />;
  return <FileIcon />;
}

/** 图标卡片：没有地址、不认识的类型或图片加载失败时的兜底；有地址保留「打开」链接 */
function OutputFallback({
  output,
  kind,
  hasUrl,
}: {
  output: UsageOutput;
  kind: OutputRender["kind"];
  hasUrl: boolean;
}) {
  return (
    <div className="bg-muted/40 flex items-center gap-2.5 rounded-lg border p-3">
      <span className="bg-muted text-muted-foreground grid size-10 shrink-0 place-items-center rounded-md [&>svg]:size-4">
        <OutputIcon kind={kind} />
      </span>
      <span className="min-w-0 text-xs">
        <span className="block truncate">{hasUrl ? output.media_type : "没有可访问地址"}</span>
      </span>
      {hasUrl && (
        <a
          href={output.url}
          target="_blank"
          rel="noreferrer"
          className="text-primary ml-auto inline-flex shrink-0 items-center gap-1 text-xs hover:underline"
        >
          打开
          <ExternalLink className="size-3.5" />
        </a>
      )}
    </div>
  );
}

/** 图片缩略图：点击新标签页看原图；加载失败退回图标卡片 */
function OutputImage({ output }: { output: UsageOutput }) {
  const [failed, setFailed] = useState(false);
  if (failed) return <OutputFallback output={output} kind="image" hasUrl />;
  return (
    <a
      href={output.url}
      target="_blank"
      rel="noreferrer"
      title="在新标签页打开原图"
      className="bg-muted block overflow-hidden rounded-lg border"
    >
      <img
        src={output.url}
        alt="产物图片"
        loading="lazy"
        onError={() => setFailed(true)}
        className="mx-auto max-h-80 w-full object-contain"
      />
    </a>
  );
}

/** 一个产物：按类型渲染真实内容，下方小字放尺寸 / 时长和素材编号 */
function OutputItem({ output }: { output: UsageOutput }) {
  const { kind, hasUrl } = outputRender(output);
  const caption = outputCaption(output);
  let content: ReactNode;
  if (kind === "image" && hasUrl) {
    content = <OutputImage output={output} />;
  } else if (kind === "video" && hasUrl) {
    content = (
      <video
        // #t=0.1 让浏览器把首帧当封面
        src={`${output.url}#t=0.1`}
        controls
        playsInline
        preload="metadata"
        className="bg-muted max-h-80 w-full rounded-lg border object-contain"
      />
    );
  } else if (kind === "audio" && hasUrl) {
    content = <audio src={output.url} controls preload="none" className="w-full" />;
  } else if (kind === "text") {
    content = (
      <pre className="bg-muted/40 max-h-64 overflow-auto rounded-lg border p-3 font-sans text-sm break-words whitespace-pre-wrap">
        {output.text || "（空文本）"}
      </pre>
    );
  } else {
    content = <OutputFallback output={output} kind={kind} hasUrl={hasUrl} />;
  }
  const note = [kind === "text" ? "" : caption, output.asset_id ? `asset #${output.asset_id}` : ""]
    .filter(Boolean)
    .join(" · ");
  return (
    <div className="flex flex-col gap-1.5">
      {content}
      {(note || kind === "text") && (
        <p className="text-muted-foreground font-mono text-xs">
          {kind === "text" ? [caption, note].filter(Boolean).join(" · ") : note}
        </p>
      )}
    </div>
  );
}

/** 产物：图片 / 视频 / 音频 / 文本直接渲染，纵向单列排开 */
function OutputsBlock({ detail }: { detail: UsageDetail }) {
  const active = usageStatusView(detail.task.status).active;
  return (
    <Section title="产物">
      {detail.outputs.length === 0 ? (
        <p className="text-muted-foreground text-sm">{active ? "尚未产出" : "没有产物"}</p>
      ) : (
        <div className="flex flex-col gap-4">
          {detail.outputs.map((output, index) => (
            <OutputItem key={output.asset_id ?? index} output={output} />
          ))}
        </div>
      )}
    </Section>
  );
}

export {
  AdviceHint,
  CreditBlock,
  FailureDetail,
  OutputsBlock,
  ParamsBlock,
  PromptBlock,
  TaskInfoBlock,
  TimelineBlock,
  VerdictCard,
};
