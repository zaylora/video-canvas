import { useState } from "react";
import { toast } from "sonner";

import { listChannelOptions, listModelOptions, upgradePluginModels } from "@/api/admin/ai";
import type { ChannelOption, ConfigListItem, PluginView } from "@/api/admin/ai/type.d";
import { ConfirmDialog } from "@/components/admin-ui/confirm-dialog";
import { openDialog, type DialogControl } from "@/store/dialog";
import { errorMessage } from "@/utils/admin/errors";
import {
  latestVersion,
  outdatedModels,
  summarizeUpgrade,
  type UpgradeSummary,
} from "@/utils/admin/plugin";

import { useAliveRef } from "../../use-admin";

/** 模型在清单里的展示名：label 优先，旧字段 name 其次，最后是 key */
const modelName = (model: Pick<ConfigListItem, "key" | "label" | "name">) =>
  model.label || model.name || model.key;

type UpgradeDialogProps = {
  /** 要升级的插件 */
  plugin: Pick<PluginView, "key" | "name">;
  /** 目标版本（插件当前最新版本） */
  toVersion: string;
  /** 协议固定在旧版本上的模型（打开时的快照，仅用于展示，实际升哪些以后端为准） */
  targets: ConfigListItem[];
  /** 渠道 key → 渠道名，展示“所属渠道” */
  channelNames: Record<string, string>;
  /** 有模型升级成功后（刷新插件列表与目录），可能随重试多次触发 */
  onDone: () => Promise<unknown> | void;
  /** 对话框结束：全部升级成功为 true，取消或仍有失败为 false */
  resolve: (allDone: boolean) => void;
};

/**
 * 升级模型确认框（插件页、总览页的“可升级”待办共用，走全局弹窗 store）：
 * 1. 确认前列出每个模型的名称、所属渠道、从哪个版本升到哪个；
 * 2. 确认后调用 upgrade-models（逐个处理，失败不中断），成功的立即生效并刷新；
 * 3. 全部成功自动关闭；有失败时留在框里，列出失败的模型与原因（失败的保持原样），可重试失败项。
 */
function UpgradeModelsDialog({
  plugin,
  toVersion,
  targets,
  channelNames,
  onDone,
  resolve,
  open,
  onClose,
  onExited,
}: UpgradeDialogProps & DialogControl) {
  const aliveRef = useAliveRef();
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState<string | null>(null);
  /** 最近一次升级的结果；null 表示还没点过确认 */
  const [result, setResult] = useState<UpgradeSummary | null>(null);
  /** 累计升级成功的模型数（重试前几次成功的也算） */
  const [upgradedTotal, setUpgradedTotal] = useState(0);

  const nameOf = (key: string) => {
    const target = targets.find((item) => item.key === key);
    return target ? modelName(target) : key;
  };
  const failed = result?.failed ?? [];
  const retrying = failed.length > 0;

  const run = async () => {
    setBusy(true);
    setError(null);
    try {
      const summary = summarizeUpgrade(await upgradePluginModels(plugin.key), nameOf);
      if (!aliveRef.current) return;
      setResult(summary);
      setUpgradedTotal((total) => total + summary.upgraded);
      if (summary.upgraded > 0) {
        toast.success(`${summary.upgraded} 个模型已升级到 v${summary.toVersion}`);
        await onDone();
      }
      if (summary.failed.length === 0) {
        resolve(true);
        onClose();
      }
    } catch (cause) {
      // 全局 toast 已弹（插件已停用、目标版本不存在等）；原因同时留在框里
      if (aliveRef.current) setError(errorMessage(cause, "升级失败"));
    } finally {
      if (aliveRef.current) setBusy(false);
    }
  };

  const cancel = () => {
    resolve(false);
    onClose();
  };

  return (
    <ConfirmDialog
      open={open}
      title={
        retrying
          ? `${failed.length} 个模型升级失败`
          : `把 ${targets.length} 个模型升级到 v${toVersion}？`
      }
      confirmLabel={retrying ? "重试失败项" : "全部升级"}
      busy={busy}
      error={error}
      description={
        retrying
          ? `已升级 ${upgradedTotal} 个模型到 v${toVersion}。失败的模型保持原样，原因见下方。`
          : "升级后新任务用新版本，进行中的任务按旧版本跑完；新版本不再声明的协议设置项会被丢弃；建议升级后对相关模型“测试模型”一次。逐个处理，失败不中断，失败原因留在这里。"
      }
      onConfirm={() => void run()}
      onCancel={cancel}
      onExited={onExited}
    >
      {retrying ? (
        <ul
          aria-label="升级失败的模型"
          className="bg-destructive/5 flex max-h-56 flex-col gap-1.5 overflow-y-auto rounded-lg border p-3 text-xs"
        >
          {failed.map((item) => (
            <li key={item.key}>
              <span className="font-medium">{item.name}</span>
              <span className="text-destructive block break-words">{item.reason}</span>
            </li>
          ))}
        </ul>
      ) : (
        <ul
          aria-label="要升级的模型"
          className="bg-muted/40 flex max-h-56 flex-col gap-1.5 overflow-y-auto rounded-lg border p-3 text-xs"
        >
          {targets.map((model) => (
            <li key={model.key} className="flex flex-wrap items-baseline gap-x-2">
              <span className="font-medium">{modelName(model)}</span>
              <span className="text-muted-foreground">
                {(model.channel && channelNames[model.channel]) || model.channel || "—"}
              </span>
              <span className="text-muted-foreground ml-auto font-mono">
                v{model.plugin_version} → v{toVersion}
              </span>
            </li>
          ))}
        </ul>
      )}
    </ConfirmDialog>
  );
}

/**
 * 一键把协议固定在插件旧版本上的模型全部升级到最新版本（插件页、总览页共用）。
 * 模型清单取 listModelOptions（plugin / plugin_version 来自协议），渠道名取渠道 options；
 * 没有旧版模型时只提示，不开确认框。
 * @param plugin 插件（要带全部版本，用来算最新版本）
 * @param onDone 有模型升级成功后（刷新插件列表与目录）
 * @returns 是否全部升级成功；取消、没有可升级的模型、清单取不到时为 false
 */
export async function confirmUpgradeModels({
  plugin,
  onDone,
}: {
  plugin: PluginView;
  onDone: () => Promise<unknown> | void;
}): Promise<boolean> {
  const latest = latestVersion(plugin);
  if (!latest) return false;
  let models: ConfigListItem[];
  let channels: ChannelOption[];
  try {
    [models, channels] = await Promise.all([listModelOptions(), listChannelOptions()]);
  } catch {
    // 请求失败的全局 toast 已弹，没有清单就不开确认框
    return false;
  }
  const targets = outdatedModels(plugin, models);
  if (targets.length === 0) {
    toast.info("没有固定在旧版本上的模型");
    return false;
  }
  const channelNames = Object.fromEntries(channels.map((item) => [item.key, item.name]));
  return new Promise<boolean>((resolve) => {
    openDialog(UpgradeModelsDialog, {
      plugin,
      toVersion: latest.version,
      targets,
      channelNames,
      onDone,
      resolve,
    });
  });
}
