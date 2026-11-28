import { compareCanvasVersions, type CanvasVersion } from "@/utils/canvas/collaboration-version";

import { useEffect, useState, useSyncExternalStore } from "react";
import { ViewportPortal, useReactFlow, useStore, useStoreApi } from "@xyflow/react";
import { Eye, MousePointer2, Users } from "lucide-react";

import type { CanvasGenerationProgress, CanvasPresenceState } from "@/api/generation-task/type";
import { canvasEditingNode, type CanvasRoomPresence } from "@/utils/ws/canvas-presence";
import { watchCanvasRoom } from "@/utils/ws/canvas-room";
import { useCanvasEditor } from "./editor-api";

const colors = ["#2dd4bf", "#fb923c", "#a78bfa", "#60a5fa", "#fb7185", "#a3e635"];
const memberColor = (id: string) =>
  colors[Number.parseInt(id.slice(-4), 16) % colors.length] ?? colors[0];

type ProgressSample = CanvasGenerationProgress & { expiresAt: number };
const progressTTL = 30_000;

/** 展示当前节点运行的公开进度，样本过期后恢复节点自身的运行提示。 */
export function CanvasGenerationProgressLayer({
  canvasId,
  epoch,
}: {
  canvasId: string;
  epoch: CanvasVersion;
}) {
  const [samples, setSamples] = useState(() => new Map<string, ProgressSample>());
  useEffect(() => {
    const unwatch = watchCanvasRoom(canvasId, (event) => {
      if (event.type === "unsubscribed") setSamples(new Map());
      if (event.type !== "canvas.generation_progress" || event.data.epoch !== epoch) return;
      const sample = event.data.progress;
      const expiresAt = performance.now() + progressTTL;
      setSamples((current) => {
        const previous = current.get(sample.nodeId);
        if (
          previous &&
          (compareCanvasVersions(previous.runRevision, sample.runRevision) > 0 ||
            (previous.runRevision === sample.runRevision && previous.percent > sample.percent))
        )
          return current;
        return new Map(current).set(sample.nodeId, { ...sample, expiresAt });
      });
    });
    const timer = window.setInterval(() => {
      const now = performance.now();
      setSamples((current) => {
        const next = [...current].filter(([, sample]) => sample.expiresAt > now);
        return next.length === current.size ? current : new Map(next);
      });
    }, 1000);
    return () => {
      unwatch();
      window.clearInterval(timer);
    };
  }, [canvasId, epoch]);
  if (!samples.size) return null;
  return (
    <ViewportPortal>
      <div className="pointer-events-none absolute inset-0 z-20">
        {[...samples.values()].map((sample) => (
          <NodeGenerationProgress key={sample.nodeId} sample={sample} />
        ))}
      </div>
    </ViewportPortal>
  );
}

function NodeGenerationProgress({ sample }: { sample: ProgressSample }) {
  const origin = useStore(
    (state) => state.nodeLookup.get(sample.nodeId)?.internals.positionAbsolute,
  );
  const running = useStore((state) => {
    const data = state.nodeLookup.get(sample.nodeId)?.data;
    return data?.status === "running" && data.generationRevision === sample.runRevision;
  });
  if (!origin || !running) return null;
  return (
    <span
      className="bg-popover text-popover-foreground absolute -translate-y-full rounded px-1.5 py-0.5 text-xs shadow-sm"
      style={{ left: origin.x, top: origin.y }}
    >
      生成中 {sample.percent}%
    </span>
  );
}

