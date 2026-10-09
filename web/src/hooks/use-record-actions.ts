import { useCallback, useState } from "react";
import { useShallow } from "zustand/react/shallow";
import { toast } from "sonner";

import { getAsset } from "@/api/asset";
import type { RecordDto } from "@/api/conversation/type";
import type { TaskOutput, TaskView } from "@/api/generation-task/type";
import { addReferenceAsset, currentComposerModel } from "@/hooks/use-composer-refs";
import { useComposerStore } from "@/store/composer";
import { useConversationRecordsStore } from "@/store/conversation-records";
import { useModelsStore } from "@/store/models";
import { useTasksStore } from "@/store/tasks";
import { restoreComposer } from "@/utils/conversation/restore";
import { fanoutParam } from "@/utils/pricing/quote";
import { cancelTasks } from "@/utils/tasks/gateway";
import { isCancelableStatus } from "@/utils/tasks/status";

/**
 * 记录里各格任务的最新快照（只含已创建的任务）：任务库里的版本优先，没有就用记录里带的那份。
 * 订阅任务库，WebSocket 推送来的进度会让用到它的组件重渲染。
 * @param record 记录
 * @returns 任务快照列表
 */
export function useRecordTasks(record: RecordDto): TaskView[] {
  return useTasksStore(
    useShallow((state) =>
      record.tasks
        .map((task) => (task ? (state.tasks[String(task.id)] ?? task) : null))
        .filter((task): task is TaskView => task !== null),
    ),
  );
}

/** 记录用的模型还在不在清单里；清单还没加载好时当作在，不误报「已下线」 */
const modelOf = (record: RecordDto) => {
  const entry = useModelsStore.getState().byKind[record.kind];
  if (!entry || entry.status !== "ready") return { loaded: false, model: undefined } as const;
  return { loaded: true, model: entry.models.find((item) => item.key === record.modelId) } as const;
};

/**
 * 判断记录的模型是不是已经下线：清单加载完成且里面没有它才算。
 * @param record 记录
 * @param loadedModels 该种类的模型 key 列表；清单没加载好为 null
 * @returns 是否已下线
 */
export const isModelOffline = (record: RecordDto, loadedModels: string[] | null) =>
  loadedModels !== null && !loadedModels.includes(record.modelId);

/**
 * 对话页里记录的操作：重新编辑、再次生成、重试一格、取消。
 * 再次生成和重试都是用记录里的输入快照新提交一条记录，追加在对话底部，不改动原来的记录。
 * @param conversationId 当前对话 ID
 * @returns 各操作，以及是否正在提交（提交期间对应按钮应禁用）
 */
export function useRecordActions(conversationId: string) {
  const [busy, setBusy] = useState(false);

  /** 重新编辑：把记录的模式、模型、参数、参考图、提示词整体填回输入卡片，不发送 */
  const reEdit = useCallback((record: RecordDto) => {
    const restored = restoreComposer(record);
    const { loaded, model } = modelOf(record);
    const entry = useModelsStore.getState().byKind[record.kind];
    const fallback = entry?.models[0];
    const offline = loaded && !model;
    if (offline)
      toast.warning(fallback ? `原模型已下线，已换成 ${fallback.label}` : "原模型已下线");
    const refs = restored.refs.map(({ kind, id }) => ({
      id: crypto.randomUUID(),
      kind,
      assetId: String(id),
      name: `素材 ${id}`,
      status: "done" as const,
    }));
    useComposerStore.getState().restore({
      mode: restored.mode,
      modelId: offline && fallback ? fallback.key : restored.modelId,
      params: offline ? {} : restored.params,
      text: restored.text,
      refs,
    });
    // 缩略图要素材地址：逐个取，取不到（素材已清理）就留着占位，不影响重新生成
    for (const ref of refs) {
      getAsset(ref.assetId)
        .then((asset) =>
          useComposerStore
            .getState()
            .updateRef(ref.id, { url: asset.url, name: asset.fileName ?? ref.name }),
        )
        .catch(() => undefined);
    }
  }, []);

  /** 用记录的快照再提交一次：count 为 null 表示沿用原数量，否则只生成这么多（重试一格用 1） */
  const resubmit = useCallback(
    async (record: RecordDto, count: number | null, done: string) => {
      const { loaded, model } = modelOf(record);
      if (loaded && !model) {
        toast.error("原模型已下线，请重新编辑后选择其他模型");
        return;
      }
      const input = { ...record.input };
      const nextCount = count ?? record.count;
      const name = fanoutParam(model?.capabilities);
      if (count !== null && name) input[name] = count;
      setBusy(true);
      try {
        await useConversationRecordsStore.getState().submit(conversationId, {
          kind: record.kind,
          modelId: record.modelId,
          prompt: record.prompt,
          input,
          count: nextCount,
        });
        toast.success(done);
      } catch {
        // 提示由请求层统一弹
      } finally {
        setBusy(false);
      }
    },
    [conversationId],
  );

  const regenerate = useCallback(
    (record: RecordDto) => resubmit(record, null, "已作为新记录追加到底部"),
    [resubmit],
  );

  const retryCell = useCallback(
    (record: RecordDto) => resubmit(record, 1, "已重试这一格，作为新记录追加到底部"),
    [resubmit],
  );

  /** 取消记录里还能取消的任务，积分退回；统一入口逐个取消，某个失败不影响其他 */
  const cancel = useCallback(async (record: RecordDto) => {
    const tasks = useTasksStore.getState().tasks;
    const ids = record.tasks
      .map((task) => (task ? (tasks[String(task.id)] ?? task) : null))
      .filter((task): task is TaskView => task !== null && isCancelableStatus(task.status))
      .map((task) => task.id);
    if (ids.length === 0) return;
    setBusy(true);
    try {
      await cancelTasks(ids);
    } finally {
      setBusy(false);
    }
  }, []);

  return { busy, reEdit, regenerate, retryCell, cancel };
}

/**
 * 把一个结果（图片、视频或音频）用作输入卡片的参考素材：直接引用已有素材，不重新上传。
 * 校验、提示和生成方式切换与上传参考是同一套（见 hooks/use-composer-refs.ts 的 addReferenceAsset）。
 * @param output 结果产出
 */
export function addResultAsReference(output: TaskOutput) {
  const kind = output.media_type;
  if (
    output.asset_id === undefined ||
    !output.url ||
    (kind !== "image" && kind !== "video" && kind !== "audio")
  ) {
    toast.error("这个结果没有可引用的素材");
    return;
  }
  const assetId = String(output.asset_id);
  addReferenceAsset(currentComposerModel(), {
    kind,
    assetId,
    url: output.url,
    name: `素材 ${assetId}`,
  });
}
