import { uploadAsset } from "@/api/asset";
import type { AssetDto } from "@/api/asset/type";
import type { CreateTaskItem, CreateTaskRequest } from "@/api/generation-task/type";
import type { Capabilities } from "@/api/model/type";
import type { CanvasNodeData } from "@/types";
import { submitCanvasTasks } from "@/utils/tasks/gateway";
import { buildSubmittedPatch, describeSubmitError } from "@/utils/tasks/submit";
import { buildTaskInput } from "@/utils/tasks/capabilities";

import {
  loadSourceImage,
  planCompose,
  renderComposite,
  type ComposePlan,
} from "./outpaint-compose";
import type { OriginSize, OutpaintFrame } from "./outpaint-frame";

/** 一次扩图要做的事：点发送那一刻就定下来，重试时原样再来一遍 */
export type OutpaintJob = {
  /** 已经建好的结果节点 */
  nodeId: string;
  /** 原图地址 */
  sourceSrc: string;
  /** 原图节点在画布上的大小（100% 缩放） */
  origin: OriginSize;
  /** 扩图框 */
  frame: OutpaintFrame;
  /** 所选模型的能力，用来拼 input 和查参考图大小上限 */
  caps: Capabilities;
  modelKey: string;
  canvasId: string;
  /** 结果节点上已经预置的 params（完整提示词、op、比例），不含 images */
  params: Record<string, unknown>;
};

/** 读到的原图：可以画的对象、真实像素，close 用来释放 ImageBitmap */
export type SourceImage = {
  image: CanvasImageSource;
  width: number;
  height: number;
  close?: () => void;
};

/** 流程里的外部依赖：测试里全换成假的 */
export type OutpaintDeps = {
  load: (src: string) => Promise<SourceImage>;
  compose: (image: CanvasImageSource, plan: ComposePlan) => Promise<Blob>;
  upload: (file: File) => Promise<AssetDto>;
  submit: (request: CreateTaskRequest) => Promise<CreateTaskItem[]>;
};

/** 把改动写回某个节点；update 拿到节点当前数据，节点已经被删了就什么也不做 */
export type PatchNode = (
  id: string,
  update: (data: CanvasNodeData) => Partial<CanvasNodeData>,
) => void;

const MB = 1024 * 1024;

const defaultDeps: OutpaintDeps = {
  load: async (src) => {
    const bitmap = await loadSourceImage(src);
    return {
      image: bitmap,
      width: bitmap.width,
      height: bitmap.height,
      close: () => bitmap.close(),
    };
  },
  compose: (image, plan) => renderComposite(image, plan),
  upload: (file) => uploadAsset(file),
  submit: (request) => submitCanvasTasks(request),
};

/**
 * 扩图的提交流程（设计稿 6.17）：读原图 → 合成透明 PNG → 上传成素材 → 以图生图提交任务。
 * 结果节点在点发送时就已经建好（状态「生成中」），这里只负责把进度和结果写回它：
 * 读图、合成、上传任何一步失败，节点显示失败原因并留下重试材料；
 * 合成图上传成功后立刻写进节点的参考图，之后提交失败走普通的「重试」就行。
 * 重试材料只活在当前页面里，刷新后没了。
 * @param deps 外部依赖，默认是真实实现；测试时注入假的
 */
export function createOutpaintRunner(deps: OutpaintDeps = defaultDeps) {
  /** 在跑的节点 */
  const running = new Set<string>();
  /** 上传成功之前失败的任务，留着重试 */
  const jobs = new Map<string, OutpaintJob>();

  const run = async (job: OutpaintJob, patch: PatchNode): Promise<void> => {
    const id = job.nodeId;
    running.add(id);
    jobs.set(id, job);
    patch(id, () => ({ status: "running", error: null }));
    const fail = (message: string) =>
      patch(id, () => ({ status: "error", error: message, taskId: undefined }));

    try {
      // 1. 读原图并合成
      let blob: Blob;
      try {
        const source = await deps.load(job.sourceSrc);
        try {
          const plan = planCompose({
            natural: { w: source.width, h: source.height },
            origin: job.origin,
            frame: job.frame,
          });
          blob = await deps.compose(source.image, plan);
        } finally {
          source.close?.();
        }
      } catch (error) {
        return fail(error instanceof Error && error.message ? error.message : "合成图失败，请重试");
      }
      const maxMb = job.caps.refs.image.max_mb;
      if (maxMb > 0 && blob.size > maxMb * MB) {
        return fail(`合成图超过模型参考图大小上限（${maxMb} MB），请缩小扩图范围`);
      }

      // 2. 上传
      let asset: AssetDto;
      try {
        asset = await deps.upload(new File([blob], "outpaint.png", { type: "image/png" }));
      } catch {
        return fail("合成图上传失败，请重试");
      }
      // 合成图已经在对象存储里，之后的失败都不用从头来
      jobs.delete(id);
      const params = { ...job.params, images: [asset.id] };
      patch(id, (data) => ({
        params: { ...data.params, images: [asset.id] },
        paramAssets: {
          ...data.paramAssets,
          [asset.id]: {
            url: asset.url,
            mediaType: "image",
            ...(asset.fileName ? { fileName: asset.fileName } : {}),
          },
        },
      }));

      // 3. 拼 input（补参数默认值、检查参考图个数），提交图生图
      const built = buildTaskInput(job.caps, params, undefined, "i2i");
      const firstError = Object.values(built.errors)[0];
      if (firstError) return fail(firstError);
      let items: CreateTaskItem[];
      try {
        items = await deps.submit({
          kind: "image",
          model_id: job.modelKey,
          canvas_id: job.canvasId,
          node_id: id,
          input: built.input,
        });
      } catch (error) {
        return fail(describeSubmitError(error).message);
      }
      const item = items.find((entry) => entry.node_id === id) ?? items[0];
      if (!item?.task) return fail(describeSubmitError(item?.error ?? null).message);
      const taskId = String(item.task.id);
      patch(id, (data) => buildSubmittedPatch("image", taskId, data));
    } finally {
      running.delete(id);
    }
  };

  return {
    /**
     * 开始一次扩图。结果节点要事先建好；这个函数不抛错，结果都写在节点上。
     * @param job 这次要做的事
     * @param patch 把进度和结果写回节点
     */
    start: (job: OutpaintJob, patch: PatchNode) => run(job, patch),
    /**
     * 重试读图 / 合成 / 上传阶段失败的节点。重试材料已经没了（刷新过页面）或正在跑时不动。
     * @returns 有没有重新开始
     */
    retry: async (id: string, patch: PatchNode): Promise<boolean> => {
      const job = jobs.get(id);
      if (!job || running.has(id)) return false;
      await run(job, patch);
      return true;
    },
    /** 这个节点正在合成 / 上传 / 提交：这段时间没有可以取消的任务 */
    isActive: (id: string) => running.has(id),
    /** 这个节点失败后还留着重试材料 */
    canRetry: (id: string) => jobs.has(id) && !running.has(id),
  };
}

/** 画布共用的扩图流程 */
export const outpaintRunner = createOutpaintRunner();
