import { useMemo } from "react";
import { useNavigate, useSearchParams } from "react-router";
import { toast } from "sonner";

import { updateChannel } from "@/api/admin-ai";
import type { ChannelView } from "@/api/admin-ai/type";
import { AdminMain } from "@/components/admin-ui/admin-main";
import { confirm } from "@/components/admin-ui/confirm-dialog";
import {
  PageHeader,
  PageHeaderDescription,
  PageHeaderHeading,
  PageHeaderTitle,
} from "@/components/admin-ui/page-header";
import { useAdminStore } from "@/store/admin";
import { openDialog } from "@/store/dialog";
import { canManageInfra } from "@/utils/admin/role";

import { openDeleteDialog } from "../delete-dialog";
import { useModelRowActions } from "../models/use-model-row-actions";
import { ReadOnlyNotice } from "../shared";
import { useAdminOutlet, useModelList } from "../use-admin";
import { ChannelMaster } from "./channel-master";
import { ChannelSheet, type ChannelSheetTarget } from "./channel-sheet";
import { ImportDialog } from "./import-dialog";
import { openSecretDialog } from "./secret-dialog";
import { useChannelChecks } from "./use-channel-check";
import { useChannelLoads } from "./use-channel-loads";

/**
 * 渠道页：列表 + 侧边抽屉。
 * - ?key=<key> 选中左栏某个渠道（深链，刷新不丢）；
 * - ?edit=<key>|new 打开抽屉（new 可带 &plugin=<插件 key> 预选插件），同时选中该渠道。
 * 页面级弹窗（设置 Key、导入、停用确认、删除、回滚）都走全局弹窗 store。
 * 新建、检查、设 Key、停用、删除只对运维渲染；导入 admin 也能用；admin 打开抽屉为只读。
 */
export default function ChannelsPage() {
  const { catalog } = useAdminOutlet();
  const role = useAdminStore((state) => state.role);
  const canWrite = canManageInfra(role);
  const navigate = useNavigate();
  const [params, setParams] = useSearchParams();
  const { checks, run: runCheck } = useChannelChecks();
  const loads = useChannelLoads(catalog.channelsStatus === "ready");
  // “使用这个渠道的模型”
  const { models, status: modelsStatus, reload: reloadModels } = useModelList();
  const row = useModelRowActions(catalog, reloadModels);

  const edit = params.get("edit");
  const presetPlugin = params.get("plugin") ?? undefined;
  // 抽屉靠 useRetained 在关闭时保留内容，target 要是稳定引用，所以用 useMemo
  const target = useMemo<ChannelSheetTarget | null>(() => {
    if (edit === "new") return canWrite ? { kind: "new", pluginKey: presetPlugin } : null;
    const channel = edit ? catalog.channels.find((item) => item.key === edit) : undefined;
    return channel ? { kind: "edit", channel } : null;
  }, [edit, canWrite, presetPlugin, catalog.channels]);
  // 选中项：?key 优先，其次是正在编辑的渠道
  const selectedKey = params.get("key") ?? (edit && edit !== "new" ? edit : null);

  const select = (key: string) => setParams({ key });
  const openEdit = (key: string) => setParams({ key, edit: key });
  const closeSheet = () => setParams(selectedKey ? { key: selectedKey } : {}, { replace: true });
  const editMissing =
    !!edit &&
    edit !== "new" &&
    catalog.channelsStatus === "ready" &&
    !catalog.channels.some((item) => item.key === edit);

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

  return (
    <div className="h-full overflow-y-auto">
      <AdminMain>
        <div className="flex flex-col">
          <PageHeader>
            <PageHeaderHeading>
              <PageHeaderTitle>渠道</PageHeaderTitle>
              <PageHeaderDescription>
                渠道把一个插件版本、一个地址和一个 Key 绑在一起，模型通过渠道调用上游。
              </PageHeaderDescription>
            </PageHeaderHeading>
          </PageHeader>
          {!canWrite && (
            <ReadOnlyNotice className="mb-4" what="新建、修改、删除渠道，设置 Key，检查连通性" />
          )}
          {editMissing && <p className="text-destructive text-sm">渠道 {edit} 不存在。</p>}
          <ChannelMaster
            channels={catalog.channels}
            plugins={catalog.plugins}
            status={catalog.channelsStatus}
            canWrite={canWrite}
            checks={checks}
            loads={loads}
            selectedKey={selectedKey}
            onSelect={select}
            onNew={() => setParams({ edit: "new" })}
            onEdit={openEdit}
            onCheck={(key) => void runCheck(key)}
            onImport={openImport}
            onRetry={() => void catalog.reloadChannels()}
            models={models}
            modelsStatus={modelsStatus}
            busyModelKey={row.busyKey}
            onSetKey={(channel) =>
              openSecretDialog({
                channelKey: channel.key,
                channelName: channel.name,
                secretSet: channel.secret_set,
                onSaved: () => void catalog.reloadChannels(),
              })
            }
            onToggleChannel={requestToggle}
            onDeleteChannel={requestDelete}
            onEditModel={(key) => navigate(`/admin/ai/models?key=${encodeURIComponent(key)}`)}
            onTestModel={(key) =>
              navigate(`/admin/ai/models?key=${encodeURIComponent(key)}&test=1`)
            }
            onNewModel={(key) =>
              navigate(`/admin/ai/models/new?channel=${encodeURIComponent(key)}`)
            }
            onToggleModel={(key, enabled) => void row.toggleEnabled(key, enabled)}
            onRollbackModel={row.requestRollback}
            onDeleteModel={row.requestDelete}
          />
        </div>

        <ChannelSheet
          target={target}
          plugins={catalog.plugins}
          pluginsReady={catalog.pluginsStatus === "ready"}
          canWrite={canWrite}
          checks={checks}
          existingKeys={catalog.channels.map((item) => item.key)}
          onCheck={(key) => void runCheck(key)}
          onSaved={() => {
            void catalog.reloadChannels();
          }}
          onImport={(channel) => {
            // 先关抽屉再开导入弹窗：子弹窗不叠在抽屉上，Esc 不会误关
            setParams({ key: channel.key }, { replace: true });
            openImport(channel);
          }}
          onClose={closeSheet}
        />
      </AdminMain>
    </div>
  );
}