/** 在线列表只响应人员或用户名变化。 */
export function CanvasPresenceRoster({ presence }: { presence: CanvasRoomPresence }) {
  const members = useSyncExternalStore(presence.subscribeRoster, presence.getRoster);
  const following = useSyncExternalStore(presence.subscribeFollowing, presence.getFollowing);
  const followingUsername = members.find(({ id }) => id === following)?.username;
  const { setCenter } = useReactFlow();
  const { locateNode } = useCanvasEditor();
  const locateMember = (id: string) => {
    presence.stopFollowing();
    const state = presence.member(id);
    const node = state?.editing ?? state?.drag?.id ?? state?.selection?.[0];
    if (node && locateNode(node)) return;
    const point = state?.cursor ?? state?.view;
    if (point) void setCenter(point.x, point.y);
  };
  return (
    <div
      className="text-muted-foreground flex items-center gap-1 px-2 text-xs"
      aria-label={`${members.length + 1} 人在线`}
    >
      <Users aria-hidden className="size-3.5" />
      <span>{members.length + 1} 人在线</span>
      {members
        .filter(({ username }) => !!username)
        .slice(0, 9)
        .map(({ id, username }) => (
          <span key={id} className="flex items-center">
            <button
              type="button"
              title={`定位${username}`}
              aria-label={`定位${username}`}
              className="hover:bg-chrome-hover grid size-6 place-items-center rounded-full"
              onClick={() => locateMember(id)}
            >
              <span className="size-2 rounded-full" style={{ backgroundColor: memberColor(id) }} />
            </button>
            <button
              type="button"
              title={following === id ? "结束跟随" : `跟随${username}的视角`}
              aria-label={following === id ? "结束跟随" : `跟随${username}的视角`}
              aria-pressed={following === id}
              className="hover:bg-chrome-hover aria-pressed:text-foreground grid size-6 place-items-center rounded-full"
              onClick={() => {
                if (following === id) presence.stopFollowing();
                else presence.follow(id);
              }}
            >
              <Eye className="size-3.5" aria-hidden />
            </button>
          </span>
        ))}
      {followingUsername && <span role="status">正在跟随{followingUsername}</span>}
    </div>
  );
}

/** 光标、选区和拖动预览使用视口层，内容树只消费确认变更。 */
export function CanvasPresenceLayer({
  presence,
  visible,
}: {
  presence: CanvasRoomPresence;
  visible: boolean;
}) {
  const peers = useSyncExternalStore(presence.subscribe, presence.getSnapshot);
  useLocalPresence(presence);
  useFollowViewport(presence);
  if (!visible) return null;
  return (
    <ViewportPortal>
      <div className="pointer-events-none absolute inset-0 z-20" aria-hidden>
        {peers.map(({ actorId, actorUsername, cursor, drag, selection, editing }) => {
          const color = memberColor(actorId);
          const label = actorUsername ? `${actorUsername} ` : "";
          return (
            <div key={actorId}>
              {selection
                ?.filter((id) => id !== editing && id !== drag?.id)
                .map((id) => (
                  <NodePresenceMark key={id} id={id} color={color} />
                ))}
              {editing && (
                <NodePresenceMark id={editing} color={color} label={`${label}正在编辑`} />
              )}
              {drag && (
                <NodePresenceMark
                  id={drag.id}
                  position={drag}
                  color={color}
                  label={`${label}正在移动`}
                />
              )}
              {cursor && (
                <div
                  className="absolute flex items-start gap-1"
                  style={{ left: cursor.x, top: cursor.y, color }}
                >
                  <MousePointer2 className="size-5 fill-current" />
                  {actorUsername && (
                    <span className="bg-popover text-popover-foreground whitespace-nowrap rounded px-1.5 py-0.5 text-xs shadow-sm">
                      {actorUsername}
                    </span>
                  )}
                </div>
              )}
            </div>
          );
        })}
      </div>
    </ViewportPortal>
  );
}

function useFollowViewport(presence: CanvasRoomPresence) {
  const following = useSyncExternalStore(presence.subscribeFollowing, presence.getFollowing);
  const { setCenter } = useReactFlow();
  useEffect(() => {
    if (!following) return;
    let previous: CanvasPresenceState["view"];
    const apply = () => {
      if (presence.getFollowing() !== following) return;
      const view = presence.member(following)?.view;
      if (!view || (view.x === previous?.x && view.y === previous.y && view.zoom === previous.zoom))
        return;
      previous = view;
      void setCenter(view.x, view.y, { zoom: view.zoom });
    };
    const escape = (event: KeyboardEvent) => {
      if (event.key === "Escape") presence.stopFollowing();
    };
    apply();
    const unsubscribe = presence.subscribe(apply);
    window.addEventListener("keydown", escape);
    return () => {
      unsubscribe();
      window.removeEventListener("keydown", escape);
    };
  }, [following, presence, setCenter]);
}

