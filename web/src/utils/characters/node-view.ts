import type { CanvasNodeData, NodeCharacter } from "@/types";
import { resolveNodeAspect } from "@/utils/canvas/node-frame";

/** 角色卡节点封面的展示状态 */
export type CharacterNodeView =
  | {
      /** 没有主图、也没有生成任务：封面是「上传」「素材库」两个入口 */
      phase: "empty";
      /** 底部状态标签 */
      tag: "待补充";
    }
  | {
      /** 有主图 */
      phase: "ready";
      /** 就绪没有标签 */
      tag: null;
    }
  | {
      /** 主图生成中（含排队、转存） */
      phase: "generating";
      tag: "生成中";
    }
  | {
      /** 主图生成失败：摆出原因和重试；不扣积分 */
      phase: "failed";
      tag: "生成失败";
      /** 失败原因，摆给用户看的那句 */
      reason: string;
    };

/** 能当主图显示的地址：blob 是上传还没落库时的本地预览，不算 */
const hasRealSrc = (src: string | null | undefined): boolean => !!src && !src.startsWith("blob:");

/**
 * 角色卡节点此刻的封面状态（设计 §6.1、§7）：生成中 > 失败 > 有主图就绪 > 空。
 * 新设计里角色数据就在节点上，不再有「读取中」「已失效」：空节点是正常的起点，不是异常。
 * @param data 节点数据
 */
export function characterNodeView(
  data: Pick<CanvasNodeData, "status" | "error" | "src" | "character">,
): CharacterNodeView {
  if (data.status === "running") return { phase: "generating", tag: "生成中" };
  if (data.status === "error") {
    return { phase: "failed", tag: "生成失败", reason: data.error || "生成失败" };
  }
  if (hasRealSrc(data.src)) return { phase: "ready", tag: null };
  return { phase: "empty", tag: "待补充" };
}

/**
 * 角色卡节点框的宽高比：和主图一样（三视图常是横图），规则同图片节点——
 * 量到的主图画幅 > 节点记下的主图画幅 > 参数面板所选比例 > 16:9。
 * @param input.measured 主图实际的宽 / 高；还没有主图或没量到为 null
 * @param input.declared 节点记下的主图画幅（data.mediaAspect）
 * @param input.selected 参数面板所选比例对应的宽 / 高
 * @returns 宽 / 高
 */
export function characterNodeAspect(input: {
  measured: number | null;
  declared: number | null | undefined;
  selected: number | null;
}): number {
  return resolveNodeAspect({ ...input, mediaType: "image" });
}

/** 封面下方声音格的内容 */
export type VoiceCell =
  | {
      /** 还没有声音 */
      kind: "none";
      title: string;
    }
  | {
      /** 正在生成声音（任务在跑） */
      kind: "generating";
      title: string;
    }
  | {
      /** 有声音：可试听，连到视频时会展开成参考音频 */
      kind: "audio";
      title: string;
      /** 连着的目标模型不收参考音频，声音在那里不生效 */
      ignored: boolean;
    };

/**
 * 声音格显示什么：生成中 > 有声音 > 没有。
 * 目标模型不收参考音频时标「声音未生效」，只带形象、不阻断。
 * @param character 角色数据
 * @param ignored 连着的某个目标收图片却不收参考音频
 * @param generating 这个节点正在生成声音
 */
export function voiceCell(
  character: NodeCharacter,
  ignored: boolean,
  generating = false,
): VoiceCell {
  if (generating) return { kind: "generating", title: "声音生成中…" };
  if (!character.voice) return { kind: "none", title: "还没有声音，点开详情设置" };
  return {
    kind: "audio",
    title: ignored ? "声音（目标模型不收参考音频，声音未生效）" : "声音",
    ignored,
  };
}
