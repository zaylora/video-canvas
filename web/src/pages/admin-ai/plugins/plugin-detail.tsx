import { useState } from "react";
import { Loader2, Trash2 } from "lucide-react";

import { deletePluginVersion, setPluginEnabled } from "@/api/admin-ai";
import type { PluginVersionView, PluginView } from "@/api/admin-ai/type";
import { Button } from "@/components/ui/button";
import { Switch } from "@/components/ui/switch";
import {
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableHeader,
  TableRow,
} from "@/components/ui/table";
import { cn } from "@/lib/utils";
import { errorMessage } from "@/utils/admin/errors";
import { latestVersion, shortSha, versionDeleteBlock } from "@/utils/admin/plugin";

import { ConfirmDialog, CopyButton, formatTime, Notice, ReadOnlyNotice, Tag } from "../shared";
import { useAliveRef } from "../use-admin";
import { MetaView } from "./meta-view";

/** 上传人：内置插件（0）显示“内置”，其余显示用户 ID */
const uploaderLabel = (version: PluginVersionView) =>
  version.created_by === 0 ? "内置" : `用户 #${version.created_by}`;

/**
 * 插件页右栏：头部（启停）、版本表（新到旧，含删除与禁用原因）、能力清单。
 * @param canWrite 是否有运维权限；没有则不渲染启停开关与删除按钮，页头写只读
 * @param onChanged 启停或删除成功后刷新清单
 */
export function PluginDetail({
  plugin,
  canWrite,
  onChanged,
}: {
  plugin: PluginView;
  canWrite: boolean;
  onChanged: () => Promise<void>;
}) {
  const aliveRef = useAliveRef();
  const latest = latestVersion(plugin);
  const [viewId, setViewId] = useState<number | null>(null);
  const [toggling, setToggling] = useState(false);
  const [deleting, setDeleting] = useState<PluginVersionView | null>(null);
  const [deleteBusy, setDeleteBusy] = useState(false);
  const [deleteError, setDeleteError] = useState<string | null>(null);

  const viewed = plugin.versions.find((version) => version.id === viewId) ?? latest;

  const toggle = async (enabled: boolean) => {
    setToggling(true);
    try {
      await setPluginEnabled(plugin.key, enabled);
      await onChanged();
    } finally {
      if (aliveRef.current) setToggling(false);
    }
  };

  const confirmDelete = async () => {
    if (!deleting) return;
    setDeleteBusy(true);
    setDeleteError(null);
    try {
      await deletePluginVersion(plugin.key, deleting.version);
      if (!aliveRef.current) return;
      setDeleting(null);
      await onChanged();
    } catch (error) {
      // 409：有渠道 / 非终态任务引用或内置。全局 toast 已弹，这里把原因留在对话框里
      if (aliveRef.current) setDeleteError(errorMessage(error, "删除失败"));
    } finally {
      if (aliveRef.current) setDeleteBusy(false);
    }
  };

  return (
    <div className="flex flex-col gap-5">
      {!canWrite && <ReadOnlyNotice what="上传、启停插件与删除版本" />}

      <header className="flex flex-wrap items-center gap-2">
        <h2 className="text-lg font-semibold">{plugin.name}</h2>
        <span className="text-muted-foreground font-mono text-sm">{plugin.key}</span>
        <Tag>{plugin.source === "builtin" ? "内置" : "已上传"}</Tag>
        <div className="ml-auto flex items-center gap-2 text-sm">
          {canWrite ? (
            <label className="flex items-center gap-2">
              <span className="text-muted-foreground text-xs">启用</span>
              <Switch
                checked={plugin.enabled}
                disabled={toggling}
                aria-label={`启用插件 ${plugin.name}`}
                onCheckedChange={(checked) => void toggle(checked)}
              />
            </label>
          ) : (
            <Tag tone={plugin.enabled ? "success" : "warning"}>
              {plugin.enabled ? "已启用" : "已停用"}
            </Tag>
          )}
        </div>
      </header>

      {!plugin.enabled && (
        <Notice tone="warning">
          停用后，所有使用它的渠道<b>不再接新任务</b>，进行中的任务不受影响。
        </Notice>
      )}

      <section className="flex flex-col gap-2">
        <h3 className="text-muted-foreground text-xs font-medium">版本（新到旧，版本不可变）</h3>
        <div className="rounded-lg border">
          <Table>
            <TableHeader>
              <TableRow>
                <TableHead>版本</TableHead>
                <TableHead>sha256</TableHead>
                <TableHead>上传时间</TableHead>
                <TableHead>上传人</TableHead>
                <TableHead>固定在此版本的渠道</TableHead>
                {canWrite && <TableHead className="text-right">操作</TableHead>}
              </TableRow>
            </TableHeader>
            <TableBody>
              {plugin.versions.map((version) => {
                const block = versionDeleteBlock(plugin, version);
                const selected = version.id === viewed?.id;
                return (
                  <TableRow key={version.id} data-state={selected ? "selected" : undefined}>
                    <TableCell>
                      <button
                        type="button"
                        className={cn("font-medium hover:underline", selected && "underline")}
                        aria-pressed={selected}
                        title="查看这个版本的能力清单"
                        onClick={() => setViewId(version.id)}
                      >
                        {version.version}
                      </button>
                      {version.id === latest?.id && (
                        <Tag tone="info" className="ml-1.5">
                          最新
                        </Tag>
                      )}
                    </TableCell>
                    <TableCell>
                      <span className="inline-flex items-center gap-1">
                        <span className="font-mono text-xs" title={version.sha256}>
                          {shortSha(version.sha256)}…
                        </span>
                        <CopyButton iconOnly text={version.sha256} label="复制完整 sha256" />
                      </span>
                    </TableCell>
                    <TableCell className="text-muted-foreground">
                      {formatTime(version.created_at)}
                    </TableCell>
                    <TableCell className="text-muted-foreground">
                      {uploaderLabel(version)}
                    </TableCell>
                    <TableCell>{version.channel_count}</TableCell>
                    {canWrite && (
                      <TableCell className="text-right">
                        <div className="flex flex-col items-end gap-0.5">
                          <Button
                            size="xs"
                            variant="outline"
                            disabled={!!block}
                            aria-label={`删除 ${version.version}`}
                            onClick={() => {
                              setDeleteError(null);
                              setDeleting(version);
                            }}
                          >
                            <Trash2 />
                            删除
                          </Button>
                          {block && (
                            <span className="text-muted-foreground max-w-48 text-[11px]">
                              {block}
                            </span>
                          )}
                        </div>
                      </TableCell>
                    )}
                  </TableRow>
                );
              })}
            </TableBody>
          </Table>
        </div>
      </section>

      {viewed && (
        <section className="flex flex-col gap-3 rounded-lg border p-4">
          <h3 className="text-sm font-medium">能力清单 · v{viewed.version}</h3>
          <MetaView meta={viewed.meta} />
        </section>
      )}

      <ConfirmDialog
        open={!!deleting}
        title="删除插件版本？"
        destructive
        confirmLabel="删除"
        busy={deleteBusy}
        error={deleteError}
        description={
          deleting && (
            <>
              将永久删除{" "}
              <b>
                {plugin.name} v{deleting.version}
              </b>
              ，不能撤销。若仍有非终态任务在使用这个版本，后端会拒绝删除。
            </>
          )
        }
        onConfirm={() => void confirmDelete()}
        onCancel={() => setDeleting(null)}
      />
      {toggling && (
        <p className="text-muted-foreground flex items-center gap-1 text-xs" role="status">
          <Loader2 className="size-3 animate-spin" />
          正在更新…
        </p>
      )}
    </div>
  );
}
