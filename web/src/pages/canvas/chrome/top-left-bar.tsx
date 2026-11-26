import { openDialog } from "@/store/dialog";
import { DramaShareDialog } from "@/pages/drama/share-dialog";
import { useEffect, useRef, useState } from "react";
import { AnimatePresence, motion } from "motion/react";
import { Link, useNavigate } from "react-router";
import {
  ArrowLeft,
  Check,
  ChevronDown,
  CloudAlert,
  FileDown,
  LoaderCircle,
  Plus,
  RefreshCw,
} from "lucide-react";
import { toast } from "sonner";

import { canvasNotice } from "@/utils/pet/notice";

import { createCanvas, exportCanvas } from "@/api/canvas";
import { Logo } from "@/components/brand/logo";
import { ChromeButton, ChromePill, ChromeTooltip } from "@/components/canvas/chrome/chrome";
import { TransferProgressDialog } from "@/components/canvas/transfer-progress-dialog";
import {
  AlertDialog,
  AlertDialogAction,
  AlertDialogCancel,
  AlertDialogContent,
  AlertDialogDescription,
  AlertDialogFooter,
  AlertDialogHeader,
  AlertDialogTitle,
  AlertDialogTrigger,
} from "@/components/ui/alert-dialog";
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
import { runCanvasExport } from "@/utils/canvas/export-flow";
import { createTransferCancel, type TransferCancel } from "@/utils/canvas/transfer-cancel";
import { downloadJson } from "@/utils/canvas/transfer-file";

import { SharedRefreshControl } from "./shared-refresh-control";

/** 画布名最长多少字 */
const TITLE_MAX = 60;

