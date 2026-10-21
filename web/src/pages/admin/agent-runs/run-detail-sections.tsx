import { ThumbsDown, ThumbsUp } from "lucide-react";
import type { ReactNode } from "react";

import type {
  AgentRunDetail,
  AgentRunDigest,
  AgentRunModelCall,
  AgentRunMutation,
  AgentRunToolCall,
} from "@/api/admin/agent-run/type.d";
import { Disclosure, DisclosureContent, DisclosureTrigger } from "@/components/admin-ui/disclosure";
import {
  DescriptionDetails,
  DescriptionItem,
  DescriptionList,
  DescriptionTerm,
} from "@/components/admin-ui/description-list";
import { Tag } from "@/components/admin-ui/tag";
import {
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableHeader,
  TableRow,
} from "@/components/ui/table";
import { cn } from "@/lib/utils";
import { runStatusView } from "@/utils/admin/agent-run";
import {
  contextLines,
  mutationRows,
  requestText,
  runHeadInfo,
  userSignals,
} from "@/utils/admin/agent-run-detail";
import { formatFullTime } from "@/utils/admin/log-rules";
import { formatDuration } from "@/utils/admin/trace";
import { formatDurationMs } from "@/utils/admin/user-rules";

/** 抽屉里的一个区块：小标题 + 内容 */
export function DrawerSection({
  title,
  hint,
  children,
}: {
  title: string;
  hint?: ReactNode;
  children: ReactNode;
}) {
  return (
    <section data-slot="drawer-section" aria-label={title} className="flex flex-col gap-2">
      <div className="flex items-baseline gap-2">
        <h3 className="text-sm font-semibold">{title}</h3>
        {hint && <span className="text-muted-foreground text-xs">{hint}</span>}
      </div>
      {children}
    </section>
  );
}

/** 折叠的区块：标题行点开才看内容（模型调用、Node 进程输出、上下文） */
function FoldSection({
  title,
  hint,
  defaultOpen,
  children,
}: {
  title: string;
  hint?: ReactNode;
  defaultOpen: boolean;
  children: ReactNode;
}) {
  return (
    <Disclosure defaultOpen={defaultOpen} data-slot="fold-section" className="flex flex-col gap-2">
      <DisclosureTrigger className="py-0.5">
        <h3 className="text-sm font-semibold">{title}</h3>
        {hint && <span className="text-muted-foreground text-xs">{hint}</span>}
      </DisclosureTrigger>
      <DisclosureContent>{children}</DisclosureContent>
    </Disclosure>
  );
}

const count = (n: number) => n.toLocaleString("en-US");

/**
 * 抽屉头部：用户请求原文（时间线里第一条用户消息，没有就用摘要里的请求首句）和基础信息——
 * 状态、创建与结束时间、耗时、步数、积分、Token（输入 / 输出）
 */
