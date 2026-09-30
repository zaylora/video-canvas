import { useState } from "react";
import { useNavigate, useSearchParams } from "react-router";

import { useAdminStore } from "@/store/admin";
import { canManageInfra } from "@/utils/admin/role";

import { useAdminOutlet } from "../use-admin";
import { PluginDetail } from "./plugin-detail";
import { PluginList } from "./plugin-list";
import { UploadDialog } from "./upload-dialog";

/**
 * 插件页：左栏插件列表、右栏详情（主从）。?key=<插件 key> 选中某个插件（深链）。
 * 上传、启停、删版本只对运维（canManageInfra）渲染；admin 只读。
 */
export default function PluginsPage() {
  const { catalog } = useAdminOutlet();
  const role = useAdminStore((state) => state.role);
  const canWrite = canManageInfra(role);
  const navigate = useNavigate();
  const [params, setParams] = useSearchParams();
  const [uploadOpen, setUploadOpen] = useState(false);

  const selectedKey = params.get("key");
  const selected = catalog.plugins.find((plugin) => plugin.key === selectedKey) ?? catalog.plugins[0] ?? null;

  return (
    <div className="grid h-full min-h-0 grid-cols-1 md:grid-cols-[19rem_1fr]">
      <aside className="max-h-72 min-h-0 border-b md:max-h-none md:border-r md:border-b-0">
        <PluginList
          plugins={catalog.plugins}
          status={catalog.pluginsStatus}
          selectedKey={selected?.key ?? null}
          onSelect={(key) => setParams({ key })}
          onUpload={canWrite ? () => setUploadOpen(true) : undefined}
          onRetry={() => void catalog.reloadPlugins()}
        />
      </aside>
      <section className="min-h-0 overflow-y-auto p-6">
        {selected ? (
          <PluginDetail
            key={selected.key}
            plugin={selected}
            canWrite={canWrite}
            onChanged={catalog.reloadPlugins}
          />
        ) : (
          <p className="text-muted-foreground text-sm">
            {catalog.pluginsStatus === "loading" ? "加载中…" : "选择左侧的插件查看详情。"}
          </p>
        )}
      </section>

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
    </div>
  );
}
