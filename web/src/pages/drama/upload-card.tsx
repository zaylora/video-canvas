import { useCallback, useMemo, useState } from "react";
import { useDropzone } from "react-dropzone";
import { useNavigate } from "react-router";
import { motion } from "motion/react";
import { ArrowUp, Loader2 } from "lucide-react";
import { toast } from "sonner";

import { createDrama } from "@/api/drama";
import { DRAMA_ERR, dramaErrorMessage, isDramaError } from "@/api/drama/errors";
import type { DramaRatio } from "@/api/drama/type";
import { AgentModelPicker } from "@/components/drama/agent-model-picker";
import {
  ChipPopover,
  EpisodesPicker,
  GenreList,
  RatioList,
  StyleGrid,
} from "@/components/drama/pickers";
import { Segmented } from "@/components/home/segmented";
import { Textarea } from "@/components/ui/textarea";
import { useDramaStyles } from "@/hooks/use-drama-styles";
import { TAP } from "@/lib/motion";
import { cn } from "@/lib/utils";
import {
  checkSourceFile,
  checkSourceText,
  CUSTOM_STYLE_KEY,
  DEFAULT_EPISODE_SECONDS,
  DEFAULT_STYLE_KEY,
  extOf,
  SOURCE_MAX_CHARS,
  targetEpisodesLabel,
} from "@/utils/drama/options";

type Tab = "file" | "paste";

const formatSize = (bytes: number) =>
  bytes >= 1024 * 1024
    ? `${(bytes / 1024 / 1024).toFixed(1)} MB`
    : `${(bytes / 1024).toFixed(1)} KB`;

/**
 * 上传区中间的一小摞稿纸：后两张斜着垫底，最上面一张画着横线；选好文件后横线上标出扩展名。
 * 悬停上传区时后两张稍微散开（只在有指针的设备上）。
 * @param ext 已选文件的扩展名；没有文件时只有横线
 */
function SourcePapers({ ext }: { ext?: string }) {
  const sheet =
    "bg-background ring-chrome-border absolute inset-0 rounded-xl shadow-[0_6px_16px_oklch(0_0_0/0.18)] ring-1 transition-transform duration-200 ease-[cubic-bezier(0.23,1,0.32,1)] dark:bg-[oklch(0.25_0_0)]";
  return (
    <span data-slot="source-papers" aria-hidden className="relative block h-13 w-11">
      <span
        className={cn(
          sheet,
          "-rotate-6 [@media(hover:hover)]:group-hover/drop:translate-x-[-8px] [@media(hover:hover)]:group-hover/drop:-rotate-[13deg]",
        )}
      />
      <span
        className={cn(
          sheet,
          "rotate-4 [@media(hover:hover)]:group-hover/drop:translate-x-[7px] [@media(hover:hover)]:group-hover/drop:rotate-[9deg]",
        )}
      />
      <span
        className={cn(
          sheet,
          "grid place-items-center bg-[repeating-linear-gradient(180deg,transparent_0_9px,var(--chrome-hover)_9px_10px)] bg-size-[64%_36px] bg-position-[center_10px] bg-no-repeat",
        )}
      >
        {ext && (
          <span className="text-beam bg-background rounded px-1 text-[11px] font-semibold uppercase dark:bg-[oklch(0.25_0_0)]">
            {ext}
          </span>
        )}
      </span>
    </span>
  );
}

/** 两块叠放的面板：选中的可交互，另一块淡出、模糊 2px、不可交互，读屏也跳过 */
const paneState = (active: boolean) => ({
  "aria-hidden": !active || undefined,
  inert: !active || undefined,
  className: cn(
    "col-start-1 row-start-1 h-44 rounded-[22px] transition-[opacity,filter] duration-180 ease-out motion-reduce:transition-none",
    !active && "pointer-events-none opacity-0 blur-[2px]",
  ),
});

/**
 * 新建短剧的创作卡（设计稿 §6.3、原型首页）：半透明胶囊面板，「上传剧本 / 小说 | 粘贴正文」二选一，
 * 两块面板叠放、同高，切换只淡入淡出；提交时只用当前那一种。上传区可拖入文件；
 * 底部的题材、画风、比例、集数与时长都是浮层选择；提交后建好短剧并进入工作台等研读。
 * 内容为空、格式不对、超长（67002 / 67003）都就地提示在卡片里，不弹全局错误。
 */
