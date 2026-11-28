import type { CanvasVersion } from "@/utils/canvas/collaboration-version";

import { useEffect, useRef, useState } from "react";
import { AtSign } from "lucide-react";

import { getCanvasMentions, type CanvasMention } from "@/api/canvas/collaboration";
import { ChromeButton } from "@/components/canvas/chrome/chrome";
import { ApiError } from "@/utils/requests/request";
import { getCurrentUserId } from "@/utils/storage/user-id";

function readCursor(key: string) {
  try {
    return localStorage.getItem(key) ?? undefined;
  } catch {
    return undefined;
  }
}

/** 显示本账号尚未打开的节点评论提及。 */
export function CanvasMentionNotice({
  canvasId,
  epoch,
  open,
}: {
  canvasId: string;
  epoch: CanvasVersion;
  open: (id: string) => void;
}) {
  const [key] = useState(() =>
    JSON.stringify(["canvas-mention", getCurrentUserId(), canvasId, epoch]),
  );
  const [items, setItems] = useState<CanvasMention[]>([]);
  const cursor = useRef<string>(undefined);

  useEffect(() => {
    const controller = new AbortController();
    cursor.current = readCursor(key);
    let loading = false;
    const load = () => {
      if (loading || controller.signal.aborted || document.visibilityState !== "visible") return;
      const after = cursor.current;
      loading = true;
      void getCanvasMentions(canvasId, epoch, after, controller.signal)
        .then(({ items: next }) => {
          if (!controller.signal.aborted && cursor.current === after) setItems(next);
        })
        .catch((error: unknown) => {
          if (!controller.signal.aborted && error instanceof ApiError && error.status === 404)
            setItems([]);
        })
        .finally(() => {
          loading = false;
        });
    };
    load();
    const timer = window.setInterval(load, 5000);
    document.addEventListener("visibilitychange", load);
    window.addEventListener("focus", load);
    return () => {
      controller.abort();
      window.clearInterval(timer);
      document.removeEventListener("visibilitychange", load);
      window.removeEventListener("focus", load);
    };
  }, [canvasId, epoch, key]);

  const first = items[0];
  if (!first) return null;
  const label = items.length === 20 ? "20+" : String(items.length);
  return (
    <ChromeButton
      aria-label={`查看提及提醒（${label}）`}
      title="查看提及提醒"
      onClick={() => {
        cursor.current = first.id;
        setItems(items.slice(1));
        open(first.nodeId);
        try {
          localStorage.setItem(key, first.id);
        } catch {
          return;
        }
      }}
    >
      <AtSign />
      <span className="text-xs">{label}</span>
    </ChromeButton>
  );
}
