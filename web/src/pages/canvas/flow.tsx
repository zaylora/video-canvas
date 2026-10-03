import { memo, useCallback, useEffect, useMemo, useRef, useState } from "react";
import {
  Background,
  MiniMap,
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
import { useNavigate } from "react-router";
import { toast } from "sonner";

import {
  AddNodeMenu,
  AnimatedSvgEdge,
  PendingConnectionLine,
  useCanvasTool,
  type AddNodeMenuItem,
} from "@/components/canvas";
import { ChromeZone } from "@/components/canvas/chrome/chrome";
import { NODE_OUTPUT_MIME } from "@/components/canvas/node-history-strip";
import { SettingsDialog, type SettingModelGroup } from "@/components/setting";
import { TooltipProvider } from "@/components/ui/tooltip";
import {
  ANIMATED_EDGE_OPTIONS,
  BACKGROUND_VARIANTS,
  NODE_LIBRARY,
  REMOTE_KIND_OF_NODE,
  UPLOAD_ACCEPT,
  UPLOAD_ACTION,
} from "@/constants/canvas";
import { CanvasHistoryProvider, useCanvasHistory } from "@/hooks/use-canvas-history";
import { useCanvasMenu } from "@/hooks/use-canvas-menu";
import { useRemoteModels } from "@/hooks/use-models";
import { useTaskBackfill } from "@/hooks/use-task-backfill";
import { rememberCanvasTitle } from "@/utils/canvas/title-cache";
import { GRID_SIZE, useSettingsStore } from "@/store";
import type { CanvasEdge, CanvasNode, NodeKind, NodeOutput, UploadNotice } from "@/types";
import { getModelOptions, pruneRemoteDefaults } from "@/utils/canvas/canvas";
import { createCanvas } from "@/api/canvas";
import type { CanvasDetailDto } from "@/api/canvas/type";
import { canLinkFrom, canLinkNodes } from "@/utils/canvas/link-rule";
import { MEDIA_KIND_OF } from "@/utils/canvas/outputs";
import {
  deserializeGraph,
  hasVolatileRunning,
  serializeGraph,
} from "@/utils/canvas/canvas-persistence";
import { useCanvasPersistence } from "@/hooks/use-canvas-persistence";
import { releaseObjectUrl } from "@/utils/canvas/media";

import { BottomToolbar } from "./chrome/bottom-toolbar";
import { EmptyState } from "./chrome/empty-state";
import { ShortcutsDialog } from "./chrome/shortcuts-dialog";
import { StatsBar } from "./chrome/stats-bar";
import { TopLeftBar } from "./chrome/top-left-bar";
import { TopRightBar } from "./chrome/top-right-bar";
import { ViewControls } from "./chrome/view-controls";
import { CanvasNodeView } from "./canvas-node";
import { ConflictDialog } from "./conflict-dialog";
import { buildAddNodeItems } from "./chrome/add-node-items";
import { OverlayGateProvider, useOverlayGate } from "./overlay-gate";
import { MultiSelectProvider, SelectionToolbar } from "./selection-toolbar";
import { useCanvasShortcuts } from "./use-canvas-shortcuts";

/** 画布最小缩放 */
const MIN_ZOOM = 0.14;

/** 新节点的估算尺寸：从视口中心落节点时，让节点正中对准视口中心 */
const NEW_NODE_SIZE = { width: 384, height: 216 };

/** 上传完那句话按语气挑 toast */
function showUploadNotice(notice: UploadNotice | null) {
  if (!notice) return;
  if (notice.tone === "error") toast.error(notice.text);
  else toast.info(notice.text);
}

/** 节点上挂着的任务号 */
const taskIdsOf = (nodes: CanvasNode[]) =>
  nodes.flatMap((node) => (node.data.taskId ? [node.data.taskId] : []));

/** 供 ReactFlow 使用的节点类型表，摆在模块顶层，重渲染时不会换新对象 */
const nodeTypes = { canvas: CanvasNodeView } satisfies NodeTypes;

/** 供 ReactFlow 使用的边类型表 */
const edgeTypes = { animatedSvgEdge: AnimatedSvgEdge } satisfies EdgeTypes;

/**
 * 画布主体。加载层退场时外面会在根节点（data-canvas-root）挂 data-entering 播入场，样式见 index.css；
 * 用 memo 包住，免得页面上的加载状态变化把整张画布重渲染一遍。
 */
export const Flow = memo(function Flow({
  canvas,
  onConflict,
}: {
  canvas: CanvasDetailDto;
  onConflict: (canvas: CanvasDetailDto) => void;
}) {
  const initial = useMemo(() => deserializeGraph(canvas.graph), [canvas.graph]);
  const [nodes, setNodes, applyNodesChange] = useNodesState<CanvasNode>(initial.nodes);
  const [edges, setEdges, applyEdgesChange] = useEdgesState<CanvasEdge>(initial.edges);
  const { getViewport, setViewport, screenToFlowPosition, getNode, getNodes } = useReactFlow<
    CanvasNode,
    CanvasEdge
  >();
  const navigate = useNavigate();
  const hydratedRef = useRef(false);
  const nodesRef = useRef(nodes);
  const edgesRef = useRef(edges);
  /** 这一轮节点/连线变化要不要存：false 不存（选中、尺寸、拖动过程中），true 要存，null 是不经过 onNodesChange 的数据修改 */
  const changeSaveRef = useRef<boolean | null>(null);
  /** 已经见过的任务号：出现新的就要立刻存，刷新后才能对账回填 */
  const knownTaskIdsRef = useRef(new Set(taskIdsOf(initial.nodes)));
  // 保存发请求的那一刻才取图谱，视口也在这时读，所以平移缩放本身不用触发保存
  const getGraph = useCallback(
    () =>
      hydratedRef.current
        ? serializeGraph(nodesRef.current, edgesRef.current, getViewport())
        : null,
    [getViewport],
  );
  const {
    status: saveStatus,
    conflict,
    changed,
    flush,
    rename,
    dismissConflict,
  } = useCanvasPersistence({
    canvasId: canvas.id,
    initialVersion: canvas.version,
    getGraph,
    onConflict,
  });
  const overlayGate = useOverlayGate(getNodes);
  const { pruneOnChange } = overlayGate;
  const onNodesChange = useCallback(
    (changes: NodeChange<CanvasNode>[]) => {
      pruneOnChange(changes);
      for (const change of changes) {
        if (change.type === "remove")
          releaseObjectUrl(nodesRef.current.find((node) => node.id === change.id)?.data.src);
      }
      const persistent = changes.filter(
        (change) => change.type !== "select" && change.type !== "dimensions",
      );
      // 只有选中、尺寸变化，或者还在拖动的过程中，都不算内容改动
      changeSaveRef.current =
        persistent.length > 0 &&
        !persistent.every((change) => change.type === "position" && change.dragging);
      applyNodesChange(changes);
    },
    [applyNodesChange, pruneOnChange],
  );
  useEffect(() => {
    nodesRef.current = nodes;
    edgesRef.current = edges;
  }, [nodes, edges]);
  // 任务结果回填节点；打开画布时对账还在 running 的节点
  useTaskBackfill(nodes, setNodes);
  useEffect(() => {
    rememberCanvasTitle(canvas.id, canvas.title);
  }, [canvas.id, canvas.title]);
  const [title, setTitle] = useState(canvas.title);
  const onRename = useCallback(
    (next: string) => {
      const previous = title;
      setTitle(next);
      void rename(next).then((ok) => {
        if (ok) rememberCanvasTitle(canvas.id, next);
        else {
          setTitle(previous);
          toast.error("改名没保存上，请稍后再试");
        }
      });
    },
    [canvas.id, rename, title],
  );
  useEffect(
    () => () => {
      for (const node of nodesRef.current) releaseObjectUrl(node.data.src);
    },
    [],
  );
  const onEdgesChange = useCallback(
    (changes: EdgeChange<CanvasEdge>[]) => {
      changeSaveRef.current = changes.some((change) => change.type !== "select");
      applyEdgesChange(changes);
    },
    [applyEdgesChange],
  );
  useEffect(() => {
    if (!hydratedRef.current) return;
    const save = changeSaveRef.current;
    changeSaveRef.current = null;
    // 本地演示的生成中状态存不下来，等它收尾再存
    if (save === false || (save === null && hasVolatileRunning(nodes))) return;
    changed();
    // 带 taskId 的任务提交后要立刻存：等停手再存的话，这几秒内刷新就对不上账了
    const taskIds = taskIdsOf(nodes);
    const fresh = taskIds.some((taskId) => !knownTaskIdsRef.current.has(taskId));
    knownTaskIdsRef.current = new Set(taskIds);
    if (fresh) void flush();
  }, [nodes, edges, changed, flush]);
  const { tool, activeTool, setTool } = useCanvasTool();
  const history = useCanvasHistory({ nodes, edges, setNodes, setEdges });

  const multiSelected = useMemo(
    () => nodes.reduce((count, node) => count + (node.selected ? 1 : 0), 0) > 1,
    [nodes],
  );
  const [shortcutsOpen, setShortcutsOpen] = useState(false);
  const [minimap, setMinimap] = useState(false);
  const openShortcuts = useCallback(() => setShortcutsOpen(true), []);
  const saveNow = useCallback(() => void flush(), [flush]);
  useCanvasShortcuts({
    undo: history.undo,
    redo: history.redo,
    setTool,
    openShortcuts,
    save: saveNow,
  });
  // 画布把设置里的几项都用上了，整份订阅，省去逐个 selector
  const settings = useSettingsStore();
  const video = useRemoteModels(REMOTE_KIND_OF_NODE.video);
  const text = useRemoteModels(REMOTE_KIND_OF_NODE.script);
  const image = useRemoteModels(REMOTE_KIND_OF_NODE.image);
  const audio = useRemoteModels(REMOTE_KIND_OF_NODE.audio);
  const remoteModels = useMemo(
    () => ({
      video: { status: video.status, options: video.options },
      script: { status: text.status, options: text.options },
      image: { status: image.status, options: image.options },
      audio: { status: audio.status, options: audio.options },
    }),
    [
      audio.options,
      audio.status,
      image.options,
      image.status,
      text.options,
      text.status,
      video.options,
      video.status,
    ],
  );
  /**
   * 四种节点的清单都由服务端下发：设置里存的默认模型可能是旧演示清单里的 id，
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
    addNodeAt,
    beginUpload,
    beginUploadAt,
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

  /** 视口中心对应的画布坐标，换算成新节点左上角 */
  const viewportCenter = useCallback(() => {
    const center = screenToFlowPosition({ x: window.innerWidth / 2, y: window.innerHeight * 0.4 });
    return { x: center.x - NEW_NODE_SIZE.width / 2, y: center.y - NEW_NODE_SIZE.height / 2 };
  }, [screenToFlowPosition]);
  const addAtCenter = useCallback(
    (kind: NodeKind) => addNodeAt(kind, viewportCenter()),
    [addNodeAt, viewportCenter],
  );
  const uploadAtCenter = useCallback(() => {
    beginUploadAt(viewportCenter());
    uploadInputRef.current?.click();
  }, [beginUploadAt, viewportCenter]);

  /** 历史浮条里的缩略图拖到画布上：以那一版为素材建一个新节点 */
  const onDragOver = useCallback((event: React.DragEvent) => {
    if (!event.dataTransfer.types.includes(NODE_OUTPUT_MIME)) return;
    event.preventDefault();
    event.dataTransfer.dropEffect = "copy";
  }, []);
  const onDrop = useCallback(
    (event: React.DragEvent) => {
      const raw = event.dataTransfer.getData(NODE_OUTPUT_MIME);
      if (!raw) return;
      event.preventDefault();
      try {
        const output = JSON.parse(raw) as NodeOutput;
        const flow = screenToFlowPosition({ x: event.clientX, y: event.clientY });
        addNodeAt(
          MEDIA_KIND_OF[output.mediaType],
          { x: flow.x - NEW_NODE_SIZE.width / 2, y: flow.y - NEW_NODE_SIZE.height / 2 },
          {
            status: "done",
            src: output.src,
            assetId: output.assetId,
            mediaType: output.mediaType,
            outputs: [output],
            activeOutputId: output.id,
          },
        );
      } catch {
        // 拖进来的不是我们塞的数据，不理它
      }
    },
    [addNodeAt, screenToFlowPosition],
  );

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
    const from = pending && getNode(pending.nodeId);
    return buildAddNodeItems((kind) =>
      from
        ? !canLinkFrom(from.data, pending.handleType, { kind, model: defaultModels?.[kind] })
        : false,
    );
  }, [defaultModels, getNode, pending]);

  /** 拖线接到连接点上时的放行规则：种类规则 + 下游当前模型收不收 */
  const isValidConnection = useCallback(
    (connection: { source: string; target: string }) => {
      const source = getNode(connection.source);
      const target = getNode(connection.target);
      return (
        !!source && !!target && source.id !== target.id && canLinkNodes(source.data, target.data)
      );
    },
    [getNode],
  );

  return (
    // data-tool 驱动 index.css 里的光标与命中规则
    <CanvasHistoryProvider value={history}>
      <MultiSelectProvider value={multiSelected}>
        <OverlayGateProvider value={overlayGate.dragSelected}>
          <TooltipProvider delay={400}>
            <div
              className="bg-canvas relative h-svh w-svw overflow-hidden"
              data-tool={activeTool}
              data-canvas-root
              onDragOver={onDragOver}
              onPointerDownCapture={overlayGate.onPointerDownCapture}
              onDrop={onDrop}
            >
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
                onNodeDragStart={overlayGate.onNodeDragStart}
                onNodeClick={overlayGate.onNodeClick}
                onConnect={onConnect}
                isValidConnection={isValidConnection}
                onConnectEnd={onConnectEnd}
                // 抓手模式下双击也只是拖画布的一部分，别在松手后冒出添加菜单
                onDoubleClick={isPanning ? undefined : onDoubleClick}
                zoomOnDoubleClick={false}
                // 大画布要能一眼看全，最小缩到 14%
                minZoom={MIN_ZOOM}
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
                  <Background variant={BACKGROUND_VARIANTS[settings.background]} gap={GRID_SIZE} />
                )}
                <SelectionToolbar />
                {minimap && (
                  <MiniMap
                    position="bottom-left"
                    pannable
                    zoomable
                    className="canvas-overlay-interactive !bottom-16 !left-1 overflow-hidden rounded-xl shadow-lg ring-1 ring-chrome-border"
                    nodeColor="var(--muted-foreground)"
                    nodeBorderRadius={12}
                  />
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
                  if (files.length) void addUploadedNodes(files).then(showUploadNotice);
                }}
              />

              {nodes.length === 0 && <EmptyState onAdd={addAtCenter} onUpload={uploadAtCenter} />}

              <ChromeZone position="top-left">
                <TopLeftBar
                  title={title}
                  onRename={onRename}
                  saveStatus={saveStatus}
                  onSaveNow={flush}
                />
              </ChromeZone>
              <ChromeZone position="top-right">
                <TopRightBar
                  onOpenSettings={() => setSettingsOpen(true)}
                  onOpenShortcuts={openShortcuts}
                />
              </ChromeZone>
              <ChromeZone position="bottom-center">
                <BottomToolbar
                  tool={tool}
                  onToolChange={setTool}
                  onAdd={addAtCenter}
                  onUpload={uploadAtCenter}
                  canUndo={history.canUndo}
                  canRedo={history.canRedo}
                  onUndo={history.undo}
                  onRedo={history.redo}
                />
              </ChromeZone>
              <ChromeZone position="bottom-left" className="max-md:hidden">
                <ViewControls
                  minimap={minimap}
                  onMinimapChange={setMinimap}
                  onOpenShortcuts={openShortcuts}
                />
              </ChromeZone>
              <ChromeZone position="bottom-right" className="max-md:hidden">
                <StatsBar />
              </ChromeZone>

              <SettingsDialog
                open={settingsOpen}
                onOpenChange={setSettingsOpen}
                modelGroups={modelGroups}
              />
              <ShortcutsDialog open={shortcutsOpen} onOpenChange={setShortcutsOpen} />
              <ConflictDialog
                open={conflict !== null}
                onLoadLatest={() => {
                  if (!conflict) return;
                  dismissConflict();
                  onConflict(conflict);
                }}
                onSaveAsCopy={async () => {
                  if (!conflict) return;
                  const graph = getGraph();
                  if (!graph) return;
                  try {
                    const copy = await createCanvas({ title: `${title} 副本`, graph });
                    toast.success(`已另存为「${copy.title}」`, {
                      action: { label: "打开", onClick: () => navigate(`/canvas/${copy.id}`) },
                    });
                  } catch {
                    toast.error("另存失败，请稍后重试");
                    return;
                  }
                  dismissConflict();
                  onConflict(conflict);
                }}
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
          </TooltipProvider>
        </OverlayGateProvider>
      </MultiSelectProvider>
    </CanvasHistoryProvider>
  );
});
