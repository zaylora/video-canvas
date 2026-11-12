import { useCallback, useEffect, useMemo, useRef, useState } from "react";
import { useNavigate } from "react-router";
import { toast } from "sonner";

import { uploadAsset } from "@/api/asset";
import { createMarketingProject, createMarketingRound, quoteMarketing } from "@/api/marketing";
import type { MarketingAngle, PlatformOption, Project } from "@/api/marketing/type";
import type { ModelInfo } from "@/api/model/type";
import { useMarketingOptions } from "@/hooks/use-marketing-options";
import { useRemoteModels } from "@/hooks/use-models";
import { useCreditsStore } from "@/store/credits";
import { useMarketingStore } from "@/store/marketing";
import { useMarketingComposerStore, type MarketingSlot } from "@/store/marketing-composer";
import {
  evaluateMarketingSend,
  MAX_ADAPT,
  MAX_ANGLES,
  pickPlatform,
  planCount,
  reconcileVideoParams,
  refVideoMaxSeconds,
  refVideoProblem,
  toRoundSettings,
  toggleAngle,
  videoModelSupport,
  visionModels,
} from "@/utils/marketing/composer";
import { refUploadError, toAssetNumber } from "@/utils/tasks/capabilities";

/** 商品图最多几张 */
const MAX_PRODUCTS = 4;

/** 预估积分的防抖（毫秒）：设置连续变化时只问最后一次 */
const QUOTE_DEBOUNCE_MS = 350;

/** 本地预览地址：上传完成换成素材地址后要还回去 */
const previews = new Map<string, string>();

const releasePreview = (id: string) => {
  const url = previews.get(id);
  if (url) URL.revokeObjectURL(url);
  previews.delete(id);
};

/** 新建一份正在上传的素材，预览用本地地址 */
function newSlot(file: File): MarketingSlot {
  const id = crypto.randomUUID();
  const url = URL.createObjectURL(file);
  previews.set(id, url);
  return { id, name: file.name, url, status: "uploading", sizeBytes: file.size };
}

/**
 * 营销生成框的全部逻辑：素材上传、设置、模型与参数联动、写脚本预估积分、发送判断和提交。
 * 列表页的大卡片和详情页底部的 dock 共用（dock 传入 project：商品图固定为项目的，提交开新一轮）。
 * @param project 详情页传当前项目；列表页不传
 */