export function RunHead({ detail }: { detail: AgentRunDetail }) {
  const request = requestText(detail);
  const head = runHeadInfo(detail);
  const status = head ? runStatusView(head.status) : null;
  return (
    <div data-slot="run-head" className="flex flex-col gap-4">
      <div className="flex flex-col gap-1">
        <span className="text-muted-foreground text-xs">用户请求</span>
        <p
          data-slot="run-request"
          className="bg-muted max-h-40 overflow-auto rounded-lg p-3 text-sm break-words whitespace-pre-wrap"
        >
          {request || "（没有记录请求原文）"}
        </p>
      </div>
      {head && status && (
        <DescriptionList className="md:grid-cols-3 2xl:grid-cols-3">
          <DescriptionItem>
            <DescriptionTerm>状态</DescriptionTerm>
            <DescriptionDetails>
              <Tag tone={status.tone}>{status.label}</Tag>
            </DescriptionDetails>
          </DescriptionItem>
          <DescriptionItem>
            <DescriptionTerm>创建时间</DescriptionTerm>
            <DescriptionDetails className="font-mono text-xs tabular-nums">
              {formatFullTime(head.createdAt)}
            </DescriptionDetails>
          </DescriptionItem>
          <DescriptionItem>
            <DescriptionTerm>结束时间</DescriptionTerm>
            <DescriptionDetails className="font-mono text-xs tabular-nums">
              {formatFullTime(head.endedAt)}
            </DescriptionDetails>
          </DescriptionItem>
          <DescriptionItem>
            <DescriptionTerm>耗时</DescriptionTerm>
            <DescriptionDetails className="tabular-nums">
              {formatDurationMs(head.durationMs)}
            </DescriptionDetails>
          </DescriptionItem>
          <DescriptionItem>
            <DescriptionTerm>步数</DescriptionTerm>
            <DescriptionDetails className="tabular-nums">{head.steps}</DescriptionDetails>
          </DescriptionItem>
          <DescriptionItem>
            <DescriptionTerm>积分</DescriptionTerm>
            <DescriptionDetails className="tabular-nums">{head.credits}</DescriptionDetails>
          </DescriptionItem>
          <DescriptionItem className="col-span-2 md:col-span-3">
            <DescriptionTerm>Token（输入 / 输出）</DescriptionTerm>
            <DescriptionDetails className="tabular-nums">
              {count(head.inputTokens)} / {count(head.outputTokens)}
            </DescriptionDetails>
          </DescriptionItem>
        </DescriptionList>
      )}
    </div>
  );
}

/** 「Harness」：这次运行用的提示词、构建、技能和模型，对比版本时靠它确认是哪一版 */
export function HarnessSection({ digest }: { digest: AgentRunDigest | null }) {
  if (!digest)
    return (
      <DrawerSection title="Harness">
        <p className="text-muted-foreground text-sm">摘要还没生成，暂无 Harness 信息。</p>
      </DrawerSection>
    );
  const skills = Object.entries(digest.skills ?? {});
  return (
    <DrawerSection title="Harness">
      <DescriptionList className="md:grid-cols-2 2xl:grid-cols-2">
        <DescriptionItem>
          <DescriptionTerm>提示词版本</DescriptionTerm>
          <DescriptionDetails className="font-mono tabular-nums">
            v{digest.prompt_version}
          </DescriptionDetails>
        </DescriptionItem>
        <DescriptionItem>
          <DescriptionTerm>harness_key</DescriptionTerm>
          <DescriptionDetails className="font-mono text-xs break-all">
            {digest.harness_key || "—"}
          </DescriptionDetails>
        </DescriptionItem>
        <DescriptionItem>
          <DescriptionTerm>构建（build_sha）</DescriptionTerm>
          <DescriptionDetails className="font-mono text-xs break-all">
            {digest.build_sha || "—"}
          </DescriptionDetails>
        </DescriptionItem>
        <DescriptionItem>
          <DescriptionTerm>模型</DescriptionTerm>
          <DescriptionDetails className="break-all">{digest.model_key || "—"}</DescriptionDetails>
        </DescriptionItem>
        <DescriptionItem className="col-span-2">
          <DescriptionTerm>技能版本</DescriptionTerm>
          <DescriptionDetails>
            {skills.length === 0 ? (
              <span className="text-muted-foreground font-normal">没有读取技能</span>
            ) : (
              skills.map(([name, version]) => (
                <Tag key={name} mono>
                  {name} {version}
                </Tag>
              ))
            )}
          </DescriptionDetails>
        </DescriptionItem>
      </DescriptionList>
    </DrawerSection>
  );
}

/** 一行用户信号：名称 + 值 */
function SignalRow({
  label,
  children,
  active,
}: {
  label: string;
  children: ReactNode;
  active?: boolean;
}) {
  return (
    <div className="flex items-start gap-3 text-sm">
      <span className="text-muted-foreground w-20 shrink-0 text-xs leading-5">{label}</span>
      <div className={cn("min-w-0 flex-1", !active && "text-muted-foreground")}>{children}</div>
    </div>
  );
}

