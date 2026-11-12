import { useEffect, useRef, useState } from "react";
import {
  BookOpen,
  Check,
  CircleX,
  Clapperboard,
  CodeXml,
  Columns2,
  Copy,
  Copyright,
  Download,
  FileText,
  Flame,
  Loader2,
  Megaphone,
  Mic,
  MoreHorizontal,
  Music,
  Play,
  RefreshCw,
  RotateCcw,
  ScanSearch,
  ShieldBan,
  Undo2,
  Workflow,
  type LucideIcon,
} from "lucide-react";
import { toast } from "sonner";

import { getMarketingPrompt } from "@/api/marketing";
import type { FlagType, ProductRole, Project, Round, Script } from "@/api/marketing/type";
import { StatusPill } from "@/components/drama/status-pill";
import { EditableText } from "@/components/marketing/editable-text";
import { Button } from "@/components/ui/button";
import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuItem,
  DropdownMenuTrigger,
} from "@/components/ui/dropdown-menu";
import { cn } from "@/lib/utils";
import { useWorkbenchStore, type WorkbenchTab } from "@/store/marketing-workbench";
import { useMarketingOptions } from "@/hooks/use-marketing-options";
import { angleName, PRODUCT_ROLE_NAME } from "@/utils/marketing/options";
import {
  patchReverseShot,
  refollowState,
  reverseEditable,
  validateAction,
} from "@/utils/marketing/reverse";
import {
  FLAG_NAME,
  flagSummary,
  isScriptBusy,
  methodLabel,
  narrationsChanged,
  playableTakes,
  scriptRenderState,
  scriptStatusLabel,
} from "@/utils/marketing/script";
import { stepRunOf } from "@/utils/marketing/status";
import { compareState, formatSeconds, refTimeAt, shotIndexAt } from "@/utils/marketing/workbench";

import { useCurrentShot, useRefollowCost } from "./hooks";
import { CanvasDialog, downloadUrl, takeFileName } from "./script-dialogs";
import type { MarketingActions } from "./use-actions";

const FLAG_ICON: Record<FlagType, LucideIcon> = {
  forbidden: ShieldBan,
  claim: Megaphone,
  brand: Copy,
};

const PANEL_CLASS =
  "bg-chrome ring-chrome-border flex min-h-0 flex-col overflow-hidden rounded-[28px] ring-1 backdrop-blur-xl";

/** 小标题：11px 灰 */
function Label({ children, className }: { children: React.ReactNode; className?: string }) {
  return (
    <div className={cn("text-muted-foreground text-[11px] tracking-[0.02em]", className)}>
      {children}
    </div>
  );
}

/**
 * 可点击的镜头块：点空白处跳到这一镜（点在输入框、下拉、按钮里不跳）；当前镜头左侧紫色竖条 + 浅底，
 * 播放时自动滚入视野，用户刚手动滚过（3 秒内）不抢滚动。
 */
function ShotBlock({
  index,
  current,
  onJump,
  scrolledAt,
  className,
  children,
}: {
  index: number;
  current: boolean;
  onJump: (() => void) | null;
  scrolledAt: React.RefObject<number>;
  className?: string;
  children: React.ReactNode;
}) {
  const ref = useRef<HTMLDivElement>(null);
  useEffect(() => {
    if (current && Date.now() - scrolledAt.current > 3000) {
      ref.current?.scrollIntoView?.({ block: "nearest", behavior: "smooth" });
    }
  }, [current, scrolledAt]);
  return (
    <div
      ref={ref}
      data-shot={index}
      data-current={current || undefined}
      onClick={(event) => {
        if (!onJump) return;
        if ((event.target as HTMLElement).closest("input, textarea, select, button, a")) return;
        onJump();
      }}
      className={cn(
        "relative rounded-[18px] px-4 py-3 transition-colors duration-150",
        onJump && "hover:bg-chrome-hover cursor-pointer",
        current &&
          "bg-beam/9 before:bg-beam before:absolute before:inset-y-3 before:left-1 before:w-0.75 before:rounded-full",
        className,
      )}
    >
      {children}
    </div>
  );
}