export function UploadCard() {
  const navigate = useNavigate();
  const { genres, styles } = useDramaStyles();
  const [tab, setTab] = useState<Tab>("file");
  const [file, setFile] = useState<File | null>(null);
  const [text, setText] = useState("");
  const [genre, setGenre] = useState("");
  const [styleKey, setStyleKey] = useState<string | null>(null);
  const [styleCustom, setStyleCustom] = useState("");
  // 画风、比例、模型进了工作台就不能再改：比例默认竖屏短剧常用的 9:16
  const [ratio, setRatio] = useState<DramaRatio>("9:16");
  const [textModel, setTextModel] = useState("");
  const [target, setTarget] = useState(0);
  const [seconds, setSeconds] = useState(DEFAULT_EPISODE_SECONDS);
  const [error, setError] = useState<string | null>(null);
  const [submitting, setSubmitting] = useState(false);

  /** 画风：用户没选时取默认（清单里有就用默认 key，没有取第一个） */
  const effectiveStyle =
    styleKey ?? (styles.find((s) => s.key === DEFAULT_STYLE_KEY) ?? styles[0])?.key ?? "";
  const styleName =
    effectiveStyle === CUSTOM_STYLE_KEY
      ? "自定义画风"
      : (styles.find((s) => s.key === effectiveStyle)?.name ?? "画风");
  const genreName = genres.find((g) => g.key === genre)?.name ?? genre;

  const onDrop = useCallback((accepted: File[]) => {
    const picked = accepted[0];
    if (!picked) return;
    const problem = checkSourceFile(picked);
    setError(problem);
    setFile(problem ? null : picked);
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
    !submitting &&
    !!effectiveStyle &&
    (tab === "file" ? !!file : text.trim().length > 0 && !localProblem);
  const shownError = error ?? localProblem;

  const submit = async () => {
    if (!canSend) return;
    setError(null);
    setSubmitting(true);
    try {
      const drama = await createDrama({
        ...(tab === "file" && file ? { file } : { text }),
        styleKey: effectiveStyle,
        styleCustom: effectiveStyle === CUSTOM_STYLE_KEY ? styleCustom.trim() : undefined,
        genre: genre || undefined,
        ratio,
        targetEpisodes: target,
        episodeSeconds: seconds,
        textModel: textModel || undefined,
      });
      navigate(`/drama/${drama.id}`);
    } catch (e) {
      if (isDramaError(e, DRAMA_ERR.sourceInvalid) || isDramaError(e, DRAMA_ERR.sourceTooLong)) {
        setError(dramaErrorMessage(e));
      } else {
        toast.error(dramaErrorMessage(e, "创建失败，请稍后重试"));
      }
      setSubmitting(false);
    }
  };

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
          <SourcePapers ext={file ? extOf(file.name) : undefined} />
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
              <div className="text-muted-foreground text-[13px] tracking-wide">
                txt · md · docx　30 万字以内
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

      <div className="flex flex-wrap items-center gap-1 px-0.5 pt-2.5">
        <ChipPopover
          variant="chrome"
          size="lg"
          label={genreName || "题材"}
          set={!!genre}
          width="w-80"
        >
          {(close) => (
            <GenreList
              genres={genres}
              value={genre}
              onPick={(key) => {
                setGenre(key);
                close();
              }}
            />
          )}
        </ChipPopover>
        <ChipPopover
          variant="chrome"
          size="lg"
          label={styleName}
          width="w-[46rem] max-w-[92vw] max-h-[min(70vh,560px)] overflow-y-auto"
        >
          {() => (
            <StyleGrid
              styles={styles}
              value={effectiveStyle}
              custom={styleCustom}
              onPick={setStyleKey}
              onCustomChange={setStyleCustom}
            />
          )}
        </ChipPopover>
        <ChipPopover variant="chrome" size="lg" label={ratio || "比例"} set={!!ratio}>
          {(close) => (
            <RatioList
              value={ratio}
              onPick={(next) => {
                setRatio(next);
                close();
              }}
            />
          )}
        </ChipPopover>
        <ChipPopover
          variant="chrome"
          size="lg"
          label={`${targetEpisodesLabel(target)} · ${seconds}s/集`}
        >
          {() => (
            <EpisodesPicker
              target={target}
              seconds={seconds}
              onTarget={setTarget}
              onSeconds={setSeconds}
            />
          )}
        </ChipPopover>
        <AgentModelPicker variant="chrome" value={textModel} onChange={setTextModel} />
        <motion.button
          type="button"
          whileTap={TAP}
          disabled={!canSend}
          onClick={() => void submit()}
          aria-label="开始"
          title={canSend ? "开始研读" : "先上传文件或粘贴文本"}
          className="bg-foreground text-background focus-visible:ring-ring/50 disabled:bg-chrome-hover disabled:text-muted-foreground ml-auto grid size-11 place-items-center rounded-full transition-colors duration-200 outline-none focus-visible:ring-3"
        >
          {submitting ? (
            <Loader2 className="size-4.5 animate-spin" />
          ) : (
            <ArrowUp className="size-4.5" />
          )}
        </motion.button>
      </div>
    </div>
  );
}
