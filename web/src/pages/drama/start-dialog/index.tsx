import { useCallback, useMemo, useState } from "react";
import { FileText, Loader2 } from "lucide-react";

import { dramaErrorMessage } from "@/api/drama/errors";
import type {
  CreateDramaInput,
  MyPrompt,
  PromptChoice,
  PromptStep,
  SourcePreflight,
  StartRatio,
} from "@/api/drama/type";
import { AgentModelPicker, useAgentModel } from "@/components/drama/agent-model-picker";
import {
  ChipPopover,
  GenreList,
  OptionRow,
  SecondsList,
  StyleGrid,
} from "@/components/drama/pickers";
import { PromptEditorPane } from "@/components/drama/prompts/prompt-editor";
import { PromptRows } from "@/components/drama/prompts/prompt-rows";
import {
  usePromptLibrary,
  type PromptLibrary,
} from "@/components/drama/prompts/use-prompt-library";
import { ModelMenu, VideoModelMenu } from "@/components/drama/video-model-menu";
import { Button } from "@/components/ui/button";
import { Dialog, DialogContent } from "@/components/ui/dialog";
import { Input } from "@/components/ui/input";
import { useDramaStyles } from "@/hooks/use-drama-styles";
import { useRemoteModels } from "@/hooks/use-models";
import { cn } from "@/lib/utils";
import type { ModelInfo } from "@/api/model/type";
import {
  CUSTOM_STYLE_KEY,
  DEFAULT_EPISODE_SECONDS,
  DEFAULT_STYLE_KEY,
} from "@/utils/drama/options";
import {
  DEFAULT_START_RATIO,
  START_RATIOS,
  buildCreateInput,
  builtinPrompts,
  charsLabel,
  durationLabel,
  emptyImageModels,
  episodesLabel,
  loadStartPrefs,
  modelDisabledReason,
  normalizeRangeEnd,
  reconcilePrompts,
  saveStartPrefs,
  secondsLock,
  startBlockReason,
  unitWord,
  type StartForm,
  type StartKind,
} from "@/utils/drama/start-settings";

import { useSourceEstimate } from "./use-source-estimate";

/** 预检结果里识别出的类型；认不出为 null（用户必须手选） */
const guessedKind = (pre: SourcePreflight): StartKind | null =>
  pre.kindGuess === "unknown" ? null : pre.kindGuess;

/** 弹窗初始内容：上次记住的选择 + 预检识别的类型；范围默认全部 */
function initialForm(pre: SourcePreflight): StartForm {
  const prefs = loadStartPrefs();
  return {
    kind: guessedKind(pre),
    rangeEnd: 0,
    ratio: prefs?.ratio ?? DEFAULT_START_RATIO,
    genre: prefs?.genre ?? "",
    styleKey: prefs?.styleKey ?? "",
    styleCustom: prefs?.styleCustom ?? "",
    seconds: prefs?.seconds ?? DEFAULT_EPISODE_SECONDS,
    textModel: prefs?.textModel ?? "",
    videoModel: prefs?.videoModel ?? "",
    imageModels: prefs?.imageModels ?? emptyImageModels(),
    prompts: prefs?.prompts ?? builtinPrompts(),
  };
}

/** 资产图片模型三个胶囊：键对应请求里的 image_models，前缀是胶囊上的字样 */
const IMAGE_MODEL_SLOTS = [
  { key: "character", prefix: "人物" },
  { key: "scene", prefix: "场景" },
  { key: "prop", prefix: "物品" },
] as const;

/** 选的模型已被下线（清单加载完且里面没有）时回落为空串；只在读取时推导，不回写 */
const liveModelKey = (key: string, remote: { status: string; models: ModelInfo[] }): string =>
  key && remote.status === "ready" && !remote.models.some((m) => m.key === key) ? "" : key;

/** 两行之间的分隔线 */
const Divider = () => <div className="bg-chrome-border h-px" />;

/** 一个带标签的设置项 */
function Field({ label, children }: { label: string; children: React.ReactNode }) {
  return (
    <div className="grid gap-2 px-3">
      <div className="text-muted-foreground text-[13px]">{label}</div>
      {children}
    </div>
  );
}

