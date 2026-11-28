import type { ReactNode } from "react";

import { ChromeZone } from "@/components/canvas/chrome/chrome";
import { useCanvasViewShortcuts } from "@/components/canvas-editor/use-canvas-view-shortcuts";
import { cn } from "@/lib/utils";

import { StatsBar } from "./stats-bar";
import { ViewControls } from "./view-controls";

export function WorkspaceChrome({
  topLeft,
  topRight,
  toolbar,
  viewToolControls,
  assistant,
  reserveForPet,
  shared,
  viewPreferences,
  minimap,
  onMinimapChange,
  onOpenShortcuts,
  onLocalViewChange,
}: {
  topLeft: ReactNode;
  topRight?: ReactNode;
  toolbar?: ReactNode;
  viewToolControls?: ReactNode;
  assistant?: ReactNode;
  reserveForPet?: boolean;
  shared?: boolean;
  viewPreferences?: "editor" | "viewer";
  minimap: boolean;
  onMinimapChange: (open: boolean) => void;
  onOpenShortcuts?: () => void;
  onLocalViewChange?: () => void;
}) {
  useCanvasViewShortcuts({ openShortcuts: onOpenShortcuts, enabled: viewPreferences === "viewer" });
  return (
    <>
      <ChromeZone position="top-left">{topLeft}</ChromeZone>
      {topRight && <ChromeZone position="top-right">{topRight}</ChromeZone>}
      {toolbar && <ChromeZone position="left-center">{toolbar}</ChromeZone>}
      <ChromeZone
        position="bottom-left"
        className={shared || viewPreferences ? undefined : "max-md:hidden"}
      >
        <ViewControls
          minimap={minimap}
          preferences={viewPreferences}
          toolControls={viewToolControls}
          onMinimapChange={onMinimapChange}
          onOpenShortcuts={onOpenShortcuts}
          onLocalViewChange={onLocalViewChange}
        />
      </ChromeZone>
      <ChromeZone position="bottom-right">
        {assistant}
        <div className={cn("max-md:hidden", reserveForPet && "mr-24")}>
          <StatsBar />
        </div>
      </ChromeZone>
    </>
  );
}