/**
 * 「用户信号」：反馈（👍/👎、原因、说明原文）、是否撤销、插话次数、被拒审批、工具报错数。
 * 一个信号都没有时只写「没有用户反馈或撤销」
 */
export function SignalsSection({ detail }: { detail: AgentRunDetail }) {
  const signals = userSignals(detail);
  return (
    <DrawerSection title="用户信号">
      {!signals.hasAny ? (
        <p className="text-muted-foreground text-sm">没有用户反馈或撤销</p>
      ) : (
        <div data-slot="run-signals" className="flex flex-col gap-2">
          <SignalRow label="反馈" active={!!signals.feedback}>
            {signals.feedback ? (
              <div className="flex flex-col gap-1.5">
                <span
                  className={cn(
                    "inline-flex items-center gap-1 font-medium",
                    signals.feedback.rating === 1 ? "text-status-success" : "text-destructive",
                  )}
                >
                  {signals.feedback.rating === 1 ? (
                    <ThumbsUp className="size-4" aria-hidden="true" />
                  ) : (
                    <ThumbsDown className="size-4" aria-hidden="true" />
                  )}
                  {signals.feedback.rating === 1 ? "有帮助" : "没帮上忙"}
                </span>
                {signals.feedback.reasons.length > 0 && (
                  <div className="flex flex-wrap gap-1.5">
                    {signals.feedback.reasons.map((reason) => (
                      <Tag key={reason} tone="danger">
                        {reason}
                      </Tag>
                    ))}
                  </div>
                )}
                {signals.feedback.comment && (
                  <blockquote className="border-border border-l-2 pl-2 break-words whitespace-pre-wrap">
                    {signals.feedback.comment}
                  </blockquote>
                )}
              </div>
            ) : (
              "没评价"
            )}
          </SignalRow>
          <SignalRow label="撤销" active={signals.undone}>
            {signals.undone ? "是，用户撤销了这一轮的画布改动" : "否"}
          </SignalRow>
          <SignalRow label="插话" active={signals.steerCount > 0}>
            {signals.steerCount} 次
          </SignalRow>
          <SignalRow label="被拒审批" active={signals.approvalsRejected > 0}>
            {signals.approvalsRejected} 次
          </SignalRow>
          <SignalRow label="工具报错" active={signals.toolErrors > 0}>
            {signals.toolErrors} 次
          </SignalRow>
        </div>
      )}
    </DrawerSection>
  );
}

/** 「画布改动」：序号、种类、改动条数、来自哪个工具调用、是否已撤销 */
export function MutationsSection({
  mutations,
  toolCalls,
}: {
  mutations: AgentRunMutation[];
  toolCalls: AgentRunToolCall[];
}) {
  const rows = mutationRows(mutations, toolCalls);
  return (
    <DrawerSection title="画布改动">
      <Table data-slot="mutation-table">
        <TableHeader>
          <TableRow>
            <TableHead className="w-12">序号</TableHead>
            <TableHead>种类</TableHead>
            <TableHead className="text-right">改动条数</TableHead>
            <TableHead>来自工具调用</TableHead>
            <TableHead>已撤销</TableHead>
          </TableRow>
        </TableHeader>
        <TableBody>
          {rows.map((row) => (
            <TableRow key={row.seq}>
              <TableCell className="tabular-nums">{row.seq}</TableCell>
              <TableCell className="font-mono text-xs">{row.kind}</TableCell>
              <TableCell className="text-right tabular-nums">{row.count}</TableCell>
              <TableCell className="text-xs" title={row.toolCallId}>
                {row.toolName ? <span className="font-mono">{row.toolName}</span> : "—"}
              </TableCell>
              <TableCell>
                {row.undone ? <Tag tone="warning">已撤销</Tag> : <span>—</span>}
              </TableCell>
            </TableRow>
          ))}
        </TableBody>
      </Table>
    </DrawerSection>
  );
}

/** 模型调用用途的中文 */
const PURPOSE_LABEL: Record<string, string> = { main: "对话", summary: "摘要" };

