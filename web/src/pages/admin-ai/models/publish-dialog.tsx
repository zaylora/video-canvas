import type { ConfigRevision } from "@/api/admin-ai/type";
import {
  describeDefaultPrice,
  readModelChannel,
  readModelPricing,
  readModelString,
} from "@/utils/admin/model-body";
import type { ModelChannelInfo } from "@/utils/admin/model-channel";
import { formatTime } from "@/utils/time";
import { useRetained } from "@/hooks/use-retained";

import { ConfirmDialog, confirm } from "@/components/admin-ui/confirm-dialog";

/** 确认框里的摘要：渠道、插件版本、上游模型、积分 */
function Summary({ body, info }: { body: unknown; info: ModelChannelInfo }) {
  const { upstreamModel } = readModelChannel(body);
  const pricing = readModelPricing(body);
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
      <dt className="text-muted-foreground">默认价格</dt>
      <dd>
        {describeDefaultPrice(pricing)}
        {!!pricing?.tiers?.length && ` · ${pricing.tiers.length} 条规格价格`}
      </dd>
    </dl>
  );
}

/**
 * 上线确认框（替代 window.confirm）：列出将上线的渠道、插件版本、上游模型与积分。
 * 确认后发布草稿并上架（见 useModelWorkspace.confirmPublish）。
 * 渠道 Key 未设置等能提前判断的情况，发布按钮在页面上就已禁用，这里只兜后端 409 等失败。
 * @param label 模型展示名（没有则用 key）
 * @param error 发布失败的就地原因（全局 toast 之外的补充）
 */
export function PublishDialog({
  open,
  online,
  modelKey,
  body,
  info,
  busy,
  error,
  onConfirm,
  onCancel,
}: {
  open: boolean;
  /** 模型当前在线：文案是“更新上线版本”；否则是“上线” */
  online: boolean;
  modelKey: string;
  body: unknown;
  info: ModelChannelInfo;
  busy: boolean;
  error: string | null;
  onConfirm: () => void;
  onCancel: () => void;
}) {
  // 关闭时 modelKey 置空，标题里的模型名留到退出动画播完
  const shownKey = useRetained(modelKey || null) ?? "";
  const label = readModelString(body, "label") || shownKey;
  return (
    <ConfirmDialog
      open={open}
      title={online ? "更新上线版本？" : "上线模型？"}
      confirmLabel={online ? "更新" : "上线"}
      busy={busy}
      error={error}
      description={
        <>
          {online ? "更新后" : "上线后"}，<b>{label}</b>{" "}
          会以下面的配置立即出现在画布里，所有用户可用；进行中的任务不受影响。
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
  // 关闭时 revision 置空，版本信息留到退出动画播完；info 由调用方按保留的目标算好传进来
  const shown = useRetained(revision);
  const shownKey = useRetained(modelKey || null) ?? "";
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
        shown && (
          <>
            将 <span className="font-mono">{shownKey}</span> 回滚到
            <b>第 {shown.revision_no} 版</b>（{formatTime(shown.created_at)}
            {shown.note ? ` · ${shown.note}` : ""}）。回滚后立即生效，进行中的任务不受影响。
          </>
        )
      }
      onConfirm={onConfirm}
      onCancel={onCancel}
    >
      {shown && <Summary body={shown.body_json} info={info} />}
    </ConfirmDialog>
  );
}

/**
 * 用全局弹窗 store 打开回滚确认（模型列表行上的“回滚”用；模型弹窗里的回滚是受控的 RollbackDialog）。
 * @param onConfirm 确认后执行回滚；抛错时原因留在框里
 * @returns 是否已回滚
 */
export function confirmRollback({
  modelKey,
  revision,
  info,
  blockReason,
  onConfirm,
}: {
  modelKey: string;
  revision: ConfigRevision;
  info: ModelChannelInfo;
  blockReason?: string | null;
  onConfirm: () => Promise<unknown>;
}) {
  return confirm({
    title: "回滚模型？",
    confirmLabel: "回滚",
    destructive: true,
    blockReason,
    description: (
      <>
        将 <span className="font-mono">{modelKey}</span> 回滚到
        <b>第 {revision.revision_no} 版</b>（{formatTime(revision.created_at)}
        {revision.note ? ` · ${revision.note}` : ""}）。回滚后立即生效，进行中的任务不受影响。
      </>
    ),
    children: <Summary body={revision.body_json} info={info} />,
    onConfirm,
  });
}