/** 检查器顶部的标签页条 */
function Tabs({
  items,
}: {
  items: { value: WorkbenchTab | "reverse"; label: string; icon: LucideIcon; disabled?: string }[];
}) {
  const tab = useWorkbenchStore((state) => state.tab);
  return (
    <div role="tablist" className="border-chrome-border flex gap-1 border-b px-3 pt-2">
      {items.map((item) => {
        const selected = items.length === 1 || item.value === tab;
        return (
          <button
            key={item.value}
            type="button"
            role="tab"
            aria-selected={selected}
            disabled={!!item.disabled}
            title={item.disabled}
            onClick={() =>
              item.value !== "reverse" && useWorkbenchStore.getState().setTab(item.value)
            }
            className={cn(
              "-mb-px inline-flex h-9 items-center gap-1.5 border-b-2 border-transparent px-2.5 text-[12.5px] transition-colors outline-none disabled:opacity-40",
              selected
                ? "border-beam text-foreground"
                : "text-muted-foreground hover:text-foreground",
            )}
          >
            <item.icon className="size-3.5" />
            {item.label}
          </button>
        );
      })}
    </div>
  );
}

/** 用户手动滚动检查器的时间：播放时的自动滚入据此让路 */
function useScrolledAt() {
  const at = useRef(0);
  return { at, onWheel: () => (at.current = Date.now()) };
}

