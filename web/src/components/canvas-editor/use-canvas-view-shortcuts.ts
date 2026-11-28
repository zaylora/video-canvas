import { useEffect } from "react";
import { useReactFlow } from "@xyflow/react";

import { isCanvasShortcutBusy } from "./keys";
import { VIEWPORT_DURATION } from "./viewport";

export function useCanvasViewShortcuts({
  openShortcuts,
  onLocalViewChange,
  enabled = true,
}: {
  openShortcuts?: () => void;
  onLocalViewChange?: () => void;
  enabled?: boolean;
}) {
  const { fitView, zoomTo, zoomIn, zoomOut } = useReactFlow();

  useEffect(() => {
    if (!enabled) return;

    const onKeyDown = (event: KeyboardEvent) => {
      if (isCanvasShortcutBusy(event)) return;
      const mod = event.metaKey || event.ctrlKey;
      const options = { duration: VIEWPORT_DURATION };

      if (mod && ["=", "+", "-"].includes(event.key)) {
        event.preventDefault();
        onLocalViewChange?.();
        void (event.key === "-" ? zoomOut(options) : zoomIn(options));
      } else if (!mod && !event.altKey && event.shiftKey && event.code === "Digit1") {
        onLocalViewChange?.();
        void fitView({ ...options, padding: 0.2 });
      } else if (!mod && !event.altKey && event.shiftKey && event.code === "Digit0") {
        onLocalViewChange?.();
        void zoomTo(1, options);
      } else if (!mod && !event.altKey && event.key === "?") {
        openShortcuts?.();
      }
    };

    window.addEventListener("keydown", onKeyDown);
    return () => window.removeEventListener("keydown", onKeyDown);
  }, [enabled, fitView, onLocalViewChange, openShortcuts, zoomIn, zoomOut, zoomTo]);
}
