import { useCallback, useRef, useState } from "react";
import { toast } from "sonner";
import {
  addEdge,
  useReactFlow,
  type Connection,
  type HandleType,
  type OnConnect,
  type OnConnectEnd,
} from "@xyflow/react";

import { getNodeHit } from "@/components/canvas";
import { newNodeLabel, uploadLabel } from "@/utils/canvas/node-label";
import {
  ANIMATED_EDGE_OPTIONS,
  NODE_LIBRARY,
  NODE_META,
  UPLOAD_STACK_COLUMNS,
  UPLOAD_STACK_GAP,
  UPLOAD_TARGET_KIND,
} from "@/constants/canvas";
import type {
  CanvasEdge,
  CanvasMenuState,
  CanvasNode,
  CanvasNodeData,
  MediaType,
  NodeKind,
  PendingGroup,
  UploadNotice,
} from "@/types";
import { canLinkFrom, opForLink, partitionLinkable } from "@/utils/canvas/link-rule";
import { releaseObjectUrl, takeUploadFile } from "@/utils/canvas/media";
import { uploadAsset } from "@/api/asset";

type UseCanvasMenuOptions = {
  setNodes: React.Dispatch<React.SetStateAction<CanvasNode[]>>;
  setEdges: React.Dispatch<React.SetStateAction<CanvasEdge[]>>;
  /** 各种类新建时预选的模型 id，缺省由节点自己回落到清单第一条 */
  defaultModels?: Record<string, string>;
};

/**
 * 「添加节点」菜单的状态机：双击空白或拉线落空时记录锚点，
 * 选定种类后就地建节点，并把拉线落空那根边补上。
 */
