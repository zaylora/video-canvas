import {
  memo,
  useCallback,
  useEffect,
  useImperativeHandle,
  useMemo,
  useRef,
  useState,
  type ReactNode,
  type Ref,
} from "react";
import {
  MiniMap,
  ReactFlow,
  SelectionMode,
  useEdgesState,
  useNodesState,
  useReactFlow,
  type NodeChange,
  type EdgeChange,
  type OnBeforeDelete,
  type OnNodeDrag,
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
import { AlignGuidesLayer } from "@/components/canvas/align-guides-layer";
import { CanvasBackgroundLayer } from "@/components/canvas/canvas-background";
import { NodeHistoryDialog } from "@/components/canvas/history-dialog";
import { MediaLightbox, type LightboxTarget } from "@/components/canvas/media-lightbox";
import { SettingsDialog, type SettingModelGroup } from "@/components/setting";
import { TooltipProvider } from "@/components/ui/tooltip";
import {
  ANIMATED_EDGE_OPTIONS,
  NODE_LIBRARY,
  REMOTE_KIND_OF_NODE,
  UPLOAD_ACCEPT,
  UPLOAD_ACTION,
  ASSET_LIBRARY_ACTION,
  CHARACTER_ACTION,
} from "@/constants/canvas";
import { CanvasHistoryProvider, useCanvasHistory } from "@/hooks/use-canvas-history";
import { useCanvasMenu } from "@/hooks/use-canvas-menu";
import { useContentNodes } from "@/hooks/use-content-nodes";
import { useRemoteModels } from "@/hooks/use-models";
import { useTaskBackfill } from "@/hooks/use-task-backfill";
import { rememberCanvasTitle } from "@/utils/canvas/title-cache";
import { useSettingsStore } from "@/store";
import { useFramePickStore } from "@/store/frame-pick";
import { useDirectorStore } from "@/store/director";
import { useTrimStore } from "@/store/trim";
import { DirectorHost } from "@/components/director/director-host";
import { openDirector } from "@/components/director/open-director";
import type { CanvasEdge, CanvasNode, FlowNode, NodeKind, UploadNotice } from "@/types";
import { getModelOptions, pruneRemoteDefaults } from "@/utils/canvas/canvas";
import { createCanvas } from "@/api/canvas";
import type { CanvasDetailDto } from "@/api/canvas/type";
import { canLinkFrom, canLinkNodes } from "@/utils/canvas/link-rule";
import { TEXT_BODY_MAX, clipFileName, clipText, type ClipboardContent } from "@/utils/canvas/paste";
import { DEFAULT_NODE_SIZE } from "@/utils/canvas/placement";
import {
  deserializeGraph,
  hasVolatileRunning,
  serializeGraph,
} from "@/utils/canvas/canvas-persistence";
import { useAgentCanvasSync } from "@/hooks/use-agent-canvas-sync";
import { useCanvasPersistence } from "@/hooks/use-canvas-persistence";
import {
  createViewportWriter,
  loadViewport,
  sameViewport,
  saveViewport,
} from "@/utils/canvas/viewport-store";
import { getCurrentUserId } from "@/utils/storage/user-id";
import { CanvasIdContext } from "@/utils/canvas/canvas-id";
import { cn } from "@/lib/utils";
import { draftStore } from "@/utils/canvas/draft-idb";
import type { Recovery } from "@/utils/canvas/draft-reconcile";
import { releaseObjectUrl } from "@/utils/canvas/media";
import { trimRunner } from "@/utils/canvas/trim-runner";
import { uploadRunner } from "@/utils/canvas/upload-runner";
import { collectPreviewItems, type PreviewItem } from "@/utils/canvas/preview-items";
import { groupMembers, isGroupNode } from "@/utils/canvas/group";

import { AssetPickerDialog } from "@/components/assets/asset-picker-dialog";
import { CharacterPickerDialog } from "@/components/characters/character-picker-dialog";
import { useCharacterReselect } from "@/store/character-reselect";
import { CanvasNodeView } from "./canvas-node";
import { GroupDeleteDialog } from "./group-delete-dialog";
import { GroupNodeView } from "./group-node";
import { GroupFanOutProvider, GroupUiProvider, useGroupUiState } from "./group-ui";
import { useAlignGuides } from "./use-align-guides";
import { useGroupDrag } from "./use-group-drag";
import { useGroupOps } from "./use-group-ops";
import { useClearSelection, useFocusNode, useLocateNode, useSelectNode } from "./use-focus-node";
import { ConflictDialog } from "./conflict-dialog";
import {
  CanvasEditorApiProvider,
  type CanvasEditorApi,
  type CanvasEditorCommands,
} from "./editor-api";
import { applyNodeDefaults, NodeExtensionsProvider, type NodeExtensions } from "./extensions";
import { OverlayGateProvider, useOverlayGate } from "./overlay-gate";
import { NodePreviewProvider } from "./preview-context";
import { MultiSelectProvider, SelectionToolbar } from "./selection-toolbar";
import { useCanvasShortcuts } from "./use-canvas-shortcuts";

/** 画布最小缩放 */
const MIN_ZOOM = 0.14;

/** 新节点的估算尺寸：从视口中心落节点时，让节点正中对准视口中心 */
const NEW_NODE_SIZE = DEFAULT_NODE_SIZE;

/** 上传完那句话按语气挑 toast */
function showUploadNotice(notice: UploadNotice | null) {
  if (!notice) return;
  if (notice.tone === "error") toast.error(notice.text);
  else toast.info(notice.text);
}

/** 节点上挂着的任务号（含批次里还没结清的） */
const taskIdsOf = (nodes: FlowNode[]) =>
  nodes.flatMap((node) =>
    isGroupNode(node)
      ? []
      : [...(node.data.taskId ? [node.data.taskId] : []), ...(node.data.pendingTaskIds ?? [])],
  );

/** 供 ReactFlow 使用的节点类型表，摆在模块顶层，重渲染时不会换新对象 */
const nodeTypes = { canvas: CanvasNodeView, group: GroupNodeView } satisfies NodeTypes;

/** 供 ReactFlow 使用的边类型表 */
const edgeTypes = { animatedSvgEdge: AnimatedSvgEdge } satisfies EdgeTypes;

export type CanvasEditorProps = {
  canvas: CanvasDetailDto;
  /** 打开时本地草稿的对账结果：restored 是已用草稿恢复，conflict 是草稿基于的版本已过期 */
  recovery: Recovery | null;
  onConflict: (canvas: CanvasDetailDto) => void;
  /**
   * 「添加节点」菜单有哪些条目，由页面决定（可用节点种类也由此限定）。
   * disabled 判断某种节点能不能点（拉线落空时只放行接得上的），页面一般直接转给条目的 disabled。
   * 条目的 value 是节点种类，或 UPLOAD_ACTION / ASSET_LIBRARY_ACTION / CHARACTER_ACTION 三个动作之一，
   * 动作由编辑器处理。要保持引用稳定（模块级函数或 useCallback）。
   */
  menuItems: (disabled: (kind: NodeKind) => boolean) => AddNodeMenuItem[];
  /** 按下快捷键帮助的快捷键（?）时的回调；快捷键说明弹窗由页面自己放 */
  onOpenShortcuts?: () => void;
  /**
   * 新建节点默认数据钩子：用户新建的素材节点（菜单、上传、粘贴、引入资产、角色卡）落进画布前经它改写，
   * 返回的节点就是最终写入的。Agent 补丁、撤销重做、复制粘贴节点不经过它。要保持引用稳定。
   */
  onNodeCreate?: (node: CanvasNode) => CanvasNode;
  /** 节点功能区追加按钮，见 NodeExtensions。要保持引用稳定 */
  toolbarExtra?: NodeExtensions["toolbarExtra"];
  /**
   * 删除节点前的确认钩子（Delete 键、选中工具栏、角色卡节点上的删除都经它）：返回 false 就不删。
   * 页面用它提示「有 N 个片段引用了它」这类影响；组删除有自己的确认框，不经这里。要保持引用稳定。
   */
  confirmDeleteNodes?: (nodes: FlowNode[]) => boolean | Promise<boolean>;
  /** 空节点（无素材无历史）默认不出功能区；返回 true 的节点仍显示，只摆 toolbarExtra 的内容。要保持引用稳定 */
  toolbarVisible?: NodeExtensions["toolbarVisible"];
  /**
   * 冲突弹窗「另存为新画布」成功后 toast「打开」要跳去的地址；默认 `/canvas/:id`。
   * 返回 null 表示不提供「打开」入口（如短剧画布，副本只是普通画布）。要保持引用稳定。
   */
  copyOpenPath?: (copyId: string) => string | null;
  /** 编辑器根节点的尺寸类，默认铺满视口（h-svh w-svw）；嵌在页面某个区域里时（如短剧资产画布）传 h-full w-full */
  containerClassName?: string;
  /** 外壳插槽：渲染在画布根节点里、ReactFlow 之后（顶栏、底栏等 ChromeZone 放这）；里面用 useCanvasEditor() 取状态和命令 */
  overlay?: ReactNode;
  /** 外壳插槽：与画布根并排的兄弟（停靠的侧栏），同样可以用 useCanvasEditor() */
  aside?: ReactNode;
  /** 对外命令（定位节点 / 选中节点 / 读取节点列表）；编辑器之外的代码经它操作画布 */
  commandsRef?: Ref<CanvasEditorCommands>;
};

/**
 * 可复用的画布编辑器：节点 / 连线状态、撤销重做、保存与草稿、revision 冲突处理、任务回填、对齐线、编组、多选、
 * 上传 / 粘贴 / 修剪 / 截帧、各种节点、生成面板、历史 / 预览 / 资产选择弹窗、快捷键、背景层都在这里。
 * 页面专属的东西（顶栏、底栏、空状态、Agent 等）不在这里，经 overlay / aside 插槽由页面组装；
 * 页面之间的差异经 menuItems、onNodeCreate、toolbarExtra 三个扩展点和 commandsRef 命令表达，不在这里加页面分支。
 * 必须放在 ReactFlowProvider 里。
 * 加载层退场时外面会在根节点（data-canvas-root）挂 data-entering 播入场，样式见 index.css；
 * 用 memo 包住，免得页面上的加载状态变化把整张画布重渲染一遍。
 */
export const CanvasEditor = memo(function CanvasEditor({
  canvas,
  recovery,
  onConflict,
  menuItems: buildMenuItems,
  onOpenShortcuts,
  onNodeCreate,
  toolbarExtra,
  toolbarVisible,
  confirmDeleteNodes,
  copyOpenPath,
  containerClassName,
  overlay,
  aside,
  commandsRef,
}: CanvasEditorProps) {
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
  const {
    adjustChanges,
    onNodeDragStart: alignDragStart,
    onNodeDragStop: alignDragStop,
  } = useAlignGuides();
  const { pruneOnChange, onNodeClick: onGateNodeClick } = overlayGate;
  const onNodesChange = useCallback(
    (incoming: NodeChange<FlowNode>[]) => {
      // 拖动单个节点时先做对齐吸附，改写完的变更再往下走（保存判定、applyNodesChange）
      const changes = adjustChanges(incoming);
      pruneOnChange(changes);
      for (const change of changes) {
        if (change.type === "remove") {
          // 删掉还在上传的节点就是取消上传
          uploadRunner.cancel(change.id);
          // 删掉正在修剪的源节点：编码结果没处挂，中止并清掉会话
          if (useTrimStore.getState().nodeId === change.id) {
            trimRunner.abort(change.id);
            useTrimStore.getState().reset();
          }
          // 删掉正在选帧的视频：暂存的帧没处挂，清掉会话
          useFramePickStore.getState().leave(change.id);
          // 删掉正打开着的导演台：编辑器跟着关（还在导出的话，编辑器卸载时会中止导出）
          if (useDirectorStore.getState().nodeId === change.id) useDirectorStore.getState().reset();
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
    [adjustChanges, applyNodesChange, pruneOnChange],
  );
  useEffect(() => {
    nodesRef.current = nodes;
    edgesRef.current = edges;
  }, [nodes, edges]);
  // 任务结果回填节点；打开画布时对账还在 running 的节点
  useTaskBackfill(contentNodes, setContentNodes);
  // 保存撞上冲突要弹「画布已在别处修改」：先关掉导演台，弹窗显示在画布上（设计稿 3D导演台 §7）。
  // 场景早已写进节点，跟着弹窗里「另存为新画布 / 加载最新」走，和画布上其他本地改动一样
  useEffect(() => {
    if (conflict !== null || draftConflict !== null) useDirectorStore.getState().reset();
  }, [conflict, draftConflict]);
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
      // 离开画布：还在传的文件一并取消，节点没存，传完也没处挂
      uploadRunner.cancelAll();
      // 离开画布：还在编码的修剪一并中止并清掉会话，编码结果没有节点可挂
      trimRunner.abortAll();
      useTrimStore.getState().reset();
      // 选帧会话只在内存里，离开画布就丢，暂存帧的本地地址一并释放
      useFramePickStore.getState().reset();
      // 导演台编辑器只属于这张画布
      useDirectorStore.getState().reset();
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
  const { onNodeDragStop: groupDragStop } = groupDrag;
  /** 松手：组归属判定 + 收掉对齐辅助线；用 useCallback 保持引用稳定，免得 ReactFlow 每次渲染都换处理函数 */
  const onNodeDragStop = useCallback<OnNodeDrag<FlowNode>>(
    (event, node, dragged) => {
      groupDragStop(event, node, dragged);
      alignDragStop();
    },
    [alignDragStop, groupDragStop],
  );
  const { setMenuId, setRenamingId } = groupUi;
  /** 会一路传到每个 NodeWrapper，要保持引用稳定，否则每次重渲染 200 个节点外壳的 memo 都失效 */
  const onNodeClick = useCallback(
    (event: React.MouseEvent, node: FlowNode) => {
      onGateNodeClick(event, node);
      // 点一下组才弹它的工具条，点别的节点则收起
      setMenuId(isGroupNode(node) ? node.id : null);
    },
    [onGateNodeClick, setMenuId],
  );
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
  const confirmDeleteRef = useRef(confirmDeleteNodes);
  useEffect(() => {
    confirmDeleteRef.current = confirmDeleteNodes;
  }, [confirmDeleteNodes]);
  const onBeforeDelete = useCallback<OnBeforeDelete<FlowNode, CanvasEdge>>(
    async ({ nodes: doomed, edges: doomedEdges }) => {
      if (confirmedDelete.current) return { nodes: doomed, edges: doomedEdges };
      const groups = doomed.filter(isGroupNode);
      if (groups.length > 0) {
        askDeleteGroup(
          groups.map((group) => group.id),
          doomed.map((node) => node.id),
        );
        return false;
      }
      if (confirmDeleteRef.current && !(await confirmDeleteRef.current(doomed))) return false;
      return { nodes: doomed, edges: doomedEdges };
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
  const [minimap, setMinimap] = useState(false);
  const openShortcuts = useCallback(() => onOpenShortcuts?.(), [onOpenShortcuts]);
  const saveNow = useCallback(() => void flush(), [flush]);
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
  const selectNode = useSelectNode();
  const clearSelection = useClearSelection();
  const locateNode = useLocateNode();
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
  /** 打开预览灯箱；origin 是节点在屏幕上的矩形，弹层从它的中心长出来 */
  const startPreview = useCallback((id: string, rect: DOMRect | null) => {
    // 节点里正在播的视频让位给预览，免得两路声音叠在一起
    document
      .querySelectorAll<HTMLVideoElement>(".react-flow__node video")
      .forEach((v) => v.pause());
    setPreview({
      id,
      origin: rect ? { x: rect.left + rect.width / 2, y: rect.top + rect.height / 2 } : null,
    });
  }, []);
  const onNodeDoubleClick = useCallback(
    (event: React.MouseEvent, node: FlowNode) => {
      if (isGroupNode(node)) return;
      // 视频控制条、按钮、输入框上的双击是它们自己的事
      if ((event.target as Element).closest("button, input, textarea, a, .nodrag")) return;
      // 导演台双击打开编辑器，不进灯箱
      if (node.data.kind === "director") {
        openDirector(node.id, node.data);
        return;
      }
      if (collectPreviewItems([node]).length === 0) return;
      startPreview(node.id, event.currentTarget.getBoundingClientRect());
    },
    [startPreview],
  );
  /** 功能区的「放大」：和双击一样的效果，弹层从节点中心长出来 */
  const previewNode = useCallback(
    (id: string) => {
      const node = getNode(id);
      if (!node || isGroupNode(node) || collectPreviewItems([node]).length === 0) return;
      const element = document.querySelector(`.react-flow__node[data-id="${CSS.escape(id)}"]`);
      startPreview(id, element?.getBoundingClientRect() ?? null);
    },
    [getNode, startPreview],
  );
  /** 用户新建节点走的写入口：新出现的节点先经页面的默认数据钩子，再写进画布 */
  const onNodeCreateRef = useRef(onNodeCreate);
  useEffect(() => {
    onNodeCreateRef.current = onNodeCreate;
  }, [onNodeCreate]);
  const setMenuNodes = useCallback<React.Dispatch<React.SetStateAction<CanvasNode[]>>>(
    (action) =>
      setContentNodes((previous) => {
        const next = typeof action === "function" ? action(previous) : action;
        return applyNodeDefaults(previous, next, onNodeCreateRef.current);
      }),
    [setContentNodes],
  );
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
    addAssetNodes,
    addCharacterNodes,
    replaceCharacter,
    onConnect,
    onConnectEnd,
    onDoubleClick,
    onPaneMouseDown,
  } = useCanvasMenu({
    setNodes: setMenuNodes,
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
  /** 资产库弹框：null 是关着，打开时记下引入的素材落在哪（底栏按钮是视口中心，菜单是菜单位置） */
  const [assetPicker, setAssetPicker] = useState<{ origin: { x: number; y: number } } | null>(null);
  const openAssetPickerAtCenter = useCallback(
    () => setAssetPicker({ origin: viewportCenter() }),
    [viewportCenter],
  );
  /** 角色卡选择框：null 是关着，打开时记下节点落在哪（底栏按钮是视口中心，菜单是菜单位置） */
  const [characterPicker, setCharacterPicker] = useState<{
    origin: { x: number; y: number };
  } | null>(null);
  const openCharacterPickerAtCenter = useCallback(
    () => setCharacterPicker({ origin: viewportCenter() }),
    [viewportCenter],
  );
  /** 已失效的角色卡节点点「重新选择」：单选替换这个节点 */
  const reselectNodeId = useCharacterReselect((state) => state.nodeId);
  /** 当前画布上已有节点的角色卡 ID，选择框里标「画布中已有」；只在选择框打开时才算 */
  const placedCharacterIds = useMemo(
    () =>
      new Set(
        characterPicker || reselectNodeId
          ? nodes.flatMap((node) =>
              node.type === "canvas" && node.data.characterId ? [node.data.characterId] : [],
            )
          : [],
      ),
    [characterPicker, nodes, reselectNodeId],
  );
  const uploadAtCenter = useCallback(() => {
    beginUploadAt(viewportCenter());
    uploadInputRef.current?.click();
  }, [beginUploadAt, viewportCenter]);
  /**
   * 粘贴来的外部内容落到画布：文件走和「上传素材」一样的流程（本地预览、上传、换正式地址），
   * 文字直接落成文本节点，正文就是粘贴的内容。落点是指针处，指针不在画布上就落在视口中心。
   */
  const pasteContent = useCallback(
    (content: ClipboardContent, at: { x: number; y: number } | null) => {
      const origin = at ?? viewportCenter();
      if (content.files.length > 0) {
        beginUploadAt(origin);
        const files = content.files.map(
          (file) => new File([file], clipFileName(file), { type: file.type }),
        );
        void addUploadedNodes(files).then(showUploadNotice);
        return;
      }

      const { text, clipped } = clipText(content.text);
      addNodeAt("script", origin, { status: "done", text });
      if (clipped) toast.info(`文字太长，只放进了前 ${TEXT_BODY_MAX} 个字`);
    },
    [addNodeAt, addUploadedNodes, beginUploadAt, viewportCenter],
  );
  useCanvasShortcuts({
    undo: history.undo,
    redo: history.redo,
    setTool,
    openShortcuts,
    save: saveNow,
    group: groupShortcuts,
    paste: pasteContent,
  });

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
    const items = buildMenuItems((kind) => {
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
  }, [buildMenuItems, defaultModels, getNode, pending, pendingGroup]);

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

  const extensions = useMemo<NodeExtensions>(
    () => ({ toolbarExtra, toolbarVisible }),
    [toolbarExtra, toolbarVisible],
  );
  const openSettings = useCallback(() => setSettingsOpen(true), []);
  const openAssetPicker = openAssetPickerAtCenter;
  const openCharacterPicker = openCharacterPickerAtCenter;
  /** 对外命令：引用稳定，读的都是最新数据（getNodes 直接取 xyflow 的当前节点表） */
  const commands = useMemo<CanvasEditorCommands>(
    () => ({ getNodes, focusNode, selectNode, locateNode, clearSelection }),
    [clearSelection, focusNode, getNodes, locateNode, selectNode],
  );
  useImperativeHandle(commandsRef, () => commands, [commands]);
  const api: CanvasEditorApi = {
    ...commands,
    canvasId: canvas.id,
    title,
    rename: onRename,
    saveStatus,
    saveNow: flush,
    nodes,
    tool,
    setTool,
    history,
    minimap,
    setMinimap,
    addAtCenter,
    uploadAtCenter,
    openAssetPicker,
    openCharacterPicker,
    openSettings,
  };

  return (
    // data-tool 驱动 index.css 里的光标与命中规则
    <CanvasIdContext.Provider value={canvas.id}>
      <CanvasEditorApiProvider value={api}>
        <NodeExtensionsProvider value={extensions}>
          <CanvasHistoryProvider value={history}>
            <MultiSelectProvider value={multiSelected}>
              <OverlayGateProvider value={overlayGate.dragSelected}>
                <GroupUiProvider value={groupUi}>
                  <GroupFanOutProvider value={openGroupMenu}>
                    <NodePreviewProvider value={previewNode}>
                      <TooltipProvider delay={400}>
                        {/* 画布和页面停靠的侧栏（aside 插槽）并排；侧栏浮窗时盖在画布上 */}
                        <div
                          className={cn(
                            "relative flex overflow-hidden",
                            containerClassName ?? "h-svh w-svw",
                          )}
                        >
                          <div
                            className="bg-canvas relative h-full min-w-0 flex-1 overflow-hidden"
                            data-tool={activeTool}
                            data-canvas-root
                            onPointerDownCapture={overlayGate.onPointerDownCapture}
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
                                alignDragStart(event, node, dragged);
                              }}
                              onNodeDrag={groupDrag.onNodeDrag}
                              onNodeDragStop={onNodeDragStop}
                              onNodeClick={onNodeClick}
                              onBeforeDelete={onBeforeDelete}
                              onConnect={onConnect}
                              isValidConnection={isValidConnection}
                              onConnectEnd={onConnectEnd}
                              // 抓手模式下双击也只是拖画布的一部分，别在松手后冒出添加菜单
                              onMouseDown={isPanning ? undefined : onPaneMouseDown}
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
                              <CanvasBackgroundLayer background={settings.background} />
                              <AlignGuidesLayer />
                              <SelectionToolbar onFanOut={openGroupMenu} onGroup={groupSelected} />
                              {minimap && (
                                <MiniMap
                                  position="bottom-left"
                                  pannable
                                  zoomable
                                  className="canvas-overlay-interactive !bottom-16 !left-[76px] overflow-hidden rounded-xl shadow-lg ring-1 ring-chrome-border"
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
                                if (files.length)
                                  void addUploadedNodes(files).then(showUploadNotice);
                              }}
                            />

                            {overlay}
                            <SettingsDialog
                              open={settingsOpen}
                              onOpenChange={setSettingsOpen}
                              modelGroups={modelGroups}
                            />
                            <NodeHistoryDialog />
                            <DirectorHost />
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
                                  const copy = await createCanvas({
                                    title: `${title} 副本`,
                                    graph,
                                  });
                                  const openPath = copyOpenPath
                                    ? copyOpenPath(copy.id)
                                    : `/canvas/${copy.id}`;
                                  toast.success(`已另存为「${copy.title}」`, {
                                    action: openPath
                                      ? { label: "打开", onClick: () => navigate(openPath) }
                                      : undefined,
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

                                // 从资产库添加：落点是菜单位置；不连线，只建节点
                                if (value === ASSET_LIBRARY_ACTION) {
                                  if (menu) setAssetPicker({ origin: menu.flow });
                                  closeMenu();
                                  return;
                                }

                                // 角色卡：落点是菜单位置；选完卡只建节点，连线用户自己拉
                                if (value === CHARACTER_ACTION) {
                                  if (menu) setCharacterPicker({ origin: menu.flow });
                                  closeMenu();
                                  return;
                                }

                                addNode(value as NodeKind);
                              }}
                            />
                            <AssetPickerDialog
                              open={assetPicker !== null}
                              onOpenChange={(open) => !open && setAssetPicker(null)}
                              onConfirm={(assets) => {
                                if (assetPicker) addAssetNodes(assets, assetPicker.origin);
                              }}
                            />
                            <CharacterPickerDialog
                              open={characterPicker !== null}
                              onOpenChange={(open) => !open && setCharacterPicker(null)}
                              placedIds={placedCharacterIds}
                              onConfirm={(cards) => {
                                if (characterPicker)
                                  addCharacterNodes(cards, characterPicker.origin);
                              }}
                            />
                            {/* 已失效节点「重新选择」：单选，替换那个节点（连线保留） */}
                            <CharacterPickerDialog
                              open={reselectNodeId !== null}
                              onOpenChange={(open) =>
                                !open && useCharacterReselect.getState().close()
                              }
                              replace
                              placedIds={placedCharacterIds}
                              onConfirm={([card]) => {
                                if (reselectNodeId && card) replaceCharacter(reselectNodeId, card);
                              }}
                            />
                          </div>
                          {aside}
                        </div>
                      </TooltipProvider>
                    </NodePreviewProvider>
                  </GroupFanOutProvider>
                </GroupUiProvider>
              </OverlayGateProvider>
            </MultiSelectProvider>
          </CanvasHistoryProvider>
        </NodeExtensionsProvider>
      </CanvasEditorApiProvider>
    </CanvasIdContext.Provider>
  );
});
