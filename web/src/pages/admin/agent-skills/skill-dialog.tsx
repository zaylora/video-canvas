import { Download, Lightbulb } from "lucide-react";
import { MotionConfig, motion } from "motion/react";
import { useCallback, useEffect, useMemo, useRef, useState, type KeyboardEvent } from "react";
import { toast } from "sonner";

import {
  deleteSkill,
  deleteSkillVersion,
  downloadSkillVersion,
  getSkill,
  getSkillDeleteCheck,
  getSkillFile,
  getSkillVersion,
} from "@/api/admin/agent-skill";
import type {
  SkillDetail,
  SkillFile,
  SkillFileContent,
  SkillItem,
  SkillVersionHead,
  SkillVersionView,
} from "@/api/admin/agent-skill/type.d";
import { confirm } from "@/components/admin-ui/confirm-dialog";
import { EmptyState, EmptyStateTitle } from "@/components/admin-ui/empty-state";
import { Segmented, SegmentedItem } from "@/components/admin-ui/segmented";
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogHeader,
  DialogTitle,
} from "@/components/ui/dialog";
import { StatusLabel } from "@/components/admin-ui/status-dot";
import { Tag } from "@/components/admin-ui/tag";
import { Button } from "@/components/ui/button";
import { Skeleton } from "@/components/ui/skeleton";
import { useRetained } from "@/hooks/use-retained";
import { DURATION, EASE_OUT, SPRING } from "@/lib/motion";
import { cn } from "@/lib/utils";

import { FileWorkbench } from "./file-workbench";
import { SkillOverview } from "./skill-overview";
import type { AgentSkillsApi } from "./use-agent-skills";
import { VersionTimeline } from "./version-timeline";

/** 详情弹窗的页签 */
type DialogTab = "overview" | "files" | "versions";

const TABS: { id: DialogTab; label: string }[] = [
  { id: "overview", label: "概览" },
  { id: "files", label: "文件" },
  { id: "versions", label: "版本" },
];

/** 页签内容切换：opacity + translateY 4→0，fast 档；减少动态效果时由 MotionConfig 去掉位移 */
const TAB_MOTION = {
  initial: { opacity: 0, y: 4 },
  animate: { opacity: 1, y: 0, transition: { duration: DURATION.fast, ease: EASE_OUT } },
};

/** 把 blob 存成文件：下载接口要带登录凭证，不能直接用链接 */
function saveBlob(blob: Blob, filename: string) {
  const url = URL.createObjectURL(blob);
  const link = document.createElement("a");
  link.href = url;
  link.download = filename;
  document.body.appendChild(link);
  link.click();
  link.remove();
  URL.revokeObjectURL(url);
}

/** 内置技能没有包：用详情里的正文拼成只有一个 SKILL.md 的清单，复用文件工作台 */
function builtinFiles(body: string): SkillFile[] {
  return [
    { path: "SKILL.md", size: new TextEncoder().encode(body).length, kind: "skill", text: true },
  ];
}

/**
 * 详情弹窗（居中，宽 min(64rem, 100%)）：概览 / 文件 / 版本三个页签，下划线滑块用 layoutId + SPRING。
 * 技能本身（开关、显示名、版本号）以列表里的最新视图为准；版本列表和各版本的文件按需请求并缓存。
 * @param name 打开的技能名；为空表示关闭
 * @param api 列表与启停（use-agent-skills）
 * @param onClose 请求关闭
 */
export function SkillDialog({
  name,
  api,
  onClose,
}: {
  name: string | null;
  api: AgentSkillsApi;
  onClose: () => void;
}) {
  const shown = useRetained(name);
  const item = shown ? api.items.find((s) => s.name === shown) : undefined;
  return (
    <Dialog open={!!name} onOpenChange={(next) => !next && onClose()}>
      <DialogContent className="flex h-[min(90svh,880px)] max-w-[calc(100%-1.5rem)] flex-col gap-0 overflow-hidden p-0 sm:max-w-5xl">
        {item && <DialogBody key={item.name} item={item} api={api} onClose={onClose} />}
      </DialogContent>
    </Dialog>
  );
}

