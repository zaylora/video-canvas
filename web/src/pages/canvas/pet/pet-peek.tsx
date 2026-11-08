import { useCanvasEditor } from "@/components/canvas-editor/editor-api";
import { useAgentSettings } from "@/store/agent-settings";
import { usePetStore } from "@/store/pet";
import { useCanvasRunningTaskCount } from "@/store/tasks";
import { petMood } from "@/utils/pet/mood";

import { PetSprite } from "./pet-sprite";
import { usePageVisible, useReducedMotionPref } from "./use-pet-activity";
import { usePetAssets } from "./use-pet-assets";

/** 探头小镜的显示宽度：按 192×208 等比缩放后约 65px 高，框沿只露出上面的头和前爪 */
const PEEK_WIDTH = 60;

const noop = () => {};

/**
 * 趴在浮窗输入框上沿探头的小镜：浮窗打开时画布上的小镜收起，换到这里继续表现状态。
 * 只负责画；位置和裁切由浮窗决定（见 AgentPanel 的 mascot）。
 * 动作规则和画布上的一样（等审批、运行中、生成中、一轮结束的反应），但不睡觉、不打招呼、不能拖、没有气泡。
 * 素材和画布上的共用一份；没加载好或隐藏档时不渲染，由调用方显示原来的标识。
 */
export function PetPeek({
  busy,
  pending,
  available,
}: {
  /** 助手正在跑一轮 */
  busy: boolean;
  /** 在等你批准或回答 */
  pending: boolean;
  /** 有没有可用的 Agent 模型；null 是还在加载 */
  available: boolean | null;
}) {
  const { canvasId } = useCanvasEditor();
  const mode = useAgentSettings((s) => s.pet.mode);
  const skin = useAgentSettings((s) => s.pet.skin);
  const { assets } = usePetAssets(skin, mode !== "hidden");
  const reaction = usePetStore((s) => s.reaction);
  const runningTasks = useCanvasRunningTaskCount(canvasId);
  const pageVisible = usePageVisible();
  const reduced = useReducedMotionPref();
  if (!assets || mode === "hidden") return null;

  // reaction 由画布上的桌宠层按时清掉；非空就是还在播
  const mood = petMood({
    dragging: false,
    dragDir: null,
    reaction,
    now: reaction ? reaction.until - 1 : 0,
    pending,
    busy,
    runningTasks,
    available,
    idleMs: 0,
    hovering: false,
    mode,
    reducedMotion: reduced,
    hasSleeping: assets.hasSleeping,
  });
  return (
    <PetSprite
      assets={assets}
      row={mood.row}
      loop={mood.loop}
      paused={!pageVisible}
      sleepFallback={mood.sleepFallback}
      width={PEEK_WIDTH}
      onAnimationEnd={noop}
    />
  );
}