/** 选中原片时的检查器：参考拆解全部字段（可改），底部恢复原稿 / 按拆解重写跟拍版 */
export function ReverseInspector({
  project,
  round,
  available,
  actions,
}: {
  project: Project;
  round: Round;
  available: number | null;
  actions: MarketingActions;
}) {
  const step = stepRunOf(round.steps, "reverse");
  const reverse = round.reverse;
  const cost = useRefollowCost(project, round);
  const controls = useWorkbenchStore((state) => state.controls);
  const current = useCurrentShot(reverse?.shots ?? []);
  const scrolled = useScrolledAt();
  const editable = reverseEditable(round);
  const saving = actions.busy[`reverse:${round.id}`];
  const follow = round.scripts.find((item) => item.kind === "follow");
  const refollow = refollowState(round, null, available, cost);
  const refollowing = actions.busy[`refollow:${round.id}`];

  const commit = (
    index: number,
    patch: Parameters<typeof patchReverseShot>[2],
    field?: "action",
  ) => {
    if (!reverse) return;
    if (field === "action") {
      const problem = validateAction(patch.action ?? "");
      if (problem) {
        toast.error(problem);
        return;
      }
    }
    void actions.saveReverse(round, patchReverseShot(reverse, index, patch));
  };

  let body: React.ReactNode;
  if (step?.status === "failed") {
    body = (
      <div className="grid justify-items-start gap-3 p-4 text-[13px]">
        <span className="flex items-start gap-2">
          <CircleX className="text-destructive mt-0.5 size-4 shrink-0" />
          参考片拆解失败：{step.error || "原因未知"}
        </span>
        <Button
          variant="outline"
          size="sm"
          disabled={actions.busy[`step:${step.id}`]}
          onClick={() => void actions.retryStep(step.id)}
        >
          <RotateCcw />
          重试拆解
        </Button>
      </div>
    );
  } else if (!reverse) {
    body = (
      <div className="text-muted-foreground flex items-center gap-2 p-4 text-[13px]">
        <Loader2 className="size-4 animate-spin" />
        正在拆解参考片
      </div>
    );
  } else {
    body = (
      <>
        <div className="grid gap-2 px-4 pt-4 pb-1">
          {(reverse.brands.length > 0 || reverse.claims.length > 0) && (
            <div className="flex flex-wrap gap-1.5">
              {reverse.brands.length > 0 && (
                <StatusPill tone="destructive">
                  <span
                    title={reverse.brands.join("、")}
                    className="inline-flex items-center gap-1"
                  >
                    <Copyright />
                    第三方品牌 {reverse.brands.length}
                  </span>
                </StatusPill>
              )}
              {reverse.claims.length > 0 && (
                <StatusPill tone="warning">
                  <span
                    title={reverse.claims.join("、")}
                    className="inline-flex items-center gap-1"
                  >
                    <Megaphone />
                    原片背书 {reverse.claims.length}
                  </span>
                </StatusPill>
              )}
            </div>
          )}
          {reverse.overall.whyViral && (
            <p className="flex items-start gap-1.5 text-[13.5px] leading-relaxed">
              <Flame className="text-status-warning mt-1 size-3.5 shrink-0" />
              {reverse.overall.whyViral}
            </p>
          )}
          {(reverse.overall.style || reverse.overall.narrativeArc) && (
            <p className="text-muted-foreground text-[12.5px] leading-relaxed">
              {[reverse.overall.style, reverse.overall.narrativeArc].filter(Boolean).join(" · ")}
            </p>
          )}
        </div>
        <div className="grid gap-0.5 px-1.5 pb-3">
          {reverse.shots.map((shot, index) => (
            <ShotBlock
              key={shot.id}
              index={index}
              current={index === current}
              scrolledAt={scrolled.at}
              onJump={controls ? () => controls.seek(shot.start) : null}
            >
              <div className="text-muted-foreground flex flex-wrap items-center gap-1.5 text-xs">
                <span className="text-foreground/85 font-mono tabular-nums">
                  {shot.start}–{shot.end}s
                </span>
                <span>
                  {shot.shotSize} · {shot.camera}
                </span>
                {shot.isOpening3s && <StatusPill tone="warning">前 3 秒</StatusPill>}
              </div>
              <Label className="mt-2 px-2">主体 + 动作</Label>
              <EditableText
                multiline
                value={shot.action}
                ariaLabel={`镜头 ${shot.id} 主体和动作`}
                disabled={!editable || saving}
                onCommit={(value) => commit(index, { action: value }, "action")}
                className="text-[13.5px] font-medium"
              />
              <Label className="mt-1.5 px-2">台词</Label>
              <EditableText
                value={shot.dialogue}
                ariaLabel={`镜头 ${shot.id} 台词`}
                disabled={!editable || saving}
                onCommit={(value) => commit(index, { dialogue: value })}
              />
              {shot.keyColors.length > 0 && (
                <div className="flex flex-wrap gap-1 px-2 pt-1.5">
                  {shot.keyColors.map((color) => (
                    <span
                      key={color}
                      className="bg-chrome-hover ring-chrome-border rounded-full px-2 text-[11.5px] leading-5 ring-1"
                    >
                      {color}
                    </span>
                  ))}
                </div>
              )}
              <Label className="mt-2.5 px-2">商品怎么出现</Label>
              <div className="flex flex-wrap items-center gap-2 px-2 pt-1">
                <select
                  value={shot.productRole}
                  disabled={!editable || saving}
                  aria-label={`镜头 ${shot.id} 商品怎么出现`}
                  onChange={(event) =>
                    commit(index, { productRole: event.target.value as ProductRole })
                  }
                  className="bg-chrome-hover ring-chrome-border focus-visible:ring-ring/50 h-7 rounded-full px-3 text-xs ring-1 outline-none focus-visible:ring-2 disabled:opacity-60"
                >
                  {(Object.keys(PRODUCT_ROLE_NAME) as ProductRole[]).map((role) => (
                    <option key={role} value={role}>
                      {PRODUCT_ROLE_NAME[role]}
                    </option>
                  ))}
                </select>
                {shot.productInFrame && (
                  <span className="text-muted-foreground min-w-36 flex-1 text-xs leading-snug">
                    {shot.productInFrame}
                  </span>
                )}
              </div>
            </ShotBlock>
          ))}
        </div>
      </>
    );
  }

  return (
    <aside data-slot="marketing-inspector" data-kind="reverse" className={PANEL_CLASS}>
      <div className="px-4 pt-4">
        <div className="flex flex-wrap items-center gap-1.5">
          <StatusPill tone="muted">
            <Clapperboard />
            原片
          </StatusPill>
          {(round.reverseEdited || round.reverseDirty) && (
            <StatusPill tone="warning">已修改</StatusPill>
          )}
        </div>
        <div className="mt-1.5 text-base font-bold">参考拆解</div>
        <div className="text-muted-foreground mt-0.5 flex flex-wrap items-center gap-1.5 text-xs">
          {reverse && <span className="tabular-nums">{reverse.shots.length} 个镜头</span>}
          {round.refVideo && (
            <span className="tabular-nums">· {formatSeconds(round.refVideo.duration)}</span>
          )}
          {step?.method && (
            <span className="inline-flex items-center gap-1">
              · <BookOpen className="size-3.5" />
              方法：{methodLabel(step.method)}
            </span>
          )}
        </div>
      </div>
      <Tabs items={[{ value: "reverse", label: "拆解", icon: ScanSearch }]} />
      <div
        className="min-h-0 flex-1 overflow-x-hidden overflow-y-auto [scrollbar-gutter:stable]"
        onWheel={scrolled.onWheel}
      >
        {body}
      </div>
      {reverse && (
        <div className="border-chrome-border flex flex-wrap items-center gap-1.5 border-t px-3 py-2.5">
          {reverse.claims.length > 0 && (
            <span
              title={reverse.claims.join("、")}
              className="text-muted-foreground w-full truncate px-1 text-xs"
            >
              原片背书：{reverse.claims.join("、")}
            </span>
          )}
          {round.reverseEdited && (
            <Button
              variant="ghost"
              size="sm"
              disabled={!editable || actions.busy[`reverse-restore:${round.id}`]}
              onClick={() => void actions.restoreReverse(round)}
            >
              <Undo2 />
              恢复原稿
            </Button>
          )}
          <span className="flex-1" />
          {follow && (
            <Button
              size="sm"
              disabled={refollow.disabled || refollowing}
              title={refollow.reason}
              onClick={() => void actions.refollow(round)}
            >
              {refollowing ? <Loader2 className="animate-spin" /> : <RefreshCw />}
              按拆解重写跟拍版
              {cost !== null && <span className="tabular-nums opacity-70">✦{cost}</span>}
            </Button>
          )}
        </div>
      )}
    </aside>
  );
}

