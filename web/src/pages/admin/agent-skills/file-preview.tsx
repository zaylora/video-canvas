import { File as FileIcon, FileText, ImageIcon, Loader2, Terminal } from "lucide-react";
import { motion } from "motion/react";
import { useEffect, useRef, useState } from "react";
import ReactMarkdown, { type Components } from "react-markdown";
import remarkGfm from "remark-gfm";

import type {
  SkillFile,
  SkillFileContent,
  SkillFileKind,
  SkillIssue,
} from "@/api/admin/agent-skill/type.d";
import { CopyButton } from "@/components/admin-ui/copy-button";
import { Notice } from "@/components/admin-ui/notice";
import { Segmented, SegmentedItem } from "@/components/admin-ui/segmented";
import { Tag } from "@/components/admin-ui/tag";
import { Button } from "@/components/ui/button";
import { Skeleton } from "@/components/ui/skeleton";
import { DURATION, EASE_OUT } from "@/lib/motion";
import { cn } from "@/lib/utils";
import {
  fileTagLabel,
  formatSize,
  ISSUE_LEVEL_LABEL,
  locateLine,
  stripFrontmatter,
} from "@/utils/admin/agent-skill";

/** 源码态最多渲染的行数，超出的注明已截断（长文件一次画几千行会卡） */
const SOURCE_MAX_LINES = 400;

/** 要在源码里高亮并滚动到的位置：行号，或要在文本里查找的字符串（MISSING_REF 的引用路径） */
export type PreviewHighlight = {
  /** 每次点问题递增，同一个目标重复点击也能重新定位 */
  nonce: number;
  /** 直接指定行号 */
  line?: number;
  /** 在文本里找第一次出现这段字符串的行 */
  needle?: string;
};

const KIND_ICON: Record<SkillFileKind, typeof FileText> = {
  skill: FileText,
  doc: FileText,
  script: Terminal,
  asset: ImageIcon,
  other: FileIcon,
};

/** 渲染态里的 Markdown 排版：全部走 token，间距取 4 的倍数 */
const MD_COMPONENTS: Components = {
  h1: ({ node: _n, ...p }) => <h1 className="mt-1 mb-3 text-xl font-semibold" {...p} />,
  h2: ({ node: _n, ...p }) => <h2 className="mt-5 mb-2 text-base font-semibold" {...p} />,
  h3: ({ node: _n, ...p }) => <h3 className="mt-4 mb-1 text-sm font-semibold" {...p} />,
  p: ({ node: _n, ...p }) => <p className="my-2" {...p} />,
  ul: ({ node: _n, ...p }) => <ul className="my-2 list-disc space-y-1 pl-5" {...p} />,
  ol: ({ node: _n, ...p }) => <ol className="my-2 list-decimal space-y-1 pl-5" {...p} />,
  blockquote: ({ node: _n, ...p }) => (
    <blockquote className="border-border text-muted-foreground my-2 border-l-2 pl-3" {...p} />
  ),
  hr: () => <hr className="border-border my-4" />,
  a: ({ node: _n, ...p }) => (
    <a
      className="text-primary underline underline-offset-2"
      target="_blank"
      rel="noreferrer noopener"
      {...p}
    />
  ),
  pre: ({ node: _n, ...p }) => (
    <pre
      className="bg-muted my-2 overflow-x-auto rounded-lg px-3 py-2 text-xs leading-relaxed"
      {...p}
    />
  ),
  code: ({ node: _n, className, ...p }) => (
    <code
      className={cn("font-mono text-xs", !className && "bg-muted rounded px-1 py-0.5", className)}
      {...p}
    />
  ),
  table: ({ node: _n, ...p }) => (
    <div className="my-2 overflow-x-auto">
      <table className="w-full border-collapse text-sm" {...p} />
    </div>
  ),
  th: ({ node: _n, ...p }) => (
    <th className="border-border bg-muted/50 border px-2 py-1 text-left font-medium" {...p} />
  ),
  td: ({ node: _n, ...p }) => <td className="border-border border px-2 py-1" {...p} />,
};

/** 头部字段卡：每个字段标“已采用 / 未支持” */
export function FrontmatterCard({
  frontmatter,
  unsupported,
  className,
}: {
  frontmatter: Record<string, unknown>;
  unsupported: readonly string[];
  className?: string;
}) {
  const rows = Object.entries(frontmatter);
  if (rows.length === 0) return null;
  return (
    <section
      data-slot="frontmatter-card"
      className={cn("mx-4 mt-4 overflow-hidden rounded-lg border", className)}
    >
      <h4 className="text-muted-foreground bg-muted/50 border-b px-3 py-1.5 text-xs">
        头部字段（<span className="tabular-nums">{rows.length}</span>）
      </h4>
      <dl>
        {rows.map(([key, value]) => (
          <div
            key={key}
            className="grid grid-cols-[120px_minmax(0,1fr)_auto] items-start gap-3 border-b px-3 py-2 text-sm last:border-b-0 max-md:grid-cols-[96px_minmax(0,1fr)_auto]"
          >
            <dt className="text-muted-foreground font-mono text-xs">{key}</dt>
            <dd className="min-w-0 break-words">
              {typeof value === "string" ? value : JSON.stringify(value)}
            </dd>
            <Tag tone={unsupported.includes(key) ? "warning" : "success"}>
              {unsupported.includes(key) ? "未支持" : "已采用"}
            </Tag>
          </div>
        ))}
      </dl>
    </section>
  );
}

