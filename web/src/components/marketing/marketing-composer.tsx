import {
  useLayoutEffect,
  useRef,
  type ClipboardEvent,
  type DragEvent,
  type FormEvent,
  type KeyboardEvent,
} from "react";
import { motion } from "motion/react";
import { ArrowUp, Loader2, Sparkle } from "lucide-react";

import { ModelMenu } from "@/components/drama/video-model-menu";
import { useMarketingComposer } from "@/hooks/use-marketing-composer";
import { TAP } from "@/lib/motion";
import { cn } from "@/lib/utils";
import { useMarketingComposerStore } from "@/store/marketing-composer";
import { modelSummary } from "@/utils/drama/video-models";
import { angleLabel, refVideoMaxSeconds } from "@/utils/marketing/composer";
import type { Project } from "@/api/marketing/type";

import { AngleMenu, ParamsMenu, PlatformMenu } from "./composer-menus";
import { ComposerSlots } from "./composer-slots";

/** 描述最长 1000 字（契约） */
const MAX_DESCRIPTION = 1000;

/** 输入框最多长到多高（px），再长就在框里滚动 */
const MAX_HEIGHT = 240;

/**
 * 营销生成框：三个上传块（商品 / 模特 / 参考视频）+ 描述 + 参数条（平台、角度、视频模型、参数）+
 * 写脚本预估积分与圆形发送键；不能发送时下方状态行写原因。
 * 外观照首页 Composer（半透明大圆角卡片，page 带一圈慢转的光环，dock 不带）；
 * 状态在 store/marketing-composer，列表页和详情页共用一份设置。
 * @param project 详情页传当前项目：商品图固定为项目的，发送在这个项目里开新一轮；列表页不传，发送新建项目
 */
