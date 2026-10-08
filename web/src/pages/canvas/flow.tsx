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
  type OnBeforeDelete,
  type EdgeTypes,
  type NodeTypes,
} from "@xyflow/react";
import { useNavigate } from "react-router";
import { toast } from "sonner";

import {
  AddNodeMenu,
  AnimatedSvgEdge,
  PendingConnectionLine,
  PendingFanLines,
  useCanvasTool,
  type AddNodeMenuItem,
} from "@/components/canvas";
import { ChromeZone } from "@/components/canvas/chrome/chrome";
import { MediaLightbox, type LightboxTarget } from "@/components/canvas/media-lightbox";
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
import { useContentNodes } from "@/hooks/use-content-nodes";
import { useRemoteModels } from "@/hooks/use-models";
import { useTaskBackfill } from "@/hooks/use-task-backfill";
import { rememberCanvasTitle } from "@/utils/canvas/title-cache";
import { GRID_SIZE, useSettingsStore } from "@/store";
import type { CanvasEdge, CanvasNode, FlowNode, NodeKind, NodeOutput, UploadNotice } from "@/types";
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
import { AnimatePresence } from "motion/react";
import { useAgentCanvasSync } from "@/hooks/use-agent-canvas-sync";
import { useAgentController, useAgentModels } from "@/hooks/use-agent-controller";
import { useCanvasPersistence } from "@/hooks/use-canvas-persistence";
import {
  createViewportWriter,
  loadViewport,
  sameViewport,
  saveViewport,
} from "@/utils/canvas/viewport-store";
import { getCurrentUserId } from "@/utils/storage/user-id";
import { draftStore } from "@/utils/canvas/draft-idb";
import type { Recovery } from "@/utils/canvas/draft-reconcile";
import { releaseObjectUrl } from "@/utils/canvas/media";
import { collectPreviewItems, type PreviewItem } from "@/utils/canvas/preview-items";
import { groupMembers, isGroupNode } from "@/utils/canvas/group";

import { BottomToolbar } from "./chrome/bottom-toolbar";
import { EmptyState } from "./chrome/empty-state";
import { ShortcutsDialog } from "./chrome/shortcuts-dialog";
import { StatsBar } from "./chrome/stats-bar";
import { TopLeftBar } from "./chrome/top-left-bar";
import { TopRightBar } from "./chrome/top-right-bar";
import { ViewControls } from "./chrome/view-controls";
import { CanvasNodeView } from "./canvas-node";
import { GroupDeleteDialog } from "./group-delete-dialog";
import { GroupNodeView } from "./group-node";
import { GroupUiProvider, useGroupUiState } from "./group-ui";
import { useGroupDrag } from "./use-group-drag";
import { useGroupOps } from "./use-group-ops";
import { useFocusNode } from "./chrome/use-focus-node";
import { AgentPanel } from "./agent/agent-panel";
import { AgentLauncher } from "./chrome/agent-launcher";
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
const taskIdsOf = (nodes: FlowNode[]) =>
  nodes.flatMap((node) => (!isGroupNode(node) && node.data.taskId ? [node.data.taskId] : []));

/** 供 ReactFlow 使用的节点类型表，摆在模块顶层，重渲染时不会换新对象 */
const nodeTypes = { canvas: CanvasNodeView, group: GroupNodeView } satisfies NodeTypes;

/** 供 ReactFlow 使用的边类型表 */
const edgeTypes = { animatedSvgEdge: AnimatedSvgEdge } satisfies EdgeTypes;

/**
 * 画布主体。加载层退场时外面会在根节点（data-canvas-root）挂 data-entering 播入场，样式见 index.css；
 * 用 memo 包住，免得页面上的加载状态变化把整张画布重渲染一遍。
 */
