import { getModelDetail, updateModel } from "@/api/admin/ai";
import { errorMessage } from "@/utils/admin/errors";
import { withModelChannel } from "@/utils/admin/model-body";

export type MigrateResult = {
  /** 迁移成功的模型 key */
  done: string[];
  /** 失败的模型及原因 */
  failed: Array<{ key: string; reason: string }>;
};

/**
 * 把一批模型迁到另一个渠道（删除渠道前用）：后端没有批量接口，逐个串行处理。
 * 每个模型在已保存的配置上换渠道并保存。已上线的模型保存即生效，
 * 新渠道不满足上线条件（缺 Key、不支持该类型）时后端会拒绝，记为失败、保持原样。
 * @param keys 模型 key
 * @param channelKey 目标渠道
 */
export async function migrateModelsToChannel(
  keys: readonly string[],
  channelKey: string,
): Promise<MigrateResult> {
  const out: MigrateResult = { done: [], failed: [] };
  for (const key of keys) {
    try {
      const detail = await getModelDetail(key);
      const body = withModelChannel(detail.body, channelKey);
      if (!body) {
        out.failed.push({ key, reason: "读不到配置正文" });
        continue;
      }
      await updateModel(key, body, "迁移渠道");
      out.done.push(key);
    } catch (error) {
      out.failed.push({ key, reason: errorMessage(error, "迁移失败") });
    }
  }
  return out;
}