/** 「模型调用」折叠区：用途、模型、渠道、上游模型、输入 / 输出 Token、耗时、首字、结束原因、错误 */
export function ModelCallsSection({
  calls,
  defaultOpen,
}: {
  calls: AgentRunModelCall[];
  defaultOpen: boolean;
}) {
  return (
    <FoldSection title="模型调用" hint={`${calls.length} 次`} defaultOpen={defaultOpen}>
      <Table data-slot="model-call-table">
        <TableHeader>
          <TableRow>
            <TableHead>用途</TableHead>
            <TableHead>模型</TableHead>
            <TableHead>渠道</TableHead>
            <TableHead>上游模型</TableHead>
            <TableHead className="text-right">输入 / 输出 Token</TableHead>
            <TableHead className="text-right">耗时</TableHead>
            <TableHead className="text-right">首字</TableHead>
            <TableHead>结束原因</TableHead>
            <TableHead>错误</TableHead>
          </TableRow>
        </TableHeader>
        <TableBody>
          {calls.map((call, i) => (
            <TableRow key={i}>
              <TableCell>{PURPOSE_LABEL[call.purpose] ?? (call.purpose || "—")}</TableCell>
              <TableCell className="font-mono text-xs">{call.model_key || "—"}</TableCell>
              <TableCell className="font-mono text-xs">{call.channel_key || "—"}</TableCell>
              <TableCell className="font-mono text-xs">{call.upstream_model || "—"}</TableCell>
              <TableCell className="text-right tabular-nums">
                {count(call.input_tokens)} / {count(call.output_tokens)}
                {call.usage_estimated && (
                  <span title="Token 数是估算的" className="text-muted-foreground">
                    {" "}
                    ≈
                  </span>
                )}
              </TableCell>
              <TableCell className="text-right tabular-nums">
                {formatDuration(call.latency_ms)}
              </TableCell>
              <TableCell className="text-right tabular-nums">
                {call.ttft_ms > 0 ? formatDuration(call.ttft_ms) : "—"}
              </TableCell>
              <TableCell className="text-xs">{call.stop_reason || "—"}</TableCell>
              <TableCell
                className="text-destructive max-w-48 truncate text-xs"
                title={call.error || undefined}
              >
                {call.error || <span className="text-muted-foreground">—</span>}
              </TableCell>
            </TableRow>
          ))}
        </TableBody>
      </Table>
    </FoldSection>
  );
}

/** 「Node 进程输出」折叠区：Agent 运行时的 stderr 末尾，等宽预格式化，过 30 天保留期后为空不显示 */
export function RuntimeTailSection({ text, defaultOpen }: { text: string; defaultOpen: boolean }) {
  return (
    <FoldSection title="Node 进程输出" hint="stderr 末尾" defaultOpen={defaultOpen}>
      <pre
        data-slot="runtime-tail"
        className="bg-muted max-h-64 overflow-auto rounded-md p-2 font-mono text-xs leading-5 break-all whitespace-pre-wrap"
      >
        {text}
      </pre>
    </FoldSection>
  );
}

/** 「上下文」折叠区：同一会话更早两轮的用户消息和 Agent 回复，每条一行（取事件 data.text） */
export function ContextSection({
  detail,
  defaultOpen,
}: {
  detail: AgentRunDetail;
  defaultOpen: boolean;
}) {
  const lines = contextLines(detail.context);
  return (
    <FoldSection title="上下文" hint="更早两轮" defaultOpen={defaultOpen}>
      {lines.length === 0 ? (
        <p className="text-muted-foreground text-sm">没有可显示的上下文。</p>
      ) : (
        <ul data-slot="run-context" className="flex flex-col gap-1.5">
          {lines.map((line, i) => (
            <li key={i} className="flex gap-2 text-sm">
              <span className="text-muted-foreground w-12 shrink-0 text-xs leading-5">
                {line.who}
              </span>
              <span className="min-w-0 break-words whitespace-pre-wrap">{line.text}</span>
            </li>
          ))}
        </ul>
      )}
    </FoldSection>
  );
}
