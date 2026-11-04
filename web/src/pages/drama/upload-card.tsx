import { useCallback, useEffect, useMemo, useState } from "react";
import { useDropzone } from "react-dropzone";
import { useNavigate } from "react-router";
import { motion } from "motion/react";
import { ArrowUp, Loader2 } from "lucide-react";
import { toast } from "sonner";

import { createDrama, preflightSource } from "@/api/drama";
import { DRAMA_ERR, dramaErrorMessage, isDramaError } from "@/api/drama/errors";
import type { CreateDramaInput, SourcePreflight } from "@/api/drama/type";
import { Segmented } from "@/components/home/segmented";
import { Textarea } from "@/components/ui/textarea";
import { TAP } from "@/lib/motion";
import { cn } from "@/lib/utils";
import { checkSourceFile, checkSourceText, SOURCE_MAX_CHARS } from "@/utils/drama/options";

import { StartDialog } from "./start-dialog";

type Tab = "file" | "paste";

const formatSize = (bytes: number) =>
  bytes >= 1024 * 1024
    ? `${(bytes / 1024 / 1024).toFixed(1)} MB`
    : `${(bytes / 1024).toFixed(1)} KB`;

/** 两块叠放的面板：选中的可交互，另一块淡出、模糊 2px、不可交互，读屏也跳过 */
const paneState = (active: boolean) => ({
  "aria-hidden": !active || undefined,
  inert: !active || undefined,
  className: cn(
    "col-start-1 row-start-1 h-44 rounded-[22px] transition-[opacity,filter] duration-180 ease-out motion-reduce:transition-none",
    !active && "pointer-events-none opacity-0 blur-[2px]",
  ),
});

/** 超过这么久还没返回，发送键上显示上传进度环（毫秒） */
const PROGRESS_AFTER_MS = 1000;

/** 发送键外圈的环形进度：ratio 为 0–1 */
function ProgressRing({ ratio }: { ratio: number }) {
  const radius = 20;
  const length = 2 * Math.PI * radius;
  return (
    <svg
      aria-hidden
      viewBox="0 0 44 44"
      className="pointer-events-none absolute -inset-px size-[calc(100%+2px)] -rotate-90"
    >
      <circle
        cx="22"
        cy="22"
        r={radius}
        fill="none"
        stroke="currentColor"
        strokeWidth="2"
        strokeLinecap="round"
        strokeDasharray={length}
        strokeDashoffset={length * (1 - Math.min(1, Math.max(0, ratio)))}
        className="text-status-running transition-[stroke-dashoffset] duration-200"
      />
    </svg>
  );
}

/**
 * 新建短剧的创作卡（设计稿 §6.2、原型首页）：半透明胶囊面板，「上传剧本 / 小说 | 粘贴正文」二选一，
 * 两块面板叠放、同高，切换只淡入淡出；底栏只剩格式提示和发送键。
 * 点发送键先预检（解析、去噪、切单元、识别类型并估价，上传超过 1 秒时发送键显示进度环），
 * 成功后弹「开拍设置」，取消弹窗保留卡片里的文件 / 文本；在弹窗里点「开始」才建剧并进入工作台。
 * 内容为空、格式不对、超长（67002 / 67003）都就地提示在卡片里，不弹全局错误。
 */
