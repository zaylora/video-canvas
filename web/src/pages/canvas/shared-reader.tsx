import {
  compareCanvasVersions,
  nextCanvasVersion,
  type CanvasVersion,
} from "@/utils/canvas/collaboration-version";

import { useCallback, useEffect, useRef, useState } from "react";
import { useParams, Link, useNavigate } from "react-router";
import { toast } from "sonner";
import { Button } from "@/components/ui/button";
import { canvasSharing } from "@/api/drama/share";
import { mapCanvasProject } from "@/api/canvas";
import { replayCollaboration } from "@/api/canvas/collaboration";
import type { BackendCanvasProjectDto, CanvasDetailDto } from "@/api/canvas/type";
import { ApiError } from "@/utils/requests/request";
import service from "@/utils/requests/service";
import { SharedCanvasScene } from "@/components/canvas-editor/shared-scene";
import { CanvasGenerationProgressLayer } from "@/components/canvas-editor/collaboration-presence";
import { usePageTitle } from "@/hooks/use-page-title";
import { canvasPageTitle } from "@/utils/page-title";
import { applyCollaborationChanges } from "@/utils/canvas/collaboration-graph";
import { createCollaborationReplayBudget } from "@/utils/canvas/collaboration-replay-budget";
import { watchCanvasRoom } from "@/utils/ws/canvas-room";

import {
  DEFAULT_SHARED_REFRESH_SECONDS,
  MAX_SHARED_REFRESH_SECONDS,
} from "./chrome/shared-refresh-control";
import { SharedTopLeftBar } from "./chrome/top-left-bar";
import { ShortcutsDialog } from "./chrome/shortcuts-dialog";
import { WorkspaceChrome } from "./chrome/workspace-chrome";

const back = "/canvases?scope=shared-canvas";
const REFRESH_PREFERENCE_KEY = "canvas:shared-refresh-seconds";
const AUTO_REFRESH_PREFERENCE_KEY = "canvas:shared-auto-refresh";
const ACCESS_CHECK_INTERVAL_MS = 5000;
type SharedCanvasResponse = BackendCanvasProjectDto | { unchanged: true };

function readAutoRefresh() {
  try {
    return localStorage.getItem(AUTO_REFRESH_PREFERENCE_KEY) !== "off";
  } catch {
    return true;
  }
}

function readRefreshSeconds() {
  try {
    const seconds = Number(localStorage.getItem(REFRESH_PREFERENCE_KEY));
    if (Number.isInteger(seconds) && seconds >= 1 && seconds <= MAX_SHARED_REFRESH_SECONDS)
      return seconds;
  } catch {
    return DEFAULT_SHARED_REFRESH_SECONDS;
  }
  return DEFAULT_SHARED_REFRESH_SECONDS;
}

export default function SharedCanvasReader() {
  const { id = "" } = useParams();
  return <CanvasReader key={id} id={id} />;
}

