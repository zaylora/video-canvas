import { useCallback, useEffect, useState } from "react";
import { Plus, RadioTower } from "lucide-react";
import { useLocation, useSearchParams } from "react-router";
import { toast } from "sonner";

import { setChannelOrder, updateChannel } from "@/api/admin/ai";
import type { ChannelOption } from "@/api/admin/ai/type.d";
import { confirm } from "@/components/admin-ui/confirm-dialog";
import { DataPage } from "@/components/admin-ui/data-panel";
import {
  EmptyState,
  EmptyStateActions,
  EmptyStateDescription,
  EmptyStateIcon,
  EmptyStateTitle,
} from "@/components/admin-ui/empty-state";
import { Notice } from "@/components/admin-ui/notice";
import { ReasonTooltip } from "@/components/admin-ui/reason-tooltip";
import { Button } from "@/components/ui/button";
import { Skeleton } from "@/components/ui/skeleton";
import { useAdminStore } from "@/store/admin";
import {
  parseChannelsQuery,
  pickSelectedChannel,
  type ChannelStatusFilter,
} from "@/utils/admin/channel-route";
import { canManageInfra } from "@/utils/admin/role";

import { openDeleteDialog } from "../delete-dialog";
import { ChannelModelsPanel } from "../models/channel-models-panel";
import { useChannelLoads } from "../overview/use-overview-stats";
import { useAdminCatalogOptions } from "../../use-admin";
import { ChannelHeader } from "./channel-header";
import { SUPER_ONLY, ChannelList } from "./channel-list";
import { openChannelDialog } from "./channel-dialog";
import { openPullModelsDialog } from "./pull-models-dialog";
import { openSecretDialog } from "./secret-dialog";
import { useModelCounts } from "./use-model-counts";

/** 深链带来的“要打开的模型”：resolving 表示链接里只有模型 key，还在反查它属于哪个渠道 */
type DeepLink = { model: string; test: boolean; resolving: boolean };

/**
 * 渠道与模型主从页（/admin/ai/channels）：左栏渠道列表，右栏当前渠道的详情与模型管理。
 * - 选中渠道放在页面 state；深链 `?channel=&model=&test=1&status=` 进来后选中渠道、
 *   把 model / test 交给模型面板打开，读完用 replace 把 model / test / status 从地址里清掉，channel 留着，刷新还在原处；
 *   只给了 model 没给 channel 时，用模型清单反查所属渠道；
 * - 写渠道（新增、编辑、设置 Key、启停、删除、排序）只对 super_admin 开放，admin 看到置灰按钮并写明原因；
 *   拉取与导入模型 admin 也可以；
 * - 弹窗（新增 / 编辑、设置 Key、拉取模型、删除确认）都走全局弹窗 store；
 * - 渠道或模型有改动：重新拉渠道清单与模型数；拉取导入后再递增 reloadToken 让模型表刷新。
 */
