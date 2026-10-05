import { Loader2 } from "lucide-react";

import type { CheckItemStatus, ProcessorView } from "@/api/admin/image-processor/type.d";
import { Notice } from "@/components/admin-ui/notice";
import {
  StepList,
  StepListDetail,
  StepListIcon,
  StepListItem,
  StepListName,
  type StepStatus,
} from "@/components/admin-ui/step-list";
import { checkIsStale, trialLine, type TrialLine } from "@/utils/admin/image-processor";

const ITEM_STATUS: Record<CheckItemStatus, StepStatus> = {
  ok: "ok",
  warn: "warn",
  fail: "failed",
};

const TRIAL_TONE_CLASS: Record<TrialLine["tone"], string> = {
  ok: "",
  warn: "text-status-warning",
  fail: "text-destructive",
  none: "text-muted-foreground",
};

/** 试跑摘要里的一行值：成功用等宽数字，其余按状态着色并显示后端给的原因 */
function TrialValue({ line }: { line: TrialLine }) {
  return (
    <dd
      className={`tabular-nums ${line.tone === "ok" ? "font-mono" : TRIAL_TONE_CLASS[line.tone]}`}
    >
      {line.text}
    </dd>
  );
}

/**
 * 第四步：校验与试跑结果。逐项展示 checks（ok / warn / fail）与试跑体积耗时；
 * 校验过期（保存后 version 超过校验针对的 version）时不展示旧结论，提示重新校验。
 * @param processor 已保存的处理服务；还没保存时为 null
 * @param running 正在校验
 */
export function StepCheck({
  processor,
  running,
}: {
  processor: ProcessorView | null;
  running: boolean;
}) {
  if (running) {
    return (
      <div className="text-muted-foreground flex items-center gap-2 py-6 text-sm" aria-busy="true">
        <Loader2 className="size-4 animate-spin" />
        正在校验并用真实素材试跑，可能需要几秒…
      </div>
    );
  }
  const check = processor?.check;
  if (!processor || !check) {
    return (
      <Notice tone="neutral">
        还没有校验结果。点“运行校验”检查绑定、域名、签名并用真实素材试跑。
      </Notice>
    );
  }
  const stale = checkIsStale(processor);
  const imageLine = trialLine(check, "image");
  const videoLine = trialLine(check, "video");
  const hasWarn = check.checks.some((item) => item.status === "warn");

  return (
    <div className="flex flex-col gap-4">
      {stale && (
        <Notice tone="warning" title="配置已修改">
          下面是旧配置（v{check.version}）的校验结果，需要重新校验后才能发布。
        </Notice>
      )}

      <StepList>
        {check.checks.map((item) => (
          <StepListItem key={item.key} status={ITEM_STATUS[item.status]}>
            <StepListIcon status={ITEM_STATUS[item.status]} />
            <StepListName>{item.label}</StepListName>
            {item.message && (
              <StepListDetail className="text-muted-foreground text-xs">
                {item.message}
              </StepListDetail>
            )}
          </StepListItem>
        ))}
      </StepList>

      <dl className="bg-muted/40 grid grid-cols-[auto_1fr] gap-x-4 gap-y-1 rounded-lg border px-3 py-2 text-sm">
        <dt className="text-muted-foreground">图片试跑</dt>
        <TrialValue line={imageLine} />
        <dt className="text-muted-foreground">视频封面试跑</dt>
        <TrialValue line={videoLine} />
      </dl>

      {!stale &&
        (check.ok ? (
          <Notice
            tone={hasWarn ? "warning" : "success"}
            title={hasWarn ? "可以发布，但有提示。" : "全部通过，可以发布。"}
          >
            发布后，「{processor.storage_name}」存储里的素材开始使用处理 URL。
          </Notice>
        ) : (
          <Notice tone="danger" title="校验未通过，不能发布。">
            改完参数后回到上一步重新保存并校验。
          </Notice>
        ))}
    </div>
  );
}
