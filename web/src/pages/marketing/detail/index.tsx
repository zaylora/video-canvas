import { useCallback, useEffect, useRef, useState } from "react";
import { Link, useParams } from "react-router";
import { ShoppingBag, X } from "lucide-react";
import { MotionConfig } from "motion/react";
import { useDefaultLayout } from "react-resizable-panels";

import type { Project } from "@/api/marketing/type";
import { Logo } from "@/components/brand/logo";
import { FocusLoader } from "@/components/focus-loader";
import { MarketingComposer } from "@/components/marketing/marketing-composer";
import { Button } from "@/components/ui/button";
import { ResizableHandle, ResizablePanel, ResizablePanelGroup } from "@/components/ui/resizable";
import { useMediaQuery } from "@/hooks/use-media-query";
import { usePageTitle } from "@/hooks/use-page-title";
import { useMarketingProject } from "@/hooks/use-marketing-project";
import { cn } from "@/lib/utils";
import { useCreditsStore } from "@/store/credits";
import { useWorkbenchStore } from "@/store/marketing-workbench";
import { formatPageTitle } from "@/utils/page-title";
import {
  defaultSelection,
  nextSelection,
  resolveSelection,
  shotIndexAt,
} from "@/utils/marketing/workbench";

import { AssetList } from "./asset-list";
import { FactDialog } from "./facts";
import { useWorkbenchTarget } from "./hooks";
import { InspectorPlaceholder, ReverseInspector, ScriptInspector } from "./inspector";
import { Stage } from "./stage";
import { TopBar } from "./top-bar";
import { useMarketingActions } from "./use-actions";

/** 加载层开始退场时不用做什么：创作台自己的入场随挂载播放 */
const noop = () => {};

const ROOT_CLASS =
  "home-theme bg-background text-foreground flex h-svh w-full flex-col gap-2.5 overflow-hidden p-2.5 max-lg:h-auto max-lg:min-h-svh max-lg:overflow-visible";

/** 正在输入或开着弹窗时，快捷键不响应 */
function shortcutsBlocked(event: KeyboardEvent): boolean {
  const target = event.target as HTMLElement | null;
  if (target?.closest("input, textarea, select, [contenteditable='true']")) return true;
  if (event.metaKey || event.ctrlKey || event.altKey) return true;
  return !!document.querySelector("[role='dialog']");
}

/** 分栏拖拽条：平时透明，悬停或拖动时中间出现一条细线 */
const HANDLE =
  "w-2.5 bg-transparent before:absolute before:inset-y-8 before:left-1/2 before:w-0.5 before:-translate-x-1/2 before:rounded-full before:transition-colors before:duration-150 hover:before:bg-beam/60 data-[separator=active]:before:bg-beam";

/** 分栏里的一格：子元素撑满高度 */
const CELL = "flex h-full min-h-0 min-w-0 flex-col *:min-h-0 *:flex-1";

/**
 * 三栏：宽屏（≥1024px）可拖动调整左栏与检查器的宽度，拖好的尺寸记在本机；窄屏上下堆叠、整页滚动。
 */
function Columns({
  list,
  stage,
  inspector,
}: {
  list: React.ReactNode;
  stage: React.ReactNode;
  inspector: React.ReactNode;
}) {
  const wide = useMediaQuery("(min-width: 1024px)");
  const layout = useDefaultLayout({
    id: "marketing-workbench-columns",
    panelIds: ["list", "stage", "inspector"],
  });
  if (!wide) {
    return (
      <div className="grid min-h-0 flex-1 gap-2.5">
        {list}
        {stage}
        {inspector}
      </div>
    );
  }
  return (
    <ResizablePanelGroup
      orientation="horizontal"
      className="min-h-0 flex-1"
      defaultLayout={layout.defaultLayout}
      onLayoutChanged={layout.onLayoutChanged}
    >
      <ResizablePanel id="list" defaultSize={248} minSize={200} maxSize="35%">
        <div className={CELL}>{list}</div>
      </ResizablePanel>
      <ResizableHandle className={HANDLE} />
      <ResizablePanel id="stage" minSize={360}>
        <div className={CELL}>{stage}</div>
      </ResizablePanel>
      <ResizableHandle className={HANDLE} />
      <ResizablePanel id="inspector" defaultSize={400} minSize={300} maxSize="45%">
        <div className={CELL}>{inspector}</div>
      </ResizablePanel>
    </ResizablePanelGroup>
  );
}

