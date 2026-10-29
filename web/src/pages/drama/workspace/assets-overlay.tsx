import { memo, useCallback, useEffect, useMemo, useState } from "react";
import { createPortal } from "react-dom";
import { AnimatePresence, motion } from "motion/react";
import { Hand, Plus, Search } from "lucide-react";
import { toast } from "sonner";

import { ChromePill, ChromeZone } from "@/components/canvas/chrome/chrome";
import { useCanvasEditor } from "@/components/canvas-editor/editor-api";
import { submitNodeById, submitVoiceById } from "@/components/canvas-editor/node-actions";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { useRemoteModels } from "@/hooks/use-models";
import { BottomToolbar } from "@/pages/canvas/chrome/bottom-toolbar";
import { ViewControls } from "@/pages/canvas/chrome/view-controls";
import { openShortcuts } from "@/pages/canvas/shortcuts-state";
import { DURATION, EASE_OUT } from "@/lib/motion";
import { cn } from "@/lib/utils";
import { useCharacterDetail } from "@/store/character-detail";
import { usePromptEditor } from "@/store/prompt-editor";
import {
  ASSET_TYPES,
  countByType,
  filterAssets,
  generatable,
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
import { useMarquee } from "./use-marquee";

/**
 * 资产库浮窗（画布视图）：已分类资产按类型分组，全集视图也展示未分类资产；点一项飞到画布节点。
 * 外观和底部工具条的胶囊同一套（半透明 chrome 底 + 模糊 + 提饱和），搜索框和条目也用 chrome 的透明层次。
 */
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
        (ui.filter.kind === "all" ||
          (ui.filter.kind === "none" ? item.episodes === null : item.episodes !== null)) &&
        (!q || `${item.name}${item.look}`.toLowerCase().includes(q)),
    ),
  })).filter((group) => group.list.length > 0);

  return (
    <ChromeZone
      position="top-left"
      className="bg-chrome ring-chrome-border text-foreground pointer-events-auto max-h-[60%] w-64 flex-col items-stretch gap-0 rounded-3xl p-0 shadow-lg ring-1 backdrop-blur-xl backdrop-saturate-150 max-md:hidden"
    >
      <div className="flex items-center gap-2 px-5 pt-4.5 pb-3 text-sm">
        <b className="font-semibold">资产库</b>
        <span className="text-muted-foreground text-xs tabular-nums">{items.length}</span>
      </div>
      <div className="relative px-3.5 pb-2">
        <Search className="text-muted-foreground pointer-events-none absolute top-2.5 left-6.5 size-3.5" />
        <Input
          value={query}
          onChange={(event) => setQuery(event.target.value)}
          placeholder="搜索资产"
          aria-label="搜索资产"
          className="bg-chrome-hover dark:bg-chrome-hover border-chrome-border h-9 rounded-full pl-9 text-[13px]"
        />
      </div>
      <div className="min-h-0 flex-1 overflow-y-auto px-2 pb-3 [scrollbar-width:thin]">
        {groups.map(({ type, list }) => {
          const Icon = TYPE_ICON[type.key];
          return (
            <div key={type.key} className="pt-2.5 first:pt-1">
              <div className="text-muted-foreground flex items-center gap-1.5 px-3 pt-1.5 pb-1.5 text-xs">
                <Icon className="size-3.5" />
                {type.label}
                <span className="tabular-nums">{list.length}</span>
              </div>
              {list.map((item) => (
                <button
                  key={item.id}
                  type="button"
                  onClick={() => focusNode(item.id)}
                  className="hover:bg-chrome-hover focus-visible:ring-ring/50 flex h-10 w-full items-center gap-2.5 rounded-xl px-3 text-left text-[13px] transition-colors duration-150 outline-none focus-visible:ring-2 focus-visible:ring-inset"
                >
                  <span className="min-w-0 flex-1 truncate">
                    {item.name}
                    {item.look && <span className="text-muted-foreground"> · {item.look}</span>}
                  </span>
                  {item.status !== "done" && (
                    <span className="bg-status-warning/15 text-status-warning shrink-0 rounded-full px-2 py-0.5 text-[10px] leading-4">
                      无图
                    </span>
                  )}
                </button>
              ))}
            </div>
          );
        })}
        {groups.length === 0 && (
          <div className="text-muted-foreground px-3 py-5 text-xs">这个筛选下没有资产</div>
        )}
      </div>
    </ChromeZone>
  );
}