function CanvasReader({ id }: { id: string }) {
  const navigate = useNavigate();
  const [canvas, setCanvas] = useState<CanvasDetailDto | null>(null);
  const [error, setError] = useState<"access" | "revoked" | "network" | null>(null);
  const [reload, setReload] = useState(0);
  const [leaving, setLeaving] = useState(false);
  const [autoRefresh, setAutoRefresh] = useState(readAutoRefresh);
  const [live, setLive] = useState(false);
  const [shortcutsOpen, setShortcutsOpen] = useState(false);
  const openShortcuts = useCallback(() => setShortcutsOpen(true), []);
  const [refreshSeconds, setRefreshSeconds] = useState(readRefreshSeconds);
  const current = useRef<CanvasDetailDto | null>(null);
  const hadAccess = useRef(false);
  const autoRefreshActions = useRef<{
    poll: () => void;
    live: () => void;
    focus: () => void;
    access: () => void;
    cancel: () => void;
    cancelAccess: () => void;
  } | null>(null);
  const liveAvailable = !!canvas?.collaborationEpoch;
  const liveEnabled = live && liveAvailable;
  usePageTitle(canvasPageTitle(canvas?.title ?? "共享画布"));
  useEffect(() => {
    let pending: { controller: AbortController; automatic: boolean } | undefined;
    let accessRequest: AbortController | undefined;
    let accessEnded = false;
    let catchUp = false;
    const denyAccess = () => {
      accessEnded = true;
      catchUp = false;
      pending?.controller.abort();
      accessRequest?.abort();
      current.current = null;
      setCanvas(null);
      setError(hadAccess.current ? "revoked" : "access");
    };
    const load = (automatic: boolean, mode: "snapshot" | "delta" = "snapshot") => {
      if (accessEnded) return;
      pending?.controller.abort();
      const request = new AbortController();
      pending = { controller: request, automatic };
      void readSharedCanvas(id, current.current, request.signal, mode)
        .then(
          (next) => {
            if (request.signal.aborted) return;
            setError(null);
            if (!next) return;
            hadAccess.current = true;
            current.current = next;
            setCanvas(next);
          },
          (failure: unknown) => {
            if (request.signal.aborted) return;
            if (isAccessDenied(failure)) denyAccess();
            else setError("network");
          },
        )
        .finally(() => {
          if (pending?.controller !== request) return;
          pending = undefined;
          if (catchUp && !accessEnded && !request.signal.aborted) {
            catchUp = false;
            load(true, "delta");
          }
        });
    };
    load(false);
    autoRefreshActions.current = {
      poll: () => {
        if (!document.hidden && !accessEnded && !pending) load(true);
      },
      live: () => {
        if (document.hidden || accessEnded) return;
        if (pending) catchUp = true;
        else load(true, "delta");
      },
      focus: () => load(true),
      access: () => {
        if (document.hidden || accessEnded || accessRequest) return;
        const request = new AbortController();
        accessRequest = request;
        void service
          .get(
            `/canvas/${id}/reader`,
            { access_only: true },
            {
              signal: request.signal,
              silent: true,
              timeout: ACCESS_CHECK_INTERVAL_MS,
            },
          )
          .catch((failure: unknown) => {
            if (!request.signal.aborted && isAccessDenied(failure)) denyAccess();
          })
          .finally(() => {
            if (accessRequest === request) accessRequest = undefined;
          });
      },
      cancel: () => {
        catchUp = false;
        if (!pending?.automatic) return;
        pending.controller.abort();
        pending = undefined;
      },
      cancelAccess: () => {
        accessRequest?.abort();
        accessRequest = undefined;
      },
    };
    return () => {
      pending?.controller.abort();
      accessRequest?.abort();
      autoRefreshActions.current = null;
    };
  }, [id, reload]);

  useEffect(() => {
    if (!autoRefresh || liveEnabled) return;
    const focus = () => autoRefreshActions.current?.focus();
    window.addEventListener("focus", focus);
    return () => {
      window.removeEventListener("focus", focus);
      autoRefreshActions.current?.cancel();
    };
  }, [autoRefresh, liveEnabled]);

  useEffect(() => {
    if (!autoRefresh || liveEnabled) return;
    const interval = window.setInterval(
      () => autoRefreshActions.current?.poll(),
      refreshSeconds * 1000,
    );
    return () => window.clearInterval(interval);
  }, [autoRefresh, liveEnabled, refreshSeconds]);

  useEffect(() => {
    if (!liveAvailable || liveEnabled) return;
    const check = () => autoRefreshActions.current?.access();
    const interval = window.setInterval(check, ACCESS_CHECK_INTERVAL_MS);
    window.addEventListener("online", check);
    document.addEventListener("visibilitychange", check);
    return () => {
      window.clearInterval(interval);
      window.removeEventListener("online", check);
      document.removeEventListener("visibilitychange", check);
      autoRefreshActions.current?.cancelAccess();
    };
  }, [liveAvailable, liveEnabled]);

  useEffect(() => {
    if (!liveAvailable || liveEnabled || autoRefresh) return;
    const check = () => autoRefreshActions.current?.access();
    window.addEventListener("focus", check);
    return () => window.removeEventListener("focus", check);
  }, [autoRefresh, liveAvailable, liveEnabled]);

  useEffect(() => {
    if (!liveEnabled) return;
    const sync = () => autoRefreshActions.current?.live();
    const unwatch = watchCanvasRoom(id, (event) => {
      if (event.type === "canvas.collaboration" && event.data.transferPending) {
        if (event.data.epoch === current.current?.collaborationEpoch) {
          toast.warning("所有者正在尝试转移画布", {
            id: `canvas-transfer-${id}`,
            description: "转移成功后，你将失去原画布的访问权限。",
          });
        }
        return;
      }
      if (event.type === "subscribed" || event.type === "unsubscribed") sync();
      if (
        event.type === "canvas.collaboration" &&
        (!event.data.revision ||
          compareCanvasVersions(event.data.revision, current.current?.version ?? 0) > 0)
      )
        sync();
    });
    const reconcile = window.setInterval(sync, 3000);
    const renewMedia = window.setInterval(() => autoRefreshActions.current?.poll(), 10 * 60_000);
    window.addEventListener("focus", sync);
    sync();
    return () => {
      unwatch();
      window.clearInterval(reconcile);
      window.clearInterval(renewMedia);
      window.removeEventListener("focus", sync);
      autoRefreshActions.current?.cancel();
    };
  }, [id, liveEnabled]);

  const changeAutoRefresh = (enabled: boolean) => {
    setLive(false);
    setAutoRefresh(enabled);
    try {
      localStorage.setItem(AUTO_REFRESH_PREFERENCE_KEY, enabled ? "on" : "off");
    } catch {
      return;
    }
  };

  const changeRefreshInterval = (seconds: number) => {
    setRefreshSeconds(seconds);
    try {
      localStorage.setItem(REFRESH_PREFERENCE_KEY, String(seconds));
    } catch {
      return;
    }
  };

  const leave = async () => {
    setLeaving(true);
    try {
      await canvasSharing.remove(id, "me");
      setCanvas(null);
      navigate(back, { replace: true });
    } catch {
      toast.error("退出共享失败，请重试");
      setLeaving(false);
    }
  };

  const refresh = () => setReload((value) => value + 1);
  if (!canvas)
    return (
      <main className="grid h-svh place-content-center justify-items-center gap-4 px-4 text-center">
        <p role="status">
          {error === "revoked" && "你已失去这张画布的访问权限。"}
          {error === "access" && "画布不可访问，请确认共享权限。"}
          {error === "network" && "加载画布失败，请刷新重试。"}
          {!error && "正在加载画布…"}
        </p>
        <div className="flex items-center gap-2">
          <Button variant="ghost" nativeButton={false} render={<Link to={back} />}>
            共享给我
          </Button>
          {error && (
            <Button variant="outline" onClick={refresh}>
              刷新
            </Button>
          )}
        </div>
      </main>
    );
  return (
    <SharedCanvasScene
      graph={canvas.graph}
      overlay={(minimap, setMinimap) => (
        <>
          {liveEnabled && canvas.collaborationEpoch && (
            <CanvasGenerationProgressLayer canvasId={id} epoch={canvas.collaborationEpoch} />
          )}
          <WorkspaceChrome
            shared
            viewPreferences={liveAvailable ? "viewer" : undefined}
            onOpenShortcuts={liveAvailable ? openShortcuts : undefined}
            minimap={minimap}
            onMinimapChange={setMinimap}
            topLeft={
              <div className="grid justify-items-start gap-2">
                <SharedTopLeftBar
                  title={canvas.title}
                  back={back}
                  leaving={leaving}
                  autoRefresh={autoRefresh}
                  refreshSeconds={refreshSeconds}
                  onAutoRefreshChange={changeAutoRefresh}
                  live={liveEnabled}
                  onLiveChange={liveAvailable ? setLive : undefined}
                  onRefreshIntervalChange={changeRefreshInterval}
                  onRefresh={refresh}
                  onLeave={() => void leave()}
                />
                {error === "network" && (
                  <span
                    role="status"
                    className="bg-background text-muted-foreground rounded-full px-3 py-1 text-sm"
                  >
                    刷新失败，请重试。
                  </span>
                )}
              </div>
            }
          />
          {liveAvailable && (
            <ShortcutsDialog open={shortcutsOpen} onOpenChange={setShortcutsOpen} scope="viewer" />
          )}
        </>
      )}
    />
  );
}

