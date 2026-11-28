import { compareCanvasVersions, type CanvasVersion } from "@/utils/canvas/collaboration-version";

import { useCallback, useEffect, useRef, useState } from "react";
import { toast } from "sonner";
import {
  commitCollaboration,
  getCollaboration,
  replayCollaboration,
  undoCollaboration,
  type CollaborationResult,
  type CollaborationRole,
} from "@/api/canvas/collaboration";
import type { CanvasGraphDto } from "@/api/canvas/type";
import {
  canonicalStructuredValue,
  collaborationFieldValue,
} from "@/utils/canvas/collaboration-graph";
import type { DirectorScene } from "@/types";
import { createCollaborationReplayBudget } from "@/utils/canvas/collaboration-replay-budget";
import {
  CanvasCollaborationSession,
  canvasCollaborationDraftKey,
  pruneCanvasCollaborationDrafts,
  type CollaborationGesture,
  type CollaborationTextOrigin,
} from "@/utils/canvas/collaboration-session";
import type { SaveStatus } from "@/utils/canvas/save-status";
import { ApiError } from "@/utils/requests/request";
import { getCurrentUserId } from "@/utils/storage/user-id";
import { watchCanvasRoom } from "@/utils/ws/canvas-room";

const samplingInterval = 100;
const reconcileInterval = 2000;
const replayPageBudget = 16;

/** 编辑器用来连接协作保存和远端增量的适配器。 */
export type CanvasCollaborationEditor = {
  /** 服务端已确认的画布标题。 */
  title: string;
  /** 当前同步状态。 */
  status: SaveStatus;
  /** 修改后安排采样。 */
  changed: () => void;
  /** 等待目前可提交的操作确认。 */
  flush: () => Promise<boolean>;
  /** 最新确认版本。 */
  getVersion: () => CanvasVersion;
  /** 固定编辑起点，最终确认时带上原版本。 */
  beginTextEdit: (
    id: string,
    field: "data.prompt" | "data.text",
    value: string,
  ) => ((confirmed: string) => void) | undefined;
  /** 锁定导演台独立编辑会话所依据的确认版本。 */
  beginSceneEdit: (
    id: string,
    value: DirectorScene | undefined,
  ) => ((confirmed: DirectorScene) => void) | undefined;
  /** 确认实际生成使用的模型、参数和素材。 */
  confirmGeneration: (
    id: string,
    model: string,
    input: Record<string, unknown>,
  ) => Promise<CanvasVersion | null>;
  /** 连接编辑器图谱读写。 */
  connect: (io: CollaborationGraphIO) => () => void;
  /** 向服务端请求个人撤销。 */
  undo: () => boolean;
  redo: () => boolean;
};

type CollaborationGraphIO = {
  read: () => CanvasGraphDto | null;
  takeFinishedGestures?: () => Map<string, CollaborationGesture>;
  render: (graph: CanvasGraphDto) => void;
};

type Snapshot = Awaited<ReturnType<typeof getCollaboration>>;

function collaborationSaveStatus(session: CanvasCollaborationSession): SaveStatus {
  if (session.conflicts.length > 0) return "conflict";
  return session.unsaved || session.undoId ? "saving" : "saved";
}

