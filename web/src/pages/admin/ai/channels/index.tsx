import { useEffect, useMemo } from "react";
import { Plus } from "lucide-react";
import { useNavigate, useSearchParams } from "react-router";
import { toast } from "sonner";

import { updateChannel } from "@/api/admin/ai";
import type { ChannelView } from "@/api/admin/ai/type.d";
import { confirm } from "@/components/admin-ui/confirm-dialog";
import {
  PageHeader,
  PageHeaderActions,
  PageHeaderDescription,
  PageHeaderHeading,
  PageHeaderTitle,
} from "@/components/admin-ui/page-header";
import { Button } from "@/components/ui/button";
import { useAdminStore } from "@/store/admin";
import { openDialog } from "@/store/dialog";
import { parseChannelRoute, type ChannelTab } from "@/utils/admin/channel-route";
import { canManageInfra } from "@/utils/admin/role";

import { openDeleteDialog } from "../delete-dialog";
import { useModelRowActions } from "../models/use-model-row-actions";
import { useAdminOutlet, useModelList } from "../../use-admin";
import type { ChannelModelActions } from "./channel-models";
import { ChannelDialog, type ChannelDialogTarget } from "./channel-dialog";
import { ChannelTable } from "./channel-table";
import { ImportDialog } from "./import-dialog";
import { openSecretDialog } from "./secret-dialog";

/**
 * 渠道页：表格 + 详情弹窗。弹窗的状态都在 URL 里（刷新不丢，别处的跳转链接也能直达）：
 * ?key=<key>[&tab=config|models] 打开渠道，?new=1[&plugin=<插件 key>] 新建；
 * 旧链接 ?edit=<key> / ?edit=new 仍然有效（见 parseChannelRoute）。
 * 页面级弹窗（设置 Key、导入、停用确认、删除、回滚）都走全局弹窗 store；
 * 从渠道弹窗里导入模型、删除渠道时先关掉渠道弹窗再开对应弹窗（两个弹窗不叠着）；设置 Key 留在原地，设完概览立刻刷新。
 * 新建、检查、设 Key、停用、删除只对运维渲染；导入 admin 也能用；admin 打开弹窗为只读。
 */