function DialogBody({
  item,
  api,
  onClose,
}: {
  item: SkillItem;
  api: AgentSkillsApi;
  onClose: () => void;
}) {
  const { name } = item;
  const [tab, setTab] = useState<DialogTab>("overview");
  const [detail, setDetail] = useState<SkillDetail | null>(null);
  const [detailFailed, setDetailFailed] = useState(false);
  const [busyVersion, setBusyVersion] = useState<number | null>(null);
  /** “文件”页签查看的版本；null 表示跟随生效版本 */
  const [viewVersion, setViewVersion] = useState<number | null>(null);

  const versionCache = useRef(new Map<number, Promise<SkillVersionView>>());
  const loadVersion = useCallback(
    (version: number) => {
      let pending = versionCache.current.get(version);
      if (!pending) {
        pending = getSkillVersion(name, version);
        versionCache.current.set(version, pending);
        pending.catch(() => versionCache.current.delete(version));
      }
      return pending;
    },
    [name],
  );

  const reloadDetail = useCallback(async () => {
    try {
      const next = await getSkill(name);
      setDetail(next);
      setDetailFailed(false);
    } catch {
      setDetailFailed(true);
    }
  }, [name]);

  useEffect(() => {
    void reloadDetail();
  }, [reloadDetail]);

  const versions = detail?.versions ?? [];
  const activeVersion = item.active_version;
  const filesVersion = viewVersion ?? activeVersion ?? versions[0]?.version ?? null;

  /** 概览要的头部字段来自生效版本的完整信息 */
  const [activeView, setActiveView] = useState<SkillVersionView | null>(null);
  useEffect(() => {
    if (item.readonly || activeVersion == null) return;
    let stale = false;
    loadVersion(activeVersion).then(
      (view) => !stale && setActiveView(view),
      () => {},
    );
    return () => {
      stale = true;
    };
  }, [item.readonly, activeVersion, loadVersion]);

  /** “文件”页签当前版本的完整信息（清单、问题、头部字段） */
  const [filesView, setFilesView] = useState<SkillVersionView | null>(null);
  const [filesFailed, setFilesFailed] = useState(false);
  useEffect(() => {
    if (tab !== "files" || item.readonly || filesVersion == null) return;
    let stale = false;
    setFilesFailed(false);
    loadVersion(filesVersion).then(
      (view) => !stale && setFilesView(view),
      () => !stale && setFilesFailed(true),
    );
    return () => {
      stale = true;
    };
  }, [tab, item.readonly, filesVersion, loadVersion]);

  const readVersionFile = useCallback(
    (path: string): Promise<SkillFileContent> =>
      filesVersion == null
        ? Promise.reject(new Error("没有版本"))
        : getSkillFile(name, filesVersion, path),
    [name, filesVersion],
  );
  const readBuiltinFile = useCallback(
    async (path: string): Promise<SkillFileContent> => {
      const body = detail?.body ?? "";
      return { path, size: new TextEncoder().encode(body).length, binary: false, text: body };
    },
    [detail?.body],
  );
  const builtinList = useMemo(() => builtinFiles(detail?.body ?? ""), [detail?.body]);

  const download = async (version: number) => {
    try {
      saveBlob(await downloadSkillVersion(name, version), `${name}-v${version}.zip`);
    } catch {
      // 下载失败的全局 toast 已弹
    }
  };

  const activate = async (head: SkillVersionHead) => {
    setBusyVersion(head.version);
    try {
      if (await api.activateVersion(item, head.version)) await reloadDetail();
    } finally {
      setBusyVersion(null);
    }
  };

  const removeVersion = (head: SkillVersionHead) => {
    void confirm({
      title: `删除 v${head.version}？`,
      description: "这个版本的整包会从存储里删除，不能撤销。已经产生的会话不受影响。",
      confirmLabel: "删除版本",
      destructive: true,
      onConfirm: async () => {
        setBusyVersion(head.version);
        try {
          await deleteSkillVersion(name, head.version);
        } finally {
          setBusyVersion(null);
        }
        versionCache.current.delete(head.version);
        if (viewVersion === head.version) setViewVersion(null);
        toast.success(`已删除 v${head.version}`);
        await Promise.all([reloadDetail(), api.reload()]);
      },
    });
  };

  /** 删除技能：先做删除预检，不能删就把原因留在确认框里 */
  const removeSkill = async () => {
    let check;
    try {
      check = await getSkillDeleteCheck(name);
    } catch {
      return;
    }
    void confirm({
      title: `删除技能「${item.title}」？`,
      description: `共 ${check.version_count} 个版本，将同时删除对象存储里的整包，不能撤销。历史会话不受影响。`,
      confirmLabel: "删除技能",
      destructive: true,
      blockReason: check.can_delete ? null : (check.reason ?? "暂时不能删除"),
      onConfirm: async () => {
        await deleteSkill(name);
        api.removeOne(name);
        onClose();
        toast.success(`已删除「${item.title}」`);
      },
    });
  };

  const onTabKey = (event: KeyboardEvent<HTMLDivElement>) => {
    if (event.key !== "ArrowLeft" && event.key !== "ArrowRight") return;
    event.preventDefault();
    const index = TABS.findIndex((t) => t.id === tab);
    const next = TABS[(index + (event.key === "ArrowRight" ? 1 : TABS.length - 1)) % TABS.length];
    setTab(next.id);
    event.currentTarget.querySelector<HTMLElement>(`[data-tab="${next.id}"]`)?.focus();
  };

  const loadingDetail = !detail && !detailFailed;

  return (
    <MotionConfig reducedMotion="user">
      <DialogHeader className="gap-0 p-0 pr-12">
        <div className="flex items-center gap-3 px-5 pt-4 pb-3">
          <span className="bg-muted text-muted-foreground grid size-9 shrink-0 place-items-center rounded-lg border">
            <Lightbulb className="size-[18px]" />
          </span>
          <div className="min-w-0 flex-1">
            <DialogTitle className="truncate text-lg font-semibold">{item.title}</DialogTitle>
            <DialogDescription className="truncate font-mono text-xs">
              {item.name}
            </DialogDescription>
          </div>
          {item.readonly ? (
            <Tag>内置</Tag>
          ) : (
            <StatusLabel tone={item.enabled ? "success" : "neutral"}>
              {item.enabled ? "已启用" : "已停用"}
            </StatusLabel>
          )}
        </div>
        <div
          role="tablist"
          aria-label="技能详情"
          onKeyDown={onTabKey}
          className="relative grid grid-flow-col auto-cols-[84px] border-b px-5 max-md:auto-cols-fr"
        >
          {TABS.map((t) => {
            const selected = tab === t.id;
            return (
              <button
                key={t.id}
                type="button"
                role="tab"
                data-tab={t.id}
                aria-selected={selected}
                tabIndex={selected ? 0 : -1}
                onClick={() => setTab(t.id)}
                className={cn(
                  "text-muted-foreground hover:text-foreground focus-visible:ring-ring/50 relative h-9 text-sm outline-none focus-visible:ring-2 focus-visible:ring-inset",
                  selected && "text-foreground font-medium",
                )}
              >
                {t.label}
                {selected && (
                  <motion.span
                    layoutId="skill-dialog-tab"
                    transition={SPRING}
                    className="bg-primary absolute inset-x-0 -bottom-px h-0.5"
                  />
                )}
              </button>
            );
          })}
        </div>
      </DialogHeader>

      <motion.div
        key={tab}
        {...TAB_MOTION}
        role="tabpanel"
        className={cn("flex min-h-0 flex-1 flex-col", tab !== "files" && "overflow-auto p-5")}
      >
        {tab === "overview" && (
          <SkillOverview
            item={item}
            version={activeView}
            busy={api.busyNames.has(name)}
            onToggle={(enabled) => void api.toggleEnabled(item, enabled)}
            onGoVersions={() => setTab("versions")}
            onRenamed={api.replaceOne}
            onDelete={() => void removeSkill()}
          />
        )}

        {tab === "versions" &&
          (item.readonly ? (
            <EmptyState>
              <EmptyStateTitle>内置技能没有版本历史</EmptyStateTitle>
            </EmptyState>
          ) : loadingDetail ? (
            <div className="grid gap-3" aria-busy="true">
              <Skeleton className="h-24 rounded-xl" />
              <Skeleton className="h-24 rounded-xl" />
            </div>
          ) : detailFailed ? (
            <EmptyState>
              <EmptyStateTitle className="text-destructive">版本列表加载失败</EmptyStateTitle>
              <Button variant="outline" size="sm" onClick={() => void reloadDetail()}>
                重试
              </Button>
            </EmptyState>
          ) : (
            <VersionTimeline
              versions={versions}
              activeVersion={activeVersion}
              busyVersion={busyVersion}
              actions={{
                onActivate: (head) => void activate(head),
                onViewFiles: (head) => {
                  setViewVersion(head.version);
                  setTab("files");
                },
                onDownload: (head) => void download(head.version),
                onDelete: removeVersion,
              }}
            />
          ))}

        {tab === "files" && (
          <>
            {!item.readonly && (
              <div className="flex shrink-0 flex-wrap items-center gap-2 border-b px-5 py-2.5">
                <span className="text-muted-foreground text-xs">查看版本</span>
                <Segmented aria-label="查看版本">
                  {versions.map((head) => (
                    <SegmentedItem
                      key={head.version}
                      slideId="files-version"
                      active={head.version === filesVersion}
                      className="px-2.5 py-1 font-mono tabular-nums"
                      onClick={() => setViewVersion(head.version)}
                    >
                      v{head.version}
                    </SegmentedItem>
                  ))}
                </Segmented>
                <Button
                  variant="outline"
                  size="sm"
                  className="ml-auto"
                  disabled={filesVersion == null}
                  onClick={() => filesVersion != null && void download(filesVersion)}
                >
                  <Download />
                  下载整包
                </Button>
              </div>
            )}
            {item.readonly ? (
              detail ? (
                <FileWorkbench
                  key="builtin"
                  files={builtinList}
                  issues={[]}
                  readFile={readBuiltinFile}
                  frontmatter={{ name: item.name, description: item.description }}
                  unsupported={[]}
                />
              ) : (
                <div className="p-5" aria-busy="true">
                  <Skeleton className="h-40 rounded-xl" />
                </div>
              )
            ) : filesFailed ? (
              <EmptyState className="m-5">
                <EmptyStateTitle className="text-destructive">文件清单加载失败</EmptyStateTitle>
              </EmptyState>
            ) : filesView && filesView.version === filesVersion ? (
              <FileWorkbench
                key={`${name}@${filesVersion}`}
                files={filesView.files}
                issues={filesView.issues}
                readFile={readVersionFile}
                frontmatter={filesView.frontmatter}
                unsupported={filesView.unsupported_fields}
              />
            ) : (
              <div className="p-5" aria-busy="true">
                <Skeleton className="h-40 rounded-xl" />
              </div>
            )}
          </>
        )}
      </motion.div>
    </MotionConfig>
  );
}
