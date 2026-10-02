import NumberFlow from "@number-flow/react";
import { useReactFlow, useStore } from "@xyflow/react";
import { CircleDashed, CircleHelp, Grid2x2, Grip, Map as MapIcon, Search } from "lucide-react";

import {
  ChromeButton,
  ChromePill,
  ChromeSegment,
  ChromeSeparator,
  ChromeTooltip,
} from "@/components/canvas/chrome/chrome";
import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuItem,
  DropdownMenuSeparator,
  DropdownMenuShortcut,
  DropdownMenuTrigger,
} from "@/components/ui/dropdown-menu";
import { useSettingsStore, type CanvasBackground } from "@/store";

import { MOD } from "./keys";

/** 视口动画时长，和设计稿的画布飞行保持一致 */
export const VIEWPORT_DURATION = 300;

/** 左下角：背景样式、缩放、小地图、快捷键帮助 */
export function ViewControls({
  minimap,
  onMinimapChange,
  onOpenShortcuts,
}: {
  minimap: boolean;
  onMinimapChange: (minimap: boolean) => void;
  onOpenShortcuts: () => void;
}) {
  const zoom = useStore((state) => state.transform[2]);
  const { zoomIn, zoomOut, zoomTo, fitView } = useReactFlow();
  const background = useSettingsStore((state) => state.background);
  const updateSettings = useSettingsStore((state) => state.updateSettings);
  const segmentValue: CanvasBackground = background === "cross" ? "lines" : background;

  return (
    <>
      <ChromePill>
        <ChromeSegment
          id="canvas-background"
          value={segmentValue}
          onValueChange={(value) => updateSettings("background", value)}
          items={[
            { value: "dots", label: "点阵背景", icon: <Grip /> },
            { value: "lines", label: "网格背景", icon: <Grid2x2 /> },
            { value: "none", label: "无背景", icon: <CircleDashed /> },
          ]}
        />
        <ChromeSeparator />
        <DropdownMenu modal={false}>
          <DropdownMenuTrigger
            render={<ChromeButton aria-label="缩放" className="min-w-18 font-mono text-xs" />}
          >
            <Search className="size-3.5!" />
            <NumberFlow value={Math.round(zoom * 100)} suffix="%" className="tabular-nums" />
          </DropdownMenuTrigger>
          <DropdownMenuContent side="top" sideOffset={12} className="w-48">
            <DropdownMenuItem onClick={() => void zoomIn({ duration: VIEWPORT_DURATION })}>
              放大
              <DropdownMenuShortcut>{MOD}+</DropdownMenuShortcut>
            </DropdownMenuItem>
            <DropdownMenuItem onClick={() => void zoomOut({ duration: VIEWPORT_DURATION })}>
              缩小
              <DropdownMenuShortcut>{MOD}−</DropdownMenuShortcut>
            </DropdownMenuItem>
            <DropdownMenuSeparator />
            <DropdownMenuItem
              onClick={() => void fitView({ duration: VIEWPORT_DURATION, padding: 0.2 })}
            >
              适应画布
              <DropdownMenuShortcut>⇧1</DropdownMenuShortcut>
            </DropdownMenuItem>
            <DropdownMenuItem onClick={() => void zoomTo(1, { duration: VIEWPORT_DURATION })}>
              缩放到 100%
              <DropdownMenuShortcut>⇧0</DropdownMenuShortcut>
            </DropdownMenuItem>
          </DropdownMenuContent>
        </DropdownMenu>
        <ChromeTooltip label={minimap ? "收起小地图" : "显示小地图"}>
          <ChromeButton
            aria-label="小地图"
            aria-pressed={minimap}
            active={minimap}
            onClick={() => onMinimapChange(!minimap)}
          >
            <MapIcon />
          </ChromeButton>
        </ChromeTooltip>
      </ChromePill>
      <ChromePill>
        <ChromeTooltip label="快捷键" shortcut="?">
          <ChromeButton aria-label="快捷键" onClick={onOpenShortcuts}>
            <CircleHelp />
          </ChromeButton>
        </ChromeTooltip>
      </ChromePill>
    </>
  );
}