export default function ChannelsPage() {
  const { catalog } = useAdminOutlet();
  const role = useAdminStore((state) => state.role);
  const canWrite = canManageInfra(role);
  const navigate = useNavigate();
  const [params, setParams] = useSearchParams();
  // 模型数、弹窗里“模型”页签
  const { models, status: modelsStatus, reload: reloadModels } = useModelList();
  const row = useModelRowActions(catalog, reloadModels);

  const route = parseChannelRoute(params, catalog.channels, {
    canWrite,
    ready: catalog.channelsStatus === "ready",
  });
  const routeTarget = route.target;
  const routeKey = routeTarget?.kind === "edit" ? routeTarget.key : null;
  const routePlugin = routeTarget?.kind === "new" ? routeTarget.pluginKey : undefined;
  const isNew = routeTarget?.kind === "new";
  // 弹窗靠 useRetained 在关闭时保留内容，target 要是稳定引用，所以用 useMemo
  const target = useMemo<ChannelDialogTarget | null>(() => {
    if (isNew) return { kind: "new", pluginKey: routePlugin };
    const channel = routeKey ? catalog.channels.find((item) => item.key === routeKey) : undefined;
    return channel ? { kind: "edit", channel } : null;
  }, [isNew, routePlugin, routeKey, catalog.channels]);
  const tab: ChannelTab = routeTarget?.kind === "edit" ? routeTarget.tab : "config";

  const closeDialog = () => setParams({}, { replace: true });

  // 链接里的渠道已经不存在（被删了）：提示一次并清掉参数
  useEffect(() => {
    if (!route.missing) return;
    toast.error(`渠道 ${route.missing} 不存在`);
    setParams({}, { replace: true });
  }, [route.missing, setParams]);

  const openImport = (channel: ChannelView) =>
    openDialog(ImportDialog, {
      channel,
      plugins: catalog.plugins,
      onSaved: () => void reloadModels(),
      onImported: (draftId: string | null) =>
        navigate(
          draftId
            ? `/admin/ai/models/new?from=import&draft=${encodeURIComponent(draftId)}`
            : `/admin/ai/models/new?channel=${encodeURIComponent(channel.key)}`,
        ),
    });

  const openSetKey = (channel: ChannelView) =>
    openSecretDialog({
      channelKey: channel.key,
      channelName: channel.name,
      secretSet: channel.secret_set,
      onSaved: () => void catalog.reloadChannels(),
    });

  const requestToggle = (channel: ChannelView) => {
    const used = models.filter((item) => item.channel === channel.key && item.enabled).length;
    void confirm({
      title: channel.enabled ? "停用渠道？" : "启用渠道？",
      destructive: channel.enabled,
      confirmLabel: channel.enabled ? "停用" : "启用",
      description: channel.enabled
        ? `停用「${channel.name}」后，使用它的${used ? ` ${used} 个上架模型` : "模型"}不再接新任务，进行中的任务不受影响。`
        : `启用「${channel.name}」后，使用它的模型可以接新任务。`,
      onConfirm: async () => {
        await updateChannel(channel.key, { enabled: !channel.enabled });
        toast.success(`渠道「${channel.name}」已${channel.enabled ? "停用" : "启用"}`);
        await catalog.reloadChannels();
      },
    });
  };

  const requestDelete = (channel: ChannelView) =>
    openDeleteDialog({
      target: "channel",
      objectKey: channel.key,
      name: channel.name,
      channels: catalog.channels,
      plugins: catalog.plugins,
      onDeleted: () => {
        setParams({}, { replace: true });
        void catalog.reloadChannels();
        void reloadModels();
      },
      onChanged: () => void reloadModels(),
    });

  const modelActions: ChannelModelActions = {
    busyKey: row.busyKey,
    onEdit: (key) => navigate(`/admin/ai/models?key=${encodeURIComponent(key)}`),
    onTest: (key) => navigate(`/admin/ai/models?key=${encodeURIComponent(key)}&test=1`),
    onNew: (channelKey) =>
      navigate(`/admin/ai/models/new?channel=${encodeURIComponent(channelKey)}`),
    onToggleEnabled: (key, enabled) => void row.toggleEnabled(key, enabled),
    onRollback: row.requestRollback,
    onDelete: row.requestDelete,
  };

  return (
    <div className="h-full overflow-y-auto">
      <main className="px-4 py-6 lg:px-6">
        <PageHeader>
          <PageHeaderHeading>
            <PageHeaderTitle>渠道</PageHeaderTitle>
            <PageHeaderDescription>
              渠道把一个插件版本、一个地址和一个 Key 绑在一起，模型通过渠道调用上游。
            </PageHeaderDescription>
          </PageHeaderHeading>
          <PageHeaderActions>
            {canWrite && (
              <Button onClick={() => setParams({ new: "1" })}>
                <Plus />
                新建渠道
              </Button>
            )}
          </PageHeaderActions>
        </PageHeader>

        <ChannelTable
          channels={catalog.channels}
          plugins={catalog.plugins}
          status={catalog.channelsStatus}
          canWrite={canWrite}
          models={models}
          modelsStatus={modelsStatus}
          onOpen={(key) => setParams({ key })}
          onNew={() => setParams({ new: "1" })}
          onImport={openImport}
          onSetKey={openSetKey}
          onToggle={requestToggle}
          onDelete={requestDelete}
          onRetry={() => void catalog.reloadChannels()}
        />

        <ChannelDialog
          target={target}
          initialTab={tab}
          plugins={catalog.plugins}
          pluginsReady={catalog.pluginsStatus === "ready"}
          canWrite={canWrite}
          channels={catalog.channels}
          models={models}
          modelsStatus={modelsStatus}
          modelActions={modelActions}
          onTabChange={(next) =>
            routeKey && setParams({ key: routeKey, tab: next }, { replace: true })
          }
          onSaved={() => void catalog.reloadChannels()}
          onClose={closeDialog}
        />
      </main>
    </div>
  );
}
