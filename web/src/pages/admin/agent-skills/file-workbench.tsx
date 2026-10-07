import {
  ChevronRight,
  CircleAlert,
  File as FileIcon,
  FileText,
  Folder,
  FolderOpen,
  ImageIcon,
  Info,
  Terminal,
  TriangleAlert,
} from "lucide-react";
import {
  useCallback,
  useMemo,
  useRef,
  useState,
  type CSSProperties,
  type KeyboardEvent,
} from "react";

import type {
  SkillFile,
  SkillFileContent,
  SkillFileKind,
  SkillIssue,
  SkillIssueLevel,
} from "@/api/admin/agent-skill/type.d";
import { Segmented, SegmentedItem } from "@/components/admin-ui/segmented";
import { Tag } from "@/components/admin-ui/tag";
import { DURATION, EASE_OUT_CSS, ms } from "@/lib/motion";
import { cn } from "@/lib/utils";
import {
  buildFileTree,
  countIssues,
  formatSize,
  ISSUE_LEVEL_LABEL,
  issueLevelByPath,
  sortIssues,
  visibleTreeRows,
} from "@/utils/admin/agent-skill";

import { FilePreview, type PreviewHighlight } from "./file-preview";

const MOTION_VARS = {
  "--motion-fast": ms(DURATION.fast),
  "--motion-ease": EASE_OUT_CSS,
} as CSSProperties;

const KIND_ICON: Record<SkillFileKind, typeof FileText> = {
  skill: FileText,
  doc: FileText,
  script: Terminal,
  asset: ImageIcon,
  other: FileIcon,
};

const LEVEL_ICON: Record<SkillIssueLevel, { icon: typeof Info; className: string }> = {
  error: { icon: CircleAlert, className: "text-red-600 dark:text-red-400" },
  warn: { icon: TriangleAlert, className: "text-amber-600 dark:text-amber-400" },
  info: { icon: Info, className: "text-sky-600 dark:text-sky-400" },
};

/** 找 SKILL.md（不区分大小写）的路径 */
const findSkillMd = (files: readonly SkillFile[]) =>
  files.find((f) => f.path.toLowerCase() === "skill.md")?.path ?? null;

/**
 * 文件工作台（导入预检与技能详情的“文件”页签共用）：
 * 左栏是「文件 / 问题 N」分段 + 文件树或问题列表，右栏是预览。点问题定位到对应文件，
 * MISSING_REF 这类“引用了不存在的文件”的问题落到 SKILL.md 并高亮引用所在的行。
 * 按包（导入 id / 技能版本）切换时请用 key 让它重新挂载：内容缓存和选中状态都挂在实例上。
 * @param files 文件清单
 * @param issues 预检问题，没有传空数组
 * @param readFile 读一个文件的内容；同一个实例内同一路径只会请求一次
 * @param frontmatter SKILL.md 的头部字段
 * @param unsupported 本系统未支持的头部字段名
 * @param className 外层样式
 */