/**
 * 创作台：轮次与选中项的初始化、跟随（新开一轮自动切过去、脚本被替换时回落）、快捷键与三栏布局。
 */
function Workbench({ project }: { project: Project }) {
  const actions = useMarketingActions(project);
  const available = useCreditsStore((state) => state.credits?.available ?? null);
  const { round, selection, script } = useWorkbenchTarget(project.id);
  const [composerOpen, setComposerOpen] = useState(false);
  const [factsOpen, setFactsOpen] = useState(false);
  const latestRound = project.rounds[0];
  const seenLatest = useRef(latestRound?.id);

  // 进页面 / 轮次被删：选最新一轮；新开了一轮（再写一轮发送成功）：切过去并收起生成框
  useEffect(() => {
    const store = useWorkbenchStore.getState();
    const known = project.rounds.find((item) => item.id === store.roundId);
    if (latestRound && latestRound.id !== seenLatest.current) {
      seenLatest.current = latestRound.id;
      store.setRound(latestRound.id, defaultSelection(latestRound));
      setComposerOpen(false);
      return;
    }
    if (!known && latestRound) store.setRound(latestRound.id, defaultSelection(latestRound));
  }, [project.rounds, latestRound]);

  // 当前选中不在这一轮了（脚本被替换、写脚本结束）：按默认规则回落
  useEffect(() => {
    if (!round) return;
    const next = resolveSelection(round, selection);
    if (next !== selection) useWorkbenchStore.getState().select(next);
  }, [round, selection]);

  // 快捷键：空格播放 / 暂停，← → 上一镜 / 下一镜，↑ ↓ 切换左栏，F 全屏，Esc 收起生成框
  useEffect(() => {
    const onKeyDown = (event: KeyboardEvent) => {
      if (event.key === "Escape" && composerOpen && !document.querySelector("[role='dialog']")) {
        setComposerOpen(false);
        return;
      }
      if (shortcutsBlocked(event) || !round) return;
      const store = useWorkbenchStore.getState();
      const { controls } = store;
      switch (event.key) {
        case " ":
          if (!controls) return;
          event.preventDefault();
          controls.toggle();
          return;
        case "ArrowLeft":
        case "ArrowRight": {
          const shots =
            store.selection === "ref" ? (round.reverse?.shots ?? []) : (script?.shots ?? []);
          if (!controls || shots.length === 0) return;
          event.preventDefault();
          const index = Math.max(0, shotIndexAt(shots, store.time));
          const next = Math.min(
            Math.max(index + (event.key === "ArrowRight" ? 1 : -1), 0),
            shots.length - 1,
          );
          controls.seek(shots[next].start, store.playing);
          return;
        }
        case "ArrowUp":
        case "ArrowDown": {
          const next = nextSelection(round, store.selection, event.key === "ArrowDown" ? 1 : -1);
          if (!next) return;
          event.preventDefault();
          store.select(next);
          return;
        }
        case "f":
        case "F":
          controls?.fullscreen();
          return;
      }
    };
    document.addEventListener("keydown", onKeyDown);
    return () => document.removeEventListener("keydown", onKeyDown);
  }, [round, script, composerOpen]);

  let inspector: React.ReactNode;
  if (!round || (round.status !== "ready" && !(selection === "ref" && round.refVideo))) {
    inspector = <InspectorPlaceholder loading={round?.status === "scripting"} />;
  } else if (selection === "ref") {
    inspector = (
      <ReverseInspector project={project} round={round} available={available} actions={actions} />
    );
  } else if (script) {
    inspector = (
      <ScriptInspector
        project={project}
        round={round}
        script={script}
        available={available}
        actions={actions}
      />
    );
  } else {
    inspector = <InspectorPlaceholder loading={false} />;
  }

  return (
    <div data-slot="marketing-workbench" className={ROOT_CLASS}>
      <TopBar
        project={project}
        round={round}
        available={available}
        actions={actions}
        composerOpen={composerOpen}
        onRound={(id) => {
          const next = project.rounds.find((item) => item.id === id);
          if (next) useWorkbenchStore.getState().setRound(id, defaultSelection(next));
        }}
        onEditFacts={() => setFactsOpen(true)}
        onComposer={() => setComposerOpen((open) => !open)}
      />
      <Columns
        list={
          <AssetList
            project={project}
            round={round}
            actions={actions}
            onEditFacts={() => setFactsOpen(true)}
          />
        }
        stage={<Stage project={project} available={available} actions={actions} />}
        inspector={inspector}
      />

      {composerOpen && (
        <div
          data-slot="marketing-composer-panel"
          className="bg-chrome ring-chrome-border fixed top-17 right-3 z-40 w-[min(760px,calc(100vw-24px))] rounded-[28px] p-2 shadow-2xl ring-1 backdrop-blur-xl"
        >
          <div className="flex justify-end px-1 pb-1">
            <Button
              variant="ghost"
              size="icon-sm"
              aria-label="收起"
              title="收起（Esc）"
              onClick={() => setComposerOpen(false)}
            >
              <X />
            </Button>
          </div>
          <MarketingComposer project={project} />
        </div>
      )}

      {project.product && factsOpen && (
        <FactDialog
          key={project.revision}
          facts={project.product}
          open
          busy={!!actions.busy.facts}
          onOpenChange={setFactsOpen}
          onSave={(next) => void actions.saveFacts(next).then((ok) => ok && setFactsOpen(false))}
        />
      )}
    </div>
  );
}