export function MarketingComposer({ project }: { project?: Project }) {
  const composer = useMarketingComposer(project);
  const { draft, state, options } = composer;
  const dock = composer.dock;
  const model = composer.videoModel;
  const textareaRef = useRef<HTMLTextAreaElement>(null);
  const side = dock ? "top" : "bottom";

  /** 输入框随内容长高，到 MAX_HEIGHT 为止 */
  useLayoutEffect(() => {
    const el = textareaRef.current;
    if (!el) return;
    el.style.height = "auto";
    el.style.height = `${Math.min(el.scrollHeight, MAX_HEIGHT)}px`;
  }, [draft.description]);

  const setDescription = (value: string) =>
    useMarketingComposerStore.getState().setDraft({ description: value });

  const handleSubmit = (event: FormEvent) => {
    event.preventDefault();
    void composer.submit();
  };

  const handleKeyDown = (event: KeyboardEvent<HTMLTextAreaElement>) => {
    if (event.key !== "Enter" || event.shiftKey || event.nativeEvent.isComposing) return;
    event.preventDefault();
    event.currentTarget.form?.requestSubmit();
  };

  /** 把拖入 / 粘贴的文件分给对应的位置：图片当商品图（dock 里当模特图），视频当参考 */
  const takeFiles = (files: File[]) => {
    const images = files.filter((file) => file.type.startsWith("image/"));
    const videos = files.filter((file) => file.type.startsWith("video/"));
    if (images.length > 0) {
      if (dock) composer.setModelFile(images[0]);
      else composer.addProductFiles(images);
    }
    if (videos.length > 0) composer.setRefFile(videos[0]);
  };

  const handlePaste = (event: ClipboardEvent<HTMLTextAreaElement>) => {
    const files = Array.from(event.clipboardData.files);
    if (files.length === 0) return;
    event.preventDefault();
    takeFiles(files);
  };

  const handleDrop = (event: DragEvent<HTMLFormElement>) => {
    const files = Array.from(event.dataTransfer.files);
    if (files.length === 0) return;
    event.preventDefault();
    takeFiles(files);
  };

  const videoCaps = model?.capabilities;
  const refDisabledReason = !model
    ? "暂无可用的视频模型"
    : (videoCaps?.refs.video.on ?? false) && (videoCaps?.refs.video.max ?? 0) > 0
      ? null
      : `${model.label} 不支持参考视频`;
  const refMax = refVideoMaxSeconds(videoCaps) ?? options.refVideoMaxSeconds;
  const refHint = `上传爆款参考视频（可选${refMax ? `，≤${refMax} 秒` : ""}）`;
  const insufficient = state.hint?.startsWith("余额") ?? false;

  return (
    <form
      data-slot="marketing-composer"
      data-placement={dock ? "dock" : "page"}
      autoComplete="off"
      onSubmit={handleSubmit}
      onDragOver={(event) => event.dataTransfer.types.includes("Files") && event.preventDefault()}
      onDrop={handleDrop}
      className={cn(
        "group/composer bg-card border-border relative grid grid-cols-[auto_1fr] gap-x-4 gap-y-2.5 rounded-3xl border p-3 pt-4 pl-4 shadow-[0_1px_2px_oklch(0_0_0/0.04),0_18px_40px_-28px_oklch(0_0_0/0.35)] transition-[border-color,box-shadow] duration-180 md:p-3 md:pt-4.5 md:pl-4.5",
        "focus-within:border-foreground/25 max-sm:grid-cols-1",
      )}
    >
      {!dock && (
        <div
          aria-hidden
          className="home-ring transition-opacity duration-240 group-focus-within/composer:opacity-0"
        />
      )}

      <ComposerSlots
        dock={dock}
        products={composer.products}
        fixedProducts={project?.productAssets ?? []}
        model={composer.modelSlot}
        projectModelUrl={project?.modelAsset?.url}
        refVideo={composer.refSlot}
        refDisabledReason={refDisabledReason}
        refHint={refHint}
        onAddProducts={composer.addProductFiles}
        onClearProducts={composer.clearProducts}
        onPickModel={composer.setModelFile}
        onClearModel={composer.clearModel}
        onPickRef={composer.setRefFile}
        onClearRef={composer.clearRef}
      />

      <div className="relative min-w-0">
        <textarea
          ref={textareaRef}
          value={draft.description}
          rows={dock ? 2 : 3}
          maxLength={MAX_DESCRIPTION}
          readOnly={composer.sending}
          aria-label="商品描述"
          onChange={(event) => setDescription(event.target.value)}
          onKeyDown={handleKeyDown}
          onPaste={handlePaste}
          className={cn(
            "block w-full resize-none bg-transparent text-[15px] leading-[1.65] outline-none",
            dock ? "min-h-14" : "min-h-21",
          )}
        />
        {draft.description.length === 0 && (
          <div
            aria-hidden
            className="text-muted-foreground pointer-events-none absolute inset-x-0 top-0 text-[15px] leading-[1.65]"
          >
            {dock ? "换个说法、补充卖点，再写一轮" : "商品名、卖点、想要的感觉，也可以不写"}
          </div>
        )}
      </div>

      <div className="col-span-full flex flex-wrap items-center gap-x-0.5 gap-y-1">
        <PlatformMenu
          platforms={options.platforms}
          value={draft.platform}
          onPick={composer.setPlatform}
          side={side}
        />
        <AngleMenu
          label={angleLabel(draft, composer.hasRef, options.angles)}
          angles={options.angles}
          draft={draft}
          hasRef={composer.hasRef}
          onPick={composer.pickAngle}
          onAuto={composer.clearAngles}
          onAutoCount={composer.setAutoCount}
          onAdaptCount={composer.setAdaptCount}
          side={side}
        />
        <ModelMenu
          models={composer.videoModels}
          loading={composer.videoStatus === "loading" || composer.videoStatus === "idle"}
          model={model}
          onPick={(key) => useMarketingComposerStore.getState().setVideoModel(key)}
          support={composer.support}
          summary={modelSummary}
          kindName="视频"
          variant="bar"
          title="写完脚本后出片用的视频模型"
        />
        <ParamsMenu
          model={model}
          draft={draft}
          textModels={composer.textModels}
          textLoading={composer.textStatus === "loading" || composer.textStatus === "idle"}
          onChange={composer.setDraft}
          side={side}
        />

        <div className="flex-1" />
        <span
          title={`写脚本预估积分（各环节之和），出片另计${
            composer.available === null ? "" : `；可用 ${composer.available}`
          }`}
          className={cn(
            "mr-1 inline-flex items-center gap-1 text-[13px] tabular-nums",
            insufficient ? "text-destructive font-medium" : "text-muted-foreground",
          )}
        >
          <Sparkle className="size-3.5" />
          {composer.credits ?? "–"}
        </span>
        <motion.button
          type="submit"
          whileTap={TAP}
          disabled={!state.canSend}
          aria-label="写脚本"
          title={state.hint ?? "写脚本（Enter，Shift+Enter 换行）"}
          className="bg-foreground text-background focus-visible:ring-ring/50 disabled:bg-muted disabled:text-muted-foreground grid size-9 shrink-0 place-items-center rounded-full outline-none transition-colors duration-120 hover:opacity-90 focus-visible:ring-3 disabled:opacity-100"
        >
          {composer.sending ? (
            <Loader2 className="size-4.5 animate-spin" />
          ) : (
            <ArrowUp className="size-4.5" />
          )}
        </motion.button>
      </div>

      <p
        role="status"
        aria-live="polite"
        className={cn(
          "col-span-full -mt-1 min-h-4 px-1 text-xs",
          state.hint
            ? state.tone === "bad"
              ? "text-destructive"
              : "text-amber-600 dark:text-amber-400"
            : "sr-only",
        )}
      >
        {state.hint ?? ""}
      </p>
    </form>
  );
}
