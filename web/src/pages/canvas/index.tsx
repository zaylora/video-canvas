import { ReactFlowProvider } from "@xyflow/react";
import { useCallback, useEffect, useState } from "react";
import { useParams } from "react-router";
import { getCanvas } from "@/api/canvas";
import type { CanvasDetailDto } from "@/api/canvas/type";
import { getCanvasTitle } from "@/utils/canvas/title-cache";

import { CanvasLoader } from "./canvas-loader";
import { Flow } from "./flow";

/** 入场动画播多久后摘掉 data-entering，免得之后新建的节点也跟着播一遍 */
const ENTERING_MS = 1200;

export default function Canvas() {
  const { id } = useParams();
  const [canvas, setCanvas] = useState<CanvasDetailDto | null>(null);
  const [error, setError] = useState(false);
  /** 加载层已经为哪张画布收起；换画布（id 变了）就重新走一遍加载 */
  const [revealedId, setRevealedId] = useState<string | null>(null);

  useEffect(() => {
    let active = true;
    if (!id) return;
    void getCanvas(id)
      .then((value) => {
        if (active) setCanvas(value);
      })
      .catch(() => {
        if (active) setError(true);
      });
    return () => {
      active = false;
    };
  }, [id]);

  const current = canvas && canvas.id === id ? canvas : null;
  // 入场直接在画布根节点上挂 data-entering，不走 state：
  // 走 state 会让整张画布在退场动画刚开始时重渲染一遍，正好卡在那一下
  const onOpen = useCallback(() => {
    const root = document.querySelector<HTMLElement>("[data-canvas-root]");
    if (!root) return;
    root.dataset.entering = "";
    window.setTimeout(() => delete root.dataset.entering, ENTERING_MS);
  }, []);
  const onDone = useCallback(() => {
    if (id) setRevealedId(id);
  }, [id]);

  if (!id) return null;
  if (error) return <div className="grid min-h-svh place-items-center">画布不存在或无法加载</div>;
  return (
    <>
      {current && (
        <ReactFlowProvider>
          <Flow key={`${id}:${current.version}`} canvas={current} onConflict={setCanvas} />
        </ReactFlowProvider>
      )}
      {revealedId !== id && (
        <CanvasLoader
          key={id}
          title={current?.title ?? getCanvasTitle(id)}
          ready={!!current}
          onOpen={onOpen}
          onDone={onDone}
        />
      )}
    </>
  );
}
