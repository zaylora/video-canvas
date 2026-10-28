import { useCallback, useEffect, useMemo, useRef, useState } from "react";
import { ReactFlowProvider } from "@xyflow/react";
import {
  ChevronDown,
  Filter,
  Frame,
  LayoutGrid,
  ListChecks,
  Loader2,
  ScanSearch,
} from "lucide-react";

import { getCanvas } from "@/api/canvas";
import { listSegments } from "@/api/drama";
import { confirm } from "@/components/admin-ui/confirm-dialog";
import { Popover, PopoverContent, PopoverTrigger } from "@/components/ui/popover";
import { CanvasEditor } from "@/components/canvas-editor/canvas-editor";
import type { CanvasEditorCommands } from "@/components/canvas-editor/editor-api";
import { Button } from "@/components/ui/button";
import { useCanvasLock } from "@/hooks/use-canvas-lock";
import { cn } from "@/lib/utils";
import { buildAddNodeItems } from "@/pages/canvas/chrome/add-node-items";
import type { CanvasNode, CanvasNodeData, FlowNode, NodeDrama, NodeKind } from "@/types";
import { draftStore } from "@/utils/canvas/draft-idb";
import { loadCanvasForEditing, type OpenedCanvas } from "@/utils/canvas/open-canvas";
import { defaultDramaForNew, type AssetType, type EpisodeFilter } from "@/utils/drama/assets";
import { busyReason } from "@/utils/drama/jobs";
import { referenceWarning } from "@/utils/drama/segment-assets";
import { getCurrentUserId } from "@/utils/storage/user-id";

import { AssetsUiContext, type AssetsUi } from "./assets-ui";
import { AssetsOverlay } from "./assets-overlay";
import { useWorkspace } from "./context";
import { NodeEpisodesButton } from "./node-episodes";

/** 短剧画布的添加菜单：没有导演台（短剧一期不做 3D 预演），其余同无限画布；引用要稳定 */
const buildMenuItems = (disabled: (kind: NodeKind) => boolean) =>
  buildAddNodeItems(disabled).filter((item) => item.value !== "director");

/** 节点功能区追加「出现集数」；模块级函数，引用稳定 */
const renderToolbarExtra = ({ id, data }: { id: string; data: CanvasNodeData }) => (
  <NodeEpisodesButton id={id} data={data} />
);

/** 空节点（无素材无历史）也显示功能区：拆解出来的资产大多是空节点，但「出现集数」必须能用；导演台除外 */
const toolbarVisible = (data: CanvasNodeData) => data.kind !== "director";

/** 「另存为副本」产生的是普通画布，不在短剧里打开：不提供「打开」入口 */
const copyOpenPath = () => null;

/** 编辑器插槽里的元素放在模块顶层，引用不变，页面重渲染时不会牵动编辑器 */
const overlay = <AssetsOverlay />;

/** 集数筛选浮层：全集 / 第 N 集 / 未分类 */
function FilterButton({
  filter,
  onChange,
}: {
  filter: EpisodeFilter;
  onChange: (filter: EpisodeFilter) => void;
}) {
  const { drama } = useWorkspace();
  const [open, setOpen] = useState(false);
  const label =
    filter.kind === "all" ? "全集" : filter.kind === "none" ? "未分类" : `第 ${filter.no} 集`;
  const row = (active: boolean) =>
    cn(
      "hover:bg-muted focus-visible:ring-ring/50 flex min-h-8 items-center gap-2 rounded-md px-2 text-left text-sm outline-none focus-visible:ring-2",
      active && "bg-muted font-medium",
    );
  const pick = (next: EpisodeFilter) => {
    onChange(next);
    setOpen(false);
  };
  return (
    <Popover open={open} onOpenChange={setOpen}>
      <PopoverTrigger className="hover:bg-muted focus-visible:ring-ring/50 inline-flex h-8 items-center gap-1.5 rounded-lg border px-2.5 text-[13px] outline-none focus-visible:ring-2 [&_svg]:size-4">
        <Filter />
        {label}
        <ChevronDown className="opacity-50" />
      </PopoverTrigger>
      <PopoverContent align="end" sideOffset={6} className="w-64 gap-1">
        <button
          type="button"
          className={row(filter.kind === "all")}
          onClick={() => pick({ kind: "all" })}
        >
          全集
        </button>
        <div className="text-muted-foreground px-1.5 pt-1 text-xs">按集</div>
        <div className="grid grid-cols-3 gap-0.5">
          {drama.episodes.map((ep) => (
            <button
              key={ep.no}
              type="button"
              className={cn(row(filter.kind === "episode" && filter.no === ep.no), "tabular-nums")}
              onClick={() => pick({ kind: "episode", no: ep.no })}
            >
              第 {ep.no} 集
            </button>
          ))}
        </div>
        <button
          type="button"
          className={row(filter.kind === "none")}
          onClick={() => pick({ kind: "none" })}
        >
          未分类
          <span className="text-muted-foreground ml-auto text-xs">画布里随手建的</span>
        </button>
      </PopoverContent>
    </Popover>
  );
}

