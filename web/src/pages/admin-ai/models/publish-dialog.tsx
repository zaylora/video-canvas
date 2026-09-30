import type { ConfigRevision } from "@/api/admin-ai/type";
import { readModelChannel, readModelNumber, readModelString } from "@/utils/admin/model-body";
import type { ModelChannelInfo } from "@/utils/admin/model-channel";

import { ConfirmDialog, formatTime } from "../shared";

/** 确认框里的摘要：渠道、插件版本、上游模型、积分 */
function Summary({ body, info }: { body: unknown; info: ModelChannelInfo }) {
  const { upstreamModel } = readModelChannel(body);
  const credits = readModelNumber(body, "credits");
  return (
    <dl
      className="bg-muted grid grid-cols-[auto_1fr] gap-x-4 gap-y-1 rounded-lg px-3 py-2 text-xs"
      data-testid="publish-summary"
    >
      <dt className="text-muted-foreground">渠道</dt>
      <dd>
        {info.channel ? (
          <>
            {info.channel.name}{" "}
            <span className="text-muted-foreground font-mono">{info.channel.key}</span>
          </>
        ) : (
          <span className="text-destructive">{info.channelKey || "未选择"}（找不到）</span>
        )}
      </dd>
      <dt className="text-muted-foreground">插件版本</dt>
      <dd>
        {info.channel ? (
          <>
            {info.pluginName} <span className="font-mono">v{info.pluginVersion}</span>
            {info.sha8 && <span className="text-muted-foreground font-mono"> · {info.sha8}</span>}
          </>
        ) : (
          "-"
        )}
      </dd>
      <dt className="text-muted-foreground">上游模型</dt>
      <dd className="font-mono">{upstreamModel || "-"}</dd>
      <dt className="text-muted-foreground">积分</dt>
      <dd>{credits ?? "-"}</dd>
    </dl>
  );
}

/**
 * 发布确认框（替代 window.confirm）：列出将发布的渠道、插件版本、上游模型与积分。
 * 渠道 Key 未设置等能提前判断的情况，发布按钮在页面上就已禁用，这里只兜后端 409 等失败。
 * @param label 模型展示名（没有则用 key）
 * @param error 发布失败的就地原因（全局 toast 之外的补充）
 */
export function PublishDialog({
  open,
  modelKey,
  body,
  info,
  busy,
  error,
  onConfirm,
  onCancel,
}: {
  open: boolean;
  modelKey: string;
  body: unknown;
  info: ModelChannelInfo;
  busy: boolean;
  error: string | null;
  onConfirm: () => void;
  onCancel: () => void;
}) {
  const label = readModelString(body, "label") || modelKey;
  return (
    <ConfirmDialog
      open={open}
      title="发布模型？"
      confirmLabel="发布"
      busy={busy}
      error={error}
      description={
        <>
          发布后，<b>{label}</b> 会以下面的配置立即对所有用户生效；进行中的任务不受影响。
        </>
      }
      onConfirm={onConfirm}
      onCancel={onCancel}
    >
      <Summary body={body} info={info} />
    </ConfirmDialog>
  );
}

/**
 * 回滚确认框：展示目标版本，以及该版本正文里的渠道与（渠道当前固定的）插件版本。
 * 注意插件版本取的是渠道现在固定的版本，不是回滚目标当年的版本。
 */
export function RollbackDialog({
  revision,
  modelKey,
  info,
  blockReason,
  busy,
  error,
  onConfirm,
  onCancel,
}: {
  revision: ConfigRevision | null;
  modelKey: string;
  info: ModelChannelInfo;
  /** 回滚目标的渠道现在不可用（例如 Key 未设置）时的原因；有值则禁用确认 */
  blockReason?: string | null;
  busy: boolean;
  error: string | null;
  onConfirm: () => void;
  onCancel: () => void;
}) {
  return (
    <ConfirmDialog
      open={!!revision}
      title="回滚模型？"
      confirmLabel="回滚"
      destructive
      busy={busy}
      error={blockReason ?? error}
      confirmDisabled={!!blockReason}
      description={
        revision && (
          <>
            将 <span className="font-mono">{modelKey}</span> 回滚到
            <b>第 {revision.revision_no} 版</b>（{formatTime(revision.created_at)}
            {revision.note ? ` · ${revision.note}` : ""}）。回滚后立即生效，进行中的任务不受影响。
          </>
        )
      }
      onConfirm={onConfirm}
      onCancel={onCancel}
    >
      {revision && <Summary body={revision.body_json} info={info} />}
    </ConfirmDialog>
  );
}
