import { Tag } from "@/components/admin-ui/tag";
import {
  StepList,
  StepListDetail,
  StepListDuration,
  StepListIcon,
  StepListItem,
  StepListName,
} from "@/components/admin-ui/step-list";
import { probeSteps, probeSummary } from "@/utils/admin/storage-probe";

import type { ProbeState } from "./use-storage-probe";

/**
 * 测试连接的结果：分步列表，✓ / ✗ / 跳过都用图标加文字；
 * 失败步骤下面写问题、处理建议，云厂商的原始错误放在可展开的“原始错误”里。
 * @param state 测试状态；还没测过时显示引导文案
 * @param withDirect 勾选了浏览器直传：提示这一步无法在这里验证
 */
export function ProbePanel({ state, withDirect }: { state: ProbeState; withDirect: boolean }) {
  if (state.kind === "idle") {
    return (
      <div className="text-muted-foreground rounded-lg border border-dashed p-3 text-xs">
        还没有测试。点“测试连接”，会依次验证鉴权、写入、读取、签名访问、删除
        {withDirect ? "；浏览器直传（CORS）需要在桶上配置后自行确认。" : "。"}
      </div>
    );
  }
  if (state.kind === "running") {
    return (
      <div className="rounded-lg border p-3" aria-busy="true">
        <StepList>
          <StepListItem status="running">
            <StepListIcon status="running" />
            <StepListName>正在测试连接…</StepListName>
          </StepListItem>
        </StepList>
      </div>
    );
  }
  const summary = probeSummary(state.result);
  const steps = probeSteps(state.result);
  return (
    <div className="rounded-lg border p-3" data-slot="probe-panel">
      <div className="mb-2 flex items-center gap-2 text-sm font-medium">
        测试结果
        <Tag tone={summary.tone}>{summary.text}</Tag>
        {state.source === "saved" && (
          <span className="text-muted-foreground text-xs font-normal">使用已保存的配置</span>
        )}
      </div>
      <StepList>
        {steps.map((step) => (
          <StepListItem key={step.index} status={step.status}>
            <StepListIcon status={step.status} />
            <StepListName>
              {step.index}. {step.name}
              {step.status === "skipped" && "（已跳过）"}
              {step.status === "notrun" && "（未执行）"}
            </StepListName>
            {step.durationText && <StepListDuration>{step.durationText}</StepListDuration>}
            {step.issue && (
              <StepListDetail>
                <div className="rounded-md bg-red-500/10 p-2 text-xs text-red-700 dark:text-red-400">
                  <b>{step.issue.title}</b>
                  {step.issue.hint && <div className="mt-0.5">{step.issue.hint}</div>}
                  {step.issue.raw && (
                    <details className="text-muted-foreground mt-1">
                      <summary className="cursor-pointer">原始错误</summary>
                      <code className="font-mono break-all whitespace-pre-wrap">
                        {step.issue.raw}
                      </code>
                    </details>
                  )}
                </div>
              </StepListDetail>
            )}
          </StepListItem>
        ))}
      </StepList>
      {withDirect && (
        <p className="text-muted-foreground mt-2 text-xs">
          浏览器直传（CORS）无法在这里验证，请确认已按上面的规则在桶上配置。
        </p>
      )}
    </div>
  );
}
