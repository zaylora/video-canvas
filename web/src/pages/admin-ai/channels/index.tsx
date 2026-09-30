import { useNavigate, useSearchParams } from "react-router";
import { Plus } from "lucide-react";

import { Button } from "@/components/ui/button";
import { useAdminStore } from "@/store/admin";
import { canManageInfra } from "@/utils/admin/role";

import { ReadOnlyNotice } from "../shared";
import { useAdminOutlet } from "../use-admin";
import { ChannelSheet, type ChannelSheetTarget } from "./channel-sheet";
import { ChannelTable } from "./channel-table";
import { ImportDialog } from "./import-dialog";
import { useChannelChecks } from "./use-channel-check";
import { useState } from "react";
import type { ChannelView } from "@/api/admin-ai/type";

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
  const [importing, setImporting] = useState<ChannelView | null>(null);

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
    <div className="h-full overflow-y-auto p-6">
      <div className="mx-auto flex max-w-6xl flex-col gap-4">
        {!canWrite && <ReadOnlyNotice what="新建、修改渠道，设置 Key，检查连通性" />}
        <div className="flex items-center gap-3">
          <div>
            <h2 className="text-lg font-semibold">渠道</h2>
            <p className="text-muted-foreground text-sm">
              渠道把一个插件版本、一个地址和一个 Key 绑在一起，模型通过渠道调用上游。
            </p>
          </div>
          {canWrite && (
            <Button className="ml-auto" onClick={() => setParams({ edit: "new" })}>
              <Plus />
              新建渠道
            </Button>
          )}
        </div>
        {editMissing && <p className="text-destructive text-sm">渠道 {edit} 不存在。</p>}
        <ChannelTable
          channels={catalog.channels}
          plugins={catalog.plugins}
          status={catalog.channelsStatus}
          canWrite={canWrite}
          checks={checks}
          onNew={() => setParams({ edit: "new" })}
          onEdit={openEdit}
          onCheck={(key) => void runCheck(key)}
          onImport={setImporting}
          onRetry={() => void catalog.reloadChannels()}
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
    </div>
  );
}
