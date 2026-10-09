import { useEffect, useMemo, useState } from "react";
import { Upload } from "lucide-react";
import { useNavigate, useSearchParams } from "react-router";
import { toast } from "sonner";

import {
  PageHeader,
  PageHeaderActions,
  PageHeaderHeading,
  PageHeaderTitle,
} from "@/components/admin-ui/page-header";
import { Button } from "@/components/ui/button";
import { useAdminStore } from "@/store/admin";
import { parsePluginRoute } from "@/utils/admin/channel-route";
import { canManageInfra } from "@/utils/admin/role";

import { openDeleteDialog } from "../delete-dialog";
import { useAdminOutlet, useModelList } from "../../use-admin";
import { PluginDialog } from "./plugin-dialog";
import { PluginTable } from "./plugin-table";
import { UploadDialog } from "./upload-dialog";
import { confirmUpgradeChannels } from "./upgrade-channels";
import { usePluginActions } from "./use-plugin-actions";

/**
 * 插件页：表格 + 详情弹窗。弹窗状态在 URL 里（刷新不丢，渠道页等处的链接直达）：
 * ?key=<插件 key>[&tab=versions]。
 * 上传、启停、升级渠道、删除版本、删除插件只对运维（canManageInfra）渲染；admin 只读。
 * 删除插件时先关详情弹窗再开删除对话框，两个弹窗不叠着。
 */
export default function PluginsPage() {
  const { catalog } = useAdminOutlet();
  const role = useAdminStore((state) => state.role);
  const canWrite = canManageInfra(role);
  const navigate = useNavigate();
  const [params, setParams] = useSearchParams();
  const [uploadOpen, setUploadOpen] = useState(false);
  const { models } = useModelList();
  const { toggling, toggle } = usePluginActions({
    channels: catalog.channels,
    models,
    onChanged: catalog.reloadPlugins,
  });

  const route = parsePluginRoute(params, catalog.plugins, catalog.pluginsStatus === "ready");
  const routeKey = route.target?.key ?? null;
  const plugin = useMemo(
    () => (routeKey ? (catalog.plugins.find((item) => item.key === routeKey) ?? null) : null),
    [routeKey, catalog.plugins],
  );

  // 链接里的插件已经不存在（被删了）：提示一次并清掉参数
  useEffect(() => {
    if (!route.missing) return;
    toast.error(`插件 ${route.missing} 不存在`);
    setParams({}, { replace: true });
  }, [route.missing, setParams]);

  const closeDialog = () => setParams({}, { replace: true });
  const requestDelete = (target: { key: string; name: string }) =>
    openDeleteDialog({
      target: "plugin",
      objectKey: target.key,
      name: target.name,
      onDeleted: () => {
        setParams({}, { replace: true });
        void catalog.reloadPlugins();
      },
    });

  return (
    <div className="h-full overflow-y-auto">
      <main className="px-4 py-6 lg:px-6">
        <PageHeader>
          <PageHeaderHeading>
            <PageHeaderTitle>插件</PageHeaderTitle>
          </PageHeaderHeading>
          <PageHeaderActions>
            {canWrite && (
              <Button onClick={() => setUploadOpen(true)}>
                <Upload />
                上传插件
              </Button>
            )}
          </PageHeaderActions>
        </PageHeader>

        <PluginTable
          plugins={catalog.plugins}
          channels={catalog.channels}
          status={catalog.pluginsStatus}
          canWrite={canWrite}
          toggling={toggling}
          onOpen={(key) => setParams({ key })}
          onUpload={() => setUploadOpen(true)}
          onToggle={toggle}
          onUpgrade={(target) =>
            void confirmUpgradeChannels({
              plugin: target,
              channels: catalog.channels,
              plugins: catalog.plugins,
              onDone: catalog.reloadChannels,
            })
          }
          onDelete={requestDelete}
          onRetry={() => void catalog.reloadPlugins()}
        />

        <PluginDialog
          plugin={plugin}
          tab={route.target?.tab ?? "overview"}
          onTabChange={(tab) => routeKey && setParams({ key: routeKey, tab }, { replace: true })}
          plugins={catalog.plugins}
          channels={catalog.channels}
          canWrite={canWrite}
          onChanged={catalog.reloadPlugins}
          onChannelsChanged={catalog.reloadChannels}
          onClose={closeDialog}
        />

        {canWrite && (
          <UploadDialog
            open={uploadOpen}
            onClose={() => setUploadOpen(false)}
            onUploaded={() => void catalog.reloadPlugins()}
            onNextStep={(pluginKey) => {
              setUploadOpen(false);
              navigate(`/admin/ai/channels?new=1&plugin=${encodeURIComponent(pluginKey)}`);
            }}
          />
        )}
      </main>
    </div>
  );
}