export function UploadCard() {
  const navigate = useNavigate();
  const [tab, setTab] = useState<Tab>("file");
  const [file, setFile] = useState<File | null>(null);
  const [text, setText] = useState("");
  const [error, setError] = useState<string | null>(null);
  const [submitting, setSubmitting] = useState(false);
  /** 预检结果：内容没变时再点发送键直接重新打开弹窗，不重复上传 */
  const [pre, setPre] = useState<SourcePreflight | null>(null);
  const [dialogOpen, setDialogOpen] = useState(false);
  /** 上传进度 0–1；null = 还没有进度或上传不足 1 秒 */
  const [progress, setProgress] = useState<number | null>(null);
  const [slow, setSlow] = useState(false);

  useEffect(() => {
    if (!submitting) return;
    const timer = setTimeout(() => setSlow(true), PROGRESS_AFTER_MS);
    return () => {
      clearTimeout(timer);
      setSlow(false);
      setProgress(null);
    };
  }, [submitting]);

  const onDrop = useCallback((accepted: File[]) => {
    const picked = accepted[0];
    if (!picked) return;
    const problem = checkSourceFile(picked);
    setError(problem);
    setFile(problem ? null : picked);
    setPre(null);
  }, []);
  const { getRootProps, getInputProps, isDragActive, open } = useDropzone({
    onDrop,
    multiple: false,
    noClick: true,
    noKeyboard: true,
    disabled: tab !== "file" || submitting,
  });

  const localProblem = useMemo(
    () => (tab === "paste" && text.length > 0 ? checkSourceText(text) : null),
    [tab, text],
  );
  const canSend =
    !submitting && (tab === "file" ? !!file : text.trim().length > 0 && !localProblem);
  const shownError = error ?? localProblem;

  const preflight = async () => {
    if (!canSend) return;
    if (pre) {
      setDialogOpen(true);
      return;
    }
    setError(null);
    setSubmitting(true);
    try {
      const result = await preflightSource(tab === "file" && file ? { file } : { text }, (ratio) =>
        setProgress(ratio),
      );
      setPre(result);
      setDialogOpen(true);
    } catch (e) {
      if (isDramaError(e, DRAMA_ERR.sourceInvalid) || isDramaError(e, DRAMA_ERR.sourceTooLong)) {
        setError(dramaErrorMessage(e));
      } else {
        toast.error(dramaErrorMessage(e, "预检失败，请稍后重试"));
      }
    } finally {
      setSubmitting(false);
    }
  };

  /** 弹窗里点「开始」：建剧并进工作台；草稿过期（67014）回到卡片提示重新上传，其他错误抛回弹窗就地显示 */
  const create = useCallback(
    async (input: CreateDramaInput) => {
      try {
        const drama = await createDrama(input);
        navigate(`/drama/${drama.id}`);
      } catch (e) {
        if (isDramaError(e, DRAMA_ERR.sourceExpired)) {
          setDialogOpen(false);
          setPre(null);
          setError(dramaErrorMessage(e));
          return;
        }
        throw e;
      }
    },
    [navigate],
  );

  const upload = paneState(tab === "file");
  const paste = paneState(tab === "paste");

  return (
    <div
      data-slot="drama-upload-card"
      className="bg-chrome ring-chrome-border mx-auto mt-6 w-full max-w-270 rounded-[30px] p-2.5 shadow-lg ring-1 backdrop-blur-xl backdrop-saturate-150 md:mt-7"
    >
      <Segmented<Tab>
        variant="chrome"
        label="剧本来源"
        value={tab}
        onChange={setTab}
        options={[
          {
            value: "file",
            label: "上传剧本 / 小说",
          },
          {
            value: "paste",
            label: "粘贴正文",
          },
        ]}
      />

      <div className="mt-2.5 grid">
        <div
          {...getRootProps()}
          data-pane="upload"
          aria-hidden={upload["aria-hidden"]}
          inert={upload.inert}
          role="button"
          tabIndex={0}
          aria-label="选择剧本文件"
          onClick={open}
          onKeyDown={(event) => {
            if (event.key === "Enter" || event.key === " ") {
              event.preventDefault();
              open();
            }
          }}
          className={cn(
            upload.className,
            "group/drop focus-visible:ring-ring/50 flex cursor-pointer flex-col outline-none focus-visible:ring-3 items-center justify-center gap-3 border border-dashed border-[color-mix(in_oklch,var(--foreground)_16%,transparent)] px-4 text-center",
            file && "border-chrome-border border-solid",
            isDragActive && "border-beam bg-beam/5",
            shownError && "border-destructive/50",
          )}
        >
          <input {...getInputProps()} accept=".txt,.md,.docx" />
          {file ? (
            <motion.div
              initial={{ opacity: 0 }}
              animate={{ opacity: 1 }}
              className="flex max-w-[90%] flex-col items-center gap-1"
            >
              <div className="max-w-full truncate text-[15.5px] font-medium">{file.name}</div>
              <div className="text-muted-foreground flex items-center gap-1 text-[13px] tabular-nums">
                {formatSize(file.size)}
                <span className="hover:bg-chrome-hover hover:text-foreground rounded-full px-2 py-0.5 transition-colors">
                  更换
                </span>
                <button
                  type="button"
                  disabled={submitting}
                  onClick={(event) => {
                    event.stopPropagation();
                    setFile(null);
                    setPre(null);
                    setError(null);
                  }}
                  className="hover:bg-chrome-hover hover:text-foreground focus-visible:ring-ring/50 rounded-full px-2 py-0.5 transition-colors outline-none focus-visible:ring-2"
                >
                  移除
                </button>
              </div>
            </motion.div>
          ) : (
            <>
              <div className="flex items-center gap-2.5 text-[15.5px]">
                拖入剧本文件，或
                <span className="bg-chrome-hover ring-chrome-border group-hover/drop:text-foreground inline-flex h-9 items-center rounded-full px-4 text-sm ring-1 transition-colors">
                  选择文件
                </span>
              </div>
            </>
          )}
        </div>

        <div
          data-pane="paste"
          aria-hidden={paste["aria-hidden"]}
          inert={paste.inert}
          className={cn(paste.className, "ring-chrome-border relative ring-1")}
        >
          <Textarea
            value={text}
            disabled={submitting}
            onChange={(event) => {
              setText(event.target.value);
              setPre(null);
              setError(null);
            }}
            placeholder="粘贴剧本或小说正文"
            aria-label="剧本或小说正文"
            className="h-full resize-none border-0 bg-transparent px-5 pt-4 pb-9 text-[15.5px] leading-relaxed shadow-none focus-visible:ring-0 dark:bg-transparent"
          />
          {text.length > 0 && (
            <div
              className={cn(
                "text-muted-foreground pointer-events-none absolute right-4 bottom-3 text-xs tabular-nums",
                text.length > SOURCE_MAX_CHARS && "text-destructive",
              )}
            >
              {text.length.toLocaleString()} / {SOURCE_MAX_CHARS.toLocaleString()} 字
            </div>
          )}
        </div>
      </div>

      {shownError && (
        <p role="alert" className="text-destructive px-2 pt-2 text-[13px]">
          {shownError}
        </p>
      )}

      <div className="flex items-center gap-2 px-0.5 pt-2.5">
        <span className="text-muted-foreground pl-2.5 text-[13px] tracking-wide">
          txt · md · docx　500 万字以内
        </span>
        <motion.button
          type="button"
          whileTap={TAP}
          disabled={!canSend}
          onClick={() => void preflight()}
          aria-label="开始"
          title={canSend ? "预检原文并设置开拍" : "先上传文件或粘贴文本"}
          className="bg-foreground text-background focus-visible:ring-ring/50 disabled:bg-chrome-hover disabled:text-muted-foreground relative ml-auto grid size-11 place-items-center rounded-full transition-colors duration-200 outline-none focus-visible:ring-3"
        >
          {submitting ? (
            <Loader2 className="size-4.5 animate-spin" />
          ) : (
            <ArrowUp className="size-4.5" />
          )}
          {submitting && slow && progress !== null && <ProgressRing ratio={progress} />}
        </motion.button>
      </div>

      <StartDialog open={dialogOpen} onOpenChange={setDialogOpen} pre={pre} onSubmit={create} />
    </div>
  );
}
