import type { ModelInfo, RefKind } from "@/api/model/type";
import { fanoutCount, quote } from "@/utils/pricing/quote";
import {
  acceptableRefKinds,
  buildTaskInput,
  OP_LABEL,
  priceSpecOf,
  REF_KEYS,
  toAssetNumber,
} from "@/utils/tasks/capabilities";

/** 输入卡片里的一份参考素材（图片、视频或音频） */
export type ComposerRef = {
  /** 素材种类 */
  kind: RefKind;
  /** 本地唯一标识：上传期间还没有素材 ID 时用它区分 */
  id: string;
  /** 素材 ID（十进制数字串），上传完成后才有 */
  assetId?: string;
  /** 素材地址，用来画缩略图 */
  url?: string;
  /** 文件名 */
  name: string;
  /** 上传状态：上传中、已完成、失败 */
  status: "uploading" | "done" | "error";
};

/** 判断能不能发送所需的现状 */
export type SendArgs = {
  /** 当前选中的模型；没有可用模型时为 undefined */
  model: ModelInfo | undefined;
  /** 输入框里的提示词 */
  text: string;
  /** 参数面板里的取值（含生成方式 op、生成数量等），没填的取模型默认值 */
  params: Record<string, unknown>;
  /** 参考图 */
  refs: ComposerRef[];
  /** 可用积分；还没拿到为 null */
  available: number | null;
  /** 是否正在发送：发送期间不能重复提交 */
  sending: boolean;
};

/** 发送前的判断结果 */
export type SendState = {
  /** 能不能发送 */
  canSend: boolean;
  /** 不能发送时要告诉用户的原因；null 表示不需要说（例如还没写提示词） */
  hint: string | null;
  /** 提示的语气：warn 是还可以补救的，bad 是需要处理的 */
  tone: "warn" | "bad" | null;
  /** 提交给后端的 input */
  input: Record<string, unknown>;
  /** 生成数量，也是结果格子数 */
  count: number;
  /** 每个任务冻结的积分 */
  creditsEach: number;
  /** 本次一共冻结的积分 */
  creditsTotal: number;
};

const blocked = (
  hint: string | null,
  tone: SendState["tone"],
  rest: Partial<SendState> = {},
): SendState => ({
  canSend: false,
  hint,
  tone: hint ? tone : null,
  input: {},
  count: 1,
  creditsEach: 0,
  creditsTotal: 0,
  ...rest,
});

/**
 * 发送前的全部判断：按模型能力组装 input、算出生成数量和积分，并给出不能发送的原因。
 * 原因的顺序是用户最该先处理的：模型 → 参考图 → 参数与提示词 → 余额。
 * 还没写提示词、正在发送、没有模型这几种情况只禁用按钮，不提示（模型的状态由模型按钮自己显示）。
 * @param args 现状
 * @returns 判断结果
 */
export function evaluateSend(args: SendArgs): SendState {
  const { model, text, params, refs, available, sending } = args;
  if (sending) return blocked(null, null);

  // 1. 模型：清单没回来、加载失败、没有可用模型都不能发；
  //    输入卡片的模型按钮已经写明原因（加载模型… / 模型加载失败 / 暂无可用模型），这里不再重复提示
  if (!model) return blocked(null, null);

  // 2. 参考图：上传中要等，失败要处理
  if (refs.some((ref) => ref.status === "uploading")) return blocked("等待参考上传完成", "warn");
  if (refs.some((ref) => ref.status === "error"))
    return blocked("有参考上传失败，重试或移除后再发送", "bad");

  // 3. 按模型能力组装 input 并校验：提示词、生成方式、参数、参考素材数量——直接用画布同一个 buildTaskInput，
  //    参考素材当作「手动添加的素材」放进 images / videos / audios，生成方式取用户设的（没设按模型规则）
  const ids = refIdsByKind(refs);
  const built = buildTaskInput(model.capabilities, {
    ...params,
    prompt: text.trim(),
    images: ids.image,
    videos: ids.video,
    audios: ids.audio,
  });
  // 3.1 先查有没有参考素材被丢：它比「全能参考需要至少 1 个参考素材」这类连带错误更接近根因。
  //     有参考素材却没进 input：当前生成方式或模型不收这种素材，buildTaskInput 会把它们丢掉。
  //     不能悄悄丢：用户以为参考生效了，结果却没用上
  const dropped = REF_KEYS.find(
    (ref) => ids[ref.kind].length > 0 && !Array.isArray(built.input[ref.key]),
  );
  if (dropped) return blocked(droppedHint(model, dropped.kind, built.input.op), "warn");

  const first = Object.entries(built.errors)[0];
  if (first) {
    // 提示词为空时只禁用按钮：刚打开页面就满屏红字没有意义
    const silent = first[0] === "prompt" && text.trim() === "";
    return blocked(silent ? null : first[1], "warn");
  }

  // 4. 计价：每个任务的积分 × 生成数量；只用于显示，下单以后端算的为准
  const spec = priceSpecOf(model.capabilities, built.input);
  const creditsEach = quote(model.pricing, model.capabilities, spec);
  const count = fanoutCount(model.capabilities, spec.params);
  const creditsTotal = creditsEach * count;
  const result = { input: built.input, count, creditsEach, creditsTotal };

  // 5. 余额：拿到了余额又不够才拦；还没拿到时交给后端兜底
  if (available !== null && creditsTotal > available)
    return blocked(`余额不足（需 ${creditsTotal}，可用 ${available}）`, "bad", result);

  return { canSend: true, hint: null, tone: null, ...result };
}

/** 把参考素材按种类分组，只取上传完成的素材 ID（数字） */
export function refIdsByKind(refs: ComposerRef[]): Record<RefKind, number[]> {
  const out: Record<RefKind, number[]> = { image: [], video: [], audio: [] };
  for (const ref of refs) {
    const id = toAssetNumber(ref.assetId);
    if (id !== null) out[ref.kind].push(id);
  }
  return out;
}

/** 参考素材被丢弃时的提示：模型根本不收这种素材，还是当前生成方式不收（切换生成方式就行） */
function droppedHint(model: ModelInfo, kind: RefKind, op: unknown): string {
  const label = REF_KEYS.find((ref) => ref.kind === kind)?.label.replace("参考", "") ?? "素材";
  if (!acceptableRefKinds(model.capabilities).includes(kind))
    return `当前模型不支持${label}参考，请移除或换个模型`;
  const name =
    typeof op === "string" && op in OP_LABEL ? OP_LABEL[op as keyof typeof OP_LABEL] : "当前方式";
  return `「${name}」不接收${label}参考，请切换生成方式或移除`;
}
