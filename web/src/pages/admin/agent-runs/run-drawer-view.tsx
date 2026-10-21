import { Download, SearchX } from "lucide-react";

import {
  EmptyState,
  EmptyStateDescription,
  EmptyStateIcon,
  EmptyStateTitle,
} from "@/components/admin-ui/empty-state";
import { Notice } from "@/components/admin-ui/notice";
import { Button } from "@/components/ui/button";
import { Skeleton } from "@/components/ui/skeleton";
import { runHeadInfo, type RunDrawerState } from "@/utils/admin/agent-run-detail";

import {
  ContextSection,
  DrawerSection,
  HarnessSection,
  ModelCallsSection,
  MutationsSection,
  RunHead,
  RuntimeTailSection,
  SignalsSection,
} from "./run-detail-sections";
import { RunTimeline } from "./run-timeline";

/** 内容区骨架：版式照真实内容画（请求原文块 + 信息格 + 几行时间线），换上内容时不跳 */
function DrawerSkeleton() {
  return (
    <div data-slot="run-drawer-skeleton" className="flex flex-col gap-5" aria-busy="true">
      <Skeleton className="h-20 rounded-lg" />
      <div className="grid grid-cols-3 gap-4">
        {Array.from({ length: 6 }, (_, i) => (
          <Skeleton key={i} className="h-8" />
        ))}
      </div>
      {Array.from({ length: 5 }, (_, i) => (
        <Skeleton key={i} className="h-4" style={{ width: `${50 + ((i * 17) % 45)}%` }} />
      ))}
    </div>
  );
}

/** 抽屉的全部输入：状态和回调都由外面给，视图本身没有隐藏状态，便于单独验证 */
export type RunDrawerViewProps = {
  /** 抽屉状态（加载中 / 有详情 / 失败 / 运行不存在） */
  state: RunDrawerState;
  /** 正在调导出接口复制 Markdown */
  copying: boolean;
  onCopyMarkdown: () => void;
  onDownload: () => void;
  /** 加载失败后重试 */
  onRetry: () => void;
  /** 默认展开所有折叠内容（工具步骤、思考、模型调用等）；页面不用，只用于单独验证展开后的内容 */
  expandAll?: boolean;
};

/**
 * 运行详情抽屉的内容：加载中给骨架；404 / 60012 显示「运行不存在」；其他错误给 Notice 加重试；
 * 有详情时依次是头部（请求原文与基础信息）、操作栏、Harness、用户信号、时间线、画布改动，
 * 以及有内容才出现的折叠区（模型调用、Node 进程输出、上下文）。
 * 标题和关闭按钮在外面的 Sheet 里。
 */
export function RunDrawerView({
  state,
  copying,
  onCopyMarkdown,
  onDownload,
  onRetry,
  expandAll = false,
}: RunDrawerViewProps) {
  if (state.phase === "idle" || state.phase === "loading") return <DrawerSkeleton />;
  if (state.phase === "notfound")
    return (
      <EmptyState data-slot="run-drawer-notfound" className="mt-2">
        <EmptyStateIcon>
          <SearchX />
        </EmptyStateIcon>
        <EmptyStateTitle>运行不存在</EmptyStateTitle>
        <EmptyStateDescription>编号错误，或这次运行已经被清理。</EmptyStateDescription>
      </EmptyState>
    );
  if (state.phase === "error" || !state.detail)
    return (
      <Notice
        tone="danger"
        title="运行详情加载失败"
        action={
          <Button variant="outline" size="sm" onClick={onRetry}>
            重试
          </Button>
        }
      />
    );

  const detail = state.detail;
  const startedAt = runHeadInfo(detail)?.createdAt;
  const startMs = startedAt ? Date.parse(startedAt) : NaN;
  return (
    <div data-slot="run-drawer-body" className="flex flex-col gap-6">
      <RunHead detail={detail} />
      <div
        data-slot="run-actions"
        className="bg-popover sticky top-0 z-10 -mx-4 flex flex-wrap items-center gap-2 border-y px-4 py-2"
      >
        <Button variant="outline" size="sm" disabled={copying} onClick={onCopyMarkdown}>
          {copying ? "复制中…" : "复制为 Markdown"}
        </Button>
        <Button variant="outline" size="sm" onClick={onDownload}>
          <Download />
          下载 JSON
        </Button>
      </div>
      <HarnessSection digest={detail.digest} />
      <SignalsSection detail={detail} />
      <DrawerSection title="时间线" hint="编号 #n 可在分析里引用">
        <RunTimeline
          steps={detail.timeline}
          startMs={Number.isNaN(startMs) ? null : startMs}
          expandAll={expandAll}
        />
      </DrawerSection>
      {detail.mutations.length > 0 && (
        <MutationsSection mutations={detail.mutations} toolCalls={detail.tool_calls} />
      )}
      {detail.model_calls.length > 0 && (
        <ModelCallsSection calls={detail.model_calls} defaultOpen={expandAll} />
      )}
      {detail.runtime_tail && (
        <RuntimeTailSection text={detail.runtime_tail} defaultOpen={expandAll} />
      )}
      {detail.context.length > 0 && <ContextSection detail={detail} defaultOpen={expandAll} />}
    </div>
  );
}
