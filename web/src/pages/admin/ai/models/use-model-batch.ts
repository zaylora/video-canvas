import { useState } from "react";

import { deleteTarget, getModelDetail, setModelEnabled, updateModel } from "@/api/admin/ai";
import type { ConfigListItem } from "@/api/admin/ai/type.d";
import { errorMessage } from "@/utils/admin/errors";
import { withDefaultPrice, withModelChannel, withModelField } from "@/utils/admin/model-body";

/** 批量修改要改的字段：值为 undefined 表示不改 */
export type BatchPatch = {
  /** 统一默认价格（按次 / 按秒的默认价；Token 计费的模型跳过） */
  price?: number;
  deadline?: string;
  channel?: string;
};

export type BatchAction = "enable" | "disable" | "edit" | "delete";

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
  enable: "批量上线",
  disable: "批量下线",
  edit: "批量修改",
  delete: "批量删除",
};
export const batchLabel = (action: BatchAction) => LABEL[action];

/**
 * 模型批量操作：后端没有批量接口，这里对选中的模型逐个调用现有接口（串行，避免把后端打满），
 * 每个的成功 / 跳过 / 失败都记下来，最后给一份结果，不因为某一个失败而中断。
 * 批量修改直接保存：已上线的模型保存即生效，没上线的只改配置。
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
      if (!!item.enabled === enabled) return enabled ? "已经在线" : "已经下线";
      await setModelEnabled(item.key, enabled);
    });

  const edit = (keys: string[], patch: BatchPatch) =>
    run("edit", keys, async (item) => {
      const detail = await getModelDetail(item.key);
      let body: Record<string, unknown> | null = null;
      const source = detail.body;
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
      // 已上线的模型保存即生效，校验不过后端会拒绝（记为失败）；没上线的有问题也存下，提示一下
      const saved = await updateModel(item.key, body, "批量修改");
      if (saved.issues.length > 0)
        return `已保存，但有 ${saved.issues.length} 个问题，上线前要处理`;
    });

  /** 批量删除：还在上线的跳过（要先下线），其余逐个彻底删除 */
  const remove = (keys: string[]) =>
    run("delete", keys, async (item) => {
      if (item.enabled) return "还在上线，先下线再删除";
      await deleteTarget("model", item.key);
    });

  return {
    running,
    progress,
    result,
    clearResult: () => setResult(null),
    setEnabled,
    edit,
    remove,
  };
}

export type ModelBatch = ReturnType<typeof useModelBatch>;