/** 带行号的源码；highlightLine 那一行琥珀高亮并滚动到视野中央，flashKey 变化时闪一下 */
function SourceView({
  text,
  highlightLine,
  flashKey,
}: {
  text: string;
  highlightLine: number | null;
  flashKey: number;
}) {
  const lines = text.split("\n");
  const limit = Math.max(SOURCE_MAX_LINES, (highlightLine ?? 0) + 20);
  const shown = lines.slice(0, limit);
  const box = useRef<HTMLOListElement>(null);
  useEffect(() => {
    if (highlightLine == null) return;
    box.current?.querySelector("[data-highlight]")?.scrollIntoView({ block: "center" });
  }, [highlightLine, flashKey]);
  return (
    <>
      <ol
        ref={box}
        data-slot="source-view"
        className="py-2.5 font-mono text-xs leading-5 tabular-nums"
      >
        {shown.map((line, index) => {
          const hit = highlightLine === index + 1;
          return (
            <li
              key={index}
              data-highlight={hit || undefined}
              className={cn(
                "relative grid grid-cols-[44px_minmax(0,1fr)]",
                hit && "bg-amber-500/14 shadow-[inset_2px_0_0_var(--color-amber-500)]",
              )}
            >
              {hit && (
                <motion.span
                  key={flashKey}
                  aria-hidden="true"
                  className="pointer-events-none absolute inset-0 bg-violet-500/20"
                  initial={{ opacity: 1 }}
                  animate={{ opacity: 0 }}
                  transition={{ duration: DURATION.slow, ease: EASE_OUT }}
                />
              )}
              <span className="text-muted-foreground/70 pr-3 text-right select-none">
                {index + 1}
              </span>
              <span className="pr-3.5 break-words whitespace-pre-wrap">{line || " "}</span>
            </li>
          );
        })}
      </ol>
      {lines.length > shown.length && (
        <p className="text-muted-foreground border-t px-4 py-2 text-xs">
          源码较长，只显示前 <span className="tabular-nums">{shown.length}</span> 行，共{" "}
          <span className="tabular-nums">{lines.length}</span> 行
        </p>
      )}
    </>
  );
}

/** 二进制或过大文件：不预览，说明 Agent 读到它会得到什么 */
function BinaryPanel({ size }: { size: number }) {
  return (
    <div className="text-muted-foreground flex h-full min-h-56 flex-col items-center justify-center gap-2 p-6 text-center">
      <span className="bg-muted grid size-11 place-items-center rounded-xl border">
        <FileIcon className="size-5" />
      </span>
      <p className="text-foreground text-sm font-medium">不能预览这个文件</p>
      <p className="max-w-sm text-xs leading-relaxed">
        它是二进制文件，或超过 1 MB。Agent 读到它只会得到“二进制文件，大小{" "}
        <span className="tabular-nums">{formatSize(size)}</span>，无法读取”。
      </p>
    </div>
  );
}

/**
 * 文件预览：头部（图标 / 路径 / 类型 / 大小 / 渲染·源码 / 复制）+ 正文。
 * SKILL.md 渲染态先显示头部字段卡再显示 Markdown；源码态带行号；二进制与超过 1 MB 的文件只给说明。
 * 内容由 readFile 取（上层负责缓存与按导入暂存 / 技能版本选择接口）。
 * @param file 选中的文件清单项；为空时显示“选择一个文件”
 * @param readFile 读文件内容
 * @param issues 这个文件自己的问题，显示在正文顶部
 * @param highlight 要定位的行（点击问题后）
 * @param frontmatter SKILL.md 的头部字段，仅 skill 类型文件使用
 * @param unsupported 本系统未支持的头部字段名
 */
