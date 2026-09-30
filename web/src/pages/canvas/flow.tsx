import { useCallback, useEffect, useMemo, useRef, useState } from "react";
import {
  Background,
  ControlButton,
  Controls,
  Panel,
  ReactFlow,
  SelectionMode,
  useEdgesState,
  useNodesState,
  useReactFlow,
  type NodeChange,
  type EdgeChange,
  type EdgeTypes,
  type NodeTypes,
} from "@xyflow/react";
import { Hand, MousePointer2, Settings, Upload, WifiOff, Zap } from "lucide-react";

import {
  AddNodeMenu,
  AnimatedSvgEdge,
  PendingConnectionLine,
  useCanvasTool,
  type AddNodeMenuItem,
} from "@/components/canvas";
import { SettingsDialog, type SettingModelGroup } from "@/components/setting";
import { Button } from "@/components/ui/button";
import {
  ANIMATED_EDGE_OPTIONS,
  BACKGROUND_VARIANTS,
  NODE_LIBRARY,
  REMOTE_KIND_OF_NODE,
  UPLOAD_ACCEPT,
  UPLOAD_ACTION,
  UPLOAD_NOTICE_CLASS,
  UPLOAD_NOTICE_DURATION,
} from "@/constants/canvas";
import { useCanvasMenu } from "@/hooks/use-canvas-menu";
import { useRemoteModels } from "@/hooks/use-models";
import { useTaskBackfill } from "@/hooks/use-task-backfill";
import { useCreditsStore } from "@/store/credits";
import { useWsStore } from "@/store/ws";
import { rememberCanvasTitle } from "@/utils/canvas/title-cache";
import { cn } from "@/lib/utils";
import { GRID_SIZE, useSettingsStore } from "@/store";
import type { NodeKind, UploadNotice } from "@/types";
import {
  getAllowedKinds,
  getModelOptions,
  pruneRemoteDefaults,
} from "@/utils/canvas/canvas";
import type { CanvasDetailDto } from "@/api/canvas/type";
import type { CanvasEdge, CanvasNode } from "@/types";
import {
  deserializeGraph,
  hasVolatileRunning,
  serializeGraph,
} from "@/utils/canvas/canvas-persistence";
import { useCanvasPersistence } from "@/hooks/use-canvas-persistence";
import { releaseObjectUrl } from "@/utils/canvas/media";

import { CanvasNodeView } from "./canvas-node";

/** 供 ReactFlow 使用的节点类型表，摆在模块顶层，重渲染时不会换新对象 */
const nodeTypes = { canvas: CanvasNodeView } satisfies NodeTypes;

/** 供 ReactFlow 使用的边类型表 */
const edgeTypes = { animatedSvgEdge: AnimatedSvgEdge } satisfies EdgeTypes;

