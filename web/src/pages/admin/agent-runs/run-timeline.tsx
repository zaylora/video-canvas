import {
  Bot,
  Circle,
  Flag,
  Info,
  ListChecks,
  MessageSquarePlus,
  ShieldCheck,
  ThumbsUp,
  Undo2,
  User,
  Wrench,
  type LucideIcon,
} from "lucide-react";

import type {
  AgentRunStep,
  AgentRunStepKind,
  AgentRunStepTool,
} from "@/api/admin/agent-run/type.d";
import { Disclosure, DisclosureContent, DisclosureTrigger } from "@/components/admin-ui/disclosure";
import { Tag, type TagTone } from "@/components/admin-ui/tag";
import { cn } from "@/lib/utils";
import { stepView, toolHeadline, toolNotes, type StepView } from "@/utils/admin/agent-run-detail";

/** 每种步骤的图标 */
const KIND_ICON: Record<AgentRunStepKind, LucideIcon> = {
  user: User,
  steer: MessageSquarePlus,
  assistant: Bot,
  tool: Wrench,
  approval: ShieldCheck,
  plan: ListChecks,
  undo: Undo2,
  feedback: ThumbsUp,
  notice: Info,
  status: Flag,
  other: Circle,
};

/** 色调对应的文字颜色：全走设计 token */
const TONE_TEXT: Partial<Record<TagTone, string>> = {
  success: "text-status-success",
  danger: "text-destructive",
  warning: "text-status-warning",
  info: "text-status-running",
};

const toneText = (tone: TagTone) => TONE_TEXT[tone] ?? "text-muted-foreground";

/** 参数 / 结果的等宽块：可滚动，最大高度有限制 */
const CODE_BLOCK =
  "bg-muted max-h-48 overflow-auto rounded-md p-2 font-mono text-xs leading-5 break-all whitespace-pre-wrap";

/** 步骤编号：等宽小字，分析时用 #n 引用 */
function StepId({ id }: { id: string }) {
  return <code className="text-muted-foreground shrink-0 font-mono text-[11px]">{id}</code>;
}

/** 步骤右侧的时间；悬浮看完整时间 */
function StepTime({ view }: { view: StepView }) {
  if (!view.offset) return null;
  return (
    <time
      title={view.title}
      className="text-muted-foreground ml-auto shrink-0 font-mono text-[11px] tabular-nums"
    >
      {view.offset}
    </time>
  );
}

/** 一段带小标题的内容 */
function Labeled({ title, children }: { title: string; children: React.ReactNode }) {
  return (
    <div className="flex flex-col gap-1">
      <span className="text-muted-foreground text-xs">{title}</span>
      {children}
    </div>
  );
}

/** 工具步骤展开后的内容：提示、参数、结果、摘要 */
function ToolDetail({ tool }: { tool: AgentRunStepTool }) {
  const notes = toolNotes(tool);
  return (
    <div data-slot="tool-detail" className="mt-2 flex flex-col gap-2">
      {notes.map((note) => (
        <p
          key={note.key}
          data-note={note.key}
          className={cn(
            "text-xs",
            note.tone === "warning" ? "text-status-warning" : "text-muted-foreground",
          )}
        >
          {note.text}
        </p>
      ))}
      {tool.args && (
        <Labeled title="参数">
          <pre className={CODE_BLOCK}>{tool.args}</pre>
        </Labeled>
      )}
      {tool.result && (
        <Labeled title="结果">
          <pre className={cn(CODE_BLOCK, tool.is_error && "text-destructive bg-destructive/10")}>
            {tool.result}
          </pre>
        </Labeled>
      )}
      {tool.summary && (
        <Labeled title="摘要">
          <p className={cn("text-sm break-words", tool.is_error && "text-destructive")}>
            {tool.summary}
          </p>
        </Labeled>
      )}
    </div>
  );
}

/** 工具步骤：一行标题（工具名 · 耗时 · 成败），默认折叠，点开看参数、结果和摘要 */
function ToolStep({
  step,
  view,
  defaultOpen,
}: {
  step: AgentRunStep;
  view: StepView;
  defaultOpen: boolean;
}) {
  const tool = step.tool;
  if (!tool) return null;
  const head = toolHeadline(tool);
  const Icon = KIND_ICON.tool;
  return (
    <Disclosure defaultOpen={defaultOpen}>
      <DisclosureTrigger className="py-0.5">
        <StepId id={view.id} />
        {view.indent && <span aria-hidden="true">↳</span>}
        <Icon className={cn("size-3.5 shrink-0", toneText(view.tone))} aria-hidden="true" />
        <span className="text-muted-foreground shrink-0 text-xs">{view.label}</span>
        <span
          className={cn("min-w-0 truncate text-sm", head.failed && "text-destructive font-medium")}
        >
          <span className="font-mono">{head.name}</span>
          {head.duration && <span> · {head.duration}</span>}
          <span> · {head.outcome}</span>
        </span>
        <StepTime view={view} />
      </DisclosureTrigger>
      <DisclosureContent className="pl-5">
        <ToolDetail tool={tool} />
      </DisclosureContent>
    </Disclosure>
  );
}

