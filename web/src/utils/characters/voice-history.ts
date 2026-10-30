import type { TaskView } from "@/api/generation-task/type";
import type { CanvasNodeData, NodeCharacter } from "@/types";
import { formatShortTime } from "@/utils/time";

import { applyVoiceAudio, emptyCharacter } from "./node-character";
import { voiceTaskView, type VoiceTaskView } from "./voice-task";

/** 生成记录里提示词摘要的长度上限（字） */
export const PROMPT_EXCERPT_MAX = 40;

/** 详情页「生成记录」里的一行：一次声音生成任务 */
export type VoiceHistoryRow = {
  /** 任务 id（字符串） */
  taskId: string;
  /** 提示词摘要，任务没有提示词时为空串 */
  prompt: string;
  /** 模型展示名 */
  model: string;
  /** 提交时间，短格式「10-31 14:20」 */
  createdAt: string;
  /** 任务此刻的状态；成功时带产出的音频，失败时带原因 */
  state: Exclude<VoiceTaskView, { phase: "idle" }>;
  /** 产出的音频就是角色眼下的声音 */
  current: boolean;
};

/** voiceHistoryRows 的可选输入 */
export type VoiceHistoryOptions = {
  /** 角色眼下声音的素材 id，没有声音时不传 */
  voiceAssetId?: string;
  /** 按模型 key 查清单里的展示名，查不到返回 undefined */
  modelLabel?: (key: string) => string | undefined;
  /** 按任务 id 查任务库里的最新快照（WebSocket 推送进来的），没有返回 undefined */
  live?: (taskId: string) => TaskView | undefined;
};

/**
 * 提示词摘要：空白折成一个空格，超过上限按字符截断并加省略号。
 * @param text 提示词
 * @param max 上限（字）
 */
export function promptExcerpt(text: string, max = PROMPT_EXCERPT_MAX): string {
  const flat = text.replace(/\s+/g, " ").trim();
  const chars = [...flat];
  return chars.length > max ? `${chars.slice(0, max).join("")}…` : flat;
}

/**
 * 按节点查到的声音任务变成生成记录的行，保持接口给的顺序（新的在前）。
 * 任务库里有版本更新的快照时状态以它为准（列表拉下来之后任务还在跑），
 * 提示词、模型名只有列表接口才带，始终取列表里的。
 * @param views 按节点查到的任务
 * @param options 当前声音、模型名、任务库
 */
export function voiceHistoryRows(
  views: readonly TaskView[],
  options: VoiceHistoryOptions = {},
): VoiceHistoryRow[] {
  return views.map((listed) => {
    const taskId = String(listed.id);
    const live = options.live?.(taskId);
    const view = live && live.version > listed.version ? live : listed;
    const state = voiceTaskView(taskId, view) as VoiceHistoryRow["state"];
    return {
      taskId,
      prompt: promptExcerpt(listed.prompt ?? ""),
      model: listed.model_label || options.modelLabel?.(listed.model_id) || listed.model_id,
      createdAt: formatShortTime(listed.created_at),
      state,
      current:
        state.phase === "done" &&
        options.voiceAssetId !== undefined &&
        state.asset.id === options.voiceAssetId,
    };
  });
}

/**
 * 把一条生成记录的音频设为角色的声音（来源 generated）。不碰进行中的声音任务记录：
 * 那是另一次生成，结果回来时照常回填。已经是当前声音时返回 null，调用方不用写节点。
 * @param data 节点数据
 * @param asset 这条记录产出的音频
 * @returns 要写回节点的补丁
 */
export function setVoiceFromHistory(
  data: Pick<CanvasNodeData, "character" | "label">,
  asset: { id: string; url: string },
): { character: NodeCharacter } | null {
  const character = data.character ?? emptyCharacter(data.label);
  if (character.voice?.assetId === asset.id) return null;
  return { character: applyVoiceAudio(character, asset, "generated") };
}

/**
 * 把选好的音频模型记在角色数据上（character.voiceModel），节点卸载、刷新后都还在。
 * @param data 节点数据
 * @param key 音频模型 key
 * @returns 要写回节点的补丁
 */
export function withVoiceModel(
  data: Pick<CanvasNodeData, "character" | "label">,
  key: string,
): { character: NodeCharacter } {
  const character = data.character ?? emptyCharacter(data.label);
  return { character: { ...character, voiceModel: key } };
}
