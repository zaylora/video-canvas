import {
  useEffect,
  useLayoutEffect,
  useMemo,
  useRef,
  useState,
  type ClipboardEvent,
  type DragEvent,
  type FormEvent,
  type KeyboardEvent,
} from "react";
import { motion } from "motion/react";
import { ArrowUp, AtSign, ChevronDown, Loader2, Sparkle } from "lucide-react";

import type { SubmitTarget } from "@/api/conversation/type";
import { ComposerRefs } from "@/components/home/composer-refs";
import { ModelMenu } from "@/components/home/composer-model";
import { ParamsPopover } from "@/components/home/composer-params";
import { SoonTip } from "@/components/home/soon";
import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuRadioGroup,
  DropdownMenuRadioItem,
  DropdownMenuTrigger,
} from "@/components/ui/dropdown-menu";
import { CREATION_MODES } from "@/constants/creation";
import { useComposerRefs } from "@/hooks/use-composer-refs";
import { useConversationSubmit } from "@/hooks/use-conversation-submit";
import { useRemoteModels } from "@/hooks/use-models";
import { TAP } from "@/lib/motion";
import { cn } from "@/lib/utils";
import { useComposerStore } from "@/store/composer";
import { useCreditsStore } from "@/store/credits";
import { evaluateSend } from "@/utils/conversation/submission";

/** 输入框最多长到多高（px），再长就在框里滚动 */
const MAX_HEIGHT = 240;

/** 外部填充输入框后，描边闪一下的时长（毫秒） */
const FLASH_MS = 600;

/** 工具条按钮的公共样式：窄屏只留图标 */
const BAR_BUTTON =
  "text-foreground hover:bg-muted focus-visible:ring-ring/50 inline-flex h-8 items-center gap-1.5 rounded-[10px] px-2.5 text-[13px] outline-none focus-visible:ring-3 disabled:opacity-60";

/**
 * 创作页和对话页共用的输入卡片：左侧参考图，中间输入框，底部依次是模式、模型、参数、引用、积分和发送。
 * 模式、模型、参数、草稿和参考图都在 useComposerStore 里，两个页面各渲一份也不会丢。
 * 发送一次提交一条生成记录（见 hooks/use-conversation-submit.ts）：
 * 发送键旁显示预估积分，余额不足、参考图没传完、提示词不合法时禁用并写明原因。
 * 引用资产（@）要等素材列表接口，先禁用；Agent 只在画布里，这里没有。
 * @param placement page 是创作页里的大卡片；dock 是对话页贴在底部的那一份，菜单向上弹、输入框矮一点
 * @param target 记录提交到哪里：首页是默认创作，对话页是当前对话，新对话页是 new
 */