/** 提示词页：格式化后的视频提示词全文，可复制 */
function PromptPanel({ script, round }: { script: Script; round: Round }) {
  const [state, setState] = useState<
    { status: "loading" } | { status: "error" } | { status: "ready"; prompt: string }
  >({ status: "loading" });
  useEffect(() => {
    let alive = true;
    setState({ status: "loading" });
    getMarketingPrompt(script.id, round.settings.videoModel)
      .then((res) => alive && setState({ status: "ready", prompt: res.prompt }))
      .catch(() => alive && setState({ status: "error" }));
    return () => {
      alive = false;
    };
  }, [script.id, script.revision, round.settings.videoModel]);

  const copy = async () => {
    if (state.status !== "ready") return;
    try {
      await navigator.clipboard.writeText(state.prompt);
      toast.success("已复制");
    } catch {
      toast.error("复制失败，请手动选中复制");
    }
  };

  return (
    <div className="grid gap-2 p-4">
      <div className="flex items-center justify-end">
        <Button
          variant="ghost"
          size="sm"
          disabled={state.status !== "ready"}
          onClick={() => void copy()}
        >
          <Copy />
          复制
        </Button>
      </div>
      <pre className="bg-chrome-hover min-h-32 overflow-auto rounded-[18px] p-3 font-mono text-[11.5px] leading-relaxed whitespace-pre-wrap">
        {state.status === "loading" && (
          <span className="text-muted-foreground inline-flex items-center gap-2">
            <Loader2 className="size-3.5 animate-spin" />
            拼装中
          </span>
        )}
        {state.status === "error" && <span className="text-destructive">提示词加载失败</span>}
        {state.status === "ready" && state.prompt}
      </pre>
    </div>
  );
}

