import { useCallback, useEffect, useRef, useState, type ReactNode } from "react";
import { motion } from "motion/react";
import { useStore } from "@xyflow/react";
import {
  CloudCheck,
  Download,
  Grid2x2,
  LoaderCircle,
  Move,
  PanelLeftClose,
  PanelLeftOpen,
  Redo2,
  RotateCw,
  Scan,
  Trash2,
  TriangleAlert,
  Type,
  Undo2,
  X,
} from "lucide-react";
import { toast } from "sonner";

import { DURATION, EASE_OUT, TAP } from "@/lib/motion";
import { cn } from "@/lib/utils";
import { useDirectorStore } from "@/store/director";
import type { CanvasNodeData, FigureModel } from "@/types";
import { activeCameraOf } from "@/utils/director/camera-view";
import {
  HEADER_HEIGHT,
  NAV_WIDTH,
  type PanelKind,
  fitPanels,
  panelMax,
} from "@/utils/director/layout";
import { UNSUPPORTED_EXPORT_MESSAGE, canExportVideo } from "@/utils/director/export-engine";
import { disposeThumbRenderer } from "@/utils/director/asset-thumbs";
import { figureModelStatus, loadFigureModels } from "@/utils/director/figure";

import { DirectorAssets } from "./director-assets";
import { DirectorExportDialog } from "./director-export-dialog";
import { DirectorPreview } from "./director-preview";
import { DirectorSidebar } from "./director-sidebar";
import { DirectorTimeline } from "./director-timeline";
import { DirectorViewport, type DirectorViewportApi } from "./director-viewport";
import { NUDGE_STEP, ResizeHandle } from "./resize-handle";
import { MENU_ATTR, ViewModeSwitch } from "./view-mode-switch";
import { useDirectorSync } from "./use-director-sync";

/** 分界线的可点击宽度（像素），压在两个面板的交界上 */
const HANDLE_SIZE = 8;
const HANDLE = { width: HANDLE_SIZE, marginLeft: -HANDLE_SIZE / 2 } as const;

/** 视口左上角的操作提示：两个视角共用一套相机操作，机位视角改的是当前机位 */
const HINT = {
  director:
    "导演视角 · 左键按住拖动 · 右键按住平移 · 滚轮上下摇 · Ctrl / Alt + 滚轮变焦 · WASD / 方向键平移 · 点选对象后拖动彩色轴",
  camera:
    "机位视角 · 左键按住拖动调整朝向 · 滚轮上下摇 · Ctrl / Alt + 滚轮变焦 · WASD / 方向键平移机位 · 要摆人物请切回导演视角",
} as const;

/** 键盘事件落在这些元素上时交给它们自己（打字、选下拉） */
const isTyping = (target: EventTarget | null) =>
  target instanceof HTMLElement &&
  !!target.closest(
    `input:not([type=checkbox]):not([type=radio]), textarea, select, [role=separator], [${MENU_ATTR}]`,
  );

/**
 * 导演台全屏编辑器（设计稿 docs/design/3D导演台 §6.3）：懒加载，带着 three.js。
 * 始终是深色（根节点加 dark 类，里面的 token 都取深色那套）。
 * 场景改动自动写回节点；只有导出会在画布上建新节点。
 */
