import { useCallback, useMemo, useState } from "react";
import { useDropzone } from "react-dropzone";
import { useNavigate } from "react-router";
import { motion } from "motion/react";
import {
  ArrowUp,
  ClipboardPaste,
  FileUp,
  ListOrdered,
  Loader2,
  Palette,
  Ratio,
  Theater,
  Upload,
  X,
} from "lucide-react";
import { toast } from "sonner";

import { createDrama } from "@/api/drama";
import { DRAMA_ERR, dramaErrorMessage, isDramaError } from "@/api/drama/errors";
import type { DramaRatio } from "@/api/drama/type";
import {
  ChipPopover,
  EpisodesPicker,
  GenreList,
  RatioList,
  StyleGrid,
} from "@/components/drama/pickers";
import { Button } from "@/components/ui/button";
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

/** 切换 tab 的小按钮 */
function TabButton({
  active,
  onClick,
  children,
}: {
  active: boolean;
  onClick: () => void;
  children: React.ReactNode;
}) {
  return (
    <button
      type="button"
      role="tab"
      aria-selected={active}
      onClick={onClick}
      className={cn(
        "focus-visible:ring-ring/50 inline-flex h-8 items-center gap-1.5 rounded-lg px-3 text-[13px] outline-none focus-visible:ring-2 [&_svg]:size-4",
        active ? "bg-muted font-medium" : "text-muted-foreground hover:text-foreground",
      )}
    >
      {children}
    </button>
  );
}

/**
 * 新建短剧的创作卡（设计稿 §6.3、原型首页）：上传 / 粘贴两个 tab，可拖入文件；
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
  const [ratio, setRatio] = useState<DramaRatio>("");
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

  return (
    <div
      data-slot="drama-upload-card"
      className="bg-card ring-border mx-auto mt-6 w-full max-w-3xl rounded-2xl p-2 shadow-sm ring-1"
    >
      <div role="tablist" className="flex gap-1 px-1 pt-1 pb-2">
        <TabButton active={tab === "file"} onClick={() => setTab("file")}>
          <Upload />
          上传剧本 / 小说
        </TabButton>
        <TabButton active={tab === "paste"} onClick={() => setTab("paste")}>
          <ClipboardPaste />
          粘贴文本
        </TabButton>
      </div>

      <div
        {...getRootProps()}
        className={cn(
          "bg-muted/50 grid min-h-44 place-items-center rounded-xl border border-dashed p-4 transition-colors duration-150",
          isDragActive && "border-beam bg-muted",
          shownError && "border-destructive/50",
        )}
      >
        <input {...getInputProps()} accept=".txt,.md,.docx" />
        {tab === "paste" ? (
          <div className="w-full self-stretch">
            <Textarea
              value={text}
              disabled={submitting}
              onChange={(event) => {
                setText(event.target.value);
                setError(null);
              }}
              placeholder="把剧本或小说正文粘贴到这里（≤ 30 万字）"
              aria-label="剧本或小说正文"
              className="min-h-36 resize-none border-0 bg-transparent p-0 shadow-none focus-visible:ring-0"
            />
            {text.length > 0 && (
              <div
                className={cn(
                  "text-muted-foreground text-right text-xs tabular-nums",
                  text.length > SOURCE_MAX_CHARS && "text-destructive",
                )}
              >
                {text.length.toLocaleString()} / {SOURCE_MAX_CHARS.toLocaleString()} 字
              </div>
            )}
          </div>
        ) : file ? (
          <motion.div
            initial={{ opacity: 0, y: 6 }}
            animate={{ opacity: 1, y: 0 }}
            className="bg-card flex w-full max-w-sm items-center gap-3 rounded-xl border p-3"
          >
            <span className="bg-muted grid size-10 shrink-0 place-items-center rounded-lg text-xs font-semibold uppercase">
              {extOf(file.name)}
            </span>
            <div className="min-w-0 flex-1">
              <div className="truncate text-sm font-medium">{file.name}</div>
              <div className="text-muted-foreground text-xs tabular-nums">
                {formatSize(file.size)}
              </div>
            </div>
            <Button
              variant="ghost"
              size="icon-sm"
              aria-label="移除文件"
              disabled={submitting}
              onClick={() => {
                setFile(null);
                setError(null);
              }}
            >
              <X />
            </Button>
          </motion.div>
        ) : (
          <div className="flex flex-col items-center gap-2 text-center text-sm">
            <span className="bg-card grid size-11 place-items-center rounded-xl">
              <FileUp className="size-5" />
            </span>
            <div className="flex items-center gap-2">
              拖入文件，或
              <Button variant="outline" size="sm" onClick={open}>
                选择文件
              </Button>
            </div>
            <div className="text-muted-foreground text-xs">
              支持 txt / md / docx · 最多 30 万字 · PDF 暂不支持
            </div>
          </div>
        )}
      </div>

      {shownError && (
        <p role="alert" className="text-destructive px-2 pt-2 text-[13px]">
          {shownError}
        </p>
      )}

      <div className="flex flex-wrap items-center gap-1.5 px-1 pt-2 pb-1">
        <ChipPopover icon={<Theater />} label={genreName || "题材"} set={!!genre} width="w-80">
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
        <ChipPopover icon={<Palette />} label={styleName} width="w-[26rem] max-w-[90vw]">
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
        <ChipPopover icon={<Ratio />} label={ratio || "比例"} set={!!ratio}>
          {(close) => (
            <RatioList
              value={ratio}
              hint="可以之后再定，拆解前必选"
              onPick={(next) => {
                setRatio(next);
                close();
              }}
            />
          )}
        </ChipPopover>
        <ChipPopover
          icon={<ListOrdered />}
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
        <motion.button
          type="button"
          whileTap={TAP}
          disabled={!canSend}
          onClick={() => void submit()}
          aria-label="开始"
          title={canSend ? "开始研读" : "先上传文件或粘贴文本"}
          className="bg-primary text-primary-foreground focus-visible:ring-ring/50 ml-auto grid size-9 place-items-center rounded-full outline-none focus-visible:ring-3 disabled:opacity-40"
        >
          {submitting ? (
            <Loader2 className="size-4 animate-spin" />
          ) : (
            <ArrowUp className="size-4" />
          )}
        </motion.button>
      </div>
    </div>
  );
}
