import { useNavigate, useSearchParams } from "react-router";

import { useAdminStore } from "@/store/admin";
import { canManageInfra } from "@/utils/admin/role";

import { AdminMain } from "@/components/admin-ui/admin-main";
import {
  PageHeader,
  PageHeaderDescription,
  PageHeaderHeading,
  PageHeaderTitle,
} from "@/components/admin-ui/page-header";
import { ReadOnlyNotice } from "../shared";
import { useAdminOutlet } from "../use-admin";
import { ChannelSheet, type ChannelSheetTarget } from "./channel-sheet";
import { ChannelMaster } from "./channel-master";
import { ImportDialog } from "./import-dialog";
import { useChannelChecks } from "./use-channel-check";
import { useChannelLoads } from "./use-channel-loads";
import { listModels, updateChannel } from "@/api/admin-ai";
import type { ChannelView, ConfigListItem } from "@/api/admin-ai/type";
import { ConfirmDialog } from "@/components/admin-ui/confirm-dialog";
import { useCallback, useEffect, useState } from "react";
import { toast } from "sonner";

import { RollbackDialog } from "../models/publish-dialog";
import { useModelRowActions } from "../models/use-model-row-actions";
import type { LoadStatus } from "../use-admin";
import { SecretDialog } from "./secret-dialog";

/**
 * 渠道页：列表 + 侧边抽屉。?edit=<key>|new 打开抽屉（深链，new 可带 &plugin=<插件 key> 预选插件）。
 * 新建、检查、设 Key、危险开关只对运维渲染；导入 admin 也能用；admin 打开抽屉为只读。
 */
export default function ChannelsPage() {
  const { catalog } = useAdminOutlet();
  const role = useAdminStore((state) => state.role);
  const canWrite = canManageInfra(role);
  const navigate = useNavigate();
  const [params, setParams] = useSearchParams();
  const { checks, run: runCheck } = useChannelChecks();
  const loads = useChannelLoads(catalog.channelsStatus === "ready");
  const [importing, setImporting] = useState<ChannelView | null>(null);
  const [keyTarget, setKeyTarget] = useState<ChannelView | null>(null);
  const [toggleTarget, setToggleTarget] = useState<ChannelView | null>(null);
  const [toggling, setToggling] = useState(false);
  // “使用这个渠道的模型”：模型清单只在渠道页用，在这里取一次
  const [models, setModels] = useState<ConfigListItem[]>([]);
  const [modelsStatus, setModelsStatus] = useState<LoadStatus>("loading");
  const reloadModels = useCallback(async () => {
    try {
      setModels(await listModels());
      setModelsStatus("ready");
    } catch {
      setModelsStatus("error");
    }
  }, []);
  useEffect(() => {
    void reloadModels();
  }, [reloadModels]);
  const row = useModelRowActions(catalog, reloadModels);

  const confirmToggle = async () => {
    if (!toggleTarget) return;
    setToggling(true);
    try {
      await updateChannel(toggleTarget.key, { enabled: !toggleTarget.enabled });
      toast.success(`渠道「${toggleTarget.name}」已${toggleTarget.enabled ? "停用" : "启用"}`);
      setToggleTarget(null);
      await catalog.reloadChannels();
    } finally {
      setToggling(false);
    }
  };

  const edit = params.get("edit");
  const target: ChannelSheetTarget | null =
    edit === "new"
      ? canWrite
        ? { kind: "new", pluginKey: params.get("plugin") ?? undefined }
        : null
      : edit
        ? (() => {
            const channel = catalog.channels.find((item) => item.key === edit);
            return channel ? ({ kind: "edit", channel } as const) : null;
          })()
        : null;

  const openEdit = (key: string) => setParams({ edit: key });
  const closeSheet = () => setParams({}, { replace: true });
  const editMissing =
    !!edit &&
    edit !== "new" &&
    catalog.channelsStatus === "ready" &&
    !catalog.channels.some((item) => item.key === edit);

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
            <ReadOnlyNotice className="mb-4" what="新建、修改渠道，设置 Key，检查连通性" />
          )}
          {editMissing && <p className="text-destructive text-sm">渠道 {edit} 不存在。</p>}
          <ChannelMaster
            channels={catalog.channels}
            plugins={catalog.plugins}
            status={catalog.channelsStatus}
            canWrite={canWrite}
            checks={checks}
            loads={loads}
            onNew={() => setParams({ edit: "new" })}
            onEdit={openEdit}
            onCheck={(key) => void runCheck(key)}
            onImport={setImporting}
            onRetry={() => void catalog.reloadChannels()}
            models={models}
            modelsStatus={modelsStatus}
            busyModelKey={row.busyKey}
            onSetKey={setKeyTarget}
            onToggleChannel={setToggleTarget}
            onEditModel={(key) => navigate(`/admin/ai/models?key=${encodeURIComponent(key)}`)}
            onTestModel={(key) =>
              navigate(`/admin/ai/models?key=${encodeURIComponent(key)}&test=1`)
            }
            onNewModel={(key) =>
              navigate(`/admin/ai/models/new?channel=${encodeURIComponent(key)}`)
            }
            onToggleModel={(key, enabled) => void row.toggleEnabled(key, enabled)}
            onRollbackModel={row.requestRollback}
          />
        </div>

        <ChannelSheet
          target={target}
          plugins={catalog.plugins}
          pluginsReady={catalog.pluginsStatus === "ready"}
          canWrite={canWrite}
          checks={checks}
          onCheck={(key) => void runCheck(key)}
          onSaved={() => {
            void catalog.reloadChannels();
          }}
          onClose={closeSheet}
        />

        <ImportDialog
          channel={importing}
          plugins={catalog.plugins}
          onClose={() => setImporting(null)}
          onImported={(draftId) => {
            setImporting(null);
            navigate(
              draftId
                ? `/admin/ai/models/new?from=import&draft=${encodeURIComponent(draftId)}`
                : "/admin/ai/models/new",
            );
          }}
        />
      </AdminMain>
      <RollbackDialog {...row.rollback} onConfirm={() => void row.rollback.onConfirm()} />
      {keyTarget && (
        <SecretDialog
          open
          channelKey={keyTarget.key}
          channelName={keyTarget.name}
          secretSet={keyTarget.secret_set}
          onClose={() => setKeyTarget(null)}
          onSaved={() => {
            setKeyTarget(null);
            void catalog.reloadChannels();
          }}
        />
      )}
      <ConfirmDialog
        open={!!toggleTarget}
        title={toggleTarget?.enabled ? "停用渠道？" : "启用渠道？"}
        destructive={toggleTarget?.enabled}
        confirmLabel={toggleTarget?.enabled ? "停用" : "启用"}
        busy={toggling}
        description={
          toggleTarget?.enabled
            ? `停用「${toggleTarget.name}」后，使用它的模型不再接新任务，进行中的任务不受影响。`
            : `启用「${toggleTarget?.name ?? ""}」后，使用它的模型可以接新任务。`
        }
        onConfirm={() => void confirmToggle()}
        onCancel={() => setToggleTarget(null)}
      />
    </div>
  );
}