export function Composer({
  placement = "page",
  target = "default",
}: {
  placement?: "page" | "dock";
  target?: SubmitTarget;
}) {
  const mode = useComposerStore((state) => state.mode);
  const text = useComposerStore((state) => state.text);
  const refs = useComposerStore((state) => state.refs);
  const sending = useComposerStore((state) => state.sending);
  const fillSeq = useComposerStore((state) => state.fillSeq);
  const modelKey = useComposerStore((state) => state.modelByMode[state.mode]);
  const paramsByModel = useComposerStore((state) => state.paramsByModel);
  const setMode = useComposerStore((state) => state.setMode);
  const setModel = useComposerStore((state) => state.setModel);
  const setParam = useComposerStore((state) => state.setParam);
  const setText = useComposerStore((state) => state.setText);
  const available = useCreditsStore((state) => state.credits?.available ?? null);
  const refreshCredits = useCreditsStore((state) => state.refresh);

  const { models, status } = useRemoteModels(mode);
  /** 记住的模型已下线或没记过：取清单第一个（后台的排序） */
  const model = models.find((item) => item.key === modelKey) ?? models[0];
  const params = useMemo(
    () => (model ? (paramsByModel[model.key] ?? {}) : {}),
    [model, paramsByModel],
  );
  const refTools = useComposerRefs(model);
  const { submit } = useConversationSubmit(target);

  const textareaRef = useRef<HTMLTextAreaElement>(null);
  const [flash, setFlash] = useState(false);
  /** 挂载时已有的填充次数：之前的填充不该让新挂上的输入卡片再聚焦、闪一次 */
  const seenFill = useRef(fillSeq);
  const current = CREATION_MODES.find((item) => item.id === mode) ?? CREATION_MODES[0];

  const state = useMemo(
    () => evaluateSend({ model, text, params, refs, available, sending }),
    [model, text, params, refs, available, sending],
  );
  const showCredits = model && state.creditsTotal > 0;

  /** 余额没拿到时拉一次：发送前要知道够不够 */
  useEffect(() => {
    if (available === null) void refreshCredits();
  }, [available, refreshCredits]);

  /** 输入框随内容长高，到 MAX_HEIGHT 为止 */
  useLayoutEffect(() => {
    const el = textareaRef.current;
    if (!el) return;
    el.style.height = "auto";
    el.style.height = `${Math.min(el.scrollHeight, MAX_HEIGHT)}px`;
  }, [text]);

  /** 被外部填充：聚焦、光标放到末尾、描边闪一下 */
  useEffect(() => {
    if (fillSeq === seenFill.current) return;
    seenFill.current = fillSeq;
    const el = textareaRef.current;
    if (!el) return;
    el.focus();
    el.setSelectionRange(el.value.length, el.value.length);
    setFlash(true);
    const timer = window.setTimeout(() => setFlash(false), FLASH_MS);
    return () => window.clearTimeout(timer);
  }, [fillSeq]);

  const handleSubmit = (event: FormEvent) => {
    event.preventDefault();
    if (!state.canSend || !model) return;
    void submit({ kind: mode, modelId: model.key, prompt: text.trim(), send: state });
  };

  const handleKeyDown = (event: KeyboardEvent<HTMLTextAreaElement>) => {
    if (event.key !== "Enter" || event.shiftKey || event.nativeEvent.isComposing) return;
    event.preventDefault();
    event.currentTarget.form?.requestSubmit();
  };

  /** 粘贴里有图片就当参考图，文字照常粘贴 */
  const handlePaste = (event: ClipboardEvent<HTMLTextAreaElement>) => {
    const files = Array.from(event.clipboardData.files);
    if (files.length === 0) return;
    event.preventDefault();
    refTools.addFiles(files);
  };

  const handleDrop = (event: DragEvent<HTMLFormElement>) => {
    const files = Array.from(event.dataTransfer.files);
    if (files.length === 0) return;
    event.preventDefault();
    refTools.addFiles(files);
  };

  const menuSide = placement === "dock" ? "top" : "bottom";

  return (
    <form
      data-slot="composer"
      data-placement={placement}
      autoComplete="off"
      onSubmit={handleSubmit}
      onDragOver={(event) => event.dataTransfer.types.includes("Files") && event.preventDefault()}
      onDrop={handleDrop}
      className={cn(
        "bg-card border-border relative grid grid-cols-[auto_1fr] gap-x-4 gap-y-2.5 rounded-3xl border p-3 pt-4 pl-4 shadow-[0_1px_2px_oklch(0_0_0/0.04),0_18px_40px_-28px_oklch(0_0_0/0.35)] transition-[border-color,box-shadow] duration-180 md:p-3 md:pt-4.5 md:pl-4.5",
        "focus-within:border-foreground/25",
        flash && "ring-beam/30 ring-4",
      )}
    >
      <ComposerRefs
        refs={refs}
        caps={model?.capabilities}
        modelReady={!!model}
        onFiles={refTools.addFiles}
        onRetry={refTools.retry}
        onRemove={refTools.remove}
      />

      <div className="relative min-w-0">
        <textarea
          ref={textareaRef}
          value={text}
          rows={placement === "dock" ? 2 : 3}
          maxLength={model?.capabilities.prompt.max_length || 4000}
          readOnly={sending}
          aria-label="输入想法、剧本或上传参考"
          onChange={(event) => setText(event.target.value)}
          onKeyDown={handleKeyDown}
          onPaste={handlePaste}
          className={cn(
            "block w-full resize-none bg-transparent text-[15px] leading-[1.65] outline-none",
            placement === "dock" ? "min-h-14" : "min-h-21",
          )}
        />
        {text.length === 0 && (
          <div
            aria-hidden
            className="text-muted-foreground pointer-events-none absolute inset-x-0 top-0 text-[15px] leading-[1.65]"
          >
            描述你想生成的画面、镜头或声音，也可以上传、粘贴参考图
          </div>
        )}
      </div>

      <div className="col-span-full flex items-center gap-0.5">
        <DropdownMenu modal={false}>
          <DropdownMenuTrigger
            render={<motion.button type="button" whileTap={TAP} />}
            aria-label="创作模式"
            className={cn(BAR_BUTTON, "group/mode text-beam font-medium")}
          >
            <current.icon className="size-4" />
            <span>{current.label}</span>
            <ChevronDown className="size-3.5 transition-transform duration-120 group-data-popup-open/mode:rotate-180" />
          </DropdownMenuTrigger>
          <DropdownMenuContent side={menuSide} sideOffset={6} className="w-72">
            <DropdownMenuRadioGroup
              value={mode}
              onValueChange={(value) => setMode(value as typeof mode)}
            >
              {CREATION_MODES.map((item) => (
                <DropdownMenuRadioItem
                  key={item.id}
                  value={item.id}
                  className="items-start gap-2.5 rounded-[10px] py-2 pr-8 pl-2.5"
                >
                  <item.icon className="mt-0.5" />
                  <span className="grid gap-0.5">
                    <span className="text-[13.5px] font-medium">{item.label}</span>
                    <span className="text-muted-foreground text-xs leading-snug">{item.desc}</span>
                  </span>
                </DropdownMenuRadioItem>
              ))}
            </DropdownMenuRadioGroup>
            <p className="text-muted-foreground border-border mt-1 border-t px-2.5 pt-2 pb-1 text-xs">
              想让 Agent 帮你拆分镜，请在画布里使用
            </p>
          </DropdownMenuContent>
        </DropdownMenu>

        <ModelMenu
          models={models}
          status={status}
          value={model}
          onChange={(key) => setModel(mode, key)}
          side={menuSide}
        />
        {model && (
          <ParamsPopover
            model={model}
            params={params}
            refs={refs}
            onChange={(name, value) => setParam(model.key, name, value)}
            side={menuSide}
          />
        )}
        <SoonTip>
          <button
            type="button"
            disabled
            aria-label="引用资产，即将上线"
            className={cn(BAR_BUTTON, "w-8 justify-center px-0")}
          >
            <AtSign className="size-4" />
          </button>
        </SoonTip>

        <div className="flex-1" />
        {showCredits && (
          <span
            title={`预估积分，以实际扣费为准${available === null ? "" : `；可用 ${available}`}`}
            className={cn(
              "mr-1 inline-flex items-center gap-1 text-[13px] tabular-nums",
              state.tone === "bad" && state.hint?.startsWith("余额")
                ? "text-destructive font-medium"
                : "text-muted-foreground",
            )}
          >
            <Sparkle className="size-3.5" />
            {state.creditsTotal}
          </span>
        )}
        <motion.button
          type="submit"
          whileTap={TAP}
          disabled={!state.canSend}
          aria-label="开始创作"
          title={state.hint ?? "开始创作（Enter，Shift+Enter 换行）"}
          className="bg-foreground text-background focus-visible:ring-ring/50 disabled:bg-muted disabled:text-muted-foreground grid size-9 shrink-0 place-items-center rounded-full outline-none transition-colors duration-120 hover:opacity-90 focus-visible:ring-3 disabled:opacity-100"
        >
          {sending ? (
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
