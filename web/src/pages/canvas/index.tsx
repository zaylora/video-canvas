import { ReactFlowProvider } from "@xyflow/react";
import { useCallback, useEffect, useState } from "react";
import { useParams } from "react-router";
import { getCanvas } from "@/api/canvas";
import { draftStore } from "@/utils/canvas/draft-idb";
import { loadCanvasForEditing, type OpenedCanvas } from "@/utils/canvas/open-canvas";
import { getCanvasTitle } from "@/utils/canvas/title-cache";
import { getCurrentUserId } from "@/utils/storage/user-id";

import { CanvasLoader } from "./canvas-loader";
import { Flow } from "./flow";

/** 入场动画播多久后摘掉 data-entering，免得之后新建的节点也跟着播一遍 */
const ENTERING_MS = 1200;

export default function Canvas() {
  const { id } = useParams();
  /** 打开结果：云端画布，加上本地草稿对账后的恢复信息 */
  const [opened, setOpened] = useState<OpenedCanvas | null>(null);
  const [error, setError] = useState(false);
  /** 加载层已经为哪张画布收起；换画布（id 变了）就重新走一遍加载 */
  const [revealedId, setRevealedId] = useState<string | null>(null);

  useEffect(() => {
    let active = true;
    if (!id) return;
    const userId = getCurrentUserId();
    void loadCanvasForEditing({
      canvasId: id,
      userId,
      getCanvas,
      loadDraft: (user, canvasId) => draftStore.load(user, canvasId),
      removeDraft: (canvasId) => (userId ? draftStore.remove(userId, canvasId) : Promise.resolve()),
    })
      .then((value) => {
        if (active) setOpened(value);
      })
      .catch(() => {
        if (active) setError(true);
      });
    return () => {
      active = false;
    };
  }, [id]);

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
  if (error) return <div className="grid min-h-svh place-items-center">画布不存在或无法加载</div>;
  return (
    <>
      {current && (
        <ReactFlowProvider>
          <Flow
            key={`${id}:${current.canvas.version}`}
            canvas={current.canvas}
            recovery={current.recovery}
            onConflict={(latest) => setOpened({ canvas: latest, recovery: null })}
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
