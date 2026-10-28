import { memo, useCallback, useEffect, useMemo, useState } from "react";
import { Hand, Plus, Search, Sparkles, WandSparkles } from "lucide-react";
import { toast } from "sonner";

import { ChromePill, ChromeZone } from "@/components/canvas/chrome/chrome";
import { useCanvasEditor } from "@/components/canvas-editor/editor-api";
import { submitNodeById } from "@/components/canvas-editor/node-actions";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { useRemoteModels } from "@/hooks/use-models";
import { BottomToolbar } from "@/pages/canvas/chrome/bottom-toolbar";
import { ViewControls } from "@/pages/canvas/chrome/view-controls";
import { openShortcuts } from "@/pages/canvas/shortcuts-state";
import { cn } from "@/lib/utils";
import {
  ASSET_TYPES,
  countByType,
  filterAssets,
  newNodeKindOf,
  dramaForNewAsset,
  pendingGenerate,
  toAssetItems,
  type AssetItem,
} from "@/utils/drama/assets";
import { isActiveJob } from "@/utils/drama/jobs";
import { lowestUnit } from "@/utils/pricing/quote";

import { AssetCard, TYPE_ICON } from "./asset-card";
import { useAssetsUi } from "./assets-ui";
import { useWorkspace } from "./context";

/** 资产库浮窗（画布视图）：当前集数筛选下的资产按类型分组，点一项飞到画布上的节点 */
function LibraryFloat({ items }: { items: AssetItem[] }) {
  const ui = useAssetsUi();
  const { focusNode } = useCanvasEditor();
  const [query, setQuery] = useState("");
  const q = query.trim().toLowerCase();
  const groups = ASSET_TYPES.map((type) => ({
    type,
    list: items.filter(
      (item) =>
        item.type === type.key &&
        (ui.filter.kind === "none" ? item.episodes === null : item.episodes !== null) &&
        (!q || `${item.name}${item.look}`.toLowerCase().includes(q)),
    ),
  })).filter((group) => group.list.length > 0);

  return (
    <ChromeZone
      position="top-left"
      className="bg-chrome ring-chrome-border pointer-events-auto left-[4.5rem] max-h-[60%] w-56 flex-col items-stretch gap-0 rounded-2xl p-0 shadow-lg ring-1 backdrop-blur-xl max-md:hidden"
    >
      <div className="flex items-center gap-2 px-3 pt-3 pb-1.5 text-sm">
        <b>资产库</b>
        <span className="text-muted-foreground text-xs tabular-nums">{items.length}</span>
      </div>
      <div className="relative px-3 pb-2">
        <Search className="text-muted-foreground absolute top-2 left-5 size-3.5" />
        <Input
          value={query}
          onChange={(event) => setQuery(event.target.value)}
          placeholder="搜索资产"
          aria-label="搜索资产"
          className="h-7 pl-7 text-xs"
        />
      </div>
      <div className="min-h-0 flex-1 overflow-y-auto pb-2">
        {groups.map(({ type, list }) => {
          const Icon = TYPE_ICON[type.key];
          return (
            <div key={type.key}>
              <div className="text-muted-foreground flex items-center gap-1.5 px-3 pt-2 pb-1 text-xs">
                <Icon className="size-3.5" />
                {type.label}
                <span className="tabular-nums">{list.length}</span>
              </div>
              {list.map((item) => (
                <button
                  key={item.id}
                  type="button"
                  onClick={() => focusNode(item.id)}
                  className="hover:bg-chrome-hover focus-visible:ring-ring/50 flex h-8 w-full items-center gap-2 px-3 text-left text-[13px] outline-none focus-visible:ring-2 focus-visible:ring-inset"
                >
                  <span className="min-w-0 flex-1 truncate">
                    {item.name}
                    {item.look && <span className="text-muted-foreground"> · {item.look}</span>}
                  </span>
                  {item.status !== "done" && (
                    <span className="bg-status-warning/15 text-status-warning shrink-0 rounded-full px-1.5 text-[10px] leading-4">
                      无图
                    </span>
                  )}
                </button>
              ))}
            </div>
          );
        })}
        {groups.length === 0 && (
          <div className="text-muted-foreground px-3 py-4 text-xs">这个筛选下没有资产</div>
        )}
      </div>
    </ChromeZone>
  );
}

