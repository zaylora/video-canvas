import { useCallback, useEffect, useMemo, useState } from "react";
import {
  Position,
  addEdge,
  useNodeConnections,
  useNodesData,
  useReactFlow,
  useStore,
} from "@xyflow/react";

import type { GenerationOp, ModelInfo } from "@/api/model/type";
import type { NodeCardHandle } from "@/components/canvas";
import type { PromptMentionSource, RefSource } from "@/components/canvas/prompt-mention";
import type { ManualRef, RefItem, UploadKind } from "@/components/canvas/ref-strip";
import type { AssetChoice } from "@/components/canvas/video-param-panel";
import { useCanvasHistoryContext } from "@/hooks/use-canvas-history";
import { useNow } from "@/hooks/use-now";
import { useRemoteModels } from "@/hooks/use-models";
import type { TaskGeneration, TaskNodeKind } from "@/hooks/use-task-generation";
import { ANIMATED_EDGE_OPTIONS, REMOTE_KIND_OF_NODE } from "@/constants/canvas";
import { useCreditsStore } from "@/store/credits";
import { useTask } from "@/store/tasks";
import type { CanvasEdge, CanvasNode, CanvasNodeData, ParamAsset } from "@/types";
import { mentionableNodes, opForLink, unlinkSource } from "@/utils/canvas/link-rule";
import { removePromptRef } from "@/utils/canvas/prompt-tokens";
import { isSourceEdge } from "@/utils/canvas/source-edge";
import { referencedText } from "@/utils/canvas/text-body";
import {
  REF_KEYS,
  PORT_OF_KIND,
  buildTaskInput,
  currentOp,
  hasImageRefs,
  isAutoOp,
  legacyHandleFixes,
  manualRefs,
  priceSpecOf,
  readParams,
  refKindsOf,
  refPanelOp,
  resolveBindings,
  switchModelParams,
  type IncomingLink,
  type RefKey,
} from "@/utils/tasks/capabilities";
import { deriveVideoNodeView } from "@/utils/tasks/node-view";
import { fanoutCount, quote } from "@/utils/pricing/quote";

/** 节点的连接点：左进右出各一个 */
const SINGLE_HANDLES: NodeCardHandle[] = [
  { type: "target", position: Position.Left },
  { type: "source", position: Position.Right },
];

/** 画布节点摊成引用条 / @ 菜单认的素材信息 */
const toRefSource = (node: Pick<CanvasNode, "id" | "data">): RefSource => ({
  id: node.id,
  kind: node.data.kind,
  label: node.data.label,
  src: node.data.src,
  mediaType: node.data.mediaType,
  text: node.data.text,
});

/** 切换模型时要用户确认的那次切换 */
export type PendingModelSwitch = {
  key: string;
  droppedLabels: string[];
};

/**
 * 生成任务节点的全部业务状态：模型清单与下线判断、按模型能力（capabilities）驱动的生成方式与参数、上游连线绑定、
 * 提交 / 取消 / 重试、展示状态。节点正文和下方的提示词面板共用一份，
 * 所以在始终挂载的节点组件里调用一次，再分发下去。
 */