/** 画幅按钮：带比例示意框 */
function RatioButton({
  ratio,
  active,
  onPick,
}: {
  ratio: (typeof START_RATIOS)[number];
  active: boolean;
  onPick: () => void;
}) {
  return (
    <button
      type="button"
      aria-pressed={active}
      onClick={onPick}
      className={cn(
        "focus-visible:ring-ring/50 flex h-16 w-28 flex-col items-center justify-center gap-1.5 rounded-2xl text-[13px] tabular-nums outline-none focus-visible:ring-2",
        active
          ? "bg-chrome-hover ring-foreground/60 font-medium ring-1.5"
          : "bg-chrome-hover/50 ring-chrome-border hover:bg-chrome-hover ring-1",
      )}
    >
      <i
        aria-hidden
        className="rounded-[3px] border-[1.5px] border-current"
        style={{ width: ratio.w, height: ratio.h }}
      />
      {ratio.key}
    </button>
  );
}

/** 规划范围下拉：全部 / 几个常用值 / 自己输入第 N 个 */
function RangeList({
  total,
  word,
  rangeEnd,
  onPick,
  close,
}: {
  total: number;
  word: string;
  rangeEnd: number;
  onPick: (rangeEnd: number) => void;
  close: () => void;
}) {
  const [draft, setDraft] = useState(String(rangeEnd || Math.min(total, 120)));
  const commit = () => {
    onPick(normalizeRangeEnd(Number(draft), total));
    close();
  };
  return (
    <>
      <OptionRow
        active={rangeEnd === 0}
        onClick={() => {
          onPick(0);
          close();
        }}
      >
        全部（{total.toLocaleString("en-US")} {word}）
      </OptionRow>
      {[20, 60, 120, 300]
        .filter((n) => n < total)
        .map((n) => (
          <OptionRow
            key={n}
            active={rangeEnd === n}
            onClick={() => {
              onPick(n);
              close();
            }}
          >
            规划到第 {n} {word}
          </OptionRow>
        ))}
      <div className="flex items-center gap-1.5 px-2 py-1 text-[13px]">
        第
        <Input
          type="number"
          min={1}
          max={total}
          value={draft}
          aria-label={`规划到第几${word}`}
          onChange={(event) => setDraft(event.target.value)}
          onKeyDown={(event) => event.key === "Enter" && commit()}
          onBlur={commit}
          className="h-8 w-24 tabular-nums"
        />
        {word}
      </div>
    </>
  );
}

/**
 * 开拍设置（弹窗主体，单页，自上而下：来源 → 规划范围 → 画幅 → 题材 / 画风 / 单集时长 → 文本模型 → 提示词 → 取消 / 开始）。
 * 提示词行的「新建 / 编辑」会让整页向左滑到编辑器，← 返回原位。
 * 剧本型输入：剧本改编置灰；有「第N集」时单集时长锁成「按原分集 · N 集」；规划范围不可选（一次切完）。
 * 识别不出原文类型时胶囊显示「请选择」并描边警示，「开始」禁用直到选好。
 */