export default function DirectorEditor() {
  const nodeId = useDirectorStore((state) => state.nodeId)!;
  const label = useStore(
    (state) =>
      (state.nodeLookup.get(nodeId)?.data as CanvasNodeData | undefined)?.label ?? "导演台",
  );
  const hasThumb = useStore(
    (state) => !!(state.nodeLookup.get(nodeId)?.data as CanvasNodeData | undefined)?.scene?.thumb,
  );
  const { status, finish } = useDirectorSync(nodeId);
  const viewport = useRef<DirectorViewportApi>(null);
  const rootRef = useRef<HTMLDivElement>(null);
  const [exportOpen, setExportOpen] = useState(() =>
    useDirectorStore.getState().takeExportOnOpen(),
  );
  const [exportSupported, setExportSupported] = useState<boolean | null>(null);
  /** 场景里用到的人物模型（男、女），形如 "female,male"；变化时才去加载新出现的那个 */
  const modelsKey = useDirectorStore((state) =>
    [...new Set((state.scene?.characters ?? []).map((c) => c.model ?? "male"))].sort().join(","),
  );
  /** 人台模型有没有加载失败：失败时人物是简化形体，给个提示条和重试 */
  const [modelFailed, setModelFailed] = useState(false);
  const retryModel = useCallback(() => {
    setModelFailed(false);
    void loadFigureModels(modelsKey.split(",").filter(Boolean) as FigureModel[]).then((ok) =>
      setModelFailed(!ok),
    );
  }, [modelsKey]);
  const scene = useDirectorStore((state) => state.scene);
  const playing = useDirectorStore((state) => state.playing);
  const selectedId = useDirectorStore((state) => state.selectedId);
  const tool = useDirectorStore((state) => state.tool);
  const snap = useDirectorStore((state) => state.snap);
  const labels = useDirectorStore((state) => state.labels);
  const split = useDirectorStore((state) => state.previewSplit);
  const camView = useDirectorStore((state) => activeCameraOf(state.scene, state.viewMode) !== null);
  const hint = camView ? HINT.camera : HINT.director;
  const assetsOpen = useDirectorStore((state) => state.assetsOpen);
  const panels = useDirectorStore((state) => state.panels);
  const mainRef = useRef<HTMLElement>(null);
  /** 窗口大小：编辑器是全屏的，面板尺寸要按它收一收，保证 3D 视口区留够 */
  const [area, setArea] = useState(() => ({
    width: window.innerWidth,
    height: window.innerHeight,
  }));
  useEffect(() => {
    const onResize = () => setArea({ width: window.innerWidth, height: window.innerHeight });
    window.addEventListener("resize", onResize);
    return () => window.removeEventListener("resize", onResize);
  }, []);
  const sizes = fitPanels(panels, area, assetsOpen);
  /** 改面板尺寸：夹在它自己的范围里，也不挤掉 3D 视口区的最小尺寸 */
  const setPanel = (kind: PanelKind, value: number) =>
    useDirectorStore
      .getState()
      .setPanel(kind, Math.min(value, panelMax(kind, sizes, area, assetsOpen)));
  const savePanels = () => useDirectorStore.getState().savePanels();
  const resetPanel = (kind: PanelKind) => useDirectorStore.getState().resetPanel(kind);
  const canUndo = useDirectorStore((state) => state.past.length > 0);
  const canRedo = useDirectorStore((state) => state.future.length > 0);

  useEffect(() => {
    let alive = true;
    void canExportVideo().then((ok) => alive && setExportSupported(ok));
    rootRef.current?.focus({ preventScroll: true });
    return () => {
      alive = false;
      // 编辑器关了，拍资产缩略图用的渲染器一并释放；拍好的图留在缓存里
      disposeThumbRenderer();
    };
  }, []);

  // 场景里用到哪些人物模型就加载哪些：加载好之前先用简化形体，加载好舞台自己换成人台；
  // 新加一个女性人物时这里会去读女性模型
  useEffect(() => {
    let alive = true;
    const models = modelsKey.split(",").filter(Boolean) as FigureModel[];
    if (models.every((model) => figureModelStatus(model) === "ready")) return;
    void loadFigureModels(models).then((ok) => alive && setModelFailed(!ok));
    return () => {
      alive = false;
    };
  }, [modelsKey]);

  const close = useCallback(() => {
    const state = useDirectorStore.getState();
    if (state.exporting) {
      toast("导出中，先取消导出再关闭");
      return;
    }
    if (state.scene) finish(state.scene, hasThumb);
    state.close();
  }, [finish, hasThumb]);

  // 播放：按真实时间推进播放头，到末尾停下
  useEffect(() => {
    if (!playing) return;
    const store = useDirectorStore.getState();
    if (store.scene && store.t >= store.scene.duration) store.setTime(0);
    let last = performance.now();
    let frame = requestAnimationFrame(function tick(now) {
      const state = useDirectorStore.getState();
      if (!state.scene || !state.playing) return;
      const next = state.t + (now - last) / 1000;
      last = now;
      state.setTime(next);
      if (next >= state.scene.duration) state.setPlaying(false);
      else frame = requestAnimationFrame(tick);
    });
    return () => cancelAnimationFrame(frame);
  }, [playing]);

  const cameraFromView = useCallback(() => {
    const pose = viewport.current?.viewPose();
    if (!pose) return;
    const message = useDirectorStore.getState().addObject("camera", { cameraPose: pose });
    if (message) toast(message);
    else toast.success("已从当前视角新建机位");
  }, []);

  // 快捷键：在捕获阶段处理并拦住，画布自己的快捷键（删节点、复制粘贴）在编辑器打开时不生效
  useEffect(() => {
    const onKey = (event: KeyboardEvent) => {
      if (isTyping(event.target)) return;
      event.stopPropagation();
      const store = useDirectorStore.getState();
      const key = event.key.toLowerCase();
      const mod = event.metaKey || event.ctrlKey;
      if (mod && key === "z") {
        event.preventDefault();
        if (event.shiftKey) store.redo();
        else store.undo();
      } else if (mod && key === "y") {
        event.preventDefault();
        store.redo();
      } else if (mod) {
        return;
      } else if (key === "k") {
        if (store.selectedId) store.recordKey(store.selectedId);
      } else if (key === " ") {
        event.preventDefault();
        store.setPlaying(!store.playing);
      } else if (key === "1") store.setTool("move");
      else if (key === "2") store.setTool("rotate");
      else if (key === "f") viewport.current?.focusSelected();
      else if (key === "delete" || key === "backspace") {
        if (store.selectedKey) store.deleteSelectedKey();
        else if (store.selectedId) store.removeObject(store.selectedId);
      } else if (key === "escape") {
        if (exportOpen) {
          if (!store.exporting) setExportOpen(false);
        } else if (store.selectedId) store.select(null);
        else close();
      }
    };
    // 粘贴也拦住：编辑器打开时不往画布上贴节点
    const onPaste = (event: ClipboardEvent) => {
      if (!isTyping(event.target)) event.stopPropagation();
    };
    window.addEventListener("keydown", onKey, true);
    window.addEventListener("paste", onPaste, true);
    return () => {
      window.removeEventListener("keydown", onKey, true);
      window.removeEventListener("paste", onPaste, true);
    };
  }, [close, exportOpen]);

  if (!scene) return null;
  const empty = !scene.characters.length && !scene.props.length && !scene.cameras.length;
  const exportBlocked = !scene.cameras.length
    ? "没有机位，先添加一台机位"
    : exportSupported === false
      ? UNSUPPORTED_EXPORT_MESSAGE
      : null;

  return (
    <motion.div
      ref={rootRef}
      tabIndex={-1}
      role="dialog"
      aria-modal
      aria-label={`导演台：${label}`}
      initial={{ opacity: 0 }}
      animate={{ opacity: 1 }}
      transition={{ duration: DURATION.slow, ease: EASE_OUT }}
      style={{ gridTemplateRows: `${HEADER_HEIGHT}px minmax(0,1fr) ${sizes.timeline}px` }}
      className="dark bg-background text-foreground fixed inset-0 z-50 grid outline-none"
    >
      <header className="border-border flex items-center gap-1 border-b px-3">
        <span className="mr-2 max-w-60 truncate text-sm font-semibold">{label}</span>
        <ToolButton
          title="撤销（Ctrl+Z）"
          disabled={!canUndo}
          onClick={() => useDirectorStore.getState().undo()}
        >
          <Undo2 />
        </ToolButton>
        <ToolButton
          title="重做（Ctrl+Shift+Z）"
          disabled={!canRedo}
          onClick={() => useDirectorStore.getState().redo()}
        >
          <Redo2 />
        </ToolButton>
        <Divider />
        <ToolButton
          title="吸附：移动 0.25 米、旋转 15° 一档"
          active={snap}
          wide
          onClick={() => useDirectorStore.getState().toggleSnap()}
        >
          <Grid2x2 />
          吸附
        </ToolButton>
        <ToolButton
          title="显示人物和机位名称"
          active={labels}
          wide
          onClick={() => useDirectorStore.getState().toggleLabels()}
        >
          <Type />
          标签
        </ToolButton>
        <span className="ml-auto" />
        <SaveStatus status={status} />
        <motion.button
          type="button"
          whileTap={exportBlocked ? undefined : TAP}
          disabled={!!exportBlocked}
          title={exportBlocked ?? "导出运镜预览视频"}
          onClick={() => setExportOpen(true)}
          className="bg-primary text-primary-foreground hover:bg-primary/85 focus-visible:ring-ring ml-2 inline-flex h-8 items-center gap-1.5 rounded-md px-3 text-xs font-semibold outline-none focus-visible:ring-2 disabled:opacity-40"
        >
          <Download className="size-3.5" />
          导出视频
        </motion.button>
        <ToolButton title="关闭（自动保存）" onClick={close}>
          <X />
        </ToolButton>
      </header>

      <div
        style={{
          gridTemplateColumns: [
            `${NAV_WIDTH}px`,
            ...(assetsOpen ? [`${sizes.assets}px`] : []),
            "minmax(0,1fr)",
            `${sizes.sidebar}px`,
          ].join(" "),
        }}
        className="relative grid min-h-0"
      >
        <nav
          className="border-border flex flex-col items-center gap-1 border-r py-2"
          aria-label="工具"
        >
          <ToolButton
            title={assetsOpen ? "折叠资产面板" : "展开资产面板（添加人物、物体、机位）"}
            active={assetsOpen}
            onClick={() => useDirectorStore.getState().toggleAssets()}
          >
            {assetsOpen ? <PanelLeftClose /> : <PanelLeftOpen />}
          </ToolButton>
          <span className="bg-border my-1 h-px w-6" />
          <ToolButton
            title="移动（1）"
            active={tool === "move"}
            onClick={() => useDirectorStore.getState().setTool("move")}
          >
            <Move />
          </ToolButton>
          <ToolButton
            title="旋转（2）"
            active={tool === "rotate"}
            onClick={() => useDirectorStore.getState().setTool("rotate")}
          >
            <RotateCw />
          </ToolButton>
          <span className="bg-border my-1 h-px w-6" />
          <ToolButton
            title="聚焦选中（F）"
            disabled={!selectedId}
            onClick={() => viewport.current?.focusSelected()}
          >
            <Scan />
          </ToolButton>
          <ToolButton
            title="删除选中（Delete）"
            disabled={!selectedId}
            onClick={() => selectedId && useDirectorStore.getState().removeObject(selectedId)}
          >
            <Trash2 />
          </ToolButton>
        </nav>

        {assetsOpen && <DirectorAssets onCameraFromView={cameraFromView} />}

        <main ref={mainRef} className="relative min-h-0 min-w-0 overflow-hidden">
          {/* 导演视角：分栏时收成左边一块（右边是机位画面）；视口自己始终挂着，只是这个容器变窄，WebGL 上下文不重建 */}
          <div
            style={{ right: split ? `${sizes.split * 100}%` : 0 }}
            className="absolute inset-y-0 left-0"
          >
            <DirectorViewport ref={viewport} />
            <span
              title={hint}
              className="bg-card/80 text-muted-foreground border-border pointer-events-none absolute top-2.5 left-2.5 max-w-[calc(100%-1.25rem)] truncate rounded-md border px-2 py-1 text-[11px]"
            >
              {hint}
            </span>
            {modelFailed && (
              <div className="bg-status-running/15 text-foreground border-status-running/40 absolute top-12 left-1/2 flex max-w-[calc(100%-1.5rem)] -translate-x-1/2 items-center gap-2 rounded-md border px-3 py-1.5 text-xs">
                <TriangleAlert className="text-status-running size-3.5" />
                人物模型加载失败，已用简化形体代替（导出仍可用）
                <button
                  type="button"
                  onClick={retryModel}
                  className="hover:bg-muted focus-visible:ring-ring rounded px-1.5 py-0.5 font-medium underline-offset-2 outline-none hover:underline focus-visible:ring-2"
                >
                  重试
                </button>
              </div>
            )}
            {empty && (
              <p className="text-muted-foreground pointer-events-none absolute inset-0 grid place-items-center text-sm">
                添加人物或机位开始摆位
              </p>
            )}
            <ViewModeSwitch />
          </div>
          <DirectorPreview />
          {split && (
            <ResizeHandle
              orientation="vertical"
              label="机位画面宽度"
              style={{ left: `${(1 - sizes.split) * 100}%`, top: 0, bottom: 0, ...HANDLE }}
              onStart={() => sizes.split * (mainRef.current?.clientWidth ?? 0)}
              onMove={(start, delta) => {
                const width = mainRef.current?.clientWidth ?? 0;
                if (width > 0) setPanel("split", (start - delta) / width);
              }}
              onEnd={savePanels}
              onReset={() => resetPanel("split")}
              onNudge={(direction) => {
                const width = mainRef.current?.clientWidth ?? 0;
                if (width > 0) setPanel("split", sizes.split - (direction * NUDGE_STEP) / width);
              }}
            />
          )}
        </main>

        <DirectorSidebar />

        {assetsOpen && (
          <ResizeHandle
            orientation="vertical"
            label="资产面板宽度"
            style={{
              left: NAV_WIDTH + sizes.assets - HANDLE_SIZE / 2,
              top: 0,
              bottom: 0,
              width: HANDLE_SIZE,
            }}
            onStart={() => sizes.assets}
            onMove={(start, delta) => setPanel("assets", start + delta)}
            onEnd={savePanels}
            onReset={() => resetPanel("assets")}
            onNudge={(direction) => setPanel("assets", sizes.assets + direction * NUDGE_STEP)}
          />
        )}
        <ResizeHandle
          orientation="vertical"
          label="属性栏宽度"
          style={{ right: sizes.sidebar - HANDLE_SIZE / 2, top: 0, bottom: 0, width: HANDLE_SIZE }}
          onStart={() => sizes.sidebar}
          onMove={(start, delta) => setPanel("sidebar", start - delta)}
          onEnd={savePanels}
          onReset={() => resetPanel("sidebar")}
          onNudge={(direction) => setPanel("sidebar", sizes.sidebar - direction * NUDGE_STEP)}
        />
      </div>

      <DirectorTimeline />
      <ResizeHandle
        orientation="horizontal"
        label="时间轴高度"
        style={{ left: 0, right: 0, bottom: sizes.timeline - HANDLE_SIZE / 2, height: HANDLE_SIZE }}
        onStart={() => sizes.timeline}
        onMove={(start, delta) => setPanel("timeline", start - delta)}
        onEnd={savePanels}
        onReset={() => resetPanel("timeline")}
        onNudge={(direction) => setPanel("timeline", sizes.timeline - direction * NUDGE_STEP)}
      />

      {exportOpen && (
        <DirectorExportDialog
          nodeId={nodeId}
          onClose={() => setExportOpen(false)}
          onBackToCanvas={() => {
            setExportOpen(false);
            close();
          }}
        />
      )}
    </motion.div>
  );
}

