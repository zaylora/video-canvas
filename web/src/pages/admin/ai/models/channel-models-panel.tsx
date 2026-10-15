import { useCallback, useEffect, useMemo, useRef, useState } from "react";
import { Box, Check, ListOrdered, Loader2, Plus, RefreshCw } from "lucide-react";
import { useSearchParams } from "react-router";
import { toast } from "sonner";

import { listModels, setChannelModelOrder } from "@/api/admin/ai";
import type { ChannelOption, ModelListItem, PluginView } from "@/api/admin/ai/type.d";
import {
  EmptyState,
  EmptyStateActions,
  EmptyStateDescription,
  EmptyStateIcon,
  EmptyStateTitle,
} from "@/components/admin-ui/empty-state";
import { ReasonTooltip } from "@/components/admin-ui/reason-tooltip";
import { Tag } from "@/components/admin-ui/tag";
import { Button } from "@/components/ui/button";
import { clearListParams } from "@/utils/admin/list-params";
import { modelListQuery } from "@/utils/admin/model-list";

import { useAliveRef, type LoadStatus } from "../../use-admin";
import { useAdminList } from "../../use-admin-list";
import { BatchBar } from "./batch-bar";
import { ModelEditorDialogs } from "./model-editor-dialogs";
import { useModelPanelRoute } from "./model-route";
import { ModelTable } from "./model-table";
import { useModelBatch } from "./use-model-batch";
import { useModelRowActions } from "./use-model-row-actions";
import { useModelWorkspace, type ModelCatalog } from "./use-model-workspace";

/** 列表认的筛选参数：能力（含“待配置”）、状态；渠道由面板固定，不进筛选 */
const MODEL_FILTERS = ["kind", "status"] as const;

/** 排序模式一次拉取的页大小（后端上限 100） */
const SORT_PAGE_SIZE = 100;

/** 模型管理面板的属性（契约：渠道主从页右栏直接使用） */
export type ChannelModelsPanelProps = {
  /** 当前渠道 */
  channel: ChannelOption;
  /** 全部渠道（模型弹窗“所属渠道”下拉用） */
  channels: ChannelOption[];
  /** 全部插件（协议列表与协议设置项用） */
  plugins: PluginView[];
  /** 插件清单的加载状态，缺省按已加载；没加载好时模型弹窗不让切能力 */
  pluginsStatus?: LoadStatus;
  /** 是否 super_admin：面板里没有渠道写操作，只用来决定空状态里“缺 Key”的提示该找谁 */
  canWriteChannel: boolean;
  /** 外部递增（例如拉取并导入完成）后面板重新拉取列表 */
  reloadToken: number;
  /** 模型增删改 / 启停 / 排序后调用，宿主用它刷新左栏的模型数与健康状态 */
  onChanged: () => void;
  /** 点“拉取模型”时调用；拉取弹窗由宿主负责 */
  onPull: () => void;
  /** 深链：要打开编辑弹窗的模型 key（见 onOpenHandled） */
  openModelKey?: string | null;
  /** 深链：直接打开测试弹窗而不是编辑弹窗 */
  openTest?: boolean;
  /** 外部递增（例如拉取弹窗里点“手动新增模型”）后面板打开“新增模型”；首次渲染不算 */
  newModelToken?: number;
  /** 深链被面板消费（已打开弹窗）后调用，宿主据此清掉自己保存的深链，免得切走再切回又弹出来 */
  onOpenHandled?: () => void;
};

/**
 * 渠道下的模型管理（渠道与模型主从页的右栏）：标题与数量、自定义排序 / 拉取模型 / 新增模型、
 * 搜索与能力 / 状态筛选、模型表格、批量栏、模型编辑弹窗与测试弹窗。
 * 列表用后端分页，范围固定在当前渠道；选中的模型与弹窗状态放在面板自己的 state 里，不进 URL。
 * 切换渠道时整块重建，搜索、筛选、页码一并回到初始。
 */
export function ChannelModelsPanel(props: ChannelModelsPanelProps) {
  const channelKey = props.channel.key;
  const [, setParams] = useSearchParams();
  const previous = useRef(channelKey);
  // 切到另一个渠道：清掉 URL 里上一个渠道的搜索、筛选与页码（首次进入保留，概览页会带 ?status= 来）
  useEffect(() => {
    if (previous.current === channelKey) return;
    previous.current = channelKey;
    setParams((prev) => clearListParams(prev, MODEL_FILTERS), { replace: true });
  }, [channelKey, setParams]);
  return <PanelBody key={channelKey} {...props} />;
}

