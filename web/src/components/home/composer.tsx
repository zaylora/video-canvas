import {
  useEffect,
  useLayoutEffect,
  useRef,
  useState,
  type FormEvent,
  type KeyboardEvent,
} from "react";
import { motion } from "motion/react";
import { ArrowUp, AtSign, ChevronDown, Plus, SlidersHorizontal, WandSparkles } from "lucide-react";
import { toast } from "sonner";

import { SoonTip } from "@/components/home/soon";
import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuRadioGroup,
  DropdownMenuRadioItem,
  DropdownMenuTrigger,
} from "@/components/ui/dropdown-menu";
import { CREATION_MODES } from "@/constants/creation";
import { TAP } from "@/lib/motion";
import { cn } from "@/lib/utils";
import { useComposerStore } from "@/store/composer";
import type { CreationMode } from "@/types";

/** 输入框最多长到多高（px），再长就在框里滚动 */
const MAX_HEIGHT = 240;

/** 外部填充输入框后，描边闪一下的时长（毫秒） */
const FLASH_MS = 600;

/** 工具条按钮的公共样式：窄屏只留图标 */
const BAR_BUTTON =
  "text-foreground hover:bg-muted focus-visible:ring-ring/50 inline-flex h-8 items-center gap-1.5 rounded-[10px] px-2.5 text-[13px] outline-none focus-visible:ring-3 disabled:opacity-60";

/**
 * 创作页和对话页共用的输入卡片：左侧参考上传块，中间输入框，底部是模式、模型、技能、引用和发送。
 * 模式和草稿在 useComposerStore 里，两个页面各渲一份也不会丢。
 * 生成接口还没有：模式切换、技能和引用符号是真的，发送只给出「即将上线」的提示，
 * 参考上传和选模型先禁用。
 * @param placement page 是创作页里的大卡片；dock 是对话页贴在底部的那一份，菜单向上弹、输入框矮一点
 */
export function Composer({ placement = "page" }: { placement?: "page" | "dock" }) {
  const mode = useComposerStore((state) => state.mode);
  const text = useComposerStore((state) => state.text);
  const fillSeq = useComposerStore((state) => state.fillSeq);
  const setMode = useComposerStore((state) => state.setMode);
  const setText = useComposerStore((state) => state.setText);
  const fill = useComposerStore((state) => state.fill);
  const textareaRef = useRef<HTMLTextAreaElement>(null);
  const [flash, setFlash] = useState(false);
  /** 挂载时已有的填充次数：之前的填充不该让新挂上的输入卡片再聚焦、闪一次 */
  const seenFill = useRef(fillSeq);
  const current = CREATION_MODES.find((item) => item.id === mode) ?? CREATION_MODES[0];

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
    if (!text.trim()) return;
    toast.info("生成功能即将上线");
  };

  const handleKeyDown = (event: KeyboardEvent<HTMLTextAreaElement>) => {
    if (event.key !== "Enter" || event.shiftKey || event.nativeEvent.isComposing) return;
    event.preventDefault();
    event.currentTarget.form?.requestSubmit();
  };

  return (
    <form
      data-slot="composer"
      data-placement={placement}
      autoComplete="off"
      onSubmit={handleSubmit}
      className={cn(
        "bg-card border-border relative grid grid-cols-[auto_1fr] gap-x-4 gap-y-2.5 rounded-3xl border p-3 pt-4 pl-4 shadow-[0_1px_2px_oklch(0_0_0/0.04),0_18px_40px_-28px_oklch(0_0_0/0.35)] transition-[border-color,box-shadow] duration-180 md:p-3 md:pt-4.5 md:pl-4.5",
        "focus-within:border-foreground/25",
        flash && "ring-beam/30 ring-4",
      )}
    >
      <SoonTip className="inline-flex cursor-not-allowed">
        <button
          type="button"
          disabled
          aria-label="上传参考图、视频或音频"
          className="bg-muted text-muted-foreground border-foreground/20 grid h-14 w-11 -rotate-6 place-items-center rounded-[10px] border border-dashed md:h-18 md:w-14"
        >
          <Plus className="size-4.5" />
        </button>
      </SoonTip>

      <div className="relative min-w-0">
        <textarea
          ref={textareaRef}
          value={text}
          rows={placement === "dock" ? 2 : 3}
          maxLength={2000}
          aria-label="输入想法、剧本或上传参考"
          onChange={(event) => setText(event.target.value)}
          onKeyDown={handleKeyDown}
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
            输入想法、剧本或上传参考，支持 <Hint>/</Hint> 使用技能，<Hint>@</Hint> 引用资产，和 Agent
            一起创作
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
          <DropdownMenuContent
            side={placement === "dock" ? "top" : "bottom"}
            sideOffset={6}
            className="w-72"
          >
            <DropdownMenuRadioGroup
              value={mode}
              onValueChange={(value) => setMode(value as CreationMode)}
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
          </DropdownMenuContent>
        </DropdownMenu>

        <SoonTip>
          <button type="button" disabled aria-label="选择模型，默认自动" className={BAR_BUTTON}>
            <SlidersHorizontal className="size-4" />
            <span className="max-md:hidden">自动</span>
          </button>
        </SoonTip>
        <motion.button
          type="button"
          whileTap={TAP}
          title="使用技能（输入 / 也可以）"
          onClick={() => fill(`/${text.startsWith("/") ? text.slice(1) : text}`)}
          className={BAR_BUTTON}
        >
          <WandSparkles className="size-4" />
          <span className="max-md:hidden">技能</span>
        </motion.button>
        <motion.button
          type="button"
          whileTap={TAP}
          aria-label="引用资产"
          title="引用资产（输入 @ 也可以）"
          onClick={() => fill(`${text}@`)}
          className={cn(BAR_BUTTON, "w-8 justify-center px-0")}
        >
          <AtSign className="size-4" />
        </motion.button>

        <div className="flex-1" />
        <motion.button
          type="submit"
          whileTap={TAP}
          disabled={!text.trim()}
          aria-label="开始创作"
          title="开始创作（Enter，Shift+Enter 换行）"
          className="bg-foreground text-background focus-visible:ring-ring/50 disabled:bg-muted disabled:text-muted-foreground grid size-9 shrink-0 place-items-center rounded-full outline-none transition-colors duration-120 hover:opacity-90 focus-visible:ring-3 disabled:opacity-100"
        >
          <ArrowUp className="size-4.5" />
        </motion.button>
      </div>
    </form>
  );
}

/** 占位提示里的按键小块 */
function Hint({ children }: { children: string }) {
  return (
    <kbd className="bg-muted border-border text-foreground mx-0.5 inline-grid h-5.5 min-w-5.5 place-items-center rounded-md border px-1 align-[1px] font-sans text-[13px]">
      {children}
    </kbd>
  );
}
