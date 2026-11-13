import { useCallback, useRef, useState } from "react";
import { toast } from "sonner";

import {
  cancelMarketingRound,
  newIdempotencyKey,
  refollowMarketingRound,
  refreshMarketingFacts,
  renderMarketingRound,
  renderMarketingScript,
  restoreMarketingReverse,
  restoreMarketingScript,
  retryMarketingStep,
  saveMarketingReverse,
  saveMarketingScript,
  sendMarketingScriptToCanvas,
  updateMarketingProject,
  updateMarketingRoundSettings,
} from "@/api/marketing";
import { isMarketingConflict } from "@/api/marketing/errors";
import type {
  ProductFacts,
  Project,
  Reverse,
  Round,
  Script,
  UpdateRoundSettingsInput,
} from "@/api/marketing/type";
import { useCreditsStore } from "@/store/credits";
import { useMarketingStore } from "@/store/marketing";
import { reloadMarketingProject } from "@/utils/marketing/events";

/**
 * 详情页上的全部操作：每个操作成功后把服务端返回的最新轮次 / 脚本并进本地缓存，
 * 失败的提示由请求层统一弹；撞上 68009（别处改过）时重新拉详情载入最新版。
 * busy 记录正在进行的操作（键如 `render:{脚本id}`），按钮据此转圈、防重复点击。
 * 出片的幂等键按「脚本 / 轮次」记住：同一次点击失败后重试沿用同一个，成功后换新的。
 * @param project 当前项目
 */
