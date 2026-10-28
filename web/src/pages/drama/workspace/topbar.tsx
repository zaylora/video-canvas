import { useEffect, useRef, useState } from "react";
import {
  Check,
  CloudAlert,
  CloudCheck,
  Loader2,
  PanelRight,
  Palette,
  Ratio,
  Type,
} from "lucide-react";
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

import { useWorkspace } from "./context";
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
      className="hover:bg-muted focus:bg-muted focus-visible:ring-ring/50 h-8 w-48 min-w-0 rounded-lg bg-transparent px-2 text-sm font-semibold outline-none focus-visible:ring-2 max-sm:w-32"
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

/** 文本模型：列后台「文本」类别的模型，第一个标默认；留空表示跟随默认 */
function ModelChip() {
  const { drama, patch } = useWorkspace();
  const remote = useRemoteModels("text");
  const first = remote.models[0];
  const selected = drama.textModel ? remote.models.find((m) => m.key === drama.textModel) : first;
  const label = selected?.label ?? (drama.textModel || first?.label || "文本模型");
  const stale = !!drama.textModel && remote.status === "ready" && !selected;

  return (
    <ChipPopover
      variant="bar"
      icon={<Type />}
      label={stale ? `${drama.textModel}（已停用）` : label}
      title="跟随后台「文本」类别"
      width="w-72"
    >
      {(close) => (
        <>
          <div className="text-muted-foreground px-1.5 pt-1 text-xs">
            文本模型 · 跟随后台「文本」类别
          </div>
          {remote.models.length === 0 && (
            <div className="text-muted-foreground px-2 py-3 text-sm">
              {remote.status === "loading" ? "加载中…" : "暂无可用的文本模型"}
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

/** 工作台顶栏：项目名（可改）、保存状态、画风、比例、文本模型、积分、右栏开关 */
export function Topbar({
  sideOpen,
  onToggleSide,
}: {
  sideOpen: boolean;
  onToggleSide: () => void;
}) {
  const { save } = useWorkspace();
  const status = SAVE_LABEL[save];
  return (
    <header className="border-border flex h-12 shrink-0 items-center gap-1 border-b px-3">
      <TitleInput />
      <span
        role="status"
        className={cn(
          "text-muted-foreground ml-1 inline-flex items-center gap-1 text-xs max-sm:hidden",
          status.tone,
        )}
      >
        {status.icon}
        {status.text}
      </span>
      <span className="flex-1" />
      <div className="flex items-center gap-0.5 max-md:hidden">
        <StyleChip />
        <RatioChip />
        <ModelChip />
      </div>
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
    </header>
  );
}