export function useCanvasMenu({ setNodes, setEdges, defaultModels }: UseCanvasMenuOptions) {
  const { flowToScreenPosition, getInternalNode, getNode, getNodes, screenToFlowPosition } =
    useReactFlow<CanvasNode, CanvasEdge>();
  const [menu, setMenu] = useState<CanvasMenuState | null>(null);
  /*
   * 菜单里点「上传」到文件真选好，中间隔着一个系统文件框，
   * 菜单这时早关了，所以先把落点扣在这儿等着。
   */
  const uploadPlacement = useRef<CanvasMenuState | null>(null);

  const pending = menu?.connection ?? null;
  const pendingGroup = menu?.group ?? null;

  const closeMenu = useCallback(() => setMenu(null), []);

  /** 上游接进来下游当前方式收不下时，下游切到收得下的方式（文生视频 → 全能参考） */
  const switchOpForLink = useCallback(
    (sourceId: string, targetId: string) =>
      setNodes((nds) => {
        const source = nds.find((node) => node.id === sourceId);
        const target = nds.find((node) => node.id === targetId);
        const op = source && target && opForLink(source.data, target.data);
        if (!op) return nds;
        return nds.map((node) =>
          node.id === targetId
            ? { ...node, data: { ...node.data, params: { ...node.data.params, op } } }
            : node,
        );
      }),
    [setNodes],
  );

  // 新连线直接套上流动高亮：AnimatedSvgEdge 必须拿到 data.shape 才渲染得出光点
  const onConnect = useCallback<OnConnect>(
    (connection) => {
      setEdges((eds) => addEdge({ ...connection, ...ANIMATED_EDGE_OPTIONS }, eds));
      switchOpForLink(connection.source, connection.target);
    },
    [setEdges, switchOpForLink],
  );

  // 拉出端是 source 时对方是下游，是 target 时反过来
  const connectNodes = useCallback(
    (
      fromNodeId: string,
      fromHandleId: string | null,
      fromHandleType: HandleType,
      toNodeId: string,
    ) => {
      const edge: Connection =
        fromHandleType === "source"
          ? {
              source: fromNodeId,
              sourceHandle: fromHandleId,
              target: toNodeId,
              targetHandle: null,
            }
          : {
              source: toNodeId,
              sourceHandle: null,
              target: fromNodeId,
              targetHandle: fromHandleId,
            };

      setEdges((eds) => addEdge({ ...edge, ...ANIMATED_EDGE_OPTIONS }, eds));
      switchOpForLink(edge.source, edge.target);
    },
    [setEdges, switchOpForLink],
  );

  /** 从多选区右侧拉出来松手：在松手处弹菜单，选完种类后每个接得上的节点各连一根线 */
  const openGroupMenu = useCallback(
    (screen: { x: number; y: number }, group: PendingGroup) =>
      setMenu({ screen, flow: screenToFlowPosition(screen), connection: null, group }),
    [screenToFlowPosition],
  );

  // 只有双击空白画布才弹菜单
  const onDoubleClick = useCallback(
    (event: React.MouseEvent) => {
      const target = event.target as Element;
      /*
       * node-toolbar 得单列：它的 class 是 react-flow__node-toolbar，
       * 和 react-flow__node 是两个不同的 class，closest 认不出亲戚关系，
       * 漏了它双击输入框就会当成双击空白、平白弹出建节点菜单
       */
      if (
        target.closest(
          ".react-flow__node, .react-flow__node-toolbar, .react-flow__edge, .react-flow__panel",
        )
      ) {
        return;
      }

      const screen = { x: event.clientX, y: event.clientY };
      setMenu({
        screen,
        flow: screenToFlowPosition(screen),
        connection: null,
      });
    },
    [screenToFlowPosition],
  );

  // 连线松手：落在别的节点身上就直接接过去，落在空白处才在线头弹菜单建新节点
  const onConnectEnd = useCallback<OnConnectEnd>(
    (event, connectionState) => {
      // toNode 才是可靠的落点判断，event.target 会被 pointer capture 骗到
      if (connectionState.toNode || !connectionState.fromHandle || !connectionState.from) {
        return;
      }

      const { from, fromHandle, fromPosition } = connectionState;
      const source = getNode(fromHandle.nodeId);
      if (!source) return;

      const { clientX, clientY } = "changedTouches" in event ? event.changedTouches[0] : event;
      const screen = { x: clientX, y: clientY };
      const flow = screenToFlowPosition(screen);

      /*
       * 线头压在节点身上就算接上：xyflow 只认连接点附近那一圈，
       * 落在节点中间它给不出 toNode，得自己拿落点和节点矩形比。
       * 从后往前找，后加的节点画在上层，压住谁就接谁。
       */
      const target = getNodes()
        .slice()
        .reverse()
        .find((node) => {
          if (node.id === source.id) return false;
          if (!canLinkFrom(source.data, fromHandle.type, node.data)) return false;

          const internalNode = getInternalNode(node.id);
          return !!internalNode && !!getNodeHit(internalNode, flow);
        });

      if (target) {
        connectNodes(source.id, fromHandle.id ?? null, fromHandle.type, target.id);
        return;
      }

      setMenu({
        screen,
        flow,
        connection: {
          nodeId: source.id,
          handleId: fromHandle.id ?? null,
          handleType: fromHandle.type,
          kind: source.data.kind,
          fromScreen: flowToScreenPosition(from),
          fromPosition,
        },
      });
    },
    [connectNodes, flowToScreenPosition, getInternalNode, getNode, getNodes, screenToFlowPosition],
  );

  /**
   * 在记下的落点建一个节点，该接的线一并补上。
   * 种类接不上拉出来那根线时只建节点不接线——上传是先选文件后知种类，
   * 拦不到菜单那一步，只能在这儿兜着。
   */
  const placeNode = useCallback(
    (kind: NodeKind, placement: CanvasMenuState, extra?: Partial<CanvasNodeData>) => {
      const meta = NODE_META.get(kind) ?? NODE_LIBRARY[0];
      const { connection, group } = placement;
      const id = crypto.randomUUID();
      const node: CanvasNode = {
        id,
        type: "canvas",
        position: placement.flow,
        data: {
          kind: meta.kind,
          // 上传的用文件名，其余按种类名编号（「图片 2」），@ 素材时才分得清
          label: newNodeLabel(
            extra?.fileName ? uploadLabel(extra.fileName, meta.label) : meta.label,
            getNodes().map((item) => item.data.label),
          ),
          model: defaultModels?.[meta.kind],
          ...extra,
        },
        // 拉线生成时让落点落在新节点自己的连接点上，线头才不会飘在半空；从多选区拉出同理，接在新节点左侧
        origin: group
          ? [0, 0.5]
          : connection
            ? connection.handleType === "source"
              ? [0, 0.5]
              : [1, 0.5]
            : [0, 0],
      };

      // 新节点直接选中：面板浮出来就能写提示词
      setNodes((nds) =>
        nds
          .map((item) => (item.selected ? { ...item, selected: false } : item))
          .concat({ ...node, selected: true }),
      );

      const from = connection && getNode(connection.nodeId);
      const connected =
        !!connection && !!from && canLinkFrom(from.data, connection.handleType, node.data);

      if (connection && connected) {
        connectNodes(connection.nodeId, connection.handleId, connection.handleType, id);
      }

      // 多选引用：接得上的各连一根线，接不上的跳过并告知，不静默丢
      if (group) {
        const sources = group.nodeIds.flatMap((nodeId) => {
          const source = getNode(nodeId);
          return source ? [{ id: nodeId, ...source.data }] : [];
        });
        const { linkable, skipped } = partitionLinkable(sources, node.data);
        for (const source of linkable) connectNodes(source.id, null, "source", id);
        if (skipped.length > 0) {
          toast.info(`${skipped.length} 个节点接不到${meta.label}上，已跳过`);
        }
      }

      return { id, connected };
    },
    [connectNodes, defaultModels, getNode, getNodes, setNodes],
  );

  const addNode = useCallback(
    (kind: NodeKind) => {
      if (!menu) return;

      placeNode(kind, menu);
      setMenu(null);
    },
    [menu, placeNode],
  );

  /** 不经过菜单，直接在画布坐标 flow 处建一个节点（底部工具条、空状态卡片用） */
  const addNodeAt = useCallback(
    (kind: NodeKind, flow: { x: number; y: number }, extra?: Partial<CanvasNodeData>) =>
      placeNode(kind, { screen: flowToScreenPosition(flow), flow, connection: null }, extra).id,
    [flowToScreenPosition, placeNode],
  );

  /** 不经过菜单的上传：先记下落点，文件框交给调用方弹 */
  const beginUploadAt = useCallback(
    (flow: { x: number; y: number }) => {
      uploadPlacement.current = { screen: flowToScreenPosition(flow), flow, connection: null };
    },
    [flowToScreenPosition],
  );

  /** 点了「上传」：记下落点、收起菜单，文件框交给调用方弹 */
  const beginUpload = useCallback(() => {
    uploadPlacement.current = menu;
    setMenu(null);
  }, [menu]);

  /**
   * 落一个收下的文件：先按类型建出节点挂上本地预览，再把文件传上去换成正式地址。
   * 回一份结果交给调用方汇总文案——多文件时每个都自说自话会刷屏。
   */
  const placeUploadedNode = useCallback(
    async (
      item: { file: File; mediaType: MediaType; src: string },
      placement: CanvasMenuState,
    ): Promise<{ uploaded: boolean; detached: boolean }> => {
      const kind = UPLOAD_TARGET_KIND[item.mediaType];
      const { id, connected } = placeNode(kind, placement, {
        status: "idle",
        src: item.src,
        mediaType: item.mediaType,
        uploaded: true,
        fileName: item.file.name,
      });
      const detached = !!placement.connection && !connected;

      try {
        const asset = await uploadAsset(item.file);
        releaseObjectUrl(item.src);
        setNodes((nodes) =>
          nodes.map((node) =>
            node.id === id
              ? {
                  ...node,
                  data: {
                    ...node.data,
                    status: "done",
                    src: asset.url,
                    assetId: asset.id,
                  },
                }
              : node,
          ),
        );
      } catch {
        setNodes((nodes) =>
          nodes.map((node) =>
            node.id === id
              ? {
                  ...node,
                  data: {
                    ...node.data,
                    status: "error",
                    error: "上传失败，请重试",
                  },
                }
              : node,
          ),
        );
        return { uploaded: false, detached };
      }

      return { uploaded: true, detached };
    },
    [placeNode, setNodes],
  );

  /**
   * 文件选好了（可能是好几个）：按文件类型各落成图片或视频节点，素材直接挂上去，
   * 节点以落点为起点按网格排开。有话要说时交回一句摆给用户看：
   * 文件不合规就不建那一个，类型接不上刚才那根线则节点照建、线不接。
   */
  const addUploadedNodes = useCallback(
    async (files: File[]): Promise<UploadNotice | null> => {
      const placement = uploadPlacement.current;
      uploadPlacement.current = null;
      if (!placement || files.length === 0) return null;

      // 先把不合规的挑出去，剩下的才按序号排位置，免得中间空出格子
      const taken: { file: File; mediaType: MediaType; src: string }[] = [];
      const rejected: string[] = [];
      for (const file of files) {
        const result = takeUploadFile(file);
        if ("error" in result) {
          rejected.push(result.error);
          continue;
        }
        taken.push({ file, ...result });
      }

      if (taken.length === 0) {
        return { tone: "error", text: rejected[0] ?? "没有能收下的文件" };
      }

      const results = await Promise.all(
        taken.map((item, index) =>
          placeUploadedNode(item, {
            ...placement,
            flow: {
              x: placement.flow.x + (index % UPLOAD_STACK_COLUMNS) * UPLOAD_STACK_GAP.x,
              y: placement.flow.y + Math.floor(index / UPLOAD_STACK_COLUMNS) * UPLOAD_STACK_GAP.y,
            },
          }),
        ),
      );

      // 三类话按轻重挑一句说：没收下的最要紧，其次传失败，最后才是没接上线
      if (rejected.length) {
        return {
          tone: "error",
          text:
            rejected.length === 1 ? rejected[0] : `${rejected.length} 个文件没收下：${rejected[0]}`,
        };
      }

      const failed = results.filter((result) => !result.uploaded).length;
      if (failed) {
        return {
          tone: "error",
          text:
            failed === 1 ? "上传失败，本地预览已保留" : `${failed} 个文件上传失败，本地预览已保留`,
        };
      }

      const detached = results.filter((result) => result.detached).length;
      if (detached) {
        const meta = NODE_META.get(UPLOAD_TARGET_KIND[taken[0].mediaType]) ?? NODE_LIBRARY[0];
        return {
          tone: "info",
          text:
            detached === 1
              ? `${meta.label}接不到刚才那根线上，已单独放下`
              : `${detached} 个节点接不到刚才那根线上，已单独放下`,
        };
      }

      return null;
    },
    [placeUploadedNode],
  );

  return {
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
  };
}
