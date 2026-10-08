import { useEffect, useRef, useState } from "react";
import { AnimatePresence, motion } from "motion/react";
import { useNavigate } from "react-router";
import {
  ArrowLeft,
  Check,
  ChevronDown,
  CloudAlert,
  LoaderCircle,
  Plus,
  RefreshCw,
} from "lucide-react";
import { toast } from "sonner";

import { createCanvas } from "@/api/canvas";
import { Logo } from "@/components/brand/logo";
import { ChromeButton, ChromePill, ChromeTooltip } from "@/components/canvas/chrome/chrome";
import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuItem,
  DropdownMenuSeparator,
  DropdownMenuTrigger,
} from "@/components/ui/dropdown-menu";
import type { SaveStatus } from "@/hooks/use-canvas-persistence";
import { DURATION, EASE_OUT } from "@/lib/motion";
import { cn } from "@/lib/utils";

/** 画布名最长多少字 */
const TITLE_MAX = 60;

/** 画布名：点一下原地变输入框，Enter / 失焦提交，Esc 放弃 */
function CanvasTitle({ title, onRename }: { title: string; onRename: (title: string) => void }) {
  // 没在编辑时显示外面给的名字；一聚焦就拿当前名字起草
  const [draft, setDraft] = useState<string | null>(null);
  const inputRef = useRef<HTMLInputElement>(null);
  const shown = draft ?? title;

  const commit = () => {
    if (draft === null) return;
    const next = draft.trim().slice(0, TITLE_MAX);
    setDraft(null);
    if (next && next !== title) onRename(next);
  };

  return (
    <span className="relative grid min-w-0">
      {/* 隐形的同文本撑出宽度，输入框跟着字数变宽 */}
      <span className="invisible col-start-1 row-start-1 max-w-[28ch] truncate px-2 text-sm font-semibold whitespace-pre">
        {shown || " "}
      </span>
      <input
        ref={inputRef}
        value={shown}
        maxLength={TITLE_MAX}
        aria-label="画布名称"
        spellCheck={false}
        className={cn(
          "col-start-1 row-start-1 h-8 w-full min-w-[4ch] truncate rounded-lg bg-transparent px-2 text-sm font-semibold outline-none",
          "hover:bg-chrome-hover focus:bg-chrome-hover transition-colors",
        )}
        onFocus={(event) => {
          setDraft(title);
          event.currentTarget.select();
        }}
        onChange={(event) => setDraft(event.target.value)}
        onBlur={commit}
        onKeyDown={(event) => {
          if (event.nativeEvent.isComposing) return;
          if (event.key === "Enter") inputRef.current?.blur();
          if (event.key === "Escape") {
            setDraft(null);
            requestAnimationFrame(() => inputRef.current?.blur());
          }
        }}
      />
    </span>
  );
}

const SAVE_TEXT: Record<SaveStatus, string> = {
  loading: "加载中",
  saved: "已保存",
  saving: "正在保存",
  error: "保存失败 · 重试",
  conflict: "存在冲突",
};

/**
 * 保存状态：正在保存转圈（连续编辑期间一直保持，不来回闪），保存好的字 2 秒后收起只留勾，失败标红可点重试。
 * 调用方以 status 作 key，状态一变就重挂，收起的计时从头算。
 */
function SaveIndicator({ status, onSave }: { status: SaveStatus; onSave: () => void }) {
  const [quiet, setQuiet] = useState(false);
  useEffect(() => {
    if (status !== "saved") return;
    const timer = window.setTimeout(() => setQuiet(true), 2000);
    return () => window.clearTimeout(timer);
  }, [status]);

  const Icon =
    status === "saving"
      ? LoaderCircle
      : status === "error"
        ? CloudAlert
        : status === "conflict"
          ? RefreshCw
          : Check;
  const error = status === "error";
  const clickable = error;

  return (
    <button
      type="button"
      role="status"
      disabled={!clickable}
      onClick={onSave}
      className={cn(
        "text-muted-foreground flex h-8 items-center gap-1.5 rounded-full pr-2.5 pl-1.5 text-xs transition-colors",
        error && "text-destructive hover:bg-destructive/10 cursor-pointer",
      )}
    >
      <Icon className={cn("size-3.5 shrink-0", status === "saving" && "animate-spin")} />
      <AnimatePresence>
        {!quiet && (
          <motion.span
            key={status}
            initial={{ opacity: 0, width: 0 }}
            animate={{ opacity: 1, width: "auto" }}
            exit={{ opacity: 0, width: 0 }}
            transition={{ duration: DURATION.slow, ease: EASE_OUT }}
            className="overflow-hidden whitespace-nowrap"
          >
            {SAVE_TEXT[status]}
          </motion.span>
        )}
      </AnimatePresence>
    </button>
  );
}

/** 左上角：品牌菜单（回列表、新建画布）、画布名、保存状态 */
export function TopLeftBar({
  title,
  onRename,
  saveStatus,
  onSaveNow,
}: {
  title: string;
  onRename: (title: string) => void;
  saveStatus: SaveStatus;
  /** 立即保存，返回保存后是否已经没有未保存的内容 */
  onSaveNow: () => Promise<boolean>;
}) {
  const navigate = useNavigate();
  const [creating, setCreating] = useState(false);

  /** 离开前先把没存的内容存掉；存不上就留在原地，别让内容悄悄丢了 */
  const leaveTo = async (to: string) => {
    if (!(await onSaveNow())) {
      toast.error("还有内容没保存上，已留在当前画布");
      return;
    }
    navigate(to);
  };

  const newCanvas = async () => {
    if (creating) return;
    setCreating(true);
    try {
      if (!(await onSaveNow())) {
        toast.error("还有内容没保存上，已留在当前画布");
        return;
      }
      const canvas = await createCanvas();
      navigate(`/canvas/${canvas.id}`);
    } catch {
      toast.error("新建画布失败，请稍后重试");
    } finally {
      setCreating(false);
    }
  };

  return (
    <ChromePill className="max-w-[calc(100vw-24px)] pr-1">
      <DropdownMenu modal={false}>
        <ChromeTooltip label="画布菜单" side="bottom">
          <DropdownMenuTrigger
            render={
              <ChromeButton aria-label="画布菜单" className="gap-1 pr-1.5 pl-1">
                <Logo size={22} />
                <ChevronDown className="size-3! opacity-60" />
              </ChromeButton>
            }
          />
        </ChromeTooltip>
        <DropdownMenuContent className="w-52" sideOffset={10}>
          <DropdownMenuItem onClick={() => void leaveTo("/")}>
            <ArrowLeft />
            返回项目列表
          </DropdownMenuItem>
          <DropdownMenuSeparator />
          <DropdownMenuItem disabled={creating} onClick={() => void newCanvas()}>
            <Plus />
            新建画布
          </DropdownMenuItem>
        </DropdownMenuContent>
      </DropdownMenu>
      <CanvasTitle title={title} onRename={onRename} />
      <SaveIndicator key={saveStatus} status={saveStatus} onSave={() => void onSaveNow()} />
    </ChromePill>
  );
}
