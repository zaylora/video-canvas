import { useCallback, useEffect, useRef, useState, type ReactNode } from "react";
import { motion } from "motion/react";
import { useStore } from "@xyflow/react";
import {
  Aperture,
  Box,
  CloudCheck,
  Download,
  Grid2x2,
  LoaderCircle,
  Move,
  Redo2,
  RotateCw,
  Scan,
  Trash2,
  TriangleAlert,
  Type,
  Undo2,
  UserRoundPlus,
  Video,
  X,
} from "lucide-react";
import { toast } from "sonner";

import { DURATION, EASE_OUT, TAP } from "@/lib/motion";
import { cn } from "@/lib/utils";
import { useDirectorStore, type DirectorAddKind } from "@/store/director";
import type { CanvasNodeData } from "@/types";
import { UNSUPPORTED_EXPORT_MESSAGE, canExportVideo } from "@/utils/director/export-engine";
import { canAdd, limitMessage } from "@/utils/director/scene";

import { DirectorExportDialog } from "./director-export-dialog";
import { DirectorPreview } from "./director-preview";
import { DirectorSidebar } from "./director-sidebar";
import { DirectorTimeline } from "./director-timeline";
import { DirectorViewport, type DirectorViewportApi } from "./director-viewport";
import { useDirectorSync } from "./use-director-sync";

/** 键盘事件落在这些元素上时交给它们自己（打字、选下拉） */
const isTyping = (target: EventTarget | null) =>
  target instanceof HTMLElement &&
  !!target.closest("input:not([type=checkbox]):not([type=radio]), textarea, select");

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
  const scene = useDirectorStore((state) => state.scene);
  const playing = useDirectorStore((state) => state.playing);
  const selectedId = useDirectorStore((state) => state.selectedId);
  const tool = useDirectorStore((state) => state.tool);
  const snap = useDirectorStore((state) => state.snap);
  const labels = useDirectorStore((state) => state.labels);
  const canUndo = useDirectorStore((state) => state.past.length > 0);
  const canRedo = useDirectorStore((state) => state.future.length > 0);

  useEffect(() => {
    let alive = true;
    void canExportVideo().then((ok) => alive && setExportSupported(ok));
    rootRef.current?.focus({ preventScroll: true });
    return () => {
      alive = false;
    };
  }, []);

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

  const addObject = useCallback((kind: DirectorAddKind) => {
    const message = useDirectorStore.getState().addObject(kind);
    if (message) toast(message);
  }, []);
  const cameraFromView = useCallback(() => {
    const pose = viewport.current?.viewPose();
    if (!pose) return;
    const message = useDirectorStore.getState().addObject("camera", pose);
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
  const addBlocked = (kind: DirectorAddKind) => (canAdd(scene, kind) ? null : limitMessage(kind));

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
      className="dark bg-background text-foreground fixed inset-0 z-50 grid grid-rows-[48px_minmax(0,1fr)_minmax(168px,28vh)] outline-none"
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

      <div className="grid min-h-0 grid-cols-[52px_minmax(0,1fr)_minmax(240px,280px)]">
        <nav
          className="border-border flex flex-col items-center gap-1 border-r py-2"
          aria-label="工具"
        >
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
            title={addBlocked("character") ?? "添加人物"}
            disabled={!!addBlocked("character")}
            onClick={() => addObject("character")}
          >
            <UserRoundPlus />
          </ToolButton>
          <ToolButton
            title={addBlocked("prop") ?? "添加道具（几何体）"}
            disabled={!!addBlocked("prop")}
            onClick={() => addObject("prop")}
          >
            <Box />
          </ToolButton>
          <ToolButton
            title={addBlocked("camera") ?? "添加机位"}
            disabled={!!addBlocked("camera")}
            onClick={() => addObject("camera")}
          >
            <Video />
          </ToolButton>
          <ToolButton
            title={addBlocked("camera") ?? "从当前视角新建机位"}
            disabled={!!addBlocked("camera")}
            onClick={cameraFromView}
          >
            <Aperture />
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

        <main className="relative min-h-0 min-w-0 overflow-hidden">
          <DirectorViewport ref={viewport} />
          <span className="bg-card/80 text-muted-foreground border-border pointer-events-none absolute top-2.5 left-2.5 rounded-md border px-2 py-1 text-[11px]">
            导演视角 · 左键拖空白处旋转 · 右键平移 · 滚轮缩放 · 点选对象后拖动彩色轴
          </span>
          {empty && (
            <p className="text-muted-foreground pointer-events-none absolute inset-0 grid place-items-center text-sm">
              添加人物或机位开始摆位
            </p>
          )}
          <DirectorPreview />
        </main>

        <DirectorSidebar />
      </div>

      <DirectorTimeline />

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