/** 批量生成按钮里的积分估算；估不出来（没有定价）就不显示 */
function Credits({ value }: { value: number }) {
  if (value <= 0) return null;
  return (
    <>
      {" · 约 "}
      <span className="tabular-nums">{value}</span> 积分起
    </>
  );
}

/** 列表视图：类型 tab、资产卡网格（可鼠标框选，已生成的也能选上重新生成）、有选中时底部浮出批量操作栏 */
function AssetList({ items }: { items: AssetItem[] }) {
  const ui = useAssetsUi();
  const editor = useCanvasEditor();
  const imageModels = useRemoteModels("image");
  const audioModels = useRemoteModels("audio");
  const [batching, setBatching] = useState(false);

  const counts = useMemo(() => countByType(items, ui.filter), [items, ui.filter]);
  const shown = useMemo(() => filterAssets(items, ui.type, ui.filter), [items, ui.filter, ui.type]);
  const pending = useMemo(() => pendingGenerate(items, ui.type), [items, ui.type]);
  /** 能勾选生成的：待生成、失败，以及已生成（重新生成） */
  const candidates = useMemo(() => generatable(items, ui.type), [items, ui.type]);
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
  const selectedItems = candidates.filter((item) => ui.selected.has(item.id));
  const estimate = selectedItems.reduce((sum, item) => sum + unitOf(item), 0);
  /** 含音色时再加上每个角色一段音频（取第一个音频模型，和详情页默认一致） */
  const voiceEstimate =
    estimate + selectedItems.length * lowestUnit(audioModels.models[0]?.pricing);
  /** 能框选、全选的：当前显示出来的可生成卡片 */
  const selectable = useMemo(() => {
    const ids = new Set(candidates.map((item) => item.id));
    return shown.filter((item) => ids.has(item.id)).map((item) => item.id);
  }, [candidates, shown]);

  const { setMulti, setSelected, multi } = ui;
  const {
    ref: marqueeRef,
    box: marqueeBox,
    handlers: marqueeHandlers,
  } = useMarquee({
    selectable,
    selected: ui.selected,
    onSelect: (next) => {
      if (next.size > 0 && !multi) setMulti(true);
      setSelected(next);
    },
    onClear: () => setMulti(false),
  });
  // Esc 退出多选（开着弹窗时让弹窗自己处理）
  useEffect(() => {
    if (!multi) return;
    const onKeyDown = (event: KeyboardEvent) => {
      if (event.key === "Escape" && !document.querySelector("[role='dialog']")) setMulti(false);
    };
    document.addEventListener("keydown", onKeyDown);
    return () => document.removeEventListener("keydown", onKeyDown);
  }, [multi, setMulti]);

  /** 逐个提交生成；withVoice 时角色卡在形象之后再提交一段音色 */
  const generate = useCallback(async (ids: string[], withVoice: boolean) => {
    setBatching(true);
    let started = 0;
    let voiced = 0;
    const blocked: string[] = [];
    for (const id of ids) {
      // 成败以提交结果为准：节点是受控状态，提交刚结束时读到的 status 还是旧的，不能拿来判断
      const reason = await submitNodeById(id);
      if (reason) blocked.push(reason);
      else started += 1;
      if (!withVoice) continue;
      const voiceReason = await submitVoiceById(id);
      if (voiceReason) blocked.push(`音色：${voiceReason}`);
      else voiced += 1;
    }
    setBatching(false);
    if (started + voiced > 0) {
      toast.success(
        withVoice
          ? `已提交 ${started} 个形象、${voiced} 个音色任务`
          : `已提交 ${started} 个生成任务`,
      );
    }
    if (blocked.length > 0) {
      toast.warning(`有 ${blocked.length} 项没能提交：${blocked[0]}`);
    }
  }, []);

  const run = (withVoice: boolean) => {
    const ids = selectedItems.map((item) => item.id);
    ui.setMulti(false);
    void generate(ids, withVoice);
  };

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
      <div
        role="tablist"
        className="border-border flex items-center gap-0.5 overflow-x-auto border-b px-3"
      >
        {ASSET_TYPES.map((type) => (
          <button
            key={type.key}
            type="button"
            role="tab"
            aria-selected={ui.type === type.key}
            onClick={() => {
              ui.setType(type.key);
              ui.setMulti(false);
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
      {ui.generateAllTarget &&
        pending.length > 0 &&
        !ui.multi &&
        createPortal(
          <Button
            variant="outline"
            size="sm"
            onClick={() => {
              ui.setMulti(true);
              ui.setSelected(new Set(pending.map((item) => item.id)));
            }}
          >
            全部生成
          </Button>,
          ui.generateAllTarget,
        )}

      <div className="min-h-0 flex-1 overflow-y-auto">
        {/* 框选区：铺满滚动区，卡片之间和下方的空白都能按下开始框选 */}
        <div
          ref={marqueeRef}
          {...marqueeHandlers}
          className={cn(
            "relative min-h-full p-4",
            selectedItems.length > 0 && "pb-24",
            marqueeBox && "select-none",
          )}
        >
          <div className="grid grid-cols-2 gap-3 sm:grid-cols-3 md:grid-cols-4 lg:grid-cols-5 xl:grid-cols-6">
            {shown.map((item, index) => (
              <AssetCard
                key={item.id}
                item={item}
                index={index}
                multi={ui.multi}
                selected={ui.selected.has(item.id)}
                onToggle={() => {
                  const next = new Set(ui.selected);
                  if (!next.delete(item.id)) next.add(item.id);
                  if (next.size === 0) ui.setMulti(false);
                  else ui.setSelected(next);
                }}
                onLocate={() => locate(item.id)}
                onEditPrompt={() => usePromptEditor.getState().open(item.id)}
                onEditCharacter={() => useCharacterDetail.getState().open(item.id)}
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
          {marqueeBox && (
            <div
              aria-hidden
              className="border-primary bg-primary/10 pointer-events-none absolute z-20 rounded-sm border"
              style={marqueeBox}
            />
          )}
        </div>
      </div>

      <AnimatePresence>
        {ui.multi && selectedItems.length > 0 && (
          <div className="pointer-events-none absolute inset-x-0 bottom-5 z-30 flex justify-center px-4">
            <motion.div
              role="toolbar"
              aria-label="批量操作"
              initial={{ opacity: 0, y: 12 }}
              animate={{ opacity: 1, y: 0 }}
              exit={{ opacity: 0, y: 12, transition: { duration: DURATION.exit } }}
              transition={{ duration: DURATION.base, ease: EASE_OUT }}
              className="bg-popover text-popover-foreground ring-border pointer-events-auto flex items-center gap-1 rounded-xl p-1.5 pl-3.5 shadow-lg ring-1"
            >
              <span className="mr-1.5 text-sm whitespace-nowrap">
                已选 <b className="tabular-nums">{selectedItems.length}</b> 个
              </span>
              {selectedItems.length < selectable.length && (
                <Button
                  variant="ghost"
                  size="sm"
                  onClick={() => ui.setSelected(new Set([...ui.selected, ...selectable]))}
                >
                  全选
                </Button>
              )}
              <Button variant="ghost" size="sm" onClick={() => ui.setMulti(false)}>
                取消
              </Button>
              {ui.type === "character" ? (
                <>
                  <Button
                    variant="outline"
                    size="sm"
                    disabled={batching}
                    title="只生成角色形象图"
                    onClick={() => run(false)}
                  >
                    生成角色图
                    <Credits value={estimate} />
                  </Button>
                  <Button
                    size="sm"
                    disabled={batching}
                    title="生成角色形象图，并按音色描述生成声音"
                    onClick={() => run(true)}
                  >
                    含音色生成
                    <Credits value={voiceEstimate} />
                  </Button>
                </>
              ) : (
                <Button size="sm" disabled={batching} onClick={() => run(false)}>
                  生成 {selectedItems.length} 个
                  <Credits value={estimate} />
                </Button>
              )}
            </motion.div>
          </div>
        )}
      </AnimatePresence>
    </div>
  );
}

/** 画布视图下的外壳：左上资产库浮窗、底部居中横排工具条、视图控件、操作提示 */
function CanvasChrome({ items }: { items: AssetItem[] }) {
  const editor = useCanvasEditor();
  return (
    <>
      <LibraryFloat items={items} />
      <ChromeZone position="bottom-center">
        <BottomToolbar
          orientation="horizontal"
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
