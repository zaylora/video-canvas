import { useCallback, useEffect, useMemo, useState } from "react";
import { Plus } from "lucide-react";
import { useNavigate } from "react-router";
import { toast } from "sonner";

import { listChannels, updateChannel } from "@/api/admin/ai";
import type { ChannelListItem } from "@/api/admin/ai/type.d";
import { confirm } from "@/components/admin-ui/confirm-dialog";
import { DataPage } from "@/components/admin-ui/data-panel";
import {
  PageHeader,
  PageHeaderActions,
  PageHeaderHeading,
  PageHeaderTitle,
} from "@/components/admin-ui/page-header";
import { Button } from "@/components/ui/button";
import { useAdminStore } from "@/store/admin";
import { openDialog } from "@/store/dialog";
import {
  resolveChannelDialog,
  type ChannelDialogOpen,
  type ChannelTab,
} from "@/utils/admin/channel-route";
import { dialogEntry } from "@/utils/admin/dialog-entry";
import { canManageInfra } from "@/utils/admin/role";

import { openDeleteDialog } from "../delete-dialog";
import { useModelRowActions } from "../models/use-model-row-actions";
import { useAdminCatalogOptions } from "../../use-admin";
import { useAdminList } from "../../use-admin-list";
import { useDialogTarget } from "../../use-dialog-target";
import type { ChannelModelActions } from "./channel-models";
import { ChannelDialog, type ChannelDialogTarget } from "./channel-dialog";
import { ChannelModelEditor, type ChannelModelEditing } from "./channel-model-editor";
import { ChannelTable } from "./channel-table";
import { ImportDialog } from "./import-dialog";
import { openSecretDialog } from "./secret-dialog";

/** 列表认的筛选参数：能力、插件、状态 */
const CHANNEL_FILTERS = ["kind", "plugin", "status"] as const;

/**
 * 渠道页：表格 + 详情弹窗。弹窗开着什么放在页面自己的 state 里，不进 URL
 * （进了 URL 会把类型、搜索、页码筛选冲掉）；别的页面要直达，用 dialogEntry 经路由 state 带过来
 * （见 ChannelDialogOpen：打开某个渠道的某个页签，或新建并预选插件）。
 * 页面级弹窗（设置 Key、导入、停用确认、删除）都走全局弹窗 store；
 * 从渠道弹窗里导入模型、删除渠道时先关掉渠道弹窗再开对应弹窗（两个弹窗不叠着）；设置 Key 留在原地，设完概览立刻刷新。
 * 新建、检查、设 Key、停用、删除只对运维渲染；导入 admin 也能用；admin 打开弹窗为只读。
 */