/** 画布名：点一下原地变输入框，Enter / 失焦提交，Esc 放弃 */
function CanvasTitle({
  title,
  onRename,
  readOnly,
}: {
  title: string;
  onRename?: (title: string) => void;
  /** 只读预览：名字能看不能改 */
  readOnly?: boolean;
}) {
  // 没在编辑时显示外面给的名字；一聚焦就拿当前名字起草
  const [draft, setDraft] = useState<string | null>(null);
  const inputRef = useRef<HTMLInputElement>(null);
  const shown = draft ?? title;

  const commit = () => {
    if (draft === null) return;
    const next = draft.trim().slice(0, TITLE_MAX);
    setDraft(null);
    if (next && next !== title) onRename?.(next);
  };

  return (
    <span className="relative grid min-w-0">
      {/* 隐形的同文本撑出宽度，输入框跟着字数变宽 */}
      <span className="invisible col-start-1 row-start-1 max-w-[28ch] truncate px-2 text-[13px] font-semibold whitespace-pre">
        {shown || " "}
      </span>
      <input
        ref={inputRef}
        value={shown}
        maxLength={TITLE_MAX}
        aria-label="画布名称"
        spellCheck={false}
        readOnly={readOnly}
        className={cn(
          "col-start-1 row-start-1 h-8 w-full min-w-[4ch] truncate rounded-lg bg-transparent px-2 text-[13px] font-semibold outline-none",
          "hover:bg-chrome-hover focus:bg-chrome-hover transition-colors",
          readOnly && "hover:bg-transparent focus:bg-transparent cursor-default",
        )}
        onFocus={(event) => {
          if (readOnly) {
            event.currentTarget.blur();
            return;
          }
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

export function SharedTopLeftBar({
  title,
  back,
  leaving,
  autoRefresh,
  refreshSeconds,
  onAutoRefreshChange,
  onRefreshIntervalChange,
  onRefresh,
  onLeave,
}: {
  title: string;
  back: string;
  leaving: boolean;
  autoRefresh: boolean;
  refreshSeconds: number;
  onAutoRefreshChange: (enabled: boolean) => void;
  onRefreshIntervalChange: (seconds: number) => void;
  onRefresh: () => void;
  onLeave: () => void;
}) {
  const [confirmLeave, setConfirmLeave] = useState(false);

  return (
    <ChromePill className="max-w-[calc(100vw-24px)] pr-1">
      <ChromeTooltip label="共享给我" side="bottom">
        <Link
          to={back}
          aria-label="共享给我"
          className="hover:bg-chrome-hover grid size-8 shrink-0 place-items-center rounded-full"
        >
          <ArrowLeft className="size-4" />
        </Link>
      </ChromeTooltip>
      <CanvasTitle title={title} readOnly />
      <span className="text-muted-foreground shrink-0 px-1 text-xs">仅可查看</span>
      <SharedRefreshControl
        enabled={autoRefresh}
        seconds={refreshSeconds}
        onEnabledChange={onAutoRefreshChange}
        onIntervalChange={onRefreshIntervalChange}
        onRefresh={onRefresh}
      />
      <AlertDialog open={confirmLeave} onOpenChange={(open) => !leaving && setConfirmLeave(open)}>
        <AlertDialogTrigger
          render={
            <ChromeButton className="bg-destructive/10 text-destructive hover:bg-destructive/20 hover:text-destructive">
              退出共享
            </ChromeButton>
          }
        />
        <AlertDialogContent>
          <AlertDialogHeader>
            <AlertDialogTitle>退出这张画布的共享？</AlertDialogTitle>
            <AlertDialogDescription>
              退出后，这张画布将从你的「共享给我」列表中移除，你也将失去查看权限。原画布会保留在分享者的作品中。如需再次查看，请联系分享者重新共享。
            </AlertDialogDescription>
          </AlertDialogHeader>
          <AlertDialogFooter>
            <AlertDialogCancel disabled={leaving}>取消</AlertDialogCancel>
            <AlertDialogAction variant="destructive" disabled={leaving} onClick={onLeave}>
              {leaving ? "正在退出…" : "确认退出共享"}
            </AlertDialogAction>
          </AlertDialogFooter>
        </AlertDialogContent>
      </AlertDialog>
    </ChromePill>
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

/** 左上角：品牌菜单（回列表、新建画布、导出画布）、画布名、保存状态 */
export function TopLeftBar({
  canvasId,
  title,
  onRename,
  saveStatus,
  onSaveNow,
  getVersion,
  readOnly,
}: {
  /** 当前画布 ID，导出用 */
  canvasId: string;
  title: string;
  onRename: (title: string) => void;
  saveStatus: SaveStatus;
  /** 立即保存，返回保存后是否已经没有未保存的内容 */
  getVersion?: () => number;
  onSaveNow: () => Promise<boolean>;
  /** 只读预览：名字不能改，也不摆保存状态（这里没有要保存的东西，「已保存」会让人误以为是这一份） */
  readOnly?: boolean;
}) {
  const navigate = useNavigate();
  const [creating, setCreating] = useState(false);
  const [exporting, setExporting] = useState(false);
  /** 已点取消、正在等请求收尾；弹窗据此把按钮置灰 */
  const [cancelling, setCancelling] = useState(false);
  /** 当前这次导出的取消控制器；每次导出新建一个 */
  const exportCancel = useRef<TransferCancel | null>(null);

  /** 离开前先把没存的内容存掉；存不上就留在原地，别让内容悄悄丢了 */
  const leaveTo = async (to: string) => {
    if (!(await onSaveNow())) {
      canvasNotice("error", "还有内容没保存上，已留在当前画布", { id: "leave-save-failed" });
      return;
    }
    navigate(to);
  };

  const newCanvas = async () => {
    if (creating) return;
    setCreating(true);
    try {
      if (!(await onSaveNow())) {
        canvasNotice("error", "还有内容没保存上，已留在当前画布", { id: "leave-save-failed" });
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

  /** 导出期间菜单项置灰，防止重复点击连发请求、连下多份文件 */
  const exportThisCanvas = async () => {
    if (exporting) return;
    const cancel = createTransferCancel();
    exportCancel.current = cancel;
    setExporting(true);
    try {
      const outcome = await runCanvasExport({
        canvasId,
        title,
        saveNow: onSaveNow,
        exportCanvas,
        signal: cancel.signal,
        download: downloadJson,
        notify: toast,
      });
      // 请求层不会为主动取消弹提示，这里告诉用户；导出不写数据，不需要刷新什么
      if (outcome === "cancelled") toast.info("已取消导出");
    } finally {
      exportCancel.current = null;
      setCancelling(false);
      setExporting(false);
    }
  };

  /** 点「取消」：中止请求，弹窗继续锁住，等 exportThisCanvas 收尾后才关闭 */
  const cancelExport = () => {
    if (!exportCancel.current || cancelling) return;
    setCancelling(true);
    exportCancel.current.cancel();
  };

  return (
    <ChromePill className="max-w-[calc(100vw-24px)] pr-1">
      <DropdownMenu modal={false}>
        <ChromeTooltip label="画布菜单" side="bottom">
          <DropdownMenuTrigger
            render={
              <ChromeButton aria-label="画布菜单" className="gap-1 pr-1.5 pl-1">
                {/* 按钮带着 [&_svg]:size-4 会把 svg 压成 16px，这里用 ! 压回来，字形才有 21px 高 */}
                <Logo size={30} className="size-7.5!" />
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
          <DropdownMenuSeparator />
          {!readOnly && getVersion && (
            <DropdownMenuItem
              onClick={async () => {
                if (!(await onSaveNow())) {
                  toast.error("共享前保存失败，请先处理未保存的改动");
                  return;
                }
                openDialog(DramaShareDialog, {
                  id: canvasId,
                  title,
                  revision: getVersion(),
                  transferred: () => navigate("/canvases?scope=mine", { replace: true }),
                });
              }}
            >
              共享画布
            </DropdownMenuItem>
          )}
          <DropdownMenuItem disabled={exporting} onClick={() => void exportThisCanvas()}>
            <FileDown />
            导出画布
          </DropdownMenuItem>
        </DropdownMenuContent>
      </DropdownMenu>
      <CanvasTitle title={title} onRename={onRename} readOnly={readOnly} />
      {!readOnly && (
        <SaveIndicator key={saveStatus} status={saveStatus} onSave={() => void onSaveNow()} />
      )}
      {/* 导出期间把用户锁在进度弹窗里：导出的是已保存的内容，此时再改画布不会进这份文件 */}
      <TransferProgressDialog
        open={exporting}
        kind="export"
        cancelling={cancelling}
        onCancel={cancelExport}
      />
    </ChromePill>
  );
}