/** 画布主体 */
export function Flow({ canvas, onConflict }: { canvas: CanvasDetailDto; onConflict: (canvas: CanvasDetailDto) => void }) {
  const initial = useMemo(() => deserializeGraph(canvas.graph), [canvas.graph]);
  const [nodes, setNodes, applyNodesChange] = useNodesState<CanvasNode>(initial.nodes);
  const [edges, setEdges, applyEdgesChange] = useEdgesState<CanvasEdge>(initial.edges);
  const { getViewport, setViewport } = useReactFlow<CanvasNode, CanvasEdge>();
  const hydratedRef = useRef(false);
  const nodesRef = useRef(nodes);
  const changeDelayRef = useRef<number | false | null>(null);
  const { status: saveStatus, changed } = useCanvasPersistence({
    canvasId: canvas.id,
    initialVersion: canvas.version,
    onConflict,
  });
  const scheduleSave = useCallback((delay = 800) => {
    if (!hydratedRef.current) return;
    changed(serializeGraph(nodes, edges, getViewport()), delay);
  }, [changed, edges, getViewport, nodes]);
  const onNodesChange = useCallback((changes: NodeChange<CanvasNode>[]) => {
    for (const change of changes) {
      if (change.type === "remove") releaseObjectUrl(nodesRef.current.find((node) => node.id === change.id)?.data.src);
    }
    const persistent = changes.filter((change) => change.type !== "select" && change.type !== "dimensions");
    changeDelayRef.current = persistent.length === 0
      ? false
      : persistent.every((change) => change.type === "position" && change.dragging)
        ? false
        : persistent.some((change) => change.type !== "position" || change.dragging === false) ? 0 : 800;
    applyNodesChange(changes);
  }, [applyNodesChange]);
  useEffect(() => { nodesRef.current = nodes; }, [nodes]);
  // 任务结果回填节点；打开画布时对账还在 running 的节点
  useTaskBackfill(nodes, setNodes);
  useEffect(() => { rememberCanvasTitle(canvas.id, canvas.title); }, [canvas.id, canvas.title]);
  const connection = useWsStore((state) => state.connection);
  const availableCredits = useCreditsStore((state) => state.credits?.available ?? null);
  useEffect(() => () => {
    for (const node of nodesRef.current) releaseObjectUrl(node.data.src);
  }, []);
  const onEdgesChange = useCallback((changes: EdgeChange<CanvasEdge>[]) => {
    changeDelayRef.current = changes.some((change) => change.type !== "select") ? 0 : false;
    applyEdgesChange(changes);
  }, [applyEdgesChange]);
  useEffect(() => {
    if (!hydratedRef.current) return;
    const delay = changeDelayRef.current;
    changeDelayRef.current = null;
    // 本地演示的生成中状态存不下来，等它收尾再存；带 taskId 的视频任务要立刻存，刷新后才能对账回填
    if (delay !== false && !(delay === null && hasVolatileRunning(nodes))) {
      scheduleSave(delay ?? 800);
    }
  }, [nodes, edges, scheduleSave]);
  const { activeTool, toggleTool } = useCanvasTool();
  // 画布把设置里的几项都用上了，整份订阅，省去逐个 selector
  const settings = useSettingsStore();
  const video = useRemoteModels(REMOTE_KIND_OF_NODE.video);
  const text = useRemoteModels(REMOTE_KIND_OF_NODE.script);
  const remoteModels = useMemo(
    () => ({
      video: { status: video.status, options: video.options },
      script: { status: text.status, options: text.options },
    }),
    [text.options, text.status, video.options, video.status],
  );
  /**
   * 视频、文本清单由服务端下发：设置里存的默认模型可能是旧演示清单里的 id，
   * 对不上就不往新节点上写
   */
  const defaultModels = useMemo(
    () => pruneRemoteDefaults(settings.defaultModels, remoteModels),
    [remoteModels, settings.defaultModels],
  );
  const [settingsOpen, setSettingsOpen] = useState(false);
  const {
    menu,
    pending,
    closeMenu,
    addNode,
    beginUpload,
    addUploadedNodes,
    onConnect,
    onConnectEnd,
    onDoubleClick,
  } = useCanvasMenu({
    setNodes,
    setEdges,
    defaultModels,
  });

  const isPanning = activeTool === "pan";
  const isWheelZoom = settings.wheelMode === "zoom";

  // 文件框常驻在画布里：菜单点完就关，藏在菜单里的 input 会跟着没
  const uploadInputRef = useRef<HTMLInputElement>(null);
  const [notice, setNotice] = useState<UploadNotice | null>(null);

  // 上传那句话摆一会儿就撤，不必让人再点一下关掉
  useEffect(() => {
    if (!notice) return;

    const timer = setTimeout(() => setNotice(null), UPLOAD_NOTICE_DURATION);
    return () => clearTimeout(timer);
  }, [notice]);

  // 设置里要列的模型清单：每种节点一行，内置清单后面接上自定义模型
  const modelGroups = useMemo<SettingModelGroup[]>(
    () =>
      NODE_LIBRARY.map((meta) => ({
        kind: meta.kind,
        label: meta.label,
        icon: <meta.icon />,
        models: getModelOptions(
          meta.kind,
          settings.customModels,
          remoteModels[meta.kind as keyof typeof remoteModels]?.options,
        ),
      })).filter((group) => group.models.length > 0),
    [remoteModels, settings.customModels],
  );

  // 拉线落空时只放行接得上的种类，双击空白则全部可点
  const menuItems = useMemo<AddNodeMenuItem[]>(() => {
    const allowed =
      pending && new Set(getAllowedKinds(pending.kind, pending.handleType));

    return [
      ...NODE_LIBRARY.map((meta) => ({
        value: meta.kind,
        label: meta.label,
        icon: <meta.icon />,
        disabled: allowed ? !allowed.has(meta.kind) : false,
      })),
      {
        value: UPLOAD_ACTION,
        label: "上传",
        icon: <Upload />,
        // 传进来的素材落成图片或视频节点，这两种都接不上就没法上传
        disabled: allowed
          ? !allowed.has("image") && !allowed.has("video")
          : false,
        separated: true,
      },
    ];
  }, [pending]);

  return (
    // data-tool 驱动 index.css 里的光标与命中规则
    <div className="h-svh w-svw" data-tool={activeTool}>
      <ReactFlow
        nodeTypes={nodeTypes}
        edgeTypes={edgeTypes}
        defaultEdgeOptions={ANIMATED_EDGE_OPTIONS}
        nodes={nodes}
        edges={edges}
        onNodesChange={onNodesChange}
        onEdgesChange={onEdgesChange}
        onInit={() => {
          void setViewport(initial.viewport);
          hydratedRef.current = true;
        }}
        onMoveEnd={() => scheduleSave(1000)}
        onConnect={onConnect}
        onConnectEnd={onConnectEnd}
        // 抓手模式下双击也只是拖画布的一部分，别在松手后冒出添加菜单
        onDoubleClick={isPanning ? undefined : onDoubleClick}
        zoomOnDoubleClick={false}
        // 默认滚轮只平移（shift+滚轮由 xyflow 内部转成左右平移），缩放交给 Ctrl/Cmd+滚轮；
        // 设置里切成缩放后，滚轮直接缩放，不再需要按键
        panOnScroll={!isWheelZoom}
        zoomOnScroll={isWheelZoom}
        zoomActivationKeyCode={isWheelZoom ? null : ["Control", "Meta"]}
        snapToGrid={settings.snapToGrid}
        snapGrid={[GRID_SIZE, GRID_SIZE]}
        // 箭头：左键框选，画布只让中键拖；抓手：左键即拖画布，节点不可拖
        panOnDrag={isPanning ? true : [1]}
        selectionOnDrag={!isPanning}
        // 框选相交即选中，不要求完整包住
        selectionMode={SelectionMode.Partial}
        // 抓手是纯粹的画布模式：节点不能拖、不能选，也拉不出连线
        nodesDraggable={!isPanning}
        nodesConnectable={!isPanning}
        elementsSelectable={!isPanning}
      >
        {settings.background !== "none" && (
          <Background
            variant={BACKGROUND_VARIANTS[settings.background]}
            gap={GRID_SIZE}
          />
        )}
        <Controls
          position="bottom-center"
          orientation="horizontal"
          className="bg-card overflow-hidden rounded-lg border shadow-sm"
        >
          <ControlButton
            onClick={toggleTool}
            title={isPanning ? "抓手：拖动画布" : "箭头：选中节点"}
            aria-label={isPanning ? "切换为箭头工具" : "切换为抓手工具"}
            aria-pressed={isPanning}
          >
            {isPanning ? <Hand /> : <MousePointer2 />}
          </ControlButton>
        </Controls>
        {/* 抓手模式下悬浮层整体不接指针事件，这块跟控制条一样走 index.css 里的例外 */}
        <Panel position="bottom-left" className="canvas-overlay-interactive">
          <Button
            variant="outline"
            size="icon"
            className="bg-card shadow-sm"
            title="画布设置"
            aria-label="打开画布设置"
            onClick={() => setSettingsOpen(true)}
          >
            <Settings />
          </Button>
        </Panel>
        {notice && (
          <Panel
            position="top-center"
            className={cn(
              "rounded-md border px-3 py-1.5 text-xs backdrop-blur",
              UPLOAD_NOTICE_CLASS[notice.tone],
            )}
            role="alert"
          >
            {notice.text}
          </Panel>
        )}
        <Panel position="top-right" className="flex items-center gap-2">
          {connection === "reconnecting" && (
            <span
              role="status"
              className="border-destructive/40 bg-destructive/10 text-destructive flex items-center gap-1.5 rounded-md border px-3 py-1.5 text-xs backdrop-blur"
            >
              <WifiOff className="size-3.5" />
              连接中断，正在重连
            </span>
          )}
          {availableCredits !== null && (
            <span
              className="bg-card/80 text-muted-foreground flex items-center gap-1 rounded-md border px-3 py-1.5 text-xs tabular-nums backdrop-blur"
              title="当前可用积分"
            >
              <Zap className="size-3.5" />
              {availableCredits}
            </span>
          )}
          <span className="bg-card/80 text-muted-foreground rounded-md border px-3 py-1.5 text-xs backdrop-blur">
            {saveStatus === "saving" ? "正在保存…" : saveStatus === "saved" ? "已保存" : saveStatus === "conflict" ? "已载入其他位置的修改" : "保存失败，继续编辑时重试"}
          </span>
        </Panel>
        {!notice && settings.showHints && nodes.length === 0 && (
          <Panel
            position="top-center"
            className="bg-card/80 text-muted-foreground rounded-md border px-3 py-1.5 text-xs backdrop-blur"
          >
            双击画布空白处添加节点，
            {isWheelZoom
              ? "滚轮缩放画布"
              : "滚轮上下移动，Shift+滚轮左右移动，Ctrl+滚轮缩放"}
          </Panel>
        )}
      </ReactFlow>

      {!isPanning && pending && menu && (
        <PendingConnectionLine
          from={pending.fromScreen}
          fromPosition={pending.fromPosition}
          to={menu.screen}
        />
      )}

      <input
        ref={uploadInputRef}
        type="file"
        accept={UPLOAD_ACCEPT}
        multiple
        className="sr-only"
        aria-hidden
        tabIndex={-1}
        onChange={(event) => {
          const files = Array.from(event.target.files ?? []);
          // 同一批文件连着选两次也得有反应，所以选完就把值清掉
          event.target.value = "";
          if (files.length) void addUploadedNodes(files).then(setNotice);
        }}
      />

      <SettingsDialog
        open={settingsOpen}
        onOpenChange={setSettingsOpen}
        modelGroups={modelGroups}
      />

      <AddNodeMenu
        position={isPanning ? null : (menu?.screen ?? null)}
        label={pending ? "引用该节点生成" : "添加节点"}
        items={menuItems}
        onClose={closeMenu}
        onSelect={(value) => {
          // 上传得先知道文件是什么才知道建哪种节点，落点先记下，节点等选完再建
          if (value === UPLOAD_ACTION) {
            beginUpload();
            uploadInputRef.current?.click();
            return;
          }

          addNode(value as NodeKind);
        }}
      />
    </div>
  );
}
