import { useEffect, useRef, type MutableRefObject } from "react";
import { ViewportPortal, useStore, type ReactFlowState } from "@xyflow/react";
import { Loader2 } from "lucide-react";
import { toast } from "sonner";

import { cn } from "@/lib/utils";
import { doodleCanvasSize, doodleDisplayRect, doodleStageStyle } from "@/utils/canvas/doodle";
import { doodleStageGuards } from "@/utils/canvas/doodle-guards";
import {
  createDoodleEngine,
  type DoodleEngine,
  type DoodleTool,
} from "@/utils/canvas/doodle-engine";

/** 加载 Fabric 失败时的提示 */
const LOAD_FAILED_TEXT = "涂鸦组件加载失败，请检查网络后重试";

/**
 * 涂鸦画布（设计稿 5.3）：在节点位置盖一块与图同大的 Fabric 画布，背景透明，图片就是节点本身。
 * 画布逻辑尺寸是原图像素，用 CSS 缩到节点大小；Fabric 只在进入涂鸦时才动态加载，不进主包。
 * 挂在 ViewportPortal 里，跟着画布平移缩放；加 nodrag nopan nowheel 并拦住指针事件，
 * 不让拖节点、平移、缩放、点空白取消选中这些画布手势穿透进来。
 * 卸载（保存、关闭、取消选中）时释放 Fabric 画布；React 严格模式下 effect 跑两次，
 * 用 disposed 标记让第一次尚未加载完的结果作废，不会建出两块画布。
 * @param nodeId 被涂鸦的图片节点 id，画布的位置和大小跟着它
 * @param natural 原图真实像素；量到之前不建画布
 * @param tool 当前工具
 * @param color 当前颜色
 * @param width 当前粗细（屏幕粗细）
 * @param ready 引擎建好了没有：没好之前画布区显示加载态
 * @param engineRef 引擎建好后放进来，给工具栏的撤销、重做和保存用
 * @param onChange 撤销栈或重做栈变了
 * @param onReady 引擎建好了（或销毁了）
 * @param onFail Fabric 加载失败：外面据此退出涂鸦
 */
export function DoodleStage({
  nodeId,
  natural,
  tool,
  color,
  width,
  ready,
  engineRef,
  onChange,
  onReady,
  onFail,
}: {
  nodeId: string;
  natural: { w: number; h: number } | null;
  tool: DoodleTool;
  color: string;
  width: number;
  ready: boolean;
  engineRef: MutableRefObject<DoodleEngine | null>;
  onChange: () => void;
  onReady: (ready: boolean) => void;
  onFail: () => void;
}) {
  const hostRef = useRef<HTMLDivElement>(null);

  // 节点左上角的画布坐标和大小，拼成字符串让选择器只在真的变了时才触发重渲染
  const geometry = useStore((state: ReactFlowState) => {
    const node = state.nodeLookup.get(nodeId);
    const size = node?.measured;
    if (!node || !size?.width || !size.height) return "";
    const { x, y } = node.internals.positionAbsolute;
    return `${x},${y},${size.width},${size.height}`;
  });
  const [x, y, boxW, boxH] = geometry ? geometry.split(",").map(Number) : [0, 0, 0, 0];
  const rect =
    natural && boxW > 0 ? doodleDisplayRect(natural, { width: boxW, height: boxH }) : null;
  const size = natural ? doodleCanvasSize(natural) : null;

  // 最新的工具、颜色、粗细、显示大小和回调：建引擎时取一次，之后由下面几个 effect 同步。
  // 放进 ref 是为了让“建引擎”的 effect 不因它们变化而重建画布（会丢笔迹）
  const latest = useRef({ tool, color, width, rect });
  const onChangeRef = useRef(onChange);
  const callbacks = useRef({ onReady, onFail });
  useEffect(() => {
    latest.current = { tool, color, width, rect };
    onChangeRef.current = onChange;
    callbacks.current = { onReady, onFail };
  });

  const logicalW = size?.width ?? 0;
  const logicalH = size?.height ?? 0;
  const hasRect = rect !== null;
  useEffect(() => {
    const host = hostRef.current;
    const first = latest.current.rect;
    if (!host || !logicalW || !logicalH || !first) return;
    let disposed = false;
    let engine: DoodleEngine | null = null;
    // 动态加载：Fabric 只在进入涂鸦时才下载，不进主包
    void import("fabric")
      .then((fabric) => {
        if (disposed) return;
        const current = latest.current;
        engine = createDoodleEngine({
          fabric,
          host,
          size: { width: logicalW, height: logicalH },
          display: current.rect ?? first,
          tool: current.tool,
          color: current.color,
          width: current.width,
          onChange: () => onChangeRef.current(),
        });
        engineRef.current = engine;
        callbacks.current.onReady(true);
        onChangeRef.current();
      })
      .catch((error: unknown) => {
        if (disposed) return;
        console.error("[doodle] 加载 Fabric 失败", error);
        toast.error(LOAD_FAILED_TEXT);
        callbacks.current.onFail();
      });
    return () => {
      disposed = true;
      if (engine && engineRef.current === engine) engineRef.current = null;
      engine?.dispose();
      callbacks.current.onReady(false);
    };
    // 只在原图逻辑尺寸变了、或第一次量到节点大小时重建
  }, [logicalW, logicalH, hasRect, engineRef]);

  useEffect(() => engineRef.current?.setTool(tool), [tool, engineRef]);
  useEffect(() => engineRef.current?.setColor(color), [color, engineRef]);
  useEffect(() => engineRef.current?.setWidth(width), [width, engineRef]);
  const displayW = rect?.width ?? 0;
  const displayH = rect?.height ?? 0;
  useEffect(() => {
    if (displayW > 0 && displayH > 0) {
      engineRef.current?.setDisplay({ width: displayW, height: displayH });
    }
  }, [displayW, displayH, engineRef]);

  if (!rect) return null;

  return (
    <ViewportPortal>
      <div
        className={cn(
          "nodrag nopan nowheel pointer-events-auto absolute touch-none select-none",
          "ring-foreground/40 ring-1",
        )}
        style={doodleStageStyle({ x, y }, rect)}
        {...doodleStageGuards}
      >
        <div ref={hostRef} className="absolute inset-0" />
        {!ready && (
          <div className="bg-chrome text-muted-foreground pointer-events-none absolute inset-0 grid place-items-center">
            <Loader2 className="size-5 animate-spin" aria-label="涂鸦画布加载中" />
          </div>
        )}
      </div>
    </ViewportPortal>
  );
}
