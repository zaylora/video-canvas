import { useCallback, useMemo, useState } from "react";
import { Dialog as DialogPrimitive } from "@base-ui/react/dialog";
import { useNodesData, useReactFlow } from "@xyflow/react";
import { motion } from "motion/react";
import {
  AudioLines,
  Ban,
  CircleX,
  Copy,
  Download,
  Ellipsis,
  ImagePlus,
  Image as ImageIcon,
  Loader2,
  Maximize2,
  X,
  ZoomIn,
} from "lucide-react";
import { toast } from "sonner";

import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuItem,
  DropdownMenuTrigger,
} from "@/components/ui/dropdown-menu";
import { useCanvasHistoryContext } from "@/hooks/use-canvas-history";
import { SPRING } from "@/lib/motion";
import { cn } from "@/lib/utils";
import { useNodeHistoryStore } from "@/store/node-history";
import { useTasksStore } from "@/store/tasks";
import type { CanvasEdge, CanvasNode, NodeOutput } from "@/types";
import { downloadMedia, downloadName } from "@/utils/canvas/download";
import { activeOutputIdOf, readOutputs, selectOutput } from "@/utils/canvas/outputs";
import type { PreviewItem } from "@/utils/canvas/preview-items";
import { buildVersionNodes, canSetMain, recentFailedTasks } from "@/utils/canvas/versions";
import { cancelTasks } from "@/utils/tasks/gateway";

import { MediaLightbox, type LightboxTarget } from "./media-lightbox";
import { MediaPreview, VideoPoster } from "./media-preview";

/** 一次「添加到画布」超过这个数先确认，免得一下子铺满画布 */
const ADD_CONFIRM_OVER = 8;

/** 悬停工具条 / 菜单按钮的统一外观 */
const TOOL_BUTTON =
  "grid size-6.5 place-items-center rounded-[7px] text-white outline-none hover:bg-white/15 focus-visible:ring-2 focus-visible:ring-white/70";

/** 格子里的图：图片缩略图、视频封面、音频图标 */
function TileMedia({ output }: { output: NodeOutput }) {
  if (output.mediaType === "image")
    return <MediaPreview src={output.src} mode="thumb" fit="contain" draggable={false} />;
  if (output.mediaType === "video") return <VideoPoster src={output.src} iconClassName="size-6" />;
  return (
    <span className="text-muted-foreground grid size-full place-items-center">
      <AudioLines className="size-8" />
    </span>
  );
}

type TileProps = {
  output: NodeOutput;
  index: number;
  current: boolean;
  /** 全屏层里：主图用角标，弹窗里用「当前图片」条 */
  fullscreen?: boolean;
  disabledReason: string | null;
  onPick: () => void;
  onZoom: () => void;
  onAdd: () => void;
};

