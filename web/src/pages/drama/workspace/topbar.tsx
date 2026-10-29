import { useEffect, useRef, useState } from "react";
import { ArrowLeft, CloudAlert, CloudCheck, Loader2, Search } from "lucide-react";
import { Link } from "react-router";

import { ChromePill, ChromeSeparator } from "@/components/canvas/chrome/chrome";
import { useAgentModel } from "@/components/drama/agent-model-picker";
import { ModelLogo } from "@/components/model-logo";
import { ThemeToggle } from "@/components/theme-toggle";
import { Button } from "@/components/ui/button";
import { useDramaStyles } from "@/hooks/use-drama-styles";
import { cn } from "@/lib/utils";
import { CreditsPill } from "@/pages/canvas/chrome/top-right-bar";
import { CUSTOM_STYLE_KEY } from "@/utils/drama/options";
import type { StageHints, WorkspaceStage } from "@/utils/drama/stages";

import { useWorkspace } from "./context";
import { StageTabs } from "./stage-tabs";
import type { SaveSummary } from "./use-workspace";

const SAVE_LABEL: Record<SaveSummary, { text: string; icon: React.ReactNode; tone?: string }> = {
  saved: { text: "已保存", icon: <CloudCheck className="size-3.5" /> },
  saving: { text: "保存中", icon: <Loader2 className="size-3.5 animate-spin" /> },
  error: {
    text: "保存失败，将重试",
    icon: <CloudAlert className="size-3.5" />,
    tone: "text-destructive",
  },
  conflict: {
    text: "有冲突，已停止保存",
    icon: <CloudAlert className="size-3.5" />,
    tone: "text-status-warning",
  },
};

/** 项目名：失焦或回车保存，空名恢复原名 */
function TitleInput() {
  const { drama, patch, reportSave } = useWorkspace();
  const [value, setValue] = useState(drama.title);
  const lastRemote = useRef(drama.title);
  // 服务端名字变了（别处改的）且自己没在编辑：跟着换
  useEffect(() => {
    if (drama.title !== lastRemote.current) {
      lastRemote.current = drama.title;
      setValue(drama.title);
    }
  }, [drama.title]);

  const commit = () => {
    const next = value.trim();
    if (!next) {
      setValue(drama.title);
      return;
    }
    if (next === drama.title) return;
    reportSave("title", "saving");
    patch({ title: next })
      .then(() => reportSave("title", "saved"))
      .catch(() => {
        setValue(drama.title);
        reportSave("title", "saved");
      });
  };

  return (
    <input
      value={value}
      maxLength={64}
      aria-label="项目名"
      onChange={(event) => setValue(event.target.value)}
      onBlur={commit}
      onKeyDown={(event) => {
        if (event.key === "Enter") event.currentTarget.blur();
        if (event.key === "Escape") {
          setValue(drama.title);
          event.currentTarget.blur();
        }
      }}
      className="hover:bg-muted focus:bg-muted focus-visible:ring-ring/50 font-display h-8 w-44 min-w-0 rounded-lg bg-transparent px-2 text-sm font-bold tracking-wide outline-none focus-visible:ring-2 max-sm:w-28"
    />
  );
}

/**
 * 这部短剧的画风 / 比例 / Agent 模型：在首页新建时选定，进了工作台只显示、不能改
 * （改画风、比例会让前后生成的资产不一致）。
 */
function ProjectSettings() {
  const { drama } = useWorkspace();
  const { styles } = useDramaStyles();
  const agent = useAgentModel(drama.textModel);
  const style =
    drama.styleKey === CUSTOM_STYLE_KEY
      ? "自定义画风"
      : (styles.find((item) => item.key === drama.styleKey)?.name ?? "画风");
  const item = "flex h-8 items-center gap-1.5 px-3 text-[13px] whitespace-nowrap";
  return (
    <ChromePill className="mr-1 max-xl:hidden" aria-label="创作设置">
      <span
        className={item}
        title={drama.styleKey === CUSTOM_STYLE_KEY ? drama.styleCustom : undefined}
      >
        {style}
      </span>
      <ChromeSeparator />
      <span className={cn(item, "tabular-nums")}>{drama.ratio || "比例未定"}</span>
      <ChromeSeparator />
      <span className={item}>
        {agent.model && !agent.stale && (
          <ModelLogo
            vendor={agent.model.vendor}
            name={agent.label}
            className="size-4 rounded-[5px]"
          />
        )}
        <span className="max-w-40 truncate">{agent.label}</span>
      </span>
    </ChromePill>
  );
}

/**
 * 工作台顶栏：左边返回列表 + 项目名（可改）+ 保存状态，中间是工序标签，
 * 右边是画风 / 比例 / Agent 模型（只读）、明暗、积分。
 * 三列：右列至少是自己的内容宽度（minmax(max-content,1fr)），宽屏时和左列等宽、标签居中；
 * 窗口变窄时由左列（项目名）让位。左右强制等宽的话，右列装不下就会向左溢出、压在标签上。
 */
export function Topbar({
  stage,
  hints,
  onStage,
  onPalette,
}: {
  stage: WorkspaceStage;
  hints: StageHints;
  onStage: (stage: WorkspaceStage) => void;
  onPalette: () => void;
}) {
  const { save } = useWorkspace();
  const status = SAVE_LABEL[save];
  return (
    <header className="border-border bg-chrome relative z-30 grid h-14 shrink-0 grid-cols-[minmax(0,1fr)_auto_minmax(max-content,1fr)] items-center gap-3 border-b px-3 backdrop-blur-xl">
      <div className="flex min-w-0 items-center gap-1">
        <Button
          variant="ghost"
          size="icon-sm"
          aria-label="返回短剧列表"
          render={<Link to="/drama" />}
        >
          <ArrowLeft />
        </Button>
        <TitleInput />
        <span
          role="status"
          className={cn(
            "text-muted-foreground ml-1 inline-flex items-center gap-1 text-xs max-lg:hidden",
            status.tone,
          )}
        >
          {status.icon}
          {status.text}
        </span>
      </div>
      <StageTabs stage={stage} hints={hints} onChange={onStage} />
      <div className="flex items-center justify-end gap-0.5">
        {/* 画风 / 比例 / 模型：首页新建时选定，这里只显示 */}
        <ProjectSettings />
        <Button
          variant="ghost"
          size="sm"
          aria-label="命令面板"
          title="命令面板（⌘K）"
          onClick={onPalette}
          className="text-muted-foreground max-2xl:hidden"
        >
          <Search />
          <kbd className="bg-muted rounded px-1 font-mono text-[11px]">⌘K</kbd>
        </Button>
        <ThemeToggle />
        <CreditsPill />
      </div>
    </header>
  );
}