export default function ChannelsPage() {
  const { catalog } = useAdminCatalogOptions();
  const { channels, channelsStatus, plugins, pluginsStatus, reloadChannels } = catalog;
  const role = useAdminStore((state) => state.role);
  const canWrite = canManageInfra(role);
  const location = useLocation();
  const [, setSearchParams] = useSearchParams();
  const modelCounts = useModelCounts();
  const { reload: reloadModelCounts } = modelCounts;
  const loads = useChannelLoads();

  /** 想选中的渠道（可能还没加载出来、也可能已被删）；实际选中项用 pickSelectedChannel 推出 */
  const [wanted, setWanted] = useState<string | null>(null);
  const [query, setQuery] = useState("");
  const [statusFilter, setStatusFilter] = useState<ChannelStatusFilter>("all");
  const [deepLink, setDeepLink] = useState<DeepLink | null>(null);
  /** 深链里指定的渠道：渠道清单到位后核对它存不存在，不存在提示一次 */
  const [linkedChannel, setLinkedChannel] = useState<string | null>(null);
  /** 模型表的刷新信号：拉取导入后加一 */
  const [reloadToken, setReloadToken] = useState(0);
  const [newModelToken, setNewModelToken] = useState(0);
  const [toggling, setToggling] = useState(false);

  /** 同一次导航只读一次深链 */
  const [handledKey, setHandledKey] = useState<string | null>(null);
  const link = parseChannelsQuery(location.search);
  if (handledKey !== location.key) {
    setHandledKey(location.key);
    if (link.channel && link.channel !== wanted) {
      setWanted(link.channel);
      setLinkedChannel(link.channel);
    }
    if (new URLSearchParams(location.search).has("status")) setStatusFilter(link.status);
    if (link.model) setDeepLink({ model: link.model, test: link.test, resolving: !link.channel });
  }

  // 读完就清：model / test / status 只用一次，channel 留在地址里
  const hasTransient = link.model !== null || link.test || /[?&]status=/.test(location.search);
  useEffect(() => {
    if (!hasTransient) return;
    setSearchParams(
      (prev) => {
        const next = new URLSearchParams(prev);
        next.delete("model");
        next.delete("test");
        next.delete("status");
        return next;
      },
      { replace: true },
    );
  }, [hasTransient, location.key, setSearchParams]);

  // 深链里的渠道已经不存在（被删了）：提示一次
  useEffect(() => {
    if (!linkedChannel || channelsStatus !== "ready") return;
    if (!channels.some((channel) => channel.key === linkedChannel)) {
      toast.error(`渠道 ${linkedChannel} 不存在`);
    }
    setLinkedChannel(null);
  }, [linkedChannel, channels, channelsStatus]);

  // 深链里只有模型 key：用模型清单反查它的渠道
  useEffect(() => {
    if (!deepLink?.resolving || modelCounts.status === "loading") return;
    const hit = modelCounts.models.find((model) => model.key === deepLink.model);
    if (hit?.channel) {
      setWanted(hit.channel);
      setDeepLink({ ...deepLink, resolving: false });
    } else {
      toast.error(`模型 ${deepLink.model} 不存在`);
      setDeepLink(null);
    }
  }, [deepLink, modelCounts.models, modelCounts.status]);

  const selectedKey = pickSelectedChannel(channels, wanted);
  const channel = channels.find((item) => item.key === selectedKey) ?? null;

  /** 选中渠道：换渠道后深链的模型不再适用，地址里的 channel 同步更新，刷新还在原处 */
  const selectChannel = useCallback(
    (key: string) => {
      setWanted(key);
      setDeepLink(null);
      setSearchParams({ channel: key }, { replace: true });
    },
    [setSearchParams],
  );

  const refreshAll = useCallback(() => {
    void reloadChannels();
    void reloadModelCounts();
  }, [reloadChannels, reloadModelCounts]);

  const newChannel = () =>
    openChannelDialog({
      onSaved: (key, created) => {
        if (created) selectChannel(key);
        void reloadChannels();
      },
    });

  const editChannel = (target: ChannelOption) =>
    openChannelDialog({
      channel: target,
      onSaved: () => void reloadChannels(),
    });

  const setKey = (target: ChannelOption) =>
    openSecretDialog({
      channelKey: target.key,
      channelName: target.name,
      secretSet: target.secret_set,
      secretKeySet: target.secret_key_set,
      onSaved: () => void reloadChannels(),
    });

  const setEnabled = async (target: ChannelOption, enabled: boolean) => {
    await updateChannel(target.key, { enabled });
    toast.success(
      enabled
        ? `渠道「${target.name}」已启用`
        : `渠道「${target.name}」已停用，画布里它的模型已不可见`,
    );
    await reloadChannels();
  };

  const toggleChannel = (target: ChannelOption) => {
    if (!target.enabled) {
      setToggling(true);
      void setEnabled(target, true)
        .catch(() => undefined)
        .finally(() => setToggling(false));
      return;
    }
    void confirm({
      title: "停用渠道？",
      destructive: true,
      confirmLabel: "停用",
      description: `停用「${target.name}」后，它的所有模型在画布里不可见；进行中的任务按快照继续。`,
      onConfirm: () => setEnabled(target, false),
    });
  };

  const deleteChannel = (target: ChannelOption) =>
    openDeleteDialog({
      target: "channel",
      objectKey: target.key,
      name: target.name,
      channels,
      onDeleted: () => {
        setWanted(null);
        setDeepLink(null);
        setSearchParams({}, { replace: true });
        refreshAll();
      },
      onChanged: () => {
        void reloadModelCounts();
        setReloadToken((n) => n + 1);
      },
    });

  const pullModels = (target: ChannelOption) =>
    openPullModelsDialog({
      channel: target,
      canWriteChannel: canWrite,
      onImported: () => {
        setReloadToken((n) => n + 1);
        refreshAll();
      },
      onManualAdd: () => setNewModelToken((n) => n + 1),
    });

  /** 拖动排序松手：提交完整的新顺序；失败时也重拉清单，让列表回到服务端的顺序 */
  const reorderChannels = (keys: string[]) => {
    void setChannelOrder(keys)
      .catch(() => undefined)
      .finally(() => void reloadChannels());
  };

  const modelDeepLink = deepLink && !deepLink.resolving ? deepLink : null;

  return (
    <DataPage>
      <h1 className="sr-only">渠道与模型</h1>
      <div className="grid gap-4 lg:min-h-0 lg:flex-1 lg:grid-cols-[250px_minmax(0,1fr)] lg:grid-rows-[minmax(0,1fr)]">
        <ChannelList
          channels={channels}
          status={channelsStatus}
          selectedKey={selectedKey}
          modelCounts={modelCounts.counts}
          query={query}
          statusFilter={statusFilter}
          canWrite={canWrite}
          onSelect={selectChannel}
          onNew={newChannel}
          onQueryChange={setQuery}
          onStatusChange={setStatusFilter}
          onReorder={reorderChannels}
          onRetry={() => void reloadChannels()}
        />

        <section data-slot="channel-detail" className="flex min-h-0 min-w-0 flex-col gap-5">
          {channelsStatus === "loading" ? (
            <>
              <Skeleton className="h-24" />
              <Skeleton className="h-64" />
            </>
          ) : channelsStatus === "error" && channels.length === 0 ? (
            <Notice
              tone="danger"
              action={
                <Button size="xs" variant="outline" onClick={() => void reloadChannels()}>
                  重试
                </Button>
              }
            >
              渠道清单加载失败
            </Notice>
          ) : !channel ? (
            <EmptyState className="py-16">
              <EmptyStateIcon>
                <RadioTower />
              </EmptyStateIcon>
              <EmptyStateTitle>还没有渠道</EmptyStateTitle>
              <EmptyStateDescription>先新增渠道，再拉取模型。</EmptyStateDescription>
              <EmptyStateActions>
                <ReasonTooltip reason={canWrite ? null : SUPER_ONLY}>
                  <Button disabled={!canWrite} onClick={newChannel}>
                    <Plus />
                    新增渠道
                  </Button>
                </ReasonTooltip>
              </EmptyStateActions>
            </EmptyState>
          ) : (
            <>
              <ChannelHeader
                channel={channel}
                load={loads.loads.find((item) => item.channel === channel.key)}
                loadStale={loads.stale}
                canWrite={canWrite}
                toggling={toggling}
                onEdit={() => editChannel(channel)}
                onSetKey={() => setKey(channel)}
                onToggle={() => toggleChannel(channel)}
                onDelete={() => deleteChannel(channel)}
              />
              <ChannelModelsPanel
                key={channel.key}
                channel={channel}
                channels={channels}
                plugins={plugins}
                pluginsStatus={pluginsStatus}
                newModelToken={newModelToken}
                onOpenHandled={() => setDeepLink(null)}
                canWriteChannel={canWrite}
                reloadToken={reloadToken}
                onChanged={refreshAll}
                onPull={() => pullModels(channel)}
                openModelKey={modelDeepLink?.model ?? null}
                openTest={modelDeepLink?.test ?? false}
              />
            </>
          )}
        </section>
      </div>
    </DataPage>
  );
}