/** 镜头对照页（对比模式）：当前镜头的原片（跟拍版可改）与成片 */
function PairPanel({
  round,
  script,
  actions,
}: {
  round: Round;
  script: Script;
  actions: MarketingActions;
}) {
  const time = useWorkbenchStore((state) => state.time);
  const reverse = round.reverse;
  if (!reverse || !round.refVideo || script.shots.length === 0) return null;
  const index = Math.max(0, shotIndexAt(script.shots, time));
  const shot = script.shots[index];
  const follow = script.kind === "follow";
  const refIndex =
    follow && shot.refShot !== null
      ? reverse.shots.findIndex((item) => item.id === shot.refShot)
      : shotIndexAt(reverse.shots, refTimeAt(time, script, reverse, round.refVideo.duration));
  const refShot = reverse.shots[Math.max(refIndex, 0)];
  const editable = follow && reverseEditable(round) && !actions.busy[`reverse:${round.id}`];
  const commit = (patch: Parameters<typeof patchReverseShot>[2], field?: "action") => {
    if (field === "action") {
      const problem = validateAction(patch.action ?? "");
      if (problem) {
        toast.error(problem);
        return;
      }
    }
    void actions.saveReverse(round, patchReverseShot(reverse, Math.max(refIndex, 0), patch));
  };

  return (
    <div className="grid gap-2.5 p-4">
      <div className="text-muted-foreground text-xs">
        原片含第三方品牌 {reverse.brands.length} 处、促销背书 {reverse.claims.length} 处
        {follow ? "，跟拍版已去掉" : "，改编版未使用"}
      </div>
      {refShot && (
        <div className="bg-chrome-hover rounded-[18px] px-3.5 py-3 text-[13px]">
          <div className="text-muted-foreground mb-1.5 flex items-center gap-1.5 text-xs">
            原片 · 镜头 {refShot.id}
            {refShot.isOpening3s && <StatusPill tone="warning">前 3 秒</StatusPill>}
            {!follow && <StatusPill tone="muted">只读</StatusPill>}
          </div>
          <Label className="px-2">主体 + 动作</Label>
          <EditableText
            key={`a-${refShot.id}`}
            multiline
            value={refShot.action}
            ariaLabel={`原片镜头 ${refShot.id} 主体和动作`}
            disabled={!editable}
            onCommit={(value) => commit({ action: value }, "action")}
          />
          <Label className="mt-1 px-2">台词</Label>
          <EditableText
            key={`d-${refShot.id}`}
            value={refShot.dialogue}
            ariaLabel={`原片镜头 ${refShot.id} 台词`}
            disabled={!editable}
            onCommit={(value) => commit({ dialogue: value })}
          />
          <div className="text-muted-foreground px-2 pt-1.5 text-xs">
            商品怎么出现：{PRODUCT_ROLE_NAME[refShot.productRole]}
            {refShot.productInFrame && ` · ${refShot.productInFrame}`}
          </div>
        </div>
      )}
      <div className="bg-chrome-hover rounded-[18px] px-3.5 py-3 text-[13px]">
        <div className="text-muted-foreground mb-1.5 flex items-center text-xs">
          成片 · 镜头 {index + 1}
          <span className="flex-1" />
          <Button
            variant="ghost"
            size="xs"
            onClick={() => useWorkbenchStore.getState().focus(index)}
          >
            去改
          </Button>
        </div>
        <p className="leading-relaxed">{shot.scene}</p>
        <p className="mt-1.5 font-semibold">旁白：{shot.narration || "无"}</p>
      </div>
    </div>
  );
}

