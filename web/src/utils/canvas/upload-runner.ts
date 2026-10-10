import { uploadAsset } from "@/api/asset";
import type { AssetDto, UploadOptions } from "@/api/asset/type";
import type { CanvasNodeData } from "@/types";

import { isUploadAborted } from "../asset/direct-upload";

/** 一次上传怎么收场：成功、失败（文件还留着，可以重试）、被取消 */
export type UploadOutcome = "done" | "failed" | "aborted";

/** 把改动写回某个节点；节点已经被删了就什么也不做 */
export type PatchNode = (id: string, patch: Partial<CanvasNodeData>) => void;

/** 上传失败时节点上写的话 */
const FAILED_TEXT = "上传失败，请重试";

/**
 * 画布节点上传的流程控制：上传中把进度写进节点，成功换成正式地址，
 * 失败把文件留在内存里等重试，节点被删就中止请求。
 * 上传任务、暂存的文件都按节点 id 存，只活在当前页面里，刷新后失败的节点没法再重试。
 * @param upload 实际上传文件的函数，默认走 uploadAsset；测试时注入假的
 */
export function createUploadRunner(
  upload: (file: File, options: UploadOptions) => Promise<AssetDto> = uploadAsset,
) {
  /** 在传的任务：节点 id -> 它的中止控制器 */
  const running = new Map<string, AbortController>();
  /** 留着的文件：上传中和失败后都在，成功或取消就丢 */
  const files = new Map<string, File>();

  const start = async (id: string, file: File, patch: PatchNode): Promise<UploadOutcome> => {
    const controller = new AbortController();
    running.set(id, controller);
    files.set(id, file);
    patch(id, { status: "running", uploadProgress: 0, error: null });

    /** 取消或被新的一次上传顶替后，这次的结果不该再碰节点 */
    const current = () => running.get(id) === controller;
    let last = 0;
    try {
      const asset = await upload(file, {
        signal: controller.signal,
        onProgress: (percent) => {
          // 进度每个整数百分点才写一次，免得大文件把整张画布刷爆；降级重传时允许回退
          if (!current() || percent === last) return;
          last = percent;
          patch(id, { uploadProgress: percent });
        },
      });
      if (!current()) return "aborted";
      running.delete(id);
      files.delete(id);
      patch(id, { status: "done", src: asset.url, assetId: asset.id, uploadProgress: undefined });
      return "done";
    } catch (error) {
      if (isUploadAborted(error) || !current()) return "aborted";
      running.delete(id);
      patch(id, { status: "error", error: FAILED_TEXT, uploadProgress: undefined });
      return "failed";
    }
  };

  return {
    /**
     * 开始上传一个文件到某个节点。
     * @param id 节点 id
     * @param file 要上传的文件
     * @param patch 把进度和结果写回节点
     * @returns 收场方式；取消时不会碰节点
     */
    start,
    /**
     * 重试失败的上传：用留着的文件在同一个节点上重新开始。文件已经没了或正在传时不动，按失败返回。
     * @param id 节点 id
     * @param patch 把进度和结果写回节点
     */
    retry: async (id: string, patch: PatchNode): Promise<UploadOutcome> => {
      const file = files.get(id);
      if (!file || running.has(id)) return "failed";
      return start(id, file, patch);
    },
    /**
     * 取消并忘掉某个节点的上传：节点被删时调用。在传的请求被中止，暂存的文件也丢掉。
     * @param id 节点 id
     */
    cancel: (id: string) => {
      running.get(id)?.abort();
      running.delete(id);
      files.delete(id);
    },
    /** 取消所有上传：画布关闭时调用 */
    cancelAll: () => {
      for (const controller of running.values()) controller.abort();
      running.clear();
      files.clear();
    },
    /** 这个节点是不是真有一个上传在跑（撤销恢复出来的上传中节点没有，只是个空壳） */
    isActive: (id: string) => running.has(id),
    /** 这个节点失败后还留着文件，可以重试 */
    canRetry: (id: string) => files.has(id) && !running.has(id),
  };
}

/** 画布共用的上传流程控制 */
export const uploadRunner = createUploadRunner();
