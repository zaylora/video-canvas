import {
  memo,
  useCallback,
  useEffect,
  useLayoutEffect,
  useMemo,
  useRef,
  useState,
  type RefObject,
} from "react";

import { ContextMenu, ContextMenuTrigger } from "@/components/ui/context-menu";
import { ASSISTANT_NAME } from "@/constants/agent";
import { useIsMobile } from "@/hooks/use-mobile";
import { cn } from "@/lib/utils";
import { useAgentSettings } from "@/store/agent-settings";
import { usePetStore } from "@/store/pet";
import { useCanvasRunningTaskCount } from "@/store/tasks";
import { claimFirstOpenToday, claimNightEgg, pickEasterLine } from "@/utils/pet/easter";
import { IDLE_SLEEP_MS, REACTION_HOLD_MS, petMood, type PetMode } from "@/utils/pet/mood";
import { DEFAULT_ANCHOR, anchorOf, rectOf, type Rect, type Size } from "@/utils/pet/placement";
import { PET_ROW } from "@/utils/pet/rows";

import { useAgentHost } from "../agent-host";
import { PetBubble } from "./pet-bubble";
import { PetMenuContent } from "./pet-menu";
import { PET_WIDTH, PetSprite, petHeight } from "./pet-sprite";
import { useAssistantBubbles } from "./use-assistant-bubbles";
import {
  usePageVisible,
  usePetGreeting,
  usePetSleep,
  useReducedMotionPref,
} from "./use-pet-activity";
import { usePetAssets, type PetAssets } from "./use-pet-assets";
import { petTransform, usePetDrag, type DragDir } from "./use-pet-drag";

const dismissBubble = (id: string) => usePetStore.getState().dismiss(id);

/** 量画布区域（桌宠所在层）的尺寸；侧栏开合、窗口缩放都会触发 */
function useStageArea(ref: RefObject<HTMLElement | null>): Size | null {
  const [area, setArea] = useState<Size | null>(null);
  useLayoutEffect(() => {
    const el = ref.current;
    if (!el) return;
    const measure = () =>
      setArea((prev) =>
        prev && prev.width === el.clientWidth && prev.height === el.clientHeight
          ? prev
          : { width: el.clientWidth, height: el.clientHeight },
      );
    measure();
    const observer = new ResizeObserver(measure);
    observer.observe(el);
    return () => observer.disconnect();
  }, [ref]);
  return area;
}

/**
 * 画布上的桌宠层（设计稿 桌宠.md 6.1）：和 ChromeZone 同层的绝对定位层，只有桌宠和气泡接收指针事件。
 * 挂载就加载素材，成功且档位不是「隐藏」时接管画布提醒（setActive）；浮窗打开时小镜淡出、换成趴在输入框上沿探头，
 * 这期间提醒走 toast（见 petTakesNotices）；
 * 没加载好、加载失败或隐藏时什么都不渲染：加载中外壳留空位，失败、超时或隐藏时页面显示原来的胶囊。
 */
export const PetLayer = memo(function PetLayer({ canvasId }: { canvasId: string }) {
  const mode = useAgentSettings((s) => s.pet.mode);
  const skin = useAgentSettings((s) => s.pet.skin);
  const { assets, status } = usePetAssets(skin, mode !== "hidden");
  const active = assets !== null && mode !== "hidden";

  // 先于绘制写入：外壳第一次渲染画了胶囊，也会在显示前换成留位
  useLayoutEffect(() => {
    usePetStore.getState().setStatus(status);
    return () => usePetStore.getState().setStatus("off");
  }, [status]);

  useEffect(() => {
    usePetStore.getState().setActive(active);
    return () => usePetStore.getState().setActive(false);
  }, [active]);

  // 动画样式注入一次，素材换了或卸载时移除
  useLayoutEffect(() => {
    if (!assets) return;
    const style = document.createElement("style");
    style.dataset.petStyle = assets.skin;
    style.textContent = assets.css;
    document.head.appendChild(style);
    return () => style.remove();
  }, [assets]);

  if (!assets || mode === "hidden") return null;
  return <PetStage assets={assets} canvasId={canvasId} mode={mode} />;
});