/** 选中脚本时的检查器：脚本 / 提示词 / 镜头对照，底部是配乐 · 音色、问题、方法、更多、出片 */
export function ScriptInspector({
  project,
  round,
  script,
  available,
  actions,
}: {
  project: Project;
  round: Round;
  script: Script;
  available: number | null;
  actions: MarketingActions;
}) {
  const { angles } = useMarketingOptions();
  const tab = useWorkbenchStore((state) => state.tab);
  const mode = useWorkbenchStore((state) => state.mode);
  const controls = useWorkbenchStore((state) => state.controls);
  const focusShot = useWorkbenchStore((state) => state.focusShot);
  const current = useCurrentShot(script.shots);
  const scrolled = useScrolledAt();
  const bodyRef = useRef<HTMLDivElement>(null);
  const [canvasOpen, setCanvasOpen] = useState(false);

  const follow = script.kind === "follow";
  const busy = isScriptBusy(script.status);
  const render = scriptRenderState(script, available);
  const rendering = actions.busy[`render:${script.id}`];
  const compare = compareState(round, script);
  const playing = script.status === "done" && !!controls;
  const flagsByShot = new Map<number, string[]>();
  script.flags.forEach((flag) =>
    flagsByShot.set(flag.shot, [
      ...(flagsByShot.get(flag.shot) ?? []),
      `${FLAG_NAME[flag.type]}「${flag.text}」`,
    ]),
  );
  const pairTab = tab === "pair" && mode === "compare" && compare.ok;
  const activeTab: WorkbenchTab = pairTab ? "pair" : tab === "pair" ? "script" : tab;
  const takes = playableTakes(script);

  /** 对比层「去改」：滚到那一镜并聚焦旁白 */
  useEffect(() => {
    if (!focusShot) return;
    const input = bodyRef.current?.querySelector<HTMLInputElement>(
      `[data-shot="${focusShot.index}"] input`,
    );
    input?.scrollIntoView?.({ block: "center" });
    input?.focus();
  }, [focusShot]);

  const saveNarration = (index: number, value: string) => {
    const narrations = script.shots.map((shot, i) => (i === index ? value : shot.narration));
    if (narrationsChanged(script, narrations)) void actions.saveNarrations(script, narrations);
  };

  /** 「参考 n」：已出片 → 对比并定位；没出片 → 选中原片并跳到那一镜 */
  const openRef = (index: number) => {
    const shot = script.shots[index];
    const store = useWorkbenchStore.getState();
    if (compare.ok) {
      store.setMode("compare");
      store.controls?.seek(shot.start);
      return;
    }
    const ref = round.reverse?.shots.find((item) => item.id === shot.refShot);
    store.select("ref", ref?.start ?? 0);
  };

  /** 先同步打开空白标签（异步完成后再开会被拦截），拿到画布 ID 再导航过去 */
  const sendToCanvas = async () => {
    const tabWindow = window.open("", "_blank");
    const id = await actions.toCanvas(script);
    if (id) {
      setCanvasOpen(false);
      if (tabWindow) tabWindow.location.href = `/canvas/${id}`;
      else window.open(`/canvas/${id}`, "_blank");
    } else {
      tabWindow?.close();
    }
  };

  return (
    <aside data-slot="marketing-inspector" data-kind="script" className={PANEL_CLASS}>
      <div className="px-4 pt-4">
        <div className="flex flex-wrap items-center gap-1.5">
          <StatusPill tone="muted" className={cn(follow && "bg-beam/15 text-beam")}>
            {follow && <Copy />}
            {angleName(script.angle, angles)}
          </StatusPill>
          <StatusPill
            tone={
              script.status === "done"
                ? "success"
                : script.status === "failed"
                  ? "destructive"
                  : busy
                    ? "running"
                    : "muted"
            }
          >
            {script.status === "done" && <Check />}
            {busy && <Loader2 className="animate-spin" />}
            {scriptStatusLabel(script.status)}
          </StatusPill>
          {script.edited && <StatusPill tone="muted">已修改</StatusPill>}
        </div>
        <div className="mt-1.5 text-base leading-snug font-bold">{script.title}</div>
      </div>
      <Tabs
        items={[
          { value: "script", label: "脚本", icon: FileText },
          { value: "prompt", label: "提示词", icon: CodeXml },
          {
            value: "pair",
            label: "镜头对照",
            icon: Columns2,
            disabled: mode === "compare" && compare.ok ? undefined : "切到对比原片后可用",
          },
        ]}
      />
      <div
        ref={bodyRef}
        className="min-h-0 flex-1 overflow-x-hidden overflow-y-auto [scrollbar-gutter:stable]"
        onWheel={scrolled.onWheel}
      >
        {activeTab === "script" && (
          <>
            {script.hook && (
              <p className="border-beam/70 mx-4 mt-4 border-l-2 pl-2.5 text-[13.5px]">
                {script.hook}
              </p>
            )}
            <div className="grid gap-0.5 px-1.5 py-2">
              {script.shots.map((shot, index) => {
                const problems = flagsByShot.get(index);
                return (
                  <ShotBlock
                    key={index}
                    index={index}
                    current={index === current}
                    scrolledAt={scrolled.at}
                    onJump={playing ? () => controls?.seek(shot.start) : null}
                  >
                    <div className="text-muted-foreground flex flex-wrap items-center gap-1.5 text-xs">
                      <span className="text-foreground/85 font-mono tabular-nums">
                        {shot.start}–{shot.end}s
                      </span>
                      <span>
                        {shot.shot} · {shot.camera}
                      </span>
                      {shot.refShot !== null && (
                        <button
                          type="button"
                          onClick={() => openRef(index)}
                          title={compare.ok ? "对比原片这一镜" : "看原片这一镜"}
                          className="bg-status-warning/12 text-status-warning hover:bg-status-warning/20 rounded-full px-2 text-[11px] leading-5"
                        >
                          参考 {shot.refShot}
                        </button>
                      )}
                    </div>
                    <p className="text-foreground/88 mt-1.5 text-[13px] leading-relaxed">
                      {shot.scene}
                    </p>
                    <div className="mt-1 flex items-center gap-0.5">
                      <Mic className="text-muted-foreground size-3.5 shrink-0" />
                      <EditableText
                        value={shot.narration}
                        ariaLabel={`镜头 ${index + 1} 旁白`}
                        placeholder="无旁白"
                        disabled={busy}
                        invalid={!!problems}
                        onCommit={(value) => saveNarration(index, value)}
                        className="font-semibold"
                      />
                    </div>
                    {problems && (
                      <p className="text-destructive mt-0.5 pl-5 text-xs">{problems.join("、")}</p>
                    )}
                  </ShotBlock>
                );
              })}
            </div>
          </>
        )}
        {activeTab === "prompt" && <PromptPanel script={script} round={round} />}
        {activeTab === "pair" && <PairPanel round={round} script={script} actions={actions} />}
      </div>
      <div className="border-chrome-border flex flex-wrap items-center gap-1.5 border-t px-3 py-2.5">
        <span
          title={`配乐：${script.music}；旁白音色：${script.voice}`}
          className="text-muted-foreground flex w-full min-w-0 items-center gap-1.5 px-1 text-xs"
        >
          <Music className="size-3.5 shrink-0" />
          <span className="truncate">
            {[script.music, script.voice].filter(Boolean).join(" · ")}
          </span>
        </span>
        {flagSummary(script.flags).map((type) => {
          const Icon = FLAG_ICON[type];
          return (
            <StatusPill key={type} tone={type === "claim" ? "warning" : "destructive"}>
              <Icon />
              {FLAG_NAME[type]}
            </StatusPill>
          );
        })}
        {script.method && (
          <span className="text-muted-foreground inline-flex items-center gap-1 px-1 text-xs">
            <BookOpen className="size-3.5" />
            方法：{methodLabel(script.method)}
          </span>
        )}
        <span className="flex-1" />
        <DropdownMenu modal={false}>
          <DropdownMenuTrigger
            aria-label="更多操作"
            className="hover:bg-chrome-hover focus-visible:ring-ring/50 text-muted-foreground hover:text-foreground data-popup-open:bg-chrome-hover grid size-8 place-items-center rounded-full outline-none focus-visible:ring-2 [&_svg]:size-4"
          >
            <MoreHorizontal />
          </DropdownMenuTrigger>
          <DropdownMenuContent align="end" sideOffset={6} className="w-44">
            <DropdownMenuItem
              disabled={takes.length === 0}
              onClick={() => {
                const url = takes.at(-1)?.outputs[0]?.url;
                if (url) downloadUrl(url, takeFileName(project, script, takes.length - 1, angles));
              }}
            >
              <Download />
              下载成片
            </DropdownMenuItem>
            <DropdownMenuItem disabled={busy} onClick={() => setCanvasOpen(true)}>
              <Workflow />
              发到画布精修
            </DropdownMenuItem>
            <DropdownMenuItem
              disabled={busy || !script.edited || actions.busy[`script-restore:${script.id}`]}
              onClick={() => void actions.restoreScript(script)}
            >
              <Undo2 />
              恢复原稿
            </DropdownMenuItem>
          </DropdownMenuContent>
        </DropdownMenu>
        <Button
          size="sm"
          disabled={render.disabled || rendering}
          title={render.reason || (render.again ? "按当前脚本再出一条" : "按当前脚本出片")}
          onClick={() => void actions.render(script)}
        >
          {rendering ? (
            <Loader2 className="animate-spin" />
          ) : render.again ? (
            <RotateCcw />
          ) : (
            <Play />
          )}
          {render.again ? "重出" : "出片"}
          <span className="tabular-nums opacity-70">✦{script.renderCredits}</span>
        </Button>
      </div>
      <CanvasDialog
        project={project}
        round={round}
        script={script}
        angles={angles}
        busy={!!actions.busy[`canvas:${script.id}`]}
        open={canvasOpen}
        onOpenChange={setCanvasOpen}
        onConfirm={() => void sendToCanvas()}
      />
    </aside>
  );
}

/** 写脚本中 / 没有内容时检查器的占位 */
export function InspectorPlaceholder({ loading }: { loading: boolean }) {
  return (
    <aside className={cn(PANEL_CLASS, "gap-3 p-4")}>
      {loading ? (
        <>
          <div className="bg-muted h-4 w-1/2 animate-pulse rounded-lg motion-reduce:animate-none" />
          <div className="bg-muted h-3 w-4/5 animate-pulse rounded-lg motion-reduce:animate-none" />
          <div className="bg-muted h-32 animate-pulse rounded-[18px] motion-reduce:animate-none" />
        </>
      ) : null}
    </aside>
  );
}