export default function ChannelsPage() {
  const { catalog } = useAdminCatalogOptions();
  const role = useAdminStore((state) => state.role);
  const canWrite = canManageInfra(role);
  const navigate = useNavigate();
  const [open, setOpen] = useDialogTarget<ChannelDialogOpen>();
  // 渠道分页列表（搜索 / 筛选 / 翻页都在服务端）；弹窗要的全量渠道和插件在 catalog 里（options）
  const list = useAdminList<ChannelListItem>(CHANNEL_FILTERS, (query) => listChannels(query));
  /** 渠道有改动之后：重新拉当前页，并刷新弹窗用的全量渠道（健康状态、固定版本） */
  const { reload: reloadList } = list;
  const { reloadChannels } = catalog;
  const refreshChannels = useCallback(() => {
    reloadList();
    void reloadChannels();
  }, [reloadList, reloadChannels]);
  /** “模型”页签的刷新信号：模型有改动就加一，页签据此重新拉取 */
  const [modelsRefreshKey, setModelsRefreshKey] = useState(0);
  const reloadModels = useCallback(async () => {
    setModelsRefreshKey((n) => n + 1);
    reloadList(); // 渠道行上的模型数也要跟着变
  }, [reloadList]);
  const row = useModelRowActions(reloadModels);

  const route = resolveChannelDialog(open, catalog.channels, {
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

  const closeDialog = () => setOpen(null);

  // 渠道弹窗“模型”页签里新建 / 编辑模型：叠在渠道弹窗上，关掉后渠道弹窗保持原样
  const [editing, setEditing] = useState<ChannelModelEditing | null>(null);
  const closeModelEditor = useCallback(() => setEditing(null), []);
  const refreshModels = useCallback(() => void reloadModels(), [reloadModels]);
  /** 弹窗用的全量插件和渠道要先到位，才能判断深链里的渠道存不存在 */

  // 要打开的渠道已经不存在（被删了）：提示一次并清掉
  useEffect(() => {
    if (!route.missing) return;
    toast.error(`渠道 ${route.missing} 不存在`);
    setOpen(null);
  }, [route.missing, setOpen]);

  const openImport = (channel: ChannelListItem) =>
    openDialog(ImportDialog, {
      channel,
      plugins: catalog.plugins,
      onSaved: () => void reloadModels(),
      onImported: (draftId: string | null) =>
        navigate("/admin/ai/models", {
          state: dialogEntry(
            draftId
              ? { isNew: true, fromImport: true, draftId }
              : { isNew: true, presetChannel: channel.key },
          ),
        }),
    });

  const openSetKey = (channel: ChannelListItem) =>
    openSecretDialog({
      channelKey: channel.key,
      channelName: channel.name,
      secretSet: channel.secret_set,
      onSaved: refreshChannels,
    });

  const requestToggle = (channel: ChannelListItem) => {
    const used = channel.online_model_count;
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
        refreshChannels();
      },
    });
  };

  const requestDelete = (channel: ChannelListItem) =>
    openDeleteDialog({
      target: "channel",
      objectKey: channel.key,
      name: channel.name,
      channels: catalog.channels,
      onDeleted: () => {
        setOpen(null);
        refreshChannels();
        void reloadModels();
      },
      onChanged: () => void reloadModels(),
    });

  const modelActions: ChannelModelActions = {
    busyKey: row.busyKey,
    onEdit: (key, channel) => setEditing({ channel, key }),
    onTest: (key) => navigate("/admin/ai/models", { state: dialogEntry({ key, test: true }) }),
    onNew: (channel) => setEditing({ channel, key: null }),
    onToggleEnabled: (key, enabled) => void row.toggleEnabled(key, enabled),
    onDelete: row.requestDelete,
  };

  return (
    <DataPage>
      <PageHeader>
        <PageHeaderHeading>
          <PageHeaderTitle>渠道</PageHeaderTitle>
        </PageHeaderHeading>
        <PageHeaderActions>
          {canWrite && (
            <Button onClick={() => setOpen({ kind: "new" })}>
              <Plus />
              新建渠道
            </Button>
          )}
        </PageHeaderActions>
      </PageHeader>

      <ChannelTable
        list={list}
        plugins={catalog.plugins}
        canWrite={canWrite}
        onOpen={(key) => setOpen({ kind: "edit", key })}
        onNew={() => setOpen({ kind: "new" })}
        onImport={openImport}
        onSetKey={openSetKey}
        onToggle={requestToggle}
        onDelete={requestDelete}
      />

      <ChannelDialog
        target={target}
        initialTab={tab}
        plugins={catalog.plugins}
        pluginsReady={catalog.pluginsStatus === "ready"}
        canWrite={canWrite}
        channels={catalog.channels}
        modelsRefreshKey={modelsRefreshKey}
        modelActions={modelActions}
        onSaved={refreshChannels}
        onClose={closeDialog}
      >
        <ChannelModelEditor
          catalog={catalog}
          editing={editing}
          onChange={setEditing}
          onClose={closeModelEditor}
          onListChanged={refreshModels}
        />
      </ChannelDialog>
    </DataPage>
  );
}
