import { useMemo, useState } from "react";
import { Search, Upload } from "lucide-react";

import type { PluginView } from "@/api/admin-ai/type";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { Skeleton } from "@/components/ui/skeleton";
import { cn } from "@/lib/utils";
import { latestVersion, pluginChannelCount } from "@/utils/admin/plugin";

import type { LoadStatus } from "../use-admin";
import { Tag } from "../shared";

/**
 * 插件页左栏：搜索 + 列表。每行：名称与 key、来源、最新版本号、“N 个渠道在用”、停用标记。
 * @param onUpload 点“上传插件”；只有运维会传，不传就不渲染按钮
 */
export function PluginList({
  plugins,
  status,
  selectedKey,
  onSelect,
  onUpload,
  onRetry,
}: {
  plugins: PluginView[];
  status: LoadStatus;
  selectedKey: string | null;
  onSelect: (key: string) => void;
  onUpload?: () => void;
  onRetry: () => void;
}) {
  const [query, setQuery] = useState("");
  const shown = useMemo(() => {
    const text = query.trim().toLowerCase();
    if (!text) return plugins;
    return plugins.filter(
      (plugin) => plugin.key.toLowerCase().includes(text) || plugin.name.toLowerCase().includes(text),
    );
  }, [plugins, query]);

  return (
    <div className="flex h-full min-h-0 flex-col">
      <div className="flex items-center gap-2 border-b p-3">
        <span className="text-sm font-medium">插件</span>
        <div className="ml-auto">
          {onUpload && (
            <Button size="sm" onClick={onUpload}>
              <Upload />
              上传插件
            </Button>
          )}
        </div>
      </div>
      <div className="relative border-b p-2">
        <Search className="text-muted-foreground pointer-events-none absolute top-1/2 left-4.5 size-3.5 -translate-y-1/2" />
        <Input
          value={query}
          onChange={(event) => setQuery(event.target.value)}
          placeholder="搜索名称或 key"
          aria-label="搜索插件"
          className="pl-7"
        />
      </div>
      <ul className="min-h-0 flex-1 overflow-y-auto">
        {status === "loading" &&
          Array.from({ length: 3 }, (_, index) => (
            <li key={index} className="border-b p-3">
              <Skeleton className="h-4 w-1/2" />
              <Skeleton className="mt-2 h-3 w-3/4" />
            </li>
          ))}
        {status === "error" && (
          <li className="text-destructive p-3 text-xs">
            加载失败，
            <button type="button" className="underline" onClick={onRetry}>
              重试
            </button>
          </li>
        )}
        {status === "ready" && plugins.length === 0 && (
          <li className="text-muted-foreground p-4 text-xs">
            还没有插件。内置插件由后端启动时登记；如果这里一直是空的，说明后端还没有登记内置插件。
          </li>
        )}
        {status === "ready" && plugins.length > 0 && shown.length === 0 && (
          <li className="text-muted-foreground p-4 text-xs">没有匹配的插件</li>
        )}
        {shown.map((plugin) => {
          const latest = latestVersion(plugin);
          const active = plugin.key === selectedKey;
          return (
            <li key={plugin.key}>
              <button
                type="button"
                aria-current={active}
                className={cn(
                  "hover:bg-muted flex w-full flex-col gap-1 border-b px-3 py-2.5 text-left",
                  active && "bg-muted shadow-[inset_3px_0_0_var(--primary)]",
                )}
                onClick={() => onSelect(plugin.key)}
              >
                <span className="flex items-center gap-2">
                  <span className="text-sm font-medium">{plugin.name}</span>
                  {!plugin.enabled && <Tag tone="warning">已停用</Tag>}
                </span>
                <span className="text-muted-foreground flex flex-wrap items-center gap-1.5 text-xs">
                  <span className="font-mono">{plugin.key}</span>
                  <Tag>{plugin.source === "builtin" ? "内置" : "已上传"}</Tag>
                  {latest && <span>v{latest.version}</span>}
                  <span className="ml-auto">{pluginChannelCount(plugin)} 个渠道在用</span>
                </span>
              </button>
            </li>
          );
        })}
      </ul>
    </div>
  );
}
