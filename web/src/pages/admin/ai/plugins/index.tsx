import { useCallback, useEffect, useState } from "react";
import { Upload } from "lucide-react";
import { useNavigate } from "react-router";
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
import type { PluginTab } from "@/utils/admin/channel-route";
import { dialogEntry } from "@/utils/admin/dialog-entry";
import { canManageInfra } from "@/utils/admin/role";

import { openDeleteDialog } from "../delete-dialog";
import { useAdminCatalogOptions } from "../../use-admin";
import { useAdminList } from "../../use-admin-list";
import { useDialogTarget } from "../../use-dialog-target";
import { PluginDialog } from "./plugin-dialog";
import { PluginTable } from "./plugin-table";
import { UploadDialog } from "./upload-dialog";
import { confirmUpgradeChannels } from "./upgrade-channels";
import { usePluginActions } from "./use-plugin-actions";

/** 列表认的筛选参数：能力、来源、状态 */
const PLUGIN_FILTERS = ["kind", "source", "status"] as const;

/** 插件弹窗要打开的插件，页签不给停在概览 */
type PluginDialogOpen = { key: string; tab?: PluginTab };

/**
 * 插件页：表格 + 详情弹窗。弹窗开着哪个插件放在页面自己的 state 里，不进 URL
 * （进了 URL 会把类型、搜索、页码筛选冲掉）；别的页面要直达，用 dialogEntry 经路由 state 带过来。
 * 点了就先开弹窗，列表 / 目录里已有的内容先渲染，详情在后台请求，回来后换成最新的。
 * 上传、启停、升级渠道、删除版本、删除插件只对运维（canManageInfra）渲染；admin 只读。
 * 删除插件时先关详情弹窗再开删除对话框，两个弹窗不叠着。
 */
export default function PluginsPage() {
  const { catalog } = useAdminCatalogOptions();
  const role = useAdminStore((state) => state.role);
  const canWrite = canManageInfra(role);
  const navigate = useNavigate();
  const [uploadOpen, setUploadOpen] = useState(false);
  // 插件分页列表（搜索 / 筛选 / 翻页都在服务端）；弹窗要的全量插件和渠道在 catalog 里（options）
  const list = useAdminList<PluginListItem>(PLUGIN_FILTERS, (query) => listPlugins(query));
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

  // 弹窗里的插件按 key 单独取，不依赖列表里有没有这一行（别页直达、翻页都不影响）
  const [open, setOpen] = useDialogTarget<PluginDialogOpen>();
  const routeKey = open?.key ?? null;
  const [detail, setDetail] = useState<PluginView | null>(null);
  /** 弹窗里的改动（删版本、启停）后加一，重新取详情 */
  const [detailVersion, setDetailVersion] = useState(0);
  useEffect(() => {
    if (!routeKey) return;
    let cancelled = false;
    getPlugin(routeKey)
      .then((next) => !cancelled && setDetail(next))
      .catch(() => {
        // 插件已经不存在（被删了）：提示一次并关掉弹窗
        if (cancelled) return;
        toast.error(`插件 ${routeKey} 不存在`);
        setOpen(null);
      });
    return () => {
      cancelled = true;
    };
  }, [routeKey, detailVersion, setOpen]);
  /** 头部用目录里已有的这一行；内容区只认详情接口返回的，换了插件时不能拿上一个的顶着 */
  const summary = routeKey ? (catalog.plugins.find((item) => item.key === routeKey) ?? null) : null;
  const plugin = routeKey && detail?.key === routeKey ? detail : null;

  const closeDialog = () => setOpen(null);
  const requestDelete = (target: { key: string; name: string }) =>
    openDeleteDialog({
      target: "plugin",
      objectKey: target.key,
      name: target.name,
      onDeleted: () => {
        setOpen(null);
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
        onOpen={(key) => setOpen({ key })}
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
        open={!!open}
        pluginKey={routeKey}
        summary={summary}
        detail={plugin}
        tab={open?.tab ?? "overview"}
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
            navigate("/admin/ai/channels", { state: dialogEntry({ kind: "new", pluginKey }) });
          }}
        />
      )}
    </DataPage>
  );
}