function isAccessDenied(failure: unknown) {
  return failure instanceof ApiError && [401, 403, 404, 410].includes(failure.status);
}

async function readSharedCanvas(
  id: string,
  current: CanvasDetailDto | null,
  signal: AbortSignal,
  mode: "snapshot" | "delta",
) {
  if (mode === "delta" && current?.collaborationEpoch) {
    const result = await catchUpSharedCanvas(id, current, current.collaborationEpoch, signal);
    if (result !== undefined) return result;
  }
  const row = await service.get<SharedCanvasResponse>(
    `/canvas/${id}/reader`,
    current ? { revision: current.version } : undefined,
    { signal, silent: true },
  );
  return "unchanged" in row ? null : mapCanvasProject(row);
}

async function catchUpSharedCanvas(
  id: string,
  current: CanvasDetailDto,
  epoch: CanvasVersion,
  signal: AbortSignal,
) {
  let version = current.version;
  let graph = current.graph;
  const hasReplayBudget = createCollaborationReplayBudget(graph);
  try {
    for (let page = 0; page < 10; page++) {
      const reply = await replayCollaboration(id, epoch, version, signal);
      if (reply.snapshotRequired || reply.epoch !== epoch) return undefined;
      for (const event of reply.events) {
        if (event.revision !== nextCanvasVersion(version) || event.epoch !== epoch)
          return undefined;
        graph = applyCollaborationChanges(graph, event.changes ?? []);
        version = event.revision;
      }
      if (version === reply.revision) {
        const title = reply.title ?? current.title;
        return version === current.version && title === current.title
          ? null
          : { ...current, title, version, graph };
      }
      if (reply.events.length === 0 || !hasReplayBudget(reply.events)) return undefined;
    }
  } catch (failure) {
    if (!(failure instanceof ApiError) || failure.code !== 30018) throw failure;
  }
  return undefined;
}