function SaveStatus({ status }: { status: ReturnType<typeof useDirectorSync>["status"] }) {
  return (
    <span
      className={cn(
        "text-muted-foreground inline-flex items-center gap-1.5 text-xs",
        status === "too-large" && "text-destructive",
      )}
    >
      {status === "saving" ? (
        <>
          <LoaderCircle className="size-3.5 animate-spin" />
          保存中…
        </>
      ) : status === "too-large" ? (
        <>
          <TriangleAlert className="size-3.5" />
          场景过大，未保存
        </>
      ) : (
        <>
          <CloudCheck className="size-3.5" />
          已保存到节点
        </>
      )}
    </span>
  );
}

function ToolButton({
  title,
  active,
  disabled,
  wide,
  onClick,
  children,
}: {
  title: string;
  active?: boolean;
  disabled?: boolean;
  /** 带文字的按钮 */
  wide?: boolean;
  onClick: () => void;
  children: ReactNode;
}) {
  return (
    <motion.button
      type="button"
      title={title}
      aria-label={title}
      aria-pressed={active}
      disabled={disabled}
      whileTap={disabled ? undefined : TAP}
      onClick={onClick}
      className={cn(
        "text-foreground hover:bg-muted focus-visible:ring-ring inline-flex h-8 shrink-0 items-center justify-center gap-1.5 rounded-md text-xs outline-none focus-visible:ring-2 disabled:opacity-40 [&_svg]:size-4",
        wide ? "px-2.5" : "w-8",
        active && "bg-muted ring-border ring-1",
      )}
    >
      {children}
    </motion.button>
  );
}

function Divider() {
  return <span className="bg-border mx-1 h-5 w-px" />;
}