export const Flow = memo(function Flow({
  canvas,
  recovery,
  onConflict,
}: {
  canvas: CanvasDetailDto;
  /** 打开时本地草稿的对账结果：restored 是已用草稿恢复，conflict 是草稿基于的版本已过期 */
  recovery: Recovery | null;
  onConflict: (canvas: CanvasDetailDto) => void;
}) {
  const initial = useMemo(() => deserializeGraph(canvas.graph), [canvas.graph]);
  const [nodes, setNodes, applyNodesChange] = useNodesState<FlowNode>(initial.nodes);
  const [edges, setEdges, applyEdgesChange] = useEdgesState<CanvasEdge>(initial.edges);
  const [contentNodes, setContentNodes] = useContentNodes(nodes, setNodes);
  const { getViewport, setViewport, screenToFlowPosition, getNode, getNodes, deleteElements } =
    useReactFlow<FlowNode, CanvasEdge>();
  const navigate = useNavigate();
  const hydratedRef = useRef(false);
  const nodesRef = useRef(nodes);
  const edgesRef = useRef(edges);
  /** 这一轮节点/连线变化要不要存：false 不存（选中、尺寸、拖动过程中），true 要存，null 是不经过 onNodesChange 的数据修改 */
  const changeSaveRef = useRef<boolean | null>(null);
  /** 已经见过的任务号：出现新的就要立刻存，刷新后才能对账回填 */
  const knownTaskIdsRef = useRef(new Set(taskIdsOf(initial.nodes)));
  /**
   * 视口是这台设备此刻看哪里，不是画布内容：平移缩放不触发保存，只防抖写进本机 localStorage，
   * 刷新、返回再进来停在移动之后的位置；云端里的视口只在内容保存时顺带更新，用于第一次打开和换设备。
   */
  const userId = useMemo(() => getCurrentUserId(), []);
  const appliedViewportRef = useRef(initial.viewport);
  /** 打开时就发现草稿和云端冲突：把草稿内容留给冲突弹窗，让用户选加载最新还是另存为 */
  const [draftConflict, setDraftConflict] = useState(
    recovery?.kind === "conflict" ? recovery.graph : null,
  );
  /** 冲突处理完（加载最新或另存为）后草稿就没用了，删掉，免得下次打开又弹一次 */
  const discardDraft = useCallback(() => {
    if (userId) void draftStore.remove(userId, canvas.id);
  }, [canvas.id, userId]);
  const viewportWriter = useMemo(
    () =>
      createViewportWriter((viewport) => {
        if (userId) saveViewport(userId, canvas.id, viewport);
      }),
    [canvas.id, userId],
  );
  useEffect(() => {
    // 页面隐藏、关闭、离开画布时把还没写的视口立刻写出去（localStorage 同步写，一定写得完）
    const flushViewport = () => viewportWriter.flush();
    window.addEventListener("pagehide", flushViewport);
    return () => {
      window.removeEventListener("pagehide", flushViewport);
      viewportWriter.flush();
    };
  }, [viewportWriter]);
  // 保存发请求的那一刻才取图谱，视口也在这时读，所以平移缩放本身不用触发保存
  /** Agent 改画布的同步（见 useAgentCanvasSync）；保存的回调经它转给同步 */
  const agentSyncRef = useRef<ReturnType<typeof useAgentCanvasSync> | null>(null);
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
    getVersion,
    mergeVersion,
    hasUnsaved,
  } = useCanvasPersistence({
    canvasId: canvas.id,
    initialVersion: canvas.version,
    getGraph,
    onConflict,
    // 同步要等撤销栈建好才能创建，这里先经 ref 转一道
    autoMerge: () => agentSyncRef.current?.autoMerge() ?? Promise.resolve(null),
    onSaved: (graph, version) => agentSyncRef.current?.saved(graph, version),
  });
  const overlayGate = useOverlayGate(getNodes);
  const { pruneOnChange } = overlayGate;
  const onNodesChange = useCallback(
    (changes: NodeChange<FlowNode>[]) => {
      pruneOnChange(changes);
      for (const change of changes) {
        if (change.type === "remove") {
          const removed = nodesRef.current.find((node) => node.id === change.id);
          if (removed && !isGroupNode(removed)) releaseObjectUrl(removed.data.src);
        }
      }
      // 测量产生的尺寸变化不算内容改动；缩放组框（NodeResizer）发出的带 setAttributes，要存
      const persistent = changes.filter(
        (change) =>
          change.type !== "select" && (change.type !== "dimensions" || change.setAttributes),
      );
      // 只有选中、测量尺寸，或者还在拖动的过程中，都不算内容改动
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
  useTaskBackfill(contentNodes, setContentNodes);
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
      for (const node of nodesRef.current) if (!isGroupNode(node)) releaseObjectUrl(node.data.src);
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
  const agentSync = useAgentCanvasSync({
    canvas,
    getVersion,
    mergeVersion,
    hasUnsaved,
    nodesRef,
    edgesRef,
    setNodes,
    setEdges,
    skipNextSave: () => {
      changeSaveRef.current = false;
    },
    markTasksKnown: (ids) => {
      for (const id of ids) knownTaskIdsRef.current.add(id);
    },
    resetHistory: history.reset,
  });
  useEffect(() => {
    agentSyncRef.current = agentSync;
  }, [agentSync]);
  // 画布 Agent 浮窗：控制器常驻（收起后运行状态、事件接收照常），⌘/ 开关
  const agentModels = useAgentModels();
  const [agentOpen, setAgentOpen] = useState(false);
  const agentCtl = useAgentController({
    canvasId: canvas.id,
    models: agentModels ?? [],
    getSelection: () => nodesRef.current.filter((node) => node.selected).map((node) => node.id),
    getViewport,
  });
  const toggleAgent = useCallback(() => setAgentOpen((open) => !open), []);
  /** 输入框里 @ 能引用的节点（组不算） */
  const agentNodeOptions = useMemo(
    () =>
      nodes
        .filter((node): node is CanvasNode => node.type === "canvas")
        .map((node) => ({ id: node.id, label: node.data.label, kind: node.data.kind })),
    [nodes],
  );
  useEffect(() => {
    const onKey = (event: KeyboardEvent) => {
      if ((event.metaKey || event.ctrlKey) && event.key === "/") {
        event.preventDefault();
        setAgentOpen((open) => !open);
      }
    };
    window.addEventListener("keydown", onKey);
    return () => window.removeEventListener("keydown", onKey);
  }, []);

  /**
   * 组（设计稿 6.10）：删除要确认，确认后组和成员一起删。
   * pendingDelete 记下这次删除的全部节点 id（含被一起选中的别的节点），确认时一次删完。
   */
  const [pendingDelete, setPendingDelete] = useState<{
    groups: string[];
    nodeIds: string[];
  } | null>(null);
  const confirmedDelete = useRef(false);
  const askDeleteGroup = useCallback(
    (groupIds: string[], alsoDelete: string[] = []) => {
      const all = getNodes();
      const ids = new Set(alsoDelete);
      for (const id of groupIds) {
        ids.add(id);
        for (const member of groupMembers(all, id)) ids.add(member.id);
      }
      setPendingDelete({ groups: groupIds, nodeIds: [...ids] });
    },
    [getNodes],
  );
  const groupUi = useGroupUiState(
    useCallback((id: string) => askDeleteGroup([id]), [askDeleteGroup]),
  );
  const groupOps = useGroupOps(groupUi);
  const groupDrag = useGroupDrag(groupUi);
  const { setMenuId, setRenamingId } = groupUi;
  const { groupSelected, ungroupById, selectedGroupId } = groupOps;
  const groupShortcuts = useMemo(
    () => ({
      group: groupSelected,
      ungroup: () => {
        const id = selectedGroupId();
        if (id) ungroupById(id);
      },
      rename: () => {
        const id = selectedGroupId();
        if (!id) return false;
        setRenamingId(id);
        return true;
      },
    }),
    [groupSelected, selectedGroupId, setRenamingId, ungroupById],
  );
  /** Delete / Backspace 删到组时先拦下来问一声；确认之后那一次放行 */
  const onBeforeDelete = useCallback<OnBeforeDelete<FlowNode, CanvasEdge>>(
    async ({ nodes: doomed, edges: doomedEdges }) => {
      const groups = doomed.filter(isGroupNode);
      if (groups.length === 0 || confirmedDelete.current)
        return { nodes: doomed, edges: doomedEdges };
      askDeleteGroup(
        groups.map((group) => group.id),
        doomed.map((node) => node.id),
      );
      return false;
    },
    [askDeleteGroup],
  );
  const deleteTarget = useMemo(() => {
    if (!pendingDelete) return null;
    const all = nodes;
    const groups = all.filter((node) => pendingDelete.groups.includes(node.id));
    const name =
      groups.length === 1 && isGroupNode(groups[0])
        ? groups[0].data.label
        : `${groups.length} 个组`;
    return { name, memberCount: pendingDelete.nodeIds.length - pendingDelete.groups.length };
  }, [nodes, pendingDelete]);
  const confirmDeleteGroup = useCallback(() => {
    const target = pendingDelete;
    setPendingDelete(null);
    if (!target) return;
    confirmedDelete.current = true;
    void deleteElements({ nodes: target.nodeIds.map((id) => ({ id })) }).finally(() => {
      confirmedDelete.current = false;
    });
  }, [deleteElements, pendingDelete]);

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
    group: groupShortcuts,
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

  /**
   * 双击节点预览（设计稿 6.9）：弹层状态留在这里，不进 store，免得整张画布跟着重渲染。
   * 可预览项只在弹层开着时才随节点重算；关闭的出场动画里还要接着画，所以留一份最近的。
   */
  const focusNode = useFocusNode();
  const [preview, setPreview] = useState<LightboxTarget | null>(null);
  const previewOpen = preview !== null;
  const liveItems = useMemo(
    () => (previewOpen ? collectPreviewItems(nodes) : null),
    [nodes, previewOpen],
  );
  const [lastItems, setLastItems] = useState<PreviewItem[]>([]);
  if (liveItems && liveItems !== lastItems) setLastItems(liveItems);
  const closePreview = useCallback(() => setPreview(null), []);
  const changePreview = useCallback(
    (id: string) => setPreview((current) => (current ? { ...current, id } : current)),
    [],
  );
  const locatePreview = useCallback(
    (id: string) => {
      setPreview(null);
      focusNode(id);
    },
    [focusNode],
  );
  const onNodeDoubleClick = useCallback((event: React.MouseEvent, node: FlowNode) => {
    if (isGroupNode(node)) return;
    // 视频控制条、按钮、输入框上的双击是它们自己的事
    if ((event.target as Element).closest("button, input, textarea, a, .nodrag")) return;
    if (collectPreviewItems([node]).length === 0) return;
    const rect = event.currentTarget.getBoundingClientRect();
    // 节点里正在播的视频让位给预览，免得两路声音叠在一起
    document
      .querySelectorAll<HTMLVideoElement>(".react-flow__node video")
      .forEach((v) => v.pause());
    setPreview({
      id: node.id,
      origin: { x: rect.left + rect.width / 2, y: rect.top + rect.height / 2 },
    });
  }, []);
  const {
    menu,
    pending,
    pendingGroup,
    openGroupMenu,
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
    setNodes: setContentNodes,
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
  /** 给 Agent 的图片附件：先在画布中心落成图片节点（走现有上传），再把新建的节点作为 chip 引用 */
  const attachImagesForAgent = useCallback(
    async (files: File[]) => {
      const before = new Set(nodesRef.current.map((node) => node.id));
      beginUploadAt(viewportCenter());
      showUploadNotice(await addUploadedNodes(files));
      return nodesRef.current
        .filter((node): node is CanvasNode => node.type === "canvas" && !before.has(node.id))
        .map((node) => ({ type: "node" as const, id: node.id, name: node.data.label }));
    },
    [addUploadedNodes, beginUploadAt, viewportCenter],
  );

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

  // 拉线落空时只放行接得上的种类，双击空白则全部可点；
  // 多选引用时只要有一个被选节点接得上就放行（接不上的建完会跳过），上传素材的节点不接输入，整项禁用
  const menuItems = useMemo<AddNodeMenuItem[]>(() => {
    const from = pending && getNode(pending.nodeId);
    const items = buildAddNodeItems((kind) => {
      const target = { kind, model: defaultModels?.[kind] };
      if (pendingGroup) {
        return !pendingGroup.nodeIds.some((id) => {
          const source = getNode(id);
          return !!source && !isGroupNode(source) && canLinkFrom(source.data, "source", target);
        });
      }
      return from && !isGroupNode(from)
        ? !canLinkFrom(from.data, pending.handleType, target)
        : false;
    });
    return pendingGroup
      ? items.map((item) => (item.value === UPLOAD_ACTION ? { ...item, disabled: true } : item))
      : items;
  }, [defaultModels, getNode, pending, pendingGroup]);

  /** 拖线接到连接点上时的放行规则：种类规则 + 下游当前模型收不收 */
  const isValidConnection = useCallback(
    (connection: { source: string; target: string }) => {
      const source = getNode(connection.source);
      const target = getNode(connection.target);
      return (
        !!source &&
        !!target &&
        !isGroupNode(source) &&
        !isGroupNode(target) &&
        source.id !== target.id &&
        canLinkNodes(source.data, target.data)
      );
    },
    [getNode],
  );

  return (
    // data-tool 驱动 index.css 里的光标与命中规则
    <CanvasHistoryProvider value={history}>
      <MultiSelectProvider value={multiSelected}>
        <OverlayGateProvider value={overlayGate.dragSelected}>
          <GroupUiProvider value={groupUi}>
            <TooltipProvider delay={400}>
              {/* 画布和停靠的 Agent 侧栏并排；浮窗时 Agent 盖在画布上（设计稿 画布Agent助手设计 6.8） */}
              <div className="relative flex h-svh w-svw overflow-hidden">
                <div
                  className="bg-canvas relative h-full min-w-0 flex-1 overflow-hidden"
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
                      // 本机视口优先于云端视口：刷新后停在移动之后的位置
                      const local = userId ? loadViewport(userId, canvas.id) : null;
                      appliedViewportRef.current = local ?? initial.viewport;
                      void setViewport(appliedViewportRef.current);
                      hydratedRef.current = true;
                      if (recovery?.kind === "restored") {
                        // 内容来自本地草稿，云端还没有：标脏让它排上传
                        changed();
                        toast.info("已恢复上次未同步的改动");
                      }
                    }}
                    onMoveEnd={(_event, viewport) => {
                      // 恢复视口那一下不算用户移动，别把云端视口写成本机视口
                      if (
                        !hydratedRef.current ||
                        sameViewport(appliedViewportRef.current, viewport)
                      )
                        return;
                      appliedViewportRef.current = viewport;
                      viewportWriter.schedule(viewport);
                    }}
                    onNodeDragStart={(event, node, dragged) => {
                      overlayGate.onNodeDragStart(event, node, dragged);
                      groupDrag.onNodeDragStart(event, node, dragged);
                    }}
                    onNodeDrag={groupDrag.onNodeDrag}
                    onNodeDragStop={groupDrag.onNodeDragStop}
                    onNodeClick={(event, node) => {
                      overlayGate.onNodeClick(event, node);
                      // 点一下组才弹它的工具条，点别的节点则收起
                      setMenuId(isGroupNode(node) ? node.id : null);
                    }}
                    onBeforeDelete={onBeforeDelete}
                    onConnect={onConnect}
                    isValidConnection={isValidConnection}
                    onConnectEnd={onConnectEnd}
                    // 抓手模式下双击也只是拖画布的一部分，别在松手后冒出添加菜单
                    onDoubleClick={isPanning ? undefined : onDoubleClick}
                    onNodeDoubleClick={isPanning ? undefined : onNodeDoubleClick}
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
                      <Background
                        variant={BACKGROUND_VARIANTS[settings.background]}
                        gap={GRID_SIZE}
                      />
                    )}
                    <SelectionToolbar onFanOut={openGroupMenu} onGroup={groupSelected} />
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
                  {!isPanning && pendingGroup && menu && (
                    <PendingFanLines froms={pendingGroup.froms} to={menu.screen} />
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

                  {nodes.length === 0 && (
                    <EmptyState onAdd={addAtCenter} onUpload={uploadAtCenter} />
                  )}

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
                  <ChromeZone position="bottom-right">
                    <AgentLauncher
                      open={agentOpen}
                      available={agentModels === null ? null : agentModels.length > 0}
                      running={agentCtl.busy}
                      onToggle={toggleAgent}
                    />
                    <div className="max-md:hidden">
                      <StatsBar />
                    </div>
                  </ChromeZone>
                  <SettingsDialog
                    open={settingsOpen}
                    onOpenChange={setSettingsOpen}
                    modelGroups={modelGroups}
                  />
                  <ShortcutsDialog open={shortcutsOpen} onOpenChange={setShortcutsOpen} />
                  <MediaLightbox
                    items={liveItems ?? lastItems}
                    target={preview}
                    onActiveChange={changePreview}
                    onLocate={locatePreview}
                    onClose={closePreview}
                  />
                  <GroupDeleteDialog
                    target={deleteTarget}
                    onConfirm={confirmDeleteGroup}
                    onCancel={() => setPendingDelete(null)}
                  />
                  <ConflictDialog
                    open={conflict !== null || draftConflict !== null}
                    onLoadLatest={() => {
                      if (draftConflict) {
                        // 打开时的冲突：当前编辑器本来就是云端最新，丢掉草稿即可
                        discardDraft();
                        setDraftConflict(null);
                        return;
                      }
                      if (!conflict) return;
                      dismissConflict();
                      onConflict(conflict);
                    }}
                    onSaveAsCopy={async () => {
                      if (!conflict && !draftConflict) return;
                      const graph = draftConflict ?? getGraph();
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
                      if (draftConflict) {
                        discardDraft();
                        setDraftConflict(null);
                        return;
                      }
                      if (!conflict) return;
                      dismissConflict();
                      onConflict(conflict);
                    }}
                  />

                  <AddNodeMenu
                    position={isPanning ? null : (menu?.screen ?? null)}
                    label={
                      pendingGroup
                        ? `引用选中的 ${pendingGroup.nodeIds.length} 个节点生成`
                        : pending
                          ? "引用该节点生成"
                          : "添加节点"
                    }
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
                <AnimatePresence>
                  {agentOpen && agentModels && agentModels.length > 0 && (
                    <AgentPanel
                      key="agent-panel"
                      ctl={agentCtl}
                      models={agentModels}
                      selectionCount={nodes.filter((node) => node.selected).length}
                      nodes={agentNodeOptions}
                      onAttachImages={attachImagesForAgent}
                      onClose={toggleAgent}
                    />
                  )}
                </AnimatePresence>
              </div>
            </TooltipProvider>
          </GroupUiProvider>
        </OverlayGateProvider>
      </MultiSelectProvider>
    </CanvasHistoryProvider>
  );
});