export function FilePreview({
  file,
  readFile,
  issues,
  highlight,
  frontmatter,
  unsupported,
}: {
  file: SkillFile | null;
  readFile: (path: string) => Promise<SkillFileContent>;
  issues: readonly SkillIssue[];
  highlight: PreviewHighlight | null;
  frontmatter: Record<string, unknown>;
  unsupported: readonly string[];
}) {
  const [content, setContent] = useState<{ path: string; value: SkillFileContent } | null>(null);
  const [failedPath, setFailedPath] = useState<string | null>(null);
  const [attempt, setAttempt] = useState(0);
  const isMarkdown = !!file && /\.md$/i.test(file.path);
  const [mode, setMode] = useState<"render" | "source">("render");

  const path = file?.path ?? null;
  const readable = !!file && file.text;

  useEffect(() => {
    if (!path || !readable) return;
    let stale = false;
    readFile(path).then(
      (value) => {
        if (!stale) {
          setContent({ path, value });
          setFailedPath(null);
        }
      },
      () => {
        if (!stale) setFailedPath(path);
      },
    );
    return () => {
      stale = true;
    };
  }, [path, readable, readFile, attempt]);

  useEffect(() => {
    if (highlight) setMode("source");
  }, [highlight]);

  if (!file) {
    return (
      <div className="text-muted-foreground grid h-full min-h-56 place-items-center p-6 text-sm">
        选择左侧的一个文件查看内容
      </div>
    );
  }

  const Icon = KIND_ICON[file.kind];
  const value = content?.path === file.path ? content.value : null;
  const failed = failedPath === file.path;
  const text = value?.text ?? "";
  const showSource = mode === "source" || !isMarkdown;
  const highlightLine =
    highlight && value && !value.binary
      ? (highlight.line ?? (highlight.needle ? locateLine(text, highlight.needle) : null))
      : null;

  return (
    <div data-slot="file-preview" className="flex min-h-0 min-w-0 flex-1 flex-col">
      <div className="flex h-11 shrink-0 items-center gap-2 border-b px-3.5">
        <Icon className="text-muted-foreground size-4 shrink-0" />
        <span className="min-w-0 truncate font-mono text-sm" title={file.path}>
          {file.path}
        </span>
        <Tag className="max-md:hidden">{fileTagLabel(file)}</Tag>
        <span className="text-muted-foreground text-xs tabular-nums max-md:hidden">
          {formatSize(file.size)}
        </span>
        <div className="ml-auto flex shrink-0 items-center gap-1.5">
          {isMarkdown && file.text && (
            <Segmented aria-label="预览方式">
              <SegmentedItem
                slideId="preview-mode"
                active={!showSource}
                onClick={() => setMode("render")}
              >
                渲染
              </SegmentedItem>
              <SegmentedItem
                slideId="preview-mode"
                active={showSource}
                onClick={() => setMode("source")}
              >
                源码
              </SegmentedItem>
            </Segmented>
          )}
          {file.text && value && !value.binary && <CopyButton text={text} />}
        </div>
      </div>

      <div className="min-h-0 flex-1 overflow-auto">
        {issues.length > 0 && (
          <div className="grid gap-1.5 px-3.5 pt-2.5">
            {issues.map((issue, index) => (
              <Notice
                key={`${issue.code}-${index}`}
                tone={
                  issue.level === "error" ? "danger" : issue.level === "warn" ? "warning" : "info"
                }
                title={ISSUE_LEVEL_LABEL[issue.level]}
              >
                {issue.message}
              </Notice>
            ))}
          </div>
        )}

        {!file.text || value?.binary ? (
          <BinaryPanel size={file.size} />
        ) : failed ? (
          <div className="text-muted-foreground flex h-full min-h-56 flex-col items-center justify-center gap-2 p-6 text-sm">
            文件读取失败
            <Button variant="outline" size="sm" onClick={() => setAttempt((n) => n + 1)}>
              重试
            </Button>
          </div>
        ) : !value ? (
          <div className="grid gap-2 p-4" aria-busy="true" aria-label="正在读取文件">
            <Loader2 className="text-muted-foreground size-4 animate-spin" />
            <Skeleton className="h-4 w-2/3" />
            <Skeleton className="h-4 w-1/2" />
            <Skeleton className="h-4 w-3/4" />
          </div>
        ) : showSource ? (
          <>
            <SourceView
              text={text}
              highlightLine={highlightLine}
              flashKey={highlight?.nonce ?? 0}
            />
            {value.truncated && (
              <p className="text-muted-foreground border-t px-4 py-2 text-xs">
                文件较长，后端只返回了开头的一部分。
              </p>
            )}
          </>
        ) : (
          <>
            {file.kind === "skill" && (
              <FrontmatterCard frontmatter={frontmatter} unsupported={unsupported} />
            )}
            <div className="max-w-3xl px-5 pt-4 pb-6 text-sm leading-relaxed">
              <ReactMarkdown remarkPlugins={[remarkGfm]} components={MD_COMPONENTS}>
                {stripFrontmatter(text).body}
              </ReactMarkdown>
            </div>
          </>
        )}
      </div>
    </div>
  );
}
