import { useEffect, useState } from "react";
import { motion } from "motion/react";
import { NodeToolbar, Position, ViewportPortal, useInternalNode, useStore } from "@xyflow/react";
import { toast } from "sonner";

import { OutpaintFrame } from "@/components/canvas/outpaint-frame";
import { OutpaintPanel } from "@/components/canvas/outpaint-panel";
import { PANEL_OFFSET, usePanelPlacement } from "@/components/canvas/hooks/use-panel-placement";
import { useOutpaintSubmit } from "@/hooks/use-outpaint-submit";
import { useRemoteModels } from "@/hooks/use-models";
import { DURATION, EASE_OUT } from "@/lib/motion";
import { useCreditsStore } from "@/store/credits";
import { useOutpaintStore } from "@/store/outpaint";
import type { CanvasNodeData } from "@/types";
import {
  OUTPAINT_DEFAULT_MULT,
  OUTPAINT_RATIOS,
  containRect,
  frameByMult,
  frameByRatio,
  outpaintBlocker,
  type OutpaintFrame as Frame,
  type RatioKey,
} from "@/utils/canvas/outpaint";
import { loadOutpaintImage, type OutpaintImage } from "@/utils/canvas/outpaint-compose";
import { DEFAULT_NODE_SIZE } from "@/utils/canvas/placement";
import { quote } from "@/utils/pricing/quote";
import { priceSpecOf } from "@/utils/tasks/capabilities";

/** 扩图面板的最大宽度（屏幕像素）：比例条加提示词，比生成面板窄 */
const PANEL_MAX_WIDTH = 560;
/** 面板离框下沿的距离（屏幕像素） */
const PANEL_GAP = 14;
/** 切换比例 / 倍数的补间放完后，把「补间中」标记收掉，之后的拖动要跟手 */
const TWEEN_SETTLE_MS = DURATION.base * 1000 + 40;

/**
 * 图片节点的扩图（设计稿 docs/design/画布UI设计 6.17）：点「扩图」后，节点周围出现可调范围的框，
 * 框下方是倍数、比例条和提示词面板；点发送后在原图右侧生成新节点，扩图界面随即退出。
 * 框、比例、提示词都是这里的局部状态；进入前先读原图的真实尺寸（读不出就提示并退出），
 * 当前模型不支持图生图时同样提示并退出。退出：Esc、点画布空白（选中别的节点）、发送。
 */
