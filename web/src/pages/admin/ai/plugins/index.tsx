import { useState } from "react";
import { useNavigate, useSearchParams } from "react-router";

import { useAdminStore } from "@/store/admin";
import { canManageInfra } from "@/utils/admin/role";

import { Upload } from "lucide-react";

import { Button } from "@/components/ui/button";

import {
  PageHeader,
  PageHeaderActions,
  PageHeaderDescription,
  PageHeaderHeading,
  PageHeaderTitle,
} from "@/components/admin-ui/page-header";
import { openDeleteDialog } from "../delete-dialog";
import { useAdminOutlet, useModelList } from "../../use-admin";
import { PluginDetail } from "./plugin-detail";
import { PluginList } from "./plugin-list";
import { UploadDialog } from "./upload-dialog";

/**
 * 插件页：左栏插件列表、右栏详情（主从）。?key=<插件 key> 选中某个插件（深链）。
 * 上传、启停、删版本、删除插件只对运维（canManageInfra）渲染；admin 只读。
 */
export default function PluginsPage() {
  const { catalog } = useAdminOutlet();
  const role = useAdminStore((state) => state.role);
  const canWrite = canManageInfra(role);
  const navigate = useNavigate();
  const [params, setParams] = useSearchParams();
  const [uploadOpen, setUploadOpen] = useState(false);
  const { models } = useModelList();

  const selectedKey = params.get("key");
  const selected =
    catalog.plugins.find((plugin) => plugin.key === selectedKey) ?? catalog.plugins[0] ?? null;

  return (
    <div className="h-full overflow-y-auto">
      <main className="px-4 py-6 lg:px-6">
        <PageHeader>
          <PageHeaderHeading>
            <PageHeaderTitle>插件</PageHeaderTitle>
            <PageHeaderDescription>
              插件把一种上游协议翻译成统一的请求与结果；渠道固定在插件的某个版本上，版本登记后不可变。
            </PageHeaderDescription>
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
        <div className="grid grid-cols-1 items-start gap-4 xl:grid-cols-[17rem_minmax(0,1fr)]">
          <PluginList
            plugins={catalog.plugins}
            status={catalog.pluginsStatus}
            selectedKey={selected?.key ?? null}
            onSelect={(key) => setParams({ key })}
            onRetry={() => void catalog.reloadPlugins()}
          />
          <section className="min-w-0">
            {selected ? (
              <PluginDetail
                key={selected.key}
                plugin={selected}
                plugins={catalog.plugins}
                channels={catalog.channels}
                onChannelsChanged={catalog.reloadChannels}
                models={models}
                canWrite={canWrite}
                onChanged={catalog.reloadPlugins}
                onDelete={() =>
                  openDeleteDialog({
                    target: "plugin",
                    objectKey: selected.key,
                    name: selected.name,
                    onDeleted: () => {
                      setParams({}, { replace: true });
                      void catalog.reloadPlugins();
                    },
                  })
                }
              />
            ) : (
              <p className="text-muted-foreground text-sm">
                {catalog.pluginsStatus === "loading" ? "加载中…" : "选择左侧的插件查看详情。"}
              </p>
            )}
          </section>
        </div>

        {canWrite && (
          <UploadDialog
            open={uploadOpen}
            onClose={() => setUploadOpen(false)}
            onUploaded={() => void catalog.reloadPlugins()}
            onNextStep={(pluginKey) => {
              setUploadOpen(false);
              navigate(`/admin/ai/channels?edit=new&plugin=${encodeURIComponent(pluginKey)}`);
            }}
          />
        )}
      </main>
    </div>
  );
}
