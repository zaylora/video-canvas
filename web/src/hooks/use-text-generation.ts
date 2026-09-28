import { useCallback, useEffect, useRef } from "react";
import { useReactFlow } from "@xyflow/react";

// import { requestText } from "@/api/text";
import { useSettingsStore } from "@/store";
import type { CanvasNode, CanvasNodeData } from "@/types";

/**
 * 文本节点的生成：真去调模型，结果和失败原因都写回节点数据，
 * 于是折叠面板、拖动节点甚至重渲染都不会把已经出的正文弄丢。
 *
 * 内置清单里的模型只是演示用的名字，没有可调的地址；
 * 真要出东西得先在设置里把自己的文本服务接进来。
 */
export function useTextGeneration(id: string, data: CanvasNodeData) {
  const { updateNodeData } = useReactFlow<CanvasNode>();
  const customModels = useSettingsStore((state) => state.customModels);
  const abortRef = useRef<AbortController | null>(null);

  // 节点没了就把还在飞的请求掐掉，免得它回头往一个不存在的节点上写结果
  useEffect(() => () => abortRef.current?.abort(), []);

  const prompt = data.prompt ?? "";
  const modelId = data.model;

  return useCallback(async () => {
    const input = prompt.trim();
    if (!input) return;

    const model = customModels.find((item) => item.id === modelId);
    if (!model) {
      updateNodeData(id, {
        status: "error",
        error: "内置清单里的模型只是占位，先去设置里接入自己的文本服务",
      });
      return;
    }

    // 上一趟还没回来就又按了发送：旧的作废，只认最后这一次
    abortRef.current?.abort();
    const controller = new AbortController();
    abortRef.current = controller;

    updateNodeData(id, { status: "running", text: null, error: null });

    try {
      // const content = await requestText({
      //   model,
      //   prompt: input,
      //   signal: controller.signal,
      // });
      updateNodeData(id, {
        status: "done",
        text: "Generated text",
        error: null,
      });
    } catch (error) {
      // 自己撤销的那趟不算失败，状态归后来发起的那次管
      if (controller.signal.aborted) return;

      updateNodeData(id, {
        status: "error",
        error: error instanceof Error ? error.message : "生成失败",
      });
    } finally {
      if (abortRef.current === controller) abortRef.current = null;
    }
  }, [customModels, id, modelId, prompt, updateNodeData]);
}