export function useTaskNode(
  id: string,
  data: CanvasNodeData,
  nodeKind: TaskNodeKind,
  generation: TaskGeneration,
) {
  const { updateNodeData, setEdges, getNodes, getEdges } = useReactFlow<CanvasNode, CanvasEdge>();
  const history = useCanvasHistoryContext();
  const remote = useRemoteModels(REMOTE_KIND_OF_NODE[nodeKind]);
  const availableCredits = useCreditsStore((state) => state.credits?.available ?? null);
  const [submitError, setSubmitError] = useState<string | null>(null);
  const [pendingSwitch, setPendingSwitch] = useState<PendingModelSwitch | null>(null);

  // ---- 模型：清单成功加载后找不到才算下线，加载中 / 加载失败都不能误判 ----
  const { models, status: modelsStatus } = remote;
  const modelKey = data.model ?? models[0]?.key;
  const model: ModelInfo | undefined = models.find((item) => item.key === modelKey);
  const offline = modelsStatus === "ready" && !!modelKey && !model;
  const caps = model?.capabilities;

  // ---- 参数、生成方式与上游连线 ----
  const params = useMemo(() => readParams(data), [data]);
  // 来源线只是派生关系，不是引用：从连线里去掉，不进引用条，也不进提交的素材
  const edgeLookup = useStore((state) => state.edgeLookup);
  const allConnections = useNodeConnections({ id, handleType: "target" });
  const connections = useMemo(
    () => allConnections.filter((item) => !isSourceEdge(edgeLookup.get(item.edgeId) ?? {})),
    [allConnections, edgeLookup],
  );
  const upstream = useNodesData<CanvasNode>(connections.map((item) => item.source));
  const links = useMemo<IncomingLink[]>(() => {
    const byId = new Map(upstream.map((node) => [node.id, node.data]));
    return connections.flatMap((connection) => {
      const source = byId.get(connection.source);
      if (!source) return [];
      const generating = source.status === "running";
      return [
        {
          edgeId: connection.edgeId,
          sourceId: connection.source,
          sourceKind: source.kind,
          sourceLabel: source.fileName ? `${source.label}（${source.fileName}）` : source.label,
          targetHandle: connection.targetHandle ?? null,
          assetId: generating ? undefined : source.assetId,
          text: referencedText(source),
        },
      ];
    });
  }, [connections, upstream]);
  // 图片模型不让用户选生成方式：连着图片或手动加了参考图就是图生图，否则文生图
  const hasImageRef = useMemo(
    () => hasImageRefs(params, { images: links.filter((link) => link.sourceKind === "image") }),
    [links, params],
  );
  const op = useMemo(() => currentOp(caps, params, hasImageRef), [caps, params, hasImageRef]);
  const autoOp = isAutoOp(caps);
  // 自动切换时文生图状态下也要摆出参考图入口，用户加了图才会转成图生图
  const refOp = refPanelOp(caps, op);
  const refKinds = useMemo(() => refKindsOf(caps, refOp), [caps, refOp]);
  const bindings = useMemo(() => resolveBindings(caps, op, links), [caps, op, links]);
  const built = useMemo(
    () => buildTaskInput(caps, params, bindings, op, links),
    [caps, params, bindings, op, links],
  );

  // ---- 引用条与 @ 素材（设计稿 6.7） ----
  /** 接进来的全部上游，按连线先后；当前生成方式用不上的标出来 */
  const refItems = useMemo<RefItem[]>(() => {
    const byId = new Map(upstream.map((node) => [node.id, node]));
    const seen = new Set<string>();
    return connections.flatMap((connection) => {
      const node = byId.get(connection.source);
      if (!node || seen.has(node.id)) return [];
      seen.add(node.id);
      const port = PORT_OF_KIND[node.data.kind];
      return [
        {
          ...toRefSource(node),
          used: !caps || port === "text" || refKindsOf(caps, op).includes(port),
          running: node.data.status === "running",
        },
      ];
    });
  }, [caps, connections, op, upstream]);
  const linkedIds = useMemo(() => new Set(refItems.map((item) => item.id)), [refItems]);

  /** 从画布里接一根线进来；@ 时顺手连的线和正在打的字算同一步撤销 */
  const linkSource = useCallback(
    (source: RefSource, withTyping = false) => {
      if (getEdges().some((edge) => edge.source === source.id && edge.target === id)) return;
      if (withTyping) history?.absorbTyping();
      // 当前方式收不下这种素材（比如文生视频）就切到全能参考之类收得下的方式
      const sourceNode = getNodes().find((node) => node.id === source.id);
      const self = getNodes().find((node) => node.id === id);
      const nextOp = sourceNode && self && opForLink(sourceNode.data, self.data);
      if (nextOp) {
        updateNodeData(id, (node) => ({ params: { ...node.data.params, op: nextOp } }));
      }
      setEdges((edges) =>
        addEdge(
          {
            source: source.id,
            target: id,
            sourceHandle: null,
            targetHandle: null,
            ...ANIMATED_EDGE_OPTIONS,
          },
          edges,
        ),
      );
    },
    [getEdges, getNodes, history, id, setEdges, updateNodeData],
  );

  /**
   * 引用条上点 ×：断开这个上游，提示词里引用它的 chip 一起删掉。
   * 两处改动在同一次渲染里落地，撤销栈里只算一步，⌘Z 线和 chip 一起回来。
   */
  const unlink = useCallback(
    (sourceId: string) => {
      setEdges((edges) => unlinkSource(edges, sourceId, id));
      updateNodeData(id, (node) => {
        const prompt = readParams(node.data).prompt;
        if (typeof prompt !== "string") return {};
        const next = removePromptRef(prompt, sourceId);
        return next === prompt
          ? {}
          : { prompt: next, params: { ...node.data.params, prompt: next } };
      });
    },
    [id, setEdges, updateNodeData],
  );

  /** 画布里能 @ 的素材，打开菜单时才取，不订阅 */
  const listMentionables = useCallback(() => {
    const { linked, canvas } = mentionableNodes(id, getNodes(), getEdges());
    return { linked: linked.map(toRefSource), canvas: canvas.map(toRefSource) };
  }, [getEdges, getNodes, id]);

  const mention = useMemo<PromptMentionSource>(
    () => ({
      list: listMentionables,
      link: (source) => linkSource(source, true),
      linkedIds,
    }),
    [linkSource, linkedIds, listMentionables],
  );

  /** 手动上传的参考素材，只列当前生成方式收的种类 */
  const manualRefItems = useMemo<ManualRef[]>(
    () =>
      REF_KEYS.filter((ref) => refKinds.includes(ref.kind)).flatMap((ref) =>
        manualRefs(params, ref.key).map((assetId) => ({
          key: ref.key,
          assetId: String(assetId),
          asset: data.paramAssets?.[String(assetId)],
        })),
      ),
    [data.paramAssets, params, refKinds],
  );
  const uploadKinds = useMemo<UploadKind[]>(
    () =>
      caps
        ? REF_KEYS.filter((ref) => refKinds.includes(ref.kind)).map((ref) => ({
            key: ref.key,
            kind: ref.kind,
            label: ref.label,
            maxMb: caps.refs[ref.kind].max_mb,
          }))
        : [],
    [caps, refKinds],
  );

  // ---- 本地计价：每个任务的积分 × 生成数量；只用于显示，下单以后端算的为准 ----
  const price = useMemo(() => {
    if (!model) return null;
    const spec = priceSpecOf(caps, built.input);
    const one = quote(model.pricing, caps, spec);
    const count = fanoutCount(caps, spec.params);
    const billing = model.pricing?.billing;
    const unit =
      billing === "per_second"
        ? `${one / Math.max(1, Number(spec.params.duration) || 1)} 积分/秒 × ${spec.params.duration} 秒`
        : `${one} 积分`;
    return {
      one,
      count,
      total: one * count,
      isMax: billing === "token",
      detail:
        billing === "token" ? "按 Token 预估上限" : count > 1 ? `${unit} × ${count} 个` : unit,
    };
  }, [built.input, caps, model]);

  // 节点只有一个输入口：旧画布里挂在具名口上的线改回默认口，免得 xyflow 找不到 handle 把线藏掉
  const fixes = useMemo(() => legacyHandleFixes(links), [links]);
  useEffect(() => {
    if (Object.keys(fixes).length === 0) return;
    setEdges((edges) =>
      edges.map((edge) => (edge.id in fixes ? { ...edge, targetHandle: null } : edge)),
    );
  }, [fixes, setEdges]);

  // 左边一个输入口、右边一个输出口；接进来的线做什么用由 resolveBindings 按上游种类决定
  const handles = SINGLE_HANDLES;

  // ---- 展示状态 ----
  const running = data.status === "running";
  const task = useTask(data.taskId);
  const now = useNow(running);
  const view = deriveVideoNodeView(data, task, now);

  // ---- 能不能生成，不能的话为什么 ----
  const promptBinding = bindings.prompt;
  const firstError = Object.values(built.errors)[0];
  const blockedReason: string | null = running
    ? "生成中"
    : generation.submitting
      ? "正在提交"
      : modelsStatus === "idle" || modelsStatus === "loading"
        ? "正在加载模型清单"
        : modelsStatus === "error"
          ? "模型清单加载失败，请稍后重试"
          : offline
            ? "该模型已下线，请换一个模型"
            : !model
              ? "暂无可用模型"
              : (firstError ?? null);

  /** 提交失败就地提示在提示词面板里，全局错误 toast 由请求层统一弹出 */
  const submit = useCallback(async () => {
    if (blockedReason || !model) return;
    setSubmitError(null);
    const outcome = await generation.submit({
      modelKey: model.key,
      input: built.input,
      currentSrc: data.src,
      count: price?.count ?? 1,
      // Token 计费的预估不含固定系统提示（画布拿不到），和后端本来就会不同，不比对
      expectedCredits: price && !price.isMax ? price.one : undefined,
    });
    if (outcome.ok) return;
    setSubmitError(outcome.error.message);
  }, [blockedReason, built.input, data.src, generation, model, price]);

  const cancel = useCallback(() => {
    if (data.taskId) void generation.cancel(data.taskId);
  }, [data.taskId, generation]);

  // ---- 编辑 ----
  const setPrompt = useCallback(
    (prompt: string) => {
      setSubmitError(null);
      updateNodeData(id, (node) => ({
        prompt,
        params: { ...node.data.params, prompt },
      }));
    },
    [id, updateNodeData],
  );

  const setParam = useCallback(
    (name: string, value: unknown, asset?: ParamAsset | null) => {
      setSubmitError(null);
      updateNodeData(id, (node) => {
        const nextParams = { ...node.data.params };
        if (value === undefined) delete nextParams[name];
        else nextParams[name] = value;
        const patch: Partial<CanvasNodeData> = { params: nextParams };
        if (asset !== undefined) {
          const assets = { ...node.data.paramAssets };
          if (asset === null) delete assets[name];
          else assets[name] = asset;
          patch.paramAssets = assets;
        }
        return patch;
      });
    },
    [id, updateNodeData],
  );

  /** 切换生成方式；新方式不接收的素材连线保留，只是提交时忽略 */
  const setOp = useCallback(
    (next: GenerationOp) => {
      setSubmitError(null);
      updateNodeData(id, (node) => ({ params: { ...node.data.params, op: next } }));
    },
    [id, updateNodeData],
  );

  /** 手动添加一个参考素材（上传或选画布素材）；已有同一个素材时不重复添加 */
  const addRef = useCallback(
    (key: RefKey, assetId: string | number, asset: ParamAsset) => {
      setSubmitError(null);
      updateNodeData(id, (node) => {
        const existing = manualRefs(node.data.params ?? {}, key).map(String);
        const nextId = String(assetId);
        return {
          params: {
            ...node.data.params,
            [key]: existing.includes(nextId) ? existing : [...existing, nextId],
          },
          paramAssets: { ...node.data.paramAssets, [nextId]: asset },
        };
      });
    },
    [id, updateNodeData],
  );

  /** 移除一个手动添加的参考素材 */
  const removeRef = useCallback(
    (key: RefKey, assetId: string | number) => {
      updateNodeData(id, (node) => {
        const target = String(assetId);
        const rest = manualRefs(node.data.params ?? {}, key)
          .map(String)
          .filter((value) => value !== target);
        const assets = { ...node.data.paramAssets };
        delete assets[target];
        const params = { ...node.data.params };
        if (rest.length > 0) params[key] = rest;
        else delete params[key];
        return { params, paramAssets: assets };
      });
    },
    [id, updateNodeData],
  );

  const applyModel = useCallback(
    (key: string) => {
      const next = models.find((item) => item.key === key);
      if (!next) return;
      setSubmitError(null);
      const switched = switchModelParams(caps, next.capabilities, readParams(data));
      // 素材展示信息只留给还在用的手动素材
      const keptIds = new Set(
        (["images", "videos", "audios"] as const).flatMap((key) =>
          manualRefs(switched.params, key).map(String),
        ),
      );
      const assets = Object.fromEntries(
        Object.entries(data.paramAssets ?? {}).filter(([assetId]) => keptIds.has(assetId)),
      );
      updateNodeData(id, {
        model: key,
        params: switched.params,
        paramAssets: assets,
        prompt: typeof switched.params.prompt === "string" ? switched.params.prompt : data.prompt,
      });
    },
    [data, id, models, caps, updateNodeData],
  );

  /** 换模型：会丢参数时先挂起等用户确认，不丢就直接换 */
  const setModel = useCallback(
    (key: string) => {
      const next = models.find((item) => item.key === key);
      if (!next || key === modelKey) return;
      const switched = switchModelParams(caps, next.capabilities, readParams(data));
      if (switched.droppedLabels.length > 0) {
        setPendingSwitch({ key, droppedLabels: switched.droppedLabels });
        return;
      }
      applyModel(key);
    },
    [applyModel, data, modelKey, models, caps],
  );

  const confirmSwitch = useCallback(() => {
    if (pendingSwitch) applyModel(pendingSwitch.key);
    setPendingSwitch(null);
  }, [applyModel, pendingSwitch]);

  const cancelSwitch = useCallback(() => setPendingSwitch(null), []);

  /** 画布里已有的素材，给参数面板「选择画布素材」用（打开时才取，不订阅） */
  const listAssets = useCallback(
    (type: "image" | "video" | "audio"): AssetChoice[] =>
      getNodes()
        .filter(
          (node) =>
            node.id !== id &&
            !!node.data.assetId &&
            !!node.data.src &&
            node.data.mediaType === type,
        )
        .map((node) => ({
          assetId: node.data.assetId as string,
          url: node.data.src as string,
          label: node.data.fileName ?? `${node.data.label} #${node.data.assetId}`,
          mediaType: type,
        })),
    [getNodes, id],
  );

  return {
    // 模型
    modelOptions: remote.options,
    modelsStatus,
    reloadModels: remote.reload,
    modelKey,
    model,
    offline,
    setModel,
    pendingSwitch,
    confirmSwitch,
    cancelSwitch,
    // 引用条与 @ 素材
    refItems,
    manualRefItems,
    uploadKinds,
    mention,
    linkSource,
    unlink,
    // 参数
    caps,
    op,
    /** 生成方式由有没有图片引用决定，面板不再让用户选 */
    autoOp,
    /** 摆素材口时用的生成方式，自动切换的模型始终按图生图算 */
    refOp,
    setOp,
    refKinds,
    addRef,
    removeRef,
    params,
    bindings,
    promptBinding,
    errors: built.errors,
    setPrompt,
    setParam,
    listAssets,
    // 连接点
    handles,
    // 提交
    price,
    availableCredits,
    blockedReason,
    submit,
    submitting: generation.submitting,
    submitError,
    // 展示与取消
    running,
    view,
    cancel,
    cancelling: generation.cancelling,
  };
}

export type TaskNodeModel = ReturnType<typeof useTaskNode>;