/** 一版结果：单击设为主图，悬停出放大 / 复制 / 下载 / 添加到画布 */
function Tile({
  output,
  index,
  current,
  fullscreen,
  disabledReason,
  onPick,
  onZoom,
  onAdd,
}: TileProps) {
  const isImage = output.mediaType === "image";
  const stop = (fn: () => void) => (event: React.SyntheticEvent) => {
    event.stopPropagation();
    fn();
  };
  const copyImage = async () => {
    try {
      const blob = await (await fetch(output.src)).blob();
      await navigator.clipboard.write([new ClipboardItem({ [blob.type]: blob })]);
      toast.success("已复制图片");
    } catch {
      toast.error("复制失败，请改用下载");
    }
  };
  return (
    <div
      role="button"
      tabIndex={0}
      aria-pressed={current}
      aria-label={`第 ${index + 1} 版${current ? "（当前主图）" : ""}`}
      title={disabledReason ?? (current ? "当前主图" : "设为主图")}
      data-history-tile
      onClick={onPick}
      onDoubleClick={fullscreen ? onZoom : undefined}
      onKeyDown={(event) => {
        if (event.key === "Enter" || event.key === " ") {
          event.preventDefault();
          onPick();
        }
      }}
      className={cn(
        "group/tile bg-muted/40 focus-visible:ring-foreground relative aspect-square overflow-hidden rounded-xl outline-none ring-1 ring-white/12 transition-shadow hover:ring-white/35 focus-visible:ring-2",
        disabledReason ? "cursor-not-allowed" : "cursor-pointer",
        current && "ring-2 ring-white hover:ring-white",
      )}
    >
      <TileMedia output={output} />
      {current &&
        (fullscreen ? (
          <span className="absolute top-2 left-2 z-10 rounded-md bg-white/90 px-2 py-0.5 text-xs font-semibold text-black">
            主图
          </span>
        ) : (
          <div className="pointer-events-none absolute inset-x-0 top-0 z-10 flex items-center gap-1.5 bg-linear-to-b from-black/55 to-transparent px-2.5 pt-2 pb-4 text-xs text-white">
            <ImageIcon className="size-3.5" />
            当前图片
          </div>
        ))}
      <div className="absolute right-1.5 bottom-1.5 z-10 flex gap-0.5 rounded-[10px] bg-neutral-800/85 p-0.5 opacity-0 transition-opacity group-focus-within/tile:opacity-100 group-hover/tile:opacity-100 pointer-coarse:opacity-100">
        <button
          type="button"
          aria-label="放大"
          title="放大"
          className={TOOL_BUTTON}
          onClick={stop(onZoom)}
        >
          <ZoomIn className="size-3.5" />
        </button>
        {isImage && (
          <button
            type="button"
            aria-label="复制图片"
            title="复制图片"
            className={TOOL_BUTTON}
            onClick={stop(() => void copyImage())}
          >
            <Copy className="size-3.5" />
          </button>
        )}
        <button
          type="button"
          aria-label="下载"
          title="下载"
          className={TOOL_BUTTON}
          onClick={stop(
            () => void downloadMedia(output.src, downloadName(`版本${index + 1}`, output.src)),
          )}
        >
          <Download className="size-3.5" />
        </button>
        <button
          type="button"
          aria-label="添加到画布"
          title="添加到画布"
          className={TOOL_BUTTON}
          onClick={stop(onAdd)}
        >
          <ImagePlus className="size-3.5" />
        </button>
      </div>
    </div>
  );
}

/** 生成中的占位格 */
function PendingTile() {
  return (
    <div
      aria-label="生成中"
      className="bg-muted/30 relative grid aspect-square place-items-center overflow-hidden rounded-xl ring-1 ring-white/10"
    >
      <div className="text-muted-foreground flex flex-col items-center gap-1.5 text-xs">
        <Loader2 className="size-5 animate-spin" />
        生成中
      </div>
    </div>
  );
}

/** 生成失败的格：原因 + 积分已退回，不可点；失败任务不进历史，关掉弹窗再开、或下一批开始后就不显示了 */
function FailedTile({ message }: { message: string }) {
  return (
    <div
      aria-label={`生成失败：${message}`}
      className="bg-destructive/8 ring-destructive/40 grid aspect-square place-items-center overflow-hidden rounded-xl p-3 ring-1"
    >
      <div className="flex flex-col items-center gap-1 text-center text-xs">
        <CircleX className="text-destructive size-5" />
        <b className="font-medium">生成失败</b>
        <span className="text-muted-foreground line-clamp-3">{message}</span>
        <span className="text-muted-foreground">积分已退回</span>
      </div>
    </div>
  );
}

/** 方向键在网格里移动焦点 */
function moveFocus(event: React.KeyboardEvent<HTMLElement>) {
  const delta = { ArrowRight: 1, ArrowLeft: -1 }[event.key];
  const tiles = [...event.currentTarget.querySelectorAll<HTMLElement>("[data-history-tile]")];
  const at = tiles.indexOf(document.activeElement as HTMLElement);
  if (at < 0) return;
  let next = at + (delta ?? 0);
  if (event.key === "ArrowDown" || event.key === "ArrowUp") {
    const top = tiles[0].offsetTop;
    const cols = tiles.filter((tile) => tile.offsetTop === top).length || 1;
    next = at + (event.key === "ArrowDown" ? cols : -cols);
  } else if (!delta) return;
  if (tiles[next]) {
    event.preventDefault();
    tiles[next].focus();
  }
}

