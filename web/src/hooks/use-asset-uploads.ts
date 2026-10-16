import { useEffect, useState, useSyncExternalStore } from "react";

import { uploadAsset } from "@/api/asset";
import { addToLibrary } from "@/api/asset-library";
import type { LibraryItemDto } from "@/api/asset-library/type";
import { isUploadAborted } from "@/utils/asset/direct-upload";
import { createUploadQueue, type UploadTask } from "@/utils/assets/upload-queue";

/**
 * 资产库上传：多选文件逐个走 uploadAsset，传完登记到目标文件夹；每个任务有进度、可取消、失败可重试。
 * 队列随页面存在，离开资产库页（组件卸载）时中止未完成的上传。
 * @param onDone 一个文件传完并登记成功后回调，页面据此把新素材插进列表、刷新文件夹计数
 */
export function useAssetUploads(onDone: (item: LibraryItemDto, task: UploadTask) => void) {
  const [queue] = useState(() =>
    createUploadQueue(
      { upload: uploadAsset, register: addToLibrary, isAborted: isUploadAborted },
      onDone,
    ),
  );
  /** 队列只建一次，完成回调每次渲染都换成最新的，拿到的才是页面当前的筛选与文件夹 */
  useEffect(() => {
    queue.setOnDone(onDone);
  });
  const tasks = useSyncExternalStore(queue.subscribe, queue.getState);

  useEffect(
    () => () => {
      for (const task of queue.getState()) queue.cancel(task.id);
    },
    [queue],
  );

  return {
    tasks,
    add: queue.add,
    cancel: queue.cancel,
    retry: queue.retry,
  };
}
