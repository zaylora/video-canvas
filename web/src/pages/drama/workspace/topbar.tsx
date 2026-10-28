import { useEffect, useRef, useState } from "react";
import {
  ArrowLeft,
  Check,
  CloudAlert,
  CloudCheck,
  Loader2,
  PanelRight,
  Palette,
  Ratio,
  Search,
  Type,
} from "lucide-react";
import { Link } from "react-router";
import { toast } from "sonner";

import { ChipPopover, RatioList, StyleGrid } from "@/components/drama/pickers";
import { ThemeToggle } from "@/components/theme-toggle";
import { Button } from "@/components/ui/button";
import { useDramaStyles } from "@/hooks/use-drama-styles";
import { useRemoteModels } from "@/hooks/use-models";
import { cn } from "@/lib/utils";
import { CreditsPill } from "@/pages/canvas/chrome/top-right-bar";
import { ratioFixed } from "@/utils/drama/episodes";
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

/** 画风：浮层选择，改了只影响之后生成的内容；自定义描述防抖保存 */
function StyleChip() {
  const { drama, patch } = useWorkspace();
  const { styles } = useDramaStyles();
  const [custom, setCustom] = useState(drama.styleCustom);
  const timer = useRef<ReturnType<typeof setTimeout> | null>(null);
  useEffect(() => () => void (timer.current && clearTimeout(timer.current)), []);

  const name =
    drama.styleKey === CUSTOM_STYLE_KEY
      ? "自定义画风"
      : (styles.find((s) => s.key === drama.styleKey)?.name ?? "画风");
  const save = (styleKey: string, styleCustom: string) =>
    void patch({ styleKey, styleCustom }).catch(() => undefined);

  return (
    <ChipPopover variant="bar" icon={<Palette />} label={name} width="w-[26rem] max-w-[90vw]">
      {() => (
        <StyleGrid
          styles={styles}
          value={drama.styleKey}
          custom={custom}
          note="改画风只影响之后生成的内容"
          onPick={(key) => save(key, key === CUSTOM_STYLE_KEY ? custom : "")}
          onCustomChange={(text) => {
            setCustom(text);
            if (timer.current) clearTimeout(timer.current);
            timer.current = setTimeout(() => save(CUSTOM_STYLE_KEY, text), 600);
          }}
        />
      )}
    </ChipPopover>
  );
}

/** Agent 模型：列后台「Agent」类别的模型，第一个标默认；留空表示跟随默认 */
function ModelChip() {
  const { drama, patch } = useWorkspace();
  const remote = useRemoteModels("agent");
  const first = remote.models[0];
  const selected = drama.textModel ? remote.models.find((m) => m.key === drama.textModel) : first;
  const label = selected?.label ?? (drama.textModel || first?.label || "Agent 模型");
  const stale = !!drama.textModel && remote.status === "ready" && !selected;

  return (
    <ChipPopover
      variant="bar"
      icon={<Type />}
      label={stale ? `${drama.textModel}（已停用）` : label}
      title="跟随后台「Agent」类别"
      width="w-72"
    >
      {(close) => (
        <>
          <div className="text-muted-foreground px-1.5 pt-1 text-xs">
            Agent 模型 · 跟随后台「Agent」类别
          </div>
          {remote.models.length === 0 && (
            <div className="text-muted-foreground px-2 py-3 text-sm">
              {remote.status === "loading" ? "加载中…" : "暂无可用的 Agent 模型"}
            </div>
          )}
          {remote.models.map((model, index) => {
            const active = selected?.key === model.key;
            return (
              <button
                key={model.key}
                type="button"
                onClick={() => {
                  close();
                  // 选默认模型等于「跟随后台」：存空串，后台调整默认后自动跟着变
                  const next = index === 0 ? "" : model.key;
                  if (next === drama.textModel) return;
                  void patch({ textModel: next })
                    .then(() => toast.info(`之后的流水线任务改用 ${model.label}`))
                    .catch(() => undefined);
                }}
                className={cn(
                  "hover:bg-muted focus-visible:ring-ring/50 flex min-h-8 items-center gap-2 rounded-md px-2 text-left text-sm outline-none focus-visible:ring-2",
                  active && "bg-muted font-medium",
                )}
              >
                <Type className="size-4 shrink-0" />
                <span className="min-w-0 flex-1 truncate">{model.label}</span>
                {index === 0 && (
                  <span className="bg-muted text-muted-foreground rounded-full px-1.5 text-[10px] leading-4">
                    默认
                  </span>
                )}
                {active && <Check className="size-4 shrink-0" />}
              </button>
            );
          })}
        </>
      )}
    </ChipPopover>
  );
}

/** 比例：已有锁定集（资产已按当前比例生成）时固定不可改 */
function RatioChip() {
  const { drama, patch } = useWorkspace();
  const fixed = ratioFixed(drama.episodes);
  return (
    <ChipPopover variant="bar" icon={<Ratio />} label={drama.ratio || "比例未定"}>
      {(close) => (
        <RatioList
          value={drama.ratio}
          fixed={fixed}
          onPick={(ratio) => {
            close();
            if (ratio !== drama.ratio) void patch({ ratio }).catch(() => undefined);
          }}
        />
      )}
    </ChipPopover>
  );
}

/**
 * 工作台顶栏：左边返回列表 + 项目名（可改）+ 保存状态，中间是工序标签，
 * 右边是画风 / 比例 / Agent 模型、明暗、积分、右栏开关。
 * 三列：右列至少是自己的内容宽度（minmax(max-content,1fr)），宽屏时和左列等宽、标签居中；
 * 窗口变窄时由左列（项目名）让位。左右强制等宽的话，右列装不下就会向左溢出、压在标签上。
 */
export function Topbar({
  sideOpen,
  onToggleSide,
  stage,
  hints,
  onStage,
  onPalette,
}: {
  sideOpen: boolean;
  onToggleSide: () => void;
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
        <div className="flex items-center gap-0.5 max-xl:hidden">
          <StyleChip />
          <RatioChip />
          <ModelChip />
        </div>
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
        <Button
          variant="ghost"
          size="icon-sm"
          aria-label={sideOpen ? "收起右栏" : "展开右栏"}
          aria-pressed={sideOpen}
          onClick={onToggleSide}
        >
          <PanelRight />
        </Button>
      </div>
    </header>
  );
}