export function useMarketingActions(project: Project) {
  const [busy, setBusy] = useState<Record<string, boolean>>({});
  const keys = useRef<Record<string, string>>({});

  const busyRef = useRef<Record<string, boolean>>({});

  /** 记录一个操作的进行中状态；同一个键进行中时直接忽略 */
  const run = useCallback(
    async <T>(key: string, task: () => Promise<T>): Promise<T | undefined> => {
      if (busyRef.current[key]) return undefined;
      busyRef.current[key] = true;
      setBusy((prev) => ({ ...prev, [key]: true }));
      try {
        return await task();
      } catch (error) {
        if (isMarketingConflict(error)) void reloadMarketingProject(project.id);
        return undefined;
      } finally {
        busyRef.current[key] = false;
        setBusy((prev) => ({ ...prev, [key]: false }));
      }
    },
    [project.id],
  );

  const applyRound = useCallback(
    (round: Round) => useMarketingStore.getState().applyRound(project.id, round),
    [project.id],
  );
  const applyScript = useCallback(
    (script: Script) =>
      useMarketingStore.getState().applyScript(project.id, script.roundId, script),
    [project.id],
  );
  const refreshCredits = () => void useCreditsStore.getState().refresh();

  /** 取消写脚本中的轮次，冻结积分退回 */
  const cancelRound = (round: Round) =>
    run(`cancel:${round.id}`, async () => {
      applyRound(await cancelMarketingRound(round.id));
      refreshCredits();
      toast.info("已取消，积分已退回");
    });

  /** 重试一个失败的环节，只重跑它 */
  const retryStep = (stepRunId: string) =>
    run(`step:${stepRunId}`, async () => {
      applyRound(await retryMarketingStep(stepRunId));
      refreshCredits();
    });

  /** 保存拆解修改；成功后轮次里 reverseDirty 会变成 true，出现「按拆解重写跟拍版」 */
  const saveReverse = (round: Round, reverse: Reverse) =>
    run(`reverse:${round.id}`, async () => {
      applyRound(await saveMarketingReverse(round.id, reverse, round.reverseRevision));
    });

  const restoreReverse = (round: Round) =>
    run(`reverse-restore:${round.id}`, async () => {
      applyRound(await restoreMarketingReverse(round.id));
      toast.success("拆解已恢复原稿");
    });

  const refollow = (round: Round) =>
    run(`refollow:${round.id}`, async () => {
      applyRound(await refollowMarketingRound(round.id));
      refreshCredits();
    });

  /** 保存旁白：服务端重扫问题旁白，返回的脚本带最新 flags */
  const saveNarrations = (script: Script, narrations: string[]) =>
    run(`script:${script.id}`, async () => {
      applyScript(await saveMarketingScript(script.id, narrations, script.revision));
    });

  const restoreScript = (script: Script) =>
    run(`script-restore:${script.id}`, async () => {
      applyScript(await restoreMarketingScript(script.id));
      toast.success("已恢复原稿");
    });

  /** 单条出片 / 重出 */
  const render = (script: Script) =>
    run(`render:${script.id}`, async () => {
      const key = (keys.current[script.id] ??= newIdempotencyKey());
      const next = await renderMarketingScript(script.id, key);
      delete keys.current[script.id];
      applyScript(next);
      refreshCredits();
    });

  /** 全部出片：被跳过的脚本说明原因 */
  const renderAll = (round: Round) =>
    run(`render-all:${round.id}`, async () => {
      const key = (keys.current[`round:${round.id}`] ??= newIdempotencyKey());
      const result = await renderMarketingRound(round.id, key);
      delete keys.current[`round:${round.id}`];
      applyRound(result.round);
      refreshCredits();
      const started = result.round.scripts.filter((s) => s.status === "rendering").length;
      if (result.skipped.length > 0) {
        toast.warning(
          `已开始 ${started} 条；${result.skipped.length} 条被跳过（${result.skipped
            .map((item) => item.reason)
            .join("；")}）`,
        );
      } else {
        toast.success(`已开始 ${started} 条`);
      }
    });

  /** 改本轮出片设置（视频模型、分辨率），只影响之后的出片 */
  const updateSettings = (round: Round, input: UpdateRoundSettingsInput) =>
    run(`settings:${round.id}`, async () => {
      applyRound(await updateMarketingRoundSettings(round.id, input));
    });

  /**
   * 保存商品信息：事实卡，以及换过的商品图（项目级）、模特图（当前这一轮）；成功返回 true。
   * 只传变了的素材。
   */
  const saveFacts = async (input: FactsSave): Promise<boolean> => {
    const result = await run("facts", async () => {
      const next = await updateMarketingProject(project.id, {
        title: input.facts.name,
        product: input.facts,
        productAssetIds: input.productAssetIds,
        revision: project.revision,
      });
      useMarketingStore.getState().setProject(next);
      if (input.modelAssetId !== undefined && input.round) {
        const { settings } = input.round;
        applyRound(
          await updateMarketingRoundSettings(input.round.id, {
            videoModel: settings.videoModel,
            params: settings.params,
            modelAssetId: input.modelAssetId,
          }),
        );
      }
      toast.success("已保存，之后出片按新的商品信息");
      return true;
    });
    return result === true;
  };

  /** 按项目当前商品图重新识别外观锚点（在最新一轮重跑事实卡环节，扣积分）；成功返回 true */
  const refreshFacts = async (): Promise<boolean> => {
    const result = await run("refresh-facts", async () => {
      applyRound(await refreshMarketingFacts(project.id));
      refreshCredits();
      toast.info("正在按商品图重新识别外观锚点");
      return true;
    });
    return result === true;
  };

  /** 发到画布：成功返回新画布 ID */
  const toCanvas = (script: Script) =>
    run(`canvas:${script.id}`, () => sendMarketingScriptToCanvas(script.id));

  return {
    busy,
    cancelRound,
    retryStep,
    saveReverse,
    restoreReverse,
    refollow,
    saveNarrations,
    restoreScript,
    render,
    renderAll,
    updateSettings,
    saveFacts,
    refreshFacts,
    toCanvas,
  };
}

/** 保存商品信息的内容：素材只放变了的 */
export interface FactsSave {
  /** 事实卡 */
  facts: ProductFacts;
  /** 新的商品图素材 ID（1–4 张）；没换不传 */
  productAssetIds?: number[];
  /** 换用的模特图素材 ID；没换不传 */
  modelAssetId?: number;
  /** 模特图所在的轮次（当前看的这一轮） */
  round?: Round;
}

/** 操作集合的类型，子组件的 props 用 */
export type MarketingActions = ReturnType<typeof useMarketingActions>;