function PanelBody({
  channel,
  channels,
  plugins,
  pluginsStatus = "ready",
  canWriteChannel,
  reloadToken,
  onChanged,
  onPull,
  openModelKey,
  openTest,
  onOpenHandled,
  newModelToken = 0,
}: ChannelModelsPanelProps) {
  const aliveRef = useAliveRef();
  const catalog = useMemo<ModelCatalog>(
    () => ({ channels, plugins, pluginsStatus }),
    [channels, plugins, pluginsStatus],
  );
  const list = useAdminList<ModelListItem>(MODEL_FILTERS, (query) =>
    listModels(modelListQuery(query, channel.key)),
  );
  const { route, test } = useModelPanelRoute(openModelKey, openTest, onOpenHandled, channel.key);
  const onChangedRef = useRef(onChanged);
  useEffect(() => {
    onChangedRef.current = onChanged;
  });
  // 模型增删改 / 启停 / 排序之后：刷新当前页，并通知宿主刷新左栏
  const { reload } = list;
  const reloadAll = useCallback(() => {
    reload();
    onChangedRef.current();
  }, [reload]);
  const ws = useModelWorkspace(catalog, route, reloadAll);
  const testOnly = test && ws.selection === "model";

  // 外部递增 reloadToken（拉取并导入完成等）：重新拉取；首次渲染不算
  const lastToken = useRef(reloadToken);
  useEffect(() => {
    if (lastToken.current === reloadToken) return;
    lastToken.current = reloadToken;
    reload();
  }, [reloadToken, reload]);

  // 外部递增 newModelToken：打开“新增模型”；首次渲染不算
  const lastNewToken = useRef(newModelToken);
  const { startNew } = ws;
  useEffect(() => {
    if (lastNewToken.current === newModelToken) return;
    lastNewToken.current = newModelToken;
    startNew();
  }, [newModelToken, startNew]);

  // ---- 勾选与批量：只作用于当前渠道的当前页，换页、换搜索 / 筛选后清空
  const [selected, setSelected] = useState<string[]>([]);
  const { q, page, filters } = list;
  const filterKey = JSON.stringify(filters);
  useEffect(() => setSelected([]), [q, page, filterKey, list.pageSize]);
  const checked = selected.filter((key) => list.items.some((item) => item.key === key));
  const batch = useModelBatch(list.items, reloadAll);
  const row = useModelRowActions(reloadAll, list.patchItems);

  // ---- 模型总数：筛选时 list.total 是命中数，标题要显示渠道下的全部
  const [allCount, setAllCount] = useState<number | null>(null);
  useEffect(() => {
    if (list.state === "ready" && !list.filtering) setAllCount(list.total);
  }, [list.state, list.filtering, list.total]);
  const count = allCount ?? list.total;

  // ---- 自定义排序：一次拉取渠道下全部模型，上移 / 下移后一次性保存
  const [sortMode, setSortMode] = useState(false);
  const [sortModels, setSortModels] = useState<ModelListItem[] | null>(null);
  const [sortOriginal, setSortOriginal] = useState<string[]>([]);
  const [sortSaving, setSortSaving] = useState(false);
  const sortToken = useRef(0);

  const enterSort = async () => {
    const token = ++sortToken.current;
    setSelected([]);
    setSortMode(true);
    setSortModels(null);
    const all: ModelListItem[] = [];
    try {
      for (let pageNo = 1; ; pageNo += 1) {
        const result = await listModels({
          channel: channel.key,
          page: pageNo,
          page_size: SORT_PAGE_SIZE,
        });
        all.push(...result.list);
        if (result.list.length < SORT_PAGE_SIZE || all.length >= result.total) break;
      }
    } catch {
      // 全局 toast 已弹；拉不全就不能排序（排序要提交渠道下全部模型）
      if (aliveRef.current && sortToken.current === token) setSortMode(false);
      return;
    }
    if (!aliveRef.current || sortToken.current !== token) return;
    setSortModels(all);
    setSortOriginal(all.map((item) => item.key));
  };

  const leaveSort = () => {
    sortToken.current += 1;
    setSortMode(false);
    setSortModels(null);
  };

  const moveModel = (key: string, direction: -1 | 1) =>
    setSortModels((prev) => {
      if (!prev) return prev;
      const from = prev.findIndex((item) => item.key === key);
      const to = from + direction;
      if (from < 0 || to < 0 || to >= prev.length) return prev;
      const next = [...prev];
      [next[from], next[to]] = [next[to], next[from]];
      return next;
    });

  const finishSort = async () => {
    if (!sortModels) return;
    const keys = sortModels.map((item) => item.key);
    if (keys.join("\n") === sortOriginal.join("\n")) {
      leaveSort();
      return;
    }
    setSortSaving(true);
    try {
      await setChannelModelOrder(channel.key, keys);
      if (!aliveRef.current) return;
      toast.success("渠道内顺序已保存", {
        description: "画布里用户看到的模型顺序按“渠道顺序 + 渠道内顺序”排列。",
      });
      leaveSort();
      reloadAll();
    } finally {
      if (aliveRef.current) setSortSaving(false);
    }
  };

  const empty = !sortMode && list.state === "ready" && !list.filtering && list.total === 0;
  const sortDeny = count < 2 ? "渠道下至少有 2 个模型才需要排序" : null;

  return (
    <section className="flex min-h-0 min-w-0 flex-1 flex-col gap-3" aria-label="模型管理">
      <div className="flex flex-wrap items-center gap-2">
        <h2 className="text-base font-semibold">模型管理</h2>
        <Tag className="tabular-nums">{count}</Tag>
        <div className="ml-auto flex flex-wrap items-center gap-2">
          {sortMode ? (
            <>
              <Button variant="outline" disabled={sortSaving} onClick={leaveSort}>
                取消
              </Button>
              <Button disabled={!sortModels || sortSaving} onClick={() => void finishSort()}>
                {sortSaving ? <Loader2 className="animate-spin" /> : <Check />}
                完成排序
              </Button>
            </>
          ) : (
            <>
              <ReasonTooltip reason={sortDeny}>
                <Button variant="outline" disabled={!!sortDeny} onClick={() => void enterSort()}>
                  <ListOrdered />
                  自定义排序
                </Button>
              </ReasonTooltip>
              <Button variant="outline" onClick={onPull}>
                <RefreshCw />
                拉取模型
              </Button>
              <Button onClick={ws.startNew}>
                <Plus />
                新增模型
              </Button>
            </>
          )}
        </div>
      </div>

      {empty ? (
        <EmptyState className="bg-card min-h-64">
          <EmptyStateIcon>
            <Box />
          </EmptyStateIcon>
          <EmptyStateTitle>这个渠道下还没有模型</EmptyStateTitle>
          <EmptyStateDescription>
            从上游拉取模型清单，或手动新增。拉到的模型是“待配置”，要逐个选好能力和协议才能启用。
            {!channel.secret_set &&
              (canWriteChannel
                ? "渠道还没有设置 API Key，拉取前请先设置。"
                : "渠道还没有设置 API Key，拉取前请联系 super_admin 设置。")}
          </EmptyStateDescription>
          <EmptyStateActions>
            <Button onClick={onPull}>
              <RefreshCw />
              拉取模型
            </Button>
            <Button variant="outline" onClick={ws.startNew}>
              <Plus />
              新增模型
            </Button>
          </EmptyStateActions>
        </EmptyState>
      ) : (
        <ModelTable
          list={list}
          sortMode={sortMode}
          sortModels={sortModels}
          onMove={moveModel}
          selected={checked}
          onSelectedChange={setSelected}
          busyKey={row.busyKey}
          onEdit={(key) => ws.selectModel(key)}
          onTest={(key) => ws.selectModel(key, { test: true })}
          onToggleEnabled={(key, enabled) => void row.toggleEnabled(key, enabled)}
          onDelete={row.requestDelete}
        />
      )}

      <BatchBar
        selected={checked}
        catalog={catalog}
        batch={batch}
        onClear={() => setSelected([])}
      />

      <ModelEditorDialogs
        ws={ws}
        catalog={catalog}
        testOnly={testOnly}
        seed={list.items.find((item) => item.key === ws.modelKey)}
      />
    </section>
  );
}
