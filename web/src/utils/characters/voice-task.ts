import type { TaskView } from "@/api/generation-task/type";
import type { Capabilities, ModelInfo } from "@/api/model/type";
import type { CanvasNodeData, NodeCharacter } from "@/types";
import { isGenerating } from "@/utils/canvas/outputs";
import { buildTaskInput, toAssetNumber } from "@/utils/tasks/capabilities";
import { canceledRefunded, isTerminalStatus } from "@/utils/tasks/status";

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
  /** refunded：积分有没有退回（排队中取消退，已进入生成取消不退） */
  | { phase: "canceled"; refunded: boolean }
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
  if (view.status === "canceled") return { phase: "canceled", refunded: canceledRefunded(view) };
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

/**
 * 生成声音用哪个音频模型：节点上记着的（character.voiceModel）还在清单里就用它，
 * 没记过、或已经下架，取清单第一个。
 * @param models 音频模型清单
 * @param key 节点上记着的模型 key
 * @returns 清单为空时为 undefined
 */
export function pickVoiceModel<T extends Pick<ModelInfo, "key">>(
  models: readonly T[],
  key: string | undefined,
): T | undefined {
  return models.find((item) => item.key === key) ?? models[0];
}

/** 组装好的提交内容；error 非空时不能提交 */
export type VoiceInput = { input: Record<string, unknown>; error: string | null };

/**
 * 按所选音频模型的能力组装提交内容。详情页没有参数面板，所以只提交提示词；
 * 模型收参考图时再带上角色主图，让声音贴合形象，不收就不带。
 * 开放给用户又没有默认值的必填参数会在这里报缺什么。
 * @param caps 音频模型的能力，清单还没到时为 undefined
 * @param prompt 提示词（音色描述加一句试音台词）
 * @param imageAssetId 角色主图的素材 id；没有主图（或主图还是本地文件）时不参考
 */
export function buildVoiceInput(
  caps: Capabilities | undefined,
  prompt: string,
  imageAssetId?: string,
): VoiceInput {
  if (!caps) return { input: {}, error: "音频模型还没加载好" };
  const image = toAssetNumber(imageAssetId);
  const params: Record<string, unknown> = { prompt: prompt.trim() };
  // 手动参考素材的通道：模型不收图片时 buildTaskInput 自己会略过
  if (image !== null) params.images = [image];
  const built = buildTaskInput(caps, params);
  const error = Object.values(built.errors)[0] ?? null;
  return { input: built.input, error };
}

/** 「生成（含音色）」出图后的下一步：接着等、用新主图生成音色、或放弃 */
export type VoiceAfterImageStep =
  | { kind: "wait" }
  | { kind: "submit"; imageAssetId: string }
  | { kind: "abort"; reason: string };

/**
 * 节点记着「出图后生成音色」（voiceAfterImage）时，此刻该做什么。
 * 只认这次提交的任务出的图：形象失败、被取消（节点退回旧主图）都放弃，不拿旧图生成音色。
 * @param data 节点数据
 * @returns 没有记着时为 null
 */
export function voiceAfterImageStep(
  data: Pick<
    CanvasNodeData,
    "voiceAfterImage" | "status" | "pendingTaskIds" | "assetId" | "outputs" | "taskId"
  >,
): VoiceAfterImageStep | null {
  const taskIds = data.voiceAfterImage;
  if (!taskIds?.length) return null;
  if (isGenerating(data)) return { kind: "wait" };
  const current = data.outputs?.find((output) => output.assetId === data.assetId);
  if (data.status === "done" && data.assetId && current?.taskId && taskIds.includes(current.taskId))
    return { kind: "submit", imageAssetId: data.assetId };
  return { kind: "abort", reason: "形象没有生成成功，音色未生成" };
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