/** 画布区：加载短剧专属画布，用可复用的编辑器组装 */
function DramaCanvas({
  ui,
  commandsRef,
  onNodeCreate,
}: {
  ui: AssetsUi;
  commandsRef: React.RefObject<CanvasEditorCommands | null>;
  onNodeCreate: (node: CanvasNode) => CanvasNode;
}) {
  const { drama } = useWorkspace();
  const canvasId = drama.canvasId;
  const lock = useCanvasLock(canvasId);
  const dramaRef = useRef(drama);
  useEffect(() => {
    dramaRef.current = drama;
  }, [drama]);
  /** 删资产节点前：有片段的提示词引用了它就提示一声（只提示，不阻止） */
  const confirmDeleteNodes = useCallback(async (nodes: FlowNode[]): Promise<boolean> => {
    const doomed = nodes.flatMap((node) => {
      if (node.type === "group") return [];
      const data = node.data as CanvasNodeData;
      return data.drama?.type ? [{ id: node.id, label: data.label }] : [];
    });
    const current = dramaRef.current;
    const episodes = current.episodes.filter((ep) => ep.segmentCount > 0);
    if (doomed.length === 0 || episodes.length === 0) return true;
    let prompts: string[];
    try {
      const lists = await Promise.all(episodes.map((ep) => listSegments(current.id, ep.no)));
      prompts = lists.flat().map((segment) => segment.prompt);
    } catch {
      // 读不到片段就不拦着，提示只是善意提醒
      return true;
    }
    const warning = referenceWarning(doomed, prompts);
    if (!warning) return true;
    return confirm({
      title: `有 ${warning.segments} 个片段引用了它`,
      description: `「${warning.names.join("」「")}」被 ${warning.segments} 个片段的提示词引用。删除后这些引用会显示为失效，片段仍可生成，只是这部分只用文字。`,
      confirmLabel: "仍然删除",
      destructive: true,
    });
  }, []);
  const [opened, setOpened] = useState<OpenedCanvas | null>(null);
  const [error, setError] = useState(false);

  useEffect(() => {
    if (lock !== "held") return;
    let active = true;
    const userId = getCurrentUserId();
    void loadCanvasForEditing({
      canvasId,
      userId,
      getCanvas,
      loadDraft: (user, id) => draftStore.load(user, id),
      removeDraft: (id) => (userId ? draftStore.remove(userId, id) : Promise.resolve()),
    })
      .then((value) => active && setOpened(value))
      .catch(() => active && setError(true));
    return () => {
      active = false;
    };
  }, [canvasId, lock]);

  if (lock === "waiting") {
    return (
      <div className="text-muted-foreground grid h-full place-items-center text-sm">
        这部短剧的资产画布已在另一个标签页打开，关闭那边后会自动接管。
      </div>
    );
  }
  if (error) {
    return (
      <div className="text-muted-foreground grid h-full place-items-center text-sm">
        资产画布加载失败，请刷新页面重试
      </div>
    );
  }
  const current = opened && opened.canvas.id === canvasId ? opened : null;
  if (!current) {
    return (
      <div className="text-muted-foreground grid h-full place-items-center text-sm">
        <Loader2 className="size-5 animate-spin" />
      </div>
    );
  }
  return (
    <AssetsUiContext.Provider value={ui}>
      <ReactFlowProvider>
        <CanvasEditor
          key={`${canvasId}:${current.canvas.version}`}
          canvas={current.canvas}
          recovery={current.recovery}
          onConflict={(latest) => setOpened({ canvas: latest, recovery: null })}
          menuItems={buildMenuItems}
          onNodeCreate={onNodeCreate}
          toolbarExtra={renderToolbarExtra}
          toolbarVisible={toolbarVisible}
          confirmDeleteNodes={confirmDeleteNodes}
          copyOpenPath={copyOpenPath}
          containerClassName="h-full w-full"
          commandsRef={commandsRef}
          overlay={overlay}
        />
      </ReactFlowProvider>
    </AssetsUiContext.Provider>
  );
}

