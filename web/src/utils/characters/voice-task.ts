import type { TaskView } from "@/api/generation-task/type";
import type { Capabilities } from "@/api/model/type";
import type { CanvasNodeData, NodeCharacter } from "@/types";
import { buildTaskInput } from "@/utils/tasks/capabilities";
import { isTerminalStatus } from "@/utils/tasks/status";

import { applyVoiceAudio } from "./node-character";

/**
 * 角色声音生成任务的 node_id 前缀：任务绑的不是画布上的节点，而是 voice:{角色卡节点id}，
 * 和短剧的 seg:、首页的 rec: 一样靠前缀区分（后端不校验 node_id）。
 */
export const VOICE_NODE_PREFIX = "voice:";

/** 某个角色卡节点的声音任务 node_id */
export const voiceNodeId = (nodeId: string): string => `${VOICE_NODE_PREFIX}${nodeId}`;

/** 节点上记着的声音任务此刻的样子 */
export type VoiceTaskView =
  | { phase: "idle" }
  | { phase: "generating"; progress: number | null }
  | { phase: "failed"; reason: string }
  | { phase: "canceled" }
  | { phase: "done"; asset: { id: string; url: string } };

/**
 * 节点记着的声音任务（voiceTaskId）对应哪种状态。
 * 记着任务却还没有快照（刚提交、刷新后等对账）按生成中算，对账确认任务不存在时由调用方清掉记录。
 * 成功必须带一份音频产出才算完成，否则按失败：不凭空绑定。
 * @param taskId 节点上的 voiceTaskId
 * @param view 任务库里这个任务的最新快照
 */
export function voiceTaskView(
  taskId: string | undefined,
  view: TaskView | undefined,
): VoiceTaskView {
  if (!taskId) return { phase: "idle" };
  if (!view || !isTerminalStatus(view.status)) {
    return { phase: "generating", progress: view?.progress ?? null };
  }
  if (view.status === "canceled") return { phase: "canceled" };
  if (view.status === "succeeded") {
    const audio = view.outputs?.find(
      (output) =>
        output.media_type === "audio" &&
        output.asset_id !== undefined &&
        output.asset_id !== null &&
        output.url,
    );
    if (audio) return { phase: "done", asset: { id: String(audio.asset_id), url: audio.url! } };
    return { phase: "failed", reason: "没有生成出音频" };
  }
  return { phase: "failed", reason: view.error_message || "生成失败" };
}

/** 组装好的提交内容；error 非空时不能提交 */
export type VoiceInput = { input: Record<string, unknown>; error: string | null };

/**
 * 按所选音频模型的能力组装提交内容。详情页没有参数面板，所以只提交提示词；
 * 开放给用户又没有默认值的必填参数会在这里报缺什么。
 * @param caps 音频模型的能力，清单还没到时为 undefined
 * @param prompt 提示词（音色描述加一句试音台词）
 */
export function buildVoiceInput(caps: Capabilities | undefined, prompt: string): VoiceInput {
  if (!caps) return { input: {}, error: "音频模型还没加载好" };
  const built = buildTaskInput(caps, { prompt: prompt.trim() });
  const error = Object.values(built.errors)[0] ?? null;
  return { input: built.input, error };
}

/**
 * 声音任务成功后写回节点的补丁：音频绑成角色的声音（来源 generated），并清掉任务记录。
 * voiceTaskId 显式写成 undefined，updateNodeData 合并后字段即被去掉。
 * 节点没有角色数据（异常数据）时只清记录。
 * @param data 节点数据
 * @param asset 任务产出的音频
 */
export function applyVoiceResult(
  data: Pick<CanvasNodeData, "character">,
  asset: { id: string; url: string },
): { character?: NodeCharacter; voiceTaskId: undefined } {
  if (!data.character) return { voiceTaskId: undefined };
  return { character: applyVoiceAudio(data.character, asset, "generated"), voiceTaskId: undefined };
}