export function OutpaintSession({ id, data }: { id: string; data: CanvasNodeData }) {
  const internal = useInternalNode(id);
  const zoom = useStore((state) => state.transform[2]);
  const { width: placedWidth, shift } = usePanelPlacement(id);
  const end = useOutpaintStore((state) => state.end);
  const submit = useOutpaintSubmit();
  const { models, status } = useRemoteModels("image");
  const availableCredits = useCreditsStore((state) => state.credits?.available ?? null);

  const modelKey = data.model ?? models[0]?.key;
  const model = models.find((item) => item.key === modelKey);
  const caps = model?.capabilities;

  const [image, setImage] = useState<OutpaintImage | null>(null);
  const [frame, setFrame] = useState<Frame | null>(null);
  const [mult, setMult] = useState<number>(OUTPAINT_DEFAULT_MULT);
  const [prompt, setPrompt] = useState("");
  const [tween, setTween] = useState(false);

  // 读原图的真实尺寸：框的几何都按原图像素算；读不出来（没配跨域、格式不支持）就说清原因并退出
  const src = data.src;
  useEffect(() => {
    if (!src) return;
    let cancelled = false;
    loadOutpaintImage(src)
      .then((loaded) => {
        if (cancelled) return;
        setImage(loaded);
        setFrame(frameByMult(loaded.size, OUTPAINT_DEFAULT_MULT));
      })
      .catch((error: Error) => {
        if (cancelled) return;
        toast.error(error.message);
        end(id);
      });
    return () => {
      cancelled = true;
    };
  }, [src, end, id]);

  // 模型清单加载好之后，当前模型不能扩图（不支持图生图、不收参考图、已下线）就提示并退出
  const blocker =
    status !== "ready"
      ? null
      : model
        ? outpaintBlocker(caps)
        : "当前图片模型不可用，换一个模型再扩图";
  useEffect(() => {
    if (!blocker) return;
    toast.error(blocker);
    end(id);
  }, [blocker, end, id]);

  // Esc 退出；菜单里按 Esc 是先关菜单，那次事件已经被处理过，不再往下退出
  useEffect(() => {
    const onKey = (event: KeyboardEvent) => {
      if (event.key === "Escape" && !event.defaultPrevented) end(id);
    };
    document.addEventListener("keydown", onKey);
    return () => document.removeEventListener("keydown", onKey);
  }, [end, id]);

  // 补间放完就收掉标记：拖动要跟手，不能带补间
  useEffect(() => {
    if (!tween) return;
    const timer = setTimeout(() => setTween(false), TWEEN_SETTLE_MS);
    return () => clearTimeout(timer);
  }, [tween, frame]);

  if (!internal || !image || !frame) return null;

  const { size } = image;
  const nodeWidth = internal.measured.width ?? DEFAULT_NODE_SIZE.width;
  const nodeHeight = internal.measured.height ?? DEFAULT_NODE_SIZE.height;
  const shown = containRect(size, { width: nodeWidth, height: nodeHeight });
  const absolute = internal.internals.positionAbsolute;
  const origin = { x: absolute.x + shown.x, y: absolute.y + shown.y };
  const k = shown.scale;

  // 面板摆在框的下方：框比节点底边多出的部分（画布单位）换成屏幕像素，加进面板离节点的偏移
  const frameBottom = origin.y + frame.y1 * k;
  const extra = Math.max(0, frameBottom - (absolute.y + nodeHeight));
  // 框没超出节点（比如原图被留白包着、还没扩）时，和生成面板一样离节点 PANEL_OFFSET
  const offset = extra > 0 ? extra * zoom + PANEL_GAP : PANEL_OFFSET;

  const cost = model ? quote(model.pricing, caps, priceSpecOf(caps, { op: "i2i" })) : undefined;
  const insufficient = cost !== undefined && availableCredits !== null && availableCredits < cost;

  const pickRatio = (key: RatioKey) => {
    setTween(true);
    const ratio = OUTPAINT_RATIOS.find((item) => item.key === key)?.ratio;
    setFrame(ratio === undefined ? frameByMult(size, mult) : frameByRatio(size, ratio));
  };
  const pickMult = (next: number) => {
    setMult(next);
    setTween(true);
    setFrame(frameByMult(size, next));
  };
  const send = () => {
    if (!modelKey || insufficient) return;
    // 节点在这一步里同步建出来，不用等拼图和上传；扩图界面随即退出
    void submit({ sourceId: id, image, frame, prompt, modelKey, caps });
    end(id);
  };

  return (
    <>
      <ViewportPortal>
        <OutpaintFrame
          size={size}
          frame={frame}
          origin={origin}
          k={k}
          zoom={zoom}
          animate={tween}
          onChange={setFrame}
          // 固定 id：连续撞上限只留一条提示
          onLimit={() => toast("已到 3 倍上限", { id: "outpaint-limit" })}
        />
      </ViewportPortal>
      <NodeToolbar isVisible position={Position.Bottom} offset={offset}>
        <motion.div
          initial={{ opacity: 0, y: -6, scale: 0.985 }}
          animate={{
            opacity: 1,
            y: 0,
            scale: 1,
            transition: { duration: DURATION.base, ease: EASE_OUT },
          }}
          exit={{ opacity: 0, transition: { duration: DURATION.exit, ease: EASE_OUT } }}
          style={{ translate: `${shift}px 0` }}
        >
          <OutpaintPanel
            size={size}
            frame={frame}
            width={Math.min(PANEL_MAX_WIDTH, placedWidth)}
            prompt={prompt}
            onPrompt={setPrompt}
            onPickRatio={pickRatio}
            onPickMult={pickMult}
            credits={cost}
            insufficient={insufficient}
            busy={false}
            onSubmit={send}
          />
        </motion.div>
      </NodeToolbar>
    </>
  );
}