/** 列表视图：类型 tab、批量生成横幅、资产卡网格 */
function AssetList({ items }: { items: AssetItem[] }) {
  const ui = useAssetsUi();
  const editor = useCanvasEditor();
  const imageModels = useRemoteModels("image");
  const [batching, setBatching] = useState(false);

  const counts = useMemo(() => countByType(items, ui.filter), [items, ui.filter]);
  const shown = useMemo(() => filterAssets(items, ui.type, ui.filter), [items, ui.filter, ui.type]);
  const pending = useMemo(() => pendingGenerate(items, ui.type), [items, ui.type]);
  const typeMeta = ASSET_TYPES.find((type) => type.key === ui.type)!;

  /** 估算：每个选中的节点按它的模型（缺省取第一个）最低单价累加 */
  const unitOf = useCallback(
    (item: AssetItem) => {
      const model =
        imageModels.models.find((m) => m.key === item.data.model) ?? imageModels.models[0];
      return lowestUnit(model?.pricing);
    },
    [imageModels.models],
  );
  const selectedItems = pending.filter((item) => ui.selected.has(item.id));
  const estimate = selectedItems.reduce((sum, item) => sum + unitOf(item), 0);

  const generate = useCallback(
    async (ids: string[]) => {
      setBatching(true);
      let started = 0;
      const blocked: string[] = [];
      for (const id of ids) {
        const reason = await submitNodeById(id);
        if (reason) {
          blocked.push(reason);
          continue;
        }
        // 提交本身的失败（积分不足等）由节点就地提示；这里按节点数据确认是不是真的开始了
        const node = editor.getNodes().find((n) => n.id === id);
        if (node && node.type !== "group" && node.data.status === "running") started += 1;
        else blocked.push("没有提交成功，到画布里选中它可以看到原因");
      }
      setBatching(false);
      if (started > 0) toast.success(`已提交 ${started} 个生成任务`);
      if (blocked.length > 0) {
        toast.warning(`有 ${blocked.length} 个没能提交：${blocked[0]}`);
      }
    },
    [editor],
  );

  const locate = (id: string) => {
    ui.setView("canvas");
    // 切回画布视图后再飞过去，让用户看得到节点
    requestAnimationFrame(() => editor.focusNode(id));
  };

  const createNew = () => {
    const kind = newNodeKindOf(ui.type);
    ui.setNewDrama({ drama: dramaForNewAsset(ui.type, ui.filter) });
    editor.addAtCenter(kind);
    toast.info("已在短剧画布上新建节点，列表同步出现");
  };

  const hint =
    ui.filter.kind === "none"
      ? `画布里没有未分类的${typeMeta.label}`
      : `这个筛选下还没有${typeMeta.label}`;

  return (
    <div className="bg-background absolute inset-0 z-20 flex flex-col">
      <div role="tablist" className="border-border flex gap-0.5 overflow-x-auto border-b px-3">
        {ASSET_TYPES.map((type) => (
          <button
            key={type.key}
            type="button"
            role="tab"
            aria-selected={ui.type === type.key}
            onClick={() => {
              ui.setType(type.key);
              ui.setSelected(new Set());
            }}
            className={cn(
              "focus-visible:ring-ring/50 relative flex h-10 shrink-0 items-center gap-1.5 px-3 text-sm outline-none focus-visible:ring-2 focus-visible:ring-inset",
              ui.type === type.key
                ? "text-foreground after:bg-foreground font-medium after:absolute after:inset-x-2 after:bottom-0 after:h-0.5 after:rounded-full"
                : "text-muted-foreground hover:text-foreground",
            )}
          >
            {type.label}
            <span className="text-muted-foreground text-xs tabular-nums">{counts[type.key]}</span>
          </button>
        ))}
      </div>

      <div className="min-h-0 flex-1 overflow-y-auto p-4">
        {pending.length > 0 && (
          <div className="bg-muted/60 mb-4 flex flex-wrap items-center gap-2 rounded-xl px-3 py-2 text-sm">
            <Sparkles className="text-credit size-4 shrink-0" />
            <div className="min-w-0 flex-1">
              <b>
                还有 {pending.length} 个{typeMeta.label}没有图
              </b>
              <span className="text-muted-foreground">　可以上传，或勾选后批量生成</span>
            </div>
            {ui.multi ? (
              <Button
                size="sm"
                disabled={selectedItems.length === 0 || batching}
                title={selectedItems.length === 0 ? "先勾选卡片" : undefined}
                onClick={() => {
                  const ids = selectedItems.map((item) => item.id);
                  ui.setSelected(new Set());
                  ui.setMulti(false);
                  void generate(ids);
                }}
              >
                生成 {selectedItems.length} 个
                {estimate > 0 && (
                  <>
                    {" · 约 "}
                    <span className="tabular-nums">{estimate}</span> 积分起
                  </>
                )}
              </Button>
            ) : (
              <Button
                variant="outline"
                size="sm"
                onClick={() => {
                  ui.setMulti(true);
                  ui.setSelected(new Set(pending.map((item) => item.id)));
                }}
              >
                <WandSparkles />
                全部生成
              </Button>
            )}
          </div>
        )}

        <div className="grid grid-cols-2 gap-3 sm:grid-cols-3 md:grid-cols-4 lg:grid-cols-5 xl:grid-cols-6">
          {shown.map((item, index) => (
            <AssetCard
              key={item.id}
              item={item}
              index={index}
              multi={ui.multi}
              selected={ui.selected.has(item.id)}
              generating={batching}
              onToggle={() => {
                const next = new Set(ui.selected);
                if (!next.delete(item.id)) next.add(item.id);
                ui.setSelected(next);
              }}
              onGenerate={() => void generate([item.id])}
              onLocate={() => locate(item.id)}
            />
          ))}
          <button
            type="button"
            onClick={createNew}
            className="text-muted-foreground hover:bg-muted/60 focus-visible:ring-ring/50 flex aspect-3/4 flex-col items-center justify-center gap-1.5 rounded-xl border border-dashed text-sm outline-none focus-visible:ring-2"
          >
            <Plus className="size-5" />
            {ui.type === "character" ? "引入角色卡" : `新建${typeMeta.label}`}
            <span className="text-xs">
              {ui.filter.kind === "none"
                ? "未分类"
                : ui.filter.kind === "episode"
                  ? `出现在第 ${ui.filter.no} 集`
                  : "全集出现"}
            </span>
          </button>
        </div>
        {shown.length === 0 && (
          <div className="text-muted-foreground py-8 text-center text-sm">{hint}</div>
        )}
      </div>
    </div>
  );
}

