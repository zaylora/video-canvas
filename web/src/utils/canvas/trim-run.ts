import { UPLOAD_SIZE_LIMIT } from "@/constants/canvas";
import { useTrimStore } from "@/store/trim";

import { estimatedTrimBytes, trimUrl } from "./trim";

/** 一次修剪要做的事：点 ✓ 那一刻就定下来 */
export type TrimJob = {
  /** 源视频节点 id */
  nodeId: string;
  /** 源视频地址（稳定的 /files/ 路径） */
  src: string;
  /** 保留片段的起止（秒） */
  start: number;
  end: number;
  /** 源视频总时长（秒），用来粗估体积 */
  duration: number;
  /** 去掉音轨 */
  discardAudio: boolean;
  /** 源文件大小（字节），量不出来给 0 */
  sourceBytes: number;
  /** 产出文件名 */
  fileName: string;
};

/** 读出来的源视频信息 */
export type TrimProbe = {
  duration: number;
  hasAudio: boolean;
  width: number;
  height: number;
};

/** 编码引擎的编码参数 */
export type TrimEncodeArgs = {
  src: string;
  start: number;
  end: number;
  discardAudio: boolean;
  /** 进度 0 到 1 */
  onProgress: (ratio: number) => void;
  signal: AbortSignal;
};

/** 引擎：读信息、取缩略图、编码。真实实现用 Mediabunny，测试里换成假的 */
export type TrimEngine = {
  probe: (src: string, signal: AbortSignal) => Promise<TrimProbe>;
  /** 按时刻取缩略图，返回 object URL 或 data URL；取不到的格子为 null */
  thumbnails: (
    src: string,
    times: number[],
    height: number,
    signal: AbortSignal,
  ) => Promise<(string | null)[]>;
  encode: (args: TrimEncodeArgs) => Promise<Blob>;
};

/** 失败的几种原因，界面按它给话 */
export type TrimErrorCode = "unsupported" | "no-video" | "undecodable" | "cors" | "too-large";

const MESSAGES: Record<TrimErrorCode, string> = {
  unsupported: "当前浏览器不支持视频修剪，请换用最新版 Chrome、Edge 或 Safari",
  "no-video": "这个文件没有画面，没法修剪",
  undecodable: "读不出视频：视频格式不支持修剪",
  cors: "读不出视频：存储没有开启跨域读取，或网络中断，请联系管理员在存储桶配置 CORS",
  "too-large": "剪出来的视频超过 200MB，缩短选区试试",
};

/** 修剪失败：code 是原因，message 已经是写给用户的话 */
export class TrimError extends Error {
  readonly code: TrimErrorCode;

  constructor(code: TrimErrorCode, message: string = MESSAGES[code]) {
    super(message);
    this.name = "TrimError";
    this.code = code;
  }
}

/** 把任何错误翻成写给用户的一句话；内部报错不原样端出去 */
export function describeTrimError(error: unknown): string {
  if (error instanceof TrimError) return error.message;
  // fetch 被跨域或断网拦下时浏览器只抛 TypeError，分不出是哪一种，两种都提示
  if (error instanceof TypeError) return MESSAGES.cors;
  return "修剪失败，请稍后重试";
}

/** 是不是用户或程序主动中止 */
const isAbort = (error: unknown) => error instanceof DOMException && error.name === "AbortError";

/** 一次编码怎么收场 */
export type TrimResult =
  | { outcome: "done"; file: File }
  | { outcome: "failed" }
  | { outcome: "aborted" };

/**
 * 修剪的编码流程控制：同一时刻只编码一个；进度、失败原因写进 useTrimStore，
 * 成功把产物包成 File 交给调用方去建节点、上传。
 * 编码任务不跟面板组件的挂载走（面板卸载不该中止它），只有 abort 才会中止。
 * @param engine 实际读和编码视频的引擎，默认走 Mediabunny；测试时注入假的
 * @param options.maxBytes 产物体积上限，默认和视频上传上限一致
 */
export function createTrimRunner(engine: TrimEngine, options: { maxBytes?: number } = {}) {
  const maxBytes = options.maxBytes ?? UPLOAD_SIZE_LIMIT.video;
  /** 在编码的任务：节点 id -> 它的中止控制器 */
  const running = new Map<string, AbortController>();

  const fail = (message: string): TrimResult => {
    useTrimStore.getState().fail(message);
    return { outcome: "failed" };
  };

  return {
    /**
     * 开始编码一段选区。体积预估已经超限就不开编码；编码完发现产物超限也算失败。
     * @returns 收场方式；中止时不写错误
     */
    async start(job: TrimJob): Promise<TrimResult> {
      if (running.size > 0) return fail("正在修剪另一个视频，请稍后再试");
      if (estimatedTrimBytes(job.sourceBytes, job.start, job.end, job.duration) > maxBytes) {
        return fail(MESSAGES["too-large"]);
      }

      const controller = new AbortController();
      running.set(job.nodeId, controller);
      const store = useTrimStore.getState();
      store.setPhase("encoding");
      try {
        const blob = await engine.encode({
          src: trimUrl(job.src),
          start: job.start,
          end: job.end,
          discardAudio: job.discardAudio,
          onProgress: (ratio) => useTrimStore.getState().setProgress(ratio * 100),
          signal: controller.signal,
        });
        if (controller.signal.aborted) return { outcome: "aborted" };
        if (blob.size > maxBytes) return fail(MESSAGES["too-large"]);
        return {
          outcome: "done",
          file: new File([blob], job.fileName, { type: blob.type || "video/mp4" }),
        };
      } catch (error) {
        if (isAbort(error) || controller.signal.aborted) return { outcome: "aborted" };
        return fail(describeTrimError(error));
      } finally {
        running.delete(job.nodeId);
      }
    },
    /** 中止某个节点的编码 */
    abort(nodeId: string) {
      running.get(nodeId)?.abort();
    },
    /** 中止所有编码：离开画布时 */
    abortAll() {
      for (const controller of running.values()) controller.abort();
    },
    /** 这个节点是不是正在编码 */
    isActive: (nodeId: string) => running.has(nodeId),
  };
}
