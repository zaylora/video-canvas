import type { AssetDto, UploadOptions } from "@/api/asset/type";
import type { LibraryItemDto } from "@/api/asset-library/type";

/** 同时上传的文件数上限：多选一大批时不把带宽和浏览器连接数占满 */
export const UPLOAD_CONCURRENCY = 3;

/** 一个上传任务 */
export type UploadTask = {
  /** 任务 ID，本地生成 */
  id: string;
  /** 待上传的文件 */
  file: File;
  /** 登记到哪个文件夹；null 是根目录 */
  folderId: string | null;
  /** queued 排队；uploading 传文件；registering 已传完、正在登记进资产库；failed 失败可重试 */
  status: "queued" | "uploading" | "registering" | "failed";
  /** 上传进度 0–100；传完不代表登记完成 */
  percent: number;
  /** 失败原因，其余状态为 null */
  error: string | null;
};

/** 上传队列用到的外部能力：由调用方注入，测试时换成假的 */
export type UploadDeps = {
  /** 上传文件，取消时抛出能被 isAborted 认出的错误 */
  upload: (file: File, options: UploadOptions) => Promise<AssetDto>;
  /** 把传好的素材登记到资产库 */
  register: (assetId: string, folderId: string | null) => Promise<LibraryItemDto>;
  /** 是不是「用户取消」：取消不算失败 */
  isAborted: (error: unknown) => boolean;
};

/** 上传队列：不依赖 React，hooks 用 useSyncExternalStore 订阅 */
export type UploadQueue = {
  /** 当前所有未完成的任务（完成的会被移出） */
  getState: () => UploadTask[];
  /** 订阅变化，返回取消函数 */
  subscribe: (listener: () => void) => () => void;
  /** 加入一批文件，逐个上传 */
  add: (files: File[], folderId: string | null) => void;
  /** 取消进行中的任务，或丢弃失败的任务 */
  cancel: (id: string) => void;
  /** 重试失败的任务；文件已传好只是登记失败时，只重新登记 */
  retry: (id: string) => void;
  /** 换掉完成回调：队列只建一次，回调要跟着页面最新的状态走 */
  setOnDone: (onDone: (item: LibraryItemDto, task: UploadTask) => void) => void;
};

/** 失败原因：请求层已经把业务错误整理成 Error.message，没有就给兜底文案 */
const reasonOf = (error: unknown) =>
  error instanceof Error && error.message ? error.message : "上传失败";

/**
 * 创建资产库上传队列：每个文件走 uploadAsset，传完再登记到当前文件夹。
 * 登记失败时记住已传好的素材，重试只补登记，不重复传文件。
 * @param deps 上传、登记、识别取消
 * @param initialOnDone 一个文件传完并登记成功后回调，任务随后被移出队列；之后可用 setOnDone 替换
 * @param concurrency 并发上限
 * @returns 上传队列
 */
export function createUploadQueue(
  deps: UploadDeps,
  initialOnDone: (item: LibraryItemDto, task: UploadTask) => void,
  concurrency: number = UPLOAD_CONCURRENCY,
): UploadQueue {
  let onDone = initialOnDone;
  let tasks: UploadTask[] = [];
  /** 每个进行中任务的中止控制器 */
  const controllers = new Map<string, AbortController>();
  /** 已传好但登记失败的素材 */
  const uploaded = new Map<string, AssetDto>();
  const listeners = new Set<() => void>();
  let seq = 0;

  const emit = (next: UploadTask[]) => {
    tasks = next;
    listeners.forEach((listener) => listener());
  };
  const patch = (id: string, change: Partial<UploadTask>) =>
    emit(tasks.map((task) => (task.id === id ? { ...task, ...change } : task)));
  const drop = (id: string) => {
    controllers.delete(id);
    uploaded.delete(id);
    emit(tasks.filter((task) => task.id !== id));
  };
  const alive = (id: string) => tasks.some((task) => task.id === id);

  /** 有空位就让排队的开始 */
  const pump = () => {
    let running = tasks.filter(
      (task) => task.status === "uploading" || task.status === "registering",
    ).length;
    for (const task of tasks) {
      if (running >= concurrency) break;
      if (task.status === "queued") {
        running++;
        void run(task.id);
      }
    }
  };

  const register = async (task: UploadTask, asset: AssetDto) => {
    patch(task.id, { status: "registering", percent: 100, error: null });
    try {
      const item = await deps.register(asset.id, task.folderId);
      if (!alive(task.id)) return;
      drop(task.id);
      onDone(item, task);
    } catch (error) {
      if (!alive(task.id)) return;
      uploaded.set(task.id, asset);
      patch(task.id, { status: "failed", error: reasonOf(error) });
    }
  };

  const run = async (id: string) => {
    const task = tasks.find((item) => item.id === id);
    if (!task) return;
    const done = uploaded.get(id);
    if (done) {
      await register(task, done);
      pump();
      return;
    }
    const controller = new AbortController();
    controllers.set(id, controller);
    patch(id, { status: "uploading", percent: 0, error: null });
    try {
      const asset = await deps.upload(task.file, {
        signal: controller.signal,
        onProgress: (percent) => alive(id) && patch(id, { percent }),
      });
      if (alive(id)) await register(task, asset);
    } catch (error) {
      if (alive(id) && !deps.isAborted(error)) {
        patch(id, { status: "failed", error: reasonOf(error) });
      }
    }
    pump();
  };

  return {
    setOnDone: (next) => {
      onDone = next;
    },
    getState: () => tasks,
    subscribe: (listener) => {
      listeners.add(listener);
      return () => {
        listeners.delete(listener);
      };
    },
    add: (files, folderId) => {
      const added: UploadTask[] = files.map((file) => ({
        id: `upload-${++seq}`,
        file,
        folderId,
        status: "queued",
        percent: 0,
        error: null,
      }));
      emit([...tasks, ...added]);
      pump();
    },
    cancel: (id) => {
      controllers.get(id)?.abort();
      drop(id);
      pump();
    },
    retry: (id) => {
      const task = tasks.find((item) => item.id === id);
      if (!task || task.status !== "failed") return;
      patch(id, { status: "queued", percent: 0, error: null });
      pump();
    },
  };
}