/** 画布视图下的外壳：左侧工具条、资产库浮窗、视图控件、操作提示 */
function CanvasChrome({ items }: { items: AssetItem[] }) {
  const editor = useCanvasEditor();
  return (
    <>
      <LibraryFloat items={items} />
      <ChromeZone position="left-center">
        <BottomToolbar
          tool={editor.tool}
          onToolChange={editor.setTool}
          onAdd={editor.addAtCenter}
          onUpload={editor.uploadAtCenter}
          onOpenAssets={editor.openAssetPicker}
          canUndo={editor.history.canUndo}
          canRedo={editor.history.canRedo}
          onUndo={editor.history.undo}
          onRedo={editor.history.redo}
        />
      </ChromeZone>
      <ChromeZone position="bottom-left" className="max-md:hidden">
        <ViewControls
          minimap={editor.minimap}
          onMinimapChange={editor.setMinimap}
          onOpenShortcuts={openShortcuts}
        />
      </ChromeZone>
      <ChromeZone position="bottom-right" className="max-md:hidden">
        <ChromePill className="text-muted-foreground gap-1.5 px-3 text-xs">
          <Hand className="size-3.5" />
          拖动空白处平移 · 点节点设置出现集数
        </ChromePill>
      </ChromeZone>
    </>
  );
}

/**
 * 短剧资产区放进编辑器 overlay 插槽的内容：列表视图盖在画布上，画布视图摆画布外壳。
 * 列表和画布读的是同一份节点数据（editor.nodes），不另存；切到列表时取消画布里的选中，免得 Delete 误删节点。
 */
export const AssetsOverlay = memo(function AssetsOverlay() {
  const ui = useAssetsUi();
  const editor = useCanvasEditor();
  const { jobs } = useWorkspace();
  const analyzing = jobs.some((job) => job.step === "asset_prompts" && isActiveJob(job));
  const items = useMemo(() => toAssetItems(editor.nodes, { analyzing }), [analyzing, editor.nodes]);

  const { clearSelection } = editor;
  const { setNewDrama } = ui;
  useEffect(() => {
    if (ui.view === "list") clearSelection();
    // 回到画布视图后，节点都是用户在画布上自己建的，不再沿用列表里设的归类
    else setNewDrama(null);
  }, [ui.view, clearSelection, setNewDrama]);

  return ui.view === "list" ? <AssetList items={items} /> : <CanvasChrome items={items} />;
});