function PetStage({
  assets,
  canvasId,
  mode,
}: {
  assets: PetAssets;
  canvasId: string;
  mode: Exclude<PetMode, "hidden">;
}) {
  const mobile = useIsMobile() === true;
  const { open, toggle, models, ctl } = useAgentHost();
  const stageRef = useRef<HTMLDivElement>(null);
  const petRef = useRef<HTMLDivElement>(null);
  const area = useStageArea(stageRef);
  const anchor = useAgentSettings((s) => s.pet.anchor);
  const pageVisible = usePageVisible();
  const reduced = useReducedMotionPref();
  const runningTasks = useCanvasRunningTaskCount(canvasId);
  const reaction = usePetStore((s) => s.reaction);
  const bubble = usePetStore((s) => s.queue.current);

  const [dragging, setDragging] = useState(false);
  const [dragDir, setDragDir] = useState<DragDir>(null);

  const available = models === null ? null : models.length > 0;
  const pending = ctl.pending;
  const hasPending = pending !== null;

  // 上下文要最先写入：后面的彩蛋、助手冒泡入队时，队列已经知道档位和浮窗状态
  useEffect(() => {
    usePetStore.getState().setContext({
      mode,
      dragging,
      panelOpen: open,
      assistantBusy: ctl.busy,
      assistantPending: hasPending,
      pageVisible,
    });
  }, [mode, dragging, open, ctl.busy, hasPending, pageVisible]);

  const openRef = useRef(open);
  useEffect(() => {
    openRef.current = open;
  });
  const openPanel = useCallback(() => {
    if (!openRef.current) toggle();
  }, [toggle]);

  useAssistantBubbles({
    open,
    busy: ctl.busy,
    activeRun: ctl.activeRun,
    pending: pending ? { id: pending.id, kind: pending.kind } : null,
    runs: ctl.state.runs,
    openPanel,
  });

  // 位置：窄屏固定右下角；否则按锚点还原并夹回区域内
  const width = mobile ? PET_WIDTH.narrow : PET_WIDTH.desktop;
  const size = useMemo(() => ({ width, height: petHeight(width) }), [width]);
  const rect: Rect | null = useMemo(
    () => (area ? rectOf(mobile ? DEFAULT_ANCHOR : (anchor ?? DEFAULT_ANCHOR), area, size) : null),
    [area, anchor, mobile, size],
  );
  useEffect(() => {
    if (rect && area) usePetStore.getState().setPlacement({ rect, area });
  }, [rect, area]);

  // 睡着、醒来、打招呼
  const { greet, onHover, wave, clear: clearGreet } = usePetGreeting(mode);
  const onWake = useCallback(() => {
    usePetStore.getState().push({
      id: `easter:wake:${Date.now()}`,
      source: "easter",
      priority: "easter",
      text: pickEasterLine("wake"),
      mood: "waving",
      ttl: 4000,
    });
  }, []);
  const { asleep, touch } = usePetSleep(mode, onWake);

  // 助手和生成的事件也算活动
  const bubbleKey = bubble ? `${bubble.id}:${bubble.version}` : "";
  const bubbleMood = bubble?.mood;
  useEffect(() => {
    touch();
  }, [touch, ctl.busy, hasPending, runningTasks, bubbleKey]);

  // 气泡的动作是打招呼时，播一次 waving
  useEffect(() => {
    if (bubbleMood === "waving") wave();
  }, [bubbleMood, bubbleKey, wave]);

  // 彩蛋：当天第一次打开、夜里第一次；只在活泼档，冷却由队列负责
  const eggDone = useRef(false);
  useEffect(() => {
    if (mode !== "lively" || eggDone.current) return;
    eggDone.current = true;
    const now = new Date();
    const first = claimFirstOpenToday(now);
    const night = claimNightEgg(now);
    const kind = night ? "night" : first ? "firstOpen" : null;
    if (!kind) return;
    usePetStore.getState().push({
      id: `easter:${kind}:${now.getTime()}`,
      source: "easter",
      priority: "easter",
      text: pickEasterLine(kind),
      mood: "waving",
      ttl: 4000,
    });
  }, [mode]);

  // 一次性反应：播完后停住 600ms 再清；这里的计时兜底（减少动态效果时没有 animationend）
  useEffect(() => {
    if (!reaction) return;
    const timer = window.setTimeout(
      () => usePetStore.getState().clearReaction(),
      Math.max(0, reaction.until - Date.now()) + 50,
    );
    return () => window.clearTimeout(timer);
  }, [reaction]);

  // reaction 非空就是还在播：过期由上面的计时清掉，渲染期不读时钟
  const mood = petMood({
    dragging,
    dragDir,
    reaction,
    now: reaction ? reaction.until - 1 : 0,
    pending: hasPending,
    busy: ctl.busy,
    runningTasks,
    available,
    idleMs: asleep ? IDLE_SLEEP_MS : 0,
    hovering: greet,
    mode,
    reducedMotion: reduced,
    hasSleeping: assets.hasSleeping,
  });

  const moodRef = useRef(mood);
  useEffect(() => {
    moodRef.current = mood;
  });
  const onAnimationEnd = useCallback(() => {
    const current = moodRef.current;
    if (current.loop) return;
    if (current.row === PET_ROW.waving) {
      clearGreet();
      return;
    }
    const played = usePetStore.getState().reaction;
    if (!played) return;
    window.setTimeout(() => {
      if (usePetStore.getState().reaction === played) usePetStore.getState().clearReaction();
    }, REACTION_HOLD_MS);
  }, [clearGreet]);

  const drag = usePetDrag({
    elRef: petRef,
    rect: rect ?? { x: 0, y: 0, ...size },
    area: area ?? { width: 0, height: 0 },
    enabled: !mobile && area !== null,
    onDraggingChange: setDragging,
    onDirChange: setDragDir,
    onCommit: (next) => {
      if (area) useAgentSettings.getState().patchPet({ anchor: anchorOf(next, area) });
    },
    onClick: () => {
      if (available) toggle();
    },
  });

  return (
    <div
      ref={stageRef}
      data-slot="pet-layer"
      className="pointer-events-none absolute inset-0 z-[15]"
    >
      {rect && area && (
        <>
          <div
            ref={petRef}
            // 浮窗打开时淡出但不卸载：位置照样给浮窗定位用，关上后原地淡入
            inert={open || undefined}
            aria-hidden={open || undefined}
            className={cn(
              "canvas-overlay-interactive absolute top-0 left-0 transition-opacity duration-200 motion-reduce:transition-none",
              open ? "pointer-events-none opacity-0" : "pointer-events-auto",
            )}
            style={{
              width: size.width,
              height: size.height,
              transform: petTransform(rect.x, rect.y),
            }}
          >
            <ContextMenu>
              <ContextMenuTrigger
                render={
                  <button
                    type="button"
                    aria-label={ASSISTANT_NAME}
                    aria-pressed={open}
                    aria-disabled={available === false}
                    title={available === false ? "管理员尚未配置 Agent 模型" : undefined}
                    className={cn(
                      "focus-visible:ring-node-ring/60 block size-full touch-none rounded-2xl outline-none focus-visible:ring-2",
                      mobile ? "cursor-pointer" : "cursor-grab active:cursor-grabbing",
                    )}
                    onPointerEnter={onHover}
                    {...drag}
                  />
                }
              >
                <PetSprite
                  assets={assets}
                  row={mood.row}
                  loop={mood.loop}
                  paused={!pageVisible}
                  sleepFallback={mood.sleepFallback}
                  width={size.width}
                  onAnimationEnd={onAnimationEnd}
                />
              </ContextMenuTrigger>
              <PetMenuContent
                mode={mode}
                canReset={!mobile}
                onMode={(next) => useAgentSettings.getState().patchPet({ mode: next })}
                onReset={() => useAgentSettings.getState().patchPet({ anchor: null })}
                onHide={() => useAgentSettings.getState().patchPet({ mode: "hidden" })}
              />
            </ContextMenu>
          </div>
          {bubble && !dragging && !open && (
            <PetBubble
              key={bubble.id}
              bubble={bubble}
              rect={rect}
              area={area}
              paused={!pageVisible}
              onDismiss={dismissBubble}
            />
          )}
        </>
      )}
    </div>
  );
}
