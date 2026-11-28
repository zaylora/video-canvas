import { useCallback, useEffect, useRef, useState } from "react";
import { useReactFlow, useStore } from "@xyflow/react";
import { toast } from "sonner";

import { uploadAsset } from "@/api/asset";
import { useNodeExtensions } from "@/components/canvas-editor/extensions";
import { useDirectorStore } from "@/store/director";
import type { CanvasEdge, CanvasNode, DirectorScene } from "@/types";
import { renderThumbnail } from "@/utils/director/render";
import { canonicalStructuredValue } from "@/utils/canvas/collaboration-graph";
import { createDefaultScene, sceneSizeError } from "@/utils/director/scene";

/** 停手多久才把场景写回节点（毫秒） */
const WRITE_DELAY = 500;

/** 场景写回节点的状态：已写入、等待写入、太大写不进去 */
export type DirectorSaveStatus = "saved" | "saving" | "too-large";

function sameSceneContent(first: DirectorScene | undefined, second: DirectorScene | undefined) {
  if (!first || !second) return first === second;
  return (
    JSON.stringify(canonicalStructuredValue({ ...first, thumb: undefined })) ===
    JSON.stringify(canonicalStructuredValue({ ...second, thumb: undefined }))
  );
}

/**
 * 编辑器里的场景自动写回导演台节点的 data.scene（随画布自动保存落库，设计稿 §7）。
 * 停手 500ms 写一次；超过体积上限时不写并提示。缩略图由节点保管，写场景时保留节点上现有的缩略图。
 * @returns status 给顶栏显示；finish 在关闭编辑器时调用：立刻写入，改过的话再渲染并上传缩略图
 */
export function useDirectorSync(nodeId: string) {
  const { getNode, updateNodeData } = useReactFlow<CanvasNode, CanvasEdge>();
  const { beginSceneEdit } = useNodeExtensions();
  const received = useStore(
    (state) => (state.nodeLookup.get(nodeId)?.data as CanvasNode["data"] | undefined)?.scene,
  );
  const origin = useRef<ReturnType<NonNullable<typeof beginSceneEdit>>>(undefined);
  const applyingRemote = useRef(false);
  const [status, setStatus] = useState<DirectorSaveStatus>("saved");
  const timer = useRef<number | null>(null);
  const latest = useRef<DirectorScene | null>(null);
  /** 这次打开后场景改过没有：没改过、节点也有缩略图就不重新渲染 */
  const changed = useRef(false);

  const write = useCallback(() => {
    timer.current = null;
    const scene = latest.current;
    if (!scene) return;
    latest.current = null;
    const error = sceneSizeError(scene);
    if (error) {
      setStatus("too-large");
      toast.error(error, { id: "director-scene-size" });
      return;
    }
    const current = getNode(nodeId)?.data.scene;
    const next = { ...scene, thumb: current?.thumb };
    origin.current?.(next);
    updateNodeData(nodeId, { scene: next });
    setStatus("saved");
  }, [getNode, nodeId, updateNodeData]);

  useEffect(() => {
    if (!beginSceneEdit || latest.current) return;
    const store = useDirectorStore.getState();
    if (store.nodeId !== nodeId) return;
    origin.current = beginSceneEdit(nodeId, received);
    const next = received ?? createDefaultScene();
    if (sameSceneContent(store.scene ?? undefined, next)) return;
    applyingRemote.current = true;
    useDirectorStore.setState({
      scene: next,
      past: [],
      future: [],
      selectedId: null,
      selectedKey: null,
      pending: {},
      gestureBase: null,
      playing: false,
      t: Math.min(store.t, next.duration),
    });
    applyingRemote.current = false;
  }, [beginSceneEdit, nodeId, received]);

  useEffect(
    () =>
      useDirectorStore.subscribe((state, prev) => {
        if (
          applyingRemote.current ||
          state.nodeId !== nodeId ||
          !state.scene ||
          state.scene === prev.scene
        )
          return;
        if (prev.nodeId !== nodeId) return;
        latest.current = state.scene;
        changed.current = true;
        setStatus("saving");
        if (timer.current !== null) window.clearTimeout(timer.current);
        timer.current = window.setTimeout(write, WRITE_DELAY);
      }),
    [nodeId, write],
  );
  // 卸载时（删节点、冲突、离开画布）把没写完的写掉；节点已经没了的话 updateNodeData 什么也不做
  useEffect(
    () => () => {
      if (timer.current !== null) {
        window.clearTimeout(timer.current);
        write();
      }
    },
    [write],
  );

  const finish = useCallback(
    (scene: DirectorScene, hasThumb: boolean) => {
      if (timer.current !== null) {
        window.clearTimeout(timer.current);
        write();
      }
      if (!changed.current && hasThumb) return;
      // 关闭后在后台渲染、上传缩略图；失败（请求层已提示）就留着旧图
      void (async () => {
        const blob = await renderThumbnail(scene);
        if (!blob) return;
        const asset = await uploadAsset(
          new File([blob], "director-thumb.jpg", { type: "image/jpeg" }),
        );
        updateNodeData(nodeId, (node) =>
          node.data.scene && sameSceneContent(node.data.scene, scene)
            ? { scene: { ...node.data.scene, thumb: { assetId: asset.id, url: asset.url } } }
            : {},
        );
      })().catch(() => {});
    },
    [nodeId, updateNodeData, write],
  );

  return { status, finish };
}