/**
 * 「历史记录」弹窗（docs/design/多结果归并 §6.3–6.6）：节点右上角徽标打开，网格看这个节点的全部版本。
 * 单击一张就是设为主图（下游读的就是它，⌘Z 可撤销）；放大只看不改；全屏查看是黑底网格；
 * 下载全部、全部添加到画布（每一版拆成一个独立节点）。
 * 挂在画布层，用 store 里的 nodeId 找节点，节点被删时自动关闭。
 */
export function NodeHistoryDialog() {
  const nodeId = useNodeHistoryStore((state) => state.nodeId);
  const close = useNodeHistoryStore((state) => state.close);
  const node = useNodesData<CanvasNode>(nodeId ?? "");
  const { updateNodeData, getNode, getNodes, setNodes } = useReactFlow<CanvasNode, CanvasEdge>();
  const history = useCanvasHistoryContext();

  const [fullscreen, setFullscreen] = useState(false);
  const [zoom, setZoom] = useState<LightboxTarget | null>(null);
  const [confirm, setConfirm] = useState<NodeOutput[] | null>(null);

  const data = nodeId && node ? node.data : null;
  const outputs = useMemo(() => (data ? readOutputs(data) : []), [data]);
  // 最新在前
  const ordered = useMemo(() => [...outputs].reverse(), [outputs]);
  const activeId = data ? activeOutputIdOf(data) : undefined;
  const pending = data
    ? data.pendingTaskIds?.length || (data.status === "running" && outputs.length > 0 ? 1 : 0)
    : 0;
  const open = !!nodeId && !!data;
  const tasks = useTasksStore((state) => state.tasks);
  const failed = useMemo(() => (nodeId ? recentFailedTasks(tasks, nodeId) : []), [tasks, nodeId]);

  const handleClose = useCallback(() => {
    setFullscreen(false);
    setZoom(null);
    setConfirm(null);
    close();
  }, [close]);

  const setMain = useCallback(
    (outputId: string) => {
      if (!nodeId || !data) return;
      const verdict = canSetMain(data, outputId);
      if (!verdict.ok) {
        if (verdict.reason) toast(verdict.reason);
        return;
      }
      history?.record();
      updateNodeData(nodeId, (current) => {
        const patch = selectOutput(current.data, outputId);
        return patch ? { ...patch, status: "done", error: null } : {};
      });
    },
    [data, history, nodeId, updateNodeData],
  );

  const addToCanvas = useCallback(
    (versions: NodeOutput[], confirmed = false) => {
      const source = nodeId ? getNode(nodeId) : undefined;
      if (!source || versions.length === 0) return;
      if (versions.length > ADD_CONFIRM_OVER && !confirmed) {
        setConfirm(versions);
        return;
      }
      history?.record();
      const created = buildVersionNodes(source, getNodes(), versions);
      setNodes((nodes) => [...nodes.map((item) => ({ ...item, selected: false })), ...created]);
      toast.success(`已添加 ${created.length} 个节点到画布`);
    },
    [getNode, getNodes, history, nodeId, setNodes],
  );

  const downloadAll = () => {
    const label = data?.label ?? "生成结果";
    outputs.forEach((output, index) => {
      void downloadMedia(output.src, downloadName(`${label}-${index + 1}`, output.src));
    });
  };

  const cancelRest = () => {
    const ids = data?.pendingTaskIds?.length
      ? data.pendingTaskIds
      : data?.taskId
        ? [data.taskId]
        : [];
    if (ids.length) void cancelTasks(ids);
  };

  const zoomItems = useMemo<PreviewItem[]>(
    () =>
      ordered.flatMap((output) =>
        output.mediaType === "audio"
          ? []
          : [
              {
                id: output.id,
                src: output.src,
                mediaType: output.mediaType,
                label: `${data?.label ?? "版本"} · 版本${outputs.indexOf(output) + 1}`,
              },
            ],
      ),
    [data?.label, ordered, outputs],
  );

  const disabledReason = data?.status === "running" ? "生成中，出第一张后可切换" : null;

  const grid = (full: boolean) => (
    <div
      onKeyDown={moveFocus}
      className={cn(
        "grid content-start gap-2.5",
        full
          ? "grid-cols-[repeat(auto-fill,minmax(150px,1fr))] overflow-auto px-5 pb-6"
          : "max-h-[min(520px,70svh)] grid-cols-[repeat(auto-fill,minmax(150px,1fr))] overflow-auto px-5.5 pb-5.5",
      )}
    >
      {Array.from({ length: pending }, (_, i) => (
        <PendingTile key={`pending-${i}`} />
      ))}
      {!full &&
        failed.map((item) => <FailedTile key={`failed-${item.id}`} message={item.message} />)}
      {ordered.map((output) => (
        <Tile
          key={output.id}
          output={output}
          index={outputs.indexOf(output)}
          current={output.id === activeId && data?.status === "done"}
          fullscreen={full}
          disabledReason={disabledReason}
          onPick={() => setMain(output.id)}
          onZoom={() => setZoom({ id: output.id, origin: null })}
          onAdd={() => addToCanvas([output])}
        />
      ))}
    </div>
  );

  return (
    <>
      <DialogPrimitive.Root open={open} onOpenChange={(next) => !next && handleClose()}>
        <DialogPrimitive.Portal>
          <DialogPrimitive.Backdrop className="bg-cover-scrim fixed inset-0 z-50 backdrop-blur-sm transition-opacity duration-200 data-ending-style:opacity-0 data-starting-style:opacity-0" />
          <DialogPrimitive.Popup
            aria-label="历史记录"
            data-slot="node-history-dialog"
            className="bg-card text-foreground fixed top-1/2 left-1/2 z-50 flex max-h-[92svh] w-[min(720px,92vw)] -translate-x-1/2 -translate-y-1/2 flex-col rounded-3xl shadow-2xl outline-none transition-[opacity,scale] duration-[240ms] ease-[cubic-bezier(0.2,0,0,1)] data-ending-style:scale-[0.96] data-ending-style:opacity-0 data-starting-style:scale-[0.96] data-starting-style:opacity-0"
          >
            <div className="flex items-center gap-1 px-5.5 pt-4.5 pb-3">
              <DialogPrimitive.Title className="text-[15px] font-semibold">
                历史记录
                <span className="text-muted-foreground ml-1.5 font-normal tabular-nums">
                  {outputs.length}
                </span>
              </DialogPrimitive.Title>
              <span className="flex-1" />
              <button
                type="button"
                onClick={() => setFullscreen(true)}
                className="hover:bg-accent focus-visible:ring-node-ring flex h-8 items-center gap-1.5 rounded-lg px-2.5 text-sm outline-none focus-visible:ring-2"
              >
                <Maximize2 className="size-4" />
                全屏查看
              </button>
              <DropdownMenu>
                <DropdownMenuTrigger
                  aria-label="更多"
                  className="hover:bg-accent focus-visible:ring-node-ring grid size-8 place-items-center rounded-lg outline-none focus-visible:ring-2"
                >
                  <Ellipsis className="size-4" />
                </DropdownMenuTrigger>
                <DropdownMenuContent align="end" className="min-w-44">
                  <DropdownMenuItem onClick={downloadAll}>
                    <Download /> 下载全部
                  </DropdownMenuItem>
                  <DropdownMenuItem onClick={() => addToCanvas([...outputs])}>
                    <ImagePlus /> 全部添加到画布
                  </DropdownMenuItem>
                  <DropdownMenuItem disabled={pending === 0} onClick={cancelRest}>
                    <Ban /> 取消剩余{pending > 0 ? `（${pending} 个）` : ""}
                  </DropdownMenuItem>
                </DropdownMenuContent>
              </DropdownMenu>
              <span className="bg-border mx-1 h-4.5 w-px" />
              <DialogPrimitive.Close
                aria-label="关闭"
                className="hover:bg-accent focus-visible:ring-node-ring grid size-8 place-items-center rounded-lg outline-none focus-visible:ring-2"
              >
                <X className="size-4" />
              </DialogPrimitive.Close>
            </div>
            {grid(false)}
          </DialogPrimitive.Popup>
        </DialogPrimitive.Portal>
      </DialogPrimitive.Root>

      <DialogPrimitive.Root
        open={open && fullscreen}
        onOpenChange={(next) => !next && setFullscreen(false)}
      >
        <DialogPrimitive.Portal>
          <DialogPrimitive.Popup
            aria-label="全屏查看"
            className="fixed inset-0 z-50 flex flex-col bg-black text-white outline-none transition-opacity duration-[240ms] data-ending-style:opacity-0 data-starting-style:opacity-0"
          >
            <DialogPrimitive.Title className="sr-only">全屏查看</DialogPrimitive.Title>
            <div className="flex flex-wrap items-center gap-2.5 p-4">
              <button
                type="button"
                onClick={downloadAll}
                className="flex h-9.5 items-center gap-2 rounded-xl bg-neutral-900 px-3.5 text-sm ring-1 ring-white/12 outline-none hover:bg-neutral-800 focus-visible:ring-2"
              >
                <Download className="size-4" />
                下载全部
              </button>
              <button
                type="button"
                onClick={() => addToCanvas([...outputs])}
                className="flex h-9.5 items-center gap-2 rounded-xl bg-neutral-900 px-3.5 text-sm ring-1 ring-white/12 outline-none hover:bg-neutral-800 focus-visible:ring-2"
              >
                <ImagePlus className="size-4" />
                全部添加到画布
              </button>
              <span className="flex-1" />
              <DialogPrimitive.Close
                aria-label="关闭"
                className="grid size-9.5 place-items-center rounded-xl bg-neutral-900 ring-1 ring-white/12 outline-none hover:bg-neutral-800 focus-visible:ring-2"
              >
                <X className="size-5" />
              </DialogPrimitive.Close>
            </div>
            {grid(true)}
          </DialogPrimitive.Popup>
        </DialogPrimitive.Portal>
      </DialogPrimitive.Root>

      <MediaLightbox
        items={zoomItems}
        target={open ? zoom : null}
        onActiveChange={(id) => setZoom({ id, origin: null })}
        countLabel="历史版本"
        onClose={() => setZoom(null)}
        renderAction={(item) =>
          item.id === activeId && data?.status === "done" ? (
            <span className="bg-chrome ring-chrome-border flex h-7 items-center rounded-full px-3 ring-1">
              主图
            </span>
          ) : (
            <motion.button
              type="button"
              whileTap={{ scale: 0.96 }}
              transition={SPRING}
              disabled={data?.status === "running"}
              onClick={() => setMain(item.id)}
              className="bg-chrome ring-chrome-border text-foreground hover:bg-chrome-hover flex h-7 items-center gap-1.5 rounded-full px-3 ring-1 outline-none disabled:opacity-40"
            >
              <ImageIcon className="size-3.5" />
              设为主图
            </motion.button>
          )
        }
      />

      <DialogPrimitive.Root
        open={confirm !== null}
        onOpenChange={(next) => !next && setConfirm(null)}
      >
        <DialogPrimitive.Portal>
          <DialogPrimitive.Popup className="bg-card text-foreground fixed top-1/2 left-1/2 z-[60] w-[min(360px,90vw)] -translate-x-1/2 -translate-y-1/2 rounded-2xl p-5 shadow-2xl outline-none">
            <DialogPrimitive.Title className="mb-4 text-sm">
              将在画布上新建 {confirm?.length ?? 0} 个节点，确定吗？
            </DialogPrimitive.Title>
            <div className="flex justify-end gap-2">
              <button
                type="button"
                onClick={() => setConfirm(null)}
                className="hover:bg-accent h-8 rounded-lg px-3 text-sm ring-1 ring-border"
              >
                取消
              </button>
              <button
                type="button"
                onClick={() => {
                  const list = confirm ?? [];
                  setConfirm(null);
                  addToCanvas(list, true);
                }}
                className="bg-primary text-primary-foreground h-8 rounded-lg px-3 text-sm"
              >
                确定
              </button>
            </div>
          </DialogPrimitive.Popup>
        </DialogPrimitive.Portal>
      </DialogPrimitive.Root>
    </>
  );
}