export function StartBody({
  pre,
  library,
  onCancel,
  onSubmit,
}: {
  /** 预检结果 */
  pre: SourcePreflight;
  /** 提示词库（内置正文、我的提示词、技能） */
  library: PromptLibrary;
  /** 取消：保留卡片里的文件 / 文本 */
  onCancel: () => void;
  /** 点「开始」：调用方用请求体建剧；失败时抛出，错误在弹窗里就地显示 */
  onSubmit: (input: CreateDramaInput) => Promise<void>;
}) {
  const { genres, styles } = useDramaStyles();
  const [form, setForm] = useState<StartForm>(() => initialForm(pre));
  const [editor, setEditor] = useState<{ step: PromptStep; item: MyPrompt | null } | null>(null);
  const [submitting, setSubmitting] = useState(false);
  const [error, setError] = useState<string | null>(null);
  const patch = useCallback((change: Partial<StartForm>) => {
    setForm((prev) => ({ ...prev, ...change }));
  }, []);

  /** 提示词选择：库加载完后，被删的我的提示词 / 停用的技能回落为内置（读取时推导，不回写） */
  const prompts = useMemo(
    () =>
      library.ready ? reconcilePrompts(form.prompts, library.mine, library.skills) : form.prompts,
    [form.prompts, library.ready, library.mine, library.skills],
  );

  const word = unitWord(pre.unitKind);
  const script = form.kind === "script";
  const lock = secondsLock(form.kind, pre);
  /** 画风：用户没选时取默认（清单里有就用默认 key，没有取第一个） */
  const styleKey =
    (styles.some((item) => item.key === form.styleKey) || form.styleKey === CUSTOM_STYLE_KEY
      ? form.styleKey
      : "") ||
    (styles.find((item) => item.key === DEFAULT_STYLE_KEY) ?? styles[0])?.key ||
    "";
  const styleName =
    styleKey === CUSTOM_STYLE_KEY
      ? "自定义画风"
      : (styles.find((item) => item.key === styleKey)?.name ?? "画风");
  const genreName = genres.find((item) => item.key === form.genre)?.name ?? form.genre;

  const agent = useAgentModel(form.textModel);
  const modelBlocked =
    modelDisabledReason(agent.model?.capabilities.context?.window) !== null && !!agent.model;
  const block = startBlockReason({ kind: form.kind, styleKey, modelBlocked });

  const videoRemote = useRemoteModels("video");
  const imageRemote = useRemoteModels("image");
  const videoModel = liveModelKey(form.videoModel, videoRemote);
  const imageModels = {
    character: liveModelKey(form.imageModels.character, imageRemote),
    scene: liveModelKey(form.imageModels.scene, imageRemote),
    prop: liveModelKey(form.imageModels.prop, imageRemote),
  };
  const modelsLoading = (remote: { status: string }) =>
    remote.status === "idle" || remote.status === "loading";

  const estimate = useSourceEstimate(
    pre.sourceToken,
    form.kind
      ? {
          sourceKind: form.kind,
          rangeEnd: script ? 0 : form.rangeEnd,
          episodeSeconds: form.seconds,
          textModel: form.textModel,
        }
      : null,
    {
      input: {
        sourceKind: guessedKind(pre) ?? "novel",
        rangeEnd: 0,
        episodeSeconds: DEFAULT_EPISODE_SECONDS,
        textModel: "",
      },
      estimate: pre.estimate,
    },
  );
  const est = estimate.estimate ?? pre.estimate;

  const submit = async () => {
    if (block || submitting) return;
    setSubmitting(true);
    setError(null);
    try {
      const next = { ...form, styleKey, prompts, videoModel, imageModels };
      await onSubmit(buildCreateInput(pre, next));
      saveStartPrefs(next);
    } catch (e) {
      setError(dramaErrorMessage(e, "创建失败，请稍后重试"));
      setSubmitting(false);
    }
  };

  const basis = [
    pre.kindSignals && `依据：${pre.kindSignals}`,
    form.kind === "novel" && "先改编成剧本",
    script && (pre.unitKind === "episode" ? "按原分集直接导入" : "未找到分集标记，将按场次切集"),
  ].filter(Boolean);

  return (
    <div className="overflow-hidden rounded-[inherit]">
      <div
        className="flex w-[200%] transition-transform duration-300 ease-out motion-reduce:transition-none"
        style={{ transform: `translateX(${editor ? "-50%" : "0"})` }}
      >
        <div
          aria-hidden={editor ? true : undefined}
          inert={editor ? true : undefined}
          className="grid max-h-[86vh] w-1/2 shrink-0 content-start gap-4 overflow-y-auto p-5"
        >
          <h2 className="px-3 text-base font-semibold">开拍设置</h2>

          <div className="bg-chrome-hover/50 ring-chrome-border flex items-center gap-3 rounded-2xl p-3 ring-1">
            <span className="bg-chrome-hover grid size-10 shrink-0 place-items-center rounded-xl">
              <FileText className="size-5" />
            </span>
            <div className="min-w-0 flex-1">
              <div className="truncate font-medium">{pre.filename}</div>
              <div className="text-muted-foreground text-[12.5px] tabular-nums">
                {charsLabel(pre.chars)} · {pre.unitsTotal.toLocaleString("en-US")} {word}
              </div>
            </div>
            <ChipPopover
              variant="field"
              tone={form.kind ? undefined : "warn"}
              label={form.kind ? (form.kind === "script" ? "剧本" : "小说") : "请选择"}
              width="w-56"
            >
              {(close) => (
                <>
                  {(
                    [
                      ["novel", "小说", "先改编成剧本"],
                      ["script", "剧本", "直接导入，不改编"],
                    ] as const
                  ).map(([key, name, note]) => (
                    <OptionRow
                      key={key}
                      active={form.kind === key}
                      onClick={() => {
                        patch({ kind: key });
                        close();
                      }}
                    >
                      <span className="min-w-0">
                        <span className="block">{name}</span>
                        <span className="text-muted-foreground block text-xs">{note}</span>
                      </span>
                    </OptionRow>
                  ))}
                </>
              )}
            </ChipPopover>
          </div>
          {(basis.length > 0 || pre.issues.length > 0) && (
            <div className="text-muted-foreground -mt-2 px-4 text-[12.5px] leading-5">
              {basis.join(" · ")}
              {pre.issues.length > 0 && (
                <>
                  {basis.length > 0 && " · "}
                  <details className="inline">
                    <summary className="inline cursor-pointer underline decoration-dotted">
                      已处理 {pre.issues.length} 处
                    </summary>
                    <ul className="grid gap-0.5 pt-1">
                      {pre.issues.map((issue) => (
                        <li key={`${issue.kind}-${issue.message}`}>{issue.message}</li>
                      ))}
                    </ul>
                  </details>
                </>
              )}
            </div>
          )}

          <Divider />

          <Field label="规划范围">
            {script ? (
              <span className="text-muted-foreground text-[13.5px] tabular-nums">
                剧本一次切完 · {episodesLabel(est)} 集
              </span>
            ) : (
              <div className="flex flex-wrap items-center gap-x-3 gap-y-2">
                <ChipPopover
                  variant="field"
                  label={
                    form.rangeEnd === 0
                      ? "全部"
                      : `规划到第 ${form.rangeEnd.toLocaleString("en-US")} ${word}`
                  }
                  width="w-64"
                >
                  {(close) => (
                    <RangeList
                      total={pre.unitsTotal}
                      word={word}
                      rangeEnd={form.rangeEnd}
                      onPick={(rangeEnd) => patch({ rangeEnd })}
                      close={close}
                    />
                  )}
                </ChipPopover>
                <EstimateLine state={estimate} />
              </div>
            )}
            {script && estimate.status === "error" && <EstimateLine state={estimate} />}
          </Field>

          <Field label="画幅">
            <div className="flex gap-2">
              {START_RATIOS.map((ratio) => (
                <RatioButton
                  key={ratio.key}
                  ratio={ratio}
                  active={form.ratio === ratio.key}
                  onPick={() => patch({ ratio: ratio.key as StartRatio })}
                />
              ))}
            </div>
          </Field>

          <Field label="风格">
            <div className="flex flex-wrap gap-2">
              <ChipPopover
                variant="field"
                label={genreName || "题材"}
                set={!!form.genre}
                width="w-80"
              >
                {(close) => (
                  <GenreList
                    genres={genres}
                    value={form.genre}
                    onPick={(key) => {
                      patch({ genre: key });
                      close();
                    }}
                  />
                )}
              </ChipPopover>
              <ChipPopover
                variant="field"
                label={styleName}
                width="w-[46rem] max-w-[92vw] max-h-[min(70vh,560px)] overflow-y-auto"
              >
                {() => (
                  <StyleGrid
                    styles={styles}
                    value={styleKey}
                    custom={form.styleCustom}
                    onPick={(key) => patch({ styleKey: key })}
                    onCustomChange={(text) => patch({ styleCustom: text })}
                  />
                )}
              </ChipPopover>
              <ChipPopover
                variant="field"
                disabled={lock.locked}
                title={lock.locked ? "剧本按原分集" : undefined}
                label={lock.label ?? `${form.seconds}s/集${script ? "（参考）" : ""}`}
                width="w-44"
              >
                {(close) => (
                  <SecondsList
                    seconds={form.seconds}
                    onSeconds={(seconds) => {
                      patch({ seconds });
                      close();
                    }}
                  />
                )}
              </ChipPopover>
            </div>
          </Field>

          <Field label="文本模型">
            <div className="grid gap-1.5">
              <div>
                <AgentModelPicker
                  variant="field"
                  value={form.textModel}
                  onChange={(textModel) => patch({ textModel })}
                  disabledReason={(model) =>
                    modelDisabledReason(model.capabilities.context?.window)
                  }
                />
              </div>
              {modelBlocked && (
                <p role="alert" className="text-destructive text-[13px]">
                  {modelDisabledReason(agent.model?.capabilities.context?.window)}
                </p>
              )}
            </div>
          </Field>

          <Field label="视频模型">
            <div>
              <VideoModelMenu
                variant="field"
                models={videoRemote.models}
                loading={modelsLoading(videoRemote)}
                model={videoRemote.models.find((m) => m.key === videoModel)}
                onPick={(key) => patch({ videoModel: key })}
                defaultLabel="跟随默认"
              />
            </div>
          </Field>

          <Field label="资产图片模型">
            <div className="flex flex-wrap gap-2">
              {IMAGE_MODEL_SLOTS.map(({ key, prefix }) => (
                <ModelMenu
                  key={key}
                  variant="field"
                  prefix={prefix}
                  kindName="图片"
                  models={imageRemote.models}
                  loading={modelsLoading(imageRemote)}
                  model={imageRemote.models.find((m) => m.key === imageModels[key])}
                  onPick={(picked) => patch({ imageModels: { ...imageModels, [key]: picked } })}
                  defaultLabel="跟随默认"
                />
              ))}
            </div>
          </Field>

          <Divider />

          <div className="grid gap-1">
            <div className="text-muted-foreground px-3 text-[13px]">提示词</div>
            <PromptRows
              kind={form.kind}
              value={prompts}
              library={library}
              onChange={(step: PromptStep, choice: PromptChoice) =>
                patch({ prompts: { ...prompts, [step]: choice } })
              }
              onEdit={(step, item) => setEditor({ step, item })}
            />
          </div>

          {error && (
            <p
              role="alert"
              className="bg-destructive/10 text-destructive rounded-xl px-3 py-2 text-[13px]"
            >
              {error}
            </p>
          )}

          <div className="flex items-center justify-end gap-2 px-1">
            <Button variant="ghost" className="rounded-full" onClick={onCancel}>
              取消
            </Button>
            <Button
              className="rounded-full px-6"
              disabled={!!block || submitting}
              title={block ?? undefined}
              onClick={() => void submit()}
            >
              {submitting && <Loader2 className="animate-spin" />}
              开始
            </Button>
          </div>
        </div>

        <div
          aria-hidden={editor ? undefined : true}
          inert={editor ? undefined : true}
          className="max-h-[86vh] w-1/2 shrink-0 overflow-y-auto p-5"
        >
          {editor && (
            <PromptEditorPane
              key={`${editor.step}:${editor.item?.id ?? "new"}`}
              step={editor.step}
              item={editor.item}
              library={library}
              onClose={(saved) => {
                if (saved) {
                  patch({
                    prompts: { ...prompts, [saved.step]: { source: "mine", id: saved.id } },
                  });
                }
                setEditor(null);
              }}
            />
          )}
        </div>
      </div>
    </div>
  );
}

