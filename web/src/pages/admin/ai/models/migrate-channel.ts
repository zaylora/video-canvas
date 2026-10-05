import { getModelDetail, publishModel, updateModelDraft } from "@/api/admin/ai";
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
 * 每个模型在最新草稿（没有草稿用已发布版本）上换渠道并保存草稿；
 * 发布过的模型接着重新发布——否则线上版本还指向旧渠道，旧渠道仍然删不掉。
 * 注意：有未上线修改的模型，重新发布时这些修改会一起上线（界面上要提前说明）。
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
      const body = withModelChannel(
        detail.draft?.body_json ?? detail.published?.body_json,
        channelKey,
      );
      if (!body) {
        out.failed.push({ key, reason: "读不到配置正文" });
        continue;
      }
      const saved = await updateModelDraft(key, body, "迁移渠道");
      if (detail.published) {
        if (saved.issues.length > 0) {
          out.failed.push({
            key,
            reason: `草稿已改，但有 ${saved.issues.length} 个问题，未能重新上线`,
          });
          continue;
        }
        await publishModel(key);
      }
      out.done.push(key);
    } catch (error) {
      out.failed.push({ key, reason: errorMessage(error, "迁移失败") });
    }
  }
  return out;
}