/**
 * 营销片详情页（/marketing/:id，全屏创作台，设计 docs/design/营销工作室/创作台.md）：
 * 顶栏切轮次，左栏选原片 / 脚本并看商品信息，中间舞台播放与对比原片，右栏检查器看拆解 / 脚本 / 提示词。
 * 数据读本地缓存，进页面拉一次，之后靠 WebSocket 推送；断线重连后重新拉详情对账。
 */
export default function MarketingDetailPage() {
  const { id } = useParams();
  const { project, status, reload } = useMarketingProject(id);
  usePageTitle(formatPageTitle(project?.title, "营销工作室"));

  // 离开页面或换项目时清空视图状态（选中项、模式、时间）。只在清理阶段做：
  // 父组件的 effect 晚于子组件执行，进页面时清空会冲掉创作台刚选好的轮次
  useEffect(() => () => useWorkbenchStore.getState().reset(), [id]);
  /** 进场加载层已收起 */
  const [revealed, setRevealed] = useState(false);
  const onLoaderDone = useCallback(() => setRevealed(true), []);

  if (status === "error" && !project) {
    return (
      <div className={cn(ROOT_CLASS, "items-center justify-center")}>
        <div className="text-muted-foreground flex flex-col items-center gap-3 text-sm">
          <ShoppingBag className="size-8 opacity-50" />
          营销片不存在或加载失败
          <div className="flex gap-2">
            <Button variant="outline" size="sm" onClick={() => void reload()}>
              重试
            </Button>
            <Button
              variant="outline"
              size="sm"
              nativeButton={false}
              render={<Link to="/marketing" />}
            >
              返回营销工作室
            </Button>
          </div>
        </div>
      </div>
    );
  }

  // 进场加载层（和进画布、短剧工作台同一套）：数据到了且 Logo 聚焦完才退场。
  // 固定放在创作台后面：数据到达时它的位置不变，否则会被卸载重挂，动画从头再播
  const loader = !revealed && (
    <FocusLoader
      icon={<Logo size={56} animated />}
      plainIcon
      title="连镜"
      subtitle={project ? `正在打开「${project.title}」` : "正在打开营销片"}
      label={project ? `正在打开营销片「${project.title}」` : "正在打开营销片"}
      ready={!!project}
      onOpen={noop}
      onDone={onLoaderDone}
    />
  );

  return (
    <MotionConfig reducedMotion="user">
      {project ? <Workbench key={project.id} project={project} /> : null}
      {loader}
    </MotionConfig>
  );
}