/** 预估一行：预计约 900–1,400 集 · 研读与规划约 3,200 积分 · 约 40 分钟 */
function EstimateLine({ state }: { state: ReturnType<typeof useSourceEstimate> }) {
  const est = state.estimate;
  if (state.status === "error") {
    return (
      <span role="alert" className="text-destructive text-[12.5px]">
        {state.message}
      </span>
    );
  }
  if (!est) return null;
  return (
    <span
      className={cn(
        "text-muted-foreground text-[12.5px] tabular-nums transition-opacity",
        state.status === "loading" && "opacity-50",
      )}
    >
      预计约 {episodesLabel(est)} 集 · 研读与规划约{" "}
      <span className="text-credit">{est.credits.toLocaleString("en-US")}</span> 积分 · 约{" "}
      {durationLabel(est.minutes)}
    </span>
  );
}

/** 弹窗打开后才加载提示词库（内置正文、我的提示词、技能），关闭即释放 */
function StartDialogBody({
  pre,
  onCancel,
  onSubmit,
}: {
  pre: SourcePreflight;
  onCancel: () => void;
  onSubmit: (input: CreateDramaInput) => Promise<void>;
}) {
  const library = usePromptLibrary();
  return <StartBody pre={pre} library={library} onCancel={onCancel} onSubmit={onSubmit} />;
}

/**
 * 开拍设置弹窗：预检成功后由上传卡片打开。宽 640、最高 86vh 内部滚动，清透风格；
 * 取消 / 关闭保留卡片里的文件和文本。
 */
export function StartDialog({
  open,
  onOpenChange,
  pre,
  onSubmit,
}: {
  open: boolean;
  onOpenChange: (open: boolean) => void;
  /** 预检结果；弹窗关闭时可以为 null */
  pre: SourcePreflight | null;
  /** 点「开始」建剧 */
  onSubmit: (input: CreateDramaInput) => Promise<void>;
}) {
  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <DialogContent
        showCloseButton={false}
        aria-label="开拍设置"
        className="bg-chrome ring-chrome-border w-160 max-w-[calc(100%-2rem)] gap-0 rounded-3xl p-0 shadow-lg ring-1 backdrop-blur-xl backdrop-saturate-150 sm:max-w-160"
      >
        {open && pre && (
          <StartDialogBody
            key={pre.sourceToken}
            pre={pre}
            onCancel={() => onOpenChange(false)}
            onSubmit={onSubmit}
          />
        )}
      </DialogContent>
    </Dialog>
  );
}