export function useMarketingComposer(project?: Project) {
  const dock = !!project;
  const navigate = useNavigate();
  const options = useMarketingOptions();

  const products = useMarketingComposerStore((state) => state.products);
  const modelSlot = useMarketingComposerStore((state) => state.model);
  const refSlot = useMarketingComposerStore((state) => state.ref);
  const draft = useMarketingComposerStore((state) => state.draft);
  const videoModelKey = useMarketingComposerStore((state) => state.videoModel);
  const sending = useMarketingComposerStore((state) => state.sending);
  const available = useCreditsStore((state) => state.credits?.available ?? null);
  const refreshCredits = useCreditsStore((state) => state.refresh);

  const video = useRemoteModels("video");
  const text = useRemoteModels("agent");
  const vision = useMemo(() => visionModels(text.models), [text.models]);

  const productCount = dock ? project.productAssets.length : products.length;
  const hasModelImage = !!modelSlot || !!project?.modelAsset;
  const hasRef = !!refSlot;
  const needImages = Math.max(productCount, 1) + (hasModelImage ? 1 : 0);

  /** 每个视频模型能不能选（全能参考、图片参考上限）；不能选的置灰并写原因 */
  const support = useCallback(
    (info: ModelInfo) => videoModelSupport(info, needImages),
    [needImages],
  );

  /** 选中的视频模型：记住的那个能用就用，否则第一个能用的；一个都没有为 undefined */
  const videoModel: ModelInfo | undefined = useMemo(() => {
    const usable = video.models.filter((info) => support(info).ok);
    return usable.find((info) => info.key === videoModelKey) ?? usable[0];
  }, [video.models, videoModelKey, support]);

  const scripting = dock && project.rounds.some((round) => round.status === "scripting");
  const uploading = [...products, modelSlot, refSlot].some((slot) => slot?.status === "uploading");
  const uploadFailed = [...products, modelSlot, refSlot].some((slot) => slot?.status === "error");

  /** 模型变了（含第一次拿到清单）：整理画幅 / 分辨率 / 时长；新模型不收参考视频或超限就移除 */
  const platformRatio = options.platforms.find((item) => item.key === draft.platform)?.aspectRatio;
  const lastKey = useRef<string | null>(null);
  useEffect(() => {
    if (!videoModel || lastKey.current === videoModel.key) return;
    lastKey.current = videoModel.key;
    const store = useMarketingComposerStore.getState();
    const next = reconcileVideoParams(
      videoModel.capabilities,
      {
        aspectRatio: store.draft.aspectRatio,
        resolution: store.draft.resolution,
        duration: store.draft.duration,
      },
      platformRatio,
    );
    if (
      next.aspectRatio !== store.draft.aspectRatio ||
      next.resolution !== store.draft.resolution ||
      next.duration !== store.draft.duration
    ) {
      store.setDraft(next);
    }
    const ref = store.ref;
    if (ref && ref.status === "done") {
      const problem = refVideoProblem(
        videoModel.capabilities,
        { durationSec: ref.durationSec, sizeBytes: ref.sizeBytes },
        videoModel.label,
        refVideoMaxSeconds(videoModel.capabilities) ?? options.refVideoMaxSeconds,
      );
      if (problem) {
        store.setRef(null);
        releasePreview(ref.id);
        toast.warning(`${problem}，已移除参考视频`);
      }
    }
  }, [videoModel, platformRatio, options.refVideoMaxSeconds]);

  /** 写脚本预估积分：设置变化后防抖请求；失败不拦发送，由后端兜底 */
  const [credits, setCredits] = useState<number | null>(null);
  const quoteKey = useMemo(
    () =>
      videoModel
        ? JSON.stringify([project?.id ?? "", hasRef, videoModel.key, draft, hasModelImage])
        : null,
    [videoModel, project?.id, hasRef, draft, hasModelImage],
  );
  useEffect(() => {
    if (!videoModel || !quoteKey) return;
    let alive = true;
    const timer = window.setTimeout(() => {
      quoteMarketing({
        projectId: project?.id,
        hasRef,
        settings: toRoundSettings({ draft, hasRef, videoModel }),
      })
        .then((quote) => alive && setCredits(quote.credits))
        .catch(() => alive && setCredits(null));
    }, QUOTE_DEBOUNCE_MS);
    return () => {
      alive = false;
      window.clearTimeout(timer);
    };
    // quoteKey 已经包含了 draft / 模型 / 参考的全部变化
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [quoteKey]);

  /** 余额没拿到时拉一次：发送前要知道够不够 */
  useEffect(() => {
    if (available === null) void refreshCredits();
  }, [available, refreshCredits]);

  const state = useMemo(
    () =>
      evaluateMarketingSend(
        {
          dock,
          productCount,
          hasModelImage,
          uploading,
          uploadFailed,
          ref: refSlot ? { durationSec: refSlot.durationSec, sizeBytes: refSlot.sizeBytes } : null,
          videoModel,
          visionModels: vision,
          credits,
          available,
          sending,
          scripting,
        },
        draft,
      ),
    [
      dock,
      productCount,
      hasModelImage,
      uploading,
      uploadFailed,
      refSlot,
      videoModel,
      vision,
      credits,
      available,
      sending,
      scripting,
      draft,
    ],
  );

  const count = planCount({
    hasRef,
    angles: draft.angles,
    autoCount: draft.autoCount,
    adaptCount: draft.adaptCount,
  });

  /** 上传一个素材并把结果写回对应位置；失败的提示由请求层统一弹 */
  const runUpload = useCallback(
    async (
      file: File,
      slot: MarketingSlot,
      update: (id: string, patch: Partial<MarketingSlot>) => void,
      onDone?: (assetDurationSec: number | undefined) => void,
    ) => {
      try {
        const asset = await uploadAsset(file);
        releasePreview(slot.id);
        update(slot.id, {
          assetId: asset.id,
          url: asset.url,
          status: "done",
          durationSec: asset.durationMs ? asset.durationMs / 1000 : undefined,
        });
        onDone?.(asset.durationMs ? asset.durationMs / 1000 : undefined);
      } catch {
        update(slot.id, { status: "error" });
      }
    },
    [],
  );

  const addProductFiles = useCallback(
    (files: File[]) => {
      const store = useMarketingComposerStore.getState();
      for (const file of files) {
        if (!file.type.startsWith("image/")) {
          toast.error("商品图只支持图片");
          continue;
        }
        if (useMarketingComposerStore.getState().products.length >= MAX_PRODUCTS) {
          toast.error(`商品图最多 ${MAX_PRODUCTS} 张`);
          break;
        }
        const caps = videoModel?.capabilities;
        const used =
          useMarketingComposerStore.getState().products.length +
          (useMarketingComposerStore.getState().model ? 1 : 0);
        const reason = caps ? refUploadError(caps.refs.image, "image", file, used) : null;
        if (reason) {
          toast.error(reason);
          continue;
        }
        const slot = newSlot(file);
        store.addProduct(slot);
        void runUpload(file, slot, store.updateProduct);
      }
    },
    [videoModel, runUpload],
  );

  const setModelFile = useCallback(
    (file: File) => {
      const store = useMarketingComposerStore.getState();
      if (!file.type.startsWith("image/")) {
        toast.error("模特图只支持图片");
        return;
      }
      const caps = videoModel?.capabilities;
      const reason = caps
        ? refUploadError(caps.refs.image, "image", file, store.products.length)
        : null;
      if (reason) {
        toast.error(reason);
        return;
      }
      if (store.model) releasePreview(store.model.id);
      const slot = newSlot(file);
      store.setModel(slot);
      void runUpload(file, slot, store.updateModel);
    },
    [videoModel, runUpload],
  );

  const setRefFile = useCallback(
    (file: File) => {
      const store = useMarketingComposerStore.getState();
      if (!file.type.startsWith("video/")) {
        toast.error("参考只支持视频");
        return;
      }
      if (videoModel) {
        const problem = refVideoProblem(
          videoModel.capabilities,
          { sizeBytes: file.size },
          videoModel.label,
        );
        if (problem) {
          toast.error(problem);
          return;
        }
      }
      if (store.ref) releasePreview(store.ref.id);
      const slot = newSlot(file);
      store.setRef(slot);
      void runUpload(file, slot, store.updateRef, (durationSec) => {
        if (!videoModel) return;
        const problem = refVideoProblem(
          videoModel.capabilities,
          { durationSec },
          videoModel.label,
          refVideoMaxSeconds(videoModel.capabilities) ?? options.refVideoMaxSeconds,
        );
        if (problem) {
          toast.error(problem);
          useMarketingComposerStore.getState().setRef(null);
        }
      });
    },
    [videoModel, runUpload, options.refVideoMaxSeconds],
  );

  const removeProduct = useCallback((id: string) => {
    releasePreview(id);
    useMarketingComposerStore.getState().removeProduct(id);
  }, []);
  const clearProducts = useCallback(() => {
    const store = useMarketingComposerStore.getState();
    for (const item of store.products) releasePreview(item.id);
    store.products.forEach((item) => store.removeProduct(item.id));
  }, []);
  const clearModel = useCallback(() => {
    const store = useMarketingComposerStore.getState();
    if (store.model) releasePreview(store.model.id);
    store.setModel(null);
  }, []);
  const clearRef = useCallback(() => {
    const store = useMarketingComposerStore.getState();
    if (store.ref) releasePreview(store.ref.id);
    store.setRef(null);
  }, []);

  const setPlatform = useCallback(
    (platform: PlatformOption) =>
      useMarketingComposerStore
        .getState()
        .setDraft(pickPlatform(platform, videoModel?.capabilities)),
    [videoModel],
  );

  /** 点角度：无参考最多 4 个，有参考最多改编份数 */
  const pickAngle = useCallback(
    (key: MarketingAngle) => {
      const store = useMarketingComposerStore.getState();
      const max = hasRef ? Math.max(store.draft.adaptCount, 1) : MAX_ANGLES;
      store.setDraft({ angles: toggleAngle(store.draft.angles, key, max) });
    },
    [hasRef],
  );

  const clearAngles = useCallback(
    () => useMarketingComposerStore.getState().setDraft({ angles: [] }),
    [],
  );

  const setAutoCount = useCallback(
    (n: number) => useMarketingComposerStore.getState().setDraft({ autoCount: n }),
    [],
  );

  /** 改编份数 0–3；已选的角度超出份数时截掉 */
  const setAdaptCount = useCallback((n: number) => {
    const store = useMarketingComposerStore.getState();
    const adaptCount = Math.min(MAX_ADAPT, Math.max(0, n));
    store.setDraft({
      adaptCount,
      angles: store.draft.angles.slice(0, Math.max(adaptCount, 0)),
    });
  }, []);

  /** 提交：列表页建项目并进详情，dock 在当前项目开新一轮 */
  const submit = useCallback(async () => {
    if (!state.canSend || !videoModel) return;
    const store = useMarketingComposerStore.getState();
    const settings = toRoundSettings({ draft: store.draft, hasRef, videoModel });
    const modelAssetId = toAssetNumber(store.model?.assetId) ?? undefined;
    const refVideoAssetId = toAssetNumber(store.ref?.assetId) ?? undefined;
    store.setSending(true);
    try {
      if (project) {
        const round = await createMarketingRound(project.id, {
          modelAssetId,
          refVideoAssetId,
          settings,
        });
        useMarketingStore.getState().applyRound(project.id, round);
        useMarketingStore.getState().bumpList();
        store.clearAfterRound();
      } else {
        const created = await createMarketingProject({
          productAssetIds: store.products
            .map((item) => toAssetNumber(item.assetId))
            .filter((id): id is number => id !== null),
          modelAssetId,
          refVideoAssetId,
          settings,
        });
        useMarketingStore.getState().setProject(created);
        clearProducts();
        store.clearAfterProject();
        navigate(`/marketing/${created.id}`);
      }
      void refreshCredits();
    } catch {
      // 请求层已弹错误提示（积分不足 40001、正在写脚本 68007 等），保留草稿方便重试
    } finally {
      useMarketingComposerStore.getState().setSending(false);
    }
  }, [state.canSend, videoModel, hasRef, project, navigate, clearProducts, refreshCredits]);

  return {
    dock,
    options,
    draft,
    products,
    modelSlot,
    refSlot,
    productCount,
    hasModelImage,
    hasRef,
    count,
    videoModels: video.models,
    videoStatus: video.status,
    videoModel,
    support,
    textModels: vision,
    textStatus: text.status,
    credits,
    available,
    state,
    sending,
    addProductFiles,
    setModelFile,
    setRefFile,
    removeProduct,
    clearProducts,
    clearModel,
    clearRef,
    setDraft: useMarketingComposerStore.getState().setDraft,
    setPlatform,
    pickAngle,
    clearAngles,
    setAutoCount,
    setAdaptCount,
    submit,
  };
}
