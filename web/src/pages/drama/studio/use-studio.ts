import {
  createContext,
  useCallback,
  useContext,
  useEffect,
  useMemo,
  useRef,
  useState,
} from "react";
import { toast } from "sonner";

import { addSegment, deleteSegment, reorderSegments, updateSegment } from "@/api/drama";
import { DRAMA_ERR, dramaErrorMessage, isDramaError, isRevisionConflict } from "@/api/drama/errors";
import type { Drama, EpisodeSummary, Segment, UpdateSegmentInput } from "@/api/drama/type";
import type { ModelInfo } from "@/api/model/type";
import { confirm } from "@/components/admin-ui/confirm-dialog";
import { useDramaAssets, type DramaAssetsState } from "@/hooks/use-drama-assets";
import { useDramaVideoModel } from "@/hooks/use-drama-video-model";
import { useRemoteModels } from "@/hooks/use-models";
import { useCreditsStore } from "@/store/credits";
import { useDramaJobsStore } from "@/store/drama";
import { segmentKey, useDramaSegmentsStore } from "@/store/drama-segments";
import { useStudioPrefs, useStudioPrefsStore } from "@/store/drama-studio";
import { reloadSegments } from "@/utils/drama/segment-events";
import type { RefAsset } from "@/utils/drama/segment-assets";
import { planKeyframeInput, planSegmentInput, type SegmentPlan } from "@/utils/drama/segment-input";
import {
  applyOrder,
  batchTargets,
  chooseCheck,
  isRunning,
  orderIds,
  sameOrder,
} from "@/utils/drama/segments";
import { describeSubmitError } from "@/utils/tasks/submit";
import { submitSegmentTask } from "@/utils/tasks/gateway";
import { priceSpecOf } from "@/utils/tasks/capabilities";
import { quote } from "@/utils/pricing/quote";

import { summarizeSave, type SaveSummary } from "../workspace/use-workspace";
import type { AutosaveStatus } from "@/utils/drama/autosave";

const NO_SEGMENTS: Segment[] = [];
/** 有片段在生成时多久对账一次：WebSocket 是主通道，这里兜底 */
const POLL_MS = 8000;
/** 新增片段的默认时长（秒），不超过模型上限 */
const DEFAULT_SECONDS = 10;

/** 片段工作区的全部状态与动作；各区域组件从 context 取 */
export type Studio = {
  drama: Drama;
  episode: EpisodeSummary;
  no: number;
  segments: Segment[];
  segmentsStatus: "loading" | "ready" | "error";
  selected: Segment | null;
  select: (id: string) => void;
  assets: RefAsset[];
  assetsStatus: DramaAssetsState["status"];
  video: ReturnType<typeof useDramaVideoModel>;
  /** 首帧用的图片模型 */
  image: { models: ModelInfo[]; model: ModelInfo | undefined; setModel: (key: string) => void };
  /** 顶栏选的生成参数（分辨率等） */
  overrides: Record<string, unknown>;
  setOverride: (name: string, value: unknown) => void;
  /** 选中片段的编辑中提示词（还没存下来的也算），参考素材条跟着它变 */
  prompt: string;
  setDraft: (id: string, text: string) => void;
  /** 选中片段此刻的组装结果 */
  plan: SegmentPlan | null;
  /** 选中片段的预估积分；算不出来为 undefined */
  credits: number | undefined;
  keyframeCredits: number | undefined;
  availableCredits: number | null;
  save: SaveSummary;
  reportSave: (key: string, status: AutosaveStatus) => void;
  /** 正在提交（视频 / 首帧）的片段 */
  submitting: ReadonlySet<string>;
  /** 提示词编辑器登记：插入引用 / 立即存盘 */
  registerEditor: (handle: EditorHandle | null) => void;
  insertRef: (asset: RefAsset) => void;
  actions: {
    add: () => Promise<void>;
    remove: (segment: Segment) => Promise<void>;
    reorder: (ids: string[]) => Promise<void>;
    setSeconds: (segment: Segment, seconds: number) => Promise<void>;
    patch: (sid: string, fields: Omit<UpdateSegmentInput, "revision">) => Promise<Segment>;
    choose: (segment: Segment, taskId: string) => Promise<void>;
    generate: (segment: Segment) => Promise<void>;
    keyframe: (segment: Segment) => Promise<void>;
    batch: () => Promise<void>;
  };
};

