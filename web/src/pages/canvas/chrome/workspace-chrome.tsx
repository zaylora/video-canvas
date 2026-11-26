import type { ReactNode } from "react";

import { ChromeZone } from "@/components/canvas/chrome/chrome";
import { cn } from "@/lib/utils";

import { StatsBar } from "./stats-bar";
import { ViewControls } from "./view-controls";

export function WorkspaceChrome({
  topLeft,
  topRight,
  toolbar,
  assistant,
  reserveForPet,
  shared,
  minimap,
  onMinimapChange,
  onOpenShortcuts,
}: {
  topLeft: ReactNode;
  topRight?: ReactNode;
  toolbar?: ReactNode;
  assistant?: ReactNode;
  reserveForPet?: boolean;
  shared?: boolean;
  minimap: boolean;
  onMinimapChange: (open: boolean) => void;
  onOpenShortcuts?: () => void;
}) {
  return (
    <>
      <ChromeZone position="top-left">{topLeft}</ChromeZone>
      {topRight && <ChromeZone position="top-right">{topRight}</ChromeZone>}
      {toolbar && <ChromeZone position="left-center">{toolbar}</ChromeZone>}
      <ChromeZone position="bottom-left" className={shared ? undefined : "max-md:hidden"}>
        <ViewControls
          minimap={minimap}
          onMinimapChange={onMinimapChange}
          onOpenShortcuts={onOpenShortcuts}
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