/** Connects one entered canvas to its queue and authorized room. */
export function useCanvasCollaboration(snapshot: Snapshot) {
  const { canvas, epoch } = snapshot;
  const [account] = useState(getCurrentUserId);
  const draftKey = canvasCollaborationDraftKey(account, canvas.id, epoch);
  const initiallyCanGenerate =
    snapshot.role === "owner" || (snapshot.role === "editor" && snapshot.canGenerate === true);
  const [session] = useState(() => {
    const current = new CanvasCollaborationSession(epoch, canvas.version, canvas.graph);
    if (snapshot.role !== "viewer") {
      current.restore(draftKey);
      if (!initiallyCanGenerate) {
        current.discardGeneration();
        current.store(draftKey);
      }
    }
    return current;
  });
  const [role, setRole] = useState<CollaborationRole | null>(snapshot.role);
  const [sessionEnded, setSessionEnded] = useState(false);
  const [title, setTitle] = useState(canvas.title);
  const [status, setStatus] = useState(() => collaborationSaveStatus(session));
  const [conflicts, setConflicts] = useState(() => session.conflicts);
  const [undoResult, setUndoResult] = useState<CollaborationResult | null>(null);
  const graphIO = useRef<CollaborationGraphIO | null>(null);
  const baseline = useRef({ graph: canvas.graph, revision: canvas.version });
  const timer = useRef<ReturnType<typeof setTimeout> | null>(null);
  const inFlight = useRef<Promise<boolean> | null>(null);
  const needsSync = useRef(false);
  const textOrigins = useRef(new Map<string, CollaborationTextOrigin>());
  const access = useRef<CollaborationRole | null>(snapshot.role);
  const [canGenerate, setCanGenerate] = useState(initiallyCanGenerate);
  const [canCopyToLibrary, setCanCopyToLibrary] = useState(
    snapshot.role === "editor" && snapshot.canCopyToLibrary === true,
  );
  const generateAccess = useRef(canGenerate);
  const active = useRef(false);

  const capture = useCallback(() => {
    if (timer.current !== null) clearTimeout(timer.current);
    timer.current = null;
    if (!access.current || access.current === "viewer") return;
    const graph = graphIO.current?.read();
    if (!graph) return;
    const shown = baseline.current;
    const changed = session.capture(
      shown.graph,
      graph,
      graphIO.current?.takeFinishedGestures?.(),
      textOrigins.current,
      shown.revision,
    );
    session.store(draftKey);
    if (changed) baseline.current = { graph, revision: shown.revision };
  }, [session, draftKey]);

  const render = useCallback(() => {
    const graph = session.optimistic;
    const previous = baseline.current.graph;
    baseline.current = { graph, revision: session.revision };
    if (previous !== graph) graphIO.current?.render(graph);
    session.store(draftKey);
    setConflicts(session.conflicts);
    setStatus(collaborationSaveStatus(session));
  }, [session, draftKey]);

  const updateRole = useCallback(
    (current: CollaborationRole | null, generate?: boolean, copyToLibrary?: boolean) => {
      access.current = current;
      setRole(current);
      generateAccess.current = current === "owner" || (current === "editor" && generate === true);
      setCanGenerate(generateAccess.current);
      setCanCopyToLibrary(current === "editor" && copyToLibrary === true);
      if (!generateAccess.current) session.discardGeneration();
      if (!current || current === "viewer") {
        textOrigins.current.clear();
        setUndoResult(null);
        session.clear(draftKey);
        pruneCanvasCollaborationDrafts(account, canvas.id);
      }
    },
    [account, canvas.id, session, draftKey],
  );

  const sync = useCallback(async () => {
    const hasReplayBudget = createCollaborationReplayBudget(session.confirmed);
    for (let page = 0; page < replayPageBudget; page++) {
      const after = session.revision;
      const replay = await replayCollaboration(canvas.id, epoch, after);
      if (!active.current) return;
      capture();
      updateRole(replay.role, replay.canGenerate, replay.canCopyToLibrary);
      if (replay.role === "viewer") return;
      if (replay.snapshotRequired || replay.events.some((event) => !session.apply(event, "replay")))
        break;
      if (compareCanvasVersions(session.revision, replay.revision) >= 0) {
        if (replay.title !== undefined) setTitle(replay.title);
        render();
        return;
      }
      if (session.revision === after || !hasReplayBudget(replay.events)) break;
    }
    const replacement = await getCollaboration(canvas.id);
    if (!active.current) return;
    if (replacement.epoch !== epoch) {
      setSessionEnded(true);
      updateRole(null);
      return;
    }
    capture();
    updateRole(replacement.role, replacement.canGenerate, replacement.canCopyToLibrary);
    if (replacement.role === "viewer") return;
    session.replace(replacement.canvas.version, replacement.canvas.graph);
    setTitle(replacement.canvas.title);
    render();
  }, [canvas.id, epoch, session, capture, updateRole, render]);

  const reportFailure = useCallback(
    (error: unknown) => {
      if (!active.current) return;
      if (error instanceof ApiError && error.code === 30018) {
        setSessionEnded(true);
        updateRole(null);
      } else if (
        error instanceof ApiError &&
        (error.status === 401 || error.status === 403 || error.status === 404)
      )
        updateRole(null);
      else setStatus("error");
    },
    [updateRole],
  );

  const flush = useCallback(
    (catchUp = false): Promise<boolean> => {
      needsSync.current ||= catchUp;
      capture();
      if (inFlight.current) return inFlight.current;
      const work = async () => {
        let sent = 0;
        while (active.current && access.current && access.current !== "viewer") {
          if (needsSync.current) {
            needsSync.current = false;
            await sync();
            continue;
          }
          if (sent >= 32) break;
          const pending = session.next();
          const undoId = !pending && !session.unsaved ? session.undoId : null;
          if (!pending && !undoId) break;
          session.store(draftKey);
          const request = pending
            ? commitCollaboration(canvas.id, pending)
            : undoCollaboration(canvas.id, epoch, undoId!, session.redoPending);
          const result = await request.catch(async (error: unknown) => {
            if (
              pending &&
              error instanceof ApiError &&
              error.status === 400 &&
              error.code === 10001
            )
              return "rejected" as const;
            if (error instanceof ApiError && error.status === 404 && active.current) await sync();
            if (access.current === "viewer" || !access.current) return null;
            if (
              !generateAccess.current &&
              pending?.changes[0]?.fields?.["data.generationInput"] !== undefined &&
              error instanceof ApiError &&
              error.status === 404
            )
              return null;
            throw error;
          });
          sent++;
          if (!result) return false;
          if (!active.current) return false;
          if (undoId && session.undoId !== undoId) return false;
          capture();
          if (result === "rejected") {
            if (pending) session.reject(pending.opId);
            render();
            continue;
          }
          if (!session.apply(result)) {
            await sync();
            if (!active.current || (undoId && session.undoId !== undoId) || !session.apply(result))
              return false;
          }
          if (undoId) {
            session.finishUndo();
            setUndoResult(result);
          }
          render();
        }
        return (
          !!access.current && access.current !== "viewer" && !session.unsaved && !session.undoId
        );
      };
      const pending = work()
        .catch((error: unknown) => {
          needsSync.current = true;
          reportFailure(error);
          return false;
        })
        .finally(() => {
          inFlight.current = null;
        });
      inFlight.current = pending;
      return pending;
    },
    [canvas.id, epoch, session, draftKey, capture, sync, render, reportFailure],
  );

  const changed = useCallback(() => {
    if (!access.current || access.current === "viewer") return;
    if (textOrigins.current.size) {
      void flush();
      return;
    }
    if (timer.current !== null) return;
    setStatus("saving");
    timer.current = setTimeout(() => void flush(), samplingInterval);
  }, [flush]);

  useEffect(() => {
    pruneCanvasCollaborationDrafts(
      account,
      canvas.id,
      snapshot.role === "viewer" ? undefined : epoch,
    );
  }, [account, canvas.id, epoch, snapshot.role]);

  useEffect(() => {
    active.current = true;
    const reconcile = () => {
      if (access.current && document.visibilityState === "visible") void flush(true);
    };
    const unwatch = watchCanvasRoom(canvas.id, (event) => {
      if (event.type === "canvas.collaboration" && event.data.transferPending) {
        if (access.current === "editor" && event.data.epoch === epoch) {
          toast.warning("所有者正在尝试转移画布", {
            id: `canvas-transfer-${canvas.id}`,
            description: "转移成功后，你将失去原画布的访问权限。",
          });
        }
        return;
      }
      if (
        event.type === "canvas.collaboration" &&
        event.data.epoch === epoch &&
        compareCanvasVersions(event.data.revision ?? 0, 0) > 0 &&
        compareCanvasVersions(event.data.revision ?? 0, session.revision) <= 0
      )
        return;
      if (event.type !== "canvas.presence" && event.type !== "canvas.generation_progress")
        void flush(true);
    });
    const interval = setInterval(reconcile, reconcileInterval);
    document.addEventListener("visibilitychange", reconcile);
    window.addEventListener("online", reconcile);
    window.addEventListener("pagehide", capture);
    if (session.unsaved || session.undoId) void flush(true);
    return () => {
      capture();
      active.current = false;
      unwatch();
      clearInterval(interval);
      document.removeEventListener("visibilitychange", reconcile);
      window.removeEventListener("online", reconcile);
      window.removeEventListener("pagehide", capture);
    };
  }, [canvas.id, epoch, session, capture, flush]);

  const connect = useCallback(
    (io: CollaborationGraphIO) => {
      graphIO.current = io;
      if (session.unsaved) render();
      return () => {
        capture();
        if (graphIO.current === io) graphIO.current = null;
      };
    },
    [session, render, capture],
  );
  const getVersion = useCallback(() => baseline.current.revision, []);
  const beginFieldEdit = useCallback(
    (id: string, field: CollaborationTextOrigin["field"], value: unknown) => {
      if (!access.current || access.current === "viewer") return;
      const origin = { field, revision: baseline.current.revision, value };
      return (confirmed: unknown) => {
        if (!active.current || !access.current || access.current === "viewer") return;
        const node = baseline.current.graph.nodes.find((current) => current.id === id);
        if (
          !node ||
          JSON.stringify(collaborationFieldValue(node, field)) === JSON.stringify(confirmed)
        )
          return;
        textOrigins.current.set(id, { ...origin, confirmed });
      };
    },
    [],
  );
  const beginSceneEdit = useCallback(
    (id: string, value: DirectorScene | undefined) => {
      const finish = beginFieldEdit(id, "data.scene", canonicalStructuredValue(value));
      return finish && ((confirmed: DirectorScene) => finish(canonicalStructuredValue(confirmed)));
    },
    [beginFieldEdit],
  );
  const confirmGeneration = useCallback(
    async (id: string, model: string, input: Record<string, unknown>) => {
      if (!generateAccess.current || !(await flush()) || !generateAccess.current) return null;
      if (!session.confirmed.nodes.some((node) => node.id === id)) return null;
      const receiptRevision = session.confirmGeneration(id, model, input);
      return (await flush()) && generateAccess.current ? receiptRevision() : null;
    },
    [flush, session],
  );
  const requestHistory = useCallback(
    (redo: boolean) => {
      if (
        !access.current ||
        access.current === "viewer" ||
        session.conflicts.length > 0 ||
        session.undoId
      )
        return false;
      session.startUndo(redo);
      session.store(draftKey);
      setStatus("saving");
      setUndoResult(null);
      void flush(true);
      return true;
    },
    [draftKey, flush, session],
  );
  const undo = useCallback(() => requestHistory(false), [requestHistory]);
  const redo = useCallback(() => requestHistory(true), [requestHistory]);
  const resolve = (id: string, applyDraft: boolean) => {
    capture();
    session.resolve(id, applyDraft);
    render();
    void flush();
  };

  return {
    role,
    sessionEnded,
    canGenerate,
    canCopyToLibrary,
    discardAccess: () => updateRole(null),
    editor: {
      title,
      status,
      changed,
      flush,
      getVersion,
      beginTextEdit: beginFieldEdit,
      beginSceneEdit,
      confirmGeneration,
      connect,
      undo,
      redo,
    } satisfies CanvasCollaborationEditor,
    conflicts,
    undoResult,
    dismissUndo: () => setUndoResult(null),
    resolve,
  };
}