/**
 * 资产区：顶部是「列表 ⇄ 画布」切换、集数筛选、多选、资产拆解；下面是短剧专属画布（一直挂着），
 * 列表视图盖在画布上，读的是画布里同一份节点数据。
 */
export function AssetsArea({
  ep,
  onBreakdownOpen,
}: {
  /** 左侧分集栏当前选中的集；0 = 全集 */
  ep: number;
  onBreakdownOpen: () => void;
}) {
  const { drama, jobs } = useWorkspace();
  const [view, setView] = useState<"list" | "canvas">("list");
  const [filter, setFilter] = useState<EpisodeFilter>(
    ep === 0 ? { kind: "all" } : { kind: "episode", no: ep },
  );
  // 分集栏换了集，筛选跟着换（顶部的「未分类」筛选只在栏没动时保留）
  const [seenEp, setSeenEp] = useState(ep);
  if (seenEp !== ep) {
    setSeenEp(ep);
    setFilter(ep === 0 ? { kind: "all" } : { kind: "episode", no: ep });
  }
  const [type, setType] = useState<AssetType>("character");
  const [multi, setMulti] = useState(false);
  const [selected, setSelected] = useState<ReadonlySet<string>>(new Set());
  const commands = useRef<CanvasEditorCommands | null>(null);
  const newDrama = useRef<{ drama: NodeDrama | null } | null>(null);
  const setNewDrama = useCallback((pending: { drama: NodeDrama | null } | null) => {
    newDrama.current = pending;
  }, []);

  const ui = useMemo<AssetsUi>(
    () => ({
      view,
      setView: (next) => {
        setView(next);
        setMulti(false);
        setSelected(new Set());
      },
      filter,
      setFilter,
      type,
      setType,
      multi,
      setMulti: (next) => {
        setMulti(next);
        if (!next) setSelected(new Set());
      },
      selected,
      setSelected,
      commands,
      setNewDrama,
    }),
    [filter, multi, selected, setNewDrama, type, view],
  );

  /** 新建节点默认「全集」；从列表新建时按列表当前的类型 / 筛选归类（未分类筛选下就是未分类） */
  const onNodeCreate = useCallback((node: CanvasNode): CanvasNode => {
    const pending = newDrama.current;
    if (pending) {
      if (pending.drama === null) return node;
      return { ...node, data: { ...node.data, drama: node.data.drama ?? pending.drama } };
    }
    return { ...node, data: { ...node.data, drama: defaultDramaForNew(node.data) } };
  }, []);

  const breakdownBlocked = busyReason(jobs);

  return (
    <div className="flex h-full min-h-0 flex-col">
      <div className="border-border flex shrink-0 flex-wrap items-center gap-2 border-b px-3 py-2">
        <div role="group" aria-label="视图" className="bg-muted flex rounded-lg p-0.5">
          {(
            [
              ["list", "列表", LayoutGrid],
              ["canvas", "画布", Frame],
            ] as const
          ).map(([key, label, Icon]) => (
            <button
              key={key}
              type="button"
              aria-pressed={view === key}
              onClick={() => ui.setView(key)}
              className={cn(
                "focus-visible:ring-ring/50 inline-flex h-7 items-center gap-1.5 rounded-md px-3 text-[13px] outline-none focus-visible:ring-2 [&_svg]:size-4",
                view === key
                  ? "bg-background font-medium shadow-sm"
                  : "text-muted-foreground hover:text-foreground",
              )}
            >
              <Icon />
              {label}
            </button>
          ))}
        </div>
        <span className="text-muted-foreground text-xs max-md:hidden">同一份资产，两种看法</span>
        <span className="flex-1" />
        <FilterButton filter={filter} onChange={setFilter} />
        {view === "list" && (
          <Button
            variant={multi ? "default" : "outline"}
            size="sm"
            onClick={() => ui.setMulti(!multi)}
          >
            <ListChecks />
            {multi ? "完成" : "多选"}
          </Button>
        )}
        <Button
          variant="outline"
          size="sm"
          disabled={!!breakdownBlocked || drama.episodes.length === 0}
          title={breakdownBlocked ?? (drama.episodes.length === 0 ? "还没有分集" : undefined)}
          onClick={onBreakdownOpen}
        >
          <ScanSearch />
          资产拆解
        </Button>
      </div>
      <div className="relative min-h-0 flex-1">
        {/* 绝对定位铺满：编辑器的 h-full 才有确定的高度可依 */}
        <div className="absolute inset-0">
          <DramaCanvas ui={ui} commandsRef={commands} onNodeCreate={onNodeCreate} />
        </div>
      </div>
    </div>
  );
}
