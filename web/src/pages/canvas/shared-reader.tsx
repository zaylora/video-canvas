import { useEffect, useRef, useState } from "react";
import { useParams, Link, useNavigate } from "react-router";
import { toast } from "sonner";
import { Button } from "@/components/ui/button";
import { canvasSharing } from "@/api/drama/share";
import { mapCanvasProject } from "@/api/canvas";
import type { BackendCanvasProjectDto, CanvasDetailDto } from "@/api/canvas/type";
import { ApiError } from "@/utils/requests/request";
import service from "@/utils/requests/service";
import { SharedCanvasScene } from "@/components/canvas-editor/shared-scene";
import { usePageTitle } from "@/hooks/use-page-title";
import { canvasPageTitle } from "@/utils/page-title";

import {
  DEFAULT_SHARED_REFRESH_SECONDS,
  MAX_SHARED_REFRESH_SECONDS,
} from "./chrome/shared-refresh-control";
import { SharedTopLeftBar } from "./chrome/top-left-bar";
import { WorkspaceChrome } from "./chrome/workspace-chrome";

const back = "/canvases?scope=shared-canvas";
const REFRESH_PREFERENCE_KEY = "canvas:shared-refresh-seconds";
const AUTO_REFRESH_PREFERENCE_KEY = "canvas:shared-auto-refresh";
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
  const [error, setError] = useState<"access" | "network" | null>(null);
  const [reload, setReload] = useState(0);
  const [leaving, setLeaving] = useState(false);
  const [autoRefresh, setAutoRefresh] = useState(readAutoRefresh);
  const [refreshSeconds, setRefreshSeconds] = useState(readRefreshSeconds);
  const version = useRef<number | undefined>(undefined);
  const autoRefreshActions = useRef<{
    poll: () => void;
    focus: () => void;
    cancel: () => void;
  } | null>(null);
  usePageTitle(canvasPageTitle(canvas?.title ?? "共享画布"));
  useEffect(() => {
    let pending: { controller: AbortController; automatic: boolean } | undefined;
    let accessEnded = false;
    const load = (automatic: boolean) => {
      pending?.controller.abort();
      const request = new AbortController();
      pending = { controller: request, automatic };
      void service
        .get<SharedCanvasResponse>(
          `/canvas/${id}/reader`,
          version.current === undefined ? undefined : { revision: version.current },
          { signal: request.signal, silent: true },
        )
        .then(
          (row) => {
            if (request.signal.aborted) return;
            accessEnded = false;
            setError(null);
            if ("unchanged" in row) return;
            const next = mapCanvasProject(row);
            version.current = next.version;
            setCanvas(next);
          },
          (failure: unknown) => {
            if (request.signal.aborted) return;
            const denied =
              failure instanceof ApiError && [401, 403, 404, 410].includes(failure.status);
            if (denied) {
              accessEnded = true;
              version.current = undefined;
              setCanvas(null);
            }
            setError(denied ? "access" : "network");
          },
        )
        .finally(() => {
          if (pending?.controller === request) pending = undefined;
        });
    };
    load(false);
    autoRefreshActions.current = {
      poll: () => {
        if (!document.hidden && !accessEnded && !pending) load(true);
      },
      focus: () => load(true),
      cancel: () => {
        if (!pending?.automatic) return;
        pending.controller.abort();
        pending = undefined;
      },
    };
    return () => {
      pending?.controller.abort();
      autoRefreshActions.current = null;
    };
  }, [id, reload]);

  useEffect(() => {
    if (!autoRefresh) return;
    const focus = () => autoRefreshActions.current?.focus();
    window.addEventListener("focus", focus);
    return () => {
      window.removeEventListener("focus", focus);
      autoRefreshActions.current?.cancel();
    };
  }, [autoRefresh]);

  useEffect(() => {
    if (!autoRefresh) return;
    const interval = window.setInterval(
      () => autoRefreshActions.current?.poll(),
      refreshSeconds * 1000,
    );
    return () => window.clearInterval(interval);
  }, [autoRefresh, refreshSeconds]);

  const changeAutoRefresh = (enabled: boolean) => {
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
          {error === "access"
            ? "画布不可访问，请确认共享权限。"
            : error === "network"
              ? "加载画布失败，请刷新重试。"
              : "正在加载画布…"}
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
        <WorkspaceChrome
          shared
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
      )}
    />
  );
}