function NodePresenceMark({
  id,
  color,
  position,
  label,
}: {
  id: string;
  color: string;
  position?: CanvasPresenceState["cursor"];
  label?: string;
}) {
  const origin = useStore((state) => state.nodeLookup.get(id)?.internals.positionAbsolute);
  const width = useStore((state) => state.nodeLookup.get(id)?.measured?.width);
  const height = useStore((state) => state.nodeLookup.get(id)?.measured?.height);
  const at = position ?? origin;
  if (!origin || !at || !width || !height) return null;
  return (
    <div
      className="absolute rounded-xl border-2"
      style={{
        left: at.x,
        top: at.y,
        width,
        height,
        borderColor: color,
        borderStyle: label ? "dashed" : "solid",
      }}
    >
      {label && (
        <span className="bg-popover text-popover-foreground absolute bottom-full left-0 mb-1 whitespace-nowrap rounded px-1.5 py-0.5 text-xs shadow-sm">
          {label}
        </span>
      )}
    </div>
  );
}

function useLocalPresence(presence: CanvasRoomPresence) {
  const { canvasId } = useCanvasEditor();
  const store = useStoreApi();
  const flowElement = useStore((state) => state.domNode);
  const { screenToFlowPosition } = useReactFlow();
  useEffect(() => presence.connect(), [presence]);
  useEffect(() => {
    const shareView = ({ transform, width, height }: ReturnType<typeof store.getState>) => {
      if (!width || !height || presence.getFollowing()) return;
      const [x, y, zoom] = transform;
      presence.update({ view: { x: (width / 2 - x) / zoom, y: (height / 2 - y) / zoom, zoom } });
    };
    shareView(store.getState());
    return store.subscribe((state, previous) => {
      if (
        state.transform !== previous.transform ||
        state.width !== previous.width ||
        state.height !== previous.height
      )
        shareView(state);
    });
  }, [presence, store]);
  useEffect(() => {
    if (!flowElement) return;
    const move = (event: PointerEvent) =>
      presence.update({ cursor: screenToFlowPosition({ x: event.clientX, y: event.clientY }) });
    const leave = () => presence.update({ cursor: undefined });
    const updateEditing = (target: EventTarget | null) =>
      presence.update({ editing: canvasEditingNode(flowElement, canvasId, target) });
    const focus = (event: FocusEvent) => updateEditing(event.target);
    const blur = (event: FocusEvent) => updateEditing(event.relatedTarget);
    flowElement.addEventListener("pointermove", move, { passive: true });
    flowElement.addEventListener("pointerleave", leave);
    const doc = flowElement.ownerDocument;
    doc.addEventListener("focusin", focus);
    doc.addEventListener("focusout", blur);
    updateEditing(doc.activeElement);
    return () => {
      flowElement.removeEventListener("pointermove", move);
      flowElement.removeEventListener("pointerleave", leave);
      doc.removeEventListener("focusin", focus);
      doc.removeEventListener("focusout", blur);
    };
  }, [canvasId, flowElement, presence, screenToFlowPosition]);
  useEffect(
    () =>
      store.subscribe((state, previous) => {
        if (state.nodes === previous.nodes) return;
        const selection: string[] = [];
        let drag: CanvasPresenceState["drag"];
        for (const node of state.nodes) {
          if (node.selected && selection.length < 16) selection.push(node.id);
          if (node.dragging && !drag) {
            const at = state.nodeLookup.get(node.id)?.internals.positionAbsolute ?? node.position;
            drag = { id: node.id, ...at };
          }
        }
        presence.update({ selection, drag });
      }),
    [presence, store],
  );
}
