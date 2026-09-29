import { useCallback, useEffect, useMemo, useState } from "react";
import {
  Position,
  useNodeConnections,
  useNodesData,
  useReactFlow,
} from "@xyflow/react";

import type { ModelInfo } from "@/api/model/type";
import type { NodeCardHandle } from "@/components/canvas";
import type { AssetChoice } from "@/components/canvas/video-param-panel";
import { useNow } from "@/hooks/use-now";
import { useRemoteModels } from "@/hooks/use-models";
import { useVideoGeneration } from "@/hooks/use-video-generation";
import { useCreditsStore } from "@/store/credits";
import { useTask } from "@/store/tasks";
import type { CanvasEdge, CanvasNode, CanvasNodeData, ParamAsset } from "@/types";
import {
  buildTaskInput,
  computeHandleFixes,
  portFields,
  readParams,
  resolveBindings,
  switchModelParams,
  type IncomingLink,
} from "@/utils/tasks/input-schema";
import { deriveVideoNodeView } from "@/utils/tasks/node-view";

/** 切换模型时要用户确认的那次切换 */
export type PendingModelSwitch = {
  key: string;
  droppedLabels: string[];
};

/**
 * 视频节点的全部业务状态：模型清单与下线判断、schema 驱动的参数、上游连线绑定、
 * 提交 / 取消 / 重试、展示状态。节点正文和下方的提示词面板共用一份，
 * 所以在始终挂载的节点组件里调用一次，再分发下去。
 */
export function useVideoNode(id: string, data: CanvasNodeData) {
  const { updateNodeData, setEdges, getNodes } = useReactFlow<CanvasNode, CanvasEdge>();
  const remote = useRemoteModels("video");
  const generation = useVideoGeneration(id);
  const availableCredits = useCreditsStore((state) => state.credits?.available ?? null);
  const [submitError, setSubmitError] = useState<string | null>(null);
  const [pendingSwitch, setPendingSwitch] = useState<PendingModelSwitch | null>(null);

  // ---- 模型：清单成功加载后找不到才算下线，加载中 / 加载失败都不能误判 ----
  const { models, status: modelsStatus } = remote;
  const modelKey = data.model ?? models[0]?.key;
  const model: ModelInfo | undefined = models.find((item) => item.key === modelKey);
  const offline = modelsStatus === "ready" && !!modelKey && !model;
  const schema = model?.input_schema;
  const hasPromptField = schema ? schema.prompt?.type === "text" : true;

  // ---- 参数与上游连线 ----
  const params = useMemo(() => readParams(data), [data]);
  const connections = useNodeConnections({ id, handleType: "target" });
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
          text: generating ? undefined : (source.text ?? source.prompt),
        },
      ];
    });
  }, [connections, upstream]);
  const bindings = useMemo(() => resolveBindings(schema, links), [schema, links]);
  const built = useMemo(
    () => buildTaskInput(schema, params, bindings),
    [schema, params, bindings],
  );

  // 连线落点和实际绑定的输入口对齐；换模型后失效的口也在这里收拾，免得线被 xyflow 藏掉
  const fixes = useMemo(() => computeHandleFixes(schema, links), [schema, links]);
  useEffect(() => {
    if (Object.keys(fixes).length === 0) return;
    setEdges((edges) =>
      edges.map((edge) =>
        edge.id in fixes ? { ...edge, targetHandle: fixes[edge.id] } : edge,
      ),
    );
  }, [fixes, setEdges]);

  // 输入口：schema 里每个 port 字段一个；清单还没到时先沿用连线上已有的口，线不会闪没
  const handles = useMemo<NodeCardHandle[]>(() => {
    const ports = schema
      ? portFields(schema).map((field) => ({ id: field.name, label: field.label }))
      : [...new Set(links.map((link) => link.targetHandle).filter((h): h is string => !!h))].map(
          (handle) => ({ id: handle, label: undefined as string | undefined }),
        );
    const inputs: NodeCardHandle[] =
      ports.length === 0
        ? [{ type: "target", position: Position.Left }]
        : ports.map((port, index) => ({
            type: "target",
            position: Position.Left,
            id: port.id,
            label: port.label,
            top: ports.length > 1 ? `${((index + 1) / (ports.length + 1)) * 100}%` : undefined,
            compact: ports.length > 1,
          }));
    return [...inputs, { type: "source", position: Position.Right }];
  }, [links, schema]);

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
  const submit = useCallback(
    async () => {
      if (blockedReason || !model) return;
      setSubmitError(null);
      const outcome = await generation.submit({
        modelKey: model.key,
        input: built.input,
        currentSrc: data.src,
      });
      if (outcome.ok) return;
      setSubmitError(outcome.error.message);
    },
    [blockedReason, built.input, data.src, generation, model],
  );

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

  const applyModel = useCallback(
    (key: string) => {
      const next = models.find((item) => item.key === key);
      if (!next) return;
      setSubmitError(null);
      const switched = switchModelParams(schema, next.input_schema, readParams(data));
      const keptNames = new Set(Object.keys(switched.params));
      const assets = Object.fromEntries(
        Object.entries(data.paramAssets ?? {}).filter(([name]) => keptNames.has(name)),
      );
      updateNodeData(id, {
        model: key,
        params: switched.params,
        paramAssets: assets,
        // 提示词只在新模型还有 prompt 字段时才继续用；旧字段保持兼容
        prompt: typeof switched.params.prompt === "string" ? switched.params.prompt : data.prompt,
      });
    },
    [data, id, models, schema, updateNodeData],
  );

  /** 换模型：会丢参数时先挂起等用户确认，不丢就直接换 */
  const setModel = useCallback(
    (key: string) => {
      const next = models.find((item) => item.key === key);
      if (!next || key === modelKey) return;
      const switched = switchModelParams(schema, next.input_schema, readParams(data));
      if (switched.droppedLabels.length > 0) {
        setPendingSwitch({ key, droppedLabels: switched.droppedLabels });
        return;
      }
      applyModel(key);
    },
    [applyModel, data, modelKey, models, schema],
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
    // 参数
    schema,
    hasPromptField,
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

export type VideoNodeModel = ReturnType<typeof useVideoNode>;
