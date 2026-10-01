import { useState } from "react";

import { getModelDetail, publishModel, setModelEnabled, updateModelDraft } from "@/api/admin-ai";
import type { ConfigListItem } from "@/api/admin-ai/type";
import { errorMessage } from "@/utils/admin/errors";
import { withDefaultPrice, withModelChannel, withModelField } from "@/utils/admin/model-body";

/** 批量修改要改的字段：值为 undefined 表示不改 */
export type BatchPatch = {
  /** 统一默认价格（按次 / 按秒的默认价；Token 计费的模型跳过） */
  price?: number;
  deadline?: string;
  channel?: string;
};

export type BatchAction = "enable" | "disable" | "publish" | "edit";

export type BatchResult = {
  action: BatchAction;
  /** 成功的 key */
  done: string[];
  /** 跳过的（不满足前提）及原因 */
  skipped: Array<{ key: string; reason: string }>;
  /** 失败的及原因 */
  failed: Array<{ key: string; reason: string }>;
};

const LABEL: Record<BatchAction, string> = {
  enable: "批量上架",
  disable: "批量下架",
  publish: "批量发布草稿",
  edit: "批量修改",
};
export const batchLabel = (action: BatchAction) => LABEL[action];

/**
 * 模型批量操作：后端没有批量接口，这里对选中的模型逐个调用现有接口（串行，避免把后端打满），
 * 每个的成功 / 跳过 / 失败都记下来，最后给一份结果，不因为某一个失败而中断。
 * 批量修改只写草稿，不会改线上版本；勾了“同时发布”才会接着发布。
 */
export function useModelBatch(models: ConfigListItem[], reload: () => Promise<void> | void) {
  const [running, setRunning] = useState<BatchAction | null>(null);
  const [progress, setProgress] = useState({ done: 0, total: 0 });
  const [result, setResult] = useState<BatchResult | null>(null);

  const run = async (
    action: BatchAction,
    keys: string[],
    handler: (item: ConfigListItem) => Promise<string | void>,
  ) => {
    const out: BatchResult = { action, done: [], skipped: [], failed: [] };
    setRunning(action);
    setResult(null);
    setProgress({ done: 0, total: keys.length });
    for (const [index, key] of keys.entries()) {
      const item = models.find((m) => m.key === key);
      try {
        if (!item) out.skipped.push({ key, reason: "模型已不在列表里" });
        else {
          const skip = await handler(item);
          if (skip) out.skipped.push({ key, reason: skip });
          else out.done.push(key);
        }
      } catch (error) {
        out.failed.push({ key, reason: errorMessage(error, "失败") });
      }
      setProgress({ done: index + 1, total: keys.length });
    }
    setRunning(null);
    setResult(out);
    await reload();
    return out;
  };

  const setEnabled = (keys: string[], enabled: boolean) =>
    run(enabled ? "enable" : "disable", keys, async (item) => {
      if (enabled && item.published_revision_no === null) return "还没发布过，不能上架";
      if (!!item.enabled === enabled) return enabled ? "已经是上架状态" : "已经是下架状态";
      await setModelEnabled(item.key, enabled);
    });

  const publishDrafts = (keys: string[]) =>
    run("publish", keys, async (item) => {
      if (!item.has_unpublished_draft) return "没有未发布的草稿";
      await publishModel(item.key);
    });

  const edit = (keys: string[], patch: BatchPatch, publish: boolean) =>
    run("edit", keys, async (item) => {
      const detail = await getModelDetail(item.key);
      let body: Record<string, unknown> | null = null;
      const source = detail.draft?.body_json ?? detail.published?.body_json;
      body = source && typeof source === "object" ? { ...(source as object) } : null;
      if (!body) return "读不到配置正文";
      if (patch.price !== undefined) {
        body = withDefaultPrice(body, patch.price);
        if (!body) return "按 Token 计费，没有统一价格，已跳过";
      }
      if (body && patch.deadline !== undefined)
        body = withModelField(body, "deadline", patch.deadline);
      if (body && patch.channel !== undefined) body = withModelChannel(body, patch.channel);
      if (!body) return "读不到配置正文";
      const saved = await updateModelDraft(item.key, body, "批量修改");
      if (publish) {
        if (saved.issues.length > 0)
          return `草稿已保存，但有 ${saved.issues.length} 个问题，未发布`;
        await publishModel(item.key);
      }
    });

  return {
    running,
    progress,
    result,
    clearResult: () => setResult(null),
    setEnabled,
    publishDrafts,
    edit,
  };
}

export type ModelBatch = ReturnType<typeof useModelBatch>;