export type EditorHandle = {
  insertRef: (asset: RefAsset) => void;
  flush: () => Promise<void>;
};

export const StudioContext = createContext<Studio | null>(null);

export function useStudio(): Studio {
  const value = useContext(StudioContext);
  if (!value) throw new Error("useStudio 必须在片段工作区内部使用");
  return value;
}

/**
 * 片段工作区的数据层：片段列表（HTTP 读取 + WebSocket 推送 + 生成中兜底轮询）、资产库、模型与偏好、
 * 选中的片段及其参考素材组装、积分预估，以及增删改排序、采用结果、立即生成、首帧、批量生成这些动作。
 */
export function useStudioState(drama: Drama, no: number, refresh: () => Promise<void>): Studio {
  const dramaId = drama.id;
  const episode = drama.episodes.find((ep) => ep.no === no);
  const key = segmentKey(dramaId, no);
  const list = useDramaSegmentsStore((state) => state.byEpisode[key]);
  const segments = list ?? NO_SEGMENTS;
  const [segmentsStatus, setSegmentsStatus] = useState<Studio["segmentsStatus"]>("loading");
  const [selectedId, setSelectedId] = useState<string | null>(null);
  const [draft, setDraftState] = useState<{ id: string; text: string } | null>(null);
  const [submitting, setSubmitting] = useState<ReadonlySet<string>>(new Set());
  const [saveStates, setSaveStates] = useState<Record<string, AutosaveStatus>>({});
  const editor = useRef<EditorHandle | null>(null);
  const queues = useRef(new Map<string, Promise<unknown>>());

  // 载入 + 登记「正在看这一集」（WebSocket 重连 / 回到前台时据此对账）
  useEffect(() => {
    let active = true;
    void reloadSegments(dramaId, no).then(
      (ok) => active && setSegmentsStatus(ok ? "ready" : "error"),
    );
    const unwatch = useDramaSegmentsStore.getState().watch(dramaId, no);
    return () => {
      active = false;
      unwatch();
    };
  }, [dramaId, no]);

  // 有片段在生成：定时对账，推送丢了也能看到结果
  const anyRunning = segments.some(isRunning);
  useEffect(() => {
    if (!anyRunning) return;
    const timer = setInterval(() => void reloadSegments(dramaId, no), POLL_MS);
    return () => clearInterval(timer);
  }, [anyRunning, dramaId, no]);

  const settled = useDramaJobsStore((state) => state.settled[dramaId] ?? 0);
  const {
    assets,
    status: assetsStatus,
    reload: reloadAssets,
  } = useDramaAssets(drama.canvasId, settled);
  const video = useDramaVideoModel(dramaId);
  const imageRemote = useRemoteModels("image");
  const prefs = useStudioPrefs(dramaId);
  const imageModel = useMemo(
    () => imageRemote.models.find((m) => m.key === prefs.imageModel) ?? imageRemote.models[0],
    [imageRemote.models, prefs.imageModel],
  );
  const availableCredits = useCreditsStore((state) => state.credits?.available ?? null);

  const selected = useMemo(
    () => segments.find((item) => item.id === selectedId) ?? segments[0] ?? null,
    [segments, selectedId],
  );
  const prompt = selected ? (draft?.id === selected.id ? draft.text : selected.prompt) : "";
  const setDraft = useCallback((id: string, text: string) => setDraftState({ id, text }), []);

  const caps = video.model?.capabilities;
  const plan = useMemo(
    () =>
      selected
        ? planSegmentInput({
            caps,
            prompt,
            seconds: selected.seconds,
            ratio: drama.ratio,
            overrides: prefs.overrides,
            assets,
            keyframeAssetId: selected.keyframe?.id ?? null,
          })
        : null,
    [assets, caps, drama.ratio, prefs.overrides, prompt, selected],
  );
  const credits = useMemo(() => {
    if (!plan || !video.model || Object.keys(plan.input).length === 0) return undefined;
    return quote(video.model.pricing, caps, priceSpecOf(caps, plan.input));
  }, [caps, plan, video.model]);
  const keyframeCredits = useMemo(() => {
    if (!selected || !imageModel) return undefined;
    const built = planKeyframeInput({
      caps: imageModel.capabilities,
      prompt,
      ratio: drama.ratio,
      assets,
    });
    return quote(
      imageModel.pricing,
      imageModel.capabilities,
      priceSpecOf(imageModel.capabilities, built.input),
    );
  }, [assets, drama.ratio, imageModel, prompt, selected]);

  const reportSave = useCallback((name: string, status: AutosaveStatus) => {
    setSaveStates((prev) => (prev[name] === status ? prev : { ...prev, [name]: status }));
  }, []);
  const save = useMemo(() => summarizeSave(saveStates), [saveStates]);

  /** 改片段：同一个片段的修改串行，永远用 store 里最新的 revision；冲突 / 不存在时提示并重新读取 */
  const patch = useCallback<Studio["actions"]["patch"]>(
    (sid, fields) => {
      const run = async () => {
        const current = useDramaSegmentsStore
          .getState()
          .byEpisode[key]?.find((item) => item.id === sid);
        if (!current) throw new Error("片段不存在");
        try {
          const next = await updateSegment(sid, { ...fields, revision: current.revision });
          useDramaSegmentsStore.getState().apply(dramaId, no, next);
          return next;
        } catch (e) {
          if (isRevisionConflict(e)) {
            toast.warning("这个片段在别处被修改过，已为你加载最新内容");
            void reloadSegments(dramaId, no);
          } else if (isDramaError(e, DRAMA_ERR.segmentNotFound)) {
            toast.error(dramaErrorMessage(e));
            useDramaSegmentsStore.getState().remove(dramaId, no, sid);
          } else if (isDramaError(e, DRAMA_ERR.takeInvalid)) {
            toast.error(dramaErrorMessage(e));
            void reloadSegments(dramaId, no);
          } else {
            toast.error(dramaErrorMessage(e, "保存失败，请稍后重试"));
          }
          throw e;
        }
      };
      const prev = queues.current.get(sid) ?? Promise.resolve();
      const result = prev.then(run, run);
      queues.current.set(
        sid,
        result.catch(() => undefined),
      );
      return result;
    },
    [dramaId, key, no],
  );

  /** 带保存状态的一次性修改（时长、采用） */
  const patchWithStatus = useCallback(
    async (sid: string, fields: Omit<UpdateSegmentInput, "revision">) => {
      reportSave("op", "saving");
      try {
        await patch(sid, fields);
      } finally {
        reportSave("op", "saved");
      }
    },
    [patch, reportSave],
  );

  const markSubmitting = useCallback((sid: string, on: boolean) => {
    setSubmitting((prev) => {
      const next = new Set(prev);
      if (on) next.add(sid);
      else next.delete(sid);
      return next;
    });
  }, []);

  /**
   * 提交前重读一次画布里的资产，用最新的形象和声音组装（资产页刚改的、还没被定时重读捕到的也算）；
   * 读取失败返回 null
   */
  const freshAssets = useCallback(() => reloadAssets(), [reloadAssets]);

  const submitVideo = useCallback(
    async (segment: Segment, latest: RefAsset[]) => {
      const model = video.model;
      if (!model) return { ok: false as const, message: "没有可用的视频模型" };
      const built = planSegmentInput({
        caps: model.capabilities,
        prompt: segment.prompt,
        seconds: segment.seconds,
        ratio: drama.ratio,
        overrides: prefs.overrides,
        assets: latest,
        keyframeAssetId: segment.keyframe?.id ?? null,
      });
      const firstError = Object.values(built.errors)[0];
      if (firstError) return { ok: false as const, message: firstError };
      try {
        await submitSegmentTask("video", segment.id, { model_id: model.key, input: built.input });
        return { ok: true as const };
      } catch (e) {
        return { ok: false as const, message: describeSubmitError(e).message };
      }
    },
    [drama.ratio, prefs.overrides, video.model],
  );

  const generate = useCallback<Studio["actions"]["generate"]>(
    async (segment) => {
      if (submitting.has(segment.id)) return;
      markSubmitting(segment.id, true);
      try {
        // 编辑中的提示词先存盘，提交的就是屏幕上看到的
        await editor.current?.flush();
        const fresh = useDramaSegmentsStore
          .getState()
          .byEpisode[key]?.find((item) => item.id === segment.id);
        const current = fresh ?? segment;
        const latest = await freshAssets();
        if (!latest) {
          toast.error("资产信息读取失败，请稍后重试");
          return;
        }
        const preview = planSegmentInput({
          caps: video.model?.capabilities,
          prompt: current.prompt,
          seconds: current.seconds,
          ratio: drama.ratio,
          overrides: prefs.overrides,
          assets: latest,
          keyframeAssetId: current.keyframe?.id ?? null,
        });
        if (preview.deadLabels.length > 0) {
          const go = await confirm({
            title: "有引用已失效",
            description: `「${preview.deadLabels.join("」「")}」对应的资产已被删除，这部分只会按文字提交。要继续生成吗？`,
            confirmLabel: "继续生成",
          });
          if (!go) return;
        }
        const result = await submitVideo(current, latest);
        if (!result.ok) {
          toast.error(result.message);
          return;
        }
        void reloadSegments(dramaId, no);
      } finally {
        markSubmitting(segment.id, false);
      }
    },
    [
      drama.ratio,
      dramaId,
      freshAssets,
      key,
      markSubmitting,
      no,
      prefs.overrides,
      submitVideo,
      submitting,
      video.model,
    ],
  );

  const keyframe = useCallback<Studio["actions"]["keyframe"]>(
    async (segment) => {
      if (!imageModel || submitting.has(segment.id)) {
        if (!imageModel) toast.error("没有可用的图片模型");
        return;
      }
      markSubmitting(segment.id, true);
      try {
        await editor.current?.flush();
        const current =
          useDramaSegmentsStore.getState().byEpisode[key]?.find((item) => item.id === segment.id) ??
          segment;
        const latest = await freshAssets();
        if (!latest) {
          toast.error("资产信息读取失败，请稍后重试");
          return;
        }
        const built = planKeyframeInput({
          caps: imageModel.capabilities,
          prompt: current.prompt,
          ratio: drama.ratio,
          assets: latest,
        });
        const firstError = Object.values(built.errors)[0];
        if (firstError) {
          toast.error(firstError);
          return;
        }
        try {
          await submitSegmentTask("image", segment.id, {
            model_id: imageModel.key,
            input: built.input,
          });
          toast.info("首帧生成中，完成后这个片段会改走图生视频");
          void reloadSegments(dramaId, no);
        } catch (e) {
          toast.error(describeSubmitError(e).message);
        }
      } finally {
        markSubmitting(segment.id, false);
      }
    },
    [drama.ratio, dramaId, freshAssets, imageModel, key, markSubmitting, no, submitting],
  );

  const batch = useCallback<Studio["actions"]["batch"]>(async () => {
    const targets = batchTargets(useDramaSegmentsStore.getState().byEpisode[key] ?? NO_SEGMENTS);
    if (targets.length === 0) {
      toast.info("没有待生成的片段");
      return;
    }
    await editor.current?.flush();
    const latest = await freshAssets();
    if (!latest) {
      toast.error("资产信息读取失败，请稍后重试");
      return;
    }
    // 引用失效的先一起问一次，允许继续
    const dead = new Set<string>();
    for (const item of targets) {
      const planned = planSegmentInput({
        caps: video.model?.capabilities,
        prompt: item.prompt,
        seconds: item.seconds,
        ratio: drama.ratio,
        overrides: prefs.overrides,
        assets: latest,
        keyframeAssetId: item.keyframe?.id ?? null,
      });
      planned.deadLabels.forEach((label) => dead.add(label));
    }
    if (dead.size > 0) {
      const go = await confirm({
        title: "有引用已失效",
        description: `「${[...dead].join("」「")}」对应的资产已被删除，这部分只会按文字提交。要继续批量生成吗？`,
        confirmLabel: "继续生成",
      });
      if (!go) return;
    }
    let ok = 0;
    const failed: string[] = [];
    for (const item of targets) {
      markSubmitting(item.id, true);
      const result = await submitVideo(item, latest);
      markSubmitting(item.id, false);
      if (result.ok) ok += 1;
      else {
        failed.push(result.message);
        // 积分不足、排队已满之类每个片段都一样，不必再试
        if (/积分不足|排队任务已达上限/.test(result.message)) break;
      }
    }
    void reloadSegments(dramaId, no);
    if (ok > 0) toast.info(`已提交 ${ok} 个片段`);
    if (failed.length > 0) toast.error(`有 ${targets.length - ok} 个片段没能提交：${failed[0]}`);
  }, [
    drama.ratio,
    dramaId,
    freshAssets,
    key,
    markSubmitting,
    no,
    prefs.overrides,
    submitVideo,
    video.model,
  ]);

  const add = useCallback<Studio["actions"]["add"]>(async () => {
    try {
      const max = video.model?.capabilities.params?.duration?.max;
      const created = await addSegment(dramaId, no, {
        seconds: max ? Math.min(DEFAULT_SECONDS, max) : DEFAULT_SECONDS,
      });
      useDramaSegmentsStore.getState().apply(dramaId, no, created);
      setSelectedId(created.id);
      void refresh();
    } catch (e) {
      toast.error(dramaErrorMessage(e, "新增失败，请稍后重试"));
    }
  }, [dramaId, no, refresh, video.model]);

  const remove = useCallback<Studio["actions"]["remove"]>(
    async (segment) => {
      const ok = await confirm({
        title: `删除片段 ${String(segment.seq + 1).padStart(2, "0")}？`,
        description: "只删除这个片段和它的提示词；已经生成的视频和素材不会被删除。",
        confirmLabel: "删除",
        destructive: true,
        onConfirm: () => deleteSegment(segment.id),
      });
      if (!ok) return;
      useDramaSegmentsStore.getState().remove(dramaId, no, segment.id);
      void reloadSegments(dramaId, no);
      void refresh();
    },
    [dramaId, no, refresh],
  );

  const reorder = useCallback<Studio["actions"]["reorder"]>(
    async (ids) => {
      const current = useDramaSegmentsStore.getState().byEpisode[key] ?? NO_SEGMENTS;
      if (sameOrder(ids, orderIds(current))) return;
      // 先在本地换序，界面不等请求
      useDramaSegmentsStore.getState().setList(dramaId, no, applyOrder(current, ids));
      try {
        useDramaSegmentsStore
          .getState()
          .setList(dramaId, no, await reorderSegments(dramaId, no, ids));
        // 顺序变了：成片是否过期要跟着刷新
        void refresh();
      } catch (e) {
        toast.error(dramaErrorMessage(e, "排序失败，已恢复原顺序"));
        void reloadSegments(dramaId, no);
      }
    },
    [dramaId, key, no, refresh],
  );

  const setSeconds = useCallback<Studio["actions"]["setSeconds"]>(
    async (segment, seconds) => {
      if (seconds === segment.seconds) return;
      await patchWithStatus(segment.id, { seconds }).catch(() => undefined);
      void refresh();
    },
    [patchWithStatus, refresh],
  );

  const choose = useCallback<Studio["actions"]["choose"]>(
    async (segment, taskId) => {
      const check = chooseCheck(segment, taskId);
      if (check === "already") return;
      if (check === "invalid") {
        toast.error(dramaErrorMessage(null, "只能采用本片段已成功的结果"));
        return;
      }
      await patchWithStatus(segment.id, { chosenTaskId: taskId }).catch(() => undefined);
      void refresh();
    },
    [patchWithStatus, refresh],
  );

  const registerEditor = useCallback((handle: EditorHandle | null) => {
    editor.current = handle;
  }, []);
  const insertRef = useCallback((asset: RefAsset) => editor.current?.insertRef(asset), []);

  const setOverride = useCallback(
    (name: string, value: unknown) =>
      useStudioPrefsStore
        .getState()
        .patch(dramaId, { overrides: { ...prefs.overrides, [name]: value } }),
    [dramaId, prefs.overrides],
  );
  const imageSetModel = useCallback(
    (modelKey: string) => useStudioPrefsStore.getState().patch(dramaId, { imageModel: modelKey }),
    [dramaId],
  );

  return {
    drama,
    // 调用方保证这一集存在
    episode: episode as EpisodeSummary,
    no,
    segments,
    segmentsStatus,
    selected,
    select: setSelectedId,
    assets,
    assetsStatus,
    video,
    image: { models: imageRemote.models, model: imageModel, setModel: imageSetModel },
    overrides: prefs.overrides,
    setOverride,
    prompt,
    setDraft,
    plan,
    credits,
    keyframeCredits,
    availableCredits,
    save,
    reportSave,
    submitting,
    registerEditor,
    insertRef,
    actions: { add, remove, reorder, setSeconds, patch, choose, generate, keyframe, batch },
  };
}
