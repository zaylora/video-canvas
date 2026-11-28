import { ReactFlowProvider } from "@xyflow/react";
import { useCallback, useEffect, useRef, useState } from "react";
import { Navigate, useParams } from "react-router";
import { useCanvasLock } from "@/hooks/use-canvas-lock";
import { useCanvasOpening } from "@/hooks/use-canvas-opening";
import { useAgentSettings } from "@/store/agent-settings";
import { getCanvasTitle } from "@/utils/canvas/title-cache";

import { CanvasLoader } from "./canvas-loader";
import { Flow } from "./flow";
import { prefetchPetAssets } from "./pet/use-pet-assets";

/** 入场动画播多久后摘掉 data-entering，免得之后新建的节点也跟着播一遍 */
const ENTERING_MS = 1200;

export default function Canvas() {
  const { id } = useParams();
  const [revealedId, setRevealedId] = useState<string | null>(null);

  /**
   * 同一张画布只让一个标签页编辑：
   * 拿不到锁不是拦住，而是以只读预览打开（只读云端、不碰草稿），对方关闭后自动转成可编辑。
   * 被要求让位时要先存盘，所以把编辑器的 saveNow 交出去。
   */
  const saveRef = useRef<() => Promise<boolean>>(() => Promise.resolve(true));
  const lock = useCanvasLock(id, { onYield: () => saveRef.current() });
  const { opened, error, mountSeq, readOnly, replace } = useCanvasOpening(id, lock.status);

  // 桌宠素材和画布数据并行拉：等画布加载完、外壳挂上时素材多半已经到了
  const petSkin = useAgentSettings((s) => s.pet.skin);
  const petHidden = useAgentSettings((s) => s.pet.mode === "hidden");
  useEffect(() => {
    if (!petHidden) prefetchPetAssets(petSkin);
  }, [petSkin, petHidden]);

  const current = opened && opened.canvas.id === id ? opened : null;
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
  if (current?.canvas.collaborationActive)
    return <Navigate to={`/canvas/${id}/collaboration`} replace />;
  if (error) return <div className="grid min-h-svh place-items-center">画布不存在或无法加载</div>;
  return (
    <>
      {current && (
        <ReactFlowProvider>
          <Flow
            // 只读转可编辑时数据是重新拉的：模型和绘制都得跟着换一份，别拿旧节点接着渲染
            key={`${id}:${current.canvas.version}:${mountSeq}`}
            canvas={current.canvas}
            recovery={current.recovery}
            readOnly={readOnly}
            takeoverState={lock.takeoverState}
            onTakeover={lock.takeover}
            saveRef={saveRef}
            onConflict={(latest) => replace({ canvas: latest, recovery: null })}
          />
        </ReactFlowProvider>
      )}
      {revealedId !== id && (
        <CanvasLoader
          key={id}
          title={current?.canvas.title ?? getCanvasTitle(id)}
          ready={!!current}
          onOpen={onOpen}
          onDone={onDone}
        />
      )}
    </>
  );
}