export function FileWorkbench({
  files,
  issues,
  readFile,
  frontmatter,
  unsupported,
  className,
}: {
  files: readonly SkillFile[];
  issues: readonly SkillIssue[];
  readFile: (path: string) => Promise<SkillFileContent>;
  frontmatter: Record<string, unknown>;
  unsupported: readonly string[];
  className?: string;
}) {
  const skillMd = findSkillMd(files);
  const count = countIssues(issues);
  const sorted = useMemo(() => sortIssues(issues), [issues]);
  const tree = useMemo(() => buildFileTree(files), [files]);
  /** 文件树上的问题圆点：MISSING_REF 的 path 是不存在的文件，圆点落到 SKILL.md 上 */
  const levels = useMemo(
    () =>
      issueLevelByPath(
        issues.map((i) => (i.code === "MISSING_REF" && skillMd ? { ...i, path: skillMd } : i)),
      ),
    [issues, skillMd],
  );

  const [tab, setTab] = useState<"files" | "issues">(count.error > 0 ? "issues" : "files");
  const [selected, setSelected] = useState<string | null>(skillMd ?? files[0]?.path ?? null);
  const [collapsed, setCollapsed] = useState<ReadonlySet<string>>(new Set());
  const [highlight, setHighlight] = useState<PreviewHighlight | null>(null);
  const nonce = useRef(0);
  const listRef = useRef<HTMLDivElement>(null);

  const cache = useRef(new Map<string, Promise<SkillFileContent>>());
  const cachedRead = useCallback(
    (path: string) => {
      let pending = cache.current.get(path);
      if (!pending) {
        pending = readFile(path);
        cache.current.set(path, pending);
        pending.catch(() => cache.current.delete(path));
      }
      return pending;
    },
    [readFile],
  );

  const rows = useMemo(() => visibleTreeRows(tree, collapsed), [tree, collapsed]);
  const selectedFile = files.find((f) => f.path === selected) ?? null;
  const fileIssues = useMemo(
    () =>
      issues.filter(
        (i) =>
          i.path === selected || (i.code === "MISSING_REF" && !!skillMd && selected === skillMd),
      ),
    [issues, selected, skillMd],
  );

  const select = (path: string) => {
    setSelected(path);
    setHighlight(null);
  };

  const toggleDir = (path: string) =>
    setCollapsed((prev) => {
      const next = new Set(prev);
      if (next.has(path)) next.delete(path);
      else next.add(path);
      return next;
    });

  /** ↑↓ 在可见的文件之间切换（目录跳过），焦点跟着走 */
  const onTreeKey = (event: KeyboardEvent<HTMLDivElement>) => {
    if (event.key !== "ArrowDown" && event.key !== "ArrowUp") return;
    const visible = rows.filter((r) => r.node.kind === "file").map((r) => r.node.path);
    if (visible.length === 0) return;
    event.preventDefault();
    const index = selected ? visible.indexOf(selected) : -1;
    const next =
      event.key === "ArrowDown"
        ? visible[Math.min(visible.length - 1, index + 1)]
        : visible[Math.max(0, index - 1)];
    select(next);
    listRef.current?.querySelector<HTMLElement>(`[data-path="${CSS.escape(next)}"]`)?.focus();
  };

  const focusIssue = (issue: SkillIssue) => {
    if (!issue.path) return;
    const exists = files.some((f) => f.path === issue.path);
    if (issue.code === "MISSING_REF" && skillMd) {
      setSelected(skillMd);
      nonce.current += 1;
      setHighlight({ nonce: nonce.current, needle: issue.path });
    } else if (exists) {
      select(issue.path);
    }
  };

  return (
    <div
      data-slot="file-workbench"
      style={MOTION_VARS}
      className={cn(
        "grid min-h-0 flex-1 grid-cols-[296px_minmax(0,1fr)] max-md:grid-cols-1 max-md:grid-rows-[220px_minmax(0,1fr)]",
        className,
      )}
    >
      <div className="bg-muted/30 flex min-h-0 flex-col border-r max-md:border-r-0 max-md:border-b">
        {issues.length > 0 && (
          <Segmented aria-label="文件或问题" className="mx-2.5 mt-2.5 mb-1.5 shrink-0">
            <SegmentedItem
              slideId="workbench-tab"
              active={tab === "files"}
              className="flex-1"
              onClick={() => setTab("files")}
            >
              文件
            </SegmentedItem>
            <SegmentedItem
              slideId="workbench-tab"
              active={tab === "issues"}
              className="flex-1"
              onClick={() => setTab("issues")}
            >
              问题 <span className="tabular-nums">{count.problems}</span>
            </SegmentedItem>
          </Segmented>
        )}

        {tab === "files" || issues.length === 0 ? (
          <div
            ref={listRef}
            role="tree"
            aria-label="文件"
            onKeyDown={onTreeKey}
            className="min-h-0 flex-1 overflow-auto px-1.5 pt-1 pb-2.5"
          >
            {rows.length === 0 && (
              <p className="text-muted-foreground px-3 py-6 text-center text-xs">
                这个包里没有文件
              </p>
            )}
            {rows.map(({ node, depth }) => {
              const isDir = node.kind === "dir";
              const open = isDir && !collapsed.has(node.path);
              const level = levels.get(node.path);
              const file = node.file;
              const Icon = isDir ? (open ? FolderOpen : Folder) : KIND_ICON[file?.kind ?? "other"];
              const active = !isDir && node.path === selected;
              return (
                <button
                  key={node.path}
                  type="button"
                  role="treeitem"
                  data-path={node.path}
                  aria-level={depth + 1}
                  aria-selected={active}
                  aria-expanded={isDir ? open : undefined}
                  tabIndex={active || (!selected && depth === 0) ? 0 : -1}
                  onClick={() => (isDir ? toggleDir(node.path) : select(node.path))}
                  style={{ paddingLeft: 6 + depth * 14 }}
                  className={cn(
                    "hover:bg-accent focus-visible:ring-ring/50 flex h-7 w-full items-center gap-1.5 rounded-md pr-2 text-left text-[13px] outline-none transition-colors duration-(--motion-fast) ease-(--motion-ease) focus-visible:ring-2",
                    active && "bg-accent font-medium shadow-[inset_2px_0_0_var(--primary)]",
                  )}
                >
                  {isDir ? (
                    <ChevronRight
                      className={cn(
                        "text-muted-foreground size-3.5 shrink-0 transition-transform duration-(--motion-fast) ease-(--motion-ease) motion-reduce:transition-none",
                        open && "rotate-90",
                      )}
                    />
                  ) : (
                    <span className="size-3.5 shrink-0" />
                  )}
                  <Icon className="text-muted-foreground size-3.5 shrink-0" />
                  <span className="min-w-0 truncate">{node.name}</span>
                  {file?.kind === "script" && file.lang && (
                    <span className="font-mono text-[11px] text-sky-600 dark:text-sky-400">
                      {file.lang}
                    </span>
                  )}
                  {level && (
                    <span
                      role="img"
                      aria-label={level === "error" ? "有错误" : "有提示"}
                      className={cn(
                        "size-1.5 shrink-0 rounded-full",
                        level === "error" ? "bg-red-500" : "bg-amber-500",
                      )}
                    />
                  )}
                  {file && (
                    <span className="text-muted-foreground ml-auto text-xs tabular-nums">
                      {formatSize(file.size)}
                    </span>
                  )}
                </button>
              );
            })}
          </div>
        ) : (
          <ul className="grid min-h-0 flex-1 content-start gap-1.5 overflow-auto px-2.5 pt-1 pb-3">
            {sorted.map((issue, index) => {
              const { icon: LevelIcon, className: levelClass } = LEVEL_ICON[issue.level];
              const clickable = !!issue.path;
              const body = (
                <>
                  <LevelIcon className={cn("mt-0.5 size-3.5 shrink-0", levelClass)} />
                  <span className="min-w-0 flex-1">
                    <span className="flex flex-wrap items-center gap-1.5">
                      <Tag
                        tone={
                          issue.level === "error"
                            ? "danger"
                            : issue.level === "warn"
                              ? "warning"
                              : "info"
                        }
                      >
                        {ISSUE_LEVEL_LABEL[issue.level]}
                      </Tag>
                      <code className="text-muted-foreground text-[11px]">{issue.code}</code>
                    </span>
                    <span className="mt-1 block text-[13px] leading-snug break-words">
                      {issue.message}
                    </span>
                    {issue.path && (
                      <span className="text-muted-foreground mt-0.5 block font-mono text-xs break-all">
                        {issue.path}
                      </span>
                    )}
                  </span>
                </>
              );
              const cls =
                "bg-background flex w-full items-start gap-2 rounded-lg border px-2.5 py-2 text-left";
              return (
                <li key={`${issue.code}-${index}`}>
                  {clickable ? (
                    <button
                      type="button"
                      onClick={() => focusIssue(issue)}
                      className={cn(
                        cls,
                        "hover:bg-accent focus-visible:ring-ring/50 transition-colors duration-(--motion-fast) ease-(--motion-ease) outline-none focus-visible:ring-2",
                      )}
                    >
                      {body}
                    </button>
                  ) : (
                    <div className={cls}>{body}</div>
                  )}
                </li>
              );
            })}
          </ul>
        )}
      </div>

      <FilePreview
        file={selectedFile}
        readFile={cachedRead}
        issues={fileIssues}
        highlight={highlight}
        frontmatter={frontmatter}
        unsupported={unsupported}
      />
    </div>
  );
}
