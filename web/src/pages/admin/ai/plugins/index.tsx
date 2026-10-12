import { useCallback, useEffect, useState } from "react";
import { Upload } from "lucide-react";
import { useNavigate, useSearchParams } from "react-router";
import { toast } from "sonner";

import { getPlugin, listPlugins } from "@/api/admin/ai";
import type { PluginListItem, PluginView } from "@/api/admin/ai/type.d";
import { DataPage } from "@/components/admin-ui/data-panel";
import {
  PageHeader,
  PageHeaderActions,
  PageHeaderHeading,
  PageHeaderTitle,
} from "@/components/admin-ui/page-header";
import { Button } from "@/components/ui/button";
import { useAdminStore } from "@/store/admin";
import { canManageInfra } from "@/utils/admin/role";

import { openDeleteDialog } from "../delete-dialog";
import { useAdminCatalogOptions } from "../../use-admin";
import { useAdminList } from "../../use-admin-list";
import { PluginDialog } from "./plugin-dialog";
import { PluginTable } from "./plugin-table";
import { UploadDialog } from "./upload-dialog";
import { confirmUpgradeChannels } from "./upgrade-channels";
import { usePluginActions } from "./use-plugin-actions";

/** 列表认的筛选参数：能力、来源、状态 */
const PLUGIN_FILTERS = ["kind", "source", "status"] as const;

/**
 * 插件页：表格 + 详情弹窗。弹窗状态在 URL 里（刷新不丢，渠道页等处的链接直达）：
 * ?key=<插件 key>[&tab=versions]。
 * 上传、启停、升级渠道、删除版本、删除插件只对运维（canManageInfra）渲染；admin 只读。
 * 删除插件时先关详情弹窗再开删除对话框，两个弹窗不叠着。
 */
export default function PluginsPage() {
  const { catalog } = useAdminCatalogOptions();
  const role = useAdminStore((state) => state.role);
  const canWrite = canManageInfra(role);
  const navigate = useNavigate();
  const [params, setParams] = useSearchParams();
  const [uploadOpen, setUploadOpen] = useState(false);
  // 插件分页列表（搜索 / 筛选 / 翻页都在服务端）；弹窗要的全量插件和渠道在 catalog 里（options）
  const list = useAdminList<PluginListItem>(PLUGIN_FILTERS, (query) =>
    listPlugins(query),
  );
  /** 插件有改动之后：重新拉当前页，并刷新弹窗用的全量插件 */
  const { reload: reloadList } = list;
  const { reloadPlugins, reloadChannels } = catalog;
  const refresh = useCallback(async () => {
    reloadList();
    await reloadPlugins();
  }, [reloadList, reloadPlugins]);
  const refreshChannels = useCallback(async () => {
    reloadList(); // 行上的旧版渠道数要跟着变
    await reloadChannels();
  }, [reloadList, reloadChannels]);
  const { toggling, toggle } = usePluginActions({ onChanged: refresh });

  // 弹窗里的插件按 ?key= 单独取，不依赖列表里有没有这一行（深链、翻页都不影响）
  const routeKey = params.get("key");
  const tab = params.get("tab") === "versions" ? "versions" : "overview";
  const [plugin, setPlugin] = useState<PluginView | null>(null);
  /** 弹窗里的改动（删版本、启停）后加一，重新取详情 */
  const [detailVersion, setDetailVersion] = useState(0);
  useEffect(() => {
    if (!routeKey) return setPlugin(null);
    let cancelled = false;
    getPlugin(routeKey)
      .then((next) => !cancelled && setPlugin(next))
      .catch(() => {
        // 插件已经不存在（被删了）：提示一次并清掉参数
        if (cancelled) return;
        toast.error(`插件 ${routeKey} 不存在`);
        setParams({}, { replace: true });
      });
    return () => {
      cancelled = true;
    };
  }, [routeKey, detailVersion, setParams]);

  const closeDialog = () => setParams({}, { replace: true });
  const requestDelete = (target: { key: string; name: string }) =>
    openDeleteDialog({
      target: "plugin",
      objectKey: target.key,
      name: target.name,
      onDeleted: () => {
        setParams({}, { replace: true });
        void refresh();
      },
    });

  return (
    <DataPage>
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
        list={list}
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
            onDone: refreshChannels,
          })
        }
        onDelete={requestDelete}
      />

      <PluginDialog
        plugin={plugin}
        tab={tab}
        onTabChange={(tab) =>
          routeKey && setParams({ key: routeKey, tab }, { replace: true })
        }
        plugins={catalog.plugins}
        channels={catalog.channels}
        canWrite={canWrite}
        onChanged={async () => {
          setDetailVersion((n) => n + 1);
          await refresh();
        }}
        onChannelsChanged={refreshChannels}
        onClose={closeDialog}
      />

      {canWrite && (
        <UploadDialog
          open={uploadOpen}
          onClose={() => setUploadOpen(false)}
          onUploaded={() => void refresh()}
          onNextStep={(pluginKey) => {
            setUploadOpen(false);
            navigate(
              `/admin/ai/channels?new=1&plugin=${encodeURIComponent(pluginKey)}`,
            );
          }}
        />
      )}
    </DataPage>
  );
}
