import { useMemo, useState } from "react";

import type { PluginView } from "@/api/admin-ai/type.d";
import {
  ListPanel,
  ListPanelContent,
  ListPanelCount,
  ListPanelEmpty,
  ListPanelHeader,
  ListPanelItem,
  ListPanelItemRow,
  ListPanelTitle,
} from "@/components/admin-ui/list-panel";
import { SearchInput } from "@/components/admin-ui/search-input";
import { StatusLabel } from "@/components/admin-ui/status-dot";
import { Tag } from "@/components/admin-ui/tag";
import { Skeleton } from "@/components/ui/skeleton";
import { latestVersion, pluginChannelCount } from "@/utils/admin/plugin";

import { KindIcons } from "../kind";
import type { LoadStatus } from "../../use-admin";

/**
 * 插件页左栏：搜索 + 列表。每行：名称与 key、来源、最新版本号、“N 个渠道在用”、停用标记。
 */
export function PluginList({
  plugins,
  status,
  selectedKey,
  onSelect,
  onRetry,
}: {
  plugins: PluginView[];
  status: LoadStatus;
  selectedKey: string | null;
  onSelect: (key: string) => void;
  onRetry: () => void;
}) {
  const [query, setQuery] = useState("");
  const shown = useMemo(() => {
    const text = query.trim().toLowerCase();
    if (!text) return plugins;
    return plugins.filter(
      (plugin) =>
        plugin.key.toLowerCase().includes(text) || plugin.name.toLowerCase().includes(text),
    );
  }, [plugins, query]);

  return (
    <ListPanel className="xl:sticky xl:top-0">
      <ListPanelHeader>
        <ListPanelTitle>
          全部插件
          <ListPanelCount>{plugins.length}</ListPanelCount>
        </ListPanelTitle>
        <SearchInput
          value={query}
          onChange={(event) => setQuery(event.target.value)}
          placeholder="搜索名称或 key"
          aria-label="搜索插件"
        />
      </ListPanelHeader>
      <ListPanelContent className="max-h-72 xl:max-h-[calc(100svh-16rem)]">
        {status === "loading" &&
          Array.from({ length: 3 }, (_, index) => (
            <li key={index} className="p-3">
              <Skeleton className="h-4 w-1/2" />
              <Skeleton className="mt-2 h-3 w-3/4" />
            </li>
          ))}
        {status === "error" && (
          <ListPanelEmpty className="text-destructive">
            加载失败，
            <button type="button" className="underline" onClick={onRetry}>
              重试
            </button>
          </ListPanelEmpty>
        )}
        {status === "ready" && plugins.length === 0 && (
          <ListPanelEmpty className="text-xs">
            还没有插件。内置插件由后端启动时登记；如果这里一直是空的，说明后端还没有登记内置插件。
          </ListPanelEmpty>
        )}
        {status === "ready" && plugins.length > 0 && shown.length === 0 && (
          <ListPanelEmpty>没有匹配的插件</ListPanelEmpty>
        )}
        {shown.map((plugin) => {
          const latest = latestVersion(plugin);
          return (
            <ListPanelItem
              key={plugin.key}
              active={plugin.key === selectedKey}
              onClick={() => onSelect(plugin.key)}
            >
              <ListPanelItemRow>
                <span className="truncate text-sm font-medium">{plugin.name}</span>
                <Tag tone={plugin.source === "builtin" ? "neutral" : "violet"}>
                  {plugin.source === "builtin" ? "内置" : "已上传"}
                </Tag>
                <StatusLabel tone={plugin.enabled ? "success" : "neutral"} className="ml-auto">
                  {plugin.enabled ? "启用" : "停用"}
                </StatusLabel>
              </ListPanelItemRow>
              <ListPanelItemRow className="text-muted-foreground mt-1.5 text-xs">
                <KindIcons kinds={Object.keys(latest?.meta?.endpoints ?? {})} />
                <span className="ml-auto shrink-0">
                  {latest && `v${latest.version} · `}
                  {pluginChannelCount(plugin)} 个渠道
                </span>
              </ListPanelItemRow>
            </ListPanelItem>
          );
        })}
      </ListPanelContent>
    </ListPanel>
  );
}