/** 对话类步骤（用户 / 插话 / 模型回复）：标题行加正文；模型回复的思考默认折叠 */
function MessageStep({
  step,
  view,
  defaultOpenThinking,
}: {
  step: AgentRunStep;
  view: StepView;
  defaultOpenThinking: boolean;
}) {
  const Icon = KIND_ICON[step.kind];
  return (
    <div className="flex flex-col gap-1">
      <div className="flex items-center gap-1.5">
        <StepId id={view.id} />
        <Icon className="text-muted-foreground size-3.5 shrink-0" aria-hidden="true" />
        <span className="text-xs font-medium">{view.label}</span>
        {step.partial && (
          <Tag tone="warning" title="回答没说完就被中断了">
            半截回答，被中断
          </Tag>
        )}
        <StepTime view={view} />
      </div>
      {step.text && (
        <p
          className={cn(
            "pl-5 text-sm break-words whitespace-pre-wrap",
            step.kind === "user" && "bg-muted ml-5 rounded-md p-2 pl-2",
          )}
        >
          {step.text}
        </p>
      )}
      {step.thinking && (
        <Disclosure defaultOpen={defaultOpenThinking} className="pl-5">
          <DisclosureTrigger className="text-muted-foreground text-xs">思考</DisclosureTrigger>
          <DisclosureContent>
            <p className="text-muted-foreground mt-1 pl-5 text-xs break-words whitespace-pre-wrap">
              {step.thinking}
            </p>
          </DisclosureContent>
        </Disclosure>
      )}
    </div>
  );
}

/** 事件类步骤（审批、计划、撤销、反馈、提示、状态、其他）：编号 + 图标 + 类型名 + 一行正文 */
function EventStep({ step, view }: { step: AgentRunStep; view: StepView }) {
  const Icon = KIND_ICON[step.kind];
  return (
    <div className="flex items-start gap-1.5">
      <StepId id={view.id} />
      <Icon className={cn("mt-0.5 size-3.5 shrink-0", toneText(view.tone))} aria-hidden="true" />
      <span className={cn("min-w-0 text-sm break-words", toneText(view.tone))}>
        <span className="font-medium">{view.label}</span>
        {view.text && <span className="opacity-90"> {view.text}</span>}
      </span>
      <StepTime view={view} />
    </div>
  );
}

/**
 * 时间线：按后端给的 timeline 逐步渲染（和 Markdown 导出同一份步骤，前端不再自己拼）。
 * 每步带 #编号、图标、类型名和相对运行开始的时间；工具步骤默认折叠，codemode 子调用缩进并带「↳」。
 * @param steps 时间线步骤
 * @param startMs 运行开始时间（毫秒），用来算每步的偏移；未知为 null
 * @param expandAll 默认全部展开（工具步骤和思考）；页面不用，只用于单独验证展开后的内容
 */
export function RunTimeline({
  steps,
  startMs,
  expandAll = false,
}: {
  steps: AgentRunStep[];
  startMs: number | null;
  expandAll?: boolean;
}) {
  if (steps.length === 0)
    return <p className="text-muted-foreground text-sm">这次运行没有记录到步骤。</p>;
  return (
    <ol data-slot="run-timeline" className="flex flex-col">
      {steps.map((step) => {
        const view = stepView(step, startMs);
        return (
          <li
            key={step.n}
            data-step={step.n}
            data-kind={step.kind}
            data-nested={view.indent || undefined}
            className={cn(
              "border-border/60 border-b py-2 last:border-b-0",
              view.indent && "ml-5 border-l pl-3",
              step.tool?.is_error && "bg-destructive/5 rounded-md",
            )}
          >
            {step.kind === "tool" ? (
              <ToolStep step={step} view={view} defaultOpen={expandAll} />
            ) : step.kind === "user" || step.kind === "steer" || step.kind === "assistant" ? (
              <MessageStep step={step} view={view} defaultOpenThinking={expandAll} />
            ) : (
              <EventStep step={step} view={view} />
            )}
          </li>
        );
      })}
    </ol>
  );
}
